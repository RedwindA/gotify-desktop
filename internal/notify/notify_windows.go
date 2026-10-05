//go:build windows

package notify

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"git.sr.ht/~jackmordaunt/go-toast/v2"
	"git.sr.ht/~jackmordaunt/go-toast/v2/wintoast"
	"golang.org/x/sys/windows/registry"
)

const activatorGUID = "{6B1F3A52-8C4D-4E7A-9B2E-5D0C7A1F4E93}"

// winNotifier runs every COM call on one locked OS thread so the multithreaded
// apartment that go-toast initialises outlives the calls.
type winNotifier struct {
	appID string
	act   activator
	work  chan func()
}

func New(appID, appName, cacheDir string, appIconPNG []byte) (Notifier, error) {
	iconPath := ""
	if len(appIconPNG) > 0 {
		if err := os.MkdirAll(cacheDir, 0o755); err != nil {
			return nil, err
		}
		iconPath = filepath.Join(cacheDir, "appicon.png")
		if err := os.WriteFile(iconPath, appIconPNG, 0o644); err != nil {
			return nil, err
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	if err := toast.SetAppData(toast.AppData{AppID: appID, GUID: activatorGUID, ActivationExe: exe, IconPath: iconPath}); err != nil {
		return nil, err
	}
	if err := refreshRegistry(appID, appName, iconPath, exe); err != nil {
		return nil, err
	}
	n := &winNotifier{appID: appID, work: make(chan func())}
	toast.SetActivationCallback(func(args string, _ []toast.UserData) { n.act.fire(args) })
	go func() {
		runtime.LockOSThread()
		for f := range n.work {
			f()
		}
	}()
	// A push with unparsable XML initialises COM and registers the activation
	// class factory without showing anything, so a relaunch via -Embedding can
	// receive its click before the first real toast.
	n.run(func() error { return wintoast.Push(appID, "") })
	return n, nil
}

// go-toast writes these registry values only when missing; rewrite them so a
// moved executable, a new icon or the display name take effect.
func refreshRegistry(appID, appName, iconPath, exe string) error {
	set := func(path, name, value string) error {
		k, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.SET_VALUE)
		if err != nil {
			return err
		}
		defer k.Close()
		return k.SetStringValue(name, value)
	}
	app := `SOFTWARE\Classes\AppUserModelId\` + appID
	if err := set(app, "DisplayName", appName); err != nil {
		return err
	}
	if iconPath != "" {
		if err := set(app, "IconUri", iconPath); err != nil {
			return err
		}
	}
	return set(`SOFTWARE\Classes\CLSID\`+activatorGUID+`\LocalServer32`, "", `"`+exe+`"`)
}

func (n *winNotifier) run(f func() error) error {
	res := make(chan error, 1)
	n.work <- func() { res <- f() }
	return <-res
}

func (n *winNotifier) Supported() bool { return true }

func (n *winNotifier) Remove(string) {}

func (n *winNotifier) OnActivate(fn func(string)) { n.act.set(fn) }

func (n *winNotifier) Show(nn Notification) error {
	body := strings.ReplaceAll(nn.Body, "]]>", "]] >")
	if nn.AppName != "" && nn.AppName != nn.Title {
		body = nn.AppName + " · " + body
	}
	t := toast.Notification{
		AppID:               n.appID,
		Title:               strings.ReplaceAll(nn.Title, "]]>", "]] >"),
		Body:                body,
		Icon:                html.EscapeString(nn.IconPath),
		IconCrop:            toast.CropStyleCircle,
		HeroIcon:            html.EscapeString(nn.ImagePath),
		ActivationType:      toast.Foreground,
		ActivationArguments: html.EscapeString(nn.ID),
		Audio:               toast.Default,
		Duration:            toast.Short,
	}
	switch nn.Level {
	case LevelSilent:
		t.Audio = toast.Silent
	case LevelHigh:
		t.Audio = toast.Reminder
		t.Duration = toast.Long
	}
	if err := n.run(t.Push); err != nil {
		return fmt.Errorf("notify: %w", err)
	}
	return nil
}
