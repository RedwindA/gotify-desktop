//go:build darwin

package appearance

import (
	"context"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
)

// AppKit color calls have to run on the main thread. When they cannot, the
// AppleAccentColor default is the same choice the user made.

var (
	appKitOnce sync.Once
	appKitOK   bool
	selCache   sync.Map
)

func readAccent() string {
	var hex string
	mygo.RunOnMain(func() { hex = accentFromAppKit() })
	if hex != "" {
		return hex
	}
	return accentFromDefaults()
}

func loadAppKit() bool {
	appKitOnce.Do(func() {
		_, err := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_GLOBAL|purego.RTLD_NOW)
		appKitOK = err == nil
	})
	return appKitOK && objc.GetClass("NSColor") != 0
}

func sel(name string) objc.SEL {
	if s, ok := selCache.Load(name); ok {
		return s.(objc.SEL)
	}
	s := objc.RegisterName(name)
	selCache.Store(name, s)
	return s
}

func cls(name string) objc.ID { return objc.ID(objc.GetClass(name)) }

func send(obj objc.ID, selector string, args ...any) objc.ID {
	return obj.Send(sel(selector), args...)
}

func withPool(fn func()) {
	pool := send(send(cls("NSAutoreleasePool"), "alloc"), "init")
	defer send(pool, "drain")
	fn()
}

func accentFromAppKit() string {
	if !loadAppKit() {
		return ""
	}
	var hex string
	withPool(func() {
		color := send(cls("NSColor"), "controlAccentColor")
		if color == 0 {
			return
		}
		if space := send(cls("NSColorSpace"), "sRGBColorSpace"); space != 0 {
			if c := send(color, "colorUsingColorSpace:", space); c != 0 {
				color = c
			}
		}
		r, g, b := math.NaN(), math.NaN(), math.NaN()
		var a float64
		send(color, "getRed:green:blue:alpha:", unsafe.Pointer(&r), unsafe.Pointer(&g), unsafe.Pointer(&b), unsafe.Pointer(&a))
		if math.IsNaN(r) || math.IsNaN(g) || math.IsNaN(b) {
			r = objc.Send[float64](color, sel("redComponent"))
			g = objc.Send[float64](color, sel("greenComponent"))
			b = objc.Send[float64](color, sel("blueComponent"))
		}
		hex = ComponentsHex(r, g, b)
	})
	return hex
}

func accentFromDefaults() string {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	out, err := exec.CommandContext(ctx, "defaults", "read", "-g", "AppleAccentColor").Output()
	if err != nil {
		return appleAccentHex(0, true)
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return ""
	}
	return appleAccentHex(n, false)
}
