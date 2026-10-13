package plugins

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"time"
)

// 🔴 THE OUTPUT ENVELOPE IS POD-VALIDATED, AND TWO OF ITS FIELDS ARE THE POD'S, NEVER THE BODY'S
// (decision 9). `plugin` and `plugin_version` are taken from the TOKEN's registry entry — a plugin
// cannot claim to be another, and a body naming another plugin is OVERWRITTEN, not refused, so a
// buggy plugin's output is still attributed to the program that actually wrote it. `produced_at` is
// the pod clock. The body is decoded against the in-tree schema for its TYPE with unknown fields
// refused and every string bounded, because the renderer (S6) must know each type to render it.

// Envelope is what a plugin POSTs.
type Envelope struct {
	Plugin         string           `json:"plugin"`
	PluginVersion  string           `json:"plugin_version"`
	Model          string           `json:"model"`
	Session        string           `json:"session"`
	InputWatermark map[string]int64 `json:"input_watermark"`
	Type           string           `json:"type"`
	Body           json.RawMessage  `json:"body"`
}

// Output is one STORED output: the envelope as the pod accepted it, plus its id and clock.
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

// Bounds.
const (
	MaxEnvelopeBytes = 64 << 10
	maxModel         = 128
	maxSummary       = 16 << 10
	maxRationale     = 280
	maxStreams       = 1024
)

// TicketEdge is decision 14's deterministic match.
type TicketEdge struct {
	System   string  `json:"system"`
	TicketID string  `json:"ticket_id"`
	CustomID string  `json:"custom_id,omitempty"`
	Signal   string  `json:"signal"`
	Locator  Locator `json:"locator"`
}

// Locator names WHERE a match was found — exactly ONE of its shapes.
type Locator struct {
	Stream string `json:"stream,omitempty"`
	Seq    int64  `json:"seq,omitempty"`
	Commit string `json:"commit,omitempty"`
	PR     int    `json:"pr,omitempty"`
	Entry  string `json:"entry,omitempty"`
}

// TicketSuggestion is decision 14's LLM suggestion: never an edge.
type TicketSuggestion struct {
	TicketID   string  `json:"ticket_id"`
	Confidence float64 `json:"confidence"`
	Rationale  string  `json:"rationale"`
}

// TicketScan records that a ticket matcher examined a session through the envelope's watermark: how
// many edges and suggestions that pass wrote, and whether a model was asked at all — so "0
// suggestions" from a model that found none is told apart from a pass that asked nobody (no key, or
// the plugin's spend cap). The numbers are the PLUGIN's claim, like every output.
type TicketScan struct {
	System      string `json:"system"`
	Edges       int    `json:"edges"`
	Suggestions int    `json:"suggestions"`
	ModelAsked  bool   `json:"model_asked"`
}

// maxScanCount bounds a scan's counts.
const maxScanCount = 10000

// Summary is plugin A's output.
type Summary struct {
	Text string `json:"text"`
}

// Signals is decision 14's closed signal set.
var Signals = []string{"branch-name", "commit-message", "commit-trailer", "pr-body", "pr-link", "transcript-url", "entry-ref"}

var (
	// ticketID is a CANONICAL ticket id: the `refurl` template refuses `/`, and a canonical id is
	// alphanumeric.
	ticketID   = regexp.MustCompile(`^[A-Za-z0-9]{1,32}$`)
	customID   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
	commitSHA  = regexp.MustCompile(`^[0-9a-f]{7,64}$`)
	streamWire = regexp.MustCompile(`^(main|subagent:[A-Za-z0-9][A-Za-z0-9_.-]{0,63}|child:[A-Za-z0-9][A-Za-z0-9_.-]{0,63})$`)
	entryRef   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,255}$`)
)

// ValidStream reports whether s is a stream name of decision 7's vocabulary.
func ValidStream(s string) bool { return streamWire.MatchString(s) }

func strict(raw []byte, into any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return fmt.Errorf("trailing content")
	}
	return nil
}

// ValidateBody checks a body against its type's in-tree schema.
func ValidateBody(t string, raw json.RawMessage) error {
	_, err := CanonicalBody(t, raw)
	return err
}

// CanonicalBody decodes a body against its type's in-tree schema and returns it RE-ENCODED from the
// decoded value — what the pod stores.
//
// 🔴 THE STORED BYTES ARE THE VALIDATED VALUE, NEVER THE SENT ONES (#270 round 1). The decoder takes
// the LAST of two duplicate keys, so `{"text":"FIRST","text":"ok"}` validated as "ok" while the raw
// bytes stored "FIRST" too — and a renderer that read the first would have shown text nobody
// validated. Re-encoding also drops case-variant keys and repairs invalid UTF-8.
func CanonicalBody(t string, raw json.RawMessage) (json.RawMessage, error) {
	var v any
	switch t {
	case TypeSummary:
		var s Summary
		if err := strict(raw, &s); err != nil {
			return nil, err
		}
		if s.Text == "" || len(s.Text) > maxSummary {
			return nil, fmt.Errorf("a summary's text must be 1–%d bytes", maxSummary)
		}
		v = s
	case TypeTicketEdge:
		var e TicketEdge
		if err := strict(raw, &e); err != nil {
			return nil, err
		}
		if e.System != "clickup" {
			return nil, fmt.Errorf("system %q is not clickup", e.System)
		}
		if !ticketID.MatchString(e.TicketID) {
			return nil, fmt.Errorf("ticket_id is not a canonical id")
		}
		if e.CustomID != "" && !customID.MatchString(e.CustomID) {
			return nil, fmt.Errorf("custom_id does not match %s", customID)
		}
		if !slices.Contains(Signals, e.Signal) {
			return nil, fmt.Errorf("signal %q is not one of %v", e.Signal, Signals)
		}
		if err := validLocator(e.Locator); err != nil {
			return nil, err
		}
		v = e
	case TypeTicketScan:
		var s TicketScan
		if err := strict(raw, &s); err != nil {
			return nil, err
		}
		if s.System != "clickup" {
			return nil, fmt.Errorf("system %q is not clickup", s.System)
		}
		if s.Edges < 0 || s.Edges > maxScanCount || s.Suggestions < 0 || s.Suggestions > maxScanCount {
			return nil, fmt.Errorf("a scan's counts must be 0–%d", maxScanCount)
		}
		v = s
	case TypeTicketSuggestion:
		var s TicketSuggestion
		if err := strict(raw, &s); err != nil {
			return nil, err
		}
		if !ticketID.MatchString(s.TicketID) {
			return nil, fmt.Errorf("ticket_id is not a canonical id")
		}
		if s.Confidence < 0 || s.Confidence > 1 {
			return nil, fmt.Errorf("confidence must be in [0, 1]")
		}
		if len(s.Rationale) > maxRationale || !printable(s.Rationale) {
			return nil, fmt.Errorf("rationale must be at most %d bytes with no control character", maxRationale)
		}
		v = s
	default:
		return nil, fmt.Errorf("type %q is not one of %v", t, OutputTypes())
	}
	return json.Marshal(v)
}

func validLocator(l Locator) error {
	shapes := 0
	if l.Stream != "" || l.Seq != 0 {
		if !ValidStream(l.Stream) || l.Seq < 1 {
			return fmt.Errorf("a record locator needs a stream and a seq ≥ 1")
		}
		shapes++
	}
	if l.Commit != "" {
		if !commitSHA.MatchString(l.Commit) {
			return fmt.Errorf("commit is not a hex SHA")
		}
		shapes++
	}
	if l.PR != 0 {
		if l.PR < 1 {
			return fmt.Errorf("pr must be positive")
		}
		shapes++
	}
	if l.Entry != "" {
		if !entryRef.MatchString(l.Entry) || strings.Contains(l.Entry, "..") {
			return fmt.Errorf("entry is not an entry ref")
		}
		shapes++
	}
	if shapes != 1 {
		return fmt.Errorf("a locator names exactly ONE place (a record, a commit, a PR or an entry); this names %d", shapes)
	}
	return nil
}

// DecodeEnvelope reads a POSTed envelope strictly.
func DecodeEnvelope(raw []byte) (Envelope, error) {
	var e Envelope
	if len(raw) > MaxEnvelopeBytes {
		return e, fmt.Errorf("the envelope is over %d bytes", MaxEnvelopeBytes)
	}
	if err := strict(raw, &e); err != nil {
		return e, fmt.Errorf("the envelope does not decode: %v", err)
	}
	if e.Model == "" || len(e.Model) > maxModel || !printable(e.Model) {
		return e, fmt.Errorf("model must be 1–%d bytes with no control character", maxModel)
	}
	if len(e.InputWatermark) == 0 || len(e.InputWatermark) > maxStreams {
		return e, fmt.Errorf("input_watermark must name 1–%d streams", maxStreams)
	}
	for stream, seq := range e.InputWatermark {
		if !ValidStream(stream) || seq < 0 {
			return e, fmt.Errorf("input_watermark holds a bad stream or a negative seq")
		}
	}
	if len(e.Body) == 0 {
		return e, fmt.Errorf("body is required")
	}
	body, err := CanonicalBody(e.Type, e.Body)
	if err != nil {
		return e, fmt.Errorf("body: %v", err)
	}
	e.Body = body
	return e, nil
}

// Stamp turns an accepted envelope into the stored output: the id and the clock are the pod's, and
// so are the plugin's NAME and VERSION — read off the registry entry the TOKEN named.
func Stamp(e Envelope, plugin Entry, now time.Time) (Output, error) {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return Output{}, err
	}
	return Output{
		ID:             "o-" + hex.EncodeToString(buf),
		Plugin:         plugin.Name,
		PluginVersion:  plugin.Version,
		Model:          e.Model,
		Session:        e.Session,
		InputWatermark: e.InputWatermark,
		Type:           e.Type,
		Body:           e.Body,
		ProducedAt:     now.UTC().Format(time.RFC3339),
	}, nil
}
