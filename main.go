// Gotify Desktop is a desktop client that receives messages from Gotify servers.
package main

import (
	_ "embed"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"gotify-desktop/internal/app"
	"gotify-desktop/internal/conn"
	"gotify-desktop/internal/markdown"
	"gotify-desktop/internal/notify"
	"gotify-desktop/internal/secret"
	"gotify-desktop/internal/view"
)

const (
	appID      = "com.austin.gotifydesktop"
	appName    = "Gotify Desktop"
	appVersion = "0.1.0"
)

var (
	//go:embed resources/icon.png
	appIcon []byte
	//go:embed resources/tray-normal.png
	trayNormal []byte
	//go:embed resources/tray-unread.png
	trayUnread []byte
	//go:embed resources/tray-offline.png
	trayOffline []byte
	//go:embed resources/tray-mac-normal.png
	trayMacNormal []byte
	//go:embed resources/tray-mac-unread.png
	trayMacUnread []byte
	//go:embed resources/tray-mac-offline.png
	trayMacOffline []byte
)

func main() {
	mygo.App.SetName(appName)
	mygo.App.SetVersion(appVersion)
	setupLogging()
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	startHidden := mygo.App.WasOpenedAtLogin() || notify.IsActivationLaunch(os.Args)
	d := &desktop{startHidden: startHidden}
	mygo.App.OnSecondInstance(func(args []string, _ string) {
		if !notify.IsActivationLaunch(args) {
			d.showWindow()
		}
	})
	mygo.App.OnActivate(func(hasVisible bool) {
		if !hasVisible {
			d.showWindow()
		}
	})
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.OnQuit(d.close)
	mygo.App.WhenReady(d.start)
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

type desktop struct {
	startHidden bool

	ctrl     *app.App
	model    *view.Model
	notifier notify.Notifier
	tray     *mygo.Tray
	images   *markdown.ImageCache

	mu         sync.Mutex
	win        *mygo.Window
	closing    atomic.Bool
	dirty      atomic.Bool
	trayState  string
	pauseTimer *time.Timer
}

func (d *desktop) start() {
	dataDir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		log.Fatal(err)
	}
	cacheDir, err := mygo.App.Path(mygo.PathCache)
	if err != nil {
		log.Fatal(err)
	}
	d.notifier, err = notify.New(appID, appName, cacheDir, appIcon)
	if err != nil {
		log.Printf("notifications unavailable: %v", err)
		d.notifier, _ = notify.New("", appName, cacheDir, nil)
	}
	d.ctrl, err = app.New(app.Options{
		DataDir: dataDir, CacheDir: cacheDir, Tokens: secret.Keyring(), Notifier: d.notifier,
		OnChange: d.onChange,
	})
	if err != nil {
		log.Printf("starting: %v", err)
		mygo.Dialog.Error(appName, "Gotify Desktop could not open its data: "+err.Error())
		mygo.App.Exit(1)
		return
	}
	d.images = markdown.NewImageCache(&http.Client{Timeout: 10 * time.Second}, d.invalidate)
	d.model = view.New(d.ctrl, view.Platform{
		Version:        appVersion,
		DataDir:        dataDir,
		OpenAtLogin:    mygo.App.OpenAtLogin,
		SetOpenAtLogin: mygo.App.SetOpenAtLogin,
		PickCA:         d.pickCA,
		Focused:        d.focused,
		Update:         d.update,
	}, d.images)

	d.notifier.OnActivate(d.activated)
	mygo.Power.OnResume(d.ctrl.KickAll)
	mygo.Power.OnUnlockScreen(d.ctrl.KickAll)
	d.makeTray()
	d.refreshTray()
	if !d.startHidden {
		d.showWindow()
	}
}

func (d *desktop) close() {
	d.closing.Store(true)
	if d.ctrl != nil {
		d.ctrl.Close()
	}
}

func (d *desktop) window() *mygo.Window {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.win != nil && d.win.IsDestroyed() {
		d.win = nil
	}
	return d.win
}

func (d *desktop) focused() bool {
	w := d.window()
	return w != nil && w.IsFocused()
}

// update runs fn on the UI thread: through the window, which redraws, or on the main thread when there is none.
func (d *desktop) update(fn func()) {
	if w := d.window(); w != nil {
		w.Update(fn)
		return
	}
	mygo.RunOnMain(fn)
}

// invalidate redraws the window, coalescing requests so callers never wait for the main thread.
func (d *desktop) invalidate() {
	if d.closing.Load() || !d.dirty.CompareAndSwap(false, true) {
		return
	}
	go func() {
		d.dirty.Store(false)
		if w := d.window(); w != nil && !d.closing.Load() {
			w.Update(func() {})
		}
	}()
}

func (d *desktop) onChange() {
	if d.closing.Load() {
		return
	}
	d.invalidate()
	go d.refreshTray()
}

func (d *desktop) showWindow() {
	mygo.RunOnMain(func() {
		d.mu.Lock()
		w := d.win
		if w != nil && w.IsDestroyed() {
			w = nil
		}
		d.mu.Unlock()
		if w != nil {
			w.Show()
			w.Restore()
			w.Focus()
			return
		}
		w = mygo.NewWindow(mygo.WindowOptions{
			Title: appName, Width: 1100, Height: 740, MinWidth: 760, MinHeight: 480,
			StateKey: "main", Content: ui.View(d.model.View),
		})
		w.SetIcon(appIcon)
		w.OnFocus(d.model.Touch)
		w.OnClosed(func() {
			d.mu.Lock()
			if d.win == w {
				d.win = nil
			}
			d.mu.Unlock()
		})
		d.mu.Lock()
		d.win = w
		d.mu.Unlock()
		w.Focus()
	})
}

func (d *desktop) activated(id string) {
	act := d.ctrl.HandleActivation(id)
	mygo.RunOnMain(func() {
		if act.ClickURL != "" {
			go mygo.Shell.OpenExternal(act.ClickURL)
			return
		}
		d.showWindow()
		if act.ServerID != 0 {
			d.model.Navigate(act.ServerID, act.AppID, act.MessageID)
		}
	})
}

func (d *desktop) pickCA() (string, []byte, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent: d.window(), Title: "Choose a CA certificate",
		Filters: []mygo.FileFilter{{Name: "Certificates", Extensions: []string{"pem", "crt", "cer"}}, {Name: "All files", Extensions: []string{"*"}}},
	})
	if err != nil || len(paths) == 0 {
		return "", nil, err
	}
	data, err := os.ReadFile(paths[0])
	if err != nil {
		return "", nil, err
	}
	return filepath.Base(paths[0]), data, nil
}

func (d *desktop) makeTray() {
	icon, template := trayNormal, false
	if runtime.GOOS == "darwin" {
		icon, template = trayMacNormal, true
	}
	tray, err := mygo.NewTray(mygo.TrayOptions{Icon: icon, IconIsTemplate: template, ToolTip: appName, Menu: d.trayMenu(false)})
	if err != nil {
		log.Printf("tray unavailable: %v", err)
		return
	}
	tray.OnClick(d.showWindow)
	d.tray = tray
}

func (d *desktop) trayMenu(paused bool) *mygo.Menu {
	pause := &mygo.MenuItem{Label: "Pause notifications for 1 hour", Click: func(*mygo.MenuItem, *mygo.Window) { d.setPause(time.Now().Add(time.Hour)) }}
	if paused {
		pause = &mygo.MenuItem{Label: "Resume notifications", Click: func(*mygo.MenuItem, *mygo.Window) { d.setPause(time.Time{}) }}
	}
	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: "Open " + appName, Click: func(*mygo.MenuItem, *mygo.Window) { d.showWindow() }},
		pause,
		mygo.Separator(),
		{Role: mygo.RoleQuit},
	})
}

func (d *desktop) setPause(until time.Time) {
	go func() {
		s := d.ctrl.Settings()
		s.PausedUntil = until
		if err := d.ctrl.SetSettings(s); err != nil {
			log.Printf("pause: %v", err)
		}
	}()
}

// refreshTray updates the icon, tooltip, menu and badge to the current state.
func (d *desktop) refreshTray() {
	if d.ctrl == nil || d.closing.Load() {
		return
	}
	snap := d.ctrl.Snapshot()
	offline := false
	for _, sv := range snap.Servers {
		if sv.State != conn.Connected {
			offline = true
		}
	}
	paused := time.Now().Before(snap.Settings.PausedUntil)
	state := "normal"
	switch {
	case offline:
		state = "offline"
	case snap.Unread > 0:
		state = "unread"
	}
	key := fmt.Sprintf("%s/%d/%t", state, snap.Unread, paused)
	d.mu.Lock()
	changed := key != d.trayState
	d.trayState = key
	if d.pauseTimer != nil {
		d.pauseTimer.Stop()
		d.pauseTimer = nil
	}
	if paused {
		d.pauseTimer = time.AfterFunc(time.Until(snap.Settings.PausedUntil)+time.Second, d.refreshTray)
	}
	d.mu.Unlock()
	if !changed {
		return
	}
	mygo.App.SetBadgeCount(snap.Unread)
	if d.tray == nil {
		return
	}
	icons := map[string][2][]byte{"normal": {trayNormal, trayMacNormal}, "unread": {trayUnread, trayMacUnread}, "offline": {trayOffline, trayMacOffline}}[state]
	if runtime.GOOS == "darwin" {
		d.tray.SetIcon(icons[1], true)
	} else {
		d.tray.SetIcon(icons[0], false)
	}
	tip := appName + " — no unread messages"
	if snap.Unread > 0 {
		tip = fmt.Sprintf("%s — %d unread", appName, snap.Unread)
	}
	if offline {
		tip += " (a server is offline)"
	}
	if paused {
		tip += ", notifications paused"
	}
	d.tray.SetToolTip(tip)
	d.tray.SetMenu(d.trayMenu(paused))
}

// setupLogging writes the log to a file in the logs directory, rotating it at 5 MB.
func setupLogging() {
	dir, err := mygo.App.Path(mygo.PathLogs)
	if err != nil {
		return
	}
	w, err := newRotatingFile(filepath.Join(dir, "gotify-desktop.log"), 5<<20)
	if err != nil {
		return
	}
	log.SetOutput(w)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
}

type rotatingFile struct {
	mu   sync.Mutex
	path string
	max  int64
	f    *os.File
	size int64
}

func newRotatingFile(path string, max int64) (*rotatingFile, error) {
	r := &rotatingFile{path: path, max: max}
	if err := r.open(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *rotatingFile) open() error {
	if err := os.MkdirAll(filepath.Dir(r.path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	r.f, r.size = f, info.Size()
	return nil
}

func (r *rotatingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.size+int64(len(p)) > r.max {
		r.f.Close()
		os.Remove(r.path + ".1")
		os.Rename(r.path, r.path+".1")
		if err := r.open(); err != nil {
			return 0, errors.Join(err, errors.New("log rotation failed"))
		}
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, err
}
