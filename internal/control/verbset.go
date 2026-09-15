package control

import (
	"encoding/json"
	"fmt"
	"strings"
)

// VerbSet is a set of verbs.
//
// 🔴 IT IS A VALUE, NOT A MAP, AND THAT IS LOAD-BEARING. Two authority computations
// that produce the same verbs must COMPARE EQUAL with `==`, because the matrix test
// that pins principal x scope x verb compares whole rows, and a map-backed set
// would force every comparison through a helper that a future edit can get wrong in
// one direction. A bitmask over a three-element closed set is comparable, hashable,
// and cheap enough that intersection is one instruction.
//
// 🔴 THE ZERO VALUE IS THE EMPTY SET, WHICH IS THE FAIL-CLOSED DIRECTION. A VerbSet
// nobody assigned permits nothing. There is no sentinel for "all verbs" — `AllSet`
// is an ordinary value with all three bits, not a wildcard — because the whole
// lesson of `store.ScopeSet` is that a set with a "means everything" sentinel gets
// reached by a caller that meant "nothing set yet".
type VerbSet uint8

const (
	bitRead VerbSet = 1 << iota
	bitWrite
	bitAdmin
)

// verbBits is the ONE mapping from a verb to its bit. A verb absent from it has no
// bit, so `NewVerbSet` cannot silently accept a string that is not a verb.
var verbBits = map[Verb]VerbSet{
	VerbRead:  bitRead,
	VerbWrite: bitWrite,
	VerbAdmin: bitAdmin,
}

// NewVerbSet folds verbs into a set. An unknown verb is DROPPED rather than
// accepted.
//
// ⚠ DROPPED, NOT REFUSED, AND THE ASYMMETRY IS DELIBERATE: this is the narrowing
// direction. A caller that hands in a verb this build does not know gets LESS
// authority, never more. The place that refuses an unknown verb loudly is
// `Event.validate`, at the journal boundary, where the input is untrusted and a
// silent drop would let a newer writer's grant be replayed as a weaker one without
// anybody noticing. Two different answers to the same input, at two boundaries,
// each in the safe direction for that boundary.
func NewVerbSet(verbs ...Verb) VerbSet {
	var s VerbSet
	for _, v := range verbs {
		s |= verbBits[v]
	}
	return s
}

// AllSet is every verb this build defines. Not a wildcard — see the type comment.
var AllSet = NewVerbSet(AllVerbs...)

// Has answers whether one verb is in the set.
func (s VerbSet) Has(v Verb) bool {
	bit, known := verbBits[v]
	if !known {
		return false
	}
	return s&bit != 0
}

// Union is the wider of two authorities. Used where a principal reaches a scope by
// more than one route — a personal grant AND a project grant, say — and gets the
// best of them.
func (s VerbSet) Union(o VerbSet) VerbSet { return s | o }

// Intersect is the narrower. This is the ONLY operation a credential's
// `NarrowedScopes` and any future per-credential verb narrowing may use.
func (s VerbSet) Intersect(o VerbSet) VerbSet { return s & o }

// Empty answers whether the set permits nothing.
func (s VerbSet) Empty() bool { return s == 0 }

// Verbs lists the set in `AllVerbs` order.
//
// Order comes from the declared slice rather than from map iteration, so the same
// set always renders the same string — the determinism discipline the rest of this
// repository applies to every rendered list.
func (s VerbSet) Verbs() []Verb {
	out := make([]Verb, 0, len(AllVerbs))
	for _, v := range AllVerbs {
		if s.Has(v) {
			out = append(out, v)
		}
	}
	return out
}

// String renders the set as a comma-separated list, or `-` when empty.
//
// `-` rather than the empty string because this lands in audit lines and rendered
// tables, where an empty field is indistinguishable from a missing one.
func (s VerbSet) String() string {
	if s.Empty() {
		return "-"
	}
	parts := make([]string, 0, 3)
	for _, v := range s.Verbs() {
		parts = append(parts, string(v))
	}
	return strings.Join(parts, ",")
}

// MarshalJSON writes the set as a LIST OF NAMES, never as the integer.
//
// 🔴 THE BITMASK IS AN IMPLEMENTATION DETAIL AND MUST NOT REACH THE JOURNAL. The
// journal is the durable authority and is read by humans auditing a grant log; a
// row saying `3` is unreadable, and worse, it silently re-interprets if the bit
// order ever changes. Names survive a reordering; a mask does not.
func (s VerbSet) MarshalJSON() ([]byte, error) {
	names := make([]string, 0, len(AllVerbs))
	for _, v := range s.Verbs() {
		names = append(names, string(v))
	}
	if names == nil {
		names = []string{}
	}
	return json.Marshal(names)
}

// UnmarshalJSON reads a list of names, REFUSING one it does not recognise.
//
// 🔴 REFUSING, NOT DROPPING — the mirror of `NewVerbSet`'s deliberate silence, and
// the reason that asymmetry is safe. This is the untrusted boundary: a journal
// written by a newer build carries a verb this one cannot enforce, and replaying it
// as a narrower grant would produce a model that looks complete and is not. Failing
// the replay is loud, and loud is correct for an authority nobody can check by
// reading the bytes.
func (s *VerbSet) UnmarshalJSON(b []byte) error {
	var names []string
	if err := json.Unmarshal(b, &names); err != nil {
		return err
	}
	var out VerbSet
	for _, n := range names {
		v := Verb(n)
		if !v.Valid() {
			return fmt.Errorf(
				"unknown verb %q: this build defines %v — a journal naming a verb it cannot enforce is refused rather than replayed as a narrower grant, because a model that looks complete and is not is the failure this refusal exists for",
				n, AllVerbs)
		}
		out |= verbBits[v]
	}
	*s = out
	return nil
}
