package nativeui

import (
	"context"
	"time"

	"github.com/egoist/mygo/ui"

	"gotify-desktop/internal/api"
)

// settingsPage is the page's SettingsPage. Edits show at once and save right
// away, numbered from an epoch the service gives, so that it keeps the newest
// whatever order the saves arrive in; the draft shows until the state holds
// the last edit.
type settingsPage struct {
	u       *UI
	draft   *api.Settings
	epoch   uint64
	edits   uint64
	lastSeq uint64
	pending int
}

func (s *settingsPage) current() api.Settings {
	if s.draft != nil {
		return *s.draft
	}
	return s.u.state.Settings
}

func (s *settingsPage) update(change func(*api.Settings)) {
	u, t := s.u, s.u.text()
	next := s.current()
	change(&next)
	s.draft = &next
	if s.epoch == 0 {
		s.epoch = u.svc.SettingsEpoch()
	}
	s.edits++
	seq := s.epoch<<20 + s.edits
	s.lastSeq = seq
	s.pending++
	async(u, func(context.Context) (api.State, error) {
		err := u.svc.SetSettings(seq, next)
		return u.svc.State(), err
	}, func(st api.State, err error) {
		s.pending--
		if err != nil {
			u.toast(t.couldNotSaveSettings(err.Error()))
			// A save that failed must not keep the draft forever.
			if s.lastSeq == seq {
				s.lastSeq = 0
			}
		}
		if u.state == nil || st.Gen >= u.state.Gen {
			u.state = &st
		}
		s.settle()
	})
}

// settle drops the draft once the state holds the last edit.
func (s *settingsPage) settle() {
	if s.draft != nil && s.pending == 0 && s.u.state != nil && s.u.state.SettingsSeq >= s.lastSeq {
		s.draft = nil
	}
}

func (s *settingsPage) view(c *ui.Context, wide bool) {
	u, t := s.u, s.u.text()
	st := s.current()
	w, _ := c.Size()
	if wide {
		w -= sidebarWidth
	}
	padX := max(20, (w-680)/2)
	u.toolbar(c, wide, padX, t.settings, "", nil, nil, func(top float32) { s.content(c, padX, top, st) })
}

func (s *settingsPage) content(c *ui.Context, padX, top float32, st api.Settings) {
	u, p, t := s.u, paletteOf(c), s.u.text()
	ui.Scroll(c).Grow(1).Padding(top, padX, 32).Gap(24).Children(func() {
		group(c, t.general, func() {
			langs := []string{t.languageSystem, "简体中文", "English"}
			langVals := []api.Language{api.LanguageSystem, api.LanguageChinese, api.LanguageEnglish}
			lang := langs[indexOf(langVals, st.Language)]
			field(c, t.language, "", func() {
				if styleInput(c, ui.Select(c, &lang, langs).Label(t.language).Width(240), false).Changed() {
					v := langVals[indexOf(langs, lang)]
					s.update(func(x *api.Settings) { x.Language = v })
				}
			})
			themes := []string{t.appearanceSystem, t.appearanceLight, t.appearanceDark}
			themeVals := []api.Theme{api.ThemeSystem, api.ThemeLight, api.ThemeDark}
			theme := themes[indexOf(themeVals, st.Theme)]
			field(c, t.appearance, "", func() {
				if styleInput(c, ui.Select(c, &theme, themes).Label(t.appearance).Width(240), false).Changed() {
					v := themeVals[indexOf(themes, theme)]
					s.update(func(x *api.Settings) { x.Theme = v })
				}
			})
			on := u.state.OpenAtLogin
			if switchRow(c, t.startAtLogin, t.startAtLoginText, &on, false) {
				async(u, func(context.Context) (api.State, error) {
					err := u.svc.SetOpenAtLogin(on)
					return u.svc.State(), err
				}, func(st api.State, err error) {
					if err != nil {
						u.toast(t.couldNotLoginItem(err.Error()))
					}
					u.state = &st
				})
			}
		})

		group(c, t.notifications, func() {
			nowT := now(c, 30*time.Second)
			paused := st.PausedUntil != nil && st.PausedUntil.After(nowT)
			ui.Row(c).Gap(12).Wrap().Children(func() {
				if paused {
					labelled(c, t.notificationsPaused, t.resumesAt(t.pausedFormat(st.PausedUntil.Local()))).MinWidth(200)
					if button(c, t.resume, buttonOpts{kind: primary}).Clicked() {
						s.update(func(x *api.Settings) { x.PausedUntil = nil })
					}
				} else {
					labelled(c, t.notificationsOn, t.notificationsOnText).MinWidth(200)
					if button(c, t.pauseHour, buttonOpts{}).Clicked() {
						until := time.Now().Add(time.Hour)
						s.update(func(x *api.Settings) { x.PausedUntil = &until })
					}
					if button(c, t.pauseTomorrow, buttonOpts{}).Clicked() {
						until := tomorrowMorning()
						s.update(func(x *api.Settings) { x.PausedUntil = &until })
					}
				}
			})
			ui.Divider(c).Background(p.border)
			dnd := st.DND
			if switchRow(c, t.dnd, t.dndText, &dnd, false) {
				s.update(func(x *api.Settings) { x.DND = dnd })
			}
			ui.Row(c).Gap(12).AlignItems(ui.End).Children(func() {
				for i, label := range []string{t.from, t.to} {
					mins := []int{st.DNDStart, st.DNDEnd}[i]
					tm := time.Date(2000, 1, 1, mins/60, mins%60, 0, 0, time.Local)
					ui.Column(c).Key(i).Children(func() {
						field(c, label, "", func() {
							in := styleInput(c, ui.TimeInput(c, &tm).Label(label).Width(140).Disabled(!st.DND), false)
							if in.Changed() {
								v := tm.Hour()*60 + tm.Minute()
								s.update(func(x *api.Settings) {
									if i == 0 {
										x.DNDStart = v
									} else {
										x.DNDEnd = v
									}
								})
							}
						})
					})
				}
			})
			bypass := st.HighBypassesDND
			if switchRow(c, t.highBypass, t.highBypassText, &bypass, !st.DND) {
				s.update(func(x *api.Settings) { x.HighBypassesDND = bypass })
			}
			ui.Divider(c).Background(p.border)
			ui.Row(c).Gap(12).Children(func() {
				labelled(c, t.testTitle, t.testText)
				if button(c, t.sendTest, buttonOpts{}).Clicked() {
					async(u, func(context.Context) (struct{}, error) { return struct{}{}, u.svc.TestNotification() }, func(_ struct{}, err error) {
						if err != nil {
							u.toast(t.notificationsUnavailable(err.Error()))
						} else {
							u.toast(t.testSent)
						}
					})
				}
			})
		})

		group(c, t.about, func() {
			ui.Column(c).Gap(4).Children(func() {
				ui.Text(c, "Gotify Desktop "+u.state.Version).FontWeight(500)
				ui.RichText(c).FontSize(fontSm).TextColor(p.muted).Children(func() {
					ui.Text(c, t.servers(len(u.state.Servers))+" · "+t.dataIn+" ")
					ui.Text(c, u.state.DataDir).Font("monospace").Background(p.code)
				}).Selectable()
			})
		})
	})
}

// group is a heading over a section of settings.
func group(c *ui.Context, title string, fn func()) {
	p := paletteOf(c)
	ui.Column(c).Gap(8).Children(func() {
		sectionHeading(c, title)
		ui.Column(c).Padding(16).Gap(16).Radius(radiusContainer).Background(p.card).Border(1, p.border).Children(fn)
	})
}

// field is a label over a control, with a supporting or an error text below.
func field(c *ui.Context, label, errText string, control func()) {
	p := paletteOf(c)
	ui.Column(c).Gap(6).Children(func() {
		ui.Text(c, label).FontWeight(500)
		control()
		if errText != "" {
			ui.Text(c, errText).FontSize(fontSm).TextColor(p.danger)
		}
	})
}

// switchRow is a label and its description beside a switch at the end, as
// the page's Switch with its label at the start; it reports a change.
func switchRow(c *ui.Context, label, description string, on *bool, disabled bool) bool {
	changed := false
	r := ui.Row(c).Gap(12)
	if disabled {
		r.Opacity(0.5)
	}
	r.Children(func() {
		labelled(c, label, description)
		changed = ui.Switch(c, on).Label(label).Disabled(disabled).Changed()
	})
	return changed
}

func indexOf[T comparable](s []T, v T) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return 0
}

func tomorrowMorning() time.Time {
	d := time.Now().AddDate(0, 0, 1)
	return time.Date(d.Year(), d.Month(), d.Day(), 8, 0, 0, 0, time.Local)
}
