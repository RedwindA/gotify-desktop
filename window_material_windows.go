//go:build windows

package main

import (
	"golang.org/x/sys/windows"

	"gotify-desktop/internal/api"
)

// windowMaterial is Mica where DWM draws a system backdrop: Windows 11 22H2
// (build 22621) or later, and any later major version. mygo maps
// VibrancySidebar onto that backdrop and leaves it off on older Windows.
func windowMaterial() api.Material {
	v := windows.RtlGetVersion()
	if micaSupported(v.MajorVersion, v.BuildNumber) {
		return api.MaterialMica
	}
	return api.MaterialNone
}

func micaSupported(major, build uint32) bool {
	return major > 10 || (major == 10 && build >= 22621)
}
