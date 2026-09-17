package identity

// The gate for P5's first slice: a session authenticates against an authority a user was
// CREATED in, with authority it can actually use.
//
// 🔴 WHY A TEST THAT ONLY ASSERTS "IT AUTHENTICATED" WOULD HAVE PASSED BEFORE THIS SLICE
// EXISTED, AND IS THEREFORE THE WRONG ASSERTION. Both session backends resolve a
// principal out of a `control.Model` and carry `control.Resolve`'s answer beside it. The
// authority every binary wired was `tokenfile.Source`, which mints ONE synthetic user at
// provider `cairn-token-file`/subject `operator`, emits no `member-set`, and makes every
// grant a `KindProject` subject. Aim a trusted-header backend at that exact pair and it
// resolves: `PrincipalFor` finds the row, `Authenticate` returns a nil error, `Valid()` is
// true — and `Auth` is EMPTY, because the user is a member of nothing and no grant names
// them. Every read then answers as if the scopes did not exist. So these tests assert the
// CONTENT of the authorization: a named scope, a named verb, and a scope that must NOT be
// reachable so "allows everything" cannot pass either.
//
// 🔴 AND THEY GO THROUGH `FromEnvironment` RATHER THAN CONSTRUCTING A BACKEND DIRECTLY,
// BECAUSE THE DEFECT WAS IN THE WIRING AND NOT IN THE BACKENDS. `supabase_test.go` and
// `trustedheader_test.go` already build backends over a hand-made `control.Model` and
// assert real authority — they were green through the whole period in which no deployment
// could authenticate anybody. What was missing was a way for the environment to hand the
// session backends an authority that holds users, which is the parameter under test here.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/authz"
	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/control/tokenfile"
)

// provisionedJournal creates a user through `control.ProvisionUser` — the operator path —
// and returns a materialized authority over the journal it wrote.
//
// ⚠ IT WRITES A REAL FILE AND REPLAYS IT, RATHER THAN HANDING BACK A `Model` BUILT IN
// MEMORY. The in-memory version is what `fixtureModel` already does, and it cannot see the
// half of this slice that is about a journal: that the events the provisioning path emits
// are ones `ReadEvents`/`Replay` accept, in an order `apply` accepts, and that the world
// they project has the membership the authorization depends on.
func provisionedJournal(t *testing.T, scopes ...string) (*control.Cache, control.Provisioned) {
	t.Helper()
	store, err := control.OpenFileStore(filepath.Join(t.TempDir(), "control.journal"))
	if err != nil {
		t.Fatalf("opening a journal: %v", err)
	}
	made, err := control.ProvisionUser(context.Background(), store, control.NewUser{
		Provider:    testProvider,
		Subject:     testSubject,
		Email:       testEmail,
		ProjectName: "quarry",
		ScopeNames:  scopes,
		At:          testClock,
	})
	if err != nil {
		t.Fatalf("provisioning a user: %v", err)
	}
	// Read through `ReloadingSource`, which is what the pod uses, so the test exercises
	// the same read path rather than a shorter one that happens to work.
	cache := control.NewCache(control.ReloadingSource{Store: store},
		control.CacheOptions{Now: fixedNow})
	if err := cache.Refresh(context.Background()); err != nil {
		t.Fatalf("materializing the journal: %v", err)
	}
	return cache, made
}

// tokenFileAuthority is the authority every deployment had before this slice: the
// projection of a token file, materialized.
//
// 🔴 IT IS THE NEGATIVE CONTROL FOR THIS WHOLE FILE, AND IT IS THE MEASUREMENT BEHIND THE
// WORD "INERT". Without it "a session authenticated and reached its scope" is a claim
// about a test fixture; with it the same configuration, differing only in which authority
// the session backends were handed, is shown to authenticate NOBODY.
func tokenFileAuthority(t *testing.T) *control.Cache {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "quarry-notes"), 0o755); err != nil {
		t.Fatalf("seeding a scope directory: %v", err)
	}
	// A MAPPED row, which is what a modern token file holds: an identity and an
	// allowlist. It is what makes the projection non-empty, so the refusal below is
	// about the PROVIDER namespace rather than about an authority that failed to build.
	records := []authz.TokenRecord{{
		Token:    strings.Repeat("s", authz.MinTokenChars),
		Identity: "ci",
		Scopes:   []string{"quarry-notes"},
	}}
	cache := control.NewCache(tokenfile.Source{
		StoreRoot: root,
		Records:   func() []authz.TokenRecord { return records },
	}, control.CacheOptions{Now: fixedNow})
	if err := cache.Refresh(context.Background()); err != nil {
		t.Fatalf("materializing the token-file projection: %v", err)
	}
	return cache
}

// liveClaims is `defaultClaims` stamped against the REAL clock.
//
// 🔴 THE PACKAGE'S YEAR-2000 FIXTURE CLOCK CANNOT BE USED IN THESE TESTS, AND THE REASON
// IS A PROPERTY OF THE THING UNDER TEST RATHER THAN A CONVENIENCE. `SupabaseConfig.Now`
// has no environment variable — deliberately: a pod verifies against the host's clock —
// so a backend built by `FromEnvironment` compares `exp` to `time.Now()`. A token minted
// at `testClock` is then a quarter-century expired, and the refusal would be about the
// clock rather than about the authority. The DATE is still not a fixture literal, which
// is what the repository's synthetic-date rule is about.
func liveClaims() claimSet {
	now := time.Now().UTC()
	claims := defaultClaims()
	claims["iat"] = now.Add(-time.Minute).Unix()
	claims["exp"] = now.Add(time.Hour).Unix()
	return claims
}

// TestAnOperatorProvisionedSupabaseSessionAuthenticatesWithREALAuthority is the closing
// condition of P5's first slice.
func TestAnOperatorProvisionedSupabaseSessionAuthenticatesWithRealAuthority(t *testing.T) {
	const scope = "quarry-notes"
	sessions, made := provisionedJournal(t, scope)
	signer := newRSASigner(t, "rsa-1", 2048)
	idp := newJWKSServer(t, jwksDocumentOf(t, signer.publicJWK(t)))

	chain, supabase, err := FromEnvironment(map[string]string{
		EnvSupabaseIssuer:  testIssuer,
		EnvSupabaseJWKSURL: idp.url(),
	}, newTestAuthority(t), sessions)
	if err != nil {
		t.Fatalf("a Supabase deployment over a control journal must build: %v", err)
	}
	if supabase == nil {
		t.Fatal("no Supabase backend was returned, so nothing could refresh its keys")
	}
	if err := supabase.RefreshKeys(t.Context()); err != nil {
		t.Fatalf("the loopback JWKS must materialize: %v", err)
	}

	who, err := chain.Authenticate(bearer(t, signer.sign(t, liveClaims(), nil)))
	if err != nil {
		t.Fatalf("a verified session for a PROVISIONED user must authenticate: %v", err)
	}
	if who.Principal.Kind != control.KindUser {
		t.Fatalf("a session resolves to a USER principal, got %q", who.Principal.Kind)
	}
	if who.Principal.ID != made.User {
		t.Fatalf("principal id = %q, want the id `ProvisionUser` minted, %q", who.Principal.ID, made.User)
	}
	if who.Principal.Display != testEmail {
		t.Fatalf("display = %q, want the control plane's own record %q", who.Principal.Display, testEmail)
	}

	// 🔴 THE ASSERTION THE CLOSING CONDITION IS ABOUT. An empty `Authorization` is what
	// every configuration produced before this slice, and it satisfies every check above.
	assertReachesExactly(t, who, made, scope)
}

// TestAnOperatorProvisionedTrustedHeaderSessionAuthenticatesWithRealAuthority is the same
// claim through the OTHER session backend.
//
// ⚠ TWO BACKENDS, BECAUSE "the session backends resolve against the configured authority"
// IS A CLAIM ABOUT A SET AND THE FALLBACK IS APPLIED ONCE FOR BOTH. One measurement would
// leave the other backend's plumbing asserted by nothing — and the trusted-header one is
// the backend whose blast radius `TrustedHeader`'s own comment calls the most dangerous
// thing in P4.
func TestAnOperatorProvisionedTrustedHeaderSessionAuthenticatesWithRealAuthority(t *testing.T) {
	const scope = "quarry-notes"
	sessions, made := provisionedJournal(t, scope)

	chain, supabase, err := FromEnvironment(map[string]string{
		EnvProxyFronted:       "yes",
		EnvProxySubjectHeader: testSubjectHeader,
		EnvProxySecret:        string(testProxySecret),
		EnvProxyProvider:      testProvider,
	}, newTestAuthority(t), sessions)
	if err != nil {
		t.Fatalf("a proxy-fronted deployment over a control journal must build: %v", err)
	}
	if supabase != nil {
		t.Fatal("no Supabase variable was set and a Supabase backend was built")
	}

	who, err := chain.Authenticate(proxyRequest(t, map[string]string{
		testSubjectHeader:        testSubject,
		DefaultProxySecretHeader: string(testProxySecret),
	}, "192.0.2.10:4444"))
	if err != nil {
		t.Fatalf("a proxy-established identity for a PROVISIONED user must authenticate: %v", err)
	}
	if who.Principal.ID != made.User {
		t.Fatalf("principal id = %q, want %q", who.Principal.ID, made.User)
	}
	assertReachesExactly(t, who, made, scope)
}

// TestTheSameConfigurationOverTheTOKENFILEProjectionAuthenticatesNobody is the
// measurement the word "inert" rests on.
//
// 🔴 IT IS THE SAME ENVIRONMENT AND THE SAME TOKEN AS THE SUPABASE TEST ABOVE, DIFFERING
// ONLY IN WHICH AUTHORITY THE SESSION BACKENDS WERE HANDED. That makes it a controlled
// comparison rather than two separate facts: whatever else changed between the two runs,
// it was not the token, the issuer, the key set or the provider.
//
// ⚠ IT IS AN INVARIANT GUARD GOING FORWARD, NOT REGRESSION COVERAGE, AND IS LABELLED AS
// ONE. The behaviour it pins is what the code did before this change and still does — a
// session for a subject no authority holds is refused. What earns it a place is that it is
// the only assertion in this file that could distinguish "the journal wiring works" from
// "the fixture would have worked either way".
func TestTheSameConfigurationOverTheTokenFileProjectionAuthenticatesNobody(t *testing.T) {
	signer := newRSASigner(t, "rsa-1", 2048)
	idp := newJWKSServer(t, jwksDocumentOf(t, signer.publicJWK(t)))
	projection := tokenFileAuthority(t)

	// Sanity: the projection is not empty. Without this the refusal below would be
	// satisfied by an authority that failed to materialize at all.
	if len(projection.Model().Users) != 1 {
		t.Fatalf("the token-file projection holds %d users, want exactly the one synthetic operator",
			len(projection.Model().Users))
	}
	if _, held := projection.Model().UserByProviderSubject(tokenfile.Provider, tokenfile.OperatorSubject); !held {
		t.Fatal("the token-file projection does not hold its own synthetic user, so this control measures nothing")
	}

	chain, supabase, err := FromEnvironment(map[string]string{
		EnvSupabaseIssuer:  testIssuer,
		EnvSupabaseJWKSURL: idp.url(),
	}, projection, nil)
	if err != nil {
		t.Fatalf("the pre-slice wiring must still build: %v", err)
	}
	if err := supabase.RefreshKeys(t.Context()); err != nil {
		t.Fatalf("the loopback JWKS must materialize: %v", err)
	}
	if _, err := chain.Authenticate(bearer(t, signer.sign(t, liveClaims(), nil))); err == nil {
		t.Fatal("a verified Supabase session resolved against the TOKEN-FILE projection, " +
			"which holds no user any identity provider can name")
	}
}

// TestASessionAuthorityNobodyReadsRefusesToStart.
//
// 🔴 A JOURNAL WITH NO SESSION BACKEND IS THE "CAME UP HEALTHY AND ANSWERS NOTHING" SHAPE
// EVERY LEDGER IN `config.go` REFUSES, ONE LEVEL OVER. An operator who provisioned users
// and forgot the Supabase block gets a pod in which nothing reads the journal and every
// sign-in is refused by the machine-token backend alone — indistinguishable, from outside,
// from an empty journal or a wrong subject.
func TestASessionAuthorityNobodyReadsRefusesToStart(t *testing.T) {
	sessions, _ := provisionedJournal(t, "quarry-notes")

	_, _, err := FromEnvironment(map[string]string{}, newTestAuthority(t), sessions)
	if !errors.Is(err, ErrSessionAuthorityUnread) {
		t.Fatalf("the WRONG guard fired.\n  got:  %v\n  want: %v", err, ErrSessionAuthorityUnread)
	}

	// The positive control for the refusal: the SAME authority, with a session backend
	// configured, builds. Without it the arm above is satisfied by a `FromEnvironment`
	// that refuses every non-nil `sessions`.
	if _, _, err := FromEnvironment(map[string]string{
		EnvProxyFronted:       "yes",
		EnvProxySubjectHeader: testSubjectHeader,
		EnvProxySecret:        string(testProxySecret),
		EnvProxyProvider:      testProvider,
	}, newTestAuthority(t), sessions); err != nil {
		t.Fatalf("a session authority WITH a backend that reads it must build: %v", err)
	}

	// And a nil session authority with nothing configured is still the default chain —
	// the compatibility claim, asserted here too because this refusal is the one edit
	// that could have broken it.
	chain, _, err := FromEnvironment(map[string]string{}, newTestAuthority(t), nil)
	if err != nil || len(chain) != 1 {
		t.Fatalf("no journal and nothing configured must stay machine-token-only: %v / %d", err, len(chain))
	}
}

// assertReachesExactly pins the CONTENT of a resolved authorization: the provisioned scope
// at every verb the owner role carries, and nothing outside it.
//
// 🔴 "NON-EMPTY" ALONE IS NOT ENOUGH IN THE OTHER DIRECTION EITHER. A resolver that
// returned authority over every scope in the model would satisfy a non-emptiness check
// while being the cross-tenant read `internal/control` exists to prevent, so a scope the
// user was NOT given is asserted unreachable. The unreachable scope is one the fixture
// world (`newTestAuthority`) holds and the journal does not, which makes it a name that
// exists somewhere — a name that exists nowhere would be refused by a resolver that
// confuses the two worlds as well as by a correct one.
func assertReachesExactly(t *testing.T, who Identity, made control.Provisioned, name string) {
	t.Helper()
	if len(made.Scopes) != 1 || made.Scopes[0].Name != name {
		t.Fatalf("the fixture did not provision exactly %q: %+v", name, made.Scopes)
	}
	scope := made.Scopes[0].ID

	for _, verb := range control.AllVerbs {
		if !who.Auth.Allows(scope, verb) {
			t.Fatalf("the provisioned owner cannot %s their own scope %s (%q) — the authorization is "+
				"empty or narrower than RoleOwner, which is what EVERY deployment produced before a "+
				"journal-backed authority could be wired", verb, scope, name)
		}
	}
	// The name-based projection is what the reader actually narrows on, so the id-keyed
	// assertion above is not the whole claim: a scope id with no `names` entry would pass
	// it and still serve nothing.
	visible := who.Auth.VisibleScopes(control.VerbRead)
	if !visible.Allows(name) {
		t.Fatalf("the authorization does not project onto the scope NAME %q, which is what the reader "+
			"narrows the store root's directories by: %v", name, visible)
	}

	// The negative half.
	stranger := control.DerivedID(control.PrefixScope, "quarry-notes")
	if stranger == scope {
		t.Fatal("the unreachable-scope control collided with the provisioned scope id, so it measures nothing")
	}
	if who.Auth.Allows(stranger, control.VerbRead) {
		t.Fatalf("the provisioned user reaches scope %s, which nothing in their journal grants them", stranger)
	}
	if visible.Allows("a-scope-nobody-provisioned") {
		t.Fatal("the visible set answers yes to a name no scope carries — it is an unrestricted sentinel rather than an enumeration")
	}
}
