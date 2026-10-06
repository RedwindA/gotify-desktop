package notify

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gotify-desktop/internal/conn"
	"gotify-desktop/internal/gotify"
	"gotify-desktop/internal/i18n"
	"gotify-desktop/internal/store"
)

const (
	notificationMaxAttempts = 5
	notificationKeepFor     = 24 * time.Hour
	imageTimeout            = 5 * time.Second
	maxImageBytes           = 5 << 20
	imageKeepFor            = 24 * time.Hour
)

// Dispatcher consumes durable notification jobs. Handle only wakes the worker;
// a coalesced wake cannot lose work, and startup resumes unfinished jobs.
type Dispatcher struct {
	n        Notifier
	st       *store.Store
	settings func() Settings
	cacheDir string
	http     *http.Client
	planner  *Planner

	wake   chan struct{}
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
		wake: make(chan struct{}, 1), done: make(chan struct{}), ctx: ctx, cancel: cancel, icons: map[string]string{},
	}
	pruneDir(filepath.Join(cacheDir, "images"), imageKeepFor)
	go d.run()
	return d
}

// Handle never blocks; messages and notification jobs were already committed.
func (d *Dispatcher) Handle(ev conn.Event) {
	if ev.Kind != conn.EventMessages || ev.Silent {
		return
	}
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

func (d *Dispatcher) Close() { d.cancel(); <-d.done }

func (d *Dispatcher) run() {
	defer close(d.done)
	timer := time.NewTicker(time.Second)
	defer timer.Stop()
	for d.ctx.Err() == nil {
		job, err := d.st.NextNotification(time.Now())
		if err == nil && job != nil {
			if time.Since(job.CreatedAt) > notificationKeepFor || job.Attempts >= notificationMaxAttempts {
				log.Printf("notify: expiring job %d after %d attempts", job.ID, job.Attempts)
				err = d.st.FinishNotification(job.ID)
			} else {
				err = d.process(job)
				if err != nil && d.ctx.Err() == nil {
					log.Printf("notify: job %d: %v", job.ID, err)
					err = d.st.RetryNotification(job.ID, time.Now().Add(time.Second<<job.Attempts))
				}
			}
			if err == nil {
				continue
			}
		}
		if err != nil {
			log.Printf("notify: outbox: %v", err)
		}
		select {
		case <-d.ctx.Done():
			return
		case <-d.wake:
		case <-timer.C:
		}
	}
}

func (d *Dispatcher) process(job *store.NotificationJob) error {
	storedApps, err := d.st.Apps(job.ServerID)
	if err != nil {
		return err
	}
	apps := make(map[uint]store.App, len(storedApps))
	for _, a := range storedApps {
		apps[a.ID] = a
	}
	prefs := map[uint]AppPrefs{}
	for _, m := range job.Messages {
		if _, ok := prefs[m.AppID]; ok {
			continue
		}
		p, err := d.st.GetAppPref(job.ServerID, m.AppID)
		if err != nil {
			return err
		}
		prefs[m.AppID] = AppPrefs{Muted: p.Muted, MinPriority: p.MinPriority}
	}
	var plans []Planned
	checkpoint := func() error {
		b, err := json.Marshal(plans)
		if err != nil {
			return err
		}
		return d.st.SetNotificationPlans(job.ID, b)
	}
	if job.Plans != nil {
		if err := json.Unmarshal(job.Plans, &plans); err != nil {
			return err
		}
	} else {
		var fresh []gotify.Message
		for _, m := range job.Messages {
			if pending, err := d.pending(job.ServerID, m.ID); err != nil {
				return err
			} else if pending {
				fresh = append(fresh, m)
			}
		}
		nextPlanner := d.planner.clone()
		plans = nextPlanner.Plan(conn.Event{Kind: conn.EventMessages, ServerID: job.ServerID, Messages: fresh, CatchUp: job.CatchUp}, apps, prefs, d.settings(), time.Now())
		if err := checkpoint(); err != nil {
			return err
		}
		d.planner = nextPlanner
	}
	for len(plans) > 0 {
		if err := d.ctx.Err(); err != nil {
			return err
		}
		p, relevant, err := d.refreshPlan(plans[0], apps)
		if err != nil {
			return err
		}
		if relevant {
			icon := ""
			if a, ok := apps[p.AppID]; ok && p.AppID != 0 {
				icon = d.writeIcon(job.ServerID, a)
			}
			image := ""
			if p.ImageURL != "" {
				image = d.downloadImage(p.ImageURL)
			}
			if err := d.ctx.Err(); err != nil {
				return err
			}
			p, relevant, err = d.refreshPlan(p, apps)
			if err != nil {
				return err
			}
			if relevant {
				p.IconPath, p.ImagePath = icon, image
				if err := d.n.Show(p.Notification); err != nil && !errors.Is(err, ErrUnsupported) {
					return err
				}
			}
		}
		plans = plans[1:]
		if err := checkpoint(); err != nil {
			return err
		}
	}
	return d.st.FinishNotification(job.ID)
}

func (d *Dispatcher) pending(serverID int64, id uint) (bool, error) {
	msgs, err := d.st.Messages(store.MessageQuery{ServerID: serverID, ID: id, Limit: 1})
	return len(msgs) == 1 && !msgs[0].Read, err
}

// Re-read both policy and message state, including on retry. Summaries must not
// keep text from a message that has since been read, deleted or muted.
func (d *Dispatcher) refreshPlan(p Planned, apps map[uint]store.App) (Planned, bool, error) {
	s, now := d.settings(), time.Now()
	if now.Before(s.PausedUntil) {
		return p, false, nil
	}
	ids := p.MessageIDs
	if p.MessageID != 0 {
		ids = []uint{p.MessageID}
	}
	var fresh []gotify.Message
	for _, id := range ids {
		ms, err := d.st.Messages(store.MessageQuery{ServerID: p.ServerID, ID: id, Limit: 1})
		if err != nil {
			return p, false, err
		}
		if len(ms) == 0 || ms[0].Read {
			continue
		}
		m := ms[0].Message
		pref, err := d.st.GetAppPref(p.ServerID, m.AppID)
		if err != nil {
			return p, false, err
		}
		level, show := LevelFor(m.Priority)
		if !show || pref.Muted || pref.MinPriority != nil && m.Priority < *pref.MinPriority {
			continue
		}
		if s.dndActive(now) && !(level == LevelHigh && s.HighBypassesDND) {
			continue
		}
		fresh = append(fresh, m)
	}
	if len(fresh) == 0 {
		return p, false, nil
	}
	if p.MessageID == 0 {
		updated := missedSummary(p.ServerID, fresh, apps)
		updated.ID, updated.Group, updated.AppID = p.ID, p.Group, p.AppID
		if p.AppID != 0 {
			updated.Title = i18n.T("%d new messages from %s", len(fresh), appName(apps, p.AppID))
		}
		p = updated
	}
	return p, true, nil
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

// FetchImage downloads the image at url, of at most 5 MB.
func FetchImage(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "image/") {
		return nil, fmt.Errorf("not an image: %q", ct)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes+1))
	switch {
	case err != nil:
		return nil, err
	case len(data) == 0:
		return nil, errors.New("empty image")
	case len(data) > maxImageBytes:
		return nil, errors.New("image larger than 5 MB")
	}
	return data, nil
}

// downloadImage returns a local path, or "" when the image cannot be fetched.
func (d *Dispatcher) downloadImage(url string) string {
	dir := filepath.Join(d.cacheDir, "images")
	ctx, cancel := context.WithTimeout(d.ctx, imageTimeout)
	defer cancel()
	data, err := FetchImage(ctx, d.http, url)
	if err != nil {
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
