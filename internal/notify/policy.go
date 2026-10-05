package notify

import (
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"

	"gotify-desktop/internal/conn"
	"gotify-desktop/internal/gotify"
	"gotify-desktop/internal/mdtext"
	"gotify-desktop/internal/store"
)

type AppPrefs struct {
	Muted       bool
	MinPriority *int
}

// Settings holds the global notification policy. Zero BurstWindow, BurstMax
// and CatchUpSummaryOver select the defaults; HighBypassesDND is used as is.
type Settings struct {
	PausedUntil        time.Time
	DND                bool
	DNDStart, DNDEnd   int // minutes of day; the window may wrap midnight
	HighBypassesDND    bool
	BurstWindow        time.Duration
	BurstMax           int
	CatchUpSummaryOver int
}

func DefaultSettings() Settings {
	return Settings{DNDStart: 22 * 60, DNDEnd: 7 * 60, HighBypassesDND: true, BurstWindow: 10 * time.Second, BurstMax: 3, CatchUpSummaryOver: 3}
}

func (s Settings) withDefaults() Settings {
	d := DefaultSettings()
	if s.BurstWindow <= 0 {
		s.BurstWindow = d.BurstWindow
	}
	if s.BurstMax <= 0 {
		s.BurstMax = d.BurstMax
	}
	if s.CatchUpSummaryOver <= 0 {
		s.CatchUpSummaryOver = d.CatchUpSummaryOver
	}
	return s
}

func (s Settings) dndActive(now time.Time) bool {
	if !s.DND || s.DNDStart == s.DNDEnd {
		return false
	}
	m := now.Hour()*60 + now.Minute()
	if s.DNDStart < s.DNDEnd {
		return m >= s.DNDStart && m < s.DNDEnd
	}
	return m >= s.DNDStart || m < s.DNDEnd
}

// LevelFor maps a Gotify priority to a notification level; priority 0 is never shown.
func LevelFor(priority int) (Level, bool) {
	switch {
	case priority <= 0:
		return LevelSilent, false
	case priority <= 3:
		return LevelSilent, true
	case priority <= 7:
		return LevelNormal, true
	default:
		return LevelHigh, true
	}
}

type Planned struct {
	Notification
	ImageURL  string
	ServerID  int64
	AppID     uint
	MessageID uint
	// MessageIDs are the messages a summary stands for.
	MessageIDs []uint
}

const (
	maxBodyRunes    = 300
	summaryMaxLines = 5
	burstMaxLines   = 3
)

type burstKey struct {
	server int64
	app    uint
}

type burstEntry struct {
	id    uint
	at    time.Time
	line  string
	level Level
}

type burstState struct {
	entries    []burstEntry
	lastAt     time.Time
	suppressed bool
}

// Planner turns connection events into notifications and remembers recent
// ones per (server, app) to collapse bursts: once a burst summary is out, the
// key stays quiet until BurstWindow passes without a new message for it.
type Planner struct {
	mu    sync.Mutex
	burst map[burstKey]*burstState
}

func NewPlanner() *Planner { return &Planner{burst: map[burstKey]*burstState{}} }

func (p *Planner) Plan(ev conn.Event, apps map[uint]store.App, prefs map[uint]AppPrefs, s Settings, now time.Time) []Planned {
	if ev.Kind != conn.EventMessages || ev.Silent {
		return nil
	}
	s = s.withDefaults()
	if now.Before(s.PausedUntil) {
		return nil
	}
	dnd := s.dndActive(now)
	var msgs []gotify.Message
	for _, m := range ev.Messages {
		pref := prefs[m.AppID]
		level, ok := LevelFor(m.Priority)
		switch {
		case !ok, pref.Muted, pref.MinPriority != nil && m.Priority < *pref.MinPriority:
			continue
		case dnd && !(level == LevelHigh && s.HighBypassesDND):
			continue
		}
		msgs = append(msgs, m)
	}
	if len(msgs) == 0 {
		return nil
	}
	if ev.CatchUp && len(msgs) > s.CatchUpSummaryOver {
		return []Planned{missedSummary(ev.ServerID, msgs, apps)}
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	var out []Planned
	var bursting []burstKey
	for _, m := range msgs {
		level, _ := LevelFor(m.Priority)
		key := burstKey{ev.ServerID, m.AppID}
		st := p.burst[key]
		if st == nil {
			st = &burstState{}
			p.burst[key] = st
		}
		if now.Sub(st.lastAt) >= s.BurstWindow {
			st.entries, st.suppressed = nil, false
		}
		st.lastAt = now
		kept := st.entries[:0:0]
		for _, e := range st.entries {
			if now.Sub(e.at) < s.BurstWindow {
				kept = append(kept, e)
			}
		}
		st.entries = append(kept, burstEntry{m.ID, now, headline(m, apps), level})
		if st.suppressed {
			continue
		}
		if len(st.entries) > s.BurstMax {
			st.suppressed = true
			bursting = append(bursting, key)
			continue
		}
		out = append(out, individual(ev.ServerID, m, level, apps))
	}
	for _, key := range bursting {
		out = append(out, burstSummary(key, p.burst[key].entries, apps))
	}
	return out
}

func containsKey(keys []burstKey, k burstKey) bool {
	for _, x := range keys {
		if x == k {
			return true
		}
	}
	return false
}

func appName(apps map[uint]store.App, id uint) string {
	if a, ok := apps[id]; ok {
		return a.Name
	}
	return ""
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}

func headline(m gotify.Message, apps map[uint]store.App) string {
	h := m.Title
	if h == "" {
		h = firstLine(PlainBody(m))
	}
	return h
}

func individual(serverID int64, m gotify.Message, level Level, apps map[uint]store.App) Planned {
	name := appName(apps, m.AppID)
	title := m.Title
	if title == "" {
		title = name
	}
	if title == "" {
		title = "Gotify"
	}
	return Planned{
		Notification: Notification{
			ID:      fmt.Sprintf("s%d-m%d", serverID, m.ID),
			Title:   title,
			Body:    PlainBody(m),
			AppName: name,
			Group:   fmt.Sprintf("s%d-a%d", serverID, m.AppID),
			Level:   level,
		},
		ImageURL:  bigImageURL(m.Extras),
		ServerID:  serverID,
		AppID:     m.AppID,
		MessageID: m.ID,
	}
}

func missedSummary(serverID int64, msgs []gotify.Message, apps map[uint]store.App) Planned {
	var lines []string
	var msgIDs []uint
	level := LevelSilent
	for i, m := range msgs {
		msgIDs = append(msgIDs, m.ID)
		l, _ := LevelFor(m.Priority)
		level = max(level, l)
		if i >= len(msgs)-summaryMaxLines {
			lines = append(lines, appLine(apps, m))
		}
	}
	return Planned{
		Notification: Notification{
			ID:    fmt.Sprintf("s%d-missed", serverID),
			Title: fmt.Sprintf("%d missed messages", len(msgs)),
			Body:  strings.Join(lines, "\n"),
			Group: fmt.Sprintf("s%d", serverID),
			Level: level,
		},
		ServerID:   serverID,
		MessageIDs: msgIDs,
	}
}

func appLine(apps map[uint]store.App, m gotify.Message) string {
	h := headline(m, apps)
	if name := appName(apps, m.AppID); name != "" {
		return name + ": " + h
	}
	return h
}

func burstSummary(key burstKey, entries []burstEntry, apps map[uint]store.App) Planned {
	name := appName(apps, key.app)
	if name == "" {
		name = "Gotify"
	}
	level := LevelSilent
	for _, e := range entries {
		level = max(level, e.level)
	}
	var lines []string
	var msgIDs []uint
	for _, e := range entries {
		msgIDs = append(msgIDs, e.id)
	}
	for _, e := range entries[max(0, len(entries)-burstMaxLines):] {
		lines = append(lines, e.line)
	}
	return Planned{
		Notification: Notification{
			ID:      fmt.Sprintf("s%d-a%d-burst", key.server, key.app),
			Title:   fmt.Sprintf("%d new messages from %s", len(entries), name),
			Body:    strings.Join(lines, "\n"),
			AppName: name,
			Group:   fmt.Sprintf("s%d-a%d", key.server, key.app),
			Level:   level,
		},
		ServerID:   key.server,
		AppID:      key.app,
		MessageIDs: msgIDs,
	}
}

func extraMap(extras map[string]any, key string) map[string]any {
	m, _ := extras[key].(map[string]any)
	return m
}

func httpURL(v any) string {
	s, _ := v.(string)
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	return s
}

func bigImageURL(extras map[string]any) string {
	return httpURL(extraMap(extras, "client::notification")["bigImageUrl"])
}

// ClickURL returns the http(s) URL a message asks to open when clicked, if any.
func ClickURL(extras map[string]any) string {
	click, _ := extraMap(extras, "client::notification")["click"].(map[string]any)
	return httpURL(click["url"])
}

// PlainBody renders a message body as plain text of at most ~300 runes.
func PlainBody(m gotify.Message) string {
	body := strings.TrimSpace(m.Message)
	if ct, _ := extraMap(m.Extras, "client::display")["contentType"].(string); ct == "text/markdown" {
		body = markdownToPlain(body)
	}
	if utf8.RuneCountInString(body) > maxBodyRunes {
		body = string([]rune(body)[:maxBodyRunes]) + "…"
	}
	return body
}

func markdownToPlain(src string) string {
	b := []byte(src)
	doc := goldmark.DefaultParser().Parse(text.NewReader(b))
	var sb strings.Builder
	newline := func() {
		if s := sb.String(); s != "" && !strings.HasSuffix(s, "\n") {
			sb.WriteByte('\n')
		}
	}
	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		switch n := n.(type) {
		case *ast.Text:
			if entering {
				if _, code := n.Parent().(*ast.CodeSpan); code {
					sb.Write(n.Segment.Value(b))
				} else {
					sb.WriteString(mdtext.Decode(n.Segment.Value(b)))
				}
				if n.HardLineBreak() || n.SoftLineBreak() {
					sb.WriteByte('\n')
				}
			}
		case *ast.String:
			if entering {
				sb.Write(n.Value)
			}
		case *ast.CodeBlock, *ast.FencedCodeBlock:
			if entering {
				lines := n.Lines()
				for i := 0; i < lines.Len(); i++ {
					seg := lines.At(i)
					sb.Write(seg.Value(b))
				}
				newline()
			}
			return ast.WalkSkipChildren, nil
		case *ast.HTMLBlock, *ast.RawHTML:
			return ast.WalkSkipChildren, nil
		case *ast.ListItem:
			if entering {
				newline()
				sb.WriteString("• ")
			}
		case *ast.Paragraph, *ast.Heading, *ast.Blockquote, *ast.ThematicBreak:
			if !entering {
				newline()
			}
		}
		return ast.WalkContinue, nil
	})
	return strings.TrimSpace(sb.String())
}
