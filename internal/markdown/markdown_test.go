package markdown

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func view(r *Renderer, src string, md bool) func(c *ui.Context) {
	return func(c *ui.Context) {
		ui.Scroll(c).Fill().Padding(12).Children(func() { r.Render(c, "1/1", src, md) })
	}
}

func hasAll(t *testing.T, tt *ui.Tester, want ...string) {
	t.Helper()
	all := strings.Join(tt.Texts(), "\n")
	for _, w := range want {
		if !strings.Contains(all, w) {
			t.Errorf("missing %q in %q", w, tt.Texts())
		}
	}
}

func TestMarkdownConstructs(t *testing.T) {
	src := "# Title\n\nSome **bold**, _italic_, ~~gone~~ and `code`.\n\n- one\n- two\n\n1. first\n2. second\n\n> quoted\n\n```\nfmt.Println(1)\n```\n\n---\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n- [x] done\n- [ ] todo\n"
	tt := ui.NewTester(view(New(NewImageCache(nil, nil)), src, true), 500, 900)
	hasAll(t, tt, "Title", "bold", "italic", "gone", "code", "one", "two", "first", "second", "quoted", "fmt.Println(1)", "a", "b", "1", "2")
	for _, s := range tt.Texts() {
		if strings.ContainsAny(s, "*`#") && !strings.Contains(s, "fmt") {
			t.Errorf("markdown syntax leaked: %q", s)
		}
	}
}

func TestMarkdownLinks(t *testing.T) {
	src := "Go to [docs](https://example.com/docs), [bad](javascript:alert(1)), <https://auto.example/x> and [mail](mailto:a@b.c)."
	tt := ui.NewTester(view(New(NewImageCache(nil, nil)), src, true), 500, 300)
	if err := tt.Click("docs"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("bad"); err == nil {
		t.Fatal("unsafe link is clickable")
	}
	hasAll(t, tt, "bad")
	if err := tt.Click("https://auto.example/x"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("mail"); err != nil {
		t.Fatal(err)
	}
	got := tt.OpenedURLs()
	want := []string{"https://example.com/docs", "https://auto.example/x", "mailto:a@b.c"}
	if !slices.Equal(got, want) {
		t.Fatalf("opened %q, want %q", got, want)
	}
}

func TestPlainTextKeepsNewlinesAndLinksURLs(t *testing.T) {
	src := "line one\nsee https://example.com/a?b=1, ok\nline three **not bold**"
	tt := ui.NewTester(view(New(NewImageCache(nil, nil)), src, false), 500, 300)
	hasAll(t, tt, "line one", "https://example.com/a?b=1", "**not bold**")
	if err := tt.Click("https://example.com/a?b=1"); err != nil {
		t.Fatal(err)
	}
	if got := tt.OpenedURLs(); !slices.Equal(got, []string{"https://example.com/a?b=1"}) {
		t.Fatalf("opened %q", got)
	}
}

func TestParseOnceAndCacheBound(t *testing.T) {
	r := New(NewImageCache(nil, nil))
	a := r.doc("1/1", "**x**", true)
	b := r.doc("1/1", "**x**", true)
	if len(a) == 0 || &a[0] != &b[0] {
		t.Fatal("document parsed twice")
	}
	for i := 0; i < maxDocs+50; i++ {
		r.doc(strings.Repeat("k", i%7)+string(rune('a'+i%26)), strings.Repeat("y", i), true)
	}
	if len(r.docs) > maxDocs {
		t.Fatalf("cache unbounded: %d", len(r.docs))
	}
}

func TestImageLoadsAsynchronously(t *testing.T) {
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 40, 20))
	for x := 0; x < 40; x++ {
		img.Set(x, 3, color.RGBA{255, 0, 0, 255})
	}
	png.Encode(&buf, img)
	var served int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bad.png" {
			w.Write([]byte("not an image"))
			return
		}
		served++
		w.Write(buf.Bytes())
	}))
	defer srv.Close()
	loaded := make(chan struct{}, 4)
	r := New(NewImageCache(nil, func() { loaded <- struct{}{} }))
	tt := ui.NewTester(view(r, "![pic]("+srv.URL+"/ok.png)\n\n![broken]("+srv.URL+"/bad.png)\n\n![js](javascript:x)", true), 500, 400)
	if !tt.HasText("Loading image…") {
		t.Fatalf("no placeholder: %q", tt.Texts())
	}
	for i := 0; i < 2; i++ {
		select {
		case <-loaded:
		case <-time.After(5 * time.Second):
			t.Fatal("image never loaded")
		}
	}
	tt.Frame()
	if tt.HasText("Loading image…") {
		t.Fatalf("still loading: %q", tt.Texts())
	}
	hasAll(t, tt, "broken")
	tt.Frame()
	if served != 1 {
		t.Fatalf("image fetched %d times", served)
	}
}
