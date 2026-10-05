package view

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"

	"gotify-desktop/internal/app"
	"gotify-desktop/internal/conn"
	"gotify-desktop/internal/notify"
	"gotify-desktop/internal/store"
)

// FakeBackend is an in-memory Backend, for tests and screenshots.
type FakeBackend struct {
	mu       sync.Mutex
	Servers  []app.ServerInfo
	Msgs     []store.StoredMessage
	Settings notify.Settings
	// Errors to return from the matching calls.
	AddErr, DeleteErr error
	Added             []app.ServerInput
	Deleted           []uint
	Tested            atomic.Int32
	snap              atomic.Pointer[app.Snapshot]
	gen               uint64
}

func NewFakeBackend() *FakeBackend {
	f := &FakeBackend{Settings: notify.DefaultSettings()}
	f.refresh()
	return f
}

// Refresh rebuilds the snapshot after the fields changed.
func (f *FakeBackend) Refresh() { f.mu.Lock(); defer f.mu.Unlock(); f.refresh() }

func (f *FakeBackend) refresh() {
	f.gen++
	snap := &app.Snapshot{Settings: f.Settings, Gen: f.gen, MsgGen: f.gen}
	for _, sv := range f.Servers {
		sv.Unread = 0
		apps := make([]app.AppInfo, len(sv.Apps))
		copy(apps, sv.Apps)
		for i := range apps {
			apps[i].Unread = 0
		}
		for _, m := range f.Msgs {
			if m.ServerID != sv.ID || m.Read {
				continue
			}
			sv.Unread++
			for i := range apps {
				if apps[i].ID == m.AppID {
					apps[i].Unread++
				}
			}
		}
		sv.Apps = apps
		snap.Unread += sv.Unread
		snap.Servers = append(snap.Servers, sv)
	}
	f.snap.Store(snap)
}

func (f *FakeBackend) Snapshot() *app.Snapshot { return f.snap.Load() }

func (f *FakeBackend) Messages(q store.MessageQuery) ([]store.StoredMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []store.StoredMessage
	for _, m := range f.Msgs {
		switch {
		case q.ServerID != 0 && m.ServerID != q.ServerID, q.AppID != 0 && m.AppID != q.AppID:
			continue
		case q.Search != "" && !strings.Contains(strings.ToLower(m.Title+" "+m.Message.Message), strings.ToLower(q.Search)):
			continue
		}
		out = append(out, m)
		if q.Limit > 0 && len(out) == q.Limit {
			break
		}
	}
	return out, nil
}

func (f *FakeBackend) DeleteMessage(ctx context.Context, serverID int64, id uint) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.DeleteErr != nil {
		return f.DeleteErr
	}
	for i, m := range f.Msgs {
		if m.ServerID == serverID && m.ID == id {
			f.Msgs = append(f.Msgs[:i:i], f.Msgs[i+1:]...)
			break
		}
	}
	f.Deleted = append(f.Deleted, id)
	f.refresh()
	return nil
}

func (f *FakeBackend) MarkRead(serverID int64, ids ...uint) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.Msgs {
		for _, id := range ids {
			if f.Msgs[i].ServerID == serverID && f.Msgs[i].ID == id {
				f.Msgs[i].Read = true
			}
		}
	}
	f.refresh()
}

func (f *FakeBackend) MarkAllRead(serverID int64, appID uint) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.Msgs {
		if f.Msgs[i].ServerID == serverID && (appID == 0 || f.Msgs[i].AppID == appID) {
			f.Msgs[i].Read = true
		}
	}
	f.refresh()
}

func (f *FakeBackend) AddServer(ctx context.Context, in app.ServerInput) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.AddErr != nil {
		return 0, f.AddErr
	}
	f.Added = append(f.Added, in)
	id := int64(len(f.Servers) + 1)
	f.Servers = append(f.Servers, app.ServerInfo{ID: id, Name: in.Name, URL: in.URL, State: conn.Connected})
	f.refresh()
	return id, nil
}

func (f *FakeBackend) Relogin(ctx context.Context, serverID int64, user, pass string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.AddErr != nil {
		return f.AddErr
	}
	for i := range f.Servers {
		if f.Servers[i].ID == serverID {
			f.Servers[i].State, f.Servers[i].Err, f.Servers[i].NeedLogin = conn.Connected, "", false
		}
	}
	f.refresh()
	return nil
}

func (f *FakeBackend) UpdateServer(ctx context.Context, serverID int64, name string, insecure bool, ca []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.Servers {
		if f.Servers[i].ID == serverID {
			f.Servers[i].Name, f.Servers[i].Insecure = name, insecure
		}
	}
	f.refresh()
	return nil
}

func (f *FakeBackend) RemoveServer(ctx context.Context, serverID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.Servers {
		if f.Servers[i].ID == serverID {
			f.Servers = append(f.Servers[:i:i], f.Servers[i+1:]...)
			break
		}
	}
	f.refresh()
	return nil
}

func (f *FakeBackend) SetSettings(s notify.Settings) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Settings = s
	f.refresh()
	return nil
}

func (f *FakeBackend) SetAppPref(serverID int64, appID uint, p store.AppPref) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.Servers {
		for j := range f.Servers[i].Apps {
			if f.Servers[i].ID == serverID && f.Servers[i].Apps[j].ID == appID {
				f.Servers[i].Apps[j].Muted, f.Servers[i].Apps[j].MinPriority = p.Muted, p.MinPriority
			}
		}
	}
	f.refresh()
	return nil
}

func (f *FakeBackend) Test() error { f.Tested.Add(1); return nil }
