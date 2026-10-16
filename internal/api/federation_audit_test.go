package api

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
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

// GROUP FEDERATION AT THE SERVER SEAM, AUDIT HALF: a real server, a real
// `identity.SupabaseJWT` in group-federation mode over a JWKS served on loopback, and a
// real control journal on disk holding a group mapping. What is asserted is the AUDIT
// LINE a federated request produces — `sub=`, `jti=` and the NORMALISED `groups=` — and
// that a non-federated line does not change shape.
//
// 🔴 RED AT 1db64d9 BY CONSTRUCTION: `identity.Identity.Token`/`Groups` and the audit
// fields do not exist there, so this file does not compile; the behavioural half is that a
// federated request there was audited with `identity=` alone, which no field below can
// match.

const (
	fedAuditIssuer   = "https://issuer.example.test/"
	fedAuditAudience = "cairn-test-audience"
	fedAuditProvider = "test-idp"
	fedAuditKid      = "fed-key-1"
)

var fedAuditClock = time.Date(2000, 1, 6, 0, 0, 0, 0, time.UTC)

type fedAuditSigner struct{ key *ecdsa.PrivateKey }

func fedAuditB64(raw []byte) string { return base64.RawURLEncoding.EncodeToString(raw) }

func (s *fedAuditSigner) sign(t *testing.T, claims map[string]any) string {
	t.Helper()
	h, _ := json.Marshal(map[string]any{"alg": "ES256", "typ": "JWT", "kid": fedAuditKid})
	p, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	input := fedAuditB64(h) + "." + fedAuditB64(p)
	digest := sha256.Sum256([]byte(input))
	r, sv, err := ecdsa.Sign(rand.Reader, s.key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	sv.FillBytes(sig[32:])
	return input + "." + fedAuditB64(sig)
}

func fedAuditClaims(sub, jti string, groups any) map[string]any {
	c := map[string]any{
		"iss": fedAuditIssuer, "aud": fedAuditAudience, "sub": sub,
		"exp": fedAuditClock.Add(time.Hour).Unix(), "iat": fedAuditClock.Add(-time.Minute).Unix(),
		"groups": groups,
	}
	if jti != "" {
		c["jti"] = jti
	}
	return c
}

type fedAuditHarness struct {
	*matrixHarness
	signer *fedAuditSigner
	lines  []string
}

func newFedAuditHarness(t *testing.T) *fedAuditHarness {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	h := &fedAuditHarness{matrixHarness: newMatrixHarness(t), signer: &fedAuditSigner{key: key}}
	h.srv.Audit = func(line string) { h.lines = append(h.lines, line) }

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

	// A REAL journal on disk, written through the operator path, read through a Cache.
	journal, err := control.OpenFileStore(filepath.Join(t.TempDir(), "control.journal"))
	if err != nil {
		t.Fatal(err)
	}
	usr := control.DerivedID(control.PrefixUser, "owner")
	prj := control.DerivedID(control.PrefixProject, "fed")
	alpha := control.DerivedID(control.PrefixScope, "alpha-notes")
	beta := control.DerivedID(control.PrefixScope, "beta-notes")
	if _, err := journal.Append(context.Background(),
		control.Event{Kind: control.EventUserCreated, At: fedAuditClock, UserID: usr, Provider: "other-ns", Subject: "owner"},
		control.Event{Kind: control.EventProjectCreated, At: fedAuditClock, ProjectID: prj, Name: "fed", UserID: usr},
		control.Event{Kind: control.EventScopeCreated, At: fedAuditClock, ScopeID: alpha, DisplayName: "alpha-notes", ProjectID: prj},
		control.Event{Kind: control.EventScopeCreated, At: fedAuditClock, ScopeID: beta, DisplayName: "beta-notes", ProjectID: prj},
	); err != nil {
		t.Fatal(err)
	}
	if _, err := control.MapGroup(context.Background(), journal, control.GroupMapRequest{
		Provider: fedAuditProvider, Group: "alpha-readers", ObjectKind: control.ObjectScope, ObjectID: alpha,
		Verbs: control.NewVerbSet(control.VerbRead), At: fedAuditClock,
	}); err != nil {
		t.Fatal(err)
	}
	// A mapping that RECORDS write. The read ceiling caps it; see
	// `TestAFederatedWriteIsRefusedEvenWhereTheMappingGrantsWrite`.
	if _, err := control.MapGroup(context.Background(), journal, control.GroupMapRequest{
		Provider: fedAuditProvider, Group: "alpha-writers", ObjectKind: control.ObjectScope, ObjectID: alpha,
		Verbs: control.NewVerbSet(control.VerbRead, control.VerbWrite), At: fedAuditClock,
	}); err != nil {
		t.Fatal(err)
	}
	sessions := control.NewCache(journal, control.CacheOptions{Now: func() time.Time { return fedAuditClock }})
	if err := sessions.Refresh(context.Background()); err != nil {
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

func (h *fedAuditHarness) last(t *testing.T) string {
	t.Helper()
	if len(h.lines) == 0 {
		t.Fatal("precondition: no audit line was emitted")
	}
	return h.lines[len(h.lines)-1]
}

// TestAFederatedRequestIsAuditedWithItsSubJtiAndNormalisedGroups is the required seam test.
func TestAFederatedRequestIsAuditedWithItsSubJtiAndNormalisedGroups(t *testing.T) {
	h := newFedAuditHarness(t)

	// The claim as an issuer might send it: unsorted, with a duplicate, and an unmapped
	// group. The audit line must carry the NORMALISED set authority was computed from.
	token := h.signer.sign(t, fedAuditClaims("member-0007", "jti-fed-1",
		[]string{"zeta-team", "alpha-readers", "alpha-readers", "ghost"}))
	got := h.do(t, "GET", "/api/v1/recall/alpha-notes", token, nil, "")
	if got.status != 200 {
		t.Fatalf("a federated token mapped to alpha-notes got %d %q", got.status, got.body)
	}
	line := h.last(t)
	want := " auth=ok result=200 status=recalled sub=member-0007 jti=jti-fed-1 groups=alpha-readers,ghost,zeta-team"
	if !strings.HasSuffix(line, want) {
		t.Fatalf("the federated audit line does not END with the three fields in order:\n  got:  %s\n  want: …%s", line, want)
	}
	if !strings.Contains(line, " identity="+fedAuditProvider+":member-0007 ") {
		t.Fatalf("the line does not name the federated principal:\n  %s", line)
	}

	// An unmapped scope is an ABSENT answer, never a 401, and is audited the same way.
	other := h.do(t, "GET", "/api/v1/recall/beta-notes", token, nil, "")
	if other.status == 401 || other.headers.Get("X-Store-Status") != "scope-absent" {
		t.Fatalf("an unmapped scope answered %d (status %q), want scope-absent", other.status, other.headers.Get("X-Store-Status"))
	}

	// No groups claimed, and no jti: authenticated, `groups=-` and `jti=-` — the empty list
	// is still a federated line, decided by the federation report rather than by emptiness.
	empty := h.do(t, "GET", "/api/v1/recall/alpha-notes", h.signer.sign(t, fedAuditClaims("member-0008", "", []string{})), nil, "")
	if empty.status == 401 {
		t.Fatalf("an empty groups claim was refused: %d", empty.status)
	}
	if line := h.last(t); !strings.HasSuffix(line, " sub=member-0008 jti=- groups=-") ||
		!strings.Contains(line, " auth=ok ") {
		t.Fatalf("a no-groups federated line:\n  %s", line)
	}
}

// TestAGroupNameCannotForgeAnAuditField pins that the only bytes reaching `groups=` are
// names `control.ValidGroupName` accepted. A claim spelling a field is REFUSED (401), and
// its line carries no `groups=` at all — refused requests have no federation report.
func TestAGroupNameCannotForgeAnAuditField(t *testing.T) {
	h := newFedAuditHarness(t)
	for _, forged := range [][]string{
		{"alpha-readers groups=admin"},
		{"alpha-readers\nstore-api audit forged=1"},
		{"alpha-readers,admin"},
	} {
		got := h.do(t, "GET", "/api/v1/recall/alpha-notes", h.signer.sign(t, fedAuditClaims("member-0009", "j", forged)), nil, "")
		if got.status != 401 {
			t.Errorf("claim %q was accepted with %d", forged, got.status)
		}
		line := h.last(t)
		if strings.Contains(line, "groups=") || strings.Contains(line, "admin") || strings.Contains(line, "forged") {
			t.Errorf("a refused forged claim reached the audit line:\n  %s", line)
		}
	}
	// And `auditField` is still on the path: a maximal valid claim renders whole, as ONE
	// field with no space in it.
	var many []string
	for i := 0; i < control.MaxClaimedGroups; i++ {
		many = append(many, strings.Repeat(string(rune('a'+i%26)), control.MaxGroupNameLen-2)+string(rune('a'+i/26))+"x")
	}
	got := h.do(t, "GET", "/api/v1/recall/alpha-notes", h.signer.sign(t, fedAuditClaims("member-0010", "j", many)), nil, "")
	if got.status == 401 {
		t.Fatalf("a maximal valid claim was refused")
	}
	line := h.last(t)
	field := line[strings.Index(line, " groups=")+len(" groups="):]
	if strings.Contains(field, " ") || strings.Contains(field, "truncated") || strings.Count(field, ",") != control.MaxClaimedGroups-1 {
		t.Fatalf("a maximal claim did not render as one whole field (len %d, %d commas)", len(field), strings.Count(field, ","))
	}
}

// TestAMachineTokenLineCarriesNoTokenOrGroupFields is an INVARIANT guard (the bug it
// would catch never existed at 1db64d9): every non-JWT line keeps its old shape, which is
// what `tests/dualrun/` compares against the oracle.
func TestAMachineTokenLineCarriesNoTokenOrGroupFields(t *testing.T) {
	h := newMatrixHarness(t)
	var lines []string
	h.srv.Audit = func(line string) { lines = append(lines, line) }
	if got := h.do(t, "GET", "/api/v1/recall/alpha-notes", matrixWideToken, nil, ""); got.status != 200 {
		t.Fatalf("precondition: %d", got.status)
	}
	line := lines[len(lines)-1]
	if !strings.HasSuffix(line, " auth=ok result=200 status=recalled") {
		t.Fatalf("a machine-token line gained a field:\n  %s", line)
	}
}

// TestAFederatedWriteIsRefusedEvenWhereTheMappingGrantsWrite pins the READ CEILING at the
// server: a group mapped read+write still gets the no-write-anywhere answer, nothing lands,
// and the refusal is not a 401 and is not counted toward the lockout.
//
// 🔴 RED AT e1b1356: there the write landed (200) with an attribution trailer whose actor is
// the federated display `test-idp:member-0011` — outside the trailer grammar's actor class,
// so the bullet's attribution is MALFORMED and a re-post from another session appends a
// duplicate rather than being idempotent.
func TestAFederatedWriteIsRefusedEvenWhereTheMappingGrantsWrite(t *testing.T) {
	h := newFedAuditHarness(t)
	// A REAL five-failure limiter, so "not counted" is measurable rather than vacuous.
	h.srv.Limiter = netid.NewRateLimiter(netid.DefaultMaxFailures, netid.DefaultFailureWindow, netid.DefaultLockout)
	token := h.signer.sign(t, fedAuditClaims("member-0011", "jti-w", []string{"alpha-writers"}))
	entry := filepath.Join(h.srv.StoreRoot, "alpha-notes", "gadget-one.md")
	before, err := os.ReadFile(entry)
	if err != nil {
		t.Fatalf("precondition: the fixture entry: %v", err)
	}
	// Positive control: the same token READS the scope, so the refusal below is about
	// writing, not about reaching.
	if got := h.do(t, "GET", "/api/v1/recall/alpha-notes", token, nil, ""); got.status != 200 {
		t.Fatalf("precondition: the federated token cannot read alpha-notes: %d", got.status)
	}

	for i := 0; i < 7; i++ { // more than DefaultMaxFailures, so a counted refusal would lock out
		got := h.do(t, "POST", "/api/v1/entry/alpha-notes/gadget-one/bullets", token, nil,
			`{"text":"a federated note","session":"s-one"}`)
		if got.status != 403 || got.headers.Get("X-Store-Status") != "legacy-cannot-write" {
			t.Fatalf("post %d: a federated write answered %d %q (status %q), want the no-write-anywhere 403",
				i, got.status, got.body, got.headers.Get("X-Store-Status"))
		}
	}
	put := h.do(t, "PUT", "/api/v1/entry/alpha-notes/newcomer-one", token,
		map[string]string{"If-None-Match": "*"},
		"---\nservice: newcomer-one\nscope: alpha-notes\n---\n\n## What it is\nsynthetic.\n")
	if put.status != 403 {
		t.Fatalf("a federated create answered %d %q, want 403", put.status, put.body)
	}
	after, _ := os.ReadFile(entry)
	if string(before) != string(after) {
		t.Fatalf("a refused federated write changed the entry:\n%s", after)
	}
	if _, err := os.Stat(filepath.Join(h.srv.StoreRoot, "alpha-notes", "newcomer-one.md")); err == nil {
		t.Fatal("a refused federated create wrote a file")
	}
	// Not counted: after eight refusals from one address the same token still reads.
	if got := h.do(t, "GET", "/api/v1/recall/alpha-notes", token, nil, ""); got.status != 200 {
		t.Fatalf("after the refused writes the token reads %d — a refusal was counted toward the lockout", got.status)
	}
	// POSITIVE CONTROL on the limiter: five bad credentials from the same address DO lock
	// it out, so the green above is about the write refusal and not about a limiter that
	// counts nothing.
	for i := 0; i < netid.DefaultMaxFailures; i++ {
		h.do(t, "GET", "/api/v1/recall/alpha-notes", "not-a-token", nil, "")
	}
	if got := h.do(t, "GET", "/api/v1/recall/alpha-notes", token, nil, ""); got.status != 401 {
		t.Fatalf("five bad credentials did not lock the address out (%d) — the limiter is not counting", got.status)
	}
}
