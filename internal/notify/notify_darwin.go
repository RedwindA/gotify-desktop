//go:build darwin

package notify

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// Notifications use UNUserNotificationCenter, which needs a bundled app with
// a bundle identifier. The delegate class is registered once and routes to
// the notifier through a package variable (purego callbacks are never freed).

const (
	presentSound  = 1 << 1
	presentList   = 1 << 3
	presentBanner = 1 << 4

	nsUTF8 = 4

	interruptPassive       = 0
	interruptActive        = 1
	interruptTimeSensitive = 2
)

type darwinNotifier struct {
	supported bool
	center    objc.ID
	cacheDir  string
	act       activator
	seq       atomic.Uint64
}

var (
	active    atomic.Pointer[darwinNotifier]
	classOnce sync.Once
	selCache  sync.Map
)

func sel(name string) objc.SEL {
	if s, ok := selCache.Load(name); ok {
		return s.(objc.SEL)
	}
	s := objc.RegisterName(name)
	selCache.Store(name, s)
	return s
}

func cls(name string) objc.ID { return objc.ID(objc.GetClass(name)) }

func send(obj objc.ID, selector string, args ...any) objc.ID { return obj.Send(sel(selector), args...) }

func sendBool(obj objc.ID, selector string, args ...any) bool {
	return objc.Send[bool](obj, sel(selector), args...)
}

func nsString(s string) objc.ID {
	s = strings.ReplaceAll(strings.ToValidUTF8(s, "\uFFFD"), "\x00", "")
	return send(cls("NSString"), "stringWithUTF8String:", s)
}

func goString(str objc.ID) string {
	if str == 0 {
		return ""
	}
	n := int(send(str, "lengthOfBytesUsingEncoding:", uint(nsUTF8)))
	buf := make([]byte, n+1)
	send(str, "getCString:maxLength:encoding:", unsafe.Pointer(&buf[0]), uint(n+1), uint(nsUTF8))
	return string(buf[:n])
}

// withPool runs fn in an autorelease pool on a pinned thread, as pushes and pops must match.
func withPool(fn func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool := send(send(cls("NSAutoreleasePool"), "alloc"), "init")
	defer send(pool, "drain")
	fn()
}

// callBlock invokes a block handed over by the system, such as a completion handler.
func callBlock(block uintptr, args ...uintptr) {
	lit := *(**[3]uintptr)(unsafe.Pointer(&block))
	var a [8]uintptr
	a[0] = block
	n := copy(a[1:], args)
	purego.SyscallN(lit[2], a[:n+1]...)
}

func load(path string) { purego.Dlopen(path, purego.RTLD_GLOBAL|purego.RTLD_NOW) }

func registerDelegateClass() {
	var protocols []*objc.Protocol
	if p := objc.GetProtocol("UNUserNotificationCenterDelegate"); p != nil {
		protocols = append(protocols, p)
	}
	_, err := objc.RegisterClass("GotifyDesktopNotificationDelegate", objc.GetClass("NSObject"), protocols, nil, []objc.MethodDef{
		{Cmd: sel("userNotificationCenter:willPresentNotification:withCompletionHandler:"), Fn: func(self objc.ID, _ objc.SEL, center, n objc.ID, handler uintptr) {
			opts := uintptr(presentBanner | presentList)
			if !userInfoSilent(requestOf(n)) {
				opts |= presentSound
			}
			callBlock(handler, opts)
		}},
		{Cmd: sel("userNotificationCenter:didReceiveNotificationResponse:withCompletionHandler:"), Fn: func(self objc.ID, _ objc.SEL, center, resp objc.ID, handler uintptr) {
			req := requestOf(send(resp, "notification"))
			id := goString(send(userInfoOf(req), "objectForKey:", nsString("id")))
			if id == "" {
				id = goString(send(req, "identifier"))
			}
			if n := active.Load(); n != nil {
				n.act.fire(id)
			}
			callBlock(handler)
		}},
	})
	if err != nil {
		panic(fmt.Sprintf("notify: %v", err))
	}
}

func requestOf(n objc.ID) objc.ID { return send(n, "request") }

func userInfoOf(req objc.ID) objc.ID { return send(send(req, "content"), "userInfo") }

func userInfoSilent(req objc.ID) bool {
	v := send(userInfoOf(req), "objectForKey:", nsString("silent"))
	return v != 0 && sendBool(v, "boolValue")
}

func New(appID, appName, cacheDir string, appIconPNG []byte) (Notifier, error) {
	load("/System/Library/Frameworks/Foundation.framework/Foundation")
	load("/System/Library/Frameworks/UserNotifications.framework/UserNotifications")
	n := &darwinNotifier{cacheDir: filepath.Join(cacheDir, "attachments")}
	if cls("UNUserNotificationCenter") == 0 {
		return n, nil
	}
	var bundleID objc.ID
	withPool(func() { bundleID = send(send(cls("NSBundle"), "mainBundle"), "bundleIdentifier") })
	if bundleID == 0 {
		return n, nil
	}
	if err := os.MkdirAll(n.cacheDir, 0o755); err != nil {
		return nil, err
	}
	n.supported = true
	classOnce.Do(registerDelegateClass)
	active.Store(n)
	withPool(func() {
		n.center = send(cls("UNUserNotificationCenter"), "currentNotificationCenter")
		delegate := send(send(cls("GotifyDesktopNotificationDelegate"), "alloc"), "init") // owned for the process lifetime
		send(n.center, "setDelegate:", delegate)
		blk := objc.NewBlock(func(_ objc.Block, granted bool, err objc.ID) {})
		send(n.center, "requestAuthorizationWithOptions:completionHandler:", uint(1<<0|1<<1|1<<2), blk) // badge | sound | alert
		blk.Release()
	})
	return n, nil
}

func (n *darwinNotifier) Supported() bool { return n.supported }

func (n *darwinNotifier) OnActivate(fn func(string)) { n.act.set(fn) }

func (n *darwinNotifier) Show(nn Notification) error {
	if !n.supported {
		return ErrUnsupported
	}
	withPool(func() {
		content := send(send(cls("UNMutableNotificationContent"), "alloc"), "init")
		defer send(content, "release")
		send(content, "setTitle:", nsString(nn.Title))
		if nn.AppName != "" {
			send(content, "setSubtitle:", nsString(nn.AppName))
		}
		send(content, "setBody:", nsString(nn.Body))
		if nn.Group != "" {
			send(content, "setThreadIdentifier:", nsString(nn.Group))
		}
		if nn.Level != LevelSilent {
			send(content, "setSound:", send(cls("UNNotificationSound"), "defaultSound"))
		}
		if sendBool(content, "respondsToSelector:", sel("setInterruptionLevel:")) {
			level := map[Level]uint{LevelSilent: interruptPassive, LevelNormal: interruptActive, LevelHigh: interruptTimeSensitive}[nn.Level]
			send(content, "setInterruptionLevel:", level)
		}
		info := send(cls("NSMutableDictionary"), "dictionary")
		send(info, "setObject:forKey:", nsString(nn.ID), nsString("id"))
		send(info, "setObject:forKey:", send(cls("NSNumber"), "numberWithBool:", nn.Level == LevelSilent), nsString("silent"))
		send(content, "setUserInfo:", info)
		src := nn.ImagePath
		if src == "" {
			src = nn.IconPath
		}
		if att := n.attachment(src); att != 0 {
			send(content, "setAttachments:", send(cls("NSArray"), "arrayWithObject:", att))
		}
		req := send(cls("UNNotificationRequest"), "requestWithIdentifier:content:trigger:", nsString(nn.ID), content, objc.ID(0))
		send(n.center, "addNotificationRequest:withCompletionHandler:", req, objc.ID(0))
	})
	return nil
}

// attachment copies path first because the system moves the file it is given.
func (n *darwinNotifier) attachment(path string) objc.ID {
	if path == "" {
		return 0
	}
	tmp := filepath.Join(n.cacheDir, fmt.Sprintf("%d%s", n.seq.Add(1), filepath.Ext(path)))
	if err := copyFile(path, tmp); err != nil {
		return 0
	}
	url := send(cls("NSURL"), "fileURLWithPath:", nsString(tmp))
	att := send(cls("UNNotificationAttachment"), "attachmentWithIdentifier:URL:options:error:", nsString(""), url, objc.ID(0), objc.ID(0))
	if att == 0 {
		os.Remove(tmp)
	}
	return att
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	return out.Close()
}

func (n *darwinNotifier) Remove(id string) {
	if !n.supported {
		return
	}
	withPool(func() {
		ids := send(cls("NSArray"), "arrayWithObject:", nsString(id))
		send(n.center, "removeDeliveredNotificationsWithIdentifiers:", ids)
		send(n.center, "removePendingNotificationRequestsWithIdentifiers:", ids)
	})
}
