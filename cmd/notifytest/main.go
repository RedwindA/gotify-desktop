// Command notifytest shows one notification per level and logs clicks, to
// try the native notification layer on a real desktop.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"gotify-desktop/internal/notify"
)

func main() {
	wait := flag.Duration("wait", 2*time.Minute, "how long to keep running for clicks")
	flag.Parse()

	dir := filepath.Join(os.TempDir(), "gotify-notifytest")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Fatal(err)
	}
	if f, err := os.OpenFile(filepath.Join(dir, "notifytest.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		defer f.Close()
		log.SetOutput(io.MultiWriter(os.Stderr, f))
	}

	icon := circleIcon(128)
	n, err := notify.New("dev.gotify.desktop.notifytest", "Gotify Notify Test", dir, icon)
	if err != nil {
		log.Fatalf("notify.New: %v", err)
	}
	log.Printf("args=%q supported=%v activationLaunch=%v", os.Args[1:], n.Supported(), notify.IsActivationLaunch(os.Args[1:]))
	n.OnActivate(func(id string) { log.Printf("ACTIVATED %q", id) })

	if notify.IsActivationLaunch(os.Args[1:]) {
		log.Printf("started by a toast click; waiting for the activation")
		time.Sleep(30 * time.Second)
		return
	}

	iconPath := filepath.Join(dir, "icon.png")
	heroPath := filepath.Join(dir, "hero.png")
	must(os.WriteFile(iconPath, icon, 0o644))
	must(os.WriteFile(heroPath, heroImage(364, 180), 0o644))

	for i, c := range []struct {
		level notify.Level
		name  string
	}{{notify.LevelSilent, "silent"}, {notify.LevelNormal, "normal"}, {notify.LevelHigh, "high"}} {
		err := n.Show(notify.Notification{
			ID:        fmt.Sprintf("s1-m%d", i+1),
			Title:     "Level: " + c.name,
			Body:      "Click me; the ID is logged. Sent by notifytest.",
			AppName:   "Backup",
			IconPath:  iconPath,
			ImagePath: heroPath,
			Group:     "s1-a1",
			Level:     c.level,
		})
		log.Printf("show %s: err=%v", c.name, err)
		time.Sleep(2 * time.Second)
	}
	log.Printf("waiting %s for clicks", *wait)
	time.Sleep(*wait)
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func circleIcon(size int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	c, r := float64(size)/2, float64(size)/2-2
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if dx, dy := float64(x)-c, float64(y)-c; dx*dx+dy*dy <= r*r {
				img.Set(x, y, color.RGBA{0x2b, 0x8c, 0xf0, 0xff})
			}
		}
	}
	return encode(img)
}

func heroImage(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 255 / w), 0x60, uint8(y * 255 / h), 0xff})
		}
	}
	return encode(img)
}

func encode(img image.Image) []byte {
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}
