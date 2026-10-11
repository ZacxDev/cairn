package memo

import (
	"strconv"
	"strings"
	"time"
)

// Fields is the INPUT a caller builds a [Stored] from: one memo's columns, plus the
// authority's answers about its sender and retractor, exactly as stored or resolved.
//
// It carries the caller's OWN text INTO this package and nothing in this package returns or
// retains one: [NewStored] copies it behind a pointer. Parameters are deliberately not
// constrained (plan, Guard C's round-5 scope correction) — the store must be able to hand over
// what it read. What Guard A constrains is how text gets back OUT.
//
// Every string field is interpolated into some output, so every one passes through the
// render-side replacement; `TestEveryFieldsStringIsSanitisedOnEveryOutput` plants an unsafe
// code point in EVERY string field by reflection, so a field added here and printed raw fails it
// without anyone remembering to extend a list.
type Fields struct {
	ID    int64
	Scope string

	// SenderKind and SenderID are the principal's stable key; SenderDisplay is the authority's
	// display for it, resolved at READ time ("" when the principal no longer resolves).
	SenderKind, SenderID, SenderDisplay string
	// CredentialLabel is the operator-written label of the credential that sent it ("" when
	// none, e.g. a browser session).
	CredentialLabel string

	Subject, Body string

	CreatedAt, ExpiresAt time.Time

	// RetractedAt is zero for a live memo. The retractor is resolved the way the sender is.
	RetractedAt                                                  time.Time
	RetractorKind, RetractorID, RetractorDisplay, RetractorLabel string
}

// Stored is one memo's stored text. It has NO exported field and NO method: outside this
// package its text is reachable only through [Render], [RenderPreview] and [RenderFull], each of
// which applies the render-side replacement first (Guard A).
//
// 🔴 THE TEXT IS BEHIND A FUNC, NOT A PLAIN POINTER, AND BOTH HALVES OF THAT ARE MEASURED
// (Guard B; go1.25.14, every verb %a–%z, %A–%Z, %v, %+v, %#v, over eight holders: direct,
// behind a pointer, in an exported field, in an UNEXPORTED field, two unexported levels deep, in
// a slice, in a map, as `any`).
//   - `fmt` prints unexported fields, and a value nested in one bypasses its own
//     String/GoString/Format, so methods cannot be the guard. A VALUE field leaked in 429 of 440.
//   - A plain POINTER (`p *stored`, which the plan adopted from a three-verb measurement) is
//     printed as an address by `%v`/`%+v`/`%#v` — but every verb that is invalid for a pointer
//     (`%s`, `%q`, `%c`, …) takes fmt's bad-verb path, which prints `%!s(*memo.stored=&{…})`:
//     the pointee, CONTENTS INCLUDED. 344 of 440 leaked.
//   - A func is never dereferenced or called by `fmt`, under any verb: 0 of 440. (A
//     pointer-to-pointer also measured 0; the func is the shape whose reason does not depend on
//     how many levels `fmt` happens to expand.)
//
// `TestGuardBNoStoredTextReachesFmt` is the behavioural pin over that whole grid; a value field
// and a single pointer are each a mutant it turns red.
type Stored struct {
	text func() *stored
}

type stored struct {
	id                                  int64
	scope                               string
	senderKind, senderID, senderDisplay string
	credentialLabel                     string
	subject, body                       string
	createdAt, expiresAt, retractedAt   time.Time
	retractorKind, retractorID          string
	retractorDisplay, retractorLabel    string
}

var zero stored

// t is the ONE accessor, so the carrier's shape above is two declarations (this and NewStored).
func (s Stored) t() *stored {
	if s.text == nil {
		return &zero
	}
	return s.text()
}

// NewStored copies f into a [Stored]. The copy is made here so a caller mutating f afterwards
// cannot change what was rendered.
func NewStored(f Fields) Stored {
	p := &stored{
		id: f.ID, scope: f.Scope,
		senderKind: f.SenderKind, senderID: f.SenderID, senderDisplay: f.SenderDisplay,
		credentialLabel: f.CredentialLabel,
		subject:         f.Subject, body: f.Body,
		createdAt: f.CreatedAt, expiresAt: f.ExpiresAt, retractedAt: f.RetractedAt,
		retractorKind: f.RetractorKind, retractorID: f.RetractorID,
		retractorDisplay: f.RetractorDisplay, retractorLabel: f.RetractorLabel,
	}
	return Stored{text: func() *stored { return p }}
}

// Rendered is one memo with EVERY text field already passed through the render-side
// replacement — the type the listener's JSON (S2) and the browser tab (S5) are built from
// (decision 19). Its string fields are display strings, never stored text.
type Rendered struct {
	ID    int64
	Scope string
	// Sender is the sender line: "<display> via <label> (<kind>)", or
	// "<kind>:<id> (no longer known) via <label>" when the principal no longer resolves.
	Sender string
	// CreatedAt is RFC 3339 in UTC; ExpiresAt is the UTC date.
	CreatedAt, ExpiresAt string
	// Subject and Body are empty for a retracted memo, whose Tombstone is then set:
	// "retracted by <display> via <label> (<kind>) at <time>".
	Subject, Body, Tombstone string
}

// Render applies the render-side replacement to every field of s.
func Render(s Stored) Rendered {
	t := s.t()
	r := Rendered{
		ID:        t.id,
		Scope:     sanitize(t.scope),
		Sender:    who(t.senderKind, t.senderID, t.senderDisplay, t.credentialLabel),
		CreatedAt: t.createdAt.UTC().Format(time.RFC3339),
		ExpiresAt: t.expiresAt.UTC().Format(time.DateOnly),
	}
	if !t.retractedAt.IsZero() {
		r.Tombstone = "retracted by " +
			who(t.retractorKind, t.retractorID, t.retractorDisplay, t.retractorLabel) +
			" at " + t.retractedAt.UTC().Format(time.RFC3339)
		return r
	}
	r.Subject = sanitize(t.subject)
	r.Body = sanitize(t.body)
	return r
}

// who is the sender (or retractor) line, built ONLY from the authority's answers (decision 5):
// nothing in a memo's body can set it. Every part is sanitised.
func who(kind, id, display, label string) string {
	var b strings.Builder
	if display == "" {
		b.WriteString(sanitize(kind) + ":" + sanitize(id) + " (no longer known)")
		if label != "" {
			b.WriteString(" via " + sanitize(label))
		}
		return b.String()
	}
	b.WriteString(sanitize(display))
	if label != "" {
		b.WriteString(" via " + sanitize(label))
	}
	b.WriteString(" (" + sanitize(kind) + ")")
	return b.String()
}

func idString(id int64) string { return strconv.FormatInt(id, 10) }
