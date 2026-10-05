// Command preview serves the built frontend to a browser, backed by the real
// service over demo data, for working on the layout and for screenshots
// (frontend/scripts/screenshots.ts). It stands in for the MyGo runtime with a
// small script: calls go over HTTP and events over server-sent events.
//
//	bun run --cwd frontend build && go run ./cmd/preview
package main

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"reflect"
	"strings"
	"sync"

	"encoding/json/jsontext"

	"gotify-desktop/internal/api"
	"gotify-desktop/internal/i18n"
)

const shim = `(() => {
  const listeners = new Map();
  const noop = async () => {};
  window.mygo = {
    platform: "linux", windowId: 1, version: "preview",
    window: { minimize: noop, maximize: noop, unmaximize: noop, toggleMaximize: noop, isMaximized: async () => false, toggleFullScreen: noop, close: noop, setTitle: noop },
    async call(method, ...args) {
      const res = await fetch("/__call", { method: "POST", body: JSON.stringify({ method, args }) });
      const body = await res.json();
      if (!res.ok) {
        const err = new Error(body.error);
        err.name = "CallError";
        err.method = method;
        throw err;
      }
      return body.result;
    },
    channel() { throw new Error("channels are not available in the preview"); },
    on(name, fn) {
      if (!listeners.has(name)) listeners.set(name, new Set());
      listeners.get(name).add(fn);
      return () => listeners.get(name).delete(fn);
    },
    once(name, fn) {
      const off = this.on(name, (p) => { off(); fn(p); });
      return off;
    },
  };
  new EventSource("/__events").onmessage = (e) => {
    const { name, payload } = JSON.parse(e.data);
    for (const fn of listeners.get(name) ?? []) fn(payload);
  };
})();
`

// events fans the service's events out to the connected pages.
type events struct {
	mu   sync.Mutex
	subs map[chan []byte]bool
}

func (e *events) send(name string, payload any) {
	data, err := json.Marshal(map[string]any{"name": name, "payload": payload})
	if err != nil {
		log.Print(err)
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for ch := range e.subs {
		select {
		case ch <- data:
		default:
		}
	}
}

func (e *events) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ch := make(chan []byte, 16)
	e.mu.Lock()
	e.subs[ch] = true
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		delete(e.subs, ch)
		e.mu.Unlock()
	}()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	w.(http.Flusher).Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case data := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", data)
			w.(http.Flusher).Flush()
		}
	}
}

var (
	ctxType = reflect.TypeFor[context.Context]()
	errType = reflect.TypeFor[error]()
)

// call runs a method of svc as MyGo does: a leading context, JSON arguments, and a result and an error.
func call(svc any, w http.ResponseWriter, r *http.Request) {
	var req struct {
		Method string           `json:"method"`
		Args   []jsontext.Value `json:"args"`
	}
	fail := func(status int, err error) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.MarshalWrite(w, map[string]string{"error": err.Error()})
	}
	if err := json.UnmarshalRead(r.Body, &req); err != nil {
		fail(http.StatusBadRequest, err)
		return
	}
	name, ok := strings.CutPrefix(req.Method, "Desktop.")
	m := reflect.ValueOf(svc).MethodByName(name)
	if !ok || !m.IsValid() {
		fail(http.StatusNotFound, fmt.Errorf("no method %s", req.Method))
		return
	}
	var in []reflect.Value
	args := req.Args
	for i := range m.Type().NumIn() {
		t := m.Type().In(i)
		if i == 0 && t == ctxType {
			in = append(in, reflect.ValueOf(r.Context()))
			continue
		}
		v := reflect.New(t)
		if len(args) > 0 {
			if err := json.Unmarshal(args[0], v.Interface()); err != nil {
				fail(http.StatusBadRequest, err)
				return
			}
			args = args[1:]
		}
		in = append(in, v.Elem())
	}
	var result any
	for _, out := range m.Call(in) {
		if out.Type() == errType {
			if !out.IsNil() {
				fail(http.StatusInternalServerError, out.Interface().(error))
				return
			}
			continue
		}
		result = out.Interface()
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.MarshalWrite(w, map[string]any{"result": result}); err != nil {
		log.Print(err)
	}
}

// spa serves the frontend, with the shim before its scripts and index.html for unknown paths.
func spa(dist fs.FS) http.Handler {
	files := http.FileServerFS(dist)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if _, err := fs.Stat(dist, p); p != "" && err == nil {
			files.ServeHTTP(w, r)
			return
		}
		f, err := dist.Open("index.html")
		if err != nil {
			http.Error(w, "build the frontend first: bun run --cwd frontend build", http.StatusNotFound)
			return
		}
		defer f.Close()
		html, _ := io.ReadAll(f)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(bytes.Replace(html, []byte("<head>"), []byte("<head><script src=\"/__mygo.js\"></script>"), 1))
	})
}

func main() {
	addr := flag.String("addr", "127.0.0.1:5174", "address to listen on")
	dist := flag.String("dist", "frontend/dist", "the built frontend")
	empty := flag.Bool("empty", false, "start without servers")
	lang := flag.String("lang", "en", "the system language the demo follows, such as zh-CN")
	flag.Parse()
	i18n.SetSystem(*lang)

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	base := "http://" + ln.Addr().String()
	be := api.Demo(base + "/chart.png")
	if *empty {
		be = api.NewFakeBackend()
	}
	ev := &events{subs: map[chan []byte]bool{}}
	ctrl := api.New(api.Platform{
		Version: "0.1.0",
		OpenURL: func(u string) { log.Printf("open %s", u) },
		PickCA: func(context.Context) (string, []byte, error) {
			return "", nil, nil
		},
	})
	ctrl.Redirect(func(s api.State) { ev.send("state", s) }, func(n api.Navigation) { ev.send("navigate", n) })
	ctrl.Start(be, "~/.config/Gotify Desktop")
	be.OnChange = ctrl.Changed

	mux := http.NewServeMux()
	mux.HandleFunc("POST /__call", func(w http.ResponseWriter, r *http.Request) { call(ctrl.Service(), w, r) })
	mux.Handle("GET /__events", ev)
	mux.HandleFunc("GET /__mygo.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		io.WriteString(w, shim)
	})
	mux.HandleFunc("GET /chart.png", func(w http.ResponseWriter, r *http.Request) { w.Write(api.DemoChart()) })
	mux.Handle("GET /", spa(os.DirFS(*dist)))
	fmt.Println(base)
	log.Fatal(http.Serve(ln, mux))
}
