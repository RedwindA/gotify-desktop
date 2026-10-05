package nativeui

import (
	"time"

	"github.com/egoist/mygo/ui"

	"gotify-desktop/internal/api"
)

// sidebar is the sidebar of macOS 26's Notes: a toolbar beside the window
// controls, with the buttons to add a server and open the settings on glass,
// then all messages and a section of each server with its apps. left is how
// far from the window's left edge it starts, to keep clear of the controls.
func (u *UI) sidebar(c *ui.Context, left float32) {
	t := u.text()
	bar := c.TitleBar()
	ui.Column(c).Grow(1).MinHeight(0).Children(func() {
		ui.Row(c).Height(toolbarHeight(c)).Padding(0, 10, 0, max(10, bar.Left+8-left)).Gap(8).DragWindow().Shrink(0).Children(func() {
			ui.Box(c).Grow(1)
			glassGroup(c, func() {
				if groupButton(c, t.addServer, iconPlus, false).Clicked() {
					u.drawer = false
					u.serverDlg = newServerDialog(u, addMode, nil)
				}
				if groupButton(c, t.settings, iconSettings, u.route.settings).Clicked() {
					u.go_(route{settings: true})
				}
			})
		})
		ui.Scroll(c).Grow(1).Padding(4, 10, 16).Gap(2).Children(func() {
			u.navItem(c, route{}, t.allMessages, func() { ui.Icon(c, iconInbox).Size(16, 16) }, func(sel bool) {
				countBadge(c, u.state.Unread, sel)
			}, nil)
			for i := range u.state.Servers {
				sv := &u.state.Servers[i]
				ui.Column(c).Key(sv.ID).Gap(2).Children(func() { u.serverSection(c, sv) })
			}
		})
	})
}

func (u *UI) serverSection(c *ui.Context, sv *api.Server) {
	p := paletteOf(c)
	t := u.text()
	ui.Row(c).Padding(14, 8, 6).Gap(8).Children(func() {
		ui.Text(c, sv.Name).FontSize(fontSm).FontWeight(600).TextColor(p.muted).SingleLine().Grow(1)
		if sv.State == api.StateBackoff {
			c.After(time.Second) // the countdown
		}
		status := statusText(t, sv, c.Now())
		dot(c, u.dotColor(p, sv.State), 8, sv.State == api.StateConnecting).Label(status).Tooltip(status).Margin(0, 4, 0, 0)
	})
	u.navItem(c, route{serverID: sv.ID}, t.allFromServer, func() { ui.Icon(c, iconInbox).Size(16, 16) }, func(sel bool) {
		countBadge(c, sv.Unread, sel)
	}, func() {
		moreButton(c, t.serverOptions(sv.Name), func(m *ui.Menu) {
			if m.Item(t.editServer).Chosen() {
				u.serverDlg = newServerDialog(u, editMode, sv)
			}
			if m.Item(t.signInAgainMenu).Chosen() {
				u.serverDlg = newServerDialog(u, reloginMode, sv)
			}
			if m.Item(t.markAllRead).Disabled(sv.Unread == 0).Chosen() {
				go u.svc.MarkAllRead(sv.ID, 0)
			}
			m.Separator()
			if m.Item(t.removeServer).Chosen() {
				cp := *sv
				u.removing, u.removeOpen = &cp, true
			}
		})
	})
	for j := range sv.Apps {
		a := &sv.Apps[j]
		ui.Column(c).Key(a.ID).Children(func() {
			u.navItem(c, route{serverID: sv.ID, appID: a.ID}, a.Name, func() {
				avatar(c, a.Name, u.images.app(sv.ID, a), 20)
			}, func(sel bool) {
				if a.Muted {
					ui.Icon(c, iconBellOff).Size(16, 16).TextColor(p.muted).Label(t.muted)
				} else {
					countBadge(c, a.Unread, sel)
				}
			}, func() {
				moreButton(c, t.appOptions(a.Name), func(m *ui.Menu) {
					if m.Item(t.notificationSettings).Chosen() {
						u.prefs = &prefsDialog{serverID: sv.ID, appID: a.ID, open: true}
					}
					if m.Item(t.markAllRead).Disabled(a.Unread == 0).Chosen() {
						go u.svc.MarkAllRead(sv.ID, a.ID)
					}
				})
			})
		})
	}
}

// navItem is a SideNavItem: an icon, a label, and what ends it, chosen when
// the route is to; a route of serverID -1 is no page, for an action.
func (u *UI) navItem(c *ui.Context, to route, label string, icon func(), end func(selected bool), actions func()) *ui.Element {
	p := paletteOf(c)
	sel := to.serverID >= 0 && u.route == to
	item := ui.ButtonBase(c).Height(32).PaddingX(8).Gap(8).Radius(radiusElement).Label(label)
	switch {
	case sel:
		item.Background(p.gray)
	case item.Pressed():
		item.Background(p.pressed)
	case item.Hovered():
		item.Background(p.hover)
	}
	item.Children(func() {
		ui.Row(c).Width(20).Justify(ui.Center).Children(icon)
		ui.Text(c, label).SingleLine().Grow(1).MinWidth(0).FontWeight(map[bool]int{true: 500, false: 400}[sel])
		// The actions take the place of the end on the item pointed at or
		// chosen; hidden, they stay, so that their open menu does.
		shown := float32(0)
		if actions != nil && (sel || item.Hovered()) {
			shown = 1
		}
		ui.Row(c).Shrink(0).Justify(ui.End).MinWidth(24).Children(func() {
			if end != nil {
				ui.Row(c).Opacity(1 - shown).Children(func() { end(sel) })
			}
			if actions != nil {
				ui.Row(c).Absolute().Right(0).Top(0).Bottom(0).Opacity(shown).Children(actions)
			}
		})
	})
	if item.Clicked() && to.serverID >= 0 {
		u.go_(to)
	}
	return item
}

func (u *UI) dotColor(p *palette, s api.ConnState) ui.Color {
	switch s {
	case api.StateConnected:
		return p.success
	case api.StateConnecting, api.StateBackoff:
		return p.warning
	case api.StateAuthFailed:
		return p.danger
	}
	return p.disabled
}

// statusText says how a server's connection is, as the page's status dots do.
func statusText(t *messages, sv *api.Server, now time.Time) string {
	switch sv.State {
	case api.StateConnected:
		return t.connected
	case api.StateConnecting:
		return t.connecting
	case api.StateBackoff:
		wait := 0
		if sv.RetryAt != nil {
			wait = max(0, int(sv.RetryAt.Sub(now).Round(time.Second)/time.Second))
		}
		s := t.retryingIn(wait)
		if sv.Error != "" {
			s += " — " + sv.Error
		}
		return s
	case api.StateAuthFailed:
		s := t.signInAgainStatus
		if sv.Error != "" {
			s += " — " + sv.Error
		}
		return s
	}
	if sv.Error != "" {
		return sv.Error
	}
	return t.disconnected
}
