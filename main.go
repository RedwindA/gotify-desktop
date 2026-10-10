// Gotify Desktop is a desktop client that receives messages from Gotify servers.
package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"

	"gotify-desktop/internal/api"
	"gotify-desktop/internal/app"
	"gotify-desktop/internal/appearance"
	"gotify-desktop/internal/conn"
	"gotify-desktop/internal/i18n"
	"gotify-desktop/internal/notify"
	"gotify-desktop/internal/secret"
)

const (
	appID   = "com.austin.gotifydesktop"
	appName = "Gotify Desktop"
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
	// The version is "version" in mygo.json, which mygo build stamps into
	// the app; the updater compares it with the published one.
	if mygo.App.Version() == "" {
		mygo.App.SetVersion("dev")
	}
	d := &desktop{activationLaunch: notify.IsActivationLaunch(os.Args), shots: os.Getenv("GOTIFY_SCREENSHOTS")}
	// Screenshots never update the app, log to stderr, and run beside an
	// installed app, which holds the lock.
	if d.shots == "" {
		mygo.Use(updater.Plugin)
		setupLogging()
		if !mygo.App.RequestSingleInstanceLock() {
			return
		}
	}
	// The images of messages come from a scheme of their own, which WebView2
	// serves under http://<scheme>.localhost.
	imageBase := imageScheme + "://localhost/"
	if runtime.GOOS == "windows" {
		imageBase = "http://" + imageScheme + ".localhost/"
	}
	d.api = api.New(api.Platform{
		Version:         mygo.App.Version(),
		OpenAtLogin:     mygo.App.OpenAtLogin,
		SetOpenAtLogin:  mygo.App.SetOpenAtLogin,
		PickCA:          d.pickCA,
		OpenURL:         func(u string) { mygo.Shell.OpenExternal(u) },
		CopyText:        mygo.Clipboard.WriteText,
		ImageBase:       imageBase,
		Confirm:         d.confirm,
		Appearance:      d.appearance,
		CheckForUpdates: updater.CheckForUpdates,
	})
	// Before Run, so macOS never installs the default menu (it has Reload and zoom).
	if runtime.GOOS == "darwin" {
		mygo.App.SetMenu(d.applicationMenu())
	}
	mygo.Bind(d.api.Service())
	mygo.Protocol.Handle(imageScheme, d.api.ImageHandler())
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

// imageScheme serves the images of messages (api.Controller.ImageHandler).
const imageScheme = "msgimg"

// controller is what the desktop needs of the controller: *app.App, or demoController.
type controller interface {
	api.Backend
	Settings() notify.Settings
	KickAll()
	HandleActivation(id string) app.Activation
	Close()
}

// demoController serves made-up servers and messages from memory, with
// GOTIFY_DEMO=1: no data or network, for working on the page. Its
// test notification is real, and clicking a notification shows its message.
type demoController struct {
	*api.FakeBackend
	notifier notify.Notifier
}

func (c demoController) Settings() notify.Settings { return c.Snapshot().Settings }
func (demoController) KickAll()                    {}
func (demoController) Close()                      {}

func (c demoController) Test() error {
	return c.notifier.Show(notify.Notification{
		ID: "test", Title: appName, Body: i18n.T("Notifications work."), AppName: appName, Level: notify.LevelNormal,
	})
}

func (demoController) HandleActivation(id string) app.Activation {
	serverID, appID, msgID, kind := notify.ParseID(id)
	return app.Activation{Kind: kind, ServerID: serverID, AppID: appID, MessageID: msgID}
}

func (d *desktop) demo() demoController {
	// The demo's image comes from a scheme of its own, which WebView2 serves under http://<scheme>.localhost.
	mygo.Protocol.HandleFunc("demo", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(api.DemoChart())
	})
	chart := "demo://localhost/chart.png"
	if runtime.GOOS == "windows" {
		chart = "http://demo.localhost/chart.png"
	}
	f := api.Demo(chart)
	f.OnChange = d.onChange
	return demoController{f, d.notifier}
}

type desktop struct {
	activationLaunch bool
	startHidden      bool
	shots            string // GOTIFY_SCREENSHOTS: the directory to render the views into, then quit

	ctrl     controller
	api      *api.Controller
	notifier notify.Notifier
	tray     *mygo.Tray

	mu         sync.Mutex
	win        *mygo.Window
	closing    atomic.Bool
	trayDirty  atomic.Bool
	trayState  string
	pauseTimer *time.Timer
	theme      *string // the appearance last applied, on the main thread
	menuHave   bool    // an application menu was installed for the language in menuLang
	menuLang   string
}

func (d *desktop) start() {
	i18n.SetSystem(mygo.App.Locale())
	d.startHidden = d.activationLaunch || mygo.App.WasOpenedAtLogin()
	dataDir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		log.Fatal(err)
	}
	cacheDir, err := mygo.App.Path(mygo.PathCache)
	if err != nil {
		log.Fatal(err)
	}
	if d.notifier, err = notify.New(appID, appName, cacheDir, appIcon); err != nil {
		log.Printf("notifications unavailable: %v", err)
		d.notifier, err = notify.Unsupported(), nil
	}
	if d.shots != "" {
		d.ctrl = screenshotController{d.demo()}
	} else if os.Getenv("GOTIFY_DEMO") == "1" {
		d.ctrl = d.demo()
	} else {
		d.ctrl, err = app.New(app.Options{
			DataDir: dataDir, CacheDir: cacheDir, Tokens: secret.NewFile(filepath.Join(dataDir, "tokens.json")), Notifier: d.notifier,
			OnChange: d.onChange,
		})
	}
	if err != nil {
		log.Printf("starting: %v", err)
		mygo.Dialog.Error(appName, i18n.T("Gotify Desktop could not open its data: ")+err.Error())
		mygo.App.Exit(1)
		return
	}
	d.applyTheme(d.ctrl.Settings().Theme)
	d.installAppMenu()
	d.api.Start(d.ctrl, dataDir)
	if d.shots != "" {
		d.showWindow()
		go d.screenshots()
		return
	}

	d.notifier.OnActivate(d.activated)
	mygo.Power.OnResume(d.ctrl.KickAll)
	mygo.Power.OnUnlockScreen(d.ctrl.KickAll)
	d.makeTray()
	d.requestTray()
	if !d.startHidden {
		d.showWindow()
	}
}

func (d *desktop) close() {
	d.closing.Store(true)
	d.api.Close()
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

func (d *desktop) onChange() {
	if d.closing.Load() {
		return
	}
	d.api.Changed()
	go d.requestTray()
}

// applyTheme gives the window, its title bar and the page the appearance the
// settings choose. It runs on the main thread, with the tray.
func (d *desktop) applyTheme(theme string) {
	if d.theme != nil && *d.theme == theme {
		return
	}
	d.theme = &theme
	switch theme {
	case "light":
		mygo.Theme.SetSource(mygo.ThemeLight)
	case "dark":
		mygo.Theme.SetSource(mygo.ThemeDark)
	default:
		mygo.Theme.SetSource(mygo.ThemeSystem)
	}
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
		opts := mygo.WindowOptions{
			Title: appName, Width: 1100, Height: 740, MinWidth: 420, MinHeight: 480,
			StateKey: "main", URL: "/",
			// The page draws the title bar: the sidebar and the page headers
			// move the window, with the system's window controls over them.
			TitleBarStyle: mygo.TitleBarHiddenInset,
			// The page's toolbar is one 48px row. Caption buttons fill it on
			// Windows and are centered in it on Linux; macOS keeps the traffic lights.
			TitleBarHeight: 48,
			// The page's body background (--color-background-body of the neutral theme).
			// It also paints the first frame over a material, so that frame is not white.
			BackgroundColor: "light-dark(#f1f1f1, #1b1b1b)",
		}
		if d.shots != "" {
			// The same size every time, and the page paints its own background.
			opts.StateKey = ""
		} else if windowMaterial() != api.MaterialNone {
			opts.Transparent = true
			opts.Vibrancy = mygo.VibrancySidebar
		}
		w = mygo.NewWindow(opts)
		w.SetIcon(appIcon)
		// Links in messages open in the browser, never in the window.
		w.Page().OnWillNavigate(func(e *mygo.NavigateEvent) {
			if u, err := url.Parse(e.URL); err == nil && (u.Scheme == "http" || u.Scheme == "https") && !isAppOrigin(u) {
				e.PreventDefault()
				go mygo.Shell.OpenExternal(e.URL)
			}
		})
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
			d.api.Navigate(api.Navigation{ServerID: act.ServerID, AppID: act.AppID, MessageID: act.MessageID})
		}
	})
}

// isAppOrigin reports whether u is a page of the app: its frontend on Windows, or the dev server.
func isAppOrigin(u *url.URL) bool {
	h := u.Hostname()
	return h == "mygo.localhost" || h == "localhost" || h == "127.0.0.1"
}

func (d *desktop) appearance() api.Appearance {
	if d.shots != "" {
		return api.Appearance{} // the theme's own accent and no material, as on every system
	}
	return api.Appearance{Accent: appearance.Accent(), Material: windowMaterial()}
}

// confirm asks in a native message box. Cancel is the default and the Escape
// button; index 0 would mean "a button named Cancel", which a custom label is not.
// mygo has no destructive-button style, so a destructive question uses the warning icon.
func (d *desktop) confirm(_ context.Context, c api.Confirmation) (bool, error) {
	res, err := mygo.Dialog.Message(confirmDialog(d.window(), c))
	if err != nil {
		return false, err
	}
	return res.Button == 0, nil
}

func confirmDialog(parent *mygo.Window, c api.Confirmation) mygo.MessageOptions {
	kind := mygo.MessageQuestion
	if c.Destructive {
		kind = mygo.MessageWarning
	}
	// The question is the bold main text (macOS's message text, the main
	// instruction of a Windows task dialog) and the explanation goes below it.
	message, detail := c.Title, c.Message
	if message == "" {
		message, detail = c.Message, ""
	}
	return mygo.MessageOptions{
		Parent: parent, Type: kind, Title: appName, Message: message, Detail: detail,
		Buttons: []string{c.ConfirmLabel, c.CancelLabel}, DefaultButton: 1, CancelButton: 1,
	}
}

// runCommand opens the window, then asks the page to run cmd. A page that is
// not loaded yet still receives it, through TakeCommand.
func (d *desktop) runCommand(cmd api.Command) {
	d.showWindow()
	d.api.Command(cmd)
}

// installAppMenu sets the macOS menu bar. Windows and Linux have none: the
// page handles Ctrl+, and Ctrl+N. It runs on the main thread.
func (d *desktop) installAppMenu() {
	if runtime.GOOS != "darwin" {
		return
	}
	lang := string(i18n.Current())
	if d.menuHave && d.menuLang == lang {
		return
	}
	d.menuHave = true
	d.menuLang = lang
	mygo.App.SetMenu(d.applicationMenu())
}

func (d *desktop) applicationMenu() *mygo.Menu {
	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: appName, Submenu: []*mygo.MenuItem{
			{Label: i18n.T("About Gotify Desktop"), Role: mygo.RoleAbout},
			mygo.Separator(),
			{Label: i18n.T("Settings…"), Accelerator: "Cmd+,", Click: func(*mygo.MenuItem, *mygo.Window) { d.runCommand(api.CommandSettings) }},
			{Label: i18n.T("Check for Updates…"), Click: func(*mygo.MenuItem, *mygo.Window) { updater.CheckForUpdates() }},
			mygo.Separator(),
			// The role alone is not a submenu, so macOS would not fill Services.
			{Label: i18n.T("Services"), Role: mygo.RoleServices, Submenu: []*mygo.MenuItem{}},
			{Label: i18n.T("Hide Gotify Desktop"), Role: mygo.RoleHide},
			{Label: i18n.T("Hide Others"), Role: mygo.RoleHideOthers},
			{Label: i18n.T("Show All"), Role: mygo.RoleUnhide},
			mygo.Separator(),
			{Label: i18n.T("Quit Gotify Desktop"), Role: mygo.RoleQuit},
		}},
		{Label: i18n.T("File"), Submenu: []*mygo.MenuItem{
			{Label: i18n.T("Add Server…"), Accelerator: "Cmd+N", Click: func(*mygo.MenuItem, *mygo.Window) { d.runCommand(api.CommandAddServer) }},
			mygo.Separator(),
			{Label: i18n.T("Close Window"), Role: mygo.RoleClose},
		}},
		{Label: i18n.T("Edit"), Role: mygo.RoleEditMenu},
		{Label: i18n.T("Window"), Role: mygo.RoleWindowMenu},
	})
}

func (d *desktop) pickCA(context.Context) (string, []byte, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent: d.window(), Title: i18n.T("Choose a CA certificate"),
		Filters: []mygo.FileFilter{{Name: i18n.T("Certificates"), Extensions: []string{"pem", "crt", "cer"}}, {Name: i18n.T("All files"), Extensions: []string{"*"}}},
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
	pause := &mygo.MenuItem{Label: i18n.T("Pause notifications for 1 hour"), Click: func(*mygo.MenuItem, *mygo.Window) { d.setPause(time.Now().Add(time.Hour)) }}
	if paused {
		pause = &mygo.MenuItem{Label: i18n.T("Resume notifications"), Click: func(*mygo.MenuItem, *mygo.Window) { d.setPause(time.Time{}) }}
	}
	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: i18n.T("Open Gotify Desktop"), Click: func(*mygo.MenuItem, *mygo.Window) { d.showWindow() }},
		pause,
		{Label: i18n.T("Check for Updates…"), Click: func(*mygo.MenuItem, *mygo.Window) { updater.CheckForUpdates() }},
		mygo.Separator(),
		{Label: i18n.T("Quit"), Role: mygo.RoleQuit},
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

// requestTray asks for the tray to be brought up to date. Requests coalesce, and
// the update runs on the main thread, one at a time, from the latest snapshot.
func (d *desktop) requestTray() {
	if d.closing.Load() || !d.trayDirty.CompareAndSwap(false, true) {
		return
	}
	mygo.RunOnMain(d.applyTray)
}

func (d *desktop) applyTray() {
	d.trayDirty.Store(false)
	if d.ctrl == nil || d.closing.Load() {
		return
	}
	snap := d.ctrl.Snapshot()
	d.applyTheme(snap.Settings.Theme)
	d.installAppMenu()
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
	key := fmt.Sprintf("%s/%d/%t/%s", state, snap.Unread, paused, i18n.Current())
	d.mu.Lock()
	changed := key != d.trayState
	d.trayState = key
	if d.pauseTimer != nil {
		d.pauseTimer.Stop()
		d.pauseTimer = nil
	}
	if paused {
		d.pauseTimer = time.AfterFunc(time.Until(snap.Settings.PausedUntil)+time.Second, d.requestTray)
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
	tip := i18n.T("Gotify Desktop — no unread messages")
	if snap.Unread > 0 {
		tip = i18n.T("Gotify Desktop — %d unread", snap.Unread)
	}
	if offline {
		tip += i18n.T(" (a server is offline)")
	}
	if paused {
		tip += i18n.T(", notifications paused")
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
