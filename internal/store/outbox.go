package store

import (
	"database/sql"
	"encoding/json"
	"time"

	"gotify-desktop/internal/gotify"
)

// NotificationJob survives a process exit between receiving and displaying a
// notification. Plans is opaque to the store and is checkpointed after each show.
type NotificationJob struct {
	ID, ServerID int64
	Messages     []gotify.Message
	CatchUp      bool
	Plans        []byte
	Attempts     int
	CreatedAt    time.Time
}

func queueNotifications(tx *sql.Tx, serverID int64, msgs []gotify.Message, catchUp bool) error {
	if len(msgs) == 0 {
		return nil
	}
	b, err := json.Marshal(msgs)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO notification_outbox(server_id,messages,catch_up,created_at) VALUES(?,?,?,?)`, serverID, string(b), catchUp, time.Now().UnixMilli())
	return err
}

// QueueNotifications is also useful for explicitly replaying stored messages.
func (s *Store) QueueNotifications(serverID int64, msgs []gotify.Message, catchUp bool) error {
	return s.tx(func(tx *sql.Tx) error { return queueNotifications(tx, serverID, msgs, catchUp) })
}

func (s *Store) NextNotification(now time.Time) (*NotificationJob, error) {
	var j NotificationJob
	var messages string
	var created int64
	err := s.db.QueryRow(`SELECT id,server_id,messages,catch_up,plans,attempts,created_at FROM notification_outbox WHERE next_at<=? ORDER BY id LIMIT 1`, now.UnixMilli()).Scan(&j.ID, &j.ServerID, &messages, &j.CatchUp, &j.Plans, &j.Attempts, &created)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	j.CreatedAt = time.UnixMilli(created)
	err = json.Unmarshal([]byte(messages), &j.Messages)
	return &j, err
}

func (s *Store) SetNotificationPlans(id int64, plans []byte) error {
	_, err := s.db.Exec(`UPDATE notification_outbox SET plans=? WHERE id=?`, plans, id)
	return err
}

func (s *Store) FinishNotification(id int64) error {
	_, err := s.db.Exec(`DELETE FROM notification_outbox WHERE id=?`, id)
	return err
}

func (s *Store) RetryNotification(id int64, next time.Time) error {
	_, err := s.db.Exec(`UPDATE notification_outbox SET attempts=attempts+1,next_at=? WHERE id=?`, next.UnixMilli(), id)
	return err
}
