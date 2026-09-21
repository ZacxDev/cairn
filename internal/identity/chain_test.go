package identity

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
)

// scriptedBackend answers whatever a test tells it to, and COUNTS how often it was
// asked. The count is what makes the chain's short-circuit a measurement rather than an
// assertion about the code.
type scriptedBackend struct {
	name  string
	give  Identity
	err   error
	calls int
}

func (s *scriptedBackend) Authenticate(*http.Request) (Identity, error) {
	s.calls++
	return s.give, s.err
}

func namedIdentity(name string) Identity {
	return Identity{Principal: control.Principal{
		Kind: control.KindUser, ID: control.ID("usr_" + name), Display: name,
	}}
}

// TestTheChainTakesTheFirstBackendThatSucceedsAndSTOPS.
//
// 🔴 THE COUNTS ARE THE ASSERTION. "The right principal came back" is satisfied by a
// chain that runs every backend and happens to prefer the first answer; the claim being
// made is that the later backends are NOT ASKED, because running a full JWT verification
// per configured backend on every anonymous request is a cheap amplifier for anyone who
// can reach the pod.
func TestTheChainTakesTheFirstBackendThatSucceedsAndStops(t *testing.T) {
	first := &scriptedBackend{name: "first", err: control.ErrNoCredential{}}
	second := &scriptedBackend{name: "second", give: namedIdentity("second")}
	third := &scriptedBackend{name: "third", give: namedIdentity("third")}

	who, err := Chain{first, second, third}.Authenticate(request(t))
	if err != nil {
		t.Fatalf("the chain refused a request a backend accepted: %v", err)
	}
	if who.Principal.Display != "second" {
		t.Fatalf("the chain returned %q, want the FIRST backend that succeeded", who.Principal.Display)
	}
	if first.calls != 1 {
		t.Fatalf("the first backend was asked %d times, want 1 — a refusal must not stop the chain", first.calls)
	}
	if second.calls != 1 {
		t.Fatalf("the accepting backend was asked %d times, want 1", second.calls)
	}
	if third.calls != 0 {
		t.Fatalf("the backend AFTER the successful one was asked %d times — the chain does not short-circuit, so one anonymous request costs every configured backend's work", third.calls)
	}
}

// TestAnEmptyChainAuthenticatesNOBODY.
//
// 🔴 THE FAIL-CLOSED DIRECTION, AND IT IS THE ONE A MISCONFIGURATION LANDS ON. "Nothing
// to check, so nothing objected" is the reading that returns a nil error, and it would
// authenticate every request as a principal naming nobody.
func TestAnEmptyChainAuthenticatesNobody(t *testing.T) {
	_, err := Chain{}.Authenticate(request(t))
	if err == nil {
		t.Fatal("an empty chain authenticated a request")
	}
	if !errors.Is(err, control.ErrNoCredential{}) {
		t.Fatalf("the refusal must be the uniform one, got %v", err)
	}
	// And a chain of nothing but nils, which is the shape a wiring mistake produces.
	if _, err := (Chain{nil, nil}).Authenticate(request(t)); err == nil {
		t.Fatal("a chain of nil backends authenticated a request")
	}
}

// TestABackendThatNamesNOBODYIsRefusedRatherThanPropagated.
//
// 🔴 THE ONE FAIL-OPEN SHAPE THE INTERFACE MAKES POSSIBLE. A backend returning
// `Identity{}, nil` has said "yes" without naming anybody: the zero
// `control.Authorization` permits nothing, so no scope leaks — but the request would be
// audited as `auth=ok identity=-` and would satisfy every guard that asks whether
// authentication succeeded. The chain refuses it for the WHOLE chain rather than
// skipping to the next backend, because a broken authenticator is not a credential
// outcome.
func TestABackendThatNamesNobodyIsRefusedRatherThanPropagated(t *testing.T) {
	for _, arm := range []struct {
		name string
		give Identity
	}{
		{"a wholly zero identity", Identity{}},
		{"a principal with no id", Identity{Principal: control.Principal{Kind: control.KindUser, Display: "x"}}},
		{"a principal with no display", Identity{Principal: control.Principal{Kind: control.KindUser, ID: "usr_x"}}},
		{"a principal with no kind", Identity{Principal: control.Principal{ID: "usr_x", Display: "x"}}},
		{"a principal with a kind this model does not define", Identity{Principal: control.Principal{Kind: "robot", ID: "usr_x", Display: "x"}}},
	} {
		t.Run(arm.name, func(t *testing.T) {
			if arm.give.Valid() {
				t.Fatal("this arm's fixture is VALID, so it measures nothing")
			}
			broken := &scriptedBackend{name: "broken", give: arm.give}
			next := &scriptedBackend{name: "next", give: namedIdentity("next")}
			if _, err := (Chain{broken, next}).Authenticate(request(t)); err == nil {
				t.Fatal("a backend that answered yes without naming anybody was propagated")
			}
			if next.calls != 0 {
				t.Fatal("the chain fell through to the next backend; a broken authenticator must stop the chain rather than be skipped")
			}
		})
	}
	// The positive control: a COMPLETE identity is propagated, so the table above is not
	// measuring "the chain refuses everything".
	good := &scriptedBackend{give: namedIdentity("real")}
	if _, err := (Chain{good}).Authenticate(request(t)); err != nil {
		t.Fatalf("a complete identity must be propagated: %v", err)
	}
}

// TestBackendsOrdersTheChainMachineTokenFirstAndTrustedHeaderLAST.
//
// 🔴 ORDER IS A SECURITY DECISION AND IT IS PINNED AS ONE, POSITION BY POSITION WITH A
// MESSAGE PER POSITION. The machine token is the only credential this pod MINTED as a
// bearer token and the only one whose revocation is one edit away, so it must win where a
// request carries both. The cookie session is third because it is the only AMBIENT
// credential — the browser attaches it without anybody deciding to — and an explicitly
// presented credential must beat one that arrived by itself. The trusted header is last
// because its source check is a property of the deployment rather than of the request.
//
// ⚠ A SINGLE `reflect.DeepEqual` OVER THE TYPE LIST WOULD FAIL FOR ANY PERMUTATION AND
// SAY ONLY "NOT EQUAL". Four assertions with four sentences is what tells the next reader
// which rule the chain broke, which is the difference between a guard and an alarm.
func TestBackendsOrdersTheChainMachineTokenFirstAndTrustedHeaderLast(t *testing.T) {
	authority := newTestAuthority(t)
	machine, err := NewMachineToken(authority)
	if err != nil {
		t.Fatal(err)
	}
	supabase, err := NewSupabaseJWT(goodSupabaseConfig(t, keySetOver(t, newRSASigner(t, "rsa-1", 2048))))
	if err != nil {
		t.Fatal(err)
	}
	cookie, err := NewCookieSession(newMemorySessions(), authority)
	if err != nil {
		t.Fatal(err)
	}
	cfg := goodProxyConfig(t)
	trusted, err := NewTrustedHeader(cfg)
	if err != nil {
		t.Fatal(err)
	}

	chain, err := Backends(machine, supabase, cookie, trusted)
	if err != nil {
		t.Fatal(err)
	}
	if len(chain) != 4 {
		t.Fatalf("expected four backends, got %d", len(chain))
	}
	if _, ok := chain[0].(*MachineToken); !ok {
		t.Fatalf("the machine token must be FIRST, got %T", chain[0])
	}
	if _, ok := chain[1].(*SupabaseJWT); !ok {
		t.Fatalf("the Supabase backend must be SECOND, got %T", chain[1])
	}
	if _, ok := chain[2].(*CookieSession); !ok {
		t.Fatalf("the cookie session must be THIRD, got %T. It is the only credential the browser sends "+
			"WITHOUT the caller deciding to, so every header-borne credential is tried ahead of it: a request "+
			"carrying both must resolve as the one somebody chose to present, not as whatever cookie the browser "+
			"still had.", chain[2])
	}
	if _, ok := chain[3].(*TrustedHeader); !ok {
		t.Fatalf("the trusted header must be LAST, got %T", chain[3])
	}

	// A chain with nothing in it is a configuration refusal, not a silent pass-through.
	if _, err := Backends(nil, nil, nil, nil); !errors.Is(err, ErrNoBackends) {
		t.Fatalf("expected %v, got %v", ErrNoBackends, err)
	}
	// And the default shape — machine token alone — is legal.
	if got, err := Backends(machine, nil, nil, nil); err != nil || len(got) != 1 {
		t.Fatalf("machine-token-only must be a legal chain, got %v / %d", err, len(got))
	}
}

// TestAChosenCredentialBeatsTheAmbientOne is the ORDERING's behavioural half, and it is
// the reason the type-position assertions above are not the whole guard.
//
// 🔴 IT DRIVES ONE REQUEST CARRYING BOTH CREDENTIALS AND READS WHICH PRINCIPAL CAME BACK.
// The positional test pins the chain's SHAPE; this pins what that shape DOES, and they
// fail for different reasons — a `Chain.Authenticate` that ran the list backwards would
// pass the first and fail this one.
func TestAChosenCredentialBeatsTheAmbientOne(t *testing.T) {
	authority := newCredentialedAuthority(t)
	machine, err := NewMachineToken(authority)
	if err != nil {
		t.Fatal(err)
	}

	// The cookie backend resolves to a DIFFERENT principal from the machine token, or
	// "the header won" and "the cookie won" are the same observable.
	sessions := newMemorySessions()
	id := "ambient-session-id-fixture"
	sessions.put(Session{
		Digest:    SessionDigest(id),
		Kind:      control.KindProject,
		Principal: testProjectID,
		ExpiresAt: time.Now().Add(time.Hour),
	})
	cookie, err := NewCookieSession(sessions, authority)
	if err != nil {
		t.Fatal(err)
	}

	chain, err := Backends(machine, nil, cookie, nil)
	if err != nil {
		t.Fatal(err)
	}

	// POSITIVE CONTROL, both halves: each backend must answer on its own, or "the other
	// one won" below is indistinguishable from "this one never works".
	cookieOnly := request(t)
	cookieOnly.AddCookie(&http.Cookie{Name: SessionCookieName, Value: id})
	viaCookie, err := chain.Authenticate(cookieOnly)
	if err != nil {
		t.Fatalf("POSITIVE CONTROL FAILED: the cookie alone did not authenticate (%v), so the combined request "+
			"below cannot show that the header beat it", err)
	}
	headerOnly := request(t)
	headerOnly.Header.Set("Authorization", "Bearer "+testToken)
	viaHeader, err := chain.Authenticate(headerOnly)
	if err != nil {
		t.Fatalf("POSITIVE CONTROL FAILED: the bearer token alone did not authenticate (%v)", err)
	}
	if viaCookie.Principal.ID == viaHeader.Principal.ID {
		t.Fatalf("PRECONDITION FAILED: both credentials resolve to principal %q, so the assertion below cannot "+
			"tell which backend answered", viaHeader.Principal.ID)
	}

	// THE PIN: one request, both credentials.
	both := request(t)
	both.Header.Set("Authorization", "Bearer "+testToken)
	both.AddCookie(&http.Cookie{Name: SessionCookieName, Value: id})
	got, err := chain.Authenticate(both)
	if err != nil {
		t.Fatalf("a request carrying both credentials was refused: %v", err)
	}
	if got.Principal.ID != viaHeader.Principal.ID {
		t.Errorf("a request carrying BOTH a bearer token and a session cookie resolved to %q, the COOKIE's "+
			"principal, rather than to %q, the header's. The cookie is attached by the browser without the caller "+
			"deciding to; the header is the credential somebody chose to present. Resolving as the ambient one "+
			"means a stale cookie silently shadows a deliberately presented credential, and the person reading the "+
			"page is reading it as a principal they did not ask to be.",
			got.Principal.ID, viaHeader.Principal.ID)
	}
	t.Logf("chain resolution: cookie-only=%s header-only=%s both=%s",
		viaCookie.Principal.ID, viaHeader.Principal.ID, got.Principal.ID)
}

func request(t *testing.T) *http.Request {
	t.Helper()
	return httptest.NewRequest(http.MethodGet, "/api/v1/recall/quarry-notes", nil)
}
