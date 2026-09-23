package control

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// TestTheJournalRoundTrips is the durability claim: what is written is what comes
// back.
func TestTheJournalRoundTrips(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteEvents(&buf, worldEvents()); err != nil {
		t.Fatalf("write: %v", err)
	}
	back, err := ReadEvents(&buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(back) != len(worldEvents()) {
		t.Fatalf("wrote %d events, read %d", len(worldEvents()), len(back))
	}
	original, err := Replay(worldEvents())
	if err != nil {
		t.Fatalf("replay original: %v", err)
	}
	reloaded, err := Replay(back)
	if err != nil {
		t.Fatalf("replay round-tripped: %v", err)
	}
	// Compare through the thing that matters — the authority — rather than by
	// reflect-deep-equalling two Models. A structural comparison would pass on a
	// world that round-tripped its fields and produced different answers, which is
	// the failure this format has to not have.
	for _, principal := range sortedLedgerPrincipals() {
		a := Resolve(original, user(principal))
		b := Resolve(reloaded, user(principal))
		for scopeID := range expectedMatrix[principal] {
			if verbsOrEmpty(a.VerbsOn(scopeID)) != verbsOrEmpty(b.VerbsOn(scopeID)) {
				t.Errorf("%s on %s: %q before the round trip, %q after",
					principal, scopeID, verbsOrEmpty(a.VerbsOn(scopeID)), verbsOrEmpty(b.VerbsOn(scopeID)))
			}
		}
	}
	if original.Epoch != reloaded.Epoch {
		t.Errorf("epoch %d before, %d after", original.Epoch, reloaded.Epoch)
	}
}

// TestAnEmptyNarrowingSurvivesTheDurableFormat is the `omitempty` trap, pinned.
//
// 🔴 A NON-NIL EMPTY `narrowed_scopes` MEANS "NOTHING IS VISIBLE" AND `nil` MEANS
// "NO NARROWING". With `omitempty` on the field, the empty case would be dropped
// from the line and read back as nil — a silent WIDENING, through the durable
// format, in the one field whose entire purpose is to restrict. The two arms below
// are the only thing standing between that tag and the authority.
func TestAnEmptyNarrowingSurvivesTheDurableFormat(t *testing.T) {
	cases := []struct {
		name string
		in   []ID
		want string
	}{
		{"no narrowing", nil, "null"},
		{"narrowed to nothing", []ID{}, "[]"},
		{"narrowed to one scope", []ID{sBeaconNotes}, `["` + string(sBeaconNotes) + `"]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := Event{Kind: EventCredentialIssued, At: at(1), CredentialID: "crd_x",
				SubjectKind: KindUser, SubjectID: uCarol,
				TokenHash: HashToken("x"), NarrowedScopes: tc.in}
			line, err := json.Marshal(e)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if !strings.Contains(string(line), `"narrowed_scopes":`+tc.want) {
				t.Fatalf("the line does not carry narrowed_scopes:%s — got %s", tc.want, line)
			}
			var back Event
			if err := json.Unmarshal(line, &back); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if (back.NarrowedScopes == nil) != (tc.in == nil) {
				t.Fatalf("nil-ness did not survive: in=%v(nil=%v) out=%v(nil=%v)",
					tc.in, tc.in == nil, back.NarrowedScopes, back.NarrowedScopes == nil)
			}
			if len(back.NarrowedScopes) != len(tc.in) {
				t.Fatalf("length %d in, %d out", len(tc.in), len(back.NarrowedScopes))
			}
		})
	}
}

// TestTheJournalRefusesWhatItCannotEnforce covers every boundary refusal in one
// table.
//
// 🔴 EACH ROW IS A CASE WHERE REPLAYING WOULD PRODUCE A MODEL THAT LOOKS COMPLETE
// AND IS NOT. The direction matters: a dropped revocation is a grant that keeps
// working, and a verb this build cannot map would be replayed as a NARROWER grant
// than a newer build intends. Both are silent, so both are refusals.
//
// 🔴 AND ONE ROW IS REFUSED AT THE BOUNDARY AND DROPPED AT REPLAY, WHICH IS TWO CLAIMS AND
// IS WHY THE TABLE CARRIES A `replay` COLUMN RATHER THAN ONE ANSWER. `Event.validate`
// refuses a raw token in `token_hash` from EVERY writer, `Append` included; `Replay` drops
// that record and loads the rest of the file, because a replay-time refusal costs the
// operator their whole control plane and the record it removes provably authenticates
// nobody (`replayDroppable`). A row that only asserted the refusal would have gone green
// against a build that silently persisted it.
func TestTheJournalRefusesWhatItCannotEnforce(t *testing.T) {
	// How the JOURNAL answers this line at replay. The boundary refusal is asserted for
	// every row either way.
	const (
		refuseWhole = false
		dropRecord  = true
	)
	for _, tc := range []struct {
		name    string
		line    string
		want    string
		dropped bool
	}{
		{
			"an event kind from a newer build",
			`{"kind":"scope-archived","at":"2000-01-01T00:00:00Z","scope_id":"scp_x"}`,
			"unknown event kind",
			refuseWhole,
		},
		{
			"a verb this build cannot enforce",
			`{"kind":"granted","at":"2000-01-01T00:00:00Z","grant_id":"grt_x","subject_kind":"user","subject_id":"usr_alice","object_kind":"scope","object_id":"scp_atlas_notes","verbs":["read","delete"],"narrowed_scopes":null}`,
			"unknown verb",
			refuseWhole,
		},
		{
			"a role this build cannot map",
			`{"kind":"member-set","at":"2000-01-01T00:00:00Z","project_id":"prj_atlas","user_id":"usr_alice","role":"observer","narrowed_scopes":null}`,
			"unknown role",
			refuseWhole,
		},
		{
			"a grant conferring no verbs",
			`{"kind":"granted","at":"2000-01-01T00:00:00Z","grant_id":"grt_x","subject_kind":"user","subject_id":"usr_alice","object_kind":"scope","object_id":"scp_atlas_notes","verbs":[],"narrowed_scopes":null}`,
			"verbs is empty",
			refuseWhole,
		},
		{
			"a raw token where its digest belongs",
			`{"kind":"credential-issued","at":"2000-01-01T00:00:00Z","credential_id":"crd_x","subject_kind":"user","subject_id":"usr_alice","token_hash":"cairn-test-raw-token-value","narrowed_scopes":null}`,
			"token_hash is",
			dropRecord,
		},
		{
			"an event with no timestamp",
			`{"kind":"grant-revoked","grant_id":"grt_x","narrowed_scopes":null}`,
			"at is required",
			refuseWhole,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			events, err := ReadEvents(strings.NewReader(tc.line))
			if err != nil {
				// The verb case is refused during DECODING, by
				// `VerbSet.UnmarshalJSON`, before an Event exists at all.
				if !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("read error = %v, want one naming %q", err, tc.want)
				}
				return
			}
			// The BOUNDARY claim, asserted for every row including the dropped one: this
			// is the check `Append` runs, and it is what stops such a record being
			// WRITTEN.
			if err := events[0].validate(); err == nil {
				t.Fatalf("the event validated — want a refusal naming %q, which is what keeps this "+
					"record out of a file no writer can take it back out of", tc.want)
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validate error = %v, want one naming %q", err, tc.want)
			}

			m, err := Replay(events)
			if !tc.dropped {
				if err == nil {
					t.Fatalf("replayed cleanly — want a refusal naming %q", tc.want)
				} else if !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("replay error = %v, want one naming %q", err, tc.want)
				}
				return
			}
			if err != nil {
				t.Fatalf("replay = %v, want the record DROPPED and the file loaded — a replay-time "+
					"refusal empties a pod's whole authority, and this record authenticates nobody", err)
			}
			if len(m.Credentials) != 0 {
				t.Fatalf("the model holds %d credential(s) — the dropped record must not enter the "+
					"authority, only the file must survive it", len(m.Credentials))
			}
			if len(m.Dropped) != 1 || !strings.Contains(m.Dropped[0].Reason, tc.want) {
				t.Fatalf("Dropped = %v, want one record naming %q — a drop nothing can report is a "+
					"silent narrowing", m.Dropped, tc.want)
			}
		})
	}
}

// TestAFailedReplayReturnsNoModelAtAll pins the fail-whole rule.
//
// 🔴 A PARTIALLY-REPLAYED MODEL ALONGSIDE AN ERROR IS THE SHAPE THAT GETS USED: a
// caller logs the error and serves the model, and the model is missing every event
// after the failure. If one of those was a revocation, the served authority is
// WIDER than the journal describes.
func TestAFailedReplayReturnsNoModelAtAll(t *testing.T) {
	events := append(worldEvents(),
		Event{Kind: EventGranted, At: at(30), GrantID: "grt_bad", Actor: uAlice,
			SubjectKind: KindUser, SubjectID: "usr_nobody",
			ObjectKind: ObjectScope, ObjectID: sAtlasNotes, Verbs: AllSet},
	)
	m, err := Replay(events)
	if err == nil {
		t.Fatal("a grant naming a non-existent subject replayed cleanly")
	}
	if m.Epoch != 0 || len(m.Users) != 0 || len(m.Grants) != 0 {
		t.Fatalf("a failed replay returned a populated model (epoch=%d users=%d grants=%d) — there is no partial answer here on purpose",
			m.Epoch, len(m.Users), len(m.Grants))
	}
}

// TestTheEpochCountsAppliedEvents pins what a materialized copy will compare on.
func TestTheEpochCountsAppliedEvents(t *testing.T) {
	events := worldEvents()
	m, err := Replay(events)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if m.Epoch != uint64(len(events)) {
		t.Errorf("epoch = %d after %d events, want %d", m.Epoch, len(events), len(events))
	}
	if !m.At.Equal(events[len(events)-1].At) {
		t.Errorf("model clock = %v, want the last event's %v", m.At, events[len(events)-1].At)
	}
	// Monotonic: every prefix has a strictly smaller epoch.
	for n := 1; n < len(events); n++ {
		prefix, err := Replay(events[:n])
		if err != nil {
			t.Fatalf("replay prefix %d: %v", n, err)
		}
		if prefix.Epoch >= m.Epoch {
			t.Fatalf("prefix of %d events has epoch %d, not below the full %d", n, prefix.Epoch, m.Epoch)
		}
	}
}

// TestEveryDeclaredEventKindHasAnApplyArm is a ledger over the switch.
//
// 🔴 A KIND ADDED TO `AllEventKinds` AND FORGOTTEN IN `apply` WOULD BE A NO-OP THAT
// STILL ADVANCES THE EPOCH — an authority that reports itself current while missing
// whatever that kind was for. Feeding each declared kind through `validate` is what
// proves every one of them is reachable; the `default` arm in `apply` is what turns
// a forgotten one into an error rather than a silent skip.
func TestEveryDeclaredEventKindHasAnApplyArm(t *testing.T) {
	for _, kind := range AllEventKinds {
		e := Event{Kind: kind, At: at(1)}
		err := e.validate()
		if err != nil && strings.Contains(err.Error(), "unknown event kind") {
			t.Errorf("%s is declared in AllEventKinds but validate() does not know it", kind)
		}
	}
	// And the negative control: a kind NOT in the set must be refused, or the
	// check above would pass for a validate() with no default arm at all.
	e := Event{Kind: EventKind("not-a-real-kind"), At: at(1)}
	if err := e.validate(); err == nil || !strings.Contains(err.Error(), "unknown event kind") {
		t.Fatalf("an undeclared kind validated with err=%v — the check above then proves nothing", err)
	}
}

// TestABlankLineIsSkippedAndAMalformedOneIsNot.
//
// Tolerating a malformed line would mean serving an authority with a hole in it,
// and a hole in an append-only log is indistinguishable from a revocation that
// never happened.
func TestABlankLineIsSkippedAndAMalformedOneIsNot(t *testing.T) {
	good := `{"kind":"user-created","at":"2000-01-01T00:00:00Z","user_id":"usr_a","provider":"example","subject":"a","narrowed_scopes":null}`

	events, err := ReadEvents(strings.NewReader("\n" + good + "\n\n   \n"))
	if err != nil {
		t.Fatalf("blank lines were not skipped: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("read %d events, want 1", len(events))
	}

	if _, err := ReadEvents(strings.NewReader(good + "\n{not json}\n")); err == nil {
		t.Fatal("a malformed line was tolerated — a hole in an append-only log is indistinguishable from a revocation that never happened")
	}
}

// ── The replay exemption: which records a REPLAY may drop, and which it may not ──────
//
// 🔴 EVERY TEST BELOW IS ABOUT ONE ASYMMETRY: a guard that runs at REPLAY has the whole
// FILE as its blast radius, where the same guard at APPEND has one write. `Model.apply` is
// reached from `Replay`, which fails a journal whole, and `FileStore.Reload` then serves
// `lastKnownGood()` — empty on a cold start. So a `credential-issued` record an older build
// ACCEPTED, met by a newer build's refusal, is not one refused credential: it is the
// operator's entire control-plane authority, remediable only by hand-editing an append-only
// file. Both cases below were measured doing exactly that at `0fb61d4`.
//
// ⚠ THE EXEMPTION IS `replayDroppable` AND NOTHING ELSE, WHICH IS WHAT THE LEDGER TEST AND
// THE TWO "STILL REFUSES WHOLE" TESTS ARE FOR. Narrowing an authority by one credential is
// safe in a way that dropping a revocation or a kind this build cannot read is not.

// aJournalWithOneUnusableDigest is the fixture world plus one good credential and one
// `token_hash` this build cannot use.
//
// ⚠ BOTH TOKENS ARE SYNTHETIC LITERALS IN A PUBLIC REPOSITORY and have never authorised
// anything. The "unusable" one is a fixed pattern of exactly `HashHexLen` characters that is
// not hex — the realistic shape, since a length-only check once accepted a 64-character raw
// secret.
func aJournalWithOneUnusableDigest(t *testing.T) []Event {
	t.Helper()
	unusable := strings.Repeat("cairn-test-not-a-digest-", 3)[:HashHexLen]
	if len(unusable) != HashHexLen {
		t.Fatalf("the fixture is %d characters and the case only exists at %d", len(unusable), HashHexLen)
	}
	if isHexDigest(unusable) {
		t.Fatal("the fixture is valid hex, so it is not the case this test is about")
	}
	return append(worldEvents(),
		Event{Kind: EventCredentialIssued, At: at(40), CredentialID: "crd_broken",
			SubjectKind: KindUser, SubjectID: uCarol, TokenHash: unusable,
			Label: "hand-appended, digest field holds the wrong thing"},
		Event{Kind: EventCredentialIssued, At: at(41), CredentialID: "crd_good",
			SubjectKind: KindUser, SubjectID: uCarol, TokenHash: HashToken(carolToken),
			Label: "carol laptop"},
	)
}

// TestAnUnusableDigestDropsOnlyItsOwnRecord is REGRESSION COVERAGE for a measured outage.
//
// 🔴 RED AT `0fb61d4`: this journal loaded **0** credentials and `Model()` returned
// "token_hash is not a 64-character hex digest". The record it refuses is one an earlier
// build's length-only check ACCEPTED, so the population holding it is real rather than
// hypothetical, and the operator's only remedy was editing an append-only authority by hand.
func TestAnUnusableDigestDropsOnlyItsOwnRecord(t *testing.T) {
	m, err := Replay(aJournalWithOneUnusableDigest(t))
	if err != nil {
		t.Fatalf("the journal did not load at all (%v). One record this build cannot use is not a "+
			"reason to empty an authority: through `FileStore.Reload` that is every credential in "+
			"the file, falling back to a `lastKnownGood()` that is empty on a cold start", err)
	}
	if _, held := m.Credentials["crd_broken"]; held {
		t.Fatal("the unusable record entered the model. It is dropped, not admitted: a `token_hash` " +
			"that is not hex may be a raw secret, and it can authenticate nobody either way")
	}
	// The load-bearing half: the OTHER credential still works. Without it, "the journal
	// loaded" is satisfied by a replay that produced an empty authority.
	p, auth, err := Authenticate(m, carolToken)
	if err != nil {
		t.Fatalf("the good credential beside the dropped one does not authenticate: %v", err)
	}
	if p.CredentialID != "crd_good" || p.ID != uCarol {
		t.Fatalf("authenticated as %+v, want crd_good for %s", p, uCarol)
	}
	if len(auth.ScopeIDs(VerbRead)) == 0 {
		t.Fatal("the surviving credential resolves to an EMPTY authority, which is indistinguishable " +
			"from a broken credential at every call site")
	}
	if len(m.Dropped) != 1 || m.Dropped[0].CredentialID != "crd_broken" ||
		!strings.Contains(m.Dropped[0].Reason, "hex digest") {
		t.Fatalf("Dropped = %v, want exactly crd_broken with the hex reason", m.Dropped)
	}
	if !strings.Contains(m.Dropped[0].String(), "crd_broken") {
		t.Fatalf("the rendered drop does not name the credential: %s", m.Dropped[0])
	}
}

// TestOneSecretRecordedTwiceDropsTheLaterRecord is REGRESSION COVERAGE for an outage the
// PREVIOUS round's own fix introduced.
//
// 🔴 RED AT `0fb61d4`: normalising the digest at `apply` made the duplicate refusal — a
// string compare — able to see one secret written in two case spellings, and a journal
// carrying that loaded **0** credentials. The refusal is right about the ambiguity and was
// wrong about the blast radius: what has no defined precedence is TWO principals for one
// digest, and dropping the later record removes exactly that.
//
// 🔴 AND THE SECOND ARM IS THE CASE THE JUSTIFICATION WAS WRITTEN WITHOUT. "The secret
// keeps working, as the credential it was FIRST recorded as" — `ErrDuplicateTokenDigest`'s
// own sentence — holds only while the first record is LIVE. Revoke it and the later record
// is still dropped, so the secret authenticates as NOTHING. That is the safe direction and
// it is announced at load, but it is not the no-op the sentence implied, and an operator
// re-issuing a rotated secret is exactly who walks into it.
func TestOneSecretRecordedTwiceDropsTheLaterRecord(t *testing.T) {
	digest := HashToken(carolToken)
	m, err := Replay(append(worldEvents(),
		Event{Kind: EventCredentialIssued, At: at(40), CredentialID: "crd_dup0",
			SubjectKind: KindUser, SubjectID: uCarol, TokenHash: digest, Label: "carol laptop"},
		Event{Kind: EventCredentialIssued, At: at(41), CredentialID: "crd_dup1",
			SubjectKind: KindUser, SubjectID: uDave, TokenHash: strings.ToUpper(digest),
			Label: "the same secret, shouted"},
		Event{Kind: EventCredentialIssued, At: at(42), CredentialID: "crd_other",
			SubjectKind: KindUser, SubjectID: uDave, TokenHash: HashToken(atlasToken),
			Label: "an unrelated credential"},
	))
	if err != nil {
		t.Fatalf("the journal did not load at all (%v) — one duplicated digest is not a reason to "+
			"empty an authority", err)
	}
	if _, held := m.Credentials["crd_dup1"]; held {
		t.Fatal("both records for one secret entered the model: `Resolve` would then pick a " +
			"principal by map iteration order, which is the ambiguity the refusal exists for")
	}
	p, _, err := Authenticate(m, carolToken)
	if err != nil {
		t.Fatalf("the secret no longer authenticates at all: %v", err)
	}
	if p.CredentialID != "crd_dup0" || p.ID != uCarol {
		t.Fatalf("authenticated as %+v, want the FIRST record crd_dup0 for %s", p, uCarol)
	}
	// The unrelated credential is the load-bearing half, exactly as in the test above.
	if _, _, err := Authenticate(m, atlasToken); err != nil {
		t.Fatalf("an unrelated credential in the same file no longer authenticates: %v", err)
	}
	if len(m.Dropped) != 1 || m.Dropped[0].CredentialID != "crd_dup1" ||
		!strings.Contains(m.Dropped[0].Reason, "same token digest") {
		t.Fatalf("Dropped = %v, want exactly crd_dup1 with the duplicate reason", m.Dropped)
	}

	// 🔴 THE REVOKED-FIRST ARM. Same three records with a revocation of the first between
	// them — the ordinary shape of a rotation done in the wrong order, or of an operator
	// re-issuing a secret they had already retired. The later record is dropped for the same
	// reason, and there is now no live record carrying the digest at all.
	revoked, err := Replay(append(worldEvents(),
		Event{Kind: EventCredentialIssued, At: at(40), CredentialID: "crd_dup0",
			SubjectKind: KindUser, SubjectID: uCarol, TokenHash: digest, Label: "carol laptop"},
		Event{Kind: EventCredentialRevoked, At: at(41), CredentialID: "crd_dup0"},
		Event{Kind: EventCredentialIssued, At: at(42), CredentialID: "crd_dup1",
			SubjectKind: KindUser, SubjectID: uDave, TokenHash: digest,
			Label: "the same secret again, after the first was retired"},
		Event{Kind: EventCredentialIssued, At: at(43), CredentialID: "crd_other",
			SubjectKind: KindUser, SubjectID: uDave, TokenHash: HashToken(atlasToken),
			Label: "an unrelated credential"},
	))
	if err != nil {
		t.Fatalf("the journal did not load at all (%v) — the duplicate is still a dropped RECORD "+
			"and not a reason to empty an authority, whether or not the first one is live", err)
	}
	if _, held := revoked.Credentials["crd_dup1"]; held {
		t.Fatal("the later record entered the model. A revoked first record does not make the " +
			"digest free: `apply`'s duplicate refusal compares every recorded credential, live or " +
			"not, because a revocation is a state on a row rather than its deletion")
	}
	if _, _, err := Authenticate(revoked, carolToken); err == nil {
		t.Fatal("the secret STILL authenticates after its only live record was dropped, which " +
			"would mean the drop had not happened")
	}
	// 🔴 AND THIS IS THE ASSERTION THE SENTINEL'S OLD SENTENCE WOULD HAVE FAILED: the claim
	// was that the secret keeps working as the FIRST credential. Here the first is revoked,
	// so it does not work at all — a narrowing, in the safe direction, and loud at load.
	if len(revoked.Dropped) != 1 || revoked.Dropped[0].CredentialID != "crd_dup1" ||
		!strings.Contains(revoked.Dropped[0].Reason, "same token digest") {
		t.Fatalf("Dropped = %v, want exactly crd_dup1 with the duplicate reason — a secret that "+
			"silently stops working is precisely what the drop record exists to announce",
			revoked.Dropped)
	}
	// The unrelated credential is unaffected here too, which is the whole-file claim.
	if _, _, err := Authenticate(revoked, atlasToken); err != nil {
		t.Fatalf("an unrelated credential in the same file no longer authenticates: %v", err)
	}
}

// TestOnlyCredentialIssuedMayBeDroppedAtReplay is the LEDGER that makes the exemption's
// narrowness structural rather than conventional.
//
// ⚠ AN INVARIANT GUARD, NOT REGRESSION COVERAGE. No build ever dropped another kind; what
// this pins is that none can be added quietly. `replayDroppable` is a table `Replay` looks
// a kind up in, so a kind with no entry cannot be dropped whatever error it raises — and
// this test is what fails when a row appears for one.
//
// 🔴 THE DIRECTION IS THE WHOLE ARGUMENT. `validate`'s `default:` arm refuses an unknown
// kind whole "because a dropped revocation is a grant that keeps working" — reasoning about
// WIDENING. Dropping a `credential-issued` record narrows. Any other kind here would be a
// second reading of that rule, applied one case too far.
func TestOnlyCredentialIssuedMayBeDroppedAtReplay(t *testing.T) {
	if len(replayDroppable) != 1 {
		t.Fatalf("replayDroppable carries %d kind(s): %v. Exactly one kind may be dropped at replay, "+
			"and adding another is a decision about the direction `validate`'s default arm refuses — "+
			"not a table entry", len(replayDroppable), replayDroppable)
	}
	for _, kind := range AllEventKinds {
		_, droppable := replayDroppable[kind]
		if droppable != (kind == EventCredentialIssued) {
			t.Fatalf("%s is droppable=%v at replay. Only credential-issued may be, because only its "+
				"two listed failures are provably NARROWING; every other kind — a revocation above "+
				"all — has to fail the journal whole", kind, droppable)
		}
	}
	// And the two sentinels are the ones the drop is justified for, named rather than
	// counted: a third error added to this list is a third claim about narrowing.
	sentinels := replayDroppable[EventCredentialIssued]
	if len(sentinels) != 2 ||
		!errors.Is(sentinels[0], ErrUnusableTokenDigest) ||
		!errors.Is(sentinels[1], ErrDuplicateTokenDigest) {
		t.Fatalf("credential-issued is droppable for %v, want exactly [ErrUnusableTokenDigest "+
			"ErrDuplicateTokenDigest] in that order — each has its own written argument for why "+
			"dropping the record narrows the authority rather than widening it", sentinels)
	}
}

// TestAKindWithNoDroppableEntryStillRefusesTheWholeJournal is the BEHAVIOURAL half of the
// ledger above: a structural assertion about a map type-checks past a `Replay` that ignores
// it.
//
// ⚠ REGRESSION COVERAGE FOR THE DIRECTION, NOT FOR A SHIPPED BUG. The cases are the two the
// exemption must never absorb: an unknown kind from a newer build, and a revocation naming a
// credential the model does not hold.
func TestAKindWithNoDroppableEntryStillRefusesTheWholeJournal(t *testing.T) {
	good := Event{Kind: EventCredentialIssued, At: at(40), CredentialID: "crd_good",
		SubjectKind: KindUser, SubjectID: uCarol, TokenHash: HashToken(carolToken)}

	for _, tc := range []struct {
		name  string
		event Event
		want  string
	}{
		{
			"an event kind from a newer build",
			Event{Kind: EventKind("credential-archived"), At: at(41), CredentialID: "crd_good"},
			"unknown event kind",
		},
		{
			"a revocation naming a credential that does not exist",
			Event{Kind: EventCredentialRevoked, At: at(41), CredentialID: "crd_nobody_issued"},
			"does not exist",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, err := Replay(append(worldEvents(), good, tc.event))
			if err == nil {
				t.Fatalf("the journal loaded with %d credential(s) — this record is NOT droppable, "+
					"and a replay that skipped it would serve an authority the file does not describe",
					len(m.Credentials))
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("refusal = %v, want one naming %q", err, tc.want)
			}
			if m.Epoch != 0 || len(m.Credentials) != 0 {
				t.Fatalf("a failed replay returned a populated model (epoch=%d credentials=%d)",
					m.Epoch, len(m.Credentials))
			}
		})
	}
}

// TestARevocationOfADroppedCredentialRefusesTheFileAndSaysWhy pins the one interaction the
// exemption CREATES, and pins that it is answered with a message rather than a second
// exemption.
//
// 🔴 THE CASE IS REACHABLE AND ITS DEFAULT MESSAGE POINTS AT THE WRONG LINE. An operator who
// hand-wrote a `credential-issued` with an unusable digest and then hand-wrote its
// revocation now gets the issue dropped — so the revocation names a credential the model
// does not hold, and the file is refused whole with "credential … does not exist" about a
// line they wrote correctly. `dropHint` is what tells them the other line is the one to
// remove.
//
// ⚠ AND THE REVOCATION IS STILL NOT DROPPED. Widening `replayDroppable` to cover it would
// be exactly the "never a revocation" rule being reinterpreted by whoever hit this case
// next; the answer is a better refusal, not a second exemption.
func TestARevocationOfADroppedCredentialRefusesTheFileAndSaysWhy(t *testing.T) {
	events := append(aJournalWithOneUnusableDigest(t),
		Event{Kind: EventCredentialRevoked, At: at(42), CredentialID: "crd_broken"})
	_, err := Replay(events)
	if err == nil {
		t.Fatal("a revocation naming a DROPPED credential was itself dropped. The exemption is " +
			"`credential-issued` and nothing else: a rule that reaches revocations is the one " +
			"`validate`'s default arm refuses by name")
	}
	if !strings.Contains(err.Error(), "crd_broken") ||
		!strings.Contains(err.Error(), "dropped at event") {
		t.Fatalf("refusal = %v, want one naming crd_broken and saying its ISSUE record was dropped "+
			"— without that, this reads as a missing credential and sends the operator to the line "+
			"they got right", err)
	}
}
