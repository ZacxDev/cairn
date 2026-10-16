package arcs

import (
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/ZacxDev/cairn/internal/memo"
	"github.com/ZacxDev/cairn/internal/redact"
	"github.com/ZacxDev/cairn/internal/store"
)

// 🔴 SCHEMA 2: AN OPTIONAL, BOUNDED SUMMARY OF WHERE THE ARC STANDS, AND AN OPT-IN TO SHOW THE ARC
// TO READERS OF ITS DECLARED SCOPES.
//
// Schema 1 carried metadata only (status, members, counts), so "how far along is this effort, and
// what is next" could be answered only by reading the handoff document in its repository — outside
// every grant this store enforces. Schema 2 lets the registering tool push that answer HERE, where a
// reader's scope grant decides who sees it.
//
// 🔴 THIS DELIBERATELY NARROWS THE SCHEMA-1 "NEVER STORED" RULE, AND ONLY HERE. Schema 1 stored no
// free text and no commit SHA by construction (`DecodePayload`'s comment). A summary is free text the
// tooling chose to publish, plus the ONE commit it was read at; nothing else free-form reaches the
// journal, and an unknown field is still refused. Every string is bounded, refused on a control
// character, and refused on a confident secret hit (`memo`'s subset of `internal/redact`) — never
// truncated and never redacted in place, so what is stored is exactly what the tool sent or nothing.
//
// 🔴 A RECORD IS SCHEMA 2 IFF IT CARRIES A SUMMARY OR THE OPT-IN. A schema-2 payload carrying
// neither is stored as schema 1, byte-identical to what a schema-1 payload produces — so a pod rolled
// BACK to a build that knows only schema 1 still reads every record that uses nothing new. A record
// that does use them is SKIPPED by such a build (its fold refuses an unknown key and any schema but
// 1) and counted as damage; the arc then shows its last schema-1 state, or nothing.

// SchemaSummary is the schema a record carrying a summary or the opt-in is written under.
const SchemaSummary = 2

// The summary's bounds. 🔴 OVER-CAP IS REFUSED, NEVER TRUNCATED: a truncated "next step" is a
// different instruction, and a reader cannot tell it was cut. The per-field caps are generous
// against a handoff's real sections; MaxSummaryBytes bounds the whole independently (20 items at the
// item cap overshoot it), because every byte is paid on every later read — nothing is compacted.
const (
	MaxClosingConditionBytes = 1000
	MaxSummaryItems          = 20
	MaxSummaryItemBytes      = 500
	MaxRepoPathBytes         = 256
	MaxTaskRefs              = 32
	MaxTaskRefBytes          = 200
	MaxAliases               = 16
	MaxAliasBytes            = 64
	MaxSummaryBytes          = 16 * 1024
)

// Source is where the tool read the summary: a repository-relative path and the commit it was read
// at. It is what lets a reader cite the summary with a freshness date.
type Source struct {
	RepoPath    string `json:"repo_path"`
	Commit      string `json:"commit"`
	CommittedAt string `json:"committed_at"`
}

// Summary is the tool's own account of the arc. Every field is the TOOL'S word, exactly like the
// status (the package doc's "what a registration proves").
type Summary struct {
	ClosingCondition string   `json:"closing_condition,omitempty"`
	StateNow         []string `json:"state_now"`
	// NextSteps is RANKED: order is meaning, so it is never sorted or deduplicated.
	NextSteps []string `json:"next_steps"`
	Source    Source   `json:"source"`
	TaskRefs  []string `json:"task_refs"`
	// Aliases are other names a reader may ask for the arc by, folded like a slug, sorted, deduped.
	Aliases []string `json:"aliases"`
}

var (
	commitHex = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
	repoPath  = regexp.MustCompile(`^[A-Za-z0-9._-]+(?:/[A-Za-z0-9._-]+)*$`)
)

// scanner is ONE secret scanner per process, built lazily: `memo.NewScanner`'s fresh in-memory key,
// read only for `Hit.Rule`, so its key is never observable.
var scanner = sync.OnceValues(memo.NewScanner)

// secretRule is the first CONFIDENT rule `s` hits, or "". The confident subset is memo's, spelled
// once there: the rules that match a credential by its own shape. The broad rules (`key-context`,
// `entropy`) are not refusals anywhere in this tree, for memo's measured reason — they damage
// honest prose at a rate a refusal would turn into rejected handoffs.
func secretRule(s string) (string, error) {
	scan, err := scanner()
	if err != nil {
		return "", err
	}
	return firstConfident(scan, s), nil
}

func firstConfident(scan *redact.Redactor, s string) string {
	_, hits := scan.String(s)
	confident := memo.ConfidentSecretRules()
	for _, h := range hits {
		if slices.Contains(confident, h.Rule) {
			return h.Rule
		}
	}
	return ""
}

// checkText is the rule every summary string meets: non-blank, valid UTF-8, within `max` bytes, no
// control character and no Unicode line or paragraph separator — because the rendered answer is
// LINES, and a newline inside a "state now" bullet would let a registration print a line that reads
// as the pod's own (`status: closed`) — and no confident secret. The refusal names the FIELD, never
// the value.
func checkText(field, s string, max int) error {
	if strings.TrimSpace(s) == "" {
		return payloadErr("%s must not be blank", field)
	}
	if !utf8.ValidString(s) {
		return payloadErr("%s must be valid UTF-8", field)
	}
	if len(s) > max {
		return payloadErr("%s is %d bytes, at most %d (refused, not truncated)", field, len(s), max)
	}
	for _, r := range s {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return payloadErr("%s must be one line with no control characters", field)
		}
	}
	rule, err := secretRule(s)
	if err != nil {
		return err
	}
	if rule != "" {
		return payloadErr("%s looks like it carries a credential (rule %s); nothing was registered — remove it at the source", field, rule)
	}
	return nil
}

func checkList(field string, items []string, maxItems, maxBytes int) error {
	if len(items) > maxItems {
		return payloadErr("at most %d %s, got %d (refused, not truncated)", maxItems, field, len(items))
	}
	for i, s := range items {
		if err := checkText(field+"["+strconv.Itoa(i)+"]", s, maxBytes); err != nil {
			return err
		}
	}
	return nil
}

// decodeSummary validates one payload summary into the record's. nil in, nil out: a summary is
// optional.
func decodeSummary(raw *Summary) (*Summary, error) {
	if raw == nil {
		return nil, nil
	}
	s := *raw
	if s.ClosingCondition != "" {
		if err := checkText("summary.closing_condition", s.ClosingCondition, MaxClosingConditionBytes); err != nil {
			return nil, err
		}
	}
	if err := checkList("summary.state_now", s.StateNow, MaxSummaryItems, MaxSummaryItemBytes); err != nil {
		return nil, err
	}
	if err := checkList("summary.next_steps", s.NextSteps, MaxSummaryItems, MaxSummaryItemBytes); err != nil {
		return nil, err
	}
	if err := checkList("summary.task_refs", s.TaskRefs, MaxTaskRefs, MaxTaskRefBytes); err != nil {
		return nil, err
	}
	for i, ref := range s.TaskRefs {
		if strings.ContainsFunc(ref, unicode.IsSpace) {
			return nil, payloadErr("summary.task_refs[%d] must contain no whitespace", i)
		}
	}
	if err := checkList("summary.aliases", s.Aliases, MaxAliases, MaxAliasBytes); err != nil {
		return nil, err
	}
	aliases := []string{}
	for i, a := range s.Aliases {
		folded := store.NormalizeRef(a)
		if folded == "" {
			return nil, payloadErr("summary.aliases[%d] normalizes to nothing", i)
		}
		if !slices.Contains(aliases, folded) {
			aliases = append(aliases, folded)
		}
	}
	slices.Sort(aliases)
	s.Aliases = aliases
	// The provenance is REQUIRED: a summary nobody can date or cite is the stale-claim shape the
	// summary exists to replace.
	if !repoPath.MatchString(s.Source.RepoPath) || len(s.Source.RepoPath) > MaxRepoPathBytes ||
		slices.Contains(strings.Split(s.Source.RepoPath, "/"), "..") || slices.Contains(strings.Split(s.Source.RepoPath, "/"), ".") {
		return nil, payloadErr("summary.source.repo_path must be a repository-relative path of at most %d bytes (no leading /, no . or .. segment)", MaxRepoPathBytes)
	}
	if !commitHex.MatchString(s.Source.Commit) {
		return nil, payloadErr("summary.source.commit must be a full lower-case commit id (40 or 64 hex)")
	}
	if !validTime(s.Source.CommittedAt) {
		return nil, payloadErr("summary.source.committed_at must be RFC 3339")
	}
	if s.StateNow == nil {
		s.StateNow = []string{}
	}
	if s.NextSteps == nil {
		s.NextSteps = []string{}
	}
	if s.TaskRefs == nil {
		s.TaskRefs = []string{}
	}
	if n := summaryBytes(s); n > MaxSummaryBytes {
		return nil, payloadErr("summary is %d bytes as stored, at most %d (refused, not truncated)", n, MaxSummaryBytes)
	}
	return &s, nil
}

func summaryBytes(s Summary) int {
	b, err := json.Marshal(s)
	if err != nil {
		return MaxSummaryBytes + 1
	}
	return len(b)
}

// validSummary is the READ-side check on a stored summary: the payload rules minus the secret scan
// (a stored record was scanned on its way in, and re-scanning every record on every read would make
// a later, stricter rule table silently hide arcs).
func validSummary(s *Summary) bool {
	if s == nil {
		return true
	}
	if s.StateNow == nil || s.NextSteps == nil || s.TaskRefs == nil || s.Aliases == nil {
		return false
	}
	if len(s.StateNow) > MaxSummaryItems || len(s.NextSteps) > MaxSummaryItems ||
		len(s.TaskRefs) > MaxTaskRefs || len(s.Aliases) > MaxAliases {
		return false
	}
	return commitHex.MatchString(s.Source.Commit) && validTime(s.Source.CommittedAt) &&
		repoPath.MatchString(s.Source.RepoPath) && summaryBytes(*s) <= MaxSummaryBytes
}

// VisibleTo is THE arc visibility predicate (operator decision Q1, widened by schema 2's opt-in):
// an arc exists for a caller iff its HOME scope is readable — or the arc OPTED IN
// (`visible_to_declared`) and at least one of its DECLARED scopes is readable.
//
// 🔴 ONE PREDICATE, EVERY READER. `report.Arcs`, `report.Arc`, `report.ArcsAcross`,
// `report.ArcsCheck` and `report.SessionsAcross` all call this and nothing else, so a listing and a
// show cannot disagree about whether an arc exists — the bug class a per-site `Allows(reg.Home)`
// would regrow the day one site learned the opt-in and another did not.
//
// 🔴 WHY THE OPT-IN IS SAFE TO HONOUR: `PUT arc/…` already requires the WRITE verb on the home AND
// on every declared scope, so an arc can be shown to a scope's readers only by somebody who may
// write that scope. ⚠ The authority is checked at REGISTRATION: a registrant whose write on a
// declared scope is later revoked leaves the arc visible there until the next push re-declares it.
func (r Registration) VisibleTo(visible store.ScopeSet) bool {
	if visible.Allows(r.Home) {
		return true
	}
	if !r.VisibleToDeclared {
		return false
	}
	for _, d := range r.DeclaredScopes {
		if visible.Allows(d) {
			return true
		}
	}
	return false
}

// usesSchemaTwo is "this record needs schema 2 to be written down".
func (r Registration) usesSchemaTwo() bool {
	return r.Summary != nil || r.VisibleToDeclared
}
