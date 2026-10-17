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
			"clipped summary.state_now[0] bytes=509 max=500", func(s Summary) string { return s.StateNow[0] }, MaxSummaryItemBytes},
		{"next", "next_steps", jsonList(1, strings.Repeat("n", MaxSummaryItemBytes)), jsonList(1, strings.Repeat("n", MaxSummaryItemBytes+9)),
			"clipped summary.next_steps[0] bytes=509 max=500", func(s Summary) string { return s.NextSteps[0] }, MaxSummaryItemBytes},
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
	// A 3-byte rune straddling the cut is kept whole or dropped, never split: 499 ASCII + one `é`-run.
	straddle, _ := decode(t, summaryJSON(map[string]string{"next_steps": jsonList(1, strings.Repeat("x", 496)+"日本語")}))
	if got := straddle.Summary.NextSteps[0]; !utf8.ValidString(got) || len(got) > MaxSummaryItemBytes || got != strings.Repeat("x", 496)+"…" {
		t.Fatalf("clip split a rune or overshot: %d bytes, valid=%v, tail %q", len(got), utf8.ValidString(got), got[len(got)-6:])
	}
	// Over a COUNT: the tail is dropped, each named with its sent index.
	over, _ := decode(t, summaryJSON(map[string]string{"state_now": jsonList(MaxSummaryItems+2, "s")}))
	if len(over.Summary.StateNow) != MaxSummaryItems || adjusted(over) != AdjustmentLinePrefix+"dropped summary.state_now[20] reason=over-count max=20\n"+
		AdjustmentLinePrefix+"dropped summary.state_now[21] reason=over-count max=20" {
		t.Fatalf("over the count: kept %d, adjustments %q", len(over.Summary.StateNow), adjusted(over))
	}
}

// TestOverTheTotalDropsFromTheListEndsAndNamesEachDrop: every item within its own cap, the whole over
// 16 KiB. The pod drops items from the END — the longer of state_now/next_steps first — until it
// fits, names each with its sent index, and keeps everything else. The control is a summary half
// that size, which is stored with no adjustment.
func TestOverTheTotalDropsFromTheListEndsAndNamesEachDrop(t *testing.T) {
	item := strings.Repeat("x", MaxSummaryItemBytes)
	reg, _ := decode(t, summaryJSON(map[string]string{"state_now": jsonList(MaxSummaryItems, item), "next_steps": jsonList(MaxSummaryItems, item)}))
	if reg.Summary == nil || summaryBytes(*reg.Summary) > MaxSummaryBytes {
		t.Fatalf("the summary must be kept within the total: %+v", reg.Summary)
	}
	dropped := strings.Count(adjusted(reg), "reason=over-total max=16384")
	if dropped == 0 || len(reg.Summary.StateNow)+len(reg.Summary.NextSteps)+dropped != 2*MaxSummaryItems {
		t.Fatalf("each drop is named once: kept %d+%d, named %d\n%s", len(reg.Summary.StateNow), len(reg.Summary.NextSteps), dropped, adjusted(reg))
	}
	if !strings.HasPrefix(adjusted(reg), AdjustmentLinePrefix+"dropped summary.state_now[19] reason=over-total") ||
		!strings.Contains(adjusted(reg), "dropped summary.next_steps[19] reason=over-total") {
		t.Fatalf("drops start at the lists' ends, state_now first on a tie:\n%s", adjusted(reg))
	}
	if half, _ := decode(t, summaryJSON(map[string]string{"state_now": jsonList(MaxSummaryItems, item)})); half.Summary == nil || adjusted(half) != "" {
		t.Fatalf("control: half of that total is stored as sent: %q", adjusted(half))
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

// TestARealCredentialIsNeverExplainedAwayAsAPlaceholder: the RAW text is scanned, and these were
// stored verbatim by a draft that masked every `<…>` before scanning. Each must be redacted. Every
// value is built at run time so no literal token sits in this file. The `$`-glued rows pin the other
// half: a lower-case `$word` is not a placeholder, and a `$UPPER` run that is itself a credential
// is a value. The kept-as-written controls are the placeholders proper.
func TestARealCredentialIsNeverExplainedAwayAsAPlaceholder(t *testing.T) {
	pw := "Xk9mQ2pL" + "r7vB"
	ghp := "gh" + "p_" + strings.Repeat("Ab3d", 9)
	npm := "np" + "m_" + strings.Repeat("Ab3d", 9)
	aiza := "AI" + "za" + strings.Repeat("Ab3dE", 7)
	akia := "AK" + "IA" + strings.Repeat("Q7ZX", 4)
	for text, rule := range map[string]string{
		"db is <postgres://app:" + pw + "@db.example.test:5432/app>": "url-userinfo-password",
		"use <redis://:" + pw + "@cache-0:6379>":                     "url-userinfo-password",
		"token <" + ghp + ">":                                        "github-token",
		"<" + npm + ">":                                              "npm-token",
		"<" + aiza + ">":                                             "google-api-key",
		"<" + akia + ">":                                             "aws-access-key-id",
		"https://app:$ecretPass9" + "x@db.example.test/app":          "url-userinfo-password",
		"https://app:$" + akia + "@db.example.test/app":              "",
		// A placeholder beside an UNRELATED credential explains nothing about it.
		"postgresql://app:${PGPASSWORD}@db/app and " + ghp: "github-token",
		// Deleting the placeholder would join the token to the next letter, past the rule's
		// boundary; it is replaced by a space instead.
		"key " + akia + "${SUFFIX}Q": "aws-access-key-id",
	} {
		reg, _ := decode(t, summaryJSON(map[string]string{"state_now": jsonList(1, text)}))
		got := adjusted(reg)
		if !strings.HasPrefix(got, AdjustmentLinePrefix+"redacted summary.state_now[0] rule=") ||
			(rule != "" && got != AdjustmentLinePrefix+"redacted summary.state_now[0] rule="+rule) ||
			!strings.HasPrefix(reg.Summary.StateNow[0], "[redacted: ") {
			t.Errorf("%q must be redacted (%s): adjustments %q, stored %q", text, rule, got, reg.Summary.StateNow[0])
		}
	}
	for _, keep := range []string{
		"postgresql://app:${PGPASSWORD}@db/app",
		"postgresql://app:$PGPASSWORD@db/app",
		"https://user:***@host.example.test/path",
		"Authorization: Bearer <token>",
	} {
		reg, _ := decode(t, summaryJSON(map[string]string{"state_now": jsonList(1, keep)}))
		if adjusted(reg) != "" || reg.Summary.StateNow[0] != keep {
			t.Errorf("control: %q must be kept as written: %q", keep, adjusted(reg))
		}
	}
}

// TestAPlaceholderBesideOrInsideARealValueExplainsNothing is round 4's ten rows: a placeholder-shaped
// run inside, before or after a REAL value. Replacing it by a space splits the value into pieces no
// rule's character class spans, so a space-only scan read the raw hit as explained and stored these
// verbatim; the last four also need the deleted scan, because the bearer/authorization classes
// cannot match through `$`, `{` or `*` at all. Each must be redacted. Values built at run time.
func TestAPlaceholderBesideOrInsideARealValueExplainsNothing(t *testing.T) {
	pw := "Xk9mQ2pL" + "r7vB"
	for _, text := range []string{
		"postgres://app:${X}" + pw + "@db",
		"postgres://app:" + pw + "${X}@db",
		"postgres://app:Xk9mQ2$PLr7vB@db",
		"postgres://app:hunter2***Xk9mQ@db",
		"postgres://app:Xk9m***@db",
		"redis://:$REDIS" + pw + "@r:6379",
		"Authorization: Bearer $X" + pw,
		"Authorization: Bearer ${X} " + pw,
		"Bearer *** " + pw,
		"x-api-key: ***" + pw,
	} {
		reg, _ := decode(t, summaryJSON(map[string]string{"state_now": jsonList(1, text)}))
		if !strings.HasPrefix(adjusted(reg), AdjustmentLinePrefix+"redacted summary.state_now[0] rule=") ||
			!strings.HasPrefix(reg.Summary.StateNow[0], "[redacted: ") {
			t.Errorf("%q must be redacted: adjustments %q, stored %q", text, adjusted(reg), reg.Summary.StateNow[0])
		}
	}
}

// TestEachReadingOfAGluedPlaceholderHasItsOwnRow: one row per reading `secretRule` scans, each built
// so that ONLY that reading catches it — so dropping any one reading turns exactly its row red. The
// AWS rule is the instrument because its trailing `\b` makes joins and splits observable. Values are
// built at run time.
func TestEachReadingOfAGluedPlaceholderHasItsOwnRow(t *testing.T) {
	akia := "AK" + "IA" + "Q7ZXQ7ZXQ7ZXQ7ZX"
	for name, text := range map[string]string{
		"sigils deleted, name kept":   "Bearer $QQQQ7abc",
		"whole run deleted (join)":    "key " + akia[:12] + "${X}" + akia[12:],
		"run replaced by a space":     "key " + akia + "${SUFFIX}Q",
		"a $NAME prefix deleted":      "key $X" + akia,
		"a stray sigil deleted":       "Bearer Q7IpSjk$0exuW00S",
		"a glued *** is no stand-in":  "x-api-key: ***Xk9mQ2pLr7vB",
		"a secret straddling the cut": strings.Repeat("x", 480) + " " + "gh" + "p_" + strings.Repeat("Ab3d", 9),
	} {
		reg, _ := decode(t, summaryJSON(map[string]string{"next_steps": jsonList(1, text)}))
		if !strings.HasPrefix(adjusted(reg), AdjustmentLinePrefix+"redacted summary.next_steps[0] rule=") {
			t.Errorf("%s: %q must be redacted, adjustments %q", name, text, adjusted(reg))
		}
	}
}

// TestEveryRuleHoldsAgainstPlaceholderNeighbours generates, for EVERY confident rule the scan acts on,
// a real-shaped value in its credential position, and then each placeholder BEFORE, INSIDE (three
// cut points) and AFTER it, with and without a separating space, plus values with a `$UPPER` run
// occurring naturally inside them (seeded). A variant is a LEAK when it is stored verbatim although
// the SAME text without the placeholder (the generator's own `without`, separators kept) is
// redacted: the placeholder then hid a credential the scan would otherwise have caught. The kept
// controls are the placeholder ALONE in each credential position. The counts are printed; the
// positive control is that the placeholder-free texts are redacted at least at a floor, so a
// generator that stopped producing secrets cannot pass.
func TestEveryRuleHoldsAgainstPlaceholderNeighbours(t *testing.T) {
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
	ran, rawHits, leaks, kept := 0, 0, 0, 0
	check := func(r rule, text, without string) {
		ran++
		if base, err := secretRule(without); err != nil || base == "" {
			return // without the placeholder this text is not caught either; nothing was hidden
		}
		rawHits++
		got, err := secretRule(text)
		if err != nil {
			t.Fatal(err)
		}
		if got == "" {
			leaks++
			if leaks <= 10 {
				t.Errorf("LEAK (%s): %q stored verbatim", r.name, text)
			}
		}
	}
	for seed := uint64(1); seed <= 4; seed++ {
		rng = rand.New(rand.NewPCG(289, seed))
		for _, r := range rules {
			for _, ph := range placeholders {
				for range 3 {
					v := r.value()
					type variant struct{ with, without string }
					variants := []variant{{ph + v, v}, {v + ph, v}, {ph + " " + v, " " + v}, {v + " " + ph, v + " "}}
					for _, k := range []int{1, len(v) / 2, len(v) - 1} {
						variants = append(variants, variant{v[:k] + ph + v[k:], v})
					}
					for _, vv := range variants {
						check(r, r.tmpl(vv.with), r.tmpl(vv.without))
					}
				}
				// The placeholder ALONE in the credential position is what it stands for: kept, when
				// the raw scan hits at all.
				alone := r.tmpl(ph)
				if firstConfident(scan, alone) != "" {
					if got, _ := secretRule(alone); got != "" {
						t.Errorf("control: the placeholder alone %q must be kept, got rule %s", alone, got)
					}
					kept++
				}
			}
			// `$UPPER` runs that occur naturally inside random values.
			for range 40 {
				v := r.value()
				k := 1 + rng.IntN(len(v)-1)
				run := "$" + pick(upper, 1+rng.IntN(4))
				check(r, r.tmpl(v[:k]+run+v[k:]), r.tmpl(v[:k]+run[1:]+v[k:]))
			}
		}
	}
	t.Logf("placeholder neighbours: %d texts generated over %d rules, %d whose placeholder-free twin is redacted, %d of those leaked; %d placeholder-alone controls kept",
		ran, len(rules), rawHits, leaks, kept)
	if leaks != 0 {
		t.Fatalf("%d of %d texts leaked", leaks, rawHits)
	}
	if rawHits < ran/2 || kept < 40 {
		t.Fatalf("POSITIVE CONTROL: only %d of %d placeholder-free twins were redacted and %d controls kept — the generator is not producing what it claims", rawHits, ran, kept)
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
	text := strings.Repeat(" ", 200) + strings.Repeat("x", 450)
	reg, _ := decode(t, summaryJSON(map[string]string{"next_steps": jsonList(1, text)}))
	want := AdjustmentLinePrefix + "sanitized summary.next_steps[0] replaced=200\n" +
		AdjustmentLinePrefix + "clipped summary.next_steps[0] bytes=1050 max=500 sanitized_bytes=650"
	if adjusted(reg) != want {
		t.Fatalf("adjustments:\n%s\nwant:\n%s", adjusted(reg), want)
	}
	plain, _ := decode(t, summaryJSON(map[string]string{"next_steps": jsonList(1, strings.Repeat("x", 650))}))
	if adjusted(plain) != AdjustmentLinePrefix+"clipped summary.next_steps[0] bytes=650 max=500" {
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

// TestAPlaceholderIsNotASecret: the placeholder shapes in a credential position are kept as written
// — `${NAME}`, an upper-case `$NAME`, `***` — while the SAME positions holding a real-shaped value
// are still redacted. Both halves, so a scan that stopped looking passes neither. The `<…>` rows are
// kept for a different reason: `<…>` is NOT a placeholder, and no confident rule matches through `<`,
// so they never produce a hit to explain.
func TestAPlaceholderIsNotASecret(t *testing.T) {
	for _, keep := range []string{
		"postgresql://app:${PGPASSWORD}@db:5432/app",
		"postgresql://app:$PGPASSWORD@db:5432/app",
		"redis://:<password>@cache:6379",
		"https://user:***@host.example.test/path",
		"Authorization: Bearer ${API_TOKEN}",
		"x-api-key: <your-key>",
	} {
		reg, _ := decode(t, summaryJSON(map[string]string{"state_now": jsonList(1, keep)}))
		if adjusted(reg) != "" || reg.Summary.StateNow[0] != keep {
			t.Fatalf("placeholder %q must be kept as written: %q", keep, adjusted(reg))
		}
	}
	for real, rule := range map[string]string{
		"postgresql://app:" + "s3cr3t-Value9" + "@db:5432/app": "url-userinfo-password",
		"Authorization: Bearer " + "abc12345":                  "authorization",
	} {
		reg, _ := decode(t, summaryJSON(map[string]string{"state_now": jsonList(1, real)}))
		if adjusted(reg) != AdjustmentLinePrefix+"redacted summary.state_now[0] rule="+rule {
			t.Fatalf("POSITIVE CONTROL: %q in the same position must still be redacted (%s): %q", real, rule, adjusted(reg))
		}
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
