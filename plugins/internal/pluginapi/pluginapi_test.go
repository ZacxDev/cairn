package pluginapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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
