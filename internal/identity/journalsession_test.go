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
//
// # HOW TO RE-DERIVE THE RED, RATHER THAN TRUST THIS FILE'S CLAIM TO HAVE SEEN IT
//
// 🔴 THE FIRST ROUND'S "RED AT THE BASE" WAS TAKEN WITH SCRATCH FILES THAT WERE NEVER
// COMMITTED, SO THE CLAIM WAS UNREPRODUCIBLE FROM THE REPOSITORY. That is the failure
// this block exists to close: the steps are here, in the tree, keyed to exact commits and
// exact edits, so a later reader re-runs them instead of believing a sentence.
//
// The two bases, and what each is the base FOR:
//
//   - `229c142` — the last commit before the user-creation path. `FromEnvironment` takes
//     TWO parameters there, so these tests do not compile against it; the measurement that
//     commit supports is the one `internal/control/README.md` records — no non-test file
//     under `cmd/` or `internal/` constructs a `control.FileStore`, so the only authority
//     any binary wires is `tokenfile.Source`, and a trusted-header backend aimed at its
//     one synthetic pair authenticates with ZERO readable scopes. Reproduce it by
//     enumerating `OpenFileStore` over that tree; `filestore_test.go` and `cache_test.go`
//     are the positive control that the sweep can hit.
//
//   - `e11c3a7` — the user-creation path, with the session-authority parameter and
//     `ErrSessionAuthorityUnread`, but WITHOUT the mirror refusal. That is the base for
//     `TestASessionBackendWithNoSessionAuthorityRefusesToStart`: at that commit an armed
//     session backend with a nil session authority returns err=nil and a two-backend
//     chain. Measured red on BOTH of that test's arms.
//
// The mutations that reproduce each red on the CURRENT tree, which is the cheaper route
// and the one the battery runs on every CI `go` job — `python3 tests/control_mutants.py
// --show` prints them exactly:
//
//   - `a-session-backend-with-no-authority-comes-up-quietly` — `internal/identity/config.go`,
//     `if (supabaseArmed || proxyArmed) && sessions == nil {` → `if false {`. Kills
//     `TestASessionBackendWithNoSessionAuthorityRefusesToStart`.
//   - `the-supabase-backend-resolves-against-the-token-file-authority-again` and its
//     trusted-header twin — the `sessions` argument at each constructor call replaced by
//     `authority`. Those two kill the two `…AuthenticatesWithRealAuthority` tests, which
//     is the wiring half of the slice.
//   - `membership-omitted-from-the-provisioning-batch` — `internal/control/provision.go`,
//     the `EventMemberSet` element dropped from the batch. That is the one worth running
//     by hand at least once: every structural check still passes and the authorization is
//     empty, which is the exact state the whole slice exists to leave behind.

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
	// Read through a `Cache` over the `FileStore` itself, which is what the pod does, so
	// the test exercises the same read path rather than a shorter one that happens to work.
	cache := control.NewCache(store, control.CacheOptions{Now: fixedNow})
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
//
// 🔴 AND IT NOW NAMES THE PROJECTION AS THE SESSION AUTHORITY EXPLICITLY, WHICH IS A
// CHANGE IN WHAT THE TEST IS ABOUT. It used to pass `nil` and let `FromEnvironment` fall
// back to the token authority — the same silent substitution a real deployment got. That
// configuration is now a refusal to start (`ErrSessionBackendWithoutAuthority`, gated by
// `TestASessionBackendWithNoSessionAuthorityRefusesToStart`), so reaching this state at
// all takes an operator deliberately handing the projection over as a session authority,
// which nothing in `cmd/cairn-server` does. The measurement is kept because it is what
// the word "inert" rests on; what is gone is the wiring that produced it by accident.
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
	}, projection, projection)
	if err != nil {
		t.Fatalf("the projection named as its own session authority must still build: %v", err)
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

// TestASessionBackendWithNoSessionAuthorityRefusesToStart is the MIRROR of the test
// above it, and it is the one that closes the direction the defect was measured in.
//
// 🔴 RED AT `e11c3a7`, GREEN AT HEAD, AND THE RED IS NOT A MISSING ERROR — IT IS A
// SUCCESSFUL BUILD. Measured at that commit with the arm below: `err` was nil, a Supabase
// backend was returned, and the chain had two members. The pod then starts, fetches its
// JWKS, passes its health check, and resolves every verified session against the
// token-file projection — which holds one synthetic user at provider `cairn-token-file`
// and no membership. The session AUTHENTICATES (`Identity.Valid()` true, an audit line
// naming a principal) and reads nothing. `ErrSessionAuthorityUnread` refused the opposite
// arrangement — a journal with no backend — which is the arrangement that was never the
// incident, so the closing condition was satisfied by a test while the failure stayed
// configurable in production.
//
// ⚠ TO RE-DERIVE THE RED: check out `e11c3a7`, add this file's `wantsRefusal` arm, and
// run it. It fails on the FIRST assertion — "a build that should have refused"; at that
// commit `FromEnvironment` has no `ErrSessionBackendWithoutAuthority` at all, so the
// compile is the first thing to go. Delete the two `errors.Is` lines and the arm still
// fails on `err == nil`, which is the behavioural half and the one worth reading.
func TestASessionBackendWithNoSessionAuthorityRefusesToStart(t *testing.T) {
	// Both session backends, because the refusal is `supabaseArmed || proxyArmed` and one
	// arm alone would leave the other operand asserted by nothing.
	for _, arm := range []struct {
		name string
		env  map[string]string
	}{
		{
			name: "supabase",
			env: map[string]string{
				EnvSupabaseIssuer:  testIssuer,
				EnvSupabaseJWKSURL: "https://notes-idp.example.test/jwks",
			},
		},
		{
			name: "trusted-header",
			env: map[string]string{
				EnvProxyFronted:       "yes",
				EnvProxySubjectHeader: testSubjectHeader,
				EnvProxySecret:        string(testProxySecret),
				EnvProxyProvider:      testProvider,
			},
		},
	} {
		t.Run(arm.name, func(t *testing.T) {
			_, _, err := FromEnvironment(arm.env, newTestAuthority(t), nil)
			if err == nil {
				t.Fatal("an armed session backend with NO session authority built a chain. That pod " +
					"starts, authenticates a verified session against the token-file projection, and " +
					"hands it an EMPTY authorization — it comes up healthy and reads nothing")
			}
			if !errors.Is(err, ErrSessionBackendWithoutAuthority) {
				t.Fatalf("the WRONG guard fired.\n  got:  %v\n  want: %v", err, ErrSessionBackendWithoutAuthority)
			}

			// The positive control: the SAME environment, with a session authority, builds.
			// Without it the arm above is satisfied by a `FromEnvironment` that refuses
			// this environment for any reason at all.
			if _, _, err := FromEnvironment(arm.env, newTestAuthority(t), newTestSessionAuthority(t)); err != nil {
				t.Fatalf("the same environment WITH a session authority must build: %v", err)
			}
		})
	}

	// 🔴 AND THE COMPATIBILITY CLAIM, ASSERTED BESIDE THE REFUSAL RATHER THAN ELSEWHERE.
	// A guard written as `sessions == nil` alone — dropping the armed operands — would
	// refuse every deployment that exists today. This is the arm that catches that, and it
	// is the reason the refusal asks the ARMED flags rather than just the parameter.
	chain, supabase, err := FromEnvironment(map[string]string{}, newTestAuthority(t), nil)
	if err != nil {
		t.Fatalf("no session backend and no session authority is today's wiring and must build: %v", err)
	}
	if len(chain) != 1 || supabase != nil {
		t.Fatalf("machine-token-only expected, got a chain of %d and supabase=%v", len(chain), supabase != nil)
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
