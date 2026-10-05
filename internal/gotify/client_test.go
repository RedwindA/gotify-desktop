package gotify

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func newServer(t *testing.T, prefix string, h http.HandlerFunc) *Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle(prefix+"/", http.StripPrefix(prefix, h))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, err := New(srv.URL+prefix+"/", "tok", Options{})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestSubPathAuthAndMessages(t *testing.T) {
	c := newServer(t, "/gotify", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Gotify-Key") != "tok" || r.URL.Path != "/message" {
			t.Errorf("bad request %s %s key=%q", r.Method, r.URL, r.Header.Get("X-Gotify-Key"))
		}
		if r.URL.Query().Get("limit") != "50" || r.URL.Query().Get("since") != "9" {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		w.Write([]byte(`{"paging":{"size":2,"limit":50,"since":7,"next":"/message?limit=50&since=7"},"messages":[
{"id":8,"appid":1,"message":"a","title":"t","priority":null,"date":"2024-01-02T03:04:05Z"},
{"id":7,"appid":2,"message":"b","title":"","priority":5,"extras":{"k::v":{"x":1}},"date":"2024-01-02T03:04:05Z"}]}`))
	})
	p, err := c.Messages(context.Background(), 50, 9)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Messages) != 2 || p.Messages[0].Priority != 0 || p.Messages[1].Priority != 5 || p.Paging.Since != 7 || p.Paging.Next == "" {
		t.Fatalf("%+v", p)
	}
	if p.Messages[1].Extras["k::v"] == nil || p.Messages[0].Date.Year() != 2024 {
		t.Fatalf("%+v", p.Messages)
	}
}

func TestMessagesSinceZeroOmitted(t *testing.T) {
	c := newServer(t, "", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("since") {
			t.Errorf("since sent: %s", r.URL.RawQuery)
		}
		w.Write([]byte(`{"paging":{},"messages":[]}`))
	})
	if _, err := c.Messages(context.Background(), 10, 0); err != nil {
		t.Fatal(err)
	}
}

func TestErrors(t *testing.T) {
	c := newServer(t, "", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/current/user":
			w.WriteHeader(401)
		case "/message/5":
			w.WriteHeader(404)
		case "/client/3":
			w.WriteHeader(403)
			w.Write([]byte(`{"error":"Forbidden"}`))
		}
	})
	ctx := context.Background()
	if _, err := c.CurrentUser(ctx); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("got %v", err)
	}
	if err := c.DeleteMessage(ctx, 5); err != nil {
		t.Fatalf("404 should be nil, got %v", err)
	}
	var he *HTTPError
	if err := c.DeleteClient(ctx, 3); !errors.As(err, &he) || he.Status != 403 {
		t.Fatalf("got %v", err)
	}
}

func TestLoginVersionApplications(t *testing.T) {
	c := newServer(t, "", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/client":
			u, p, _ := r.BasicAuth()
			if r.Method != "POST" || u != "admin" || p != "pw" {
				w.WriteHeader(401)
				return
			}
			w.Write([]byte(`{"id":12,"token":"CTOKEN","name":"desk"}`))
		case "/version":
			w.Write([]byte(`{"version":"2.9.0","commit":"c","buildDate":"d"}`))
		case "/application":
			w.Write([]byte(`[{"id":1,"name":"A","description":"d","internal":true,"image":"image/a.png","defaultPriority":4,"sortKey":"a0","lastUsed":null}]`))
		case "/image/a.png":
			w.Write([]byte("PNG"))
		}
	})
	ctx := context.Background()
	base := c.base.String()
	tok, id, err := Login(ctx, base, "admin", "pw", "desk", Options{})
	if err != nil || tok != "CTOKEN" || id != 12 {
		t.Fatalf("%q %d %v", tok, id, err)
	}
	if _, _, err := Login(ctx, base, "admin", "bad", "desk", Options{}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("got %v", err)
	}
	if v, err := c.Version(ctx); err != nil || v.Version != "2.9.0" {
		t.Fatalf("%+v %v", v, err)
	}
	apps, err := c.Applications(ctx)
	if err != nil || len(apps) != 1 || apps[0].DefaultPriority != 4 || !apps[0].Internal {
		t.Fatalf("%+v %v", apps, err)
	}
	if b, err := c.Image(ctx, apps[0].Image); err != nil || string(b) != "PNG" {
		t.Fatalf("%q %v", b, err)
	}
}

func TestDial(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sub/stream" || r.Header.Get("X-Gotify-Key") != "good" {
			w.WriteHeader(401)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		conn.Write(r.Context(), websocket.MessageText, []byte(`{"id":3,"appid":1,"message":"hi","priority":null,"date":"2024-01-02T03:04:05Z"}`))
		conn.Close(websocket.StatusNormalClosure, "")
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	bad, _ := New(srv.URL+"/sub", "bad", Options{})
	if _, err := bad.Dial(ctx); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("got %v", err)
	}
	good, _ := New(srv.URL+"/sub", "good", Options{})
	s, err := good.Dial(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Abort()
	m, err := s.Read(ctx)
	if err != nil || m.ID != 3 || m.Message != "hi" {
		t.Fatalf("%+v %v", m, err)
	}
}

func TestInvalidOptions(t *testing.T) {
	if _, err := New("ftp://x", "", Options{}); err == nil {
		t.Fatal("want error for bad scheme")
	}
	if _, err := New("http://x", "", Options{CACertPEM: []byte("junk")}); err == nil {
		t.Fatal("want error for bad PEM")
	}
}
