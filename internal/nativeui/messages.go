package nativeui

import (
	"context"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"gotify-desktop/internal/api"
)

const (
	pageSize = 100
	// markReadDelay is how long an unread card stays on screen, in a focused
	// window, before it is marked read.
	markReadDelay = 1200 * time.Millisecond
	// The next page loads when the rows in view reach this close to the end.
	loadMoreAhead = 10
	searchDelay   = 200 * time.Millisecond
	contentWidth  = 860
)

// msgKey identifies a message among all servers.
type msgKey struct {
	serverID int64
	id       uint
}

func keyOf(m *api.Message) msgKey { return msgKey{m.ServerID, m.ID} }

// loadKey is what a page of messages was loaded for.
type loadKey struct {
	query   string
	limit   int
	msgGen  uint64
	include msgKey
}

// messagesPage shows the messages of all servers, of a server or of an app,
// newest first, as the page's MessagesPage does.
type messagesPage struct {
	u        *UI
	serverID int64
	appID    uint

	search, query string
	editedAt      time.Time
	limit         int

	requested loadKey
	seq       int
	page      *api.MessagePage
	list      ui.ListState
	listBox   ui.Rect

	docs     map[msgKey]docEntry
	deleting map[msgKey]bool
	// target is the message a notification asked to show; highlight shows
	// it until highlightUntil.
	target         *api.Navigation
	highlight      msgKey
	highlightUntil time.Time
	// seen holds when each unread card in view came into view, while the
	// window had the focus; marked those already marked read.
	seen   map[msgKey]time.Time
	marked map[msgKey]bool
}

type docEntry struct {
	body string
	doc  *mdDoc
}

func newMessagesPage(u *UI, serverID int64, appID uint) *messagesPage {
	m := &messagesPage{u: u, serverID: serverID, appID: appID, limit: pageSize, requested: loadKey{limit: -1},
		docs: map[msgKey]docEntry{}, deleting: map[msgKey]bool{}, seen: map[msgKey]time.Time{}, marked: map[msgKey]bool{}}
	m.list.Key = func(i int) any {
		if m.page != nil && i < len(m.page.Messages) {
			return keyOf(&m.page.Messages[i])
		}
		return "more"
	}
	return m
}

// showTarget shows a notification's message, whatever the search was.
func (m *messagesPage) showTarget(n api.Navigation) {
	m.target = &n
	m.search, m.query = "", ""
}

func (m *messagesPage) load() {
	want := loadKey{query: m.query, limit: m.limit, msgGen: m.u.state.MsgGen}
	if m.target != nil {
		want.query = ""
		want.include = msgKey{m.target.ServerID, m.target.MessageID}
	}
	if want == m.requested {
		return
	}
	m.requested = want
	m.seq++
	seq := m.seq
	q := api.Query{ServerID: m.serverID, AppID: m.appID, Search: want.query, Limit: want.limit}
	if want.include.id != 0 {
		q.Include = &api.MessageRef{ServerID: want.include.serverID, ID: want.include.id}
	}
	t := m.u.text()
	async(m.u, func(context.Context) (api.MessagePage, error) { return m.u.svc.Messages(q) }, func(pg api.MessagePage, err error) {
		if seq != m.seq || m.u.msgs != m {
			return
		}
		if err != nil {
			m.u.toast(t.couldNotLoad(err.Error()))
			return
		}
		m.page = &pg
		if m.target == nil {
			return
		}
		key := msgKey{m.target.ServerID, m.target.MessageID}
		for i := range pg.Messages {
			if keyOf(&pg.Messages[i]) == key {
				m.limit = max(m.limit, len(pg.Messages))
				m.requested.limit = m.limit
				m.list.ScrollTo(i, ui.Center)
				m.highlight, m.highlightUntil = key, time.Now().Add(4*time.Second)
				break
			}
		}
		m.target = nil
	})
}

func (m *messagesPage) view(c *ui.Context, wide bool) {
	u, t := m.u, m.u.text()
	// A new search starts again from the first page, once typing paused.
	if m.search != m.query {
		if wait := searchDelay - c.Now().Sub(m.editedAt); wait > 0 {
			c.After(wait)
		} else {
			m.query, m.limit = m.search, pageSize
			m.list.ScrollTo(0, ui.Start)
		}
	}
	m.load()

	sv := u.server(m.serverID)
	a := appOf(sv, m.appID)
	title, subtitle, unread := t.allMessages, t.servers(len(u.state.Servers)), u.state.Unread
	switch {
	case a != nil:
		title, subtitle, unread = a.Name, sv.Name, a.Unread
	case sv != nil:
		title, subtitle, unread = sv.Name, sv.URL, sv.Unread
	}
	if unread > 0 {
		subtitle = t.unreadCount(unread) + " · " + subtitle
	}
	w, _ := c.Size()
	if wide {
		w -= sidebarWidth
	}
	padX := max(20, (w-contentWidth)/2)

	scoped := u.state.Servers
	if sv != nil {
		scoped = []api.Server{*sv}
	}
	u.toolbar(c, wide, padX, title, subtitle, func() {
		if unread > 0 && glassButton(c, t.markAllRead, iconCheckCheck, false).Clicked() {
			go u.svc.MarkAllRead(m.serverID, m.appID)
		}
		width := float32(220)
		if !wide {
			width = 150
		}
		if glassSearch(c, &m.search, t.searchMessages, t.search, width).Changed() {
			m.editedAt = c.Now()
		}
	}, func() {
		m.banners(c, scoped)
	}, func(top float32) {
		m.messages(c, padX, top)
	})
}

// banners tell about the servers in scope that do not receive messages,
// under the toolbar.
func (m *messagesPage) banners(c *ui.Context, servers []api.Server) {
	u, t := m.u, m.u.text()
	var troubled []api.Server
	for _, sv := range servers {
		if sv.State == api.StateAuthFailed || sv.State == api.StateBackoff || (sv.State == api.StateDisconnected && sv.Error != "") {
			troubled = append(troubled, sv)
		}
	}
	if len(troubled) == 0 {
		return
	}
	ui.Column(c).Padding(4, 0, 8).Gap(8).Children(func() {
		for _, sv := range troubled {
			ui.Column(c).Key(sv.ID).Children(func() {
				switch sv.State {
				case api.StateAuthFailed:
					desc := sv.Error
					if desc == "" {
						desc = t.sessionEnded
					}
					banner(c, bannerError, t.signInTo(sv.Name), desc, func() {
						if button(c, t.signIn, buttonOpts{small: true}).Clicked() {
							u.serverDlg = newServerDialog(u, reloginMode, &sv)
						}
					})
				case api.StateBackoff:
					banner(c, bannerWarning, t.cantReach(sv.Name), statusText(t, &sv, now(c, time.Second)), nil)
				default:
					banner(c, bannerError, t.notConnected(sv.Name), sv.Error, nil)
				}
			})
		}
	})
}

func (m *messagesPage) messages(c *ui.Context, padX, top float32) {
	t := m.u.text()
	var msgs []api.Message
	hasMore := false
	if m.page != nil {
		msgs, hasMore = m.page.Messages, m.page.HasMore
	}
	n := len(msgs)
	if hasMore {
		n++
	}
	visible := map[msgKey]bool{}
	youngest := time.Duration(1<<63 - 1)
	nowT := c.Now()
	list := ui.List(c, &m.list, n, func(i int) {
		if i >= len(msgs) {
			ui.Row(c).Justify(ui.Center).Padding(16).Children(func() { ui.Spinner(c) })
			return
		}
		msg := &msgs[i]
		youngest = min(youngest, nowT.Sub(msg.Date))
		card := m.card(c, msg)
		if !msg.Read {
			// On screen once any of it reaches the upper three quarters of
			// the list, below what floats over its top.
			r, box := card.Bounds(), m.listBox
			if r.H > 0 && r.Y < box.Y+box.H*0.75 && r.Y+r.H > box.Y+top {
				visible[keyOf(msg)] = true
			}
		}
	}).Grow(1).Gap(12).Padding(top, padX, 20)
	list.Children(func() {
		switch {
		case m.page == nil:
			ui.Row(c).Justify(ui.Center).Padding(40).Children(func() { ui.Spinner(c).Size(24, 24) })
		case len(msgs) == 0 && m.query != "":
			emptyState(c, iconSearchX, 40, t.noMatchTitle(m.query), t.noMatchText, nil)
		case len(msgs) == 0:
			emptyState(c, iconInbox, 40, t.noMessagesTitle, t.noMessagesText, nil)
		}
	})
	m.listBox = list.Bounds()

	// Relative times tick while they show.
	if youngest < time.Minute {
		c.After(time.Second)
	} else if youngest < 24*time.Hour {
		c.After(30 * time.Second)
	}
	if m.highlight != (msgKey{}) {
		if wait := m.highlightUntil.Sub(nowT); wait > 0 {
			c.After(wait)
		} else {
			m.highlight = msgKey{}
		}
	}

	// Loads more as the end comes near.
	if _, last := m.list.Visible(); hasMore && last >= len(msgs)-loadMoreAhead && m.limit <= len(msgs) {
		m.limit = len(msgs) + pageSize
	}

	m.markVisibleRead(c, visible)
}

// markVisibleRead marks unread messages read once their cards stayed in
// view for a moment, in a focused window: time in the background does not count.
func (m *messagesPage) markVisibleRead(c *ui.Context, visible map[msgKey]bool) {
	nowT := c.Now()
	focused := true
	if w := m.u.win.Load(); w != nil && !w.IsDestroyed() {
		focused = w.IsFocused()
	}
	for k := range m.seen {
		if !visible[k] {
			delete(m.seen, k)
		}
	}
	byServer := map[int64][]uint{}
	for k := range visible {
		since, ok := m.seen[k]
		if !ok || !focused {
			m.seen[k] = nowT
			continue
		}
		if !m.marked[k] && nowT.Sub(since) >= markReadDelay {
			m.marked[k] = true
			byServer[k.serverID] = append(byServer[k.serverID], k.id)
		}
	}
	for server, ids := range byServer {
		go m.u.svc.MarkRead(server, ids)
	}
	if len(m.seen) > 0 {
		c.After(200 * time.Millisecond)
	}
}

func (m *messagesPage) doc(msg *api.Message) *mdDoc {
	k := keyOf(msg)
	if e, ok := m.docs[k]; ok && e.body == msg.Body {
		return e.doc
	}
	d := parseMarkdown(msg.Body)
	m.docs[k] = docEntry{msg.Body, d}
	return d
}

func (m *messagesPage) delete(msg api.Message) {
	k, t := keyOf(&msg), m.u.text()
	m.deleting[k] = true
	async(m.u, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, m.u.svc.DeleteMessage(ctx, msg.ServerID, msg.ID)
	}, func(_ struct{}, err error) {
		delete(m.deleting, k)
		if err != nil {
			m.u.toast(t.couldNotDelete(err.Error()))
		}
	})
}

// card is the page's MessageCard: the app's image, the title, where and when
// the message came from, its body, image and link, and its actions.
func (m *messagesPage) card(c *ui.Context, msg *api.Message) *ui.Element {
	u, p, t := m.u, paletteOf(c), m.u.text()
	sv := u.server(msg.ServerID)
	a := appOf(sv, msg.AppID)
	k := keyOf(msg)
	title := msg.Title
	if title == "" && a != nil {
		title = a.Name
	}
	if title == "" {
		title = t.message
	}
	var meta []string
	if a != nil {
		meta = append(meta, a.Name)
	}
	if sv != nil && m.serverID == 0 && len(u.state.Servers) > 1 {
		meta = append(meta, sv.Name)
	}
	meta = append(meta, m.when(msg.Date, c.Now()))
	deleting := m.deleting[k]
	actions := func(menu *ui.Menu) {
		if menu.Item(t.copyText).Chosen() {
			c.WriteClipboard(msg.Body)
			u.toast(t.copied)
		}
		if msg.ClickURL != "" && menu.Item(t.openLink).Chosen() {
			u.openURL(msg.ClickURL)
		}
		if msg.ImageURL != "" && menu.Item(t.openImageInBrowser).Chosen() {
			u.openURL(msg.ImageURL)
		}
		menu.Separator()
		if menu.Item(t.delete).Disabled(deleting).Chosen() {
			m.delete(*msg)
		}
	}

	card := ui.Row(c).Padding(12).Gap(12).AlignItems(ui.Start).Radius(radiusContainer).
		Background(p.card).Border(1, p.border).ContextMenu(actions)
	if m.highlight == k {
		card.Shadow(0, 0, 0, 2, p.accent)
	}
	if deleting {
		card.Opacity(0.5)
	}
	return card.Children(func() {
		avatar(c, func() string {
			if a != nil {
				return a.Name
			}
			return "?"
		}(), u.images.app(msg.ServerID, a), 36)
		ui.Column(c).Grow(1).MinWidth(0).Gap(8).Children(func() {
			ui.Row(c).Gap(8).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
					ui.Row(c).Gap(8).Children(func() {
						if !msg.Read {
							dot(c, p.blue, 8, false).Label(t.unread)
						}
						ui.Text(c, title).FontWeight(600).SingleLine().MinWidth(0)
					})
					ui.Text(c, strings.Join(meta, " · ")).FontSize(fontSm).TextColor(p.muted).SingleLine().
						Tooltip(t.date(msg.Date.Local()))
				})
				if msg.Priority >= 8 {
					badge(c, t.priority(msg.Priority), p.danger, p.onDanger)
				}
				moreButton(c, t.messageActions, actions)
			})
			if msg.Body != "" {
				if msg.Markdown {
					u.markdown(c, m.doc(msg))
				} else {
					u.plainBody(c, msg.Body)
				}
			}
			if msg.ImageSrc != "" && !(msg.Markdown && strings.Contains(msg.Body, msg.ImageURL)) {
				b, failed := u.images.message(msg)
				u.shownImage(c, b, failed, 360)
			}
			if msg.ClickURL != "" {
				ui.Row(c).Children(func() {
					if button(c, t.openLink, buttonOpts{small: true, icon: iconExternalLink}).Tooltip(msg.ClickURL).Clicked() {
						u.openURL(msg.ClickURL)
					}
				})
			}
		})
	})
}

// when writes the time of a message as the page's Timestamp does: how long
// ago, then the date after a week.
func (m *messagesPage) when(d, now time.Time) string {
	t := m.u.text()
	if ago := now.Sub(d); ago < 7*24*time.Hour {
		return t.ago(max(ago, 0))
	}
	return t.date(d.Local())
}
