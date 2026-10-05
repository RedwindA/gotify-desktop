//go:build windows

package notify

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"git.sr.ht/~jackmordaunt/go-toast/v2"
	"git.sr.ht/~jackmordaunt/go-toast/v2/wintoast"
	"golang.org/x/sys/windows/registry"
)

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
	guid := activatorCLSID(appID)
	if err := toast.SetAppData(toast.AppData{AppID: appID, GUID: guid, ActivationExe: exe, IconPath: iconPath}); err != nil {
		return nil, err
	}
	if err := refreshRegistry(appID, guid, appName, iconPath, exe); err != nil {
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

const legacyActivatorCLSID = "{6B1F3A52-8C4D-4E7A-9B2E-5D0C7A1F4E93}"

func setRegistryString(path, name, value string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(name, value)
}

// refreshRegistry rewrites the values go-toast only writes when missing, so a moved
// executable, a new icon or a changed CLSID take effect, and removes the class an
// earlier build registered under a fixed CLSID.
func refreshRegistry(appID, guid, appName, iconPath, exe string) error {
	registry.DeleteKey(registry.CURRENT_USER, `SOFTWARE\Classes\CLSID\`+legacyActivatorCLSID+`\LocalServer32`)
	registry.DeleteKey(registry.CURRENT_USER, `SOFTWARE\Classes\CLSID\`+legacyActivatorCLSID)
	app := `SOFTWARE\Classes\AppUserModelId\` + appID
	values := [][2]string{{"DisplayName", appName}, {"CustomActivator", guid}}
	if iconPath != "" {
		values = append(values, [2]string{"IconUri", iconPath})
	}
	for _, v := range values {
		if err := setRegistryString(app, v[0], v[1]); err != nil {
			return err
		}
	}
	return setRegistryString(`SOFTWARE\Classes\CLSID\`+guid+`\LocalServer32`, "", `"`+exe+`"`)
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
	xml := buildToastXML(nn)
	if err := n.run(func() error { return wintoast.Push(n.appID, xml) }); err != nil {
		return fmt.Errorf("notify: %w", err)
	}
	return nil
}
