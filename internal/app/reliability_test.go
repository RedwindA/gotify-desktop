package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"gotify-desktop/internal/secret"
	"gotify-desktop/internal/store"
)

type failingTokens struct {
	secret.Memory
	fail bool
}

func (s *failingTokens) Set(id int64, token string) error {
	if s.fail {
		return errors.New("token file is read-only")
	}
	return s.Memory.Set(id, token)
}

func loginServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var revoked atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/version":
			w.Write([]byte(`{"version":"2"}`))
		case r.URL.Path == "/client" && r.Method == "POST":
			w.Write([]byte(`{"id":9,"token":"new"}`))
		case r.URL.Path == "/current/user":
			w.Write([]byte(`{"id":1,"name":"alice"}`))
		case r.URL.Path == "/client/9" && r.Method == "DELETE":
			revoked.Add(1)
		default:
			w.WriteHeader(401)
		}
	}))
	t.Cleanup(s.Close)
	return s, &revoked
}

func TestAddServerCompensatesLocalFailures(t *testing.T) {
	for _, mode := range []string{"token", "database"} {
		t.Run(mode, func(t *testing.T) {
			srv, revoked := loginServer(t)
			tokens := &failingTokens{fail: mode == "token"}
			a, _, _ := newApp(t, tokens)
			if mode == "database" {
				a.st.Close()
			}
			if _, err := a.AddServer(context.Background(), ServerInput{URL: srv.URL, User: "alice", Pass: "pw"}); err == nil {
				t.Fatal("expected local write failure")
			}
			if revoked.Load() != 1 {
				t.Fatal("remote client leaked")
			}
			if mode == "token" {
				if svs, _ := a.st.Servers(); len(svs) != 0 {
					t.Fatal("local server leaked")
				}
			}
		})
	}
}

func TestReloginRestoresOldTokenWhenDatabaseWriteFails(t *testing.T) {
	srv, revoked := loginServer(t)
	tokens := &secret.Memory{}
	a, _, _ := newApp(t, tokens)
	sv := store.Server{URL: srv.URL, ClientID: 3, UserID: 1, UserName: "alice"}
	id, err := a.st.AddServer(sv)
	if err != nil {
		t.Fatal(err)
	}
	sv.ID = id
	tokens.Set(id, "old")
	// Keep this runtime disconnected to isolate the transaction failure.
	a.servers[id] = &runtime{sv: sv}
	a.st.Close()
	if err := a.Relogin(context.Background(), id, "alice", "pw"); err == nil {
		t.Fatal("expected database failure")
	}
	if token, _ := tokens.Get(id); token != "old" {
		t.Fatal("old token was overwritten")
	}
	if revoked.Load() != 1 {
		t.Fatal("remote replacement client leaked")
	}
	if a.servers[id].sv.ClientID != 3 {
		t.Fatal("runtime changed despite failure")
	}
}

func TestReadOperationsReturnStorageFailures(t *testing.T) {
	a, _, _ := newApp(t, &secret.Memory{})
	a.st.Close()
	if err := a.MarkRead(1, 1); err == nil {
		t.Fatal("MarkRead hid the failure")
	}
	if err := a.MarkAllRead(1, 0); err == nil {
		t.Fatal("MarkAllRead hid the failure")
	}
}
