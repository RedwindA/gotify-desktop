package store

import (
	"path/filepath"
	"testing"
	"time"

	"gotify-desktop/internal/gotify"
)

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
