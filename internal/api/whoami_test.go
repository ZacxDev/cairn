package api

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/netid"
)

// THE WHOAMI SELF-VIEW, at the server seam: a real server, a real `identity.SupabaseJWT` in
// group-federation mode over a loopback JWKS, and a real control journal on disk holding
// group mappings — so every body below is what an issuing service would read.
//
// 🔴 EVERY EXPECTED BODY IS A LITERAL, never derived from the implementation, and this file
// names no symbol the route added (only string literals), so it COMPILES at the pre-route
// commit and goes RED there behaviourally: `/api/v1/whoami` answers the no-route 404.
//
// The world: project `fed` holds alpha-notes and beta-notes; project `other` holds
// gamma-notes. The map: `alpha-readers` → alpha-notes (read); `alpha-writers` → alpha-notes
// (read, write — RECORDED, and capped at read on resolve); `team-all` → the PROJECT `fed`
// (read, so both its scopes); `gamma-team` → gamma-notes (read).

type whoamiHarness struct {
	*matrixHarness
	signer  *fedAuditSigner
	journal string
}

func newWhoamiHarness(t *testing.T) *whoamiHarness {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	h := &whoamiHarness{matrixHarness: newMatrixHarness(t), signer: &fedAuditSigner{key: key}}
	pad := func(n *big.Int) string { out := make([]byte, 32); n.FillBytes(out); return fedAuditB64(out) }
	jwks, _ := json.Marshal(map[string]any{"keys": []any{map[string]any{
		"kty": "EC", "crv": "P-256", "kid": fedAuditKid, "alg": "ES256", "use": "sig",
		"x": pad(key.X), "y": pad(key.Y)}}})
	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(jwks)
	}))
	t.Cleanup(idp.Close)
	keys, err := identity.NewKeySet(identity.JWKSOptions{URL: idp.URL + "/jwks"})
	if err != nil {
		t.Fatal(err)
	}
	if err := keys.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	h.journal = filepath.Join(t.TempDir(), "control.journal")
	journal, err := control.OpenFileStore(h.journal)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	usr := control.DerivedID(control.PrefixUser, "owner")
	fed := control.DerivedID(control.PrefixProject, "fed")
	other := control.DerivedID(control.PrefixProject, "other")
	alpha := control.DerivedID(control.PrefixScope, "alpha-notes")
	beta := control.DerivedID(control.PrefixScope, "beta-notes")
	gamma := control.DerivedID(control.PrefixScope, "gamma-notes")
	if _, err := journal.Append(ctx,
		control.Event{Kind: control.EventUserCreated, At: fedAuditClock, UserID: usr, Provider: "other-ns", Subject: "owner"},
		control.Event{Kind: control.EventProjectCreated, At: fedAuditClock, ProjectID: fed, Name: "fed", UserID: usr},
		control.Event{Kind: control.EventProjectCreated, At: fedAuditClock, ProjectID: other, Name: "other", UserID: usr},
		control.Event{Kind: control.EventScopeCreated, At: fedAuditClock, ScopeID: alpha, DisplayName: "alpha-notes", ProjectID: fed},
		control.Event{Kind: control.EventScopeCreated, At: fedAuditClock, ScopeID: beta, DisplayName: "beta-notes", ProjectID: fed},
		control.Event{Kind: control.EventScopeCreated, At: fedAuditClock, ScopeID: gamma, DisplayName: "gamma-notes", ProjectID: other},
	); err != nil {
		t.Fatal(err)
	}
	for _, m := range []control.GroupMapRequest{
		{Group: "alpha-readers", ObjectKind: control.ObjectScope, ObjectID: alpha, Verbs: control.NewVerbSet(control.VerbRead)},
		{Group: "alpha-writers", ObjectKind: control.ObjectScope, ObjectID: alpha, Verbs: control.NewVerbSet(control.VerbRead, control.VerbWrite)},
		{Group: "team-all", ObjectKind: control.ObjectProject, ObjectID: fed, Verbs: control.NewVerbSet(control.VerbRead)},
		{Group: "gamma-team", ObjectKind: control.ObjectScope, ObjectID: gamma, Verbs: control.NewVerbSet(control.VerbRead)},
	} {
		m.Provider, m.At = fedAuditProvider, fedAuditClock
		if _, err := control.MapGroup(ctx, journal, m); err != nil {
			t.Fatal(err)
		}
	}
	sessions := control.NewCache(journal, control.CacheOptions{Now: func() time.Time { return fedAuditClock }})
	if err := sessions.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	jwt, err := identity.NewSupabaseJWT(identity.SupabaseConfig{
		Authority: sessions, Keys: keys, Issuer: fedAuditIssuer, Audience: fedAuditAudience,
		Provider: fedAuditProvider, GroupsClaim: "groups", Now: func() time.Time { return fedAuditClock },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.srv.UseAuthenticator(identity.Chain{jwt}); err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *whoamiHarness) as(t *testing.T, sub string, groups []string) string {
	t.Helper()
	return h.signer.sign(t, fedAuditClaims(sub, "jti-"+sub, groups))
}

// assertWhoami requires the full contract: 200, the route's own status header, JSON, and the
// body BYTE FOR BYTE.
func assertWhoami(t *testing.T, got reply, want string) {
	t.Helper()
	if got.status != 200 {
		t.Fatalf("whoami answered %d %q (status %q), want 200", got.status, got.body, got.headers.Get("X-Store-Status"))
	}
	if s := got.headers.Get("X-Store-Status"); s != "whoami" {
		t.Fatalf("X-Store-Status = %q, want %q", s, "whoami")
	}
	if ct := got.headers.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	if got.body != want {
		t.Fatalf("the whoami body is not the contract:\n  got:  %s\n  want: %s", got.body, want)
	}
}

// TestWhoamiAnswersEachFederationStatusAsTheContract pins the three federated statuses as
// literal bodies, including the mapped/unmapped split of an UNSORTED, DUPLICATED claim.
func TestWhoamiAnswersEachFederationStatusAsTheContract(t *testing.T) {
	h := newWhoamiHarness(t)
	for _, tc := range []struct {
		name, sub string
		groups    []string
		want      string
	}{
		{"mapped, with an unmapped group beside it", "member-0001",
			[]string{"zeta-x", "team-all", "alpha-readers", "ghost", "team-all"},
			`{"principal":"test-idp:member-0001","status":"mapped","groups":{"mapped":["alpha-readers","team-all"],"unmapped":["ghost","zeta-x"]},"scopes":["alpha-notes","beta-notes"]}` + "\n"},
		{"groups claimed, none mapped", "member-0002", []string{"ghost", "another-ghost"},
			`{"principal":"test-idp:member-0002","status":"groups-unmapped","groups":{"mapped":[],"unmapped":["another-ghost","ghost"]},"scopes":[]}` + "\n"},
		{"no groups claimed", "member-0003", []string{},
			`{"principal":"test-idp:member-0003","status":"no-groups","groups":{"mapped":[],"unmapped":[]},"scopes":[]}` + "\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertWhoami(t, h.do(t, "GET", "/api/v1/whoami", h.as(t, tc.sub, tc.groups), nil, ""), tc.want)
		})
	}
}

// TestWhoamiListsExactlyTheReadSetAsNames: scopes are the caller's READ set, as names.
//   - one scope-mapped group reaches one scope; beta-notes is ABSENT from its view, and the
//     POSITIVE CONTROL that beta-notes exists and is reachable is the project-mapped caller,
//     whose one mapping covers both of `fed`'s scopes;
//   - a mapping that RECORDS write still lists the scope (read, under the ceiling) — so a
//     view built from the write set would list nothing here;
//   - the same caller's `recall` agrees: what whoami lists, recall reads.
func TestWhoamiListsExactlyTheReadSetAsNames(t *testing.T) {
	h := newWhoamiHarness(t)
	assertWhoami(t, h.do(t, "GET", "/api/v1/whoami", h.as(t, "member-0010", []string{"alpha-readers"}), nil, ""),
		`{"principal":"test-idp:member-0010","status":"mapped","groups":{"mapped":["alpha-readers"],"unmapped":[]},"scopes":["alpha-notes"]}`+"\n")
	assertWhoami(t, h.do(t, "GET", "/api/v1/whoami", h.as(t, "member-0011", []string{"team-all"}), nil, ""),
		`{"principal":"test-idp:member-0011","status":"mapped","groups":{"mapped":["team-all"],"unmapped":[]},"scopes":["alpha-notes","beta-notes"]}`+"\n")
	writer := h.as(t, "member-0012", []string{"alpha-writers"})
	assertWhoami(t, h.do(t, "GET", "/api/v1/whoami", writer, nil, ""),
		`{"principal":"test-idp:member-0012","status":"mapped","groups":{"mapped":["alpha-writers"],"unmapped":[]},"scopes":["alpha-notes"]}`+"\n")
	if got := h.do(t, "GET", "/api/v1/recall/alpha-notes", writer, nil, ""); got.status != 200 ||
		got.headers.Get("X-Store-Status") != "recalled" {
		t.Fatalf("recall disagrees with whoami: %d %q", got.status, got.headers.Get("X-Store-Status"))
	}
	if got := h.do(t, "GET", "/api/v1/recall/beta-notes", writer, nil, ""); got.headers.Get("X-Store-Status") != "scope-absent" {
		t.Fatalf("a scope whoami does not list must be absent to recall, got %q", got.headers.Get("X-Store-Status"))
	}
}

// TestWhoamiIsASelfViewNotAnEnumeration: two callers, disjoint groups. Neither body names the
// other's groups or scopes, and neither names an object no claimed group reaches.
func TestWhoamiIsASelfViewNotAnEnumeration(t *testing.T) {
	h := newWhoamiHarness(t)
	a := h.do(t, "GET", "/api/v1/whoami", h.as(t, "member-0020", []string{"alpha-readers"}), nil, "")
	b := h.do(t, "GET", "/api/v1/whoami", h.as(t, "member-0021", []string{"gamma-team"}), nil, "")
	assertWhoami(t, b,
		`{"principal":"test-idp:member-0021","status":"mapped","groups":{"mapped":["gamma-team"],"unmapped":[]},"scopes":["gamma-notes"]}`+"\n")
	for _, leak := range []string{"gamma", "team-all", "alpha-writers", "member-0021", "fed", "other", "prj_", "scp_"} {
		if strings.Contains(a.body, leak) {
			t.Fatalf("caller A's whoami names %q, which only the map or another caller holds:\n%s", leak, a.body)
		}
	}
	for _, leak := range []string{"alpha", "team-all", "member-0020"} {
		if strings.Contains(b.body, leak) {
			t.Fatalf("caller B's whoami names %q:\n%s", leak, b.body)
		}
	}
}

// TestWhoamiGivesANonFederatedCallerItsOwnView: a machine token is not federated, so its
// status is `not-federated` and its groups are empty — and its scopes are still its read set.
func TestWhoamiGivesANonFederatedCallerItsOwnView(t *testing.T) {
	h := newMatrixHarness(t)
	assertWhoami(t, h.do(t, "GET", "/api/v1/whoami", matrixWideToken, nil, ""),
		`{"principal":"wide-reader","status":"not-federated","groups":{"mapped":[],"unmapped":[]},"scopes":["alpha-notes","beta-notes"]}`+"\n")
	assertWhoami(t, h.do(t, "GET", "/api/v1/whoami", matrixNarrowToken, nil, ""),
		`{"principal":"narrow-reader","status":"not-federated","groups":{"mapped":[],"unmapped":[]},"scopes":["beta-notes"]}`+"\n")
}

// TestWhoamiAddsNoRefusalAndABadCredentialIsTheUniform401: a bad token gets the 401 every route
// gives it, byte-identical to recall's, and it is COUNTED like recall's — five of them lock the
// address out, which a real limiter then shows on a good token. The route adds no 401 of its
// own: a caller that reaches nothing still gets 200 (the no-groups body above).
func TestWhoamiAddsNoRefusalAndABadCredentialIsTheUniform401(t *testing.T) {
	h := newWhoamiHarness(t)
	h.srv.Limiter = netid.NewRateLimiter(netid.DefaultMaxFailures, netid.DefaultFailureWindow, netid.DefaultLockout)
	good := h.as(t, "member-0030", []string{"alpha-readers"})
	if got := h.do(t, "GET", "/api/v1/whoami", good, nil, ""); got.status != 200 {
		t.Fatalf("POSITIVE CONTROL: a good token must reach whoami, got %d %q", got.status, got.body)
	}
	recall := h.do(t, "GET", "/api/v1/recall/alpha-notes", "not-a-token", nil, "")
	who := h.do(t, "GET", "/api/v1/whoami", "not-a-token", nil, "")
	if who.status != 401 || who.body != recall.body || who.headers.Get("X-Store-Status") != recall.headers.Get("X-Store-Status") {
		t.Fatalf("whoami's bad-token answer differs from recall's:\n  whoami %d %q %q\n  recall %d %q %q",
			who.status, who.body, who.headers.Get("X-Store-Status"), recall.status, recall.body, recall.headers.Get("X-Store-Status"))
	}
	for i := 0; i < netid.DefaultMaxFailures-2; i++ {
		h.do(t, "GET", "/api/v1/whoami", "not-a-token", nil, "")
	}
	if got := h.do(t, "GET", "/api/v1/whoami", good, nil, ""); got.status != 401 {
		t.Fatalf("five bad tokens (one on recall, four on whoami) did not lock the address out: good token got %d", got.status)
	}
}

// TestWhoamiWritesNothing: the control journal is byte-identical after a battery of whoami
// calls across every status, and the store is untouched.
func TestWhoamiWritesNothing(t *testing.T) {
	h := newWhoamiHarness(t)
	before, err := os.ReadFile(h.journal)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) == 0 {
		t.Fatal("POSITIVE CONTROL: the journal must hold the world's events")
	}
	served := 0
	for i, groups := range [][]string{{"alpha-readers"}, {"team-all", "ghost"}, {"ghost"}, {}} {
		got := h.do(t, "GET", "/api/v1/whoami", h.as(t, "member-004"+string(rune('0'+i)), groups), nil, "")
		if got.status == 200 {
			served++
		}
	}
	if served != 4 {
		t.Fatalf("POSITIVE CONTROL: %d of 4 whoami calls answered 200", served)
	}
	after, _ := os.ReadFile(h.journal)
	if string(before) != string(after) {
		t.Fatalf("whoami wrote to the control journal (%d bytes → %d)", len(before), len(after))
	}
}
