// Package endpoint is the two rules every HTTP client in this module shares, because each of them
// carries a credential in a header.
//
// 🔴 NO REDIRECT IS FOLLOWED. Go's client re-sends custom headers to a redirect target — `x-api-key`
// even to ANOTHER HOST, and `Authorization` to the same host on another port — so a 3xx from the
// provider or the pod (or from anything answering in their place) would hand the key or the plugin
// token to wherever it pointed. [Client] answers the 3xx itself, and both callers treat it as an
// error.
//
// 🔴 NO CLEARTEXT OFF THE HOST. [Validate] takes `https`, or plain `http` to a loopback address only
// — the rule `cairn-capture` applies to its worker URL, for the same reason: the credential would
// cross the network readable.
package endpoint

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Validate returns the base URL without a trailing slash, or why it is refused. `what` names the flag.
func Validate(what, raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("%s is not a URL with a host", what)
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("%s must be a bare base URL: no userinfo, query or fragment", what)
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !Loopback(u.Hostname()) {
			return "", fmt.Errorf("%s is plain http to a non-loopback host; the credential would cross the network in cleartext", what)
		}
	default:
		return "", fmt.Errorf("%s must be https (or http to a loopback address)", what)
	}
	return strings.TrimRight(raw, "/"), nil
}

// Loopback reports whether a URL host is this machine.
func Loopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// NoRedirect is the `CheckRedirect` that follows nothing: the 3xx itself is the response.
func NoRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

// Client is a client that follows no redirect. A caller's own client is COPIED and its redirect
// policy replaced, so no configuration can turn following back on.
func Client(base *http.Client, timeout time.Duration) *http.Client {
	c := &http.Client{Timeout: timeout}
	if base != nil {
		*c = *base
	}
	c.CheckRedirect = NoRedirect
	return c
}
