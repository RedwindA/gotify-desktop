package nativeui

import (
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
)

// The page's components (Astryx's), drawn with the toolkit's widgets without
// a look, so that the window looks as the page does.

type buttonKind uint8

const (
	secondary buttonKind = iota
	primary
	ghost
)

// buttonOpts sets a button up: a small one, with an icon, showing only the
// icon, loading or disabled.
type buttonOpts struct {
	kind     buttonKind
	small    bool
	icon     *ui.SVG
	iconOnly bool
	loading  bool
	disabled bool
}

// button is Astryx's Button: a pill of the theme's radius, 32 DIPs high (28
// small), in the neutral face, the accent, or no face until hovered.
func button(c *ui.Context, label string, o buttonOpts) *ui.Element {
	p := paletteOf(c)
	h, px, iconSize := float32(controlHeight), float32(12), float32(16)
	if o.small {
		h, px, iconSize = 28, 10, 14
	}
	b := ui.ButtonBase(c).Height(h).Gap(6).Radius(radiusElement).Justify(ui.Center).Shrink(0)
	if o.iconOnly {
		b.Width(h).Label(label).Tooltip(label)
	} else {
		b.PaddingX(px)
	}
	disabled := o.disabled || o.loading
	b.Disabled(disabled)
	face, fg := p.neutral, p.text
	switch o.kind {
	case primary:
		face, fg = p.accent, p.onAccent
	case ghost:
		face = ui.Transparent
	}
	switch {
	case disabled:
	case b.Pressed():
		face = p.pressed.Over(face.Over(p.surface))
	case b.Hovered():
		face = p.hover.Over(face.Over(p.surface))
	}
	if o.kind == primary && !disabled && (b.Hovered() || b.Pressed()) {
		face = p.accent.Mix(p.surface, 0.15)
	}
	b.Background(face).TextColor(fg).FontSize(fontBase).FontWeight(500)
	if disabled {
		b.Opacity(0.5)
	}
	return b.Children(func() {
		switch {
		case o.loading:
			ui.Spinner(c).Size(iconSize, iconSize)
		case o.icon != nil:
			ui.Icon(c, o.icon).Size(iconSize, iconSize)
		}
		if !o.iconOnly {
			ui.Text(c, label).SingleLine()
		}
	})
}

// moreButton is Astryx's MoreMenu: a small ghost button showing "…", which
// opens a menu. It has no tooltip, which would show beside the open menu.
func moreButton(c *ui.Context, label string, build func(m *ui.Menu)) *ui.Element {
	p := paletteOf(c)
	b := ui.ButtonBase(c).Size(24, 24).Radius(radiusInner).Justify(ui.Center).Shrink(0).Label(label)
	if b.Hovered() || b.Pressed() {
		b.Background(p.hover)
	}
	return b.TextColor(p.text).Children(func() { ui.Icon(c, iconEllipsis).Size(16, 16) }).Menu(build)
}

// badge is Astryx's Badge: a count, or a short text, in a pill.
func badge(c *ui.Context, text string, bg, fg ui.Color) *ui.Element {
	return ui.Row(c).Height(20).MinWidth(20).PaddingX(6).Radius(999).Justify(ui.Center).Shrink(0).
		Background(bg).Children(func() {
		ui.Text(c, text).FontSize(fontSm).FontWeight(500).TextColor(fg).SingleLine().FontFeatures("tnum")
	})
}

// countBadge is the unread count of a sidebar item, a muted number at its
// end as Notes shows its counts, or nothing at zero.
func countBadge(c *ui.Context, n int, selected bool) {
	if n <= 0 {
		return
	}
	p := paletteOf(c)
	text := "999+"
	if n <= 999 {
		text = strconv.Itoa(n)
	}
	fg := p.muted
	if selected {
		fg = p.text
	}
	ui.Text(c, text).TextColor(fg).SingleLine().FontFeatures("tnum").Shrink(0)
}

// toolbarItem is the size of the buttons on glass in a toolbar, as macOS 26's.
const toolbarItem = 36

// toolbarHeight is the height of the toolbars: the band of the window
// controls, at least.
func toolbarHeight(c *ui.Context) float32 { return max(c.TitleBar().Height, 52) }

// glassButton is a toolbar button of macOS 26: an icon on a disc of Liquid
// Glass, tinted with the accent while on.
func glassButton(c *ui.Context, label string, icon *ui.SVG, on bool) *ui.Element {
	p := paletteOf(c)
	g, fg := glass.Glass{Interactive: true}, p.text
	if on {
		g.Tint, fg = p.accent, p.onAccent
	}
	return ui.ButtonBase(c).Size(toolbarItem, toolbarItem).Radius(toolbarItem / 2).Justify(ui.Center).Shrink(0).
		Label(label).Tooltip(label).Material(g).TextColor(fg).Children(func() {
		ui.Icon(c, icon).Size(18, 18)
	})
}

// glassGroup is a capsule of Liquid Glass holding toolbar buttons, as
// macOS 26 groups related ones.
func glassGroup(c *ui.Context, buttons func()) *ui.Element {
	return ui.Row(c).Height(toolbarItem).Radius(toolbarItem/2).Padding(0, 2).Material(glass.Glass{}).Shrink(0).Children(buttons)
}

// groupButton is a button in a glassGroup: an icon, on a disc that shows
// as it is pointed at, pressed or on.
func groupButton(c *ui.Context, label string, icon *ui.SVG, on bool) *ui.Element {
	p := paletteOf(c)
	b := ui.ButtonBase(c).Size(toolbarItem-4, toolbarItem-4).Radius((toolbarItem - 4) / 2).Justify(ui.Center).Shrink(0).Label(label).Tooltip(label)
	switch {
	case on:
		b.Background(p.gray)
	case b.Pressed():
		b.Background(p.pressed)
	case b.Hovered():
		b.Background(p.hover)
	}
	return b.TextColor(p.text).Children(func() { ui.Icon(c, icon).Size(18, 18) })
}

// glassSearch is a search field on a capsule of Liquid Glass, as the
// toolbars of macOS 26 have, outlined while it has the focus.
func glassSearch(c *ui.Context, value *string, label, placeholder string, width float32) *ui.Element {
	p := paletteOf(c)
	var f *ui.Element
	pill := ui.Row(c).Height(toolbarItem).Width(width).Radius(toolbarItem / 2).Material(glass.Glass{}).Shrink(0)
	if pill.FocusWithin() {
		pill.Border(1, p.accent)
	}
	pill.Children(func() {
		f = ui.SearchField(c, value).Label(label).Placeholder(placeholder).Grow(1).MinWidth(0).
			Height(toolbarItem).Radius(toolbarItem/2).Background(ui.Transparent).Border(0, ui.Transparent).FocusRing(false)
	})
	return f
}

// dot is Astryx's StatusDot: a disc of a color, which pulses while
// something goes on.
func dot(c *ui.Context, color ui.Color, size float32, pulsing bool) *ui.Element {
	d := ui.Box(c).Size(size, size).Radius(size / 2).Background(color).Shrink(0)
	if pulsing {
		d.Opacity(0.45 + 0.55*d.Loop("pulse", 1200*time.Millisecond, ui.Bounce(ui.EaseInOut)))
	}
	return d
}

// avatar is Astryx's Avatar of shape "rounded": an image, or the initials of
// the name on the neutral face.
func avatar(c *ui.Context, name string, img *ui.Bitmap, size float32) *ui.Element {
	p := paletteOf(c)
	radius := size / 4
	if img != nil {
		return ui.Image(c, img).Size(size, size).Fit(ui.Cover).Radius(radius).Shrink(0).Label(name)
	}
	return ui.Box(c).Size(size, size).Radius(radius).Center().Background(p.neutral).Shrink(0).Label(name).Children(func() {
		ui.Text(c, initials(name)).FontSize(max(7, size*0.4)).FontWeight(500).TextColor(p.text).SingleLine()
	})
}

// initials returns the first letters of the first two words of a name, as
// Astryx's Avatar shows them.
func initials(name string) string {
	var out []rune
	for w := range strings.FieldsFuncSeq(name, func(r rune) bool { return unicode.IsSpace(r) || r == '-' || r == '_' }) {
		r, _ := utf8.DecodeRuneInString(w)
		out = append(out, unicode.ToUpper(r))
		if len(out) == 2 {
			break
		}
	}
	if len(out) == 0 {
		return "?"
	}
	return string(out)
}

type bannerStatus uint8

const (
	bannerWarning bannerStatus = iota
	bannerError
)

// banner is Astryx's Banner: an icon, a title and a description on the
// color of a status, with an action at the end.
func banner(c *ui.Context, status bannerStatus, title, description string, end func()) *ui.Element {
	p := paletteOf(c)
	bg, fg, icon := p.warningBg, p.warningText, iconTriangle
	if status == bannerError {
		bg, fg, icon = p.errorBg, p.errorText, iconCircleAlert
	}
	return ui.Row(c).Padding(12, 16).Gap(8).Radius(radiusContainer).Background(bg).TextColor(fg).AlignItems(ui.Start).Children(func() {
		ui.Icon(c, icon).Size(20, 20).Margin(1, 0, 0, 0)
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, title).FontWeight(600)
			if description != "" {
				ui.Text(c, description).FontSize(fontSm)
			}
		})
		if end != nil {
			ui.Row(c).AlignSelf(ui.Center).Children(end)
		}
	})
}

// emptyState is Astryx's EmptyState: an icon, a title and a description in
// the middle, and actions below.
func emptyState(c *ui.Context, icon *ui.SVG, iconSize float32, title, description string, actions func()) *ui.Element {
	p := paletteOf(c)
	return ui.Column(c).AlignItems(ui.Center).Gap(8).Padding(32, 16).MaxWidth(440).AlignSelf(ui.Center).Children(func() {
		ui.Icon(c, icon).Size(iconSize, iconSize).TextColor(p.muted).Margin(0, 0, 8, 0)
		ui.Text(c, title).FontSize(fontLg).FontWeight(600).TextAlign(ui.Center)
		ui.Text(c, description).TextColor(p.muted).TextAlign(ui.Center)
		if actions != nil {
			ui.Row(c).Gap(8).Margin(8, 0, 0, 0).Children(actions)
		}
	})
}

// dialog is Astryx's Dialog over the dimmed window: a title, a subtitle and
// a button closing it, the content, and a footer of buttons at the end.
func dialog(c *ui.Context, open *bool, width float32, title, subtitle string, content, footer func()) {
	p := paletteOf(c)
	ui.DialogBase(c, open, func(backdrop, panel *ui.Element) {
		backdrop.Background(p.overlay)
		panel.Width(width).MaxWidthPercent(92).MaxHeightPercent(90).Radius(radiusContainer).
			Background(p.popover).Border(1, p.border).Shadow(0, 8, 24, 0, p.shadow).Clip()
		ui.Row(c).Padding(20, 20, 4).Gap(12).AlignItems(ui.Start).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
				ui.Text(c, title).FontSize(fontLg).FontWeight(600)
				if subtitle != "" {
					ui.Text(c, subtitle).FontSize(fontSm).TextColor(p.muted)
				}
			})
			if button(c, "Close", buttonOpts{kind: ghost, small: true, icon: iconX, iconOnly: true}).Clicked() {
				*open = false
			}
		})
		ui.Scroll(c).Shrink(1).Padding(16, 20, 20).Gap(16).Children(content)
		if footer != nil {
			ui.Row(c).Padding(0, 20, 20).Gap(8).Justify(ui.End).Children(footer)
		}
	})
}

// sectionHeading is Astryx's Heading of level 2.
func sectionHeading(c *ui.Context, text string) *ui.Element {
	return ui.Text(c, text).FontSize(fontLg).FontWeight(600)
}

// labelled is a label over a supporting text, as the page shows beside its
// switches and buttons.
func labelled(c *ui.Context, label, supporting string) *ui.Element {
	p := paletteOf(c)
	return ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
		ui.Text(c, label).FontWeight(500)
		if supporting != "" {
			ui.Text(c, supporting).FontSize(fontSm).TextColor(p.muted)
		}
	})
}

// styleInput gives a text input, a select or a time input the look of
// Astryx's inputs: a border, the theme's radius, 32 DIPs high.
func styleInput(c *ui.Context, e *ui.Element, invalid bool) *ui.Element {
	p := paletteOf(c)
	border := p.strong
	if invalid {
		border = p.danger
	} else if e.Focused() || e.FocusWithin() {
		border = p.accent
	}
	return e.MinHeight(controlHeight).Radius(radiusElement).Background(p.popover).Border(1, border)
}
