package api

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestCopyMessageImage(t *testing.T) {
	const chartURL = "https://img.example/chart.png"
	f := Demo(chartURL)
	f.Images[chartURL] = DemoChart()
	_, d := start(t, f, Platform{})
	if err := d.CopyMessageImage(t.Context(), ImageRequest{ServerID: 1, ID: 14}); err == nil {
		t.Fatal("no clipboard: want an error")
	}
	var clip []byte
	_, d = start(t, f, Platform{CopyImage: func(b []byte) error { clip = b; return nil }})
	copyImage := func(id uint, dark bool) image.Image {
		t.Helper()
		clip = nil
		if err := d.CopyMessageImage(t.Context(), ImageRequest{ServerID: 1, ID: id, Dark: dark, Scale: 2}); err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(clip))
		if err != nil {
			t.Fatal(err)
		}
		return img
	}

	table := copyImage(14, false)
	if b := table.Bounds(); b.Dx() != 2*shotWidth || b.Dy() < 2*150 {
		t.Fatalf("a table and code at 2x: %v", b)
	}
	luma := func(img image.Image) uint8 { return color.GrayModel.Convert(img.At(2, 2)).(color.Gray).Y }
	if l := luma(table); l < 200 {
		t.Errorf("light background: luma %d", l)
	}
	if l := luma(copyImage(14, true)); l > 60 {
		t.Errorf("dark background: luma %d", l)
	}
	short := copyImage(5, false).Bounds().Dy()
	if short >= table.Bounds().Dy() {
		t.Errorf("one line is %d tall, a table %d", short, table.Bounds().Dy())
	}
	// The image in the body and the message's own image show, once downloaded.
	withChart, withSnapshot := copyImage(6, false).Bounds().Dy(), copyImage(8, false).Bounds().Dy()
	delete(f.Images, chartURL)
	delete(f.Images, demoSnapshotURL)
	if without := copyImage(6, false).Bounds().Dy(); withChart < without+2*100 {
		t.Errorf("the chart did not show: %d tall, %d without it", withChart, without)
	}
	if without := copyImage(8, false).Bounds().Dy(); withSnapshot < without+2*100 {
		t.Errorf("the snapshot did not show: %d tall, %d without it", withSnapshot, without)
	}
	if err := d.CopyMessageImage(t.Context(), ImageRequest{ServerID: 1, ID: 99}); err == nil {
		t.Error("an unknown message: want an error")
	}
}

func TestMessageShotShowsTheBodyAlone(t *testing.T) {
	s := &messageShot{body: "## Summary\n\n```sh\nrestic check\n```", markdown: true}
	tt, err := s.tester()
	if err != nil {
		t.Fatal(err)
	}
	texts := tt.Texts()
	if !slices.Contains(texts, "Summary") || !slices.ContainsFunc(texts, func(s string) bool { return strings.Contains(s, "restic check") }) {
		t.Errorf("the body is missing: %q", texts)
	}
	if slices.Contains(texts, "Copy") {
		t.Errorf("a code block's copy button shows: %q", texts)
	}
}

func TestMessageShotTooLong(t *testing.T) {
	s := &messageShot{body: strings.Repeat("line\n\n", 40000), markdown: true, scale: 2}
	if _, err := s.render(); err == nil {
		t.Error("want an error")
	}
}

func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestDecodeDataImage(t *testing.T) {
	png := testPNG(t, 1, 1)
	b64 := base64.StdEncoding.EncodeToString(png)
	src := "data:image/png;base64," + b64
	got, ok := decodeDataImage(src)
	if !ok || !bytes.Equal(got, png) {
		t.Fatalf("png: ok=%v len=%d", ok, len(got))
	}
	if got, ok := decodeDataImage("data: image/jpeg;charset=utf-8;base64," + b64); !ok || !bytes.Equal(got, png) {
		t.Fatalf("jpeg alias with charset: ok=%v", ok)
	}
	if _, ok := decodeDataImage("data:image/svg+xml;base64," + b64); ok {
		t.Fatal("svg must be rejected")
	}
	if _, ok := decodeDataImage("data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg=="); ok {
		t.Fatal("html must be rejected")
	}
	if _, ok := decodeDataImage("javascript:alert(1)"); ok {
		t.Fatal("javascript must be rejected")
	}
	if _, ok := decodeDataImage("data:image/png;base64,"); ok {
		t.Fatal("empty payload must be rejected")
	}
}

func TestBodyImagesIncludesDataURI(t *testing.T) {
	png := testPNG(t, 1, 1)
	src := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(png)
	body := "邮件通知 识别文字: 国家任务达成奖励\n\n![](" + src + ")"
	if got := bodyImages(body); !slices.Equal(got, []string{src}) {
		t.Fatalf("bodyImages: %q", got)
	}
}

func TestMessageShotShowsDataImage(t *testing.T) {
	png := testPNG(t, 320, 180)
	src := "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	bmp, err := ui.DecodeBitmap(png)
	if err != nil {
		t.Fatal(err)
	}
	body := "alert\n\n![](" + src + ")"
	with, err := (&messageShot{body: body, markdown: true, images: map[string]*ui.Bitmap{src: bmp}}).render()
	if err != nil {
		t.Fatal(err)
	}
	without, err := (&messageShot{body: body, markdown: true}).render()
	if err != nil {
		t.Fatal(err)
	}
	if with.Bounds().Dy() < without.Bounds().Dy()+100 {
		t.Errorf("the data image did not show: %d tall, %d without it", with.Bounds().Dy(), without.Bounds().Dy())
	}
}
