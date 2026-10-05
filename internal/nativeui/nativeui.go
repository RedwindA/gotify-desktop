// Package nativeui is the window of the app drawn by MyGo itself, in Go with
// package ui, instead of the page in a webview: the same sidebar, messages,
// settings and dialogs, in the look of the page's theme. It calls the
// service the page calls (api.Desktop) directly, and takes its state and
// navigations from the controller's events.
package nativeui

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"

	"gotify-desktop/internal/api"
)

// UI is the view of the app's windows, and what it keeps between them: the
// route, the state, the images. Its fields belong to the main thread, where
// the view runs; other goroutines hand it work through post.
type UI struct {
	ctrl *api.Controller
	svc  *api.Desktop

	win atomic.Pointer[mygo.Window]
	// queue holds the functions goroutines posted, which the next frame runs.
	mu    sync.Mutex
	queue []func()

	state  *api.State
	route  route
	msgs   *messagesPage
	set    settingsPage
	images images
	toasts []string

	// The dialogs: a server's, an app's preferences, the server to remove,
	// the image viewer and the drawer of a narrow window.
	serverDlg  *serverDialog
	prefs      *prefsDialog
	removing   *api.Server
	removeOpen bool
	removeBusy bool
	viewing    *ui.Bitmap
	viewOpen   bool
	drawer     bool
	// reach is how far down the bar floating over a page, and what floats
	// under it, reached in the last frame.
	reach float32
}

// route is the page the window shows: the messages of all servers, of a
// server or of an app, or the settings.
type route struct {
	settings bool
	serverID int64
	appID    uint
}

// New returns the view of the controller's service. Give it the
// controller's events with Redirect before the controller starts.
func New(ctrl *api.Controller) *UI {
	u := &UI{ctrl: ctrl, svc: ctrl.Service()}
	u.images.u = u
	u.set.u = u
	return u
}

// Content is the content of a window showing the view.
func (u *UI) Content() *ui.Content { return ui.View(u.view) }

// Attach makes w the window that posted work wakes.
func (u *UI) Attach(w *mygo.Window) { u.win.Store(w) }

// Detach forgets w once it closed, unless another window took its place.
func (u *UI) Detach(w *mygo.Window) { u.win.CompareAndSwap(w, nil) }

// SetState takes a new state from the controller, from any goroutine.
func (u *UI) SetState(s api.State) {
	u.post(func() {
		if u.state == nil || s.Gen >= u.state.Gen {
			u.state = &s
			u.set.settle()
		}
	})
}

// Navigate shows what a clicked notification asked for, from any goroutine.
func (u *UI) Navigate(api.Navigation) {
	u.post(func() {
		if n := u.svc.TakeNavigation(); n != nil {
			u.navigate(*n)
		}
	})
}

// post runs fn on the main thread before the next frame, which it asks for.
func (u *UI) post(fn func()) {
	u.mu.Lock()
	u.queue = append(u.queue, fn)
	u.mu.Unlock()
	if w := u.win.Load(); w != nil && !w.IsDestroyed() {
		w.Invalidate()
	}
}

// async runs work on a goroutine, and done with its outcome on the main thread.
func async[T any](u *UI, work func(ctx context.Context) (T, error), done func(T, error)) {
	go func() {
		v, err := work(context.Background())
		u.post(func() { done(v, err) })
	}()
}

func (u *UI) drain() {
	for {
		u.mu.Lock()
		q := u.queue
		u.queue = nil
		u.mu.Unlock()
		if len(q) == 0 {
			return
		}
		for _, fn := range q {
			fn()
		}
	}
}

func (u *UI) toast(msg string) { u.toasts = append(u.toasts, msg) }

func (u *UI) text() *messages { return textOf(u.state) }

func (u *UI) server(id int64) *api.Server {
	if u.state == nil {
		return nil
	}
	for i := range u.state.Servers {
		if u.state.Servers[i].ID == id {
			return &u.state.Servers[i]
		}
	}
	return nil
}

func appOf(sv *api.Server, id uint) *api.App {
	if sv == nil {
		return nil
	}
	for i := range sv.Apps {
		if sv.Apps[i].ID == id {
			return &sv.Apps[i]
		}
	}
	return nil
}

func (u *UI) go_(r route) {
	u.route = r
	u.drawer = false
}

func (u *UI) navigate(n api.Navigation) {
	u.go_(route{serverID: n.ServerID, appID: n.AppID})
	u.messages().showTarget(n)
}

// messages returns the messages page of the route, made anew when the route changed.
func (u *UI) messages() *messagesPage {
	r := u.route
	if u.msgs == nil || u.msgs.serverID != r.serverID || u.msgs.appID != r.appID {
		u.msgs = newMessagesPage(u, r.serverID, r.appID)
	}
	return u.msgs
}

func (u *UI) view(c *ui.Context) {
	u.drain()
	if u.state == nil {
		// The first frame: the state is ready once the controller started.
		s := u.svc.State()
		u.state = &s
		if n := u.svc.TakeNavigation(); n != nil {
			u.navigate(*n)
		}
	}
	p := paletteOf(c)
	applyTheme(c, p)
	for _, m := range u.toasts {
		c.Toast(m)
	}
	u.toasts = nil

	// A server or an app that went away leaves its page for all messages.
	if !u.route.settings && u.route.serverID != 0 {
		if sv := u.server(u.route.serverID); sv == nil {
			u.route = route{}
		} else if u.route.appID != 0 && appOf(sv, u.route.appID) == nil {
			u.route = route{serverID: sv.ID}
		}
	}

	w, _ := c.Size()
	wide := w >= narrow
	c.Root().Background(p.surface).FontSize(fontBase).TextColor(p.text)
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		if wide {
			ui.Column(c).Width(sidebarWidth).Background(p.body).Shrink(0).Children(func() {
				u.sidebar(c, 0)
			})
		}
		ui.Column(c).Grow(1).MinWidth(0).Background(p.surface).Children(func() {
			switch {
			case u.route.settings:
				u.set.view(c, wide)
			case len(u.state.Servers) == 0:
				u.welcome(c, wide)
			default:
				u.messages().view(c, wide)
			}
		})
	})
	if !wide {
		ui.DialogBase(c, &u.drawer, func(backdrop, panel *ui.Element) {
			// The backdrop centers its panel: the drawer takes the window's
			// left edge, from top to bottom, instead: a pane of glass
			// floating over the dimmed page.
			backdrop.Background(p.overlay)
			panel.Absolute().Left(8).Top(8).Bottom(8).Width(sidebarWidth).Radius(radiusPane).Material(glass.Glass{})
			u.sidebar(c, 8)
		})
	} else {
		u.drawer = false
	}
	u.dialogs(c)
}

// sidebarToggle opens the drawer of a narrow window.
func (u *UI) sidebarToggle(c *ui.Context) {
	if glassButton(c, u.text().openSidebar, iconPanelLeft, false).Clicked() {
		u.drawer = true
	}
}

// toolbar lays a page out as macOS 26's Notes does: a toolbar of the
// page's title and, at its end, buttons on glass, which moves the window
// and keeps clear of the window controls; below it what below shows, then
// content, which starts top DIPs down and scrolls under them, fading out
// at their edge.
func (u *UI) toolbar(c *ui.Context, wide bool, padX float32, title, subtitle string, end, below func(), content func(top float32)) {
	p := paletteOf(c)
	tb := c.TitleBar()
	left, right := float32(20), max(12, tb.Right+8)
	if !wide {
		left = tb.Left + 8
		if tb.Left == 0 {
			left = 12
		}
	}
	ui.Box(c).Grow(1).MinHeight(0).Children(func() {
		content(u.reach + 12)
		var solid *ui.Element
		ui.Column(c).Absolute().Top(0).Left(0).Right(0).PassThrough().Children(func() {
			solid = ui.Column(c).Background(p.surface).Children(func() {
				ui.Row(c).Height(toolbarHeight(c)).Padding(0, right, 0, left).Gap(8).DragWindow().Children(func() {
					if !wide {
						u.sidebarToggle(c)
					}
					ui.Column(c).Grow(1).MinWidth(0).Margin(0, 0, 0, 4).Children(func() {
						ui.Text(c, title).FontSize(fontLg).FontWeight(700).SingleLine()
						if subtitle != "" {
							ui.Text(c, subtitle).FontSize(fontSm).TextColor(p.muted).SingleLine()
						}
					})
					if end != nil {
						end()
					}
				})
				if below != nil {
					ui.Column(c).Padding(0, padX).Children(below)
				}
			})
			// The edge the content scrolls under fades out, as macOS 26's.
			clear := p.surface
			clear.A = 0
			ui.Box(c).Height(16).Gradient(p.surface, clear, 180).PassThrough()
		})
		// The bounds are the last frame's: draw another once they changed.
		if r := solid.Bounds(); r.H > 0 && r.H != u.reach {
			u.reach = r.H
			c.Invalidate()
		}
	})
}

func (u *UI) welcome(c *ui.Context, wide bool) {
	t := u.text()
	ui.Column(c).Fill().DragWindow().Children(func() {
		if !wide {
			ui.Row(c).Height(toolbarHeight(c)).Padding(0, 12, 0, max(12, c.TitleBar().Left+8)).DragWindow().Children(func() { u.sidebarToggle(c) })
		}
		ui.Column(c).Grow(1).Justify(ui.Center).Children(func() {
			emptyState(c, iconBellRing, 48, t.welcomeTitle, t.welcomeText, func() {
				if button(c, t.addServer, buttonOpts{kind: primary}).Clicked() {
					u.serverDlg = newServerDialog(u, addMode, nil)
				}
			})
		})
	})
}

// now is the time the view shows, ticking every interval while it is shown.
func now(c *ui.Context, every time.Duration) time.Time {
	c.After(every)
	return c.Now()
}
