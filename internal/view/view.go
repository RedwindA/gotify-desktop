package view

import (
	"fmt"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"

	"gotify-desktop/internal/app"
	"gotify-desktop/internal/conn"
)

// View builds the window.
func (m *Model) View(c *ui.Context) {
	snap := m.be.Snapshot()
	m.syncSettings(snap)
	m.validateScope(snap)
	for _, s := range m.toasts {
		c.Toast(s)
	}
	m.toasts = nil
	if now := m.plat.Now(); now.After(m.nextTick) {
		m.nextTick = now.Add(30 * time.Second)
		c.After(30 * time.Second)
	}
	for _, sv := range snap.Servers {
		if sv.State == conn.Backoff {
			c.After(time.Second)
			break
		}
	}
	ui.Split(c, &m.sidebarW, func() { m.sidebar(c, snap) }, func() { m.mainPane(c, snap) }).Fill()
	m.dialogs(c, snap)
}

func (m *Model) mainPane(c *ui.Context, snap *app.Snapshot) {
	switch {
	case m.page == pageSettings:
		m.settingsPage(c, snap)
	case len(snap.Servers) == 0:
		m.welcome(c)
	default:
		m.messagesPage(c, snap)
	}
}

func (m *Model) welcome(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Center().Gap(10).Padding(32).Children(func() {
		ui.Icon(c, iconBell).Size(48, 48).TextColor(t.Accent)
		ui.Text(c, "Welcome to Gotify Desktop").FontSize(22).Bold()
		ui.Text(c, "Get your Gotify notifications on this computer. Add a server to start receiving messages.").
			TextColor(t.TextMuted).TextAlign(ui.Center).MaxWidth(420)
		ui.Box(c).Height(8)
		if ui.PrimaryButton(c, "Add server").Padding(10, 22).Clicked() {
			m.openAdd()
		}
	})
}

func (m *Model) messagesPage(c *ui.Context, snap *app.Snapshot) {
	t := c.Theme()
	m.refreshMessages(snap)
	unread := m.scopeUnread(snap)
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Gap(12).Padding(14, 20).AlignItems(ui.Center).Children(func() {
			ui.Text(c, m.scopeTitle(snap)).FontSize(20).Bold().SingleLine().Grow(1)
			if unread > 0 && ui.Button(c, "Mark all read").Clicked() {
				m.markScopeRead(snap)
			}
			ui.SearchField(c, &m.query).Width(220).Label("Search messages")
		})
		ui.Divider(c)
		m.list.Key = func(i int) any { return msgKey{m.msgs[i].ServerID, m.msgs[i].ID} }
		m.list.Label = func(i int) string { return m.msgs[i].Title }
		ui.List(c, &m.list, len(m.msgs), func(i int) { m.card(c, snap, i) }).Grow(1).Padding(16, 20).Gap(10).Children(func() {
			if len(m.msgs) == 0 {
				text := "No messages yet"
				if m.query != "" {
					text = fmt.Sprintf("No messages match “%s”", m.query)
				}
				ui.Text(c, text).TextColor(t.TextMuted).TextAlign(ui.Center).Padding(40, 0)
			}
		})
	})
	m.markVisibleRead()
	if k := m.scrollTo; k != (msgKey{}) {
		if i := m.indexOf(k); i >= 0 {
			m.list.ScrollTo(i, ui.Center)
			m.scrollTo = msgKey{}
			c.After(m.hlUntil.Sub(m.plat.Now()))
		}
	}
	m.loadMore()
}

func (m *Model) sidebarRow(c *ui.Context, selected bool, build func()) *ui.Element {
	t := c.Theme()
	r := ui.Row(c).Gap(8).Padding(6, 10).Radius(8).AlignItems(ui.Center).Cursor(ui.CursorPointer)
	switch {
	case selected:
		r.Background(t.Accent.Alpha(0.16))
	case r.Hovered():
		r.Background(t.SurfaceHover)
	}
	return r.Children(build)
}

func statusColor(t *ui.Theme, s conn.State) ui.Color {
	switch s {
	case conn.Connected:
		return t.Success
	case conn.AuthFailed:
		return t.Danger
	}
	return t.Warning
}

func (m *Model) statusText(sv app.ServerInfo) string {
	switch sv.State {
	case conn.Connected:
		return "Connected"
	case conn.Connecting:
		return "Connecting…"
	case conn.Backoff:
		wait := max(0, int(time.Until(sv.RetryAt).Round(time.Second).Seconds()))
		if m.plat.Now != nil {
			wait = max(0, int(sv.RetryAt.Sub(m.plat.Now()).Round(time.Second).Seconds()))
		}
		text := fmt.Sprintf("Retrying in %ds", wait)
		if sv.Err != "" {
			text += " — " + sv.Err
		}
		return text
	case conn.AuthFailed:
		return "Sign in again — " + sv.Err
	}
	if sv.Err != "" {
		return sv.Err
	}
	return "Disconnected"
}

func badge(c *ui.Context, n int) {
	if n > 0 {
		ui.Badge(c, strconv.Itoa(n))
	}
}

func (m *Model) sidebar(c *ui.Context, snap *app.Snapshot) {
	t := c.Theme()
	ui.Column(c).Fill().Background(t.Surface).Children(func() {
		ui.Scroll(c).Grow(1).Padding(10, 8).Gap(2).Children(func() {
			all := m.page == pageMessages && m.scope == (scope{})
			if m.sidebarRow(c, all, func() {
				ui.Icon(c, iconInbox)
				ui.Text(c, "All messages").Grow(1).SingleLine()
				badge(c, snap.Unread)
			}).Clicked() {
				m.page, m.scope = pageMessages, scope{}
			}
			for _, sv := range snap.Servers {
				m.serverSection(c, snap, sv)
			}
		})
		ui.Divider(c)
		ui.Row(c).Gap(8).Padding(10).Children(func() {
			if ui.Button(c, "").Label("Add server").Grow(1).Children(func() {
				ui.Icon(c, iconPlus)
				ui.Text(c, "Add server").SingleLine()
			}).Clicked() {
				m.openAdd()
			}
			if ui.Button(c, "").Label("Settings").Tooltip("Settings").Children(func() { ui.Icon(c, iconSettings) }).Clicked() {
				m.page = pageSettings
			}
		})
	})
}

func (m *Model) serverSection(c *ui.Context, snap *app.Snapshot, sv app.ServerInfo) {
	t := c.Theme()
	selectedServer := m.page == pageMessages && m.scope == (scope{sv.ID, 0})
	ui.Box(c).Height(6)
	header := m.sidebarRow(c, selectedServer, func() {
		ui.Box(c).Size(9, 9).Radius(5).Background(statusColor(t, sv.State)).Tooltip(m.statusText(sv)).Label(m.statusText(sv))
		ui.Text(c, sv.Name).Bold().SingleLine().Grow(1)
		badge(c, sv.Unread)
	})
	if header.Clicked() {
		m.page, m.scope = pageMessages, scope{sv.ID, 0}
	}
	header.ContextMenu(func(mn *ui.Menu) {
		if mn.Item("Edit…").Chosen() {
			m.openEdit(sv)
		}
		if mn.Item("Sign in again…").Chosen() {
			m.openRelogin(sv)
		}
		if mn.Item("Mark all read").Chosen() {
			id := sv.ID
			m.async(func() { m.be.MarkAllRead(id, 0) })
		}
		mn.Separator()
		if mn.Item("Remove…").Chosen() {
			m.removeAsk, m.removeID = true, sv.ID
		}
	})
	if sv.State == conn.AuthFailed {
		ui.Row(c).Padding(0, 10, 4, 27).Children(func() {
			if ui.Link(c, "Sign in again", "").TextColor(t.Danger).Clicked() {
				m.openRelogin(sv)
			}
		})
	}
	for _, a := range sv.Apps {
		sel := m.page == pageMessages && m.scope == (scope{sv.ID, a.ID})
		row := m.sidebarRow(c, sel, func() {
			ui.Avatar(c, a.Name, m.bitmap(sv.ID, a)).Size(22, 22).Margin(0, 0, 0, 8)
			name := ui.Text(c, a.Name).SingleLine().Grow(1)
			if a.Muted {
				name.TextColor(t.TextMuted).Italic()
			}
			badge(c, a.Unread)
		})
		if row.Clicked() {
			m.page, m.scope = pageMessages, scope{sv.ID, a.ID}
		}
		row.ContextMenu(func(mn *ui.Menu) { m.appMenu(mn, sv, a) })
	}
}

func (m *Model) appMenu(mn *ui.Menu, sv app.ServerInfo, a app.AppInfo) {
	if mn.Item("Muted").Checked(a.Muted).Chosen() {
		m.setAppPref(sv.ID, a, !a.Muted, a.MinPriority)
	}
	mn.Submenu("Minimum priority", func(sub *ui.Menu) {
		for _, opt := range []struct {
			label string
			min   int
		}{{"Off", 0}, {"1 and up", 1}, {"4 and up", 4}, {"8 and up", 8}} {
			on := (a.MinPriority == nil && opt.min == 0) || (a.MinPriority != nil && *a.MinPriority == opt.min)
			if sub.Item(opt.label).Checked(on).Chosen() {
				var min *int
				if opt.min > 0 {
					v := opt.min
					min = &v
				}
				m.setAppPref(sv.ID, a, a.Muted, min)
			}
		}
	})
	mn.Separator()
	if mn.Item("Mark all read").Chosen() {
		sid, aid := sv.ID, a.ID
		m.async(func() { m.be.MarkAllRead(sid, aid) })
	}
}
