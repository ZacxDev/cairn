package arcs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

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

// TestEverySummaryCapRefusesAndNeverTruncates walks each cap with a value AT it (accepted) and
// one well PAST it (refused, naming the field). The past-cap values overshoot by more than one unit
// so an off-by-one in the comparison cannot be what the test measures.
func TestEverySummaryCapRefusesAndNeverTruncates(t *testing.T) {
	q := func(s string) string { return fmt.Sprintf("%q", s) }
	for _, tc := range []struct {
		field, at, past, want string
	}{
		{"closing_condition", q(strings.Repeat("c", MaxClosingConditionBytes)), q(strings.Repeat("c", MaxClosingConditionBytes+7)), "summary.closing_condition is 1007 bytes"},
		{"state_now", jsonList(MaxSummaryItems, "s"), jsonList(MaxSummaryItems+3, "s"), "at most 20 summary.state_now, got 23"},
		{"state_now", jsonList(1, strings.Repeat("s", MaxSummaryItemBytes)), jsonList(1, strings.Repeat("s", MaxSummaryItemBytes+9)), "summary.state_now[0] is 509 bytes"},
		{"next_steps", jsonList(MaxSummaryItems, "n"), jsonList(MaxSummaryItems+3, "n"), "at most 20 summary.next_steps, got 23"},
		{"next_steps", jsonList(1, strings.Repeat("n", MaxSummaryItemBytes)), jsonList(1, strings.Repeat("n", MaxSummaryItemBytes+9)), "summary.next_steps[0] is 509 bytes"},
		{"task_refs", jsonList(MaxTaskRefs, "t:1"), jsonList(MaxTaskRefs+3, "t:1"), "at most 32 summary.task_refs, got 35"},
		{"task_refs", jsonList(1, strings.Repeat("t", MaxTaskRefBytes)), jsonList(1, strings.Repeat("t", MaxTaskRefBytes+9)), "summary.task_refs[0] is 209 bytes"},
		{"aliases", jsonList(MaxAliases, "a"), jsonList(MaxAliases+3, "a"), "at most 16 summary.aliases, got 19"},
		{"aliases", jsonList(1, strings.Repeat("a", MaxAliasBytes)), jsonList(1, strings.Repeat("a", MaxAliasBytes+9)), "summary.aliases[0] is 73 bytes"},
	} {
		t.Run(tc.field, func(t *testing.T) {
			reg, _ := decode(t, summaryJSON(map[string]string{tc.field: tc.at}))
			if reg.Summary == nil || reg.Schema != SchemaSummary {
				t.Fatalf("a value at the cap must be accepted as schema 2: %+v", reg)
			}
			refused(t, summaryJSON(map[string]string{tc.field: tc.past}), tc.want)
		})
	}
	// The TOTAL cap binds on its own: every field within its own cap, the whole over 16 KiB.
	item := strings.Repeat("x", MaxSummaryItemBytes)
	refused(t, summaryJSON(map[string]string{"state_now": jsonList(MaxSummaryItems, item), "next_steps": jsonList(MaxSummaryItems, item)}),
		"summary is")
	if reg, _ := decode(t, summaryJSON(map[string]string{"state_now": jsonList(MaxSummaryItems, item)})); reg.Summary == nil {
		t.Fatal("control: half of that total is within the cap")
	}
	repo := `{"repo_path":"` + strings.Repeat("r", MaxRepoPathBytes+9) + `","commit":"` + testCommit + `","committed_at":"2000-01-04T00:00:00Z"}`
	refused(t, summaryJSON(map[string]string{"source": repo}), "summary.source.repo_path must be")
}

// TestEverySummaryStringIsOneCleanLine: blank, control characters (a newline would let the tool
// print a line that reads as the pod's own), line separators, invalid provenance, a confident
// secret, whitespace in a ref, an unknown key — each refused with its own sentence. The positive
// control is the default fixture, which decodes.
func TestEverySummaryStringIsOneCleanLine(t *testing.T) {
	if reg, _ := decode(t, summaryJSON(nil)); reg.Summary == nil || reg.Summary.Source.Commit != testCommit {
		t.Fatalf("POSITIVE CONTROL: the default summary decodes: %+v", reg.Summary)
	}
	// A credential-shaped value built at run time, so no literal token sits in this file.
	token := "gh" + "p_" + strings.Repeat("Ab3", 12)
	for _, tc := range []struct{ field, value, want string }{
		{"state_now", `["  "]`, "summary.state_now[0] must not be blank"},
		{"state_now", `["two\nlines"]`, "summary.state_now[0] must be one line"},
		{"next_steps", `["status: closed\u2028x"]`, "summary.next_steps[0] must be one line"},
		{"closing_condition", `"tab\there"`, "summary.closing_condition must be one line"},
		{"next_steps", `["rotate ` + token + `"]`, "summary.next_steps[0] looks like it carries a credential (rule github-token)"},
		{"task_refs", `["has space"]`, "summary.task_refs[0] must contain no whitespace"},
		{"aliases", `["___"]`, "summary.aliases[0] normalizes to nothing"},
		{"source", `{"repo_path":"/abs.md","commit":"` + testCommit + `","committed_at":"2000-01-04T00:00:00Z"}`, "repo_path must be"},
		{"source", `{"repo_path":"a/../b.md","commit":"` + testCommit + `","committed_at":"2000-01-04T00:00:00Z"}`, "repo_path must be"},
		{"source", `{"repo_path":"a.md","commit":"ABC","committed_at":"2000-01-04T00:00:00Z"}`, "summary.source.commit must be"},
		{"source", `{"repo_path":"a.md","commit":"` + testCommit + `","committed_at":"yesterday"}`, "committed_at must be RFC 3339"},
		{"source", `{"repo_path":"a.md","commit":"` + testCommit + `","committed_at":"2000-01-04T00:00:00Z","subject":"x"}`, "unknown field"},
		{"extra", `"x"`, "unknown field"},
	} {
		refused(t, summaryJSON(map[string]string{tc.field: tc.value}), tc.want)
	}
	// The refusal NEVER echoes the value it refused.
	_, _, err := DecodePayload([]byte(summaryJSON(map[string]string{"next_steps": `["rotate ` + token + `"]`})), "alpha-notes", "gadget-rollout")
	if err == nil || strings.Contains(err.Error(), token) {
		t.Fatalf("the secret refusal must not echo the value: %v", err)
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
// row's LAST ` · ` segment. One that folds to nothing is refused rather than stored empty.
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
	refused(t, summaryJSON(map[string]string{"aliases": `[" · "]`}), "summary.aliases[0] normalizes to nothing")
}

// TestAScannerConstructionErrorIsNotCached: the first construction fails, and that call's summary
// is refused with the error; the SECOND call constructs again, succeeds, and is the scanner used
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
	if _, _, err := DecodePayload([]byte(body), "alpha-notes", "gadget-rollout"); err == nil ||
		!strings.Contains(err.Error(), "synthetic construction failure") {
		t.Fatalf("the first call must surface the construction error, got %v", err)
	}
	if reg, _ := decode(t, body); reg.Summary == nil {
		t.Fatal("the second call must construct again and decode")
	}
	decode(t, body)
	if calls != 2 {
		t.Fatalf("constructions = %d, want 2 (one failure, one success, then cached)", calls)
	}
}
