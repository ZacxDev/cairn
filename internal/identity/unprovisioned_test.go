package identity

import (
	"errors"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/control"
)

// TestTheUnprovisionedRefusalPrintsExactlyWhatItAlwaysDid is the assertion the whole type
// rests on.
//
// 🔴 IF THIS SENTENCE GAINS THE SUBJECT, THE CHANGE IS A DISCLOSURE. The string is what the
// OAuth callback writes to the operator's log on every unknown-subject probe, and the
// refusal it replaced deliberately named nobody. So the test does not check "contains" — it
// compares against a plain [refuse] built from the SAME backend and reason, which is the only
// form of this assertion that a later edit to either spelling cannot walk past.
func TestTheUnprovisionedRefusalPrintsExactlyWhatItAlwaysDid(t *testing.T) {
	const backend, reason = SupabaseBackend, "the token verifies and names a subject this control plane holds no user for"
	plain := refuse(backend, reason)
	rich := refuseUnprovisioned(backend, reason, "supabase", "sub-nobody-knows")

	if plain.Error() != rich.Error() {
		t.Fatalf(`the two refusals print differently, so this type changed what a log says.

  plain: %q
  rich : %q`, plain.Error(), rich.Error())
	}
	// And the subject must not be in it, asserted directly as well as by the equality
	// above — the equality would also pass if BOTH gained the subject.
	if strings.Contains(rich.Error(), "sub-nobody-knows") {
		t.Fatalf("the refusal echoes the subject: %q", rich.Error())
	}
	// The String() form is where a subject IS allowed, so a test can print it. If this
	// stopped carrying it, the type's own doc would be wrong.
	var u *UnprovisionedSubject
	if !errors.As(rich, &u) {
		t.Fatal("errors.As did not match the type the constructor returns")
	}
	if !strings.Contains(u.String(), "sub-nobody-knows") {
		t.Errorf("String() should name the subject for a test's benefit, got %q", u.String())
	}
}

// TestTheUnprovisionedRefusalKeepsTheWholeExistingErrorChain is what makes this change
// invisible to every caller that has not opted in.
//
// 🔴 THREE RELATIONSHIPS, AND EACH ONE IS A SEPARATE WAY A SERVING PATH ASKS ITS QUESTION.
// A type that satisfied only `errors.As(&*UnprovisionedSubject)` would silently change the
// answer for `internal/api`'s uniform 401 and for every `*Refusal` inspection in the tree.
func TestTheUnprovisionedRefusalKeepsTheWholeExistingErrorChain(t *testing.T) {
	const backend, reason = SupabaseBackend, "a reason"
	rich := refuseUnprovisioned(backend, reason, "supabase", "sub-1")

	// (1) the uniform question every serving path asks
	if !errors.Is(rich, control.ErrNoCredential{}) {
		t.Error("errors.Is(err, control.ErrNoCredential{}) is false — the uniform 401 path would stop recognising this")
	}
	// (2) the Refusal inspection, with its fields intact
	var ref *Refusal
	if !errors.As(rich, &ref) {
		t.Fatal("errors.As(err, &*Refusal) is false — every existing Refusal reader would stop matching")
	}
	if ref.Backend != backend || ref.Reason != reason {
		t.Errorf("the wrapped Refusal lost its fields: %+v", ref)
	}
	// (3) the new, opted-in question
	var u *UnprovisionedSubject
	if !errors.As(rich, &u) {
		t.Fatal("errors.As(err, &*UnprovisionedSubject) is false")
	}
	if u.Provider != "supabase" || u.Subject != "sub-1" {
		t.Errorf("carried the wrong identity: %s", u)
	}

	// 🔴 THE NEGATIVE CONTROL, WITHOUT WHICH (3) PROVES NOTHING: a PLAIN refusal must NOT
	// match the new type. If it did, every refusal in the tree would look provisionable and
	// the invite path would provision on a bad signature.
	var other *UnprovisionedSubject
	if errors.As(refuse(backend, reason), &other) {
		t.Fatal("a PLAIN refusal matched *UnprovisionedSubject — every refusal would read as provisionable")
	}
}

// TestARefusalCarryingHalfAnIdentityIsNotProvisionable.
//
// 🔴 THE FAILURE THIS CLOSES IS A WRITE, NOT A READ. A caller doing `errors.As` treats a
// match as "I may provision this subject", so a match carrying an empty provider or subject
// would send an `EventUserCreated` with an empty key. `Event.validate` would refuse that,
// but the honest place to stop is where the reason is known.
func TestARefusalCarryingHalfAnIdentityIsNotProvisionable(t *testing.T) {
	for _, tc := range []struct{ name, provider, subject string }{
		{"no provider", "", "sub-1"},
		{"no subject", "supabase", ""},
		{"neither", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := refuseUnprovisioned(SupabaseBackend, "a reason", tc.provider, tc.subject)
			var u *UnprovisionedSubject
			if errors.As(err, &u) {
				t.Fatalf("matched as provisionable while carrying %s", u)
			}
			// It is still a refusal, and still the uniform one.
			if !errors.Is(err, control.ErrNoCredential{}) {
				t.Error("the fallback is not a credential refusal")
			}
		})
	}
}

// TestOnlyTheVerifiedUnknownSubjectArmIsProvisionable drives the REAL backend rather than
// the constructor, because what matters is which arm reaches it.
//
// 🔴 A CONSTRUCTOR TEST CANNOT SEE THIS. `refuseUnprovisioned` returning the right type says
// nothing about whether a BAD SIGNATURE also reaches it — and a bad signature that came back
// provisionable would let anybody who can reach the callback create a principal. So the
// discriminating pair is driven through `Authenticate`: a verified-but-unknown subject IS
// provisionable, and a forged token is NOT.
func TestOnlyTheVerifiedUnknownSubjectArmIsProvisionable(t *testing.T) {
	s := newRSASigner(t, "rsa-1", 2048)
	cfg := goodSupabaseConfig(t, keySetOver(t, s))
	cfg.Authority = newTestAuthority(t)
	backend, err := NewSupabaseJWT(cfg)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("a VERIFIED token naming an unknown subject is provisionable", func(t *testing.T) {
		claims := defaultClaims()
		claims["sub"] = testStranger
		// PRECONDITION, the same one the sibling refusal test makes: the token verifies, so
		// the arm under test is the user lookup and not the signature.
		if _, err := Verify(s.sign(t, claims, nil), backend.verify); err != nil {
			t.Fatalf("precondition: the stranger's token must verify, got %v", err)
		}
		_, err := backend.Authenticate(bearer(t, s.sign(t, claims, nil)))
		var u *UnprovisionedSubject
		if !errors.As(err, &u) {
			t.Fatalf("the verified-unknown arm is not provisionable: %v", err)
		}
		if u.Subject != testStranger {
			t.Errorf("carried subject %q, want %q", u.Subject, testStranger)
		}
		// 🔴 COMPARED AGAINST THE BACKEND'S OWN RESOLVED PROVIDER, NOT `cfg.Provider`. The
		// config leaves it EMPTY and the constructor defaults it, so an assertion against
		// the config field compares the carried value against `""` and fails — measured,
		// on the first run of this test. What must match is the provider the LOOKUP used,
		// because that plus the subject is the key an `EventUserCreated` would carry: a
		// provisioning write under a different provider namespace would create a user the
		// next sign-in could never resolve.
		if u.Provider != backend.provider {
			t.Errorf("carried provider %q, want the backend's own %q", u.Provider, backend.provider)
		}
		if backend.provider == "" {
			t.Fatal("internal: the backend resolved an empty provider, so the assertion above is vacuous")
		}
	})

	t.Run("a FORGED token is not provisionable", func(t *testing.T) {
		// A different key, so the signature does not verify against the configured set.
		forger := newRSASigner(t, "rsa-1", 2048)
		claims := defaultClaims()
		claims["sub"] = testStranger
		_, err := backend.Authenticate(bearer(t, forger.sign(t, claims, nil)))
		if err == nil {
			t.Fatal("a forged token authenticated")
		}
		var u *UnprovisionedSubject
		if errors.As(err, &u) {
			t.Fatalf("A FORGED TOKEN CAME BACK PROVISIONABLE (%s). Anybody who can reach the "+
				"callback could then create a principal.", u)
		}
	})

	t.Run("a role-refused token is not provisionable", func(t *testing.T) {
		// The role gate sits ABOVE the user lookup, so this also pins the ORDER: a token
		// refused for its role must not reach the provisionable arm even when its subject
		// is genuinely unknown.
		cfg2 := goodSupabaseConfig(t, keySetOver(t, s))
		cfg2.Authority = newTestAuthority(t)
		cfg2.RequireRole = "authenticated"
		b2, err := NewSupabaseJWT(cfg2)
		if err != nil {
			t.Fatal(err)
		}
		claims := defaultClaims()
		claims["sub"] = testStranger
		claims["role"] = "anon"
		_, aerr := b2.Authenticate(bearer(t, s.sign(t, claims, nil)))
		if aerr == nil {
			t.Fatal("an `anon` token passed a backend requiring `authenticated`")
		}
		var u *UnprovisionedSubject
		if errors.As(aerr, &u) {
			t.Fatalf("a role-refused token came back provisionable (%s) — the role gate no "+
				"longer precedes the user lookup", u)
		}
	})
}
