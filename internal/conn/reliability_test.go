package conn

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"gotify-desktop/internal/gotify"
	"gotify-desktop/internal/store"
)

func TestQueueBackpressureUnblocksOnDrainAndCancel(t *testing.T) {
	q := newQueue()
	m := gotify.Message{ID: 1, Message: strings.Repeat("x", maxQueuedBytes/2)}
	if err := q.push(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- q.push(ctx, m) }()
	select {
	case <-done:
		t.Fatal("queue exceeded byte budget")
	case <-time.After(20 * time.Millisecond):
	}
	if len(q.take()) != 1 {
		t.Fatal("unexpected queue contents")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("drain did not release producer")
	}
	go func() { done <- q.push(ctx, m) }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel left producer blocked")
	}
}

type interruptCatchUpStore struct {
	*store.Store
	batches int
	failed  bool
}

func (s *interruptCatchUpStore) SaveCatchUpBatch(id int64, msgs []gotify.Message, catchUp bool) ([]gotify.Message, error) {
	s.batches++
	if s.batches == 2 && !s.failed {
		s.failed = true
		return nil, errors.New("disk temporarily unavailable")
	}
	return s.Store.SaveCatchUpBatch(id, msgs, catchUp)
}

func TestInterruptedPagedCatchUpRecoversEveryMessage(t *testing.T) {
	f := newFake(t)
	f.post(1, 30, false)
	s, _, st := setup(t, f, Config{PageSize: 5})
	st.SaveMessages(s.id, []gotify.Message{{ID: 1}})
	faults := &interruptCatchUpStore{Store: st}
	s.st = faults
	c := s.getClient()
	if err := s.catchUp(context.Background(), c, newQueue()); err == nil {
		t.Fatal("expected interrupted page")
	}
	if last, _, _ := st.LastSeen(s.id); last != 1 {
		t.Fatal("partial catchup skipped the unsaved gap")
	}
	if err := s.catchUp(context.Background(), c, newQueue()); err != nil {
		t.Fatal(err)
	}
	msgs, err := st.Messages(store.MessageQuery{ServerID: s.id})
	if err != nil || len(msgs) != 30 {
		t.Fatalf("lost messages: %d %v", len(msgs), err)
	}
	if last, _, _ := st.LastSeen(s.id); last != 30 {
		t.Fatal("cursor not committed after recovery")
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
