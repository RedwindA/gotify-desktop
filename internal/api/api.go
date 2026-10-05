// Package api is what the web UI calls: a service bound with mygo.Bind, the
// events it listens to, and the JSON shapes of both. It has no UI of its own.
package api

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"

	"gotify-desktop/internal/app"
	"gotify-desktop/internal/conn"
	"gotify-desktop/internal/i18n"
	"gotify-desktop/internal/notify"
	"gotify-desktop/internal/store"
)

// Backend is the part of the controller the UI uses; *app.App implements it.
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

// Platform holds what the service needs from the desktop; every field is optional.
type Platform struct {
	Version        string
	OpenAtLogin    func() bool
	SetOpenAtLogin func(bool) error
	// PickCA asks for a CA certificate file and returns its name and contents,
	// or empty values when the user canceled.
	PickCA   func(ctx context.Context) (name string, data []byte, err error)
	OpenURL  func(url string)
	CopyText func(text string)
}

// StateChanged carries the new state after every change.
var StateChanged = mygo.NewEvent[State]("state")

// NavigateRequested asks the page to show a message, after a notification was clicked.
var NavigateRequested = mygo.NewEvent[Navigation]("navigate")

// ConnState is the state of a server's connection.
type ConnState string

const (
	StateConnected    ConnState = "connected"
	StateConnecting   ConnState = "connecting"
	StateBackoff      ConnState = "backoff"
	StateAuthFailed   ConnState = "authFailed"
	StateDisconnected ConnState = "disconnected"
)

// State is everything the page shows besides messages.
type State struct {
	Servers  []Server `json:"servers"`
	Unread   int      `json:"unread"`
	Settings Settings `json:"settings"`
	// Gen grows with every change: a state with a lower one is out of date.
	Gen uint64 `json:"gen"`
	// MsgGen changes whenever messages were added, read or deleted.
	MsgGen uint64 `json:"msgGen"`
	// SettingsSeq is the sequence number of the newest settings write; see SettingsEpoch.
	SettingsSeq uint64 `json:"settingsSeq"`
	// Language is the language to show: Settings.Language, or the system's.
	Language    Language `json:"lang"`
	Version     string   `json:"version"`
	DataDir     string   `json:"dataDir"`
	OpenAtLogin bool     `json:"openAtLogin"`
}

// Server is a Gotify server and its connection.
type Server struct {
	ID       int64     `json:"id"`
	Name     string    `json:"name"`
	URL      string    `json:"url"`
	Insecure bool      `json:"insecure"`
	HasCA    bool      `json:"hasCA"`
	State    ConnState `json:"state"`
	// Error explains why the server is not connected.
	Error string `json:"error"`
	// RetryAt is when a server in backoff tries again.
	RetryAt *time.Time `json:"retryAt"`
	Apps    []App      `json:"apps"`
	Unread  int        `json:"unread"`
}

// App is an application of a server, which sends messages.
type App struct {
	ID          uint   `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// ImageKey changes with the app's image; empty when it has none. AppImage returns it.
	ImageKey    string `json:"imageKey"`
	Unread      int    `json:"unread"`
	Muted       bool   `json:"muted"`
	MinPriority *int   `json:"minPriority"`
}

// Settings are the notification settings the user edits.
type Settings struct {
	// PausedUntil is when paused notifications resume; null when they are not paused.
	PausedUntil *time.Time `json:"pausedUntil"`
	DND         bool       `json:"dnd"`
	// DNDStart and DNDEnd are minutes of the day; the window may wrap midnight.
	DNDStart        int  `json:"dndStart"`
	DNDEnd          int  `json:"dndEnd"`
	HighBypassesDND bool `json:"highBypassesDnd"`
	// Language is the language the user chose; "" follows the system.
	Language Language `json:"language"`
}

// Language is a language the app can show, or "" for the system's.
type Language string

const (
	LanguageSystem  Language = ""
	LanguageEnglish Language = "en"
	LanguageChinese Language = "zh-CN"
)

// Navigation names a server, an app and a message to show; zero values widen it.
type Navigation struct {
	ServerID  int64 `json:"serverId"`
	AppID     uint  `json:"appId"`
	MessageID uint  `json:"messageId"`
}

// Query selects messages, newest first.
type Query struct {
	// ServerID and AppID narrow the messages to a server and one of its apps; 0 is all.
	ServerID int64  `json:"serverId"`
	AppID    uint   `json:"appId"`
	Search   string `json:"search"`
	Limit    int    `json:"limit"`
	// Include grows the page until it holds this message, if it is in the scope.
	Include *MessageRef `json:"include"`
}

// MessageRef identifies a message.
type MessageRef struct {
	ServerID int64 `json:"serverId"`
	ID       uint  `json:"id"`
}

// Message is a received message.
type Message struct {
	ServerID int64     `json:"serverId"`
	ID       uint      `json:"id"`
	AppID    uint      `json:"appId"`
	Title    string    `json:"title"`
	Body     string    `json:"body"`
	Markdown bool      `json:"markdown"`
	Priority int       `json:"priority"`
	Date     time.Time `json:"date"`
	Read     bool      `json:"read"`
	// ClickURL is where clicking the message's notification leads.
	ClickURL string `json:"clickUrl"`
}

// MessagePage is a page of messages.
type MessagePage struct {
	Messages []Message `json:"messages"`
	// HasMore is true when a larger limit would return more messages.
	HasMore bool `json:"hasMore"`
	// MsgGen is the State.MsgGen the page was read at.
	MsgGen uint64 `json:"msgGen"`
}

// ServerForm adds a server, edits it or signs in to it again.
type ServerForm struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	User     string `json:"user"`
	Pass     string `json:"pass"`
	Insecure bool   `json:"insecure"`
	// CA is a PEM CA certificate to trust; empty for none.
	CA string `json:"ca"`
	// KeepCA makes UpdateServer leave the server's CA certificate as it is.
	KeepCA bool `json:"keepCA"`
}

// CAFile is a CA certificate the user chose.
type CAFile struct {
	Name string `json:"name"`
	PEM  string `json:"pem"`
}

// AppPrefs are the notification preferences of an app.
type AppPrefs struct {
	Muted bool `json:"muted"`
	// MinPriority hides notifications of lower priority; null shows all.
	MinPriority *int `json:"minPriority"`
}

const pageSize = 100

// Controller owns the service and tells pages about changes.
type Controller struct {
	svc *Desktop

	started   atomic.Bool
	dirty     atomic.Bool
	closed    atomic.Bool
	emitState func(State)
	emitNav   func(Navigation)
	// settle delays a state broadcast so that bursts of changes send one.
	settle time.Duration
}

// New returns the controller of a service, to bind before the app runs. Start
// gives it its backend, before any page can call it.
func New(plat Platform) *Controller {
	return &Controller{
		svc:       &Desktop{plat: plat},
		emitState: func(s State) { StateChanged.Broadcast(s) },
		emitNav:   func(n Navigation) { NavigateRequested.Broadcast(n) },
		settle:    50 * time.Millisecond,
	}
}

// Redirect sends the events to these functions instead of the pages, for
// tests and previews without windows.
func (c *Controller) Redirect(state func(State), nav func(Navigation)) {
	c.emitState, c.emitNav = state, nav
}

// Start serves be, whose data is in dataDir.
func (c *Controller) Start(be Backend, dataDir string) {
	c.svc.be, c.svc.dataDir = be, dataDir
	c.started.Store(true)
}

// Service returns the value to bind.
func (c *Controller) Service() *Desktop { return c.svc }

// Close stops the broadcasts.
func (c *Controller) Close() { c.closed.Store(true) }

// Changed tells pages that the state changed. Calls coalesce and never block.
func (c *Controller) Changed() {
	if !c.started.Load() || c.closed.Load() || !c.dirty.CompareAndSwap(false, true) {
		return
	}
	go func() {
		time.Sleep(c.settle)
		c.dirty.Store(false)
		if !c.closed.Load() {
			c.emitState(c.svc.State())
		}
	}()
}

// Navigate asks pages to show a message, or the messages of a server or an app.
// A page that is not loaded yet takes it with TakeNavigation.
func (c *Controller) Navigate(n Navigation) {
	c.svc.mu.Lock()
	c.svc.pendingNav = &n
	c.svc.mu.Unlock()
	c.emitNav(n)
}

// Desktop is the service the page calls.
type Desktop struct {
	be      Backend
	plat    Platform
	dataDir string

	mu         sync.Mutex
	pendingNav *Navigation
	// settingsMu serializes settings writes, which read, change and write them.
	settingsMu sync.Mutex
	// settingsSeq is the sequence number of the newest settings write.
	settingsSeq atomic.Uint64
	// epoch is the newest SettingsEpoch handed out.
	epoch atomic.Uint64
}

// State returns the servers, their apps, the unread counts and the settings.
func (d *Desktop) State() State {
	seq := d.settingsSeq.Load()
	snap := d.be.Snapshot()
	s := State{Unread: snap.Unread, Gen: snap.Gen, MsgGen: snap.MsgGen, SettingsSeq: seq, Version: d.plat.Version, DataDir: d.dataDir, Servers: []Server{}}
	if d.plat.OpenAtLogin != nil {
		s.OpenAtLogin = d.plat.OpenAtLogin()
	}
	s.Settings = settingsOf(snap.Settings)
	s.Language = Language(i18n.Resolve(snap.Settings.Language))
	for _, sv := range snap.Servers {
		out := Server{ID: sv.ID, Name: sv.Name, URL: sv.URL, Insecure: sv.Insecure, HasCA: sv.HasCA,
			State: connState(sv.State), Error: sv.Err, Unread: sv.Unread, Apps: []App{}}
		if sv.State == conn.Backoff && !sv.RetryAt.IsZero() {
			t := sv.RetryAt
			out.RetryAt = &t
		}
		for _, a := range sv.Apps {
			out.Apps = append(out.Apps, App{ID: a.ID, Name: a.Name, Description: a.Description, ImageKey: a.ImageKey,
				Unread: a.Unread, Muted: a.Muted, MinPriority: a.MinPriority})
		}
		s.Servers = append(s.Servers, out)
	}
	return s
}

func connState(s conn.State) ConnState {
	switch s {
	case conn.Connected:
		return StateConnected
	case conn.Connecting:
		return StateConnecting
	case conn.Backoff:
		return StateBackoff
	case conn.AuthFailed:
		return StateAuthFailed
	}
	return StateDisconnected
}

func settingsOf(s notify.Settings) Settings {
	out := Settings{DND: s.DND, DNDStart: s.DNDStart, DNDEnd: s.DNDEnd, HighBypassesDND: s.HighBypassesDND, Language: Language(s.Language)}
	if !s.PausedUntil.IsZero() {
		t := s.PausedUntil
		out.PausedUntil = &t
	}
	return out
}

// AppImage returns the image of an app as a data URL, or "" when it has none.
func (d *Desktop) AppImage(serverID int64, appID uint) string {
	sv, ok := d.be.Snapshot().Server(serverID)
	if !ok {
		return ""
	}
	a, ok := sv.App(appID)
	if !ok || len(a.Image) == 0 {
		return ""
	}
	ct := http.DetectContentType(a.Image)
	if !strings.HasPrefix(ct, "image/") {
		return ""
	}
	return "data:" + ct + ";base64," + base64.StdEncoding.EncodeToString(a.Image)
}

// Messages returns the newest messages that match q.
func (d *Desktop) Messages(q Query) (MessagePage, error) {
	gen := d.be.Snapshot().MsgGen
	limit := q.Limit
	if limit <= 0 {
		limit = pageSize
	}
	for {
		msgs, err := d.be.Messages(store.MessageQuery{ServerID: q.ServerID, AppID: q.AppID, Search: strings.TrimSpace(q.Search), Limit: limit + 1})
		if err != nil {
			return MessagePage{}, err
		}
		more := len(msgs) > limit
		if more {
			msgs = msgs[:limit]
		}
		if q.Include != nil && more && !contains(msgs, *q.Include) {
			limit += pageSize
			continue
		}
		page := MessagePage{HasMore: more, MsgGen: gen, Messages: make([]Message, 0, len(msgs))}
		for _, m := range msgs {
			page.Messages = append(page.Messages, Message{ServerID: m.ServerID, ID: m.ID, AppID: m.AppID, Title: m.Title,
				Body: m.Message.Message, Markdown: isMarkdown(m.Extras), Priority: m.Priority, Date: m.Date, Read: m.Read,
				ClickURL: notify.ClickURL(m.Extras)})
		}
		return page, nil
	}
}

func contains(msgs []store.StoredMessage, r MessageRef) bool {
	for _, m := range msgs {
		if m.ServerID == r.ServerID && m.ID == r.ID {
			return true
		}
	}
	return false
}

func isMarkdown(extras map[string]any) bool {
	display, _ := extras["client::display"].(map[string]any)
	ct, _ := display["contentType"].(string)
	return ct == "text/markdown"
}

// MarkRead marks messages of a server read.
func (d *Desktop) MarkRead(serverID int64, ids []uint) {
	if len(ids) > 0 {
		d.be.MarkRead(serverID, ids...)
	}
}

// MarkAllRead marks the messages of an app, a server (appID 0) or every server (serverID 0) read.
func (d *Desktop) MarkAllRead(serverID int64, appID uint) {
	if serverID != 0 {
		d.be.MarkAllRead(serverID, appID)
		return
	}
	for _, sv := range d.be.Snapshot().Servers {
		d.be.MarkAllRead(sv.ID, 0)
	}
}

// DeleteMessage deletes a message on its server and here.
func (d *Desktop) DeleteMessage(ctx context.Context, serverID int64, id uint) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return explain(d.be.DeleteMessage(ctx, serverID, id))
}

// AddServer signs in to a server and adds it. It returns the new server's ID.
func (d *Desktop) AddServer(ctx context.Context, f ServerForm) (int64, error) {
	raw, err := normalizeURL(f.URL)
	if err != nil {
		return 0, err
	}
	if f.User == "" || f.Pass == "" {
		return 0, errors.New(i18n.T("Enter your username and password."))
	}
	ca, err := caOf(f.CA)
	if err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	id, err := d.be.AddServer(ctx, app.ServerInput{Name: strings.TrimSpace(f.Name), URL: raw, User: f.User, Pass: f.Pass, Insecure: f.Insecure, CACertPEM: ca})
	return id, explain(err)
}

// UpdateServer changes a server's name and TLS options.
func (d *Desktop) UpdateServer(ctx context.Context, serverID int64, f ServerForm) error {
	var ca []byte // nil keeps the server's CA, empty removes it
	if !f.KeepCA {
		var err error
		if ca, err = caOf(f.CA); err != nil {
			return err
		}
		if ca == nil {
			ca = []byte{}
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return explain(d.be.UpdateServer(ctx, serverID, strings.TrimSpace(f.Name), f.Insecure, ca))
}

// Relogin signs in to a server again after its session ended.
func (d *Desktop) Relogin(ctx context.Context, serverID int64, user, pass string) error {
	if user == "" || pass == "" {
		return errors.New(i18n.T("Enter your username and password."))
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return explain(d.be.Relogin(ctx, serverID, user, pass))
}

// RemoveServer signs out of a server and deletes its messages from this computer.
func (d *Desktop) RemoveServer(ctx context.Context, serverID int64) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	return explain(d.be.RemoveServer(ctx, serverID))
}

// PickCA asks for a CA certificate file. It returns null when the user canceled.
func (d *Desktop) PickCA(ctx context.Context) (*CAFile, error) {
	if d.plat.PickCA == nil {
		return nil, errors.New(i18n.T("Choosing files is not available."))
	}
	name, data, err := d.plat.PickCA(ctx)
	if err != nil || (name == "" && data == nil) {
		return nil, err
	}
	if !x509.NewCertPool().AppendCertsFromPEM(data) {
		return nil, errors.New(i18n.T("This file is not a PEM certificate."))
	}
	return &CAFile{Name: name, PEM: string(data)}, nil
}

// SettingsEpoch returns a number for a page to number its settings writes
// from: epoch<<20 plus a count of its writes. Every call returns a higher one
// than before, above every write saved, so a page loaded later wins over one
// loaded earlier, even while the earlier page's writes are still on their way.
func (d *Desktop) SettingsEpoch() uint64 {
	for {
		cur := d.epoch.Load()
		next := max(cur, d.settingsSeq.Load()>>20) + 1
		if d.epoch.CompareAndSwap(cur, next) {
			return next
		}
	}
}

// SetSettings saves the notification settings.
//
// seq orders the writes, which may arrive in any order: a write numbered no
// higher than one already saved is dropped. Pages number their writes on from
// SettingsEpoch.
func (d *Desktop) SetSettings(seq uint64, s Settings) error {
	d.settingsMu.Lock()
	defer d.settingsMu.Unlock()
	if seq <= d.settingsSeq.Load() {
		return nil
	}
	// Numbered once written, failed or not, so that a state with this number
	// holds the outcome: State reads the number before the settings.
	defer d.settingsSeq.Store(seq)
	cur := d.be.Snapshot().Settings
	cur.DND, cur.DNDStart, cur.DNDEnd, cur.HighBypassesDND = s.DND, clampMinutes(s.DNDStart), clampMinutes(s.DNDEnd), s.HighBypassesDND
	switch s.Language {
	case LanguageEnglish, LanguageChinese:
		cur.Language = string(s.Language)
	default:
		cur.Language = ""
	}
	cur.PausedUntil = time.Time{}
	if s.PausedUntil != nil {
		cur.PausedUntil = *s.PausedUntil
	}
	return d.be.SetSettings(cur)
}

func clampMinutes(m int) int { return min(max(m, 0), 24*60-1) }

// SetAppPrefs saves the notification preferences of an app.
func (d *Desktop) SetAppPrefs(serverID int64, appID uint, p AppPrefs) error {
	return d.be.SetAppPref(serverID, appID, store.AppPref{Muted: p.Muted, MinPriority: p.MinPriority})
}

// SetOpenAtLogin starts the app at login, or stops doing so.
func (d *Desktop) SetOpenAtLogin(on bool) error {
	if d.plat.SetOpenAtLogin == nil {
		return errors.New(i18n.T("Starting at login is not available."))
	}
	return d.plat.SetOpenAtLogin(on)
}

// TestNotification shows a sample notification.
func (d *Desktop) TestNotification() error { return d.be.Test() }

// OpenURL opens an http(s) or mailto URL in the default app.
func (d *Desktop) OpenURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "mailto") {
		return errors.New(i18n.T("Can't open %q.", raw))
	}
	if d.plat.OpenURL != nil {
		d.plat.OpenURL(u.String())
	}
	return nil
}

// CopyText puts text on the clipboard.
func (d *Desktop) CopyText(text string) {
	if d.plat.CopyText != nil {
		d.plat.CopyText(text)
	}
}

// TakeNavigation returns the navigation a clicked notification asked for, once, or null.
func (d *Desktop) TakeNavigation() *Navigation {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := d.pendingNav
	d.pendingNav = nil
	return n
}

func caOf(pem string) ([]byte, error) {
	if strings.TrimSpace(pem) == "" {
		return nil, nil
	}
	if !x509.NewCertPool().AppendCertsFromPEM([]byte(pem)) {
		return nil, errors.New(i18n.T("The CA certificate is not a PEM certificate."))
	}
	return []byte(pem), nil
}

func explain(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(app.Explain(err))
}

func normalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New(i18n.T("Enter the server's address."))
	}
	lower := strings.ToLower(raw)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", errors.New(i18n.T("This is not a valid address."))
	}
	return strings.TrimRight(raw, "/"), nil
}
