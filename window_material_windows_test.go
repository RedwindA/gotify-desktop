//go:build windows

package main

import (
	"testing"

	"golang.org/x/sys/windows"

	"gotify-desktop/internal/api"
)

func TestMicaSupported(t *testing.T) {
	if !micaSupported(10, 22621) || !micaSupported(10, 26100) || !micaSupported(11, 0) {
		t.Fatal("22H2 and later should get Mica")
	}
	if micaSupported(10, 22620) || micaSupported(10, 22000) || micaSupported(10, 19045) || micaSupported(6, 22621) {
		t.Fatal("Windows 10 and Windows 11 before 22H2 stay opaque")
	}
}

func TestWindowMaterialMatchesBuild(t *testing.T) {
	v := windows.RtlGetVersion()
	want := api.MaterialNone
	if micaSupported(v.MajorVersion, v.BuildNumber) {
		want = api.MaterialMica
	}
	if got := windowMaterial(); got != want {
		t.Fatalf("Windows %d build %d: got %q, want %q", v.MajorVersion, v.BuildNumber, got, want)
	}
}
