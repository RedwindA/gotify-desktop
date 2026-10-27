//go:build darwin

package notify

import "testing"

func TestPermissionOf(t *testing.T) {
	for _, c := range []struct {
		status, alertStyle int
		want               Permission
	}{
		{0, 0, PermissionPending},
		{1, 1, PermissionDenied}, // denied keeps the banner style it had
		{2, 0, PermissionQuiet},
		{2, 1, PermissionGranted},
		{3, 2, PermissionGranted}, // provisional
	} {
		if got := permissionOf(c.status, c.alertStyle); got != c.want {
			t.Errorf("status %d, style %d: %q, want %q", c.status, c.alertStyle, got, c.want)
		}
	}
}

func TestMajorVersion(t *testing.T) {
	for v, want := range map[string]int{"27.2": 27, "12.7.6": 12, "13": 13, "": 0} {
		if got := majorVersion(v); got != want {
			t.Errorf("%q: %d", v, got)
		}
	}
}
