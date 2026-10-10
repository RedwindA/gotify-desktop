# AGENTS.md

Receive-only Gotify desktop client in Go (Windows first, then macOS; Linux is the dev box).
The window is a web page in the system webview, through the `mygo` framework (`github.com/egoist/mygo`):
a React frontend in `frontend/` built with the Astryx design system (`@astryxdesign/*`, docs: `cd frontend && node node_modules/@astryxdesign/cli/clients/cli/bin/astryx.mjs component <Name>`).

## Hard rules

- No cgo. Everything must build with `CGO_ENABLED=0` for windows, darwin and linux.
- Dependencies are pinned to exact versions published at least 7 days ago (`frontend/bunfig.toml` enforces it for npm).
- Use Astryx components and props for the UI; plain CSS in `frontend/src/app.css` only for what they do not cover.
- A major UI change (layout, navigation, the look of the main window) refreshes the README's `docs/main-*.png` (see Commands). For the macOS frosted-glass sidebar, capture the native demo app from the screen; GOTIFY_SCREENSHOTS disables native materials.

## Layout

- `internal/gotify`: REST + websocket client (`X-Gotify-Key` auth, sub-path aware base URL).
- `internal/store`: SQLite (modernc, WAL) with versioned migrations; `SaveMessages` is the dedup point.
- `internal/secret`: client tokens in `tokens.json` (mode 0600) in the data dir, not the OS keyring: unsigned macOS builds would be asked for keychain access after every update (`Tokens` interface, `Memory` for tests).
- `internal/conn`: per-server supervisor (dial, REST catch-up, ping/pong liveness, backoff, auth failure).
- `internal/notify`: native notifications (Windows toasts, macOS UNUserNotificationCenter, Linux D-Bus), policy and dispatcher.
- `internal/appearance`: the system's accent color (`#rrggbb`), read per OS and cached briefly.
- `internal/app`: the controller (servers, supervisors, store, notifications) with no UI.
- `internal/api`: the service the page calls (`mygo.Bind`) and its events; `Demo` and `FakeBackend` back its tests and the preview.
- `frontend/`: the page (Vite, React, Astryx); `src/mygo.ts` is generated from `internal/api` by `mygo generate`. Message bodies render with Astryx `Markdown`.
- `internal/itest`: integration tests against the real gotify server (`harness` runs it).
- `main.go`: window, tray, single instance, power events; `cmd/` has notifytest, preview (the frontend in a browser over demo data) and genicons.

## Commands

```sh
go vet ./...
go test ./...
cd frontend && bun install && bun run typecheck && bun run build   # the page, into frontend/dist
go run github.com/egoist/mygo/cmd/mygo generate                     # after changing internal/api
go run github.com/egoist/mygo/cmd/mygo dev                          # the app with the Vite dev server
GOTIFY_DEMO=1 go run github.com/egoist/mygo/cmd/mygo dev            # the same over demo data: no data dir or network
CGO_ENABLED=0 GOOS=windows go build ./... && CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build ./... && CGO_ENABLED=0 GOOS=linux go build ./...

# Integration tests: builds the gotify server from $GOTIFY_SERVER_SRC (default /home/austin/server)
# with CGO_ENABLED=1 into the user cache dir (harness only), runs it on a random port.
GOTIFY_IT=1 go test -race -count=1 ./internal/itest ./internal/app

# Packaging and screenshots
go run ./cmd/genicons                      # resources/icon.svg and the PNGs (mygo's own SVG renderer)
go run ./cmd/preview                       # the built page with demo data at http://127.0.0.1:5174
bun run --cwd frontend screenshots         # dist/screenshots/*.png through the preview and Chrome, light and dark
# Automated reference images: the app built for this machine renders its views over demo data (GOTIFY_SCREENSHOTS, screenshots.go)
# in a window of its own, English and Chinese, light and dark, at the display's scale (2x on Retina or at 200%), then quits.
go run github.com/egoist/mygo/cmd/mygo build -o build
GOTIFY_SCREENSHOTS=dist/app-screenshots "build/darwin-arm64/Gotify Desktop.app/Contents/MacOS/Gotify Desktop"      # macOS
$env:GOTIFY_SCREENSHOTS = "dist/app-screenshots"; & "build/windows-amd64/Gotify Desktop.exe" 2>&1 | Out-Host        # Windows PowerShell: the pipe waits and shows the log
GDK_BACKEND=x11 GDK_SCALE=2 GOTIFY_SCREENSHOTS=dist/app-screenshots xvfb-run -a -s "-screen 0 2560x1600x24" build/linux-amd64/gotify-desktop
# (Linux, also headless: Xvfb at 2x; without GDK_BACKEND=x11 GTK may pick a Wayland socket and ignore GDK_SCALE)
# The README's macOS docs/main-*.png need the composited screen, not CapturePage or a window-only capture:
# GOTIFY_SCREENSHOTS disables native materials; window-only capture omits the desktop behind the sidebar.
GOTIFY_DEMO=1 "build/darwin-arm64/Gotify Desktop.app/Contents/MacOS/Gotify Desktop"
# In Settings, choose English / 简体中文 and Light / Dark; select "CPU temperature normal" in All messages.
# Keep the window focused, with no overlays, over the same wallpaper. Capture its on-screen bounds:
# screencapture -x -R<x>,<y>,<width>,<height> docs/main-light.png
# Repeat for main-dark.png, main-zh-light.png and main-zh-dark.png (actual bounds, not literal placeholders).
# Windows/macOS/Linux packages into build/ (macOS signing and .dmg need a Mac; the Windows installer needs makensis)
go run github.com/egoist/mygo/cmd/mygo build -platform windows/amd64,windows/arm64,darwin/universal,linux/amd64 -o build

# Releases: run go vet and go test (CI does not), bump "version" in mygo.json, add its "## <version>"
# section to CHANGELOG.md (the update window's notes), then push the tag v<version>.
# .github/workflows/release.yml checks the page, builds every platform unsigned with signed updates (secret MYGO_UPDATER_PRIVATE_KEY) and publishes the GitHub
# release, which installed apps update to (updater plugin, tray item). No draft: the tag is the release.
```
