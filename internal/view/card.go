package view

import (
	"fmt"

	"github.com/egoist/mygo/ui"

	"gotify-desktop/internal/app"
	"gotify-desktop/internal/notify"
	"gotify-desktop/internal/store"
)

func (m *Model) card(c *ui.Context, snap *app.Snapshot, i int) {
	t := c.Theme()
	msg := m.msgs[i]
	sv, _ := snap.Server(msg.ServerID)
	a, _ := sv.App(msg.AppID)
	key := msgKey{msg.ServerID, msg.ID}
	now := m.plat.Now()
	title := msg.Title
	if title == "" {
		title = a.Name
	}
	if title == "" {
		title = "Message"
	}
	clickURL := notify.ClickURL(msg.Extras)

	card := ui.Column(c).Gap(8).Padding(14, 16).Radius(12).Background(t.Surface).Border(1, t.Border)
	if key == m.highlight && now.Before(m.hlUntil) {
		card.Border(2, t.Accent)
	}
	card.Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Avatar(c, a.Name, m.bitmap(msg.ServerID, a)).Size(28, 28)
			ui.Column(c).Grow(1).Shrink(1).Children(func() {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					if !msg.Read {
						ui.Box(c).Size(8, 8).Radius(4).Background(t.Accent).Label("Unread")
					}
					ui.Text(c, title).Bold().SingleLine()
				})
				meta := relTime(now, msg.Date)
				if a.Name != "" {
					meta = a.Name + " · " + meta
				}
				if len(snap.Servers) > 1 && sv.Name != "" {
					meta += " · " + sv.Name
				}
				ui.Text(c, meta).FontSize(t.FontSize * 0.88).TextColor(t.TextMuted).SingleLine().
					Tooltip(msg.Date.Format("Mon 2 Jan 2006, 15:04:05"))
			})
			if msg.Priority >= 8 {
				ui.Badge(c, fmt.Sprintf("Priority %d", msg.Priority)).Background(t.Danger).TextColor(t.AccentText)
			}
			ui.Button(c, "").Label("More").Padding(4, 6).Children(func() { ui.Icon(c, iconMore) }).
				Menu(func(mn *ui.Menu) { m.messageMenu(c, mn, msg, clickURL) })
		})
		if body := msg.Message.Message; body != "" {
			m.md.Render(c, fmt.Sprintf("%d/%d", msg.ServerID, msg.ID), body, isMarkdown(msg.Extras))
		}
		if clickURL != "" {
			ui.Row(c).Children(func() {
				if ui.Button(c, "").Children(func() {
					ui.Icon(c, iconLink)
					ui.Text(c, "Open link").SingleLine()
				}).Tooltip(clickURL).Clicked() {
					c.OpenURL(clickURL)
				}
			})
		}
	}).ContextMenu(func(mn *ui.Menu) { m.messageMenu(c, mn, msg, clickURL) })
}

func (m *Model) messageMenu(c *ui.Context, mn *ui.Menu, msg store.StoredMessage, clickURL string) {
	if mn.Item("Copy text").Chosen() {
		c.WriteClipboard(msg.Message.Message)
	}
	if clickURL != "" && mn.Item("Open link").Chosen() {
		c.OpenURL(clickURL)
	}
	mn.Separator()
	if mn.Item("Delete").Disabled(m.deleting[msgKey{msg.ServerID, msg.ID}]).Chosen() {
		m.deleteMessage(msg)
	}
}
