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

	// RefTo is the REVERSE LOOKUP: "which entries reference this `<system>:<id>`". HasRefTo
	// separates "no filter was sent" from `?ref-to=` with an empty value, exactly as
	// HasRef does — the latter still narrows, and to nothing.
	//
	// 🔴 IT IS A DIFFERENT QUESTION FROM `Ref`, AND THE TWO COMPOSE RATHER THAN CONFLICT.
	// `Ref` names an ENTRY (a filename stem or an alias); `RefTo` names something an entry
	// POINTS AT. Both present means "narrow to the entries referencing X, then surface
	// entry Y among them", which is a sensible request and needs no special case.
	//
	// ⚠ THE QUERY PARAMETER IS `?ref-to=`, NOT `?ref=`, AND THAT IS FORCED RATHER THAN
	// CHOSEN. `?ref=` ALREADY MEANS `Ref` on `/api/v1/recall/{scope}` and the conformance
	// corpus pins its behaviour, so spelling the reverse lookup `?ref=` would silently
	// redefine a live parameter. The flag and the parameter therefore share one spelling,
	// `ref-to`, which is also what stops a reader mapping the wrong one to the other.
	RefTo    string
	HasRefTo bool

	// Tag/HasTag is the `?tag=`/`--tag` CATEGORY narrowing: keep only entries whose `tags:`
	// carry it. The operand is carried AS WRITTEN; `canonicalTag` folds it once, inside
	// `Recall`/`Search`.
	//
	// 🔴 IT CARRIES A `Has…` FOR THE REASON `Ref` AND `RefTo` DO, AND THAT PAIR IS WHAT MAKES
	// AN EMPTY `?tag=` REFUSABLE. `?tag=` with an empty value is a present operand that names
	// no category, and it must be answered 400 rather than read as "no filter was sent" — the
	// widening direction. One string cannot hold both states, so the flag is not decoration: a
	// `Tag string` alone would make `?tag=` and no `?tag=` at all the same request. ⚠ The
	// REPORT types carry a bare `Tag` instead, because by then the operand has been validated
	// and a non-empty folded tag IS "a filter ran" — see `RecallReport.Tag`.
	Tag    string
	HasTag bool

	Limit int
	Mode  string
	Page  int

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

	// RefTo is the same reverse-lookup narrowing `RecallOptions.RefTo` is, applied to the
	// entry set each searched scope contributes. See that field for why the parameter is
	// spelled `ref-to`.
	RefTo    string
	HasRefTo bool

	// Tag/HasTag is the same category narrowing `RecallOptions.Tag` is, applied to the entry
	// set each searched scope contributes. See that field for why the pair is not decoration.
	Tag    string
	HasTag bool
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
	// 🔴 LAST IN THE LADDER, DELIBERATELY, BECAUSE THE ORDER ABOVE IS A RECORDED CONTRACT.
	// Putting a new guard anywhere but the end changes which message a request carrying two
	// bad parameters receives, and every golden that pins one of those messages would move
	// for a reason unrelated to this change.
	if err := validateRefTo(opts.RefTo, opts.HasRefTo); err != nil {
		return err
	}
	// AFTER `ref-to`, for the same reason `ref-to` is after `mode`: the ladder's order decides
	// which message a request carrying two bad parameters gets, and that order is recorded in
	// the goldens. A new guard goes at the END.
	if err := validateTag(opts.Tag, opts.HasTag); err != nil {
		return err
	}
	return nil
}

// validateTag refuses a tag OPERAND that folds away, which is the one thing about a tag that
// can be wrong. `!has` is "no filter was sent" and passes.
//
// 🔴 A REFUSAL RATHER THAN A NARROWING TO NOTHING, WHICH IS THE OPPOSITE CHOICE FROM `?ref=`.
// `?ref=` with an empty value narrows and finds nothing, and that is right there: a ref names an
// ENTRY, and "no entry is called that" is a fact about the store worth reporting. A tag operand
// that folds away names NO CATEGORY AT ALL, so answering "no entry carries this" would be an
// empty result whose cause is invisible — indistinguishable from a tag nobody has used yet, when
// the real cause is that the query said nothing. An operator who typed `--tag ''` or `--tag '!!'`
// gets told so.
//
// ⚠ AND IT IS WHY THE REPORT TYPES CARRY A BARE `Tag`: with an empty operand refused here, no
// report can ever hold a present-but-empty tag, so `Tag != ""` is the whole "was a filter sent"
// question DOWNSTREAM of this guard. The OPTIONS still need `HasTag`, because this guard is what
// reads it — see that field.
//
// ⚠ THE FOLD IS `store.NormalizeRef`, THE SAME FUNCTION `parseTagsField` applies to a DECLARED
// tag, so the rule the query side enforces is the rule the FILE side enforces — one function,
// not two spellings of "folds to something". A second rule here is how a tag an operator can
// write becomes one they cannot ask for.
func validateTag(tag string, has bool) error {
	if !has {
		return nil
	}
	if store.NormalizeRef(tag) == "" {
		return fmt.Errorf("tag %s normalizes to the empty string — a tag must fold to at "+
			"least one of `[a-z0-9.-]`", store.PyRepr(tag))
	}
	return nil
}

// canonicalTag is the ONE conversion from a caller's raw `--tag`/`?tag=` operand to the folded
// tag the filter and the rendered header both use.
//
// 🔴 IT LIVES IN THE RENDERER RATHER THAN AT EACH CALL SITE, AND THAT IS THE SAME RULING
// `ValidateRecall` ITSELF CARRIES. Two callers set these options — the pod's route and the Go
// client's verb — and a "validate then normalise" pair spelled at both is one predicate at two
// sites, wrong at the one that gets a new step first. The options therefore carry the operand as
// WRITTEN and `Recall`/`Search` canonicalise once, which is also why `RecallOptions.Tag` is not
// pre-folded: a report must be reproducible from the options a request actually carried.
//
// 🔴 IT RE-VALIDATES AND RETURNS THE ERROR, EXACTLY AS THE `ref-to` FILTER RE-PARSES ITS OPERAND.
// Unreachable from either real caller, because the option ladder refuses a folding-away tag
// first. Returned rather than ignored so a future caller that skips the ladder cannot silently
// widen: a folded-away operand would leave the empty string, and the filter treats an empty tag
// as "no filter was sent" — so swallowing the error here would turn a malformed filter into no
// filter at all.
func canonicalTag(raw string) (string, error) {
	if err := validateTag(raw, true); err != nil {
		return "", err
	}
	return store.NormalizeRef(raw), nil
}

// validateRefTo is the ONE place the reverse-lookup operand's shape is refused, shared by
// both option types.
//
// 🔴 IT DELEGATES TO `store.ParseTaskRef` RATHER THAN RE-SPELLING THE RULES. The operand is
// the SAME `<system>:<id>` grammar an entry's `refs:` item is, and a second parser for it
// would drift: a query the writer accepts but the reader refuses (or worse, the reverse)
// answers "no entries reference this" for a ref that is written in the store. The refusal
// sentence is the parser's own, prefixed so an operator can see it is about their query
// rather than about a file.
func validateRefTo(refTo string, has bool) error {
	if !has {
		return nil
	}
	if _, err := store.ParseTaskRef(refTo); err != nil {
		return fmt.Errorf("ref-to is not a well-formed `<system>:<id>` ref: %s", err.Error())
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
	// Last, for the reason `ValidateRecall`'s own trailing guard gives.
	if err := validateRefTo(opts.RefTo, opts.HasRefTo); err != nil {
		return err
	}
	if err := validateTag(opts.Tag, opts.HasTag); err != nil {
		return err
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
