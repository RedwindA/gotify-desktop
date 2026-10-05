# AGENTS.md

Receive-only Gotify desktop client in Go (Windows first, then macOS; Linux is the dev box).
UI is native, drawn by the `mygo` framework (`github.com/egoist/mygo`, package `ui`, no webview).

## Hard rules

- No cgo. Everything must build with `CGO_ENABLED=0` for windows, darwin and linux.
- Dependencies are pinned to exact versions published at least 7 days ago.

## Layout

- `internal/gotify`: REST + websocket client (`X-Gotify-Key` auth, sub-path aware base URL).
- `internal/store`: SQLite (modernc, WAL) with versioned migrations; `SaveMessages` is the dedup point.
- `internal/secret`: client tokens in the OS keyring (`Tokens` interface, `Memory` for tests).
- `internal/conn`: per-server supervisor (dial, REST catch-up, ping/pong liveness, backoff, auth failure).
- `internal/itest`: integration tests against the real gotify server.

## Commands

```sh
go vet ./...
go test ./...
CGO_ENABLED=0 GOOS=windows go build ./... && CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build ./... && CGO_ENABLED=0 GOOS=linux go build ./...

# Integration tests: builds the gotify server from $GOTIFY_SERVER_SRC (default /home/austin/server)
# with CGO_ENABLED=1 into the user cache dir (harness only), runs it on a random port.
GOTIFY_IT=1 go test -race -count=1 ./internal/itest
```
