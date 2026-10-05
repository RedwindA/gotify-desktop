package conn

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"gotify-desktop/internal/gotify"
	"gotify-desktop/internal/store"
)

type fakeServer struct {
	*httptest.Server
	mu        sync.Mutex
	msgs      []gotify.Message
	apps      []gotify.Application
	conns     map[*websocket.Conn]bool
	unauth    atomic.Bool
	streams   atomic.Int32
	images    atomic.Int32
	onMessage func()
}

func newFake(t *testing.T) *fakeServer {
	f := &fakeServer{conns: map[*websocket.Conn]bool{}}
	f.apps = []gotify.Application{{ID: 1, Name: "A", Image: "image/a.png"}}
	mux := http.NewServeMux()
	guard := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if f.unauth.Load() {
				w.WriteHeader(401)
				return
			}
			h(w, r)
		}
	}
	mux.HandleFunc("/application", guard(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		json.NewEncoder(w).Encode(f.apps)
	}))
	mux.HandleFunc("/image/", func(w http.ResponseWriter, r *http.Request) {
		f.images.Add(1)
		w.Write([]byte("IMG"))
	})
	mux.HandleFunc("/message", guard(func(w http.ResponseWriter, r *http.Request) {
		if f.onMessage != nil {
			f.onMessage()
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		since, _ := strconv.Atoi(r.URL.Query().Get("since"))
		f.mu.Lock()
		defer f.mu.Unlock()
		var page []gotify.Message
		for i := len(f.msgs) - 1; i >= 0; i-- {
			if m := f.msgs[i]; since == 0 || int(m.ID) < since {
				page = append(page, m)
			}
		}
		p := gotify.PagedMessages{Messages: []gotify.Message{}}
		if len(page) > limit {
			page = page[:limit]
			p.Paging.Since = page[len(page)-1].ID
			p.Paging.Next = "/message?since=" + strconv.Itoa(int(p.Paging.Since))
		}
		p.Messages = append(p.Messages, page...)
		json.NewEncoder(w).Encode(p)
	}))
	mux.HandleFunc("/stream", guard(func(w http.ResponseWriter, r *http.Request) {
		f.streams.Add(1)
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		c.SetReadLimit(64)
		f.mu.Lock()
		f.conns[c] = true
		f.mu.Unlock()
		for {
			if _, _, err := c.Read(context.Background()); err != nil {
				break
			}
		}
		f.mu.Lock()
		delete(f.conns, c)
		f.mu.Unlock()
	}))
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func (f *fakeServer) post(appID uint, n int, live bool) []gotify.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []gotify.Message
	for i := 0; i < n; i++ {
		id := uint(1)
		if len(f.msgs) > 0 {
			id = f.msgs[len(f.msgs)-1].ID + 1
		}
		m := gotify.Message{ID: id, AppID: appID, Message: "m" + strconv.Itoa(int(id)), Date: time.Now()}
		f.msgs = append(f.msgs, m)
		out = append(out, m)
		if live {
			b, _ := json.Marshal(m)
			for c := range f.conns {
				c.Write(context.Background(), websocket.MessageText, b)
			}
		}
	}
	return out
}

func (f *fakeServer) dropConns() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for c := range f.conns {
		c.CloseNow()
	}
}

type recorder struct {
	mu     sync.Mutex
	events []Event
	sig    chan struct{}
}

func newRecorder() *recorder { return &recorder{sig: make(chan struct{}, 1)} }

func (r *recorder) sink(e Event) {
	r.mu.Lock()
	r.events = append(r.events, e)
	r.mu.Unlock()
	select {
	case r.sig <- struct{}{}:
	default:
	}
}

func (r *recorder) snapshot() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Event(nil), r.events...)
}

func (r *recorder) wait(t *testing.T, from int, pred func(Event) bool) (Event, int) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		evs := r.snapshot()
		for i := from; i < len(evs); i++ {
			if pred(evs[i]) {
				return evs[i], i + 1
			}
		}
		select {
		case <-r.sig:
		case <-deadline:
			t.Fatalf("timeout waiting for event; got %+v", evs[from:])
		}
	}
}

func isState(s State) func(Event) bool {
	return func(e Event) bool { return e.Kind == EventState && e.State == s }
}

func isMsgs(e Event) bool { return e.Kind == EventMessages }

func ids(ms []gotify.Message) (out []uint) {
	for _, m := range ms {
		out = append(out, m.ID)
	}
	return
}

func setup(t *testing.T, f *fakeServer, cfg Config) (*Supervisor, *recorder, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	sid, _ := st.AddServer(store.Server{Name: "t", URL: f.URL})
	c, _ := gotify.New(f.URL, "tok", gotify.Options{})
	if cfg.MinBackoff == 0 {
		cfg.MinBackoff = 20 * time.Millisecond
	}
	rec := newRecorder()
	s := New(sid, c, st, rec.sink, cfg)
	t.Cleanup(s.Stop)
	return s, rec, st
}

func TestFirstConnectImportsSilently(t *testing.T) {
	f := newFake(t)
	f.post(1, 5, false)
	s, rec, st := setup(t, f, Config{ImportOnFirstConnect: 3})
	s.Start()
	e, _ := rec.wait(t, 0, isMsgs)
	if !e.Silent || !e.CatchUp || !reflect.DeepEqual(ids(e.Messages), []uint{3, 4, 5}) {
		t.Fatalf("%+v", e)
	}
	if last, init, _ := st.LastSeen(e.ServerID); last != 5 || !init {
		t.Fatalf("last=%d init=%v", last, init)
	}
	apps, _ := st.Apps(e.ServerID)
	if len(apps) != 1 || string(apps[0].Image) != "IMG" {
		t.Fatalf("%+v", apps)
	}
}

func TestFirstConnectEmptyHistoryThenLiveNotSilent(t *testing.T) {
	f := newFake(t)
	s, rec, _ := setup(t, f, Config{})
	s.Start()
	_, n := rec.wait(t, 0, isState(Connected))
	rec.wait(t, 0, func(e Event) bool { return e.Kind == EventApps })
	time.Sleep(100 * time.Millisecond)
	f.post(1, 1, true)
	e, _ := rec.wait(t, n, isMsgs)
	if e.Silent || e.CatchUp || !reflect.DeepEqual(ids(e.Messages), []uint{1}) {
		t.Fatalf("%+v", e)
	}
}

func TestCatchUpPaginationDedupAndGapMessage(t *testing.T) {
	f := newFake(t)
	f.post(1, 2, false)
	s, rec, _ := setup(t, f, Config{})
	s.Start()
	_, n := rec.wait(t, 0, isMsgs)
	f.dropConns()
	rec.wait(t, n, isState(Backoff))

	f.post(1, 250, false)
	var once sync.Once
	f.onMessage = func() {
		once.Do(func() {
			go func() { f.post(1, 1, true) }()
			time.Sleep(100 * time.Millisecond)
		})
	}
	var got []uint
	for len(got) < 251 {
		var e Event
		e, n = rec.wait(t, n, isMsgs)
		got = append(got, ids(e.Messages)...)
		if !sort.SliceIsSorted(e.Messages, func(i, j int) bool { return e.Messages[i].ID < e.Messages[j].ID }) {
			t.Fatal("event not ascending")
		}
	}
	time.Sleep(200 * time.Millisecond)
	for _, e := range rec.snapshot()[n:] {
		if isMsgs(e) {
			got = append(got, ids(e.Messages)...)
		}
	}
	want := make([]uint, 251)
	for i := range want {
		want[i] = uint(i + 3)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %d ids, first %v last %v", len(got), got[:3], got[len(got)-3:])
	}
}

func TestUnknownAppTriggersResync(t *testing.T) {
	f := newFake(t)
	s, rec, st := setup(t, f, Config{})
	s.Start()
	_, n := rec.wait(t, 0, isState(Connected))
	rec.wait(t, 0, func(e Event) bool { return e.Kind == EventApps })
	time.Sleep(100 * time.Millisecond)
	f.mu.Lock()
	f.apps = append(f.apps, gotify.Application{ID: 9, Name: "New"})
	f.mu.Unlock()
	f.post(9, 1, true)
	e, _ := rec.wait(t, n, isMsgs)
	apps, _ := st.Apps(e.ServerID)
	if len(apps) != 2 || apps[1].Name != "New" {
		t.Fatalf("%+v", apps)
	}
}

func TestAuthFailedStopsRetryingUntilKick(t *testing.T) {
	f := newFake(t)
	f.unauth.Store(true)
	s, rec, _ := setup(t, f, Config{})
	s.Start()
	_, n := rec.wait(t, 0, isState(AuthFailed))
	time.Sleep(300 * time.Millisecond)
	if s.State() != AuthFailed || f.streams.Load() != 0 {
		t.Fatalf("state=%v streams=%d", s.State(), f.streams.Load())
	}
	f.unauth.Store(false)
	s.Kick()
	rec.wait(t, n, isState(Connected))
}

func TestKickInBackoffAndStopIsFinal(t *testing.T) {
	f := newFake(t)
	st, _ := store.Open(filepath.Join(t.TempDir(), "t.db"))
	defer st.Close()
	sid, _ := st.AddServer(store.Server{Name: "t", URL: "http://x"})
	f.post(1, 1, false)
	c, _ := gotify.New(f.URL, "tok", gotify.Options{})
	rec := newRecorder()
	s := New(sid, c, st, rec.sink, Config{MinBackoff: time.Hour, MaxBackoff: time.Hour})
	s.Start()
	_, n := rec.wait(t, 0, isState(Connected))
	f.dropConns()
	e, n := rec.wait(t, n, isState(Backoff))
	if time.Until(e.RetryAt) < 30*time.Minute {
		t.Fatalf("RetryAt %v", e.RetryAt)
	}
	s.Kick()
	rec.wait(t, n, isState(Connected))
	s.Stop()
	if s.State() != Stopped {
		t.Fatal(s.State())
	}
	count := len(rec.snapshot())
	f.post(1, 1, true)
	time.Sleep(100 * time.Millisecond)
	if len(rec.snapshot()) != count {
		t.Fatal("sink called after Stop")
	}
}

func TestPongTimeoutDetectsDeadServer(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/application", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("[]")) })
	mux.HandleFunc("/message", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"paging":{},"messages":[]}`))
	})
	var dials atomic.Int32
	mux.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
		dials.Add(1)
		// Nothing reads the socket, so pings are never answered.
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		time.Sleep(10 * time.Second)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	defer srv.CloseClientConnections()
	st, _ := store.Open(filepath.Join(t.TempDir(), "t.db"))
	defer st.Close()
	sid, _ := st.AddServer(store.Server{Name: "t", URL: srv.URL})
	c, _ := gotify.New(srv.URL, "tok", gotify.Options{})
	rec := newRecorder()
	s := New(sid, c, st, rec.sink, Config{PingInterval: 100 * time.Millisecond, PongTimeout: 100 * time.Millisecond, MinBackoff: 10 * time.Millisecond})
	s.Start()
	defer s.Stop()
	_, n := rec.wait(t, 0, isState(Connected))
	e, _ := rec.wait(t, n, isState(Backoff))
	if e.Err == nil {
		t.Fatal("backoff without error")
	}
	rec.wait(t, n, func(e Event) bool { return e.Kind == EventState && e.State == Connected })
}
