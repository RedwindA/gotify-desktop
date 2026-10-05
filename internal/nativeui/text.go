package nativeui

import (
	"fmt"
	"time"

	"gotify-desktop/internal/api"
)

// messages is the window's text in one language, as the page's
// (frontend/src/i18n.ts), whose keys it keeps.
type messages struct {
	appName, allMessages, allFromServer, addServer, settings, openSidebar string
	serverOptions, appOptions                                             func(string) string
	editServer, signInAgainMenu, markAllRead, removeServer                string
	notificationSettings, muted                                           string

	connected, connecting     string
	retryingIn                func(int) string
	signInAgainStatus         string
	disconnected              string
	welcomeTitle, welcomeText string

	servers, unreadCount                         func(int) string
	search, searchMessages                       string
	noMatchTitle                                 func(string) string
	noMatchText, noMessagesTitle, noMessagesText string
	signInTo                                     func(string) string
	sessionEnded, signIn                         string
	cantReach, notConnected                      func(string) string
	couldNotLoad, couldNotDelete                 func(string) string

	message, messageActions, copyText, openLink, openImageInBrowser, delete, unread string
	priority                                                                        func(int) string
	copied                                                                          string

	addServerTitle, addServerSubtitle, connect, editServerTitle, save, signInAgainTitle string
	sessionEndedOn                                                                      func(string) string
	serverAddress, enterAddress, name, namePlaceholder, username, enterUsername         string
	password, enterPassword, advanced, skipTLS, skipTLSText, skipTLSWarning             string
	caCertificate, caHint, customCA, remove, chooseFile, cancel, serverAdded, signedIn  string

	removeTitle                               func(string) string
	removeText, removeAction                  string
	couldNotRemove                            func(string) string
	appPrefsSubtitle                          func(string) string
	mute, muteText, notifyAbout, everyMessage string
	priorityAndUp                             func(int) string
	couldNotSave                              func(string) string

	general, language, languageSystem, appearance, appearanceSystem, appearanceLight, appearanceDark string
	startAtLogin, startAtLoginText                                                                   string
	couldNotLoginItem                                                                                func(string) string
	notifications, notificationsPaused, notificationsOn                                              string
	resumesAt                                                                                        func(string) string
	notificationsOnText, resume, pauseHour, pauseTomorrow, dnd, dndText, from, to                    string
	highBypass, highBypassText, testTitle, testText, sendTest, testSent                              string
	notificationsUnavailable, couldNotSaveSettings                                                   func(string) string
	about, dataIn                                                                                    string

	// ago and pausedFormat write times as the page's Timestamp and Intl formats do.
	ago          func(time.Duration) string
	date         func(time.Time) string
	pausedFormat func(time.Time) string
}

var en = &messages{
	appName: "Gotify Desktop", allMessages: "All messages", allFromServer: "All from this server", addServer: "Add server",
	settings: "Settings", openSidebar: "Show sidebar",
	serverOptions: func(n string) string { return n + " options" }, appOptions: func(n string) string { return n + " options" },
	editServer: "Edit server…", signInAgainMenu: "Sign in again…", markAllRead: "Mark all as read", removeServer: "Remove server…",
	notificationSettings: "Notification settings…", muted: "Muted",

	connected: "Connected", connecting: "Connecting…", retryingIn: func(s int) string { return fmt.Sprintf("Retrying in %ds", s) },
	signInAgainStatus: "Sign in again", disconnected: "Disconnected",
	welcomeTitle: "Welcome to Gotify Desktop",
	welcomeText:  "Get your Gotify notifications on this computer. Add a server to start receiving messages.",

	servers: func(n int) string {
		if n == 1 {
			return "1 server"
		}
		return fmt.Sprintf("%d servers", n)
	},
	unreadCount: func(n int) string { return fmt.Sprintf("%d unread", n) },
	search:      "Search", searchMessages: "Search messages",
	noMatchTitle:    func(q string) string { return "No messages match “" + q + "”" },
	noMatchText:     "Try other words, or search all messages.",
	noMessagesTitle: "No messages yet", noMessagesText: "New messages from this scope show up here as they arrive.",
	signInTo:     func(n string) string { return "Sign in to " + n + " again" },
	sessionEnded: "The session ended, so no new messages arrive.", signIn: "Sign in",
	cantReach:      func(n string) string { return "Can't reach " + n },
	notConnected:   func(n string) string { return "Not connected to " + n },
	couldNotLoad:   func(e string) string { return "Couldn't load messages: " + e },
	couldNotDelete: func(e string) string { return "Couldn't delete the message: " + e },

	message: "Message", messageActions: "Message actions", copyText: "Copy text", openLink: "Open link",
	openImageInBrowser: "Open image in browser", delete: "Delete", unread: "Unread",
	priority: func(n int) string { return fmt.Sprintf("Priority %d", n) }, copied: "Copied",

	addServerTitle: "Add a server", addServerSubtitle: "Sign in to receive its messages on this computer.", connect: "Connect",
	editServerTitle: "Edit server", save: "Save", signInAgainTitle: "Sign in again",
	sessionEndedOn: func(n string) string {
		return "Your session on “" + n + "” ended. Sign in to keep receiving messages."
	},
	serverAddress: "Server address", enterAddress: "Enter the server's address.", name: "Name",
	namePlaceholder: "Defaults to the server's address", username: "Username", enterUsername: "Enter your username.",
	password: "Password", enterPassword: "Enter your password.", advanced: "Advanced",
	skipTLS:        "Skip TLS certificate verification",
	skipTLSText:    "Anyone on the network could read your messages and token. Only use this for servers you control.",
	skipTLSWarning: "Certificate verification is off.", caCertificate: "CA certificate",
	caHint: "Trust a private certificate authority (PEM).", customCA: "Custom CA certificate", remove: "Remove",
	chooseFile: "Choose file…", cancel: "Cancel", serverAdded: "Server added", signedIn: "Signed in",

	removeTitle:      func(n string) string { return "Remove “" + n + "”?" },
	removeText:       "Its messages are deleted from this computer and this device signs out of the server.",
	removeAction:     "Remove server",
	couldNotRemove:   func(e string) string { return "Couldn't remove the server: " + e },
	appPrefsSubtitle: func(s string) string { return "Notifications from this app on " + s },
	mute:             "Mute notifications", muteText: "Messages still arrive and count as unread.",
	notifyAbout: "Notify me about", everyMessage: "Every message",
	priorityAndUp: func(n int) string { return fmt.Sprintf("Priority %d and up", n) },
	couldNotSave:  func(e string) string { return "Couldn't save: " + e },

	general: "General", language: "Language", languageSystem: "System language", appearance: "Appearance",
	appearanceSystem: "Match the system", appearanceLight: "Light", appearanceDark: "Dark",
	startAtLogin: "Start at login", startAtLoginText: "Open Gotify Desktop in the background when you sign in to this computer.",
	couldNotLoginItem: func(e string) string { return "Couldn't change the login item: " + e },
	notifications:     "Notifications", notificationsPaused: "Notifications are paused", notificationsOn: "Notifications are on",
	resumesAt:           func(w string) string { return "They resume " + w + "." },
	notificationsOnText: "Messages show a notification as they arrive.", resume: "Resume",
	pauseHour: "Pause for 1 hour", pauseTomorrow: "Until tomorrow", dnd: "Do not disturb",
	dndText: "Hide notifications during these hours. Messages still arrive.", from: "From", to: "To",
	highBypass: "Let priority 8 and up through", highBypassText: "Urgent messages still notify during do not disturb.",
	testTitle: "Test notifications", testText: "Check that this computer shows notifications from Gotify Desktop.",
	sendTest: "Send a test", testSent: "Test notification sent",
	notificationsUnavailable: func(e string) string { return "Notifications are not available: " + e },
	couldNotSaveSettings:     func(e string) string { return "Couldn't save settings: " + e },
	about:                    "About", dataIn: "data in",

	ago: func(d time.Duration) string {
		plural := func(n int, unit string) string {
			if n == 1 {
				return "1 " + unit + " ago"
			}
			return fmt.Sprintf("%d %ss ago", n, unit)
		}
		switch {
		case d < 5*time.Second:
			return "now"
		case d < time.Minute:
			return plural(int(d/time.Second), "second")
		case d < time.Hour:
			return plural(int(d/time.Minute), "minute")
		case d < 24*time.Hour:
			return plural(int(d/time.Hour), "hour")
		}
		return plural(int(d/(24*time.Hour)), "day")
	},
	date:         func(t time.Time) string { return t.Format("Jan 2, 2006, 15:04") },
	pausedFormat: func(t time.Time) string { return t.Format("Mon 15:04") },
}

var zh = &messages{
	appName: "Gotify Desktop", allMessages: "全部消息", allFromServer: "此服务器的全部消息", addServer: "添加服务器",
	settings: "设置", openSidebar: "显示侧栏",
	serverOptions: func(n string) string { return n + " 选项" }, appOptions: func(n string) string { return n + " 选项" },
	editServer: "编辑服务器…", signInAgainMenu: "重新登录…", markAllRead: "全部标为已读", removeServer: "移除服务器…",
	notificationSettings: "通知设置…", muted: "已静音",

	connected: "已连接", connecting: "正在连接…", retryingIn: func(s int) string { return fmt.Sprintf("%d 秒后重试", s) },
	signInAgainStatus: "需要重新登录", disconnected: "未连接",
	welcomeTitle: "欢迎使用 Gotify Desktop", welcomeText: "在这台电脑上接收 Gotify 通知。添加一个服务器即可开始接收消息。",

	servers:     func(n int) string { return fmt.Sprintf("%d 个服务器", n) },
	unreadCount: func(n int) string { return fmt.Sprintf("%d 条未读", n) },
	search:      "搜索", searchMessages: "搜索消息",
	noMatchTitle:    func(q string) string { return "没有匹配“" + q + "”的消息" },
	noMatchText:     "换个关键词试试，或在全部消息中搜索。",
	noMessagesTitle: "还没有消息", noMessagesText: "新消息到达后会显示在这里。",
	signInTo:     func(n string) string { return "请重新登录 " + n },
	sessionEnded: "登录已失效，收不到新消息。", signIn: "登录",
	cantReach:      func(n string) string { return "无法连接 " + n },
	notConnected:   func(n string) string { return "未连接到 " + n },
	couldNotLoad:   func(e string) string { return "无法加载消息：" + e },
	couldNotDelete: func(e string) string { return "无法删除消息：" + e },

	message: "消息", messageActions: "消息操作", copyText: "复制文本", openLink: "打开链接",
	openImageInBrowser: "在浏览器中打开图片", delete: "删除", unread: "未读",
	priority: func(n int) string { return fmt.Sprintf("优先级 %d", n) }, copied: "已复制",

	addServerTitle: "添加服务器", addServerSubtitle: "登录后即可在这台电脑上接收它的消息。", connect: "连接",
	editServerTitle: "编辑服务器", save: "保存", signInAgainTitle: "重新登录",
	sessionEndedOn: func(n string) string {
		return "你在“" + n + "”上的登录已失效。重新登录以继续接收消息。"
	},
	serverAddress: "服务器地址", enterAddress: "请输入服务器地址。", name: "名称", namePlaceholder: "默认使用服务器地址",
	username: "用户名", enterUsername: "请输入用户名。", password: "密码", enterPassword: "请输入密码。", advanced: "高级",
	skipTLS: "跳过 TLS 证书验证", skipTLSText: "同一网络中的任何人都可能读取你的消息和令牌。仅对你自己管理的服务器使用。",
	skipTLSWarning: "证书验证已关闭。", caCertificate: "CA 证书", caHint: "信任私有证书颁发机构（PEM）。",
	customCA: "自定义 CA 证书", remove: "移除", chooseFile: "选择文件…", cancel: "取消", serverAdded: "已添加服务器", signedIn: "已登录",

	removeTitle:      func(n string) string { return "移除“" + n + "”？" },
	removeText:       "这台电脑上它的消息会被删除，并且此设备会从该服务器注销。",
	removeAction:     "移除服务器",
	couldNotRemove:   func(e string) string { return "无法移除服务器：" + e },
	appPrefsSubtitle: func(s string) string { return s + " 上此应用的通知" },
	mute:             "静音通知", muteText: "消息仍会接收，并计入未读。", notifyAbout: "通知范围", everyMessage: "所有消息",
	priorityAndUp: func(n int) string { return fmt.Sprintf("优先级 %d 及以上", n) },
	couldNotSave:  func(e string) string { return "无法保存：" + e },

	general: "通用", language: "语言", languageSystem: "跟随系统", appearance: "外观", appearanceSystem: "跟随系统",
	appearanceLight: "浅色", appearanceDark: "深色", startAtLogin: "开机启动", startAtLoginText: "登录这台电脑时在后台打开 Gotify Desktop。",
	couldNotLoginItem: func(e string) string { return "无法更改开机启动：" + e },
	notifications:     "通知", notificationsPaused: "通知已暂停", notificationsOn: "通知已开启",
	resumesAt:           func(w string) string { return "将于 " + w + " 恢复。" },
	notificationsOnText: "消息到达时会显示通知。", resume: "恢复", pauseHour: "暂停 1 小时", pauseTomorrow: "暂停到明天",
	dnd: "勿扰模式", dndText: "在这段时间内不显示通知，消息仍会接收。", from: "开始", to: "结束",
	highBypass: "允许优先级 8 及以上的消息", highBypassText: "勿扰期间紧急消息仍会通知。",
	testTitle: "测试通知", testText: "检查这台电脑能否显示 Gotify Desktop 的通知。", sendTest: "发送测试", testSent: "已发送测试通知",
	notificationsUnavailable: func(e string) string { return "通知不可用：" + e },
	couldNotSaveSettings:     func(e string) string { return "无法保存设置：" + e },
	about:                    "关于", dataIn: "数据位于",

	ago: func(d time.Duration) string {
		switch {
		case d < 5*time.Second:
			return "刚刚"
		case d < time.Minute:
			return fmt.Sprintf("%d 秒前", int(d/time.Second))
		case d < time.Hour:
			return fmt.Sprintf("%d 分钟前", int(d/time.Minute))
		case d < 24*time.Hour:
			return fmt.Sprintf("%d 小时前", int(d/time.Hour))
		}
		return fmt.Sprintf("%d 天前", int(d/(24*time.Hour)))
	},
	date: func(t time.Time) string { return t.Format("2006年1月2日 15:04") },
	pausedFormat: func(t time.Time) string {
		return []string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}[t.Weekday()] + " " + t.Format("15:04")
	},
}

// textOf returns the text of the language the state shows.
func textOf(s *api.State) *messages {
	if s != nil && s.Language == api.LanguageChinese {
		return zh
	}
	return en
}
