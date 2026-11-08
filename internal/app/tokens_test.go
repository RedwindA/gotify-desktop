package app

import (
	"context"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	goruntime "runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gotify-desktop/internal/conn"
	"gotify-desktop/internal/secret"
	"gotify-desktop/internal/store"
)

// flakyTokens is a token store whose reads and deletions fail while their errors are set.
type flakyTokens struct {
	secret.Memory
	mu                sync.Mutex
	getErr, deleteErr error
	gets              atomic.Int32
}

func (f *flakyTokens) fail(get, del error) {
	f.mu.Lock()
	f.getErr, f.deleteErr = get, del
	f.mu.Unlock()
}

func (f *flakyTokens) Get(id int64) (string, error) {
	f.gets.Add(1)
	f.mu.Lock()
	err := f.getErr
	f.mu.Unlock()
	if err != nil {
		return "", err
	}
	return f.Memory.Get(id)
}

func (f *flakyTokens) Delete(id int64) error {
	f.mu.Lock()
	err := f.deleteErr
	f.mu.Unlock()
	if err != nil {
		return err
	}
	return f.Memory.Delete(id)
}

var errLocked = errors.New("tokens.json: the file is locked")

// startWithTokens opens an app over a database holding sv, whose token is
// "tok" in tokens, and waits until reading it failed.
func startWithTokens(t *testing.T, sv store.Server, tokens *flakyTokens, cfg conn.Config) (*App, int64) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(dir + "/gotify.db")
	if err != nil {
		t.Fatal(err)
	}
	id, _ := st.AddServer(sv)
	st.Close()
	tokens.Memory.Set(id, "tok")
	a, err := New(Options{DataDir: dir, CacheDir: t.TempDir(), Tokens: tokens, Notifier: &fakeNotifier{}, ConnConfig: cfg})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	eventually(t, "the token error", func() bool {
		sv, _ := a.Snapshot().Server(id)
		return sv.State == Unavailable
	})
	return a, id
}

func unauthorized(t *testing.T) *httptest.Server {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) }))
	t.Cleanup(s.Close)
	return s
}

func TestUnreadableTokenIsReadAgain(t *testing.T) {
	tokens := &flakyTokens{}
	tokens.fail(errLocked, nil)
	a, id := startWithTokens(t, store.Server{Name: "x", URL: unauthorized(t).URL}, tokens,
		conn.Config{MinBackoff: 10 * time.Millisecond, MaxBackoff: 40 * time.Millisecond})
	sv, _ := a.Snapshot().Server(id)
	if sv.NeedLogin || !strings.Contains(sv.Err, "locked") {
		t.Fatalf("while the store fails: %+v", sv)
	}
	eventually(t, "retries", func() bool { return tokens.gets.Load() >= 3 })
	tokens.fail(nil, nil)
	// The server refuses the token: the supervisor ran with it.
	eventually(t, "the token read again", func() bool {
		sv, _ := a.Snapshot().Server(id)
		return sv.State == AuthFailed && !sv.NeedLogin
	})
	n := tokens.gets.Load()
	time.Sleep(100 * time.Millisecond)
	if tokens.gets.Load() != n {
		t.Fatal("the token is still read again after it was")
	}
}

func TestKickAllReadsAnUnreadableTokenAtOnce(t *testing.T) {
	tokens := &flakyTokens{}
	tokens.fail(errLocked, nil)
	a, id := startWithTokens(t, store.Server{Name: "x", URL: unauthorized(t).URL}, tokens,
		conn.Config{MinBackoff: time.Hour, MaxBackoff: time.Hour})
	tokens.fail(nil, nil)
	a.KickAll()
	eventually(t, "the token read again", func() bool {
		sv, _ := a.Snapshot().Server(id)
		return sv.State == AuthFailed && !sv.NeedLogin
	})
}

func TestRemoveServerWithAnUnreadableTokenStore(t *testing.T) {
	var revoked atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			revoked.Add(1)
		}
		w.WriteHeader(401)
	}))
	t.Cleanup(srv.Close)
	tokens := &flakyTokens{}
	tokens.fail(errLocked, nil)
	a, id := startWithTokens(t, store.Server{Name: "x", URL: srv.URL, ClientID: 3}, tokens,
		conn.Config{MinBackoff: time.Hour, MaxBackoff: time.Hour})
	tokens.fail(errLocked, errLocked)
	if err := a.RemoveServer(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if svs, _ := a.st.Servers(); len(svs) != 0 {
		t.Fatalf("still stored: %+v", svs)
	}
	if _, ok := a.Snapshot().Server(id); ok {
		t.Fatal("still shown")
	}
	if revoked.Load() != 0 {
		t.Fatal("revoked a client without its token")
	}
}

func TestReloginWithAnUnreadableTokenStore(t *testing.T) {
	srv, _ := loginServer(t)
	tokens := &flakyTokens{}
	tokens.fail(errLocked, nil)
	a, id := startWithTokens(t, store.Server{Name: "x", URL: srv.URL, ClientID: 3, UserID: 1, UserName: "alice"}, tokens,
		conn.Config{MinBackoff: time.Hour, MaxBackoff: time.Hour})
	if err := a.Relogin(context.Background(), id, "alice", "pw"); err != nil {
		t.Fatal(err)
	}
	if tok, _ := tokens.Memory.Get(id); tok != "new" {
		t.Fatalf("token %q", tok)
	}
	a.mu.Lock()
	started := a.servers[id].sup != nil
	a.mu.Unlock()
	if !started {
		t.Fatal("no supervisor after the sign-in")
	}
}

func TestReloginRemovesItsTokenWhenTheOldOneWasUnreadable(t *testing.T) {
	srv, revoked := loginServer(t)
	tokens := &flakyTokens{}
	a, _, _ := newApp(t, tokens)
	sv := store.Server{URL: srv.URL, ClientID: 3, UserID: 1, UserName: "alice"}
	id, err := a.st.AddServer(sv)
	if err != nil {
		t.Fatal(err)
	}
	sv.ID = id
	tokens.Memory.Set(id, "old")
	tokens.fail(errLocked, nil)
	a.servers[id] = &runtime{sv: sv}
	a.st.Close()
	if err := a.Relogin(context.Background(), id, "alice", "pw"); err == nil {
		t.Fatal("expected database failure")
	}
	if _, err := tokens.Memory.Get(id); !errors.Is(err, secret.ErrNotFound) {
		t.Fatalf("the new token stayed: %v", err)
	}
	if revoked.Load() != 1 {
		t.Fatal("remote replacement client leaked")
	}
}

func TestCloseEndsTheRetriesOfTokens(t *testing.T) {
	before := goruntime.NumGoroutine()
	tokens := &flakyTokens{}
	tokens.fail(errLocked, nil)
	dir := t.TempDir()
	st, err := store.Open(dir + "/gotify.db")
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		st.AddServer(store.Server{Name: "x", URL: "http://127.0.0.1:1"})
	}
	st.Close()
	a, err := New(Options{DataDir: dir, CacheDir: t.TempDir(), Tokens: tokens, Notifier: &fakeNotifier{},
		ConnConfig: conn.Config{MinBackoff: time.Hour, MaxBackoff: time.Hour}})
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "every token read", func() bool { return tokens.gets.Load() >= 3 })
	closed := make(chan struct{})
	go func() { a.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close waited for the backoff")
	}
	eventually(t, "the goroutines to end", func() bool { return goruntime.NumGoroutine() <= before })
}

func TestUpdateServerKeepsSetsOrRemovesTheCA(t *testing.T) {
	tls := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(tls.Close)
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: tls.Certificate().Raw})
	a, _, _ := newApp(t, &secret.Memory{})
	id, err := a.st.AddServer(store.Server{Name: "x", URL: "https://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	a.servers[id] = &runtime{sv: store.Server{ID: id, Name: "x", URL: "https://127.0.0.1:1"}}
	ctx := context.Background()
	for _, c := range []struct {
		change CAChange
		want   string
	}{
		{CAChange{Action: CASet, PEM: ca}, string(ca)},
		{CAChange{Action: CAKeep}, string(ca)},
		{CAChange{Action: CAKeep, PEM: []byte("ignored")}, string(ca)},
		{CAChange{Action: CARemove}, ""},
	} {
		if err := a.UpdateServer(ctx, id, "x", false, c.change); err != nil {
			t.Fatal(err)
		}
		if sv, _ := a.st.Server(id); sv.CACertPEM != c.want {
			t.Fatalf("after %+v: %q", c.change.Action, sv.CACertPEM)
		}
	}
	if err := a.UpdateServer(ctx, id, "x", false, CAChange{Action: CASet, PEM: []byte("junk")}); err == nil {
		t.Fatal("a bad CA was stored")
	}
}
