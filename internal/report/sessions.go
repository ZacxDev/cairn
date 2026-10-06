package report

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/ZacxDev/cairn/internal/store"
	"github.com/ZacxDev/cairn/internal/touch"
)

// 🔴 THE `sessions` ANSWER — "WHICH SESSIONS WROTE HERE" — AND THE ONE RENDERER BOTH SURFACES RUN.
// The pod's `GET sessions/<scope>` reaches it through `Reader.Sessions`; the Go client's `cairn
// sessions` calls `Sessions` + `RenderText` + `Exit` directly over its synced cache. Neither has a
// second spelling of any line below, so pod and CLI agree byte-for-byte on the report by
// construction, and `internal/client`'s `TestTheSessionsReportIsByteIdenticalOnPodAndCLI` measures
// it over one synthetic store.
//
// 🔴 IT IS GO-ONLY BY DECISION (decision 3 of `claudedocs/plan-cairn-arcs-sessions.md`): the
// oracle has no twin, so there is no differential fixture for these bytes. The contract witnesses
// are the literal expected bodies in `sessions_test.go`; the conformance goldens for the route are
// recorded from `cmd/cairn-server` and are CHANGE DETECTORS, not oracle witnesses.
//
// 🔴 NOTHING HERE PRINTS A PATH OR A HOST. `recall` prints the store root and the host, which is why
// a pod body and a CLI stdout of the same recall can never be equal. This report names only the
// scope, so the property the byte-identity test pins is reachable at all.

// The status tokens of a sessions report. Each is a DISTINCT MECHANISM, and no two render alike:
//
//   - `scope-absent`: the read index holds no such scope FOR THIS CALLER — never existed, refused,
//     or (from a cache) not synced. The three are indistinguishable BY DESIGN: refused == absent.
//   - `scope-empty`: the scope exists and holds no entry, and nothing in it was rejected.
//   - `scope-unreadable`: the scope holds entry files and NONE could be scanned (every one rejected
//     by the index, or unreadable). Reused from `recall` so `ExitFor`'s one decision maps it to 3.
//   - `no-attributed-writes`: bullets were scanned and none carries a write trailer — their
//     writers are UNKNOWN, not absent. Also the status when entries hold no nuance bullet at all
//     (`0 of 0`), because the counts line already says which.
//   - `sessions-listed`: at least one attributed bullet; the list follows.
const (
	StatusNoAttributedWrites = "no-attributed-writes"
	StatusSessionsListed     = "sessions-listed"
)

// The three coverage sentences every sessions answer carries, whatever its status. Fixed text,
// pinned as WHOLE strings by `sessions_test.go` — a guard on words is walkable by rewording.
//
// ⚠ `SessionsReadsLine` IS A VALUE, NOT A LITERAL IN THE RENDERER, because the plan's reads phase
// turns it into a measured statement; the seam is this constant and nothing else reads around it.
const (
	SessionsReadsLine       = "coverage: writes measured from entry trailers · reads NOT recorded (not collected in this phase)"
	SessionsAttributionLine = "attribution: trailers are self-reported — actor as written in the entry (only appended bullets had it set by the pod); session ids are declared by the writer"
)

// SessionsReport is one deterministic answer to "which sessions wrote attributed bullets in this
// scope?". Plain values; nothing here knows a server exists.
type SessionsReport struct {
	Status string
	// Scope is the NORMALIZED scope name — what the body prints and what the exit label reads.
	Scope string
	// Coverage is `touch.Writes`'s own counts, carried unchanged so no count is re-derived here.
	// Zero on `scope-absent`, where nothing was looked at.
	Coverage touch.Coverage
	// Sessions in `touch.Result`'s order: by session id, byte-wise ascending.
	Sessions []touch.Session
}

// Sessions derives the report for one scope from `storeRoot`, through an index narrowed to
// `visible`.
//
// 🔴 `visible` IS PASSED, NEVER RE-DERIVED — the rule `Recall` states. A scope the caller may not
// read is absent from the INDEX, so `touch.Writes` answers `*store.UnknownScopeError` for it,
// exactly as for a scope that never existed, and both become `scope-absent`. There is no second
// authorisation check here to drift from the first; the pod passes `rq.visible` (the same value
// `recall` gets) and the CLI passes `store.Unrestricted()` over a cache the pod already narrowed.
//
// Errors are the store's own and classifiable with `errors.As`: a missing store root is
// `*store.StoreMissingError` (the pod's 503, the CLI's banner), the same as `Recall`.
func Sessions(storeRoot, scope string, visible store.ScopeSet) (SessionsReport, error) {
	index, err := store.LoadStore(storeRoot, "scanned", visible)
	if err != nil {
		return SessionsReport{}, err
	}
	res, err := touch.Writes(storeRoot, index, scope)
	if err != nil {
		var unknown *store.UnknownScopeError
		if !errors.As(err, &unknown) {
			return SessionsReport{}, err
		}
		return SessionsReport{Status: StatusScopeAbsent, Scope: store.NormalizeRef(scope)}, nil
	}
	rep := SessionsReport{Scope: res.Scope, Coverage: res.Coverage, Sessions: res.Sessions}
	c := res.Coverage
	scanned := c.Entries - c.EntriesUnreadable
	switch {
	case c.Entries == 0 && c.EntriesMalformed == 0:
		rep.Status = StatusScopeEmpty
	// 🔴 BEFORE THE ATTRIBUTION BRANCHES. A scope whose every file was rejected or unreadable
	// scanned ZERO bullets, which would otherwise read as `no-attributed-writes` — a claim about
	// bullets nobody looked at.
	case scanned == 0:
		rep.Status = StatusScopeUnreadable
	case c.Attributed == 0:
		rep.Status = StatusNoAttributedWrites
	default:
		rep.Status = StatusSessionsListed
	}
	return rep, nil
}

// Partial is true when some entries in the scope were never scanned, so every count is a LOWER
// bound. It is the one predicate the renderer's lower-bound line reads.
func (r SessionsReport) Partial() bool {
	return r.Coverage.EntriesMalformed > 0 || r.Coverage.EntriesUnreadable > 0
}

// AttributedLine is the denominator sentence, printed on every status that scanned anything.
func (r SessionsReport) AttributedLine() string {
	c := r.Coverage
	return fmt.Sprintf("attributed: %d of %d bullets carry a write trailer (%d have none — their writers are NOT listed)",
		c.Attributed, c.Bullets, c.Bullets-c.Attributed)
}

// ScannedLine is what was and was not looked at — the counts that make an empty list honest.
func (r SessionsReport) ScannedLine() string {
	c := r.Coverage
	return fmt.Sprintf("scanned: %d of %d entries · %d unreadable · %d rejected by the index · %d bullets with a refused trailer",
		c.Entries-c.EntriesUnreadable, c.Entries+c.EntriesMalformed, c.EntriesUnreadable,
		c.EntriesMalformed, c.MalformedTrailers)
}

// RenderText is the report as both surfaces print it. No trailing newline (the pod's
// `serveReport` and the CLI each add their own, exactly as they do for `recall`).
//
// The header and the two fixed coverage lines print on EVERY status, `scope-absent` included,
// because a reader of an empty answer is the reader who most needs to know reads were never
// measured. The count lines print wherever something was looked at.
//
// 🔴 EVERY LINE IT PRINTS COMES FROM AN EXPORTED METHOD OR CONSTANT, BECAUSE THE BROWSER SURFACE
// PRINTS THE SAME CLAIMS IN A DIFFERENT LAYOUT. `internal/ui` reads `ScannedLine`,
// `AttributedLine`, `LowerBoundLine`, `StatusSentence`, `ListHeading` and `SessionLine` rather than
// spelling a second copy of any of them, so the page and the pod cannot disagree about a count or
// a caveat — only about where it sits.
func (r SessionsReport) RenderText() string {
	var b strings.Builder
	fmt.Fprintf(&b, "cairn-sessions: status=%s scope=%s\n", r.Status, r.Scope)
	b.WriteString("  " + SessionsReadsLine + "\n")
	b.WriteString("  " + SessionsAttributionLine + "\n")
	if r.Status != StatusScopeAbsent {
		b.WriteString("  " + r.ScannedLine() + "\n")
		b.WriteString("  " + r.AttributedLine() + "\n")
	}
	if lower := r.LowerBoundLine(); lower != "" {
		b.WriteString("  " + lower + "\n")
	}
	b.WriteString("\n")
	if sentence := r.StatusSentence(); sentence != "" {
		b.WriteString(sentence)
		return b.String()
	}
	b.WriteString(r.ListHeading())
	for _, s := range r.Sessions {
		b.WriteString("\n- " + SessionLine(s))
	}
	return b.String()
}

// LowerBoundLine is the warning that some entries were never scanned, or "" when none applies.
func (r SessionsReport) LowerBoundLine() string {
	if !r.Partial() || r.Status == StatusScopeUnreadable {
		return ""
	}
	return fmt.Sprintf("⚠ LOWER BOUND — %d entry file(s) in `%s/` were never scanned, so a session that wrote only there is NOT listed. `cairn validate --scope %s` names each file that fails to parse.",
		r.Coverage.EntriesMalformed+r.Coverage.EntriesUnreadable, r.Scope, r.Scope)
}

// StatusSentence is the answer's body for every status that lists NOTHING, and "" for
// `sessions-listed`, whose body is `ListHeading` plus one `SessionLine` per session.
func (r SessionsReport) StatusSentence() string {
	switch r.Status {
	case StatusScopeAbsent:
		return fmt.Sprintf("NO SCOPE `%s/` IS READABLE HERE. A scope that does not exist and one this credential may not read answer identically, by design; a cache that has not synced it answers the same way. Nothing was scanned, so nothing can be concluded about who wrote there.", r.Scope)
	case StatusScopeEmpty:
		return fmt.Sprintf("`%s/` EXISTS AND HOLDS NO ENTRY — nothing was ever recorded there, so no session wrote there through an entry.", r.Scope)
	case StatusScopeUnreadable:
		return fmt.Sprintf("NOTHING IN `%s/` COULD BE SCANNED — every entry file was rejected by the index or could not be read. This is NOT an empty scope and NOT 'no attributed writes': the bullets were never looked at. `cairn validate --scope %s` names each file that fails to parse.", r.Scope, r.Scope)
	case StatusNoAttributedWrites:
		return fmt.Sprintf("NO ATTRIBUTED WRITES — %d bullet(s) in `%s/` were scanned and none carries a write trailer. Their writers are UNKNOWN, not absent: a bullet written before trailers existed, or through `put`/`create` without one, names nobody.", r.Coverage.Bullets, r.Scope)
	}
	return ""
}

// ListHeading is the line above a `sessions-listed` answer's rows.
func (r SessionsReport) ListHeading() string {
	return fmt.Sprintf("sessions: %d distinct, ordered by session id (byte-wise); dates are each bullet's own `- YYYY-MM-DD:` opener", len(r.Sessions))
}

// SessionLine is one session's row: id, actor(s) as written, bullet count, date span. Exported so
// the browser surface prints these bytes rather than a second spelling of them.
func SessionLine(s touch.Session) string {
	label := "actor"
	if len(s.Actors) > 1 {
		label = "actors"
	}
	n := len(s.Bullets)
	return s.ID + " · " + label + " " + strings.Join(s.Actors, ", ") + " · " +
		strconv.Itoa(n) + " bullet" + plural(n) + " · " + dateSpan(s)
}

func dateSpan(s touch.Session) string {
	var span string
	switch {
	case s.FirstDate == "":
		return "undated"
	case s.FirstDate == s.LastDate:
		span = s.FirstDate
	default:
		span = s.FirstDate + " → " + s.LastDate
	}
	if s.Undated > 0 {
		span += " (+" + strconv.Itoa(s.Undated) + " undated)"
	}
	return span
}

// Exit is the exit code and the one warning line, for both surfaces.
//
// 🔴 THE CODE IS `ExitFor`'S DECISION, NOT A SECOND ONE: `scope-unreadable` is in
// `UnreadableStatuses`, so it maps to 3 and every other status to 0 — the existing read contract,
// no new constant. Only the WARNING is this report's own, because `ExitFor`'s sentence counts
// MALFORMED files and a sessions scope can be unscannable through a read error too.
func (r SessionsReport) Exit() (int, string) {
	code, _ := ExitFor(r.Status, r.Scope+"/", nil)
	if code == 0 {
		return 0, ""
	}
	c := r.Coverage
	return code, fmt.Sprintf("cairn-sessions: %s: none of the %d entry file(s) under `%s/` could be scanned (%d rejected by the index, %d unreadable) — this is NOT an empty scope and NOT 'no attributed writes'.",
		r.Status, c.Entries+c.EntriesMalformed, r.Scope, c.EntriesMalformed, c.EntriesUnreadable)
}
