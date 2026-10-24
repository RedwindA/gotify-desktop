package api

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"slices"
	"strings"
	"testing"
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
