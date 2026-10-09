//go:build linux

package appearance

import (
	"context"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	portalBus       = "org.freedesktop.portal.Desktop"
	portalPath      = "/org/freedesktop/portal/desktop"
	portalInterface = "org.freedesktop.portal.Settings"
	appearanceNS    = "org.freedesktop.appearance"
	accentKey       = "accent-color"
	accentTimeout   = 500 * time.Millisecond
)

func readAccent() string {
	ctx, cancel := context.WithTimeout(context.Background(), accentTimeout)
	defer cancel()
	ch := make(chan string, 1)
	go func() { ch <- portalAccent(ctx) }()
	select {
	case s := <-ch:
		return s
	case <-ctx.Done():
		return ""
	}
}

func portalAccent(ctx context.Context) string {
	conn, err := dbus.ConnectSessionBus(dbus.WithContext(ctx))
	if err != nil {
		return ""
	}
	defer conn.Close()
	obj := conn.Object(portalBus, dbus.ObjectPath(portalPath))
	if hex, ok := readOne(ctx, obj); ok {
		return hex
	}
	return readNamespace(ctx, obj)
}

func readOne(ctx context.Context, obj dbus.BusObject) (string, bool) {
	var v dbus.Variant
	if err := obj.CallWithContext(ctx, portalInterface+".ReadOne", 0, appearanceNS, accentKey).Store(&v); err != nil {
		return "", false
	}
	return hexOf(unwrap(v)), true
}

func readNamespace(ctx context.Context, obj dbus.BusObject) string {
	var dict map[string]dbus.Variant
	if err := obj.CallWithContext(ctx, portalInterface+".Read", 0, appearanceNS).Store(&dict); err != nil {
		return ""
	}
	v, ok := dict[accentKey]
	if !ok {
		return ""
	}
	return hexOf(unwrap(v))
}

func unwrap(v any) any {
	for {
		d, ok := v.(dbus.Variant)
		if !ok {
			break
		}
		v = d.Value()
	}
	items, ok := v.([]any)
	if !ok {
		return v
	}
	out := make([]any, len(items))
	for i, item := range items {
		out[i] = unwrap(item)
	}
	return out
}

func hexOf(v any) string {
	r, g, b, ok := floatComponents(v)
	if !ok {
		return ""
	}
	return ComponentsHex(r, g, b)
}
