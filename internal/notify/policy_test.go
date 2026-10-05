package notify

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"gotify-desktop/internal/conn"
	"gotify-desktop/internal/gotify"
	"gotify-desktop/internal/store"
)

var t0 = time.Date(2026, 3, 1, 12, 0, 0, 0, time.Local)

var testApps = map[uint]store.App{1: {ID: 1, Name: "Backup"}, 2: {ID: 2, Name: "Web"}}

func msg(id, app uint, prio int, title, body string) gotify.Message {
	return gotify.Message{ID: id, AppID: app, Priority: prio, Title: title, Message: body}
}

func live(msgs ...gotify.Message) conn.Event {
	return conn.Event{ServerID: 1, Kind: conn.EventMessages, Messages: msgs}
}

func plan(ev conn.Event, prefs map[uint]AppPrefs, s Settings, now time.Time) []Planned {
	return NewPlanner().Plan(ev, testApps, prefs, s, now)
}

func TestLevelFor(t *testing.T) {
	for _, c := range []struct {
		p  int
		l  Level
		ok bool
	}{{-1, 0, false}, {0, 0, false}, {1, LevelSilent, true}, {3, LevelSilent, true}, {4, LevelNormal, true}, {7, LevelNormal, true}, {8, LevelHigh, true}, {10, LevelHigh, true}} {
		if l, ok := LevelFor(c.p); l != c.l || ok != c.ok {
			t.Errorf("LevelFor(%d) = %v,%v", c.p, l, ok)
		}
	}
}

func TestDNDWrapAround(t *testing.T) {
	s := DefaultSettings()
	s.DND, s.DNDStart, s.DNDEnd = true, 22*60, 7*60
	at := func(h, m int) time.Time { return time.Date(2026, 3, 1, h, m, 0, 0, time.Local) }
	for _, c := range []struct {
		h, m int
		on   bool
	}{{21, 59, false}, {22, 0, true}, {23, 30, true}, {0, 0, true}, {6, 59, true}, {7, 0, false}, {12, 0, false}} {
		if got := s.dndActive(at(c.h, c.m)); got != c.on {
			t.Errorf("%02d:%02d = %v", c.h, c.m, got)
		}
	}
	s.DNDStart, s.DNDEnd = 9*60, 17*60
	if !s.dndActive(at(12, 0)) || s.dndActive(at(18, 0)) {
		t.Error("same-day window")
	}
	s.DNDStart = s.DNDEnd
	if s.dndActive(at(17, 0)) {
		t.Error("empty window must be inactive")
	}
}

func TestDNDHighBypass(t *testing.T) {
	s := DefaultSettings()
	s.DND, s.DNDStart, s.DNDEnd = true, 0, 23*60
	ev := live(msg(1, 1, 5, "n", ""), msg(2, 1, 9, "h", ""))
	got := plan(ev, nil, s, t0)
	if len(got) != 1 || got[0].MessageID != 2 || got[0].Level != LevelHigh {
		t.Fatalf("%+v", got)
	}
	s.HighBypassesDND = false
	if got := plan(ev, nil, s, t0); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestMuteMinPriorityPausedAndZero(t *testing.T) {
	min5 := 5
	prefs := map[uint]AppPrefs{1: {Muted: true}, 2: {MinPriority: &min5}}
	ev := live(msg(1, 1, 9, "muted", ""), msg(2, 2, 4, "low", ""), msg(3, 2, 5, "ok", ""), msg(4, 3, 0, "zero", ""), msg(5, 3, 1, "quiet", ""))
	got := plan(ev, prefs, DefaultSettings(), t0)
	if len(got) != 2 || got[0].MessageID != 3 || got[1].MessageID != 5 || got[1].Level != LevelSilent {
		t.Fatalf("%+v", got)
	}
	s := DefaultSettings()
	s.PausedUntil = t0.Add(time.Minute)
	if got := plan(ev, prefs, s, t0); len(got) != 0 {
		t.Fatal("paused should show nothing")
	}
	if got := plan(ev, prefs, s, t0.Add(2*time.Minute)); len(got) != 2 {
		t.Fatal("pause should have expired")
	}
}

func TestSilentAndNonMessageEvents(t *testing.T) {
	ev := live(msg(1, 1, 9, "x", ""))
	ev.Silent = true
	if len(plan(ev, nil, DefaultSettings(), t0)) != 0 {
		t.Fatal("silent import must not notify")
	}
	if len(plan(conn.Event{Kind: conn.EventState}, nil, DefaultSettings(), t0)) != 0 {
		t.Fatal("state event")
	}
}

func TestIndividualFields(t *testing.T) {
	m := msg(42, 1, 5, "", "hello **world**")
	m.Extras = map[string]any{
		"client::display":      map[string]any{"contentType": "text/markdown"},
		"client::notification": map[string]any{"bigImageUrl": "https://x/y.png", "click": map[string]any{"url": "https://x/z"}},
	}
	got := plan(live(m), nil, DefaultSettings(), t0)
	if len(got) != 1 {
		t.Fatal(got)
	}
	p := got[0]
	if p.ID != "s1-m42" || p.Title != "Backup" || p.Body != "hello world" || p.AppName != "Backup" || p.Group != "s1-a1" || p.Level != LevelNormal ||
		p.ImageURL != "https://x/y.png" || p.ServerID != 1 || p.AppID != 1 || p.MessageID != 42 {
		t.Fatalf("%+v", p)
	}
	if ClickURL(m.Extras) != "https://x/z" {
		t.Fatal("click url")
	}
	unknown := plan(live(msg(1, 99, 5, "", "b")), nil, DefaultSettings(), t0)[0]
	if unknown.Title != "Gotify" {
		t.Fatalf("title fallback: %q", unknown.Title)
	}
}

func TestExtrasURLValidation(t *testing.T) {
	for _, bad := range []any{"javascript:alert(1)", "file:///etc/passwd", "ftp://x/y", "", "http://", 5, nil} {
		ex := map[string]any{"client::notification": map[string]any{"bigImageUrl": bad, "click": map[string]any{"url": bad}}}
		if bigImageURL(ex) != "" || ClickURL(ex) != "" {
			t.Errorf("accepted %v", bad)
		}
	}
	if bigImageURL(nil) != "" || ClickURL(nil) != "" {
		t.Error("nil extras")
	}
	if ClickURL(map[string]any{"client::notification": map[string]any{"click": "bad"}}) != "" {
		t.Error("malformed click")
	}
}

func TestMarkdownToPlain(t *testing.T) {
	md := func(s string) string {
		return PlainBody(gotify.Message{Message: s, Extras: map[string]any{"client::display": map[string]any{"contentType": "text/markdown"}}})
	}
	for in, want := range map[string]string{
		"**bold** and _it_":              "bold and it",
		"see [the docs](https://x.y/z)!": "see the docs!",
		"# Title\n\ntext":                "Title\ntext",
		"- a\n- b":                       "• a\n• b",
		"`code` here":                    "code here",
		"![alt](http://i/p.png)":         "alt",
		"```\nfmt.Println()\n```":        "fmt.Println()",
		"line1  \nline2":                 "line1\nline2",
	} {
		if got := md(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
	if got := PlainBody(gotify.Message{Message: "**not** parsed"}); got != "**not** parsed" {
		t.Errorf("plain text altered: %q", got)
	}
	long := PlainBody(gotify.Message{Message: strings.Repeat("é", 500)})
	if r := []rune(long); len(r) != maxBodyRunes+1 || r[len(r)-1] != '…' {
		t.Errorf("truncate: %d runes", len(r))
	}
}

func TestCatchUpSummary(t *testing.T) {
	var msgs []gotify.Message
	for i := uint(1); i <= 8; i++ {
		msgs = append(msgs, msg(i, 1+i%2, 5, fmt.Sprintf("t%d", i), ""))
	}
	msgs = append(msgs, msg(9, 1, 0, "hidden", ""), msg(10, 1, 9, "", "first line\nsecond"))
	ev := live(msgs...)
	ev.CatchUp = true
	got := plan(ev, nil, DefaultSettings(), t0)
	if len(got) != 1 {
		t.Fatalf("%+v", got)
	}
	p := got[0]
	lines := strings.Split(p.Body, "\n")
	if p.ID != "s1-missed" || p.Title != "9 missed messages" || p.Group != "s1" || p.Level != LevelHigh || len(lines) != 5 ||
		lines[4] != "Backup: first line" || lines[0] != "Web: t5" {
		t.Fatalf("%+v\n%v", p, lines)
	}
	ev.Messages = msgs[:3]
	if got := plan(ev, nil, DefaultSettings(), t0); len(got) != 3 {
		t.Fatalf("at the threshold messages stay individual, got %d", len(got))
	}
}

func TestBurstSummary(t *testing.T) {
	pl := NewPlanner()
	s := DefaultSettings()
	push := func(now time.Time, id uint, app uint, prio int) []Planned {
		return pl.Plan(live(msg(id, app, prio, fmt.Sprintf("t%d", id), "")), testApps, nil, s, now)
	}
	for i := uint(1); i <= 3; i++ {
		if got := push(t0.Add(time.Duration(i)*time.Second), i, 1, 5); len(got) != 1 || got[0].MessageID != i {
			t.Fatalf("msg %d: %+v", i, got)
		}
	}
	got := push(t0.Add(4*time.Second), 4, 1, 9)
	if len(got) != 1 || got[0].ID != "s1-a1-burst" || got[0].Title != "4 new messages from Backup" || got[0].Level != LevelHigh || got[0].Body != "t2\nt3\nt4" {
		t.Fatalf("%+v", got)
	}
	if got := push(t0.Add(5*time.Second), 5, 2, 5); len(got) != 1 || got[0].MessageID != 5 {
		t.Fatalf("other app is independent: %+v", got)
	}
	if got := push(t0.Add(6*time.Second), 6, 1, 5); len(got) != 0 {
		t.Fatalf("burst key must stay quiet after its summary: %+v", got)
	}
	if got := push(t0.Add(14*time.Second), 7, 1, 5); len(got) != 0 {
		t.Fatalf("messages keep the quiet period going: %+v", got)
	}
	if got := push(t0.Add(30*time.Second), 8, 1, 5); len(got) != 1 || got[0].MessageID != 8 {
		t.Fatalf("a quiet window should start fresh: %+v", got)
	}
}

func TestBurstWithinOneEvent(t *testing.T) {
	var msgs []gotify.Message
	for i := uint(1); i <= 6; i++ {
		msgs = append(msgs, msg(i, 1, 5, fmt.Sprintf("t%d", i), ""))
	}
	got := plan(live(msgs...), nil, DefaultSettings(), t0)
	if len(got) != 4 || got[3].ID != "s1-a1-burst" || got[3].Title != "6 new messages from Backup" {
		t.Fatalf("%+v", got)
	}
}

func TestParseID(t *testing.T) {
	for id, want := range map[string][4]any{
		"s1-m42":         {int64(1), uint(0), uint(42), KindMessage},
		"s12-a3-burst":   {int64(12), uint(3), uint(0), KindBurst},
		"s7-missed":      {int64(7), uint(0), uint(0), KindMissed},
		"s7-x":           {int64(0), uint(0), uint(0), KindUnknown},
		"":               {int64(0), uint(0), uint(0), KindUnknown},
		"https://evil/x": {int64(0), uint(0), uint(0), KindUnknown},
	} {
		s, a, m, k := ParseID(id)
		if s != want[0] || a != want[1] || m != want[2] || k != want[3] {
			t.Errorf("%q -> %v %v %v %v", id, s, a, m, k)
		}
	}
}

func TestIsActivationLaunch(t *testing.T) {
	if !IsActivationLaunch([]string{"app.exe", "-Embedding"}) || !IsActivationLaunch([]string{"/embedding"}) || IsActivationLaunch([]string{"app.exe", "--hidden"}) {
		t.Fatal("embedding detection")
	}
}

func TestActivatorReplaysPending(t *testing.T) {
	var a activator
	a.fire("a")
	a.fire("")
	a.fire("b")
	var got []string
	a.set(func(id string) { got = append(got, id) })
	a.fire("c")
	if strings.Join(got, ",") != "a,b,c" {
		t.Fatal(got)
	}
}
