package gotify

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var ErrUnauthorized = errors.New("gotify: unauthorized")

// HTTPError is returned for any non-2xx status other than 401.
type HTTPError struct {
	Status int
	Body   string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("gotify: http %d: %s", e.Status, e.Body)
}

type Options struct {
	InsecureSkipVerify bool
	CACertPEM          []byte
	Timeout            time.Duration
}

type Client struct {
	base  *url.URL
	token string
	http  *http.Client
	ws    *http.Client
}

func New(baseURL, token string, opts Options) (*Client, error) {
	base, err := parseBase(baseURL)
	if err != nil {
		return nil, err
	}
	tr, err := transport(opts)
	if err != nil {
		return nil, err
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		base:  base,
		token: token,
		http:  &http.Client{Transport: tr, Timeout: timeout, CheckRedirect: checkRedirect},
		ws:    &http.Client{Transport: tr, CheckRedirect: checkRedirect},
	}, nil
}

const maxRedirects = 10

// checkRedirect keeps the client token from leaving the server it belongs to
// and refuses to follow an https server to plain http.
func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return fmt.Errorf("gotify: stopped after %d redirects", maxRedirects)
	}
	first := via[0].URL
	if first.Scheme == "https" && req.URL.Scheme != "https" {
		return errors.New("gotify: refusing a redirect from https to http")
	}
	if req.URL.Scheme != first.Scheme || req.URL.Host != first.Host {
		req.Header.Del("X-Gotify-Key")
		req.Header.Del("Authorization")
	}
	return nil
}

func parseBase(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, err
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("gotify: invalid server url %q", raw)
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	u.RawQuery, u.Fragment = "", ""
	return u, nil
}

func transport(opts Options) (*http.Transport, error) {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	cfg := &tls.Config{InsecureSkipVerify: opts.InsecureSkipVerify}
	if len(opts.CACertPEM) > 0 {
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(opts.CACertPEM) {
			return nil, errors.New("gotify: invalid CA certificate PEM")
		}
		cfg.RootCAs = pool
	}
	tr.TLSClientConfig = cfg
	return tr, nil
}

func (c *Client) endpoint(path string, query url.Values) string {
	u := *c.base
	u.Path = c.base.Path + "/" + strings.TrimLeft(path, "/")
	u.RawQuery = query.Encode()
	return u.String()
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint(path, query), nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Gotify-Key", c.token)
	return doRequest(c.http, req, out)
}

func doRequest(hc *http.Client, req *http.Request, out any) error {
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		return ErrUnauthorized
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<12))
		return &HTTPError{Status: resp.StatusCode, Body: strings.TrimSpace(string(b))}
	}
	if out == nil {
		io.Copy(io.Discard, resp.Body)
		return nil
	}
	if b, ok := out.(*[]byte); ok {
		*b, err = io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		return err
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Login creates a client on the server using basic auth and returns its token.
func Login(ctx context.Context, baseURL, user, pass, clientName string, opts Options) (string, uint, error) {
	c, err := New(baseURL, "", opts)
	if err != nil {
		return "", 0, err
	}
	body, _ := json.Marshal(map[string]string{"name": clientName})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("/client", nil), bytes.NewReader(body))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(user, pass)
	var res struct {
		ID    uint   `json:"id"`
		Token string `json:"token"`
	}
	if err := doRequest(c.http, req, &res); err != nil {
		return "", 0, err
	}
	return res.Token, res.ID, nil
}

func (c *Client) CurrentUser(ctx context.Context) (u User, err error) {
	err = c.do(ctx, http.MethodGet, "/current/user", nil, &u)
	return
}

func (c *Client) Version(ctx context.Context) (v VersionInfo, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint("/version", nil), nil)
	if err != nil {
		return v, err
	}
	err = doRequest(c.http, req, &v)
	return
}

func (c *Client) Applications(ctx context.Context) (apps []Application, err error) {
	err = c.do(ctx, http.MethodGet, "/application", nil, &apps)
	return
}

// Messages returns messages with id < since, newest first. since 0 means the newest.
func (c *Client) Messages(ctx context.Context, limit int, since uint) (p PagedMessages, err error) {
	q := url.Values{}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if since > 0 {
		q.Set("since", strconv.FormatUint(uint64(since), 10))
	}
	err = c.do(ctx, http.MethodGet, "/message", q, &p)
	return
}

func (c *Client) DeleteMessage(ctx context.Context, id uint) error {
	err := c.do(ctx, http.MethodDelete, "/message/"+strconv.FormatUint(uint64(id), 10), nil, nil)
	var he *HTTPError
	if errors.As(err, &he) && he.Status == http.StatusNotFound {
		return nil
	}
	return err
}

// DeleteClient may fail with a 403 *HTTPError when the session is not elevated.
func (c *Client) DeleteClient(ctx context.Context, id uint) error {
	return c.do(ctx, http.MethodDelete, "/client/"+strconv.FormatUint(uint64(id), 10), nil, nil)
}

func (c *Client) Image(ctx context.Context, path string) (b []byte, err error) {
	err = c.do(ctx, http.MethodGet, path, nil, &b)
	return
}

// DeleteClientBasic deletes a client with the account's password, for the
// session of a login that must be undone.
func DeleteClientBasic(ctx context.Context, baseURL, user, pass string, clientID uint, opts Options) error {
	c, err := New(baseURL, "", opts)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.endpoint("/client/"+strconv.FormatUint(uint64(clientID), 10), nil), nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(user, pass)
	return doRequest(c.http, req, nil)
}
