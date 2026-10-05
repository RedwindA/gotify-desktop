package api

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gotify-desktop/internal/app"
	"gotify-desktop/internal/conn"
	"gotify-desktop/internal/gotify"
	"gotify-desktop/internal/notify"
	"gotify-desktop/internal/store"
)

func start(t *testing.T, f *FakeBackend, plat Platform) (*Controller, *Desktop) {
	t.Helper()
	c := New(plat)
	c.Redirect(func(State) {}, func(Navigation) {})
	c.Start(f, "/data")
	return c, c.Service()
}

func TestStateMapsServersAppsAndSettings(t *testing.T) {
	f := Demo("http://img.example/chart.png")
	_, d := start(t, f, Platform{Version: "1.2.3", OpenAtLogin: func() bool { return true }})
	s := d.State()
	if s.Version != "1.2.3" || s.DataDir != "/data" || !s.OpenAtLogin {
		t.Fatalf("platform fields: %+v", s)
	}
	if len(s.Servers) != 2 || s.Unread != 3 {
		t.Fatalf("servers %d unread %d", len(s.Servers), s.Unread)
	}
	home, work := s.Servers[0], s.Servers[1]
	if home.State != StateConnected || home.RetryAt != nil || home.Unread != 2 {
		t.Errorf("home: %+v", home)
	}
	if work.State != StateBackoff || work.RetryAt == nil || !strings.Contains(work.Error, "connection refused") {
		t.Errorf("work: %+v", work)
	}
	if home.Apps[0].ImageKey == "" || home.Apps[2].ImageKey != "" {
		t.Errorf("image keys: %+v", home.Apps)
	}
	if s.Settings.PausedUntil != nil || s.Settings.DNDStart != 22*60 {
		t.Errorf("settings: %+v", s.Settings)
	}
}

func TestEmptyStateHasNoNilSlices(t *testing.T) {
	_, d := start(t, NewFakeBackend(), Platform{})
	if s := d.State(); s.Servers == nil {
		t.Fatal("servers must encode as [], not null")
	}
	p, err := d.Messages(Query{})
	if err != nil || p.Messages == nil || p.HasMore {
		t.Fatalf("page %+v, %v", p, err)
	}
}

func TestAppImageIsADataURL(t *testing.T) {
	_, d := start(t, Demo(""), Platform{})
	if img := d.AppImage(1, 1); !strings.HasPrefix(img, "data:image/png;base64,") {
		t.Errorf("image: %.40q", img)
	}
	if img := d.AppImage(1, 3); img != "" {
		t.Errorf("an app without an image: %q", img)
	}
	if img := d.AppImage(9, 1); img != "" {
		t.Errorf("an unknown server: %q", img)
	}
}

func TestMessageImage(t *testing.T) {
	f := Demo("")
	c, d := start(t, f, Platform{ImageBase: "msgimg://localhost/"})
	page, _ := d.Messages(Query{ServerID: 1, AppID: 3})
	if len(page.Messages) != 1 || page.Messages[0].ImageURL != demoSnapshotURL || page.Messages[0].ImageSrc != "msgimg://localhost/1/8" {
		t.Fatalf("the image URLs are not in the message: %+v", page.Messages)
	}
	if other, _ := d.Messages(Query{ServerID: 1, AppID: 1}); other.Messages[0].ImageSrc != "" {
		t.Errorf("a message without an image has a source: %+v", other.Messages[0])
	}
	h := c.ImageHandler()
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "msgimg://localhost"+path, nil))
		return rec
	}
	if rec := get("/1/8"); rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" || rec.Body.Len() == 0 {
		t.Errorf("image: %d %q, %d bytes", rec.Code, rec.Header().Get("Content-Type"), rec.Body.Len())
	}
	for _, path := range []string{"/1/9", "/2/8", "/1", "/x/8", "/"} {
		if rec := get(path); rec.Code != 404 {
			t.Errorf("%s: %d", path, rec.Code)
		}
	}
	delete(f.Images, demoSnapshotURL)
	if rec := get("/1/8"); rec.Code != 502 {
		t.Errorf("a failed download: %d", rec.Code)
	}
}

func TestMessagesScopeSearchAndFields(t *testing.T) {
	_, d := start(t, Demo(""), Platform{})
	all, _ := d.Messages(Query{})
	if len(all.Messages) != 8 || all.HasMore {
		t.Fatalf("all: %d, more %v", len(all.Messages), all.HasMore)
	}
	first := all.Messages[0]
	if !first.Markdown || first.ClickURL != "https://grafana.home.example/d/disk" || first.Read || first.Priority != 9 {
		t.Errorf("first message: %+v", first)
	}
	app, _ := d.Messages(Query{ServerID: 1, AppID: 1})
	for _, m := range app.Messages {
		if m.ServerID != 1 || m.AppID != 1 {
			t.Errorf("outside the scope: %+v", m)
		}
	}
	found, _ := d.Messages(Query{Search: "  backup "})
	if len(found.Messages) == 0 {
		t.Error("search found nothing")
	}
	for _, m := range found.Messages {
		if !strings.Contains(strings.ToLower(m.Title+m.Body), "backup") {
			t.Errorf("does not match: %+v", m)
		}
	}
}

func TestMessagesPagesAndGrowToInclude(t *testing.T) {
	f := NewFakeBackend()
	f.Servers = []app.ServerInfo{{ID: 1, Name: "S", State: conn.Connected}}
	for i := 250; i >= 1; i-- {
		f.Msgs = append(f.Msgs, msg(1, uint(i)))
	}
	f.Refresh()
	_, d := start(t, f, Platform{})
	p, _ := d.Messages(Query{Limit: 100})
	if len(p.Messages) != 100 || !p.HasMore {
		t.Fatalf("page: %d, more %v", len(p.Messages), p.HasMore)
	}
	p, _ = d.Messages(Query{Limit: 100, Include: &MessageRef{ServerID: 1, ID: 120}})
	if len(p.Messages) != 200 || !p.HasMore {
		t.Fatalf("the page should grow by pages until it holds message 120: %d, more %v", len(p.Messages), p.HasMore)
	}
	p, _ = d.Messages(Query{Limit: 100, Include: &MessageRef{ServerID: 2, ID: 30}})
	if len(p.Messages) != 250 {
		t.Errorf("a message that is not there loads everything once: %d", len(p.Messages))
	}
}

func msg(server int64, id uint) store.StoredMessage {
	return store.StoredMessage{ServerID: server, Message: gotify.Message{ID: id, AppID: 1, Title: "m", Date: time.Unix(int64(id), 0)}}
}

func TestMarkReadAndMarkAllRead(t *testing.T) {
	f := Demo("")
	_, d := start(t, f, Platform{})
	d.MarkRead(1, []uint{9})
	if s := d.State(); s.Unread != 2 {
		t.Fatalf("unread after one: %d", s.Unread)
	}
	d.MarkRead(1, nil)
	d.MarkAllRead(2, 0)
	if s := d.State(); s.Servers[1].Unread != 0 || s.Servers[0].Unread != 1 {
		t.Fatalf("after server: %+v", s)
	}
	d.MarkAllRead(0, 0)
	if s := d.State(); s.Unread != 0 {
		t.Fatalf("after all: %d", s.Unread)
	}
}

func TestAddServerValidatesAndNormalizes(t *testing.T) {
	f := NewFakeBackend()
	_, d := start(t, f, Platform{})
	ctx := context.Background()
	if _, err := d.AddServer(ctx, ServerForm{URL: " ", User: "a", Pass: "b"}); err == nil || !strings.Contains(err.Error(), "address") {
		t.Errorf("empty address: %v", err)
	}
	if _, err := d.AddServer(ctx, ServerForm{URL: "gotify.example.com"}); err == nil {
		t.Error("no credentials were accepted")
	}
	if _, err := d.AddServer(ctx, ServerForm{URL: "gotify.example.com", User: "a", Pass: "b", CA: "junk"}); err == nil {
		t.Error("a bad CA was accepted")
	}
	id, err := d.AddServer(ctx, ServerForm{Name: " Home ", URL: "gotify.example.com/sub/", User: "a", Pass: "b"})
	if err != nil || id != 1 {
		t.Fatalf("add: %d, %v", id, err)
	}
	if in := f.Added[0]; in.URL != "https://gotify.example.com/sub" || in.Name != "Home" || in.CACertPEM != nil {
		t.Errorf("input: %+v", in)
	}
	f.AddErr = gotify.ErrUnauthorized
	if _, err := d.AddServer(ctx, ServerForm{URL: "http://x", User: "a", Pass: "b"}); err == nil || err.Error() != "Wrong username or password" {
		t.Errorf("errors are explained: %v", err)
	}
}

type caRecorder struct {
	*FakeBackend
	ca [][]byte
}

func (r *caRecorder) UpdateServer(ctx context.Context, id int64, name string, insecure bool, ca []byte) error {
	r.ca = append(r.ca, ca)
	return r.FakeBackend.UpdateServer(ctx, id, name, insecure, ca)
}

func TestUpdateServerKeepsRemovesOrSetsCA(t *testing.T) {
	r := &caRecorder{FakeBackend: Demo("")}
	c := New(Platform{})
	c.Redirect(func(State) {}, func(Navigation) {})
	c.Start(r, "")
	d := c.Service()
	ctx := context.Background()
	if err := d.UpdateServer(ctx, 1, ServerForm{Name: "A", KeepCA: true}); err != nil {
		t.Fatal(err)
	}
	if err := d.UpdateServer(ctx, 1, ServerForm{Name: "A"}); err != nil {
		t.Fatal(err)
	}
	if err := d.UpdateServer(ctx, 1, ServerForm{Name: "A", CA: "junk"}); err == nil {
		t.Fatal("a bad CA was accepted")
	}
	if len(r.ca) != 2 || r.ca[0] != nil || r.ca[1] == nil || len(r.ca[1]) != 0 {
		t.Errorf("CA arguments: %#v", r.ca)
	}
}

func TestSettingsKeepHiddenFieldsAndDropStaleWrites(t *testing.T) {
	f := NewFakeBackend()
	_, d := start(t, f, Platform{})
	until := time.Date(2030, 1, 2, 8, 0, 0, 0, time.UTC)
	if err := d.SetSettings(2, Settings{DND: true, DNDStart: 120, DNDEnd: 480, PausedUntil: &until}); err != nil {
		t.Fatal(err)
	}
	// An older write that arrives late is dropped.
	if err := d.SetSettings(1, Settings{DND: true, DNDStart: 60, DNDEnd: 3000}); err != nil {
		t.Fatal(err)
	}
	got := f.Snapshot().Settings
	if got.DNDStart != 120 || got.DNDEnd != 480 || !got.PausedUntil.Equal(until) {
		t.Errorf("the newest write should win: %+v", got)
	}
	if got.BurstMax != notify.DefaultSettings().BurstMax {
		t.Errorf("hidden settings were lost: %+v", got)
	}
	d.SetSettings(3, Settings{DNDEnd: 5000})
	if got := f.Snapshot().Settings; !got.PausedUntil.IsZero() || got.DNDEnd != 24*60-1 {
		t.Errorf("resume and clamp: %+v", got)
	}
}

func TestSettingsTheme(t *testing.T) {
	f := NewFakeBackend()
	_, d := start(t, f, Platform{})
	for i, c := range []struct{ in, want Theme }{{ThemeDark, ThemeDark}, {ThemeLight, ThemeLight}, {"purple", ThemeSystem}} {
		if err := d.SetSettings(uint64(i+1), Settings{Theme: c.in}); err != nil {
			t.Fatal(err)
		}
		if got := d.State().Settings.Theme; got != c.want {
			t.Errorf("theme %q: got %q, want %q", c.in, got, c.want)
		}
	}
}

func TestOpenURLAllowsOnlyWebAndMail(t *testing.T) {
	var opened []string
	_, d := start(t, NewFakeBackend(), Platform{OpenURL: func(u string) { opened = append(opened, u) }})
	for _, u := range []string{"https://example.com/a", "http://x", "mailto:a@b.c"} {
		if err := d.OpenURL(u); err != nil {
			t.Errorf("%s: %v", u, err)
		}
	}
	for _, u := range []string{"javascript:alert(1)", "file:///etc/passwd", "ms-settings:", ""} {
		if err := d.OpenURL(u); err == nil {
			t.Errorf("%s was opened", u)
		}
	}
	if len(opened) != 3 {
		t.Errorf("opened: %q", opened)
	}
}

func TestPickCA(t *testing.T) {
	name, data, err := "", []byte(nil), error(nil)
	_, d := start(t, NewFakeBackend(), Platform{PickCA: func(context.Context) (string, []byte, error) { return name, data, err }})
	if f, e := d.PickCA(context.Background()); f != nil || e != nil {
		t.Errorf("canceled: %v, %v", f, e)
	}
	name, data = "ca.pem", []byte("junk")
	if _, e := d.PickCA(context.Background()); e == nil {
		t.Error("junk was accepted")
	}
	err = errors.New("boom")
	if _, e := d.PickCA(context.Background()); e == nil {
		t.Error("the error was lost")
	}
}

func TestChangedCoalescesAndWaitsForStart(t *testing.T) {
	f := Demo("")
	c := New(Platform{})
	var sent atomic.Int32
	c.Redirect(func(State) { sent.Add(1) }, func(Navigation) {})
	c.settle = 20 * time.Millisecond
	c.Changed() // before Start: no backend to read yet
	c.Start(f, "")
	for range 10 {
		c.Changed()
	}
	time.Sleep(80 * time.Millisecond)
	if n := sent.Load(); n != 1 {
		t.Fatalf("broadcasts: %d", n)
	}
	c.Close()
	c.Changed()
	time.Sleep(50 * time.Millisecond)
	if n := sent.Load(); n != 1 {
		t.Fatalf("broadcast after close: %d", n)
	}
}

func TestNavigateIsTakenOnce(t *testing.T) {
	c, d := start(t, NewFakeBackend(), Platform{})
	var got []Navigation
	c.Redirect(func(State) {}, func(n Navigation) { got = append(got, n) })
	c.Navigate(Navigation{ServerID: 1, AppID: 2, MessageID: 3})
	if len(got) != 1 {
		t.Fatalf("events: %v", got)
	}
	if n := d.TakeNavigation(); n == nil || n.MessageID != 3 {
		t.Fatalf("take: %v", n)
	}
	if n := d.TakeNavigation(); n != nil {
		t.Fatalf("taken twice: %v", n)
	}
}

func TestSettingsEpochsRiseAboveEverySave(t *testing.T) {
	_, d := start(t, NewFakeBackend(), Platform{})
	a := d.SettingsEpoch()
	d.SetSettings(a<<20+5, Settings{DNDStart: 1})
	b := d.SettingsEpoch()
	if b <= a {
		t.Fatalf("epochs %d then %d", a, b)
	}
	// A page loaded while the first page's write is still on its way wins over it.
	d.SetSettings(b<<20+1, Settings{DNDStart: 2})
	d.SetSettings(a<<20+6, Settings{DNDStart: 3})
	if got := d.State().Settings.DNDStart; got != 2 {
		t.Fatalf("DNDStart %d", got)
	}
	d.SetSettings(5000<<20, Settings{DNDStart: 4})
	if c := d.SettingsEpoch(); c <= 5000 {
		t.Fatalf("epoch %d is not above the saved write", c)
	}
}
