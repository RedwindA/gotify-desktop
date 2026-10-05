package app

import (
	"context"
	"crypto/x509"
	"errors"
	"net"
	"strings"

	"github.com/coder/websocket"

	"gotify-desktop/internal/gotify"
	"gotify-desktop/internal/i18n"
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
		return i18n.T("Wrong username or password")
	case errors.As(err, &unknownAuth), errors.As(err, &hostErr), errors.As(err, &certErr), strings.Contains(err.Error(), "x509:"):
		return i18n.T("The server's TLS certificate is not trusted. Add its CA certificate or skip verification under Advanced.")
	case errors.Is(err, context.DeadlineExceeded):
		return i18n.T("The server took too long to answer")
	case errors.As(err, &dnsErr):
		return i18n.T("Can't find the server: %s", dnsErr.Name)
	case errors.As(err, &httpErr):
		if httpErr.Status == 404 {
			return i18n.T("No Gotify server found at this address (HTTP 404). Check the URL, including any sub-path.")
		}
		return i18n.T("The server answered with an error: %s", httpErr.Error())
	case errors.As(err, &netErr), errors.As(err, new(websocket.CloseError)):
		return i18n.T("Can't reach the server: %s", shorten(err.Error()))
	}
	return err.Error()
}

func shorten(s string) string {
	if len(s) > 160 {
		return s[:160] + "…"
	}
	return s
}
