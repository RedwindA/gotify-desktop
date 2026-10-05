package notify

import (
	"crypto/sha1"
	"encoding/xml"
	"fmt"
	"strings"
)

// xmlSafe removes the characters XML 1.0 cannot carry.
func xmlSafe(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == 0x9, r == 0xA, r == 0xD, r >= 0x20 && r <= 0xD7FF, r >= 0xE000 && r <= 0xFFFD, r >= 0x10000 && r <= 0x10FFFF:
			return r
		}
		return -1
	}, s)
}

func xmlEscape(s string) string {
	var sb strings.Builder
	xml.EscapeText(&sb, []byte(xmlSafe(s)))
	return sb.String()
}

// buildToastXML renders the toast for Windows. Every piece of text is escaped
// here, since the XML goes straight to the Windows Runtime.
func buildToastXML(n Notification) string {
	duration, audio := "short", `<audio src="ms-winsoundevent:Notification.Default" loop="false"/>`
	switch n.Level {
	case LevelSilent:
		audio = `<audio silent="true"/>`
	case LevelHigh:
		duration, audio = "long", `<audio src="ms-winsoundevent:Notification.Reminder" loop="false"/>`
	}
	body := n.Body
	if n.AppName != "" && n.AppName != n.Title {
		body = n.AppName + " · " + body
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, `<toast activationType="foreground" launch="%s" duration="%s"><visual><binding template="ToastGeneric">`, xmlEscape(n.ID), duration)
	if n.ImagePath != "" {
		fmt.Fprintf(&sb, `<image placement="hero" src="%s"/>`, xmlEscape(n.ImagePath))
	}
	if n.IconPath != "" {
		fmt.Fprintf(&sb, `<image placement="appLogoOverride" hint-crop="circle" src="%s"/>`, xmlEscape(n.IconPath))
	}
	if n.Title != "" {
		fmt.Fprintf(&sb, `<text hint-maxLines="1">%s</text>`, xmlEscape(n.Title))
	}
	if body != "" {
		fmt.Fprintf(&sb, `<text>%s</text>`, xmlEscape(body))
	}
	sb.WriteString(`</binding></visual>` + audio + `</toast>`)
	return sb.String()
}

// activatorCLSID derives the COM class id of the activation callback from the
// app id (a version 5 UUID), so that apps with different ids never share one.
func activatorCLSID(appID string) string {
	ns := [16]byte{0x6b, 0xa7, 0xb8, 0x10, 0x9d, 0xad, 0x11, 0xd1, 0x80, 0xb4, 0x00, 0xc0, 0x4f, 0xd4, 0x30, 0xc8}
	h := sha1.New()
	h.Write(ns[:])
	h.Write([]byte("gotify-desktop/toast-activator/" + appID))
	sum := h.Sum(nil)[:16]
	sum[6] = sum[6]&0x0f | 0x50
	sum[8] = sum[8]&0x3f | 0x80
	return fmt.Sprintf("{%X-%X-%X-%X-%X}", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}
