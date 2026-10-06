// Package arcs is the ARC REGISTRY: which efforts (one handoff doc in one repo) the operator
// tooling has registered, stored in an append-only journal OUTSIDE the store tree.
//
// 🔴 AN ARC IS REGISTERED, NEVER DERIVED. A session→scope write edge is read off the bytes the
// store already holds (`internal/touch`); an arc, its declared scopes and its members exist
// only because the tooling pushed them with `PUT arc/<home>/<slug>`. This package is the
// payload's ONE validator, the journal's ONE reader and its ONE writer — the pod calls it, and
// nothing else in the tree knows the record shape.
//
// 🔴 A LIBRARY, NOT A HANDLER — the `internal/report` shape. Plain values in, plain values out,
// no `net/http` type, every error classifiable with `errors.As` (`*PayloadError` is the caller's
// fault and a 400; `*JournalUnreadableError` is "could not look" and a 503; `*InsideStoreError`
// is a refusal to start). It is stdlib-only and inside `depspolicy.LinkedBinaryRoots`' closure.
//
// 🔴 WHAT A REGISTRATION PROVES, AND DOES NOT. Every field but `registered_by` and
// `registered_at` is the tooling's own word: the members are whoever IT saw stamp commits or
// resume the doc, and the status is ITS verdict. The pod stamps who pushed (the authenticated
// principal's display name, never the body) and when (the pod clock). Nothing here can check
// the rest, and every rendered answer says so.
package arcs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/ZacxDev/cairn/internal/authz"
	"github.com/ZacxDev/cairn/internal/store"
	"github.com/ZacxDev/cairn/internal/write"
)

// Schema is the one payload and record schema this package reads and writes.
const Schema = 1

// The three statuses. 🔴 `unknown` IS A STATUS, NOT A MISSING VALUE (operator decision Q4): a
// payload that sends none is `unknown`, and it is never rendered as, sorted with, or counted as
// `open`. Tooling that cannot compute a verdict says so by omitting it.
const (
	StatusOpen    = "open"
	StatusClosed  = "closed"
	StatusUnknown = "unknown"
)

// The closing-condition kinds the tooling's handoff writer parses, plus `none` for a doc that
// declares no closing condition.
const (
	ClosingCheck     = "check"
	ClosingJudgement = "judgement"
	ClosingNone      = "none"
)

// Roles are the tooling's `ArcReport.members[].role` values. 🔴 `resumed` IS THE READER ROLE —
// it comes from a transcript kickoff line, the leg `readers_measured` describes — and every
// other role comes from `Claude-Session-Id:` commit trailers, the leg `writers_measured`
// describes. Merge rule 7 keys on exactly that split, so it is spelled once, here.
var (
	writerRoles = []string{"originated", "earliest-stamped", "wrote"}
	readerRoles = []string{"resumed"}
)

// Bounds on one payload. Generous against the tooling's real output and finite so one request
// cannot make the pod hold an unbounded member list per record forever (Q7: nothing is ever
// compacted, so a record's size is paid on every later read).
const (
	MaxDeclaredScopes = 64
	MaxMembers        = 512
)

// Member is one session the tooling attributes to an arc.
type Member struct {
	// Session is opaque and byte-exact — never lowercased, never shape-checked beyond
	// `write.SessionComponent`, which is what makes it JOINABLE with a trailer's session.
	Session string `json:"session"`
	Role    string `json:"role"`
	// FirstSeen is RFC 3339 or "" (the tooling could not date it).
	FirstSeen string `json:"first_seen"`
	// Carried is set by the POD, never the payload: this member was kept from an earlier
	// registration because the push that replaced it did not measure this member's leg (merge
	// rule 7). A payload carrying the key is refused, so the tooling cannot fake it.
	Carried bool `json:"carried,omitempty"`
}

// IsReader is true for the transcript-derived role.
func (m Member) IsReader() bool { return slices.Contains(readerRoles, m.Role) }

// Registration is ONE journal record and ONE arc's state as displayed. The fold keeps the last
// valid record per key; every earlier one stays in the file (Q7).
type Registration struct {
	Schema           int      `json:"schema"`
	Home             string   `json:"home"`
	Slug             string   `json:"slug"`
	Status           string   `json:"status"`
	ClosingKind      string   `json:"closing_kind"`
	DeclaredScopes   []string `json:"declared_scopes"`
	WritersMeasured  bool     `json:"writers_measured"`
	ReadersMeasured  bool     `json:"readers_measured"`
	CommitsTotal     int      `json:"commits_total"`
	CommitsUnstamped int      `json:"commits_unstamped"`
	ReportedAt       string   `json:"reported_at"`
	Members          []Member `json:"members"`
	// RegisteredBy is the authenticated principal's DISPLAY name — `rq.identity`, the same value
	// the append route writes as a trailer's actor. RegisteredAt is the pod clock, RFC 3339 UTC.
	RegisteredBy string `json:"registered_by"`
	RegisteredAt string `json:"registered_at"`
}

// Key is the arc's identity: `(home scope, slug)`, both normalized.
type Key struct{ Home, Slug string }

// Key returns this registration's key.
func (r Registration) Key() Key { return Key{r.Home, r.Slug} }

// Declares is true when `scope` (any spelling) is one of the declared scopes.
func (r Registration) Declares(scope string) bool {
	return slices.Contains(r.DeclaredScopes, store.NormalizeRef(scope))
}

// PayloadError is a request body this package refuses. The message rides the 400.
type PayloadError struct{ Message string }

func (e *PayloadError) Error() string { return e.Message }

func payloadErr(format string, args ...any) error {
	return &PayloadError{Message: fmt.Sprintf(format, args...)}
}

// payload is the request body's shape. Pointers separate "absent" from a zero value, because
// an absent status is `unknown` and an absent count is 0 — two different defaults, both stated.
type payload struct {
	Schema           *int            `json:"schema"`
	Status           *string         `json:"status"`
	ClosingKind      *string         `json:"closing_kind"`
	DeclaredScopes   []string        `json:"declared_scopes"`
	WritersMeasured  *bool           `json:"writers_measured"`
	ReadersMeasured  *bool           `json:"readers_measured"`
	CommitsTotal     *int            `json:"commits_total"`
	CommitsUnstamped *int            `json:"commits_unstamped"`
	ReportedAt       *string         `json:"reported_at"`
	Members          []payloadMember `json:"members"`
}

type payloadMember struct {
	Session   string `json:"session"`
	Role      string `json:"role"`
	FirstSeen string `json:"first_seen"`
}

// NormalizeKey folds a home and a slug the way every lookup does, refusing either one that
// folds to nothing. Both arrive as URL path components, which `authz.SafePathComponent` has
// already vetted, so the fold is the store's own `NormalizeRef` and nothing more.
func NormalizeKey(home, slug string) (Key, error) {
	k := Key{store.NormalizeRef(home), store.NormalizeRef(slug)}
	if k.Home == "" || k.Slug == "" {
		return Key{}, payloadErr("the home scope and the slug must each normalize to a non-empty slug")
	}
	return k, nil
}

// DecodePayload validates a `PUT arc/<home>/<slug>` body into the registration it would record,
// minus the two pod-stamped fields. `unjoinable` counts members whose session id could never
// equal a trailer's (`write.SessionComponent` refuses it): they are NOT stored, and the response
// says how many, so the tooling can report them rather than lose them silently.
//
// 🔴 UNKNOWN FIELDS ARE REFUSED, NOT IGNORED. The plan's "never stored" list — the doc body,
// commit subjects and SHAs, the tooling's free-text notes, transcript text, any filesystem path
// — is enforced here by construction: a key this function does not name cannot reach the
// journal, and a sender who added one is told rather than silently dropped.
func DecodePayload(body []byte, home, slug string) (reg Registration, unjoinable int, err error) {
	key, err := NormalizeKey(home, slug)
	if err != nil {
		return Registration{}, 0, err
	}
	var p payload
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return Registration{}, 0, payloadErr("body must be one JSON object of the registration schema (%s)", err.Error())
	}
	if _, err := dec.Token(); err != io.EOF {
		return Registration{}, 0, payloadErr("body must be ONE JSON object; trailing data follows it")
	}
	if p.Schema == nil || *p.Schema != Schema {
		return Registration{}, 0, payloadErr("schema must be %d", Schema)
	}
	reg = Registration{Schema: Schema, Home: key.Home, Slug: key.Slug,
		Status: StatusUnknown, ClosingKind: ClosingNone}
	if p.Status != nil {
		switch *p.Status {
		case StatusOpen, StatusClosed, StatusUnknown:
			reg.Status = *p.Status
		default:
			return Registration{}, 0, payloadErr("status must be open, closed or unknown (omit it when the tool has no verdict), got %s", store.PyRepr(*p.Status))
		}
	}
	if p.ClosingKind != nil {
		switch *p.ClosingKind {
		case ClosingCheck, ClosingJudgement, ClosingNone:
			reg.ClosingKind = *p.ClosingKind
		default:
			return Registration{}, 0, payloadErr("closing_kind must be check, judgement or none, got %s", store.PyRepr(*p.ClosingKind))
		}
	}
	scopes, err := declaredScopes(p.DeclaredScopes, key.Home)
	if err != nil {
		return Registration{}, 0, err
	}
	reg.DeclaredScopes = scopes
	if p.WritersMeasured != nil {
		reg.WritersMeasured = *p.WritersMeasured
	}
	if p.ReadersMeasured != nil {
		reg.ReadersMeasured = *p.ReadersMeasured
	}
	if p.CommitsTotal != nil {
		reg.CommitsTotal = *p.CommitsTotal
	}
	if p.CommitsUnstamped != nil {
		reg.CommitsUnstamped = *p.CommitsUnstamped
	}
	if reg.CommitsTotal < 0 || reg.CommitsUnstamped < 0 || reg.CommitsUnstamped > reg.CommitsTotal {
		return Registration{}, 0, payloadErr("commits_total and commits_unstamped must be non-negative with commits_unstamped <= commits_total, got %d and %d", reg.CommitsTotal, reg.CommitsUnstamped)
	}
	if p.ReportedAt != nil && *p.ReportedAt != "" {
		if !validTime(*p.ReportedAt) {
			return Registration{}, 0, payloadErr("reported_at must be RFC 3339 or empty, got %s", store.PyRepr(*p.ReportedAt))
		}
		reg.ReportedAt = *p.ReportedAt
	}
	if len(p.Members) > MaxMembers {
		return Registration{}, 0, payloadErr("at most %d members, got %d", MaxMembers, len(p.Members))
	}
	seen := map[string]bool{}
	reg.Members = []Member{}
	for i, m := range p.Members {
		if !slices.Contains(writerRoles, m.Role) && !slices.Contains(readerRoles, m.Role) {
			return Registration{}, 0, payloadErr("members[%d].role must be one of %s, got %s", i,
				strings.Join(append(append([]string{}, writerRoles...), readerRoles...), ", "), store.PyRepr(m.Role))
		}
		if m.FirstSeen != "" && !validTime(m.FirstSeen) {
			return Registration{}, 0, payloadErr("members[%d].first_seen must be RFC 3339 or empty, got %s", i, store.PyRepr(m.FirstSeen))
		}
		if !write.SessionComponent.MatchString(m.Session) {
			unjoinable++
			continue
		}
		if seen[m.Session] {
			// Refused rather than resolved: two roles for one session is a question about the
			// tooling's report this package cannot answer by picking one.
			return Registration{}, 0, payloadErr("members[%d]: session %s appears twice", i, store.PyRepr(m.Session))
		}
		seen[m.Session] = true
		reg.Members = append(reg.Members, Member{Session: m.Session, Role: m.Role, FirstSeen: m.FirstSeen})
	}
	return reg, unjoinable, nil
}

// declaredScopes folds, deduplicates and sorts the declared scopes, adding the home scope —
// which is ALWAYS declared (design decision 5).
//
// Each must be spelled as a URL path component would be (`authz.SafePathComponent`), because
// that is the only way a scope is ever addressed: a dot-prefixed or slash-bearing name here
// could name nothing a reader can ask for.
func declaredScopes(raw []string, home string) ([]string, error) {
	if len(raw) > MaxDeclaredScopes {
		return nil, payloadErr("at most %d declared_scopes, got %d", MaxDeclaredScopes, len(raw))
	}
	set := map[string]bool{home: true}
	for i, s := range raw {
		if !authz.SafePathComponent.MatchString(s) {
			return nil, payloadErr("declared_scopes[%d] must match %s, got %s", i, authz.SafePathComponentPattern, store.PyRepr(s))
		}
		folded := store.NormalizeRef(s)
		if folded == "" {
			return nil, payloadErr("declared_scopes[%d] normalizes to nothing: %s", i, store.PyRepr(s))
		}
		set[folded] = true
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	slices.Sort(out)
	return out, nil
}

func validTime(s string) bool {
	_, err := time.Parse(time.RFC3339, s)
	return err == nil
}

// Merge applies MERGE RULE 7 (design decision 7): registration REPLACES state per key, but an
// UNMEASURED leg never erases a MEASURED one.
//
// 🔴 THE TOOLING PUSHES ITS WHOLE CURRENT REPORT ON EVERY HANDOFF, FROM WHICHEVER HOST RAN IT.
// A push with `readers_measured: false` comes from a host whose transcript corpus was not
// walked; that is not evidence that the readers another host measured have vanished. So every
// member of the unmeasured leg's roles in `prev` that `next` does not name is CARRIED into
// `next`, marked `Carried`, rather than dropped. The same rule holds for the writer leg. A leg
// the new push DID measure replaces the old one outright — that is what "replaces" means.
//
// `prev` is nil for a first registration, which carries nothing.
func Merge(prev *Registration, next Registration) Registration {
	if prev == nil {
		return next
	}
	named := map[string]bool{}
	for _, m := range next.Members {
		named[m.Session] = true
	}
	members := append([]Member{}, next.Members...)
	for _, m := range prev.Members {
		if named[m.Session] {
			continue
		}
		unmeasured := (m.IsReader() && !next.ReadersMeasured) || (!m.IsReader() && !next.WritersMeasured)
		if !unmeasured {
			continue
		}
		m.Carried = true
		members = append(members, m)
	}
	next.Members = members
	return next
}

// sameState is "a re-push that would change nothing a reader sees": every field equal except
// the pod's own `registered_at`. A different principal re-pushing identical content IS a change,
// because `registered_by` is displayed.
func sameState(a, b Registration) bool {
	a.RegisteredAt, b.RegisteredAt = "", ""
	ja, errA := json.Marshal(a)
	jb, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(ja, jb)
}

// validRecord is the READ-side check on one journal line: the same invariants a payload must
// meet, plus the two pod-stamped fields. A line failing it is SKIPPED and counted, never
// applied — see `Read`.
func validRecord(r Registration) bool {
	if r.Schema != Schema || r.Home == "" || r.Slug == "" ||
		store.NormalizeRef(r.Home) != r.Home || store.NormalizeRef(r.Slug) != r.Slug {
		return false
	}
	switch r.Status {
	case StatusOpen, StatusClosed, StatusUnknown:
	default:
		return false
	}
	switch r.ClosingKind {
	case ClosingCheck, ClosingJudgement, ClosingNone:
	default:
		return false
	}
	if !slices.Contains(r.DeclaredScopes, r.Home) || r.RegisteredAt == "" || !validTime(r.RegisteredAt) {
		return false
	}
	for _, m := range r.Members {
		if !write.SessionComponent.MatchString(m.Session) {
			return false
		}
		if !slices.Contains(writerRoles, m.Role) && !slices.Contains(readerRoles, m.Role) {
			return false
		}
	}
	return r.CommitsTotal >= 0 && r.CommitsUnstamped >= 0 && r.CommitsUnstamped <= r.CommitsTotal
}
