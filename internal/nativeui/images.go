package nativeui

import (
	"context"
	"fmt"

	"github.com/egoist/mygo/ui"

	"gotify-desktop/internal/api"
)

// images keeps the bitmaps the window shows: the apps' images, decoded once
// each, and the images of messages, downloaded as they first show.
type images struct {
	u      *UI
	apps   map[string]*ui.Bitmap
	remote map[string]*remoteImage
}

// remoteImage is an image being downloaded, downloaded, or that failed.
type remoteImage struct {
	bmp    *ui.Bitmap
	failed bool
}

// app returns the image of an app, or nil when it has none.
func (im *images) app(serverID int64, a *api.App) *ui.Bitmap {
	if a == nil || a.ImageKey == "" {
		return nil
	}
	key := fmt.Sprintf("%d/%d/%s", serverID, a.ID, a.ImageKey)
	if b, ok := im.apps[key]; ok {
		return b
	}
	if im.apps == nil {
		im.apps = map[string]*ui.Bitmap{}
	}
	var b *ui.Bitmap
	if data := im.u.ctrl.AppImageData(serverID, a.ID); data != nil {
		b, _ = ui.DecodeBitmap(data)
	}
	im.apps[key] = b
	return b
}

// message returns the image a message shows below its body, downloading it
// the first time: nil while it loads, and failed when it could not.
func (im *images) message(m *api.Message) (*ui.Bitmap, bool) {
	key := fmt.Sprintf("msg:%d/%d", m.ServerID, m.ID)
	return im.fetch(key, func(ctx context.Context) ([]byte, error) {
		return im.u.ctrl.MessageImage(ctx, m.ServerID, m.ID)
	})
}

// url returns an image of a Markdown body, as message does.
func (im *images) url(url string) (*ui.Bitmap, bool) {
	return im.fetch("url:"+url, func(ctx context.Context) ([]byte, error) {
		return im.u.ctrl.FetchImage(ctx, url)
	})
}

func (im *images) fetch(key string, get func(ctx context.Context) ([]byte, error)) (*ui.Bitmap, bool) {
	if r, ok := im.remote[key]; ok {
		return r.bmp, r.failed
	}
	if im.remote == nil {
		im.remote = map[string]*remoteImage{}
	}
	r := &remoteImage{}
	im.remote[key] = r
	async(im.u, func(ctx context.Context) (*ui.Bitmap, error) {
		data, err := get(ctx)
		if err != nil || data == nil {
			return nil, err
		}
		return ui.DecodeBitmap(data)
	}, func(b *ui.Bitmap, err error) {
		r.bmp, r.failed = b, err != nil || b == nil
	})
	return nil, false
}

// shownImage shows an image of a message at its own proportions, no wider
// than its column nor taller than maxHeight, which a click opens in the viewer.
func (u *UI) shownImage(c *ui.Context, b *ui.Bitmap, failed bool, maxHeight float32) {
	p := paletteOf(c)
	if failed {
		return
	}
	if b == nil {
		// The room of a placeholder until it loaded.
		ui.Box(c).Size(240, 135).Radius(8).Background(p.neutral).AlignSelf(ui.Start)
		return
	}
	w, h := b.Size()
	if w <= 0 || h <= 0 {
		return
	}
	dw, dh := float32(w), float32(h)
	if dh > maxHeight {
		dw, dh = dw*maxHeight/dh, maxHeight
	}
	img := ui.Image(c, b).Width(dw).MaxWidthPercent(100).AspectRatio(float32(w) / float32(h)).Fit(ui.Contain).
		Radius(8).AlignSelf(ui.Start).Cursor(ui.CursorPointer)
	if img.Clicked() {
		u.viewing, u.viewOpen = b, true
	}
}
