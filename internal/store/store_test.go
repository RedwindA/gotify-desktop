package store

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"gotify-desktop/internal/gotify"
)

func open(t *testing.T) (*Store, int64) {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "g.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	id, err := s.AddServer(Server{Name: "home", URL: "https://x/gotify", ClientID: 4})
	if err != nil {
		t.Fatal(err)
	}
	return s, id
}

func msg(id, app uint, title string) gotify.Message {
	return gotify.Message{ID: id, AppID: app, Title: title, Message: "body " + title, Priority: 3, Date: time.UnixMilli(1700000000000 + int64(id))}
}

func ids(ms []gotify.Message) (out []uint) {
	for _, m := range ms {
		out = append(out, m.ID)
	}
	return
}

func TestServersCRUDAndCascade(t *testing.T) {
	s, id := open(t)
	sv, err := s.Server(id)
	if err != nil || sv.Name != "home" || sv.ClientID != 4 || sv.InsecureSkipVerify {
		t.Fatalf("%+v %v", sv, err)
	}
	sv.InsecureSkipVerify, sv.Name = true, "renamed"
	if err := s.UpdateServer(sv); err != nil {
		t.Fatal(err)
	}
	all, _ := s.Servers()
	if len(all) != 1 || !all[0].InsecureSkipVerify || all[0].Name != "renamed" {
		t.Fatalf("%+v", all)
	}
	s.SaveMessages(id, []gotify.Message{msg(1, 1, "a")})
	s.ReplaceApps(id, []gotify.Application{{ID: 1, Name: "A"}})
	s.SetAppPref(id, 1, AppPref{Muted: true})
	if err := s.DeleteServer(id); err != nil {
		t.Fatal(err)
	}
	for _, tbl := range []string{"messages", "apps", "server_state", "app_prefs"} {
		var n int
		s.db.QueryRow("SELECT count(*) FROM " + tbl).Scan(&n)
		if n != 0 {
			t.Errorf("%s not cascaded: %d", tbl, n)
		}
	}
}

func TestSaveMessagesDedupAndLastSeen(t *testing.T) {
	s, id := open(t)
	if _, init, _ := s.LastSeen(id); init {
		t.Fatal("should start uninitialized")
	}
	got, err := s.SaveMessages(id, []gotify.Message{msg(5, 1, "e"), msg(3, 1, "c"), msg(4, 1, "d")})
	if err != nil || !reflect.DeepEqual(ids(got), []uint{3, 4, 5}) {
		t.Fatalf("%v %v", ids(got), err)
	}
	got, _ = s.SaveMessages(id, []gotify.Message{msg(4, 1, "d"), msg(2, 1, "b"), msg(6, 1, "f")})
	if !reflect.DeepEqual(ids(got), []uint{2, 6}) {
		t.Fatalf("%v", ids(got))
	}
	if last, init, _ := s.LastSeen(id); last != 6 || !init {
		t.Fatalf("last=%d init=%v", last, init)
	}
	s.SaveMessages(id, []gotify.Message{msg(1, 1, "a")})
	if last, _, _ := s.LastSeen(id); last != 6 {
		t.Fatalf("last_seen went backwards: %d", last)
	}
}

func TestEmptySaveMarksInitialized(t *testing.T) {
	s, id := open(t)
	if _, err := s.SaveMessages(id, nil); err != nil {
		t.Fatal(err)
	}
	if last, init, _ := s.LastSeen(id); last != 0 || !init {
		t.Fatalf("last=%d init=%v", last, init)
	}
}

func TestMessagesQueryAndRoundTrip(t *testing.T) {
	s, id := open(t)
	m := msg(1, 7, "50%_off")
	m.Extras = map[string]any{"client::display": map[string]any{"contentType": "text/markdown"}}
	s.SaveMessages(id, []gotify.Message{m, msg(2, 8, "other"), msg(3, 7, "third"), msg(4, 7, "fourth")})
	q := func(q MessageQuery) []uint {
		q.ServerID = id
		r, err := s.Messages(q)
		if err != nil {
			t.Fatal(err)
		}
		var out []uint
		for _, m := range r {
			out = append(out, m.ID)
		}
		return out
	}
	if g := q(MessageQuery{}); !reflect.DeepEqual(g, []uint{4, 3, 2, 1}) {
		t.Fatal(g)
	}
	if g := q(MessageQuery{AppID: 7, BeforeID: 4, Limit: 1}); !reflect.DeepEqual(g, []uint{3}) {
		t.Fatal(g)
	}
	if g := q(MessageQuery{Search: "%_o"}); !reflect.DeepEqual(g, []uint{1}) {
		t.Fatalf("escaped search: %v", g)
	}
	if g := q(MessageQuery{Search: "BODY oth"}); !reflect.DeepEqual(g, []uint{2}) {
		t.Fatalf("body search: %v", g)
	}
	r, _ := s.Messages(MessageQuery{ServerID: id, AppID: 7, Search: "50%", Limit: 1})
	if len(r) != 1 || !reflect.DeepEqual(r[0].Extras, m.Extras) || !r[0].Date.Equal(m.Date) || r[0].Priority != 3 || r[0].Read {
		t.Fatalf("%+v", r)
	}
}

func TestReadAndUnread(t *testing.T) {
	s, id := open(t)
	id2, _ := s.AddServer(Server{Name: "b", URL: "http://b"})
	s.SaveMessages(id, []gotify.Message{msg(1, 1, "a"), msg(2, 1, "b"), msg(3, 2, "c")})
	s.SaveMessages(id2, []gotify.Message{msg(1, 1, "z")})
	c, _ := s.UnreadCounts()
	if c[id][1] != 2 || c[id][2] != 1 || c[id2][1] != 1 {
		t.Fatalf("%v", c)
	}
	s.MarkRead(id, 1)
	s.MarkAllRead(id, 2)
	c, _ = s.UnreadCounts()
	if c[id][1] != 1 || c[id][2] != 0 || c[id2][1] != 1 {
		t.Fatalf("%v", c)
	}
	s.MarkAllRead(id, 0)
	s.DeleteMessage(id2, 1)
	if c, _ = s.UnreadCounts(); len(c) != 0 {
		t.Fatalf("%v", c)
	}
}

func TestReplaceAppsKeepsImageUntilPathChanges(t *testing.T) {
	s, id := open(t)
	apps := []gotify.Application{{ID: 1, Name: "A", Image: "image/a.png", DefaultPriority: 2}, {ID: 2, Name: "B", Image: "image/b.png"}}
	s.ReplaceApps(id, apps)
	s.SetAppImage(id, 1, []byte("A1"))
	s.SetAppImage(id, 2, []byte("B1"))
	apps[0].Name = "A2"
	apps[1].Image = "image/b2.png"
	apps = append(apps, gotify.Application{ID: 3, Name: "C"})
	s.ReplaceApps(id, apps)
	got, _ := s.Apps(id)
	if len(got) != 3 || got[0].Name != "A2" || string(got[0].Image) != "A1" || got[0].DefaultPriority != 2 || got[1].Image != nil {
		t.Fatalf("%+v", got)
	}
	s.ReplaceApps(id, apps[:1])
	if got, _ = s.Apps(id); len(got) != 1 || got[0].ID != 1 {
		t.Fatalf("%+v", got)
	}
}

func TestSettingsAndPrefs(t *testing.T) {
	s, id := open(t)
	if v, _ := s.GetSetting("k"); v != "" {
		t.Fatal(v)
	}
	s.SetSetting("k", "1")
	s.SetSetting("k", "2")
	if v, _ := s.GetSetting("k"); v != "2" {
		t.Fatal(v)
	}
	if p, _ := s.GetAppPref(id, 1); p.Muted || p.MinPriority != nil {
		t.Fatalf("%+v", p)
	}
	min := 4
	s.SetAppPref(id, 1, AppPref{Muted: true, MinPriority: &min})
	if p, _ := s.GetAppPref(id, 1); !p.Muted || p.MinPriority == nil || *p.MinPriority != 4 {
		t.Fatalf("%+v", p)
	}
	s.SetAppPref(id, 1, AppPref{})
	if p, _ := s.GetAppPref(id, 1); p.Muted || p.MinPriority != nil {
		t.Fatalf("%+v", p)
	}
}

func TestReopenKeepsDataAndConcurrentUse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := s.AddServer(Server{Name: "x", URL: "http://x"})
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 1; i <= 25; i++ {
				if _, err := s.SaveMessages(id, []gotify.Message{msg(uint(i), 1, "m")}); err != nil {
					t.Error(err)
				}
				s.Messages(MessageQuery{ServerID: id})
			}
		}()
	}
	wg.Wait()
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r, _ := s.Messages(MessageQuery{ServerID: id})
	if last, _, _ := s.LastSeen(id); len(r) != 25 || last != 25 {
		t.Fatalf("n=%d last=%d", len(r), last)
	}
	var v int
	s.db.QueryRow("PRAGMA user_version").Scan(&v)
	if v != len(migrations) {
		t.Fatalf("version %d", v)
	}
}

func TestMigrationRebuildsServersKeepingChildren(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range migrations[:2] {
		if _, err := raw.Exec(m.sql); err != nil {
			t.Fatal(err)
		}
	}
	raw.Exec(`INSERT INTO servers(id,name,url,created_at) VALUES(1,'a','http://a',1),(2,'b','http://b',2)`)
	raw.Exec(`INSERT INTO messages(server_id,id,app_id,date) VALUES(1,5,1,1),(2,6,1,1)`)
	raw.Exec(`INSERT INTO apps(server_id,id,name) VALUES(1,1,'App')`)
	raw.Exec(`INSERT INTO server_state(server_id,last_seen_id,initialized) VALUES(1,5,1)`)
	raw.Exec(`PRAGMA user_version=2`)
	raw.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	svs, _ := s.Servers()
	msgs, _ := s.Messages(MessageQuery{})
	apps, _ := s.Apps(1)
	if last, init, _ := s.LastSeen(1); len(svs) != 2 || len(msgs) != 2 || len(apps) != 1 || last != 5 || !init {
		t.Fatalf("data lost: %d servers %d msgs %d apps last=%d", len(svs), len(msgs), len(apps), last)
	}
	if err := s.DeleteServer(2); err != nil {
		t.Fatal(err)
	}
	id, _ := s.AddServer(Server{Name: "c", URL: "http://c"})
	if id != 3 {
		t.Fatalf("id %d reused", id)
	}
	if left, _ := s.Messages(MessageQuery{ServerID: 2}); len(left) != 0 {
		t.Fatal("cascade lost after migration")
	}
}

func TestDeletedMessagesAreNotReinserted(t *testing.T) {
	s, id := open(t)
	s.SaveMessages(id, []gotify.Message{msg(1, 1, "a"), msg(2, 1, "b")})
	if err := s.DeleteMessage(id, 2); err != nil {
		t.Fatal(err)
	}
	got, err := s.SaveMessages(id, []gotify.Message{msg(2, 1, "b"), msg(3, 1, "c")})
	if err != nil || !reflect.DeepEqual(ids(got), []uint{3}) {
		t.Fatalf("%v %v", ids(got), err)
	}
	got, _ = s.SaveMessages(id, []gotify.Message{msg(2, 1, "b")})
	if len(got) != 0 {
		t.Fatal("tombstoned message came back")
	}
	if last, _, _ := s.LastSeen(id); last != 3 {
		t.Fatalf("last_seen %d", last)
	}
	s.DeleteServer(id)
	var n int
	s.db.QueryRow("SELECT count(*) FROM deleted_messages").Scan(&n)
	if n != 0 {
		t.Fatal("tombstones not cascaded")
	}
}

func TestServerUserPersisted(t *testing.T) {
	s, _ := open(t)
	id, _ := s.AddServer(Server{Name: "u", URL: "http://u", UserID: 7, UserName: "alice"})
	sv, _ := s.Server(id)
	if sv.UserID != 7 || sv.UserName != "alice" {
		t.Fatalf("%+v", sv)
	}
}

func TestSaveInitialImportIsAtomicAndSetsFloor(t *testing.T) {
	s, id := open(t)
	live, hist, err := s.SaveInitialImport(id, []gotify.Message{msg(6, 1, "live")}, []gotify.Message{msg(3, 1, "c"), msg(4, 1, "d"), msg(5, 1, "e"), msg(6, 1, "live")}, 3)
	if err != nil || !reflect.DeepEqual(ids(live), []uint{6}) || !reflect.DeepEqual(ids(hist), []uint{3, 4, 5}) {
		t.Fatalf("%v %v %v", ids(live), ids(hist), err)
	}
	if last, init, _ := s.LastSeen(id); last != 6 || !init {
		t.Fatalf("last=%d init=%v", last, init)
	}
	if floor, _ := s.ImportFloor(id); floor != 3 {
		t.Fatalf("floor %d", floor)
	}
	s.SaveMessages(id, []gotify.Message{msg(7, 1, "later")})
	if floor, _ := s.ImportFloor(id); floor != 3 {
		t.Fatalf("SaveMessages changed the floor: %d", floor)
	}

	s2, id2 := open(t)
	bad := msg(9, 1, "bad")
	bad.Extras = map[string]any{"x": make(chan int)}
	if _, _, err := s2.SaveInitialImport(id2, []gotify.Message{msg(8, 1, "live")}, []gotify.Message{bad}, 9); err == nil {
		t.Fatal("expected an error")
	}
	if _, init, _ := s2.LastSeen(id2); init {
		t.Fatal("a failed import initialized the server")
	}
	if left, _ := s2.Messages(MessageQuery{ServerID: id2}); len(left) != 0 {
		t.Fatalf("partial import kept: %d", len(left))
	}
}
