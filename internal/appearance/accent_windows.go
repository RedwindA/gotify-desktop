//go:build windows

package appearance

import "golang.org/x/sys/windows/registry"

func readAccent() string {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\DWM`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("AccentColor")
	if err != nil {
		return ""
	}
	return ABGRHex(uint32(v))
}
