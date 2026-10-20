//go:build windows

package notify

import (
	"os"
	"testing"

	"github.com/go-ole/go-ole"
)

func TestToastHStringPreservesUnicode(t *testing.T) {
	for _, s := range []string{
		"",
		"Gotify 通知",
		buildToastXML(Notification{ID: "unicode", Title: "🔔 通知", Body: "✅ 完成 🚀 𠮷", Level: LevelNormal}),
	} {
		h, err := newHString(s)
		if err != nil {
			t.Fatal(err)
		}
		got := h.String()
		ole.DeleteHString(h)
		if got != s {
			t.Errorf("WinRT string was truncated: got %q, want %q", got, s)
		}
	}
}

func TestToastTagsAreStableAndBounded(t *testing.T) {
	a := toastTag("s9223372036854775807-m18446744073709551615")
	if len(a) > 16 || a != toastTag("s9223372036854775807-m18446744073709551615") || a == toastTag("s1-m1") {
		t.Fatalf("invalid toast tag %q", a)
	}
}

// Opt in on a Windows desktop: exercises the actual ABI, briefly shows a
// silent notification, then removes it. Unit tests never modify toast history.
func TestWinRTToastRoundTrip(t *testing.T) {
	if os.Getenv("GOTIFY_NOTIFY_IT") != "1" {
		t.Skip("set GOTIFY_NOTIFY_IT=1 on a Windows desktop")
	}
	n := Notification{ID: "gotify-backend-reliability-test", Title: "🔔 Gotify 通知测试", Body: "✅ This silent test notification is removed immediately. 🚀 𠮷", Level: LevelSilent}
	const appID = "dev.gotify.desktop.notifytest"
	notifier, err := New(appID, "Gotify notification test", t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	win := notifier.(*winNotifier)
	if err := notifier.Show(n); err != nil {
		t.Fatal(err)
	}
	if err := win.run(func() error { return removeTaggedToast(appID, n.ID) }); err != nil {
		t.Fatal(err)
	}
}
