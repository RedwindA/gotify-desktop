package view

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"gotify-desktop/internal/app"
	"gotify-desktop/internal/conn"
	"gotify-desktop/internal/gotify"
	"gotify-desktop/internal/markdown"
)

type harness struct {
	t       *testing.T
	tt      *ui.Tester
	m       *Model
	f       *FakeBackend
	pending chan func()
}

func newHarness(t *testing.T, f *FakeBackend) *harness {
	h := &harness{t: t, f: f, pending: make(chan func(), 64)}
	h.m = New(f, Platform{
		Version: "0.1.0", DataDir: "/data",
		Update:  func(fn func()) { h.pending <- fn },
		Focused: func() bool { return false },
		PickCA:  func() (string, []byte, error) { return "ca.pem", []byte("junk"), nil },
	}, markdown.NewImageCache(nil, nil))
	h.tt = ui.NewTester(h.m.View, 1000, 700)
	return h
}

// settle runs the updates goroutines posted, then draws.
func (h *harness) settle() {
	h.t.Helper()
	for deadline := time.Now().Add(300 * time.Millisecond); time.Now().Before(deadline); {
		select {
		case fn := <-h.pending:
			fn()
			deadline = time.Now().Add(100 * time.Millisecond)
		case <-time.After(20 * time.Millisecond):
		}
	}
	h.tt.Frame()
}

func (h *harness) has(s string) bool {
	for _, tx := range h.tt.Texts() {
		if strings.Contains(tx, s) {
			return true
		}
	}
	return false
}

func (h *harness) want(s ...string) {
	h.t.Helper()
	for _, x := range s {
		if !h.has(x) {
			h.t.Errorf("missing %q in %q", x, h.tt.Texts())
		}
	}
}

func (h *harness) click(label string) {
	h.t.Helper()
	if err := h.tt.Click(label); err != nil {
		h.t.Fatal(err)
	}
}

func TestEmptyStateWelcome(t *testing.T) {
	h := newHarness(t, NewFakeBackend())
	h.want("Welcome to Gotify Desktop", "Add server")
	if h.has("Mark all read") {
		t.Fatal("no messages page without servers")
	}
}

func TestAddServerDialogValidationAndSubmit(t *testing.T) {
	f := NewFakeBackend()
	h := newHarness(t, f)
	h.click("Add server")
	h.want("Server address", "Username", "Password", "Advanced")
	h.click("Connect")
	h.want("Enter the server's address.", "Enter your username.", "Enter your password.")
	if len(f.Added) != 0 {
		t.Fatal("submitted an invalid form")
	}
	h.click("Server address")
	h.tt.Type("gotify.example.com/sub/")
	h.click("Username")
	h.tt.Type("alice")
	h.click("Password")
	h.tt.Type("secret")
	h.tt.Frame()
	if h.has("Enter your username.") {
		t.Fatal("error should clear once filled")
	}
	f.AddErr = gotify.ErrUnauthorized
	h.click("Connect")
	h.settle()
	h.want("Wrong username or password")
	f.AddErr = nil
	h.click("Connect")
	h.settle()
	if len(f.Added) != 1 || f.Added[0].URL != "https://gotify.example.com/sub" || f.Added[0].User != "alice" || f.Added[0].Pass != "secret" {
		t.Fatalf("%+v", f.Added)
	}
	if h.has("Wrong username") || h.has("Server address") {
		t.Fatalf("dialog should be closed: %q", h.tt.Texts())
	}
}

func TestAdvancedOptionsAndBadCA(t *testing.T) {
	h := newHarness(t, NewFakeBackend())
	h.click("Add server")
	h.click("Advanced")
	h.click("Skip TLS certificate verification")
	h.want("Anyone on the network could read your messages and token.")
	h.click("Choose file…")
	h.settle()
	h.want("This file is not a PEM certificate.")
}

func TestMessagesRenderWithMarkdown(t *testing.T) {
	h := newHarness(t, Demo("http://127.0.0.1:1/none.png"))
	h.want("All messages", "Home Server", "Work Gotify", "Backup", "Monitoring", "CI",
		"Disk almost full on nas-01", "Nightly backup finished", "Front door opened", "Build #1842 failed", "Priority 9")
	h.want("Largest folder: ", "Summary", "restic check: no errors", "nas-01")
	for _, s := range h.tt.Texts() {
		if strings.Contains(s, "**") || strings.Contains(s, "## ") {
			t.Fatalf("raw markdown leaked: %q", s)
		}
	}
	h.click("Monitoring")
	if h.has("Front door opened") || !h.has("Disk almost full on nas-01") {
		t.Fatalf("app scope: %q", h.tt.Texts())
	}
	h.click("Work Gotify")
	if !h.has("Build #1842 failed") || h.has("Nightly backup finished") {
		t.Fatalf("server scope: %q", h.tt.Texts())
	}
}

func TestSearchFiltersMessages(t *testing.T) {
	h := newHarness(t, Demo(""))
	h.click("Search messages")
	h.tt.Type("front door")
	h.tt.Frame()
	if !h.has("Front door opened") || h.has("Nightly backup finished") {
		t.Fatalf("%q", h.tt.Texts())
	}
	h.tt.Type("zzz")
	h.tt.Frame()
	h.want("No messages match")
}

func TestAuthFailedServerOffersSignIn(t *testing.T) {
	f := Demo("")
	f.Servers[1].State, f.Servers[1].Err, f.Servers[1].NeedLogin = conn.AuthFailed, "Wrong username or password", true
	f.Refresh()
	h := newHarness(t, f)
	h.want("Sign in again")
	h.click("Sign in again")
	h.want("Your session on “Work Gotify” ended.", "Username", "Password")
	h.click("Username")
	h.tt.Type("bob")
	h.click("Password")
	h.tt.Type("pw")
	h.click("Sign in")
	h.settle()
	if h.has("Your session on") {
		t.Fatal("dialog should close after signing in")
	}
	if f.Servers[1].State != conn.Connected {
		t.Fatalf("relogin not called: %v", f.Servers[1].State)
	}
}

func TestDeleteMessageAndFailure(t *testing.T) {
	f := Demo("")
	h := newHarness(t, f)
	f.DeleteErr = gotify.ErrUnauthorized
	if err := h.tt.RightClick("Front door opened"); err != nil {
		t.Fatal(err)
	}
	if err := h.tt.ChooseMenuItem("Delete"); err != nil {
		t.Fatal(err)
	}
	h.settle()
	h.want("Couldn't delete the message: Wrong username or password", "Front door opened")
	f.DeleteErr = nil
	if err := h.tt.RightClick("Front door opened"); err != nil {
		t.Fatal(err)
	}
	if err := h.tt.ChooseMenuItem("Delete"); err != nil {
		t.Fatal(err)
	}
	h.settle()
	f.Refresh()
	h.settle()
	if h.has("Front door opened") || len(f.Deleted) != 1 {
		t.Fatalf("deleted %v: %q", f.Deleted, h.tt.Texts())
	}
}

func TestCopyTextAndOpenLink(t *testing.T) {
	h := newHarness(t, Demo(""))
	if err := h.tt.RightClick("Front door opened"); err != nil {
		t.Fatal(err)
	}
	if err := h.tt.ChooseMenuItem("Copy text"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if !strings.Contains(h.tt.Clipboard(), "front door was opened") {
		t.Fatalf("clipboard %q", h.tt.Clipboard())
	}
	h.click("Open link")
	if got := h.tt.OpenedURLs(); len(got) != 1 || got[0] != "https://grafana.home.example/d/disk" {
		t.Fatalf("%q", got)
	}
}

func TestMarkAllReadAndRemoveServer(t *testing.T) {
	f := Demo("")
	h := newHarness(t, f)
	if f.Snapshot().Unread == 0 {
		t.Fatal("demo should have unread messages")
	}
	h.click("Mark all read")
	h.settle()
	if f.Snapshot().Unread != 0 {
		t.Fatalf("unread %d", f.Snapshot().Unread)
	}
	if err := h.tt.RightClick("Work Gotify"); err != nil {
		t.Fatal(err)
	}
	if err := h.tt.ChooseMenuItem("Remove…"); err != nil {
		t.Fatal(err)
	}
	h.want("Remove “Work Gotify”?")
	h.click("Remove")
	h.settle()
	if len(f.Snapshot().Servers) != 1 || h.has("Work Gotify") {
		t.Fatalf("%q", h.tt.Texts())
	}
}

func TestSettingsPage(t *testing.T) {
	f := Demo("")
	h := newHarness(t, f)
	h.click("Settings")
	h.want("Start at login", "Do not disturb", "Pause for 1 hour", "Send a test notification", "Gotify Desktop 0.1.0", "Data: /data")
	h.click("Pause for 1 hour")
	h.settle()
	if !f.Settings.PausedUntil.After(time.Now().Add(50 * time.Minute)) {
		t.Fatalf("not paused: %v", f.Settings.PausedUntil)
	}
	h.want("Paused until", "Resume")
	h.click("Resume")
	h.settle()
	if !f.Settings.PausedUntil.IsZero() {
		t.Fatal("not resumed")
	}
	h.click("Send a test notification")
	h.settle()
	if f.Tested.Load() != 1 {
		t.Fatal("test notification not sent")
	}
	h.click("Do not disturb")
	h.tt.Frame()
}

func TestNavigateHighlightsMessage(t *testing.T) {
	f := Demo("")
	h := newHarness(t, f)
	h.m.Navigate(1, 2, 9)
	h.settle()
	h.tt.Frame()
	if !h.has("Disk almost full on nas-01") || h.has("Front door opened") {
		t.Fatalf("%q", h.tt.Texts())
	}
	h.m.Navigate(99, 0, 0)
	h.settle()
	h.tt.Frame()
	h.want("All messages")
}

func TestRelTime(t *testing.T) {
	now := time.Date(2026, 3, 10, 15, 0, 0, 0, time.Local)
	for d, want := range map[time.Duration]string{10 * time.Second: "just now", 5 * time.Minute: "5m ago", 2 * time.Hour: "2h ago"} {
		if got := relTime(now, now.Add(-d)); got != want {
			t.Errorf("%v -> %q", d, got)
		}
	}
	if got := relTime(now, time.Date(2026, 3, 9, 20, 5, 0, 0, time.Local)); got != "Yesterday, 20:05" {
		t.Error(got)
	}
	if got := relTime(now, time.Date(2026, 1, 2, 9, 0, 0, 0, time.Local)); got != "Jan 2" {
		t.Error(got)
	}
	if _, err := normalizeURL("  "); err == nil {
		t.Error("empty url accepted")
	}
	if u, _ := normalizeURL("host:8080/gotify/"); u != "https://host:8080/gotify" {
		t.Error(u)
	}
	_ = app.Explain
	_ = errors.New
}
