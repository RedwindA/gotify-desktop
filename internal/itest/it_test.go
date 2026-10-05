package itest

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"gotify-desktop/internal/conn"
	"gotify-desktop/internal/gotify"
	"gotify-desktop/internal/store"
)

type env struct {
	srv    *server
	proxy  *proxy
	client *gotify.Client
	id     uint
	sup    *conn.Supervisor
	rec    *recorder
	st     *store.Store
	sid    int64
}

func newEnv(t *testing.T, cfg conn.Config) *env {
	t.Helper()
	requireIT(t)
	e := &env{srv: startServer(t)}
	e.proxy = startProxy(t, e.srv.URL()[len("http://"):])
	e.client, e.id = login(t, e.proxy.URL())
	st, err := store.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	e.st = st
	e.sid, _ = st.AddServer(store.Server{Name: "it", URL: e.proxy.URL()})
	e.rec = newRecorder()
	e.sup = conn.New(e.sid, e.client, st, e.rec.sink, cfg)
	t.Cleanup(e.sup.Stop)
	return e
}

func fast() conn.Config {
	return conn.Config{PingInterval: time.Second, PongTimeout: time.Second, MinBackoff: 100 * time.Millisecond, MaxBackoff: time.Second}
}

func (e *env) waitReady(t *testing.T, from int) int {
	t.Helper()
	_, n := e.rec.wait(t, from, state(conn.Connected))
	_, n = e.rec.wait(t, n, func(ev conn.Event) bool { return ev.Kind == conn.EventApps })
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if _, init, _ := e.st.LastSeen(e.sid); init {
			return n
		}
	}
	t.Fatal("first catch-up did not finish")
	return n
}

func TestLoginAndCurrentUser(t *testing.T) {
	requireIT(t)
	srv := startServer(t)
	ctx := context.Background()
	tok, id, err := gotify.Login(ctx, srv.URL(), adminUser, adminPass, "desk", gotify.Options{})
	if err != nil || tok == "" || id == 0 {
		t.Fatalf("%q %d %v", tok, id, err)
	}
	c, _ := gotify.New(srv.URL(), tok, gotify.Options{})
	u, err := c.CurrentUser(ctx)
	if err != nil || u.Name != adminUser || !u.Admin || u.ClientID != id {
		t.Fatalf("%+v %v", u, err)
	}
	if v, err := c.Version(ctx); err != nil || v.Version == "" {
		t.Fatalf("%+v %v", v, err)
	}
	if _, _, err := gotify.Login(ctx, srv.URL(), adminUser, "wrong", "desk", gotify.Options{}); !errors.Is(err, gotify.ErrUnauthorized) {
		t.Fatalf("got %v", err)
	}
	bad, _ := gotify.New(srv.URL(), "nope", gotify.Options{})
	if _, err := bad.Dial(ctx); !errors.Is(err, gotify.ErrUnauthorized) {
		t.Fatalf("dial: %v", err)
	}
	if err := c.DeleteClient(ctx, id); err == nil {
		t.Log("DeleteClient succeeded (session elevated)")
	} else {
		var he *gotify.HTTPError
		if !errors.As(err, &he) || he.Status != 403 {
			t.Fatalf("DeleteClient: %v", err)
		}
	}
}

func TestLiveMessageDeliveredOnce(t *testing.T) {
	e := newEnv(t, fast())
	app := e.srv.newApp("live")
	e.sup.Start()
	n := e.waitReady(t, 0)
	e.srv.post(app, 1, "live")
	ev, n := e.rec.wait(t, n, msgs)
	if ev.Silent || ev.CatchUp || !reflect.DeepEqual(messageBodies(ev.Messages), []string{"live-1"}) {
		t.Fatalf("%+v", ev)
	}
	time.Sleep(time.Second)
	for _, x := range e.rec.all()[n:] {
		if msgs(x) {
			t.Fatalf("duplicate delivery: %+v", x)
		}
	}
	if apps, _ := e.st.Apps(e.sid); len(apps) != 1 || apps[0].Name != "live" {
		t.Fatalf("apps not synced: %+v", apps)
	}
}

func TestFirstConnectImportsSilently(t *testing.T) {
	e := newEnv(t, fast())
	app := e.srv.newApp("hist")
	e.srv.post(app, 5, "old")
	e.sup.Start()
	ev, n := e.rec.wait(t, 0, msgs)
	if !ev.Silent || !ev.CatchUp || len(ev.Messages) != 5 || ev.Messages[0].Message != "old-1" || ev.Messages[4].Message != "old-5" {
		t.Fatalf("%+v", ev)
	}
	e.srv.post(app, 1, "new")
	ev, _ = e.rec.wait(t, n, msgs)
	if ev.Silent || len(ev.Messages) != 1 {
		t.Fatalf("%+v", ev)
	}
}

func TestCatchUpAfterServerRestart(t *testing.T) {
	cfg := fast()
	cfg.MinBackoff, cfg.MaxBackoff = 200*time.Millisecond, 500*time.Millisecond
	e := newEnv(t, cfg)
	app := e.srv.newApp("restart")
	e.sup.Start()
	n := e.waitReady(t, 0)
	e.srv.post(app, 2, "before")
	_, n = e.rec.wait(t, n, msgs)
	if len(e.rec.all()[n-1].Messages) < 1 {
		t.Fatal("no live message")
	}
	if got := len(allBodies(e.rec)); got < 2 {
		_, n = e.rec.wait(t, n, msgs)
	}

	e.proxy.Refuse(true)
	e.srv.restart()
	e.rec.wait(t, n, state(conn.Backoff))
	e.srv.post(app, 3, "gap")
	e.proxy.Refuse(false)
	ev, _ := e.rec.wait(t, n, msgs)
	if !ev.CatchUp || ev.Silent || !reflect.DeepEqual(messageBodies(ev.Messages), []string{"gap-1", "gap-2", "gap-3"}) {
		t.Fatalf("%+v", ev)
	}
	time.Sleep(time.Second)
	seen := map[string]int{}
	for _, b := range allBodies(e.rec) {
		seen[b]++
		if seen[b] > 1 {
			t.Fatalf("duplicate %s", b)
		}
	}
	if len(seen) != 5 {
		t.Fatalf("delivered %v", seen)
	}
}

func allBodies(r *recorder) (out []string) {
	for _, ev := range r.all() {
		if msgs(ev) {
			out = append(out, messageBodies(ev.Messages)...)
		}
	}
	return
}

func TestPingsAcceptedByRealServer(t *testing.T) {
	cfg := fast()
	cfg.PingInterval = 300 * time.Millisecond
	cfg.PongTimeout = 2 * time.Second
	e := newEnv(t, cfg)
	e.sup.Start()
	n := e.waitReady(t, 0)
	time.Sleep(3 * time.Second)
	for _, x := range e.rec.all()[n:] {
		if x.Kind == conn.EventState {
			t.Fatalf("connection dropped: %+v", x)
		}
	}
}

func TestBlackholeDetectedAndCaughtUp(t *testing.T) {
	e := newEnv(t, fast())
	app := e.srv.newApp("hole")
	e.sup.Start()
	n := e.waitReady(t, 0)
	e.proxy.Blackhole(true)
	ev, n := e.rec.wait(t, n, state(conn.Backoff))
	if ev.Err == nil {
		t.Fatal("backoff without error")
	}
	e.srv.post(app, 3, "hole")
	e.proxy.Blackhole(false)
	got, _ := e.rec.wait(t, n, msgs)
	if !got.CatchUp || !reflect.DeepEqual(messageBodies(got.Messages), []string{"hole-1", "hole-2", "hole-3"}) {
		t.Fatalf("%+v", got)
	}
	if e.sup.State() != conn.Connected {
		t.Logf("state %v", e.sup.State())
	}
}

func TestClientDeletedAuthFailedThenRecovers(t *testing.T) {
	e := newEnv(t, fast())
	app := e.srv.newApp("auth")
	e.sup.Start()
	n := e.waitReady(t, 0)
	e.srv.api("DELETE", "/client/"+uitoa(e.id), nil, nil)
	_, n = e.rec.wait(t, n, state(conn.AuthFailed))
	accepted := e.proxy.Accepted()
	time.Sleep(1500 * time.Millisecond)
	if e.proxy.Accepted() != accepted || e.sup.State() != conn.AuthFailed {
		t.Fatalf("retrying after auth failure: accepted %d -> %d, state %v", accepted, e.proxy.Accepted(), e.sup.State())
	}
	e.srv.post(app, 2, "after")
	c, _ := login(t, e.proxy.URL())
	e.sup.SetClient(c)
	e.sup.Kick()
	ev, _ := e.rec.wait(t, n, msgs)
	if !ev.CatchUp || !reflect.DeepEqual(messageBodies(ev.Messages), []string{"after-1", "after-2"}) {
		t.Fatalf("%+v", ev)
	}
}

func TestKickDuringBackoff(t *testing.T) {
	e := newEnv(t, conn.Config{MinBackoff: time.Hour, MaxBackoff: time.Hour})
	e.sup.Start()
	n := e.waitReady(t, 0)
	e.proxy.Refuse(true)
	ev, n := e.rec.wait(t, n, state(conn.Backoff))
	if time.Until(ev.RetryAt) < 30*time.Minute {
		t.Fatalf("RetryAt %v", ev.RetryAt)
	}
	e.proxy.Refuse(false)
	start := time.Now()
	e.sup.Kick()
	e.rec.wait(t, n, state(conn.Connected))
	if time.Since(start) > 5*time.Second {
		t.Fatal("kick did not reconnect promptly")
	}
}

func uitoa(v uint) string { return fmt.Sprint(v) }
