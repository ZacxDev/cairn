package identity

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/control"
)

// The group federation tests, over a REAL journal read through a `control.Cache` — the
// read path the pod uses — so "nothing is written on a read" is measured on the file.

const testGroupsClaim = "groups"

// federatedJournal provisions the fixture user (who OWNS `quarry-notes`) and maps the group
// `notes-readers` at THIS verifier's provider to that scope, read only.
func federatedJournal(t *testing.T) (*control.FileStore, string, *control.Cache, control.Provisioned) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "control.journal")
	store, err := control.OpenFileStore(path)
	if err != nil {
		t.Fatalf("opening a journal: %v", err)
	}
	made, err := control.ProvisionUser(context.Background(), store, control.NewUser{
		Provider: testProvider, Subject: testSubject, Email: testEmail,
		ProjectName: "quarry", ScopeNames: []string{"quarry-notes"}, At: testClock,
	})
	if err != nil {
		t.Fatalf("provisioning: %v", err)
	}
	if _, err := control.MapGroup(context.Background(), store, control.GroupMapRequest{
		Provider: testProvider, Group: "notes-readers",
		ObjectKind: control.ObjectScope, ObjectID: made.Scopes[0].ID,
		Verbs: control.NewVerbSet(control.VerbRead), At: testClock,
	}); err != nil {
		t.Fatalf("mapping the group: %v", err)
	}
	cache := control.NewCache(store, control.CacheOptions{Now: fixedNow})
	if err := cache.Refresh(context.Background()); err != nil {
		t.Fatalf("materializing: %v", err)
	}
	return store, path, cache, made
}

// federatedChain builds the pod's chain from the environment, with the mode on or off.
//
// ⚠ IT RETURNS THE BACKEND TOO, because the chain answers every refusal with one uniform
// sentence (the wire must not discriminate), so a test that asserts WHICH check refused has
// to ask the backend.
func federatedChain(t *testing.T, sessions ModelSource, signer *signer, mode bool) (Chain, *SupabaseJWT) {
	t.Helper()
	idp := newJWKSServer(t, jwksDocumentOf(t, signer.publicJWK(t)))
	env := map[string]string{EnvSupabaseIssuer: testIssuer, EnvSupabaseJWKSURL: idp.url()}
	if mode {
		env[EnvSupabaseGroupsClaim] = testGroupsClaim
	}
	chain, supabase, err := FromEnvironment(env, newTestAuthority(t), sessions)
	if err != nil {
		t.Fatalf("building the chain: %v", err)
	}
	if err := supabase.RefreshKeys(t.Context()); err != nil {
		t.Fatalf("materializing the loopback JWKS: %v", err)
	}
	if got := supabase.GroupsClaim(); (got != "") != mode {
		t.Fatalf("the backend's groups claim is %q with the mode %v", got, mode)
	}
	return chain, supabase
}

func claimsWith(sub string, groups any) claimSet {
	c := liveClaims()
	c["sub"] = sub
	if groups != nil {
		c[testGroupsClaim] = groups
	}
	return c
}

// TestAFederatedTokenReachesExactlyItsMappedGroupsAndWritesNothing is the end-to-end claim
// and the no-write claim together: a battery of authentications — mapped, unmapped, empty,
// malformed — leaves the journal byte-identical and its epoch unmoved.
//
// RED at the base commit by construction (`EnvSupabaseGroupsClaim` and `control.MapGroup` do
// not exist there). The behavioural red is `TestWithTheModeOffAGroupsClaimIsIgnored`'s arm,
// which is what the base commit does with every token below.
func TestAFederatedTokenReachesExactlyItsMappedGroupsAndWritesNothing(t *testing.T) {
	store, path, cache, made := federatedJournal(t)
	s := newRSASigner(t, "rsa-1", 2048)
	chain, _ := federatedChain(t, cache, s, true)

	beforeBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	beforeModel, err := store.Model(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	who, err := chain.Authenticate(bearer(t, s.sign(t, claimsWith(testStranger, []string{"notes-readers", "ghost"}), nil)))
	if err != nil {
		t.Fatalf("a verified token claiming a mapped group must authenticate: %v", err)
	}
	if who.Principal.Kind != control.KindFederated || who.Principal.Display != testProvider+":"+testStranger {
		t.Fatalf("principal = %+v, want a federated %s:%s", who.Principal, testProvider, testStranger)
	}
	if got := who.Auth.ScopeIDs(control.VerbRead); len(got) != 1 || got[0] != made.Scopes[0].ID {
		t.Fatalf("reaches %v for read, want exactly the mapped scope %s", got, made.Scopes[0].ID)
	}
	if who.Auth.Allows(made.Scopes[0].ID, control.VerbWrite) {
		t.Fatal("the group was mapped read-only and the token can write")
	}
	// The audit facts: the token's own sub, and the NORMALISED claim (sorted) — not the
	// order the issuer sent.
	if who.Token == nil || who.Token.Subject != testStranger {
		t.Fatalf("the federated identity carries token facts %+v, want sub %s", who.Token, testStranger)
	}
	if strings.Join(who.Groups, ",") != "ghost,notes-readers" {
		t.Fatalf("Identity.Groups = %v, want the normalised [ghost notes-readers]", who.Groups)
	}
	report, ok := who.Auth.Federation()
	if !ok || report.Status != control.FederationMapped ||
		strings.Join(report.Mapped, ",") != "notes-readers" || strings.Join(report.Unmapped, ",") != "ghost" {
		t.Fatalf("federation report = %+v (%v)", report, ok)
	}

	// The battery. Every shape a caller can send, many times.
	authenticated := 0
	for i := 0; i < 5; i++ {
		for _, groups := range []any{[]string{"notes-readers"}, []string{"ghost"}, []string{}, "not-a-list", []string{"BAD"}} {
			if _, err := chain.Authenticate(bearer(t, s.sign(t, claimsWith(fmt.Sprintf("subject-%d", i), groups), nil))); err == nil {
				authenticated++
			}
		}
	}
	// POSITIVE CONTROL on the battery: the three valid shapes authenticated every time, so
	// the zero below is about the journal and not about a battery that never got in.
	if authenticated != 15 {
		t.Fatalf("%d of 25 authenticated, want 15 (3 valid shapes × 5)", authenticated)
	}

	afterBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	afterModel, err := store.Model(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeBytes, afterBytes) || afterModel.Epoch != beforeModel.Epoch ||
		len(afterModel.Users) != len(beforeModel.Users) {
		t.Fatalf("authentication WROTE to the journal: %d → %d bytes, epoch %d → %d, users %d → %d",
			len(beforeBytes), len(afterBytes), beforeModel.Epoch, afterModel.Epoch,
			len(beforeModel.Users), len(afterModel.Users))
	}
}

// TestFederatedModeIsOnePathAndDoesNotConsultAJournalUser pins the decision: with the mode
// on, a token for a subject the journal DOES hold resolves from its groups, not from that
// user's membership — the operator was warned at startup (`announceGroupFederation`).
func TestFederatedModeIsOnePathAndDoesNotConsultAJournalUser(t *testing.T) {
	_, _, cache, made := federatedJournal(t)
	s := newRSASigner(t, "rsa-1", 2048)
	token := s.sign(t, claimsWith(testSubject, []string{}), nil)

	// Positive control: with the mode OFF the same subject is the owner, with full authority.
	offChain, _ := federatedChain(t, cache, s, false)
	off, err := offChain.Authenticate(bearer(t, token))
	if err != nil || off.Principal.ID != made.User || !off.Auth.Allows(made.Scopes[0].ID, control.VerbAdmin) {
		t.Fatalf("mode off: %+v %v — the fixture user should be the owner", off.Principal, err)
	}

	onChain, _ := federatedChain(t, cache, s, true)
	on, err := onChain.Authenticate(bearer(t, token))
	if err != nil {
		t.Fatalf("mode on: %v", err)
	}
	if on.Principal.Kind != control.KindFederated {
		t.Fatalf("mode on resolved %+v — the journal user was consulted", on.Principal)
	}
	if got := on.Auth.ScopeIDs(control.VerbRead); len(got) != 0 {
		t.Fatalf("an empty groups claim reaches %v — the owner's membership leaked into a federated principal", got)
	}
	if r, _ := on.Auth.Federation(); r.Status != control.FederationNoGroups {
		t.Fatalf("status = %q, want %q", r.Status, control.FederationNoGroups)
	}
}

// TestWithTheModeOffAGroupsClaimIsIgnored is the compatibility claim: a deployment that does
// not set the variable behaves as the base commit does, even for a token CARRYING a groups
// claim the journal maps.
func TestWithTheModeOffAGroupsClaimIsIgnored(t *testing.T) {
	_, _, cache, _ := federatedJournal(t)
	s := newRSASigner(t, "rsa-1", 2048)
	token := s.sign(t, claimsWith(testStranger, []string{"notes-readers"}), nil)

	// Positive control: the token really carries the claim, and it is a valid one.
	groups, err := claimedGroups(token, testGroupsClaim)
	if err != nil || len(groups) != 1 || groups[0] != "notes-readers" {
		t.Fatalf("the token does not carry the claim this test is about: %v %v", groups, err)
	}
	_, backend := federatedChain(t, cache, s, false)
	_, err = backend.Authenticate(bearer(t, token))
	if err == nil {
		t.Fatal("with the mode off, a token for an unknown subject authenticated on its groups claim")
	}
	if !strings.Contains(err.Error(), "holds no user for") {
		t.Fatalf("refused for a different reason than the unknown user: %v", err)
	}
}

// TestAMalformedGroupsClaimRefusesTheWholeToken walks the shapes. Each refusal is the
// claim's own (`control.ErrGroupsClaim` in the chain), never a signature or claims refusal.
func TestAMalformedGroupsClaimRefusesTheWholeToken(t *testing.T) {
	_, _, cache, _ := federatedJournal(t)
	s := newRSASigner(t, "rsa-1", 2048)
	chain, backend := federatedChain(t, cache, s, true)

	many := make([]string, 0, control.MaxClaimedGroups+1)
	for i := 0; i <= control.MaxClaimedGroups; i++ {
		many = append(many, fmt.Sprintf("g%d", i))
	}
	for name, groups := range map[string]any{
		"absent":              nil,
		"null":                "__null__",
		"a string":            "notes-readers",
		"an object":           map[string]any{"notes-readers": true},
		"a number member":     []any{"notes-readers", 7},
		"an invalid name":     []string{"notes-readers", "Notes Readers"},
		"more than the bound": many,
		"a nested array":      []any{[]string{"notes-readers"}},
		"an empty string":     []string{""},
	} {
		claims := claimsWith(testStranger, groups)
		if groups == "__null__" {
			claims[testGroupsClaim] = nil
		}
		token := s.sign(t, claims, nil)
		if _, err := Verify(token, VerifyOptions{Algs: []Alg{AlgRS256}, Issuer: testIssuer, Audience: testAudience,
			Keys: keySetOver(t, s)}); err != nil {
			t.Fatalf("%s: precondition — the token must VERIFY, so the refusal below is the claim's: %v", name, err)
		}
		if _, err := chain.Authenticate(bearer(t, token)); err == nil {
			t.Errorf("%s: a malformed groups claim authenticated through the chain", name)
		}
		_, err := backend.Authenticate(bearer(t, token))
		if err == nil {
			t.Errorf("%s: a malformed groups claim authenticated", name)
			continue
		}
		if !strings.Contains(err.Error(), "groups claim") {
			t.Errorf("%s: refused, but not by the groups-claim check: %v", name, err)
		}
	}
}

// TestTheGroupsClaimSettingRefusesWhatItCannotRead pins the configuration refusals.
func TestTheGroupsClaimSettingRefusesWhatItCannotRead(t *testing.T) {
	s := newRSASigner(t, "rsa-1", 2048)
	cfg := goodSupabaseConfig(t, keySetOver(t, s))
	cfg.GroupsClaim = "realm_access.roles"
	if _, err := NewSupabaseJWT(cfg); !errors.Is(err, ErrGroupsClaimName) {
		t.Errorf("a nested claim path built: %v", err)
	}
	cfg.GroupsClaim = testGroupsClaim
	if _, err := NewSupabaseJWT(cfg); err != nil {
		t.Errorf("a plain claim name was refused: %v", err)
	}

	// The browser surface refuses the mode outright, before building anything.
	_, armed, err := SupabaseBackendFromEnvironment(map[string]string{
		EnvSupabaseIssuer: testIssuer, EnvSupabaseJWKSURL: "https://idp.example.test/jwks",
		EnvSupabaseGroupsClaim: testGroupsClaim,
	}, newTestAuthority(t))
	if !errors.Is(err, ErrGroupFederationOnBrowserSurface) || armed {
		t.Errorf("the browser surface built a federated backend: armed=%v err=%v", armed, err)
	}
	// Positive control: without the setting the same environment builds there.
	if _, armed, err := SupabaseBackendFromEnvironment(map[string]string{
		EnvSupabaseIssuer: testIssuer, EnvSupabaseJWKSURL: "https://idp.example.test/jwks",
	}, newTestAuthority(t)); err != nil || !armed {
		t.Errorf("the browser surface did not build its ordinary backend: armed=%v err=%v", armed, err)
	}
}
