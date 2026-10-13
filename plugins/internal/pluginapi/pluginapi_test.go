package pluginapi

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

const testToken = "plugin-token-synthetic-0002"

// TestARedirectIsNotFollowedWithTheToken — Go's client re-sends `Authorization` to a redirect target on
// the same host (another port included). The pod answers 307 to another listener; that listener must
// receive NOTHING, and the 3xx is a [StatusError]. Red with the default redirect policy: the second
// listener (same host, other port) received the bearer token.
func TestARedirectIsNotFollowedWithTheToken(t *testing.T) {
	var hits atomic.Int32
	var leaked atomic.Bool
	other := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if strings.Contains(r.Header.Get("Authorization"), testToken) {
			leaked.Store(true)
		}
		_, _ = rw.Write([]byte(`{"items":[]}`))
	}))
	defer other.Close()
	front := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		http.Redirect(rw, r, other.URL+r.URL.RequestURI(), http.StatusTemporaryRedirect)
	}))
	defer front.Close()
	c := &Client{Base: front.URL, Token: testToken}
	_, err := c.Pending(context.Background(), 10)
	var se *StatusError
	if !errors.As(err, &se) || se.Status != http.StatusTemporaryRedirect {
		t.Errorf("a redirecting pod answered %v, want a 307 StatusError", err)
	}
	if _, err := c.WriteOutput(context.Background(), Envelope{Session: "s-0001"}); !errors.As(err, &se) {
		t.Errorf("a redirected write answered %v, want a StatusError", err)
	}
	if hits.Load() != 0 || leaked.Load() {
		t.Errorf("the redirect target received %d request(s) (token leaked: %t), want none", hits.Load(), leaked.Load())
	}
	// The same with a caller's own client: its redirect policy is replaced, not trusted.
	c.HTTP = &http.Client{}
	if _, err := c.Pending(context.Background(), 10); !errors.As(err, &se) || hits.Load() != 0 {
		t.Errorf("a caller's client followed the redirect: %v, %d hits", err, hits.Load())
	}
}

// TestTheStatusesAreTheContracts — 403 and 401 are their own errors, any other non-2xx a StatusError
// whose detail has the token scrubbed, and an answer without its documented key is not "nothing".
func TestTheStatusesAreTheContracts(t *testing.T) {
	status, body := 200, `{}`
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(status)
		_, _ = rw.Write([]byte(strings.ReplaceAll(body, "TOKEN", strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))))
	}))
	defer srv.Close()
	c := &Client{Base: srv.URL, Token: testToken}
	ctx := context.Background()
	if _, err := c.Pending(ctx, 1); err == nil || !strings.Contains(err.Error(), "no `items`") {
		t.Errorf("an answer with no items key: %v", err)
	}
	if _, err := c.Outputs(ctx, "s-0001", "summary"); err == nil || !strings.Contains(err.Error(), "no `outputs`") {
		t.Errorf("an answer with no outputs key: %v", err)
	}
	status = 403
	if _, err := c.Pending(ctx, 1); !errors.Is(err, ErrForbidden) {
		t.Errorf("403: %v", err)
	}
	status = 401
	if _, err := c.Pending(ctx, 1); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("401: %v", err)
	}
	status, body = 507, `{"refused":"quota","echo":"TOKEN"}`
	_, err := c.WriteOutput(ctx, Envelope{Session: "s-0001"})
	var se *StatusError
	if !errors.As(err, &se) || se.Status != 507 || strings.Contains(err.Error(), testToken) || !strings.Contains(err.Error(), `"echo":"[token]"`) {
		t.Errorf("507: %v, want a StatusError whose detail carries the body with the token scrubbed", err)
	}
	status, body = 201, `{"id":"o-0001"}`
	if id, err := c.WriteOutput(ctx, Envelope{Session: "s-0001"}); err != nil || id != "o-0001" {
		t.Errorf("POSITIVE CONTROL: a 201 write answered %q %v", id, err)
	}
}

// TestAWorkerURLMustBeHTTPSOrLoopback — refused before any request, so the token goes nowhere.
func TestAWorkerURLMustBeHTTPSOrLoopback(t *testing.T) {
	for _, base := range []string{"http://pod.example.invalid", "ftp://pod.example.invalid", "pod.example.invalid", ""} {
		c := &Client{Base: base, Token: testToken}
		if _, err := c.Pending(context.Background(), 1); err == nil || strings.Contains(err.Error(), "dial") {
			t.Errorf("Base %q: %v, want a refusal before any request", base, err)
		}
	}
}

// TestPlainHTTPOffTheHostNeedsTheOperatorToNameTheHost — `-allow-plaintext-host`: a worker URL that is
// plain http to a non-loopback host is refused BEFORE any request unless that exact host is listed; a
// different host listed, or a host the listed one is merely a prefix or suffix of, is still refused;
// and with the exception in use a redirect is still not followed. The server is reached through a
// transport that dials it whatever the URL's host says, so "accepted" is a request that ARRIVED.
func TestPlainHTTPOffTheHostNeedsTheOperatorToNameTheHost(t *testing.T) {
	const host = "mesh-host.invalid"
	var hits, redirected atomic.Int32
	redirect := false
	target := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) { redirected.Add(1) }))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if redirect {
			http.Redirect(rw, r, target.URL+r.URL.RequestURI(), http.StatusFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+testToken {
			rw.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = rw.Write([]byte(`{"items":[]}`))
	}))
	defer srv.Close()
	dialSrv := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
	}}}
	ask := func(urlHost string, allow ...string) (string, error) {
		c := &Client{Base: "http://" + urlHost + ":8080", Token: testToken, HTTP: dialSrv, AllowPlaintextHosts: allow}
		used, verr := c.ValidateBase()
		_, err := c.Pending(context.Background(), 1)
		if (verr != nil) != (err != nil && strings.Contains(err.Error(), "non-loopback")) {
			t.Errorf("%s allow %v: ValidateBase says %v and the request says %v — one rule, two answers", urlHost, allow, verr, err)
		}
		return used, err
	}
	for name, c := range map[string]struct {
		urlHost string
		allow   []string
	}{
		"no host listed":                       {host, nil},
		"a different host listed":              {host, []string{"other-host.invalid"}},
		"the listed host is a suffix of it":    {"evil." + host, []string{host}},
		"the listed host is a prefix of it":    {host + ".evil.invalid", []string{host}},
		"the URL's host:port listed, not host": {host, []string{host + ":8080"}},
	} {
		if _, err := ask(c.urlHost, c.allow...); err == nil || !strings.Contains(err.Error(), "plain http to a non-loopback host") {
			t.Errorf("%s: %v, want the cleartext refusal", name, err)
		}
	}
	if hits.Load() != 0 {
		t.Fatalf("a refused URL still sent %d request(s) — the token left the process", hits.Load())
	}
	if used, err := ask(host, "other-host.invalid", host); err != nil || used != host || hits.Load() != 1 {
		t.Fatalf("the exact host listed: used %q, %v, %d request(s) arrived; want it accepted and ONE request", used, err, hits.Load())
	}
	redirect = true
	_, err := ask(host, host)
	var se *StatusError
	if !errors.As(err, &se) || se.Status != http.StatusFound || redirected.Load() != 0 {
		t.Errorf("a redirect under the plaintext exception: %v, %d request(s) at its target; want a 302 error and none", err, redirected.Load())
	}

	var list HostList
	for _, bad := range []string{"", "http://mesh-host.invalid", "mesh-host.invalid:8080", "mesh-host.invalid/x", "u@mesh-host.invalid"} {
		if list.Set(bad) == nil {
			t.Errorf("-allow-plaintext-host %q was accepted; it takes a bare host", bad)
		}
	}
	if list.Set(host) != nil || list.Set("fd00::1") != nil || len(list) != 2 {
		t.Errorf("a bare host and a bare IPv6 address must be accepted: %v", list)
	}
}

// proxyChildEnv marks the re-executed test binary that makes ONE request to the worker URL it names.
const proxyChildEnv = "CAIRN_PLUGINAPI_TEST_PROXY_CHILD_URL"

// TestTheWorkerListenerIsNeverReachedThroughAProxyFromTheEnvironment — with `-allow-plaintext-host`
// the worker URL may be plain http to a non-loopback host, and Go's default transport sends exactly
// that request, whole, to `HTTP_PROXY`: the bearer token and every transcript byte. Red before the
// transport's proxy was removed: the listener standing in for the proxy received the request and its
// `Authorization` header. In a SUBPROCESS, because Go reads the proxy environment once per process.
func TestTheWorkerListenerIsNeverReachedThroughAProxyFromTheEnvironment(t *testing.T) {
	if u := os.Getenv(proxyChildEnv); u != "" {
		c := &Client{Base: u, Token: testToken, AllowPlaintextHosts: []string{"worker.example.invalid"}}
		_, err := c.Pending(context.Background(), 1)
		if err == nil {
			t.Fatal("the request to a host that does not resolve succeeded: something answered for it")
		}
		return
	}
	var mu sync.Mutex
	var got []string
	proxy := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = append(got, r.Method+" "+r.URL.String()+" "+r.Header.Get("Authorization"))
		mu.Unlock()
		rw.Header().Set("Content-Type", "application/json")
		_, _ = rw.Write([]byte(`{"items":[]}`))
	}))
	defer proxy.Close()
	cmd := exec.Command(os.Args[0], "-test.run", "^TestTheWorkerListenerIsNeverReachedThroughAProxyFromTheEnvironment$", "-test.v")
	cmd.Env = append(os.Environ(), proxyChildEnv+"=http://worker.example.invalid", "HTTP_PROXY="+proxy.URL, "http_proxy="+proxy.URL,
		"NO_PROXY=", "no_proxy=")
	out, err := cmd.CombinedOutput()
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 0 {
		t.Fatalf("the proxy named by HTTP_PROXY received the worker request — token included: %q\n%s", got, out)
	}
	if err != nil {
		t.Fatalf("the child failed for another reason: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "--- PASS") {
		t.Fatalf("the child did not run its request:\n%s", out)
	}
}
