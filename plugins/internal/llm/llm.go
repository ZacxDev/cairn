// Package llm is the model-provider half of the example plugins: ONE request shape (a system prompt,
// one user message, an output bound) and two wire spellings of it, over `net/http` with no SDK.
//
// 🔴 NO SDK, DELIBERATELY: this module is STDLIB-ONLY (`internal/depspolicy`'s nested-module ledger
// pins an empty dependency set for it), so a provider's client library — and its graph — never
// enters a binary that holds an API key and reads transcripts. The cost is that each wire format is
// spelled here by hand; the in-process fake providers in the tests are written from the same
// spelling, so what they prove is the PLUGIN's behaviour against a provider that answers this shape,
// not that the provider still answers it. A real call is the operator's rollout step (the plan's
// closing condition (iii)), never CI's.
//
// 🔴 THE KEY IS SENT IN ONE HEADER AND NOWHERE ELSE. A provider's error body is quoted back only after
// every occurrence of the key is replaced, and only its first few hundred bytes.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ZacxDev/cairn/plugins/internal/endpoint"
)

// Request is one completion.
type Request struct {
	System string
	User   string
	// MaxTokens bounds the answer (thinking included, where the provider counts it there).
	MaxTokens int
}

// Response is a completion's text and what the provider says it cost.
type Response struct {
	Text string
	// Model is the model the provider says answered — what an output's `model` field records.
	Model                     string
	InputTokens, OutputTokens int
}

// Provider is one model provider.
type Provider interface {
	Complete(ctx context.Context, r Request) (Response, error)
	// Name is the provider's name as the `-provider` flag spells it.
	Name() string
}

// Providers is the closed set `-provider` may name.
func Providers() []string { return []string{"anthropic", "openai"} }

// Config chooses and configures a provider.
type Config struct {
	Provider string
	// BaseURL overrides the provider's public endpoint (a proxy, or a test's fake).
	BaseURL string
	Model   string
	// Key is the API key, read by the caller from a file ([secretfile.Read]).
	Key string
	// Effort is Anthropic's `output_config.effort` ("" sends none).
	Effort string
	HTTP   *http.Client
}

// DefaultTimeout bounds one request when no client is given: a summary of a large chunk can take
// minutes on a model that thinks.
const DefaultTimeout = 10 * time.Minute

// New returns the configured provider.
func New(c Config) (Provider, error) {
	if c.Model == "" {
		return nil, errors.New("no model was named")
	}
	if c.Key == "" {
		return nil, errors.New("no API key was read")
	}
	// 🔴 REDIRECTS ARE NOT FOLLOWED, whatever client the caller passed: Go re-sends `x-api-key` to a
	// redirect target on ANY host (`internal/endpoint`). A 3xx is a [StatusError].
	c.HTTP = endpoint.Client(c.HTTP, DefaultTimeout)
	switch c.Provider {
	case "anthropic":
		if c.BaseURL == "" {
			c.BaseURL = "https://api.anthropic.com"
		}
	case "openai":
		if c.BaseURL == "" {
			c.BaseURL = "https://api.openai.com"
		}
	default:
		return nil, fmt.Errorf("provider %q is not one of %v", c.Provider, Providers())
	}
	// No plaintext exception for a provider: https, or http to loopback (a local proxy), only.
	base, _, err := endpoint.Validate("the provider URL", c.BaseURL, nil)
	if err != nil {
		return nil, err
	}
	c.BaseURL = base
	switch c.Provider {
	case "anthropic":
		return &anthropic{c}, nil
	case "openai":
		if c.Effort != "" {
			return nil, errors.New("-effort is an Anthropic setting; the openai provider takes none")
		}
		return &openai{c}, nil
	}
	return nil, fmt.Errorf("provider %q is not one of %v", c.Provider, Providers())
}

// maxAnswer bounds a provider's answer body.
const maxAnswer = 8 << 20

// post sends one JSON body and decodes the answer, quoting a failure with the key scrubbed.
func post(ctx context.Context, c Config, path string, headers map[string]string, body, into any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("the %s provider could not be reached: %s", c.Provider, scrub(err.Error(), c.Key))
	}
	defer resp.Body.Close()
	ans, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer+1))
	if err != nil {
		return fmt.Errorf("reading the %s provider's answer: %w", c.Provider, err)
	}
	if resp.StatusCode != http.StatusOK {
		detail := scrub(string(ans), c.Key)
		if len(detail) > 400 {
			detail = detail[:400]
		}
		return &StatusError{Provider: c.Provider, Status: resp.StatusCode, Detail: strings.TrimSpace(detail)}
	}
	if len(ans) > maxAnswer {
		return fmt.Errorf("the %s provider's answer is over %d bytes", c.Provider, maxAnswer)
	}
	if err := json.Unmarshal(ans, into); err != nil {
		return fmt.Errorf("the %s provider's answer is not the documented JSON: %w", c.Provider, err)
	}
	return nil
}

func scrub(s, key string) string {
	if key == "" {
		return s
	}
	return strings.ReplaceAll(s, key, "[key]")
}

// StatusError is a provider answering non-200.
type StatusError struct {
	Provider string
	Status   int
	Detail   string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("the %s provider answered %d: %s", e.Provider, e.Status, e.Detail)
}

// StopError is a 200 answer that is NOT a finished summary: a refusal, an answer cut at the output
// bound, or any stop the provider did not call a normal end. Deterministic for the same input, which
// is why the caller backs the session off rather than re-paying for it every run.
type StopError struct {
	Provider, Reason string
}

func (e *StopError) Error() string {
	return fmt.Sprintf("the %s provider stopped with %q, not a finished answer", e.Provider, e.Reason)
}

// ---------------------------------------------------------------- Anthropic Messages API

type anthropic struct{ c Config }

func (a *anthropic) Name() string { return "anthropic" }

// AnthropicVersion is the `anthropic-version` header this spelling is written against.
const AnthropicVersion = "2023-06-01"

func (a *anthropic) Complete(ctx context.Context, r Request) (Response, error) {
	body := map[string]any{
		"model":      a.c.Model,
		"max_tokens": r.MaxTokens,
		"system":     r.System,
		"messages":   []map[string]any{{"role": "user", "content": r.User}},
	}
	if a.c.Effort != "" {
		body["output_config"] = map[string]any{"effort": a.c.Effort}
	}
	var ans struct {
		Model   string `json:"model"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		Usage      struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	err := post(ctx, a.c, "/v1/messages", map[string]string{"x-api-key": a.c.Key, "anthropic-version": AnthropicVersion}, body, &ans)
	if err != nil {
		return Response{}, err
	}
	// A refusal or a truncated answer is not a summary: writing either would store a claim the model
	// did not finish making.
	if ans.StopReason != "end_turn" && ans.StopReason != "stop_sequence" {
		return Response{}, &StopError{Provider: "anthropic", Reason: ans.StopReason}
	}
	var parts []string
	for _, b := range ans.Content {
		if b.Type == "text" {
			parts = append(parts, b.Text)
		}
	}
	return Response{Text: strings.TrimSpace(strings.Join(parts, "")), Model: orDefault(ans.Model, a.c.Model),
		InputTokens: ans.Usage.InputTokens, OutputTokens: ans.Usage.OutputTokens}, nil
}

// ---------------------------------------------------------------- OpenAI-compatible chat completions

type openai struct{ c Config }

func (o *openai) Name() string { return "openai" }

func (o *openai) Complete(ctx context.Context, r Request) (Response, error) {
	body := map[string]any{
		"model":                 o.c.Model,
		"max_completion_tokens": r.MaxTokens,
		"messages": []map[string]any{
			{"role": "system", "content": r.System},
			{"role": "user", "content": r.User},
		},
	}
	var ans struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := post(ctx, o.c, "/v1/chat/completions", map[string]string{"Authorization": "Bearer " + o.c.Key}, body, &ans); err != nil {
		return Response{}, err
	}
	if len(ans.Choices) == 0 {
		return Response{}, errors.New("the openai provider answered no choice")
	}
	if fr := ans.Choices[0].FinishReason; fr != "stop" {
		return Response{}, &StopError{Provider: "openai", Reason: fr}
	}
	return Response{Text: strings.TrimSpace(ans.Choices[0].Message.Content), Model: orDefault(ans.Model, o.c.Model),
		InputTokens: ans.Usage.PromptTokens, OutputTokens: ans.Usage.CompletionTokens}, nil
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}
