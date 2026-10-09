// Package i18n translates the text Go shows: the tray, notifications and
// errors. The page translates its own. Text is written in English, which is
// also the key of its translation, so a missing translation shows English.
package i18n

import (
	"fmt"
	"strings"
	"sync/atomic"
)

// Lang is a language the app is translated to.
type Lang string

const (
	English Lang = "en"
	Chinese Lang = "zh-CN"
)

var (
	system atomic.Pointer[string]
	pref   atomic.Pointer[string]
)

// SetSystem records the system's locale, such as "zh-CN" or "en_US.UTF-8",
// which an empty preference follows.
func SetSystem(locale string) { system.Store(&locale) }

// SetPreference sets the language the user chose: "en", "zh-CN", or "" to follow the system.
func SetPreference(p string) { pref.Store(&p) }

// Resolve returns the language of a preference: "" follows the system.
func Resolve(p string) Lang {
	if p == "" {
		if s := system.Load(); s != nil {
			p = *s
		}
	}
	if l := strings.ToLower(p); strings.HasPrefix(l, "zh") {
		return Chinese
	}
	return English
}

// Current returns the language the app shows now.
func Current() Lang {
	p := ""
	if s := pref.Load(); s != nil {
		p = *s
	}
	return Resolve(p)
}

// T translates text, a fmt format, to the current language and formats it with args.
func T(text string, args ...any) string {
	if Current() == Chinese {
		if zh, ok := chinese[text]; ok {
			text = zh
		}
	}
	if len(args) == 0 {
		return text
	}
	return fmt.Sprintf(text, args...)
}

var chinese = map[string]string{
	// Tray.
	"Open Gotify Desktop":                 "打开 Gotify Desktop",
	"Pause notifications for 1 hour":      "暂停通知 1 小时",
	"Resume notifications":                "恢复通知",
	"Quit":                                "退出",
	"Check for Updates…":                  "检查更新…",
	"Gotify Desktop — no unread messages": "Gotify Desktop — 没有未读消息",
	"Gotify Desktop — %d unread":          "Gotify Desktop — %d 条未读",
	" (a server is offline)":              "（有服务器离线）",
	", notifications paused":              "，通知已暂停",

	// Menus (macOS).
	"About Gotify Desktop": "关于 Gotify Desktop",
	"Settings…":            "设置…",
	"Add Server…":          "添加服务器…",
	"Services":             "服务",
	"Hide Gotify Desktop":  "隐藏 Gotify Desktop",
	"Hide Others":          "隐藏其他",
	"Show All":             "全部显示",
	"Quit Gotify Desktop":  "退出 Gotify Desktop",
	"Close Window":         "关闭窗口",
	"File":                 "文件",
	"Edit":                 "编辑",
	"Window":               "窗口",

	// Dialogs.
	"Choose a CA certificate": "选择 CA 证书",
	"Certificates":            "证书",
	"All files":               "所有文件",
	"Gotify Desktop could not open its data: ": "Gotify Desktop 无法打开数据：",

	// Notifications.
	"%d missed messages":      "错过了 %d 条消息",
	"%d new messages from %s": "%[2]s 发来 %[1]d 条新消息",
	"Notifications work.":     "通知工作正常。",

	// Errors.
	"Wrong username or password": "用户名或密码错误",
	"The server's TLS certificate is not trusted. Add its CA certificate or skip verification under Advanced.": "服务器的 TLS 证书不受信任。请在“高级”中添加其 CA 证书，或跳过证书验证。",
	"The server took too long to answer": "服务器响应超时",
	"Can't find the server: %s":          "找不到服务器：%s",
	"No Gotify server found at this address (HTTP 404). Check the URL, including any sub-path.": "该地址没有 Gotify 服务器（HTTP 404）。请检查网址，包括子路径。",
	"The server answered with an error: %s":                                                     "服务器返回错误：%s",
	"Can't reach the server: %s":                                                                "无法连接服务器：%s",
	"This does not look like a Gotify server.":                                                  "这看起来不是 Gotify 服务器。",
	"This server was added as %s. Remove it and add it again to use another account.":           "此服务器是以 %s 的身份添加的。要使用其他账号，请移除后重新添加。",
	"Couldn't store the token: %v":                                                              "无法保存令牌：%v",
	"Can't read the token: %v":                                                                  "无法读取令牌：%v",
	"Enter the server's address.":                                                               "请输入服务器地址。",
	"This is not a valid address.":                                                              "这不是有效的地址。",
	"Enter your username and password.":                                                         "请输入用户名和密码。",
	"The CA certificate is not a PEM certificate.":                                              "CA 证书不是 PEM 格式。",
	"This file is not a PEM certificate.":                                                       "此文件不是 PEM 证书。",
	"Choosing files is not available.":                                                          "无法选择文件。",
	"Starting at login is not available.":                                                       "无法设置开机启动。",
	"Checking for updates is not available.":                                                    "无法检查更新。",
	"Can't open %q.":                                                                            "无法打开 %q。",
}
