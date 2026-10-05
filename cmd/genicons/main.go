// Command genicons rasterizes the app and tray icons into resources/ with
// MyGo's own SVG renderer: it renders each over white and over black and
// recovers the transparency from the two.
package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"strconv"

	"github.com/egoist/mygo/ui"
)

const bell = `<path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9" fill="%[1]s" stroke="%[1]s" stroke-width="2" stroke-linejoin="round" stroke-linecap="round"/>
<path d="M10.3 21a1.9 1.9 0 0 0 3.4 0" fill="none" stroke="%[1]s" stroke-width="2" stroke-linecap="round"/>`

func svg(body string) string {
	return `<svg xmlns="http://www.w3.org/2000/svg" width="1024" height="1024" viewBox="0 0 1024 1024">` + body + `</svg>`
}

func bellGroup(color string, x, y, scale float64) string {
	return `<g transform="translate(` + f(x) + ` ` + f(y) + `) scale(` + f(scale) + `)">` + sprintf(bell, color) + `</g>`
}

func main() {
	appIcon := svg(`<defs><linearGradient id="g" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#58b0ff"/><stop offset="1" stop-color="#1e55dc"/></linearGradient></defs>
<rect x="0" y="0" width="1024" height="1024" rx="232" fill="url(#g)"/>` + bellGroup("#ffffff", 212, 196, 25))
	write("resources/icon.svg", []byte(appIcon))
	writePNG("resources/icon.png", appIcon, 1024)

	dot := `<circle cx="780" cy="244" r="150" fill="#ff453a" stroke="#ffffff" stroke-width="40"/>`
	gray := `<defs><linearGradient id="g" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#9aa3b2"/><stop offset="1" stop-color="#6b7384"/></linearGradient></defs>
<rect x="0" y="0" width="1024" height="1024" rx="232" fill="url(#g)"/>` + bellGroup("#ffffff", 212, 196, 25)
	tray := map[string]string{
		"tray-normal":  appIcon,
		"tray-unread":  appIcon[:len(appIcon)-6] + dot + `</svg>`,
		"tray-offline": svg(gray),
	}
	for name, s := range tray {
		writePNG("resources/"+name+".png", s, 64)
	}

	mono := func(extra string) string {
		return svg(`<g transform="translate(40 40) scale(38)">` + sprintf(bell, "#000000") + `</g>` + extra)
	}
	template := map[string]string{
		"tray-mac-normal":  mono(""),
		"tray-mac-unread":  mono(`<circle cx="800" cy="224" r="170" fill="#000000"/>`),
		"tray-mac-offline": mono(`<path d="M110 910 914 110" stroke="#000000" stroke-width="80" stroke-linecap="round"/>`),
	}
	for name, s := range template {
		writePNG("resources/"+name+".png", s, 64)
	}
}

func render(s string, size int, bg ui.Color) *image.RGBA {
	parsed, err := ui.ParseSVG([]byte(s))
	if err != nil {
		log.Fatal(err)
	}
	return ui.Render(func(c *ui.Context) {
		t := *c.Theme()
		t.Background = bg
		c.SetTheme(&t)
		ui.Box(c).Fill().Padding(0).Background(bg).Children(func() {
			ui.Image(c, parsed).Size(float32(size), float32(size))
		})
	}, size, size, 1)
}

func writePNG(path, s string, size int) {
	onWhite := render(s, size, ui.RGB(255, 255, 255))
	onBlack := render(s, size, ui.RGB(0, 0, 0))
	out := image.NewNRGBA(onWhite.Rect)
	for i := 0; i < len(out.Pix); i += 4 {
		// white - black = 255 * (1 - alpha), black = color * alpha
		alpha := 255 - (int(onWhite.Pix[i+1]) - int(onBlack.Pix[i+1]))
		alpha = max(0, min(255, alpha))
		if alpha == 0 {
			continue
		}
		for k := 0; k < 3; k++ {
			out.Pix[i+k] = uint8(min(255, int(onBlack.Pix[i+k])*255/alpha))
		}
		out.Pix[i+3] = uint8(alpha)
	}
	_ = color.RGBA{}
	var b bytes.Buffer
	if err := png.Encode(&b, out); err != nil {
		log.Fatal(err)
	}
	write(path, b.Bytes())
}

func write(path string, data []byte) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		log.Fatal(err)
	}
	log.Println("wrote", path)
}

func f(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func sprintf(format string, a ...any) string { return fmt.Sprintf(format, a...) }
