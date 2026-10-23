package api

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestCopyImageStitchesSlices(t *testing.T) {
	_, d := start(t, NewFakeBackend(), Platform{})
	if err := d.BeginImage(10, 10); err == nil {
		t.Fatal("no capture: want an error")
	}

	// A page 20 CSS pixels wide and 4 tall at 2x, whose fill changes with every capture.
	fills := []color.RGBA{{255, 0, 0, 255}, {0, 0, 255, 255}}
	n := 0
	capture := func() ([]byte, error) {
		img := image.NewRGBA(image.Rect(0, 0, 40, 8))
		c := fills[n%len(fills)]
		n++
		for y := range 8 {
			for x := range 40 {
				img.SetRGBA(x, y, c)
			}
		}
		var buf bytes.Buffer
		err := png.Encode(&buf, img)
		return buf.Bytes(), err
	}
	var clip []byte
	_, d = start(t, NewFakeBackend(), Platform{CapturePage: capture, CopyImage: func(b []byte) error { clip = b; return nil }})
	if err := d.CopyImage(); err == nil {
		t.Fatal("nothing captured: want an error")
	}
	// A message 10 wide and 6 tall: 4 from the first capture, 2 from the second.
	if err := d.BeginImage(10, 6); err != nil {
		t.Fatal(err)
	}
	for _, s := range []ImageSlice{{Width: 10, Height: 4, ViewportWidth: 20}, {Width: 10, Height: 2, ToY: 4, ViewportWidth: 20}} {
		if err := d.CaptureSlice(s); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.CopyImage(); err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(clip))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 20 || b.Dy() != 12 {
		t.Fatalf("size %v, want 20x12", b)
	}
	for _, p := range []struct {
		y    int
		want color.RGBA
	}{{0, fills[0]}, {7, fills[0]}, {8, fills[1]}, {11, fills[1]}} {
		if got := color.RGBAModel.Convert(img.At(19, p.y)).(color.RGBA); got != p.want {
			t.Errorf("row %d: %v, want %v", p.y, got, p.want)
		}
	}

	if err := d.BeginImage(1e6, 1e6); err != nil {
		t.Fatal(err)
	}
	if err := d.CaptureSlice(ImageSlice{Width: 10, Height: 4, ViewportWidth: 20}); err == nil {
		t.Fatal("too large: want an error")
	}
}
