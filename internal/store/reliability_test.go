package store

import (
	"math"
	"path/filepath"
	"testing"
	"time"

	"gotify-desktop/internal/gotify"
)

func TestReceivedMessagesAndNotificationJobCommitTogether(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbox.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := s.AddServer(Server{URL: "http://example.invalid"})
	m := gotify.Message{ID: 1, AppID: 1, Priority: 5, Date: time.Now()}
	bad := m
	bad.ID = 2
	bad.Extras = map[string]any{"invalid": math.NaN()}
	if _, err := s.SaveReceivedMessages(id, []gotify.Message{m, bad}, false); err == nil {
		t.Fatal("expected serialization failure")
	}
	if rows, _ := s.Messages(MessageQuery{ServerID: id}); len(rows) != 0 {
		t.Fatal("partial message commit")
	}
	if job, _ := s.NextNotification(time.Now()); job != nil {
		t.Fatal("partial notification commit")
	}
	if _, init, _ := s.LastSeen(id); init {
		t.Fatal("advanced cursor on failure")
	}
	if _, err := s.SaveReceivedMessages(id, []gotify.Message{m}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveReceivedMessages(id, []gotify.Message{m}, true); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	job, err := s.NextNotification(time.Now())
	if err != nil || job == nil || !job.CatchUp || len(job.Messages) != 1 {
		t.Fatalf("lost durable job: %+v %v", job, err)
	}
	if err := s.FinishNotification(job.ID); err != nil {
		t.Fatal(err)
	}
	if job, _ := s.NextNotification(time.Now()); job != nil {
		t.Fatal("duplicate notification job")
	}
}

func TestInitialImportQueuesOnlyLiveMessages(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "initial.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	id, _ := s.AddServer(Server{URL: "http://example.invalid"})
	_, _, err = s.SaveInitialImport(id, []gotify.Message{{ID: 3}}, []gotify.Message{{ID: 1}, {ID: 2}, {ID: 3}}, 1)
	if err != nil {
		t.Fatal(err)
	}
	job, err := s.NextNotification(time.Now())
	if err != nil || job == nil || job.CatchUp || len(job.Messages) != 1 || job.Messages[0].ID != 3 {
		t.Fatalf("history became notifications: %+v %v", job, err)
	}
	s.DeleteServer(id)
	if job, _ := s.NextNotification(time.Now()); job != nil {
		t.Fatal("removed server kept pending jobs")
	}
}

func TestPartialCatchUpDoesNotAdvanceCursor(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "catchup.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	id, _ := s.AddServer(Server{URL: "http://example.invalid"})
	s.SaveMessages(id, []gotify.Message{{ID: 1}})
	if _, err := s.SaveCatchUpBatch(id, []gotify.Message{{ID: 100}}, true); err != nil {
		t.Fatal(err)
	}
	if last, _, _ := s.LastSeen(id); last != 1 {
		t.Fatal("newest page prematurely advanced cursor")
	}
	if _, err := s.SaveCatchUpBatch(id, []gotify.Message{{ID: 2}}, true); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishCatchUp(id, 100); err != nil {
		t.Fatal(err)
	}
	if last, _, _ := s.LastSeen(id); last != 100 {
		t.Fatal("completed catchup did not advance cursor")
	}
}

func TestCursorUsesDateIDAndServerTogether(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "cursor.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, _ := s.AddServer(Server{})
	b, _ := s.AddServer(Server{})
	now := time.Now().Truncate(time.Millisecond)
	s.SaveMessages(a, []gotify.Message{{ID: 1, Date: now}, {ID: 99, Date: now.Add(-time.Hour)}})
	s.SaveMessages(b, []gotify.Message{{ID: 1, Date: now}, {ID: 2, Date: now}})
	var cursor *MessageCursor
	var got []StoredMessage
	for {
		page, err := s.Messages(MessageQuery{Before: cursor, Limit: 1})
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		m := page[0]
		got = append(got, m)
		if len(got) > 4 {
			t.Fatal("cursor did not advance")
		}
		cursor = &MessageCursor{Date: m.Date, ID: m.ID, ServerID: m.ServerID}
	}
	if len(got) != 4 || got[0].ID != 2 || got[1].ServerID != a || got[2].ServerID != b || got[3].ID != 99 {
		t.Fatalf("missing or duplicate rows: %+v", got)
	}
}
