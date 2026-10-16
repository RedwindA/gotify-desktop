package sysproxy

import (
	"net/url"
	"testing"
)

func TestParseAndPick(t *testing.T) {
	for _, tc := range []struct {
		server, override, target, want string
	}{
		{"127.0.0.1:7890", "", "https://push.example.com/stream", "http://127.0.0.1:7890"},
		{"127.0.0.1:7890", "", "wss://push.example.com/stream", "http://127.0.0.1:7890"},
		{"127.0.0.1:7890", "", "http://localhost:8080/", ""},
		{"127.0.0.1:7890", "", "http://127.0.0.1/", ""},
		{"127.0.0.1:7890", "", "http://[::1]:80/", ""},
		{"127.0.0.1:7890", "<local>", "http://nas/", ""},
		{"127.0.0.1:7890", "<local>", "http://nas.lan/", "http://127.0.0.1:7890"},
		{"127.0.0.1:7890", "localhost;127.*;10.*;192.168.*;<local>", "https://192.168.1.5:8443/", ""},
		{"127.0.0.1:7890", "*.corp.example", "https://gotify.corp.example/", ""},
		{"127.0.0.1:7890", "*.corp.example", "https://gotify.example/", "http://127.0.0.1:7890"},
		{"127.0.0.1:7890", "gotify.example:8443", "https://gotify.example:8443/", ""},
		{"127.0.0.1:7890", "gotify.example:443", "https://gotify.example/", ""},
		{"127.0.0.1:7890", "gotify.example:443", "http://gotify.example/", "http://127.0.0.1:7890"},
		{"127.0.0.1:7890", "https://gotify.example", "https://gotify.example/", ""},
		{"http=proxy:80;https=secure:443", "", "https://a.example/", "http://secure:443"},
		{"http=proxy:80;https=secure:443", "", "http://a.example/", "http://proxy:80"},
		{"http=proxy:80", "", "https://a.example/", ""},
		{"socks=127.0.0.1:1080", "", "https://a.example/", "socks5://127.0.0.1:1080"},
		{"http=http://proxy:80", "", "http://a.example/", "http://proxy:80"},
		{"", "", "https://a.example/", ""},
	} {
		c := parse(tc.server, tc.override)
		u, _ := url.Parse(tc.target)
		got := ""
		if c != nil {
			if p := c.forURL(u); p != nil {
				got = p.String()
			}
		}
		if got != tc.want {
			t.Errorf("parse(%q, %q) for %s = %q, want %q", tc.server, tc.override, tc.target, got, tc.want)
		}
	}
}

func TestNoProxy(t *testing.T) {
	for _, tc := range []struct {
		list, target string
		want         bool
	}{
		{"", "https://a.example/", false},
		{"*", "https://a.example/", true},
		{"example.com", "https://gotify.example.com/", true},
		{".example.com", "https://gotify.example.com/", true},
		{"*.example.com", "https://example.com/", true},
		{"example.com", "https://notexample.com/", false},
		{"gotify.lan:8443", "https://gotify.lan:8443/", true},
		{"gotify.lan:8443", "https://gotify.lan/", false},
		{"gotify.example:443", "https://gotify.example/", true},
		{"gotify.example:80", "http://gotify.example/", true},
		{"gotify.example:80", "https://gotify.example/", false},
		{"10.0.0.0/8, 192.168.1.5", "http://10.1.2.3/", true},
		{"10.0.0.0/8, 192.168.1.5", "http://192.168.1.5:80/", true},
		{"10.0.0.0/8, 192.168.1.5", "http://192.168.1.6/", false},
	} {
		u, _ := url.Parse(tc.target)
		if got := noProxy(tc.list, u); got != tc.want {
			t.Errorf("noProxy(%q, %s) = %v", tc.list, tc.target, got)
		}
	}
}
