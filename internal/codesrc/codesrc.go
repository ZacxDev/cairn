// Package codesrc is a scope's CODE SOURCES: which repositories, and which branch of each, a
// scope's entries describe — stored in an append-only journal OUTSIDE the store tree.
//
// 🔴 ONE PACKAGE IS THE GRAMMAR'S ONE PARSER, THE JOURNAL'S ONE READER AND ITS ONE WRITER — the
// `internal/arcs` shape (`claudedocs/plan-cairn-scope-refs.md`, decisions 1 and 2). Every caller
// goes through `Parse`/`ParseList`: the browser's POST, the writer (which re-validates), the pod's
// GET (whose fold re-validates every record it applies) and the auditor. A second parser is a
// second grammar, and the two would disagree about exactly the strings an attacker chooses.
//
// 🔴 A LIBRARY, NOT A HANDLER. Plain values in, plain values out, no `net/http` type, every error
// classifiable with `errors.As`: `*ParseError` is the caller's fault; `*JournalUnreadableError`
// is "could not look" (the pod answers it as `store-unreachable`, 503); `*StaleRevisionError` is
// a lost race; `*arcs.InsideStoreError` is a refusal to start. It is stdlib-only, so it may sit in
// `depspolicy.LinkedBinaryRoots`' closure once the pod imports it.
//
// 🔴 THE KEY IS THE NORMALISED SCOPE *NAME*, NEVER A SCOPE ID — the operator's answer to the
// plan's Q17. The browser surface and the pod resolve scope IDs from DIFFERENT authorities (a
// control journal mints random IDs; the token-file projection derives them), so a record keyed by
// one side's ID could never be found by the other. The store DIRECTORY name is the one identifier
// both share. Consequences, stated rather than discovered: a DIRECTORY rename orphans the record
// (re-declare it), and a delete followed by a recreate under the same name re-attaches it, shown
// with its original `set_by`/`set_at`.
package codesrc

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ZacxDev/cairn/internal/store"
)

// MaxSources is the cap on one scope's declaration (the plan's Q14). Counted AFTER duplicates
// are removed, so a pasted list with one line twice is not refused for its length.
const MaxSources = 8

// Key is the ONE function from a scope to its journal key: `store.NormalizeRef` of the store
// DIRECTORY name. It needs no control model, which is the point — two binaries over two
// authorities cannot make it disagree.
func Key(scopeName string) string {
	return store.NormalizeRef(scopeName)
}

// Source is one parsed `git:<host>/<repo-path>[//<subpath>]@<branch>`.
type Source struct {
	Host     string // lowercased DNS name
	RepoPath string // ≥ 2 segments, case PRESERVED, trailing `.git` stripped
	Subpath  string // "" when absent
	Branch   string
}

// Canonical is the normalised string — the only spelling the journal stores and the pod serves.
func (s Source) Canonical() string {
	var b strings.Builder
	b.WriteString("git:")
	b.WriteString(s.Host)
	b.WriteByte('/')
	b.WriteString(s.RepoPath)
	if s.Subpath != "" {
		b.WriteString("//")
		b.WriteString(s.Subpath)
	}
	b.WriteByte('@')
	b.WriteString(s.Branch)
	return b.String()
}

// The refusal rules. Each `*ParseError` carries exactly one, and a test asserts each fixture's
// OWN rule — a refusal for the wrong reason is how a dropped check hides behind a neighbour.
//
// ⚠ `#` IS NOT REFUSED (a valid branch may carry one: `git check-ref-format --branch issue#12`
// succeeds). What stops a pasted `… # a comment` from silently JOINING the source is the
// whitespace refusal: the comment can only be attached by a space, and a space is refused.
const (
	RuleUTF8        = "is not valid UTF-8"
	RuleControl     = "contains a control character"
	RuleFormat      = "contains an invisible formatting character (bidi override, zero-width or BOM)"
	RuleWhitespace  = "contains whitespace (a source is one token; put one per line)"
	RuleComma       = "contains a comma"
	RulePrefix      = "must start with 'git:'"
	RuleScheme      = "carries a URL scheme ('://'); write git:<host>/<repo-path>@<branch>"
	RuleNoBranch    = "has no '@<branch>' (the branch is required)"
	RuleUserinfo    = "carries userinfo ('user@') before the host"
	RulePort        = "carries a port; the host is a bare DNS name"
	RuleHost        = "has a host that is not a DNS name (dot-separated labels of [a-z0-9-], none leading or trailing '-')"
	RuleRepoPath    = "has a repo path of fewer than 2 segments"
	RuleSegment     = "has a path segment outside [A-Za-z0-9._-], empty, or starting with '-' or '.'"
	RuleDotDot      = "has a '..' segment in its subpath"
	RuleEmptySub    = "has an empty subpath after '//'"
	RuleBranchDash  = "has a branch starting with '-'"
	RuleBranchAt    = "has an '@' in its branch"
	RuleBranchRef   = "has a branch that is not a valid `git check-ref-format --branch` name"
	RuleTooMany     = "declares more than 8 sources"
	RuleEmptySource = "is empty"
)

// ParseError is a refused source (or list). `Index` is the 1-based position in the list given to
// `ParseList`, 0 for a single `Parse` — the browser form reports it as a line number.
type ParseError struct {
	Index int
	Input string
	Rule  string
}

func (e *ParseError) Error() string {
	if e.Index > 0 {
		return fmt.Sprintf("source %d (%q) %s", e.Index, e.Input, e.Rule)
	}
	return fmt.Sprintf("source %q %s", e.Input, e.Rule)
}

// Parse validates ONE source and returns it parsed. It never trims: a caller splitting a form
// strips its own line endings, and anything left that is whitespace is a refusal, not a repair.
//
// 🔴 EVERY STRING IT ACCEPTS SURVIVES A JSON ROUND TRIP BYTE-FOR-BYTE. Invalid UTF-8 is refused
// first because `encoding/json` rewrites a bad byte to U+FFFD on the way out: the line written
// would carry a different source than the one its revision digests, and the fold would SKIP the
// writer's own record (`TestEveryAcceptedSourceRoundTripsThroughTheJournal` pins the property).
func Parse(raw string) (Source, error) {
	refuse := func(rule string) (Source, error) {
		return Source{}, &ParseError{Input: raw, Rule: rule}
	}
	if raw == "" {
		return refuse(RuleEmptySource)
	}
	if !utf8.ValidString(raw) {
		return refuse(RuleUTF8)
	}
	// Character classes FIRST, each with its own rule, before any structure is read: a control
	// character inside the host would otherwise be reported as "not a DNS name", which is true
	// and names the wrong problem. Controls are C0, DEL and C1 (U+0080–U+009F, CSI included) —
	// the whitespace ones (tab, newline, NEL) are left to the whitespace rule. Format characters
	// (category Cf: bidi overrides and isolates, zero-width joiners/spaces, the BOM) are refused
	// because a source is a DISPLAYED string and these make two different sources render alike.
	for _, r := range raw {
		if unicode.IsControl(r) && !unicode.IsSpace(r) {
			return refuse(RuleControl)
		}
	}
	for _, r := range raw {
		if unicode.Is(unicode.Cf, r) {
			return refuse(RuleFormat)
		}
	}
	for _, r := range raw {
		if unicode.IsSpace(r) {
			return refuse(RuleWhitespace)
		}
	}
	if strings.ContainsRune(raw, ',') {
		return refuse(RuleComma)
	}
	rest, ok := strings.CutPrefix(raw, "git:")
	if !ok {
		return refuse(RulePrefix)
	}
	if strings.Contains(rest, "://") {
		return refuse(RuleScheme)
	}
	// The host ends at the FIRST '/'. An '@' before it is userinfo; the branch starts after the
	// FIRST '@' AFTER it — so a second '@' lands IN the branch and gets its own rule.
	slash := strings.IndexByte(rest, '/')
	if slash < 0 {
		if strings.ContainsRune(rest, '@') {
			return refuse(RuleRepoPath)
		}
		return refuse(RuleNoBranch)
	}
	host, after := rest[:slash], rest[slash+1:]
	if strings.ContainsRune(host, '@') {
		return refuse(RuleUserinfo)
	}
	path, branch, found := strings.Cut(after, "@")
	if !found {
		return refuse(RuleNoBranch)
	}
	if strings.ContainsRune(host, ':') {
		return refuse(RulePort)
	}
	host = strings.ToLower(host)
	if !isDNSName(host) {
		return refuse(RuleHost)
	}
	repoPath, subpath, hasSub := strings.Cut(path, "//")
	if repoPath != "" && !strings.Contains(repoPath, "/") {
		// The ONE-segment shape gets its own rule before the segment rule reads it.
		return refuse(RuleRepoPath)
	}
	segs := strings.Split(repoPath, "/")
	if repoPath == "" || len(segs) < 2 {
		return refuse(RuleRepoPath)
	}
	// A trailing `.git` is a spelling of the same repository, so it is stripped before the
	// segment rule reads it; what remains must still be a segment.
	segs[len(segs)-1] = strings.TrimSuffix(segs[len(segs)-1], ".git")
	for _, s := range segs {
		if !isSegment(s) {
			return refuse(RuleSegment)
		}
	}
	src := Source{Host: host, RepoPath: strings.Join(segs, "/"), Branch: branch}
	if hasSub {
		if subpath == "" {
			return refuse(RuleEmptySub)
		}
		for _, s := range strings.Split(subpath, "/") {
			if s == ".." {
				return refuse(RuleDotDot)
			}
		}
		for _, s := range strings.Split(subpath, "/") {
			if !isSegment(s) {
				return refuse(RuleSegment)
			}
		}
		src.Subpath = subpath
	}
	if strings.HasPrefix(branch, "-") {
		return refuse(RuleBranchDash)
	}
	if strings.ContainsRune(branch, '@') {
		return refuse(RuleBranchAt)
	}
	if !isBranchName(branch) {
		return refuse(RuleBranchRef)
	}
	return src, nil
}

// ParseList validates a declaration: every source parsed, duplicates (by canonical form) DROPPED
// with the first occurrence's position KEPT — the first source is the primary — and the result
// capped at `MaxSources`. An empty list is valid: it is an explicit "undeclared".
//
// Every `Index` it reports is the source's position in `raws` — the form's own line — never its
// position in the deduped list, so the line named is the line the user sees.
func ParseList(raws []string) ([]Source, error) {
	out := make([]Source, 0, len(raws))
	positions := make([]int, 0, len(raws))
	seen := make(map[string]bool, len(raws))
	for i, raw := range raws {
		src, err := Parse(raw)
		if err != nil {
			pe := err.(*ParseError)
			pe.Index = i + 1
			return nil, pe
		}
		c := src.Canonical()
		if seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, src)
		positions = append(positions, i+1)
	}
	if len(out) > MaxSources {
		return nil, &ParseError{Index: positions[MaxSources], Input: raws[positions[MaxSources]-1], Rule: RuleTooMany}
	}
	return out, nil
}

// Canonicals is the canonical spelling of each source, in order.
func Canonicals(srcs []Source) []string {
	out := make([]string, len(srcs))
	for i, s := range srcs {
		out[i] = s.Canonical()
	}
	return out
}

// isDNSName is a lowercased host: ≥ 2 dot-separated labels (a bare word is far more often a
// forgotten host — `git:example-org/example-repo@main` — than a real single-label name), each
// 1–63 characters of [a-z0-9-] with no leading or trailing '-', 253 at most in all.
func isDNSName(h string) bool {
	if h == "" || len(h) > 253 {
		return false
	}
	labels := strings.Split(h, ".")
	if len(labels) < 2 {
		return false
	}
	for _, l := range labels {
		if l == "" || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return false
		}
		for i := 0; i < len(l); i++ {
			c := l[i]
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

// isSegment is one path segment: non-empty [A-Za-z0-9._-]+, not starting with '-' or '.'.
func isSegment(s string) bool {
	if s == "" || s[0] == '-' || s[0] == '.' {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

// isBranchName is `git check-ref-format --branch`'s rule set, written out (git-check-ref-format(1)),
// and checked against the real `git` by `TestTheBranchRuleAgreesWithGit` wherever git exists.
// The leading '-', '@' and whitespace refusals are checked before this, each with its own rule
// (all three reachable: `TestEachRefusalIsForItsOwnRule` has a fixture for each).
func isBranchName(b string) bool {
	if b == "" || b == "HEAD" || b == "@" {
		return false
	}
	if strings.HasPrefix(b, "/") || strings.HasSuffix(b, "/") || strings.HasSuffix(b, ".") ||
		strings.Contains(b, "//") || strings.Contains(b, "..") || strings.Contains(b, "@{") {
		return false
	}
	for i := 0; i < len(b); i++ {
		c := b[i]
		if c < 0x20 || c == 0x7f || strings.IndexByte(" ~^:?*[\\", c) >= 0 {
			return false
		}
	}
	for _, comp := range strings.Split(b, "/") {
		if strings.HasPrefix(comp, ".") || strings.HasSuffix(comp, ".lock") {
			return false
		}
	}
	return true
}
