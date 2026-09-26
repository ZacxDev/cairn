package main

import (
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/ui"
)

// cleanWalk is one capture per declared viewport, all four properties satisfied.
//
// It is built from `Viewports` rather than from a literal list so that adding a sixth
// width cannot leave this control measuring five.
//
// 🔴 THE CONTENT BOX IS BUILT FROM A LITERAL FRACTION THAT IS NOT `contentWidthFloor`, AND
// NOT FROM THE CONSTANT THE GATE READS. Deriving the fixture from the threshold would make
// every case below pass for any threshold, including a mutated one — the expectation would
// be a restatement of the implementation. 0.60 is a number the honest surface clears
// (measured 49.3% at 3440 … see the note on `narrowContent`) and no boundary of the gate
// can equal.
func cleanWalk() []*Capture {
	var out []*Capture
	for _, vp := range Viewports {
		out = append(out, &Capture{
			Target:   Target{Path: "/", PushURL: "/", LedgerRow: "GET / content"},
			Viewport: vp,
			AxeJSON:  []byte(`{"testEngine":{"name":"axe-core","version":"4.x"},"violations":[]}`),
			Layout:   &PushLayout{InnerWidth: vp.Width, ScrollWidth: vp.Width},
			Content: &ContentBox{
				InnerWidth: vp.Width,
				BodyWidth:  vp.Width * 60 / 100,
				MainWidth:  vp.Width * 60 / 100,
				MainClass:  "page-main",
				MainCount:  1,
			},
		})
	}
	return out
}

// narrowContent is the MUTANT: it puts the widest capture's `<main>` back on the cap the
// defect actually had.
//
// 🔴 THE NUMBER IS THE MEASURED DEFECT, NOT A ROUND ONE UNDER THE THRESHOLD. 1232px in a
// 3440px viewport is what a real Chromium rendered on the tree this guard was written
// against — the `xl:max-w-7xl` rung (80rem = 1280px) winning the cascade over
// `ultra:max-w-[112rem]`, less 24px of `sm:px-6` gutter a side. Picking a value derived from
// `contentWidthFloor` instead would make this case pass for any threshold; picking a round
// 0 would make it pass for a gate that only refuses the impossible.
func narrowContent(cs []*Capture) {
	c := widestCapture(cs)
	c.Content.BodyWidth = 1280
	c.Content.MainWidth = 1232
}

// widestCapture finds the capture the content floor binds, BY VIEWPORT VALUE rather than by
// taking the last element. `cleanWalk` happens to order them ascending, and a case that
// relied on that would silently start breaking a different capture the day `Viewports` is
// reordered — the mutation would then die for the wrong reason, or not at all.
func widestCapture(cs []*Capture) *Capture {
	for _, c := range cs {
		if c.Viewport == Ultrawide {
			return c
		}
	}
	panic("no capture at the widest declared viewport: the fixture cannot exercise the content floor")
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
		{
			// 🔴 THE MUTANT THIS GUARD WAS WRITTEN FOR, AND THE ONE EVERY OTHER CASE IN THIS
			// TABLE IS GREEN ON. It leaves `HorizontalOverflow` false — because it IS false:
			// a container too narrow for its viewport does not overflow it — so a walk
			// carrying this capture passes the overflow refusal, the script refusal and the
			// axe refusal, which is exactly how the real defect shipped through seven CI jobs.
			name:    "content is a narrow column at the WIDEST width",
			break_:  narrowContent,
			wantSub: "CONTENT TOO NARROW",
		},
		{
			// The exemption is two conditions and this breaks the PATH half: the sign-in
			// card's class on a page that is not the sign-in page. A class-only exemption
			// would let any page opt out of the floor by spelling a word.
			name: "a narrow page wearing the sign-in card's CLASS is still refused",
			break_: func(cs []*Capture) {
				narrowContent(cs)
				widestCapture(cs).Content.MainClass = signinMainClass
			},
			wantSub: "CONTENT TOO NARROW",
		},
		{
			// …and this breaks the CLASS half: the sign-in PATH carrying an ordinary content
			// `<main>`. A path-only exemption would let wide content move behind that route
			// and stop being measured.
			name: "a narrow page at the sign-in PATH with an ordinary <main> is still refused",
			break_: func(cs []*Capture) {
				narrowContent(cs)
				widestCapture(cs).Target.Path = ui.SignInPath
			},
			wantSub: "CONTENT TOO NARROW",
		},
		{
			// A nil content box is a MEASUREMENT that did not happen, and a floor that
			// treated it as satisfied would be green on exactly the walk that measured
			// nothing.
			name:    "the widest capture carries no content box at all",
			break_:  func(cs []*Capture) { widestCapture(cs).Content = nil },
			wantSub: "carries no content box",
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

	// 🔴 THE EXEMPTION'S OWN POSITIVE SIDE, WITHOUT WHICH THE THREE RED CASES ABOVE ARE
	// SATISFIED BY A FLOOR THAT EXEMPTS NOTHING. The real sign-in page renders a 448px
	// `max-w-md` card — 13% of an ultrawide viewport — and that is design rather than
	// defect; a gate that refused it would be red on the honest tree, which is the
	// permanently-red gate this repository refuses.
	//
	// It is APPENDED to a clean walk rather than substituted into it, because the floor also
	// refuses a widest width where every capture is exempt — and a fixture that replaced the
	// only ordinary page would then go red for that reason instead, which proves nothing
	// about the exemption.
	exempted := append(cleanWalk(), &Capture{
		Target:   Target{Path: ui.SignInPath, PushURL: ui.SignInPath, LedgerRow: "GET /sign-in public"},
		Viewport: Ultrawide,
		AxeJSON:  []byte(`{"testEngine":{"name":"axe-core","version":"4.x"},"violations":[]}`),
		Layout:   &PushLayout{InnerWidth: Ultrawide.Width, ScrollWidth: Ultrawide.Width},
		Content: &ContentBox{
			InnerWidth: Ultrawide.Width,
			BodyWidth:  1792,
			MainWidth:  448,
			MainClass:  signinMainClass,
			MainCount:  1,
		},
	})
	if err := refuseWalkRegressions(exempted); err != nil {
		t.Errorf("the sign-in card (448px of %dpx = 13%%) was REFUSED by the content floor: %v. A single-field "+
			"credential form stretched across the display is worse, not better — this exemption is what keeps "+
			"the gate off the honest tree.", Ultrawide.Width, err)
	}

	// …and the case where EVERY capture at the widest width is the exemption, which is the
	// floor measuring nothing while reporting a clean verdict.
	allExempt := []*Capture{}
	for _, c := range cleanWalk() {
		if c.Viewport == Ultrawide {
			c.Target.Path = ui.SignInPath
			c.Content.MainClass = signinMainClass
			c.Content.MainWidth = 448
		}
		allExempt = append(allExempt, c)
	}
	err = refuseWalkRegressions(allExempt)
	if err == nil {
		t.Error("a walk whose ONLY capture at the widest width was the declared exemption was accepted: the " +
			"content floor bound zero captures, so its clean verdict is about nothing")
	} else if !strings.Contains(err.Error(), "BOUND 0 capture(s)") {
		t.Errorf("the bound-nothing refusal does not say so: %v", err)
	}

	// 🔴 AND THE FRACTION'S SCOPE, WHICH IS THE ONE INPUT THE GATE CANNOT DERIVE. The floor
	// is a share of a viewport and the shell's cap is an absolute 112rem, so the same honest
	// layout scores 49% at 3440 and 36% at 5000. Moving the widest capture without
	// re-deriving the fraction must refuse rather than quietly change what is being asserted.
	func() {
		saved := Ultrawide.Width
		defer func() { Ultrawide.Width = saved }()
		Ultrawide.Width = 5000
		err := refuseWalkRegressions(cleanWalk())
		if err == nil {
			t.Errorf("the widest declared viewport moved from %dpx to 5000px and the %.0f%% floor was applied "+
				"anyway: the fraction is not scale-free, so it was silently asserting something else",
				saved, contentWidthFloor*100)
		} else if !strings.Contains(err.Error(), "re-derived") {
			t.Errorf("the moved-matrix refusal does not say the fraction must be re-derived: %v", err)
		}
	}()

	t.Logf("walk refusals: clean over %d width(s) PASSES; overflow (first and widest), script, axe-absent, "+
		"a narrow <main> at %dpx and both halves of the sign-in exemption each go RED with their own message; "+
		"the sign-in card itself PASSES; a 1-width matrix, an all-exempt widest width, a moved matrix and an "+
		"empty set are refused",
		len(Viewports), Ultrawide.Width)
}
