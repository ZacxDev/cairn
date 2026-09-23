package control

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/authz"
)

// issueClock is the instant every credential below carries. Year 2000, per the repo's
// fixture rule.
//
// It deliberately reuses `provisionClock` rather than declaring a second constant: every
// test here provisions a user first, and two fixture clocks in one package is how a test
// that compares two events' timestamps starts asserting an accident.
var issueClock = provisionClock

// aProvisionedOwner is the world every case below starts from: one user, one project they
// own, and one scope in it.
//
// Returned as ids rather than as a Model, because the point of these tests is what the
// JOURNAL holds — every assertion re-reads the file rather than trusting an in-memory
// projection, for the reason `TestProvisioningAUserYieldsAnAuthorityThatAuthorisesThem`
// states one file over.
func aProvisionedOwner(t *testing.T) (*FileStore, string, Provisioned) {
	t.Helper()
	store, path := journalAt(t)
	made, err := ProvisionUser(context.Background(), store, aUser("quarry-notes"))
	if err != nil {
		t.Fatalf("provisioning the owner this credential is for: %v", err)
	}
	if len(made.Scopes) != 1 {
		t.Fatalf("the fixture expected one scope, got %+v", made.Scopes)
	}
	return store, path, made
}

func issueTo(t *testing.T, store *FileStore, kind Kind, id ID, narrowed []ID) Issued {
	t.Helper()
	issued, err := IssueCredential(context.Background(), store, NewCredential{
		SubjectKind: kind, SubjectID: id,
		Label: "fixture laptop", NarrowedScopes: narrowed, At: issueClock,
	})
	if err != nil {
		t.Fatalf("issuing: %v", err)
	}
	return issued
}

// TestAnIssuedCredentialAuthenticatesAndTheJournalHoldsOnlyItsDigest is the end-to-end
// claim, and the LOAD-BEARING assertion in it is the negative one: the token's bytes are
// not in the file.
//
// 🔴 IT ASSERTS AGAINST THE FILE, NOT AGAINST THE EVENT THIS PATH BUILT. An assertion that
// `ev.TokenHash != token` is a claim about a struct field in this process; the property
// that matters is about the durable, operator-readable, append-only artifact — and the two
// come apart the moment anything else in the batch, a label or an actor, carries the value.
// Reading the bytes back is the only version of this that keeps being true when the event
// shape changes.
//
// 🔴 AND THE AUTHORITY IS ASSERTED BY CONTENT. A test that only checked "Authenticate
// returned no error" would be satisfied by a credential that resolves to an EMPTY
// authorization — which is exactly the state `cairn-ui`'s startup refusal exists for, and
// is indistinguishable from a working credential from anywhere except here.
func TestAnIssuedCredentialAuthenticatesAndTheJournalHoldsOnlyItsDigest(t *testing.T) {
	store, path, made := aProvisionedOwner(t)
	issued := issueTo(t, store, KindUser, made.User, nil)

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the journal back: %v", err)
	}
	body := string(raw)
	if strings.Contains(body, issued.Token()) {
		// Deliberately does not print the journal: if this fails, the file holds a live
		// credential and the failure message must not re-stage it into the test log.
		t.Fatal("THE RAW TOKEN IS IN THE JOURNAL FILE. The whole credential design rests on the " +
			"journal holding only a digest: the file is append-only, operator-readable and backed up, " +
			"so a secret that reaches it cannot be taken back.")
	}
	if !strings.Contains(body, issued.TokenHash) {
		t.Fatal("the journal does not contain the digest the call reported, so the record and the " +
			"returned value describe different credentials")
	}
	if issued.TokenHash == issued.Token() {
		t.Fatal("the reported digest IS the token")
	}

	// Re-read from disk in a second `FileStore`, the way a pod in another process does.
	reread, err := OpenFileStore(path)
	if err != nil {
		t.Fatalf("re-opening the journal: %v", err)
	}
	m, err := reread.Model(context.Background())
	if err != nil {
		t.Fatalf("replaying the journal: %v", err)
	}

	p, auth, err := Authenticate(m, issued.Token())
	if err != nil {
		t.Fatalf("the issued token does not authenticate against the journal it was written to: %v", err)
	}
	if p.Kind != KindUser || p.ID != made.User {
		t.Fatalf("principal = %v, want the provisioned user %s", p, made.User)
	}
	if p.CredentialID != issued.Credential {
		t.Fatalf("principal carries credential %q, want %q — the audit line needs the credential that matched", p.CredentialID, issued.Credential)
	}
	scope := made.Scopes[0]
	for _, verb := range AllVerbs {
		if !auth.Allows(scope.ID, verb) {
			t.Fatalf("the credential cannot %s %s (%q). Its principal owns that scope, so an "+
				"authority that refuses is a credential that authenticates and sees nothing — the "+
				"state that looks identical to a broken sign-in", verb, scope.ID, scope.Name)
		}
	}
	// 🔴 THE NEGATIVE CONTROL. Without it a `Resolve` that handed out everything would
	// satisfy every assertion above.
	if auth.Allows("scp_nobody_created_this", VerbRead) {
		t.Fatal("the authority answers yes for a scope id no event ever created")
	}

	// Pinned as a LITERAL rather than derived from the batch: user + project + member +
	// scope = 4, and this credential is the fifth event.
	if issued.Epoch != 5 {
		t.Fatalf("epoch = %d after provisioning (4 events) plus one credential", issued.Epoch)
	}
}

// TestTheMintedTokenIsExactlyTheDeclaredWidthAndAlphabet.
//
// ⚠ AN INVARIANT GUARD, NOT REGRESSION COVERAGE. No bug ever produced a wrong-width token
// here, because nothing produced a token at all before this change. What it pins is the
// agreement in the next test, which is the one that has a consumer.
func TestTheMintedTokenIsExactlyTheDeclaredWidthAndAlphabet(t *testing.T) {
	store, _, made := aProvisionedOwner(t)
	issued := issueTo(t, store, KindUser, made.User, nil)
	token := issued.Token()

	// 43 as a LITERAL, never `tokenChars`: deriving the expectation from the constant under
	// test makes this assertion true by construction for any value of it.
	if len(token) != 43 {
		t.Fatalf("the minted token is %d characters, want 43 — 256 bits of base64url without padding", len(token))
	}
	if _, err := base64.RawURLEncoding.DecodeString(token); err != nil {
		t.Fatalf("the minted token is not raw base64url (%v), so a value carrying `=` or `+` or `/` "+
			"could reach an Authorization header, a shell word and a line-oriented token file", err)
	}
}

// TestTheMintedWidthAgreesWithTheTokenFileFloor is a SEAM GUARD: neither package can see
// the other's constant, and the defect lives between them.
//
// 🔴 `internal/control` DELIBERATELY DOES NOT IMPORT `internal/authz` — the model holds no
// configuration and parses no token file — so nothing in the production graph makes these
// two numbers agree. They are two independent constants about one fact: the narrowest
// bearer token this repository will accept. A credential minted below `authz.MinTokenChars`
// would be issued by the tool whose job is to produce a working one and refused as
// guessable by the pod and by `cairn-ui` at startup, and both halves would be individually
// green.
func TestTheMintedWidthAgreesWithTheTokenFileFloor(t *testing.T) {
	if tokenChars != authz.MinTokenChars {
		t.Fatalf("control.tokenChars = %d and authz.MinTokenChars = %d. A credential minted here "+
			"must clear the floor every token-consuming surface in this repository refuses below; "+
			"move the entropy, not this assertion", tokenChars, authz.MinTokenChars)
	}
	// And the derivation itself, so that a future `TokenEntropyBytes` cannot satisfy the
	// line above by accident while rendering to a different width.
	if got := len(base64.RawURLEncoding.EncodeToString(make([]byte, TokenEntropyBytes))); got != tokenChars {
		t.Fatalf("%d entropy bytes render as %d characters, but tokenChars says %d", TokenEntropyBytes, got, tokenChars)
	}
}

// TestTwoIssuedCredentialsGetDifferentTokens is the control against a mint that is not a
// mint — a constant, a counter, or a buffer nothing filled.
//
// 🔴 A DETERMINISTIC MINT PASSES EVERY OTHER TEST IN THIS FILE. The token would be the
// right width, the right alphabet, absent from the journal and able to authenticate; the
// only observable is that a second issue produces the same secret, which is also the point
// at which the journal's digest-collision rule starts refusing ordinary work.
func TestTwoIssuedCredentialsGetDifferentTokens(t *testing.T) {
	store, _, made := aProvisionedOwner(t)
	first := issueTo(t, store, KindUser, made.User, nil)
	second := issueTo(t, store, KindUser, made.User, nil)

	if first.Token() == second.Token() {
		t.Fatal("two issued credentials carry the SAME token, so the mint is deterministic: every " +
			"credential this command ever issues is the same secret")
	}
	if first.TokenHash == second.TokenHash {
		t.Fatal("two issued credentials carry the same digest")
	}
	if first.Credential == second.Credential {
		t.Fatal("two issued credentials carry the same id")
	}
}

// TestNoRenderingOfIssuedContainsTheToken pins the redaction, across every verb that can
// reach a struct.
//
// 🔴 THE REALISTIC LEAK IS NOT A DELIBERATE PRINT; IT IS `%v` OF A VALUE THAT HAPPENS TO
// HOLD A SECRET — in an error path, a debug line, a test failure message. `%#v` is the one
// worth naming: it is the verb a reader reaches for precisely when they want every field,
// and a redaction that only implemented `String()` would leak there.
//
// ⚠ IT IS A CLAIM ABOUT FORMATTING, NOT A CONFIDENTIALITY BOUNDARY. `Token()` still
// returns the secret, which is the whole point; `Issued`'s own comment enumerates what
// this does not cover.
func TestNoRenderingOfIssuedContainsTheToken(t *testing.T) {
	store, _, made := aProvisionedOwner(t)
	issued := issueTo(t, store, KindUser, made.User, nil)
	token := issued.Token()

	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
		rendered := fmt.Sprintf(verb, issued)
		if strings.Contains(rendered, token) {
			t.Errorf("%s of an Issued contains the raw token", verb)
		}
		// A rendering that contained nothing at all would also pass the line above, so pin
		// that the redacted form still identifies the record it describes.
		if !strings.Contains(rendered, string(issued.Credential)) {
			t.Errorf("%s of an Issued does not name its credential id, so the redaction has taken "+
				"the log line's usefulness with it: %s", verb, rendered)
		}
	}
	// The same for a POINTER, which is what a caller holds more often than not.
	if strings.Contains(fmt.Sprintf("%v/%+v/%#v", &issued, &issued, &issued), token) {
		t.Error("a rendering of *Issued contains the raw token")
	}
	// And for an encoder, which reaches fields rather than methods. Unexported means this
	// drops the token rather than leaking it — the safe direction, pinned so that
	// "promoting" the field to exported for convenience is a red test.
	marshalled, err := json.Marshal(issued)
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	if strings.Contains(string(marshalled), token) {
		t.Error("json.Marshal of an Issued contains the raw token")
	}
	// The positive control on THIS test: the token must be findable somewhere, or every
	// assertion above is satisfied by a mint that returned the empty string.
	if token == "" {
		t.Fatal("POSITIVE CONTROL FAILED: the token is empty, so 'no rendering contains it' is vacuous")
	}
	if !strings.Contains(issued.Token(), token) {
		t.Fatal("Token() does not return the token")
	}
}

// TestASecondCredentialCarryingTheSameDigestIsRefused exercises the model's ambiguity rule
// against a digest this path actually minted.
//
// ⚠ IT APPENDS THE COLLIDING EVENT BY HAND, AND IT HAS TO. `IssueCredential` mints from
// `crypto/rand`, so it cannot produce a collision on purpose — which is exactly why the
// rule lives in `apply` and is reachable from any writer, including a hand-edited journal
// and a future import path.
func TestASecondCredentialCarryingTheSameDigestIsRefused(t *testing.T) {
	store, _, made := aProvisionedOwner(t)
	issued := issueTo(t, store, KindUser, made.User, nil)

	_, err := store.Append(context.Background(), Event{
		Kind: EventCredentialIssued, At: issueClock, CredentialID: "crd_second",
		SubjectKind: KindUser, SubjectID: made.User,
		TokenHash: issued.TokenHash, Label: "a second record for one secret",
	})
	if err == nil {
		t.Fatal("a second credential carrying an already-issued digest was accepted. One secret " +
			"bound to two principals has no defined precedence at authentication time, so the " +
			"journal has to refuse it rather than let `Authenticate` pick by iteration order.")
	}
	if !strings.Contains(err.Error(), "same token digest") {
		t.Fatalf("refusal = %v, want one naming the shared digest", err)
	}
}

// TestANarrowingRoundTripsThroughTheJournal covers all three states of the field, and the
// third is the one that has to be measured rather than reasoned about.
//
// 🔴 `nil` AND A NON-NIL EMPTY SLICE ARE OPPOSITES, AND THE ROUND TRIP IS WHERE THEY
// COLLAPSE. The value goes through `copyIDs`, `encoding/json`, a file, `ReadEvents` and
// `apply` before anything authenticates with it; a single `append([]ID(nil), …)` anywhere
// on that path turns "this credential sees nothing" into "this credential is not narrowed
// at all", which is a silent WIDENING of the one field whose purpose is to restrict.
func TestANarrowingRoundTripsThroughTheJournal(t *testing.T) {
	store, path, made := aProvisionedOwner(t)
	scope := made.Scopes[0].ID

	for _, tc := range []struct {
		name     string
		narrowed []ID
		reaches  bool
	}{
		{"nil narrows nothing", nil, true},
		{"a subset keeps exactly that subset", []ID{scope}, true},
		// The id is well-formed and names no scope: a narrowing must INTERSECT, so this
		// confers nothing rather than granting an unknown scope.
		{"a scope the principal does not hold confers nothing", []ID{"scp_not_in_this_journal"}, false},
		{"an EMPTY non-nil narrowing sees nothing", []ID{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			issued := issueTo(t, store, KindUser, made.User, tc.narrowed)

			reread, err := OpenFileStore(path)
			if err != nil {
				t.Fatalf("re-opening: %v", err)
			}
			m, err := reread.Model(context.Background())
			if err != nil {
				t.Fatalf("replaying: %v", err)
			}
			_, auth, err := Authenticate(m, issued.Token())
			if err != nil {
				t.Fatalf("the credential does not authenticate: %v", err)
			}
			if got := auth.Allows(scope, VerbRead); got != tc.reaches {
				t.Fatalf("the credential reads %s = %v, want %v", scope, got, tc.reaches)
			}
		})
	}
}

// TestIssuingToAPrincipalTheJournalDoesNotHoldIsRefusedBeforeAnythingIsWritten.
//
// 🔴 THE SENTINEL IS THE ASSERTION, NOT MERELY "IT ERRORED". `Append` refuses this batch
// too, with `apply`'s own "subject user … does not exist" — so a test that accepted any
// error would be green with the pre-check deleted, and the pre-check's two reasons (no
// secret is minted for a doomed request; an operator surface can tell a typo from a broken
// volume) would both be unguarded.
func TestIssuingToAPrincipalTheJournalDoesNotHoldIsRefusedBeforeAnythingIsWritten(t *testing.T) {
	store, path, _ := aProvisionedOwner(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the journal: %v", err)
	}

	for _, tc := range []struct {
		name string
		kind Kind
		id   ID
	}{
		{"a user id nothing created", KindUser, "usr_nobody"},
		{"a project id nothing created", KindProject, "prj_nobody"},
		{"a kind the model does not define", Kind("robot"), "usr_nobody"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := IssueCredential(context.Background(), store, NewCredential{
				SubjectKind: tc.kind, SubjectID: tc.id, At: issueClock,
			})
			if !errors.Is(err, ErrNoSuchPrincipal) {
				t.Fatalf("err = %v, want it to wrap ErrNoSuchPrincipal", err)
			}
		})
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("re-reading the journal: %v", err)
	}
	if string(before) != string(after) {
		t.Fatal("a refused issue changed the journal. The file is append-only and holds an " +
			"authority; a refusal that writes is a record of a credential nobody holds.")
	}
}

// TestA64CharacterRawTokenIsRefusedAsADigest is the REGRESSION TEST for the guard this
// change widened, and its fixture is a realistic value rather than a textbook one.
//
// 🔴 THE PRE-CHANGE CHECK WAS `len(e.TokenHash) != HashHexLen`, AND ITS OWN MESSAGE SAID A
// SHORT HASH "IS THE SHAPE A RAW TOKEN TAKES" — which is true of the case it caught and
// false of the case that matters. `base64.RawURLEncoding` of 48 random bytes is EXACTLY 64
// characters, and 48 bytes is an ordinary width for a machine-minted token, so a raw secret
// had a natural spelling that cleared a length check and was persisted verbatim into the
// append-only authority journal.
//
// 🔴 THE FIXTURE VALIDATES ITSELF FIRST. A negative control built out of a value that is
// the wrong length would be caught by the OLD check too, and this test would then be green
// at the base commit while claiming to measure the new one. So the two properties that make
// it a real case — exactly `HashHexLen` characters, and not hex — are asserted before the
// guard is exercised.
//
// ⚠ AND IT IS SYNTHETIC. The bytes are a fixed pattern, not a captured or generated
// credential; nothing here has ever authorised anything.
func TestA64CharacterRawTokenIsRefusedAsADigest(t *testing.T) {
	// 48 bytes → 64 base64url characters. A repeating pattern rather than random, so the
	// case is reproducible and obviously not a real secret.
	pattern := make([]byte, 48)
	for i := range pattern {
		pattern[i] = byte(i * 7)
	}
	plausibleRawToken := base64.RawURLEncoding.EncodeToString(pattern)

	if len(plausibleRawToken) != HashHexLen {
		t.Fatalf("the fixture is %d characters, and the case only exists at %d: a value of any "+
			"other length was already refused by the length check this test is about",
			len(plausibleRawToken), HashHexLen)
	}
	if isHexDigest(plausibleRawToken) {
		t.Fatal("the fixture happens to be valid hex, so it is not a case the widened " +
			"guard can distinguish from a real digest — pick another pattern")
	}

	line := fmt.Sprintf(
		`{"kind":"credential-issued","at":"2000-01-01T00:00:00Z","credential_id":"crd_x","subject_kind":"user","subject_id":"usr_alice","token_hash":%q,"narrowed_scopes":null}`,
		plausibleRawToken)
	events, err := ReadEvents(strings.NewReader(line))
	if err != nil {
		t.Fatalf("the line did not even decode: %v", err)
	}
	if _, err := Replay(events); err == nil {
		t.Fatal("a 64-character RAW TOKEN was accepted as a token digest and replayed into the " +
			"authority model. The journal is append-only and operator-readable, so a secret that " +
			"reaches it cannot be taken back.")
	} else if !strings.Contains(err.Error(), "hex digest") {
		t.Fatalf("refusal = %v, want one naming the hex requirement — a refusal for some other "+
			"reason would leave this guard unmeasured", err)
	}

	// The positive control: a REAL digest of the same width must still be accepted, or the
	// widened guard is simply refusing every credential event.
	good := strings.Replace(line, plausibleRawToken, HashToken("cairn-test-not-a-real-token"), 1)
	goodEvents, err := ReadEvents(strings.NewReader(good))
	if err != nil {
		t.Fatalf("decoding the control line: %v", err)
	}
	if err := goodEvents[0].validate(); err != nil {
		t.Fatalf("POSITIVE CONTROL FAILED: a genuine sha256 hex digest was refused (%v), so every "+
			"refusal above is about a guard that admits nothing", err)
	}
}

// handAppendedToken is the secret the two cases below pretend an operator hashed by hand.
//
// ⚠ SYNTHETIC AND FIXED: it is a literal in a public repository, so it has never
// authorised anything anywhere. Its width matches what the mint produces only so that the
// fixture looks like the thing it stands for.
const handAppendedToken = "cairn-test-hand-appended-credential-0000000"

// TestAnUppercaseDigestReplaysAndTheCredentialItNamesAuthenticates is the REGRESSION TEST
// for a refusal an earlier draft of the digest guard introduced, and the arm that matters
// is the WHOLE JOURNAL rather than the one row.
//
// 🔴 THE CHECK RUNS ON REPLAY, SO ITS BLAST RADIUS IS THE FILE AND NOT THE RECORD.
// `Event.validate` is reached through `Model.apply` ← `Replay`, which fails a journal
// WHOLE. Measured on the lowercase-only draft: a journal holding one hand-written
// uppercase `credential-issued` record beside a perfectly good credential returned an
// error from `Model()` and loaded ZERO credentials — and through `FileStore.Reload` that
// is a pod's entire control-plane authority falling back to `lastKnownGood()`, which is
// empty on a cold start. So the assertion that the OTHER credential still authenticates is
// load-bearing rather than decorative.
//
// 🔴 AND THE SECOND LOAD-BEARING ASSERTION IS THAT THE UPPERCASE ONE AUTHENTICATES, NOT
// MERELY THAT IT LOADS. Accepting the spelling without lowering it in `apply` satisfies
// every "the journal replays" assertion while leaving the credential dead: `EqualHash` is
// byte-exact and `HashToken` emits lowercase, so an uppercase digest stored verbatim
// matches no presented token ever. That half is what makes this a FIX rather than a
// widening.
//
// ⚠ THE RECORD IS HAND-APPENDED, WHICH IS THE ONLY WAY THIS POPULATION EXISTS. Nothing in
// this repository has ever written an uppercase digest; the rows in the wild were written
// by operators following a refusal text that prescribed a hand-appended record, and both
// `Get-FileHash` and `certutil -hashfile` emit uppercase.
func TestAnUppercaseDigestReplaysAndTheCredentialItNamesAuthenticates(t *testing.T) {
	store, path, made := aProvisionedOwner(t)
	good := issueTo(t, store, KindUser, made.User, nil)

	upper := strings.ToUpper(HashToken(handAppendedToken))
	line := fmt.Sprintf(
		`{"kind":"credential-issued","at":"2000-01-01T00:00:00Z","credential_id":"crd_handappended","subject_kind":"user","subject_id":%q,"token_hash":%q,"label":"hand-appended by an operator"}`+"\n",
		string(made.User), upper)
	// Appended to the FILE rather than through `Append`, because the population this test
	// is about did exactly that: `Append` runs `validate`, so a record written through it
	// is a record this build already accepted.
	fh, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("opening the journal to hand-append: %v", err)
	}
	if _, err := fh.WriteString(line); err != nil {
		t.Fatalf("hand-appending: %v", err)
	}
	if err := fh.Close(); err != nil {
		t.Fatalf("closing the journal: %v", err)
	}

	reread, err := OpenFileStore(path)
	if err != nil {
		t.Fatalf("re-opening the journal: %v", err)
	}
	m, err := reread.Model(context.Background())
	if err != nil {
		t.Fatalf("a journal holding ONE uppercase token_hash would not replay (%v). The check is "+
			"reached through `Model.apply`, so this is not one refused record — it is every "+
			"credential in the file, and through `FileStore.Reload` an authority that falls back "+
			"to an empty `lastKnownGood()` on a cold start", err)
	}
	if len(m.Credentials) != 2 {
		t.Fatalf("the model holds %d credential(s), want 2 — the issued one and the hand-appended one", len(m.Credentials))
	}

	p, _, err := Authenticate(m, handAppendedToken)
	if err != nil {
		t.Fatalf("the hand-appended credential does not AUTHENTICATE (%v). Admitting the uppercase "+
			"spelling without lowering it in `apply` leaves the record replaying clean and matching "+
			"no token, because `EqualHash` is byte-exact and `HashToken` emits lowercase", err)
	}
	if p.CredentialID != "crd_handappended" || p.ID != made.User {
		t.Fatalf("it authenticated as %+v, want credential crd_handappended for %s", p, made.User)
	}
	if got := m.Credentials["crd_handappended"].TokenHash; got != HashToken(handAppendedToken) {
		t.Fatalf("the model holds the digest as %q, want the lowercase spelling — the model is the "+
			"one place a single secret may have a single spelling", got)
	}

	// 🔴 THE WHOLE-FILE CLAIM, which is the one the outage was about: the credential that
	// was already there is unaffected.
	if _, _, err := Authenticate(m, good.Token()); err != nil {
		t.Fatalf("the credential issued BEFORE the hand-appended row no longer authenticates: %v", err)
	}

	// And the file itself was not rewritten: the journal is append-only, so the operator's
	// own spelling is still what is on disk. Without this, "normalised" could mean the
	// model quietly editing a durable record.
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the journal back: %v", err)
	}
	if !strings.Contains(string(body), upper) {
		t.Fatal("the uppercase digest is no longer in the journal file, so something rewrote an " +
			"append-only record rather than normalising what the model holds")
	}
}

// TestOneSecretInTwoSpellingsIsStillRefusedAsADuplicate is the second half of the
// normalisation, and it fails in the DANGEROUS direction if the first half is done alone.
//
// 🔴 THE DUPLICATE REFUSAL IN `apply` IS A STRING COMPARE. Accept both cases without
// lowering, and the same secret recorded twice in two spellings is two rows the model
// takes — the exact state that loop's own comment refuses, because two principals sharing
// one digest have no defined precedence at authentication time and `Resolve` would pick by
// iteration order.
//
// ⚠ AT THE LOWERCASE-ONLY DRAFT THIS CASE WAS REFUSED FOR A DIFFERENT REASON — the shape
// check, not the duplicate rule — which is why the message is asserted rather than the
// error's existence. A test that accepted any error here would be green on both the draft
// and on an accept-both-cases build that had lost the duplicate rule entirely.
func TestOneSecretInTwoSpellingsIsStillRefusedAsADuplicate(t *testing.T) {
	store, _, made := aProvisionedOwner(t)
	issued := issueTo(t, store, KindUser, made.User, nil)

	_, err := store.Append(context.Background(), Event{
		Kind: EventCredentialIssued, At: issueClock, CredentialID: "crd_thesamesecret",
		SubjectKind: KindUser, SubjectID: made.User,
		TokenHash: strings.ToUpper(issued.TokenHash), Label: "one secret, shouted",
	})
	if err == nil {
		t.Fatal("a second credential carrying the SAME digest in uppercase was accepted. One secret " +
			"bound to two principals has no defined precedence at authentication time, and a case " +
			"difference must not be what reaches that state.")
	}
	if !strings.Contains(err.Error(), "same token digest") {
		t.Fatalf("refusal = %v, want the DUPLICATE rule rather than some other refusal — the "+
			"digest-shape check refuses this too, and a test satisfied by that would be green on a "+
			"build whose duplicate rule could no longer see a spelling difference", err)
	}
}
