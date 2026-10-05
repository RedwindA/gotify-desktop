package app

import (
	"context"
	"crypto/x509"
	"errors"
	"net"
	"strings"

	"github.com/coder/websocket"

	"gotify-desktop/internal/gotify"
)

// Explain turns a connection or login error into text for the user.
func Explain(err error) string {
	var (
		unknownAuth x509.UnknownAuthorityError
		hostErr     x509.HostnameError
		certErr     x509.CertificateInvalidError
		dnsErr      *net.DNSError
		httpErr     *gotify.HTTPError
		netErr      net.Error
	)
	switch {
	case err == nil:
		return ""
	case errors.Is(err, gotify.ErrUnauthorized):
		return "Wrong username or password"
	case errors.As(err, &unknownAuth), errors.As(err, &hostErr), errors.As(err, &certErr), strings.Contains(err.Error(), "x509:"):
		return "The server's TLS certificate is not trusted. Add its CA certificate or skip verification under Advanced."
	case errors.Is(err, context.DeadlineExceeded):
		return "The server took too long to answer"
	case errors.As(err, &dnsErr):
		return "Can't find the server: " + dnsErr.Name
	case errors.As(err, &httpErr):
		if httpErr.Status == 404 {
			return "No Gotify server found at this address (HTTP 404). Check the URL, including any sub-path."
		}
		return "The server answered with an error: " + httpErr.Error()
	case errors.As(err, &netErr), errors.As(err, new(websocket.CloseError)):
		return "Can't reach the server: " + shorten(err.Error())
	}
	return err.Error()
}

func shorten(s string) string {
	if len(s) > 160 {
		return s[:160] + "…"
	}
	return s
}
