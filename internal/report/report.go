// Package report is the P1a/P1b SEAM, and it is the whole of it.
//
// 🔴 WHAT P1b ADDS AND WHAT IT MUST NOT TOUCH. `/api/v1/recall/{scope}` and
// `/api/v1/search/{scope}` are the two routes whose 200 body is a RENDERED report —
// the digest, the index page, the malformed block, the sensitivity fold, the
// four-state discrimination. Porting that renderer is P1b. Everything else those two
// routes do is here and is finished: their query parameters are parsed and VALIDATED
// (a typo'd `?limit=abc` is a 400, never a silent default), the scope is narrowed by
// the caller's allowlist, and their refusals are byte-identical to every other
// route's.
//
// So P1b's change is: implement Renderer, hand it to the api package, delete
// Unimplemented. No route moves, no status code moves, no header set moves, and the
// argument validation below does not get a second spelling on the way in.
//
// 🔴 THE VALIDATION LIVES HERE RATHER THAN IN THE HANDLER BECAUSE IT IS THE
// REPORT'S OWN CONTRACT, NOT THE TRANSPORT'S. `limit must be an int >= 1` is a rule
// about a report; the handler's job is to turn a refusal into a 400. Spelling it in
// the handler would mean P1b either trusts an unvalidated option or validates it a
// second time, and a predicate at two sites is wrong at one of them.
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

// Rendered is what a route needs to answer a report request: the four-state status,
// the CLI exit code derived from it, the label the exit decision reads, and the body.
//
// It is returned as one value rather than as four out-parameters so P1b cannot add a
// field the handler forgets to read — the handler destructures exactly this and
// nothing else.
type Rendered struct {
	Status string
	Scope  string
	Exit   int
	Text   string
}

// Renderer is the seam. P1b implements it; P1a ships Unimplemented.
type Renderer interface {
	Recall(storeRoot string, opts RecallOptions, visible store.ScopeSet) (Rendered, error)
	Search(storeRoot string, opts SearchOptions, visible store.ScopeSet) (Rendered, error)
}

// ErrUnimplemented is what P1a's renderer returns. The handler answers 501 and says
// which phase owns it.
//
// 🔴 IT IS A DISTINCT ERROR AND NOT A 500, BECAUSE THE TWO MEAN OPPOSITE THINGS TO
// AN OPERATOR RUNNING BOTH SERVERS SIDE BY SIDE. A 500 says "this server broke"; a
// 501 says "this server does not implement this yet, ask the other one". During a
// dual-run those are the only two hypotheses worth telling apart.
var ErrUnimplemented = errors.New("the report renderer is not implemented in this build")

// Unimplemented is the P1a renderer. It validates nothing and renders nothing: the
// validation has already run in the handler through ValidateRecall/ValidateSearch, so
// a caller error is still a 400 here and only a well-formed request reaches this
// refusal. That ordering is the point — it is what makes the parameter contract
// measurable before the renderer exists.
type Unimplemented struct{}

func (Unimplemented) Recall(string, RecallOptions, store.ScopeSet) (Rendered, error) {
	return Rendered{}, ErrUnimplemented
}

func (Unimplemented) Search(string, SearchOptions, store.ScopeSet) (Rendered, error) {
	return Rendered{}, ErrUnimplemented
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
