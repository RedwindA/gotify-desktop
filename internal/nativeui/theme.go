package nativeui

import "github.com/egoist/mygo/ui"

// palette holds the colors of the page's theme (Astryx's neutral theme, the
// --color-* tokens of @astryxdesign/theme-neutral), for the light or the dark
// appearance, so that the window looks as the page does.
type palette struct {
	dark bool
	// body is the sidebar's background (--color-background-body), surface
	// the page's (--color-background-surface) and card a message's
	// (--color-background-card); popover is a dialog's.
	body, surface, card, popover ui.Color
	// border outlines cards and divides the header (--color-border), and
	// strong outlines inputs (--color-border-emphasized).
	border, strong        ui.Color
	text, muted, disabled ui.Color
	// gray is the background of the item chosen and of badges
	// (--color-background-gray); neutral the face of secondary buttons
	// (--color-neutral); hover and pressed the overlays over a face.
	gray, neutral, hover, pressed ui.Color
	accent, onAccent              ui.Color
	// blue marks unread messages (StatusDot's accent).
	blue ui.Color
	// The statuses: their dots, and the background and text of banners
	// and badges.
	success, warning, danger, onDanger         ui.Color
	warningBg, warningText, errorBg, errorText ui.Color
	code, codeBlock, overlay, shadow, skeleton ui.Color
}

var lightPalette = palette{
	body: ui.Hex("#f1f1f1"), surface: ui.Hex("#ffffff"), card: ui.Hex("#ffffff"), popover: ui.Hex("#ffffff"),
	border: ui.RGBA(0, 0, 0, 0x14/255.0), strong: ui.Hex("#d4d4d4"),
	text: ui.Hex("#000000"), muted: ui.Hex("#474747"), disabled: ui.Hex("#919191"),
	gray: ui.Hex("#e2e2e2"), neutral: ui.RGBA(0, 0, 0, 0x0f/255.0), hover: ui.RGBA(0, 0, 0, 0x0d/255.0), pressed: ui.RGBA(0, 0, 0, 0x1a/255.0),
	accent: ui.Hex("#1b1b1b"), onAccent: ui.Hex("#ffffff"),
	blue:    ui.Hex("#0074e2"),
	success: ui.Hex("#2e8b2e"), warning: ui.Hex("#e2b623"), danger: ui.Hex("#c9303a"), onDanger: ui.Hex("#ffffff"),
	warningBg: ui.Hex("#fae19e"), warningText: ui.Hex("#4b3900"), errorBg: ui.Hex("#ffc4be"), errorText: ui.Hex("#76000c"),
	code: ui.RGBA(0, 0, 0, 0x0f/255.0), codeBlock: ui.Hex("#ffffff"), overlay: ui.RGBA(0, 0, 0, 0.5), shadow: ui.RGBA(0, 0, 0, 0.1),
	skeleton: ui.Hex("#f1f1f1"),
}

var darkPalette = palette{
	dark: true,
	body: ui.Hex("#1b1b1b"), surface: ui.Hex("#262626"), card: ui.Hex("#1b1b1b"), popover: ui.Hex("#1b1b1b"),
	border: ui.RGBA(255, 255, 255, 0.1), strong: ui.Hex("#525252"),
	text: ui.Hex("#ffffff"), muted: ui.Hex("#9e9e9e"), disabled: ui.Hex("#525252"),
	gray: ui.Hex("#303030"), neutral: ui.RGBA(255, 255, 255, 0.1), hover: ui.RGBA(255, 255, 255, 0x0d/255.0), pressed: ui.RGBA(255, 255, 255, 0.1),
	accent: ui.Hex("#f1f1f1"), onAccent: ui.Hex("#111111"),
	blue:    ui.Hex("#6d9cfe"),
	success: ui.Hex("#6ab26b"), warning: ui.Hex("#e2b623"), danger: ui.Hex("#ff705d"), onDanger: ui.Hex("#111111"),
	warningBg: ui.Hex("#524824"), warningText: ui.Hex("#f8d36a"), errorBg: ui.Hex("#5b2b28"), errorText: ui.Hex("#ffc4be"),
	code: ui.RGBA(255, 255, 255, 0.1), codeBlock: ui.Hex("#111111"), overlay: ui.RGBA(0, 0, 0, 0.8), shadow: ui.RGBA(0, 0, 0, 0.3),
	skeleton: ui.Hex("#525252"),
}

// Sizes of the page (Astryx's tokens), in DIPs.
const (
	radiusElement   = 10 // --radius-element: buttons, inputs, sidebar items
	radiusContainer = 12 // --radius-container: cards, banners, dialogs
	radiusInner     = 6  // --radius-inner
	radiusPane      = 18 // the floating panes of glass, as macOS 26 rounds its sidebars
	controlHeight   = 32
	fontBase        = 14 // --font-size-base
	fontSm          = 12 // --font-size-sm
	fontLg          = 17 // --font-size-lg
	fontXl          = 20 // --font-size-xl
	font2xl         = 24 // --font-size-2xl
	sidebarWidth    = 260
	// narrow is the width below which the sidebar is a drawer, as Astryx's md breakpoint.
	narrow = 768
)

// paletteOf returns the palette of the window's appearance.
func paletteOf(c *ui.Context) *palette {
	if c.Theme().Dark {
		return &darkPalette
	}
	return &lightPalette
}

// applyTheme gives the toolkit's widgets the colors and sizes of the palette.
func applyTheme(c *ui.Context, p *palette) {
	t := *c.Theme()
	t.Background = p.surface
	t.Surface = p.neutral.Over(p.surface)
	t.SurfaceHover = p.hover.Over(t.Surface)
	t.SurfacePressed = p.pressed.Over(t.Surface)
	t.Border = p.strong
	t.Text, t.TextMuted = p.text, p.muted
	t.Accent, t.AccentText = p.accent, p.onAccent
	t.AccentHover = p.accent.Mix(p.surface, 0.15)
	t.AccentPressed = p.accent.Mix(p.surface, 0.25)
	t.Danger, t.Warning, t.Success = p.danger, p.warning, p.success
	t.Focus = p.accent.Alpha(0.5)
	t.Selection = p.blue.Alpha(0.3)
	t.Radius = radiusElement
	t.FontSize = fontBase
	c.SetTheme(&t)
}
