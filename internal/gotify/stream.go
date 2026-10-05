package gotify

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/coder/websocket"
)

type Stream struct{ conn *websocket.Conn }

// Dial opens the message stream. Ping needs a concurrent Read loop to see pongs.
func (c *Client) Dial(ctx context.Context) (*Stream, error) {
	u := *c.base
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	default:
		u.Scheme = "ws"
	}
	u.Path = c.base.Path + "/stream"
	h := http.Header{"X-Gotify-Key": {c.token}}
	conn, resp, err := websocket.Dial(ctx, u.String(), &websocket.DialOptions{HTTPClient: c.ws, HTTPHeader: h})
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusUnauthorized {
			return nil, ErrUnauthorized
		}
		return nil, err
	}
	conn.SetReadLimit(1 << 20)
	return &Stream{conn: conn}, nil
}

func (s *Stream) Read(ctx context.Context) (Message, error) {
	var m Message
	_, data, err := s.conn.Read(ctx)
	if err != nil {
		return m, err
	}
	err = json.Unmarshal(data, &m)
	return m, err
}

// Ping blocks until the pong arrives or ctx ends; Read must be running.
func (s *Stream) Ping(ctx context.Context) error { return s.conn.Ping(ctx) }

func (s *Stream) Close() error { return s.conn.Close(websocket.StatusNormalClosure, "") }

// Abort drops the connection without a close handshake.
func (s *Stream) Abort() { s.conn.CloseNow() }
