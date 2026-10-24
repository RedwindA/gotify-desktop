package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/egoist/mygo/ui/markdown/parser"

	"gotify-desktop/internal/i18n"
	"gotify-desktop/internal/notify"
	"gotify-desktop/internal/store"
)

// maxImagePixels bounds the image of a message, about 400 MB of RGBA.
const maxImagePixels = 100 << 20

// shotWidth is the width of the image of a message, in DIPs.
const shotWidth = 640

// shotImagesTimeout bounds the downloads of the images a message shows.
const shotImagesTimeout = 5 * time.Second

// ImageRequest asks CopyMessageImage for the image of a message, drawn as
// the page shows: Dark in its dark appearance, Scale device pixels per DIP.
type ImageRequest struct {
	ServerID int64   `json:"serverId"`
	ID       uint    `json:"id"`
	Dark     bool    `json:"dark"`
	Scale    float64 `json:"scale"`
}

func errNoCopyImage() error { return errors.New(i18n.T("Copying as an image is not available.")) }

// CopyMessageImage puts the body of a message on the clipboard as a PNG.
// Chat apps rarely render markdown, tables least of all, but every one
// shows an image. MyGo's native UI draws it without a window, so the
// window does not change while it does.
func (d *Desktop) CopyMessageImage(ctx context.Context, r ImageRequest) error {
	if d.plat.CopyImage == nil {
		return errNoCopyImage()
	}
	msgs, err := d.be.Messages(store.MessageQuery{ServerID: r.ServerID, ID: r.ID, Limit: 1})
	if err != nil {
		return err
	}
	if len(msgs) == 0 {
		return errors.New("the message is gone")
	}
	m := msgs[0]
	shot := messageShot{body: m.Message.Message, markdown: isMarkdown(m.Extras), dark: r.Dark, scale: float32(r.Scale)}
	if d.plat.Appearance != nil {
		shot.accent = d.plat.Appearance().Accent
	}
	// The images download together, those in the body as the page loads
	// them and the message's own image as ImageHandler serves it.
	ctx, cancel := context.WithTimeout(ctx, shotImagesTimeout)
	defer cancel()
	var urls []string
	if shot.markdown {
		urls = bodyImages(shot.body)
	}
	big := notify.BigImageURL(m.Extras)
	if big != "" && shot.markdown && strings.Contains(shot.body, big) {
		big = "" // the body shows it
	}
	shot.images = make(map[string]*ui.Bitmap)
	var mu sync.Mutex
	var wg sync.WaitGroup
	load := func(url string, fetch func() ([]byte, error)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			data, err := fetch()
			if err != nil || !strings.HasPrefix(http.DetectContentType(data), "image/") {
				return
			}
			if bmp, err := ui.DecodeBitmap(data); err == nil {
				mu.Lock()
				shot.images[url] = bmp
				mu.Unlock()
			}
		}()
	}
	for _, u := range urls {
		if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
			load(u, func() ([]byte, error) { return d.be.FetchImage(ctx, u) })
		}
	}
	if big != "" {
		load(big, func() ([]byte, error) { return d.messageImage(ctx, m.ServerID, m.ID) })
	}
	wg.Wait()
	if big != "" && shot.images[big] != nil {
		shot.below = shot.images[big]
	}
	img, err := shot.render()
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&buf, img); err != nil {
		return err
	}
	return d.plat.CopyImage(buf.Bytes())
}

// messageShot is what the image of a message shows and how.
type messageShot struct {
	body     string
	markdown bool
	// images are the images the body shows, by URL, and below the
	// message's own image, which shows under the body.
	images map[string]*ui.Bitmap
	below  *ui.Bitmap
	dark   bool
	accent string
	scale  float32
}

// render draws the message shotWidth wide and as tall as it is.
func (s *messageShot) render() (*image.RGBA, error) {
	tt, err := s.tester()
	if err != nil {
		return nil, err
	}
	return tt.Image(), nil
}

// tester lays the message out shotWidth wide and as tall as it is.
func (s *messageShot) tester() (tt *ui.Tester, err error) {
	// A Tester panics where a view would only log, as on duplicate keys.
	defer func() {
		if r := recover(); r != nil {
			tt, err = nil, fmt.Errorf("cannot draw the message: %v", r)
		}
	}()
	scale := s.scale
	if !(scale >= 1 && scale <= 4) {
		scale = 1
	}
	var height float32
	md := &markdown.Options{
		WrapCode:  true,
		CodeBlock: codeBlock,
		Block:     s.imageParagraph,
	}
	view := func(c *ui.Context) {
		col := ui.Column(c).Width(shotWidth).Padding(24).Gap(12).Background(c.Theme().Background)
		col.Draw(func(_ *ui.Painter, r ui.Rect) { height = r.H })
		col.Children(func() {
			if s.markdown {
				markdown.View(c, s.body, md)
			} else if s.body != "" {
				ui.Text(c, s.body)
			}
			if s.below != nil {
				picture(c, s.below)
			}
		})
	}
	tt = ui.NewTester(view, shotWidth, 1)
	tt.SetDark(s.dark)
	if s.accent != "" {
		tt.SetPreferences(ui.Preferences{Accent: ui.Hex(s.accent), ReduceMotion: true})
	} else {
		tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	}
	tt.Frame()
	h := int(math.Ceil(float64(height)))
	if h < 1 {
		return nil, errors.New("the image is empty")
	}
	if px := float64(shotWidth) * float64(h) * float64(scale*scale); px > maxImagePixels {
		return nil, errors.New(i18n.T("The message is too long for an image."))
	}
	tt.SetSize(shotWidth, h)
	tt.SetScale(scale)
	tt.Frame()
	return tt, nil
}

// imageParagraph draws a paragraph of images alone with the images
// downloaded before, as markdown.View would once it loaded them, and
// their descriptions where they did not load.
func (s *messageShot) imageParagraph(c *ui.Context, d *parser.Document, i int) bool {
	if d.Block(i).Kind != parser.Paragraph || !imagesOnly(d.Inlines(i)) {
		return false
	}
	in := d.Inlines(i)
	ui.Row(c).Wrap().Gap(8).AlignSelf(ui.Stretch).Children(func() {
		for k := 0; k < len(in); k = in[k].Next {
			if in[k].Kind != parser.Image {
				continue
			}
			if bmp := s.images[in[k].Dest]; bmp != nil {
				picture(c, bmp)
			} else if alt := parser.PlainText(in[k+1 : in[k].Next]); alt != "" {
				ui.Text(c, "["+alt+"]").TextColor(c.Theme().TextMuted)
			}
		}
	})
	return true
}

// picture shows an image as large as it is, at most as wide as the message.
func picture(c *ui.Context, bmp *ui.Bitmap) {
	w, h := bmp.Size()
	img := ui.Image(c, bmp).Radius(4).MaxWidthPercent(100).AlignSelf(ui.Start)
	if w > 0 && h > 0 {
		img.AspectRatio(float32(w) / float32(h))
	}
}

// codeBlock draws a code block without its copy button, which the image
// could not press.
func codeBlock(c *ui.Context, b *markdown.CodeBlockView) {
	th := markdown.DefaultTheme(c.Theme())
	box := ui.Column(c).Background(th.CodeBlockBackground).Border(1, th.CodeBlockBorder).Radius(th.CodeBlockRadius).AlignSelf(ui.Stretch).Clip()
	box.Children(func() {
		if b.Language != "" {
			ui.Text(c, b.Language).FontSize(0.8*th.FontSize).TextColor(th.Muted).Padding(0.35*th.FontSize, 0, 0, th.CodeBlockPadding*th.FontSize)
		}
		b.Body(c)
	})
}

// bodyImages returns the URLs of the images in paragraphs of images alone,
// which the image of a message shows.
func bodyImages(src string) []string {
	d := parser.Parse(src)
	var urls []string
	seen := map[string]bool{}
	for i := range d.Len() {
		if d.Block(i).Kind != parser.Paragraph {
			continue
		}
		in := d.Inlines(i)
		if !imagesOnly(in) {
			continue
		}
		for k := 0; k < len(in); k = in[k].Next {
			if u := in[k].Dest; in[k].Kind == parser.Image && u != "" && !seen[u] {
				seen[u] = true
				urls = append(urls, u)
			}
		}
	}
	return urls
}

// imagesOnly reports whether the inlines are images and the spaces between
// them alone, as markdown.View shows as images; other images show as their
// descriptions.
func imagesOnly(in []parser.Inline) bool {
	n := 0
	for k := 0; k < len(in); k = in[k].Next {
		switch in[k].Kind {
		case parser.Image:
			n++
		case parser.SoftBreak, parser.LineBreak:
		case parser.Text:
			if strings.TrimSpace(in[k].Text) != "" {
				return false
			}
		default:
			return false
		}
	}
	return n > 0
}
