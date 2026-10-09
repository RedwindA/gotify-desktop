package appearance

import (
	"math"
	"testing"
)

func TestABGRHex(t *testing.T) {
	// Windows blue, 0xAABBGGRR: alpha is not part of the color.
	if got, want := ABGRHex(0xFFD77800), "#0078d7"; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
	if got, want := ABGRHex(0x00D77800), "#0078d7"; got != want {
		t.Fatalf("alpha changed the color: %s", got)
	}
	if got, want := ABGRHex(0x000000FF), "#ff0000"; got != want {
		t.Fatalf("red: %s", got)
	}
	if got, want := ABGRHex(0x00FF0000), "#0000ff"; got != want {
		t.Fatalf("blue: %s", got)
	}
	if got, want := ABGRHex(0x0000FF00), "#00ff00"; got != want {
		t.Fatalf("green: %s", got)
	}
}

func TestComponentsHex(t *testing.T) {
	if got, want := ComponentsHex(0, 0.5, 1), "#0080ff"; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
	if got, want := ComponentsHex(1, 1, 1), "#ffffff"; got != want {
		t.Fatalf("white: %s", got)
	}
	if got, want := ComponentsHex(0, 0, 0), "#000000"; got != want {
		t.Fatalf("black: %s", got)
	}
	for _, c := range [][3]float64{
		{-0.01, 0, 0},
		{0, 1.01, 0},
		{0, 0, math.NaN()},
		{math.Inf(1), 0, 0},
		{2, 2, 2},
	} {
		if got := ComponentsHex(c[0], c[1], c[2]); got != "" {
			t.Fatalf("%v: got %q, want unset", c, got)
		}
	}
}

func TestAppleAccentHex(t *testing.T) {
	if got, want := appleAccentHex(0, false), "#ff3b30"; got != want {
		t.Fatalf("red: %s", got)
	}
	if got, want := appleAccentHex(-1, false), "#8e8e93"; got != want {
		t.Fatalf("graphite: %s", got)
	}
	if got, want := appleAccentHex(4, false), appleAccentHex(0, true); got != want || got != "#007aff" {
		t.Fatalf("missing key: %s", got)
	}
	if got := appleAccentHex(9, false); got != "" {
		t.Fatalf("unknown index: %s", got)
	}
}

func TestFloatComponents(t *testing.T) {
	r, g, b, ok := floatComponents([]any{0.0, 0.5, 1.0})
	if !ok || ComponentsHex(r, g, b) != "#0080ff" {
		t.Fatalf("%v %v %v %v", r, g, b, ok)
	}
	r, g, b, ok = floatComponents([]float64{1, 0, 0})
	if !ok || ComponentsHex(r, g, b) != "#ff0000" {
		t.Fatalf("slice: %v %v", r, ok)
	}
	if _, _, _, ok := floatComponents([]any{0.0, 0.5}); ok {
		t.Fatal("a short tuple was accepted")
	}
	if _, _, _, ok := floatComponents("nope"); ok {
		t.Fatal("a string was accepted")
	}
	r, g, b, ok = floatComponents([]any{-1.0, 0.0, 0.0})
	if !ok || ComponentsHex(r, g, b) != "" {
		t.Fatalf("out of range should be unset, ok %v hex %q", ok, ComponentsHex(r, g, b))
	}
}
