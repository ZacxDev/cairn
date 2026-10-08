package presence

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ZacxDev/cairn/internal/write"
)

// The wire contract between cairn and the host side (the plan's "Contract" section, decision 8).
const (
	// Schema is the one wire schema version.
	Schema = 1
	// MaxRows bounds a push.
	MaxRows = 256
	// MaxStringBytes bounds every string on the wire.
	MaxStringBytes = 128
	// MaxPushBody bounds the request body before decoding; anything larger is refused unread.
	//
	// ⚠ 1 MiB, AND THE 512 KiB IT REPLACED REFUSED A LEGAL PUSH. The worst legal push — every
	// field at its bound: MaxRows rows, a 64-byte session, `target`, `label` and `hotkey` each
	// MaxStringBytes of `<` (which Go's default `json.Marshal` escapes to six bytes apiece), a
	// MaxStringBytes `last_activity` (RFC 3339 with a long fractional second, which `time.Parse`
	// accepts) and a 64-byte host — is over 512 KiB and under this cap. Its size is pinned, in ONE
	// place, by `TestAWorstCaseLegalPushIsAccepted`, which also proves the push is accepted; no
	// number is repeated here, because a copy of it here has already gone stale once.
	MaxPushBody = 1 << 20
	// MaxClaimBody bounds a claim request, whose only valid body is `{}`.
	MaxClaimBody = 1 << 10
)

// Runtimes is the closed set a row's `runtime` must be in.
var Runtimes = []string{"claude", "opencode", "other"}

// PushKeys and RowKeys are the EXACT wire key sets of decision 8. The decoder refuses an
// unknown key AND a missing one, so the sets are exact in both directions — growth or
// shrinkage is a 400, never a silently ignored or defaulted field.
var (
	PushKeys = []string{"schema", "host", "rows"}
	RowKeys  = []string{"session", "runtime", "target", "label", "hotkey", "last_activity"}
)

// wirePush and wireRow are the JSON shapes. Pointers so a MISSING key is distinguishable from
// an empty value; `DisallowUnknownFields` refuses the other direction — including every
// never-carried field (`pane_preview`, pane tty, pane id, window id, tmux pid, cwd, …).
type wirePush struct {
	Schema *int       `json:"schema"`
	Host   *string    `json:"host"`
	Rows   *[]wireRow `json:"rows"`
}

type wireRow struct {
	Session      *string `json:"session"`
	Runtime      *string `json:"runtime"`
	Target       *string `json:"target"`
	Label        *string `json:"label"`
	Hotkey       *string `json:"hotkey"`
	LastActivity *string `json:"last_activity"`
}

// Push is a decoded, validated presence push.
type Push struct {
	Host string
	Rows []Row
}

// decodeStrict decodes exactly one JSON value with unknown fields refused and nothing after it.
func decodeStrict(body io.Reader, v any) error {
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("body is not the expected JSON object: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("body carries data after its JSON object")
	}
	return nil
}

// checkString enforces the string bound and refuses control characters, so a display string
// can never carry a terminal escape or a line break into a log or a page.
func checkString(field, v string) error {
	if len(v) > MaxStringBytes {
		return fmt.Errorf("%s is %d bytes, over the %d-byte bound", field, len(v), MaxStringBytes)
	}
	if !utf8.ValidString(v) {
		return fmt.Errorf("%s is not valid UTF-8", field)
	}
	for _, r := range v {
		// U+2028/U+2029 are line breaks that `unicode.IsControl` does not cover (Zl, Zp).
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return fmt.Errorf("%s carries a control character", field)
		}
	}
	return nil
}

// DecodePush decodes and validates a push body. Every failure is a 400 at the route.
func DecodePush(body io.Reader) (Push, error) {
	var w wirePush
	if err := decodeStrict(body, &w); err != nil {
		return Push{}, err
	}
	if w.Schema == nil || w.Host == nil || w.Rows == nil {
		return Push{}, fmt.Errorf("a push carries exactly the keys %v; one is missing or null", PushKeys)
	}
	if *w.Schema != Schema {
		return Push{}, fmt.Errorf("schema %d is not %d", *w.Schema, Schema)
	}
	if err := checkString("host", *w.Host); err != nil {
		return Push{}, err
	}
	if !ValidHostLabel(*w.Host) {
		return Push{}, fmt.Errorf("host %q is not a host label", *w.Host)
	}
	if len(*w.Rows) > MaxRows {
		return Push{}, fmt.Errorf("%d rows, over the %d-row bound", len(*w.Rows), MaxRows)
	}
	out := Push{Host: *w.Host, Rows: make([]Row, 0, len(*w.Rows))}
	seen := map[string]bool{}
	for i, wr := range *w.Rows {
		row, err := decodeRow(wr)
		if err != nil {
			return Push{}, fmt.Errorf("row %d: %w", i, err)
		}
		// One host presents a session at most once (decision 9 (iv)); a duplicate would make
		// the `(owner, host, session)` key ambiguous.
		if seen[row.Session] {
			return Push{}, fmt.Errorf("row %d: session %q appears twice in one push", i, row.Session)
		}
		seen[row.Session] = true
		out.Rows = append(out.Rows, row)
	}
	return out, nil
}

func decodeRow(w wireRow) (Row, error) {
	if w.Session == nil || w.Runtime == nil || w.Target == nil || w.Label == nil || w.Hotkey == nil || w.LastActivity == nil {
		return Row{}, fmt.Errorf("a row carries exactly the keys %v; one is missing or null", RowKeys)
	}
	r := Row{Session: *w.Session, Runtime: *w.Runtime, Target: *w.Target, Label: *w.Label,
		Hotkey: *w.Hotkey, LastActivity: *w.LastActivity}
	for _, f := range []struct{ name, v string }{
		{"session", r.Session}, {"runtime", r.Runtime}, {"target", r.Target},
		{"label", r.Label}, {"hotkey", r.Hotkey}, {"last_activity", r.LastActivity},
	} {
		if err := checkString(f.name, f.v); err != nil {
			return Row{}, err
		}
	}
	if !write.SessionComponent.MatchString(r.Session) {
		return Row{}, fmt.Errorf("session must match %s", write.SessionComponentPattern)
	}
	known := false
	for _, rt := range Runtimes {
		if r.Runtime == rt {
			known = true
		}
	}
	if !known {
		return Row{}, fmt.Errorf("runtime %q is not one of %v", r.Runtime, Runtimes)
	}
	if r.Target == "" {
		return Row{}, errors.New("target is empty")
	}
	if r.LastActivity != "" {
		t, err := time.Parse(time.RFC3339, r.LastActivity)
		if err != nil {
			return Row{}, fmt.Errorf("last_activity is neither empty nor RFC 3339: %w", err)
		}
		r.activity = t
	}
	return r, nil
}

// DecodeClaim accepts exactly `{}`.
func DecodeClaim(body io.Reader) error {
	// A map rather than `struct{}`, because `null` decodes into an empty struct without error
	// and the contract says the body is an OBJECT.
	var w map[string]json.RawMessage
	if err := decodeStrict(body, &w); err != nil {
		return err
	}
	if w == nil || len(w) != 0 {
		return errors.New("a claim body is exactly {}")
	}
	return nil
}

// wireRing and wireClaim are the claim response's shapes.
type wireRing struct {
	RingID  string `json:"ring_id"`
	Session string `json:"session"`
}

type wireClaim struct {
	Schema int        `json:"schema"`
	Rings  []wireRing `json:"rings"`
}

// EncodeClaim renders the claim response. `rings` is always an array, never `null`.
func EncodeClaim(rings []Ring) []byte {
	out := wireClaim{Schema: Schema, Rings: []wireRing{}}
	for _, r := range rings {
		out.Rings = append(out.Rings, wireRing{RingID: r.ID, Session: r.Session})
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(out)
	return buf.Bytes()
}
