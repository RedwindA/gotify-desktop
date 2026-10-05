package itest

import (
	"context"
	"sync"
	"testing"
	"time"

	"gotify-desktop/internal/conn"
	"gotify-desktop/internal/gotify"
	"gotify-desktop/internal/itest/harness"
)

type recorder struct {
	mu     sync.Mutex
	events []conn.Event
	sig    chan struct{}
}

func newRecorder() *recorder { return &recorder{sig: make(chan struct{}, 1)} }

func (r *recorder) sink(e conn.Event) {
	r.mu.Lock()
	r.events = append(r.events, e)
	r.mu.Unlock()
	select {
	case r.sig <- struct{}{}:
	default:
	}
}

func (r *recorder) all() []conn.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]conn.Event(nil), r.events...)
}

func (r *recorder) wait(t *testing.T, from int, pred func(conn.Event) bool) (conn.Event, int) {
	t.Helper()
	deadline := time.After(20 * time.Second)
	for {
		evs := r.all()
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

func state(s conn.State) func(conn.Event) bool {
	return func(e conn.Event) bool { return e.Kind == conn.EventState && e.State == s }
}

func msgs(e conn.Event) bool { return e.Kind == conn.EventMessages }

func messageBodies(ms []gotify.Message) (out []string) {
	for _, m := range ms {
		out = append(out, m.Message)
	}
	return
}

func login(t *testing.T, url string) (*gotify.Client, uint) {
	t.Helper()
	tok, id, err := gotify.Login(context.Background(), url, harness.AdminUser, harness.AdminPass, "it-client", gotify.Options{})
	if err != nil {
		t.Fatal(err)
	}
	c, err := gotify.New(url, tok, gotify.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return c, id
}
