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
// journal, and an unknown field is still refused. Every string is bounded and kept to one clean line,
// and a confident secret hit (`memo`'s subset of `internal/redact`) is stored as `[redacted: <rule>]`.
// 🔴 NONE OF THAT REFUSES THE REGISTRATION: an over-cap item is clipped, a control character
// replaced, a secret redacted, an item over a count or the total dropped — and the response to the
// push names every adjustment (`Adjustment`), so the tool always learns what was stored differently.
// The arc's schema-1 data lands whatever its summary held, as it did before schema 2 existed.
//
// 🔴 A RECORD IS SCHEMA 2 IFF IT CARRIES A SUMMARY OR THE OPT-IN. A schema-2 payload carrying
// neither is stored as schema 1, byte-identical to what a schema-1 payload produces — so a pod rolled
// BACK to a build that knows only schema 1 still reads every record that uses nothing new. A record
// that does use them is SKIPPED by such a build (its fold refuses an unknown key and any schema but
// 1) and counted as damage; the arc then shows its last schema-1 state, or nothing.

// SchemaSummary is the schema a record carrying a summary or the opt-in is written under.
const SchemaSummary = 2

// The summary's bounds. 🔴 OVER-CAP IS CLIPPED OR DROPPED, NEVER REFUSED, AND ALWAYS NAMED: an item
// over its byte cap is clipped at a rune boundary and ends in `…`, so a reader can see it was cut and
// the push's response says which. (Refusing was the first design, and measured against real handoff
// documents it dropped the whole registration — status and members — over one long bullet.)
// MaxSummaryBytes bounds the whole independently (20 items at the item cap overshoot it), because
// every byte is paid on every later read — nothing is compacted.
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
	// MaxCommittedAtBytes is the longest RFC 3339 instant with nanoseconds and a numeric offset
	// (`2006-01-02T15:04:05.999999999-07:00`). Go's parser accepts a fractional field of ANY length,
	// so without it `committed_at` alone could carry the summary past MaxSummaryBytes.
	MaxCommittedAtBytes = 35
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
//
// 🔴 ONLY A SUCCESS IS CACHED. A construction error is returned to this call and the next call
// tries again; caching it (`sync.OnceValues`) would make one transient failure refuse every
// summary for the life of the process. `newScanner` is a variable so a test can make it fail.
var (
	scannerMu  sync.Mutex
	scannerOK  *redact.Redactor
	newScanner = memo.NewScanner
)

func scanner() (*redact.Redactor, error) {
	scannerMu.Lock()
	defer scannerMu.Unlock()
	if scannerOK != nil {
		return scannerOK, nil
	}
	scan, err := newScanner()
	if err != nil {
		return nil, err
	}
	scannerOK = scan
	return scan, nil
}

// secretRule is the first CONFIDENT rule `s` hits, or "". The confident subset is memo's, spelled
// once there: the rules that match a credential by its own shape. The broad rules (`key-context`,
// `entropy`) are not acted on anywhere in this tree, for memo's measured reason — they damage
// honest prose at a rate that would redact ordinary handoff text.
//
// 🔴 A HIT IS IGNORED ONLY WHEN A *SEPARATED* PLACEHOLDER EXPLAINS IT. The placeholders are
// `${NAME}`, an upper-case `$NAME` and a run of three or more `*`; one that is a credential by itself
// is a value, not a placeholder. A placeholder is SEPARATED when the characters on both sides of it
// are not word characters (or the text ends there) — as a real stand-in is written:
// `app:${PGPASSWORD}@db`, `Bearer ${TOKEN}`, `user:***@host`. Only a separated placeholder can
// explain a hit. One GLUED to word characters (`app:${X}Xk9…@db`, `Bearer $XQ7fQkj…`, `npm_…$XJ`)
// is ambiguous — which characters are the stand-in and which the value cannot be told from the text
// — so every reading of it is scanned, and any reading that hits redacts:
//   - its sigils deleted and its name kept (`$XQ7fQkj…` → `XQ7fQkj…`);
//   - the whole run deleted (`${X}Xk9…` → `Xk9…`);
//   - the run replaced by a space (`AKIA…${X}Q` → `AKIA… Q`: deleting would join `Q` to the token,
//     past its rule's boundary);
//   - for a `$NAME`, every prefix of it deleted (`$XAIza…` → `AIza…`), because a greedy name can
//     swallow the start of a value.
//
// Separated placeholders are deleted in every reading, and a stray sigil (a `$`, `{`, `}` or `*` that
// is part of no placeholder, as in `Bearer Q7Ip$0ex…`) is deleted too, because the bearer and
// authorization classes cannot match through one. `TestEveryRuleHoldsAgainstPlaceholderNeighbours`
// measures all of this across every confident rule, and `TestEachReadingOfAGluedPlaceholderHasItsOwnRow`
// holds one row only each reading catches.
//
// 🔴 `<…>` IS DELIBERATELY NOT A PLACEHOLDER — retracted. An earlier draft masked every `<…>` before
// scanning and stored the original, which let a markdown autolink holding a real connection string
// (`<postgres://app:pw@db/app>`), or a bracketed token (`<ghp_…>`), through verbatim. And it was
// never needed: no confident rule's character class admits `<` or `>`, so a text like
// `Authorization: Bearer <token>` produces no hit to explain in the first place.
func secretRule(s string) (string, error) {
	scan, err := scanner()
	if err != nil {
		return "", err
	}
	runs := placeholder.FindAllStringIndex(s, -1)
	type run struct {
		start, end int
		separated  bool
	}
	var spans []run
	maxName := 0
	for _, r := range runs {
		m := s[r[0]:r[1]]
		// A placeholder-shaped run that HOLDS a credential can explain nothing: it is read like a
		// glued one, every reading of it scanned.
		sep := !holdsCredential(scan, m) &&
			(r[0] == 0 || !isWordByte(s[r[0]-1])) && (r[1] == len(s) || !isWordByte(s[r[1]]))
		spans = append(spans, run{r[0], r[1], sep})
		if !sep && strings.HasPrefix(m, "$") && !strings.HasPrefix(m, "${") && len(m)-1 > maxName {
			maxName = len(m) - 1
		}
	}
	// reading rebuilds the text with every separated run deleted and every glued run rewritten by
	// `glued`, then deletes the stray sigils.
	reading := func(glued func(m string) string) string {
		var b strings.Builder
		last := 0
		for _, sp := range spans {
			b.WriteString(s[last:sp.start])
			if !sp.separated {
				b.WriteString(glued(s[sp.start:sp.end]))
			}
			last = sp.end
		}
		b.WriteString(s[last:])
		return straySigil.ReplaceAllString(b.String(), "")
	}
	// With no placeholder every reading is the text with its stray sigils deleted. The raw text itself
	// is NOT a separate scan, deliberately: no confident rule's class admits a `$`, `{`, `}` or `*`
	// except url-userinfo's password, which matches the text without them too — the one difference is
	// a "password" made of nothing but sigils (`app:**@db`), which is no credential. As a separate arm
	// it changed no verdict in any test or generated text (its mutant survived), so it is not kept.
	scans := []string{
		reading(func(m string) string { return straySigil.ReplaceAllString(m, "") }),
		reading(func(string) string { return "" }),
		reading(func(string) string { return " " }),
	}
	for k := 1; k < min(maxName, maxPrefixReadings); k++ {
		scans = append(scans, reading(func(m string) string {
			if !strings.HasPrefix(m, "$") || strings.HasPrefix(m, "${") || len(m)-1 <= k {
				return ""
			}
			return m[1+k:]
		}))
	}
	// The rule NAMED is the first hit no placeholder explains.
	for _, text := range scans {
		if r := firstConfident(scan, text); r != "" {
			return r, nil
		}
	}
	return "", nil
}

// holdsCredential is "this placeholder-shaped run carries a credential of its own": the run, its
// name without the sigils, or any suffix of that name hits — so `$XAKIA…` (an `AKIA…` key behind a
// one-letter prefix that defeats the rule's leading boundary) is a value, not a variable.
func holdsCredential(scan *redact.Redactor, m string) bool {
	name := straySigil.ReplaceAllString(m, "")
	if firstConfident(scan, m) != "" {
		return true
	}
	for k := 0; k < min(len(name), maxPrefixReadings); k++ {
		if firstConfident(scan, name[k:]) != "" {
			return true
		}
	}
	return false
}

// maxPrefixReadings bounds the prefix readings of a `$NAME` run (and the suffixes `holdsCredential`
// tries): each is a full scan, and an unbounded loop over a hostile 1 MiB run of capitals would be
// quadratic. Measured: a 500-byte `$` + 498-capital item costs ~70 ms unbounded. ⚠ The limit: a
// credential hidden behind a greedy `$NAME` prefix longer than this is not seen.
const maxPrefixReadings = 64

// scanMargin is how far past an item's cap the secret scan reads (`keepText`).
const scanMargin = 1024

func isWordByte(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
}

// placeholder is every shape the scan treats as a possible stand-in for a value: purely
// word-shaped, so it can carry no `:`, `/`, `@` or `.` — never a URL, a host or a token with a
// separator. Whether it EXPLAINS anything depends on whether it is separated (`secretRule`).
var placeholder = regexp.MustCompile(`\$\{[A-Za-z_][A-Za-z0-9_]*\}|\$[A-Z_][A-Z0-9_]*|\*{3,}`)

// straySigil is a placeholder's punctuation left over once the placeholders are masked: deleted
// before scanning, because no confident rule's class admits it and a value carrying one is otherwise
// invisible.
var straySigil = regexp.MustCompile(`[${}*]`)

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

// Adjustment is one change the pod made to a summary rather than refuse the registration. 🔴 THE
// RESPONSE NAMES EVERY ONE (`Adjustment.Line`), so the tool learns exactly what was stored
// differently from what it sent. `Field` names the field as SENT (`summary.next_steps[3]` is the
// fourth item in the payload, whatever was dropped before it); a whole-summary drop is `summary`.
type Adjustment struct {
	Action string // AdjustClipped, AdjustRedacted, AdjustSanitized or AdjustDropped
	Field  string
	Detail string // `bytes=<sent> max=<cap>`, `rule=<rule>`, `replaced=<n>` or `reason=<reason>`
}

// The four adjustment actions.
const (
	AdjustClipped   = "clipped"
	AdjustRedacted  = "redacted"
	AdjustSanitized = "sanitized"
	AdjustDropped   = "dropped"
)

// AdjustmentLinePrefix opens every adjustment line of a `PUT arc/…` response.
const AdjustmentLinePrefix = "summary-adjusted: "

// Line is the adjustment as the response prints it: `summary-adjusted: <action> <field> <detail>`.
func (a Adjustment) Line() string {
	return AdjustmentLinePrefix + a.Action + " " + a.Field + " " + a.Detail
}

// RedactedText is what a secret-shaped item is stored as: the rule, never the value.
func RedactedText(rule string) string { return "[redacted: " + rule + "]" }

// clipEllipsis ends every clipped item, inside its cap.
const clipEllipsis = "…"

// clip shortens `s` to at most `max` bytes at a rune boundary, ending in `…` (whose 3 bytes count
// within `max`). Already within it, `s` is returned unchanged.
func clip(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max - len(clipEllipsis)
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + clipEllipsis
}

// sanitize makes `s` one line of valid UTF-8: an invalid byte sequence becomes U+FFFD, and every
// control character, U+2028 and U+2029 becomes a space — because the rendered answer is LINES, and a
// newline inside an item would let a registration print a line that reads as the pod's own. It
// returns how many characters it replaced.
func sanitize(s string) (string, int) {
	n := 0
	if !utf8.ValidString(s) {
		fixed := strings.ToValidUTF8(s, "\uFFFD")
		n += strings.Count(fixed, "\uFFFD") - strings.Count(s, "\uFFFD")
		s = fixed
	}
	out := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			n++
			return ' '
		}
		return r
	}, s)
	return out, n
}

// keepText is the rule every summary string meets, as ADJUSTMENTS rather than refusals: sanitized to
// one clean line, dropped when nothing but whitespace is left, stored as `[redacted: <rule>]` on a
// confident secret (scanned in full, before any clipping, so a cut cannot hide one), and clipped to
// `max` bytes. ok is false when the item is dropped.
//
// The `clipped` line's `bytes=` is the length AS SENT; when sanitizing changed the length (an
// invalid UTF-8 byte becomes the 3-byte U+FFFD) the line adds `sanitized_bytes=<n>`, the length that
// was compared with the cap. `redacted` is true when the item was stored as `[redacted: <rule>]`.
func keepText(field, s string, max int, adj *[]Adjustment) (text string, kept, redacted bool, err error) {
	sentBytes := len(s)
	s, replaced := sanitize(s)
	if replaced > 0 {
		*adj = append(*adj, Adjustment{AdjustSanitized, field, "replaced=" + strconv.Itoa(replaced)})
	}
	if strings.TrimSpace(s) == "" {
		*adj = append(*adj, Adjustment{AdjustDropped, field, "reason=blank"})
		return "", false, false, nil
	}
	// The scan reads the part that can be STORED plus a margin past the cut, so a credential that
	// straddles the cut is still seen whole, and the cost is bounded by the cap rather than by what a
	// sender chose to send.
	scanned := s
	if limit := max + scanMargin; len(scanned) > limit {
		for limit > 0 && !utf8.RuneStart(scanned[limit]) {
			limit--
		}
		scanned = scanned[:limit]
	}
	rule, err := secretRule(scanned)
	if err != nil {
		return "", false, false, err
	}
	if rule != "" {
		*adj = append(*adj, Adjustment{AdjustRedacted, field, "rule=" + rule})
		return RedactedText(rule), true, true, nil
	}
	if len(s) > max {
		detail := "bytes=" + strconv.Itoa(sentBytes) + " max=" + strconv.Itoa(max)
		if len(s) != sentBytes {
			detail += " sanitized_bytes=" + strconv.Itoa(len(s))
		}
		*adj = append(*adj, Adjustment{AdjustClipped, field, detail})
		s = clip(s, max)
	}
	return s, true, false, nil
}

// item is one kept list item and the index it was SENT at, so every later drop names the field the
// tool sent rather than a position the pod's own edits moved.
type item struct {
	sent     int
	text     string
	redacted bool
}

func texts(items []item) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.text)
	}
	return out
}

func itemField(field string, sent int) string { return field + "[" + strconv.Itoa(sent) + "]" }

// keepList applies keepText to each item and drops every item past `maxItems` (the LAST ones), in
// order, naming each.
func keepList(field string, items []string, maxItems, maxBytes int, adj *[]Adjustment) ([]item, error) {
	out := []item{}
	for i, s := range items {
		if i >= maxItems {
			*adj = append(*adj, Adjustment{AdjustDropped, itemField(field, i), "reason=over-count max=" + strconv.Itoa(maxItems)})
			continue
		}
		kept, ok, redacted, err := keepText(itemField(field, i), s, maxBytes, adj)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, item{i, kept, redacted})
		}
	}
	return out, nil
}

// decodeSummary turns one payload summary into the record's, and returns every adjustment it made.
// nil in, nil out: a summary is optional.
//
// 🔴 NOTHING IN A SUMMARY REFUSES THE REGISTRATION. The arc's schema-1 data (status, members,
// declared scopes, the opt-in) always lands; a problem in the summary is clipped, sanitized, redacted
// or dropped, and named. What still refuses is a payload that is not the schema — an unknown key or
// a wrong JSON type, at any depth — which the strict decoder rejects before this runs. The three
// whole-summary drops: a `source` that cannot be cited (required: a summary nobody can date is the
// stale-claim shape it exists to replace), one still over MaxSummaryBytes once every list is empty,
// and a secret scanner that could not be built. A summary whose every item was dropped is KEPT, with
// its source, empty — the drops are named.
func decodeSummary(raw *Summary) (*Summary, []Adjustment) {
	if raw == nil {
		return nil, nil
	}
	var adj []Adjustment
	drop := func(reason string) (*Summary, []Adjustment) {
		return nil, append(adj, Adjustment{AdjustDropped, "summary", "reason=" + reason})
	}
	switch {
	case !repoPath.MatchString(raw.Source.RepoPath) || len(raw.Source.RepoPath) > MaxRepoPathBytes ||
		slices.Contains(strings.Split(raw.Source.RepoPath, "/"), "..") || slices.Contains(strings.Split(raw.Source.RepoPath, "/"), "."):
		return drop("source.repo_path")
	case !commitHex.MatchString(raw.Source.Commit):
		return drop("source.commit")
	case len(raw.Source.CommittedAt) > MaxCommittedAtBytes || !validTime(raw.Source.CommittedAt):
		return drop("source.committed_at")
	}
	s := Summary{Source: raw.Source}
	if raw.ClosingCondition != "" {
		kept, ok, _, err := keepText("summary.closing_condition", raw.ClosingCondition, MaxClosingConditionBytes, &adj)
		if err != nil {
			return drop("secret-scan-unavailable")
		}
		if ok {
			s.ClosingCondition = kept
		}
	}
	state, err := keepList("summary.state_now", raw.StateNow, MaxSummaryItems, MaxSummaryItemBytes, &adj)
	if err != nil {
		return drop("secret-scan-unavailable")
	}
	next, err := keepList("summary.next_steps", raw.NextSteps, MaxSummaryItems, MaxSummaryItemBytes, &adj)
	if err != nil {
		return drop("secret-scan-unavailable")
	}
	sentRefs, err := keepList("summary.task_refs", raw.TaskRefs, MaxTaskRefs, MaxTaskRefBytes, &adj)
	if err != nil {
		return drop("secret-scan-unavailable")
	}
	refs := []item{}
	for _, ref := range sentRefs {
		if strings.ContainsFunc(ref.text, unicode.IsSpace) {
			adj = append(adj, Adjustment{AdjustDropped, itemField("summary.task_refs", ref.sent), "reason=whitespace"})
			continue
		}
		refs = append(refs, ref)
	}
	sentAliases, err := keepList("summary.aliases", raw.Aliases, MaxAliases, MaxAliasBytes, &adj)
	if err != nil {
		return drop("secret-scan-unavailable")
	}
	aliases := []item{}
	for _, a := range sentAliases {
		// A redacted alias is DROPPED: `[redacted: <rule>]` would fold to a junk name
		// (`redacted-github-token`) a reader could then look the arc up by.
		if a.redacted {
			adj = append(adj, Adjustment{AdjustDropped, itemField("summary.aliases", a.sent), "reason=redacted"})
			continue
		}
		folded := store.NormalizeRef(a.text)
		if folded == "" {
			adj = append(adj, Adjustment{AdjustDropped, itemField("summary.aliases", a.sent), "reason=normalizes-to-nothing"})
			continue
		}
		if !slices.ContainsFunc(aliases, func(x item) bool { return x.text == folded }) {
			aliases = append(aliases, item{sent: a.sent, text: folded})
		}
	}
	slices.SortFunc(aliases, func(x, y item) int { return strings.Compare(x.text, y.text) })
	build := func() Summary {
		s.StateNow, s.NextSteps, s.TaskRefs, s.Aliases = texts(state), texts(next), texts(refs), texts(aliases)
		return s
	}
	// 🔴 OVER THE TOTAL: DROP FROM THE ENDS OF THE LISTS, LOWEST RANK FIRST. The last item of the
	// longer of `next_steps`/`state_now` (`state_now` on a tie) goes first — a ranked list's tail is
	// its least important end — then `task_refs`, then `aliases`. Each drop names the index it was
	// SENT at. `aliases` is sorted, so its "end" is the alphabetically last.
	type list struct {
		field string
		items *[]item
	}
	for summaryBytes(build()) > MaxSummaryBytes {
		var l list
		switch {
		case len(state) > 0 && len(state) >= len(next):
			l = list{"summary.state_now", &state}
		case len(next) > 0:
			l = list{"summary.next_steps", &next}
		case len(refs) > 0:
			l = list{"summary.task_refs", &refs}
		case len(aliases) > 0:
			l = list{"summary.aliases", &aliases}
		default:
			// ⚠ DEFENSIVE, NOT REACHABLE WITHIN THE CAPS: with every list empty a summary is at most
			// closing_condition (1000 bytes, ≤ 6000 once JSON escapes `<`/`>`/`&` as `\u003c`) plus
			// source (256 + 64 + MaxCommittedAtBytes). `TestTheWorstCaseEmptySummaryFitsTheTotal`
			// pins that arithmetic; this branch exists so a future cap change cannot loop forever.
			return drop("over-total max=" + strconv.Itoa(MaxSummaryBytes))
		}
		last := len(*l.items) - 1
		adj = append(adj, Adjustment{AdjustDropped, itemField(l.field, (*l.items)[last].sent),
			"reason=over-total max=" + strconv.Itoa(MaxSummaryBytes)})
		*l.items = (*l.items)[:last]
	}
	out := build()
	return &out, adj
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
