package control

import (
	"bytes"
	"encoding/json"
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
func TestTheJournalRefusesWhatItCannotEnforce(t *testing.T) {
	for _, tc := range []struct {
		name string
		line string
		want string
	}{
		{
			"an event kind from a newer build",
			`{"kind":"scope-archived","at":"2000-01-01T00:00:00Z","scope_id":"scp_x"}`,
			"unknown event kind",
		},
		{
			"a verb this build cannot enforce",
			`{"kind":"granted","at":"2000-01-01T00:00:00Z","grant_id":"grt_x","subject_kind":"user","subject_id":"usr_alice","object_kind":"scope","object_id":"scp_atlas_notes","verbs":["read","delete"],"narrowed_scopes":null}`,
			"unknown verb",
		},
		{
			"a role this build cannot map",
			`{"kind":"member-set","at":"2000-01-01T00:00:00Z","project_id":"prj_atlas","user_id":"usr_alice","role":"observer","narrowed_scopes":null}`,
			"unknown role",
		},
		{
			"a grant conferring no verbs",
			`{"kind":"granted","at":"2000-01-01T00:00:00Z","grant_id":"grt_x","subject_kind":"user","subject_id":"usr_alice","object_kind":"scope","object_id":"scp_atlas_notes","verbs":[],"narrowed_scopes":null}`,
			"verbs is empty",
		},
		{
			"a raw token where its digest belongs",
			`{"kind":"credential-issued","at":"2000-01-01T00:00:00Z","credential_id":"crd_x","subject_kind":"user","subject_id":"usr_alice","token_hash":"cairn-test-raw-token-value","narrowed_scopes":null}`,
			"token_hash is",
		},
		{
			"an event with no timestamp",
			`{"kind":"grant-revoked","grant_id":"grt_x","narrowed_scopes":null}`,
			"at is required",
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
			if _, err := Replay(events); err == nil {
				t.Fatalf("replayed cleanly — want a refusal naming %q", tc.want)
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("replay error = %v, want one naming %q", err, tc.want)
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
