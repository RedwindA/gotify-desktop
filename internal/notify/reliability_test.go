package notify

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gotify-desktop/internal/conn"
	"gotify-desktop/internal/gotify"
	"gotify-desktop/internal/store"
)

type blockedNotifier struct {
	mu               sync.Mutex
	ids              []string
	first, released  sync.Once
	entered, release chan struct{}
}

func (n *blockedNotifier) unblock() { n.released.Do(func() { close(n.release) }) }

func (n *blockedNotifier) Supported() bool         { return true }
func (n *blockedNotifier) Remove(string)           {}
func (n *blockedNotifier) OnActivate(func(string)) {}
func (n *blockedNotifier) Show(m Notification) error {
	n.first.Do(func() { close(n.entered); <-n.release })
	n.mu.Lock()
	defer n.mu.Unlock()
	n.ids = append(n.ids, m.ID)
	return nil
}

func (n *blockedNotifier) count() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.ids)
}

func waitCount(t *testing.T, n *blockedNotifier, want int) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); n.count() < want; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d notifications shown", n.count(), want)
		}
	}
	time.Sleep(50 * time.Millisecond)
	if got := n.count(); got != want {
		t.Fatalf("%d notifications shown, want %d", got, want)
	}
}

func blockedSetup(t *testing.T) (*Dispatcher, *blockedNotifier, *store.Store, int64) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "backlog.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	id, _ := s.AddServer(store.Server{})
	n := &blockedNotifier{entered: make(chan struct{}), release: make(chan struct{})}
	d := NewDispatcher(n, s, DefaultSettings, t.TempDir(), nil)
	t.Cleanup(func() { n.unblock(); d.Close() })
	return d, n, s, id
}

// The old dispatcher dropped events once 256 were waiting behind a slow notifier.
func TestSlowNotifierDoesNotDropEvents(t *testing.T) {
	d, n, s, id := blockedSetup(t)
	for i := uint(1); i <= 300; i++ {
		// One app per message, so no burst summary folds them together.
		m := []gotify.Message{{ID: i, AppID: i, Priority: 5}}
		if _, err := s.SaveMessages(id, m); err != nil {
			t.Fatal(err)
		}
		d.Handle(conn.Event{Kind: conn.EventMessages, ServerID: id, Messages: m})
		if i == 1 {
			select {
			case <-n.entered:
			case <-time.After(time.Second):
				t.Fatal("worker did not start")
			}
		}
	}
	n.unblock()
	waitCount(t, n, 300)
}

// Catch-up events that wait together make one summary of all their messages.
func TestWaitingCatchUpEventsMerge(t *testing.T) {
	d, n, s, id := blockedSetup(t)
	first := []gotify.Message{{ID: 1, AppID: 1, Priority: 5}}
	s.SaveMessages(id, first)
	d.Handle(conn.Event{Kind: conn.EventMessages, ServerID: id, Messages: first})
	<-n.entered
	var msgs []gotify.Message
	for i := uint(2); i <= 21; i++ {
		msgs = append(msgs, gotify.Message{ID: i, AppID: 1, Priority: 5})
	}
	s.SaveMessages(id, msgs)
	d.Handle(conn.Event{Kind: conn.EventMessages, ServerID: id, CatchUp: true, Messages: msgs[:10]})
	d.Handle(conn.Event{Kind: conn.EventMessages, ServerID: id, CatchUp: true, Messages: msgs[10:]})
	n.unblock()
	waitCount(t, n, 2)
	if n.ids[1] != "s1-missed" {
		t.Fatalf("%v", n.ids)
	}
}
