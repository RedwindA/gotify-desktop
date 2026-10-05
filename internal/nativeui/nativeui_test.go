package nativeui

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"gotify-desktop/internal/api"
	"gotify-desktop/internal/i18n"
)

// demoChartURL is the image of the demo's chart message.
const demoChartURL = "https://demo.example/chart.png"

func newDemo(t *testing.T) (*UI, *api.FakeBackend) {
	t.Helper()
	i18n.SetPreference("en")
	ctrl := api.New(api.Platform{Version: "test", ImageBase: "native:"})
	u := New(ctrl)
	f := api.Demo(demoChartURL)
	f.Images[demoChartURL] = api.DemoChart()
	f.OnChange = ctrl.Changed
	ctrl.Redirect(u.SetState, u.Navigate)
	ctrl.Start(f, "/home/demo/.local/share/gotify-desktop")
	return u, f
}

// settle draws frames until the work the view started is done.
func settle(tt *ui.Tester) {
	for range 20 {
		time.Sleep(10 * time.Millisecond)
		tt.Frame()
	}
}

func TestMessagesShow(t *testing.T) {
	u, _ := newDemo(t)
	tt := ui.NewTester(u.view, 1100, 760)
	settle(tt)
	for _, s := range []string{"All messages", "Disk almost full on nas-01", "Home Server", "Can't reach Work Gotify", "Grafana dashboard"} {
		if !tt.HasText(s) {
			t.Errorf("no %q in %q", s, tt.Texts())
		}
	}
	if err := tt.Click("Settings"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if !u.route.settings || !tt.HasText("Do not disturb") {
		t.Errorf("settings not shown: %q", tt.Texts())
	}
}

func TestSearch(t *testing.T) {
	u, _ := newDemo(t)
	tt := ui.NewTester(u.view, 1100, 760)
	settle(tt)
	if err := tt.Click("Search messages"); err != nil {
		t.Fatal(err)
	}
	tt.Type("nightly")
	time.Sleep(searchDelay)
	settle(tt)
	if tt.HasText("Disk almost full on nas-01") || !tt.HasText("Nightly backup finished") {
		t.Errorf("search did not filter: %q", tt.Texts())
	}
}

func TestNarrowDrawer(t *testing.T) {
	u, _ := newDemo(t)
	tt := ui.NewTester(u.view, 600, 760)
	settle(tt)
	if tt.HasText("Deploys") {
		t.Fatalf("a narrow window shows the sidebar: %q", tt.Texts())
	}
	if err := tt.Click("Show sidebar"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	// The drawer holds every item, the servers' too, from top to bottom.
	r, ok := tt.Find("Deploys")
	if !ok || !tt.HasText("All messages") {
		t.Fatalf("the drawer lacks the servers: %q", tt.Texts())
	}
	if r.X > sidebarWidth {
		t.Errorf("Deploys at %v, outside the drawer", r)
	}
	if err := tt.Click("Deploys"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if u.drawer || u.route != (route{serverID: 2, appID: 2}) {
		t.Errorf("choosing an app left drawer %v, route %+v", u.drawer, u.route)
	}
}

// TestScreenshots writes the main views to $NATIVEUI_SHOTS, light and dark,
// to compare with the page's.
func TestScreenshots(t *testing.T) {
	dir := os.Getenv("NATIVEUI_SHOTS")
	if dir == "" {
		t.Skip("NATIVEUI_SHOTS is not set")
	}
	shots := []struct {
		name string
		w, h int
		prep func(u *UI)
		// then runs once the view settled, before the shot.
		then func(u *UI)
	}{
		{"main", 1100, 760, nil, nil},
		// The cards scroll under the bar of glass.
		{"scrolled", 1100, 760, nil, func(u *UI) { u.msgs.list.ScrollTo(1, ui.Start) }},
		{"app-scope", 1100, 760, func(u *UI) { u.go_(route{serverID: 1, appID: 1}) }, nil},
		{"settings", 1100, 760, func(u *UI) { u.go_(route{settings: true}) }, nil},
		{"add-server", 1000, 760, func(u *UI) { u.serverDlg = newServerDialog(u, addMode, nil); u.serverDlg.advanced = true }, nil},
		{"prefs", 1000, 760, func(u *UI) { u.prefs = &prefsDialog{serverID: 1, appID: 2, open: true} }, nil},
		{"narrow", 600, 760, nil, nil},
		{"drawer", 600, 760, func(u *UI) { u.drawer = true }, nil},
	}
	for _, s := range shots {
		for _, dark := range []bool{false, true} {
			u, _ := newDemo(t)
			if s.prep != nil {
				s.prep(u)
			}
			tt := ui.NewTester(u.view, s.w, s.h)
			tt.SetDark(dark)
			tt.SetScale(2)
			settle(tt)
			if s.then != nil {
				s.then(u)
				settle(tt)
			}
			name := s.name + "-light.png"
			if dark {
				name = s.name + "-dark.png"
			}
			f, err := os.Create(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			if err := png.Encode(f, tt.Image()); err != nil {
				t.Fatal(err)
			}
			f.Close()
		}
	}
}
