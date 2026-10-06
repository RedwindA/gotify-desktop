package notify

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gotify-desktop/internal/conn"
	"gotify-desktop/internal/gotify"
	"gotify-desktop/internal/store"
)

type retryNotifier struct {
	mu       sync.Mutex
	ids      []string
	failures int
}

type blockedNotifier struct {
	retryNotifier
	first            sync.Once
	entered, release chan struct{}
}

func (n *blockedNotifier) Show(m Notification) error {
	n.first.Do(func() { close(n.entered); <-n.release })
	return n.retryNotifier.Show(m)
}

func TestSlowNotifierDoesNotLoseMoreThan256Events(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "backlog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	id, _ := s.AddServer(store.Server{})
	n := &blockedNotifier{entered: make(chan struct{}), release: make(chan struct{})}
	d := NewDispatcher(n, s, DefaultSettings, t.TempDir(), nil)
	defer d.Close()
	var release sync.Once
	defer release.Do(func() { close(n.release) })
	for i := uint(1); i <= 300; i++ {
		if _, err := s.SaveReceivedMessages(id, []gotify.Message{{ID: i, AppID: i, Priority: 5}}, false); err != nil {
			t.Fatal(err)
		}
		d.Handle(conn.Event{Kind: conn.EventMessages})
		if i == 1 {
			select {
			case <-n.entered:
			case <-time.After(time.Second):
				t.Fatal("worker did not start")
			}
		}
	}
	release.Do(func() { close(n.release) })
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		n.mu.Lock()
		count := len(n.ids)
		n.mu.Unlock()
		if count == 300 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("only delivered %d of 300 jobs", count)
		}
	}
}

func (n *retryNotifier) Supported() bool         { return true }
func (n *retryNotifier) Remove(string)           {}
func (n *retryNotifier) OnActivate(func(string)) {}
func (n *retryNotifier) Show(m Notification) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.ids = append(n.ids, m.ID)
	if n.failures > 0 {
		n.failures--
		return errors.New("temporary notification failure")
	}
	return nil
}

func TestFailedNotificationResumesAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "retry.db")
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := s.AddServer(store.Server{})
	s.SaveReceivedMessages(id, []gotify.Message{{ID: 1, AppID: 1, Priority: 5, Title: "retry me"}}, false)
	n := &retryNotifier{failures: 1}
	d := &Dispatcher{n: n, st: s, settings: DefaultSettings, planner: NewPlanner(), ctx: context.Background(), http: http.DefaultClient, icons: map[string]string{}, cacheDir: t.TempDir()}
	job, _ := s.NextNotification(time.Now())
	if err := d.process(job); err == nil {
		t.Fatal("expected show failure")
	}
	job, _ = s.NextNotification(time.Now())
	if job == nil || len(job.Plans) == 0 {
		t.Fatal("failed notification was not checkpointed")
	}
	s.Close()
	s, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// Startup must drain the outbox without a connection event or Handle call.
	d = NewDispatcher(n, s, DefaultSettings, t.TempDir(), nil)
	defer d.Close()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		job, err = s.NextNotification(time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if job == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("startup did not resume notification")
		}
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.ids) != 2 || n.ids[0] != n.ids[1] {
		t.Fatalf("unstable retry identity: %v", n.ids)
	}
}

func TestNotificationWorkerRetriesWithoutAnotherMessage(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "retry.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	id, _ := s.AddServer(store.Server{})
	s.SaveReceivedMessages(id, []gotify.Message{{ID: 1, AppID: 1, Priority: 5}}, false)
	n := &retryNotifier{failures: 1}
	d := NewDispatcher(n, s, DefaultSettings, t.TempDir(), nil)
	defer d.Close()
	for deadline := time.Now().Add(6 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		n.mu.Lock()
		count := len(n.ids)
		n.mu.Unlock()
		if count >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("failure was never retried")
		}
	}
}

func TestRetryHonoursNewMuteAndReadState(t *testing.T) {
	for _, mode := range []string{"read", "mute"} {
		t.Run(mode, func(t *testing.T) {
			s, err := store.Open(filepath.Join(t.TempDir(), "retry.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			id, _ := s.AddServer(store.Server{})
			s.SaveReceivedMessages(id, []gotify.Message{{ID: 1, AppID: 1, Priority: 5}}, false)
			n := &retryNotifier{failures: 1}
			d := &Dispatcher{n: n, st: s, settings: DefaultSettings, planner: NewPlanner(), ctx: context.Background(), http: http.DefaultClient, icons: map[string]string{}, cacheDir: t.TempDir()}
			job, _ := s.NextNotification(time.Now())
			if err := d.process(job); err == nil {
				t.Fatal("expected failure")
			}
			if mode == "read" {
				s.MarkRead(id, 1)
			} else {
				s.SetAppPref(id, 1, store.AppPref{Muted: true})
			}
			job, _ = s.NextNotification(time.Now())
			if err := d.process(job); err != nil {
				t.Fatal(err)
			}
			if len(n.ids) != 1 {
				t.Fatal("stale notification was retried")
			}
		})
	}
}
