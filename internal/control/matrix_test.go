package control

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// expectedMatrix is the WHOLE authorization relationship, written out.
//
// 🔴 IT IS A LEDGER, NOT A SAMPLE, AND THAT IS THE POINT. `AGENTS.md` and the
// governing rules both say the same thing twice over: two components each tested in
// isolation can be broken together, so a seam guard must pin a RELATIONSHIP — an
// asserted ledger that fails when the set GROWS as well as when it SHRINKS. Every
// principal in the fixture appears here, against every scope, and the test refuses
// to run if the model holds a principal or a scope this table does not name.
//
// Verbs are spelled as the literal strings a grant carries, not as `VerbSet`
// constants, so the table states the expected ANSWER rather than restating the
// implementation that produces it. `""` means "nothing at all", which must be the
// same observable as a scope that does not exist.
var expectedMatrix = map[ID]map[ID]string{
	// Owner of atlas. Admin on its scopes by role; read+write on beacon-secrets
	// through the project-as-subject grant; nothing on beacon-notes.
	uAlice: {
		sAtlasNotes:    "read,write,admin",
		sAtlasRunbook:  "read,write,admin",
		sBeaconNotes:   "",
		sBeaconSecrets: "read,write",
	},
	// Admin of atlas — identical to the owner here, because `roleVerbs` maps the
	// two the same on purpose and their difference lives at the project object.
	// The revoked grt_4 is why beacon-notes is empty; without the tombstone this
	// row would read `read,write,admin`.
	uBob: {
		sAtlasNotes:    "read,write,admin",
		sAtlasRunbook:  "read,write,admin",
		sBeaconNotes:   "",
		sBeaconSecrets: "read,write",
	},
	// Member of atlas. `atlas-notes` is the UNION of her role (read,write) and a
	// personal admin grant; `atlas-runbook` is the role alone, and the difference
	// between those two cells is the only thing that can see the union.
	uCarol: {
		sAtlasNotes:    "read,write,admin",
		sAtlasRunbook:  "read,write",
		sBeaconNotes:   "read",
		sBeaconSecrets: "read,write",
	},
	// Owner of beacon, member of nothing else.
	uDave: {
		sAtlasNotes:    "",
		sAtlasRunbook:  "",
		sBeaconNotes:   "read,write,admin",
		sBeaconSecrets: "read,write,admin",
	},
	// A member of NO project. Her two non-empty cells can only come from the
	// project-as-object grant, so they are that mechanism's only witness.
	uErin: {
		sAtlasNotes:    "",
		sAtlasRunbook:  "",
		sBeaconNotes:   "read",
		sBeaconSecrets: "read",
	},
}

// expectedAllowCells is the POSITIVE CONTROL, and it is not decoration.
//
// 🔴 A MATRIX IN WHICH EVERY CELL REFUSES IS PERFECTLY SELF-CONSISTENT AND MEASURES
// NOTHING. This repository has already paid for that exact shape once: the parity
// harness reported 72 PASS / 0 FAIL while the pod was refusing every request, because
// two clients failing identically compare equal. A resolver that returned the empty
// authority for everybody would satisfy any test that only checks refusals.
//
// So the count is pinned from BOTH sides. 32 of 60 cells allow: not 0, which would
// mean the resolver grants nothing, and not 60, which would mean it grants
// everything. Any change to the fixture moves this number and forces whoever made it
// to say what they expected.
const (
	expectedAllowCells = 32
	expectedTotalCells = 5 * 4 * 3
)

func TestTheAuthorizationMatrixIsExactlyThis(t *testing.T) {
	m := world(t)

	// The ledger must cover the world. Checked BEFORE the cells, because a table
	// that silently stopped covering a principal would otherwise pass every cell
	// it does name.
	assertLedgerCoversModel(t, m)

	allowed := 0
	total := 0
	for _, principalID := range sortedLedgerPrincipals() {
		p := user(principalID)
		a := Resolve(m, p)
		for _, scopeID := range sortedLedgerScopes(principalID) {
			want := expectedMatrix[principalID][scopeID]
			got := a.VerbsOn(scopeID)
			if gotStr := verbsOrEmpty(got); gotStr != want {
				t.Errorf("Resolve(%s).VerbsOn(%s) = %q, want %q", principalID, scopeID, gotStr, want)
			}
			// Assert every verb INDIVIDUALLY as well as the row. A row comparison
			// alone would pass if `Allows` and `VerbsOn` disagreed with each
			// other, and `Allows` is the one every narrowing site calls.
			for _, v := range AllVerbs {
				total++
				wantAllow := strings.Contains(","+want+",", ","+string(v)+",")
				if gotAllow := a.Allows(scopeID, v); gotAllow != wantAllow {
					t.Errorf("Resolve(%s).Allows(%s, %s) = %v, want %v", principalID, scopeID, v, gotAllow, wantAllow)
				}
				if wantAllow {
					allowed++
				}
			}
		}
	}

	if total != expectedTotalCells {
		t.Errorf("walked %d cells, want %d — the fixture changed shape without this constant moving", total, expectedTotalCells)
	}
	// 🔴 THE POSITIVE CONTROL. See `expectedAllowCells`.
	if allowed != expectedAllowCells {
		t.Errorf("%d of %d cells ALLOW, want %d — a matrix that allows nothing (or everything) is self-consistent and measures nothing, so this count is pinned from both sides",
			allowed, total, expectedAllowCells)
	}
	if allowed == 0 || allowed == total {
		t.Fatalf("the matrix is uniform (%d of %d allow) — every refusal below compares equal for free", allowed, total)
	}
}

// assertLedgerCoversModel fails when the model holds a principal or a scope the
// ledger does not name, and when the ledger names one the model does not hold.
//
// 🔴 BOTH DIRECTIONS. A ledger that only fails on SHRINK lets a new scope — or a new
// user — land with no expectation attached, which is precisely how a new route or a
// new principal gets shipped unmeasured. Growing the world is supposed to cost the
// person who grows it one line here.
func assertLedgerCoversModel(t *testing.T, m Model) {
	t.Helper()

	var modelUsers, ledgerUsers []string
	for id := range m.Users {
		modelUsers = append(modelUsers, string(id))
	}
	for id := range expectedMatrix {
		ledgerUsers = append(ledgerUsers, string(id))
	}
	assertSameSet(t, "users", modelUsers, ledgerUsers)

	var modelScopes []string
	for id := range m.Scopes {
		modelScopes = append(modelScopes, string(id))
	}
	for principal, row := range expectedMatrix {
		var rowScopes []string
		for id := range row {
			rowScopes = append(rowScopes, string(id))
		}
		assertSameSet(t, fmt.Sprintf("scopes in row %s", principal), modelScopes, rowScopes)
	}
}

func assertSameSet(t *testing.T, what string, a, b []string) {
	t.Helper()
	sort.Strings(a)
	sort.Strings(b)
	if strings.Join(a, ",") != strings.Join(b, ",") {
		t.Fatalf("%s: model has [%s], ledger has [%s] — the ledger must fail when the set GROWS as well as when it shrinks, so this is a failure either way",
			what, strings.Join(a, ","), strings.Join(b, ","))
	}
}

func verbsOrEmpty(s VerbSet) string {
	if s.Empty() {
		return ""
	}
	return s.String()
}

func sortedLedgerPrincipals() []ID {
	var out []ID
	for id := range expectedMatrix {
		out = append(out, id)
	}
	sortIDs(out)
	return out
}

func sortedLedgerScopes(principal ID) []ID {
	var out []ID
	for id := range expectedMatrix[principal] {
		out = append(out, id)
	}
	sortIDs(out)
	return out
}

// TestRevocationIsWhatMakesBobDifferFromTheGrant pins the tombstone as the ONLY
// reason one cell of the matrix reads empty.
//
// 🔴 THIS IS THE REGRESSION CLAIM, AND IT IS STATED AS A DIFFERENTIAL RATHER THAN AS
// AN ABSENCE. "Bob cannot read beacon-notes" is satisfiable by a resolver that has
// simply never heard of grt_4. Replaying the SAME world with the revocation event
// removed and watching the cell fill is what distinguishes "revocation works" from
// "the grant never applied" — an empty result cannot tell those two apart, and this
// test is the step that differs.
func TestRevocationIsWhatMakesBobDifferFromTheGrant(t *testing.T) {
	withRevocation := world(t)
	if got := verbsOrEmpty(Resolve(withRevocation, user(uBob)).VerbsOn(sBeaconNotes)); got != "" {
		t.Fatalf("with the revocation, bob has %q on beacon-notes, want nothing", got)
	}

	var without []Event
	for _, e := range worldEvents() {
		if e.Kind == EventGrantRevoked && e.GrantID == "grt_4" {
			continue
		}
		without = append(without, e)
	}
	m, err := Replay(without)
	if err != nil {
		t.Fatalf("replaying without the revocation: %v", err)
	}
	if got := verbsOrEmpty(Resolve(m, user(uBob)).VerbsOn(sBeaconNotes)); got != "read,write,admin" {
		t.Fatalf("without the revocation, bob has %q on beacon-notes, want read,write,admin — if this is also empty then the grant never applied and the cell above proves nothing about revocation",
			got)
	}
}

// TestAProjectShareFollowsMembershipRatherThanFreezingIt is the reason project
// grants expand at resolve time.
//
// Adding a member AFTER the project was granted a scope must give that member the
// share. A resolver that expanded at grant time would freeze the membership as it
// stood, and this is the test that sees it.
func TestAProjectShareFollowsMembershipRatherThanFreezingIt(t *testing.T) {
	events := append(worldEvents(),
		Event{Kind: EventUserCreated, At: at(30), UserID: "usr_frank", Provider: "example", Subject: "frank"},
		Event{Kind: EventMemberSet, At: at(31), ProjectID: pAtlas, UserID: "usr_frank", Role: RoleMember, Actor: uAlice},
	)
	m, err := Replay(events)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	// grt_2 granted `atlas` read+write on beacon-secrets long before frank joined.
	if got := verbsOrEmpty(Resolve(m, user("usr_frank")).VerbsOn(sBeaconSecrets)); got != "read,write" {
		t.Fatalf("a member who joined AFTER the project share has %q, want read,write — a share expanded at grant time freezes the membership it was granted against", got)
	}
}

// TestLeavingAProjectTakesTheShareWith is the mirror: the same expansion has to
// stop applying when the membership ends.
func TestLeavingAProjectTakesTheShareWith(t *testing.T) {
	events := append(worldEvents(),
		Event{Kind: EventMemberRemoved, At: at(30), ProjectID: pAtlas, UserID: uCarol, Actor: uAlice},
	)
	m, err := Replay(events)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	a := Resolve(m, user(uCarol))
	if got := verbsOrEmpty(a.VerbsOn(sBeaconSecrets)); got != "" {
		t.Errorf("after leaving atlas, carol has %q on beacon-secrets via the project share, want nothing", got)
	}
	if got := verbsOrEmpty(a.VerbsOn(sAtlasRunbook)); got != "" {
		t.Errorf("after leaving atlas, carol has %q on atlas-runbook, want nothing", got)
	}
	// Her PERSONAL grants survive: leaving a project revokes what the project
	// conferred and nothing else.
	if got := verbsOrEmpty(a.VerbsOn(sAtlasNotes)); got != "admin" {
		t.Errorf("after leaving atlas, carol has %q on atlas-notes, want admin — her personal grant is not the project's to revoke", got)
	}
	if got := verbsOrEmpty(a.VerbsOn(sBeaconNotes)); got != "read" {
		t.Errorf("after leaving atlas, carol has %q on beacon-notes, want read", got)
	}
}

// TestTheZeroAuthorizationPermitsNothing pins the fail-closed direction at the type
// level.
//
// 🔴 THE VALUE A CALLER GETS BY FORGETTING TO RESOLVE MUST BE THE SAFE ONE. This is
// the same property `store.ScopeSet`'s zero value carries, one level up, and it is
// what makes "a route reached without authorization sees nothing" true by
// construction rather than by review.
func TestTheZeroAuthorizationPermitsNothing(t *testing.T) {
	var a Authorization
	for _, v := range AllVerbs {
		if a.Allows(sAtlasNotes, v) {
			t.Errorf("the zero Authorization allows %s", v)
		}
	}
	if len(a.ScopeIDs(VerbRead)) != 0 {
		t.Error("the zero Authorization lists scopes")
	}
	vs := a.VisibleScopes(VerbRead)
	if vs.Unrestricted {
		t.Fatal("the zero Authorization projects to an UNRESTRICTED ScopeSet — that is the fail-OPEN direction and the one this type exists to make unreachable")
	}
	if vs.Allows("atlas-notes") {
		t.Error("the zero Authorization's ScopeSet allows a scope")
	}
}

// TestVisibleScopesNeverProjectsToUnrestricted pins the seam's narrowing direction
// for a principal that CAN see everything in the model.
//
// 🔴 THE TWO ARE OBSERVABLY IDENTICAL TODAY AND DIVERGE ON AN UNMODELLED DIRECTORY.
// `store.Unrestricted()` means "every directory under the store root", which
// includes one the control plane has no scope record for. An enumeration of the
// scopes this principal actually holds will not serve that directory; a wildcard
// will. Since a store root is a filesystem and the model is not, the two DO come
// apart — and the direction they come apart in is a cross-tenant read.
func TestVisibleScopesNeverProjectsToUnrestricted(t *testing.T) {
	// Dave owns every beacon scope; give him atlas too, so he holds literally
	// every scope in the model.
	events := append(worldEvents(),
		Event{Kind: EventGranted, At: at(30), GrantID: "grt_all", Actor: uAlice,
			SubjectKind: KindUser, SubjectID: uDave,
			ObjectKind: ObjectProject, ObjectID: pAtlas,
			Verbs: AllSet},
	)
	m, err := Replay(events)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	a := Resolve(m, user(uDave))
	if got := len(a.ScopeIDs(VerbRead)); got != len(m.Scopes) {
		t.Fatalf("dave reaches %d of %d scopes — the fixture no longer makes him universal, so this test is not measuring what it claims", got, len(m.Scopes))
	}
	vs := a.VisibleScopes(VerbRead)
	if vs.Unrestricted {
		t.Fatal("a principal holding every scope projects to UNRESTRICTED — which would also serve a directory the model has no record of")
	}
	for _, sc := range m.Scopes {
		if !vs.Allows(sc.DisplayName) {
			t.Errorf("the projected ScopeSet does not allow %q", sc.DisplayName)
		}
	}
	if vs.Allows("a-directory-no-scope-record-names") {
		t.Fatal("the projected ScopeSet allows an unmodelled directory — that is the exact divergence this test exists for")
	}
}
