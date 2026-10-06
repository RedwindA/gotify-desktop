package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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
	eventually(t, "the token lookup", func() bool {
		sv, _ := a.Snapshot().Server(id)
		return sv.State != conn.Connecting
	})
	sv, ok := a.Snapshot().Server(id)
	if !ok || sv.State != conn.AuthFailed || !sv.NeedLogin || sv.Err == "" {
		t.Fatalf("%+v", sv)
	}
}

// stuckTokens is a token store that does not answer until released, then fails.
type stuckTokens struct {
	secret.Memory
	release chan struct{}
}

func (s *stuckTokens) Get(int64) (string, error) {
	<-s.release
	return "", errors.New("tokens.json: unexpected end of JSON input")
}

func TestStartupDoesNotWaitForTheTokens(t *testing.T) {
	dir := t.TempDir()
	st, _ := store.Open(dir + "/gotify.db")
	id, _ := st.AddServer(store.Server{Name: "x", URL: "http://127.0.0.1:1"})
	st.Close()
	tokens := &stuckTokens{release: make(chan struct{})}
	var changes atomic.Int32
	done := make(chan *App, 1)
	go func() {
		a, err := New(Options{DataDir: dir, CacheDir: t.TempDir(), Tokens: tokens, Notifier: &fakeNotifier{}, OnChange: func() { changes.Add(1) }})
		if err != nil {
			t.Error(err)
		}
		done <- a
	}()
	var a *App
	select {
	case a = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("New waited for the tokens")
	}
	defer a.Close()
	if sv, _ := a.Snapshot().Server(id); sv.State != conn.Connecting {
		t.Fatalf("while the token store is silent: %+v", sv)
	}
	close(tokens.release)
	eventually(t, "the token error", func() bool {
		sv, _ := a.Snapshot().Server(id)
		return sv.State == conn.Disconnected
	})
	sv, _ := a.Snapshot().Server(id)
	if sv.NeedLogin || !strings.Contains(sv.Err, "tokens.json") {
		t.Fatalf("a token store that fails is not a wrong password: %+v", sv)
	}
	if changes.Load() == 0 {
		t.Fatal("the change was not reported")
	}
}

func TestStartupWithSeveralServersOneWithoutToken(t *testing.T) {
	dir := t.TempDir()
	st, _ := store.Open(dir + "/gotify.db")
	unauth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) }))
	defer unauth.Close()
	tokens := &secret.Memory{}
	var ids []int64
	for i := 0; i < 4; i++ {
		id, _ := st.AddServer(store.Server{Name: fmt.Sprint("s", i), URL: unauth.URL})
		ids = append(ids, id)
		if i != 2 {
			tokens.Set(id, "tok")
		}
	}
	st.Close()
	a, err := New(Options{DataDir: dir, CacheDir: t.TempDir(), Tokens: tokens, Notifier: &fakeNotifier{},
		ConnConfig: conn.Config{MinBackoff: 10 * time.Millisecond}})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				a.Snapshot()
				a.KickAll()
				time.Sleep(time.Millisecond)
			}
		}()
	}
	wg.Wait()
	eventually(t, "every server auth-failed", func() bool {
		for _, id := range ids {
			if sv, ok := a.Snapshot().Server(id); !ok || sv.State != conn.AuthFailed {
				return false
			}
		}
		return true
	})
	if sv, _ := a.Snapshot().Server(ids[2]); !sv.NeedLogin {
		t.Fatal("server without token should need a login")
	}
}

func TestReloginDoesNotClobberConcurrentEdits(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/client":
			once.Do(func() { close(entered) })
			<-release
			w.Write([]byte(`{"id":9,"token":"newtok"}`))
		case "/current/user":
			w.Write([]byte(`{"id":1,"name":"alice"}`))
		default:
			w.WriteHeader(401)
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	st, _ := store.Open(dir + "/gotify.db")
	id, _ := st.AddServer(store.Server{Name: "orig", URL: srv.URL, ClientID: 3, UserID: 1, UserName: "alice"})
	st.Close()
	tokens := &secret.Memory{}
	tokens.Set(id, "old")
	a, err := New(Options{DataDir: dir, CacheDir: t.TempDir(), Tokens: tokens, Notifier: &fakeNotifier{},
		ConnConfig: conn.Config{MinBackoff: 10 * time.Millisecond}})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	done := make(chan error, 1)
	go func() { done <- a.Relogin(context.Background(), id, "alice", "pw") }()
	<-entered
	if err := a.UpdateServer(context.Background(), id, "Renamed", true, nil); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	sv, _ := a.st.Server(id)
	if sv.Name != "Renamed" || !sv.InsecureSkipVerify || sv.ClientID != 9 || sv.UserName != "alice" {
		t.Fatalf("%+v", sv)
	}
	if tok, _ := tokens.Get(id); tok != "newtok" {
		t.Fatalf("token %q", tok)
	}
}

func TestReloginOfARemovedServerRevokesTheNewClient(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{})
	var once sync.Once
	var mu sync.Mutex
	var revoked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/client" && r.Method == "POST":
			once.Do(func() { close(entered) })
			<-release
			w.Write([]byte(`{"id":9,"token":"newtok"}`))
		case r.URL.Path == "/current/user":
			w.Write([]byte(`{"id":1,"name":"alice"}`))
		case r.Method == "DELETE":
			_, _, basic := r.BasicAuth()
			mu.Lock()
			revoked = append(revoked, fmt.Sprint(r.URL.Path, " basic=", basic))
			mu.Unlock()
		default:
			w.WriteHeader(401)
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	st, _ := store.Open(dir + "/gotify.db")
	id, _ := st.AddServer(store.Server{Name: "gone", URL: srv.URL, ClientID: 3, UserID: 1, UserName: "alice"})
	st.Close()
	tokens := &secret.Memory{}
	tokens.Set(id, "old")
	a, err := New(Options{DataDir: dir, CacheDir: t.TempDir(), Tokens: tokens, Notifier: &fakeNotifier{},
		ConnConfig: conn.Config{MinBackoff: 10 * time.Millisecond}})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	done := make(chan error, 1)
	go func() { done <- a.Relogin(context.Background(), id, "alice", "pw") }()
	<-entered
	if err := a.RemoveServer(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err == nil {
		t.Fatal("relogin of a removed server succeeded")
	}
	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, r := range revoked {
		found = found || r == "/client/9 basic=true"
	}
	if !found {
		t.Fatalf("the new client was leaked: %q", revoked)
	}
	if _, err := tokens.Get(id); err == nil {
		t.Fatal("token stored for a removed server")
	}
}

func TestAddServerRevokesTheClientWhenTheContextExpiresMidLogin(t *testing.T) {
	var mu sync.Mutex
	var revoked []string
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/version":
			w.Write([]byte(`{"version":"2.0"}`))
		case r.URL.Path == "/client" && r.Method == "POST":
			w.Write([]byte(`{"id":9,"token":"newtok"}`))
		case r.URL.Path == "/current/user":
			close(started)
			<-r.Context().Done()
		case r.Method == "DELETE":
			_, _, basic := r.BasicAuth()
			mu.Lock()
			revoked = append(revoked, fmt.Sprint(r.URL.Path, " basic=", basic))
			mu.Unlock()
		}
	}))
	defer srv.Close()
	a, _, _ := newApp(t, &secret.Memory{})
	ctx, cancel := context.WithCancel(context.Background())
	go func() { <-started; cancel() }()
	if _, err := a.AddServer(ctx, ServerInput{URL: srv.URL, User: "alice", Pass: "pw"}); err == nil {
		t.Fatal("expected the cancelled context to fail the add")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(revoked) != 1 || revoked[0] != "/client/9 basic=true" {
		t.Fatalf("the new client was orphaned: %q", revoked)
	}
	if len(a.Snapshot().Servers) != 0 {
		t.Fatal("server added despite the error")
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
	eventually(t, "first import and coalesced snapshot", func() bool {
		_, init, _ := a.st.LastSeen(id)
		sv, _ := a.Snapshot().Server(id)
		return init && sv.State == conn.Connected && len(sv.Apps) > 0 && sv.Unread == 2
	})
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
	srv.API("POST", "/user", map[string]any{"name": "bob", "pass": "bobpass1", "admin": false}, nil)
	err = a.Relogin(ctx, id, "bob", "bobpass1")
	if err == nil || !strings.Contains(err.Error(), "This server was added as admin") {
		t.Fatalf("relogin as another user: %v", err)
	}
	if sv, _ := a.st.Server(id); sv.UserName != harness.AdminUser || sv.UserID == 0 {
		t.Fatalf("user not stored: %+v", sv)
	}
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
