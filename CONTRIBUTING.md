# Contributing

English | [简体中文](CONTRIBUTING.zh-CN.md)

Bug reports, fixes and features are welcome, written by hand or with AI. What matters is that a pull request is finished: correct, fast where it runs often, and leaving nothing behind for someone else to clean up.

## Issues

Say what you saw and what you expected, with the app's version (Settings, under About), your OS and the Gotify server's version. For a message that shows wrong, include the JSON you sent to `/message` (the `message` and `extras`), with secrets removed. A screenshot helps for anything visual.

## Pull requests written with AI

AI-written code is fine. You don't have to share the session or the prompts. Instead, the pull request shows two things:

1. **A current frontier model wrote it.** Attach a screenshot showing the model the code was written with, such as the model picker or status line of your tool. "Current" means the newest flagship model of its vendor when you open the pull request, not an older or smaller one kept for cost.
2. **A model from another vendor reviewed it.** Code written with Claude is reviewed by GPT, code written with GPT by Claude or Gemini, and so on; the reviewer is also a current flagship model. Attach a screenshot of the review (model visible) and, for each finding, say whether you fixed it or why it doesn't apply.

Run the review on the final diff, and again after any change it led to that is more than a small fix. The pull request template has a place for both. A pull request without them will be asked for them before it is reviewed.

If you wrote the code by hand, say so in the description.

## Do it once, completely

A pull request should be the last change its subject needs for a while. Before opening one:

- **Fix the cause, not the symptom**, and write down the cause in the description. If the cause is in a dependency, explain why the workaround here is the right place.
- **Mind the cost on hot paths.** Rendering a message, the message list, the websocket and the store run constantly. Don't do work proportional to a payload's size on every render, don't re-check what an earlier layer already guarantees, and return early when there is nothing to do.
- **Leave no debt.** No TODOs, no "follow-up PR", no dead code or the old path left beside the new one, no copy of logic that exists elsewhere. If you replace something, delete what it replaces.
- **Keep it small and focused.** The smallest change that solves the whole problem, one subject per pull request. Unrelated cleanups go in their own.
- **Test the bug, not just the fix.** Add a test that fails without your change: Go tests next to the code, frontend tests in `frontend/scripts`. Cover what must stay blocked or unchanged as well as what now works.
- **Match the code around it.** Same structure, naming and comment style; comments say why, not what.

## Project rules

These come from [AGENTS.md](AGENTS.md), which also lists the layout and every command; AI tools read it too.

- No cgo: everything builds with `CGO_ENABLED=0` for Windows, macOS and Linux.
- New dependencies only when they earn it, pinned to an exact version published at least 7 days ago.
- The UI uses Astryx components and props; plain CSS in `frontend/src/app.css` only for what they don't cover.
- After changing `internal/api`, regenerate `frontend/src/mygo.ts` with `go run github.com/egoist/mygo/cmd/mygo generate`.
- A change to the main window's layout or look refreshes the README screenshots (`docs/main-*.png`).
- Don't bump the version or edit CHANGELOG.md; that happens at release.

## Before you push

```sh
go vet ./...
go test ./...
cd frontend && bun install && bun run typecheck && bun run build
CGO_ENABLED=0 GOOS=windows go build ./... && CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build ./... && CGO_ENABLED=0 GOOS=linux go build ./...
```

Run the frontend test scripts your change touches (`bun run test:<name>`, see `frontend/package.json`). For a change you can see, try it in the app (`GOTIFY_DEMO=1 go run github.com/egoist/mygo/cmd/mygo dev`) or the preview (`go run ./cmd/preview`), and say in the description what you checked and on which OS, and what you couldn't.

CI runs only on release tags, so these checks are yours to run.
