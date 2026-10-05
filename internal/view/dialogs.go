package view

import (
	"context"
	"time"

	"github.com/egoist/mygo/ui"

	"gotify-desktop/internal/app"
)

type dialogKind int

const (
	dialogAdd dialogKind = iota
	dialogEdit
	dialogRelogin
)

type dialog struct {
	open      bool
	kind      dialogKind
	serverID  int64
	name      string
	url       string
	user      string
	pass      string
	insecure  bool
	advanced  bool
	caName    string
	ca        []byte
	caErr     string
	busy      bool
	err       string
	submitted bool
	serverURL string
}

func (m *Model) openAdd() {
	m.dlg = dialog{open: true, kind: dialogAdd}
}

func (m *Model) openEdit(sv app.ServerInfo) {
	m.dlg = dialog{open: true, kind: dialogEdit, serverID: sv.ID, name: sv.Name, url: sv.URL, serverURL: sv.URL, insecure: sv.Insecure, advanced: sv.Insecure || sv.HasCA}
	if sv.HasCA {
		m.dlg.caName = "Custom CA certificate"
	}
}

func (m *Model) openRelogin(sv app.ServerInfo) {
	m.dlg = dialog{open: true, kind: dialogRelogin, serverID: sv.ID, name: sv.Name, url: sv.URL, serverURL: sv.URL}
}

func (m *Model) dialogs(c *ui.Context, snap *app.Snapshot) {
	if m.dlg.open {
		ui.Modal(c, &m.dlg.open, func() { m.serverDialog(c, snap) })
	}
	if m.removeAsk {
		sv, _ := snap.Server(m.removeID)
		id := m.removeID
		if ui.AlertDialog(c, &m.removeAsk, "Remove “"+sv.Name+"”?",
			"Its messages are deleted from this computer and this device signs out of the server.", "Cancel", "Remove") == 1 {
			m.async(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				if err := m.be.RemoveServer(ctx, id); err != nil {
					m.update(func() { m.toast("Couldn't remove the server: " + err.Error()) })
				}
			})
		}
	}
}

func (m *Model) serverDialog(c *ui.Context, snap *app.Snapshot) {
	t := c.Theme()
	d := &m.dlg
	title, action := "Add server", "Connect"
	switch d.kind {
	case dialogEdit:
		title, action = "Edit server", "Save"
	case dialogRelogin:
		title, action = "Sign in again", "Sign in"
	}
	var urlErr, userErr, passErr string
	if d.submitted {
		if d.kind == dialogAdd {
			if _, err := normalizeURL(d.url); err != nil {
				urlErr = err.Error()
			}
		}
		if d.kind != dialogEdit {
			if d.user == "" {
				userErr = "Enter your username."
			}
			if d.pass == "" {
				passErr = "Enter your password."
			}
		}
	}
	ui.Column(c).Width(440).Gap(14).Padding(6).Children(func() {
		ui.Text(c, title).FontSize(18).Bold()
		if d.kind == dialogRelogin {
			ui.Text(c, "Your session on “"+d.name+"” ended. Sign in to keep receiving messages.").TextColor(t.TextMuted)
		}
		submit := false
		ui.Column(c).Gap(12).Children(func() {
			if d.kind != dialogRelogin {
				ui.Field(c, "Name", func() {
					ui.TextInput(c, &d.name).Placeholder("Defaults to the server's address")
				})
				ui.Field(c, "Server address", func() {
					in := ui.TextInput(c, &d.url).Placeholder("https://gotify.example.com").Disabled(d.kind == dialogEdit)
					if d.kind == dialogAdd {
						in.AutoFocus()
					}
					if in.Submitted() {
						submit = true
					}
				}).Error(urlErr)
			}
			if d.kind != dialogEdit {
				ui.Field(c, "Username", func() {
					in := ui.TextInput(c, &d.user)
					if d.kind == dialogRelogin {
						in.AutoFocus()
					}
					if in.Submitted() {
						submit = true
					}
				}).Error(userErr)
				ui.Field(c, "Password", func() {
					if ui.TextInput(c, &d.pass).Password().Submitted() {
						submit = true
					}
				}).Error(passErr)
			}
		})
		if d.kind != dialogRelogin {
			ui.Collapsible(c, "Advanced", &d.advanced, func() {
				ui.Column(c).Gap(10).Padding(6, 0, 0, 0).Children(func() {
					ui.Checkbox(c, &d.insecure, "Skip TLS certificate verification")
					if d.insecure {
						ui.Text(c, "Anyone on the network could read your messages and token. Only use this for servers you control.").
							TextColor(t.Danger).FontSize(t.FontSize * 0.9)
					}
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Text(c, "CA certificate").SingleLine().Shrink(0)
						name := d.caName
						if name == "" {
							name = "None"
						}
						ui.Text(c, name).TextColor(t.TextMuted).SingleLine().Grow(1)
						if d.ca != nil || d.caName != "" {
							if ui.Button(c, "Remove").Clicked() {
								d.ca, d.caName, d.caErr = nil, "", ""
							}
						}
						if ui.Button(c, "Choose file…").Disabled(m.plat.PickCA == nil).Clicked() {
							m.pickCA()
						}
					})
					if d.caErr != "" {
						ui.Text(c, d.caErr).TextColor(t.Danger)
					}
				})
			})
		}
		if d.err != "" {
			ui.Text(c, d.err).TextColor(t.Danger)
		}
		ui.Row(c).Gap(8).Justify(ui.End).AlignItems(ui.Center).Children(func() {
			if d.busy {
				ui.Spinner(c).Label("Connecting")
			}
			if ui.Button(c, "Cancel").Clicked() {
				d.open = false
			}
			if ui.PrimaryButton(c, action).Disabled(d.busy).Clicked() {
				submit = true
			}
		})
		if submit && !d.busy {
			m.submitDialog()
		}
	})
}

func (m *Model) pickCA() {
	m.async(func() {
		name, pem, err := m.plat.PickCA()
		m.update(func() {
			switch {
			case err != nil:
				m.dlg.caErr = err.Error()
			case name == "" && pem == nil:
			case !validPEM(pem):
				m.dlg.caErr = "This file is not a PEM certificate."
			default:
				m.dlg.ca, m.dlg.caName, m.dlg.caErr = pem, name, ""
			}
		})
	})
}

func (m *Model) submitDialog() {
	d := m.dlg
	m.dlg.submitted = true
	var raw string
	if d.kind == dialogAdd {
		var err error
		if raw, err = normalizeURL(d.url); err != nil {
			return
		}
	}
	if d.kind != dialogEdit && (d.user == "" || d.pass == "") {
		return
	}
	m.dlg.busy, m.dlg.err = true, ""
	m.async(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var (
			id  = d.serverID
			err error
		)
		switch d.kind {
		case dialogAdd:
			id, err = m.be.AddServer(ctx, app.ServerInput{Name: d.name, URL: raw, User: d.user, Pass: d.pass, Insecure: d.insecure, CACertPEM: d.ca})
		case dialogEdit:
			ca := d.ca
			if ca == nil && d.caName == "" {
				ca = []byte{}
			}
			err = m.be.UpdateServer(ctx, id, d.name, d.insecure, ca)
		case dialogRelogin:
			err = m.be.Relogin(ctx, id, d.user, d.pass)
		}
		m.update(func() {
			m.dlg.busy = false
			if err != nil {
				m.dlg.err = app.Explain(err)
				return
			}
			m.dlg = dialog{}
			if d.kind == dialogAdd {
				m.page, m.scope = pageMessages, scope{id, 0}
				m.toast("Server added")
			}
		})
	})
}
