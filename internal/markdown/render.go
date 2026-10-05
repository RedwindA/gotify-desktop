package markdown

import (
	"fmt"
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"
)

const maxDocs = 512

type docEntry struct {
	blocks []block
}

// Renderer renders messages, parsing each only once.
type Renderer struct {
	images *ImageCache

	mu    sync.Mutex
	docs  map[string]*docEntry
	order []string
}

func New(images *ImageCache) *Renderer {
	return &Renderer{images: images, docs: map[string]*docEntry{}}
}

func (r *Renderer) doc(key, src string, md bool) []block {
	key = fmt.Sprintf("%s/%d/%t", key, len(src), md)
	r.mu.Lock()
	defer r.mu.Unlock()
	if d, ok := r.docs[key]; ok {
		return d.blocks
	}
	var bl []block
	if md {
		bl = parseMarkdown(src)
	} else {
		bl = parsePlain(src)
	}
	r.docs[key] = &docEntry{bl}
	r.order = append(r.order, key)
	if len(r.order) > maxDocs {
		delete(r.docs, r.order[0])
		r.order = r.order[1:]
	}
	return bl
}

// Render builds the body of a message in a column. key identifies the message,
// as "server/id"; the parsed form is cached under it and the text's length.
func (r *Renderer) Render(c *ui.Context, key, src string, markdown bool) {
	bl := r.doc(key, src, markdown)
	ui.Column(c).Gap(6).Children(func() { r.blocks(c, bl) })
}

func (r *Renderer) blocks(c *ui.Context, bl []block) {
	t := c.Theme()
	for _, b := range bl {
		switch b.kind {
		case kindParagraph:
			inlineText(c, b.inlines, 0)
		case kindHeading:
			size := []float32{0, 1.5, 1.35, 1.2, 1.1, 1.05, 1.0}[min(max(b.level, 1), 6)]
			inlineText(c, b.inlines, t.FontSize*size).Bold()
		case kindList:
			ui.Column(c).Gap(2).Children(func() {
				for i, item := range b.items {
					marker := "•"
					if b.ordered {
						marker = fmt.Sprintf("%d.", b.start+i)
					}
					ui.Row(c).Gap(6).AlignItems(ui.Start).Children(func() {
						ui.Text(c, marker).MinWidth(18).TextAlign(ui.End).TextColor(t.TextMuted)
						ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() { r.blocks(c, item) })
					})
				}
			})
		case kindQuote:
			ui.Row(c).Gap(8).AlignItems(ui.Stretch).Children(func() {
				ui.Box(c).Width(3).Background(t.Border).Radius(2)
				ui.Column(c).Grow(1).Shrink(1).Gap(4).TextColor(t.TextMuted).Children(func() { r.blocks(c, b.children) })
			})
		case kindCode:
			ui.ScrollHorizontal(c).Background(codeBackground).Radius(6).Padding(8, 10).Children(func() {
				ui.Text(c, b.text).Font("monospace").FontSize(t.FontSize * 0.92).NoWrap().Selectable()
			})
		case kindRule:
			ui.Divider(c)
		case kindImage:
			r.image(c, b)
		case kindTable:
			r.table(c, b)
		}
	}
}

var codeBackground = ui.RGBA(128, 128, 128, 0.16)

func inlineText(c *ui.Context, in []inline, size float32) *ui.Element {
	t := c.Theme()
	el := ui.RichText(c).Selectable()
	if size > 0 {
		el.FontSize(size)
	}
	return el.Children(func() {
		for _, s := range in {
			if s.check != 0 {
				box := "☐ "
				if s.check == 2 {
					box = "☑ "
				}
				ui.Text(c, box)
				continue
			}
			var e *ui.Element
			if s.link != "" && safeLink(s.link) {
				e = ui.Link(c, s.text, s.link)
			} else {
				e = ui.Text(c, s.text)
			}
			if s.bold {
				e.Bold()
			}
			if s.italic {
				e.Italic()
			}
			if s.strike {
				e.Strikethrough()
			}
			if s.code {
				e.Font("monospace").TextBackground(codeBackground)
			}
			_ = t
		}
	})
}

func (r *Renderer) image(c *ui.Context, b block) {
	t := c.Theme()
	if !safeImageURL(b.url) {
		ui.Text(c, b.alt).TextColor(t.TextMuted)
		return
	}
	bmp, state := r.images.Get(b.url)
	switch state {
	case imageReady:
		w, h := bmp.Size()
		ui.Image(c, bmp).MaxWidth(float32(min(w, 480))).AspectRatio(float32(w) / float32(max(h, 1))).Radius(6).Tooltip(b.alt)
	case imageLoading:
		ui.Row(c).Gap(8).Height(48).Children(func() {
			ui.Spinner(c).Label("Loading image")
			ui.Text(c, "Loading image…").TextColor(t.TextMuted)
		})
	default:
		ui.Link(c, orDefault(b.alt, "Image"), b.url)
	}
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

func safeImageURL(u string) bool {
	return strings.HasPrefix(strings.ToLower(u), "http://") || strings.HasPrefix(strings.ToLower(u), "https://")
}

func (r *Renderer) table(c *ui.Context, b block) {
	t := c.Theme()
	cols := len(b.header)
	for _, row := range b.rows {
		cols = max(cols, len(row))
	}
	if cols == 0 {
		return
	}
	tracks := make([]ui.Track, cols)
	for i := range tracks {
		tracks[i] = ui.FitContent()
	}
	ui.ScrollHorizontal(c).Children(func() {
		ui.Grid(c).ColumnTracks(tracks...).GapX(16).GapY(4).Children(func() {
			cell := func(in []inline, bold bool) {
				e := inlineText(c, in, 0)
				if bold {
					e.Bold()
				}
			}
			for i := 0; i < cols; i++ {
				var in []inline
				if i < len(b.header) {
					in = b.header[i]
				}
				cell(in, true)
			}
			for _, row := range b.rows {
				for i := 0; i < cols; i++ {
					var in []inline
					if i < len(row) {
						in = row[i]
					}
					cell(in, false)
				}
			}
		})
	})
	_ = t
}
