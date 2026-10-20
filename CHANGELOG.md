# Changelog

The section of each version is the release notes that the update window shows.

## 0.3.3

- On Windows, messages containing emoji show notifications again.

## 0.3.2

- With the language set to follow the system, the app shows Chinese on Chinese systems again. Since 0.2.4 it showed English.
- The update window and the macOS menus show the language chosen in Settings too.

## 0.3.1

- Drag apps in the sidebar to reorder them, or use Move up and Move down in their menu. The order is the server's, so the web UI and other clients show it too (Gotify 2.9 or later).

## 0.3.0

- Messages deleted on the server, in the web UI or another client, disappear here too: on every connect and every 10 minutes.
- Settings has Message history: keep messages forever, or for 1 week up to 1 year. Older ones are deleted from this computer only.
- On Windows, the app uses the system proxy from Internet Options and its bypass list; HTTPS_PROXY, HTTP_PROXY and NO_PROXY take priority.
- Ctrl-click, Shift-click, Shift+arrow keys or Ctrl+A select several messages to mark as read or delete together. Each app and server in the sidebar has Delete all messages.
- Notifications have Mark as read and Open link buttons, and summaries Mark all as read. Marking messages read also clears their notifications.

## 0.2.4

- Inline code in markdown messages copies with a click.
- Bare web and e-mail addresses in messages are links that open in the default app, and links are underlined.
- On Windows, a maximized window stays on its display after being restored from minimized.

## 0.2.3

- Clicking a notification selects its message in the reading pane every time, also from All messages.
- On macOS, Send test notification reports why the system refused a notification instead of saying it was sent.

## 0.2.2

- On macOS, resizing the window or entering full screen no longer crashes.
- On Windows, a call from the page ends when the window closes or the webview cannot start.

## 0.2.1

- On macOS, right-clicking a message shows its menu again, and selected text stays selected while the menu is open.
- The Copy text button in the reading pane shows a check after copying.

## 0.2.0

- Messages show in a mail-style layout: a list and a reading pane, or one pane in a narrow window. Arrow keys, Delete, Ctrl+F and Escape work in the list.
- The window looks like a desktop app: Mica on Windows 11 and the sidebar material on macOS, the system accent color and font, and one 48px title bar row.
- Browser behaviour is gone: no page zoom, reload, print or find, no text selection outside messages, and dropped files no longer open in the window.
- Removing a server asks in a native dialog. Ctrl+, opens Settings and Ctrl+N adds a server; macOS gets a menu bar with both.
- Settings has Check for updates and a link to the source code on GitHub.

## 0.1.6

- Opening a notification for the newest message stays on the latest messages, so "Viewing older messages" only appears when newer ones exist.
- Notification bodies are no longer cut off at 300 characters.

## 0.1.5

- Hide the default browser context menu outside text fields in production builds, while keeping native editing commands and message actions.
- Add Copy selected text to message menus without losing the selection when the menu opens.
- Dismiss success toasts, such as Test notification sent, after two seconds.

## 0.1.2

- On Windows, reading a message removes its toast from the notification center.
- After a reconnect, missed messages are saved together and one notification shows the full count.
- Opening a notification can land on older messages, with a banner to return to the latest.
- Marking messages read reports a failure and tries those messages again.
- Pending notifications are no longer replayed after a restart.

## 0.1.1

- Fix text selection in message titles, plain-text bodies and Markdown bodies while keeping the message context menu available.

## 0.1.0

- First release: receive messages from one or more Gotify servers in the tray, with native notifications, local history and automatic updates.
