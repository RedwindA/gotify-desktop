package notify

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gotify-desktop/internal/conn"
	"gotify-desktop/internal/gotify"
	"gotify-desktop/internal/store"
)

type fakeNotifier struct {
	mu   sync.Mutex
	got  []Notification
	sig  chan struct{}
	fail error
}

func newFakeNotifier() *fakeNotifier { return &fakeNotifier{sig: make(chan struct{}, 16)} }

func (f *fakeNotifier) Supported() bool { return true }
func (f *fakeNotifier) Show(n Notification) error {
	f.mu.Lock()
	f.got = append(f.got, n)
	f.mu.Unlock()
	f.sig <- struct{}{}
	return f.fail
}
func (f *fakeNotifier) Remove(string)           {}
func (f *fakeNotifier) OnActivate(func(string)) {}

func (f *fakeNotifier) wait(t *testing.T, n int) []Notification {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-f.sig:
		case <-time.After(5 * time.Second):
			t.Fatalf("timeout waiting for notification %d", i+1)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Notification(nil), f.got...)
}

var png1x1 = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89")

func setup(t *testing.T, httpc *http.Client) (*Dispatcher, *fakeNotifier, *store.Store, int64, string) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "n.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	sid, _ := st.AddServer(store.Server{Name: "s", URL: "http://x"})
	st.ReplaceApps(sid, []gotify.Application{{ID: 1, Name: "Backup", Image: "image/a.png"}, {ID: 2, Name: "Muted"}})
	st.SetAppImage(sid, 1, png1x1)
	st.SetAppPref(sid, 2, store.AppPref{Muted: true})
	var seeded []gotify.Message
	for i := uint(1); i <= 20; i++ {
		seeded = append(seeded, gotify.Message{ID: i, AppID: 1, Priority: 5, Date: time.Now()})
	}
	st.SaveMessages(sid, seeded)
	fn := newFakeNotifier()
	cache := t.TempDir()
	d := NewDispatcher(fn, st, DefaultSettings, cache, httpc)
	t.Cleanup(d.Close)
	return d, fn, st, sid, cache
}

func TestDispatcherWritesIconOnceAndHonoursPrefs(t *testing.T) {
	d, fn, _, sid, cache := setup(t, nil)
	enqueue(t, d, conn.Event{ServerID: sid, Kind: conn.EventMessages, Messages: []gotify.Message{msg(1, 1, 5, "hi", "b"), msg(2, 2, 9, "muted", "")}})
	got := fn.wait(t, 1)
	icon := filepath.Join(cache, "icons", "s1-a1.png")
	if got[0].IconPath != icon || got[0].ID != "s1-m1" {
		t.Fatalf("%+v", got[0])
	}
	st1, err := os.Stat(icon)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	os.Chtimes(icon, old, old)
	enqueue(t, d, conn.Event{ServerID: sid, Kind: conn.EventMessages, Messages: []gotify.Message{msg(3, 1, 5, "again", "")}})
	fn.wait(t, 1)
	st2, _ := os.Stat(icon)
	if !st2.ModTime().Equal(old) || st1.Size() != st2.Size() {
		t.Fatal("icon rewritten although unchanged")
	}
	if len(fn.got) != 2 {
		t.Fatalf("muted app notified: %+v", fn.got)
	}
}

func TestDispatcherImageDownload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok.png":
			w.Header().Set("Content-Type", "image/png")
			w.Write(png1x1)
		case "/html":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte("<html>"))
		case "/big.png":
			w.Header().Set("Content-Type", "image/png")
			w.Write(make([]byte, maxImageBytes+10))
		default:
			w.WriteHeader(500)
		}
	}))
	defer srv.Close()
	d, fn, _, sid, _ := setup(t, srv.Client())
	d.settings = func() Settings { s := DefaultSettings(); s.BurstMax = 100; return s }
	withImage := func(id uint, path string) gotify.Message {
		m := msg(id, 1, 5, "img", "")
		m.Extras = map[string]any{"client::notification": map[string]any{"bigImageUrl": srv.URL + path}}
		return m
	}
	for i, path := range []string{"/ok.png", "/fail", "/html", "/big.png"} {
		enqueue(t, d, conn.Event{ServerID: sid, Kind: conn.EventMessages, Messages: []gotify.Message{withImage(uint(i+1), path)}})
	}
	got := fn.wait(t, 4)
	if got[0].ImagePath == "" {
		t.Fatal("image not downloaded")
	}
	if b, err := os.ReadFile(got[0].ImagePath); err != nil || string(b) != string(png1x1) {
		t.Fatalf("image file: %v", err)
	}
	for i := 1; i < 4; i++ {
		if got[i].ImagePath != "" || got[i].Title != "img" {
			t.Fatalf("failed download must still show without image: %+v", got[i])
		}
	}
}

func TestDispatcherSummaryAndClose(t *testing.T) {
	d, fn, _, sid, _ := setup(t, nil)
	var msgs []gotify.Message
	for i := uint(1); i <= 5; i++ {
		msgs = append(msgs, msg(i, 1, 5, "t", ""))
	}
	enqueue(t, d, conn.Event{ServerID: sid, Kind: conn.EventMessages, CatchUp: true, Messages: msgs})
	if got := fn.wait(t, 1); got[0].ID != "s1-missed" {
		t.Fatalf("%+v", got)
	}
	enqueue(t, d, conn.Event{ServerID: sid, Kind: conn.EventMessages, Silent: true, Messages: msgs})
	enqueue(t, d, conn.Event{ServerID: sid, Kind: conn.EventState})
	d.Close()
	d.Close()
	enqueue(t, d, conn.Event{ServerID: sid, Kind: conn.EventMessages, Messages: msgs[:1]})
	time.Sleep(50 * time.Millisecond)
	if len(fn.got) != 1 {
		t.Fatalf("unexpected notifications: %+v", fn.got)
	}
	if a, _, m, k := d.Activated("s1-m5"); a != 1 || m != 5 || k != KindMessage {
		t.Fatal("Activated")
	}
}

func TestDispatcherSkipsDeletedAndReadMessages(t *testing.T) {
	d, fn, st, sid, _ := setup(t, nil)
	st.DeleteMessage(sid, 1)
	st.MarkRead(sid, 2)
	enqueue(t, d, conn.Event{ServerID: sid, Kind: conn.EventMessages, Messages: []gotify.Message{msg(1, 1, 5, "deleted", ""), msg(2, 1, 5, "read", ""), msg(3, 1, 5, "fresh", "")}})
	got := fn.wait(t, 1)
	time.Sleep(100 * time.Millisecond)
	if len(fn.got) != 1 || got[0].Title != "fresh" {
		t.Fatalf("%+v", fn.got)
	}
	st.DeleteServer(sid)
	var many []gotify.Message
	for i := uint(1); i <= 5; i++ {
		many = append(many, msg(i, 1, 5, "t", ""))
	}
	enqueue(t, d, conn.Event{ServerID: sid, Kind: conn.EventMessages, CatchUp: true, Messages: many})
	time.Sleep(150 * time.Millisecond)
	if len(fn.got) != 1 {
		t.Fatalf("summary for a removed server shown: %+v", fn.got)
	}
}

func TestDispatcherDoesNotShowAfterReadDuringImageDownload(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		w.Header().Set("Content-Type", "image/png")
		w.Write(png1x1)
	}))
	defer srv.Close()
	d, fn, st, sid, _ := setup(t, srv.Client())
	m := msg(1, 1, 5, "slow image", "")
	m.Extras = map[string]any{"client::notification": map[string]any{"bigImageUrl": srv.URL + "/x.png"}}
	enqueue(t, d, conn.Event{ServerID: sid, Kind: conn.EventMessages, Messages: []gotify.Message{m}})
	<-entered
	st.MarkRead(sid, 1)
	close(release)
	time.Sleep(300 * time.Millisecond)
	if len(fn.got) != 0 {
		t.Fatalf("shown after being read: %+v", fn.got)
	}
}

func TestDispatcherDropsStaleMessagesBeforePlanning(t *testing.T) {
	d, fn, st, sid, _ := setup(t, nil)
	st.MarkRead(sid, 1, 2, 3, 4)
	var four []gotify.Message
	for i := uint(1); i <= 4; i++ {
		four = append(four, msg(i, 1, 5, "t", ""))
	}
	enqueue(t, d, conn.Event{ServerID: sid, Kind: conn.EventMessages, CatchUp: true, Messages: four})
	time.Sleep(200 * time.Millisecond)
	if len(fn.got) != 0 {
		t.Fatalf("summary of read messages shown: %+v", fn.got)
	}
	enqueue(t, d, conn.Event{ServerID: sid, Kind: conn.EventMessages, CatchUp: true, Messages: append(four, msg(5, 1, 5, "only fresh", ""))})
	got := fn.wait(t, 1)
	if len(got) != 1 || got[0].ID != "s1-m5" {
		t.Fatalf("one fresh message should be shown on its own: %+v", got)
	}
}

func enqueue(t *testing.T, d *Dispatcher, ev conn.Event) {
	t.Helper()
	if ev.Kind == conn.EventMessages && !ev.Silent && d.ctx.Err() == nil {
		// A removed server should not produce new work.
		if _, err := d.st.Server(ev.ServerID); err == nil {
			if err := d.st.QueueNotifications(ev.ServerID, ev.Messages, ev.CatchUp); err != nil {
				t.Fatal(err)
			}
		}
	}
	d.Handle(ev)
}
