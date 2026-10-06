package report

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/arcs"
	"github.com/ZacxDev/cairn/internal/store"
)

// 🔴 THE CONTRACT WITNESSES FOR THE ARC ORPHAN CHECK (S5). Go-only, so every expected body is
// spelled LITERALLY rather than built from the renderer's constants, and every exit is a NUMBER
// rather than `doctor.ExitProblem` — a test that compared against the constant would stay green
// if the constant moved, which is the one change Q5 forbids. Synthetic names throughout.

const (
	wantCheckVis      = "  visibility: only arcs whose HOME scope is readable to you are checked · a declared scope you cannot read is neither checked nor counted\n"
	wantCheckFindings = "  findings (exit 9): a home or declared scope readable to you that does not exist in the store · journal record(s) that could not be read\n"
	wantCheckNot      = "  not findings: a member session with no attributed write (members come from commits and transcripts, not entry trailers) and a writing session in no arc (arcs are registered only from now on) — both are coverage, below\n"
)

func runCheck(t *testing.T, root, scope string, all bool, visible store.ScopeSet, snap *arcs.Snapshot) (string, int) {
	t.Helper()
	rep, err := ArcsCheck(root, scope, all, visible, snap)
	if err != nil {
		t.Fatal(err)
	}
	return rep.RenderText(), rep.Exit()
}

// findingSnapshot is `arcsSnapshot` plus the three things a registry should not hold:
//   - ghost-trail, homed in alpha (readable), declares omega-notes — readable by grant, absent —
//     and zeta-notes — NOT readable and absent, so it must be neither reported nor counted;
//   - lost-home, homed in epsilon-notes — readable by grant, absent from the store;
//   - one complete journal line that did not parse.
func findingSnapshot() *arcs.Snapshot {
	snap := arcsSnapshot()
	for _, r := range []arcs.Registration{
		reg("alpha-notes", "ghost-trail", "closed", "check", []string{"alpha-notes", "omega-notes", "zeta-notes"},
			arcs.Member{Session: "s-0004", Role: "wrote"}),
		reg("epsilon-notes", "lost-home", "open", "none", []string{"epsilon-notes"},
			arcs.Member{Session: "s-0009", Role: "originated"}),
	} {
		snap.Latest[r.Key()] = r
	}
	snap.Skipped = 1
	return snap
}

var grantsAbsent = store.VisibleScopeSet([]string{"alpha-notes", "beta-notes", "gamma-notes", "epsilon-notes", "omega-notes"})

// TestTheArcsCheckIsCleanOverAHealthyRegistry is the 0: every checked arc's home and every
// READABLE declared scope exists. gadget-rollout also declares delta-notes, which this caller
// cannot read — omitted, not a finding; hidden-quest is homed in delta and is not checked at all.
// s-0003 is a member with no attributed write anywhere: COVERAGE, not a finding.
func TestTheArcsCheckIsCleanOverAHealthyRegistry(t *testing.T) {
	got, code := runCheck(t, arcsWorld(t), "alpha-notes", true, notDelta, arcsSnapshot())
	want := "cairn-arcs-check: status=arcs-check-clean scope=alpha-notes exit=0\n" +
		"  checked: every arc visible to you\n" +
		wantReads + wantAttribution + wantCheckVis + wantCheckFindings + wantCheckNot +
		"  arcs checked: 2 (open 1 · closed 0 · unknown 1)\n" +
		"  members: 2 of 3 member sessions wrote an attributed bullet in a scope readable to you (1 did not — coverage, not a finding)\n" +
		"  sessions: 1 of 2 writing sessions in the scopes readable to you belong to an arc visible to you (1 in no arc — coverage, not a finding)\n" +
		"  attributed: 3 of 5 bullets across the 3 scope(s) readable to you carry a write trailer\n" +
		"\n" +
		"NO FINDING: 2 arc(s) checked, and every home and declared scope readable to you exists. This says nothing about arcs homed in scopes you cannot read."
	if got != want || code != 0 {
		t.Fatalf("the clean check (exit %d):\n%s\n--- want (exit 0)\n%s", code, got, want)
	}
}

// TestTheArcsCheckReportsEveryFindingClass is the 9, all three classes in one answer, ordered.
func TestTheArcsCheckReportsEveryFindingClass(t *testing.T) {
	got, code := runCheck(t, arcsWorld(t), "alpha-notes", true, grantsAbsent, findingSnapshot())
	want := "cairn-arcs-check: status=arcs-check-findings scope=alpha-notes exit=9\n" +
		"  checked: every arc visible to you\n" +
		wantReads + wantAttribution + wantCheckVis + wantCheckFindings + wantCheckNot +
		"  arcs checked: 4 (open 2 · closed 1 · unknown 1)\n" +
		"  members: 3 of 5 member sessions wrote an attributed bullet in a scope readable to you (2 did not — coverage, not a finding)\n" +
		"  sessions: 2 of 2 writing sessions in the scopes readable to you belong to an arc visible to you (0 in no arc — coverage, not a finding)\n" +
		"  attributed: 3 of 5 bullets across the 3 scope(s) readable to you carry a write trailer\n" +
		"\n" +
		"findings: 3\n" +
		"- journal-damaged · the registration journal holds record(s) that could not be read; an arc registered only by such a record was NOT checked\n" +
		"- declared-scope-absent · alpha-notes/ghost-trail · it declares `omega-notes/`, which is readable to you and does not exist in the store\n" +
		"- home-scope-absent · epsilon-notes/lost-home · its home scope `epsilon-notes/` is readable to you and does not exist in the store"
	if got != want || code != 9 {
		t.Fatalf("the findings check (exit %d):\n%s\n--- want (exit 9)\n%s", code, got, want)
	}
}

// TestEachFindingClassAloneExits9 isolates each class, so no class's 9 is carried by another's —
// and the control (none of the three) is the 0 beside them.
func TestEachFindingClassAloneExits9(t *testing.T) {
	root := arcsWorld(t)
	cases := map[string]struct {
		mutate func(*arcs.Snapshot)
		want   string // the one finding line expected, or "" for the clean control
	}{
		"control": {func(*arcs.Snapshot) {}, ""},
		"skipped": {func(s *arcs.Snapshot) { s.Skipped = 2 }, "- journal-damaged · "},
		"torn":    {func(s *arcs.Snapshot) { s.TornTail = true }, "- journal-damaged · "},
		"home": {func(s *arcs.Snapshot) {
			r := reg("epsilon-notes", "lost-home", "open", "none", []string{"epsilon-notes"})
			s.Latest[r.Key()] = r
		}, "- home-scope-absent · epsilon-notes/lost-home · "},
		"declared": {func(s *arcs.Snapshot) {
			r := reg("gamma-notes", "ghost-trail", "open", "none", []string{"gamma-notes", "omega-notes"})
			s.Latest[r.Key()] = r
		}, "- declared-scope-absent · gamma-notes/ghost-trail · it declares `omega-notes/`"},
	}
	for name, c := range cases {
		snap := arcsSnapshot()
		c.mutate(snap)
		got, code := runCheck(t, root, "alpha-notes", true, grantsAbsent, snap)
		if c.want == "" {
			if code != 0 || !strings.Contains(got, "status=arcs-check-clean") {
				t.Fatalf("%s: the control must be clean at 0, got %d\n%s", name, code, got)
			}
			continue
		}
		if code != 9 || !strings.Contains(got, "status=arcs-check-findings") ||
			!strings.Contains(got, "\nfindings: 1\n"+c.want) {
			t.Fatalf("%s: want exactly one finding %q at exit 9, got %d\n%s", name, c.want, code, got)
		}
	}
}

// TestAnUnreadableDeclaredScopeIsNeitherReportedNorCounted is refused == absent for the check.
// zeta-notes is declared and absent; for a caller WITHOUT a grant on it the answer must be the
// answer for a registry that never declared it — the name appears nowhere. The POSITIVE CONTROL
// grants it, and the same absence becomes a finding; without that half this test would pass over a
// renderer that never reported declared scopes at all.
func TestAnUnreadableDeclaredScopeIsNeitherReportedNorCounted(t *testing.T) {
	root := arcsWorld(t)
	snap := arcsSnapshot()
	r := reg("alpha-notes", "ghost-trail", "open", "none", []string{"alpha-notes", "zeta-notes"})
	snap.Latest[r.Key()] = r

	hidden, code := runCheck(t, root, "alpha-notes", true, notDelta, snap)
	if code != 0 || strings.Contains(hidden, "zeta") {
		t.Fatalf("an unreadable declared scope must be invisible to the check (exit 0, name absent), got %d\n%s", code, hidden)
	}
	never := arcsSnapshot()
	r.DeclaredScopes = []string{"alpha-notes"}
	never.Latest[r.Key()] = r
	if body, _ := runCheck(t, root, "alpha-notes", true, notDelta, never); body != hidden {
		t.Fatalf("declaring an unreadable scope must answer exactly what not declaring it answers:\n%s\n--- never declared\n%s", hidden, body)
	}

	granted := store.VisibleScopeSet([]string{"alpha-notes", "beta-notes", "gamma-notes", "zeta-notes"})
	shown, code := runCheck(t, root, "alpha-notes", true, granted, snap)
	if code != 9 || !strings.Contains(shown, "- declared-scope-absent · alpha-notes/ghost-trail · it declares `zeta-notes/`") {
		t.Fatalf("POSITIVE CONTROL: with a grant on zeta-notes its absence is a finding, got %d\n%s", code, shown)
	}
}

// TestAnArcHomedInAnUnreadableScopeIsNotChecked: hidden-quest is homed in delta and declares an
// absent scope the caller CAN read. The caller cannot see the arc, so the check must not see it
// either — its slug, home and finding all absent. The control reads delta and gets the finding.
func TestAnArcHomedInAnUnreadableScopeIsNotChecked(t *testing.T) {
	root := arcsWorld(t)
	snap := arcsSnapshot()
	r := snap.Latest[arcs.Key{Home: "delta-notes", Slug: "hidden-quest"}]
	r.DeclaredScopes = []string{"delta-notes", "omega-notes"}
	snap.Latest[r.Key()] = r
	visible := store.VisibleScopeSet([]string{"alpha-notes", "beta-notes", "gamma-notes", "omega-notes"})
	got, code := runCheck(t, root, "alpha-notes", true, visible, snap)
	if code != 0 || strings.Contains(got, "hidden-quest") || strings.Contains(got, "delta") {
		t.Fatalf("an arc homed in an unreadable scope must not be checked or named, got %d\n%s", code, got)
	}
	withDelta := store.VisibleScopeSet([]string{"alpha-notes", "beta-notes", "gamma-notes", "delta-notes", "omega-notes"})
	got, code = runCheck(t, root, "alpha-notes", true, withDelta, snap)
	if code != 9 || !strings.Contains(got, "- declared-scope-absent · delta-notes/hidden-quest · it declares `omega-notes/`") {
		t.Fatalf("POSITIVE CONTROL: a caller who reads delta gets the finding, got %d\n%s", code, got)
	}
}

// TestTheScopedCheckChecksOnlyArcsHomedThere: without all_scopes the check covers arcs HOMED in
// the scope, so beta's check passes while a finding sits on an arc homed elsewhere; the sessions
// line is about beta alone. And the same registry checked at all_scopes is the 9 — the control.
func TestTheScopedCheckChecksOnlyArcsHomedThere(t *testing.T) {
	root := arcsWorld(t)
	got, code := runCheck(t, root, "beta-notes", false, grantsAbsent, findingSnapshotUndamaged())
	want := "cairn-arcs-check: status=arcs-check-clean scope=beta-notes exit=0\n" +
		"  checked: arcs homed in `beta-notes/`\n" +
		wantReads + wantAttribution + wantCheckVis + wantCheckFindings + wantCheckNot +
		"  arcs checked: 1 (open 0 · closed 0 · unknown 1)\n" +
		"  members: 1 of 1 member sessions wrote an attributed bullet in a scope readable to you (0 did not — coverage, not a finding)\n" +
		"  sessions: 1 of 1 writing sessions in `beta-notes/` belong to an arc visible to you (0 in no arc — coverage, not a finding)\n" +
		"  attributed: 3 of 5 bullets across the 3 scope(s) readable to you carry a write trailer\n" +
		"\n" +
		"NO FINDING: 1 arc(s) checked, and every home and declared scope readable to you exists. This says nothing about arcs homed in scopes you cannot read."
	if got != want || code != 0 {
		t.Fatalf("the scoped check (exit %d):\n%s\n--- want (exit 0)\n%s", code, got, want)
	}
	if _, code := runCheck(t, root, "beta-notes", true, grantsAbsent, findingSnapshotUndamaged()); code != 9 {
		t.Fatalf("CONTROL: the same registry at all_scopes must find the absent scopes, got %d", code)
	}
}

func findingSnapshotUndamaged() *arcs.Snapshot {
	s := findingSnapshot()
	s.Skipped = 0
	return s
}

// TestAnUnconfiguredJournalIsCouldNotLook is the 10: no journal, nothing checked — never a 0.
func TestAnUnconfiguredJournalIsCouldNotLook(t *testing.T) {
	got, code := runCheck(t, arcsWorld(t), "alpha-notes", false, notDelta, nil)
	want := "cairn-arcs-check: status=registrations-unconfigured scope=alpha-notes exit=10\n" +
		"  checked: arcs homed in `alpha-notes/`\n" +
		wantReads + wantAttribution + wantCheckVis + wantCheckFindings + wantCheckNot +
		"\n" +
		"REGISTRATIONS ARE NOT CONFIGURED ON THIS POD — it was started without -arc-journal / $CAIRN_ARC_JOURNAL, so no arc can be registered or shown. This is the designed off state, NOT 'no arc touched this scope'. Nothing was checked (exit 10: could not look)."
	if got != want || code != 10 {
		t.Fatalf("the unconfigured check (exit %d):\n%s\n--- want (exit 10)\n%s", code, got, want)
	}
}

// TestAStoreThatCannotBeReadIsAnErrorNotAnAnswer: a missing store root is the store's own error
// (the pod's 503, the client's 10) — never a clean report over nothing.
func TestAStoreThatCannotBeReadIsAnErrorNotAnAnswer(t *testing.T) {
	_, err := ArcsCheck(filepath.Join(t.TempDir(), "no-such-store"), "alpha-notes", true, notDelta, arcsSnapshot())
	var missing *store.StoreMissingError
	if !errors.As(err, &missing) {
		t.Fatalf("want *store.StoreMissingError, got %v", err)
	}
}

// TestArcsCheckExitIsDoctorsLegendAndNothingElse pins the ONE status→exit mapping to LITERAL
// numbers (Q5): 0 / 9 / 10, and a status that is not a check answer — a LISTING from a pod that
// ignored `?check=1` — is 10 and `ok == false`, never a 0.
func TestArcsCheckExitIsDoctorsLegendAndNothingElse(t *testing.T) {
	for status, want := range map[string]int{
		"arcs-check-clean": 0, "arcs-check-findings": 9, "registrations-unconfigured": 10,
	} {
		if got, ok := ArcsCheckExit(status); got != want || !ok {
			t.Fatalf("%s: want %d/true, got %d/%v", status, want, got, ok)
		}
	}
	for _, status := range []string{"arcs-listed", "no-arc-registered", "scope-absent", ""} {
		if got, ok := ArcsCheckExit(status); got != 10 || ok {
			t.Fatalf("%q is not a check answer: want 10/false, got %d/%v", status, got, ok)
		}
	}
}
