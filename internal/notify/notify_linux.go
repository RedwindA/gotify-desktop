//go:build linux

package notify

import (
	"html"
	"sync"

	"github.com/godbus/dbus/v5"
)

const (
	dbusDest = "org.freedesktop.Notifications"
	dbusPath = "/org/freedesktop/Notifications"
)

type linuxNotifier struct {
	conn    *dbus.Conn
	obj     dbus.BusObject
	act     activator
	appName string
	mu      sync.Mutex
	byKey   map[string]uint32
	byNum   map[uint32]string
}

// New connects to the session bus; without one the notifier reports Supported() == false.
func New(appID, appName, cacheDir string, appIconPNG []byte) (Notifier, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return unsupported{}, nil
	}
	n := &linuxNotifier{conn: conn, obj: conn.Object(dbusDest, dbusPath), byKey: map[string]uint32{}, byNum: map[uint32]string{}}
	if err := n.obj.Call(dbusDest+".GetServerInformation", 0).Err; err != nil {
		conn.Close()
		return unsupported{}, nil
	}
	if err := conn.AddMatchSignal(dbus.WithMatchObjectPath(dbusPath), dbus.WithMatchInterface(dbusDest)); err == nil {
		ch := make(chan *dbus.Signal, 16)
		conn.Signal(ch)
		go n.listen(ch)
	}
	n.appName = appName
	return n, nil
}

func (n *linuxNotifier) listen(ch <-chan *dbus.Signal) {
	for s := range ch {
		if len(s.Body) < 2 {
			continue
		}
		num, ok := s.Body[0].(uint32)
		if !ok {
			continue
		}
		switch s.Name {
		case dbusDest + ".ActionInvoked":
			n.mu.Lock()
			id := n.byNum[num]
			n.mu.Unlock()
			n.act.fire(id)
		case dbusDest + ".NotificationClosed":
			n.mu.Lock()
			if id, ok := n.byNum[num]; ok {
				delete(n.byNum, num)
				delete(n.byKey, id)
			}
			n.mu.Unlock()
		}
	}
}

func (n *linuxNotifier) Supported() bool { return true }

func (n *linuxNotifier) OnActivate(fn func(string)) { n.act.set(fn) }

func (n *linuxNotifier) Show(nn Notification) error {
	hints := map[string]dbus.Variant{"urgency": dbus.MakeVariant(byte(nn.Level))}
	if nn.Level == LevelSilent {
		hints["suppress-sound"] = dbus.MakeVariant(true)
	}
	if nn.ImagePath != "" {
		hints["image-path"] = dbus.MakeVariant(nn.ImagePath)
	}
	body := html.EscapeString(nn.Body)
	n.mu.Lock()
	replaces := n.byKey[nn.ID]
	n.mu.Unlock()
	var num uint32
	err := n.obj.Call(dbusDest+".Notify", 0, n.appName, replaces, nn.IconPath, nn.Title, body,
		[]string{"default", "Open"}, hints, int32(-1)).Store(&num)
	if err != nil {
		return err
	}
	n.mu.Lock()
	n.byKey[nn.ID], n.byNum[num] = num, nn.ID
	n.mu.Unlock()
	return nil
}

func (n *linuxNotifier) Remove(id string) {
	n.mu.Lock()
	num, ok := n.byKey[id]
	delete(n.byKey, id)
	delete(n.byNum, num)
	n.mu.Unlock()
	if ok {
		n.obj.Call(dbusDest+".CloseNotification", 0, num)
	}
}
