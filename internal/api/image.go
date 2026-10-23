package api

import (
	"bytes"
	"errors"
	"image"
	"image/draw"
	"image/png"
	"math"

	"gotify-desktop/internal/i18n"
)

// maxImagePixels bounds the image BeginImage assembles, about 400 MB of RGBA.
const maxImagePixels = 100 << 20

// ImageSlice is a part of the visible page that CaptureSlice copies into the
// image being assembled, in CSS pixels: the rectangle X, Y, Width, Height of
// a page ViewportWidth wide goes to ToY in the image.
type ImageSlice struct {
	X             float64 `json:"x"`
	Y             float64 `json:"y"`
	Width         float64 `json:"width"`
	Height        float64 `json:"height"`
	ToY           float64 `json:"toY"`
	ViewportWidth float64 `json:"viewportWidth"`
}

// pageImage is the image CaptureSlice assembles: its size in CSS pixels until
// the first slice gives the page's scale.
type pageImage struct {
	width, height float64
	img           *image.RGBA
	scale         float64
}

func errNoCapture() error { return errors.New(i18n.T("Copying as an image is not available.")) }

// BeginImage starts an image of width × height CSS pixels, which the page
// fills with CaptureSlice and puts on the clipboard with CopyImage. The page
// lays out what it shows that tall in slices, since only the visible page
// can be captured.
func (d *Desktop) BeginImage(width, height float64) error {
	if d.plat.CapturePage == nil || d.plat.CopyImage == nil {
		return errNoCapture()
	}
	if !(width >= 1 && height >= 1) {
		return errors.New("the image is empty")
	}
	d.mu.Lock()
	d.image = &pageImage{width: width, height: height}
	d.mu.Unlock()
	return nil
}

// CaptureSlice captures the visible page and copies a slice of it into the
// image BeginImage started.
func (d *Desktop) CaptureSlice(s ImageSlice) error {
	if d.plat.CapturePage == nil {
		return errNoCapture()
	}
	data, err := d.plat.CapturePage()
	if err != nil {
		return err
	}
	shot, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	pi := d.image
	if pi == nil {
		return errors.New("no image was begun")
	}
	if pi.img == nil {
		if !(s.ViewportWidth > 0) {
			return errors.New("the viewport has no width")
		}
		pi.scale = float64(shot.Bounds().Dx()) / s.ViewportWidth
		w, h := int(math.Round(pi.width*pi.scale)), int(math.Round(pi.height*pi.scale))
		if w < 1 || h < 1 || w*h > maxImagePixels {
			d.image = nil
			return errors.New(i18n.T("The message is too long for an image."))
		}
		pi.img = image.NewRGBA(image.Rect(0, 0, w, h))
	}
	px := func(v float64) int { return int(math.Round(v * pi.scale)) }
	b := shot.Bounds()
	src := image.Rect(px(s.X), px(s.Y), px(s.X+s.Width), px(s.Y+s.Height)).Add(b.Min).Intersect(b)
	at := image.Pt(px(s.X), px(s.ToY))
	draw.Draw(pi.img, image.Rectangle{Min: at, Max: at.Add(src.Size())}, shot, src.Min, draw.Src)
	return nil
}

// CopyImage puts the image the slices assembled on the clipboard, as a PNG.
func (d *Desktop) CopyImage() error {
	d.mu.Lock()
	pi := d.image
	d.image = nil
	d.mu.Unlock()
	if pi == nil || pi.img == nil {
		return errors.New("no image was captured")
	}
	if d.plat.CopyImage == nil {
		return errNoCapture()
	}
	var buf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&buf, pi.img); err != nil {
		return err
	}
	return d.plat.CopyImage(buf.Bytes())
}
