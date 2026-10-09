// Package appearance reads the system's accent color.
package appearance

import (
	"sync"
	"time"
)

// accentCacheFor is how long Accent reuses a reading. The page asks at
// startup and again whenever the window is focused.
const accentCacheFor = 3 * time.Second

var accentCache struct {
	sync.Mutex
	at time.Time
	v  string
}

// Accent returns the system accent as "#rrggbb", or "" when it is unknown.
func Accent() string {
	accentCache.Lock()
	if time.Since(accentCache.at) < accentCacheFor {
		v := accentCache.v
		accentCache.Unlock()
		return v
	}
	accentCache.Unlock()

	v := readAccent()

	accentCache.Lock()
	accentCache.v = v
	accentCache.at = time.Now()
	accentCache.Unlock()
	return v
}

// ABGRHex formats a Windows DWM color DWORD (0xAABBGGRR) as "#rrggbb".
func ABGRHex(v uint32) string {
	return hex(uint8(v), uint8(v>>8), uint8(v>>16))
}

// ComponentsHex formats sRGB components in [0, 1] as "#rrggbb".
// A component outside that range, or a NaN, means the color is unset.
func ComponentsHex(r, g, b float64) string {
	if !unit(r) || !unit(g) || !unit(b) {
		return ""
	}
	return hex(byteOf(r), byteOf(g), byteOf(b))
}

func unit(v float64) bool { return v >= 0 && v <= 1 }

func byteOf(v float64) byte { return byte(v*255 + 0.5) }

func hex(r, g, b byte) string {
	const digits = "0123456789abcdef"
	return string([]byte{'#', digits[r>>4], digits[r&0xf], digits[g>>4], digits[g&0xf], digits[b>>4], digits[b&0xf]})
}

// appleAccentHex maps the AppleAccentColor default: -1 graphite, 0 red,
// 1 orange, 2 yellow, 3 green, 4 blue, 5 purple, 6 pink. missing is an
// absent key, which is blue. Any other index is unknown.
func appleAccentHex(index int, missing bool) string {
	if missing {
		index = 4
	}
	switch index {
	case -1:
		return "#8e8e93"
	case 0:
		return "#ff3b30"
	case 1:
		return "#ff9500"
	case 2:
		return "#ffcc00"
	case 3:
		return "#28cd41"
	case 4:
		return "#007aff"
	case 5:
		return "#af52de"
	case 6:
		return "#ff2d55"
	default:
		return ""
	}
}

// floatComponents reads three sRGB components from a decoded tuple.
func floatComponents(v any) (r, g, b float64, ok bool) {
	switch t := v.(type) {
	case []any:
		if len(t) != 3 {
			return 0, 0, 0, false
		}
		var ok1, ok2, ok3 bool
		r, ok1 = asFloat(t[0])
		g, ok2 = asFloat(t[1])
		b, ok3 = asFloat(t[2])
		return r, g, b, ok1 && ok2 && ok3
	case []float64:
		if len(t) != 3 {
			return 0, 0, 0, false
		}
		return t[0], t[1], t[2], true
	default:
		return 0, 0, 0, false
	}
}

func asFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	default:
		return 0, false
	}
}
