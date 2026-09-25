package identity

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/testcookie"
)

// --- fixtures -------------------------------------------------------------------------
//
// 🔴 SYNTHETIC, AND THE CREDENTIAL IS A LITERAL NOBODY EVER MINTED. This repository is
// public; a token that LOOKS real is worse than one that is, because nobody can tell.

const testToken = "fixture-credential-value-which-is-not-a-real-token"

// testProjectID is the project the fixture world holds, and it is the principal the
// COOKIE fixture resolves to — deliberately a different one from the credential's, so a
// test can tell which backend answered.
var testProjectID = control.DerivedID(control.PrefixProject, "quarry")

// credentialedModel is `fixtureModel` plus one issued credential bound to the USER.
//
// The machine-token backend therefore resolves to `user:rowan` and a cookie session for
// `testProjectID` resolves to `project:quarry`. Two distinct principals is what makes
// "which backend won" observable at all.
func credentialedModel(t *testing.T) control.Model {
	t.Helper()
	events := []control.Event{
		{Kind: control.EventUserCreated, At: testClock, UserID: testUserID,
			Provider: testProvider, Subject: testSubject, Email: testEmail},
		{Kind: control.EventProjectCreated, At: testClock, ProjectID: testProjectID, Name: "quarry", UserID: testUserID},
		{Kind: control.EventMemberSet, At: testClock, ProjectID: testProjectID, UserID: testUserID, Role: control.RoleOwner},
		{Kind: control.EventScopeCreated, At: testClock,
			ScopeID: control.DerivedID(control.PrefixScope, "quarry-notes"), DisplayName: "quarry-notes", ProjectID: testProjectID},
		{Kind: control.EventCredentialIssued, At: testClock, CredentialID: "crd_fixture",
			SubjectKind: control.KindUser, SubjectID: testUserID,
			TokenHash: control.HashToken(testToken), Label: "fixture"},
	}
	m, err := control.Replay(events)
	if err != nil {
		t.Fatalf("building the credentialed fixture world: %v", err)
	}
	return m
}

func newCredentialedAuthority(t *testing.T) *control.Cache {
	t.Helper()
	c := control.NewCache(staticSource{credentialedModel(t)}, control.CacheOptions{Now: fixedNow})
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("materializing the credentialed fixture world: %v", err)
	}
	return c
}

// memorySessions is a `SessionStore` with no filesystem, for the tests that measure a
// BACKEND rather than a store.
//
// ⚠ IT IS A TEST DOUBLE AND NOT THE REJECTED OPTION (d). Option (d) was a proposal to
// ship in-memory sessions to production; this exists so `CookieSession`'s refusals can be
// exercised without a temp directory. `FileSessionStore` has its own tests below.
type memorySessions struct {
	mu      sync.Mutex
	records map[string]Session
	now     func() time.Time
	// failLookup and failCreate make the store's error paths reachable, which is the
	// only way `CookieSession`'s "the session store could not be read" refusal can be
	// watched firing.
	failLookup error
	failCreate error
	failRevoke error
}

func newMemorySessions() *memorySessions {
	return &memorySessions{records: map[string]Session{}, now: func() time.Time { return time.Now().UTC() }}
}

func (m *memorySessions) put(rec Session) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records[rec.Digest] = rec
}

func (m *memorySessions) Create(rec Session) error {
	if m.failCreate != nil {
		return m.failCreate
	}
	m.put(rec)
	return nil
}

func (m *memorySessions) Lookup(presented string) (Session, bool, error) {
	if m.failLookup != nil {
		return Session{}, false, m.failLookup
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.records[SessionDigest(presented)]
	if !ok || !rec.Live(m.now()) {
		return Session{}, false, nil
	}
	return rec, true, nil
}

func (m *memorySessions) Revoke(presented string) error {
	if m.failRevoke != nil {
		return m.failRevoke
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.records, SessionDigest(presented))
	return nil
}

// newFileSessions opens a store in a temp directory with an injectable clock.
func newFileSessions(t *testing.T, now func() time.Time) *FileSessionStore {
	t.Helper()
	store, err := OpenFileSessionStore(filepath.Join(t.TempDir(), "sessions"))
	if err != nil {
		t.Fatalf("opening the session store: %v", err)
	}
	store.Now = now
	return store
}

func liveSession(t *testing.T, id string, expires time.Time) Session {
	t.Helper()
	return Session{
		Digest:    SessionDigest(id),
		Kind:      control.KindProject,
		Principal: testProjectID,
		IssuedAt:  expires.Add(-time.Hour),
		ExpiresAt: expires,
	}
}

// --- property 3: entropy ---------------------------------------------------------------

// TestSessionIDsCarryTheDeclaredEntropyAndDoNotRepeat is the ENTROPY half of property 3.
//
// 🔴 IT MEASURES THE DECODED WIDTH, NOT THE STRING LENGTH. A base64 string's length is a
// function of the encoding as well as the payload, so asserting on it would pass for a
// 16-byte id encoded with padding — which is how a halved key space gets through a test
// that looks like it checks the key space.
//
// ⚠ DISTINCTNESS OVER 512 DRAWS IS NOT A RANDOMNESS TEST AND IS NOT CLAIMED AS ONE. What
// it catches is a generator that is CONSTANT or that repeats on a short cycle — the two
// failure modes a mistake here actually produces (a `rand.Read` whose error was swallowed
// leaves a zero buffer). A statistical test of `crypto/rand` would be testing the standard
// library.
func TestSessionIDsCarryTheDeclaredEntropyAndDoNotRepeat(t *testing.T) {
	// 🔴 THE WIDTH IS A LITERAL HERE, NOT `SessionIDBytes`. A test that compared the
	// generator against the constant the generator reads is a test that `a == a`: halve
	// the constant and both sides move together, so a session id with half the entropy
	// passes. The constant is asserted against this literal separately, which is a
	// different claim and fails for a different reason.
	const wantBytes = 32
	if SessionIDBytes != wantBytes {
		t.Fatalf("SessionIDBytes is %d, want %d. 256 bits is the width at which guessing a session id is not a "+
			"strategy at any request rate a network permits; narrowing it is a decision, not a tidy-up.",
			SessionIDBytes, wantBytes)
	}

	const draws = 512
	seen := make(map[string]struct{}, draws)
	zero := make([]byte, wantBytes)
	for i := 0; i < draws; i++ {
		id, err := NewSessionID()
		if err != nil {
			t.Fatalf("draw %d: %v", i, err)
		}
		raw, err := base64.RawURLEncoding.DecodeString(id)
		if err != nil {
			t.Fatalf("draw %d does not decode as raw base64url (%v); a value a cookie cannot carry intact is a "+
				"session that dies at the first proxy that re-encodes it", i, err)
		}
		if len(raw) != wantBytes {
			t.Fatalf("draw %d decodes to %d bytes, want %d. The width of a session id IS its security property: "+
				"it is a bearer credential with the full authority of the principal behind it, and the only thing "+
				"between an attacker and it is that they cannot guess it.", i, len(raw), wantBytes)
		}
		if bytes.Equal(raw, zero) {
			t.Fatalf("draw %d is ALL ZEROES, which is what a buffer looks like when a `crypto/rand.Read` error "+
				"was swallowed", i)
		}
		if _, repeat := seen[id]; repeat {
			t.Fatalf("draw %d repeated an id already seen in %d draws; a generator that repeats hands two browsers "+
				"one session", i, draws)
		}
		seen[id] = struct{}{}
	}
	t.Logf("entropy: %d draws, %d distinct, %d bytes each", draws, len(seen), wantBytes)
}

// TestTheLookupScanHasNoEarlyExit is the COMPARISON half of property 3, and it pins the
// property that is actually worth pinning.
//
// 🔴 A CONSTANT-TIME COMPARE INSIDE A SHORT-CIRCUITING LOOP LEAKS ANYWAY. The comparison
// is `subtle.ConstantTimeCompare`, so no single comparison leaks its operands' difference
// — but a loop that RETURNS at the first match makes the response time carry the matched
// record's position in the file, which is a fact about the store's contents.
//
// 🔴 IT IS A STRUCTURAL GUARD AND IS LABELLED AS ONE, AND IT REPLACED A BEHAVIOURAL ONE
// DELIBERATELY. The predecessor read a `comparisons atomic.Int64` counter incremented once
// per record inside `Lookup` and probed three positions. That measured the property
// directly, and it did so by carrying an instrument in the SERVING PATH — a field on the
// production struct, incremented on every request by every replica, existing for one test.
// The loop's shape is what the property actually is, so the shape is what is pinned here
// and the counter is gone.
//
// ⚠ THE TRADE, STATED RATHER THAN LEFT TO BE DISCOVERED. This pins the loop's ITERATION
// COUNT — exactly what the counter measured — and NOT the per-iteration COST. A body doing
// work whose duration depends on whether this record matched would leak the matched
// position with no branch statement anywhere, and neither this guard nor the counter it
// replaced would see it; that half belongs to `digestsEqual` and
// `TestTheSessionComparisonsUseConstantTimePrimitives`. Moving the scan out of `Lookup`
// entirely is NOT in the gap: the positive controls below fail when the `range` or the
// `digestsEqual` call leaves this function.
//
// ⚠ AND THE INSTRUMENT IS VALIDATED BEFORE ITS VERDICT IS READ, because "found no `break`"
// and "found no loop" are the same green. The walk must find `Lookup`, must find a `range`
// inside it, and that range's body must contain the `digestsEqual` call — which is what
// makes it THE scan rather than some other loop that happens to be there.
func TestTheLookupScanHasNoEarlyExit(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filepath.Join(".", "sessionstore.go"), nil, 0)
	if err != nil {
		t.Fatalf("parsing sessionstore.go: %v", err)
	}

	var lookup *ast.FuncDecl
	for _, d := range file.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "Lookup" || fn.Recv == nil {
			continue
		}
		lookup = fn
	}
	if lookup == nil {
		t.Fatal("POSITIVE CONTROL FAILED: no method named `Lookup` was found in sessionstore.go, so the " +
			"absence of a branch statement below is a fact about the walk rather than about the scan")
	}

	var scan *ast.RangeStmt
	ast.Inspect(lookup, func(n ast.Node) bool {
		if rng, ok := n.(*ast.RangeStmt); ok && scan == nil {
			scan = rng
		}
		return true
	})
	if scan == nil {
		t.Fatal("POSITIVE CONTROL FAILED: `Lookup` contains no `range` statement at all, so this guard is " +
			"inspecting nothing")
	}

	calls := 0
	ast.Inspect(scan.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "digestsEqual" {
			calls++
		}
		return true
	})
	if calls == 0 {
		t.Fatal("POSITIVE CONTROL FAILED: the `range` found in `Lookup` does not call `digestsEqual`, so it is " +
			"not the digest scan and this guard is pinning the wrong loop")
	}

	var offenders []string
	ast.Inspect(scan.Body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.BranchStmt:
			offenders = append(offenders, fmt.Sprintf("%s at sessionstore.go:%d",
				node.Tok.String(), fset.Position(node.Pos()).Line))
		case *ast.ReturnStmt:
			offenders = append(offenders, fmt.Sprintf("return at sessionstore.go:%d",
				fset.Position(node.Pos()).Line))
		}
		return true
	})

	if len(offenders) > 0 {
		t.Errorf("THE SCAN SHORT-CIRCUITS: `FileSessionStore.Lookup`'s digest loop contains %v. Leaving the "+
			"loop early makes the response time carry the matched record's POSITION in the store, which is a "+
			"fact about the store's contents — a constant-time comparison does not help, because what leaks is "+
			"how many comparisons happened rather than what any one of them found. Record the match and let the "+
			"loop run to completion.", offenders)
	}
	t.Logf("scan shape: the `Lookup` range at sessionstore.go:%d makes %d digestsEqual call(s) and contains "+
		"%d early-exit statement(s)", fset.Position(scan.Pos()).Line, calls, len(offenders))
}

// --- property 4: expiry ----------------------------------------------------------------

// TestAnExpiredSessionIsRefusedAtTheBoundary injects the clock rather than sleeping.
//
// 🔴 THE INSTANT `ExpiresAt` ITSELF IS ONE OF THE THREE POINTS, BECAUSE THAT IS WHERE AN
// OFF-BY-ONE LIVES. `now.After(exp)` and `!now.Before(exp)` differ on exactly one instant
// and on no other, so a test that measured only "an hour before" and "an hour after"
// would pass against either — and an injected clock lands on exactly that instant
// whenever a test pins it there, which is the only case in which the difference is
// reachable at all.
func TestAnExpiredSessionIsRefusedAtTheBoundary(t *testing.T) {
	expiry := time.Date(2000, 6, 1, 12, 0, 0, 0, time.UTC)
	var now time.Time
	store := newFileSessions(t, func() time.Time { return now })

	id, err := NewSessionID()
	if err != nil {
		t.Fatal(err)
	}
	now = expiry.Add(-time.Hour)
	if err := store.Create(liveSession(t, id, expiry)); err != nil {
		t.Fatal(err)
	}

	for _, arm := range []struct {
		name string
		at   time.Time
		want bool
	}{
		{"a second before expiry", expiry.Add(-time.Second), true},
		{"the expiry instant itself", expiry, false},
		{"a second after expiry", expiry.Add(time.Second), false},
		{"an hour after expiry", expiry.Add(time.Hour), false},
	} {
		now = arm.at
		_, live, err := store.Lookup(id)
		if err != nil {
			t.Fatalf("%s: %v", arm.name, err)
		}
		if live != arm.want {
			t.Errorf("%s: the store answered live=%v, want %v. A session is dead AT its expiry instant, not "+
				"after it — `now.After(exp)` keeps a session alive for exactly the instant `!now.Before(exp)` "+
				"kills it, and a clock that lands on that instant is the only thing that can tell them apart.",
				arm.name, live, arm.want)
		}
	}
	// POSITIVE CONTROL: the store CAN answer live, so the three refusals above are not
	// "this store refuses everything".
	now = expiry.Add(-time.Minute)
	if _, live, _ := store.Lookup(id); !live {
		t.Fatal("POSITIVE CONTROL FAILED: the store refused a session inside its lifetime, so its refusals above " +
			"are not evidence about expiry")
	}
}

// TestAnExpiredSessionIsDroppedByTheNextWrite pins the pruning, which is what keeps the
// file's size bounded by LIVE sessions without a background goroutine.
func TestAnExpiredSessionIsDroppedByTheNextWrite(t *testing.T) {
	base := time.Date(2000, 6, 1, 12, 0, 0, 0, time.UTC)
	var now time.Time
	store := newFileSessions(t, func() time.Time { return now })

	now = base
	stale, err := NewSessionID()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Create(liveSession(t, stale, base.Add(time.Minute))); err != nil {
		t.Fatal(err)
	}

	now = base.Add(time.Hour)
	fresh, err := NewSessionID()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Create(liveSession(t, fresh, now.Add(time.Hour))); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Count(strings.TrimSpace(string(raw)), "\n") + 1
	if lines != 1 {
		t.Errorf("the table holds %d record(s) after one expired and one live session were written; the expired "+
			"one must be dropped by the next write, or the file grows at login rate forever", lines)
	}
	if strings.Contains(string(raw), SessionDigest(stale)) {
		t.Error("the expired session's digest is still in the file after a later write")
	}
	if !strings.Contains(string(raw), SessionDigest(fresh)) {
		t.Fatal("POSITIVE CONTROL FAILED: the LIVE session's digest is not in the file either, so the absence " +
			"above is not evidence about pruning")
	}
}

// --- property 5: logout revokes --------------------------------------------------------

// TestLogoutRevokesAndTheRevocationSURVIVESARESTART is the property this whole storage
// decision was made for, and it is the strongest test in this package.
//
// 🔴 THE RESTART IS THE POINT, NOT A BONUS ARM. Every rejected alternative satisfies
// "revoke then look up in the same process": a map does, a signed cookie with an
// in-memory denylist does. What separates option (a) from option (d) — and from a
// client-held JWT, which is the design this one was chosen OVER — is that the refusal
// survives the process that issued it. So the second half of this test throws the store
// value away and opens a new one over the same path, which is what a pod restart is.
//
// 🔴 AND DURABILITY IS THE OPERATOR'S OWN REQUIREMENT, NOT AN IMPLICATION OF THE LOGOUT
// ONE. See the package comment's requirement (ii): "a rolling deploy signs NOBODY out".
// The restart arm below asserts that requirement directly; it does not derive it from
// "logout revokes", which would be circular.
//
// ⚠ AND IT ASSERTS THE PAIR. A store that refuses everything after a restart would pass a
// "the revoked session is refused" assertion perfectly; the second, untouched session
// must still be LIVE across the same restart, or the refusal is about the restart rather
// than about the revocation.
func TestLogoutRevokesAndTheRevocationSurvivesARestart(t *testing.T) {
	now := time.Date(2000, 6, 1, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	path := filepath.Join(t.TempDir(), "sessions")

	store, err := OpenFileSessionStore(path)
	if err != nil {
		t.Fatal(err)
	}
	store.Now = clock

	revoked, err := NewSessionID()
	if err != nil {
		t.Fatal(err)
	}
	kept, err := NewSessionID()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{revoked, kept} {
		if err := store.Create(liveSession(t, id, now.Add(time.Hour))); err != nil {
			t.Fatal(err)
		}
	}

	// PRECONDITION: both are live, so the refusal below is a change rather than a
	// starting state.
	for _, id := range []string{revoked, kept} {
		if _, live, _ := store.Lookup(id); !live {
			t.Fatalf("PRECONDITION FAILED: a freshly created session is not live, so the revocation below " +
				"cannot be observed")
		}
	}

	if err := store.Revoke(revoked); err != nil {
		t.Fatalf("revoking: %v", err)
	}
	if _, live, _ := store.Lookup(revoked); live {
		t.Fatal("THE REVOKED SESSION IS STILL LIVE IN THE PROCESS THAT REVOKED IT. `logout actually revokes` is " +
			"the requirement this storage design was chosen for, over a client-held JWT whose only defect is " +
			"that it cannot be taken back.")
	}
	if _, live, _ := store.Lookup(kept); !live {
		t.Fatal("revoking one session killed the other; `Revoke` removes the record a presented id names and no " +
			"other")
	}

	// THE RESTART. A new store value over the same path, with nothing carried over.
	reopened, err := OpenFileSessionStore(path)
	if err != nil {
		t.Fatal(err)
	}
	reopened.Now = clock

	if _, live, _ := reopened.Lookup(revoked); live {
		t.Error("THE REVOKED SESSION CAME BACK AFTER A RESTART. Durability across a process restart and a " +
			"rolling deploy is the OPERATOR's stated requirement for this surface, claimed on its own authority " +
			"rather than derived from the logout one: a rolling deploy must sign NOBODY out, and a brief 503 " +
			"while a replica restarts is acceptable where a sign-out is not. (Deriving it from logout is " +
			"circular — a logout that does not survive a restart is still a logout — and that is what this " +
			"message used to do.)")
	}
	if _, live, _ := reopened.Lookup(kept); !live {
		t.Error("THE UNTOUCHED SESSION DID NOT SURVIVE THE RESTART, so the refusal above is evidence about the " +
			"restart rather than about the revocation. Both halves have to hold or neither means anything.")
	}
	// ⚠ GUARDED ON `t.Failed()`. A `t.Log` after a `t.Error` still runs, so an
	// unguarded success line prints beside the failure it contradicts — and a reader
	// skimming the output sees a sentence asserting the property that just broke.
	if !t.Failed() {
		t.Log("logout: the revoked session is refused and the untouched one is honoured, in the original " +
			"process AND in a second store opened over the same path")
	}
}

// TestRevokingAnUnknownSessionSucceeds pins the one place this store is deliberately
// permissive, and says why in the failure message.
func TestRevokingAnUnknownSessionSucceeds(t *testing.T) {
	store := newFileSessions(t, nil)
	if err := store.Revoke("an-id-this-store-has-never-held"); err != nil {
		t.Errorf("revoking an unknown session failed with %v. A browser holding a cookie whose session has "+
			"already expired, or has been revoked from another tab, must still be able to complete a logout — "+
			"otherwise the one action that clears a stale credential is the one action that fails while the "+
			"credential is stale.", err)
	}
}

// --- property 7: the store holds no credential -------------------------------------------

// TestTheSessionFileHoldsNoSessionID pins that a leaked session table is not a set of
// usable cookies.
//
// It reports a PAIR: the id must be absent AND its digest present, or "the id is not in
// the file" is satisfied by a file with nothing in it.
func TestTheSessionFileHoldsNoSessionID(t *testing.T) {
	now := time.Date(2000, 6, 1, 12, 0, 0, 0, time.UTC)
	store := newFileSessions(t, func() time.Time { return now })
	id, err := NewSessionID()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Create(liveSession(t, id, now.Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if strings.Contains(body, id) {
		t.Error("THE SESSION ID ITSELF IS IN THE SESSION FILE. Anyone who can read the file is then every " +
			"signed-in user: the value is exactly what a browser presents. The stored form is `sha256(id)`, for " +
			"the same reason the token table stores digests.")
	}
	if !strings.Contains(body, SessionDigest(id)) {
		t.Fatalf("POSITIVE CONTROL FAILED: the session's DIGEST is not in the file either (%d bytes written), so "+
			"the absence above is a claim about an empty file rather than about the record", len(raw))
	}
	// And the file is not world-readable: it names every principal with a live session.
	info, err := os.Stat(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		t.Errorf("the session file's mode is %04o; it names every principal with a live browser session and must "+
			"not be group- or world-readable", mode)
	}
	t.Logf("session file: %d bytes, mode %04o, contains the digest, does not contain the id",
		len(raw), info.Mode().Perm())
}

// TestAMalformedLineIsRefusedRatherThanSkipped pins the ruling that differs from
// `control.FileStore`'s, and the message carries the reason.
func TestAMalformedLineIsRefusedRatherThanSkipped(t *testing.T) {
	store := newFileSessions(t, nil)
	if err := os.WriteFile(store.Path(), []byte("{not json}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Lookup("anything"); err == nil {
		t.Error("a malformed line was SKIPPED. The next write rewrites the whole file, so a skipped line is a " +
			"session silently dropped from the table — a user signed out with no error anywhere — and a `Revoke` " +
			"that reports success having rewritten a file that never held the record it was asked to remove.")
	}
	// And the error does not carry the line's content: it is a session record.
	if _, _, err := store.Lookup("anything"); err != nil && strings.Contains(err.Error(), "not json") {
		t.Errorf("the parse error quotes the line's CONTENT (%q); that content is a session record and this "+
			"message goes to an operator's log", err)
	}
}

// --- property 2's primitive: the CSRF derivation -----------------------------------------

// TestTheCSRFTokenIsBoundToItsSessionAndNotDerivableFromTheStore is the derivation's own
// guard. The end-to-end CSRF gate is `internal/ui`'s.
func TestTheCSRFTokenIsBoundToItsSessionAndNotDerivableFromTheStore(t *testing.T) {
	a, err := NewSessionID()
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewSessionID()
	if err != nil {
		t.Fatal(err)
	}

	if CSRFTokenFor(a) == CSRFTokenFor(b) {
		t.Fatal("two DIFFERENT sessions derive the SAME CSRF token, so a token from any session validates " +
			"against every session and the guard is decorative")
	}
	if !CSRFTokenValid(a, CSRFTokenFor(a)) {
		t.Fatal("a session's own token does not validate against it, so every state-changing request would be " +
			"refused and the guard would be removed as broken")
	}
	if CSRFTokenValid(a, CSRFTokenFor(b)) {
		t.Error("ANOTHER SESSION'S TOKEN VALIDATES. The token's whole job is to prove the request came from a " +
			"page rendered for THIS session; a token that is valid across sessions is one an attacker obtains by " +
			"signing in themselves.")
	}
	// The empty cases, which is where a "compare two derivations of the empty string"
	// defect would live.
	for _, arm := range [][2]string{{"", ""}, {a, ""}, {"", CSRFTokenFor("")}} {
		if CSRFTokenValid(arm[0], arm[1]) {
			t.Errorf("CSRFTokenValid(%q-ish, %q-ish) accepted an empty operand; a caller that lost its cookie "+
				"would be comparing two derivations of the empty string", arm[0], arm[1])
		}
	}

	// 🔴 THE STORE CANNOT MINT ONE. This is the property that made a DERIVED token
	// better than a stored random one: the file holds `sha256(id)`, and a digest is not
	// a key, so somebody who reads the session table cannot forge a CSRF token.
	if CSRFTokenValid(SessionDigest(a), CSRFTokenFor(a)) {
		t.Error("the STORED digest derives the same token as the session id, so the session file is a CSRF " +
			"forgery kit for every live session in it")
	}
	t.Log("csrf: bound to its own session, refused across sessions, refused on an empty operand, not derivable " +
		"from the stored digest")
}

// --- property 1's primitive: the cookie attributes ---------------------------------------

// TestTheSessionCookieCarriesItsFlagsOnTheWIRE reads the rendered `Set-Cookie` header.
//
// 🔴 THE STRUCT IS NOT THE HEADER. `http.Cookie` has fields Go will silently decline to
// render — an invalid name drops the whole header, `SameSite` has a zero value that emits
// no attribute at all — so asserting on the struct passed in measures the test's own
// literal. This writes the cookie through `http.SetCookie` and parses what came out.
func TestTheSessionCookieCarriesItsFlagsOnTheWire(t *testing.T) {
	rec := httptest.NewRecorder()
	http.SetCookie(rec, SessionCookie("a-session-id", time.Date(2000, 6, 2, 12, 0, 0, 0, time.UTC)))
	header := rec.Header().Get("Set-Cookie")
	if header == "" {
		t.Fatal("NO Set-Cookie HEADER WAS RENDERED. `http.SetCookie` silently drops a cookie it considers " +
			"invalid — an unacceptable name is the usual cause — so every assertion below would be over an " +
			"empty string.")
	}

	// `Path=/` is matched EXACTLY: `strings.Contains` accepts `Path=/sign-in`, and a
	// conforming browser DROPS a `__Host-` cookie whose path is not exactly `/` — so that
	// mutation used to survive here and signed nobody out visibly.
	for _, want := range []string{"HttpOnly", "Secure", "SameSite=Lax", "Path=/"} {
		if !testcookie.HasExactAttr(header, want) {
			t.Errorf("the Set-Cookie header does not carry %s exactly: %q", want, header)
		}
	}
	if !strings.HasPrefix(header, SessionCookieName+"=") {
		t.Errorf("the cookie is not named %s: %q", SessionCookieName, header)
	}
	if strings.Contains(header, "Domain=") {
		t.Errorf("the cookie carries a Domain attribute: %q. A `__Host-` cookie with one is refused outright by "+
			"a conforming browser, so the prefix's whole guard — that no sibling host can plant a cookie we will "+
			"honour — would be lost along with the cookie.", header)
	}

	// The cleared cookie must match on every attribute a browser uses to find the one it
	// is replacing, or the deletion silently misses and the original stays.
	rec2 := httptest.NewRecorder()
	http.SetCookie(rec2, ClearedSessionCookie())
	cleared := rec2.Header().Get("Set-Cookie")
	if !strings.Contains(cleared, "Max-Age=0") {
		t.Errorf("the cleared cookie does not expire it: %q", cleared)
	}
	for _, want := range []string{"HttpOnly", "Secure", "SameSite=Lax", "Path=/"} {
		if !testcookie.HasExactAttr(cleared, want) {
			t.Errorf("the cleared cookie does not carry %s exactly (%q); a browser matches a deletion to an "+
				"existing cookie on name, path and domain, and a mismatch leaves the original in place",
				want, cleared)
		}
	}
	t.Logf("set-cookie: %q", header)
}

// --- the backend's refusals --------------------------------------------------------------

// TestTheCookieBackendRefusesEveryBadSessionWithItsOwnReason walks each refusal and
// asserts the SPECIFIC one fired.
//
// 🔴 A KILL BY A DIFFERENT ARM IS A MISATTRIBUTION. Every one of these returns the same
// `control.ErrNoCredential` to a serving path, so a test that asked only "was it refused"
// would pass with five of the six checks deleted. `identity.Refusal` carries the reason
// for exactly this, and it never reaches the wire.
func TestTheCookieBackendRefusesEveryBadSessionWithItsOwnReason(t *testing.T) {
	authority := newCredentialedAuthority(t)
	now := time.Date(2000, 6, 1, 12, 0, 0, 0, time.UTC)

	// Each arm builds its own store so a failure mode cannot leak into the next.
	arms := []struct {
		name   string
		build  func() (*CookieSession, *http.Cookie)
		reason string
	}{
		{
			name: "no cookie at all",
			build: func() (*CookieSession, *http.Cookie) {
				c, _ := NewCookieSession(newMemorySessions(), authority)
				return c, nil
			},
			reason: "no session cookie",
		},
		{
			name: "an empty cookie value",
			build: func() (*CookieSession, *http.Cookie) {
				c, _ := NewCookieSession(newMemorySessions(), authority)
				return c, &http.Cookie{Name: SessionCookieName, Value: ""}
			},
			reason: "no session cookie",
		},
		{
			name: "a well-formed cookie naming no session",
			build: func() (*CookieSession, *http.Cookie) {
				c, _ := NewCookieSession(newMemorySessions(), authority)
				return c, &http.Cookie{Name: SessionCookieName, Value: "not-a-session"}
			},
			reason: "no live session for this cookie",
		},
		{
			name: "an EXPIRED session",
			build: func() (*CookieSession, *http.Cookie) {
				sessions := newMemorySessions()
				sessions.now = func() time.Time { return now }
				sessions.put(Session{Digest: SessionDigest("dead"), Kind: control.KindProject,
					Principal: testProjectID, ExpiresAt: now.Add(-time.Second)})
				c, _ := NewCookieSession(sessions, authority)
				return c, &http.Cookie{Name: SessionCookieName, Value: "dead"}
			},
			reason: "no live session for this cookie",
		},
		{
			name: "a store that cannot be read",
			build: func() (*CookieSession, *http.Cookie) {
				sessions := newMemorySessions()
				sessions.failLookup = constError("the disk is on fire")
				c, _ := NewCookieSession(sessions, authority)
				return c, &http.Cookie{Name: SessionCookieName, Value: "anything"}
			},
			reason: "the session store could not be read",
		},
		{
			name: "a session naming a principal the control plane no longer holds",
			build: func() (*CookieSession, *http.Cookie) {
				sessions := newMemorySessions()
				sessions.put(Session{Digest: SessionDigest("orphan"), Kind: control.KindUser,
					Principal: control.DerivedID(control.PrefixUser, "deleted-person"),
					ExpiresAt: time.Now().Add(time.Hour)})
				c, _ := NewCookieSession(sessions, authority)
				return c, &http.Cookie{Name: SessionCookieName, Value: "orphan"}
			},
			reason: "the session names a principal this control plane no longer holds",
		},
	}

	for _, arm := range arms {
		t.Run(arm.name, func(t *testing.T) {
			backend, cookie := arm.build()
			r := request(t)
			if cookie != nil {
				r.AddCookie(cookie)
			}
			_, err := backend.Authenticate(r)
			if err == nil {
				t.Fatalf("this request was AUTHENTICATED; it must be refused with %q", arm.reason)
			}
			var refusal *Refusal
			if !asRefusal(err, &refusal) {
				t.Fatalf("the refusal is %T (%v), not an *identity.Refusal, so its reason cannot be read and a "+
					"kill by a different check would be indistinguishable from this one", err, err)
			}
			if refusal.Backend != CookieSessionBackend {
				t.Errorf("the refusal names backend %q, not %q", refusal.Backend, CookieSessionBackend)
			}
			if refusal.Reason != arm.reason {
				t.Errorf("the refusal's reason is %q, want %q. A kill by a different arm proves the backend can "+
					"say no and proves nothing about the check this arm exists for.", refusal.Reason, arm.reason)
			}
		})
	}

	// POSITIVE CONTROL: a GOOD session authenticates against this same authority, with
	// real authority behind it. Six refusals with no accept is a backend that refuses
	// everything.
	sessions := newMemorySessions()
	sessions.put(Session{Digest: SessionDigest("good"), Kind: control.KindUser,
		Principal: testUserID, ExpiresAt: time.Now().Add(time.Hour)})
	backend, err := NewCookieSession(sessions, authority)
	if err != nil {
		t.Fatal(err)
	}
	r := request(t)
	r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "good"})
	id, err := backend.Authenticate(r)
	if err != nil {
		t.Fatalf("POSITIVE CONTROL FAILED: a live session over a real world was refused (%v), so the six "+
			"refusals above are not evidence about anything", err)
	}
	if !id.Valid() {
		t.Fatal("POSITIVE CONTROL FAILED: the identity names nobody")
	}
	// 🔴 AND IT CARRIES REAL AUTHORITY. A test asserting only "it authenticated" passes
	// against a backend that resolves a principal with an EMPTY authorization — the
	// exact defect `journalsession_test.go` exists for, one backend over.
	if !id.Auth.VisibleScopes(control.VerbRead).Allows("quarry-notes") {
		t.Error("the authenticated session can read NO scope. The fixture user owns a project with a scope in " +
			"it, so an empty authorization here means the principal resolved but `control.Resolve` was given " +
			"the wrong one — which authenticates a browser and then answers every page as if the store were empty.")
	}
	if id.Fingerprint != "" {
		t.Errorf("the identity carries fingerprint %q. `Identity.Fingerprint` is the 12-hex id of a bearer token "+
			"this pod minted and an operator can look up; a session has no row in that table, and putting a "+
			"session digest there would write a session identifier into every audit line.", id.Fingerprint)
	}
}

// TestACookieBackendWithNoStoreOrNoAuthorityIsRefusedAtConstruction pins the fail-closed
// direction, with a distinct sentinel per missing half.
func TestACookieBackendWithNoStoreOrNoAuthorityIsRefusedAtConstruction(t *testing.T) {
	if _, err := NewCookieSession(nil, newTestAuthority(t)); err != ErrNoSessionStore {
		t.Errorf("a backend with no session store must be %v, got %v", ErrNoSessionStore, err)
	}
	if _, err := NewCookieSession(newMemorySessions(), nil); err != ErrNoAuthority {
		t.Errorf("a backend with no authority must be %v, got %v", ErrNoAuthority, err)
	}
	if _, err := NewCookieSession(newMemorySessions(), newTestAuthority(t)); err != nil {
		t.Errorf("POSITIVE CONTROL FAILED: a fully wired backend was refused (%v), so the refusals above are "+
			"not evidence about the missing halves", err)
	}
}

// asRefusal is `errors.As` spelled once, so each arm above reads as one assertion.
func asRefusal(err error, out **Refusal) bool {
	for err != nil {
		if r, ok := err.(*Refusal); ok {
			*out = r
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// --- property 3's structural half: the comparison primitives ------------------------------

// TestTheSessionComparisonsUseConstantTimePrimitives is a STRUCTURAL guard and is
// labelled as one, because the property it pins has no behavioural observable.
//
// 🔴 A TIMING-UNSAFE COMPARE BEHAVES IDENTICALLY TO A SAFE ONE. `hmac.Equal(a, b)` and
// `a == b` return the same booleans for every input; the difference is WHEN they return,
// and a test that measured that would be a flake generator. So the mutant that matters —
// replacing `hmac.Equal` with `==` in [CSRFTokenValid] — survives every behavioural test
// in this package, which is exactly why this one exists.
//
// ⚠ AND IT IS A GUARD ON A SPELLING, WITH THE WEAKNESS THAT IMPLIES: somebody could
// write a third comparison in a helper this does not scan. What it can do is refuse the
// two primitives disappearing from the two functions that must have them, and refuse a
// `==` appearing in the two files where one would be a finding. Named here rather than
// discovered by whoever trusts the green.
func TestTheSessionComparisonsUseConstantTimePrimitives(t *testing.T) {
	const pkgDir = "."
	fset := token.NewFileSet()
	files := map[string]*ast.File{}
	for _, name := range []string{"session.go", "sessionstore.go"} {
		f, err := parser.ParseFile(fset, filepath.Join(pkgDir, name), nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		files[name] = f
	}

	calls := map[string]int{}
	var suspectEq []string
	for name, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.CallExpr:
				if sel, ok := node.Fun.(*ast.SelectorExpr); ok {
					if pkg, ok := sel.X.(*ast.Ident); ok {
						calls[pkg.Name+"."+sel.Sel.Name]++
					}
				}
			case *ast.BinaryExpr:
				if node.Op != token.EQL && node.Op != token.NEQ {
					return true
				}
				// A comparison is SUSPECT when one operand names a digest or a
				// presented value AND the other is not a literal. `rec.Digest == want`
				// is the shape the constant-time helper exists to replace.
				//
				// ⚠ `x == ""` IS EXCLUDED AND THE EXCLUSION IS NOT A LOOPHOLE. An
				// emptiness check leaks the one bit "this value is empty", which the
				// caller supplied and already knows; it cannot be extended byte by byte
				// because there is no secret on the other side to walk towards. Without
				// the exclusion this guard fires on five correct emptiness checks, and a
				// guard that fires on a safe file is a guard somebody deletes.
				if suspectComparison(node.X, node.Y) || suspectComparison(node.Y, node.X) {
					suspectEq = append(suspectEq, fmt.Sprintf("%s:%d", name, fset.Position(node.Pos()).Line))
				}
			}
			return true
		})
	}

	// INSTRUMENT CONTROL: the walker saw calls at all. A parser that resolved nothing
	// reports zero of everything, and the two assertions below would both be satisfied
	// by a scanner wired to nothing.
	if len(calls) == 0 {
		t.Fatal("the AST walk found NO call expressions in session.go or sessionstore.go, so the counts below " +
			"are about a scanner that saw nothing")
	}

	if calls["subtle.ConstantTimeCompare"] == 0 {
		t.Error("NO `subtle.ConstantTimeCompare` CALL REMAINS. Digest comparison must not return early on the " +
			"first differing byte; see `digestsEqual` for the honest scope of what that buys and why the " +
			"non-short-circuiting scan around it is the stronger property.")
	}
	if calls["hmac.Equal"] == 0 {
		t.Error("NO `hmac.Equal` CALL REMAINS. `CSRFTokenValid` compares an ATTACKER-SUPPLIED string against a " +
			"secret-derived one, which is a timing oracle anybody can drive directly: submit, measure, extend by " +
			"one byte, repeat. This is the one comparison here where constant time is load-bearing rather than " +
			"defensive, and no behavioural test in this package can see it go.")
	}
	if len(suspectEq) > 0 {
		t.Errorf("a `==`/`!=` comparison names a digest or a presented value at %v; route it through "+
			"`digestsEqual` or `hmac.Equal` instead", suspectEq)
	}
	t.Logf("comparison primitives: %d subtle.ConstantTimeCompare call(s), %d hmac.Equal call(s), %d suspect "+
		"`==` comparison(s), over %d distinct call targets inspected",
		calls["subtle.ConstantTimeCompare"], calls["hmac.Equal"], len(suspectEq), len(calls))
}

// suspectComparison answers whether `secret` names a credential-ish value and `other` is
// something other than a literal.
func suspectComparison(secret, other ast.Expr) bool {
	if !mentionsSecret(secret) {
		return false
	}
	_, literal := other.(*ast.BasicLit)
	return !literal
}

// mentionsSecret answers whether an expression names something this package treats as a
// credential-ish value. Conservative on purpose: it matches identifiers and selectors,
// not arbitrary expressions, so it cannot fire on unrelated code.
func mentionsSecret(e ast.Expr) bool {
	name := ""
	switch node := e.(type) {
	case *ast.Ident:
		name = node.Name
	case *ast.SelectorExpr:
		name = node.Sel.Name
	default:
		return false
	}
	lower := strings.ToLower(name)
	for _, needle := range []string{"digest", "presented", "csrf", "sessionid"} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}
