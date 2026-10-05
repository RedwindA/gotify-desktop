package nativeui

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"

	"gotify-desktop/internal/mdtext"
)

// markdown parses message bodies as GitHub-flavored Markdown, as the page's
// Markdown component does.
var markdown = goldmark.New(goldmark.WithExtensions(extension.GFM))

// mdDoc is a parsed Markdown body.
type mdDoc struct {
	root ast.Node
	src  []byte
}

func parseMarkdown(body string) *mdDoc {
	src := []byte(body)
	return &mdDoc{root: markdown.Parser().Parse(text.NewReader(src)), src: src}
}

// span is a run of inline text in one style.
type span struct {
	text                       string
	bold, italic, code, strike bool
	href                       string
}

// mdView shows a Markdown document, in the compact density of the page's
// Markdown, its headings from the fourth level.
type mdView struct {
	u   *UI
	c   *ui.Context
	p   *palette
	doc *mdDoc
}

func (u *UI) markdown(c *ui.Context, doc *mdDoc) {
	v := &mdView{u: u, c: c, p: paletteOf(c), doc: doc}
	ui.Column(c).Gap(8).MinWidth(0).Children(func() { v.blocks(doc.root) })
}

func (v *mdView) blocks(n ast.Node) {
	for b := n.FirstChild(); b != nil; b = b.NextSibling() {
		v.block(b)
	}
}

func (v *mdView) block(n ast.Node) {
	c, p := v.c, v.p
	switch n := n.(type) {
	case *ast.Paragraph, *ast.TextBlock:
		v.paragraph(n, nil)
	case *ast.Heading:
		size := float32(fontSm)
		if n.Level == 1 {
			size = fontBase
		}
		v.paragraph(n, func(e *ui.Element) { e.FontSize(size).FontWeight(600) })
	case *ast.ThematicBreak:
		ui.Divider(c).Background(p.border)
	case *ast.FencedCodeBlock, *ast.CodeBlock:
		v.code(strings.TrimSuffix(v.lines(n), "\n"))
	case *ast.Blockquote:
		ui.Column(c).Gap(8).PaddingX(16).BorderWidth(0, 0, 0, 2).BorderColor(p.strong).TextColor(p.muted).Children(func() {
			v.blocks(n)
		})
	case *ast.List:
		v.list(n)
	case *east.Table:
		v.table(n)
	case *ast.HTMLBlock:
		ui.Text(c, strings.TrimSuffix(v.lines(n), "\n")).TextColor(p.muted)
	default:
		v.blocks(n)
	}
}

func (v *mdView) lines(n ast.Node) string {
	var sb strings.Builder
	lines := n.Lines()
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		sb.Write(seg.Value(v.doc.src))
	}
	if h, ok := n.(*ast.HTMLBlock); ok && h.HasClosure() {
		sb.Write(h.ClosureLine.Value(v.doc.src))
	}
	return sb.String()
}

// paragraph shows the inline content of a block in one paragraph, then the
// images it holds below it.
func (v *mdView) paragraph(n ast.Node, style func(*ui.Element)) {
	var spans []span
	var images []string
	v.inline(n, span{}, &spans, &images)
	if len(spans) > 0 {
		v.spans(spans, style)
	}
	for _, url := range images {
		b, failed := v.u.images.url(url)
		v.u.shownImage(v.c, b, failed, 360)
	}
}

func (v *mdView) spans(spans []span, style func(*ui.Element)) {
	c, p := v.c, v.p
	rt := ui.RichText(c).Selectable()
	if style != nil {
		style(rt)
	}
	rt.Children(func() {
		for _, s := range spans {
			var e *ui.Element
			if s.href != "" {
				e = ui.Link(c, s.text, "").TextColor(p.text).Underline()
				if e.Clicked() {
					v.u.openURL(s.href)
				}
			} else {
				e = ui.Text(c, s.text)
			}
			if s.bold {
				e.FontWeight(600)
			}
			if s.italic {
				e.Italic()
			}
			if s.strike {
				e.Strikethrough()
			}
			if s.code {
				e.Font("monospace").Background(p.code)
			}
		}
	})
}

func (v *mdView) inline(n ast.Node, st span, out *[]span, images *[]string) {
	add := func(s string) {
		if s == "" {
			return
		}
		if k := len(*out) - 1; k >= 0 {
			last := &(*out)[k]
			if last.bold == st.bold && last.italic == st.italic && last.code == st.code && last.strike == st.strike && last.href == st.href {
				last.text += s
				return
			}
		}
		sp := st
		sp.text = s
		*out = append(*out, sp)
	}
	for ch := n.FirstChild(); ch != nil; ch = ch.NextSibling() {
		switch ch := ch.(type) {
		case *ast.Text:
			if st.code {
				add(string(ch.Segment.Value(v.doc.src)))
			} else {
				add(mdtext.Decode(ch.Segment.Value(v.doc.src)))
			}
			switch {
			case ch.HardLineBreak():
				add("\n")
			case ch.SoftLineBreak():
				add(" ")
			}
		case *ast.String:
			add(string(ch.Value))
		case *ast.CodeSpan:
			s := st
			s.code = true
			v.inline(ch, s, out, images)
		case *ast.Emphasis:
			s := st
			if ch.Level >= 2 {
				s.bold = true
			} else {
				s.italic = true
			}
			v.inline(ch, s, out, images)
		case *east.Strikethrough:
			s := st
			s.strike = true
			v.inline(ch, s, out, images)
		case *ast.Link:
			s := st
			s.href = string(ch.Destination)
			v.inline(ch, s, out, images)
		case *ast.AutoLink:
			s := st
			s.href = string(ch.URL(v.doc.src))
			if ch.AutoLinkType == ast.AutoLinkEmail && !strings.HasPrefix(strings.ToLower(s.href), "mailto:") {
				s.href = "mailto:" + s.href
			}
			lbl := string(ch.Label(v.doc.src))
			ss := st
			st = s
			add(lbl)
			st = ss
		case *ast.Image:
			*images = append(*images, string(ch.Destination))
		case *ast.RawHTML:
			for i := 0; i < ch.Segments.Len(); i++ {
				seg := ch.Segments.At(i)
				add(string(seg.Value(v.doc.src)))
			}
		case *east.TaskCheckBox:
			if ch.IsChecked {
				add("☑ ")
			} else {
				add("☐ ")
			}
		default:
			v.inline(ch, st, out, images)
		}
	}
}

func (v *mdView) list(n *ast.List) {
	c, p := v.c, v.p
	ui.Column(c).Gap(8).Children(func() {
		i := n.Start
		for item := n.FirstChild(); item != nil; item = item.NextSibling() {
			ui.Row(c).AlignItems(ui.Start).Children(func() {
				ui.Row(c).Width(24).Height(20).Justify(ui.Center).Shrink(0).Children(func() {
					if n.IsOrdered() {
						ui.Text(c, strconv.Itoa(i)+".").FontFeatures("tnum")
					} else {
						ui.Box(c).Size(5, 5).Radius(3).Background(p.text)
					}
				})
				ui.Column(c).Grow(1).MinWidth(0).Gap(8).Children(func() { v.blocks(item) })
			})
			i++
		}
	})
}

func (v *mdView) table(n *east.Table) {
	c, p := v.c, v.p
	cols := len(n.Alignments)
	if cols == 0 {
		return
	}
	tracks := make([]ui.Track, cols)
	for i := range tracks {
		tracks[i] = ui.Fr(1)
	}
	ui.Box(c).Children(func() {
		ui.Grid(c).ColumnTracks(tracks...).FillWidth().Children(func() {
			for row := n.FirstChild(); row != nil; row = row.NextSibling() {
				_, header := row.(*east.TableHeader)
				col := 0
				for cell := row.FirstChild(); cell != nil && col < cols; cell = cell.NextSibling() {
					align := n.Alignments[col]
					ui.Column(c).Padding(8).BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
						v.paragraph(cell, func(e *ui.Element) {
							if header {
								e.FontWeight(600).TextColor(p.muted)
							}
							switch align {
							case east.AlignRight:
								e.TextAlign(ui.End)
							case east.AlignCenter:
								e.TextAlign(ui.Center)
							}
						})
					})
					col++
				}
				for ; col < cols; col++ {
					ui.Box(c).BorderWidth(0, 0, 1, 0).BorderColor(p.border)
				}
			}
		})
	})
}

// code shows a block of code, with a button that copies it.
func (v *mdView) code(s string) {
	c, p := v.c, v.p
	ui.Box(c).Radius(radiusContainer).Border(1, p.border).Background(p.codeBlock).Children(func() {
		ui.ScrollHorizontal(c).Padding(14, 48, 14, 16).Children(func() {
			ui.Text(c, s).Font("monospace").NoWrap().Selectable()
		})
		b := button(c, v.u.text().copyText, buttonOpts{kind: ghost, small: true, icon: iconCopy, iconOnly: true})
		b.Attach(ui.AnchorTopRight, ui.AnchorTopRight).Top(8).Right(8).TextColor(p.muted)
		if b.Clicked() {
			c.WriteClipboard(s)
			v.u.toast(v.u.text().copied)
		}
	})
}

// plainBody shows a plain-text body with its URLs as links, keeping its newlines.
func (u *UI) plainBody(c *ui.Context, body string) {
	p := paletteOf(c)
	ui.RichText(c).Selectable().Children(func() {
		last := 0
		for _, m := range urlPattern.FindAllStringIndex(body, -1) {
			if m[0] > last {
				ui.Text(c, body[last:m[0]])
			}
			href := body[m[0]:m[1]]
			if ui.Link(c, href, "").TextColor(p.accent).Underline().Clicked() {
				u.openURL(href)
			}
			last = m[1]
		}
		if last < len(body) {
			ui.Text(c, body[last:])
		}
	})
}

var urlPattern = regexp.MustCompile(`\bhttps?://[^\s<>"')\]]+[^\s<>"')\].,;:!?]`)

// openURL opens an http(s) or mailto URL in the default app.
func (u *UI) openURL(url string) {
	go u.svc.OpenURL(url)
}
