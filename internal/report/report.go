// Package report is the store's REPORT RENDERER: the digest, the index page, the
// malformed block, the sensitivity fold, the four-state discrimination, and the search
// scorer under it. It is what `/api/v1/recall/{scope}` and `/api/v1/search/{scope}`
// answer with, and it is a port of the oracle's reader function by function.
//
// 🔴 IT IS A LIBRARY, NOT A HANDLER, AND THAT IS THE WHOLE REASON THE REWRITE IS SAFE.
// The pod and the CLI must run ONE renderer: two renderers in two languages agreeing
// byte-for-byte forever is a discipline, and drift would arrive as "a different order
// that reads as a stale cache" rather than as an error. So nothing here takes or returns
// an HTTP type, nothing reads server configuration, and every error is classifiable by a
// caller that is not a handler — `store.StoreMissingError`, `store.EntryUnreadableError`,
// `ErrFocusSelectorUnported`, and the validation errors below. A CLI consumes `Recall` /
// `Search` for the report, `RecallReport.RenderText` / `SearchReport.RenderText` for the
// bytes (with its own `extraHeader` lines), and `ExitFor` for the exit code and the one
// warning sentence. `Reader` is the thin adapter the pod hands to `internal/api`, and the
// only type in the package that knows a server exists.
//
// ⚠ IT WAS THE P1a/P1b SEAM, AND THE SEAM IS SPENT. P1a shipped the routes with their
// query parameters parsed and VALIDATED, the scope narrowed by the caller's allowlist and
// their refusals byte-identical to every other route's, plus an `Unimplemented` renderer
// answering 501. P1b implemented the renderer and deleted that type along with the
// handler branch that read its error. What P1b also had to add, which the old wording
// said would not move: `X-Store-Revision`, absent from the Go report response because no
// report response existed to carry it.
//
// 🔴 THE VALIDATION LIVES HERE RATHER THAN IN THE HANDLER BECAUSE IT IS THE
// REPORT'S OWN CONTRACT, NOT THE TRANSPORT'S. `limit must be an int >= 1` is a rule
// about a report; the handler's job is to turn a refusal into a 400. Spelling it in
// the handler would mean the renderer either trusts an unvalidated option or validates it
// a second time, and a predicate at two sites is wrong at one of them. ⚠ `Recall` and
// `Search` do NOT re-run it, for the same reason.
package report

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ZacxDev/cairn/internal/pytext"
	"github.com/ZacxDev/cairn/internal/store"
)

// The recall modes, and the default. A caller naming anything else is a caller
// error: a query parameter NEVER silently defaults, because a typo that quietly
// became the default is a caller believing a setting took effect.
var RecallModes = []string{"digest", "list", "full"}

const (
	DefaultMode       = "digest"
	DefaultEntryLimit = 12
	// ContextBullet is the sentinel `?context=` value meaning "the whole bullet".
	// It is the FLOOR of the parameter, which is why the refusal reads `>= 0`
	// while the constant is negative: 0 is the smallest LINE count, and this is
	// the one value below it that means something else.
	ContextBullet    = -1
	DefaultThreshold = 0.60
	DefaultMaxHits   = 10
)

// RecallOptions is one `/recall` request, after parsing and before rendering.
type RecallOptions struct {
	Scope string
	// Ref is the `?ref=` narrowing, and Ref == "" means "no ref was sent" — which
	// is a different request from `?ref=` with an empty value, since the latter
	// still narrows and finds nothing. HasRef separates them.
	Ref    string
	HasRef bool
	Limit  int
	Mode   string
	Page   int

	// FocusPaths is the repo-relative path window the FEATURED-ENTRY selector resolves
	// against, and FocusSource is the doc it was read out of — quoted back in the printed
	// basis and nowhere else.
	//
	// 🔴 THE STORE API NEVER SETS EITHER, AND THE CLI ALWAYS MAY. A pod has no repo to
	// read a handoff doc out of; `cairn recall` with no `--scope` and the default mode
	// reads its repo's newest one. P1b REFUSED a non-empty window (see the note where
	// `ErrFocusSelectorUnported` used to be); P2 ports the matcher instead, because the
	// CLI cannot refuse and a silent fallback would print a basis claiming a resolved
	// pick.
	//
	// ⚠ `FocusSource == ""` WITH A NON-EMPTY WINDOW IS UNREACHABLE FROM EITHER CALLER
	// and renders differently from the oracle's `None` in the fallback sentence. Stated
	// rather than defended against: the window and its source are built together by
	// `focus.Window`, which sets both or neither.
	FocusPaths  []string
	FocusSource string
}

// SearchOptions is one `/search` request, after parsing and before rendering.
type SearchOptions struct {
	Scope     string
	Query     string
	Context   int
	Threshold float64
	MaxHits   int
	AllScopes bool
}

// Rendered is what a route needs to answer a report request: the four-state status, the
// CLI exit code derived from it, the scope the answer is about, the body, and the one
// warning line that accompanies a non-zero exit.
//
// It is returned as one value rather than as out-parameters so a renderer cannot add a
// field the handler forgets to read — the handler destructures exactly this and nothing
// else. ⚠ `Warning` IS THE FIELD P1b ADDED, and the paragraph above is why it is a field
// rather than a `stderr` write inside the renderer: see ExitFor.
type Rendered struct {
	Status  string
	Scope   string
	Exit    int
	Text    string
	Warning string
}

// Renderer is the seam. Reader implements it.
//
// ⚠ P1a SHIPPED AN `Unimplemented` RENDERER HERE, ANSWERING A DISTINCT `501` SO AN
// OPERATOR RUNNING BOTH SERVERS COULD TELL "this build does not do reports yet" FROM
// "this build broke". Both it and the handler branch that read its error are GONE, deleted
// with the renderer rather than left behind: a 501 arm no code path can reach is a branch
// that reads as a live fallback, and the honest answer for a report route in this build is
// now the report.
type Renderer interface {
	Recall(storeRoot string, opts RecallOptions, visible store.ScopeSet) (Rendered, error)
	Search(storeRoot string, opts SearchOptions, visible store.ScopeSet) (Rendered, error)
}

// ValidateRecall is the guard ladder a recall's options must pass, IN THIS ORDER,
// each reachable by an input no earlier guard rejects: `limit`, then `page` (a valid
// limit still reaches it), then `mode` (a valid limit AND page still reach it).
//
// 🔴 THE ORDER IS THE CONTRACT, NOT AN IMPLEMENTATION DETAIL. A request carrying two
// bad parameters gets ONE message, and which one it gets is recorded in the goldens.
// Reordering these is a behaviour change that no single-parameter test can see.
func ValidateRecall(opts RecallOptions) error {
	if opts.Limit < 1 {
		return fmt.Errorf("limit must be an int >= 1, got %d", opts.Limit)
	}
	if opts.Page < 1 {
		return fmt.Errorf("page must be an int >= 1, got %d", opts.Page)
	}
	if !contains(RecallModes, opts.Mode) {
		// 🔴 THE VOCABULARY IS QUOTED AS A PYTHON TUPLE, and that is not an
		// accident of transcription: the oracle interpolates its own constant and
		// the golden records the result, so `('digest', 'list', 'full')` — quotes,
		// commas, spaces and parentheses — is the contract for this message.
		return fmt.Errorf("mode must be one of %s, got %s", pyTuple(RecallModes), pyStr(opts.Mode))
	}
	return nil
}

// ValidateSearch is the same ladder for a search: the query, then the threshold
// range, then `max_hits`, then `context`.
func ValidateSearch(opts SearchOptions) error {
	if pytext.StripWhitespace(opts.Query) == "" {
		return errors.New("query must be a non-empty string")
	}
	if opts.Threshold < 0.0 || opts.Threshold > 1.0 {
		return fmt.Errorf("threshold must be a number in [0, 1], got %s", pyFloat(opts.Threshold))
	}
	if opts.MaxHits < 1 {
		return fmt.Errorf("max-hits must be an int >= 1, got %d", opts.MaxHits)
	}
	if opts.Context < ContextBullet {
		return fmt.Errorf("context must be an int >= 0, got %d", opts.Context)
	}
	return nil
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

// pyTuple and pyStr render a Go value the way CPython's `repr` would, because these
// strings reach the wire. A one-element tuple would need a trailing comma; the only
// tuple here has three elements, and the helper refuses to pretend otherwise by
// handling a case it is never given.
func pyTuple(items []string) string {
	quoted := make([]string, 0, len(items))
	for _, item := range items {
		quoted = append(quoted, pyStr(item))
	}
	if len(items) == 1 {
		return "(" + quoted[0] + ",)"
	}
	return "(" + strings.Join(quoted, ", ") + ")"
}

func pyStr(s string) string { return store.PyRepr(s) }

// pyFloat renders a float the way CPython's `repr` does, for the one refusal that
// echoes a threshold back.
func pyFloat(f float64) string {
	s := fmt.Sprintf("%v", f)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}
