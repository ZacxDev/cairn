package capture

// The routing policy for `*` and for a session that read BOTH instances' scopes, decided by the
// operator:
//
//   - `*` is a VISIBILITY statement — "may be anything, so nobody but the owner may read it" — not
//     an unplaceable scope: the session ships to the alias its other scopes name, or to the default
//     instance when it names none, and it DECLARES `*` so the pod stores it owner-only.
//   - a session whose scopes name TWO instances is PLACED on the default instance, owner-only, not
//     held.
//
// Both used to be `Held`, and `Held` is FINAL in the state: on the measured host that made 851 of
// 883 held sessions permanently uncaptured (`*`), because the client records `*` for every verb whose
// output names scopes it cannot enumerate — `cairn routes`, `cairn doctor`, the transcript verbs.
//
// 🔴 THESE ARE THE REGRESSION TESTS FOR THAT. Each case fails on the pre-change code by construction
// (`Held: true`), so the assertion below is not "the new shape is present" but "the hold is gone".

import (
	"testing"

	"github.com/ZacxDev/cairn/internal/client"
)

func TestAnUnknownScopeIsPlacedOnTheDefaultInstanceOwnerOnly(t *testing.T) {
	for _, tc := range []struct {
		name string
		v    []string
		want string
	}{
		{"`*` alone", []string{"*"}, "personal"},
		{"`*` beside a default-instance scope", []string{"*", "alpha-notes"}, "personal"},
		{"`*` beside the other instance's scope", []string{"*", "beta-notes"}, "personal"},
		{"the other instance's scope, then `*`", []string{"beta-notes", "*"}, "personal"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := Decide(tc.v, Repo{}, twoInstances, RepoRules{})
			if d.Held {
				t.Fatalf("V=%v is HELD (%s) — `*` must place, not hold", tc.v, d.Reason)
			}
			if d.Instance != tc.want {
				t.Fatalf("V=%v → instance %q, want %q (%s)", tc.v, d.Instance, tc.want, d.Reason)
			}
			if !d.OwnerOnly {
				t.Fatalf("V=%v was placed WITHOUT owner-only — that is the visibility `*` asks for", tc.v)
			}
		})
	}
}

func TestASessionThatReadTwoInstancesIsPlacedOnTheDefaultOwnerOnly(t *testing.T) {
	d := Decide([]string{"alpha-notes", "beta-notes"}, Repo{}, twoInstances, RepoRules{})
	if d.Held {
		t.Fatalf("a cross-instance session is HELD (%s); it must be placed owner-only", d.Reason)
	}
	if d.Instance != "personal" || !d.OwnerOnly {
		t.Fatalf("cross-instance → %+v, want instance personal with OwnerOnly", d)
	}
	// Order must not matter: the same two scopes the other way round.
	if rev := Decide([]string{"beta-notes", "alpha-notes"}, Repo{}, twoInstances, RepoRules{}); rev.Instance != "personal" || !rev.OwnerOnly || rev.Held {
		t.Fatalf("the order of the scopes changed the answer: %+v", rev)
	}
}

// TestTheUnknownScopePolicyIsUnchangedOnOneInstance: with ONE instance there is nowhere else a
// session could go, and `*` has never held it there. Placing it must not start declaring `*` either —
// the declaration is for the case the default instance is one of several.
func TestTheUnknownScopePolicyIsUnchangedOnOneInstance(t *testing.T) {
	d := Decide([]string{"*"}, Repo{}, oneInstance, RepoRules{})
	if d.Held || d.Instance != "personal" || d.OwnerOnly {
		t.Fatalf("one-instance `*` → %+v, want personal, no hold, no extra declaration", d)
	}
	if d := Decide([]string{"*", "gamma-notes"}, Repo{}, oneInstance, RepoRules{}); d.Held || d.Instance != "personal" {
		t.Fatalf("one-instance `*` + an unnamed scope → %+v", d)
	}
}

// TestAScopeRoutedToAnUnconfiguredInstanceIsStillHeld is the ONE hold the operator decision left
// alone, and the guard on the change: the table says that scope lives on an instance this host does
// not run, and shipping its records to the one it does is the cross-instance leak decision 16 exists
// to stop. Widening `*` and cross-instance must not have widened this.
func TestAScopeRoutedToAnUnconfiguredInstanceIsStillHeld(t *testing.T) {
	two := client.Routing{
		Instances:    []client.Instance{{Alias: "personal"}, {Alias: "client"}},
		Routes:       map[string]string{"gamma-notes": "elsewhere"},
		RoutesSource: "the test's table",
	}
	if d := Decide([]string{"gamma-notes"}, Repo{}, two, RepoRules{}); !d.Held {
		t.Fatalf("a scope the table routes to an unconfigured alias was placed: %+v", d)
	}
	// And it is still a hold when it arrives BESIDE a placeable scope: one unplaceable scope is
	// enough, it is not outvoted.
	if d := Decide([]string{"alpha-notes", "gamma-notes"}, Repo{}, two, RepoRules{}); !d.Held {
		t.Fatalf("an unplaceable scope beside a placeable one was outvoted: %+v", d)
	}
}

// TestEveryScopeOfOneInstanceStillDeclaresNothing: the owner-only declaration is added for `*` and
// for a cross-instance session, and for nothing else — a plain session must keep declaring exactly
// what it read, or every session on a two-instance host silently becomes owner-only.
func TestEveryScopeOfOneInstanceStillDeclaresNothing(t *testing.T) {
	d := Decide([]string{"alpha-notes", "alpha-notes"}, Repo{}, twoInstances, RepoRules{})
	if d.Held || d.OwnerOnly || d.Instance != "personal" {
		t.Fatalf("a single-instance session → %+v, want personal with no owner-only", d)
	}
	if d := Decide(nil, Repo{Scope: "alpha-notes", Owner: "synthetic-org"}, twoInstances, RepoRules{}); d.Held || d.OwnerOnly {
		t.Fatalf("the repo arm produced owner-only: %+v", d)
	}
}
