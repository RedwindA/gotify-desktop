package notify

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"

	"gotify-desktop/internal/conn"
	"gotify-desktop/internal/gotify"
	"gotify-desktop/internal/i18n"
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
	PausedUntil      time.Time
	DND              bool
	DNDStart, DNDEnd int // minutes of day; the window may wrap midnight
	// Language is the language the app shows: "en", "zh-CN", or "" to follow the system.
	Language string
	// Theme is the appearance of the window: "light", "dark", or "" to follow the system.
	Theme string
	// KeepDays is how many days messages are kept; 0 keeps them forever.
	KeepDays           int
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

// silenced reports whether a pause or do not disturb keeps a notification of level from showing.
func (s Settings) silenced(level Level, now time.Time) bool {
	return now.Before(s.PausedUntil) || s.dndActive(now) && !(level == LevelHigh && s.HighBypassesDND)
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
	// entries are the key's messages within BurstWindow; while suppressed, the last burstMaxLines.
	entries []burstEntry
	lastAt  time.Time
	name    string
	// While suppressed: every message since the burst began (once an update
	// has gone out, only those still unread then), their highest level, when
	// the burst summary last went out, and whether messages came since.
	suppressed bool
	ids        []uint
	level      Level
	shownAt    time.Time
	dirty      bool
}

// Planner turns connection events into notifications and remembers recent
// ones per (server, app) to collapse bursts: once a burst summary is out, the
// key stays quiet until BurstWindow passes without a new message for it.
// Messages that come meanwhile update the summary quietly, at most once per
// BurstWindow: in Plan, or in Flush when no message comes to trigger it.
type Planner struct {
	mu    sync.Mutex
	burst map[burstKey]*burstState
	// unread, when set, keeps those of a server's message ids that are still
	// stored and unread; an update counts only those.
	unread func(serverID int64, ids []uint) []uint
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
	var msgs []gotify.Message
	for _, m := range ev.Messages {
		pref := prefs[m.AppID]
		level, ok := LevelFor(m.Priority)
		switch {
		case !ok, pref.Muted, pref.MinPriority != nil && m.Priority < *pref.MinPriority:
			continue
		case s.silenced(level, now):
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
	out := p.endQuiet(s, now)
	var bursting []burstKey
	for _, m := range msgs {
		level, _ := LevelFor(m.Priority)
		key := burstKey{ev.ServerID, m.AppID}
		st := p.burst[key]
		if st == nil {
			st = &burstState{}
			p.burst[key] = st
		}
		st.lastAt = now
		st.name = appName(apps, m.AppID)
		entry := burstEntry{m.ID, now, headline(m, apps), level}
		if st.suppressed {
			st.ids = append(st.ids, m.ID)
			st.level = max(st.level, level)
			st.entries = append(st.entries[max(0, len(st.entries)-burstMaxLines+1):], entry)
			st.dirty = true
			continue
		}
		kept := st.entries[:0:0]
		for _, e := range st.entries {
			if now.Sub(e.at) < s.BurstWindow {
				kept = append(kept, e)
			}
		}
		st.entries = append(kept, entry)
		if len(st.entries) > s.BurstMax {
			st.suppressed, st.shownAt = true, now
			for _, e := range st.entries {
				st.ids = append(st.ids, e.id)
				st.level = max(st.level, e.level)
			}
			st.entries = st.entries[max(0, len(st.entries)-burstMaxLines):]
			bursting = append(bursting, key)
			continue
		}
		out = append(out, individual(ev.ServerID, m, level, apps))
	}
	for _, key := range bursting {
		st := p.burst[key]
		out = append(out, burstSummary(key, st, st.level))
	}
	return append(out, p.updateDue(s, now)...)
}

// Flush returns the quiet updates that are due, among them the last one of a
// burst that has ended, and drops the state of keys quiet for BurstWindow.
func (p *Planner) Flush(s Settings, now time.Time) []Planned {
	s = s.withDefaults()
	p.mu.Lock()
	defer p.mu.Unlock()
	return append(p.endQuiet(s, now), p.updateDue(s, now)...)
}

// NextFlush is when Flush next has an update to return.
func (p *Planner) NextFlush(s Settings) (time.Time, bool) {
	s = s.withDefaults()
	p.mu.Lock()
	defer p.mu.Unlock()
	var next time.Time
	for _, st := range p.burst {
		if at := st.shownAt.Add(s.BurstWindow); st.dirty && (next.IsZero() || at.Before(next)) {
			next = at
		}
	}
	return next, !next.IsZero()
}

// endQuiet drops the state of keys quiet for BurstWindow, which would start
// afresh, after the last update of a burst whose summary misses messages.
func (p *Planner) endQuiet(s Settings, now time.Time) []Planned {
	var out []Planned
	for key, st := range p.burst {
		if now.Sub(st.lastAt) < s.BurstWindow {
			continue
		}
		if st.dirty {
			out = append(out, p.update(key, st, s, now)...)
		}
		delete(p.burst, key)
	}
	return out
}

// updateDue updates the summaries that miss messages and went out at least
// BurstWindow ago.
func (p *Planner) updateDue(s Settings, now time.Time) []Planned {
	var out []Planned
	for key, st := range p.burst {
		if st.dirty && now.Sub(st.shownAt) >= s.BurstWindow {
			out = append(out, p.update(key, st, s, now)...)
		}
	}
	return out
}

// update replaces a burst's summary in place, at the quietest level, counting
// the burst's messages still unread; with none, or while a pause or do not
// disturb silences the burst, it shows nothing.
func (p *Planner) update(key burstKey, st *burstState, s Settings, now time.Time) []Planned {
	st.shownAt, st.dirty = now, false
	if s.silenced(st.level, now) {
		return nil
	}
	if p.unread != nil {
		st.ids = p.unread(key.server, st.ids)
		// The lines are of messages still unread too.
		st.entries = slices.DeleteFunc(st.entries, func(e burstEntry) bool { return !slices.Contains(st.ids, e.id) })
	}
	if len(st.ids) == 0 {
		return nil
	}
	pl := burstSummary(key, st, LevelSilent)
	pl.Quiet = true
	return []Planned{pl}
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
	actions := messageActions(ClickURL(m.Extras) != "")
	return Planned{
		Notification: Notification{
			ID:      fmt.Sprintf("s%d-m%d", serverID, m.ID),
			Title:   title,
			Body:    PlainBody(m),
			AppName: name,
			Group:   fmt.Sprintf("s%d-a%d", serverID, m.AppID),
			Level:   level,
			Actions: actions,
		},
		ImageURL:  BigImageURL(m.Extras),
		ServerID:  serverID,
		AppID:     m.AppID,
		MessageID: m.ID,
	}
}

// messageActions are the buttons of a message's notification.
func messageActions(link bool) []Action { return messageActionsIn(i18n.Current(), link) }

func messageActionsIn(lang i18n.Lang, link bool) []Action {
	actions := []Action{{ID: ActionRead, Label: i18n.In(lang, "Mark as read")}}
	if link {
		actions = append(actions, Action{ID: ActionOpen, Label: i18n.In(lang, "Open link")})
	}
	return actions
}

// summaryActions are the buttons of a summary's notification.
func summaryActions() []Action { return summaryActionsIn(i18n.Current()) }

func summaryActionsIn(lang i18n.Lang) []Action {
	return []Action{{ID: ActionRead, Label: i18n.In(lang, "Mark all as read")}}
}

// ActionSets are the sets of buttons notifications have, in every language:
// notifications from an earlier run may be in another one.
func ActionSets() [][]Action {
	var sets [][]Action
	for _, lang := range i18n.Langs {
		sets = append(sets, messageActionsIn(lang, false), messageActionsIn(lang, true), summaryActionsIn(lang))
	}
	return sets
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
			ID:      fmt.Sprintf("s%d-missed", serverID),
			Title:   i18n.T("%d missed messages", len(msgs)),
			Body:    strings.Join(lines, "\n"),
			Group:   fmt.Sprintf("s%d", serverID),
			Level:   level,
			Actions: summaryActions(),
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

// burstTitle is the title of a burst summary of n messages; an update may count one.
func burstTitle(n int, name string) string {
	if n == 1 {
		return i18n.T("1 new message from %s", name)
	}
	return i18n.T("%d new messages from %s", n, name)
}

func burstSummary(key burstKey, st *burstState, level Level) Planned {
	name := st.name
	if name == "" {
		name = "Gotify"
	}
	var lines []string
	for _, e := range st.entries {
		lines = append(lines, e.line)
	}
	return Planned{
		Notification: Notification{
			ID:      fmt.Sprintf("s%d-a%d-burst", key.server, key.app),
			Title:   burstTitle(len(st.ids), name),
			Body:    strings.Join(lines, "\n"),
			AppName: name,
			Group:   fmt.Sprintf("s%d-a%d", key.server, key.app),
			Level:   level,
			Actions: summaryActions(),
		},
		ServerID:   key.server,
		AppID:      key.app,
		MessageIDs: st.ids,
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

// BigImageURL returns the http(s) URL of the image a message shows, if any.
func BigImageURL(extras map[string]any) string {
	return httpURL(extraMap(extras, "client::notification")["bigImageUrl"])
}

// ClickURL returns the http(s) URL a message asks to open when clicked, if any.
func ClickURL(extras map[string]any) string {
	click, _ := extraMap(extras, "client::notification")["click"].(map[string]any)
	return httpURL(click["url"])
}

// PlainBody renders a message body as plain text.
func PlainBody(m gotify.Message) string {
	body := strings.TrimSpace(m.Message)
	if ct, _ := extraMap(m.Extras, "client::display")["contentType"].(string); ct == "text/markdown" {
		body = markdownToPlain(body)
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
