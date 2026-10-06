//go:build windows

package notify

import (
	"crypto/sha256"
	"encoding/base64"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/go-ole/go-ole"
)

// These small WinRT bindings add stable toast identities and history removal,
// which go-toast's Push does not expose. ABI definitions are from Microsoft's
// Windows metadata: https://github.com/microsoft/windows-rs/blob/master/crates/libs/windows/src/Windows/UI/Notifications/mod.rs
// All calls run on winNotifier's COM thread. Slots start after IInspectable's
// six methods; factory calls also receive their interface pointer as this.
//
//go:uintptrescapes
func rtCall(obj *ole.IUnknown, slot int, args ...uintptr) error {
	method := (*[16]uintptr)(unsafe.Pointer(obj.RawVTable))[slot]
	hr, _, _ := syscall.SyscallN(method, append([]uintptr{uintptr(unsafe.Pointer(obj))}, args...)...)
	runtime.KeepAlive(obj)
	if int32(hr) < 0 {
		return ole.NewError(hr)
	}
	return nil
}

func toastTag(id string) string {
	h := sha256.Sum256([]byte(id))
	return base64.RawURLEncoding.EncodeToString(h[:12]) // Windows allows 16 characters.
}

func showTaggedToast(appID string, n Notification) error {
	doc, err := ole.RoActivateInstance("Windows.Data.Xml.Dom.XmlDocument")
	if err != nil {
		return err
	}
	defer doc.Release()
	io, err := doc.QueryInterface(ole.NewGUID("6cd0e74e-ee65-4489-9ebf-ca43e87ba637"))
	if err != nil {
		return err
	}
	defer io.Release()
	xml, err := ole.NewHString(buildToastXML(n))
	if err != nil {
		return err
	}
	defer ole.DeleteHString(xml)
	if err := rtCall(&io.IUnknown, 6, uintptr(xml)); err != nil {
		return err
	}
	factory, err := ole.RoGetActivationFactory("Windows.UI.Notifications.ToastNotification", ole.NewGUID("04124b20-82c6-4229-b109-fd9ed4662b53"))
	if err != nil {
		return err
	}
	defer factory.Release()
	var toast *ole.IUnknown
	if err := rtCall(&factory.IUnknown, 6, uintptr(unsafe.Pointer(doc)), uintptr(unsafe.Pointer(&toast))); err != nil {
		return err
	}
	defer toast.Release()
	v2, err := toast.QueryInterface(ole.NewGUID("9dfb9fd1-143a-490e-90bf-b9fba7132de7"))
	if err != nil {
		return err
	}
	defer v2.Release()
	tag, err := ole.NewHString(toastTag(n.ID))
	if err != nil {
		return err
	}
	defer ole.DeleteHString(tag)
	if err := rtCall(&v2.IUnknown, 6, uintptr(tag)); err != nil {
		return err
	}
	group, err := ole.NewHString("gotify")
	if err != nil {
		return err
	}
	defer ole.DeleteHString(group)
	if err := rtCall(&v2.IUnknown, 8, uintptr(group)); err != nil {
		return err
	}
	manager, err := ole.RoGetActivationFactory("Windows.UI.Notifications.ToastNotificationManager", ole.NewGUID("50ac103f-d235-4598-bbef-98fe4d1a3ad4"))
	if err != nil {
		return err
	}
	defer manager.Release()
	app, err := ole.NewHString(appID)
	if err != nil {
		return err
	}
	defer ole.DeleteHString(app)
	var notifier *ole.IUnknown
	if err := rtCall(&manager.IUnknown, 7, uintptr(app), uintptr(unsafe.Pointer(&notifier))); err != nil {
		return err
	}
	defer notifier.Release()
	return rtCall(notifier, 6, uintptr(unsafe.Pointer(toast)))
}

func removeTaggedToast(appID, id string) error {
	manager, err := ole.RoGetActivationFactory("Windows.UI.Notifications.ToastNotificationManager", ole.NewGUID("7ab93c52-0e48-4750-ba9d-1a4113981847"))
	if err != nil {
		return err
	}
	defer manager.Release()
	var history *ole.IUnknown
	if err := rtCall(&manager.IUnknown, 6, uintptr(unsafe.Pointer(&history))); err != nil {
		return err
	}
	defer history.Release()
	tag, err := ole.NewHString(toastTag(id))
	if err != nil {
		return err
	}
	defer ole.DeleteHString(tag)
	app, err := ole.NewHString(appID)
	if err != nil {
		return err
	}
	defer ole.DeleteHString(app)
	group, err := ole.NewHString("gotify")
	if err != nil {
		return err
	}
	defer ole.DeleteHString(group)
	// RemoveGroupedTagWithId requires the nonempty group used when showing.
	return rtCall(history, 8, uintptr(tag), uintptr(group), uintptr(app))
}
