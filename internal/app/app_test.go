package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gotify-desktop/internal/conn"
	"gotify-desktop/internal/gotify"
	"gotify-desktop/internal/itest/harness"
	"gotify-desktop/internal/notify"
	"gotify-desktop/internal/secret"
	"gotify-desktop/internal/store"
)

type fakeNotifier struct {
	mu      sync.Mutex
	shown   []notify.Notification
	removed []string
}

func (f *fakeNotifier) Supported() bool { return true }
func (f *fakeNotifier) Show(n notify.Notification) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.shown = append(f.shown, n)
	return nil
}
func (f *fakeNotifier) Remove(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, id)
}
func (f *fakeNotifier) OnActivate(func(string)) {}
func (f *fakeNotifier) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.shown)
}

func newApp(t *testing.T, tokens secret.Tokens) (*App, *fakeNotifier, *atomic.Int32) {
	t.Helper()
	fn := &fakeNotifier{}
	var changes atomic.Int32
	a, err := New(Options{
		DataDir: t.TempDir(), CacheDir: t.TempDir(), Tokens: tokens, Notifier: fn,
		ConnConfig: conn.Config{MinBackoff: 100 * time.Millisecond, MaxBackoff: time.Second},
		OnChange:   func() { changes.Add(1) },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	return a, fn, &changes
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("timeout waiting for %s", what)
}

func TestMissingTokenNeedsLogin(t *testing.T) {
	dir := t.TempDir()
	st, _ := store.Open(dir + "/gotify.db")
	id, _ := st.AddServer(store.Server{Name: "x", URL: "http://127.0.0.1:1"})
	st.Close()
	fn := &fakeNotifier{}
	a, err := New(Options{DataDir: dir, CacheDir: t.TempDir(), Tokens: &secret.Memory{}, Notifier: fn})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	sv, ok := a.Snapshot().Server(id)
	if !ok || sv.State != conn.AuthFailed || !sv.NeedLogin || sv.Err == "" {
		t.Fatalf("%+v", sv)
	}
}

func TestSettingsPersist(t *testing.T) {
	a, _, changes := newApp(t, &secret.Memory{})
	s := a.Settings()
	if !s.HighBypassesDND || s.BurstMax != 3 {
		t.Fatalf("defaults: %+v", s)
	}
	s.DND, s.DNDStart, s.DNDEnd = true, 60, 120
	if err := a.SetSettings(s); err != nil {
		t.Fatal(err)
	}
	if got := a.loadSettings(); !got.DND || got.DNDStart != 60 || got.DNDEnd != 120 || !got.HighBypassesDND {
		t.Fatalf("%+v", got)
	}
	if changes.Load() == 0 {
		t.Fatal("OnChange not called")
	}
}

func TestExplain(t *testing.T) {
	for err, want := range map[error]string{
		gotify.ErrUnauthorized:                 "Wrong username or password",
		context.DeadlineExceeded:               "The server took too long to answer",
		errors.New("x509: certificate signed"): "The server's TLS certificate is not trusted. Add its CA certificate or skip verification under Advanced.",
		errors.New("boom"):                     "boom",
	} {
		if got := Explain(err); got != want {
			t.Errorf("%v -> %q", err, got)
		}
	}
}

func TestServerLifecycleAgainstRealServer(t *testing.T) {
	harness.RequireIT(t)
	srv := harness.StartServer(t)
	ctx := context.Background()
	tokens := &secret.Memory{}
	a, fn, _ := newApp(t, tokens)

	if _, err := a.AddServer(ctx, ServerInput{URL: srv.URL(), User: harness.AdminUser, Pass: "wrong"}); !errors.Is(err, gotify.ErrUnauthorized) {
		t.Fatalf("bad password: %v", err)
	}
	app := srv.NewApp("Backup")
	srv.Post(app, 2, "old")
	id, err := a.AddServer(ctx, ServerInput{URL: srv.URL(), User: harness.AdminUser, Pass: harness.AdminPass})
	if err != nil {
		t.Fatal(err)
	}
	if tok, err := tokens.Get(id); err != nil || tok == "" {
		t.Fatalf("token not stored: %v", err)
	}
	eventually(t, "first import", func() bool { _, init, _ := a.st.LastSeen(id); return init })
	sv, _ := a.Snapshot().Server(id)
	if sv.Name == "" || sv.State != conn.Connected || len(sv.Apps) == 0 || sv.Unread != 2 {
		t.Fatalf("%+v", sv)
	}
	if fn.count() != 0 {
		t.Fatal("history import must not notify")
	}

	srv.Post(app, 1, "live")
	eventually(t, "notification", func() bool { return fn.count() == 1 })
	msgs, _ := a.Messages(store.MessageQuery{ServerID: id})
	if len(msgs) != 3 || msgs[0].Title != "live" {
		t.Fatalf("%+v", msgs)
	}

	victim := msgs[0].ID
	if err := a.DeleteMessage(ctx, id, victim); err != nil {
		t.Fatal(err)
	}
	var remote struct{ Messages []gotify.Message }
	srv.API("GET", "/message", nil, &remote)
	for _, m := range remote.Messages {
		if m.ID == victim {
			t.Fatal("message still on the server")
		}
	}
	if left, _ := a.Messages(store.MessageQuery{ServerID: id}); len(left) != 2 {
		t.Fatalf("local copy not deleted: %d", len(left))
	}
	if err := a.DeleteMessage(ctx, id, victim); err != nil {
		t.Fatalf("deleting twice should succeed (404): %v", err)
	}

	a.MarkAllRead(id, 0)
	if sv, _ = a.Snapshot().Server(id); sv.Unread != 0 {
		t.Fatalf("unread %d", sv.Unread)
	}
	act := a.HandleActivation(fmt.Sprintf("s%d-m%d", id, msgs[1].ID))
	if act.Kind != notify.KindMessage || act.MessageID != msgs[1].ID || act.AppID == 0 {
		t.Fatalf("%+v", act)
	}

	srv.API("DELETE", fmt.Sprintf("/client/%d", must(a.st.Server(id)).ClientID), nil, nil)
	eventually(t, "auth failure", func() bool { sv, _ := a.Snapshot().Server(id); return sv.State == conn.AuthFailed })
	if err := a.Relogin(ctx, id, harness.AdminUser, "wrong"); !errors.Is(err, gotify.ErrUnauthorized) {
		t.Fatalf("relogin with bad password: %v", err)
	}
	if err := a.Relogin(ctx, id, harness.AdminUser, harness.AdminPass); err != nil {
		t.Fatal(err)
	}
	eventually(t, "reconnect", func() bool { sv, _ := a.Snapshot().Server(id); return sv.State == conn.Connected })
	if err := a.UpdateServer(ctx, id, "Renamed", false, nil); err != nil {
		t.Fatal(err)
	}
	eventually(t, "rename and reconnect", func() bool {
		sv, _ := a.Snapshot().Server(id)
		return sv.Name == "Renamed" && sv.State == conn.Connected
	})

	if err := a.RemoveServer(ctx, id); err != nil {
		t.Fatal(err)
	}
	if len(a.Snapshot().Servers) != 0 {
		t.Fatal("server still in snapshot")
	}
	if _, err := tokens.Get(id); err == nil {
		t.Fatal("token not deleted")
	}
	if _, err := a.st.Server(id); err == nil {
		t.Fatal("server not deleted from the store")
	}
	if left, _ := a.st.Messages(store.MessageQuery{ServerID: id}); len(left) != 0 {
		t.Fatal("messages not deleted")
	}
	srv.Post(app, 1, "after")
	time.Sleep(500 * time.Millisecond)
	if fn.count() != 1 {
		t.Fatal("removed server still notifies")
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
