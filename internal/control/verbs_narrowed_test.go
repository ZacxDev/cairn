package control

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// The VERB narrowing of decision 17 of `claudedocs/plan-cairn-scope-mail.md`: an issued
// credential can be restricted to a subset of verbs on every scope it reaches, so an agent
// credential never carries `admin`.
//
// The fixture is carol, whose row on `atlas-notes` is the matrix's one UNION (member
// read,write plus a personal admin grant) — so there IS an `admin` for a verb narrowing to
// strip, and a narrowing that kept it is visible on one cell.

const carolAgentToken = "cairn-test-carol-agent-00000000000000000000000000"

func readWrite() *VerbSet { v := NewVerbSet(VerbRead, VerbWrite); return &v }

func verbsEvent(id ID, verbs ...Verb) Event {
	return Event{Kind: EventCredentialVerbsNarrowed, At: at(51), CredentialID: id, Verbs: NewVerbSet(verbs...)}
}

func carolAgent(narrowScopes []ID) Event {
	return Event{Kind: EventCredentialIssued, At: at(50), CredentialID: "crd_carol_agent",
		SubjectKind: KindUser, SubjectID: uCarol, TokenHash: HashToken(carolAgentToken),
		Label: "agents@host-a", NarrowedScopes: narrowScopes}
}

// TestPreconditionCarolIsAnAdminOfAtlasNotes pins the premise every test below rests on:
// without it, "the narrowed credential lacks admin" would be true of an un-narrowed one too.
func TestPreconditionCarolIsAnAdminOfAtlasNotes(t *testing.T) {
	if !Resolve(withCredentials(t), user(uCarol)).Allows(sAtlasNotes, VerbAdmin) {
		t.Fatal("precondition: carol does not hold admin on atlas-notes, so nothing below can see a verb narrowing")
	}
}

// TestAVerbNarrowedCredentialLacksAdmin — the scope- AND verb-narrowed agent credential keeps
// read and write and loses admin; the same principal's un-narrowed credential keeps admin
// (POSITIVE CONTROL), so the absence is the narrowing's doing.
func TestAVerbNarrowedCredentialLacksAdmin(t *testing.T) {
	m := withCredentials(t, carolAgent([]ID{sAtlasNotes}), verbsEvent("crd_carol_agent", VerbRead, VerbWrite))
	_, a, err := Authenticate(m, carolAgentToken)
	if err != nil {
		t.Fatalf("the agent credential does not authenticate: %v", err)
	}
	if a.Allows(sAtlasNotes, VerbAdmin) {
		t.Fatal("a credential narrowed to read,write still carries ADMIN on atlas-notes — decision 17's whole point")
	}
	if !a.Allows(sAtlasNotes, VerbRead) || !a.Allows(sAtlasNotes, VerbWrite) {
		t.Fatalf("the narrowing removed more than admin: %s", a.VerbsOn(sAtlasNotes))
	}
	if !a.Narrowed() {
		t.Error("a verb-narrowed authority reports Narrowed() = false")
	}
	_, full, err := Authenticate(m, carolToken)
	if err != nil || !full.Allows(sAtlasNotes, VerbAdmin) {
		t.Fatalf("POSITIVE CONTROL FAILED: carol's un-narrowed credential lacks admin (err=%v)", err)
	}
}

// TestVerbNarrowingIntersectsAndASecondEventCannotWiden — round 2 🟡1 (1): a second event
// naming {read,write,admin} is applied as the INTERSECTION, so `admin` stays gone. RED under a
// "replace, not intersect" mutant.
func TestVerbNarrowingIntersectsAndASecondEventCannotWiden(t *testing.T) {
	m := withCredentials(t, carolAgent([]ID{sAtlasNotes}),
		verbsEvent("crd_carol_agent", VerbRead, VerbWrite),
		verbsEvent("crd_carol_agent", VerbRead, VerbWrite, VerbAdmin))
	if got := *m.Credentials["crd_carol_agent"].NarrowedVerbs; got != NewVerbSet(VerbRead, VerbWrite) {
		t.Fatalf("after {read,write} then {read,write,admin} the credential's verbs are %s, want read,write", got)
	}
	_, a, err := Authenticate(m, carolAgentToken)
	if err != nil {
		t.Fatal(err)
	}
	if a.Allows(sAtlasNotes, VerbAdmin) {
		t.Fatal("a SECOND, wider verb narrowing restored admin — narrowing must only intersect")
	}
	// And it can narrow further.
	m = withCredentials(t, carolAgent([]ID{sAtlasNotes}),
		verbsEvent("crd_carol_agent", VerbRead, VerbWrite), verbsEvent("crd_carol_agent", VerbRead))
	if got := *m.Credentials["crd_carol_agent"].NarrowedVerbs; got != NewVerbSet(VerbRead) {
		t.Fatalf("{read,write} then {read} gave %s, want read", got)
	}
}

// TestVerbsOnlyNarrowingIsNotPassedThrough — round 2 🟡1 (2): a credential narrowed by VERBS
// ONLY (nil scope list) keeps every scope and loses admin on each. RED against the base `Narrow`,
// which returned its input unchanged on a nil scope list.
func TestVerbsOnlyNarrowingIsNotPassedThrough(t *testing.T) {
	m := withCredentials(t, carolAgent(nil), verbsEvent("crd_carol_agent", VerbRead, VerbWrite))
	_, a, err := Authenticate(m, carolAgentToken)
	if err != nil {
		t.Fatal(err)
	}
	if a.Allows(sAtlasNotes, VerbAdmin) {
		t.Fatal("a VERBS-ONLY narrowing passed through un-narrowed: admin survived on atlas-notes")
	}
	full := Resolve(m, user(uCarol))
	for _, id := range full.ScopeIDs(VerbRead) {
		want := full.VerbsOn(id).Intersect(NewVerbSet(VerbRead, VerbWrite))
		if got := a.VerbsOn(id); got != want {
			t.Errorf("verbs-only narrowing on %s gave %s, want %s (every scope kept, each intersected)", id, got, want)
		}
	}
	if !a.Narrowed() {
		t.Error("a verbs-only narrowing reports Narrowed() = false — sign-in would then accept it")
	}
}

// TestAScopeWhoseIntersectionIsEmptyIsDropped — the zero-verb scope and the absent scope stay
// one observable. carol's `beacon-notes` row is read only; narrowing to {write} empties it.
func TestAScopeWhoseIntersectionIsEmptyIsDropped(t *testing.T) {
	m := withCredentials(t, carolAgent(nil), verbsEvent("crd_carol_agent", VerbWrite))
	_, a, err := Authenticate(m, carolAgentToken)
	if err != nil {
		t.Fatal(err)
	}
	if !Resolve(m, user(uCarol)).Allows(sBeaconNotes, VerbRead) {
		t.Fatal("precondition: carol cannot read beacon-notes")
	}
	for _, id := range a.ScopeIDs(VerbRead) {
		if id == sBeaconNotes {
			t.Fatal("a scope whose verbs intersect to nothing is still listed")
		}
	}
	for _, s := range a.NamedScopes(VerbWrite) {
		if s.ID == sBeaconNotes {
			t.Fatal("beacon-notes reachable with write after a {write} narrowing of a read-only row")
		}
	}
	if _, present := a.byScope[sBeaconNotes]; present {
		t.Fatal("an EMPTY verb set was kept in the authority rather than dropped")
	}
}

// TestTwoNonEmptyEventsMayIntersectToNothingAndFailClosed — stated rather than refused: the
// credential then reaches no scope.
func TestTwoNonEmptyEventsMayIntersectToNothingAndFailClosed(t *testing.T) {
	m := withCredentials(t, carolAgent(nil), verbsEvent("crd_carol_agent", VerbRead), verbsEvent("crd_carol_agent", VerbWrite))
	_, a, err := Authenticate(m, carolAgentToken)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(a.ScopeIDs(VerbRead)) + len(a.ScopeIDs(VerbWrite)) + len(a.ScopeIDs(VerbAdmin)); n != 0 {
		t.Fatalf("{read} ∩ {write} = ∅ still reaches %d scope-verb pairs", n)
	}
}

// TestAVerbNarrowingOfNothingOrOfNobodyIsRefusedAndAppendsNothing — round 2 🟡1 (3): an EMPTY
// set, or an unknown credential, is refused at apply, and `Append` writes nothing.
func TestAVerbNarrowingOfNothingOrOfNobodyIsRefusedAndAppendsNothing(t *testing.T) {
	for _, tc := range []struct {
		name string
		e    Event
		want string
	}{
		{"an empty verb set", Event{Kind: EventCredentialVerbsNarrowed, At: at(60), CredentialID: "crd_carol"}, "verbs is empty"},
		{"an unknown credential", verbsEvent("crd_nobody", VerbRead), "does not exist"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "journal")
			s, err := OpenFileStore(path)
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if _, err := s.Append(ctx, append(worldEvents(), credentialEvents()...)...); err != nil {
				t.Fatalf("seeding: %v", err)
			}
			before, _ := s.Model(ctx)
			_, err = s.Append(ctx, tc.e)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("append of %s: err=%v, want a refusal containing %q", tc.name, err, tc.want)
			}
			after, _ := s.Model(ctx)
			if after.Epoch != before.Epoch {
				t.Fatalf("a refused narrowing moved the epoch %d -> %d", before.Epoch, after.Epoch)
			}
		})
	}
}

// TestAVerbNarrowingOfARevokedCredentialAppliesAndAuthenticatesNothing — round 2 🟡1 (4).
func TestAVerbNarrowingOfARevokedCredentialAppliesAndAuthenticatesNothing(t *testing.T) {
	m := withCredentials(t, verbsEvent("crd_dead", VerbRead))
	if v := m.Credentials["crd_dead"].NarrowedVerbs; v == nil || *v != NewVerbSet(VerbRead) {
		t.Fatalf("the narrowing of a revoked credential was not applied: %v", v)
	}
	if _, _, err := Authenticate(m, deadToken); !errors.Is(err, ErrNoCredential{}) {
		t.Fatalf("a revoked, verb-narrowed credential authenticated: %v", err)
	}
}

// TestAnOlderBuildRefusesAJournalCarryingTheNewKindWhole is the FAIL-CLOSED premise decision 17
// rests on, asserted: an event kind a build does not know refuses the WHOLE journal rather than
// being dropped. It is an INVARIANT guard — `validate`'s `default:` arm predates this change —
// and it is spelled as the older build sees the line: a kind outside its closed set. The
// cross-build measurement (the base commit's `Replay` over a journal this build wrote) is
// recorded in the plan's "S1 — measurements and corrections".
func TestAnOlderBuildRefusesAJournalCarryingTheNewKindWhole(t *testing.T) {
	events := append(append(worldEvents(), credentialEvents()...), carolAgent(nil),
		Event{Kind: "credential-verbs-narrowed-from-a-newer-build", At: at(51), CredentialID: "crd_carol_agent"})
	if _, err := Replay(events); err == nil || !strings.Contains(err.Error(), "unknown event kind") {
		t.Fatalf("an unknown kind did not refuse the journal whole: %v", err)
	}
}

// TestIssuingWithNarrowedVerbsAppendsBothEventsInOneBatch — the issue path writes the verb
// narrowing beside the credential, and the minted token lacks admin.
func TestIssuingWithNarrowedVerbsAppendsBothEventsInOneBatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal")
	s, err := OpenFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	seeded, err := s.Append(ctx, worldEvents()...)
	if err != nil {
		t.Fatal(err)
	}
	issued, err := IssueCredential(ctx, s, NewCredential{SubjectKind: KindUser, SubjectID: uCarol,
		Label: "agents@host-a", NarrowedScopes: []ID{sAtlasNotes}, NarrowedVerbs: readWrite()})
	if err != nil {
		t.Fatal(err)
	}
	if issued.Epoch != seeded.Epoch+2 {
		t.Fatalf("epoch %d -> %d: want exactly two events (issued + verbs-narrowed)", seeded.Epoch, issued.Epoch)
	}
	m, _ := s.Model(ctx)
	_, a, err := Authenticate(m, issued.Token())
	if err != nil {
		t.Fatal(err)
	}
	if a.Allows(sAtlasNotes, VerbAdmin) || !a.Allows(sAtlasNotes, VerbWrite) {
		t.Fatalf("issued with read,write: got %s on atlas-notes", a.VerbsOn(sAtlasNotes))
	}
}

// TestRevokeCredentialAppendsOneEventAndRefusesUnknownAndRevoked — decision 16's revoke path at
// the library: exactly one `credential-revoked`, the label comes back, and an unknown or
// already-revoked id each refuse with its own sentinel and append nothing.
func TestRevokeCredentialAppendsOneEventAndRefusesUnknownAndRevoked(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal")
	s, err := OpenFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	seeded, err := s.Append(ctx, append(worldEvents(), credentialEvents()...)...)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Authenticate(seeded, carolToken); err != nil {
		t.Fatalf("precondition: carol's credential does not authenticate: %v", err)
	}
	r, err := RevokeCredential(ctx, s, "crd_carol", "", at(70))
	if err != nil {
		t.Fatal(err)
	}
	if r.Epoch != seeded.Epoch+1 || r.Credential.Label != "carol laptop" || r.Credential.Live() {
		t.Fatalf("revoke: epoch %d (want %d), label %q, live=%v", r.Epoch, seeded.Epoch+1, r.Credential.Label, r.Credential.Live())
	}
	m, _ := s.Model(ctx)
	if _, _, err := Authenticate(m, carolToken); !errors.Is(err, ErrNoCredential{}) {
		t.Fatalf("a revoked credential still authenticates: %v", err)
	}
	for _, tc := range []struct {
		id   ID
		want error
	}{{"crd_nobody", ErrNoSuchCredential}, {"crd_carol", ErrAlreadyRevoked}, {"crd_dead", ErrAlreadyRevoked}} {
		before, _ := s.Model(ctx)
		if _, err := RevokeCredential(ctx, s, tc.id, "", at(71)); !errors.Is(err, tc.want) {
			t.Errorf("revoking %s: err=%v, want %v", tc.id, err, tc.want)
		}
		if after, _ := s.Model(ctx); after.Epoch != before.Epoch {
			t.Errorf("a refused revocation of %s appended (epoch %d -> %d)", tc.id, before.Epoch, after.Epoch)
		}
	}
}
