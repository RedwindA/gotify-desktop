// Package markdown parses message bodies once and renders them as native UI.
package markdown

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

type blockKind int

const (
	kindParagraph blockKind = iota
	kindHeading
	kindList
	kindQuote
	kindCode
	kindRule
	kindImage
	kindTable
)

type block struct {
	kind     blockKind
	inlines  []inline
	level    int
	ordered  bool
	start    int
	items    [][]block
	children []block
	text     string
	url, alt string
	header   [][]inline
	rows     [][][]inline
}

type inline struct {
	text                       string
	bold, italic, strike, code bool
	link                       string
	check                      int // 1 unchecked, 2 checked task box
}

var parser = goldmark.New(goldmark.WithExtensions(extension.GFM)).Parser()

func parseMarkdown(src string) []block {
	b := []byte(src)
	doc := parser.Parse(text.NewReader(b))
	return blocks(doc, b)
}

func blocks(parent ast.Node, src []byte) []block {
	var out []block
	for n := parent.FirstChild(); n != nil; n = n.NextSibling() {
		switch n := n.(type) {
		case *ast.Paragraph, *ast.TextBlock:
			out = append(out, paragraph(n, src, kindParagraph, 0)...)
		case *ast.Heading:
			out = append(out, paragraph(n, src, kindHeading, n.Level)...)
		case *ast.List:
			l := block{kind: kindList, ordered: n.IsOrdered(), start: n.Start}
			for it := n.FirstChild(); it != nil; it = it.NextSibling() {
				l.items = append(l.items, blocks(it, src))
			}
			out = append(out, l)
		case *ast.Blockquote:
			out = append(out, block{kind: kindQuote, children: blocks(n, src)})
		case *ast.FencedCodeBlock, *ast.CodeBlock:
			var sb strings.Builder
			lines := n.Lines()
			for i := 0; i < lines.Len(); i++ {
				seg := lines.At(i)
				sb.Write(seg.Value(src))
			}
			out = append(out, block{kind: kindCode, text: strings.TrimRight(sb.String(), "\n")})
		case *ast.ThematicBreak:
			out = append(out, block{kind: kindRule})
		case *east.Table:
			out = append(out, table(n, src))
		case *ast.HTMLBlock:
		default:
			if n.HasChildren() {
				out = append(out, blocks(n, src)...)
			}
		}
	}
	return out
}

type style struct {
	bold, italic, strike, code bool
	link                       string
}

// paragraph flattens the inline children of n; images split it into blocks of their own.
func paragraph(n ast.Node, src []byte, kind blockKind, level int) []block {
	var out []block
	var cur []inline
	flush := func() {
		if len(cur) > 0 {
			out = append(out, block{kind: kind, level: level, inlines: cur})
			cur = nil
		}
	}
	var walk func(n ast.Node, st style)
	walk = func(n ast.Node, st style) {
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			switch c := c.(type) {
			case *ast.Text:
				cur = append(cur, inline{text: string(c.Segment.Value(src)), bold: st.bold, italic: st.italic, strike: st.strike, code: st.code, link: st.link})
				if c.HardLineBreak() || c.SoftLineBreak() {
					cur = append(cur, inline{text: "\n"})
				}
			case *ast.String:
				cur = append(cur, inline{text: string(c.Value), bold: st.bold, italic: st.italic, strike: st.strike, code: st.code, link: st.link})
			case *ast.CodeSpan:
				var sb strings.Builder
				for t := c.FirstChild(); t != nil; t = t.NextSibling() {
					if t, ok := t.(*ast.Text); ok {
						sb.Write(t.Segment.Value(src))
					}
				}
				cur = append(cur, inline{text: sb.String(), code: true, bold: st.bold, italic: st.italic, strike: st.strike, link: st.link})
			case *ast.Emphasis:
				s := st
				if c.Level >= 2 {
					s.bold = true
				} else {
					s.italic = true
				}
				walk(c, s)
			case *east.Strikethrough:
				s := st
				s.strike = true
				walk(c, s)
			case *ast.Link:
				s := st
				s.link = string(c.Destination)
				walk(c, s)
			case *ast.AutoLink:
				u := string(c.URL(src))
				if c.AutoLinkType == ast.AutoLinkEmail {
					u = "mailto:" + u
				}
				s := st
				s.link = u
				cur = append(cur, inline{text: string(c.Label(src)), bold: s.bold, italic: s.italic, link: u})
			case *ast.Image:
				flush()
				out = append(out, block{kind: kindImage, url: string(c.Destination), alt: plainText(c, src)})
			case *east.TaskCheckBox:
				mark := 1
				if c.IsChecked {
					mark = 2
				}
				cur = append(cur, inline{check: mark})
			case *ast.RawHTML:
			default:
				walk(c, st)
			}
		}
	}
	walk(n, style{})
	flush()
	return out
}

func plainText(n ast.Node, src []byte) string {
	var sb strings.Builder
	ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if t, ok := c.(*ast.Text); ok && entering {
			sb.Write(t.Segment.Value(src))
		}
		return ast.WalkContinue, nil
	})
	return sb.String()
}

func table(n *east.Table, src []byte) block {
	t := block{kind: kindTable}
	cells := func(row ast.Node) [][]inline {
		var r [][]inline
		for c := row.FirstChild(); c != nil; c = c.NextSibling() {
			var in []inline
			for _, b := range paragraph(c, src, kindParagraph, 0) {
				in = append(in, b.inlines...)
			}
			r = append(r, in)
		}
		return r
	}
	for r := n.FirstChild(); r != nil; r = r.NextSibling() {
		if _, ok := r.(*east.TableHeader); ok {
			t.header = cells(r)
		} else {
			t.rows = append(t.rows, cells(r))
		}
	}
	return t
}

var urlPattern = regexp.MustCompile(`https?://[^\s<>"']+[^\s<>"'.,;:!?)\]]`)

// parsePlain keeps the text as it is and links the URLs in it.
func parsePlain(src string) []block {
	var in []inline
	last := 0
	for _, loc := range urlPattern.FindAllStringIndex(src, -1) {
		if loc[0] > last {
			in = append(in, inline{text: src[last:loc[0]]})
		}
		in = append(in, inline{text: src[loc[0]:loc[1]], link: src[loc[0]:loc[1]]})
		last = loc[1]
	}
	if last < len(src) {
		in = append(in, inline{text: src[last:]})
	}
	if len(in) == 0 {
		return nil
	}
	return []block{{kind: kindParagraph, inlines: in}}
}

// safeLink reports whether a link may be opened from a message.
func safeLink(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return u.Host != ""
	case "mailto":
		return true
	}
	return false
}
