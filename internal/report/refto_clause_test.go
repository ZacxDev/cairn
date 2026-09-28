package report

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/store"
)

// THE REF-TO HEADER'S SECOND CLAUSE — the claim about what is BELOW the line.
//
// The first clause is a pair of COUNTS and is guarded in `refto_test.go`. This file guards
// the second one, "and everything below is about those N", which is a claim about the BODY
// and was twice derived from something that is not the body.

// refToClauseWorld is ONE scope holding four entries, shaped so that every status the ref-to
// header can be rendered above is reachable over a single store and the assertions can tell
// "the body reports on a matched entry" from "it does not" by looking for ONE name.
//
// ⚠ ONLY `carrier.md` CARRIES THE REF, AND NO OTHER ENTRY'S NAME CONTAINS THAT WORD. That is
// what makes the body check below mechanical rather than a keyword guess: the matched set is
// exactly `{carrier}`, so `carrier` appearing under the header means the report is about a
// matched entry and its absence means it is not.
//
// ⚠ THE AMBIGUOUS ALIAS IS ON THE TWO ENTRIES THAT ARE **NOT** THE CARRIER. `ref-ambiguous`
// prints its candidates by FILENAME, so an alias shared with `carrier.md` would put `carrier`
// under the header for a reason that has nothing to do with the narrowed set — and the body
// check would then pass on the broken code.
func refToClauseWorld(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(name string, lines ...string) {
		dir := filepath.Join(root, "refto-clause")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("building the store: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(strings.Join(lines, "\n")), 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	write("carrier.md",
		"---", "service: carrier", "scope: refto-clause",
		"refs: ["+sharedRef+"]", "---", "",
		"## What it is", "", "The entry that carries the ref.", "",
		store.NuanceHeading, "", "- 2000-06-01 a bullet mentioning readiness", "")
	write("bystander.md",
		"---", "service: bystander", "scope: refto-clause", "---", "",
		"## What it is", "", "Holds no refs at all.", "")
	write("twin-a.md",
		"---", "service: twin-a", "scope: refto-clause", "aliases: [both-twins]", "---", "",
		"## What it is", "", "One of two entries sharing an alias.", "")
	write("twin-b.md",
		"---", "service: twin-b", "scope: refto-clause", "aliases: [both-twins]", "---", "",
		"## What it is", "", "The other entry sharing that alias.", "")
	return root
}

// The three WHOLE ref-to lines this fixture can produce, spelled out rather than rebuilt from
// the renderer's own pieces: a contract test's expectation may not be derived from the
// implementation it tests, and a guard on the clause's KEYWORDS is walkable by rewording the
// sentence around them. These bytes are also what `tests/parity/` and `tests/conformance/`
// compare against the oracle's.
const (
	clauseLineReports = "  ref-to: `" + sharedRef + "` — 1 of 4 entries in `refto-clause/` " +
		"reference it, and everything below is about those 1. This is a NARROWING, not a " +
		"truncation: the rest were read and did not match."
	clauseLineSilent = "  ref-to: `" + sharedRef + "` — 1 of 4 entries in `refto-clause/` " +
		"reference it, and NOTHING below is about them — the sentence below says why. This " +
		"is a NARROWING, not a truncation: the rest were read and did not match."
	clauseLineSilentZero = "  ref-to: `clickup:nothing-carries-this` — 0 of 4 entries in " +
		"`refto-clause/` reference it, and NOTHING below is about them — the sentence below " +
		"says why. This is a NARROWING, not a truncation: the rest were read and did not match."
)

// TestTheRefToClauseAgreesWithWhatTheBodyRenders guards the header's SECOND CLAUSE as a
// RELATIONSHIP — what the clause claims and what the body prints must agree, whatever the
// status is called — because a per-status assertion is the shape that got this wrong twice.
//
// 🔴 RED/GREEN MATRIX, MEASURED RATHER THAN CLAIMED. At `126d754`, where the clause was
// derived as `Status != StatusRefToAbsent`, three of the eight rows FAIL: `ref-absent`,
// `ref-ambiguous` and a `list`-mode page past the end each promised "everything below is
// about those 1" over a body that names no entry at all. The other five pass there, which is
// why the whole table is here rather than the three: the fix must not simply invert them.
//
// 🔴 AND A SECOND MATRIX, FOR THE NINTH ROW — `list` MODE ON A VALID PAGE. The table shipped
// without it and a mutant survived the whole suite in both languages: deleting
// `|| len(r.Listing) != 0` from `RendersNarrowedSet` left all eight rows passing, because no
// row emptied `Entries` while filling `Listing`. With the row present that mutant FAILS HERE —
// the clause renders the silent variant over a body that lists `carrier`, so this row's own
// line, body and predicate assertions all go red. The status ledger below could never have
// caught it: this row and the page-past-the-end row are both `recalled`.
//
// ⚠ `digest` MODE PAST THE END IS THE CONTROL, and it is the row that rules out the obvious
// second guess. `PageIsPastTheEnd()` is true on rows 7 AND 8; row 7 lists nothing while row 8
// still prints the featured body, so a predicate built out of page arithmetic gets one of
// them wrong. Only "did the renderer print a matched entry" separates the two.
func TestTheRefToClauseAgreesWithWhatTheBodyRenders(t *testing.T) {
	root := refToClauseWorld(t)
	base := func() RecallOptions {
		return RecallOptions{
			Scope: "refto-clause", Mode: DefaultMode, Limit: DefaultEntryLimit, Page: 1,
			RefTo: sharedRef, HasRefTo: true,
		}
	}
	withRef := func(ref string) RecallOptions {
		o := base()
		o.Ref, o.HasRef = ref, true
		return o
	}
	withPage := func(mode string, page int) RecallOptions {
		o := base()
		o.Mode, o.Page = mode, page
		return o
	}
	withRefTo := func(refTo string) RecallOptions {
		o := base()
		o.RefTo = refTo
		return o
	}

	cases := []struct {
		name string
		opts RecallOptions
		// wantStatus is asserted so a row that stopped reaching the shape it was written
		// for fails loudly instead of quietly re-testing a neighbour.
		wantStatus string
		wantLine   string
		// reportsOnMatched is the BEHAVIOURAL half: does anything under the header name an
		// entry from the matched set? It must equal what the line above claims.
		reportsOnMatched bool
	}{
		{"digest, filter only", base(), StatusRecalled, clauseLineReports, true},
		{"the named entry carries the ref", withRef("carrier"), StatusRecalled, clauseLineReports, true},
		{"the named entry does not carry it", withRef("bystander"), StatusRefToAbsent, clauseLineSilent, false},
		{"nothing in the scope carries it", withRefTo("clickup:nothing-carries-this"), StatusRefToAbsent, clauseLineSilentZero, false},
		{"the named entry does not exist", withRef("no-such-entry"), StatusRefAbsent, clauseLineSilent, false},
		{"the name is ambiguous", withRef("both-twins"), StatusRefAmbiguous, clauseLineSilent, false},
		{"list mode, a valid page", withPage("list", 1), StatusRecalled, clauseLineReports, true},
		{"list mode, page past the end", withPage("list", 9), StatusRecalled, clauseLineSilent, false},
		{"digest mode, page past the end", withPage("digest", 9), StatusRecalled, clauseLineReports, true},
	}

	// The SHAPE LEDGER's tally, filled in by the loop and asserted after it. See
	// `wantClauseShapes` for why the axis is this one and not the status.
	seen := map[string]bool{}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rep, err := Recall(root, tc.opts, store.Unrestricted())
			if err != nil {
				t.Fatalf("recall: %v", err)
			}
			if rep.Status != tc.wantStatus {
				t.Fatalf("status=%q, want %q — this row no longer reaches the shape it was "+
					"written for", rep.Status, tc.wantStatus)
			}
			text := rep.RenderText("synthetic-host", nil, "")
			if !strings.Contains(text, tc.wantLine) {
				t.Errorf("the ref-to line is not the one this shape must print."+
					"\nwant: %s\ngot:\n%s", tc.wantLine, text)
			}
			// The BODY, read the way the reader reads it: everything after the header line.
			cut := strings.Index(text, "  ref-to: `")
			if cut < 0 {
				t.Fatalf("no ref-to line was emitted at all:\n%s", text)
			}
			rest := text[cut:]
			rest = rest[strings.Index(rest, "\n")+1:]
			named := strings.Contains(rest, "carrier")
			if named != tc.reportsOnMatched {
				t.Errorf("the clause and the body disagree: the body %s the matched entry "+
					"(`carrier`) while the header claims it %s.\nbody:\n%s",
					clausePick(named, "NAMES", "does not name"),
					clausePick(tc.reportsOnMatched, "does", "does not"), rest)
			}
			// …and the two are the SAME claim, so the predicate the renderer consulted must
			// agree with what the body turned out to contain. This is the assertion that
			// stays meaningful if the sentence is ever reworded.
			if rep.RendersNarrowedSet() != tc.reportsOnMatched {
				t.Errorf("RendersNarrowedSet()=%v but the body %s a matched entry",
					rep.RendersNarrowedSet(), clausePick(named, "names", "does not name"))
			}
			seen[clauseShapeOf(rep)] = true
		})
	}

	// 🔴 THE LEDGER, ON THE AXIS THE PREDICATE IS ACTUALLY BUILT FROM. `RendersNarrowedSet`
	// reads two sets, so the table is exhaustive only when all FOUR combinations of their
	// emptiness are exercised — and a ledger over STATUSES cannot see that, because
	// list-mode-valid-page and list-mode-past-the-end are both `recalled`. It read as
	// coverage while the `Listing`-only shape was missing, and with it missing the mutant
	// that deletes `|| len(r.Listing) != 0` survived the whole suite in both languages.
	for _, shape := range wantClauseShapes {
		if !seen[shape] {
			t.Errorf("no row in the table produces the render shape %q, so the predicate's "+
				"behaviour on it is unmeasured. Add a row rather than deleting this line: "+
				"`RendersNarrowedSet` is a disjunction over two sets and a table that never "+
				"empties one of them cannot see that term at all.", shape)
		}
	}
	for shape := range seen {
		if !slices.Contains(wantClauseShapes, shape) {
			t.Errorf("a row produced the render shape %q, which this ledger does not list. "+
				"Either the shape set grew or a row stopped reaching what it was written "+
				"for — decide which.", shape)
		}
	}
}

// wantClauseShapes is every combination of "is `Entries` empty" × "is `Listing` empty", which
// is the axis `RendersNarrowedSet` is a disjunction over. All four are reachable under a
// ref-to line: `--ref` fills Entries only, `digest` fills both, `list` on a valid page fills
// Listing only, and the four silent shapes fill neither.
var wantClauseShapes = []string{
	"entries=filled listing=empty",
	"entries=filled listing=filled",
	"entries=empty listing=filled",
	"entries=empty listing=empty",
}

func clauseShapeOf(r RecallReport) string {
	return "entries=" + clausePick(len(r.Entries) != 0, "filled", "empty") +
		" listing=" + clausePick(len(r.Listing) != 0, "filled", "empty")
}

func clausePick(cond bool, yes, no string) string {
	if cond {
		return yes
	}
	return no
}

// TestNoRefToLineIsPrintedAboveAStatusTheFilterNeverReached is the other half of the table
// above — the statuses that CANNOT appear under a ref-to line, asserted behaviourally rather
// than declared in a list nobody re-derives.
//
// `scope-absent` and `scope-unreadable` both return before the filter runs, so `HasRefTo` is
// never set and no line is emitted: printing "narrowed to X" over "this scope does not exist"
// would suggest the narrowing is why nothing came back. `scope-empty` is unreachable after a
// filter that kept at least one entry, and a filter that kept none answers `ref-to-absent`.
// That leaves exactly the four statuses the table covers.
//
// ⚠ AN INVARIANT GUARD, LABELLED AS ONE: no bug ever violated this. It exists so the
// eight-row table above cannot silently stop being exhaustive.
func TestNoRefToLineIsPrintedAboveAStatusTheFilterNeverReached(t *testing.T) {
	root := refToClauseWorld(t)
	absent, err := Recall(root, RecallOptions{
		Scope: "never-indexed", Mode: DefaultMode, Limit: DefaultEntryLimit, Page: 1,
		RefTo: sharedRef, HasRefTo: true,
	}, store.Unrestricted())
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if absent.Status != StatusScopeAbsent {
		t.Fatalf("status=%q, want %q", absent.Status, StatusScopeAbsent)
	}
	if absent.HasRefTo || strings.Contains(absent.RenderText("synthetic-host", nil, ""), "ref-to: `") {
		t.Error("an absent scope printed a ref-to line: the filter never ran, so the line " +
			"would claim a narrowing is why nothing came back")
	}

	// `scope-unreadable`: a scope whose only file cannot be indexed, in its own directory so
	// it cannot perturb the world above.
	broken := filepath.Join(root, "all-broken")
	if err := os.MkdirAll(broken, 0o755); err != nil {
		t.Fatalf("building the broken scope: %v", err)
	}
	if err := os.WriteFile(filepath.Join(broken, "wrapped-aliases.md"),
		[]byte("---\nservice: widget\naliases: [one,\n  two]\n---\n"), 0o644); err != nil {
		t.Fatalf("writing the broken entry: %v", err)
	}
	unreadable, err := Recall(root, RecallOptions{
		Scope: "all-broken", Mode: DefaultMode, Limit: DefaultEntryLimit, Page: 1,
		RefTo: sharedRef, HasRefTo: true,
	}, store.Unrestricted())
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if unreadable.Status != StatusScopeUnreadable {
		t.Fatalf("status=%q, want %q", unreadable.Status, StatusScopeUnreadable)
	}
	if unreadable.HasRefTo || strings.Contains(unreadable.RenderText("synthetic-host", nil, ""), "ref-to: `") {
		t.Error("an unreadable scope printed a ref-to line: the filter never ran")
	}

	// A filter that matched nothing must NOT answer `scope-empty`, which claims the
	// DIRECTORY holds nothing — that is the collapse `StatusRefToAbsent`'s own header
	// refuses, and it is what would put the empty-scope wording under a ref-to line.
	none, err := Recall(root, RecallOptions{
		Scope: "refto-clause", Mode: DefaultMode, Limit: DefaultEntryLimit, Page: 1,
		RefTo: "clickup:nothing-carries-this", HasRefTo: true,
	}, store.Unrestricted())
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if none.Status != StatusRefToAbsent {
		t.Errorf("a filter that matched nothing answered %q, want %q", none.Status, StatusRefToAbsent)
	}
}
