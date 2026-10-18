package arcs

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ZacxDev/cairn/internal/memo"
	"github.com/ZacxDev/cairn/internal/redact"
	"github.com/ZacxDev/cairn/internal/store"
)

// Every name, path, commit and time here is synthetic; times are in the year 2000.

const testCommit = "0123456789abcdef0123456789abcdef01234567"

// summaryJSON builds a schema-2 payload around one summary's fields, each given as raw JSON.
func summaryJSON(fields map[string]string) string {
	base := map[string]string{
		"state_now": `[]`, "next_steps": `[]`, "task_refs": `[]`, "aliases": `[]`,
		"source": `{"repo_path":"docs/handoff.md","commit":"` + testCommit + `","committed_at":"2000-01-04T00:00:00Z"}`,
	}
	for k, v := range fields {
		base[k] = v
	}
	parts := []string{}
	for _, k := range []string{"closing_condition", "state_now", "next_steps", "source", "task_refs", "aliases", "extra"} {
		if v, ok := base[k]; ok {
			parts = append(parts, fmt.Sprintf("%q:%s", k, v))
		}
	}
	return `{"schema":2,"summary":{` + strings.Join(parts, ",") + `}}`
}

func jsonList(n int, item string) string {
	items := make([]string, n)
	for i := range items {
		items[i] = fmt.Sprintf("%q", item)
	}
	return "[" + strings.Join(items, ",") + "]"
}

func journalLine(t *testing.T, body string) string {
	t.Helper()
	j := Journal{Path: filepath.Join(t.TempDir(), "journal.jsonl")}
	reg, _ := decode(t, body)
	if _, err := j.Register(reg, clock); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(j.Path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestASchemaTwoPayloadUsingNothingNewIsStoredAsTheSchemaOneBytes is the ROLL-BACK property: a
// schema-2 payload that carries neither a summary nor the opt-in writes the byte-identical journal
// line a schema-1 payload writes — pinned as a LITERAL, so a build that started stamping `schema:2`
// (or any new key) on every record fails here, and a pod rolled back to a schema-1-only build would
// otherwise skip every arc registered after the upgrade.
func TestASchemaTwoPayloadUsingNothingNewIsStoredAsTheSchemaOneBytes(t *testing.T) {
	want := `{"schema":1,"home":"alpha-notes","slug":"gadget-rollout","status":"open","closing_kind":"none",` +
		`"declared_scopes":["alpha-notes"],"writers_measured":false,"readers_measured":false,"commits_total":0,` +
		`"commits_unstamped":0,"reported_at":"","members":[],"registered_by":"","registered_at":"2000-01-06T07:08:09Z"}` + "\n"
	one := journalLine(t, `{"schema":1,"status":"open"}`)
	two := journalLine(t, `{"schema":2,"status":"open","visible_to_declared":false}`)
	if one != want || two != want {
		t.Fatalf("schema-1 line:\n%s\nschema-2-with-nothing-new line:\n%s\nwant:\n%s", one, two, want)
	}
	// Control: a record that DOES use schema 2 is written under it.
	if got := journalLine(t, `{"schema":2,"visible_to_declared":true}`); !strings.HasPrefix(got, `{"schema":2,`) ||
		!strings.Contains(got, `"visible_to_declared":true`) {
		t.Fatalf("an opted-in record must be schema 2:\n%s", got)
	}
}

// TestASchemaOnePayloadCannotCarrySchemaTwoFields: refused, not silently ignored.
func TestASchemaOnePayloadCannotCarrySchemaTwoFields(t *testing.T) {
	refused(t, `{"schema":1,"visible_to_declared":true}`, "need schema 2")
	refused(t, `{"schema":1,"summary":{}}`, "need schema 2")
}

// adjusted is the registration's adjustment lines, one per line, as the response prints them.
func adjusted(reg Registration) string {
	var lines []string
	for _, a := range reg.SummaryAdjustments() {
		lines = append(lines, a.Line())
	}
	return strings.Join(lines, "\n")
}

// TestAnOverCapItemIsClippedAndNamedNeverRefused walks each byte cap with a value AT it (stored as
// sent, no adjustment) and one well PAST it: stored clipped to exactly the cap at a rune boundary,
// ending in `…`, and named `clipped <field> bytes=<sent> max=<cap>`. The registration always lands.
// Over-count items are DROPPED from the end and named. A multi-byte rune straddling the cut is never
// split.
func TestAnOverCapItemIsClippedAndNamedNeverRefused(t *testing.T) {
	q := func(s string) string { return fmt.Sprintf("%q", s) }
	for _, tc := range []struct {
		name, field, at, past, want string
		stored                      func(Summary) string
		max                         int
	}{
		{"closing", "closing_condition", q(strings.Repeat("c", MaxClosingConditionBytes)), q(strings.Repeat("c", MaxClosingConditionBytes+7)),
			"clipped summary.closing_condition bytes=1007 max=1000", func(s Summary) string { return s.ClosingCondition }, MaxClosingConditionBytes},
		{"state", "state_now", jsonList(1, strings.Repeat("s", MaxSummaryItemBytes)), jsonList(1, strings.Repeat("s", MaxSummaryItemBytes+9)),
			"clipped summary.state_now[0] bytes=1509 max=1500", func(s Summary) string { return s.StateNow[0] }, MaxSummaryItemBytes},
		{"next", "next_steps", jsonList(1, strings.Repeat("n", MaxSummaryItemBytes)), jsonList(1, strings.Repeat("n", MaxSummaryItemBytes+9)),
			"clipped summary.next_steps[0] bytes=1509 max=1500", func(s Summary) string { return s.NextSteps[0] }, MaxSummaryItemBytes},
		{"ref", "task_refs", jsonList(1, strings.Repeat("t", MaxTaskRefBytes)), jsonList(1, strings.Repeat("t", MaxTaskRefBytes+9)),
			"clipped summary.task_refs[0] bytes=209 max=200", func(s Summary) string { return s.TaskRefs[0] }, MaxTaskRefBytes},
	} {
		t.Run(tc.name, func(t *testing.T) {
			at, _ := decode(t, summaryJSON(map[string]string{tc.field: tc.at}))
			if at.Summary == nil || adjusted(at) != "" || len(tc.stored(*at.Summary)) != tc.max {
				t.Fatalf("a value AT the cap is stored as sent: %q", adjusted(at))
			}
			past, _ := decode(t, summaryJSON(map[string]string{tc.field: tc.past}))
			got := tc.stored(*past.Summary)
			if adjusted(past) != AdjustmentLinePrefix+tc.want || len(got) != tc.max || !strings.HasSuffix(got, "…") {
				t.Fatalf("over the cap: stored %d bytes %q…, adjustments %q", len(got), got[len(got)-6:], adjusted(past))
			}
		})
	}
	// A 3-byte rune straddling the cut is kept whole or dropped, never split: 1496 ASCII + a 3-byte-rune run.
	straddle, _ := decode(t, summaryJSON(map[string]string{"next_steps": jsonList(1, strings.Repeat("x", 1496)+"日本語")}))
	if got := straddle.Summary.NextSteps[0]; !utf8.ValidString(got) || len(got) > MaxSummaryItemBytes || got != strings.Repeat("x", 1496)+"…" {
		t.Fatalf("clip split a rune or overshot: %d bytes, valid=%v, tail %q", len(got), utf8.ValidString(got), got[len(got)-6:])
	}
	// Over a COUNT: the tail is dropped, each named with its sent index.
	over, _ := decode(t, summaryJSON(map[string]string{"state_now": jsonList(MaxSummaryItems+2, "s")}))
	if len(over.Summary.StateNow) != MaxSummaryItems || adjusted(over) != AdjustmentLinePrefix+"dropped summary.state_now[50] reason=over-count max=50\n"+
		AdjustmentLinePrefix+"dropped summary.state_now[51] reason=over-count max=50" {
		t.Fatalf("over the count: kept %d, adjustments %q", len(over.Summary.StateNow), adjusted(over))
	}
}

// TestARealHandoffsRankedItemsAreStoredWhole is the REGRESSION the caps were raised for, with every
// expectation a LITERAL rather than a constant (a test reading `MaxSummaryItemBytes` passes whatever
// the cap is). Measured over ~500 real handoff documents: a ranked next step's p95 is ~1.2 KB and a
// state line's ~0.7 KB, and one document carried 46 ranks. At 500 bytes and 20 items the pod clipped
// a reader's answer mid-sentence ("…") and dropped every rank past the 20th. The lengths below are
// that shape — synthetic text, real sizes — so each must now be stored exactly as sent, unadjusted.
func TestARealHandoffsRankedItemsAreStoredWhole(t *testing.T) {
	step := func(rank, n int) string {
		lead := fmt.Sprintf("%d. ", rank)
		return lead + strings.Repeat("w", n-len(lead))
	}
	// A 1026-byte rank among ordinary ones, and two state lines over the old cap.
	next := []string{step(1, 315), step(2, 139), step(3, 497), step(4, 496), step(5, 1026), step(6, 139)}
	state := []string{strings.Repeat("s", 651), strings.Repeat("t", 714), strings.Repeat("u", 237)}
	reg, _ := decode(t, summaryJSON(map[string]string{"next_steps": "[" + quoteAll(next) + "]", "state_now": "[" + quoteAll(state) + "]"}))
	if reg.Summary == nil || adjusted(reg) != "" {
		t.Fatalf("a typical handoff's items must be stored as sent, got adjustments:\n%s", adjusted(reg))
	}
	if !slices.Equal(reg.Summary.NextSteps, next) || !slices.Equal(reg.Summary.StateNow, state) {
		t.Fatalf("stored items differ from what was sent")
	}
	// 46 ranks — the longest ranked list measured — every one kept, in order.
	ranks := make([]string, 46)
	for i := range ranks {
		ranks[i] = step(i+1, 180)
	}
	long, _ := decode(t, summaryJSON(map[string]string{"next_steps": "[" + quoteAll(ranks) + "]"}))
	if long.Summary == nil || adjusted(long) != "" || len(long.Summary.NextSteps) != 46 || long.Summary.NextSteps[45] != ranks[45] {
		t.Fatalf("46 ranks must all be kept: kept %d, adjustments:\n%s", len(long.Summary.NextSteps), adjusted(long))
	}
}

func quoteAll(items []string) string {
	q := make([]string, len(items))
	for i, s := range items {
		q[i] = fmt.Sprintf("%q", s)
	}
	return strings.Join(q, ",")
}

// TestTheItemCapsAtTheirBoundaries walks the per-item byte cap and the per-list count with LITERAL
// values, each well clear of the boundary on its side (under by 9, over by 9, far over) as well as
// exactly at it, so a cap moved by a few bytes or one item either way fails a row.
func TestTheItemCapsAtTheirBoundaries(t *testing.T) {
	for _, tc := range []struct {
		n    int
		want string // the adjustment line, "" for stored as sent
		kept int    // stored length
	}{
		{1491, "", 1491},
		{1500, "", 1500},
		{1509, "clipped summary.next_steps[0] bytes=1509 max=1500", 1500},
		{4000, "clipped summary.next_steps[0] bytes=4000 max=1500", 1500},
	} {
		reg, _ := decode(t, summaryJSON(map[string]string{"next_steps": jsonList(1, strings.Repeat("n", tc.n))}))
		got := reg.Summary.NextSteps[0]
		want := ""
		if tc.want != "" {
			want = AdjustmentLinePrefix + tc.want
		}
		if adjusted(reg) != want || len(got) != tc.kept || (tc.want != "") != strings.HasSuffix(got, "…") {
			t.Fatalf("%d bytes: stored %d, adjustments %q, want %q", tc.n, len(got), adjusted(reg), want)
		}
	}
	for _, tc := range []struct {
		n    int
		want string
	}{
		{41, ""},
		{50, ""},
		{53, "dropped summary.state_now[50] reason=over-count max=50\n" + AdjustmentLinePrefix +
			"dropped summary.state_now[51] reason=over-count max=50\n" + AdjustmentLinePrefix +
			"dropped summary.state_now[52] reason=over-count max=50"},
	} {
		reg, _ := decode(t, summaryJSON(map[string]string{"state_now": jsonList(tc.n, "s")}))
		want, kept := "", tc.n
		if tc.want != "" {
			want, kept = AdjustmentLinePrefix+tc.want, 50
		}
		if adjusted(reg) != want || len(reg.Summary.StateNow) != kept {
			t.Fatalf("%d items: kept %d, adjustments %q", tc.n, len(reg.Summary.StateNow), adjusted(reg))
		}
	}
}

// TestTheWholeSummaryBoundStillHolds: the total is a LITERAL 24576 bytes of stored JSON. Both lists
// full of items at the item cap (~150 KB sent) store at most that, every drop named with the literal
// bound; a summary under it by a margin is stored as sent; one over it by a margin loses exactly its
// last item.
func TestTheWholeSummaryBoundStillHolds(t *testing.T) {
	item := strings.Repeat("x", 1500)
	full, _ := decode(t, summaryJSON(map[string]string{"state_now": jsonList(50, item), "next_steps": jsonList(50, item)}))
	if full.Summary == nil || summaryBytes(*full.Summary) > 24576 {
		t.Fatalf("the stored summary must fit 24576 bytes: %v", full.Summary == nil)
	}
	dropped := strings.Count(adjusted(full), "reason=over-total max=24576")
	if dropped < 80 || len(full.Summary.StateNow)+len(full.Summary.NextSteps)+dropped != 100 {
		t.Fatalf("each drop is named once against the literal bound: kept %d+%d, named %d",
			len(full.Summary.StateNow), len(full.Summary.NextSteps), dropped)
	}
	// 15 items of 1500 bytes store at ~22.8 KB: under the bound, stored as sent.
	under, _ := decode(t, summaryJSON(map[string]string{"next_steps": jsonList(15, item)}))
	if under.Summary == nil || adjusted(under) != "" || len(under.Summary.NextSteps) != 15 {
		t.Fatalf("a summary under the total is stored as sent: %q", adjusted(under))
	}
	// 17 items store at ~25.8 KB: over by a margin, so exactly the 17th is dropped.
	over, _ := decode(t, summaryJSON(map[string]string{"next_steps": jsonList(17, item)}))
	if adjusted(over) != AdjustmentLinePrefix+"dropped summary.next_steps[16] reason=over-total max=24576" || len(over.Summary.NextSteps) != 16 {
		t.Fatalf("over the total: kept %d, adjustments %q", len(over.Summary.NextSteps), adjusted(over))
	}
}

// TestTheReadSideReChecksTheTotal: the fold applies a stored summary up to the literal 24576 bytes and
// SKIPS one past it. The applied arm (~24.2 KB) is over the OLD 16 KiB total — which is exactly the
// record an older build skips, the rollback hazard the README names.
func TestTheReadSideReChecksTheTotal(t *testing.T) {
	line := strings.TrimSpace(journalLine(t, summaryJSON(nil)))
	item := strings.Repeat("x", 1500)
	for _, tc := range []struct {
		n       int
		applied bool
	}{{16, true}, {17, false}} {
		stored := strings.Replace(line, `"state_now":[]`, `"state_now":`+jsonList(tc.n, item), 1)
		snap := fold([]byte(stored + "\n"))
		if (snap.Records == 1) != tc.applied || (snap.Skipped == 1) == tc.applied {
			t.Fatalf("%d items (%d bytes stored): records %d skipped %d, want applied=%v", tc.n, len(stored), snap.Records, snap.Skipped, tc.applied)
		}
	}
}

// TestOverTheTotalDropsFromTheListEndsAndNamesEachDrop: every item within its own cap, the whole over
// 24 KiB. The pod drops items from the END — the longer of state_now/next_steps first — until it
// fits, names each with its sent index, and keeps everything else. The control is a summary half
// that size, which is stored with no adjustment.
func TestOverTheTotalDropsFromTheListEndsAndNamesEachDrop(t *testing.T) {
	item := strings.Repeat("x", MaxSummaryItemBytes)
	reg, _ := decode(t, summaryJSON(map[string]string{"state_now": jsonList(MaxSummaryItems, item), "next_steps": jsonList(MaxSummaryItems, item)}))
	if reg.Summary == nil || summaryBytes(*reg.Summary) > MaxSummaryBytes {
		t.Fatalf("the summary must be kept within the total: %+v", reg.Summary)
	}
	dropped := strings.Count(adjusted(reg), "reason=over-total max=24576")
	if dropped == 0 || len(reg.Summary.StateNow)+len(reg.Summary.NextSteps)+dropped != 2*MaxSummaryItems {
		t.Fatalf("each drop is named once: kept %d+%d, named %d\n%s", len(reg.Summary.StateNow), len(reg.Summary.NextSteps), dropped, adjusted(reg))
	}
	if !strings.HasPrefix(adjusted(reg), AdjustmentLinePrefix+"dropped summary.state_now[49] reason=over-total") ||
		!strings.Contains(adjusted(reg), "dropped summary.next_steps[49] reason=over-total") {
		t.Fatalf("drops start at the lists' ends, state_now first on a tie:\n%s", adjusted(reg))
	}
	if half, _ := decode(t, summaryJSON(map[string]string{"state_now": jsonList(15, item)})); half.Summary == nil || adjusted(half) != "" {
		t.Fatalf("control: 15 such items (~22.8 KB) are stored as sent: %q", adjusted(half))
	}
}

// TestNoSummaryProblemRefusesTheRegistration: every summary-content shape that used to refuse the
// push now lands the arc — status and members included — with the summary adjusted and each change
// named. The NEGATIVE half: what is NOT summary content (an unknown key, a wrong type) still refuses.
func TestNoSummaryProblemRefusesTheRegistration(t *testing.T) {
	// A credential-shaped value built at run time, so no literal token sits in this file.
	token := "gh" + "p_" + strings.Repeat("Ab3", 12)
	for _, tc := range []struct {
		field, value, want string
		stored             func(*Summary) string
	}{
		{"state_now", `["  ","kept"]`, "dropped summary.state_now[0] reason=blank", func(s *Summary) string { return strings.Join(s.StateNow, "|") }},
		{"state_now", `["two\nlines"]`, "sanitized summary.state_now[0] replaced=1", func(s *Summary) string { return s.StateNow[0] }},
		{"next_steps", `["closed\u2028x\u0007y"]`, "sanitized summary.next_steps[0] replaced=2", func(s *Summary) string { return s.NextSteps[0] }},
		{"closing_condition", `"tab\there"`, "sanitized summary.closing_condition replaced=1", func(s *Summary) string { return s.ClosingCondition }},
		{"next_steps", `["rotate ` + token + `","kept"]`, "redacted summary.next_steps[0] rule=github-token", func(s *Summary) string { return strings.Join(s.NextSteps, "|") }},
		{"task_refs", `["has space","t:2"]`, "dropped summary.task_refs[0] reason=whitespace", func(s *Summary) string { return strings.Join(s.TaskRefs, "|") }},
		{"aliases", `["___","ok"]`, "dropped summary.aliases[0] reason=normalizes-to-nothing", func(s *Summary) string { return strings.Join(s.Aliases, "|") }},
	} {
		body := strings.Replace(summaryJSON(map[string]string{tc.field: tc.value}), `{"schema":2,`, `{"schema":2,"status":"open","members":[{"session":"s-1","role":"wrote"}],`, 1)
		reg, _ := decode(t, body)
		if reg.Status != StatusOpen || len(reg.Members) != 1 || reg.Summary == nil {
			t.Fatalf("%s %s: the arc must land with its schema-1 data: %+v", tc.field, tc.value, reg)
		}
		if adjusted(reg) != AdjustmentLinePrefix+tc.want {
			t.Fatalf("%s %s: adjustments %q, want %q", tc.field, tc.value, adjusted(reg), tc.want)
		}
		got := tc.stored(reg.Summary)
		if strings.Contains(got, token) || strings.ContainsAny(got, "\n\t\a\u2028") {
			t.Fatalf("%s: stored %q still carries what was adjusted", tc.field, got)
		}
		if tc.want == "redacted summary.next_steps[0] rule=github-token" && got != "[redacted: github-token]|kept" {
			t.Fatalf("a secret-shaped item is stored as its rule, the next one as sent: %q", got)
		}
	}
	// A drop names the index as SENT, not the position after earlier drops: the blank ref at [1] is
	// dropped first, so the whitespace one is still [2].
	refs, _ := decode(t, summaryJSON(map[string]string{"task_refs": `["t:1","  ","has space"]`}))
	if adjusted(refs) != AdjustmentLinePrefix+"dropped summary.task_refs[1] reason=blank\n"+AdjustmentLinePrefix+"dropped summary.task_refs[2] reason=whitespace" ||
		strings.Join(refs.Summary.TaskRefs, "|") != "t:1" {
		t.Fatalf("sent indexes: %q, kept %q", adjusted(refs), refs.Summary.TaskRefs)
	}
	// A source that cannot be cited drops the SUMMARY, never the arc.
	for _, tc := range []struct{ source, reason string }{
		{`{"repo_path":"/abs.md","commit":"` + testCommit + `","committed_at":"2000-01-04T00:00:00Z"}`, "source.repo_path"},
		{`{"repo_path":"a/../b.md","commit":"` + testCommit + `","committed_at":"2000-01-04T00:00:00Z"}`, "source.repo_path"},
		{`{"repo_path":"a.md","commit":"ABC","committed_at":"2000-01-04T00:00:00Z"}`, "source.commit"},
		{`{"repo_path":"a.md","commit":"` + testCommit + `","committed_at":"yesterday"}`, "source.committed_at"},
	} {
		reg, _ := decode(t, summaryJSON(map[string]string{"source": tc.source}))
		if reg.Summary != nil || adjusted(reg) != AdjustmentLinePrefix+"dropped summary reason="+tc.reason {
			t.Fatalf("source %s: summary %+v, adjustments %q", tc.source, reg.Summary, adjusted(reg))
		}
	}
	// Not summary CONTENT: still a refusal.
	refused(t, summaryJSON(map[string]string{"source": `{"repo_path":"a.md","commit":"` + testCommit + `","committed_at":"2000-01-04T00:00:00Z","subject":"x"}`}), "unknown field")
	refused(t, summaryJSON(map[string]string{"extra": `"x"`}), "unknown field")
	refused(t, summaryJSON(map[string]string{"state_now": `"not a list"`}), "cannot unmarshal")
	// An adjustment NEVER echoes the value it changed.
	reg, _ := decode(t, summaryJSON(map[string]string{"next_steps": `["rotate ` + token + `"]`}))
	if strings.Contains(adjusted(reg), token) {
		t.Fatalf("the redaction line must not echo the value: %q", adjusted(reg))
	}
}

// storedAs runs ONE state_now item through the whole summary pipeline (sanitize, scan window,
// redaction, clip) and returns what is stored.
func storedAs(t *testing.T, text string) string {
	t.Helper()
	reg, _ := decode(t, summaryJSON(map[string]string{"state_now": jsonList(1, text)}))
	if reg.Summary == nil || len(reg.Summary.StateNow) != 1 {
		t.Fatalf("%q: no item stored: %+v %q", text, reg.Summary, adjusted(reg))
	}
	return reg.Summary.StateNow[0]
}

// TestEveryTextAConfidentRuleMatchesAsSentIsRedacted: the raw text is scanned and nothing explains a
// hit away, so every row below — each one a text some earlier design stored verbatim — is redacted.
// They are round 3's bracketed credentials, round 4's placeholder-inside-a-value rows, round 5's
// stray-sigil rows, and a placeholder in a credential position, which is now redacted BY DESIGN.
// The precondition is asserted per row: a row the raw scan does not match would prove nothing here.
// Values are built at run time so no literal token sits in this file.
func TestEveryTextAConfidentRuleMatchesAsSentIsRedacted(t *testing.T) {
	pw := "Xk9mQ2pL" + "r7vB"
	ghp := "gh" + "p_" + strings.Repeat("Ab3d", 9)
	npm := "np" + "m_" + strings.Repeat("Ab3d", 9)
	aiza := "AI" + "za" + strings.Repeat("Ab3dE", 7)
	akia := "AK" + "IA" + strings.Repeat("Q7ZX", 4)
	scan, err := scanner()
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{
		// round 3: bracketed or glued credentials
		"db is <postgres://app:" + pw + "@db.example.test:5432/app>", "use <redis://:" + pw + "@cache-0:6379>",
		"token <" + ghp + ">", "<" + npm + ">", "<" + aiza + ">", "<" + akia + ">",
		"https://app:$ecretPass9x@db.example.test/app", "https://app:$" + akia + "@db.example.test/app",
		"postgresql://app:${PGPASSWORD}@db/app and " + ghp, "key " + akia + "${SUFFIX}Q",
		// round 4: a placeholder inside or beside a real password
		"postgres://app:${X}" + pw + "@db", "postgres://app:" + pw + "${X}@db", "postgres://app:Xk9mQ2$PLr7vB@db",
		"postgres://app:hunter2***Xk9mQ@db", "postgres://app:Xk9m***@db", "redis://:$REDIS" + pw + "@r:6379",
		// round 5: a stray sigil glued to a token
		"key " + akia + "$x", "foo*" + ghp, "x{" + ghp + "}", "v**" + aiza, "w$" + akia,
		// a placeholder in a credential position: redacted by design
		"postgresql://app:${PGPASSWORD}@db/app", "postgresql://app:$PGPASSWORD@db/app", "https://user:***@host.example.test/path",
		// a credential straddling the clip cut is seen whole (the scan reads cap + scanMargin)
		strings.Repeat("x", 480) + " " + ghp,
	} {
		if firstConfident(scan, text) == "" {
			t.Errorf("PRECONDITION: no confident rule matches %q as sent; it does not belong in this table", text)
			continue
		}
		if got := storedAs(t, text); !strings.HasPrefix(got, "[redacted: ") {
			t.Errorf("%q must be redacted, stored %q", text, got)
		}
	}
}

// TestTextNoConfidentRuleMatchesIsStoredAsWritten: prose that merely LOOKS like a placeholder or a
// header is stored unchanged, because no confident rule matches it as sent — a variable in prose,
// emphasis, a bracketed token NAME, and a random id only a BROAD rule matches. The redaction is the
// confident rules' verdict on the raw text, nothing more and nothing less.
func TestTextNoConfidentRuleMatchesIsStoredAsWritten(t *testing.T) {
	for _, text := range []string{
		"add the tool to $PATH first", "***bold*** is the convention here", "Authorization: Bearer <token>",
		"x-api-key: <your-key>", "redis://:<password>@cache:6379", "costs $5 per month", "set ${HOME}/bin first",
		// A BROAD rule (entropy) matches this random id; only the confident subset redacts.
		"the run id is Qx7Lm2Np8Rt4Vw6Zy9Ab3",
	} {
		if got := storedAs(t, text); got != text {
			t.Errorf("%q must be stored as written, stored %q", text, got)
		}
	}
}

// TestEveryRawHitIsRedactedAcrossTheGeneratedCorpus generates, for EVERY confident rule the scan
// acts on, a real-shaped value in its credential position, and around it each placeholder shape
// BEFORE, INSIDE (three cut points) and AFTER, with and without a space; each stray sigil glued on
// either side; and values with a `$UPPER` run inside them (four seeds). Every text goes through the
// WHOLE pipeline. A LEAK is a text a confident rule matches as sent that is stored unredacted —
// expected 0. Also counted, and printed: texts whose placeholder-free twin a rule matches but which
// no rule matches as sent (the bearer and authorization classes cannot match through `$`, `{` or
// `*`); those are stored, by the raw-scan design. The positive control is a floor on raw hits.
func TestEveryRawHitIsRedactedAcrossTheGeneratedCorpus(t *testing.T) {
	var rng *rand.Rand
	pick := func(set string, n int) string {
		var b strings.Builder
		for range n {
			b.WriteByte(set[rng.IntN(len(set))])
		}
		return b.String()
	}
	const alnum = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	const upper = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	// value makes a fresh real-shaped value; tmpl puts it in its credential position.
	type rule struct {
		name  string
		value func() string
		tmpl  func(v string) string
	}
	pwd := func() string { return "Q7" + pick(alnum, 12) }
	rules := []rule{
		{"url-userinfo/postgres", pwd, func(v string) string { return "postgres://app:" + v + "@db.example.test:5432/app" }},
		{"url-userinfo/redis", pwd, func(v string) string { return "redis://:" + v + "@cache-0:6379" }},
		{"bearer", pwd, func(v string) string { return "Bearer " + v }},
		{"authorization", pwd, func(v string) string { return "Authorization: Bearer " + v }},
		{"x-api-key", pwd, func(v string) string { return "x-api-key: " + v }},
		{"sk-key", func() string { return "sk" + "-" + pick(alnum, 24) }, func(v string) string { return "use " + v }},
		{"github-token", func() string { return "gh" + "p_" + pick(alnum, 36) }, func(v string) string { return "token " + v }},
		{"npm-token", func() string { return "np" + "m_" + pick(alnum, 36) }, func(v string) string { return "token " + v }},
		{"google-api-key", func() string { return "AI" + "za" + pick(alnum, 35) }, func(v string) string { return "key " + v }},
		{"aws-access-key-id", func() string { return "AK" + "IA" + pick(upper, 16) }, func(v string) string { return "key " + v }},
		{"jwt", func() string {
			return "ey" + "J" + pick(alnum, 14) + ".ey" + "J" + pick(alnum, 14) + "." + pick(alnum, 14)
		}, func(v string) string { return "jwt " + v }},
		{"slack-token", func() string { return "xo" + "xb-" + pick(alnum, 24) }, func(v string) string { return "token " + v }},
		{"stripe-key", func() string { return "sk" + "_test_" + pick(alnum, 24) }, func(v string) string { return "key " + v }},
	}
	placeholders := []string{"${X}", "${PGPASSWORD}", "$X", "$PW", "$REDIS", "***", "****"}
	scan, err := scanner()
	if err != nil {
		t.Fatal(err)
	}
	ran, rawHits, leaks, twinOnly := 0, 0, 0, 0
	check := func(r rule, text, twin string) {
		ran++
		if firstConfident(scan, text) == "" {
			if firstConfident(scan, twin) != "" {
				twinOnly++
			}
			return
		}
		rawHits++
		if got := storedAs(t, text); !strings.HasPrefix(got, "[redacted: ") {
			leaks++
			if leaks <= 10 {
				t.Errorf("LEAK (%s): %q stored %q", r.name, text, got)
			}
		}
	}
	for seed := uint64(1); seed <= 4; seed++ {
		rng = rand.New(rand.NewPCG(289, seed))
		for _, r := range rules {
			for _, ph := range placeholders {
				for range 3 {
					v := r.value()
					type variant struct{ with, twin string }
					variants := []variant{{ph + v, v}, {v + ph, v}, {ph + " " + v, " " + v}, {v + " " + ph, v + " "}}
					for _, k := range []int{1, len(v) / 2, len(v) - 1} {
						variants = append(variants, variant{v[:k] + ph + v[k:], v})
					}
					for _, vv := range variants {
						check(r, r.tmpl(vv.with), r.tmpl(vv.twin))
					}
				}
				check(r, r.tmpl(ph), r.tmpl(ph)) // the placeholder alone in the credential position
			}
			for _, sig := range []string{"$", "*", "**", "{", "}", "$x", "x$"} {
				v := r.value()
				check(r, r.tmpl(sig+v), r.tmpl(v))
				check(r, r.tmpl(v+sig), r.tmpl(v))
				check(r, "w"+sig+r.tmpl(v), r.tmpl(v))
			}
			for range 40 {
				v := r.value()
				k := 1 + rng.IntN(len(v)-1)
				run := "$" + pick(upper, 1+rng.IntN(4))
				check(r, r.tmpl(v[:k]+run+v[k:]), r.tmpl(v[:k]+run[1:]+v[k:]))
			}
		}
	}
	t.Logf("generated corpus: %d texts over %d rules; %d matched as sent, %d of those leaked; %d stored because only their placeholder-free twin matches",
		ran, len(rules), rawHits, leaks, twinOnly)
	if leaks != 0 {
		t.Fatalf("%d of %d raw-hit texts leaked", leaks, rawHits)
	}
	if rawHits < ran/2 {
		t.Fatalf("POSITIVE CONTROL: only %d of %d texts matched as sent — the generator is not producing what it claims", rawHits, ran)
	}
}

// TestALongCommittedAtDropsTheSummaryNotEveryItem: Go's RFC 3339 parser accepts a fractional field
// of any length, so an unbounded `committed_at` could carry a summary past the total on its own and
// drop every item before dropping the summary. It is capped instead and classed as an uncitable
// source; the arc lands.
func TestALongCommittedAtDropsTheSummaryNotEveryItem(t *testing.T) {
	long := `{"repo_path":"a.md","commit":"` + testCommit + `","committed_at":"2000-01-04T00:00:00.` + strings.Repeat("0", 20000) + `Z"}`
	reg, _ := decode(t, summaryJSON(map[string]string{"source": long, "state_now": `["kept"]`}))
	if reg.Summary != nil || adjusted(reg) != AdjustmentLinePrefix+"dropped summary reason=source.committed_at" {
		t.Fatalf("a long committed_at: summary %+v, adjustments %q", reg.Summary, adjusted(reg))
	}
	at := `{"repo_path":"a.md","commit":"` + testCommit + `","committed_at":"2000-01-04T00:00:00.123456789+07:00"}`
	if ok, _ := decode(t, summaryJSON(map[string]string{"source": at})); ok.Summary == nil || adjusted(ok) != "" {
		t.Fatalf("control: the longest valid instant (%d bytes) is kept: %q", MaxCommittedAtBytes, adjusted(ok))
	}
}

// TestTheWorstCaseEmptySummaryFitsTheTotal pins why the whole-summary `over-total` drop is defensive
// only: the largest summary with every list empty — a closing condition of the character JSON
// escapes widest, and every source field at its cap — is still under MaxSummaryBytes.
func TestTheWorstCaseEmptySummaryFitsTheTotal(t *testing.T) {
	s := Summary{ClosingCondition: strings.Repeat("<", MaxClosingConditionBytes),
		Source:   Source{RepoPath: strings.Repeat("r", MaxRepoPathBytes), Commit: strings.Repeat("a", 64), CommittedAt: strings.Repeat("9", MaxCommittedAtBytes)},
		StateNow: []string{}, NextSteps: []string{}, TaskRefs: []string{}, Aliases: []string{}}
	if n := summaryBytes(s); n > MaxSummaryBytes {
		t.Fatalf("the worst-case empty summary is %d bytes, over %d: the defensive drop is reachable", n, MaxSummaryBytes)
	}
}

// TestAClippedLineReportsTheLengthItWasGivenAndTheLengthItCompared: `bytes=` is the item's length
// as the pod received it, and when sanitizing changed that length the line adds `sanitized_bytes=`,
// the length compared with the cap. 200 U+2028 (3 bytes each) become 200 spaces.
func TestAClippedLineReportsTheLengthItWasGivenAndTheLengthItCompared(t *testing.T) {
	text := strings.Repeat(" ", 200) + strings.Repeat("x", 1450)
	reg, _ := decode(t, summaryJSON(map[string]string{"next_steps": jsonList(1, text)}))
	want := AdjustmentLinePrefix + "sanitized summary.next_steps[0] replaced=200\n" +
		AdjustmentLinePrefix + "clipped summary.next_steps[0] bytes=2050 max=1500 sanitized_bytes=1650"
	if adjusted(reg) != want {
		t.Fatalf("adjustments:\n%s\nwant:\n%s", adjusted(reg), want)
	}
	plain, _ := decode(t, summaryJSON(map[string]string{"next_steps": jsonList(1, strings.Repeat("x", 1650))}))
	if adjusted(plain) != AdjustmentLinePrefix+"clipped summary.next_steps[0] bytes=1650 max=1500" {
		t.Fatalf("control: no sanitized_bytes when sanitizing changed nothing: %q", adjusted(plain))
	}
}

// TestARedactedAliasIsDroppedNotStoredAsAJunkName: `[redacted: github-token]` would fold to the alias
// `redacted-github-token`; the alias is dropped instead, after its redaction line.
func TestARedactedAliasIsDroppedNotStoredAsAJunkName(t *testing.T) {
	token := "gh" + "p_" + strings.Repeat("Ab3", 12)
	reg, _ := decode(t, summaryJSON(map[string]string{"aliases": jsonList(1, token) + ``}))
	reg2, _ := decode(t, summaryJSON(map[string]string{"aliases": `["` + token + `","ok"]`}))
	for _, r := range []Registration{reg, reg2} {
		for _, a := range r.Summary.Aliases {
			if strings.Contains(a, "redacted") {
				t.Fatalf("a redacted alias was stored: %q", r.Summary.Aliases)
			}
		}
	}
	if adjusted(reg2) != AdjustmentLinePrefix+"redacted summary.aliases[0] rule=github-token\n"+AdjustmentLinePrefix+"dropped summary.aliases[0] reason=redacted" ||
		strings.Join(reg2.Summary.Aliases, "|") != "ok" {
		t.Fatalf("adjustments %q, aliases %q", adjusted(reg2), reg2.Summary.Aliases)
	}
}

// TestNextStepsKeepTheirRankAndAliasesFold: order is meaning for the ranked list (never sorted or
// deduplicated); aliases fold like slugs, deduplicate and sort.
func TestNextStepsKeepTheirRankAndAliasesFold(t *testing.T) {
	reg, _ := decode(t, summaryJSON(map[string]string{"next_steps": `["zeta","alpha","zeta"]`,
		"aliases": `["Zeta Move","zeta-move","Alpha_Move"]`}))
	if got := strings.Join(reg.Summary.NextSteps, "|"); got != "zeta|alpha|zeta" {
		t.Fatalf("next steps reordered: %s", got)
	}
	if got := strings.Join(reg.Summary.Aliases, "|"); got != "alpha-move|zeta-move" {
		t.Fatalf("aliases: %s", got)
	}
}

// TestEveryPushReplacesTheSummaryAndTheOptIn: neither is carried across a push. A schema-1 push
// (a host whose tooling cannot send schema 2) CLEARS both and is stored as schema 1, and a schema-2
// push without the opt-in makes an opted-in arc home-only again — so widening an arc's visibility
// is always reversible by the next push.
func TestEveryPushReplacesTheSummaryAndTheOptIn(t *testing.T) {
	j := Journal{Path: filepath.Join(t.TempDir(), "journal.jsonl")}
	push := func(body string) Registration {
		t.Helper()
		reg, _ := decode(t, body)
		out, err := j.Register(reg, clock)
		if err != nil {
			t.Fatal(err)
		}
		return out.Record
	}
	optedIn := strings.Replace(summaryJSON(nil), `{"schema":2,`, `{"schema":2,"visible_to_declared":true,`, 1)
	first := push(optedIn)
	if first.Summary == nil || !first.VisibleToDeclared || first.Schema != SchemaSummary {
		t.Fatalf("POSITIVE CONTROL: the first push opts in with a summary: %+v", first)
	}
	cleared := push(`{"schema":1,"status":"closed"}`)
	if cleared.Status != StatusClosed || cleared.Summary != nil || cleared.VisibleToDeclared || cleared.Schema != Schema {
		t.Fatalf("a schema-1 push must CLEAR the summary and the opt-in: %+v", cleared)
	}
	push(optedIn)
	homeOnly := push(summaryJSON(nil))
	if homeOnly.VisibleToDeclared || homeOnly.Summary == nil {
		t.Fatalf("a schema-2 push without the opt-in makes the arc home-only and keeps its own summary: %+v", homeOnly)
	}
	snap, err := j.Read()
	if err != nil || snap.Skipped != 0 || snap.Records != 4 {
		t.Fatalf("every record must fold back (records %d, skipped %d, %v)", snap.Records, snap.Skipped, err)
	}
	if got := snap.Latest[Key{"alpha-notes", "gadget-rollout"}]; got.VisibleToDeclared {
		t.Fatalf("the folded state must be the last push: %+v", got)
	}
}

// TestOneArcStateHasOneSpellingInTheJournal: the fold SKIPS a record whose schema disagrees with
// what it uses — schema 1 carrying schema-2 keys, schema 2 carrying none — and a stored summary
// breaking a bound. Control: the well-formed schema-2 record is applied.
func TestOneArcStateHasOneSpellingInTheJournal(t *testing.T) {
	good := strings.TrimSpace(journalLine(t, `{"schema":2,"visible_to_declared":true}`))
	for name, line := range map[string]string{
		"schema 1 with the opt-in": strings.Replace(good, `"schema":2`, `"schema":1`, 1),
		"schema 2 using nothing":   strings.Replace(good, `,"visible_to_declared":true`, "", 1),
		"schema 3":                 strings.Replace(good, `"schema":2`, `"schema":3`, 1),
	} {
		if snap := fold([]byte(line + "\n")); snap.Skipped != 1 || snap.Records != 0 {
			t.Fatalf("%s must be skipped: %+v\n%s", name, snap, line)
		}
	}
	if snap := fold([]byte(good + "\n")); snap.Records != 1 || snap.Skipped != 0 {
		t.Fatalf("control: the well-formed schema-2 record applies: %+v", snap)
	}
	over := strings.Replace(journalLine(t, summaryJSON(nil)), `"state_now":[]`, `"state_now":`+jsonList(MaxSummaryItems+1, "s"), 1)
	if snap := fold([]byte(over)); snap.Skipped != 1 {
		t.Fatalf("a stored summary over its bound must be skipped: %+v", snap)
	}
}

// TestVisibleToIsTheHomeOrAnOptedInDeclaredScope is the predicate's own truth table. Every arm the
// opt-in adds has a twin without it, so a predicate ignoring the flag fails a row.
func TestVisibleToIsTheHomeOrAnOptedInDeclaredScope(t *testing.T) {
	reg := Registration{Home: "alpha-notes", DeclaredScopes: []string{"alpha-notes", "beta-notes"}}
	opted := reg
	opted.VisibleToDeclared = true
	for _, tc := range []struct {
		name    string
		reg     Registration
		visible store.ScopeSet
		want    bool
	}{
		{"home readable", reg, store.VisibleScopeSet([]string{"alpha-notes"}), true},
		{"declared readable, not opted in", reg, store.VisibleScopeSet([]string{"beta-notes"}), false},
		{"declared readable, opted in", opted, store.VisibleScopeSet([]string{"beta-notes"}), true},
		{"declared readable spelled differently, opted in", opted, store.VisibleScopeSet([]string{"Beta_Notes"}), true},
		{"neither readable, opted in", opted, store.VisibleScopeSet([]string{"gamma-notes"}), false},
		{"nothing readable, opted in", opted, store.VisibleScopeSet(nil), false},
	} {
		if got := tc.reg.VisibleTo(tc.visible); got != tc.want {
			t.Fatalf("%s: VisibleTo = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestAHostileAliasIsStoredInTheSlugCharsetOrRefused: an alias is folded to the slug charset
// (`[a-z0-9.-]`), so a stored alias can never carry the listing row's ` · ` separator, a `/`, a
// space or a non-ASCII character — which is what lets a consumer read the aliases segment as the
// row's LAST ` · ` segment. One that folds to nothing is dropped and named, never stored empty.
func TestAHostileAliasIsStoredInTheSlugCharsetOrRefused(t *testing.T) {
	slug := regexp.MustCompile(`^[a-z0-9.-]+$`)
	hostile := []string{"one · two", "HTTP/3", "with  spaces", "naïve café", "x · declared-visible", "a,b"}
	raw := make([]string, len(hostile))
	for i, h := range hostile {
		raw[i] = fmt.Sprintf("%q", h)
	}
	reg, _ := decode(t, summaryJSON(map[string]string{"aliases": "[" + strings.Join(raw, ",") + "]"}))
	if len(reg.Summary.Aliases) == 0 {
		t.Fatal("POSITIVE CONTROL: hostile aliases that fold to something are stored")
	}
	for _, a := range reg.Summary.Aliases {
		if !slug.MatchString(a) || strings.Contains(a, "·") {
			t.Fatalf("stored alias %q is outside the slug charset (all: %q)", a, reg.Summary.Aliases)
		}
	}
	if !slices.Contains(reg.Summary.Aliases, "http-3") || !slices.Contains(reg.Summary.Aliases, "one-two") {
		t.Fatalf("`/` and ` · ` fold to `-`: %q", reg.Summary.Aliases)
	}
	nothing, _ := decode(t, summaryJSON(map[string]string{"aliases": `[" · "]`}))
	if adjusted(nothing) != AdjustmentLinePrefix+"dropped summary.aliases[0] reason=normalizes-to-nothing" {
		t.Fatalf("an alias that folds to nothing is dropped and named: %q", adjusted(nothing))
	}
}

// TestAScannerConstructionErrorIsNotCached: the first construction fails, and that call's summary
// is DROPPED (named `reason=secret-scan-unavailable`) while the arc registers; the SECOND call constructs again, succeeds, and is the scanner used
// from then on. Caching the first error would refuse every summary for the life of the process.
func TestAScannerConstructionErrorIsNotCached(t *testing.T) {
	scannerMu.Lock()
	saved, savedNew := scannerOK, newScanner
	scannerOK = nil
	calls := 0
	newScanner = func() (*redact.Redactor, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("synthetic construction failure")
		}
		return memo.NewScanner()
	}
	scannerMu.Unlock()
	t.Cleanup(func() {
		scannerMu.Lock()
		scannerOK, newScanner = saved, savedNew
		scannerMu.Unlock()
	})
	body := summaryJSON(map[string]string{"state_now": `["moved"]`})
	if first, _ := decode(t, body); first.Summary != nil ||
		adjusted(first) != AdjustmentLinePrefix+"dropped summary reason=secret-scan-unavailable" {
		t.Fatalf("the first call drops the summary, names why, and still registers: %+v %q", first.Summary, adjusted(first))
	}
	if reg, _ := decode(t, body); reg.Summary == nil {
		t.Fatal("the second call must construct again and decode")
	}
	decode(t, body)
	if calls != 2 {
		t.Fatalf("constructions = %d, want 2 (one failure, one success, then cached)", calls)
	}
}
