package identity

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

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
// 🔴 ORDER IS A SECURITY DECISION AND IT IS PINNED AS ONE. The machine token is the only
// credential this pod MINTED and the only one whose revocation is one edit away, so it
// must win where a request carries both. The trusted header is last because its source
// check is a property of the deployment rather than of the request.
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
	cfg := goodProxyConfig(t)
	trusted, err := NewTrustedHeader(cfg)
	if err != nil {
		t.Fatal(err)
	}

	chain, err := Backends(machine, supabase, trusted)
	if err != nil {
		t.Fatal(err)
	}
	if len(chain) != 3 {
		t.Fatalf("expected three backends, got %d", len(chain))
	}
	if _, ok := chain[0].(*MachineToken); !ok {
		t.Fatalf("the machine token must be FIRST, got %T", chain[0])
	}
	if _, ok := chain[1].(*SupabaseJWT); !ok {
		t.Fatalf("the Supabase backend must be SECOND, got %T", chain[1])
	}
	if _, ok := chain[2].(*TrustedHeader); !ok {
		t.Fatalf("the trusted header must be LAST, got %T", chain[2])
	}

	// A chain with nothing in it is a configuration refusal, not a silent pass-through.
	if _, err := Backends(nil, nil, nil); !errors.Is(err, ErrNoBackends) {
		t.Fatalf("expected %v, got %v", ErrNoBackends, err)
	}
	// And the default shape — machine token alone — is legal.
	if got, err := Backends(machine, nil, nil); err != nil || len(got) != 1 {
		t.Fatalf("machine-token-only must be a legal chain, got %v / %d", err, len(got))
	}
}

func request(t *testing.T) *http.Request {
	t.Helper()
	return httptest.NewRequest(http.MethodGet, "/api/v1/recall/quarry-notes", nil)
}
