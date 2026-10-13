// Package pluginapi is the client side of cairn's plugin API (`claudedocs/plan-cairn-plugins.md`,
// decisions 9 and 13; served by `cairn-ui`'s WORKER listener, `internal/worker/plugin.go`): the
// stateless pending query, a session's transcript records, the outputs already stored, and the one
// write. Plain HTTP+JSON over `net/http` (O7, never MCP), shared by every example plugin in this
// module.
//
// 🔴 STDLIB ONLY, AND THE CONTRACT IS RE-STATED HERE RATHER THAN IMPORTED. This module cannot import
// `internal/plugins` without requiring the parent module — and with it the parent's whole dependency
// graph — so the wire shapes below are a second spelling of S5's. What keeps the two honest is a
// test that runs this client against the REAL worker listener (see the module README); the fakes in
// this module's own tests are written from the same shapes and so cannot see a drift.
//
// 🔴 THE TOKEN IS READ FROM A FILE AND NEVER PRINTED. It goes into one `Authorization` header and
// nowhere else — not into an error, not into a log line — because any line that carries it re-stages
// it in whatever captures the run.
package pluginapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ZacxDev/cairn/plugins/internal/endpoint"
)

// The routes, spelled as `internal/worker/plugin.go` spells them.
const (
	PendingPath    = "/plugin/v1/pending"
	TranscriptPath = "/plugin/v1/transcript"
	OutputsPath    = "/plugin/v1/outputs"
)

// ErrForbidden is the API's ONE refusal (403): a capability, an output type or a toggle the plugin
// does not hold — or a session that is not stored. The API does not say which, by design (T10).
var ErrForbidden = errors.New("the plugin API refused the request (403: capability, type or toggle)")

// ErrUnauthorized is a 401: the token is not one the instance holds for this plugin.
var ErrUnauthorized = errors.New("the plugin API did not accept the token (401)")

// StatusError is any other non-2xx answer.
type StatusError struct {
	Path   string
	Status int
	// Detail is the start of the body, for the operator, with the token scrubbed.
	Detail string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("%s answered %d: %s", e.Path, e.Status, e.Detail)
}

// Client is one plugin's connection to one instance's worker listener.
type Client struct {
	// Base is the worker listener's base URL (`https://…`), no trailing path.
	Base string
	// Token is the plugin token (`-plugin-token-file`'s contents).
	Token string
	// HTTP is the client to use; nil is a client with [DefaultTimeout]. Its redirect policy is always
	// replaced; a nil Transport becomes a proxy-less one, a non-nil Transport is used AS GIVEN.
	HTTP *http.Client
	// AllowPlaintextHosts is the operator's `-allow-plaintext-host` list: hosts (exact match, no
	// port) the worker URL may be plain `http` to beyond loopback — a listener on a private
	// ENCRYPTED mesh address. Empty by default; redirects are refused regardless.
	AllowPlaintextHosts []string
}

// ValidateBase checks the worker URL against the rule every request re-applies, and reports the
// listed plaintext host it relies on ("" when none) so the caller can SAY so once per run.
func (c *Client) ValidateBase() (plaintextHost string, err error) {
	_, plaintextHost, err = endpoint.Validate("the worker URL", c.Base, c.AllowPlaintextHosts)
	return plaintextHost, err
}

// HostList is a repeatable flag's value (`-allow-plaintext-host a -allow-plaintext-host b`), shared
// so every plugin in this module spells the flag one way.
type HostList []string

func (h *HostList) String() string { return strings.Join(*h, ",") }

// Set adds one host. It must be a bare host name or address — what a URL's host is compared against.
func (h *HostList) Set(v string) error {
	if v == "" || strings.ContainsAny(v, "/@ ") || (strings.Contains(v, ":") && net.ParseIP(v) == nil) {
		return fmt.Errorf("%q is not a bare host (no scheme, port or path)", v)
	}
	*h = append(*h, v)
	return nil
}

// DefaultTimeout bounds one request when [Client.HTTP] is nil.
const DefaultTimeout = 60 * time.Second

// maxAnswer bounds one answer's body: a transcript page is at most 8 MiB of records on the wire
// and JSON escaping can grow it.
const maxAnswer = 32 << 20

// http is the client every request uses: the caller's (or a default) with redirects NOT followed —
// a 3xx is answered as a [StatusError], never chased with the bearer token (`internal/endpoint`).
//
// 🔴 AND NEVER THROUGH A PROXY FROM THE ENVIRONMENT. Go's default transport sends a plain-`http`
// request for a non-loopback host to `HTTP_PROXY` — whole, `Authorization` header and transcript bytes
// included. With `-allow-plaintext-host` that is exactly the request this client makes, so an
// environment variable would hand the plugin token to whatever it named. The worker listener is
// reached DIRECTLY — unless the caller supplies its own `HTTP` client WITH a transport, which is then
// used as given, proxy and all (no program in this module does).
func (c *Client) http() *http.Client {
	cl := endpoint.Client(c.HTTP, DefaultTimeout)
	if cl.Transport == nil {
		cl.Transport = direct
	}
	return cl
}

// direct is the default transport with no proxy, shared so connections are reused.
var direct = func() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = nil
	return t
}()

// scrub removes the token from text about to be shown.
func (c *Client) scrub(s string) string {
	if c.Token == "" {
		return s
	}
	return strings.ReplaceAll(s, c.Token, "[token]")
}

func (c *Client) do(ctx context.Context, method, path string, q url.Values, body []byte, into any) error {
	base, _, err := endpoint.Validate("the worker URL", c.Base, c.AllowPlaintextHosts)
	if err != nil {
		return err
	}
	u := base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return fmt.Errorf("%s: %s", path, c.scrub(err.Error()))
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer+1))
	if err != nil {
		return fmt.Errorf("%s: reading the answer: %w", path, err)
	}
	if len(raw) > maxAnswer {
		return fmt.Errorf("%s: the answer is over %d bytes", path, maxAnswer)
	}
	switch {
	case resp.StatusCode == http.StatusForbidden:
		return ErrForbidden
	case resp.StatusCode == http.StatusUnauthorized:
		return ErrUnauthorized
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		// Scrubbed like the provider's: a body that echoes the request's credential must not print it.
		detail := c.scrub(string(raw))
		if len(detail) > 300 {
			detail = detail[:300]
		}
		return &StatusError{Path: path, Status: resp.StatusCode, Detail: strings.TrimSpace(detail)}
	}
	if into == nil {
		return nil
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("%s: the answer is not the documented JSON: %w", path, err)
	}
	return nil
}

// StreamPos is one stream's committed position in a pending item.
type StreamPos struct {
	SeqTo int64 `json:"seq_to"`
}

// PrevOutput is the plugin's last-written output for the session.
type PrevOutput struct {
	ID        string           `json:"id"`
	Watermark map[string]int64 `json:"watermark"`
}

// PendingItem is one session the plugin is ON for that has grown past its last output.
type PendingItem struct {
	Session    string               `json:"session"`
	Streams    map[string]StreamPos `json:"streams"`
	PrevOutput *PrevOutput          `json:"prev_output"`
}

// Pending is `GET /plugin/v1/pending?limit=N`.
func (c *Client) Pending(ctx context.Context, limit int) ([]PendingItem, error) {
	var body struct {
		Items *[]PendingItem `json:"items"`
	}
	if err := c.do(ctx, http.MethodGet, PendingPath, url.Values{"limit": {strconv.Itoa(limit)}}, nil, &body); err != nil {
		return nil, err
	}
	if body.Items == nil {
		// An answer without the key is not "nothing pending" — it is not the contract.
		return nil, fmt.Errorf("%s: the answer has no `items`", PendingPath)
	}
	return *body.Items, nil
}

// Omission is one thing a view left out of a record (`view=conversation`).
type Omission struct {
	Class string `json:"class"`
	Path  string `json:"path"`
	Bytes int    `json:"bytes"`
	Label string `json:"label"`
}

// Record is one transcript record as the API serves it. Rec is absent when the view left the whole
// record out, and then Omitted says what it was.
type Record struct {
	Seq     int64           `json:"seq"`
	Src     string          `json:"src"`
	Rec     json.RawMessage `json:"rec"`
	Omitted []Omission      `json:"omitted"`
}

// Page is one page of a stream's records.
type Page struct {
	Records []Record `json:"records"`
	NextSeq int64    `json:"next_seq"`
	Done    bool     `json:"done"`
	SeqTo   int64    `json:"seq_to"`
}

// TranscriptQuery names one page.
type TranscriptQuery struct {
	Session, Stream string
	FromSeq         int64
	// View is `conversation` (what the summary plugin reads) or `raw`; "" sends none.
	View     string
	MaxBytes int
}

// Transcript is `GET /plugin/v1/transcript`.
func (c *Client) Transcript(ctx context.Context, q TranscriptQuery) (Page, error) {
	v := url.Values{"session": {q.Session}, "stream": {q.Stream}, "from_seq": {strconv.FormatInt(q.FromSeq, 10)}}
	if q.View != "" {
		v.Set("view", q.View)
	}
	if q.MaxBytes > 0 {
		v.Set("max_bytes", strconv.Itoa(q.MaxBytes))
	}
	var p Page
	err := c.do(ctx, http.MethodGet, TranscriptPath, v, nil, &p)
	return p, err
}

// Output is one stored output.
type Output struct {
	ID             string           `json:"id"`
	Plugin         string           `json:"plugin"`
	PluginVersion  string           `json:"plugin_version"`
	Model          string           `json:"model"`
	Session        string           `json:"session"`
	InputWatermark map[string]int64 `json:"input_watermark"`
	Type           string           `json:"type"`
	Body           json.RawMessage  `json:"body"`
	ProducedAt     string           `json:"produced_at"`
}

// Outputs is `GET /plugin/v1/outputs?session=&type=`: every stored output of that type, any plugin's.
func (c *Client) Outputs(ctx context.Context, session, typ string) ([]Output, error) {
	var body struct {
		Outputs *[]Output `json:"outputs"`
	}
	if err := c.do(ctx, http.MethodGet, OutputsPath, url.Values{"session": {session}, "type": {typ}}, nil, &body); err != nil {
		return nil, err
	}
	if body.Outputs == nil {
		return nil, fmt.Errorf("%s: the answer has no `outputs`", OutputsPath)
	}
	return *body.Outputs, nil
}

// Envelope is what a plugin POSTs (decision 9). The pod overwrites `plugin` and `plugin_version`
// from the token's registry entry, so they are informational here.
type Envelope struct {
	Plugin         string           `json:"plugin"`
	PluginVersion  string           `json:"plugin_version"`
	Model          string           `json:"model"`
	Session        string           `json:"session"`
	InputWatermark map[string]int64 `json:"input_watermark"`
	Type           string           `json:"type"`
	Body           any              `json:"body"`
}

// WriteOutput is `POST /plugin/v1/outputs`, answering the stored output's id.
func (c *Client) WriteOutput(ctx context.Context, env Envelope) (string, error) {
	raw, err := json.Marshal(env)
	if err != nil {
		return "", err
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := c.do(ctx, http.MethodPost, OutputsPath, nil, raw, &body); err != nil {
		return "", err
	}
	if body.ID == "" {
		return "", fmt.Errorf("%s: the answer carries no id", OutputsPath)
	}
	return body.ID, nil
}
