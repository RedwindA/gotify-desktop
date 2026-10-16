// Package app is the controller: servers, their connections, the store and
// notifications, with no UI of its own.
package app

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gotify-desktop/internal/conn"
	"gotify-desktop/internal/fracdex"
	"gotify-desktop/internal/gotify"
	"gotify-desktop/internal/i18n"
	"gotify-desktop/internal/notify"
	"gotify-desktop/internal/secret"
	"gotify-desktop/internal/store"
)

const settingsKey = "notify.settings"

type Options struct {
	DataDir    string
	CacheDir   string
	Tokens     secret.Tokens
	Notifier   notify.Notifier
	ConnConfig conn.Config
	// OnChange runs after every change of what Snapshot or Messages return, on
	// any goroutine; it must not block.
	OnChange func()
}

type AppInfo struct {
	ID              uint
	Name            string
	Description     string
	DefaultPriority int
	SortKey         string
	Image           []byte
	ImageKey        string
	Unread          int
	Muted           bool
	MinPriority     *int
}

type ServerInfo struct {
	ID        int64
	Name      string
	URL       string
	Insecure  bool
	HasCA     bool
	State     conn.State
	Err       string
	RetryAt   time.Time
	NeedLogin bool
	Apps      []AppInfo
	// AppsSortable is whether the server keeps an order of the apps (gotify 2.9 and later).
	AppsSortable bool
	Unread       int
}

// Snapshot is immutable: a new one replaces it after every change.
type Snapshot struct {
	Servers  []ServerInfo
	Unread   int
	Settings notify.Settings
	// Gen changes with every snapshot, MsgGen only when messages did.
	Gen    uint64
	MsgGen uint64
}

func (s *Snapshot) Server(id int64) (ServerInfo, bool) {
	for _, sv := range s.Servers {
		if sv.ID == id {
			return sv, true
		}
	}
	return ServerInfo{}, false
}

func (s ServerInfo) App(id uint) (AppInfo, bool) {
	for _, a := range s.Apps {
		if a.ID == id {
			return a, true
		}
	}
	return AppInfo{}, false
}

type ServerInput struct {
	Name, URL, User, Pass string
	Insecure              bool
	CACertPEM             []byte
}

type Activation struct {
	Kind notify.Kind
	// Action is the button chosen (notify.ActionRead, notify.ActionOpen), or ""
	// for a click on the notification itself.
	Action    string
	ServerID  int64
	AppID     uint
	MessageID uint
	ClickURL  string
}

type runtime struct {
	// op serializes the operations that change the server: UpdateServer, RemoveServer and the end of Relogin.
	op      sync.Mutex
	sv      store.Server
	sup     *conn.Supervisor
	state   conn.State
	err     error
	retryAt time.Time
	noToken bool
}

type App struct {
	opts Options
	st   *store.Store
	disp *notify.Dispatcher

	rebuildMu     sync.Mutex
	mu            sync.Mutex
	servers       map[int64]*runtime
	closed        bool
	snap          atomic.Pointer[Snapshot]
	gen           uint64
	msgGen        uint64
	settings      atomic.Pointer[notify.Settings]
	refresh       chan struct{}
	refreshStop   chan struct{}
	refreshDone   chan struct{}
	refreshFlags  atomic.Uint32
	pruneWake     chan struct{}
	pruneDone     chan struct{}
	metadataDirty atomic.Bool
	appCache      map[int64][]AppInfo
}

func New(opts Options) (*App, error) {
	if err := os.MkdirAll(opts.DataDir, 0o755); err != nil {
		return nil, err
	}
	st, err := store.Open(filepath.Join(opts.DataDir, "gotify.db"))
	if err != nil {
		return nil, err
	}
	a := &App{opts: opts, st: st, servers: map[int64]*runtime{}, refresh: make(chan struct{}, 1), refreshStop: make(chan struct{}), refreshDone: make(chan struct{}),
		pruneWake: make(chan struct{}, 1), pruneDone: make(chan struct{})}
	a.metadataDirty.Store(true)
	s := a.loadSettings()
	a.settings.Store(&s)
	a.disp = notify.NewDispatcher(opts.Notifier, st, a.Settings, opts.CacheDir, nil)
	servers, err := st.Servers()
	if err != nil {
		a.disp.Close()
		st.Close()
		return nil, err
	}
	go a.refreshLoop()
	go a.pruneLoop()
	for _, sv := range servers {
		a.startServer(sv)
	}
	a.rebuild(true)
	return a, nil
}

func (a *App) Close() {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	a.closed = true
	var sups []*conn.Supervisor
	for _, rt := range a.servers {
		if rt.sup != nil {
			sups = append(sups, rt.sup)
		}
	}
	a.mu.Unlock()
	for _, s := range sups {
		s.Stop()
	}
	close(a.refreshStop)
	<-a.refreshDone
	<-a.pruneDone
	a.disp.Close()
	a.st.Close()
}

func (a *App) clientFor(sv store.Server, token string) (*gotify.Client, error) {
	return gotify.New(sv.URL, token, gotify.Options{InsecureSkipVerify: sv.InsecureSkipVerify, CACertPEM: []byte(sv.CACertPEM)})
}

// startServer adds a server's runtime and connects it in the background, after
// reading its token.
func (a *App) startServer(sv store.Server) {
	rt := &runtime{sv: sv, state: conn.Connecting}
	a.mu.Lock()
	a.servers[sv.ID] = rt
	a.mu.Unlock()
	go a.connect(sv.ID, rt)
}

// connect reads a server's token and starts its supervisor, unless the server
// was removed or Relogin started one meanwhile.
func (a *App) connect(id int64, rt *runtime) {
	token, err := a.opts.Tokens.Get(id)
	rt.op.Lock()
	defer rt.op.Unlock()
	a.mu.Lock()
	if a.closed || a.servers[id] != rt || rt.sup != nil {
		a.mu.Unlock()
		return
	}
	var sup *conn.Supervisor
	switch {
	case errors.Is(err, secret.ErrNotFound), err == nil && token == "":
		rt.noToken, rt.state, rt.err = true, conn.AuthFailed, gotify.ErrUnauthorized
	case err != nil:
		rt.state, rt.err = conn.Disconnected, errors.New(i18n.T("Can't read the token: %v", err))
	default:
		if client, cerr := a.clientFor(rt.sv, token); cerr != nil {
			rt.state, rt.err = conn.Disconnected, cerr
		} else {
			sup = conn.New(rt.sv.ID, client, a.st, a.sink, a.opts.ConnConfig)
			rt.sup = sup
		}
	}
	a.mu.Unlock()
	if sup != nil {
		sup.Start()
	}
	a.rebuild(false)
	a.changed()
}

// Connection callbacks only update in-memory state and request a coalesced
// rebuild. Database reads and image decoding never delay a supervisor.
func (a *App) sink(ev conn.Event) {
	if ev.Kind == conn.EventState {
		a.mu.Lock()
		if rt := a.servers[ev.ServerID]; rt != nil {
			rt.state, rt.err, rt.retryAt = ev.State, ev.Err, ev.RetryAt
		}
		a.mu.Unlock()
	}
	if ev.Kind == conn.EventApps {
		a.metadataDirty.Store(true)
	}
	if ev.Kind == conn.EventMessages {
		a.disp.Handle(ev)
	}
	if ev.Kind == conn.EventRemoved {
		go a.removeNotifications(ev.ServerID, ev.Removed)
	}
	flag := uint32(1)
	if ev.Kind == conn.EventMessages || ev.Kind == conn.EventRemoved {
		flag = 2
	}
	a.refreshFlags.Or(flag)
	select {
	case a.refresh <- struct{}{}:
	default:
	}
}

func (a *App) refreshLoop() {
	defer close(a.refreshDone)
	for {
		select {
		case <-a.refreshStop:
			return
		case <-a.refresh:
		}
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-a.refreshStop:
			timer.Stop()
			return
		case <-timer.C:
		}
		flags := a.refreshFlags.Swap(0)
		if flags != 0 {
			a.rebuild(flags&2 != 0)
			a.changed()
		}
	}
}

// pruneEvery is how often messages past Settings.KeepDays are deleted.
const pruneEvery = 6 * time.Hour

func (a *App) pruneLoop() {
	defer close(a.pruneDone)
	t := time.NewTicker(pruneEvery)
	defer t.Stop()
	for {
		a.prune()
		select {
		case <-a.refreshStop:
			return
		case <-t.C:
		case <-a.pruneWake:
		}
	}
}

// prune deletes the messages older than the settings keep them.
func (a *App) prune() {
	days := a.Settings().KeepDays
	if days <= 0 {
		return
	}
	n, err := a.st.Prune(time.Now().AddDate(0, 0, -days))
	if err != nil {
		log.Printf("prune messages: %v", err)
		return
	}
	if n > 0 {
		a.rebuild(true)
		a.changed()
	}
}

// removeNotifications takes the notifications of messages off the screen and out of the notification center.
func (a *App) removeNotifications(serverID int64, ids []uint) {
	for _, id := range ids {
		a.opts.Notifier.Remove(fmt.Sprintf("s%d-m%d", serverID, id))
	}
}

func (a *App) changed() {
	if a.opts.OnChange != nil {
		a.opts.OnChange()
	}
}

func (a *App) Snapshot() *Snapshot { return a.snap.Load() }

// ImageKey identifies an app image, to cache its decoded form.
func ImageKey(img []byte) string {
	if len(img) == 0 {
		return ""
	}
	h := fnv.New64a()
	h.Write(img)
	return fmt.Sprintf("%d-%x", len(img), h.Sum64())
}

func (a *App) rebuild(messages bool) {
	a.rebuildMu.Lock()
	defer a.rebuildMu.Unlock()
	svs, err := a.st.Servers()
	if err != nil {
		return
	}
	unread, err := a.st.UnreadCounts()
	if err != nil {
		log.Printf("snapshot unread counts: %v", err)
		return
	}
	if a.metadataDirty.Swap(false) || a.appCache == nil {
		a.appCache = map[int64][]AppInfo{}
	}
	snap := &Snapshot{Settings: a.Settings()}
	for _, sv := range svs {
		info := ServerInfo{ID: sv.ID, Name: sv.Name, URL: sv.URL, Insecure: sv.InsecureSkipVerify, HasCA: sv.CACertPEM != ""}
		a.mu.Lock()
		if rt := a.servers[sv.ID]; rt != nil {
			info.State, info.RetryAt, info.NeedLogin = rt.state, rt.retryAt, rt.noToken
			if rt.err != nil {
				info.Err = Explain(rt.err)
			}
		}
		a.mu.Unlock()
		cached, ok := a.appCache[sv.ID]
		if !ok {
			apps, err := a.st.Apps(sv.ID)
			if err != nil {
				log.Printf("snapshot apps: %v", err)
				return
			}
			prefs, err := a.st.AppPrefs(sv.ID)
			if err != nil {
				log.Printf("snapshot preferences: %v", err)
				return
			}
			for _, ap := range apps {
				pref := prefs[ap.ID]
				cached = append(cached, AppInfo{ID: ap.ID, Name: ap.Name, Description: ap.Description, DefaultPriority: ap.DefaultPriority, SortKey: ap.SortKey, Image: ap.Image, ImageKey: ImageKey(ap.Image), Muted: pref.Muted, MinPriority: pref.MinPriority})
			}
			a.appCache[sv.ID] = cached
		}
		info.AppsSortable = len(cached) > 0
		for _, ai := range cached {
			ai.Unread = unread[sv.ID][ai.ID]
			info.Apps = append(info.Apps, ai)
			info.AppsSortable = info.AppsSortable && ai.SortKey != ""
		}
		for _, n := range unread[sv.ID] {
			info.Unread += n
		}
		snap.Unread += info.Unread
		snap.Servers = append(snap.Servers, info)
	}
	a.mu.Lock()
	a.gen++
	if messages {
		a.msgGen++
	}
	snap.Gen, snap.MsgGen = a.gen, a.msgGen
	if a.closed {
		a.mu.Unlock()
		return
	}
	a.snap.Store(snap)
	a.mu.Unlock()
}

// revokeLogin undoes a login, best effort. It has a context of its own: the caller's may be
// the very reason the login is being undone.
func revokeLogin(baseURL, user, pass string, clientID uint, opts gotify.Options) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	gotify.DeleteClientBasic(ctx, baseURL, user, pass, clientID, opts)
}

func hostName(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Host
	}
	return raw
}

func clientName() string {
	h, _ := os.Hostname()
	if h == "" {
		h = "unknown"
	}
	return "Gotify Desktop (" + h + ")"
}

func (a *App) AddServer(ctx context.Context, in ServerInput) (int64, error) {
	if _, err := url.Parse(in.URL); err != nil {
		return 0, err
	}
	sv := store.Server{Name: in.Name, URL: in.URL, InsecureSkipVerify: in.Insecure, CACertPEM: string(in.CACertPEM)}
	if sv.Name == "" {
		sv.Name = hostName(in.URL)
	}
	probe, err := a.clientFor(sv, "")
	if err != nil {
		return 0, err
	}
	ver, err := probe.Version(ctx)
	if err != nil {
		return 0, err
	}
	if ver.Version == "" {
		return 0, errors.New(i18n.T("This does not look like a Gotify server."))
	}
	token, clientID, err := gotify.Login(ctx, in.URL, in.User, in.Pass, clientName(),
		gotify.Options{InsecureSkipVerify: in.Insecure, CACertPEM: in.CACertPEM})
	if err != nil {
		return 0, err
	}
	committed := false
	defer func() {
		if !committed {
			revokeLogin(in.URL, in.User, in.Pass, clientID, gotify.Options{InsecureSkipVerify: in.Insecure, CACertPEM: in.CACertPEM})
		}
	}()
	sv.ClientID = clientID
	authed, err := a.clientFor(sv, token)
	if err != nil {
		return 0, err
	}
	me, err := authed.CurrentUser(ctx)
	if err != nil {
		return 0, err
	}
	sv.UserID, sv.UserName = me.ID, me.Name
	id, err := a.st.AddServer(sv)
	if err != nil {
		return 0, err
	}
	sv.ID = id
	if err := a.opts.Tokens.Set(id, token); err != nil {
		return 0, errors.Join(errors.New(i18n.T("Couldn't store the token: %v", err)), a.st.DeleteServer(id))
	}
	committed = true
	a.startServer(sv)
	a.rebuild(false)
	a.changed()
	return id, nil
}

// rtView is a consistent copy of what a runtime holds, taken under the lock.
type rtView struct {
	rt  *runtime
	sv  store.Server
	sup *conn.Supervisor
}

func (a *App) runtimeOf(id int64) (rtView, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	rt := a.servers[id]
	if rt == nil {
		return rtView{}, fmt.Errorf("unknown server %d", id)
	}
	return rtView{rt, rt.sv, rt.sup}, nil
}

func (a *App) setServer(rt *runtime, sv store.Server) {
	a.mu.Lock()
	rt.sv = sv
	a.mu.Unlock()
}

// swapClient points the server's supervisor at a client for its settings and token, starting it if needed.
func (a *App) swapClient(v rtView, token string) error {
	client, err := a.clientFor(v.sv, token)
	if err != nil {
		return err
	}
	a.mu.Lock()
	v.rt.noToken = false
	sup := v.rt.sup
	started := false
	if sup == nil {
		sup = conn.New(v.sv.ID, client, a.st, a.sink, a.opts.ConnConfig)
		v.rt.sup, started = sup, true
	}
	a.mu.Unlock()
	if started {
		sup.Start()
		return nil
	}
	sup.SetClient(client)
	sup.Kick()
	return nil
}

func (a *App) Relogin(ctx context.Context, serverID int64, user, pass string) error {
	v, err := a.runtimeOf(serverID)
	if err != nil {
		return err
	}
	baseURL := v.sv.URL
	opts := gotify.Options{InsecureSkipVerify: v.sv.InsecureSkipVerify, CACertPEM: []byte(v.sv.CACertPEM)}
	token, clientID, err := gotify.Login(ctx, baseURL, user, pass, clientName(), opts)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			revokeLogin(baseURL, user, pass, clientID, opts)
		}
	}()
	client, err := a.clientFor(v.sv, token)
	if err != nil {
		return err
	}
	me, err := client.CurrentUser(ctx)
	if err != nil {
		return err
	}
	if v.sv.UserID != 0 && me.ID != v.sv.UserID {
		return errors.New(i18n.T("This server was added as %s. Remove it and add it again to use another account.", v.sv.UserName))
	}
	v.rt.op.Lock()
	defer v.rt.op.Unlock()
	// The login was slow: apply it to the server as it is now, not as it was.
	cur, err := a.runtimeOf(serverID)
	if err != nil {
		return err
	}
	v = cur
	// Build against the current TLS settings before changing either local store.
	if _, err := a.clientFor(v.sv, token); err != nil {
		return err
	}
	oldToken, tokenErr := a.opts.Tokens.Get(serverID)
	if tokenErr != nil && !errors.Is(tokenErr, secret.ErrNotFound) {
		return tokenErr
	}
	if err := a.opts.Tokens.Set(serverID, token); err != nil {
		return errors.New(i18n.T("Couldn't store the token: %v", err))
	}
	sv := v.sv
	sv.ClientID, sv.UserID, sv.UserName = clientID, me.ID, me.Name
	if err := a.st.UpdateServer(sv); err != nil {
		var rollback error
		if errors.Is(tokenErr, secret.ErrNotFound) {
			rollback = a.opts.Tokens.Delete(serverID)
		} else {
			rollback = a.opts.Tokens.Set(serverID, oldToken)
		}
		return errors.Join(err, rollback)
	}
	committed = true
	a.setServer(v.rt, sv)
	v.sv = sv
	if err := a.swapClient(v, token); err != nil {
		return err
	}
	a.rebuild(false)
	a.changed()
	return nil
}

// UpdateServer changes a server's settings; a nil ca keeps its CA certificate, an empty one removes it.
func (a *App) UpdateServer(ctx context.Context, serverID int64, name string, insecure bool, ca []byte) error {
	v, err := a.runtimeOf(serverID)
	if err != nil {
		return err
	}
	v.rt.op.Lock()
	defer v.rt.op.Unlock()
	if v, err = a.runtimeOf(serverID); err != nil {
		return err
	}
	sv := v.sv
	sv.Name, sv.InsecureSkipVerify = name, insecure
	if ca != nil {
		sv.CACertPEM = string(ca)
	}
	if sv.Name == "" {
		sv.Name = hostName(sv.URL)
	}
	if _, err := a.clientFor(sv, ""); err != nil {
		return err
	}
	if err := a.st.UpdateServer(sv); err != nil {
		return err
	}
	a.setServer(v.rt, sv)
	v.sv = sv
	if token, err := a.opts.Tokens.Get(serverID); err == nil && token != "" {
		if err := a.swapClient(v, token); err != nil {
			return err
		}
	}
	a.rebuild(false)
	a.changed()
	return nil
}

func (a *App) RemoveServer(ctx context.Context, serverID int64) error {
	v, err := a.runtimeOf(serverID)
	if err != nil {
		return err
	}
	v.rt.op.Lock()
	defer v.rt.op.Unlock()
	if v, err = a.runtimeOf(serverID); err != nil {
		return err
	}
	token, tokenErr := a.opts.Tokens.Get(serverID)
	if tokenErr != nil && !errors.Is(tokenErr, secret.ErrNotFound) {
		return tokenErr
	}
	// Keep the runtime and remote client intact until both local stores agree.
	if err := a.opts.Tokens.Delete(serverID); err != nil {
		return err
	}
	if err := a.st.DeleteServer(serverID); err != nil {
		if tokenErr == nil {
			err = errors.Join(err, a.opts.Tokens.Set(serverID, token))
		}
		return err
	}
	if v.sup != nil {
		v.sup.Stop()
	}
	a.mu.Lock()
	delete(a.servers, serverID)
	a.mu.Unlock()
	a.metadataDirty.Store(true)
	a.rebuild(true)
	a.changed()
	if token != "" && v.sv.ClientID != 0 {
		if client, err := a.clientFor(v.sv, token); err == nil {
			cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			if err := client.DeleteClient(cctx, v.sv.ClientID); err != nil {
				log.Printf("remove server: revoke remote client: %v", err)
			}
			cancel()
		}
	}
	return nil
}

// clientOf returns a client for a server with its token.
func (a *App) clientOf(serverID int64) (*gotify.Client, error) {
	v, err := a.runtimeOf(serverID)
	if err != nil {
		return nil, err
	}
	token, err := a.opts.Tokens.Get(serverID)
	if err != nil {
		return nil, err
	}
	return a.clientFor(v.sv, token)
}

// DeleteMessage deletes on the server first and keeps the local copy when that fails.
func (a *App) DeleteMessage(ctx context.Context, serverID int64, id uint) error {
	return a.DeleteMessages(ctx, serverID, []uint{id})
}

// deleteParallel is how many deletions DeleteMessages sends to a server at once.
const deleteParallel = 4

// DeleteMessages deletes messages on the server and then here, a few at a
// time. Those the server could not delete stay; the first error is returned.
func (a *App) DeleteMessages(ctx context.Context, serverID int64, ids []uint) error {
	client, err := a.clientOf(serverID)
	if err != nil {
		return err
	}
	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		deleted []uint
		first   error
	)
	slots := make(chan struct{}, deleteParallel)
	for _, id := range ids {
		wg.Add(1)
		slots <- struct{}{}
		go func() {
			defer wg.Done()
			err := client.DeleteMessage(ctx, id)
			<-slots
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				first = cmp.Or(first, err)
				return
			}
			deleted = append(deleted, id)
		}()
	}
	wg.Wait()
	if len(deleted) > 0 {
		if err := a.st.DeleteMessage(serverID, deleted...); err != nil {
			return err
		}
		go a.removeNotifications(serverID, deleted)
		a.rebuild(true)
		a.changed()
	}
	return first
}

// ClearMessages deletes every message of an app, or of the server when appID
// is 0, on the server and then here.
func (a *App) ClearMessages(ctx context.Context, serverID int64, appID uint) error {
	client, err := a.clientOf(serverID)
	if err != nil {
		return err
	}
	// Only the messages stored now are removed here, as ids need not arrive in
	// order. One stored meanwhile stays; if the server deleted it too, the next
	// check for deleted messages removes it.
	ids, err := a.st.AppMessageIDs(serverID, appID)
	if err != nil {
		return err
	}
	if appID == 0 {
		err = client.DeleteAllMessages(ctx)
	} else {
		err = client.DeleteAppMessages(ctx, appID)
	}
	if err != nil {
		return err
	}
	if err := a.st.DeleteMessage(serverID, ids...); err != nil {
		return err
	}
	go a.removeNotifications(serverID, ids)
	a.rebuild(true)
	a.changed()
	return nil
}

// ErrAppsNotSortable is returned by MoveApp for a server that keeps no order
// of the apps: gotify before 2.9.
var ErrAppsNotSortable = errors.New("the server keeps no order of the apps")

// MoveApp moves an app of a server before another, or last when beforeID is
// 0, on the server and then here. Like gotify's web UI it gives the app a new
// sort key between its new neighbors. It fetches the apps first: one added on
// the server since they were last fetched could have the very key, and the
// update sends the name, description and default priority along, as the
// server sets them all.
func (a *App) MoveApp(ctx context.Context, serverID int64, appID, beforeID uint) error {
	client, err := a.clientOf(serverID)
	if err != nil {
		return err
	}
	v, err := a.runtimeOf(serverID)
	if err != nil {
		return err
	}
	move := func() error {
		apps, err := client.Applications(ctx)
		if err != nil {
			return err
		}
		if err := a.st.ReplaceApps(serverID, apps); err != nil {
			return err
		}
		// The order of Store.Apps, which the server's matches.
		slices.SortStableFunc(apps, func(x, y gotify.Application) int {
			return cmp.Or(strings.Compare(x.SortKey, y.SortKey), cmp.Compare(x.ID, y.ID))
		})
		var moved *gotify.Application
		var rest []gotify.Application
		for i, ap := range apps {
			if ap.SortKey == "" {
				return ErrAppsNotSortable
			}
			if ap.ID == appID {
				moved = &apps[i]
			} else {
				rest = append(rest, ap)
			}
		}
		if moved == nil {
			return fmt.Errorf("no app %d", appID)
		}
		at := len(rest)
		if beforeID != 0 {
			if at = slices.IndexFunc(rest, func(ap gotify.Application) bool { return ap.ID == beforeID }); at < 0 {
				return fmt.Errorf("no app %d", beforeID)
			}
		}
		var prev, next string
		if at > 0 {
			prev = rest[at-1].SortKey
		}
		if at < len(rest) {
			next = rest[at].SortKey
		}
		if prev < moved.SortKey && (next == "" || moved.SortKey < next) {
			return nil // already there
		}
		key, err := fracdex.KeyBetween(prev, next)
		if err != nil {
			return err
		}
		updated, err := client.UpdateApplication(ctx, appID, gotify.ApplicationParams{
			Name: moved.Name, Description: moved.Description, DefaultPriority: moved.DefaultPriority, SortKey: key})
		if err != nil {
			return err
		}
		return a.st.SetAppSortKey(serverID, appID, cmp.Or(updated.SortKey, key))
	}
	// The supervisor's refresh of the apps must not store what it fetched
	// before the move after it. One that starts while the server connects
	// could, so a move waits for the connection.
	if v.sup == nil {
		return errors.New(i18n.T("Not connected to the server yet."))
	}
	err = v.sup.WithApps(ctx, move)
	// After the last write: a rebuild meanwhile may have cached the apps before it.
	a.metadataDirty.Store(true)
	a.rebuild(false)
	a.changed()
	return err
}

func (a *App) MarkRead(serverID int64, ids ...uint) error {
	if err := a.st.MarkRead(serverID, ids...); err != nil {
		return err
	}
	for _, id := range ids {
		a.opts.Notifier.Remove(fmt.Sprintf("s%d-m%d", serverID, id))
	}
	a.rebuild(true)
	a.changed()
	return nil
}

func (a *App) MarkAllRead(serverID int64, appID uint) error {
	ids, err := a.st.MarkAllRead(serverID, appID)
	if err != nil {
		return err
	}
	go a.removeNotifications(serverID, ids)
	a.rebuild(true)
	a.changed()
	return nil
}

func (a *App) Messages(q store.MessageQuery) ([]store.StoredMessage, error) { return a.st.Messages(q) }

func (a *App) KickAll() {
	a.mu.Lock()
	var sups []*conn.Supervisor
	for _, rt := range a.servers {
		if rt.sup != nil {
			sups = append(sups, rt.sup)
		}
	}
	a.mu.Unlock()
	for _, s := range sups {
		s.Kick()
	}
}

func (a *App) loadSettings() notify.Settings {
	s := notify.DefaultSettings()
	if raw, _ := a.st.GetSetting(settingsKey); raw != "" {
		json.Unmarshal([]byte(raw), &s)
	}
	i18n.SetPreference(s.Language)
	return s
}

func (a *App) Settings() notify.Settings { return *a.settings.Load() }

func (a *App) SetSettings(s notify.Settings) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err := a.st.SetSetting(settingsKey, string(b)); err != nil {
		return err
	}
	old := a.settings.Swap(&s)
	if s.KeepDays != old.KeepDays {
		select {
		case a.pruneWake <- struct{}{}:
		default:
		}
	}
	i18n.SetPreference(s.Language)
	a.rebuild(false)
	a.changed()
	return nil
}

func (a *App) GetSetting(key string) string {
	v, _ := a.st.GetSetting(key)
	return v
}

func (a *App) SetSetting(key, value string) error { return a.st.SetSetting(key, value) }

func (a *App) AppPref(serverID int64, appID uint) store.AppPref {
	p, _ := a.st.GetAppPref(serverID, appID)
	return p
}

func (a *App) SetAppPref(serverID int64, appID uint, p store.AppPref) error {
	if err := a.st.SetAppPref(serverID, appID, p); err != nil {
		return err
	}
	a.metadataDirty.Store(true)
	a.rebuild(false)
	a.changed()
	return nil
}

// HandleActivation resolves a clicked notification, or a button of one, and
// marks its messages read: a message's on a click, and on its buttons;
// those of a summary on Mark all as read.
func (a *App) HandleActivation(id string) Activation {
	id, action := notify.SplitActivation(id)
	serverID, appID, msgID, kind := notify.ParseID(id)
	act := Activation{Kind: kind, Action: action, ServerID: serverID, AppID: appID, MessageID: msgID}
	switch {
	case kind == notify.KindMessage:
		if msgs, err := a.st.Messages(store.MessageQuery{ServerID: serverID, ID: msgID, Limit: 1}); err == nil && len(msgs) == 1 {
			act.AppID = msgs[0].AppID
			act.ClickURL = notify.ClickURL(msgs[0].Extras)
		}
		if err := a.MarkRead(serverID, msgID); err != nil {
			log.Printf("mark activated message read: %v", err)
		}
	case action == notify.ActionRead && (kind == notify.KindBurst || kind == notify.KindMissed):
		if err := a.MarkAllRead(serverID, appID); err != nil {
			log.Printf("mark summarized messages read: %v", err)
		}
	}
	return act
}

// Test sends a sample notification through the notifier.
// FetchImage downloads the image a message shows (notify.BigImageURL).
func (a *App) FetchImage(ctx context.Context, url string) ([]byte, error) {
	return notify.FetchImage(ctx, http.DefaultClient, url)
}

func (a *App) Test() error {
	return a.opts.Notifier.Show(notify.Notification{
		ID: "test", Title: "Gotify Desktop", Body: i18n.T("Notifications work."), AppName: "Gotify Desktop", Level: notify.LevelNormal,
	})
}
