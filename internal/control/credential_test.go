package control

import (
	"errors"
	"testing"
)

// A synthetic credential. 43+ characters, matching `authz.MinTokenChars`, so the
// fixture exercises the length a real credential has.
const (
	carolToken = "cairn-test-carol-0000000000000000000000000000000"
	atlasToken = "cairn-test-atlas-0000000000000000000000000000000"
	deadToken  = "cairn-test-dead0-0000000000000000000000000000000"
)

func withCredentials(t *testing.T, extra ...Event) Model {
	t.Helper()
	events := append(worldEvents(),
		Event{Kind: EventCredentialIssued, At: at(40), CredentialID: "crd_carol",
			SubjectKind: KindUser, SubjectID: uCarol,
			TokenHash: HashToken(carolToken), Label: "carol laptop"},
		// A PROJECT principal — the service-account case. Its authority is the
		// project's own grants, which is `grt_2` and nothing else: a project is not
		// a member of itself, so it gets no role verbs over its own scopes.
		Event{Kind: EventCredentialIssued, At: at(41), CredentialID: "crd_atlas",
			SubjectKind: KindProject, SubjectID: pAtlas,
			TokenHash: HashToken(atlasToken), Label: "atlas ci"},
		Event{Kind: EventCredentialIssued, At: at(42), CredentialID: "crd_dead",
			SubjectKind: KindUser, SubjectID: uDave,
			TokenHash: HashToken(deadToken), Label: "rotated out"},
		Event{Kind: EventCredentialRevoked, At: at(43), CredentialID: "crd_dead"},
	)
	events = append(events, extra...)
	m, err := Replay(events)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	return m
}

func TestAuthenticateResolvesOneCredentialToOnePrincipalAndItsAuthority(t *testing.T) {
	m := withCredentials(t)

	p, a, err := Authenticate(m, carolToken)
	if err != nil {
		t.Fatalf("authenticating carol: %v", err)
	}
	if p.Kind != KindUser || p.ID != uCarol {
		t.Errorf("principal = %v, want the user carol", p)
	}
	if p.CredentialID != "crd_carol" {
		t.Errorf("principal carries credential %q, want crd_carol — the audit line needs the credential that actually matched, not a second lookup", p.CredentialID)
	}
	// The authority must be exactly the matrix row, i.e. the SAME answer `Resolve`
	// gives. Authenticating must not be a second place that decides visibility.
	for scopeID, want := range expectedMatrix[uCarol] {
		if got := verbsOrEmpty(a.VerbsOn(scopeID)); got != want {
			t.Errorf("authenticated carol has %q on %s, want %q (the matrix row)", got, scopeID, want)
		}
	}
}

func TestAProjectCredentialCarriesTheProjectsOwnGrantsAndNoMembersRoles(t *testing.T) {
	m := withCredentials(t)
	p, a, err := Authenticate(m, atlasToken)
	if err != nil {
		t.Fatalf("authenticating the atlas service account: %v", err)
	}
	if p.Kind != KindProject || p.ID != pAtlas {
		t.Fatalf("principal = %v, want the project atlas", p)
	}
	// grt_2 named `atlas` as subject, so the service account has it.
	if got := verbsOrEmpty(a.VerbsOn(sBeaconSecrets)); got != "read,write" {
		t.Errorf("atlas service account has %q on beacon-secrets, want read,write", got)
	}
	// 🔴 AND IT DOES *NOT* INHERIT ITS MEMBERS' ROLES OVER ITS OWN SCOPES. A
	// project is not a member of itself; a service account that silently held
	// owner-equivalent authority over everything the project owns would be a
	// privilege escalation dressed as a convenience, and it is the single most
	// plausible thing to get wrong here.
	if got := verbsOrEmpty(a.VerbsOn(sAtlasNotes)); got != "" {
		t.Errorf("atlas service account has %q on its own project's scope atlas-notes, want nothing — a project is not a member of itself, and inheriting its members' roles would be an escalation", got)
	}
}

func TestAuthenticationRefusesUniformly(t *testing.T) {
	m := withCredentials(t)
	for _, tc := range []struct {
		name  string
		token string
	}{
		{"an empty token", ""},
		{"a token nobody was ever issued", "cairn-test-nobody-000000000000000000000000000000"},
		{"a REVOKED credential", deadToken},
		{"a token that differs in one byte", carolToken[:len(carolToken)-1] + "1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, a, err := Authenticate(m, tc.token)
			if !errors.Is(err, ErrNoCredential{}) {
				t.Fatalf("err = %v, want ErrNoCredential", err)
			}
			// 🔴 THE SAME ERROR VALUE FOR ALL FOUR. A refusal that distinguished
			// "revoked" from "never existed" would tell an attacker they had found
			// a real credential, which is the enumeration the uniform-401 design
			// elsewhere in this server exists to deny.
			if err.Error() != "unauthorized" {
				t.Errorf("error text = %q, want the one constant", err.Error())
			}
			if len(a.ScopeIDs(VerbRead)) != 0 {
				t.Error("a refused authentication returned a non-empty authority")
			}
		})
	}
}

// TestNarrowingIntersectsAndCannotWiden is the credential contract's load-bearing
// half.
func TestNarrowingIntersectsAndCannotWiden(t *testing.T) {
	full := Resolve(withCredentials(t), user(uCarol))

	t.Run("nil narrows nothing", func(t *testing.T) {
		got := Narrow(full, nil)
		for scopeID, want := range expectedMatrix[uCarol] {
			if have := verbsOrEmpty(got.VerbsOn(scopeID)); have != want {
				t.Errorf("%s = %q, want %q", scopeID, have, want)
			}
		}
	})

	t.Run("an EMPTY non-nil list narrows to nothing", func(t *testing.T) {
		// 🔴 THE OPPOSITE OF nil, AND THE WHOLE REASON THE JOURNAL FORMAT
		// DISTINGUISHES THEM. If these two ever collapse, a credential meant to
		// see nothing sees everything its principal does.
		got := Narrow(full, []ID{})
		for scopeID := range expectedMatrix[uCarol] {
			if have := verbsOrEmpty(got.VerbsOn(scopeID)); have != "" {
				t.Errorf("%s = %q under an empty narrowing, want nothing", scopeID, have)
			}
		}
	})

	t.Run("a subset keeps exactly that subset, verbs intact", func(t *testing.T) {
		got := Narrow(full, []ID{sBeaconNotes})
		if have := verbsOrEmpty(got.VerbsOn(sBeaconNotes)); have != "read" {
			t.Errorf("beacon-notes = %q, want read", have)
		}
		if have := verbsOrEmpty(got.VerbsOn(sAtlasNotes)); have != "" {
			t.Errorf("atlas-notes = %q under a narrowing that excludes it, want nothing", have)
		}
	})

	t.Run("naming a scope the principal does NOT have confers nothing", func(t *testing.T) {
		// 🔴 THE ANTI-WIDENING CASE. Carol reaches all four fixture scopes by one
		// route or another, so the id used here names a scope no grant and no
		// membership can reach — which is exactly the input a narrowing must not
		// be able to turn into access.
		got := Narrow(full, []ID{sBeaconNotes, "scp_not_hers"})
		if got.Allows("scp_not_hers", VerbRead) {
			t.Fatal("a narrowing that names a scope the principal does not hold GRANTED it — the list must intersect, never widen")
		}
		if len(got.ScopeIDs(VerbRead)) != 1 {
			t.Errorf("the narrowed authority reaches %v, want beacon-notes alone", got.ScopeIDs(VerbRead))
		}
	})

	t.Run("a narrowing survives the principal LOSING the scope", func(t *testing.T) {
		// The reason narrowing is applied at resolve time rather than validated at
		// issue time: a credential narrowed to a scope its principal later loses
		// must stop seeing it, and only an intersection against TODAY's authority
		// keeps being true.
		m := withCredentials(t,
			Event{Kind: EventGrantRevoked, At: at(50), GrantID: "grt_1", Actor: uDave},
		)
		_, a, err := Authenticate(m, carolToken)
		if err != nil {
			t.Fatalf("authenticate: %v", err)
		}
		if a.Allows(sBeaconNotes, VerbRead) {
			t.Fatal("carol still reads beacon-notes after grt_1 was revoked")
		}
	})
}

func TestACredentialNarrowedAtIssueTimeIsAppliedAtAuthenticateTime(t *testing.T) {
	m := withCredentials(t,
		Event{Kind: EventCredentialIssued, At: at(44), CredentialID: "crd_narrow",
			SubjectKind: KindUser, SubjectID: uCarol,
			TokenHash: HashToken("cairn-test-narrow-000000000000000000000000000000"),
			Label:     "read-only laptop", NarrowedScopes: []ID{sBeaconNotes}},
	)
	_, a, err := Authenticate(m, "cairn-test-narrow-000000000000000000000000000000")
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if got := a.ScopeIDs(VerbRead); len(got) != 1 || got[0] != sBeaconNotes {
		t.Fatalf("the narrowed credential reaches %v, want [beacon-notes] — narrowing must be applied by Authenticate, not merely stored", got)
	}
}

func TestTwoCredentialsCannotShareOneDigest(t *testing.T) {
	_, err := Replay(append(worldEvents(),
		Event{Kind: EventCredentialIssued, At: at(40), CredentialID: "crd_a",
			SubjectKind: KindUser, SubjectID: uCarol, TokenHash: HashToken(carolToken)},
		Event{Kind: EventCredentialIssued, At: at(41), CredentialID: "crd_b",
			SubjectKind: KindUser, SubjectID: uDave, TokenHash: HashToken(carolToken)},
	))
	if err == nil {
		t.Fatal("two credentials sharing one digest replayed cleanly — at authentication time there is no defined precedence between them, so the ambiguity has to be refused here")
	}
}

func TestHashTokenNeverReturnsTheToken(t *testing.T) {
	// A guard on the one function the whole credential design rests on: it is the
	// only place a token is read, and the digest is the only thing that leaves.
	h := HashToken(carolToken)
	if h == carolToken {
		t.Fatal("HashToken returned its input")
	}
	if len(h) != HashHexLen {
		t.Fatalf("digest is %d characters, want %d", len(h), HashHexLen)
	}
	if HashToken(carolToken) != h {
		t.Fatal("HashToken is not deterministic")
	}
	if HashToken(carolToken+"x") == h {
		t.Fatal("HashToken collided on a one-character change")
	}
	if !EqualHash(h, h) || EqualHash(h, HashToken("other")) {
		t.Fatal("EqualHash disagrees with equality")
	}
}
