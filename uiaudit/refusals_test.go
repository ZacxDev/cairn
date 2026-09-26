package main

import (
	"strings"
	"testing"
)

// cleanWalk is one capture per declared viewport, all three properties satisfied.
//
// It is built from `Viewports` rather than from a literal list so that adding a sixth
// width cannot leave this control measuring five.
func cleanWalk() []*Capture {
	var out []*Capture
	for _, vp := range Viewports {
		out = append(out, &Capture{
			Target:   Target{Path: "/", PushURL: "/", LedgerRow: "GET / content"},
			Viewport: vp,
			AxeJSON:  []byte(`{"testEngine":{"name":"axe-core","version":"4.x"},"violations":[]}`),
			Layout:   &PushLayout{InnerWidth: vp.Width, ScrollWidth: vp.Width},
		})
	}
	return out
}

// TestTheWalkRefusalsCanEachGoRED is the NEGATIVE CONTROL on the three properties this
// change turned from printed numbers into refusals.
//
// 🔴 A GATE THAT HAS NEVER BEEN WATCHED TO FAIL IS A GATE NOBODY HOLDS, AND THIS ONE HAS A
// SPECIFIC REASON TO BE SUSPECTED: all three of its inputs were ALREADY being collected and
// printed before it existed. `printSignalSummary` has reported `pages with horizontal
// overflow=N` since this harness's first commit; nothing read it, so a responsive regression
// was a digit in a log beside an exit 0. Turning a number into a refusal is worth exactly as
// much as the proof that the refusal fires.
//
// Each case below breaks ONE property and requires the error to name THAT one — not merely
// to be an error. A mutant that dies for a different guard's reason is a mutant that proves
// nothing about the guard under test.
func TestTheWalkRefusalsCanEachGoRED(t *testing.T) {
	// POSITIVE CONTROL FIRST: a clean walk passes. Without it every red below is
	// satisfied by a function that refuses everything.
	if err := refuseWalkRegressions(cleanWalk()); err != nil {
		t.Fatalf("POSITIVE CONTROL FAILED: a clean walk over all %d widths was REFUSED (%v). Every red "+
			"below is then satisfied by a gate that refuses everything.", len(Viewports), err)
	}

	for _, tc := range []struct {
		name    string
		break_  func([]*Capture)
		wantSub string
	}{
		{
			// The one no unit test in this repository can see, and the one content is most
			// likely to cause: a long unbroken line in an entry body makes the whole PAGE
			// scroll sideways.
			name:    "horizontal overflow at ONE width",
			break_:  func(cs []*Capture) { cs[0].Layout.HorizontalOverflow = true },
			wantSub: "HORIZONTAL OVERFLOW",
		},
		{
			// 🔴 AT THE WIDEST WIDTH, DELIBERATELY. A gate that only inspected the first
			// capture — or only the mobile one, which is where every layout worry starts —
			// would pass this, and the ultrawide end is the width this change ADDED and the
			// one nothing had ever rendered at.
			name:    "horizontal overflow at the WIDEST width only",
			break_:  func(cs []*Capture) { cs[len(cs)-1].Layout.HorizontalOverflow = true },
			wantSub: "HORIZONTAL OVERFLOW",
		},
		{
			name:    "a script on the page",
			break_:  func(cs []*Capture) { cs[2].ScriptCount = 1 },
			wantSub: "SCRIPT ON THE PAGE",
		},
		{
			// An axe result that decodes but carries no engine block: exactly what a
			// blocked or failed injection produces, and byte-identical to a clean page in
			// the violation COUNT.
			name:    "axe never ran",
			break_:  func(cs []*Capture) { cs[1].AxeJSON = []byte(`{"violations":[]}`) },
			wantSub: "AXE DID NOT RUN",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			captures := cleanWalk()
			tc.break_(captures)
			err := refuseWalkRegressions(captures)
			if err == nil {
				t.Fatalf("the walk was NOT refused. This property is printed in the summary either way, so "+
					"a regression would be a digit in a log beside an exit 0. (%s)", tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("the walk was refused, but not for %q — a mutant that dies for another guard's "+
					"reason proves nothing about this one. Got: %v", tc.wantSub, err)
			}
			// The message must name WHERE, or a reader has 65 captures and no page.
			if !strings.Contains(err.Error(), "px)") {
				t.Errorf("the refusal names no page-and-width: %v", err)
			}
		})
	}

	// 🔴 AND THE COLLAPSED MATRIX, WHICH IS THE ONE FAILURE THAT LOOKS LIKE SUCCESS. A walk
	// that captured one width finds zero overflow at every width it looked at, and reads
	// exactly like a responsive surface. This is why the width count is part of the verdict.
	oneWidth := cleanWalk()[:1]
	err := refuseWalkRegressions(oneWidth)
	if err == nil {
		t.Fatalf("a walk over ONE of %d declared widths was accepted. Its zero overflow count is a fact "+
			"about that width, and a matrix that silently collapsed is indistinguishable from a clean run.",
			len(Viewports))
	}
	if !strings.Contains(err.Error(), "matrix collapsed") {
		t.Errorf("the collapsed-matrix refusal does not say so: %v", err)
	}

	// An EMPTY capture set is refused too: `refuseWalkRegressions(nil)` returning nil would
	// be the reassuring zero this repository's rules name by hand — a clean verdict from a
	// gate wired to nothing.
	if err := refuseWalkRegressions(nil); err == nil {
		t.Error("refuseWalkRegressions accepted an EMPTY capture set, so its clean verdict is producible " +
			"by a walk that captured nothing")
	}

	t.Logf("walk refusals: clean over %d width(s) PASSES; overflow (first and widest), script and "+
		"axe-absent each go RED with their own message; a 1-width matrix and an empty set are refused",
		len(Viewports))
}
