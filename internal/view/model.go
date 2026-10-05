// Package view is the native UI: the window's state and the functions that draw it.
package view

import (
	"context"
	"crypto/x509"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"gotify-desktop/internal/app"
	"gotify-desktop/internal/markdown"
	"gotify-desktop/internal/notify"
	"gotify-desktop/internal/store"
)

type Backend interface {
	Snapshot() *app.Snapshot
	Messages(q store.MessageQuery) ([]store.StoredMessage, error)
	DeleteMessage(ctx context.Context, serverID int64, id uint) error
	MarkRead(serverID int64, ids ...uint)
	MarkAllRead(serverID int64, appID uint)
	AddServer(ctx context.Context, in app.ServerInput) (int64, error)
	Relogin(ctx context.Context, serverID int64, user, pass string) error
	UpdateServer(ctx context.Context, serverID int64, name string, insecure bool, ca []byte) error
	RemoveServer(ctx context.Context, serverID int64) error
	SetSettings(s notify.Settings) error
	SetAppPref(serverID int64, appID uint, p store.AppPref) error
	Test() error
}

// Platform holds what the view needs from the desktop; every field is optional.
type Platform struct {
	Version        string
	DataDir        string
	OpenAtLogin    func() bool
	SetOpenAtLogin func(bool) error
	// PickCA asks for a CA certificate file and returns its name and PEM.
	PickCA func() (name string, pem []byte, err error)
	// Focused reports whether the window has the keyboard focus.
	Focused func() bool
	// Update runs fn on the UI thread and redraws; without it fn runs at once.
	Update func(fn func())
	Now    func() time.Time
}

type scope struct {
	server int64
	app    uint
}

type page int

const (
	pageMessages page = iota
	pageSettings
)

type msgKey struct {
	server int64
	id     uint
}

const pageSize = 100

type Model struct {
	be   Backend
	plat Platform
	md   *markdown.Renderer

	scope scope
	page  page
	query string

	msgs        []store.StoredMessage
	limit       int
	exhausted   bool
	loaded      bool
	loadedGen   uint64
	loadedScope scope
	loadedQuery string
	list        ui.ListState
	sidebarW    float32

	bitmaps   map[string]*ui.Bitmap
	toasts    []string
	deleting  map[msgKey]bool
	highlight msgKey
	hlUntil   time.Time
	scrollTo  msgKey
	markBusy  bool
	nextTick  time.Time

	dlg       dialog
	removeAsk bool
	removeID  int64

	settings    notify.Settings
	settingsGen uint64
	saving      int
	dndStart    time.Time
	dndEnd      time.Time
	atLogin     bool
	atLoginInit bool
}

func New(be Backend, plat Platform, images *markdown.ImageCache) *Model {
	if plat.Now == nil {
		plat.Now = time.Now
	}
	return &Model{
		be: be, plat: plat, md: markdown.New(images), limit: pageSize, sidebarW: 280,
		bitmaps: map[string]*ui.Bitmap{}, deleting: map[msgKey]bool{},
	}
}

func (m *Model) update(fn func()) {
	if m.plat.Update != nil {
		m.plat.Update(fn)
		return
	}
	fn()
}

func (m *Model) focused() bool { return m.plat.Focused != nil && m.plat.Focused() }

// Touch redraws the view, for events it cannot see, such as the window gaining focus.
func (m *Model) Touch() { m.update(func() {}) }

// Navigate shows the messages of a server and app, and highlights one.
func (m *Model) Navigate(serverID int64, appID uint, messageID uint) {
	m.update(func() {
		m.page, m.scope, m.query = pageMessages, scope{serverID, appID}, ""
		if messageID != 0 {
			m.highlight = msgKey{serverID, messageID}
			m.scrollTo = m.highlight
			m.hlUntil = m.plat.Now().Add(4 * time.Second)
		}
	})
}

func (m *Model) bitmap(serverID int64, a app.AppInfo) *ui.Bitmap {
	if a.ImageKey == "" {
		return nil
	}
	key := fmt.Sprintf("%d/%d/%s", serverID, a.ID, a.ImageKey)
	if b, ok := m.bitmaps[key]; ok {
		return b
	}
	b, err := ui.DecodeBitmap(a.Image)
	if err != nil {
		b = nil
	}
	m.bitmaps[key] = b
	return b
}

func (m *Model) async(fn func()) { go fn() }

func (m *Model) toast(s string) { m.toasts = append(m.toasts, s) }

func (m *Model) validateScope(snap *app.Snapshot) {
	if m.scope.server == 0 {
		return
	}
	sv, ok := snap.Server(m.scope.server)
	if !ok {
		m.scope = scope{}
		return
	}
	if m.scope.app != 0 {
		if _, ok := sv.App(m.scope.app); !ok {
			m.scope.app = 0
		}
	}
}

func (m *Model) scopeTitle(snap *app.Snapshot) string {
	if m.scope.server == 0 {
		return "All messages"
	}
	sv, _ := snap.Server(m.scope.server)
	if m.scope.app == 0 {
		return sv.Name
	}
	a, _ := sv.App(m.scope.app)
	if len(snap.Servers) > 1 {
		return a.Name + " · " + sv.Name
	}
	return a.Name
}

func (m *Model) scopeUnread(snap *app.Snapshot) int {
	switch {
	case m.scope.server == 0:
		return snap.Unread
	case m.scope.app == 0:
		sv, _ := snap.Server(m.scope.server)
		return sv.Unread
	}
	sv, _ := snap.Server(m.scope.server)
	a, _ := sv.App(m.scope.app)
	return a.Unread
}

func (m *Model) reload() {
	for {
		msgs, err := m.be.Messages(store.MessageQuery{ServerID: m.scope.server, AppID: m.scope.app, Search: m.query, Limit: m.limit})
		if err != nil {
			m.toast("Couldn't load messages: " + err.Error())
			return
		}
		m.msgs, m.exhausted = msgs, len(msgs) < m.limit
		if m.scrollTo != (msgKey{}) && !m.exhausted && m.indexOf(m.scrollTo) < 0 {
			m.limit += pageSize
			continue
		}
		return
	}
}

func (m *Model) indexOf(k msgKey) int {
	for i, msg := range m.msgs {
		if msg.ServerID == k.server && msg.ID == k.id {
			return i
		}
	}
	return -1
}

// refreshMessages reloads what the scope, the search or new messages changed.
func (m *Model) refreshMessages(snap *app.Snapshot) {
	if m.loaded && m.loadedGen == snap.MsgGen && m.loadedScope == m.scope && m.loadedQuery == m.query {
		return
	}
	atTop := true
	if m.loaded && m.loadedScope == m.scope && m.loadedQuery == m.query {
		first, _ := m.list.Visible()
		atTop = first == 0
		before := len(m.msgs)
		m.reload()
		if atTop && len(m.msgs) != before {
			m.list.ScrollTo(0, ui.Start)
		}
	} else {
		m.limit = pageSize
		m.reload()
		m.list.ScrollTo(0, ui.Start)
	}
	m.loaded, m.loadedGen, m.loadedScope, m.loadedQuery = true, snap.MsgGen, m.scope, m.query
}

func (m *Model) loadMore() {
	if m.exhausted || len(m.msgs) == 0 {
		return
	}
	if _, last := m.list.Visible(); last >= len(m.msgs)-15 {
		m.limit = len(m.msgs) + pageSize
		m.reload()
	}
}

// markVisibleRead marks the loaded messages read a moment after they show to a focused window.
func (m *Model) markVisibleRead() {
	if m.markBusy || !m.focused() {
		return
	}
	byServer := map[int64][]uint{}
	for _, msg := range m.msgs {
		if !msg.Read {
			byServer[msg.ServerID] = append(byServer[msg.ServerID], msg.ID)
		}
	}
	if len(byServer) == 0 {
		return
	}
	m.markBusy = true
	m.async(func() {
		time.Sleep(1200 * time.Millisecond)
		if m.focused() {
			for server, ids := range byServer {
				m.be.MarkRead(server, ids...)
			}
		}
		m.update(func() { m.markBusy = false })
	})
}

func (m *Model) markScopeRead(snap *app.Snapshot) {
	sc := m.scope
	m.async(func() {
		if sc.server != 0 {
			m.be.MarkAllRead(sc.server, sc.app)
			return
		}
		for _, sv := range snap.Servers {
			m.be.MarkAllRead(sv.ID, 0)
		}
	})
}

func (m *Model) deleteMessage(msg store.StoredMessage) {
	key := msgKey{msg.ServerID, msg.ID}
	if m.deleting[key] {
		return
	}
	m.deleting[key] = true
	m.async(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		err := m.be.DeleteMessage(ctx, msg.ServerID, msg.ID)
		m.update(func() {
			delete(m.deleting, key)
			if err != nil {
				m.toast("Couldn't delete the message: " + app.Explain(err))
			}
		})
	})
}

func (m *Model) setAppPref(serverID int64, a app.AppInfo, muted bool, min *int) {
	m.async(func() {
		if err := m.be.SetAppPref(serverID, a.ID, store.AppPref{Muted: muted, MinPriority: min}); err != nil {
			m.update(func() { m.toast("Couldn't save: " + err.Error()) })
		}
	})
}

func (m *Model) syncSettings(snap *app.Snapshot) {
	if m.saving > 0 || (m.settingsGen == snap.Gen && m.atLoginInit) {
		return
	}
	m.settingsGen = snap.Gen
	m.settings = snap.Settings
	m.dndStart, m.dndEnd = minutesToTime(m.settings.DNDStart), minutesToTime(m.settings.DNDEnd)
	if !m.atLoginInit {
		m.atLoginInit = true
		m.atLogin = m.plat.OpenAtLogin != nil && m.plat.OpenAtLogin()
	}
}

func (m *Model) saveSettings() {
	s := m.settings
	m.saving++
	m.async(func() {
		err := m.be.SetSettings(s)
		m.update(func() {
			m.saving--
			if err != nil {
				m.toast("Couldn't save settings: " + err.Error())
			}
		})
	})
}

func minutesToTime(min int) time.Time { return time.Date(2000, 1, 1, min/60, min%60, 0, 0, time.Local) }

func timeToMinutes(t time.Time) int { return t.Hour()*60 + t.Minute() }

func isMarkdown(extras map[string]any) bool {
	display, _ := extras["client::display"].(map[string]any)
	ct, _ := display["contentType"].(string)
	return ct == "text/markdown"
}

func relTime(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < -time.Minute:
		return t.Format("Jan 2, 15:04")
	case d < 45*time.Second:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", max(1, int(d.Minutes())))
	case d < 24*time.Hour && now.Day() == t.Day():
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case now.AddDate(0, 0, -1).Format("2006-01-02") == t.Format("2006-01-02"):
		return "Yesterday, " + t.Format("15:04")
	case d < 7*24*time.Hour:
		return t.Format("Mon, 15:04")
	case t.Year() == now.Year():
		return t.Format("Jan 2")
	}
	return t.Format("Jan 2, 2006")
}

func normalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("Enter the server's address.")
	}
	if !strings.HasPrefix(strings.ToLower(raw), "http://") && !strings.HasPrefix(strings.ToLower(raw), "https://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("This is not a valid address.")
	}
	return strings.TrimRight(raw, "/"), nil
}

func validPEM(pem []byte) bool {
	return x509.NewCertPool().AppendCertsFromPEM(pem)
}
