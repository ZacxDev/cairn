package control

import (
	"context"
	"encoding/base64"
	"encoding/hex"
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

// everyFormattingVerb is every `fmt` verb an `Issued` can be handed to.
//
// 🔴 THE LIST IS THE WHOLE POINT, AND THE PREVIOUS VERSION OF THIS TEST NAMED FIVE. It
// ranged over `%v %+v %#v %s %q` — exactly the verbs `fmt` routes through `Stringer` and
// `GoStringer` — under a name and a docstring claiming "across every verb that can reach a
// struct". Measured on the tree that shipped it: **14 of these 22 rendered the raw token**
// (`%d %b %o %O %c %U %e %E %f %F %g %G %t %p`), because `fmt` consults `Stringer` for that
// handful and REFLECTS the operand for everything else — and a reflected struct prints its
// unexported field's value inside `%!d(string=…)`. A guard narrower than its own
// description is the shape this repository keeps closing; this is the list that makes the
// sentence true.
//
// ⚠ `%w` IS ABSENT AND IS NOT AN OMISSION: it is `fmt.Errorf`'s wrapping verb, valid only
// there and only for an `error`, which `Issued` is not.
var everyFormattingVerb = []string{
	"%v", "%+v", "%#v", "%s", "%q", "%d", "%b", "%o", "%O", "%x", "%X",
	"%c", "%U", "%e", "%E", "%f", "%F", "%g", "%G", "%t", "%p", "%T",
}

// tokenSpellings is every rendering of a secret this sweep can recognise.
//
// 🔴 A SUBSTRING SEARCH FOR THE RAW TOKEN IS BLIND TO AN ENCODED COPY, AND `fmt` HAS TWO
// THAT MATTER. `%x`/`%X` of a string emit its bytes as hex and `%d` of a byte slice emits
// them as decimal — both are the secret, and neither contains it as a substring. The sweep
// is only as wide as the spellings it knows, which is itself a declared limit rather than a
// claim of completeness.
func tokenSpellings(token string) map[string]string {
	hexed := hex.EncodeToString([]byte(token))
	decimal := make([]string, 0, len(token))
	for i := 0; i < len(token); i++ {
		decimal = append(decimal, fmt.Sprintf("%d", token[i]))
	}
	return map[string]string{
		"raw":            token,
		"hex":            hexed,
		"HEX":            strings.ToUpper(hexed),
		"decimal bytes":  strings.Join(decimal, " "),
		"quoted decimal": "[" + strings.Join(decimal, " ") + "]",
	}
}

// leakyTwin is this sweep's POSITIVE CONTROL: a struct that holds the same secret with no
// redaction at all.
//
// 🔴 WITHOUT IT, "NO VERB LEAKED" IS INDISTINGUISHABLE FROM A SWEEP WIRED TO NOTHING — a
// misspelled verb list, a `Sprintf` whose result is discarded, a `Contains` with the
// operands the wrong way round. The control must move the number: it is required to leak at
// SOME (shape, verb) pair, and the pair is what the failure message reports.
type leakyTwin struct{ Token string }

// hiddenIn holds its operand in an UNEXPORTED field, which is the shape that made the
// previous version of this sweep structurally blind.
//
// 🔴 `fmt` CALLS A VALUE'S FORMATTING METHODS ONLY WHEN IT CAN `Interface()` THAT VALUE,
// AND A FIELD REACHED BY REFLECTION THROUGH AN UNEXPORTED NAME CANNOT BE INTERFACED. So an
// `Issued` sitting here is NEVER handed to `Issued.Format`, `String` or `GoString` however
// wide those are: `fmt` walks into its fields directly. That is the same mechanism that
// produced the original leak one level down, and it is why the redaction that actually
// closes the hole is the POINTER on the token field rather than any method.
type hiddenIn[T any] struct{ hidden T }

// shapedOperand is one way an operand can reach `fmt`, with the name the failure message
// reports it by.
type shapedOperand struct {
	shape   string
	operand any
}

// leakShapes is every SHAPE this sweep hands to `fmt`, and the list is the whole point of
// the widening.
//
// 🔴 THE PREVIOUS SWEEP RENDERED AT DEPTH 0 ONLY — the value and a pointer to it — while
// its own docstring called the nested case "the realistic leak". `fmt` dispatches to a
// `Formatter` only for a value it can `Interface()`, and it follows a POINTER only at depth
// 0; both of those facts are invisible to a depth-0 sweep, and between them they decide
// which half of this type's defence is load-bearing. Measured with the sweep widened to
// these seven shapes: reverting `token *string` to `token string` leaks at **24 of 154
// (shape, verb) pairs** — 21 verbs (every one but `%T`) through an `Issued` in an unexported
// field, where no method of any kind is consulted, plus `%p` on the value, on an exported
// field and on an interface field — while removing `Format` entirely and keeping the pointer
// leaks at **0**. The depth-0 sweep could see neither number.
//
// ⚠ EARLIER DRAFTS OF THIS DOCSTRING SAID 22 AND 19, WHICH WAS A NARROWER SWEEP'S NUMBER AND
// NOT THIS ONE'S. Run the revert and read what this test PRINTS; every other site quoting it
// says 24/21/3, and a docstring disagreeing with the test it documents is the worse copy.
//
// ⚠ SEVEN SHAPES, NOT AN EXHAUSTIVE SET. `fmt`'s reflection walker recurses without a depth
// limit, so no finite list is complete; what these cover is one representative of each way
// the walker can reach a value — addressable and not, interfaceable and not, through a
// struct field, a slice element, a map value and an interface. A shape nobody wrote here is
// a shape this sweep does not measure, which is a declared limit rather than a claim.
func leakShapes[T any](v T) []shapedOperand {
	return []shapedOperand{
		{"the value", v},
		{"a pointer to it", &v},
		// The two that decide the question, and they answer differently: `fmt` can
		// `Interface()` an exported field and cannot an unexported one.
		{"an UNEXPORTED field of another struct", hiddenIn[T]{hidden: v}},
		{"an exported field of another struct", struct{ Shown T }{Shown: v}},
		{"a slice element", []T{v}},
		{"a map value", map[string]T{"k": v}},
		{"an interface field", struct{ Held any }{Held: v}},
	}
}

// leakingRenderings renders every shape through every verb and returns the (shape, verb)
// pairs whose output carries the secret in any spelling.
//
// ⚠ IT NEVER RETURNS THE RENDERED TEXT, AND THAT IS DELIBERATE RATHER THAN TERSE. A failure
// here means a live credential is in the formatted output; interpolating it into
// `t.Errorf` would re-stage the secret into the test log, the CI transcript and any agent
// session capturing the run — the guard against printing a token, printing the token.
func leakingRenderings(shapes []shapedOperand, token string) []string {
	spellings := tokenSpellings(token)
	var leaked []string
	for _, s := range shapes {
		for _, verb := range everyFormattingVerb {
			rendered := fmt.Sprintf(verb, s.operand)
			for name, spelling := range spellings {
				if strings.Contains(rendered, spelling) {
					leaked = append(leaked, s.shape+" "+verb+" ("+name+")")
					break
				}
			}
		}
	}
	return leaked
}

// TestNoRenderingOfIssuedContainsTheToken pins the redaction, across every verb that can
// reach a struct — which is now what it measures rather than what it said.
//
// 🔴 THE REALISTIC LEAK IS NOT A DELIBERATE PRINT; IT IS `%v` OF A VALUE THAT HAPPENS TO
// HOLD A SECRET — in an error path, a debug line, a test failure message. `%#v` is the one
// worth naming among the five the old list covered: it is the verb a reader reaches for
// precisely when they want every field, and a redaction that only implemented `String()`
// would leak there.
//
// 🔴 AND THE VERBS THE OLD LIST DID NOT COVER ARE WHERE THE FIRST MEASURED LEAK WAS. `fmt`
// consults `Stringer` only for `%v %s %q %x %X` and `GoStringer` only for `%#v`; every
// other verb reflects the operand and prints the unexported field's value inside
// `%!d(string=…)`. 14 of 22 leaked at `0fb61d4`, at depth 0.
//
// 🔴 AND THE SWEEP IS NOW SHAPED AS WELL AS VERBED, BECAUSE THE DEPTH-0 VERSION CREDITED
// THE WRONG HALF. It rendered only the value and a pointer to it, so it could not see an
// `Issued` nested inside another struct — the case its own docstring called realistic — and
// on that evidence three sites said `Format` closed 21 of 22 and the pointer the
// twenty-second. Re-measured over seven shapes × 22 verbs: with `Format` present and
// `token` reverted to a plain `string`, **24 of 154 (shape, verb) pairs leak**, 21 of them
// verbs reached through an UNEXPORTED field where `fmt` consults no method at all; with the
// pointer present and `Format` deleted entirely, **0**. 🔴 **THE POINTER IS WHAT CLOSES THE
// HOLE.** `Format` earns its place by rendering a redacted line instead of a raw address,
// and as defence in depth — not as the half that makes this test pass.
//
// ⚠ IT IS A CLAIM ABOUT FORMATTING, NOT A CONFIDENTIALITY BOUNDARY. `Token()` still
// returns the secret, which is the whole point; `Issued`'s own comment enumerates what
// this does not cover.
func TestNoRenderingOfIssuedContainsTheToken(t *testing.T) {
	store, _, made := aProvisionedOwner(t)
	issued := issueTo(t, store, KindUser, made.User, nil)
	token := issued.Token()

	// The positive control on the FIXTURE, first: every assertion below is satisfied by a
	// mint that returned the empty string, and `strings.Contains(x, "")` is always true.
	if token == "" {
		t.Fatal("POSITIVE CONTROL FAILED: the token is empty, so 'no rendering contains it' is vacuous")
	}
	if issued.Token() != token {
		t.Fatal("Token() does not return the token")
	}

	shapes := leakShapes(issued)
	pairs := len(shapes) * len(everyFormattingVerb)

	// The positive control on the SWEEP: an unredacted twin must be seen leaking, or a
	// clean result below is a fact about the instrument rather than about `Issued`.
	//
	// 🔴 IT IS SWEPT THROUGH THE SAME SHAPES, WHICH IS WHAT MAKES IT A CONTROL ON THE
	// WIDENING AND NOT ONLY ON THE VERB LIST. A twin rendered at depth 0 alone would leak
	// loudly while saying nothing about whether the nested shapes are wired to anything.
	twinShapes := leakShapes(leakyTwin{Token: token})
	control := leakingRenderings(twinShapes, token)
	if len(control) == 0 {
		t.Fatal("POSITIVE CONTROL FAILED: the sweep found no leak in a struct that holds the raw " +
			"token in an EXPORTED field, so it cannot see one anywhere and every clean result it " +
			"reports is about nothing")
	}
	// And it must leak through the NESTED shapes specifically, or the widening is inert: a
	// builder that returned seven copies of the depth-0 value would satisfy the count above.
	nested := 0
	for _, pair := range control {
		if strings.HasPrefix(pair, "the value ") || strings.HasPrefix(pair, "a pointer to it ") {
			continue
		}
		nested++
	}
	if nested == 0 {
		t.Fatal("POSITIVE CONTROL FAILED: the twin leaks only at depth 0, so the five nested shapes " +
			"are rendering something the sweep cannot see and the widening measures nothing")
	}

	if leaked := leakingRenderings(shapes, token); len(leaked) > 0 {
		t.Errorf("%d of %d (shape, verb) pairs render the RAW TOKEN of an Issued: %v\n"+
			"(the control twin leaked at %d of %d, %d of them nested, so the sweep works)\n"+
			"A method-based redaction is consulted only where `fmt` can `Interface()` the value, "+
			"which an UNEXPORTED field never is — so the half that closes the hole is `token` "+
			"being a POINTER, which `fmt` renders as an address and follows only at depth 0. The "+
			"rendered text is deliberately NOT printed here: it holds a live credential.",
			len(leaked), pairs, leaked, len(control), pairs, nested)
	}

	// A rendering that contained nothing at all would satisfy the sweep above, so pin that
	// the redacted form still identifies the record it describes. Not every verb can: `%T`
	// renders a type name and `%p` an address-or-error, both by `fmt`'s own design.
	//
	// ⚠ AND `%x`/`%X` CARRY THE ID HEX-ENCODED RATHER THAN NOT AT ALL, WHICH IS WHAT `fmt`
	// DOES WITH A `Stringer`'S RESULT UNDER THOSE VERBS AND IS THEREFORE WHAT `Format` HAS
	// TO REPRODUCE. Measured before this test was widened: `%x` of an `Issued` was already
	// the hex of `String()`, so accepting only the raw spelling here would red on
	// unchanged, correct behaviour.
	id := string(issued.Credential)
	idHex := hex.EncodeToString([]byte(id))
	for _, verb := range everyFormattingVerb {
		if verb == "%T" || verb == "%p" {
			continue
		}
		rendered := fmt.Sprintf(verb, issued)
		if !strings.Contains(rendered, id) &&
			!strings.Contains(rendered, idHex) &&
			!strings.Contains(rendered, strings.ToUpper(idHex)) {
			t.Errorf("%s of an Issued does not name its credential id, so the redaction has taken "+
				"the log line's usefulness with it: %s", verb, rendered)
		}
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
}

// stringerOnly renders exactly what `Issued.String()` does, through `Stringer` and nothing
// else. It is the CONTROL for the test below: what `fmt` would have done with these
// spellings if `Format` were not in the way.
type stringerOnly struct{ rendered string }

func (s stringerOnly) String() string { return s.rendered }

// TestIssuedFormatDropsEveryFlagAndWidth pins the narrowing `Format`'s own comment declares.
//
// ⚠ AN INVARIANT GUARD, NOT REGRESSION COVERAGE. No caller in this repository flags or pads
// an `Issued`, so no defect ever came of this. It exists because the sentence beside the
// method was WRONG about it for two rounds — it said the six verbs `Stringer` already
// handled "keep their EXACT output", which is true unflagged and false for `%#q`, `%#x`,
// `% x` and `%.5s` — and a claim that cannot go red is a claim nobody re-measures.
//
// 🔴 EACH ARM CARRIES ITS OWN POSITIVE CONTROL, WHICH IS THE WHOLE INSTRUMENT. "The flagged
// spelling equals the unflagged one" is satisfied by a flag that does nothing to THIS
// string — a width narrower than the line, say — so every arm first requires the same two
// spellings to DIFFER on a `Stringer` carrying identical text. Without that pair the test
// would be green against a `Format` that honoured every flag perfectly.
func TestIssuedFormatDropsEveryFlagAndWidth(t *testing.T) {
	store, _, made := aProvisionedOwner(t)
	issued := issueTo(t, store, KindUser, made.User, nil)
	control := stringerOnly{rendered: issued.String()}

	for _, tc := range []struct{ flagged, bare string }{
		{"%#q", "%q"},
		{"%#x", "%x"},
		{"% x", "%x"},
		{"%.5s", "%s"},
		{"%200s", "%s"},
	} {
		t.Run(tc.flagged, func(t *testing.T) {
			if fmt.Sprintf(tc.flagged, control) == fmt.Sprintf(tc.bare, control) {
				t.Fatalf("POSITIVE CONTROL FAILED: %s and %s of a plain Stringer carrying the same "+
					"text are already identical, so this arm cannot tell a dropped flag from a flag "+
					"that never did anything", tc.flagged, tc.bare)
			}
			flagged, bare := fmt.Sprintf(tc.flagged, issued), fmt.Sprintf(tc.bare, issued)
			if flagged != bare {
				t.Fatalf("%s of an Issued is not %s of one, so `Format` now honours a flag its own "+
					"comment declares dropped:\n %s => %q\n %s => %q",
					tc.flagged, tc.bare, tc.flagged, flagged, tc.bare, bare)
			}
		})
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
// 🔴 AND WHAT "REFUSED" MEANS AT REPLAY MOVED, WHICH IS WHY THIS TEST ASSERTS THE RECORD
// AND NOT THE ERROR. `Event.validate` still refuses the value from every writer — that is
// the boundary claim, and it is what `Append` enforces — but `Replay` now DROPS the record
// and loads the rest of the file rather than refusing it whole, because a replay refusal
// takes the operator's entire control plane with it and this record can authenticate
// nobody. The property this test exists for is unchanged: the raw secret never enters the
// authority model.
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
	// The BOUNDARY refusal, which is what every WRITER meets — `Append` validates a batch
	// before a byte reaches disk, so this is the check that keeps such a value out of the
	// file in the first place.
	if err := events[0].validate(); err == nil {
		t.Fatal("a 64-character RAW TOKEN was accepted as a token digest. The journal is " +
			"append-only and operator-readable, so a secret that reaches it cannot be taken back.")
	} else if !strings.Contains(err.Error(), "hex digest") {
		t.Fatalf("refusal = %v, want one naming the hex requirement — a refusal for some other "+
			"reason would leave this guard unmeasured", err)
	} else if !errors.Is(err, ErrUnusableTokenDigest) {
		t.Fatalf("refusal = %v, want it to wrap ErrUnusableTokenDigest — that sentinel is what "+
			"`Replay` matches on to drop the record rather than the file, and a refusal that only "+
			"reads right is a refusal `replayDroppable` cannot see", err)
	}

	// And at REPLAY the record is dropped, not the file — the property that keeps a pod's
	// whole authority from disappearing over one bad line somebody already wrote.
	m, err := Replay(events)
	if err != nil {
		t.Fatalf("replay = %v, want the file loaded with that one record dropped. A replay-time "+
			"refusal is reached through `Model.apply` and fails the journal WHOLE, so through "+
			"`FileStore.Reload` it is every credential in the file, falling back to a "+
			"`lastKnownGood()` that is empty on a cold start", err)
	}
	if len(m.Credentials) != 0 {
		t.Fatal("the raw token was replayed INTO the authority model. Dropping the record is what " +
			"this is about; admitting it is the defect the widened guard exists for.")
	}
	if len(m.Dropped) != 1 || m.Dropped[0].CredentialID != "crd_x" ||
		!strings.Contains(m.Dropped[0].Reason, "hex digest") {
		t.Fatalf("Dropped = %v, want exactly crd_x with the hex reason — a record dropped silently "+
			"is an authority quietly narrower than the file it claims to project", m.Dropped)
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

// TestAppendStillRefusesWhatReplayWouldDrop is the APPEND half of the replay exemption, and
// it is the assertion that says the exemption did not widen into a write path.
//
// 🔴 THE SPLIT IS THE WHOLE DESIGN: a replay answers for a file somebody ALREADY wrote, an
// append decides whether one more line goes in. `FileStore.Append` validates a batch against
// a clone by calling `apply` directly and never consults `replayDroppable`, so a writer
// trying to CREATE either of the droppable records is refused outright — and the journal on
// disk is unchanged, which is the second assertion here.
//
// ⚠ AN INVARIANT GUARD FOR THE DUPLICATE ROW AND REGRESSION-ADJACENT FOR THE DIGEST ROW:
// neither refusal is new, and what is new is the possibility of losing one by accident while
// making `Replay` lenient. `TestOneSecretInTwoSpellingsIsStillRefusedAsADuplicate` covers the
// case-folded spelling of the duplicate through the same path.
func TestAppendStillRefusesWhatReplayWouldDrop(t *testing.T) {
	store, path, made := aProvisionedOwner(t)
	issued := issueTo(t, store, KindUser, made.User, nil)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the journal: %v", err)
	}

	for _, tc := range []struct {
		name  string
		event Event
		want  string
	}{
		{
			"a token_hash this build cannot use",
			Event{Kind: EventCredentialIssued, At: issueClock, CredentialID: "crd_unusable",
				SubjectKind: KindUser, SubjectID: made.User,
				TokenHash: strings.Repeat("cairn-test-not-a-digest-", 3)[:HashHexLen]},
			"hex digest",
		},
		{
			"a digest the journal already carries",
			Event{Kind: EventCredentialIssued, At: issueClock, CredentialID: "crd_duplicate",
				SubjectKind: KindUser, SubjectID: made.User, TokenHash: issued.TokenHash},
			"same token digest",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := store.Append(context.Background(), tc.event); err == nil {
				t.Fatal("the append was ACCEPTED. `Replay` drops these records for a file that " +
					"already holds them; a writer creating one is a different question and is still " +
					"refused, because nothing forces anybody to write it")
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("refusal = %v, want one naming %q", err, tc.want)
			}
		})
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("re-reading the journal: %v", err)
	}
	if string(before) != string(after) {
		t.Fatal("a refused append changed the journal. `Append` validates against a CLONE before a " +
			"byte reaches disk, and a refusal that writes is a record no replay can take back.")
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
