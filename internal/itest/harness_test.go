package itest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gotify-desktop/internal/conn"
	"gotify-desktop/internal/gotify"
)

const (
	adminUser = "admin"
	adminPass = "itpass"
)

var (
	buildOnce sync.Once
	serverBin string
	buildErr  error
)

func requireIT(t *testing.T) {
	t.Helper()
	if os.Getenv("GOTIFY_IT") != "1" {
		t.Skip("set GOTIFY_IT=1 to run integration tests against the real gotify server")
	}
}

func buildServer() (string, error) {
	buildOnce.Do(func() {
		src := os.Getenv("GOTIFY_SERVER_SRC")
		if src == "" {
			src = "/home/austin/server"
		}
		cache, err := os.UserCacheDir()
		if err != nil {
			buildErr = err
			return
		}
		dir := filepath.Join(cache, "gotify-desktop-it")
		if buildErr = os.MkdirAll(dir, 0o755); buildErr != nil {
			return
		}
		serverBin = filepath.Join(dir, "gotify-server")
		placeholder := filepath.Join(dir, "placeholder")
		overlay := filepath.Join(dir, "overlay.json")
		abs, _ := filepath.Abs(src)
		replace := map[string]string{}
		for _, f := range []string{"index.html", "manifest.json"} {
			replace[filepath.Join(abs, "ui", "build", f)] = placeholder
		}
		o, _ := json.Marshal(map[string]any{"Replace": replace})
		if buildErr = os.WriteFile(placeholder, []byte("{}"), 0o644); buildErr != nil {
			return
		}
		if buildErr = os.WriteFile(overlay, o, 0o644); buildErr != nil {
			return
		}
		cmd := exec.Command("go", "build", "-overlay", overlay, "-o", serverBin, ".")
		cmd.Dir = src
		cmd.Env = append(os.Environ(), "CGO_ENABLED=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			buildErr = fmt.Errorf("build gotify server: %v\n%s", err, out)
		}
	})
	return serverBin, buildErr
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

type server struct {
	t    *testing.T
	dir  string
	port int
	cmd  *exec.Cmd
	done chan struct{}
}

func startServer(t *testing.T) *server {
	t.Helper()
	bin, err := buildServer()
	if err != nil {
		t.Fatal(err)
	}
	s := &server{t: t, dir: t.TempDir(), port: freePort(t)}
	s.start(bin)
	t.Cleanup(s.stop)
	return s
}

func (s *server) start(bin string) {
	t := s.t
	cmd := exec.Command(bin)
	cmd.Dir = s.dir
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("GOTIFY_SERVER_PORT=%d", s.port),
		"GOTIFY_SERVER_LISTENADDR=127.0.0.1",
		"GOTIFY_DATABASE_CONNECTION="+filepath.Join(s.dir, "gotify.db"),
		"GOTIFY_UPLOADEDIMAGESDIR="+filepath.Join(s.dir, "images"),
		"GOTIFY_PLUGINSDIR="+filepath.Join(s.dir, "plugins"),
		"GOTIFY_DEFAULTUSER_NAME="+adminUser,
		"GOTIFY_DEFAULTUSER_PASS="+adminPass,
		"GOTIFY_PASSSTRENGTH=4",
		"GOTIFY_LOGLEVEL=warn",
	)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	s.cmd, s.done = cmd, make(chan struct{})
	go func() { cmd.Wait(); close(s.done) }()
	deadline := time.Now().Add(15 * time.Second)
	for {
		resp, err := http.Get(s.URL() + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return
			}
		}
		select {
		case <-s.done:
			t.Fatal("gotify server exited during startup")
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("gotify server did not become healthy")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (s *server) stop() {
	if s.cmd == nil || s.cmd.Process == nil {
		return
	}
	s.cmd.Process.Kill()
	<-s.done
	s.cmd = nil
}

func (s *server) restart() {
	s.stop()
	s.start(serverBin)
}

func (s *server) URL() string { return fmt.Sprintf("http://127.0.0.1:%d", s.port) }

func (s *server) api(method, path string, body, out any) {
	s.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, s.URL()+path, r)
	req.SetBasicAuth(adminUser, adminPass)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		s.t.Fatalf("%s %s: %d %s", method, path, resp.StatusCode, b)
	}
	if out != nil {
		json.NewDecoder(resp.Body).Decode(out)
	}
}

type appToken string

func (s *server) newApp(name string) appToken {
	s.t.Helper()
	var app struct{ Token string }
	s.api("POST", "/application", map[string]string{"name": name}, &app)
	return appToken(app.Token)
}

func (s *server) post(app appToken, n int, prefix string) {
	s.t.Helper()
	for i := 1; i <= n; i++ {
		req, _ := http.NewRequest("POST", s.URL()+"/message", bytes.NewReader([]byte(fmt.Sprintf(`{"title":"%s","message":"%s-%d","priority":5}`, prefix, prefix, i))))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Gotify-Key", string(app))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			s.t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			s.t.Fatalf("post message: %d", resp.StatusCode)
		}
	}
}

// proxy is a TCP forwarder that can refuse new connections or silently swallow traffic.
type proxy struct {
	ln        net.Listener
	target    string
	mu        sync.Mutex
	refuse    bool
	blackhole bool
	conns     map[net.Conn]bool
	held      map[net.Conn]bool
	accepted  int
}

func startProxy(t *testing.T, target string) *proxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &proxy{ln: ln, target: target, conns: map[net.Conn]bool{}, held: map[net.Conn]bool{}}
	t.Cleanup(func() { ln.Close(); p.closeAll(nil) })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go p.handle(c)
		}
	}()
	return p
}

func (p *proxy) URL() string { return "http://" + p.ln.Addr().String() }

func (p *proxy) Accepted() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.accepted
}

func (p *proxy) handle(c net.Conn) {
	p.mu.Lock()
	p.accepted++
	if p.refuse {
		p.mu.Unlock()
		c.Close()
		return
	}
	up, err := net.Dial("tcp", p.target)
	if err != nil {
		p.mu.Unlock()
		c.Close()
		return
	}
	p.conns[c], p.conns[up] = true, true
	if p.blackhole {
		p.held[c], p.held[up] = true, true
	}
	p.mu.Unlock()
	pipe := func(dst, src net.Conn) {
		buf := make([]byte, 32<<10)
		for {
			n, err := src.Read(buf)
			if n > 0 {
				p.mu.Lock()
				drop := p.blackhole
				p.mu.Unlock()
				if !drop {
					if _, werr := dst.Write(buf[:n]); werr != nil {
						err = werr
					}
				}
			}
			if err != nil {
				c.Close()
				up.Close()
				return
			}
		}
	}
	go pipe(up, c)
	pipe(c, up)
}

func (p *proxy) closeAll(only map[net.Conn]bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for c := range p.conns {
		if only == nil || only[c] {
			c.Close()
			delete(p.conns, c)
		}
	}
}

// Refuse drops every connection and rejects new ones while on.
func (p *proxy) Refuse(on bool) {
	p.mu.Lock()
	p.refuse = on
	p.mu.Unlock()
	if on {
		p.closeAll(nil)
	}
}

// Blackhole keeps sockets open but forwards nothing while on. Connections opened
// during the blackhole are closed when it ends, as their handshake bytes were lost.
func (p *proxy) Blackhole(on bool) {
	p.mu.Lock()
	p.blackhole = on
	held := p.held
	if on {
		p.held = map[net.Conn]bool{}
	}
	p.mu.Unlock()
	if !on {
		p.closeAll(held)
	}
}

type recorder struct {
	mu     sync.Mutex
	events []conn.Event
	sig    chan struct{}
}

func newRecorder() *recorder { return &recorder{sig: make(chan struct{}, 1)} }

func (r *recorder) sink(e conn.Event) {
	r.mu.Lock()
	r.events = append(r.events, e)
	r.mu.Unlock()
	select {
	case r.sig <- struct{}{}:
	default:
	}
}

func (r *recorder) all() []conn.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]conn.Event(nil), r.events...)
}

func (r *recorder) wait(t *testing.T, from int, pred func(conn.Event) bool) (conn.Event, int) {
	t.Helper()
	deadline := time.After(20 * time.Second)
	for {
		evs := r.all()
		for i := from; i < len(evs); i++ {
			if pred(evs[i]) {
				return evs[i], i + 1
			}
		}
		select {
		case <-r.sig:
		case <-deadline:
			t.Fatalf("timeout waiting for event; got %+v", evs[from:])
		}
	}
}

func state(s conn.State) func(conn.Event) bool {
	return func(e conn.Event) bool { return e.Kind == conn.EventState && e.State == s }
}

func msgs(e conn.Event) bool { return e.Kind == conn.EventMessages }

func messageBodies(ms []gotify.Message) (out []string) {
	for _, m := range ms {
		out = append(out, m.Message)
	}
	return
}

func login(t *testing.T, url string) (*gotify.Client, uint) {
	t.Helper()
	tok, id, err := gotify.Login(context.Background(), url, adminUser, adminPass, "it-client", gotify.Options{})
	if err != nil {
		t.Fatal(err)
	}
	c, err := gotify.New(url, tok, gotify.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return c, id
}
