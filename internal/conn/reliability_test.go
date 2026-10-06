package conn

import (
	"testing"
	"time"

	"gotify-desktop/internal/gotify"
	"gotify-desktop/internal/store"
)

// A catch-up that spans several REST pages is saved and announced as one event,
// so the dispatcher makes one summary with the full count.
func TestMultiPageCatchUpIsOneEvent(t *testing.T) {
	f := newFake(t)
	f.post(1, 250, false)
	s, rec, st := setup(t, f, Config{PageSize: 100})
	st.SaveMessages(s.id, []gotify.Message{{ID: 1}})
	s.Start()
	e, n := rec.wait(t, 0, isMsgs)
	if !e.CatchUp || len(e.Messages) != 249 {
		t.Fatalf("catch-up event: catchUp=%v, %d messages", e.CatchUp, len(e.Messages))
	}
	time.Sleep(50 * time.Millisecond)
	for _, e := range rec.snapshot()[n:] {
		if isMsgs(e) {
			t.Fatalf("catch-up split into another event of %d messages", len(e.Messages))
		}
	}
}

func TestSlowImageDoesNotDelayMessagePersistence(t *testing.T) {
	f := newFake(t)
	entered, release := make(chan struct{}), make(chan struct{})
	f.onImage = func() { close(entered); <-release }
	f.post(1, 2, false)
	s, rec, st := setup(t, f, Config{})
	defer close(release)
	s.Start()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("image download did not start")
	}
	_, next := rec.wait(t, 0, isMsgs)
	f.post(1, 1, true)
	rec.wait(t, next, isMsgs)
	rows, err := st.Messages(store.MessageQuery{ServerID: s.id})
	if err != nil || len(rows) != 3 {
		t.Fatalf("image blocked message persistence: %d %v", len(rows), err)
	}
}
