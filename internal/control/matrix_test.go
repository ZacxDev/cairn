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
// So the count is pinned from BOTH sides. 39 of 144 cells allow: not 0, which would
// mean the resolver grants nothing, and not 144, which would mean it grants
// everything. Any change to the fixture moves this number and forces whoever made it
// to say what they expected.
//
// ⚠ THE DELTA FROM 32/60, STATED SO IT IS AN EXPECTATION RATHER THAN A RE-PIN: group
// federation added SEVEN federated rows (`expectedFederatedMatrix`) over the same four
// scopes — 84 cells — and exactly 7 of them allow: F1 1, F2 2, F3 3, F7 1, and F4/F5/F6
// none. The five user rows are unchanged at 32 of 60, and they are walked on the model
// that ALSO holds the group map, so a group mapping that leaked into `Resolve` would move
// a user cell rather than hide in a separate test.
const (
	expectedAllowCells = 32 + 7
	expectedTotalCells = 5*4*3 + 7*4*3
)

// The group map the federated rows resolve against. Provider `issuer` is the verifier's
// namespace; `elsewhere` is a second issuer whose mapping must never reach an `issuer`
// token, so its row is the provider check's only witness.
const (
	fedProvider = "issuer"
	fedOther    = "elsewhere"
)

// federatedWorldEvents is `worldEvents` plus a group map. Each mapping is chosen so one
// mechanism is separately observable:
//
//	readers    → atlas-notes read        a single mapping
//	writers    → atlas-notes write       a UNION across two claimed groups (F2)
//	beacon-all → PROJECT beacon read     a project object, expanded at resolve time (F3)
//	ops        → beacon-secrets rw       a union of a scope and a project mapping (F3)
//	retired    → atlas-runbook admin     mapped, then UNMAPPED — the tombstone's witness (F5)
//	readers@elsewhere → atlas-runbook    another issuer's mapping of the same group name
func federatedWorldEvents() []Event {
	return append(worldEvents(),
		Event{Kind: EventGroupMapped, At: at(40), Provider: fedProvider, Group: "readers",
			ObjectKind: ObjectScope, ObjectID: sAtlasNotes, Verbs: NewVerbSet(VerbRead)},
		Event{Kind: EventGroupMapped, At: at(41), Provider: fedProvider, Group: "writers",
			ObjectKind: ObjectScope, ObjectID: sAtlasNotes, Verbs: NewVerbSet(VerbWrite)},
		Event{Kind: EventGroupMapped, At: at(42), Provider: fedProvider, Group: "beacon-all",
			ObjectKind: ObjectProject, ObjectID: pBeacon, Verbs: NewVerbSet(VerbRead)},
		Event{Kind: EventGroupMapped, At: at(43), Provider: fedProvider, Group: "ops",
			ObjectKind: ObjectScope, ObjectID: sBeaconSecrets, Verbs: NewVerbSet(VerbRead, VerbWrite)},
		Event{Kind: EventGroupMapped, At: at(44), Provider: fedProvider, Group: "retired",
			ObjectKind: ObjectScope, ObjectID: sAtlasRunbook, Verbs: NewVerbSet(VerbAdmin)},
		Event{Kind: EventGroupUnmapped, At: at(45), Provider: fedProvider, Group: "retired",
			ObjectKind: ObjectScope, ObjectID: sAtlasRunbook},
		Event{Kind: EventGroupMapped, At: at(46), Provider: fedOther, Group: "readers",
			ObjectKind: ObjectScope, ObjectID: sAtlasRunbook, Verbs: NewVerbSet(VerbRead)},
	)
}

// federatedCase is one federated principal: what its token claims, and what it must get.
type federatedCase struct {
	name    string
	claimed []string
	status  FederationStatus
	// mapped/unmapped are the expected partition of the claim, comma-joined and sorted.
	mapped, unmapped string
	cells            map[ID]string
}

// expectedFederatedMatrix is the federated half of the ledger — every row, every scope.
var expectedFederatedMatrix = []federatedCase{
	// F1 — claimed+mapped. `readers` at THIS issuer reaches atlas-notes and nothing else;
	// the same group name at `elsewhere` reaches atlas-runbook and must not reach here.
	{"F1-one-mapped-group", []string{"readers"}, FederationMapped, "readers", "",
		map[ID]string{sAtlasNotes: "read", sAtlasRunbook: "", sBeaconNotes: "", sBeaconSecrets: ""}},
	// F2 — the UNION across two claimed groups onto one scope; the first group alone is "read".
	{"F2-union-across-groups", []string{"writers", "readers"}, FederationMapped, "readers,writers", "",
		map[ID]string{sAtlasNotes: "read,write", sAtlasRunbook: "", sBeaconNotes: "", sBeaconSecrets: ""}},
	// F3 — a project object (beacon-all) unioned with a scope mapping (ops). The first group
	// in sorted order is `beacon-all`, so a resolver that stopped there reads beacon-secrets
	// as "read".
	{"F3-project-object-and-scope-union", []string{"ops", "beacon-all"}, FederationMapped, "beacon-all,ops", "",
		map[ID]string{sAtlasNotes: "", sAtlasRunbook: "", sBeaconNotes: "read", sBeaconSecrets: "read,write"}},
	// F4 — claimed+UNMAPPED: authenticates, reaches nothing, and says why.
	{"F4-only-unmapped-groups", []string{"ghost"}, FederationGroupsUnmapped, "", "ghost",
		map[ID]string{sAtlasNotes: "", sAtlasRunbook: "", sBeaconNotes: "", sBeaconSecrets: ""}},
	// F5 — a group whose mapping was UNMAPPED. Its row is the tombstone's only witness:
	// without `group-unmapped` applying, atlas-runbook reads "admin".
	{"F5-unmapped-after-mapping", []string{"retired"}, FederationGroupsUnmapped, "", "retired",
		map[ID]string{sAtlasNotes: "", sAtlasRunbook: "", sBeaconNotes: "", sBeaconSecrets: ""}},
	// F6 — a token that claims NO group.
	{"F6-no-groups", []string{}, FederationNoGroups, "", "",
		map[ID]string{sAtlasNotes: "", sAtlasRunbook: "", sBeaconNotes: "", sBeaconSecrets: ""}},
	// F7 — a mixed claim: the mapped group counts, the unmapped one contributes nothing and
	// is reported as unmapped.
	{"F7-mapped-and-unmapped", []string{"ghost", "readers"}, FederationMapped, "readers", "ghost",
		map[ID]string{sAtlasNotes: "read", sAtlasRunbook: "", sBeaconNotes: "", sBeaconSecrets: ""}},
}

func TestTheAuthorizationMatrixIsExactlyThis(t *testing.T) {
	m, err := Replay(federatedWorldEvents())
	if err != nil {
		t.Fatalf("building the federated fixture world: %v", err)
	}

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

	// The FEDERATED rows, over the same model and the same four scopes.
	for _, fc := range expectedFederatedMatrix {
		p, a, err := ResolveFederated(m, fedProvider, "subject-"+fc.name, fc.claimed)
		if err != nil {
			t.Fatalf("%s: ResolveFederated refused a valid claim: %v", fc.name, err)
		}
		if p.Kind != KindFederated || p.Display != fedProvider+":subject-"+fc.name {
			t.Errorf("%s: principal is %+v, want a federated principal displayed as provider:subject", fc.name, p)
		}
		report, ok := a.Federation()
		if !ok {
			t.Fatalf("%s: the authority carries no federation report", fc.name)
		}
		if report.Status != fc.status ||
			strings.Join(report.Mapped, ",") != fc.mapped || strings.Join(report.Unmapped, ",") != fc.unmapped {
			t.Errorf("%s: report is status=%s mapped=[%s] unmapped=[%s], want status=%s mapped=[%s] unmapped=[%s]",
				fc.name, report.Status, strings.Join(report.Mapped, ","), strings.Join(report.Unmapped, ","),
				fc.status, fc.mapped, fc.unmapped)
		}
		if len(fc.cells) != len(m.Scopes) {
			t.Fatalf("%s: the row names %d scopes and the model holds %d — the ledger must cover the world", fc.name, len(fc.cells), len(m.Scopes))
		}
		for _, scopeID := range sortedLedgerScopes(uAlice) {
			want, named := fc.cells[scopeID]
			if !named {
				t.Fatalf("%s: the row does not name %s", fc.name, scopeID)
			}
			if got := verbsOrEmpty(a.VerbsOn(scopeID)); got != want {
				t.Errorf("ResolveFederated(%s).VerbsOn(%s) = %q, want %q", fc.name, scopeID, got, want)
			}
			for _, v := range AllVerbs {
				total++
				wantAllow := strings.Contains(","+want+",", ","+string(v)+",")
				if gotAllow := a.Allows(scopeID, v); gotAllow != wantAllow {
					t.Errorf("ResolveFederated(%s).Allows(%s, %s) = %v, want %v", fc.name, scopeID, v, gotAllow, wantAllow)
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
