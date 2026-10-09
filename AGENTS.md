# AGENTS.md

Receive-only Gotify desktop client in Go (Windows first, then macOS; Linux is the dev box).
The window is a web page in the system webview, through the `mygo` framework (`github.com/egoist/mygo`):
a React frontend in `frontend/` built with the Astryx design system (`@astryxdesign/*`, docs: `cd frontend && node node_modules/@astryxdesign/cli/clients/cli/bin/astryx.mjs component <Name>`).

## Hard rules

- No cgo. Everything must build with `CGO_ENABLED=0` for windows, darwin and linux.
- Dependencies are pinned to exact versions published at least 7 days ago (`frontend/bunfig.toml` enforces it for npm).
- Use Astryx components and props for the UI; plain CSS in `frontend/src/app.css` only for what they do not cover.

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
# Windows/macOS/Linux packages into build/ (macOS signing and .dmg need a Mac; the Windows installer needs makensis)
go run github.com/egoist/mygo/cmd/mygo build -platform windows/amd64,windows/arm64,darwin/universal,linux/amd64 -o build

# Releases: run go vet and go test (CI does not), bump "version" in mygo.json, add its "## <version>"
# section to CHANGELOG.md (the update window's notes), then push the tag v<version>.
# .github/workflows/release.yml checks the page, builds every platform unsigned with signed updates (secret MYGO_UPDATER_PRIVATE_KEY) and drafts the GitHub
# release; publishing the draft is what installed apps update to (updater plugin, tray item).
```
