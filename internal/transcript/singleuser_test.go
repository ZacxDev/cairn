package transcript

import (
	"slices"
	"testing"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/transcript/archive"
	"github.com/ZacxDev/cairn/internal/transcript/transcripttest"
)

// 🔴 THE SINGLE-USER READ POLICY ([Policy.SingleUser]). On an instance with one user, the owner's
// NARROWED credentials (its agents) read every session the owner uploaded — including the ones whose
// `V` holds `*`, which the multi-user default keeps owner-only. It widens the CONTENT predicate and
// nothing else: listing, the plugin opt-out and deletion still ask the un-narrowed ownership arm.
//
// Same synthetic world as `visible_test.go`, plus ONE session added here:
//
//	s-star-09  no write; a tool input names the program, no ledger   V = {*}
//
// and s-star-03 (V = {alpha-notes, *}) from the shared world.

// singleUserWorld is the visibility world plus s-star-09, with each V instrument-checked.
func singleUserWorld(t *testing.T) visWorld {
	t.Helper()
	w := visBuild(t)
	owner := string(control.KindUser) + ":" + string(visID(control.PrefixUser, "owner"))
	names := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_su0001",` +
		`"name":"Bash","input":{"command":"cairn recall"}}]}}`
	transcripttest.Append(t, w.arc, transcripttest.Session{Root: "s-star-09", Owner: owner}, "main",
		[]string{`{"type":"user","message":{"role":"user","content":"synthetic"}}`, names})
	for session, want := range map[string][]string{
		"s-star-09": {Star},
		"s-star-03": {Star, "alpha-notes"},
		"s-solo-02": nil,
		"s-only-04": {"alpha-notes"},
	} {
		meta, err := w.arc.ReadMeta(session)
		if err != nil || meta == nil {
			t.Fatalf("%s is not stored (%v)", session, err)
		}
		written, err := WrittenScopes(w.store, append([]string{meta.Root}, meta.Children...))
		if err != nil {
			t.Fatal(err)
		}
		if got := VisibilitySet(meta, written); !slices.Equal(got, want) && !(len(got) == 0 && len(want) == 0) {
			t.Fatalf("INSTRUMENT CONTROL: V(%s) = %v, want %v", session, got, want)
		}
	}
	return w
}

// suViewers are the credentials every single-user test asks about, keyed by a name that says
// what each one IS.
func suViewers(t *testing.T, w visWorld) map[string]Viewer {
	t.Helper()
	owner, every, one := w.viewers["owner"], w.viewers["every"], w.viewers["one"]
	alpha, beta := visID(control.PrefixScope, "alpha-notes"), visID(control.PrefixScope, "beta-notes")
	readOnly := control.NewVerbSet(control.VerbRead)
	v := map[string]Viewer{
		"owner":                  owner,
		"owner-narrowed-alpha":   {Principal: owner.Principal, Auth: control.Narrow(owner.Auth, []control.ID{alpha}, nil)},
		"owner-narrowed-nothing": {Principal: owner.Principal, Auth: control.Narrow(owner.Auth, []control.ID{beta}, nil)},
		"owner-narrowed-verbs":   {Principal: owner.Principal, Auth: control.Narrow(owner.Auth, nil, &readOnly)},
		"every":                  every,
		"every-narrowed-alpha":   {Principal: every.Principal, Auth: control.Narrow(every.Auth, []control.ID{alpha}, nil)},
	}
	// A principal whose DISPLAY is the owner's display, and whose id is not the owner's.
	display := Viewer{Principal: one.Principal, Auth: control.Narrow(one.Auth, []control.ID{alpha}, nil)}
	display.Principal.Display = owner.Principal.Display
	v["display-of-owner-narrowed"] = display
	// A principal whose DISPLAY is the owner's id.
	idAsDisplay := display
	idAsDisplay.Principal.Display = string(owner.Principal.ID)
	v["id-as-display-narrowed"] = idAsDisplay
	// The owner's id under another KIND.
	project := v["owner-narrowed-alpha"]
	project.Principal.Kind = control.KindProject
	v["owner-id-project-kind-narrowed"] = project

	for name, viewer := range v {
		if want := name != "owner" && name != "every"; viewer.Auth.Narrowed() != want {
			t.Fatalf("INSTRUMENT CONTROL: %s.Auth.Narrowed() = %v, want %v", name, viewer.Auth.Narrowed(), want)
		}
	}
	if display.Principal.Display != owner.Principal.Display || display.Principal.ID == owner.Principal.ID {
		t.Fatal("INSTRUMENT CONTROL: the display impostor must share the owner's Display and not its ID")
	}
	if v["owner-narrowed-nothing"].reads("alpha-notes") || !v["owner-narrowed-alpha"].reads("alpha-notes") {
		t.Fatal("INSTRUMENT CONTROL: the owner's narrowing to beta must lack alpha-notes, and to alpha must hold it")
	}
	return v
}

// TestSingleUserLetsTheOwnersNarrowedCredentialsReadEveryOwnedSession is the regression row: with
// the setting ON, the owner's narrowed credential reads a session whose `V` is `{*}` and one whose
// `V` is `{a scope it lacks, *}` — through [Store.View], the predicate every content surface asks.
// With the setting OFF (the default) the SAME credentials are refused the same sessions.
func TestSingleUserLetsTheOwnersNarrowedCredentialsReadEveryOwnedSession(t *testing.T) {
	w := singleUserWorld(t)
	v := suViewers(t, w)
	single := NewStore(w.arc, w.store).WithPolicy(Policy{SingleUser: true})
	multi := NewStore(w.arc, w.store)
	sees := func(s *Store, viewer Viewer, session string) bool {
		t.Helper()
		view, err := s.View(viewer, session)
		if err != nil {
			t.Fatalf("View(%s): %v", session, err)
		}
		return view != nil
	}
	for _, tc := range []struct {
		viewer, session     string
		wantSingle, wantOff bool
	}{
		// 🔴 THE REGRESSION: an owner-narrowed credential and a `*` in V.
		{"owner-narrowed-alpha", "s-star-09", true, false},   // V = {*}
		{"owner-narrowed-nothing", "s-star-03", true, false}, // V = {alpha-notes (lacked), *}
		{"owner-narrowed-alpha", "s-star-03", true, false},   // V = {alpha-notes (held), *}
		{"owner-narrowed-verbs", "s-star-09", true, false},   // narrowed by verb, not by scope
		{"owner-narrowed-nothing", "s-solo-02", true, false}, // V = {} — owner-only
		// POSITIVE CONTROLS, unchanged by the setting.
		{"owner", "s-star-09", true, true},
		{"owner-narrowed-alpha", "s-only-04", true, true},
		{"owner-narrowed-nothing", "s-only-04", true, false}, // lacks alpha: only the owner arm admits it
		// 🔴 NOBODY ELSE WIDENS.
		{"every-narrowed-alpha", "s-star-09", false, false},
		{"every-narrowed-alpha", "s-star-03", false, false},
		{"every", "s-star-03", false, false},
		{"display-of-owner-narrowed", "s-star-09", false, false},
		{"display-of-owner-narrowed", "s-star-03", false, false},
		{"id-as-display-narrowed", "s-star-09", false, false},
		{"owner-id-project-kind-narrowed", "s-star-09", false, false},
		{"owner-narrowed-alpha", "s-none-99", false, false}, // never stored
	} {
		if got := sees(single, v[tc.viewer], tc.session); got != tc.wantSingle {
			t.Errorf("single-user: %s reading %s = %v, want %v", tc.viewer, tc.session, got, tc.wantSingle)
		}
		if got := sees(multi, v[tc.viewer], tc.session); got != tc.wantOff {
			t.Errorf("multi-user (default): %s reading %s = %v, want %v", tc.viewer, tc.session, got, tc.wantOff)
		}
	}
}

// TestSingleUserPolicyOnLiteralSets pins [Policy.Visible] and the default [Visible] on literal `V`s.
func TestSingleUserPolicyOnLiteralSets(t *testing.T) {
	w := singleUserWorld(t)
	v := suViewers(t, w)
	ownerKey := string(control.KindUser) + ":" + string(visID(control.PrefixUser, "owner"))
	meta := &archive.Meta{Root: "s-0001", Owner: ownerKey}
	on, off := Policy{SingleUser: true}, Policy{}
	for _, set := range [][]string{{Star}, {"gamma-notes", Star}, nil} {
		if !on.Visible(v["owner-narrowed-alpha"], meta, set) {
			t.Errorf("single-user: the owner's narrowed credential is refused V = %v", set)
		}
		if off.Visible(v["owner-narrowed-alpha"], meta, set) || Visible(v["owner-narrowed-alpha"], meta, set) {
			t.Errorf("multi-user: the owner's narrowed credential is admitted to V = %v", set)
		}
		for _, other := range []string{"every-narrowed-alpha", "display-of-owner-narrowed", "id-as-display-narrowed", "owner-id-project-kind-narrowed"} {
			if on.Visible(v[other], meta, set) {
				t.Errorf("single-user: %s is admitted to the owner's V = %v", other, set)
			}
		}
	}
	if on.Visible(v["owner-narrowed-alpha"], nil, []string{Star}) {
		t.Error("single-user: a nil meta — no stored session — is visible")
	}
}

// TestSingleUserWidensTheOutputRowsToo: [Store.ViewsWithOutputs] (a session row's derived
// one-liner) asks the same policy as [Store.View].
func TestSingleUserWidensTheOutputRowsToo(t *testing.T) {
	w := singleUserWorld(t)
	v := suViewers(t, w)
	full, err := NewStore(w.arc, w.store).View(v["owner"], "s-star-09")
	if err != nil || full == nil {
		t.Fatalf("POSITIVE CONTROL: the owner cannot open s-star-09 (%v)", err)
	}
	if err := full.AppendOutput("fixture-plugin", []byte(`{"synthetic":true}`)); err != nil {
		t.Fatal(err)
	}
	roots := []string{"s-star-09"}
	for _, tc := range []struct {
		store  *Store
		policy string
		viewer string
		want   bool
	}{
		{NewStore(w.arc, w.store).WithPolicy(Policy{SingleUser: true}), "single-user", "owner-narrowed-nothing", true},
		{NewStore(w.arc, w.store), "multi-user", "owner-narrowed-nothing", false},
		{NewStore(w.arc, w.store).WithPolicy(Policy{SingleUser: true}), "single-user", "every-narrowed-alpha", false},
		{NewStore(w.arc, w.store).WithPolicy(Policy{SingleUser: true}), "single-user", "display-of-owner-narrowed", false},
	} {
		got, err := tc.store.ViewsWithOutputs(v[tc.viewer], roots)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := got["s-star-09"]; ok != tc.want {
			t.Errorf("%s: ViewsWithOutputs(%s) holds s-star-09 = %v, want %v", tc.policy, tc.viewer, ok, tc.want)
		}
	}
}

// TestSingleUserLeavesOwnershipStrict: listing ([Store.Mine]), the plugin opt-out ([Store.OwnedBy])
// and deletion ([Store.Delete]) still refuse the owner's narrowed credential with the setting ON.
func TestSingleUserLeavesOwnershipStrict(t *testing.T) {
	w := singleUserWorld(t)
	v := suViewers(t, w)
	single := NewStore(w.arc, w.store).WithPolicy(Policy{SingleUser: true})
	agent := v["owner-narrowed-alpha"]

	if owned, err := single.OwnedBy(agent, "s-star-09"); err != nil || owned {
		t.Errorf("single-user: OwnedBy(owner's narrowed credential) = %v, %v; want false", owned, err)
	}
	if mine, err := single.Mine(agent); err != nil || len(mine.Sessions) != 0 {
		t.Errorf("single-user: Mine(owner's narrowed credential) listed %d session(s) (%v); want 0", len(mine.Sessions), err)
	}
	if deleted, err := single.Delete(agent, "s-star-09"); err != nil || deleted {
		t.Errorf("single-user: Delete(owner's narrowed credential) = %v, %v; want false", deleted, err)
	}
	if view, err := single.View(v["owner"], "s-star-09"); err != nil || view == nil {
		t.Fatalf("s-star-09 is gone after a refused delete (%v)", err)
	}
	// POSITIVE CONTROLS: the un-narrowed owner holds each surface, under the same store.
	if owned, err := single.OwnedBy(v["owner"], "s-star-09"); err != nil || !owned {
		t.Fatalf("POSITIVE CONTROL: OwnedBy(owner) = %v, %v", owned, err)
	}
	if mine, err := single.Mine(v["owner"]); err != nil || len(mine.Sessions) != 9 {
		t.Fatalf("POSITIVE CONTROL: Mine(owner) listed %d session(s) (%v), want 9", len(mine.Sessions), err)
	}
	if deleted, err := single.Delete(v["owner"], "s-star-09"); err != nil || !deleted {
		t.Fatalf("POSITIVE CONTROL: Delete(owner) = %v, %v", deleted, err)
	}
}
