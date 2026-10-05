// Command screenshots renders the main views headlessly, light and dark, into a directory.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo/ui"

	"gotify-desktop/internal/markdown"
	"gotify-desktop/internal/view"
)

func chart() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 480, 160))
	for x := 0; x < 480; x++ {
		for y := 0; y < 160; y++ {
			img.Set(x, y, color.RGBA{uint8(30 + y/2), uint8(90 + x/6), 200, 255})
		}
		y := 80 + int(40*float64((x*7)%97-48)/48)
		for d := -1; d <= 1; d++ {
			img.Set(x, y+d, color.RGBA{255, 255, 255, 255})
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

func main() {
	out := flag.String("o", "dist/screenshots", "output directory")
	flag.Parse()
	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(chart()) }))
	defer srv.Close()

	for _, dark := range []bool{false, true} {
		suffix := map[bool]string{false: "light", true: "dark"}[dark]
		shot := func(name string, f *view.FakeBackend, steps func(tt *ui.Tester, m *view.Model), w, h int) {
			m := view.New(f, view.Platform{Version: "0.1.0", DataDir: "~/.config/Gotify Desktop"}, markdown.NewImageCache(nil, nil))
			tt := ui.NewTester(m.View, w, h)
			tt.SetDark(dark)
			tt.SetScale(1.5)
			tt.Frame()
			if steps != nil {
				steps(tt, m)
			}
			time.Sleep(400 * time.Millisecond)
			tt.Frame()
			tt.Frame()
			path := filepath.Join(*out, fmt.Sprintf("%s-%s.png", name, suffix))
			var buf bytes.Buffer
			if err := png.Encode(&buf, tt.Image()); err != nil {
				log.Fatal(err)
			}
			if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
				log.Fatal(err)
			}
			fmt.Println(path)
		}
		click := func(tt *ui.Tester, s string) {
			if err := tt.Click(s); err != nil {
				log.Fatal(err)
			}
		}
		shot("welcome", view.NewFakeBackend(), nil, 1000, 640)
		shot("main", view.Demo(srv.URL+"/chart.png"), nil, 1100, 760)
		shot("app-scope", view.Demo(srv.URL+"/chart.png"), func(tt *ui.Tester, m *view.Model) { click(tt, "Backup") }, 1100, 760)
		shot("add-server", view.NewFakeBackend(), func(tt *ui.Tester, m *view.Model) {
			click(tt, "Add server")
			click(tt, "Server address")
			tt.Type("gotify.example.com")
			click(tt, "Username")
			tt.Type("alice")
			click(tt, "Connect")
			click(tt, "Advanced")
			click(tt, "Skip TLS certificate verification")
		}, 1000, 760)
		shot("settings", view.Demo(srv.URL+"/chart.png"), func(tt *ui.Tester, m *view.Model) { click(tt, "Settings") }, 1100, 820)
	}
}
