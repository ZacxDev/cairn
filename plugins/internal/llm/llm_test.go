package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

const testKey = "provider-key-synthetic-0002"

func anthropicAnswer(stop string) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(rw).Encode(map[string]any{"model": "fake-model-1", "stop_reason": stop,
			"content": []any{map[string]string{"type": "thinking", "thinking": ""}, map[string]string{"type": "text", "text": " a summary "}},
			"usage":   map[string]int{"input_tokens": 3, "output_tokens": 4}})
	}
}

func openaiAnswer(finish string) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(rw).Encode(map[string]any{"model": "fake-chat-1", "choices": []any{map[string]any{
			"message": map[string]string{"content": "a summary"}, "finish_reason": finish}}})
	}
}

func provider(t *testing.T, name, base string) Provider {
	t.Helper()
	p, err := New(Config{Provider: name, BaseURL: base, Model: "fake-model-1", Key: testKey})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// TestARedirectIsNotFollowedWithTheKey — Go's client re-sends `x-api-key` (and, same-host, the bearer)
// to a redirect target. The provider answers 307 to ANOTHER server; that server must receive NOTHING,
// and the 3xx is an error. Red with the default redirect policy: the second server received the key.
func TestARedirectIsNotFollowedWithTheKey(t *testing.T) {
	for _, name := range Providers() {
		t.Run(name, func(t *testing.T) {
			var hits atomic.Int32
			var leaked atomic.Bool
			other := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				if r.Header.Get("x-api-key") == testKey || strings.Contains(r.Header.Get("Authorization"), testKey) {
					leaked.Store(true)
				}
				anthropicAnswer("end_turn")(rw, r)
			}))
			defer other.Close()
			front := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
				http.Redirect(rw, r, other.URL+r.URL.Path, http.StatusTemporaryRedirect)
			}))
			defer front.Close()
			_, err := provider(t, name, front.URL).Complete(context.Background(), Request{System: "s", User: "u", MaxTokens: 256})
			var se *StatusError
			if !errors.As(err, &se) || se.Status != http.StatusTemporaryRedirect {
				t.Errorf("a redirecting provider answered %v, want a 307 StatusError", err)
			}
			if hits.Load() != 0 || leaked.Load() {
				t.Errorf("the redirect target received %d request(s) (key leaked: %t), want none", hits.Load(), leaked.Load())
			}
		})
	}
}

// TestOnlyAFinishedAnswerIsAResponse — each provider's stop vocabulary: a normal end is a response;
// a refusal or a truncation is a [StopError], never text.
func TestOnlyAFinishedAnswerIsAResponse(t *testing.T) {
	for _, c := range []struct {
		name, stop string
		handler    func(string) http.HandlerFunc
		ok         bool
	}{
		{"anthropic", "end_turn", anthropicAnswer, true}, {"anthropic", "refusal", anthropicAnswer, false},
		{"anthropic", "max_tokens", anthropicAnswer, false}, {"anthropic", "", anthropicAnswer, false},
		{"openai", "stop", openaiAnswer, true}, {"openai", "length", openaiAnswer, false},
		{"openai", "content_filter", openaiAnswer, false},
	} {
		srv := httptest.NewServer(c.handler(c.stop))
		resp, err := provider(t, c.name, srv.URL).Complete(context.Background(), Request{System: "s", User: "u", MaxTokens: 256})
		srv.Close()
		var stop *StopError
		switch {
		case c.ok && (err != nil || resp.Text != "a summary"):
			t.Errorf("%s %q: %q %v, want the trimmed text", c.name, c.stop, resp.Text, err)
		case !c.ok && (!errors.As(err, &stop) || stop.Reason != c.stop || resp.Text != ""):
			t.Errorf("%s %q: text %q err %v, want a StopError and no text", c.name, c.stop, resp.Text, err)
		}
	}
}

// TestTheRequestIsTheDocumentedShapeAndTheKeyIsScrubbed — the Anthropic spelling's headers and body,
// `output_config` only when an effort is set, and an error body echoing the key printed without it.
func TestTheRequestIsTheDocumentedShapeAndTheKeyIsScrubbed(t *testing.T) {
	var body map[string]any
	var hdr http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body, hdr = nil, r.Header.Clone()
		_ = json.Unmarshal(raw, &body)
		anthropicAnswer("end_turn")(rw, r)
	}))
	defer srv.Close()
	if _, err := provider(t, "anthropic", srv.URL).Complete(context.Background(), Request{System: "sys", User: "usr", MaxTokens: 512}); err != nil {
		t.Fatal(err)
	}
	if hdr.Get("x-api-key") != testKey || hdr.Get("anthropic-version") != AnthropicVersion || body["system"] != "sys" ||
		body["max_tokens"] != float64(512) || body["model"] != "fake-model-1" {
		t.Errorf("the request was %v %v", hdr, body)
	}
	if _, has := body["output_config"]; has {
		t.Error("output_config was sent with no effort set")
	}
	p, err := New(Config{Provider: "anthropic", BaseURL: srv.URL, Model: "m", Key: testKey, Effort: "low"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Complete(context.Background(), Request{User: "u", MaxTokens: 256}); err != nil {
		t.Fatal(err)
	}
	if oc, _ := body["output_config"].(map[string]any); oc["effort"] != "low" {
		t.Errorf("POSITIVE CONTROL: effort low was not sent: %v", body)
	}

	echo := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		http.Error(rw, "invalid x-api-key "+r.Header.Get("x-api-key"), http.StatusUnauthorized)
	}))
	defer echo.Close()
	_, err = provider(t, "anthropic", echo.URL).Complete(context.Background(), Request{User: "u", MaxTokens: 256})
	if err == nil || strings.Contains(err.Error(), testKey) || !strings.Contains(err.Error(), "invalid x-api-key [key]") {
		t.Errorf("the error is %v, want the body with the key scrubbed", err)
	}
}

// TestAProviderURLMustBeHTTPSOrLoopback — and a provider outside the closed set is refused.
func TestAProviderURLMustBeHTTPSOrLoopback(t *testing.T) {
	for url, ok := range map[string]bool{
		"": true, "https://provider.example.invalid": true, "http://127.0.0.1:9": true, "http://localhost:9": true, "http://[::1]:9": true,
		"http://provider.example.invalid": false, "ftp://provider.example.invalid": false, "https://u:p@provider.example.invalid": false,
	} {
		_, err := New(Config{Provider: "anthropic", BaseURL: url, Model: "m", Key: testKey})
		if (err == nil) != ok {
			t.Errorf("BaseURL %q: %v, want accepted=%t", url, err, ok)
		}
	}
	if _, err := New(Config{Provider: "other", Model: "m", Key: testKey}); err == nil {
		t.Error("an unknown provider was accepted")
	}
	if _, err := New(Config{Provider: "openai", Model: "m", Key: testKey, Effort: "low"}); err == nil {
		t.Error("-effort was accepted for the openai provider")
	}
}
