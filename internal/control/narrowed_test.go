package control

import "testing"

// TestAnAuthorizationSaysWhetherACredentialNarrowedIt pins `Authorization.Narrowed` across
// the three states a narrowing has — nil, non-nil empty, and a list — plus the one shape a
// CONTENT comparison would misread: a narrowing that names every scope the principal holds
// today, which is still a narrowed credential and stops matching the full set the moment the
// principal gains a scope.
func TestAnAuthorizationSaysWhetherACredentialNarrowedIt(t *testing.T) {
	full := Resolve(withCredentials(t), user(uCarol))
	everything := full.ScopeIDs(VerbRead)
	if len(everything) < 2 {
		t.Fatalf("precondition: carol reaches %v, need at least two scopes for a subset to be a subset", everything)
	}
	for _, tc := range []struct {
		name string
		only []ID
		want bool
	}{
		{"nil narrows nothing", nil, false},
		{"a non-nil EMPTY list is narrowed (it sees nothing)", []ID{}, true},
		{"a strict subset is narrowed", everything[:1], true},
		{"a list EQUAL to today's full set is still narrowed", everything, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Narrow(full, tc.only).Narrowed(); got != tc.want {
				t.Fatalf("Narrow(full, %#v).Narrowed() = %v, want %v", tc.only, got, tc.want)
			}
		})
	}
	if full.Narrowed() {
		t.Fatal("Resolve's own answer reports itself narrowed; no credential was involved")
	}

	// End to end through Authenticate: the un-narrowed fixture credential is not narrowed,
	// a credential issued with a narrowing is — including one whose list equals the full set.
	_, plain, err := Authenticate(withCredentials(t), carolToken)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if plain.Narrowed() {
		t.Fatal("an un-narrowed credential authenticated as narrowed")
	}
	const narrowTok = "cairn-test-narrow-111111111111111111111111111111"
	m := withCredentials(t,
		Event{Kind: EventCredentialIssued, At: at(44), CredentialID: "crd_narrow_all",
			SubjectKind: KindUser, SubjectID: uCarol, TokenHash: HashToken(narrowTok),
			Label: "narrowed to everything", NarrowedScopes: everything},
	)
	_, a, err := Authenticate(m, narrowTok)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if !a.Narrowed() {
		t.Fatal("a credential narrowed to its principal's whole current set authenticated as UN-narrowed")
	}
}
