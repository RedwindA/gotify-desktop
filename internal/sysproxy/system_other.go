//go:build !windows

package sysproxy

// system returns nil: elsewhere the environment names the proxy.
func system() *config { return nil }
