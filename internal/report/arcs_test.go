package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/arcs"
	"github.com/ZacxDev/cairn/internal/store"
)

// 🔴 THE CONTRACT WITNESSES FOR THE ARC ANSWERS. These are Go-only (decision 3), so there is no
// oracle to record against: every expected body below is spelled LITERALLY, never built from the
// constants the renderer uses, so a reworded line is a red test (a guard on words is walkable by
// rewording, and these answers are prose). All names, ids and dates are synthetic.

// The fixed lines, spelled again BY HAND.
const (
	wantReads       = "  coverage: writes measured from entry trailers · reads NOT recorded (not collected in this phase)\n"
	wantAttribution = "  attribution: trailers are self-reported — actor as written in the entry (only appended bullets had it set by the pod); session ids are declared by the writer\n"
	wantArcsVis     = "  visibility: an arc is listed only when its HOME scope is readable to you — one homed in a scope you cannot read is NOT listed, even if it touched this one\n"
	wantArcVis      = "  visibility: an arc is shown only when its HOME scope is readable to you — one homed in a scope you cannot read answers exactly like one never registered\n"
	wantProvenance  = "  provenance: declared = the registration names this scope · inferred: a member session wrote here; the arc did not declare this scope\n"
	wantRegLine     = "  registration: arcs are pushed by the operator tooling at each handoff and are its own word — an arc nobody registered is not listed, and each is only as fresh as its last push\n"
)

// arcsWorld is four scopes. s-0001 wrote in alpha, beta AND delta; s-0004 only in alpha; gamma
// holds one unsigned bullet. A test caller sees alpha, beta and gamma — NOT delta.
func arcsWorld(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	put := func(scope, service, nuance string) {
		dir := filepath.Join(root, scope)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := "---\nservice: " + service + "\nscope: " + scope + "\n---\n\n## What it is\nsynthetic.\n\n" +
			"## Nuance / work-history\n" + nuance
		if err := os.WriteFile(filepath.Join(dir, service+".md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put("alpha-notes", "gadget-one", "- 2000-01-03: one [cairn: kappa-bot/s-0001]\n- 2000-01-02: two [cairn: mu-bot/s-0004]\n- untrailered\n")
	put("beta-notes", "widget-two", "- 2000-01-04: three [cairn: kappa-bot/s-0001]\n")
	put("gamma-notes", "doodad-three", "- 2000-01-05: unsigned\n")
	put("delta-notes", "sprocket-four", "- 2000-01-06: hidden [cairn: kappa-bot/s-0001]\n")
	return root
}

func reg(home, slug, status, closing string, declared []string, members ...arcs.Member) arcs.Registration {
	return arcs.Registration{Schema: 1, Home: home, Slug: slug, Status: status, ClosingKind: closing,
		DeclaredScopes: declared, WritersMeasured: true, CommitsTotal: 7, CommitsUnstamped: 2,
		ReportedAt: "2000-01-05T00:00:00Z", Members: members, RegisteredBy: "wide-reader",
		RegisteredAt: "2000-01-06T07:08:09Z"}
}

// arcsSnapshot: A1 homed in alpha DECLARES alpha+beta; A2 homed in beta declares only beta but
// its member s-0001 WROTE in alpha (inferred there); A3 homed in delta — unreadable to the test
// caller — declares alpha.
func arcsSnapshot() *arcs.Snapshot {
	snap := &arcs.Snapshot{Latest: map[arcs.Key]arcs.Registration{}}
	for _, r := range []arcs.Registration{
		// It also declares delta-notes, which the test caller cannot read: the declared-scope
		// narrowing must omit it, and without a hidden declared scope that narrowing is unreachable.
		reg("alpha-notes", "gadget-rollout", "open", "check", []string{"alpha-notes", "beta-notes", "delta-notes"},
			arcs.Member{Session: "s-0003", Role: "resumed", Carried: true},
			arcs.Member{Session: "s-0001", Role: "originated", FirstSeen: "2000-01-01T00:00:00Z"}),
		reg("beta-notes", "widget-fix", "unknown", "none", []string{"beta-notes"},
			arcs.Member{Session: "s-0001", Role: "wrote"}),
		reg("delta-notes", "hidden-quest", "closed", "judgement", []string{"alpha-notes", "delta-notes"},
			arcs.Member{Session: "s-0001", Role: "wrote"}),
	} {
		snap.Latest[r.Key()] = r
	}
	return snap
}

var notDelta = store.VisibleScopeSet([]string{"alpha-notes", "beta-notes", "gamma-notes"})

func renderArcs(t *testing.T, root, scope string, visible store.ScopeSet, snap *arcs.Snapshot) string {
	t.Helper()
	rep, err := Arcs(root, scope, visible, snap)
	if err != nil {
		t.Fatal(err)
	}
	return rep.RenderText()
}

func renderArc(t *testing.T, root, home, slug string, visible store.ScopeSet, snap *arcs.Snapshot) string {
	t.Helper()
	rep, err := Arc(root, home, slug, visible, snap)
	if err != nil {
		t.Fatal(err)
	}
	return rep.RenderText()
}

// TestTheArcsAnswerLabelsDeclaredAndInferred is Q3's whole contract over one scope: A1 DECLARES
// alpha, A2 is listed because its member wrote here and is labelled INFERRED with the session that
// did, and A3 — homed in a scope this caller cannot read — is absent.
func TestTheArcsAnswerLabelsDeclaredAndInferred(t *testing.T) {
	got := renderArcs(t, arcsWorld(t), "alpha-notes", notDelta, arcsSnapshot())
	want := "cairn-arcs: status=arcs-listed scope=alpha-notes\n" +
		wantReads + wantAttribution + wantArcsVis + wantProvenance + wantRegLine +
		"  attributed: 2 of 3 bullets in `alpha-notes/` carry a write trailer (1 have none — an arc whose sessions wrote only those is NOT inferred)\n" +
		"  sessions: 1 of 2 writing sessions here belong to an arc listed below\n" +
		"  statuses: open 1 · closed 0 · unknown 1 (unknown is the registering tool's lack of a verdict and is never counted as open)\n" +
		"\n" +
		"arcs: 2, ordered by home scope then slug\n" +
		"- alpha-notes/gadget-rollout · declared · status open · closing check · 2 members\n" +
		"- beta-notes/widget-fix · inferred (s-0001 wrote here) · status unknown · closing none · 1 member"
	if got != want {
		t.Fatalf("the arcs answer:\n%s\n--- want\n%s", got, want)
	}
}

// TestAnArcHomedInAnUnreadableScopeLeaksNothing is Q1: the arc homed in `delta-notes` DECLARES
// alpha, and a caller who cannot read delta must not see its home, its slug, or that it exists.
// The POSITIVE CONTROL is the same request by a caller who CAN read delta — the arc must be listed
// there, or the absence below would prove nothing about the visibility rule.
func TestAnArcHomedInAnUnreadableScopeLeaksNothing(t *testing.T) {
	root := arcsWorld(t)
	withDelta := store.VisibleScopeSet([]string{"alpha-notes", "beta-notes", "gamma-notes", "delta-notes"})
	control := renderArcs(t, root, "alpha-notes", withDelta, arcsSnapshot())
	if !strings.Contains(control, "\n- delta-notes/hidden-quest · declared · status closed · closing judgement · 1 member") {
		t.Fatalf("control: a caller who reads delta-notes sees the arc it homes:\n%s", control)
	}
	for _, body := range []string{
		renderArcs(t, root, "alpha-notes", notDelta, arcsSnapshot()),
		renderArc(t, root, "delta-notes", "hidden-quest", notDelta, arcsSnapshot()),
	} {
		for _, leak := range []string{"delta-notes", "hidden-quest", "judgement"} {
			if strings.Contains(body, leak) {
				t.Fatalf("an arc homed in an unreadable scope leaked %q:\n%s", leak, body)
			}
		}
	}
	// …and the single-arc answer for it is BYTE-IDENTICAL to a key nobody ever registered.
	hidden := renderArc(t, root, "delta-notes", "hidden-quest", notDelta, arcsSnapshot())
	never := renderArc(t, root, "alpha-notes", "never-filed", notDelta, arcsSnapshot())
	if hidden != never {
		t.Fatalf("refused differs from absent:\n%s\n---\n%s", hidden, never)
	}
}

// TestAnUnknownStatusIsNeverShownOrCountedAsOpen is Q4 at the renderer. The scope's only arc is
// `unknown`: the counts line must put it under `unknown` with `open 0`, and the arc's own line must
// not contain the word `open` at all. The CONTROL is the same arc registered `open`, which must
// flip both.
func TestAnUnknownStatusIsNeverShownOrCountedAsOpen(t *testing.T) {
	root := arcsWorld(t)
	only := func(status string) *arcs.Snapshot {
		r := reg("gamma-notes", "doodad-plan", status, "none", []string{"gamma-notes"})
		return &arcs.Snapshot{Latest: map[arcs.Key]arcs.Registration{r.Key(): r}}
	}
	got := renderArcs(t, root, "gamma-notes", notDelta, only("unknown"))
	line := got[strings.LastIndex(got, "\n- ")+1:]
	if line != "- gamma-notes/doodad-plan · declared · status unknown · closing none · 0 members" ||
		!strings.Contains(got, "  statuses: open 0 · closed 0 · unknown 1 (") || strings.Contains(line, "open") {
		t.Fatalf("an unknown arc read as open somewhere:\n%s", got)
	}
	control := renderArcs(t, root, "gamma-notes", notDelta, only("open"))
	if !strings.Contains(control, "  statuses: open 1 · closed 0 · unknown 0 (") ||
		!strings.Contains(control, "· status open ·") {
		t.Fatalf("control: an open arc is counted and shown as open:\n%s", control)
	}
	shown := renderArc(t, root, "gamma-notes", "doodad-plan", notDelta, only("unknown"))
	if !strings.Contains(shown, "\nstatus: unknown\n  (the registering tool reported no verdict; this arc is not counted with any other status)\n") {
		t.Fatalf("arc-show must print unknown as unknown:\n%s", shown)
	}
}

// TestEveryEmptyStatusSaysWhichEmptinessItIs: unconfigured, absent and nothing-registered are
// three mechanisms and three bodies.
func TestEveryEmptyStatusSaysWhichEmptinessItIs(t *testing.T) {
	root := arcsWorld(t)
	unconfigured := "\nREGISTRATIONS ARE NOT CONFIGURED ON THIS POD — it was started without -arc-journal / $CAIRN_ARC_JOURNAL, so no arc can be registered or shown. This is the designed off state, NOT 'no arc touched this scope'."
	if got := renderArcs(t, root, "alpha-notes", notDelta, nil); got !=
		"cairn-arcs: status=registrations-unconfigured scope=alpha-notes\n"+
			wantReads+wantAttribution+wantArcsVis+wantProvenance+wantRegLine+unconfigured {
		t.Fatalf("unconfigured arcs:\n%s", got)
	}
	if got := renderArc(t, root, "alpha-notes", "gadget-rollout", notDelta, nil); got !=
		"cairn-arc: status=registrations-unconfigured\n"+wantReads+wantAttribution+wantArcVis+wantRegLine+unconfigured {
		t.Fatalf("unconfigured arc:\n%s", got)
	}
	// delta-notes EXISTS and is refused; ghost-void never existed. One answer.
	refusedBody := renderArcs(t, root, "delta-notes", notDelta, arcsSnapshot())
	absent := renderArcs(t, root, "ghost-void", notDelta, arcsSnapshot())
	if strings.ReplaceAll(refusedBody, "delta-notes", "<S>") != strings.ReplaceAll(absent, "ghost-void", "<S>") ||
		!strings.HasSuffix(absent, "\nNO SCOPE `ghost-void/` IS READABLE HERE. A scope that does not exist and one this credential may not read answer identically, by design; no arc is listed for it.") {
		t.Fatalf("refused/absent arcs:\n%s\n---\n%s", refusedBody, absent)
	}
	none := renderArcs(t, root, "gamma-notes", notDelta, &arcs.Snapshot{Latest: map[arcs.Key]arcs.Registration{}})
	if !strings.HasPrefix(none, "cairn-arcs: status=no-arc-registered scope=gamma-notes\n") ||
		!strings.Contains(none, "  sessions: 0 of 0 writing sessions here belong to an arc listed below\n") ||
		!strings.HasSuffix(none, "\nNO REGISTERED ARC TOUCHED `gamma-notes/` THAT YOU CAN SEE — none declares it, and no member session of a visible arc wrote an attributed bullet here. Unregistered work, unattributed bullets and arcs homed in scopes you cannot read are all invisible to this answer.") {
		t.Fatalf("no arc registered:\n%s", none)
	}
}

// TestTheArcShowAnswer is the single-arc contract, literally: members ordered by session id, each
// member's writes NARROWED (s-0001 also wrote in delta-notes, which is not named), the carried
// member marked, and the tooling's own coverage stated.
func TestTheArcShowAnswer(t *testing.T) {
	got := renderArc(t, arcsWorld(t), "alpha-notes", "gadget-rollout", notDelta, arcsSnapshot())
	want := "cairn-arc: status=arc-found home=alpha-notes slug=gadget-rollout\n" +
		wantReads + wantAttribution + wantArcVis + wantRegLine +
		"\n" +
		"status: open\n" +
		"closing condition: check\n" +
		"registered: 2000-01-06T07:08:09Z by wide-reader · reported by the tool at 2000-01-05T00:00:00Z\n" +
		"tooling coverage: 2 of 7 commits carry no session id · writers: measured · readers: NOT measured\n" +
		"  1 member(s) carried from an earlier registration: the latest push did not measure their leg, and an unmeasured leg never erases a measured one\n" +
		"declared scopes readable to you: alpha-notes, beta-notes (a declared scope you cannot read is omitted, not counted)\n" +
		"member writes: inferred from 3 of 5 bullets carrying a write trailer, across the 3 scope(s) readable to you\n" +
		"members: 2, ordered by session id (byte-wise)\n" +
		"- s-0001 · originated · first seen 2000-01-01T00:00:00Z · wrote in: alpha-notes, beta-notes\n" +
		"- s-0003 · resumed · first seen unknown · wrote in: none readable to you · carried"
	if got != want {
		t.Fatalf("the arc answer:\n%s\n--- want\n%s", got, want)
	}
}

// TestADamagedJournalIsSaidWithoutACount: the warning is a fact, never a number — a count of
// unreadable records is a count over every caller's arcs.
func TestADamagedJournalIsSaidWithoutACount(t *testing.T) {
	snap := arcsSnapshot()
	snap.Skipped, snap.TornTail = 3, true
	got := renderArcs(t, arcsWorld(t), "alpha-notes", notDelta, snap)
	if !strings.Contains(got, "\n  ⚠ the registration journal holds record(s) that could not be read; an arc registered only by such a record is NOT shown\n") ||
		strings.Contains(got, "skipped") || strings.Contains(got, "torn") {
		t.Fatalf("damaged journal:\n%s", got)
	}
	clean := renderArcs(t, arcsWorld(t), "alpha-notes", notDelta, arcsSnapshot())
	if strings.Contains(clean, "⚠") {
		t.Fatalf("control: an undamaged journal prints no warning:\n%s", clean)
	}
}
