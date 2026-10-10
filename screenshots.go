package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"

	"gotify-desktop/internal/i18n"
)

// The views GOTIFY_SCREENSHOTS renders: the page's route, and the title of
// the message to select in it.
var screenshotViews = []struct{ name, hash, message string }{
	{"main", "#/", "CPU temperature normal"},
	{"app-scope", "#/s/1/a/1", ""},
	{"settings", "#/settings", ""},
}

// screenshotController keeps the demo's messages as they are, so that
// every screenshot shows the same unread counts: the page marks the
// messages it shows read while the window has focus.
type screenshotController struct{ demoController }

func (screenshotController) MarkRead(int64, ...uint) error { return nil }

// until polls the page until ok() holds, for at most 10 seconds.
const untilJS = `const until = async (ok) => {
  for (const end = Date.now() + 10000; !ok(); await new Promise((r) => setTimeout(r, 50))) {
    if (Date.now() > end) throw new Error("timed out waiting for " + ok);
  }
};
`

// screenshots renders the views over the demo data into d.shots, in English
// and Chinese, light and dark, and quits. The images come from the webview
// itself, at the display's scale, without the system's window controls.
func (d *desktop) screenshots() {
	if err := d.takeScreenshots(); err != nil {
		log.Printf("screenshots: %v", err)
		mygo.App.Exit(1)
	}
	mygo.App.Quit()
}

func (d *desktop) takeScreenshots() error {
	if err := os.MkdirAll(d.shots, 0o755); err != nil {
		return err
	}
	var w *mygo.Window
	for end := time.Now().Add(30 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		if w == nil {
			w = d.window()
		}
		if w != nil {
			ok, _ := w.Page().Eval(`!!document.querySelector("nav")`)
			if ok == true {
				break
			}
		}
		if time.Now().After(end) {
			return errors.New("the page did not load")
		}
	}
	for _, lang := range []string{"en", "zh-CN"} {
		i18n.SetSystem(lang)
		d.api.Changed()
		if _, err := w.Page().Eval(untilJS + fmt.Sprintf(`await until(() => document.documentElement.lang === %q)`, lang)); err != nil {
			return fmt.Errorf("%s: %w", lang, err)
		}
		suffix := ""
		if lang != "en" {
			suffix = "-zh"
		}
		for _, theme := range []mygo.ThemeSource{mygo.ThemeLight, mygo.ThemeDark} {
			mygo.Theme.SetSource(theme)
			for _, v := range screenshotViews {
				_, err := w.Page().Eval(untilJS + fmt.Sprintf(`
location.hash = %q;
await until(() => matchMedia("(prefers-color-scheme: dark)").matches === %t && document.querySelector("nav"));
const title = %q;
if (title) {
  const row = () => [...document.querySelectorAll("[role=option]")].find((el) => el.textContent.includes(title));
  await until(row);
  row().dispatchEvent(new PointerEvent("pointerup", { bubbles: true, button: 0 }));
  await until(() => row().getAttribute("aria-selected") === "true");
  // Without a pointer, the focus would draw its keyboard ring.
  document.activeElement?.blur();
}
await document.fonts.ready;
await until(() => [...document.images].every((img) => img.complete));
await new Promise((r) => setTimeout(r, 500));
await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r)));`, v.hash, theme == mygo.ThemeDark, v.message))
				if err != nil {
					return fmt.Errorf("%s: %w", v.name, err)
				}
				png, err := w.CapturePage()
				if err != nil {
					return fmt.Errorf("%s: %w", v.name, err)
				}
				path := filepath.Join(d.shots, fmt.Sprintf("%s%s-%s.png", v.name, suffix, theme))
				if err := os.WriteFile(path, png, 0o644); err != nil {
					return err
				}
				log.Print(path)
			}
		}
	}
	return nil
}
