package api

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"time"

	"gotify-desktop/internal/app"
	"gotify-desktop/internal/conn"
	"gotify-desktop/internal/gotify"
	"gotify-desktop/internal/store"
)

func demoIcon(c color.RGBA) []byte {
	const n = 96
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			dx, dy := float64(x)-n/2, float64(y)-n/2
			if dx*dx+dy*dy < (n/2-2)*(n/2-2) {
				k := uint8(40 * float64(y) / n)
				img.Set(x, y, color.RGBA{c.R - min(c.R, k), c.G - min(c.G, k), c.B - min(c.B, k), 255})
			}
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

// Demo returns a backend with two servers and realistic messages. imageURL is
// shown in a markdown message.
func Demo(imageURL string) *FakeBackend {
	f := NewFakeBackend()
	now := time.Now()
	mkApp := func(id uint, name string, c color.RGBA) app.AppInfo {
		img := demoIcon(c)
		return app.AppInfo{ID: id, Name: name, Image: img, ImageKey: app.ImageKey(img)}
	}
	f.Servers = []app.ServerInfo{
		{ID: 1, Name: "Home Server", URL: "https://gotify.home.example", State: conn.Connected, Apps: []app.AppInfo{
			mkApp(1, "Backup", color.RGBA{0x2b, 0x8c, 0xf0, 255}), mkApp(2, "Monitoring", color.RGBA{0xe0, 0x5a, 0x47, 255}),
			func() app.AppInfo { a := app.AppInfo{ID: 3, Name: "Home Assistant"}; return a }(),
		}},
		{ID: 2, Name: "Work Gotify", URL: "https://notify.work.example", State: conn.Backoff, Err: "Can't reach the server: connection refused",
			RetryAt: now.Add(12 * time.Second), Apps: []app.AppInfo{mkApp(1, "CI", color.RGBA{0x2f, 0xa4, 0x6b, 255}), mkApp(2, "Deploys", color.RGBA{0x8a, 0x5c, 0xd6, 255})}},
	}
	msg := func(server int64, id, appID uint, ago time.Duration, prio int, title, body string, read bool, extras map[string]any) store.StoredMessage {
		return store.StoredMessage{ServerID: server, Read: read, Message: gotify.Message{
			ID: id, AppID: appID, Priority: prio, Title: title, Message: body, Date: now.Add(-ago), Extras: extras}}
	}
	md := map[string]any{"client::display": map[string]any{"contentType": "text/markdown"}}
	click := map[string]any{"client::notification": map[string]any{"click": map[string]any{"url": "https://grafana.home.example/d/disk"}}}
	f.Msgs = []store.StoredMessage{
		msg(1, 9, 2, 40*time.Second, 9, "Disk almost full on nas-01", "**97%** of `/volume1` is used.\n\n- Largest folder: `/volume1/backups` (1.2 TB)\n- Free space: **18 GB**\n\nSee the [Grafana dashboard](https://grafana.home.example/d/disk) for the trend.",
			false, map[string]any{"client::display": md["client::display"], "client::notification": click["client::notification"]}),
		msg(1, 14, 1, 22*time.Minute, 5, "Nightly backup finished", "## Summary\n\n| Host | Size | Time |\n|---|---|---|\n| nas-01 | 412 GB | 41m |\n| laptop | 38 GB | 6m |\n\n> All snapshots verified.\n\n```\nrestic check: no errors\n```",
			false, md),
		msg(1, 8, 3, 2*time.Hour, 4, "Front door opened", "The front door was opened at 14:02 while everyone was away.", true,
			map[string]any{"client::notification": map[string]any{"bigImageUrl": demoSnapshotURL}}),
		msg(1, 7, 1, 3*time.Hour, 2, "", "Plain text message with a link https://example.com/status and a second line.\nSecond line here.", true, nil),
		msg(1, 6, 2, 26*time.Hour, 5, "CPU temperature normal", "![chart]("+imageURL+")\n\nBack to **54 °C** after the fan curve change.", true, md),
		msg(1, 5, 1, 50*time.Hour, 1, "Backup started", "Starting nightly job…", true, nil),
		msg(2, 31, 1, 9*time.Minute, 7, "Build #1842 failed", "`main` failed at **integration-tests**\n\n1. `TestLogin` timed out\n2. `TestCheckout` flaky",
			false, md),
		msg(2, 30, 2, 5*time.Hour, 3, "Deployed v2.14.0 to production", "Rollout complete in 3m 12s.", true, nil),
	}
	f.Images = map[string][]byte{demoSnapshotURL: DemoChart()}
	f.Refresh()
	return f
}

// demoSnapshotURL is the image of a demo message, which FetchImage serves without a network.
const demoSnapshotURL = "https://ha.home.example/api/camera_proxy/camera.front_door"


// DemoChart is the image of the demo's chart message, a PNG.
func DemoChart() []byte {
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
