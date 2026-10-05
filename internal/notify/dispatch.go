package notify

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"gotify-desktop/internal/conn"
	"gotify-desktop/internal/gotify"
	"gotify-desktop/internal/store"
)

const (
	queueSize     = 256
	imageTimeout  = 5 * time.Second
	maxImageBytes = 5 << 20
	imageKeepFor  = 24 * time.Hour
)

// Dispatcher plans and shows notifications on its own goroutine so the
// connection sink never blocks.
type Dispatcher struct {
	n        Notifier
	st       *store.Store
	settings func() Settings
	cacheDir string
	http     *http.Client
	planner  *Planner

	mu     sync.Mutex
	closed bool
	q      chan conn.Event
	done   chan struct{}
	ctx    context.Context
	cancel context.CancelFunc
	icons  map[string]string // icon path -> content hash
}

func NewDispatcher(n Notifier, st *store.Store, settings func() Settings, cacheDir string, httpc *http.Client) *Dispatcher {
	if httpc == nil {
		httpc = http.DefaultClient
	}
	if settings == nil {
		settings = DefaultSettings
	}
	ctx, cancel := context.WithCancel(context.Background())
	d := &Dispatcher{
		n: n, st: st, settings: settings, cacheDir: cacheDir, http: httpc, planner: NewPlanner(),
		q: make(chan conn.Event, queueSize), done: make(chan struct{}), ctx: ctx, cancel: cancel, icons: map[string]string{},
	}
	pruneDir(filepath.Join(cacheDir, "images"), imageKeepFor)
	go d.run()
	return d
}

// Handle queues ev; it never blocks and drops the event if the queue is full.
func (d *Dispatcher) Handle(ev conn.Event) {
	if ev.Kind != conn.EventMessages || ev.Silent {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}
	select {
	case d.q <- ev:
	default:
		log.Printf("notify: queue full, dropping %d messages", len(ev.Messages))
	}
}

func (d *Dispatcher) Close() {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	d.closed = true
	close(d.q)
	d.mu.Unlock()
	d.cancel()
	<-d.done
}

func (d *Dispatcher) run() {
	defer close(d.done)
	for ev := range d.q {
		if d.ctx.Err() != nil {
			continue
		}
		d.process(ev)
	}
}

func (d *Dispatcher) process(ev conn.Event) {
	storedApps, err := d.st.Apps(ev.ServerID)
	if err != nil {
		log.Printf("notify: %v", err)
		return
	}
	apps := make(map[uint]store.App, len(storedApps))
	for _, a := range storedApps {
		apps[a.ID] = a
	}
	prefs := map[uint]AppPrefs{}
	for _, m := range ev.Messages {
		if _, ok := prefs[m.AppID]; ok {
			continue
		}
		p, err := d.st.GetAppPref(ev.ServerID, m.AppID)
		if err != nil {
			log.Printf("notify: %v", err)
		}
		prefs[m.AppID] = AppPrefs{Muted: p.Muted, MinPriority: p.MinPriority}
	}
	var fresh []gotify.Message
	for _, m := range ev.Messages {
		if d.pending(ev.ServerID, m.ID) {
			fresh = append(fresh, m)
		}
	}
	if len(fresh) == 0 {
		return
	}
	ev.Messages = fresh
	for _, p := range d.planner.Plan(ev, apps, prefs, d.settings(), time.Now()) {
		if d.ctx.Err() != nil {
			return
		}
		if !d.stillRelevant(p) {
			continue
		}
		n := p.Notification
		if a, ok := apps[p.AppID]; ok && p.AppID != 0 {
			n.IconPath = d.writeIcon(ev.ServerID, a)
		}
		if p.ImageURL != "" {
			n.ImagePath = d.downloadImage(p.ImageURL)
		}
		if d.ctx.Err() != nil || !d.stillRelevant(p) {
			continue
		}
		if err := d.n.Show(n); err != nil {
			log.Printf("notify: show %s: %v", n.ID, err)
		}
	}
}

// stillRelevant re-checks the store: a message deleted or read while the
// notification waited is not shown, nor is a summary of a removed server.
// pending reports whether a message is still stored and unread.
func (d *Dispatcher) pending(serverID int64, id uint) bool {
	msgs, err := d.st.Messages(store.MessageQuery{ServerID: serverID, ID: id, Limit: 1})
	return err == nil && len(msgs) == 1 && !msgs[0].Read
}

// stillRelevant re-checks the store: a message deleted or read while the notification
// waited is not shown, nor is a summary none of whose messages remain, or one of a removed server.
func (d *Dispatcher) stillRelevant(p Planned) bool {
	switch {
	case p.MessageID != 0:
		return d.pending(p.ServerID, p.MessageID)
	case len(p.MessageIDs) > 0:
		for _, id := range p.MessageIDs {
			if d.pending(p.ServerID, id) {
				return true
			}
		}
		return false
	}
	_, err := d.st.Server(p.ServerID)
	return err == nil
}

func imageExt(data []byte) string {
	switch ct := http.DetectContentType(data); {
	case ct == "image/jpeg":
		return ".jpg"
	case ct == "image/gif":
		return ".gif"
	case ct == "image/webp":
		return ".webp"
	case ct == "image/bmp":
		return ".bmp"
	case strings.Contains(string(data[:min(len(data), 512)]), "<svg"):
		return ".svg"
	default:
		return ".png"
	}
}

func hashHex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func (d *Dispatcher) writeIcon(serverID int64, a store.App) string {
	if len(a.Image) == 0 {
		return ""
	}
	dir := filepath.Join(d.cacheDir, "icons")
	path := filepath.Join(dir, fmt.Sprintf("s%d-a%d%s", serverID, a.ID, imageExt(a.Image)))
	sum := hashHex(a.Image)
	if d.icons[path] == sum {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	if err := os.WriteFile(path, a.Image, 0o644); err != nil {
		return ""
	}
	d.icons[path] = sum
	return path
}

// downloadImage returns a local path, or "" when the image cannot be fetched.
func (d *Dispatcher) downloadImage(url string) string {
	dir := filepath.Join(d.cacheDir, "images")
	ctx, cancel := context.WithTimeout(d.ctx, imageTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ""
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "image/") {
		return ""
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes+1))
	if err != nil || len(data) == 0 || len(data) > maxImageBytes {
		return ""
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	path := filepath.Join(dir, hashHex([]byte(url))[:24]+imageExt(data))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return ""
	}
	return path
}

func pruneDir(dir string, olderThan time.Duration) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > olderThan {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

type Kind int

const (
	KindUnknown Kind = iota
	KindMessage
	KindBurst
	KindMissed
)

var idPattern = regexp.MustCompile(`^s(\d+)(?:-m(\d+)|-a(\d+)-burst|-(missed))$`)

// ParseID decodes the IDs this package gives notifications. For KindBurst the
// appID is set, for KindMessage the messageID.
func ParseID(id string) (serverID int64, appID uint, messageID uint, kind Kind) {
	m := idPattern.FindStringSubmatch(id)
	if m == nil {
		return 0, 0, 0, KindUnknown
	}
	serverID, _ = strconv.ParseInt(m[1], 10, 64)
	switch {
	case m[2] != "":
		v, _ := strconv.ParseUint(m[2], 10, 64)
		return serverID, 0, uint(v), KindMessage
	case m[3] != "":
		v, _ := strconv.ParseUint(m[3], 10, 64)
		return serverID, uint(v), 0, KindBurst
	default:
		return serverID, 0, 0, KindMissed
	}
}

func (d *Dispatcher) Activated(id string) (serverID int64, appID uint, messageID uint, kind Kind) {
	return ParseID(id)
}
