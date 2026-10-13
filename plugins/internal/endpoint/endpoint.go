// Package endpoint is the two rules every HTTP client in this module shares, because each of them
// carries a credential in a header.
//
// 🔴 NO REDIRECT IS FOLLOWED. Go's client re-sends custom headers to a redirect target — `x-api-key`
// even to ANOTHER HOST, and `Authorization` to the same host on another port — so a 3xx from the
// provider or the pod (or from anything answering in their place) would hand the key or the plugin
// token to wherever it pointed. [Client] answers the 3xx itself, and both callers treat it as an
// error.
//
// 🔴 NO CLEARTEXT OFF THE HOST, UNLESS THE OPERATOR NAMED THE HOST. [Validate] takes `https`, or plain
// `http` to a loopback address — the rule `cairn-capture` applies to its worker URL, for the same
// reason: the credential would cross the network readable. The one exception is a host the operator
// listed BY NAME (`-allow-plaintext-host`, exact match, no default, no environment spelling): a
// deployment whose listener is plain http over a private ENCRYPTED mesh address has its transport
// encrypted below HTTP, which this program cannot see and must be told. A PROVIDER URL takes no such
// list — a model provider is on the public internet.
package endpoint

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// Validate returns the base URL without a trailing slash, or why it is refused. `what` names the flag.
// `allowPlaintext` is the hosts plain `http` is accepted for beyond loopback (nil: none); the second
// result is the listed host that was USED, "" when the URL needed no exception.
func Validate(what, raw string, allowPlaintext []string) (base, plaintextHost string, err error) {
	base, err = validate(what, raw, func(host string) bool {
		if slices.Contains(allowPlaintext, host) {
			plaintextHost = host
			return true
		}
		return false
	})
	return base, plaintextHost, err
}

func validate(what, raw string, allowed func(host string) bool) (string, error) {
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
		// 🔴 AN EXACT match of the URL's host (no port), never a suffix or a pattern: `mesh.example`
		// listed must not admit `mesh.example.attacker.invalid`.
		if !Loopback(u.Hostname()) && !allowed(u.Hostname()) {
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
