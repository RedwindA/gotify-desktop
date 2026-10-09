# Changelog

The section of each version is the release notes that the update window shows.

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
