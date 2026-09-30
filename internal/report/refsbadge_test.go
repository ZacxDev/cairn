package report

import (
	"strconv"
	"strings"
	"testing"
)

// TestTheLinkBadgeSaysREFSAndNotTASKS is a REGRESSION guard, not an invariant guard: the
// badge really did read `🔗 N task(s)` on this line until this change, while the key it
// counts has been `refs:` since `tasks:` folded into it. Measured red at `ba78dbb` with
// `🔗 1 task` / `🔗 2 tasks`, green at HEAD.
//
// 🔴 IT PINS BOTH CARDINALITIES, BECAUSE THE FIXTURE AND THE CORPUS PIN ONLY ONE EACH AND
// NEITHER PINS THE PAIR. `internal/report/testdata/reader_fixtures.json` carries a 2-ref
// entry, so a mutant that dropped `plural(...)` dies there; nothing in that fixture carries
// ONE ref, so a mutant that hardcoded `"refs"` — always plural — survives it. The corpus
// and `tests/parity/` each reach the singular through a live store, which makes them the
// reachability proof; this test is what makes the PAIR readable in one place.
//
// 🔴 IT PINS THE WHOLE NORMALISED LINE, NOT A SUBSTRING, AND THE DIFFERENCE IS MEASURED
// RATHER THAN STYLISTIC. The first version asserted `strings.Contains(line, "🔗 1 ref")` and
// a mutant that hardcoded the plural — `" refs"`, dropping `plural(...)` — SURVIVED it,
// because `🔗 1 refs` contains `🔗 1 ref`. That is the house rule's own failure mode: a guard
// spelled as a prefix is walkable by writing a longer word. The literals below are written
// out here rather than built from `ljustRunes`/`SensitivityLabel`, so a format lifted from
// the implementation cannot agree with it by construction.
func TestTheLinkBadgeSaysREFSAndNotTASKS(t *testing.T) {
	// `  <ref padded to 12>  <count right-aligned to 3> nuance   <sensitivity>   <badges>`
	const base = "  gadget-one      2 nuance   public"
	for _, tc := range []struct {
		name  string
		tasks []string
		badge string
	}{
		{"one ref is singular", []string{"github:example-org/example-repo#1"}, "🔗 1 ref"},
		{"two refs are plural", []string{
			"github:example-org/example-repo#1",
			"github:example-org/example-repo#2",
		}, "🔗 2 refs"},
		{"three refs are plural", []string{
			"github:example-org/example-repo#1",
			"github:example-org/example-repo#2",
			"github:example-org/example-repo#3",
		}, "🔗 3 refs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			line := listingLine(RecalledEntry{
				Ref:         "gadget-one",
				Sensitivity: "public",
				BulletCount: 2,
				Tasks:       tc.tasks,
			}, 12)
			if want := base + "   " + tc.badge; line != want {
				t.Errorf("index line\n  got  %q\n  want %q", line, want)
			}
			// 🔴 AND THE OLD WORD IS GONE, asserted as the whole badge segment for the same
			// reason the positive assertion is: the bare word `task` appears nowhere on this
			// row today, but `🔗 N task` is exactly what a revert re-introduces, and a
			// positive-only assertion passes against a renderer that emits BOTH.
			if old := "🔗 " + strconv.Itoa(len(tc.tasks)) + " task"; strings.Contains(line, old) {
				t.Errorf("index line still carries the retired badge %q:\n%s", old, line)
			}
		})
	}
}

// TestTheLinkBadgeIsCONDITIONAL is an INVARIANT guard — labelled as one, and not counted as
// regression coverage for the rename. It pins the property every badge on this row shares
// and that the whole feature's additivity rests on: an entry with no refs renders a row
// byte-identical to one from before the badge existed.
func TestTheLinkBadgeIsCONDITIONAL(t *testing.T) {
	entry := RecalledEntry{Ref: "gadget-one", Sensitivity: "public", BulletCount: 2}
	line := listingLine(entry, 12)
	if strings.Contains(line, "🔗") {
		t.Errorf("an entry with no refs rendered a link badge:\n%s", line)
	}
	entry.Tasks = []string{"github:example-org/example-repo#1"}
	withRef := listingLine(entry, 12)
	if withRef == line {
		t.Fatalf("the badge did not change the row at all, so the assertion above is "+
			"vacuous — this test cannot tell a conditional badge from no badge:\n%s", line)
	}
}
