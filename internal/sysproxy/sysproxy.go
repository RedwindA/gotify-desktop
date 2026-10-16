// Package sysproxy picks the proxy of a request: the environment's
// (HTTPS_PROXY, HTTP_PROXY, NO_PROXY) when it names one, or else, on Windows,
// the one in the system's Internet settings, for the hosts NO_PROXY leaves to it. Go reads only the environment,
// which Windows apps rarely get. A proxy auto-config (PAC) script is not read.
package sysproxy

import (
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Proxy is an http.Transport's Proxy.
func Proxy(req *http.Request) (*url.URL, error) {
	if fromEnv() {
		return http.ProxyFromEnvironment(req)
	}
	if p := current(); p != nil && !noProxy(os.Getenv("NO_PROXY")+","+os.Getenv("no_proxy"), req.URL) {
		return p.forURL(req.URL), nil
	}
	return nil, nil
}

// port is the port of u, written or the default of its scheme.
func port(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	return map[string]string{"http": "80", "ws": "80", "https": "443", "wss": "443"}[strings.ToLower(u.Scheme)]
}

// noProxy reports whether a NO_PROXY list excludes u: "*", a domain (which
// covers its subdomains), a host:port, an IP address or a CIDR range.
func noProxy(list string, u *url.URL) bool {
	host := strings.ToLower(u.Hostname())
	ip := net.ParseIP(host)
	port := port(u)
	for _, e := range strings.Split(strings.ToLower(list), ",") {
		e = strings.TrimSpace(e)
		switch {
		case e == "":
			continue
		case e == "*":
			return true
		}
		if _, cidr, err := net.ParseCIDR(e); err == nil {
			if ip != nil && cidr.Contains(ip) {
				return true
			}
			continue
		}
		if h, p, err := net.SplitHostPort(e); err == nil {
			if p != port {
				continue
			}
			e = h
		}
		e = strings.TrimPrefix(strings.TrimPrefix(e, "*"), ".")
		if eip := net.ParseIP(e); eip != nil {
			if ip != nil && eip.Equal(ip) {
				return true
			}
			continue
		}
		if host == e || strings.HasSuffix(host, "."+e) {
			return true
		}
	}
	return false
}

// Install makes the default transport, and so the default client, use Proxy.
func Install() {
	if t, ok := http.DefaultTransport.(*http.Transport); ok {
		t.Proxy = Proxy
	}
}

func fromEnv() bool {
	for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"} {
		if os.Getenv(k) != "" {
			return true
		}
	}
	return false
}

// cacheFor is how long the system's settings are trusted before reading them again.
const cacheFor = 10 * time.Second

var cache struct {
	mu   sync.Mutex
	at   time.Time
	conf *config
}

// current returns the system's proxy, or nil for none.
func current() *config {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.at.IsZero() || time.Since(cache.at) > cacheFor {
		cache.conf, cache.at = system(), time.Now()
	}
	return cache.conf
}

// config is a proxy configuration in the form of Windows' Internet settings.
type config struct {
	// servers are proxies by scheme ("http", "https", "socks"), or "" for
	// one that serves every scheme.
	servers map[string]*url.URL
	bypass  []*regexp.Regexp
	// local bypasses the proxy for host names without a dot (<local>).
	local bool
}

// parse reads ProxyServer, "host:port" or "http=host:port;https=host:port;socks=host:port",
// and ProxyOverride, patterns with * wildcards separated by semicolons. It returns nil
// when no proxy is set.
func parse(server, override string) *config {
	c := &config{servers: map[string]*url.URL{}}
	for _, part := range strings.Split(server, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		scheme, addr, ok := strings.Cut(part, "=")
		if !ok {
			scheme, addr = "", part
		}
		scheme = strings.ToLower(strings.TrimSpace(scheme))
		proxyScheme := "http"
		if scheme == "socks" {
			proxyScheme = "socks5"
		}
		if u := proxyURL(strings.TrimSpace(addr), proxyScheme); u != nil {
			c.servers[scheme] = u
		}
	}
	if len(c.servers) == 0 {
		return nil
	}
	for _, p := range strings.FieldsFunc(override, func(r rune) bool { return r == ';' || r == ' ' || r == ',' }) {
		p = strings.ToLower(p)
		if p == "<local>" {
			c.local = true
			continue
		}
		if _, rest, ok := strings.Cut(p, "://"); ok {
			p = rest
		}
		pat := regexp.QuoteMeta(p)
		pat = strings.ReplaceAll(pat, `\*`, `.*`)
		if re, err := regexp.Compile(`^` + pat + `$`); err == nil {
			c.bypass = append(c.bypass, re)
		}
	}
	return c
}

func proxyURL(addr, scheme string) *url.URL {
	if addr == "" {
		return nil
	}
	if !strings.Contains(addr, "://") {
		addr = scheme + "://" + addr
	}
	u, err := url.Parse(addr)
	if err != nil || u.Host == "" {
		return nil
	}
	return u
}

// forURL returns the proxy for u, or nil to connect directly.
func (c *config) forURL(u *url.URL) *url.URL {
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	if c.local && !strings.Contains(host, ".") && net.ParseIP(host) == nil {
		return nil
	}
	hostPort := net.JoinHostPort(host, port(u))
	for _, re := range c.bypass {
		if re.MatchString(host) || re.MatchString(hostPort) {
			return nil
		}
	}
	scheme := strings.ToLower(u.Scheme)
	switch scheme {
	case "ws":
		scheme = "http"
	case "wss":
		scheme = "https"
	}
	for _, k := range []string{scheme, "", "socks"} {
		if p := c.servers[k]; p != nil {
			return p
		}
	}
	return nil
}
