package report

import (
	"slices"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/arcs"
)

// 🔴 EVERY ARC READER IN THIS PACKAGE ANSWERS THE SAME VISIBILITY QUESTION — the schema-2 opt-in
// as a RELATIONSHIP across all five, over `sessionAcrossStore`'s world (north and south readable,
// east NOT). Two arcs homed in the hidden east scope, both declaring the readable south scope, both
// naming `s-roam-01`; one opted in, its twin did not. Each reader must show the opted-in arc and
// hide the twin. A reader that kept the home-only rule while the rest learned the opt-in fails its
// own row; one that ignored the flag fails the twin's.
func optInSnapshot() *arcs.Snapshot {
	reg := func(slug string, optIn bool) arcs.Registration {
		r := arcs.Registration{Schema: arcs.Schema, Home: "east-notes", Slug: slug, Status: arcs.StatusOpen,
			ClosingKind: arcs.ClosingCheck, DeclaredScopes: []string{"east-notes", "south-notes"},
			Members:      []arcs.Member{{Session: "s-roam-01", Role: "wrote"}},
			RegisteredBy: "fixture-registrar", RegisteredAt: "2000-01-06T07:08:09Z"}
		if optIn {
			r.Schema, r.VisibleToDeclared = arcs.SchemaSummary, true
		}
		return r
	}
	snap := arcs.Snapshot{Latest: map[arcs.Key]arcs.Registration{}}
	for _, r := range []arcs.Registration{reg("opted-arc", true), reg("twin-arc", false)} {
		snap.Latest[r.Key()] = r
	}
	return &snap
}

func TestEveryArcReaderHonoursTheOptInAndOnlyTheOptIn(t *testing.T) {
	root := sessionAcrossStore(t)
	snap := optInSnapshot()
	seen := map[string]map[string]bool{}
	note := func(reader, slug string) {
		if seen[reader] == nil {
			seen[reader] = map[string]bool{}
		}
		seen[reader][slug] = true
	}

	listing, err := Arcs(root, "south-notes", northSouth, snap)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range listing.Arcs {
		note("Arcs", a.Slug)
	}
	for _, slug := range []string{"opted-arc", "twin-arc"} {
		shown, err := Arc(root, "east-notes", slug, northSouth, snap)
		if err != nil {
			t.Fatal(err)
		}
		if shown.Status == StatusArcFound {
			note("Arc", slug)
		}
	}
	across, err := ArcsAcross(root, northSouth, snap)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range across.Arcs {
		note("ArcsAcross", a.Slug)
	}
	check, err := ArcsCheck(root, "south-notes", true, northSouth, snap)
	if err != nil {
		t.Fatal(err)
	}
	// The check names no arc it found clean, so its witness is the COUNT: the home-only rule
	// checks 0 arcs, a rule ignoring the flag checks 2, and only the opt-in rule checks 1.
	switch check.Checked {
	case 1:
		note("ArcsCheck", "opted-arc")
	case 2:
		note("ArcsCheck", "opted-arc")
		note("ArcsCheck", "twin-arc")
	}
	// 🔴 AND THE CHECK JUDGES ONLY A HOME THE CALLER CAN READ: the opted-in arc's home (east) is
	// not in the narrowed index, which a home-existence test would read as `home-scope-absent` — a
	// false finding claiming a grant the caller does not hold. Clean, exit 0, the exception line said.
	if len(check.Findings) != 0 || check.Status != StatusArcsCheckClean ||
		!strings.Contains(check.RenderText(), "\n  "+ArcsCheckOptInLine+"\n") {
		t.Errorf("the check over an opted-in arc with an unreadable home: status %s, findings %+v\n%s",
			check.Status, check.Findings, check.RenderText())
	}
	sessions, err := SessionsAcross(root, northSouth, snap)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sessions.Sessions {
		for _, a := range s.Arcs {
			note("SessionsAcross", a.Slug)
		}
	}

	for _, reader := range []string{"Arcs", "Arc", "ArcsAcross", "ArcsCheck", "SessionsAcross"} {
		if got := seen[reader]; len(got) != 1 || !got["opted-arc"] {
			t.Errorf("%s saw %v — want exactly the opted-in arc", reader, got)
		}
	}
}

// TestTheAliasesSegmentIsAlwaysTheRowsLastSegment: a listing row is ` · `-separated and, when an
// arc has aliases, the LAST segment is `aliases <a>, <b>` — with and without ` · declared-visible`
// before it. A consumer splits on the last ` · ` to read them, which is only sound because aliases
// are stored in the slug charset (`internal/arcs`' hostile-alias test) and nothing renders after them.
func TestTheAliasesSegmentIsAlwaysTheRowsLastSegment(t *testing.T) {
	for _, optIn := range []bool{false, true} {
		rep := ArcsReport{Status: StatusArcsListed, Scope: "south-notes", Arcs: []ArcLine{{
			Home: "east-notes", Slug: "kettle-arc", Status: arcs.StatusOpen, ClosingKind: arcs.ClosingCheck,
			Members: 2, Declared: true, OptIn: optIn, Aliases: []string{"boil-move", "http-3"}}}}
		var row string
		for _, line := range strings.Split(rep.RenderText(), "\n") {
			if strings.HasPrefix(line, "- east-notes/kettle-arc ") {
				row = line
			}
		}
		segments := strings.Split(row, " · ")
		if got := segments[len(segments)-1]; got != "aliases boil-move, http-3" {
			t.Fatalf("optIn=%v: the last segment is %q, want the aliases segment\n%s", optIn, got, row)
		}
		if hasOptIn := slices.Contains(segments, "declared-visible"); hasOptIn != optIn {
			t.Fatalf("optIn=%v: declared-visible present=%v\n%s", optIn, hasOptIn, row)
		}
		if optIn && segments[len(segments)-2] != "declared-visible" {
			t.Fatalf("declared-visible must sit immediately before the aliases segment:\n%s", row)
		}
	}
}

// TestSummaryItemsArePrintedVerbatim: the producer owns numbering. Items it ranked itself — gaps
// included — print as sent, each indented under its heading, never re-numbered (`1. 1. a`) or
// re-bulleted. The same holds for `state now`.
func TestSummaryItemsArePrintedVerbatim(t *testing.T) {
	rep := ArcReport{Status: StatusArcFound, Reg: arcs.Registration{Summary: &arcs.Summary{
		StateNow: []string{"- moved", "plain"}, NextSteps: []string{"1. a", "3. b"},
		Source:   arcs.Source{RepoPath: "docs/h.md", Commit: strings.Repeat("0", 40), CommittedAt: "2000-01-04T00:00:00Z"},
		TaskRefs: []string{}, Aliases: []string{}}}}
	got := strings.Join(rep.SummaryLines(), "\n")
	want := "state now: 2\n  - moved\n  plain\nnext steps (ranked): 2\n  1. a\n  3. b\n"
	if !strings.Contains(got, want) || strings.Contains(got, "1. 1.") {
		t.Fatalf("items must print verbatim:\n%s", got)
	}
}

// TestArcShowPrintsTheSummaryBeforeTheMemberList pins the BODY ORDER as whole literal lines: the
// summary block sits between the `member writes:` line and the `members:` heading, so a reader that
// keeps only a byte-budgeted prefix reaches the ranked steps before an unbounded member list. The
// control is a schema-1 arc (no summary), whose `member writes:` line is followed directly by the
// heading, exactly as before the summary moved.
func TestArcShowPrintsTheSummaryBeforeTheMemberList(t *testing.T) {
	reg := arcs.Registration{Schema: arcs.SchemaSummary, Home: "alpha-notes", Slug: "gadget-move", Status: arcs.StatusOpen,
		ClosingKind: arcs.ClosingCheck, DeclaredScopes: []string{"alpha-notes"},
		Members: []arcs.Member{{Session: "s-0001", Role: "originated"}, {Session: "s-0002", Role: "wrote"}},
		Summary: &arcs.Summary{StateNow: []string{"the port moved"}, NextSteps: []string{"1. drain", "2. close"},
			Source:   arcs.Source{RepoPath: "docs/h.md", Commit: strings.Repeat("0", 40), CommittedAt: "2000-01-04T00:00:00Z"},
			TaskRefs: []string{}, Aliases: []string{}}}
	body := ArcReport{Status: StatusArcFound, Reg: reg}.RenderText()
	block := "\n\nsummary: the registering tool's own word, read from docs/h.md at commit " + strings.Repeat("0", 40) +
		" (committed 2000-01-04T00:00:00Z)\nstate now: 1\n  the port moved\nnext steps (ranked): 2\n  1. drain\n  2. close\n" +
		"task refs: none\naliases: none\n\nmembers: 2, ordered by session id (byte-wise)\n- s-0001 · originated"
	if !strings.Contains(body, block) || !strings.HasSuffix(body, "\n- s-0002 · wrote · first seen unknown · wrote in: none readable to you") {
		t.Fatalf("the summary must sit before the member list, and the member list end the body:\n%s", body)
	}
	if i, j := strings.Index(body, "\nmember writes:"), strings.Index(body, "\nsummary: "); i < 0 || j < i {
		t.Fatalf("the summary must follow the member-writes line:\n%s", body)
	}
	reg.Summary, reg.Schema = nil, arcs.Schema
	plain := ArcReport{Status: StatusArcFound, Reg: reg}.RenderText()
	if strings.Contains(plain, "summary: ") || !strings.Contains(plain, "\nmembers: 2, ordered by session id (byte-wise)\n- s-0001") ||
		strings.Contains(plain, "\n\nmembers: ") {
		t.Fatalf("control: a schema-1 arc's member-writes line runs straight into the heading:\n%s", plain)
	}
}
