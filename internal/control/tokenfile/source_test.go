package tokenfile

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/authz"
	"github.com/ZacxDev/cairn/internal/control"
)

// aToken is a synthetic 43-character credential — the real floor
// (`authz.MinTokenChars`), so a fixture cannot be shorter than the deployed shape and
// accidentally exercise a path a real token never takes.
func aToken(seed byte) string { return strings.Repeat(string(rune(seed)), authz.MinTokenChars) }

// fixedClock is the year-2000 instant every synthesized event is stamped with here.
// A token file records no times at all, so the value carries no information and is
// pinned only so two projections can be compared field for field.
var fixedClock = time.Date(2000, 1, 5, 0, 0, 0, 0, time.UTC)

// storeWith builds a store root holding one directory per name, and returns the path.
func storeWith(t *testing.T, scopes ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, s := range scopes {
		if err := os.MkdirAll(filepath.Join(root, s), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func sourceOver(root string, records ...authz.TokenRecord) Source {
	return Source{
		StoreRoot: root,
		Records:   func() []authz.TokenRecord { return records },
		Now:       func() time.Time { return fixedClock },
	}
}

func modelOf(t *testing.T, s Source) control.Model {
	t.Helper()
	m, err := s.Model(context.Background())
	if err != nil {
		t.Fatalf("the projection must build: %v", err)
	}
	return m
}

// authorityOf authenticates a token and returns what it may see and write, as sorted
// name lists — which is the form every narrowing site in the server actually consumes.
func authorityOf(t *testing.T, m control.Model, token string) (identity string, read, write []string) {
	t.Helper()
	p, auth, err := control.Authenticate(m, token)
	if err != nil {
		t.Fatalf("a configured credential must authenticate: %v", err)
	}
	return p.Display, scopeNamesOf(auth, control.VerbRead), scopeNamesOf(auth, control.VerbWrite)
}

func scopeNamesOf(auth control.Authorization, verb control.Verb) []string {
	set := auth.VisibleScopes(verb)
	var out []string
	for _, name := range []string{
		// Every name any fixture in this file uses, probed through the SAME predicate
		// the server narrows with. Probing rather than enumerating is deliberate:
		// `store.ScopeSet.Allows` is what decides visibility, so a list built any other
		// way would be a second answer to the question under test.
		"alpha-notes", "beta-notes", "hollow-set", "rubble-heap",
		"late-arrival", "not-yet-created", "dot-scope", "git",
	} {
		if set.Allows(name) {
			out = append(out, name)
		}
	}
	return out
}

// renderEvents serialises a projection's journal with the package's own writer.
func renderEvents(t *testing.T, s Source) string {
	t.Helper()
	events, err := s.Events(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var buf strings.Builder
	if err := control.WriteEvents(&buf, events); err != nil {
		t.Fatalf("a synthesized journal must be writable — every event it holds is one "+
			"a migration would append verbatim: %v", err)
	}
	return buf.String()
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// The three properties that moved here from `internal/authz/token_test.go` when
// `Authorize` and `TokenRecord.VisibleScopes` were deleted. Each is asserted against
// the mechanism that now decides it.
// ---------------------------------------------------------------------------

// TestTheAllowlistAsymmetrySurvivesTheProjection is the moved
// `TestTheAllowlistAsymmetry`.
//
// 🔴 UNRESTRICTED AND EMPTY ARE OPPOSITES, AND THE PROJECTION MUST NOT COLLAPSE THEM.
// A bare row reaches every scope; a mapped row reaches only what it names; and a record
// with NO allowlist and NO legacy mark — the shape a refactor produces by forgetting to
// set a field — must reach NOTHING. The last one is the fail-closed direction the whole
// design rests on, and it is the one an adapter could most easily get wrong, because
// "no scopes named" and "every scope" are one keystroke apart in a synthesizer.
func TestTheAllowlistAsymmetrySurvivesTheProjection(t *testing.T) {
	root := storeWith(t, "alpha-notes", "beta-notes")
	bare, mapped, forgotten := aToken('a'), aToken('b'), aToken('c')
	m := modelOf(t, sourceOver(root,
		authz.LegacyRecord(bare),
		authz.TokenRecord{Token: mapped, Identity: "reader", Scopes: []string{"alpha-notes"}},
		authz.TokenRecord{Token: forgotten, Identity: "forgotten"},
	))

	if _, read, _ := authorityOf(t, m, bare); !equal(read, []string{"alpha-notes", "beta-notes"}) {
		t.Fatalf("a bare row must reach every scope the store holds, got %v", read)
	}
	if _, read, _ := authorityOf(t, m, mapped); !equal(read, []string{"alpha-notes"}) {
		t.Fatalf("a mapped row reaches only what it names, got %v", read)
	}
	// 🔴 THE FOLD IS PART OF THE ANSWER. A scope directory spelled `Alpha-Notes` must
	// match an allowlist naming `alpha-notes`, or the caller's OWN scope is silently
	// emptied.
	if _, auth, err := control.Authenticate(m, mapped); err != nil || !auth.VisibleScopes(control.VerbRead).Allows("Alpha-Notes") {
		t.Fatalf("the comparison folds both sides (err=%v)", err)
	}
	if _, read, write := authorityOf(t, m, forgotten); len(read) != 0 || len(write) != 0 {
		t.Fatalf("a record with no allowlist and no legacy mark must confer NOTHING, "+
			"got read=%v write=%v — an empty allowlist is the OPPOSITE of unrestricted", read, write)
	}
}

// TestOneMatchYieldsTheIdentityAndTheAuthorityTogether is the moved
// `TestAuthorizeReturnsTheMatchedRecord`.
//
// 🔴 ONE MATCH, THREE FACTS — who this is, what it may see, and which credential said
// so, all out of ONE lookup. A check that returned only a fingerprint would force the
// authority lookup to be a second search keyed on something else, which is the shape
// that ends up consulting a wider table than the one that authenticated.
//
// ⚠ ONE PROPERTY IS **NOT** PINNED HERE AND IS RECORDED RATHER THAN LEFT AS AN
// UNEXPLAINED SURVIVOR, exactly as the deleted test recorded it: the absence of an early
// exit in `control.Authenticate`. The property is a TIMING one and no functional
// assertion can observe it, so the guard is the comment beside the loop.
func TestOneMatchYieldsTheIdentityAndTheAuthorityTogether(t *testing.T) {
	root := storeWith(t, "alpha-notes", "beta-notes")
	bare, mapped := aToken('a'), aToken('b')
	m := modelOf(t, sourceOver(root,
		authz.LegacyRecord(bare),
		authz.TokenRecord{Token: mapped, Identity: "reader", Scopes: []string{"alpha-notes"}},
	))

	p, auth, err := control.Authenticate(m, mapped)
	if err != nil {
		t.Fatalf("a configured credential must authenticate: %v", err)
	}
	if p.Display != "reader" {
		t.Fatalf("the identity must be the row's own, verbatim — it is what the audit "+
			"line and every written bullet's actor carry. got %q", p.Display)
	}
	if p.CredentialID != control.DerivedID(control.PrefixCredential, authz.TokenID(mapped)) {
		t.Fatalf("the credential that matched must be named on the principal, got %q", p.CredentialID)
	}
	if auth.VisibleScopes(control.VerbRead).Unrestricted {
		t.Fatal("the mapped row's authority must not be the unrestricted sentinel — " +
			"`control.Authorization` has no such value at all")
	}
	// The wrong credential, and every malformed presentation, gets the SAME refusal.
	for _, bad := range []string{"", aToken('z'), strings.ToUpper(mapped)} {
		if _, _, err := control.Authenticate(m, bad); err == nil {
			t.Fatalf("a wrong or absent credential must be refused: %q", bad)
		}
	}
	if _, _, err := control.Authenticate(m, bare); err != nil {
		t.Fatalf("the OTHER row in the same table must still authenticate: %v", err)
	}
}

// TestNoPrefixOfACredentialAuthenticates is the moved
// `TestAOneCharacterCredentialCannotAuthorize`.
//
// ⚠ AN INVARIANT GUARD, LABELLED AS ONE. The Python original refuses a bare string
// loudly because iterating one yields characters, so a single character of a token would
// have authorized; Go's type system makes that spelling impossible and the digest
// comparison makes it impossible again. Nothing here ever regressed. What it pins is
// that the property survived TWO ports — into Go, and now through a sha256 digest
// comparison rather than a constant-time string one.
func TestNoPrefixOfACredentialAuthenticates(t *testing.T) {
	root := storeWith(t, "alpha-notes")
	token := aToken('a')
	m := modelOf(t, sourceOver(root, authz.LegacyRecord(token)))
	for n := 1; n < len(token); n++ {
		if _, _, err := control.Authenticate(m, token[:n]); err == nil {
			t.Fatalf("a %d-character prefix of the credential authenticated", n)
		}
	}
}

// ---------------------------------------------------------------------------
// The projection itself.
// ---------------------------------------------------------------------------

// TestALegacyRowReachesEveryScopeAndMayWriteNone is the reconciliation the whole
// package exists for.
//
// 🔴 THE BARE ROW IS UNRESTRICTED AND `control` HAS NO UNRESTRICTED PRINCIPAL, so the
// adapter enumerates. Both halves are asserted, because only the conjunction is the
// migration: it must reach EVERY scope (or a read silently stops working) and it must
// write NONE (or the actor guarantee the write path rests on is gone).
func TestALegacyRowReachesEveryScopeAndMayWriteNone(t *testing.T) {
	root := storeWith(t, "alpha-notes", "beta-notes", "hollow-set", "rubble-heap")
	token := aToken('a')
	m := modelOf(t, sourceOver(root, authz.LegacyRecord(token)))

	identity, read, write := authorityOf(t, m, token)
	if identity != authz.LegacyIdentity {
		t.Fatalf("a bare row's identity is the constant %q, got %q", authz.LegacyIdentity, identity)
	}
	if !equal(read, []string{"alpha-notes", "beta-notes", "hollow-set", "rubble-heap"}) {
		t.Fatalf("a bare row reads every scope the store holds, got %v", read)
	}
	if len(write) != 0 {
		t.Fatalf("a bare row holds the write verb NOWHERE — that is what the server's "+
			"403 is derived from. got %v", write)
	}
	// 🔴 AND THE ENUMERATION IS NOT THE SENTINEL. A `store.Unrestricted()` here would
	// answer yes to a scope the model does not know about, which is the cross-tenant
	// read `Authorization.VisibleScopes` refuses to make possible.
	_, auth, _ := control.Authenticate(m, token)
	if auth.VisibleScopes(control.VerbRead).Unrestricted {
		t.Fatal("the projection must never produce an unrestricted set")
	}
	if auth.VisibleScopes(control.VerbRead).Allows("a-scope-that-has-no-directory") {
		t.Fatal("an enumeration must refuse a scope nobody recorded — that is the whole " +
			"difference between it and the wildcard it replaces")
	}
}

// TestAMappedRowReachesAScopeThatHasNoDirectoryYet is why the enumeration is a UNION
// rather than a directory listing.
//
// 🔴 THE FIRST-ENTRY CREATE IS HOW THE STORE GAINED EVERY SCOPE IT HAS. `PUT` with
// `If-None-Match: *` writes into a scope with no directory, and the only place that
// scope is written down beforehand is the token file's own allowlist. An adapter that
// enumerated only what exists on disk would take that verb away from a mapped row, and
// no test over an already-populated fixture store would see it.
func TestAMappedRowReachesAScopeThatHasNoDirectoryYet(t *testing.T) {
	root := storeWith(t, "alpha-notes")
	token := aToken('b')
	m := modelOf(t, sourceOver(root,
		authz.TokenRecord{Token: token, Identity: "writer", Scopes: []string{"alpha-notes", "not-yet-created"}}))

	_, read, write := authorityOf(t, m, token)
	if !equal(read, []string{"alpha-notes", "not-yet-created"}) {
		t.Fatalf("a mapped row reaches every scope it names, directory or not: %v", read)
	}
	if !equal(write, []string{"alpha-notes", "not-yet-created"}) {
		t.Fatalf("and may write all of them: %v", write)
	}
}

// TestTheProjectionIsAPureFunctionOfItsInputs is what makes re-materializing a no-op.
//
// 🔴 A RANDOM ID PER REFRESH WOULD MAKE EVERY SIGHUP A RENAME. `control.DerivedID` is
// why the ids, the grants and the epoch are the same for the same world; without it a
// `Scope` would stop being the same scope across a reload and the staleness epoch would
// move on every tick with nothing having changed.
func TestTheProjectionIsAPureFunctionOfItsInputs(t *testing.T) {
	// 🔴 THE INSERTION ORDER IS REVERSE-SORTED ON PURPOSE. `scopeNames` seeds its set
	// from the records BEFORE the directories, so naming them descending here makes the
	// map's own order the OPPOSITE of the answer — which is what turns the sortedness
	// check below from a coin flip into a measurement.
	//
	// 🔴 AND THE ALLOWLIST NAMES NEITHER EVERY DIRECTORY NOR ONLY DIRECTORIES, WHICH IS
	// A FIX RATHER THAN A DETAIL. An earlier fixture gave this record all four directory
	// names, and the union then had a redundant half: deleting `storeDirs()` outright
	// left this test GREEN, because the allowlist alone still supplied every name. The
	// record now names two of the four directories plus one scope that has NO directory,
	// so the count below is 5 and BOTH halves are load-bearing — dropping the store half
	// yields 3, dropping the allowlist half yields 4, and each is this test failing on
	// its own precondition.
	root := storeWith(t, "rubble-heap", "alpha-notes", "hollow-set", "beta-notes")
	token := aToken('a')
	src := sourceOver(root, authz.TokenRecord{
		Token:    token,
		Identity: "reader",
		Scopes:   []string{"rubble-heap", "hollow-set", "epsilon-notes"},
	})

	// 🔴 THE ORDER IS ASSERTED DIRECTLY AND REPEATEDLY, AND THE BATTERY IS WHAT FORCED
	// BOTH. Comparing two projections is the CONTRACT, but Go randomises map iteration,
	// so an unsorted enumeration still agrees with itself — and still comes out sorted —
	// often enough that `projection-loses-its-order` SURVIVED twice: once against the
	// comparison alone, and once against a single sortedness check over four names.
	// A guard whose kill is probabilistic is a flaky guard in both directions.
	//
	// Two things make it deterministic in practice. The fixture's insertion order is
	// REVERSE-sorted, so the unsorted answer is the likely one rather than the unlucky
	// one; and the check runs `orderSamples` times, so the probability that an unsorted
	// enumeration passes is the per-iteration probability raised to that power. It is
	// stated as a bound rather than claimed as a certainty, because the only way to make
	// it a certainty would be to depend on Go's map internals, which are not a contract.
	const orderSamples = 24
	for i := 0; i < orderSamples; i++ {
		events, err := src.Events(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		var created []string
		for _, e := range events {
			if e.Kind == control.EventScopeCreated {
				created = append(created, e.DisplayName)
			}
		}
		// The precondition is the UNION's size: four directories and an allowlist
		// naming two of them plus one scope with no directory. A number that is not 5
		// means one half of the union stopped contributing, which is the defect this
		// fixture was rebuilt to see.
		if len(created) != 5 {
			t.Fatalf("precondition: five scopes must be enumerated — four directories "+
				"unioned with an allowlist naming two of them plus `epsilon-notes`, "+
				"which has none — got %d (%v)", len(created), created)
		}
		if !sort.StringsAreSorted(created) {
			t.Fatalf("iteration %d: the scope events must be emitted in a fixed order, got "+
				"%v — an unsorted enumeration makes the same world produce two different "+
				"journals, which in this repository is the failure that reads as a stale "+
				"cache", i, created)
		}
	}

	// Compared through the package's OWN serializer rather than field by field: an
	// `Event` carries a slice and is not `==`-comparable, and a hand-written comparison
	// would be a second spelling of "are these the same event" that a new field would
	// silently fall out of.
	if a, b := renderEvents(t, src), renderEvents(t, src); a != b {
		t.Fatalf("two projections of one world emitted different journals:\n--- first\n%s\n--- second\n%s", a, b)
	}

	first, second := modelOf(t, src), modelOf(t, src)
	if first.Epoch != second.Epoch {
		t.Fatalf("two projections of one world must carry one epoch, got %d and %d", first.Epoch, second.Epoch)
	}
	if len(first.Scopes) != len(second.Scopes) || len(first.Scopes) == 0 {
		t.Fatalf("the scope set must be the same and non-empty: %d vs %d", len(first.Scopes), len(second.Scopes))
	}
	for id := range first.Scopes {
		if _, same := second.Scopes[id]; !same {
			t.Fatalf("scope id %s was not minted the second time — the id moved with nothing having changed", id)
		}
	}
	for id := range first.Grants {
		if _, same := second.Grants[id]; !same {
			t.Fatalf("grant id %s was not minted the second time", id)
		}
	}
}

// TestTwoBareRowsAreOnePrincipalWithTwoCredentials is the rotation shape.
//
// 🔴 `authz.LoadTokens` GUARD 12 EXEMPTS `legacy` BECAUSE TWO BARE ROWS ARE AN OVERLAP
// ROTATION OF ONE HOLDER. A synthesizer that minted a project per ROW would emit
// `project-created` twice for one name, `apply` would refuse it, and `Replay` fails
// whole — so the whole authority would go dark on the one file shape rotation
// prescribes. Measured rather than reasoned about.
func TestTwoBareRowsAreOnePrincipalWithTwoCredentials(t *testing.T) {
	root := storeWith(t, "alpha-notes")
	current, previous := aToken('a'), aToken('b')
	m := modelOf(t, sourceOver(root, authz.LegacyRecord(current), authz.LegacyRecord(previous)))

	if len(m.Credentials) != 2 {
		t.Fatalf("both rows are live credentials, got %d", len(m.Credentials))
	}
	first, _, err := control.Authenticate(m, current)
	if err != nil {
		t.Fatalf("the current credential must authenticate: %v", err)
	}
	second, _, err := control.Authenticate(m, previous)
	if err != nil {
		t.Fatalf("the previous credential must authenticate during the overlap: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("two bare rows are ONE holder, got principals %s and %s", first.ID, second.ID)
	}
	if first.CredentialID == second.CredentialID {
		t.Fatal("…and TWO credentials: the fingerprints are what an operator greps for " +
			"to know which line to delete")
	}
}

// TestAnUnreadableStoreRootStillAuthenticatesEveryRow pins the deliberate leniency.
//
// 🔴 AN UNREADABLE STORE MUST NOT BECOME AN UNREADABLE CREDENTIAL TABLE. Every read
// route answers 503 `store-unreachable` when the root cannot be read — before any
// narrowing is consulted — so a principal's visible set is not observable through one.
// Refusing to project would instead stop the pod authenticating ANYBODY, which is a
// strictly wider outage than the one that is actually happening.
func TestAnUnreadableStoreRootStillAuthenticatesEveryRow(t *testing.T) {
	bare, mapped := aToken('a'), aToken('b')
	src := sourceOver(filepath.Join(t.TempDir(), "no-such-store"),
		authz.LegacyRecord(bare),
		authz.TokenRecord{Token: mapped, Identity: "reader", Scopes: []string{"alpha-notes"}})
	m := modelOf(t, src)

	if _, _, err := control.Authenticate(m, bare); err != nil {
		t.Fatalf("the bare row must still authenticate: %v", err)
	}
	// 🔴 AND THE MAPPED ROW KEEPS ITS ALLOWLIST, because that is written in the FILE
	// rather than discovered on the disk.
	if _, read, _ := authorityOf(t, m, mapped); !equal(read, []string{"alpha-notes"}) {
		t.Fatalf("a mapped row's authority does not depend on the store root, got %v", read)
	}
}

// TestTwoDirectoriesThatFoldTogetherDoNotTakeTheAuthorityDown is an availability guard
// over the synthesizer, and it has TWO halves because a mutation sweep proved one of
// them unreachable.
//
// 🔴 HALF ONE — TWO SPELLINGS ARE ONE SCOPE. `Alpha_Notes` and `alpha-notes` are two
// directories and one folded name, so the projection must record one scope. What made
// this half insufficient on its own: `apply` compares DISPLAY names, which are not equal
// before folding, so an unfolded enumeration produces two scope records and the probe
// still answers yes — `store.ScopeSet` folds its probe, so both names reach the same
// cell. The guard read as coverage and provided none.
//
// 🔴 HALF TWO — THE ENUMERATION AND THE GRANT MUST FOLD THE SAME WAY, and this is the
// half that can actually go red. A mapped row naming `Alpha_Notes` with NO directory of
// that name puts the only record of that scope in the allowlist; `grantsFor` folds when
// it derives the grant's object id, so an enumeration that did NOT fold records
// `Alpha_Notes` and the grant names `alpha-notes`, which does not exist. `apply` refuses
// the grant and `Replay` fails whole — the pod stops authenticating anybody, on a token
// file one operator typed with an underscore.
func TestTwoDirectoriesThatFoldTogetherDoNotTakeTheAuthorityDown(t *testing.T) {
	root := storeWith(t, "alpha-notes", "Alpha_Notes")
	token := aToken('a')
	m := modelOf(t, sourceOver(root, authz.LegacyRecord(token)))
	if _, read, _ := authorityOf(t, m, token); !equal(read, []string{"alpha-notes"}) {
		t.Fatalf("the two spellings are one scope, got %v", read)
	}

	// Half two. The allowlist is the ONLY place this scope is written down, and it is
	// spelled in a form that folds — which is what a programmatic record carries, since
	// `authz.ParseTokenRow` folds a row read from a file and nothing folds one built in
	// code.
	mapped := aToken('b')
	bare := storeWith(t)
	m2, err := sourceOver(bare,
		authz.TokenRecord{Token: mapped, Identity: "reader", Scopes: []string{"Alpha_Notes"}},
	).Model(context.Background())
	if err != nil {
		t.Fatalf("a grant must name a scope the projection recorded — the enumeration "+
			"and the grant have to fold the same way or the WHOLE authority fails to "+
			"build: %v", err)
	}
	if _, auth, err := control.Authenticate(m2, mapped); err != nil || !auth.VisibleScopes(control.VerbRead).Allows("alpha-notes") {
		t.Fatalf("…and the row must reach the scope it named (err=%v)", err)
	}
}

// TestTheProjectionMintsNoUnrestrictedPrincipal is an INVARIANT GUARD, labelled as one.
//
// Nothing ever regressed here — `control.Authorization` has no unrestricted value to
// mint. What it pins is that the adapter did not smuggle one back in by some other
// route: no principal it creates may answer yes to a scope the model does not hold.
// It is the one claim a reader is most likely to want checked after reading this
// package's opening comment, and "it cannot be expressed" is not the same as
// "it was measured".
func TestTheProjectionMintsNoUnrestrictedPrincipal(t *testing.T) {
	root := storeWith(t, "alpha-notes")
	bare, mapped := aToken('a'), aToken('b')
	m := modelOf(t, sourceOver(root,
		authz.LegacyRecord(bare),
		authz.TokenRecord{Token: mapped, Identity: "reader", Scopes: []string{"alpha-notes"}}))

	for _, token := range []string{bare, mapped} {
		_, auth, err := control.Authenticate(m, token)
		if err != nil {
			t.Fatal(err)
		}
		for _, verb := range control.AllVerbs {
			set := auth.VisibleScopes(verb)
			if set.Unrestricted {
				t.Fatalf("%s is unrestricted for %s", token[:4], verb)
			}
			if set.Allows("a-scope-nobody-recorded") {
				t.Fatalf("%s reaches an unmodelled scope for %s", token[:4], verb)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// The divergence, MEASURED.
// ---------------------------------------------------------------------------

// TestAScopeCreatedAfterMaterializationIsInvisibleUntilTheNextRefresh is the one
// behaviour this adapter does not reproduce, and this test is the measurement rather
// than the argument.
//
// 🔴 TWO ANSWERS, BOTH OBSERVED HERE, FOR THE SAME REQUEST AGAINST THE SAME DISK.
// Today `store.Unrestricted()` is a sentinel evaluated per request, so a directory that
// appeared one millisecond ago is readable by the next request. An enumeration is a
// claim about a moment, so it is readable after the next materialization. The test
// pins BOTH: invisible at the old epoch, visible at the new one, with nothing on disk
// changing between the two reads.
//
// ⚠ IT IS SCOPED TO A BARE ROW. A mapped row is unaffected — its allowlist is in the
// file — and a scope created THROUGH this server is unaffected, because the creating
// row is mapped and the scope was therefore in the model before the directory existed.
// What is left is a directory created out of band, which is `server/seed.sh` — and the
// runbook does NOT follow that with a reload, so what bounds the window is the refresh
// TIMER. See the divergence declared in this package's doc, and
// `cmd/cairn-server`'s `TestTheBinarysOwnTimerIsWhatClosesTheDivergence`, which is what
// gates the timer itself.
func TestAScopeCreatedAfterMaterializationIsInvisibleUntilTheNextRefresh(t *testing.T) {
	root := storeWith(t, "alpha-notes")
	token := aToken('a')
	src := sourceOver(root, authz.LegacyRecord(token))

	before := modelOf(t, src)
	if _, read, _ := authorityOf(t, before, token); !equal(read, []string{"alpha-notes"}) {
		t.Fatalf("precondition: %v", read)
	}

	// The out-of-band create. Nothing tells the projection.
	if err := os.MkdirAll(filepath.Join(root, "late-arrival"), 0o755); err != nil {
		t.Fatal(err)
	}

	// ANSWER ONE: the materialized model still says no. This is the divergence.
	if _, read, _ := authorityOf(t, before, token); equal(read, []string{"alpha-notes", "late-arrival"}) {
		t.Fatal("the materialized model must NOT have noticed — if it did, the whole " +
			"premise that reads never touch the authority is false and this test is " +
			"measuring the wrong thing")
	}
	_, auth, _ := control.Authenticate(before, token)
	if auth.VisibleScopes(control.VerbRead).Allows("late-arrival") {
		t.Fatal("answer one must be `no` at the epoch that was materialized before the create")
	}

	// ANSWER TWO: re-materialize, and the same request against the same disk says yes.
	after := modelOf(t, src)
	if after.Epoch <= before.Epoch {
		t.Fatalf("the new scope must move the epoch: %d then %d — an epoch that did not "+
			"move would make the staleness report unable to show the difference",
			before.Epoch, after.Epoch)
	}
	if _, read, _ := authorityOf(t, after, token); !equal(read, []string{"alpha-notes", "late-arrival"}) {
		t.Fatalf("answer two must be `yes` after the refresh, got %v", read)
	}
}

// TestARecordWithNoIdentityIsREFUSEDRatherThanProjected states a behaviour change that
// is unreachable from a token FILE and reachable from a programmatic caller.
//
// 🔴 A ROW WHOSE HOLDER CANNOT BE NAMED MUST NOT AUTHENTICATE, AND THE REFUSAL IS THE
// WHOLE PROJECTION RATHER THAN THE ROW. `Principal.Display` is what the audit line's
// `identity=` field and every written bullet's ACTOR carry, so a principal with no name
// is a credential whose use cannot be attributed — the same condition the bare row's
// write refusal exists for, one level worse. `apply` refuses a `project-created` with an
// empty name and `Replay` fails whole, so `api.New` refuses to START.
//
// ⚠ IT IS UNREACHABLE FROM THE DEPLOYED SHAPE, and saying so is half the point.
// `authz.ParseTokenRow` gives every row an identity — `legacy` for a bare one, a
// validated non-empty string for a mapped one — so no token file can produce this. What
// can is a `TokenRecord` built in code, which is what the api tests do. The direction is
// the safe one (refuse to serve rather than serve an unattributable credential) and it
// is louder than the old behaviour, which was an empty `identity=` field in the audit
// stream and a bullet attributed to nobody.
func TestARecordWithNoIdentityIsREFUSEDRatherThanProjected(t *testing.T) {
	root := storeWith(t, "alpha-notes")
	_, err := sourceOver(root,
		authz.TokenRecord{Token: aToken('a'), Scopes: []string{"alpha-notes"}},
	).Model(context.Background())
	if err == nil {
		t.Fatal("a record with no identity must not project into a principal: its use " +
			"could not be attributed in the audit line or in a written bullet")
	}
	if !strings.Contains(err.Error(), "token-file authority") {
		t.Fatalf("the refusal must name where it came from, got: %v", err)
	}
}
