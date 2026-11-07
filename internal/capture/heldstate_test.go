package capture

import "testing"

// The held backlog in the STATE: `Held`/`HeldCount` are what the run line's `held-backlog=` and
// `--list-held` read, and `ClearHeld` is the ONE thing that recovers a held session — `held` is
// final and no rule change can.

func TestHeldListsAndCountsEveryHeldSession(t *testing.T) {
	s := &State{Schema: 1, Sessions: map[string]*SessionState{
		"b-root": {Runtime: "opencode", Held: true, HeldReason: "an unknown scope (*)"},
		"a-root": {Runtime: "claude", Held: true, HeldReason: "its scopes route to more than one instance", Instance: "client"},
		"c-root": {Runtime: "claude"},
		"d-root": {Runtime: "claude", Deleted: "client"},
	}}
	if got := s.HeldCount(); got != 2 {
		t.Fatalf("HeldCount = %d, want 2", got)
	}
	got := s.Held()
	if len(got) != 2 || got[0].Root != "a-root" || got[1].Root != "b-root" {
		t.Fatalf("Held is not the two held sessions in root order: %+v", got)
	}
	if !got[1].Clearable() {
		t.Fatalf("a hold with no instance is not clearable: %+v", got[1])
	}
	if got[0].Clearable() {
		t.Fatalf("a hold that names an instance is clearable: %+v", got[0])
	}
}

func TestClearHeldClearsOnlyTheHoldsWithNoInstance(t *testing.T) {
	s := &State{Schema: 1, Sessions: map[string]*SessionState{
		"routing-only": {Runtime: "claude", Held: true, HeldReason: "an unknown scope (*)",
			V: []string{"*"}, Declared: []string{"*"}, Streams: map[string]*StreamState{"main": {Offset: 42, Cursor: "c"}}},
		"landed": {Runtime: "claude", Held: true, HeldReason: "prefix not withdrawn", Instance: "client"},
	}}
	cleared, kept := s.ClearHeld()
	if len(cleared) != 1 || cleared[0].Root != "routing-only" {
		t.Fatalf("cleared = %+v, want only the routing-only hold", cleared)
	}
	if len(kept) != 1 || kept[0].Root != "landed" {
		t.Fatalf("kept = %+v, want only the hold that names an instance", kept)
	}
	ss := s.Sessions["routing-only"]
	if ss.Held || ss.HeldReason != "" {
		t.Fatalf("the cleared hold still says held: %+v", ss)
	}
	// 🔴 NOTHING ELSE MOVED. The watermarks, the binding and V are what a withdrawal obligation and
	// the next pass's resume depend on; clearing a hold must not touch any of them.
	if ss.Instance != "" || ss.Streams["main"].Offset != 42 || ss.Streams["main"].Cursor != "c" ||
		len(ss.V) != 1 || len(ss.Declared) != 1 {
		t.Fatalf("clearing the hold changed more than the hold: %+v", ss)
	}
	if other := s.Sessions["landed"]; !other.Held || other.Instance != "client" || other.HeldReason == "" {
		t.Fatalf("the kept hold was disturbed: %+v", other)
	}
	if got := s.HeldCount(); got != 1 {
		t.Fatalf("after clearing, HeldCount = %d, want 1", got)
	}
}
