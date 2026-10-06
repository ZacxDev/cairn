package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 🔴 THE ARC ROUTES, MEASURED OVER THE SERVED ANSWER — authority, refused-equals-absent and the
// off state — not by reading the handlers. All names and ids are synthetic.

const arcPayload = `{"schema":1,"status":"open","closing_kind":"check","declared_scopes":["beta-notes"],` +
	`"members":[{"session":"s-0042","role":"originated","first_seen":"2000-01-01T00:00:00Z"}]}`

// withJournal configures a journal OUTSIDE the fixture store, as `cmd/cairn-server` would after
// `arcs.ResolveJournalPath`.
func withJournal(t *testing.T, h *harness) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	h.srv.ArcJournal = path
	return path
}

func TestAPodWithNoJournalAnswersTheOffStateAndNever5xx(t *testing.T) {
	h := newHarness(t)
	put := h.do(t, "PUT", "/api/v1/arc/alpha-notes/gadget-rollout", wideToken, nil, arcPayload)
	if put.status != 409 || put.headers.Get("X-Store-Status") != "registrations-unconfigured" {
		t.Fatalf("PUT with no journal: %d %s %q", put.status, put.headers.Get("X-Store-Status"), put.body)
	}
	for _, target := range []string{"/api/v1/arcs/alpha-notes", "/api/v1/arc/alpha-notes/gadget-rollout"} {
		got := h.do(t, "GET", target, wideToken, nil, "")
		if got.status != 200 || got.headers.Get("X-Store-Status") != "registrations-unconfigured" ||
			got.headers.Get("X-Store-Exit") != "0" {
			t.Fatalf("GET %s with no journal: %d %s exit=%s", target, got.status,
				got.headers.Get("X-Store-Status"), got.headers.Get("X-Store-Exit"))
		}
	}
}

// TestRegisteringAnArcNeedsTheWriteVerbOnEveryScopeItNames is the write authority, as a
// relationship. The bare row holds write NOWHERE: the credential-level 403, nothing written. The
// narrow reader may write beta but NOT alpha: declaring alpha answers the uniform write 404,
// byte-identical to declaring a scope that never existed, and nothing is written. The POSITIVE
// CONTROL is the same narrow credential declaring only what it may write, which must register —
// without it a handler refusing every PUT would pass the two refusals.
func TestRegisteringAnArcNeedsTheWriteVerbOnEveryScopeItNames(t *testing.T) {
	h := newHarness(t)
	journal := withJournal(t, h)

	legacy := h.do(t, "PUT", "/api/v1/arc/beta-notes/widget-fix", legacyToken, nil, `{"schema":1}`)
	if legacy.status != 403 || legacy.headers.Get("X-Store-Status") != "legacy-cannot-write" {
		t.Fatalf("the bare row must get the credential-level 403: %d %s", legacy.status, legacy.headers.Get("X-Store-Status"))
	}
	refused := h.do(t, "PUT", "/api/v1/arc/beta-notes/widget-fix", narrowToken, nil,
		`{"schema":1,"declared_scopes":["alpha-notes"]}`)
	absent := h.do(t, "PUT", "/api/v1/arc/beta-notes/widget-fix", narrowToken, nil,
		`{"schema":1,"declared_scopes":["ghost-void"]}`)
	homeRefused := h.do(t, "PUT", "/api/v1/arc/alpha-notes/widget-fix", narrowToken, nil, `{"schema":1}`)
	for name, got := range map[string]reply{"refused": refused, "absent": absent, "home refused": homeRefused} {
		if got.status != 404 || got.body != "not found\n" || got.headers.Get("X-Store-Status") != "not-found" {
			t.Fatalf("%s: a scope the caller may not write must answer the uniform write 404, got %d %q", name, got.status, got.body)
		}
	}
	if _, err := os.Stat(journal); !os.IsNotExist(err) {
		t.Fatalf("a refused registration wrote the journal (stat err %v)", err)
	}

	ok := h.do(t, "PUT", "/api/v1/arc/beta-notes/widget-fix", narrowToken, nil, `{"schema":1}`)
	if ok.status != 200 || ok.body != "arc-registered: beta-notes/widget-fix · members=0 · unjoinable=0\n" {
		t.Fatalf("control: the narrow credential may register in the scope it writes: %d %q", ok.status, ok.body)
	}
}

func TestARegistrationRoundTripsAndARetryIsUnchanged(t *testing.T) {
	h := newHarness(t)
	withJournal(t, h)
	first := h.do(t, "PUT", "/api/v1/arc/alpha-notes/gadget-rollout", wideToken, nil, arcPayload)
	if first.status != 200 || first.headers.Get("X-Store-Status") != "arc-registered" ||
		first.body != "arc-registered: alpha-notes/gadget-rollout · members=1 · unjoinable=0\n" {
		t.Fatalf("first registration: %d %s %q", first.status, first.headers.Get("X-Store-Status"), first.body)
	}
	again := h.do(t, "PUT", "/api/v1/arc/alpha-notes/gadget-rollout", wideToken, nil, arcPayload)
	if again.status != 200 || again.headers.Get("X-Store-Status") != "arc-unchanged" {
		t.Fatalf("an identical retry: %d %s", again.status, again.headers.Get("X-Store-Status"))
	}
	shown := h.do(t, "GET", "/api/v1/arc/alpha-notes/gadget-rollout", wideToken, nil, "")
	// `registered_by` is the AUTHENTICATED identity and the pod's clock — the body has no way to
	// set either (an unknown field is refused).
	if shown.status != 200 || shown.headers.Get("X-Store-Status") != "arc-found" ||
		!strings.Contains(shown.body, "\nregistered: 2000-01-05T00:00:00Z by wide-reader · reported by the tool at an unstated time\n") ||
		!strings.Contains(shown.body, "\n- s-0042 · originated · first seen 2000-01-01T00:00:00Z · wrote in: none readable to you") {
		t.Fatalf("arc-show:\n%s", shown.body)
	}
	bad := h.do(t, "PUT", "/api/v1/arc/alpha-notes/gadget-rollout", wideToken, nil,
		`{"schema":1,"registered_by":"someone-else"}`)
	if bad.status != 400 || !strings.Contains(bad.body, "unknown field") {
		t.Fatalf("a payload naming registered_by must be refused: %d %q", bad.status, bad.body)
	}
}

// TestTheArcRoutesRefuseExactlyLikeAbsence: an arc homed in a scope the caller cannot read answers
// what an absent key answers, header for header (`X-Store-Revision` included — `alpha-notes`
// carries a `.git/HEAD`, so it is the header that COULD tell them apart), and an arc homed there
// is not LISTED under a scope the caller can read. Positive control: the wide reader sees both.
func TestTheArcRoutesRefuseExactlyLikeAbsence(t *testing.T) {
	h := newHarness(t)
	withJournal(t, h)
	if got := h.do(t, "PUT", "/api/v1/arc/alpha-notes/gadget-rollout", wideToken, nil, arcPayload); got.status != 200 {
		t.Fatalf("seed: %d %q", got.status, got.body)
	}
	allowed := h.do(t, "GET", "/api/v1/arc/alpha-notes/gadget-rollout", wideToken, nil, "")
	refused := h.do(t, "GET", "/api/v1/arc/alpha-notes/gadget-rollout", narrowToken, nil, "")
	absent := h.do(t, "GET", "/api/v1/arc/ghost-void/gadget-rollout", narrowToken, nil, "")
	if allowed.headers.Get("X-Store-Status") != "arc-found" || refused.headers.Get("X-Store-Status") != "arc-unregistered" {
		t.Fatalf("allowed %s, refused %s", allowed.headers.Get("X-Store-Status"), refused.headers.Get("X-Store-Status"))
	}
	if refused.body != absent.body {
		t.Fatalf("refused differs from absent:\n%s\n---\n%s", refused.body, absent.body)
	}
	for _, name := range []string{"X-Store-Status", "X-Store-Exit", "X-Store-Revision", "X-Store-Snapshot", "Content-Type"} {
		if refused.headers.Get(name) != absent.headers.Get(name) {
			t.Fatalf("%s differs: refused %q, absent %q", name, refused.headers.Get(name), absent.headers.Get(name))
		}
	}
	if allowed.headers.Get("X-Store-Revision") == refused.headers.Get("X-Store-Revision") {
		t.Fatal("the revision control failed: the allowed read must expose the real revision")
	}

	wideList := h.do(t, "GET", "/api/v1/arcs/beta-notes", wideToken, nil, "")
	narrowList := h.do(t, "GET", "/api/v1/arcs/beta-notes", narrowToken, nil, "")
	if !strings.Contains(wideList.body, "\n- alpha-notes/gadget-rollout · declared · status open · closing check · 1 member") {
		t.Fatalf("control: the wide reader sees the arc that declares beta:\n%s", wideList.body)
	}
	if narrowList.headers.Get("X-Store-Status") != "no-arc-registered" ||
		strings.Contains(narrowList.body, "gadget-rollout") || strings.Contains(narrowList.body, "alpha-notes") {
		t.Fatalf("an arc homed in a scope the caller cannot read leaked into its listing:\n%s", narrowList.body)
	}
}

func TestAnUnreadableJournalIsCouldNotLookNeverNothingRegistered(t *testing.T) {
	h := newHarness(t)
	h.srv.ArcJournal = t.TempDir() // a DIRECTORY where the file should be
	got := h.do(t, "GET", "/api/v1/arcs/alpha-notes", wideToken, nil, "")
	if got.status != 503 || got.headers.Get("X-Store-Status") != "store-unreachable" ||
		!strings.Contains(got.body, "NOT 'no arc registered'") {
		t.Fatalf("an unreadable journal: %d %s %q", got.status, got.headers.Get("X-Store-Status"), got.body)
	}
}

// TestTheArcsCheckIsAModeOfTheArcsHeadAuthorisedLikeIt is S5's served contract: `?check=1` on
// `arcs/<scope>` answers the orphan check on doctor's codes in `X-Store-Exit` (0 here, 10 with no
// journal), `?check=0` is still the listing (the `all_scopes` truth table), a journal that cannot be
// read is the 503 every arc route gives, and — refused == absent — a caller who cannot read the
// path scope gets, byte for byte, what a scope that never existed gets. The POSITIVE CONTROL is the
// wide caller, for whom the same request checks the arc.
func TestTheArcsCheckIsAModeOfTheArcsHeadAuthorisedLikeIt(t *testing.T) {
	h := newHarness(t)
	if got := h.do(t, "GET", "/api/v1/arcs/alpha-notes?check=1", wideToken, nil, ""); got.status != 200 ||
		got.headers.Get("X-Store-Status") != "registrations-unconfigured" || got.headers.Get("X-Store-Exit") != "10" {
		t.Fatalf("no journal: the check could not look — 200 with exit 10, got %d %s exit=%s",
			got.status, got.headers.Get("X-Store-Status"), got.headers.Get("X-Store-Exit"))
	}
	journal := withJournal(t, h)
	if put := h.do(t, "PUT", "/api/v1/arc/alpha-notes/gadget-rollout", wideToken, nil, arcPayload); put.status != 200 {
		t.Fatalf("register: %d %q", put.status, put.body)
	}
	wide := h.do(t, "GET", "/api/v1/arcs/alpha-notes?check=1", wideToken, nil, "")
	if wide.status != 200 || wide.headers.Get("X-Store-Status") != "arcs-check-clean" ||
		wide.headers.Get("X-Store-Exit") != "0" || !strings.Contains(wide.body, "\n  arcs checked: 1 (open 1 · closed 0 · unknown 0)\n") {
		t.Fatalf("POSITIVE CONTROL: the wide caller's check sees the arc: %d %s\n%s", wide.status, wide.headers.Get("X-Store-Status"), wide.body)
	}
	if listing := h.do(t, "GET", "/api/v1/arcs/alpha-notes?check=0", wideToken, nil, ""); listing.headers.Get("X-Store-Status") != "arcs-listed" {
		t.Fatalf("check=0 is the listing: %s", listing.headers.Get("X-Store-Status"))
	}
	refused := h.do(t, "GET", "/api/v1/arcs/alpha-notes?check=1", narrowToken, nil, "")
	absent := h.do(t, "GET", "/api/v1/arcs/ghost-void?check=1", narrowToken, nil, "")
	if refused.status != 200 || refused.headers.Get("X-Store-Exit") != "0" ||
		strings.Contains(refused.body, "gadget-rollout") ||
		strings.ReplaceAll(refused.body, "alpha-notes", "ghost-void") != absent.body ||
		refused.headers.Get("X-Store-Revision") != absent.headers.Get("X-Store-Revision") {
		t.Fatalf("refused must equal absent:\n%s\n--- absent\n%s", refused.body, absent.body)
	}
	if err := os.Remove(journal); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(journal, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := h.do(t, "GET", "/api/v1/arcs/alpha-notes?check=1", wideToken, nil, ""); got.status != 503 {
		t.Fatalf("an unreadable journal is could-not-look, the 503: %d %q", got.status, got.body)
	}
}
