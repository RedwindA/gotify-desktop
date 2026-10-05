package nativeui

import (
	"context"
	"strings"

	"github.com/egoist/mygo/ui"

	"gotify-desktop/internal/api"
)

type serverMode uint8

const (
	addMode serverMode = iota
	editMode
	reloginMode
)

// serverDialog adds a server, edits one or signs in to one again, as the
// page's ServerDialog.
type serverDialog struct {
	u      *UI
	mode   serverMode
	server api.Server
	open   bool

	name, url, user, pass string
	insecure, advanced    bool
	caName, caPEM, caErr  string
	caChanged             bool
	submitted, busy       bool
	err                   string
}

func newServerDialog(u *UI, mode serverMode, sv *api.Server) *serverDialog {
	d := &serverDialog{u: u, mode: mode, open: true}
	if sv != nil {
		d.server = *sv
		d.name, d.url, d.insecure = sv.Name, sv.URL, sv.Insecure
		if sv.HasCA {
			d.caName = u.text().customCA
		}
	}
	d.advanced = d.insecure || d.caName != ""
	return d
}

func (d *serverDialog) needsLogin() bool { return d.mode != editMode }

func (d *serverDialog) submit() {
	u, t := d.u, d.u.text()
	d.submitted = true
	if (d.mode == addMode && strings.TrimSpace(d.url) == "") || (d.needsLogin() && (d.user == "" || d.pass == "")) || d.busy {
		return
	}
	d.busy, d.err = true, ""
	form := api.ServerForm{Name: d.name, URL: d.url, User: d.user, Pass: d.pass, Insecure: d.insecure, CA: d.caPEM, KeepCA: !d.caChanged}
	mode, id := d.mode, d.server.ID
	async(u, func(ctx context.Context) (int64, error) {
		switch mode {
		case addMode:
			return u.svc.AddServer(ctx, form)
		case editMode:
			return id, u.svc.UpdateServer(ctx, id, form)
		}
		return id, u.svc.Relogin(ctx, id, form.User, form.Pass)
	}, func(id int64, err error) {
		d.busy = false
		if err != nil {
			d.err = err.Error()
			return
		}
		switch mode {
		case addMode:
			// Select the server once the state has it, or the page would fall back to all messages.
			s := u.svc.State()
			u.state = &s
			u.go_(route{serverID: id})
			u.toast(t.serverAdded)
		case reloginMode:
			u.toast(t.signedIn)
		}
		d.open = false
	})
}

func (d *serverDialog) chooseCA() {
	d.caErr = ""
	async(d.u, func(ctx context.Context) (*api.CAFile, error) { return d.u.svc.PickCA(ctx) }, func(f *api.CAFile, err error) {
		switch {
		case err != nil:
			d.caErr = err.Error()
		case f != nil:
			d.caName, d.caPEM, d.caChanged = f.Name, f.PEM, true
		}
	})
}

func (d *serverDialog) view(c *ui.Context) {
	p, t := paletteOf(c), d.u.text()
	title, subtitle, action := t.addServerTitle, t.addServerSubtitle, t.connect
	switch d.mode {
	case editMode:
		title, subtitle, action = t.editServerTitle, "", t.save
	case reloginMode:
		title, subtitle, action = t.signInAgainTitle, t.sessionEndedOn(d.server.Name), t.signIn
	}
	errIf := func(cond bool, msg string) string {
		if cond {
			return msg
		}
		return ""
	}
	urlErr := errIf(d.mode == addMode && d.submitted && strings.TrimSpace(d.url) == "", t.enterAddress)
	userErr := errIf(d.needsLogin() && d.submitted && d.user == "", t.enterUsername)
	passErr := errIf(d.needsLogin() && d.submitted && d.pass == "", t.enterPassword)
	input := func(label, errText string, value *string, opts func(*ui.Element)) {
		field(c, label, errText, func() {
			in := ui.TextInput(c, value).Label(label)
			if opts != nil {
				opts(in)
			}
			styleInput(c, in, errText != "").PaddingX(10)
			if in.Submitted() {
				d.submit()
			}
		})
	}
	dialog(c, &d.open, 480, title, subtitle, func() {
		if d.err != "" {
			banner(c, bannerError, d.err, "", nil)
		}
		if d.mode != reloginMode {
			input(t.serverAddress, urlErr, &d.url, func(e *ui.Element) {
				e.Placeholder("https://gotify.example.com").Disabled(d.mode == editMode)
				if d.mode == addMode {
					e.AutoFocus()
				}
			})
			input(t.name, "", &d.name, func(e *ui.Element) { e.Placeholder(t.namePlaceholder) })
		}
		if d.needsLogin() {
			input(t.username, userErr, &d.user, func(e *ui.Element) {
				if d.mode == reloginMode {
					e.AutoFocus()
				}
			})
			input(t.password, passErr, &d.pass, func(e *ui.Element) { e.Password() })
		}
		if d.mode != reloginMode {
			ui.Collapsible(c, t.advanced, &d.advanced, func() {
				ui.Column(c).Gap(12).Padding(8, 0, 0).Children(func() {
					ui.Column(c).Gap(4).Children(func() {
						ui.Checkbox(c, &d.insecure, t.skipTLS)
						ui.Text(c, t.skipTLSText).FontSize(fontSm).TextColor(p.muted).Margin(0, 0, 0, 24)
						if d.insecure {
							ui.Text(c, t.skipTLSWarning).FontSize(fontSm).TextColor(p.warningText).Margin(0, 0, 0, 24)
						}
					})
					ui.Row(c).Gap(12).Children(func() {
						hint := d.caName
						if hint == "" {
							hint = t.caHint
						}
						labelled(c, t.caCertificate, hint)
						if d.caName != "" && button(c, t.remove, buttonOpts{kind: ghost, small: true}).Clicked() {
							d.caName, d.caPEM, d.caChanged = "", "", true
						}
						if button(c, t.chooseFile, buttonOpts{small: true}).Clicked() {
							d.chooseCA()
						}
					})
					if d.caErr != "" {
						banner(c, bannerError, d.caErr, "", nil)
					}
				})
			})
		}
	}, func() {
		if button(c, t.cancel, buttonOpts{}).Clicked() {
			d.open = false
		}
		if button(c, action, buttonOpts{kind: primary, loading: d.busy}).Clicked() {
			d.submit()
		}
	})
}

// prefsDialog edits the notification preferences of an app; changes apply at once.
type prefsDialog struct {
	serverID int64
	appID    uint
	open     bool
}

func (u *UI) prefsView(c *ui.Context, d *prefsDialog) {
	t := u.text()
	sv := u.server(d.serverID)
	a := appOf(sv, d.appID)
	if a == nil {
		d.open = false
		return
	}
	save := func(muted bool, minPriority *int) {
		go func() {
			if err := u.svc.SetAppPrefs(sv.ID, a.ID, api.AppPrefs{Muted: muted, MinPriority: minPriority}); err != nil {
				u.post(func() { u.toast(t.couldNotSave(err.Error())) })
			}
		}()
	}
	dialog(c, &d.open, 420, a.Name, t.appPrefsSubtitle(sv.Name), func() {
		muted := a.Muted
		if switchRow(c, t.mute, t.muteText, &muted, false) {
			save(muted, a.MinPriority)
		}
		levels := []int{0, 1, 4, 8}
		labels := []string{t.everyMessage, t.priorityAndUp(1), t.priorityAndUp(4), t.priorityAndUp(8)}
		cur := 0
		if a.MinPriority != nil {
			cur = *a.MinPriority
		}
		choice := labels[indexOf(levels, cur)]
		field(c, t.notifyAbout, "", func() {
			if styleInput(c, ui.Select(c, &choice, labels).Label(t.notifyAbout).Disabled(a.Muted), false).Changed() {
				var mp *int
				if v := levels[indexOf(labels, choice)]; v != 0 {
					mp = &v
				}
				save(a.Muted, mp)
			}
		})
	}, nil)
}

// dialogs shows the dialog open, if any.
func (u *UI) dialogs(c *ui.Context) {
	t := u.text()
	if d := u.serverDlg; d != nil {
		d.view(c)
		if !d.open {
			u.serverDlg = nil
		}
	}
	if d := u.prefs; d != nil {
		u.prefsView(c, d)
		if !d.open {
			u.prefs = nil
		}
	}
	if sv := u.removing; sv != nil {
		open := u.removeOpen || u.removeBusy
		dialog(c, &open, 440, t.removeTitle(sv.Name), "", func() {
			ui.Text(c, t.removeText).TextColor(paletteOf(c).muted)
		}, func() {
			if button(c, t.cancel, buttonOpts{disabled: u.removeBusy}).Clicked() {
				open = false
			}
			if button(c, t.removeAction, buttonOpts{kind: primary, loading: u.removeBusy}).Clicked() {
				u.removeBusy = true
				id := sv.ID
				async(u, func(ctx context.Context) (struct{}, error) { return struct{}{}, u.svc.RemoveServer(ctx, id) }, func(_ struct{}, err error) {
					if err != nil {
						u.toast(t.couldNotRemove(err.Error()))
					}
					u.removeBusy, u.removeOpen, u.removing = false, false, nil
				})
			}
		})
		if !open && !u.removeBusy {
			u.removeOpen, u.removing = false, nil
		}
	}
	if u.viewing != nil {
		ui.DialogBase(c, &u.viewOpen, func(backdrop, panel *ui.Element) {
			backdrop.Background(ui.RGBA(0, 0, 0, 0.85)).Padding(c.TitleBar().Height+24, 24, 24)
			panel.Grow(1).FillWidth().FillHeight()
			img := ui.Image(c, u.viewing).Fill().Fit(ui.Contain)
			if img.Clicked() {
				u.viewOpen = false
			}
			if button(c, "Close", buttonOpts{kind: ghost, icon: iconX, iconOnly: true}).TextColor(ui.RGB(255, 255, 255)).
				Attach(ui.AnchorTopRight, ui.AnchorTopRight).Clicked() {
				u.viewOpen = false
			}
		})
		if !u.viewOpen {
			u.viewing = nil
		}
	}
}
