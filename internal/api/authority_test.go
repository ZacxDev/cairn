package api

import (
	"context"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/authz"
	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/control/tokenfile"
	"github.com/ZacxDev/cairn/internal/netid"
	"github.com/ZacxDev/cairn/internal/store"
)

// ---------------------------------------------------------------------------
// THE SERVED AUTHORIZATION MATRIX
//
// 🔴 THIS IS THE RELATIONSHIP PIN THE PLAN'S §C ASKS FOR, AND IT IS AT THE SEAM
// RATHER THAN INSIDE EITHER COMPONENT. `internal/control`'s own matrix test asserts
// principal × scope × verb over the MODEL; every test in this file's neighbour asserts
// one ROUTE at a time. Both can be green while the two are broken together — the
// defect that lives in the seam nobody owns — because neither ever builds the combined
// state. This one does: it drives real HTTP requests through a real server whose
// authority is a real projection of a real token file, and writes out EVERY cell.
//
// It fails when the set GROWS as well as when it shrinks, and it carries a positive
// control on the ALLOW COUNT, because a matrix in which every cell refuses is perfectly
// self-consistent and measures nothing — the shape `tests/parity/README.md` records the
// P2 harness shipping with (72 PASS / 0 FAIL while the pod refused every request).
// ---------------------------------------------------------------------------

// The fixture's principals. Each reaches a different set, and no two rows of the matrix
// are the same, so every mechanism has its own witness.
const (
	matrixWideToken   = "matrix-wide-token-matrix-wide-token-matrix-"
	matrixNarrowToken = "matrix-narrow-token-matrix-narrow-token-mat"
	matrixBareToken   = "matrix-bare-token-matrix-bare-token-matrix-"
)

// outcome is the vocabulary the matrix is written in. Each word is pinned to exact
// bytes by `probe`, so a cell cannot pass by a word being spelled differently.
type outcome string

const (
	// allow: the operation was performed.
	allow outcome = "allow"
	// absent: refused OR never existed — and the whole point is that the caller
	// cannot tell which. Every read route answers `scope-absent` and every write
	// route answers the not-found body.
	absent outcome = "absent"
	// noWriteVerb: this principal holds the write verb NOWHERE, so every write route
	// answers the same 403 naming no scope.
	noWriteVerb outcome = "no-write-verb"
)

// matrixHarness is a server over a store the standard fixture does not build: THREE
// populated scopes, so that a scope only the unrestricted row can reach exists.
//
// 🔴 WITHOUT `gamma-notes` THE BARE ROW'S ROW OF THE MATRIX IS IDENTICAL TO
// `wide-reader`'s READ ROW, and a mutant that gave the bare row the wide row's
// allowlist would SURVIVE the whole table. The fixture's job is to make each mechanism
// separately observable; `gamma-notes` is what makes "unrestricted" observable at all.
type matrixHarness struct {
	*harness
}

func newMatrixHarness(t *testing.T) *matrixHarness {
	t.Helper()
	root := t.TempDir()
	entry := func(service, scope string) string {
		return "---\nservice: " + service + "\nscope: " + scope + "\n---\n\n" +
			"## What it is\nsynthetic.\n\n## Nuance / work-history\n- 2000-01-02: a lease note.\n"
	}
	for _, sc := range []struct{ scope, service string }{
		{"alpha-notes", "gadget-one"},
		{"beta-notes", "widget-three"},
		{"gamma-notes", "sprocket-nine"},
	} {
		dir := filepath.Join(root, sc.scope)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, sc.service+".md"),
			[]byte(entry(sc.service, sc.scope)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// `alpha-notes` carries a `.git/HEAD` for the reason the standard fixture does: the
	// revision header is the ONE answer not derived from the narrowed index, so it is
	// the one channel through which a refused scope could still be told apart from an
	// absent one.
	if err := os.MkdirAll(filepath.Join(root, "alpha-notes", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "alpha-notes", ".git", "HEAD"),
		[]byte("a11ba11ba11ba11ba11ba11ba11ba11ba11ba11b\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv, err := New(root, []authz.TokenRecord{
		{Token: matrixWideToken, Identity: "wide-reader", Scopes: []string{"alpha-notes", "beta-notes"}},
		{Token: matrixNarrowToken, Identity: "narrow-reader", Scopes: []string{"beta-notes"}},
		authz.LegacyRecord(matrixBareToken),
	}, []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")},
		netid.NewRateLimiter(1000000, time.Minute, 15*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	srv.Audit = func(string) {}
	srv.Warn = func(string) {}
	clock := time.Date(2000, 1, 5, 0, 0, 0, 0, time.UTC)
	srv.Now = func() time.Time { return clock }
	h := &harness{srv: srv, root: root, clock: clock}
	h.tsrv = httptest.NewServer(srv)
	t.Cleanup(h.tsrv.Close)
	return &matrixHarness{harness: h}
}

// probe issues ONE request and maps the response to the matrix vocabulary.
//
// 🔴 EVERY ARM PINS A STATUS **AND** AN `X-Store-Status`, so the vocabulary is a claim
// about bytes rather than about a word. A response that is neither shape is a failure
// naming what it actually was — never silently folded into `absent`, which is the
// direction that would make a broken server look uniformly refused.
func (h *matrixHarness) probe(t *testing.T, kind, token, scope string) outcome {
	t.Helper()
	switch kind {
	case "recall":
		got := h.do(t, "GET", "/api/v1/recall/"+scope, token, nil, "")
		return readOutcome(t, kind, got, "recalled")
	case "search":
		got := h.do(t, "GET", "/api/v1/search/"+scope+"?q=lease", token, nil, "")
		return readOutcome(t, kind, got, "search-hit")
	case "snapshot":
		got := h.do(t, "GET", "/api/v1/snapshot?scope="+scope, token, nil, "")
		if got.status != 200 || got.headers.Get("X-Store-Status") != "snapshot" {
			t.Fatalf("snapshot %s: every authenticated caller gets a tar, got %d %q",
				scope, got.status, got.headers.Get("X-Store-Status"))
		}
		// 🔴 THE OBSERVABLE IS THE ENTRY COUNT, NOT THE STATUS. `/snapshot` answers 200
		// to everybody and NARROWS its candidate list, so a refused scope is an empty
		// tar — which is exactly what an absent scope is. The count is the server's own
		// declaration of what it put in.
		n, err := strconv.Atoi(got.headers.Get("X-Store-Entries"))
		if err != nil {
			t.Fatalf("snapshot %s: X-Store-Entries %q is not a number", scope, got.headers.Get("X-Store-Entries"))
		}
		if n > 0 {
			return allow
		}
		return absent
	case "append":
		got := h.do(t, "POST", "/api/v1/entry/"+scope+"/"+serviceIn(scope)+"/bullets",
			token, nil, `{"text":"a matrix bullet","session":"matrix"}`)
		return writeOutcome(t, kind, got, 200)
	case "create":
		got := h.do(t, "PUT", "/api/v1/entry/"+scope+"/matrix-newcomer", token,
			map[string]string{"If-None-Match": "*"},
			"---\nservice: matrix-newcomer\nscope: "+scope+"\n---\n\n## What it is\nsynthetic.\n")
		return writeOutcome(t, kind, got, 201)
	}
	t.Fatalf("no probe named %q", kind)
	return ""
}

func readOutcome(t *testing.T, kind string, got reply, allowStatus string) outcome {
	t.Helper()
	if got.status != 200 {
		t.Fatalf("%s: a read route answers 200 for an authenticated caller, got %d %q",
			kind, got.status, got.body)
	}
	switch got.headers.Get("X-Store-Status") {
	case allowStatus:
		return allow
	case "scope-absent":
		return absent
	}
	t.Fatalf("%s: unexpected X-Store-Status %q", kind, got.headers.Get("X-Store-Status"))
	return ""
}

func writeOutcome(t *testing.T, kind string, got reply, allowStatus int) outcome {
	t.Helper()
	switch {
	case got.status == allowStatus:
		return allow
	case got.status == 404 && got.headers.Get("X-Store-Status") == "not-found":
		return absent
	case got.status == 403 && got.headers.Get("X-Store-Status") == "legacy-cannot-write":
		return noWriteVerb
	}
	t.Fatalf("%s: unexpected %d / %q / %q", kind, got.status, got.headers.Get("X-Store-Status"), got.body)
	return ""
}

// serviceIn is the ref each populated scope's one entry answers to. An absent scope
// gets a name nothing resolves, which is the right input: the answer must come from the
// narrowing, not from the ref.
func serviceIn(scope string) string {
	switch scope {
	case "alpha-notes":
		return "gadget-one"
	case "beta-notes":
		return "widget-three"
	case "gamma-notes":
		return "sprocket-nine"
	}
	return "no-such-ref"
}

// TestTheServedAuthorizationMatrixIsExactlyThis is the ledger.
//
// 🔴 THREE PRINCIPALS × FOUR SCOPES × FIVE PROBES, EVERY CELL WRITTEN OUT. The probes
// are the three narrowing sites the plan names (the index loader through `recall`, the
// result shape through `search`, the snapshot candidate filter) plus BOTH write routes,
// because the write path is part of the same predicate now and a matrix that omitted it
// would leave the one verb that can destroy content unpinned.
//
// 🔴 EACH CELL RUNS AGAINST A FRESH SERVER AND A FRESH STORE. The write probes MUTATE —
// `append` adds a bullet and `create` writes a file — so cells run over one store would
// depend on their order, and an order-dependent matrix is a matrix that passes for a
// reason nobody wrote down.
//
// 🔴 AND IT WAS RUN AGAINST THE **OLD** MECHANISM, WHICH IS WHAT MAKES IT A DIFFERENTIAL
// CLAIM RATHER THAN A DESCRIPTION OF WHAT THE NEW CODE HAPPENS TO DO. Measured at
// `3c8707c`, the token-file server: all 60 cells agree and the allow count is the same
// 24. A ledger that only ever ran against the code it was written beside would be
// indistinguishable from one derived from that code, which this repository's rules name
// explicitly.
//
// ⚠ **REPRODUCING THAT TAKES AN EXTRACTION, AND AN EARLIER DRAFT OF THIS COMMENT SAID
// OTHERWISE.** It claimed "this file references nothing from `internal/control`, so it
// compiles there". The LEDGER and its HARNESS reference nothing from it — that is the
// load-bearing half and it is true — but the FILE also holds tests that DO
// (`mustAuthorize`, the two `Source` doubles, the out-of-band-scope case), and
// `internal/control/tokenfile` does not exist at `3c8707c` at all. So the file as
// committed fails to BUILD there, and a reader checking the claim as it was written
// would have read that build failure as the claim being false. The reproduction is:
// copy this file's first ~358 lines (through `sortedKeys`) into a checkout of
// `3c8707c`, drop the `control`, `tokenfile`, `context` and `store` imports that only
// the removed tests needed, and run this one test.
//
// ⚠ It is therefore NOT regression coverage for the contract — nothing was broken — but
// it IS the seam guard for the MECHANISM: four rows of `tests/control_mutants.py` are
// killed by this test and by nothing else.
func TestTheServedAuthorizationMatrixIsExactlyThis(t *testing.T) {
	// The world, declared here so the ledger below can be checked against it rather
	// than against itself.
	principals := map[string]string{
		"wide-reader":   matrixWideToken,
		"narrow-reader": matrixNarrowToken,
		"legacy":        matrixBareToken,
	}
	scopes := []string{
		"alpha-notes", // exists; wide + bare
		"beta-notes",  // exists; everybody
		"gamma-notes", // exists; BARE ONLY — the witness for "unrestricted"
		"delta-notes", // NEVER EXISTED — the absent half of refused-versus-absent
	}
	probes := []string{"recall", "search", "snapshot", "append", "create"}

	// 🔴 THE LEDGER. Written from the CONTRACT, not from a run: a mapped row reaches
	// what it names and may write it; a bare row reaches everything and may write
	// nothing; a scope outside a principal's authority answers what a scope that never
	// existed answers, on every route.
	want := map[string]map[string]map[string]outcome{
		"wide-reader": {
			"alpha-notes": {"recall": allow, "search": allow, "snapshot": allow, "append": allow, "create": allow},
			"beta-notes":  {"recall": allow, "search": allow, "snapshot": allow, "append": allow, "create": allow},
			"gamma-notes": {"recall": absent, "search": absent, "snapshot": absent, "append": absent, "create": absent},
			"delta-notes": {"recall": absent, "search": absent, "snapshot": absent, "append": absent, "create": absent},
		},
		"narrow-reader": {
			"alpha-notes": {"recall": absent, "search": absent, "snapshot": absent, "append": absent, "create": absent},
			"beta-notes":  {"recall": allow, "search": allow, "snapshot": allow, "append": allow, "create": allow},
			"gamma-notes": {"recall": absent, "search": absent, "snapshot": absent, "append": absent, "create": absent},
			"delta-notes": {"recall": absent, "search": absent, "snapshot": absent, "append": absent, "create": absent},
		},
		// 🔴 THE BARE ROW: READS EVERYWHERE, WRITES NOWHERE — AND THE WRITE ANSWER IS
		// THE SAME FOR `delta-notes` AS FOR A SCOPE THAT EXISTS. That is the whole
		// reason the write refusal asks "may this write ANYWHERE" rather than "may this
		// write HERE": the answer names no scope, so it discriminates nothing.
		"legacy": {
			"alpha-notes": {"recall": allow, "search": allow, "snapshot": allow, "append": noWriteVerb, "create": noWriteVerb},
			"beta-notes":  {"recall": allow, "search": allow, "snapshot": allow, "append": noWriteVerb, "create": noWriteVerb},
			"gamma-notes": {"recall": allow, "search": allow, "snapshot": allow, "append": noWriteVerb, "create": noWriteVerb},
			"delta-notes": {"recall": absent, "search": absent, "snapshot": absent, "append": noWriteVerb, "create": noWriteVerb},
		},
	}

	// 🔴 THE LEDGER MUST NAME EXACTLY THE WORLD, AND IT FAILS WHEN THE SET GROWS. A
	// principal or a scope or a probe added without a row is not "untested", it is
	// unnoticed — and a table that silently ignored one would report full coverage over
	// a set that has changed underneath it.
	if len(want) != len(principals) {
		t.Fatalf("the ledger names %d principals and the fixture has %d", len(want), len(principals))
	}
	for name := range want {
		if _, known := principals[name]; !known {
			t.Fatalf("the ledger names principal %q, which the fixture does not create", name)
		}
		if len(want[name]) != len(scopes) {
			t.Fatalf("%s: the ledger names %d scopes and the matrix probes %d", name, len(want[name]), len(scopes))
		}
		for _, scope := range scopes {
			row, named := want[name][scope]
			if !named {
				t.Fatalf("%s: the ledger has no row for scope %q", name, scope)
			}
			if len(row) != len(probes) {
				t.Fatalf("%s/%s: the ledger names %d probes and the matrix runs %d", name, scope, len(row), len(probes))
			}
			for _, probe := range probes {
				if _, named := row[probe]; !named {
					t.Fatalf("%s/%s: the ledger has no cell for probe %q", name, scope, probe)
				}
			}
		}
	}

	allows := 0
	for _, name := range sortedKeys(principals) {
		for _, scope := range scopes {
			for _, probe := range probes {
				h := newMatrixHarness(t)
				got := h.probe(t, probe, principals[name], scope)
				if got != want[name][scope][probe] {
					t.Fatalf("%s / %s / %s: got %q, want %q",
						name, scope, probe, got, want[name][scope][probe])
				}
				if got == allow {
					allows++
				}
			}
		}
	}

	// 🔴 THE POSITIVE CONTROL. A server that refused everything satisfies every
	// `absent` cell above and would be reported as a fully green matrix. 24 of 60 cells
	// ALLOW — not 0 and not 60 — and any fixture change moves that number and forces
	// whoever made it to say what they expected.
	const wantAllows = 24
	if allows != wantAllows {
		t.Fatalf("the matrix allowed %d of %d cells, want %d — a matrix in which every "+
			"cell refuses is perfectly self-consistent and measures nothing",
			allows, len(principals)*len(scopes)*len(probes), wantAllows)
	}
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestARefusedScopeIsNotDiscriminatedByTheRevisionHeader closes the one channel the
// matrix's `X-Store-Status` column cannot see.
//
// 🔴 `X-Store-Revision` IS READ STRAIGHT OFF `<scope>/.git/HEAD` AND NOT FROM THE
// NARROWED INDEX, so it is the one answer that could tell a refused scope from an
// absent one. `alpha-notes` is the only scope in this fixture with a HEAD; a principal
// that may read it sees the sha, and one that may not must see exactly what it sees for
// a scope that never existed.
func TestARefusedScopeIsNotDiscriminatedByTheRevisionHeader(t *testing.T) {
	h := newMatrixHarness(t)
	allowed := h.do(t, "GET", "/api/v1/recall/alpha-notes", matrixWideToken, nil, "")
	refused := h.do(t, "GET", "/api/v1/recall/alpha-notes", matrixNarrowToken, nil, "")
	neverExisted := h.do(t, "GET", "/api/v1/recall/delta-notes", matrixNarrowToken, nil, "")

	if allowed.headers.Get("X-Store-Revision") != "a11ba11ba11ba11ba11ba11ba11ba11ba11ba11b" {
		t.Fatalf("precondition: the allowed read must carry the revision, got %q — "+
			"without it the two refusals below agree for the wrong reason",
			allowed.headers.Get("X-Store-Revision"))
	}
	if refused.headers.Get("X-Store-Revision") != neverExisted.headers.Get("X-Store-Revision") {
		t.Fatalf("refused %q vs never-existed %q",
			refused.headers.Get("X-Store-Revision"), neverExisted.headers.Get("X-Store-Revision"))
	}
}

// TestAPrincipalThatMayWriteSomewhereIsNOTForbiddenOnAScopeItMayNot is the pair the
// write refusal's shape rests on.
//
// 🔴 TWO DIFFERENT REFUSALS, AND SWAPPING THEM IS AN ENUMERATION API IN ONE DIRECTION
// AND A BROKEN CONTRACT IN THE OTHER. A principal with no write verb ANYWHERE gets 403:
// the fact is about the credential, it names no scope, and it is identical for a scope
// that exists and one that does not. A principal that may write somewhere and aims at a
// scope it may not gets 404: answering 403 there would tell an authenticated caller
// that a scope it cannot reach EXISTS.
func TestAPrincipalThatMayWriteSomewhereIsNOTForbiddenOnAScopeItMayNot(t *testing.T) {
	h := newMatrixHarness(t)
	body := `{"text":"x","session":"s"}`

	onScopeItMayNot := h.do(t, "POST", "/api/v1/entry/gamma-notes/sprocket-nine/bullets", matrixWideToken, nil, body)
	if onScopeItMayNot.status != 404 {
		t.Fatalf("a principal that may write SOMEWHERE must get the not-found answer for "+
			"a scope it may not write, got %d %q", onScopeItMayNot.status, onScopeItMayNot.body)
	}
	onScopeThatNeverExisted := h.do(t, "POST", "/api/v1/entry/delta-notes/anything/bullets", matrixWideToken, nil, body)
	if onScopeThatNeverExisted.status != 404 || onScopeThatNeverExisted.body != onScopeItMayNot.body {
		t.Fatalf("…and it must be the SAME answer a never-existed scope gets: %d %q vs %d %q",
			onScopeItMayNot.status, onScopeItMayNot.body,
			onScopeThatNeverExisted.status, onScopeThatNeverExisted.body)
	}

	noWriteAnywhere := h.do(t, "POST", "/api/v1/entry/gamma-notes/sprocket-nine/bullets", matrixBareToken, nil, body)
	if noWriteAnywhere.status != 403 {
		t.Fatalf("a principal with no write verb anywhere is refused the ROUTE, got %d %q",
			noWriteAnywhere.status, noWriteAnywhere.body)
	}
	if noWriteAnywhere.status == onScopeItMayNot.status {
		t.Fatal("the two refusals must not be the same answer — one is about the " +
			"credential and one is about a scope, and collapsing them is how an " +
			"authenticated caller learns which scopes exist")
	}
}

// TestTheWriteVerbIsWhatTheWriteRouteASKS is the mutation-visible half of the refusal.
//
// 🔴 IT DRIVES THE PREDICATE, NOT THE TOKEN SHAPE. A principal is constructed whose
// grants carry `read` and not `write` over the scope it names — a shape the token file
// cannot spell at all — and the write route must refuse it exactly as it refuses a bare
// row. That is what distinguishes "the server asks `control` for the write verb" from
// "the server still asks whether the row was bare and the adapter happens to agree".
func TestTheWriteVerbIsWhatTheWriteRouteASKS(t *testing.T) {
	h := newMatrixHarness(t)
	_, auth, err := h.srv.Authority().Authenticate(matrixWideToken)
	if err != nil {
		t.Fatal(err)
	}
	// Precondition, stated so the assertion below cannot pass vacuously: this principal
	// DOES hold the write verb, so a refusal afterwards is about the verb being removed
	// rather than about it never having been there.
	if len(auth.ScopeIDs(control.VerbWrite)) == 0 {
		t.Fatal("precondition: wide-reader must hold the write verb somewhere")
	}
	if len(auth.ScopeIDs(control.VerbRead)) == 0 {
		t.Fatal("precondition: wide-reader must hold the read verb somewhere")
	}

	// 🔴 A `request` CARRYING A READ-ONLY AUTHORITY. `mayWriteAnywhere` is the exact
	// expression the dispatcher branches on, so this asks the thing the route asks.
	readOnly := &request{auth: control.Narrow(auth, nil)}
	if !readOnly.mayWriteAnywhere() {
		t.Fatal("an unnarrowed wide-reader may write")
	}
	// Narrowing to the empty (non-nil) set is "this credential sees nothing" — the
	// opposite of nil. It must take the write verb with it.
	none := &request{auth: control.Narrow(auth, []control.ID{})}
	if none.mayWriteAnywhere() {
		t.Fatal("a credential narrowed to nothing holds the write verb nowhere")
	}
	if len(none.auth.ScopeIDs(control.VerbRead)) != 0 {
		t.Fatal("…and reaches nothing to read either")
	}
}

// TestTheAuthorityReMaterializesOnAReloadAndSaysSoWhenItCannot pins the wiring between
// the token table and the model built from it.
//
// 🔴 A RELOAD THAT PUBLISHED A TABLE WITHOUT RE-MATERIALIZING WOULD BE A REVOCATION
// THAT DID NOT HAPPEN. The operator edits the secret and sends SIGHUP to remove
// somebody; if the authority still holds the old projection, the removed credential
// keeps authenticating and the reload line says LOADED.
func TestTheAuthorityReMaterializesOnAReloadAndSaysSoWhenItCannot(t *testing.T) {
	h := newMatrixHarness(t)
	if got := h.do(t, "GET", "/api/v1/recall/beta-notes", matrixNarrowToken, nil, ""); got.status != 200 ||
		got.headers.Get("X-Store-Status") != "recalled" {
		t.Fatalf("precondition: narrow-reader must be able to read beta-notes, got %d %q",
			got.status, got.headers.Get("X-Store-Status"))
	}
	before := h.srv.Authority().Staleness().Epoch

	// The revocation: the narrow row is removed from the table.
	if err := h.srv.SetTokens([]authz.TokenRecord{
		{Token: matrixWideToken, Identity: "wide-reader", Scopes: []string{"alpha-notes", "beta-notes"}},
	}); err != nil {
		t.Fatalf("the reload must materialize: %v", err)
	}
	if h.srv.Authority().Staleness().Epoch == before {
		t.Fatal("the epoch must move: a projection that did not rebuild is a revocation " +
			"the operator believes landed")
	}
	if got := h.do(t, "GET", "/api/v1/recall/beta-notes", matrixNarrowToken, nil, ""); got.status != 401 {
		t.Fatalf("the revoked credential must stop authenticating, got %d %q", got.status, got.body)
	}
	if got := h.do(t, "GET", "/api/v1/recall/beta-notes", matrixWideToken, nil, ""); got.status != 200 {
		t.Fatalf("…and the surviving credential must not be disturbed, got %d", got.status)
	}
}

// TestAScopeCreatedOutOfBandReachesABareRowAfterARefresh is the DIVERGENCE, measured
// end-to-end through the served surface rather than only at the library.
//
// 🔴 TWO ANSWERS FOR THE SAME REQUEST AGAINST THE SAME DISK, AND BOTH ARE OBSERVED
// HERE. `internal/control/tokenfile` enumerates because `control.Authorization` has no
// unrestricted value; an enumeration is a claim about a moment, so a directory created
// out of band (`server/seed.sh` seeds through `kubectl exec … tar -xf -`) is not
// visible to a bare row until the next materialization. The window is bounded by the
// refresh schedule and reported by `Staleness`, which is the same trade the plan's §D
// already accepted for revocation — but it IS a behaviour change from the sentinel, and
// this is the measurement of it rather than the argument for it.
func TestAScopeCreatedOutOfBandReachesABareRowAfterARefresh(t *testing.T) {
	h := newMatrixHarness(t)
	target := "/api/v1/recall/epsilon-notes"

	if got := h.do(t, "GET", target, matrixBareToken, nil, ""); got.headers.Get("X-Store-Status") != "scope-absent" {
		t.Fatalf("precondition: the scope does not exist yet, got %q", got.headers.Get("X-Store-Status"))
	}

	dir := filepath.Join(h.root, "epsilon-notes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cog-seven.md"),
		[]byte("---\nservice: cog-seven\nscope: epsilon-notes\n---\n\n## What it is\nsynthetic.\n"),
		0o644); err != nil {
		t.Fatal(err)
	}

	// ANSWER ONE — the divergence. The directory is on disk and the bare row cannot see
	// it, because the model materialized before it existed.
	first := h.do(t, "GET", target, matrixBareToken, nil, "")
	if first.headers.Get("X-Store-Status") != "scope-absent" {
		t.Fatalf("answer one: a scope created after materialization is not yet visible "+
			"to a bare row — got %q. If this is now `recalled`, the divergence has "+
			"closed and `tokenfile.Divergence` plus the README row must be retired in "+
			"the same change", first.headers.Get("X-Store-Status"))
	}

	// ANSWER TWO — the remedy, which is the operator's existing muscle memory: a
	// reload, which is what `Refresh` is.
	if err := h.srv.Authority().Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	second := h.do(t, "GET", target, matrixBareToken, nil, "")
	if second.headers.Get("X-Store-Status") != "recalled" {
		t.Fatalf("answer two: after a refresh the same request against the same disk "+
			"must answer, got %q", second.headers.Get("X-Store-Status"))
	}

	// 🔴 AND A MAPPED ROW IS UNAFFECTED IN THE OTHER DIRECTION: a scope in its
	// allowlist is reachable with no directory at all, which is the first-entry create.
	// Measured here because it is the half an adapter that enumerated only directories
	// would silently break, and no test over a populated fixture would see it.
	if !mustAuthorize(t, h, matrixWideToken).VisibleScopes(control.VerbWrite).Allows("alpha-notes") {
		t.Fatal("precondition: a mapped row may write what it names")
	}
}

func mustAuthorize(t *testing.T, h *matrixHarness, token string) control.Authorization {
	t.Helper()
	_, auth, err := h.srv.Authority().Authenticate(token)
	if err != nil {
		t.Fatal(err)
	}
	return auth
}

// TestTheZeroRequestSeesNothingAtBothLevels is the fail-closed default, asserted over
// the WHOLE chain rather than over one field.
//
// ⚠ AN INVARIANT GUARD, LABELLED AS ONE: no bug ever set these wrong. What it pins is
// that the property survived the mechanism change. It used to rest on ONE zero value
// (`store.ScopeSet{}`) with `store.Unrestricted()` reachable from a legacy record; it
// now rests on TWO, because a zero `control.Authorization` has an empty scope map and
// there is no value anywhere in this path that means "everything". The day a route runs
// before authorization, it must see NOTHING.
func TestTheZeroRequestSeesNothingAtBothLevels(t *testing.T) {
	var zeroSet store.ScopeSet
	if zeroSet.Unrestricted || zeroSet.Allows("alpha-notes") {
		t.Fatal("the zero ScopeSet must allow nothing")
	}

	var zeroAuth control.Authorization
	for _, verb := range control.AllVerbs {
		if zeroAuth.Allows("scp_anything", verb) {
			t.Fatalf("the zero Authorization must permit nothing, and it permitted %s", verb)
		}
		set := zeroAuth.VisibleScopes(verb)
		if set.Unrestricted || set.Allows("alpha-notes") {
			t.Fatalf("the zero Authorization's %s set must be empty and NOT unrestricted", verb)
		}
	}
	if len(zeroAuth.ScopeIDs(control.VerbWrite)) != 0 {
		t.Fatal("…and it reaches no scope with the write verb")
	}

	rq := &request{}
	if rq.visible.Unrestricted || rq.visible.Allows("alpha-notes") {
		t.Fatal("a request that has not authenticated must see nothing")
	}
	if rq.writable.Unrestricted || rq.writable.Allows("alpha-notes") {
		t.Fatal("…and must be able to write nothing")
	}
	if rq.mayWriteAnywhere() {
		t.Fatal("…and must not pass the write-route gate")
	}
	if rq.principal.Display != "" || rq.principal.ID != "" {
		t.Fatal("…and must not be anybody")
	}
}

// TestTheHotPathDoesNotContactTheAuthority is the claim `control.Cache` is built on,
// measured HERE because the library's version of it cannot see this server.
//
// 🔴 A COUNTER THAT NEVER MOVES IS INDISTINGUISHABLE FROM A COUNTER WIRED TO NOTHING,
// so both directions are measured: zero calls while serving requests, and a NON-ZERO
// count on an explicit refresh.
func TestTheHotPathDoesNotContactTheAuthority(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "alpha-notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "alpha-notes", "gadget-one.md"),
		[]byte("---\nservice: gadget-one\nscope: alpha-notes\n---\n\n## What it is\nsynthetic.\n"),
		0o644); err != nil {
		t.Fatal(err)
	}

	// 🔴 THE COUNTING SOURCE WRAPS THE **SAME** ADAPTER THE SERVER WIRES FOR ITSELF,
	// built here rather than reached through a getter: a getter for the authority's
	// input would be a second way to read the thing this test exists to prove nobody
	// reads on the hot path.
	records := []authz.TokenRecord{
		{Token: matrixWideToken, Identity: "wide-reader", Scopes: []string{"alpha-notes"}},
	}
	counted := &countingSource{inner: tokenfile.Source{
		StoreRoot: root,
		Records:   func() []authz.TokenRecord { return records },
	}}
	srv, err := New(root, records, []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")},
		netid.NewRateLimiter(1000000, time.Minute, 15*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	srv.authority = control.NewCache(counted, control.CacheOptions{})
	if err := srv.authority.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	srv.Audit = func(string) {}
	srv.Warn = func(string) {}
	h := &harness{srv: srv, root: root}
	h.tsrv = httptest.NewServer(srv)
	t.Cleanup(h.tsrv.Close)

	// POSITIVE CONTROL: the counter CAN move.
	if counted.calls != 1 {
		t.Fatalf("the refresh must reach the authority exactly once, got %d", counted.calls)
	}
	before := counted.calls
	const requests = 5
	for i := 0; i < requests; i++ {
		if got := h.do(t, "GET", "/api/v1/recall/alpha-notes", matrixWideToken, nil, ""); got.status != 200 {
			t.Fatalf("request %d: %d", i, got.status)
		}
	}
	if counted.calls != before {
		t.Fatalf("serving %d requests contacted the authority %d times — reads must "+
			"never touch it, or an outage of whatever is behind the seam stops every read",
			requests, counted.calls-before)
	}
}

// staticSource is a `control.Source` over a Model built by hand.
//
// 🔴 IT EXISTS BECAUSE THE TOKEN FILE CANNOT SPELL THE CASE THE NEXT TEST NEEDS. A
// mapped row confers read AND write over one allowlist, so `rq.visible` and
// `rq.writable` are equal for every principal the adapter can produce — which would
// leave the write path's narrowing a branch NO TEST CAN REACH, and a guard that cannot
// be reached is a guard that is not there. A hand-built model is the cheapest way to
// prove the server asks the predicate with the verb rather than asking it once.
type staticSource struct{ m control.Model }

func (s staticSource) Model(context.Context) (control.Model, error) { return s.m, nil }

// TestTheWritePathNarrowsWithTheWriteVERB is the reachability proof for the two sets.
//
// 🔴 A PRINCIPAL THAT MAY **READ** `beta-notes` AND **WRITE** ONLY `alpha-notes`. It
// holds the write verb somewhere, so it is not the 403 case; it can read the scope it
// is aiming at, so a read-set narrowing would let the write through to the ref
// resolution and answer `ref-unknown`. The contract is that a write to a scope this
// principal may not write answers exactly what an ABSENT scope answers — so the two
// responses are compared to each other, byte for byte, rather than to a status code.
func TestTheWritePathNarrowsWithTheWriteVERB(t *testing.T) {
	h := newMatrixHarness(t)
	const token = "read-beta-write-alpha-read-beta-write-alph"

	at := time.Date(2000, 1, 5, 0, 0, 0, 0, time.UTC)
	usr := control.DerivedID(control.PrefixUser, "owner")
	prj := control.DerivedID(control.PrefixProject, "holder")
	scopes := control.DerivedID(control.PrefixProject, "scopes")
	alpha := control.DerivedID(control.PrefixScope, "alpha-notes")
	beta := control.DerivedID(control.PrefixScope, "beta-notes")
	m, err := control.Replay([]control.Event{
		{Kind: control.EventUserCreated, At: at, UserID: usr, Provider: "test", Subject: "owner"},
		{Kind: control.EventProjectCreated, At: at, ProjectID: scopes, Name: "scopes", UserID: usr},
		{Kind: control.EventProjectCreated, At: at, ProjectID: prj, Name: "split-verbs", UserID: usr},
		{Kind: control.EventScopeCreated, At: at, ScopeID: alpha, DisplayName: "alpha-notes", ProjectID: scopes},
		{Kind: control.EventScopeCreated, At: at, ScopeID: beta, DisplayName: "beta-notes", ProjectID: scopes},
		{Kind: control.EventGranted, At: at, GrantID: control.DerivedID(control.PrefixGrant, "a"),
			SubjectKind: control.KindProject, SubjectID: prj,
			ObjectKind: control.ObjectScope, ObjectID: alpha,
			Verbs: control.NewVerbSet(control.VerbRead, control.VerbWrite)},
		{Kind: control.EventGranted, At: at, GrantID: control.DerivedID(control.PrefixGrant, "b"),
			SubjectKind: control.KindProject, SubjectID: prj,
			ObjectKind: control.ObjectScope, ObjectID: beta,
			Verbs: control.NewVerbSet(control.VerbRead)},
		{Kind: control.EventCredentialIssued, At: at,
			CredentialID: control.DerivedID(control.PrefixCredential, "c"),
			SubjectKind:  control.KindProject, SubjectID: prj,
			TokenHash: control.HashToken(token), Label: "split-verbs"},
	})
	if err != nil {
		t.Fatal(err)
	}
	h.srv.authority = control.NewCache(staticSource{m}, control.CacheOptions{})
	if err := h.srv.authority.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	// PRECONDITIONS, stated so no assertion below can pass vacuously.
	if got := h.do(t, "GET", "/api/v1/recall/beta-notes", token, nil, ""); got.headers.Get("X-Store-Status") != "recalled" {
		t.Fatalf("precondition: this principal READS beta-notes, got %q", got.headers.Get("X-Store-Status"))
	}
	body := `{"text":"x","session":"s"}`
	if got := h.do(t, "POST", "/api/v1/entry/alpha-notes/gadget-one/bullets", token, nil, body); got.status != 200 {
		t.Fatalf("precondition: this principal WRITES alpha-notes, got %d %q", got.status, got.body)
	}

	// 🔴 THE MEASUREMENT. A write to a scope it may read and not write must be
	// indistinguishable from a write to a scope that never existed.
	readOnly := h.do(t, "POST", "/api/v1/entry/beta-notes/widget-three/bullets", token, nil, body)
	neverExisted := h.do(t, "POST", "/api/v1/entry/delta-notes/anything/bullets", token, nil, body)
	if readOnly.status != 404 {
		t.Fatalf("a write to a READ-ONLY scope must be refused as absent, got %d %q — "+
			"if this is 200 the write path narrowed with the READ set and the `write` "+
			"verb decides nothing", readOnly.status, readOnly.body)
	}
	if readOnly.body != neverExisted.body || readOnly.headers.Get("X-Store-Status") != neverExisted.headers.Get("X-Store-Status") {
		t.Fatalf("refused (%d %q %q) must equal absent (%d %q %q)",
			readOnly.status, readOnly.headers.Get("X-Store-Status"), readOnly.body,
			neverExisted.status, neverExisted.headers.Get("X-Store-Status"), neverExisted.body)
	}

	// The same for the CREATE half, which consults the set directly rather than
	// through the loader.
	created := h.do(t, "PUT", "/api/v1/entry/beta-notes/newcomer-nine", token,
		map[string]string{"If-None-Match": "*"},
		"---\nservice: newcomer-nine\nscope: beta-notes\n---\n\n## What it is\nsynthetic.\n")
	if created.status != 404 {
		t.Fatalf("a create in a READ-ONLY scope must be refused as absent, got %d %q",
			created.status, created.body)
	}
}

type countingSource struct {
	inner control.Source
	calls int
}

func (c *countingSource) Model(ctx context.Context) (control.Model, error) {
	c.calls++
	return c.inner.Model(ctx)
}
