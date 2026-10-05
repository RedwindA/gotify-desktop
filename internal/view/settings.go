package view

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"gotify-desktop/internal/app"
)

func (m *Model) settingsPage(c *ui.Context, snap *app.Snapshot) {
	t := c.Theme()
	s := &m.settings
	now := m.plat.Now()
	ui.Scroll(c).Fill().Padding(24, 28).Gap(22).Children(func() {
		ui.Column(c).Gap(22).MaxWidth(620).Children(func() {
			ui.Text(c, "Settings").FontSize(22).Bold()

			m.section(c, "General", func() {
				ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
					ui.Text(c, "Start at login").Grow(1)
					if ui.Switch(c, &m.atLogin).Label("Start at login").Changed() {
						on := m.atLogin
						m.async(func() {
							if m.plat.SetOpenAtLogin == nil {
								return
							}
							if err := m.plat.SetOpenAtLogin(on); err != nil {
								m.update(func() { m.atLogin = !on; m.toast("Couldn't change the login item: " + err.Error()) })
							}
						})
					}
				})
			})

			m.section(c, "Notifications", func() {
				paused := now.Before(s.PausedUntil)
				status := "Notifications are on."
				if paused {
					status = "Paused until " + s.PausedUntil.Format("Mon 15:04") + "."
				}
				ui.Text(c, status).TextColor(t.TextMuted)
				ui.Row(c).Gap(8).Children(func() {
					if ui.Button(c, "Pause for 1 hour").Clicked() {
						s.PausedUntil = now.Add(time.Hour)
						m.saveSettings()
					}
					if ui.Button(c, "Until tomorrow").Clicked() {
						s.PausedUntil = tomorrowMorning(now)
						m.saveSettings()
					}
					if paused && ui.PrimaryButton(c, "Resume").Clicked() {
						s.PausedUntil = time.Time{}
						m.saveSettings()
					}
				})
				ui.Divider(c)
				ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
					ui.Column(c).Grow(1).Children(func() {
						ui.Text(c, "Do not disturb")
						ui.Text(c, "Hide notifications during these hours. Messages still arrive.").TextColor(t.TextMuted).FontSize(t.FontSize * 0.9)
					})
					if ui.Switch(c, &s.DND).Label("Do not disturb").Changed() {
						m.saveSettings()
					}
				})
				ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "From").TextColor(t.TextMuted)
					if ui.TimeInput(c, &m.dndStart).Label("Do not disturb from").Disabled(!s.DND).Changed() {
						s.DNDStart = timeToMinutes(m.dndStart)
						m.saveSettings()
					}
					ui.Text(c, "to").TextColor(t.TextMuted)
					if ui.TimeInput(c, &m.dndEnd).Label("Do not disturb until").Disabled(!s.DND).Changed() {
						s.DNDEnd = timeToMinutes(m.dndEnd)
						m.saveSettings()
					}
				})
				ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
					ui.Text(c, "Priority 8 and up bypasses Do not disturb").Grow(1)
					if ui.Switch(c, &s.HighBypassesDND).Label("High priority bypasses do not disturb").Disabled(!s.DND).Changed() {
						m.saveSettings()
					}
				})
				ui.Divider(c)
				ui.Row(c).Children(func() {
					if ui.Button(c, "Send a test notification").Clicked() {
						m.async(func() {
							if err := m.be.Test(); err != nil {
								m.update(func() { m.toast("Notifications are not available: " + err.Error()) })
							}
						})
					}
				})
			})

			m.section(c, "About", func() {
				ui.Text(c, "Gotify Desktop "+m.plat.Version).Bold()
				ui.Text(c, fmt.Sprintf("Servers: %d", len(snap.Servers))).TextColor(t.TextMuted)
				if m.plat.DataDir != "" {
					ui.Text(c, "Data: "+m.plat.DataDir).TextColor(t.TextMuted).Selectable().FontSize(t.FontSize * 0.9)
				}
			})
		})
	})
}

func (m *Model) section(c *ui.Context, title string, body func()) {
	t := c.Theme()
	ui.Column(c).Gap(10).Children(func() {
		ui.Text(c, title).Bold().TextColor(t.TextMuted)
		ui.Column(c).Gap(12).Padding(16).Radius(12).Background(t.Surface).Border(1, t.Border).Children(body)
	})
}

func tomorrowMorning(now time.Time) time.Time {
	d := now.AddDate(0, 0, 1)
	return time.Date(d.Year(), d.Month(), d.Day(), 8, 0, 0, 0, now.Location())
}
