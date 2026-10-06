package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gotify-desktop/internal/gotify"

	_ "modernc.org/sqlite"
)

type migration struct {
	sql string
	// rebuildsTable migrations run with foreign keys off, as dropping a table
	// that others reference would cascade otherwise.
	rebuildsTable bool
}

var migrations = []migration{{sql: `
CREATE TABLE servers(
	id INTEGER PRIMARY KEY,
	name TEXT NOT NULL,
	url TEXT NOT NULL,
	insecure_skip_verify INTEGER NOT NULL DEFAULT 0,
	ca_cert TEXT NOT NULL DEFAULT '',
	client_id INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL
);
CREATE TABLE apps(
	server_id INTEGER NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
	id INTEGER NOT NULL,
	name TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	image_path TEXT NOT NULL DEFAULT '',
	default_priority INTEGER NOT NULL DEFAULT 0,
	image BLOB,
	PRIMARY KEY(server_id, id)
);
CREATE TABLE messages(
	server_id INTEGER NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
	id INTEGER NOT NULL,
	app_id INTEGER NOT NULL,
	title TEXT NOT NULL DEFAULT '',
	message TEXT NOT NULL DEFAULT '',
	priority INTEGER NOT NULL DEFAULT 0,
	extras TEXT NOT NULL DEFAULT '',
	date INTEGER NOT NULL,
	read INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY(server_id, id)
);
CREATE INDEX messages_app ON messages(server_id, app_id, id DESC);
CREATE TABLE server_state(
	server_id INTEGER PRIMARY KEY REFERENCES servers(id) ON DELETE CASCADE,
	last_seen_id INTEGER NOT NULL DEFAULT 0,
	initialized INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE app_prefs(
	server_id INTEGER NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
	app_id INTEGER NOT NULL,
	muted INTEGER NOT NULL DEFAULT 0,
	min_priority INTEGER,
	PRIMARY KEY(server_id, app_id)
);
CREATE TABLE settings(key TEXT PRIMARY KEY, value TEXT NOT NULL);
`}, {sql: `CREATE INDEX messages_date ON messages(date DESC, id DESC);`}, {rebuildsTable: true, sql: `
CREATE TABLE servers_new(
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL,
	url TEXT NOT NULL,
	insecure_skip_verify INTEGER NOT NULL DEFAULT 0,
	ca_cert TEXT NOT NULL DEFAULT '',
	client_id INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL,
	user_id INTEGER NOT NULL DEFAULT 0,
	user_name TEXT NOT NULL DEFAULT ''
);
INSERT INTO servers_new(id,name,url,insecure_skip_verify,ca_cert,client_id,created_at) SELECT id,name,url,insecure_skip_verify,ca_cert,client_id,created_at FROM servers;
DROP TABLE servers;
ALTER TABLE servers_new RENAME TO servers;
`}, {sql: `
CREATE TABLE deleted_messages(
	server_id INTEGER NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
	id INTEGER NOT NULL,
	PRIMARY KEY(server_id, id)
);
`}, {sql: `ALTER TABLE server_state ADD COLUMN import_floor INTEGER NOT NULL DEFAULT 0;`}, {sql: `
CREATE TABLE notification_outbox(
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 server_id INTEGER NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
 messages TEXT NOT NULL,
 catch_up INTEGER NOT NULL,
 plans BLOB,
 attempts INTEGER NOT NULL DEFAULT 0,
 next_at INTEGER NOT NULL DEFAULT 0,
 created_at INTEGER NOT NULL
);
CREATE INDEX notification_outbox_ready ON notification_outbox(next_at,id);
CREATE INDEX messages_unread ON messages(server_id,app_id) WHERE read=0;
`}}

type Store struct{ db *sql.DB }

// Open opens (creating if needed) the database at path and migrates it.
func Open(path string) (*Store, error) {
	q := url.Values{}
	for _, p := range []string{"busy_timeout(5000)", "foreign_keys(1)", "journal_mode(WAL)", "synchronous(NORMAL)"} {
		q.Add("_pragma", p)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?"+q.Encode())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	var v int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		return err
	}
	if v > len(migrations) {
		return fmt.Errorf("store: database version %d is newer than supported %d", v, len(migrations))
	}
	for ; v < len(migrations); v++ {
		if err := s.apply(v); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) apply(v int) error {
	m := migrations[v]
	if m.rebuildsTable {
		if _, err := s.db.Exec("PRAGMA foreign_keys=OFF"); err != nil {
			return err
		}
		defer s.db.Exec("PRAGMA foreign_keys=ON")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(m.sql); err != nil {
		tx.Rollback()
		return fmt.Errorf("store: migration %d: %w", v+1, err)
	}
	if m.rebuildsTable {
		var violations int
		if err := tx.QueryRow("SELECT count(*) FROM pragma_foreign_key_check").Scan(&violations); err != nil || violations > 0 {
			tx.Rollback()
			return fmt.Errorf("store: migration %d broke foreign keys (%v)", v+1, err)
		}
	}
	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version=%d", v+1)); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) tx(f func(*sql.Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if err := f(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

type Server struct {
	ID                 int64
	Name               string
	URL                string
	InsecureSkipVerify bool
	CACertPEM          string
	ClientID           uint
	CreatedAt          time.Time
	// UserID and UserName are the account the server was added with.
	UserID   uint
	UserName string
}

const serverCols = "id, name, url, insecure_skip_verify, ca_cert, client_id, created_at, user_id, user_name"

type scanner interface{ Scan(...any) error }

func scanServer(r scanner) (sv Server, err error) {
	var created int64
	err = r.Scan(&sv.ID, &sv.Name, &sv.URL, &sv.InsecureSkipVerify, &sv.CACertPEM, &sv.ClientID, &created, &sv.UserID, &sv.UserName)
	sv.CreatedAt = time.UnixMilli(created)
	return
}

// AddServer inserts sv and returns its id; CreatedAt defaults to now.
func (s *Store) AddServer(sv Server) (int64, error) {
	if sv.CreatedAt.IsZero() {
		sv.CreatedAt = time.Now()
	}
	res, err := s.db.Exec(`INSERT INTO servers(name,url,insecure_skip_verify,ca_cert,client_id,created_at,user_id,user_name) VALUES(?,?,?,?,?,?,?,?)`,
		sv.Name, sv.URL, sv.InsecureSkipVerify, sv.CACertPEM, sv.ClientID, sv.CreatedAt.UnixMilli(), sv.UserID, sv.UserName)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateServer(sv Server) error {
	_, err := s.db.Exec(`UPDATE servers SET name=?,url=?,insecure_skip_verify=?,ca_cert=?,client_id=?,user_id=?,user_name=? WHERE id=?`,
		sv.Name, sv.URL, sv.InsecureSkipVerify, sv.CACertPEM, sv.ClientID, sv.UserID, sv.UserName, sv.ID)
	return err
}

func (s *Store) DeleteServer(id int64) error {
	_, err := s.db.Exec(`DELETE FROM servers WHERE id=?`, id)
	return err
}

func (s *Store) Server(id int64) (Server, error) {
	return scanServer(s.db.QueryRow(`SELECT `+serverCols+` FROM servers WHERE id=?`, id))
}

func (s *Store) Servers() ([]Server, error) {
	rows, err := s.db.Query(`SELECT ` + serverCols + ` FROM servers ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Server
	for rows.Next() {
		sv, err := scanServer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sv)
	}
	return out, rows.Err()
}

type App struct {
	ID              uint
	Name            string
	Description     string
	ImagePath       string
	DefaultPriority int
	Image           []byte
}

// ReplaceApps upserts apps and deletes the ones missing; images survive while image_path is unchanged.
func (s *Store) ReplaceApps(serverID int64, apps []gotify.Application) error {
	return s.tx(func(tx *sql.Tx) error {
		keep := make(map[uint]bool, len(apps))
		for _, a := range apps {
			keep[a.ID] = true
			if _, err := tx.Exec(`INSERT INTO apps(server_id,id,name,description,image_path,default_priority) VALUES(?,?,?,?,?,?)
ON CONFLICT(server_id,id) DO UPDATE SET name=excluded.name, description=excluded.description,
default_priority=excluded.default_priority,
image=CASE WHEN apps.image_path=excluded.image_path THEN apps.image ELSE NULL END,
image_path=excluded.image_path`,
				serverID, a.ID, a.Name, a.Description, a.Image, a.DefaultPriority); err != nil {
				return err
			}
		}
		rows, err := tx.Query(`SELECT id FROM apps WHERE server_id=?`, serverID)
		if err != nil {
			return err
		}
		var stale []uint
		for rows.Next() {
			var id uint
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			if !keep[id] {
				stale = append(stale, id)
			}
		}
		rows.Close()
		for _, id := range stale {
			if _, err := tx.Exec(`DELETE FROM apps WHERE server_id=? AND id=?`, serverID, id); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) SetAppImage(serverID int64, appID uint, img []byte) error {
	_, err := s.db.Exec(`UPDATE apps SET image=? WHERE server_id=? AND id=?`, img, serverID, appID)
	return err
}

func (s *Store) Apps(serverID int64) ([]App, error) {
	rows, err := s.db.Query(`SELECT id,name,description,image_path,default_priority,image FROM apps WHERE server_id=? ORDER BY id`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []App
	for rows.Next() {
		var a App
		if err := rows.Scan(&a.ID, &a.Name, &a.Description, &a.ImagePath, &a.DefaultPriority, &a.Image); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) LastSeen(serverID int64) (uint, bool, error) {
	var id uint
	var init bool
	err := s.db.QueryRow(`SELECT last_seen_id, initialized FROM server_state WHERE server_id=?`, serverID).Scan(&id, &init)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	return id, init, err
}

// insertMessages stores msgs that are new and not deleted, and returns them with the highest id seen.
func insertMessages(tx *sql.Tx, serverID int64, msgs []gotify.Message) (inserted []gotify.Message, maxID uint, err error) {
	for _, m := range msgs {
		maxID = max(maxID, m.ID)
		extras := ""
		if len(m.Extras) > 0 {
			b, err := json.Marshal(m.Extras)
			if err != nil {
				return nil, 0, err
			}
			extras = string(b)
		}
		res, err := tx.Exec(`INSERT OR IGNORE INTO messages(server_id,id,app_id,title,message,priority,extras,date)
SELECT ?,?,?,?,?,?,?,? WHERE NOT EXISTS (SELECT 1 FROM deleted_messages WHERE server_id=? AND id=?)`,
			serverID, m.ID, m.AppID, m.Title, m.Message, m.Priority, extras, m.Date.UnixMilli(), serverID, m.ID)
		if err != nil {
			return nil, 0, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			inserted = append(inserted, m)
		}
	}
	sort.Slice(inserted, func(i, j int) bool { return inserted[i].ID < inserted[j].ID })
	return inserted, maxID, nil
}

// SaveMessages is the dedup point: it returns only newly inserted messages, ascending by id,
// and advances last_seen_id in the same transaction.
func (s *Store) SaveMessages(serverID int64, msgs []gotify.Message) ([]gotify.Message, error) {
	return s.saveMessages(serverID, msgs, false, false, true)
}

// SaveReceivedMessages atomically commits messages, their cursor and notification work.
func (s *Store) SaveReceivedMessages(serverID int64, msgs []gotify.Message, catchUp bool) ([]gotify.Message, error) {
	return s.saveMessages(serverID, msgs, true, catchUp, true)
}

func (s *Store) saveMessages(serverID int64, msgs []gotify.Message, notify, catchUp, advance bool) ([]gotify.Message, error) {
	var inserted []gotify.Message
	err := s.tx(func(tx *sql.Tx) error {
		var maxID uint
		var err error
		if inserted, maxID, err = insertMessages(tx, serverID, msgs); err != nil {
			return err
		}
		if notify {
			if err := queueNotifications(tx, serverID, inserted, catchUp); err != nil {
				return err
			}
		}
		if !advance {
			return nil
		}
		_, err = tx.Exec(`INSERT INTO server_state(server_id,last_seen_id,initialized) VALUES(?,?,1)
ON CONFLICT(server_id) DO UPDATE SET last_seen_id=max(last_seen_id,excluded.last_seen_id), initialized=1`, serverID, maxID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return inserted, nil
}

// SaveInitialImport stores the first connection's messages in one transaction: those that arrived
// live while the history loaded, and the history, below which nothing is imported (floor). Either
// everything is saved and the server initialized, or nothing is.
func (s *Store) SaveInitialImport(serverID int64, live, history []gotify.Message, floor uint) (insertedLive, insertedHistory []gotify.Message, err error) {
	err = s.tx(func(tx *sql.Tx) error {
		var maxLive, maxHist uint
		var err error
		if insertedLive, maxLive, err = insertMessages(tx, serverID, live); err != nil {
			return err
		}
		if insertedHistory, maxHist, err = insertMessages(tx, serverID, history); err != nil {
			return err
		}
		if err := queueNotifications(tx, serverID, insertedLive, false); err != nil {
			return err
		}
		_, err = tx.Exec(`INSERT INTO server_state(server_id,last_seen_id,initialized,import_floor) VALUES(?,?,1,?)
ON CONFLICT(server_id) DO UPDATE SET last_seen_id=max(last_seen_id,excluded.last_seen_id), initialized=1, import_floor=excluded.import_floor`,
			serverID, max(maxLive, maxHist), floor)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	return insertedLive, insertedHistory, nil
}

// ImportFloor is the smallest id the first import took; older messages were left out on purpose.
func (s *Store) ImportFloor(serverID int64) (uint, error) {
	var floor uint
	err := s.db.QueryRow(`SELECT import_floor FROM server_state WHERE server_id=?`, serverID).Scan(&floor)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return floor, err
}

type StoredMessage struct {
	ServerID int64
	Read     bool
	gotify.Message
}

// MessageCursor follows the total ordering shared by every server.
type MessageCursor struct {
	Date     time.Time `json:"date"`
	ID       uint      `json:"id"`
	ServerID int64     `json:"serverId"`
}
type MessageQuery struct {
	Before    *MessageCursor
	Inclusive bool
	ServerID  int64
	AppID     uint
	Search    string
	BeforeID  uint
	// ID selects one message.
	ID    uint
	Limit int
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// Messages returns matching messages newest first; zero ServerID/AppID/BeforeID mean no filter.
func (s *Store) Messages(q MessageQuery) ([]StoredMessage, error) {
	var where []string
	var args []any
	if q.ServerID != 0 {
		where, args = append(where, "server_id=?"), append(args, q.ServerID)
	}
	if q.AppID != 0 {
		where, args = append(where, "app_id=?"), append(args, q.AppID)
	}
	if q.ID != 0 {
		where, args = append(where, "id=?"), append(args, q.ID)
	}
	if q.BeforeID != 0 {
		where, args = append(where, "id<?"), append(args, q.BeforeID)
	}
	if q.Before != nil {
		op := ">"
		if q.Inclusive {
			op = ">="
		}
		where = append(where, "(date<? OR (date=? AND id<?) OR (date=? AND id=? AND server_id"+op+"?))")
		c := q.Before
		args = append(args, c.Date.UnixMilli(), c.Date.UnixMilli(), c.ID, c.Date.UnixMilli(), c.ID, c.ServerID)
	}
	if q.Search != "" {
		pat := "%" + likeEscaper.Replace(q.Search) + "%"
		where, args = append(where, `(title LIKE ? ESCAPE '\' OR message LIKE ? ESCAPE '\')`), append(args, pat, pat)
	}
	query := `SELECT server_id,id,app_id,title,message,priority,extras,date,read FROM messages`
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY date DESC, id DESC, server_id"
	if q.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, q.Limit)
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StoredMessage
	for rows.Next() {
		var m StoredMessage
		var extras string
		var date int64
		if err := rows.Scan(&m.ServerID, &m.ID, &m.AppID, &m.Title, &m.Message.Message, &m.Priority, &extras, &date, &m.Read); err != nil {
			return nil, err
		}
		m.Date = time.UnixMilli(date)
		if extras != "" {
			if err := json.Unmarshal([]byte(extras), &m.Extras); err != nil {
				return nil, err
			}
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// DeleteMessage removes a message and remembers its id, so a late copy from the stream or a catch-up is not stored again.
func (s *Store) DeleteMessage(serverID int64, id uint) error {
	return s.tx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM messages WHERE server_id=? AND id=?`, serverID, id); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT OR IGNORE INTO deleted_messages(server_id,id) VALUES(?,?)`, serverID, id)
		return err
	})
}

func (s *Store) MarkRead(serverID int64, ids ...uint) error {
	return s.tx(func(tx *sql.Tx) error {
		for _, id := range ids {
			if _, err := tx.Exec(`UPDATE messages SET read=1 WHERE server_id=? AND id=?`, serverID, id); err != nil {
				return err
			}
		}
		return nil
	})
}

// MarkAllRead marks a server's messages read, only those of appID when it is non-zero.
func (s *Store) MarkAllRead(serverID int64, appID uint) error {
	if appID == 0 {
		_, err := s.db.Exec(`UPDATE messages SET read=1 WHERE server_id=? AND read=0`, serverID)
		return err
	}
	_, err := s.db.Exec(`UPDATE messages SET read=1 WHERE server_id=? AND app_id=? AND read=0`, serverID, appID)
	return err
}

func (s *Store) UnreadCounts() (map[int64]map[uint]int, error) {
	rows, err := s.db.Query(`SELECT server_id, app_id, count(*) FROM messages WHERE read=0 GROUP BY server_id, app_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]map[uint]int{}
	for rows.Next() {
		var sid int64
		var aid uint
		var n int
		if err := rows.Scan(&sid, &aid, &n); err != nil {
			return nil, err
		}
		if out[sid] == nil {
			out[sid] = map[uint]int{}
		}
		out[sid][aid] = n
	}
	return out, rows.Err()
}

// GetSetting returns "" for a missing key.
func (s *Store) GetSetting(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

type AppPref struct {
	Muted       bool
	MinPriority *int
}

// AppPrefs loads preferences in one query for a snapshot.
func (s *Store) AppPrefs(serverID int64) (map[uint]AppPref, error) {
	rows, err := s.db.Query(`SELECT app_id,muted,min_priority FROM app_prefs WHERE server_id=?`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uint]AppPref{}
	for rows.Next() {
		var id uint
		var p AppPref
		var min sql.NullInt64
		if err := rows.Scan(&id, &p.Muted, &min); err != nil {
			return nil, err
		}
		if min.Valid {
			v := int(min.Int64)
			p.MinPriority = &v
		}
		out[id] = p
	}
	return out, rows.Err()
}

// GetAppPref returns the zero AppPref when none was set.
func (s *Store) GetAppPref(serverID int64, appID uint) (AppPref, error) {
	var p AppPref
	var min sql.NullInt64
	err := s.db.QueryRow(`SELECT muted,min_priority FROM app_prefs WHERE server_id=? AND app_id=?`, serverID, appID).Scan(&p.Muted, &min)
	if err == sql.ErrNoRows {
		return p, nil
	}
	if min.Valid {
		v := int(min.Int64)
		p.MinPriority = &v
	}
	return p, err
}

func (s *Store) SetAppPref(serverID int64, appID uint, p AppPref) error {
	var min any
	if p.MinPriority != nil {
		min = *p.MinPriority
	}
	_, err := s.db.Exec(`INSERT INTO app_prefs(server_id,app_id,muted,min_priority) VALUES(?,?,?,?)
ON CONFLICT(server_id,app_id) DO UPDATE SET muted=excluded.muted, min_priority=excluded.min_priority`, serverID, appID, p.Muted, min)
	return err
}

// SaveCatchUpBatch deliberately leaves the durable cursor unchanged: REST pages
// arrive newest first, so advancing it before the last page could skip a gap
// after a crash. Already committed pages are deduplicated on the next attempt.
func (s *Store) SaveCatchUpBatch(serverID int64, msgs []gotify.Message, catchUp bool) ([]gotify.Message, error) {
	return s.saveMessages(serverID, msgs, true, catchUp, false)
}
func (s *Store) FinishCatchUp(serverID int64, last uint) error {
	_, err := s.db.Exec(`UPDATE server_state SET last_seen_id=max(last_seen_id,?) WHERE server_id=?`, last, serverID)
	return err
}

// SetAppImageForPath ignores a download that finished after the icon changed.
func (s *Store) SetAppImageForPath(serverID int64, appID uint, path string, img []byte) error {
	_, err := s.db.Exec(`UPDATE apps SET image=? WHERE server_id=? AND id=? AND image_path=?`, img, serverID, appID, path)
	return err
}
