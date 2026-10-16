//go:build windows

package sysproxy

import "golang.org/x/sys/windows/registry"

// system reads the proxy of the user's Internet settings, or nil when it is off.
func system() *config {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.QUERY_VALUE)
	if err != nil {
		return nil
	}
	defer k.Close()
	if on, _, err := k.GetIntegerValue("ProxyEnable"); err != nil || on == 0 {
		return nil
	}
	server, _, err := k.GetStringValue("ProxyServer")
	if err != nil {
		return nil
	}
	override, _, _ := k.GetStringValue("ProxyOverride")
	return parse(server, override)
}
