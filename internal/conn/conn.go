package conn

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"sync"
	"time"

	"gotify-desktop/internal/gotify"
	"gotify-desktop/internal/store"
)

type State int

const (
	Disconnected State = iota
	Connecting
	Connected
	Backoff
	AuthFailed
	Stopped
)

func (s State) String() string {
	return [...]string{"disconnected", "connecting", "connected", "backoff", "auth-failed", "stopped"}[s]
}

type EventKind int

const (
	EventMessages EventKind = iota
	EventState
	EventApps
)

type Event struct {
	ServerID int64
	Kind     EventKind
	Messages []gotify.Message
	CatchUp  bool
	Silent   bool
	State    State
	Err      error
	RetryAt  time.Time
}

type Config struct {
	PingInterval         time.Duration
	PongTimeout          time.Duration
	MinBackoff           time.Duration
	MaxBackoff           time.Duration
	StableAfter          time.Duration
	ImportOnFirstConnect int
	// PageSize is how many messages a catch-up requests at once (100).
	PageSize int
}

func (c Config) withDefaults() Config {
	def := func(d *time.Duration, v time.Duration) {
		if *d <= 0 {
			*d = v
		}
	}
	def(&c.PingInterval, 30*time.Second)
	def(&c.PongTimeout, 15*time.Second)
	def(&c.MinBackoff, time.Second)
	def(&c.MaxBackoff, 5*time.Minute)
	def(&c.StableAfter, time.Minute)
	if c.PageSize <= 0 {
		c.PageSize = pageSize
	}
	if c.ImportOnFirstConnect <= 0 {
		c.ImportOnFirstConnect = 200
	}
	return c
}

type Store interface {
	LastSeen(serverID int64) (uint, bool, error)
	SaveMessages(serverID int64, msgs []gotify.Message) ([]gotify.Message, error)
	SaveInitialImport(serverID int64, live, history []gotify.Message, floor uint) (insertedLive, insertedHistory []gotify.Message, err error)
	ImportFloor(serverID int64) (uint, error)
	ReplaceApps(serverID int64, apps []gotify.Application) error
	Apps(serverID int64) ([]store.App, error)
	SetAppImage(serverID int64, appID uint, img []byte) error
}

const (
	pageSize       = 100
	catchUpOverlap = 20
	dialTimeout    = 15 * time.Second
	unknownAppWait = time.Minute
)

var (
	errKicked      = errors.New("conn: kicked")
	errPongTimeout = errors.New("conn: pong timeout")
)

type Supervisor struct {
	id   int64
	st   Store
	sink func(Event)
	cfg  Config

	ctx    context.Context
	cancel context.CancelFunc
	kick   chan struct{}
	done   chan struct{}
	start  sync.Once

	mu     sync.Mutex
	client *gotify.Client
	state  State

	known     map[uint]bool
	missingAt map[uint]time.Time
}

func New(serverID int64, client *gotify.Client, st Store, sink func(Event), cfg Config) *Supervisor {
	ctx, cancel := context.WithCancel(context.Background())
	if sink == nil {
		sink = func(Event) {}
	}
	return &Supervisor{
		id: serverID, st: st, sink: sink, cfg: cfg.withDefaults(), client: client,
		ctx: ctx, cancel: cancel, kick: make(chan struct{}, 1), done: make(chan struct{}),
		known: map[uint]bool{}, missingAt: map[uint]time.Time{},
	}
}

func (s *Supervisor) Start() { s.start.Do(func() { go s.run() }) }

// Stop ends the supervisor and waits for it; the sink is never called afterwards.
// It must not be called from the sink.
func (s *Supervisor) Stop() {
	s.start.Do(func() { close(s.done) })
	s.cancel()
	<-s.done
	s.mu.Lock()
	s.state = Stopped
	s.mu.Unlock()
}

// Kick reconnects now, resets the backoff and leaves AuthFailed.
func (s *Supervisor) Kick() {
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

func (s *Supervisor) SetClient(c *gotify.Client) {
	s.mu.Lock()
	s.client = c
	s.mu.Unlock()
}

func (s *Supervisor) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

func (s *Supervisor) getClient() *gotify.Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.client
}

func (s *Supervisor) setState(st State, err error, retryAt time.Time) {
	s.mu.Lock()
	s.state = st
	s.mu.Unlock()
	s.sink(Event{ServerID: s.id, Kind: EventState, State: st, Err: err, RetryAt: retryAt})
}

func (s *Supervisor) jitter(d time.Duration) time.Duration {
	return time.Duration(float64(d) * (0.8 + 0.4*rand.Float64()))
}

func (s *Supervisor) waitKick(d time.Duration) (kicked bool) {
	var timer <-chan time.Time
	if d >= 0 {
		t := time.NewTimer(d)
		defer t.Stop()
		timer = t.C
	}
	select {
	case <-s.kick:
		return true
	case <-timer:
		return false
	case <-s.ctx.Done():
		return false
	}
}

func (s *Supervisor) run() {
	defer close(s.done)
	defer func() { s.setState(Stopped, nil, time.Time{}) }()
	backoff := s.cfg.MinBackoff
	for s.ctx.Err() == nil {
		s.setState(Connecting, nil, time.Time{})
		connectedAt, err := s.session()
		if s.ctx.Err() != nil {
			return
		}
		switch {
		case errors.Is(err, errKicked):
			backoff = s.cfg.MinBackoff
			continue
		case errors.Is(err, gotify.ErrUnauthorized):
			s.setState(AuthFailed, err, time.Time{})
			s.waitKick(-1)
			backoff = s.cfg.MinBackoff
			continue
		}
		if !connectedAt.IsZero() && time.Since(connectedAt) >= s.cfg.StableAfter {
			backoff = s.cfg.MinBackoff
		}
		d := s.jitter(backoff)
		backoff = min(backoff*2, s.cfg.MaxBackoff)
		s.setState(Backoff, err, time.Now().Add(d))
		if s.waitKick(d) {
			backoff = s.cfg.MinBackoff
		}
	}
}

type queue struct {
	mu   sync.Mutex
	msgs []gotify.Message
	wake chan struct{}
}

func (q *queue) push(m gotify.Message) {
	q.mu.Lock()
	q.msgs = append(q.msgs, m)
	q.mu.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *queue) take() []gotify.Message {
	q.mu.Lock()
	defer q.mu.Unlock()
	m := q.msgs
	q.msgs = nil
	return m
}

// session runs one connection until it dies, returning why. The stream is dialed
// before the catch-up so no message falls in the gap; live messages queue meanwhile.
func (s *Supervisor) session() (connectedAt time.Time, err error) {
	client := s.getClient()
	ctx, cancel := context.WithCancelCause(s.ctx)
	var wg sync.WaitGroup
	var stream *gotify.Stream
	defer func() {
		cancel(nil)
		if stream != nil {
			stream.Abort()
		}
		wg.Wait()
		if cause := context.Cause(ctx); err != nil && cause != nil && !errors.Is(cause, context.Canceled) {
			err = cause
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case <-s.kick:
			if ctx.Err() != nil {
				s.Kick()
				return
			}
			cancel(errKicked)
		case <-ctx.Done():
		}
	}()

	dctx, dcancel := context.WithTimeout(ctx, dialTimeout)
	stream, err = client.Dial(dctx)
	dcancel()
	if err != nil {
		stream = nil
		return time.Time{}, err
	}
	connectedAt = time.Now()
	s.setState(Connected, nil, time.Time{})

	q := &queue{wake: make(chan struct{}, 1)}
	wg.Add(2)
	go func() {
		defer wg.Done()
		for {
			m, err := stream.Read(ctx)
			if err != nil {
				cancel(fmt.Errorf("stream read: %w", err))
				return
			}
			q.push(m)
		}
	}()
	go func() {
		defer wg.Done()
		t := time.NewTicker(s.cfg.PingInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			pctx, pcancel := context.WithTimeout(ctx, s.cfg.PongTimeout)
			err := stream.Ping(pctx)
			pcancel()
			if err != nil && ctx.Err() == nil {
				cancel(errPongTimeout)
				return
			}
		}
	}()

	if err = s.syncApps(ctx, client); err != nil {
		return connectedAt, err
	}
	if err = s.catchUp(ctx, client, q); err != nil {
		return connectedAt, err
	}
	for {
		select {
		case <-ctx.Done():
			return connectedAt, context.Cause(ctx)
		case <-q.wake:
		}
		if msgs := q.take(); len(msgs) > 0 {
			if err = s.deliver(ctx, client, msgs, false, false); err != nil {
				return connectedAt, err
			}
		}
	}
}

func (s *Supervisor) syncApps(ctx context.Context, client *gotify.Client) error {
	apps, err := client.Applications(ctx)
	if err != nil {
		return err
	}
	if err := s.st.ReplaceApps(s.id, apps); err != nil {
		return err
	}
	s.known = make(map[uint]bool, len(apps))
	for _, a := range apps {
		s.known[a.ID] = true
	}
	s.fetchImages(ctx, client)
	s.sink(Event{ServerID: s.id, Kind: EventApps})
	return nil
}

func (s *Supervisor) fetchImages(ctx context.Context, client *gotify.Client) {
	apps, err := s.st.Apps(s.id)
	if err != nil {
		return
	}
	for _, a := range apps {
		if a.ImagePath == "" || a.Image != nil || ctx.Err() != nil {
			continue
		}
		if img, err := client.Image(ctx, a.ImagePath); err == nil && len(img) > 0 {
			s.st.SetAppImage(s.id, a.ID, img)
		}
	}
}

func (s *Supervisor) catchUp(ctx context.Context, client *gotify.Client, q *queue) error {
	last, initialized, err := s.st.LastSeen(s.id)
	if err != nil {
		return err
	}
	var got []gotify.Message
	var cursor uint
	below := 0 // messages at or below last_seen collected so far
	for {
		page, err := client.Messages(ctx, s.cfg.PageSize, cursor)
		if err != nil {
			return err
		}
		done := false
		// The server broadcasts concurrently with committing, so a message below
		// last_seen may never have been delivered: keep going until catchUpOverlap
		// messages at or below it are in hand. SaveMessages drops the stored ones.
		for _, m := range page.Messages {
			if initialized && m.ID <= last {
				below++
			}
			got = append(got, m)
		}
		if initialized && below >= catchUpOverlap {
			done = true
		}
		if !initialized && len(got) >= s.cfg.ImportOnFirstConnect {
			got = got[:s.cfg.ImportOnFirstConnect]
			done = true
		}
		if done || page.Paging.Next == "" || page.Paging.Since == 0 || (cursor != 0 && page.Paging.Since >= cursor) {
			break
		}
		cursor = page.Paging.Since
	}
	slices.Reverse(got)
	if initialized {
		floor, err := s.st.ImportFloor(s.id)
		if err != nil {
			return err
		}
		got = slices.DeleteFunc(got, func(m gotify.Message) bool { return m.ID < floor })
		if len(got) == 0 {
			return nil
		}
		return s.deliver(ctx, client, got, true, false)
	}
	// Messages that arrived while the history loaded are news, not history; both are
	// saved together so a failed save leaves the server uninitialized.
	live := q.take()
	if err := s.syncUnknownApps(ctx, client, slices.Concat(live, got)); err != nil {
		return err
	}
	var floor uint
	if len(got) > 0 {
		floor = got[0].ID
	}
	insLive, insHist, err := s.st.SaveInitialImport(s.id, live, got, floor)
	if err != nil {
		return err
	}
	if len(insLive) > 0 {
		s.sink(Event{ServerID: s.id, Kind: EventMessages, Messages: insLive})
	}
	if len(insHist) > 0 {
		s.sink(Event{ServerID: s.id, Kind: EventMessages, Messages: insHist, CatchUp: true, Silent: true})
	}
	return nil
}

// syncUnknownApps refreshes the applications when msgs name one that is not known yet.
func (s *Supervisor) syncUnknownApps(ctx context.Context, client *gotify.Client, msgs []gotify.Message) error {
	if !s.hasUnknownApp(msgs) {
		return nil
	}
	if err := s.syncApps(ctx, client); errors.Is(err, gotify.ErrUnauthorized) {
		return err
	}
	now := time.Now()
	for _, m := range msgs {
		if !s.known[m.AppID] {
			s.missingAt[m.AppID] = now
		}
	}
	return nil
}

func (s *Supervisor) deliver(ctx context.Context, client *gotify.Client, msgs []gotify.Message, catchUp, silent bool) error {
	if err := s.syncUnknownApps(ctx, client, msgs); err != nil {
		return err
	}
	inserted, err := s.st.SaveMessages(s.id, msgs)
	if err != nil {
		return err
	}
	if len(inserted) > 0 {
		s.sink(Event{ServerID: s.id, Kind: EventMessages, Messages: inserted, CatchUp: catchUp, Silent: silent})
	}
	return nil
}

func (s *Supervisor) hasUnknownApp(msgs []gotify.Message) bool {
	for _, m := range msgs {
		if s.known[m.AppID] {
			continue
		}
		if t, ok := s.missingAt[m.AppID]; ok && time.Since(t) < unknownAppWait {
			continue
		}
		return true
	}
	return false
}
