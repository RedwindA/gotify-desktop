# Gotify Desktop

English | [简体中文](README.zh-CN.md)

A receive-only desktop client for [Gotify](https://gotify.net). It sits in the tray, keeps a connection to one or more Gotify servers and shows their messages as native notifications. Windows first, then macOS; Linux works too.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/main-dark.png">
  <img alt="The main window with messages from two servers" src="docs/main-light.png">
</picture>

## Why not just keep the Gotify web UI open?

The web UI that ships with gotify/server is good for managing a server. It is not as good for receiving notifications on a desktop:

- **No browser tab to keep alive.** The web UI only receives messages while its tab is open, and browsers throttle or freeze background tabs. Gotify Desktop runs as a single-instance tray app and keeps receiving when its window is closed. The tray icon shows whether everything is read, there is something unread, or a server is offline.
- **A connection that recovers on its own.** Each server has a supervisor that checks the websocket with ping/pong, reconnects with backoff, and reconnects right away when the computer wakes from sleep. After a reconnect it fetches the messages it missed over REST and stores each one once, so nothing is lost or shown twice.
- **Native notifications with a policy.** Windows toasts, macOS UNUserNotificationCenter and Linux D-Bus notifications, without browser permission prompts or a browser's name on them. They follow Gotify priorities: 0 never notifies, 1–3 are silent, 4–7 normal, 8 and up high. You also get:
  - do-not-disturb hours (they may wrap midnight), and high priority can still get through;
  - pausing notifications, and per-app mute or minimum priority;
  - burst summaries: more than 3 messages from one app within 10 seconds become one notification, and a catch-up of more than 3 messages becomes one "missed" summary;
  - big images and click URLs from the message extras.
- **Several servers at once.** The web UI logs in to one server; here every server has its own connection and they share one message list.
- **Local history.** Messages are kept in a local SQLite database, so history stays readable while a server is down. Client tokens are kept in plain text in `tokens.json` next to it, readable only by your user account: anyone who can read that file can act as your Gotify user.
- **A desktop window.** Markdown message bodies, images viewed in the app, light and dark themes, English and Simplified Chinese, and a message list that stays fast with many messages.

What the web UI still does better: it needs no install, works from any device with a browser, and manages the server: applications, clients, users and plugins. Gotify Desktop only receives messages, so keep using the web UI for those.

## Installing

Download the package for your system from [Releases](../../releases): the setup `.exe` for Windows, the `.dmg` for macOS, the `.deb` or the `.tar.gz` with `install.sh` for Linux.

The builds are not signed, so the system warns the first time you open them:

- **Windows:** SmartScreen says "Windows protected your PC". Click **More info**, then **Run anyway**.
- **macOS:** after the first attempt to open the app, go to **System Settings → Privacy & Security** and click **Open Anyway**. Or run `xattr -dr com.apple.quarantine "/Applications/Gotify Desktop.app"`.

## Building

You need Go and [Bun](https://bun.sh). Everything builds without cgo.

```sh
cd frontend && bun install && bun run build && cd ..
go run github.com/egoist/mygo/cmd/mygo dev                  # run the app
GOTIFY_DEMO=1 go run github.com/egoist/mygo/cmd/mygo dev    # the same over demo data
go run github.com/egoist/mygo/cmd/mygo build -platform windows/amd64,windows/arm64,darwin/universal,linux/amd64 -o build
```

Signing and the `.dmg` for macOS need a Mac; the Windows installer needs `makensis`. See [AGENTS.md](AGENTS.md) for tests and the rest of the commands.

## License

Copyright © 2026 RedwindA. Gotify Desktop is free software under the [GNU General Public License v3.0](LICENSE).
