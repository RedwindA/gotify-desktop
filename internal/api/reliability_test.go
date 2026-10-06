package api

import (
	"errors"
	"testing"

	"gotify-desktop/internal/app"
	"gotify-desktop/internal/store"
)

type measuredBackend struct {
	*FakeBackend
	queries int
	readErr error
}

func (b *measuredBackend) Messages(q store.MessageQuery) ([]store.StoredMessage, error) {
	b.queries++
	return b.FakeBackend.Messages(q)
}
func (b *measuredBackend) MarkRead(int64, ...uint) error { return b.readErr }
func (b *measuredBackend) MarkAllRead(int64, uint) error { return b.readErr }

func TestDeepAndMissingTargetsUseBoundedQueries(t *testing.T) {
	b := &measuredBackend{FakeBackend: NewFakeBackend()}
	for i := 10000; i >= 1; i-- {
		b.Msgs = append(b.Msgs, msg(1, uint(i)))
	}
	b.Refresh()
	c := New(Platform{})
	c.Start(b, "")
	d := c.Service()
	for _, id := range []uint{1, 10001} {
		b.queries = 0
		p, err := d.Messages(Query{Limit: 100, Include: &MessageRef{ServerID: 1, ID: id}})
		if err != nil {
			t.Fatal(err)
		}
		if b.queries > 2 || len(p.Messages) > 100 {
			t.Fatalf("unbounded target lookup: queries=%d rows=%d", b.queries, len(p.Messages))
		}
	}
	p, err := d.Messages(Query{Limit: 1000000})
	if err != nil || len(p.Messages) != 500 || p.Next == nil {
		t.Fatalf("page cap not enforced: rows=%d err=%v", len(p.Messages), err)
	}
	q, err := d.Messages(Query{Limit: 100, Before: p.Next})
	if err != nil || q.Messages[0].ID != 9500 {
		t.Fatalf("cursor page: %+v %v", q, err)
	}
}

func TestReadFailuresReachTheService(t *testing.T) {
	b := &measuredBackend{FakeBackend: NewFakeBackend(), readErr: errors.New("disk full")}
	b.Servers = []app.ServerInfo{{ID: 1}, {ID: 2}}
	b.Refresh()
	c := New(Platform{})
	c.Start(b, "")
	d := c.Service()
	if d.MarkRead(1, []uint{1}) == nil {
		t.Fatal("read error hidden")
	}
	if d.MarkAllRead(1, 0) == nil {
		t.Fatal("server read error hidden")
	}
	if d.MarkAllRead(0, 0) == nil {
		t.Fatal("global read error hidden")
	}
}
