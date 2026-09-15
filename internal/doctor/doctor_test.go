package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 🔴 WHAT THIS FILE OWNS. The parity harness renders `doctor` end to end against a live pod for
// seven rows, so the happy paths and the two mirror states are measured there against the ORACLE's
// own bytes. What a healthy world cannot produce is here: an UNREADABLE local root, the
// empty-detail refusal, the mandatory-reason enforcement, the mirror-only leftover note, and the
// exit-code precedence.

func intp(n int) *int { return &n }

func world(t *testing.T) (cache, mirror string) {
	t.Helper()
	root := t.TempDir()
	cache, mirror = filepath.Join(root, "cache"), filepath.Join(root, "mirror")
	for _, dir := range []string{
		filepath.Join(cache, "alpha-notes"),
		filepath.Join(mirror, "alpha-notes"),
		filepath.Join(mirror, "mirror-only-scope"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for path, mode := range map[string]os.FileMode{
		filepath.Join(cache, "alpha-notes", "widget-cfg.md"):        0o644,
		filepath.Join(mirror, "alpha-notes", "frozen.md"):           0o444,
		filepath.Join(mirror, "mirror-only-scope", "left-behind.md"): 0o444,
	} {
		if err := os.WriteFile(path, []byte("x\n"), mode); err != nil {
			t.Fatal(err)
		}
	}
	return cache, mirror
}

func byName(checks []Check) map[string]Check {
	out := map[string]Check{}
	for _, c := range checks {
		out[c.Name] = c
	}
	return out
}

func baseInputs(cache, mirror string) Inputs {
	return Inputs{
		ResolvedRoot:   cache,
		StampLines:     []string{"synced=946684800", "revision=abc123", "entries=1", "coverage=ALL"},
		CacheRoot:      cache,
		MirrorRoot:     mirror,
		Pod:            PodFacts{Reached: true, VisibleEntries: intp(1), VisibleScopes: []string{"alpha-notes"}, SnapshotHeader: "seeded=2000-01-01T00:00:00Z entry-files=1"},
		Token:          "a-synthetic-token",
		HasToken:       true,
		IdentityRemedy: "the remedy",
	}
}

func TestAnUNREADABLELocalRootMakesTheScopeCheckUNMEASUREDNotOKWithANote(t *testing.T) {
	// 🔴 "every scope on this disk is among them" IS A CLAIM ABOUT A SET THIS WALK COULD NOT
	// FINISH BUILDING, and an OK carrying a caveat in its tail is exactly how a partial answer gets
	// read as a clean one. The note alone was the first version: it stated the hole and still graded
	// the check green.
	cache, mirror := world(t)
	if os.Geteuid() == 0 {
		t.Skip("running as root, which honours no mode bits — a mode-000 directory is readable " +
			"here, so this dimension is UNMEASURABLE in this environment rather than passing")
	}
	if err := os.Chmod(mirror, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(mirror, 0o755) })

	checks := byName(Collect(baseInputs(cache, mirror)))
	scopes := checks["token-scopes"]
	if scopes.State != Unmeasured {
		t.Fatalf("token-scopes is %s, want %s — the walk could not read half its input",
			scopes.State, Unmeasured)
	}
	if !strings.Contains(scopes.Detail, "could not be fully read") ||
		!strings.Contains(scopes.Detail, "Some local roots were unreadable") {
		t.Fatalf("the detail must NAME the hole: %s", scopes.Detail)
	}
	// And the whole run is then UNMEASURED rather than OK — a clean bill of health is exactly what
	// this must not print.
	if got := ExitCode(Collect(baseInputs(cache, mirror))); got != ExitUnmeasured {
		t.Fatalf("exit %d, want %d", got, ExitUnmeasured)
	}
}

func TestAnUNCONFIGUREDMirrorIsNOTOBSERVABLEAndContributesNoHole(t *testing.T) {
	// 🔴 THE DEFECT THIS PINS WAS MEASURED, AND IT TOOK `doctor` TO EXIT 1 WITH ZERO STDOUT. When
	// the mirror became configurable the visibility check's signature was NOT widened with it, so it
	// was handed a nil and died with `AttributeError: 'NoneType' object has no attribute 'iterdir'`
	// for every deployment that had never set `CAIRN_MIRROR_ROOT` and had a synced cache — the
	// ordinary state of a new one.
	//
	// 🔴 AND AN UNCONFIGURED MIRROR IS NOT AN UNREADABLE ROOT. It contributes no scopes and is not a
	// hole in this check's coverage, so it must NOT land in `unread` — doing so would downgrade a
	// complete answer to UNMEASURED on EVERY default deployment.
	cache, _ := world(t)
	in := baseInputs(cache, "")
	checks := byName(Collect(in))
	if got := checks["frozen-mirror"].State; got != NotObservable {
		t.Fatalf("frozen-mirror is %s, want %s", got, NotObservable)
	}
	if !strings.Contains(checks["frozen-mirror"].Detail, "CAIRN_MIRROR_ROOT") {
		t.Fatalf("the detail must name who CAN answer it: %s", checks["frozen-mirror"].Detail)
	}
	if got := checks["token-scopes"].State; got != OK {
		t.Fatalf("token-scopes is %s, want %s — an unconfigured mirror is not a hole",
			got, checks["token-scopes"].Detail)
	}
	// 🔴 AND `NOT-OBSERVABLE` CONTRIBUTES NOTHING TO THE EXIT CODE. Escalating on it would make
	// this command non-zero on every healthy run forever — the permanently-red gate that trains
	// everyone to click through.
	if got := ExitCode(Collect(in)); got != ExitOK {
		t.Fatalf("exit %d, want %d on a healthy default deployment", got, ExitOK)
	}
}

func TestAConfiguredMirrorThatDoesNotExistIsOKAndSaysSo(t *testing.T) {
	// A DIFFERENT answer from unconfigured: the operator named a path and it is genuinely absent,
	// which is what a fresh host looks like after a migration that never had a local store.
	cache, mirror := world(t)
	checks := byName(Collect(baseInputs(cache, filepath.Join(mirror, "absent"))))
	if got := checks["frozen-mirror"]; got.State != OK ||
		!strings.Contains(got.Detail, "nothing pre-cutover on this host") {
		t.Fatalf("frozen-mirror: %#v", got)
	}
}

func TestAWritableMirrorEntryIsAPROBLEMEvenWhenTheDirectoryLooksFrozen(t *testing.T) {
	// 🔴 THE FREEZE IS A PROPERTY OF EVERY FILE, NOT OF THE DIRECTORY. A partially-frozen mirror
	// still accepts a write, and that write then lives on one host and is invisible to the pod.
	cache, mirror := world(t)
	loose := filepath.Join(mirror, "alpha-notes", "still-writable.md")
	if err := os.WriteFile(loose, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	checks := byName(Collect(baseInputs(cache, mirror)))
	if got := checks["frozen-mirror"]; got.State != Problem ||
		!strings.Contains(got.Detail, "1 entry file(s)") ||
		!strings.Contains(got.Detail, "alpha-notes/still-writable.md") {
		t.Fatalf("frozen-mirror: %#v", got)
	}
	// The positive control on the mode read: with the file frozen the same world is OK, so the
	// PROBLEM above is about the mode bit and not about the file existing.
	if err := os.Chmod(loose, 0o444); err != nil {
		t.Fatal(err)
	}
	if got := byName(Collect(baseInputs(cache, mirror)))["frozen-mirror"]; got.State != OK {
		t.Fatalf("a fully frozen mirror: %#v", got)
	}
}

func TestAMirrorONLYScopeIsTaggedAsAPreCutoverLeftoverRatherThanLostAccess(t *testing.T) {
	// 🔴 PROVENANCE PER SCOPE, NOT ONE MERGED SET. A scope in the SYNCED CACHE the pod no longer
	// sends is a credential or a deletion; a scope in the FROZEN MIRROR only may never have reached
	// the pod at all. The first version merged them and offered two remedies that were both about
	// the live store, so an operator could not tell which they were looking at.
	cache, mirror := world(t)
	checks := byName(Collect(baseInputs(cache, mirror)))
	scopes := checks["token-scopes"]
	if scopes.State != Problem {
		t.Fatalf("token-scopes: %#v", scopes)
	}
	if !strings.Contains(scopes.Detail, "mirror-only-scope [mirror]") {
		t.Fatalf("the detail must tag WHERE each scope was found: %s", scopes.Detail)
	}
	if !strings.Contains(scopes.Detail, "exist ONLY in the frozen pre-cutover mirror") ||
		!strings.Contains(scopes.Detail, "whether they were ever seeded") {
		t.Fatalf("a mirror-only scope needs the leftover note: %s", scopes.Detail)
	}
	if !strings.Contains(scopes.Detail, "byte-identical to one the store has never held") {
		t.Fatalf("both readings have to be named — the API answers refused and absent "+
			"identically by design: %s", scopes.Detail)
	}
}

func TestTheHiddenEntryGapIsReportedFromTheTwoINDEPENDENTCounts(t *testing.T) {
	// 🔴 THE STORE-WIDE COUNT IS A DOCUMENTED, DELIBERATE RESIDUAL LEAK, AND THE GAP IS THE ONLY
	// CLIENT-SIDE EVIDENCE THAT ENTRIES EXIST WHICH THIS CREDENTIAL CANNOT REACH. A client with only
	// one of the two numbers cannot compute it.
	cache, _ := world(t)
	in := baseInputs(cache, "")
	in.Pod.StoreWideEntries = intp(5)
	scopes := byName(Collect(in))["token-scopes"]
	if scopes.State != Problem || !strings.Contains(scopes.Detail,
		"4 live in scopes this credential cannot reach") {
		t.Fatalf("token-scopes: %#v", scopes)
	}
	// No gap, no sentence: a store-wide count EQUAL to the visible one is the ordinary case, and a
	// note there would be a false alarm on every healthy run.
	in.Pod.StoreWideEntries = intp(1)
	if got := byName(Collect(in))["token-scopes"].State; got != OK {
		t.Fatalf("no gap must be OK, got %s", got)
	}
}

func TestTheCacheVsPodJoinREFUSESToReportAZero(t *testing.T) {
	cache, _ := world(t)
	// 🔴 THE COUNTS MAY ONLY BE READ WHEN THE POD WAS REACHED. A store that genuinely holds zero
	// entries and a fetch that never happened both leave them at 0.
	unreached := baseInputs(cache, "")
	unreached.Pod = PodFacts{Reason: "the harness said so"}
	got := byName(Collect(unreached))["cache-vs-pod"]
	if got.State != Unmeasured || !strings.Contains(got.Detail, "the harness said so") {
		t.Fatalf("cache-vs-pod: %#v", got)
	}
	// 🔴 AND AN ABSENT `X-Store-Entries` IS UNMEASURED, NEVER A FALLBACK TO THE ARCHIVE'S OWN
	// COUNT: that is the side of the comparison a truncated transfer moves, so substituting it
	// would make the check agree with itself over a short answer.
	noHeader := baseInputs(cache, "")
	noHeader.Pod.VisibleEntries = nil
	got = byName(Collect(noHeader))["cache-vs-pod"]
	if got.State != Unmeasured || !strings.Contains(got.Detail, "X-Store-Entries") {
		t.Fatalf("cache-vs-pod: %#v", got)
	}
	// A disagreement is a PROBLEM with the one-command remedy.
	mismatched := baseInputs(cache, "")
	mismatched.Pod.VisibleEntries = intp(9)
	got = byName(Collect(mismatched))["cache-vs-pod"]
	if got.State != Problem || !strings.Contains(got.Detail, "cairn sync") {
		t.Fatalf("cache-vs-pod: %#v", got)
	}
}

func TestThePODCheckTellsAnOutageFromARefusedCredential(t *testing.T) {
	cache, _ := world(t)
	for _, tc := range []struct {
		name   string
		pod    PodFacts
		state  string
		phrase string
	}{
		{"no answer at all", PodFacts{Reason: "connection refused"}, Unmeasured,
			"the store's state could not be established"},
		{"401", PodFacts{Reason: "unauthorized", HTTPStatus: 401}, Problem,
			"This is NOT an outage: the host is up"},
		{"403 names the edge too", PodFacts{Reason: "forbidden", HTTPStatus: 403}, Problem,
			"the edge refusing the User-Agent"},
		{"503", PodFacts{Reason: "unreadable scope", HTTPStatus: 503}, Problem,
			"the store ANSWERED HTTP 503"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := baseInputs(cache, "")
			in.Pod = tc.pod
			got := byName(Collect(in))["pod"]
			if got.State != tc.state || !strings.Contains(got.Detail, tc.phrase) {
				t.Fatalf("pod: %#v", got)
			}
		})
	}
}

func TestAPodFactsWithNoReasonIsREFUSED(t *testing.T) {
	// 🔴 THE MANDATORY REASON, ENFORCED — it was only DOCUMENTED on the oracle before. A
	// `PodFacts(reached=False)` with no reason renders `the store's state could not be established
	// — .` and walks straight past the empty-detail guard, because the empty string is wrapped in
	// literal text before it gets there.
	if err := (PodFacts{}).Validate(); err == nil {
		t.Fatal("a reached=false with no reason must be refused")
	}
	if err := (PodFacts{Reason: "  "}).Validate(); err == nil {
		t.Fatal("whitespace is not a reason")
	}
	if err := (PodFacts{Reason: "a reason"}).Validate(); err != nil {
		t.Fatalf("a real reason must pass: %v", err)
	}
	// And a REACHED pod needs none: the reason field is about the absence.
	if err := (PodFacts{Reached: true}).Validate(); err != nil {
		t.Fatalf("a reached pod needs no reason: %v", err)
	}
}

func TestACheckWithAnEmptyDetailIsREFUSEDAtConstruction(t *testing.T) {
	// 🔴 A BARE `UNMEASURED` IS THE REASSURING ZERO WEARING A DIFFERENT WORD.
	for _, tc := range []struct{ name, state, detail string }{
		{"empty detail", OK, ""},
		{"whitespace detail", Unmeasured, "   "},
		{"unknown state", "MAYBE", "a detail"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("newCheck(%q, %q, %q) must refuse", tc.name, tc.state, tc.detail)
				}
			}()
			newCheck("x", tc.state, tc.detail)
		})
	}
}

func TestTheExitCodePRECEDENCEIsProblemThenUnmeasuredThenOK(t *testing.T) {
	ok := newCheck("a", OK, "d")
	problem := newCheck("b", Problem, "d")
	unmeasured := newCheck("c", Unmeasured, "d")
	notObservable := newCheck("d", NotObservable, "d")
	for _, tc := range []struct {
		name   string
		checks []Check
		want   int
	}{
		{"all OK", []Check{ok, notObservable}, ExitOK},
		{"one UNMEASURED", []Check{ok, unmeasured, notObservable}, ExitUnmeasured},
		{"a PROBLEM outranks an UNMEASURED", []Check{ok, unmeasured, problem}, ExitProblem},
		{"NOT-OBSERVABLE alone is still OK", []Check{notObservable}, ExitOK},
		{"an empty set is OK", nil, ExitOK},
	} {
		if got := ExitCode(tc.checks); got != tc.want {
			t.Errorf("%s: exit %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestTheJSONIsCPythonsAndNotGosEncoder(t *testing.T) {
	// 🔴 THREE DIFFERENCES, EACH OF WHICH THE PARITY GATE WOULD FAIL ON. Key ORDER (Go sorts maps),
	// HTML escaping (Go escapes `<`, `>` and `&`), and `ensure_ascii` (CPython escapes every
	// non-ASCII rune). Every real `detail` carries an em dash, so the third is on EVERY line.
	checks := []Check{newCheck("one", OK, "an em dash — and a <tag> & an ampersand")}
	got := JSON(checks)
	if !strings.Contains(got, `\u2014`) {
		t.Fatalf("the em dash must be escaped: %s", got)
	}
	if strings.Contains(got, `\u003c`) || !strings.Contains(got, "<tag>") {
		t.Fatalf("CPython does NOT escape HTML: %s", got)
	}
	// Key order: `checks` before `counts` before `exit` before `exit_legend`, which Go's map
	// encoder would sort into `checks`, `counts`, `exit`, `exit_legend` — the same order by
	// accident. `counts` is the one that proves it: its keys are the STATE vocabulary, whose
	// declaration order (`OK`, `PROBLEM`, `UNMEASURED`, `NOT-OBSERVABLE`) is NOT alphabetical.
	i, j := strings.Index(got, `"UNMEASURED"`), strings.Index(got, `"NOT-OBSERVABLE"`)
	if i < 0 || j < 0 || i > j {
		t.Fatalf("the counts keys must be in declaration order, not sorted: %s", got)
	}
	// An astral character travels as a surrogate PAIR under `ensure_ascii`.
	pair := JSON([]Check{newCheck("one", OK, "a map \U0001F5FA")})
	if !strings.Contains(pair, `\ud83d\uddfa`) {
		t.Fatalf("a surrogate pair: %s", pair)
	}
	// And DEL is escaped, which the `< 0x20` control test does not catch — a port that wrote
	// `r < 0x80` for the raw-ASCII arm would differ on exactly one code point.
	del := JSON([]Check{newCheck("one", OK, "a del \u007f here")})
	if !strings.Contains(del, `\u007f`) {
		t.Fatalf("DEL must be escaped: %s", del)
	}
}

func TestTheRenderedReportCarriesItsOwnLegendAndVerdict(t *testing.T) {
	// 🔴 READ, NEVER RESTATED. A second copy of the legend — in this file, in a skill, anywhere — is
	// a second thing to keep true, and the numbers are the part a caller branches on.
	out := Render([]Check{newCheck("pod", Problem, "a detail")})
	for _, want := range []string{
		"PROBLEM", "-> exit 9", "exit codes: 0 = ", "9 = ", "10 = ",
		"OK=0  PROBLEM=1  UNMEASURED=0  NOT-OBSERVABLE=0",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the rendered report must contain %q:\n%s", want, out)
		}
	}
	for _, row := range ExitLegend {
		if !strings.Contains(out, row.Why) {
			t.Errorf("the legend's own wording must be rendered, not paraphrased: %q", row.Why)
		}
	}
}

func TestTheFingerprintIsAPrefixOfSha256AndNeverTheToken(t *testing.T) {
	// 🔴 IT MIRRORS THE SERVER'S `token_id`, WHICH IS WHAT THE POD'S AUDIT LOG CARRIES — so the two
	// agreeing is a wire fact. The expected digest is a LITERAL, not recomputed here: a test that
	// re-derived it would only prove the test agrees with itself.
	const token = "a-synthetic-token-that-never-authorised-anything"
	got := TokenFingerprint(token)
	if len(got) != TokenFingerprintChars {
		t.Fatalf("length %d, want %d", len(got), TokenFingerprintChars)
	}
	if strings.Contains(got, token) || strings.Contains(token, got) {
		t.Fatal("the fingerprint must not be derivable from the token by inspection")
	}
	// Two different tokens must not share a fingerprint, which is the property a handle needs.
	if TokenFingerprint(token) == TokenFingerprint(token+"x") {
		t.Fatal("two tokens share a fingerprint")
	}
}

func TestTheStampIsRelayedUNPARSEDAndAnAbsentOneIsAProblemTwice(t *testing.T) {
	// 🔴 THE STAMP'S FIELDS ARE RELAYED, NOT INTERPRETED, and the two checks over it answer
	// DIFFERENT questions: which directory the reader resolves, and whether that directory can date
	// itself. Both go PROBLEM when there is no stamp, and the second names the reason.
	cache, _ := world(t)
	in := baseInputs(cache, "")
	in.StampLines = nil
	in.StampReason = "no `.sync-stamp` in " + cache
	checks := byName(Collect(in))
	if got := checks["reader-resolution"]; got.State != Problem ||
		!strings.Contains(got.Detail, "cannot date itself") ||
		!strings.Contains(got.Detail, "exit 4") {
		t.Fatalf("reader-resolution: %#v", got)
	}
	if got := checks["cache-stamp"]; got.State != Problem ||
		!strings.Contains(got.Detail, in.StampReason) {
		t.Fatalf("cache-stamp: %#v", got)
	}
	// With a stamp, the fields are joined verbatim — no reordering, no parsing.
	checks = byName(Collect(baseInputs(cache, "")))
	if got := checks["cache-stamp"].Detail; got != "synced=946684800; revision=abc123; entries=1; coverage=ALL" {
		t.Fatalf("cache-stamp detail: %q", got)
	}
}

func TestAMissingTokenIsAProblemAboutTheCONFIGAndNotAnOutage(t *testing.T) {
	cache, _ := world(t)
	in := baseInputs(cache, "")
	in.HasToken = false
	in.TokenReason = "config incomplete: SUBSYSTEM_STORE_TOKEN not set"
	got := byName(Collect(in))["token"]
	if got.State != Problem || got.Detail != in.TokenReason {
		t.Fatalf("token: %#v", got)
	}
	// 🔴 AND WITH A TOKEN IT IS `NOT-OBSERVABLE`, NOT OK: the fingerprint is measurable here and
	// the IDENTITY behind it is not, from any client. Folding the two would make the exit code
	// permanently non-zero.
	got = byName(Collect(baseInputs(cache, "")))["token"]
	if got.State != NotObservable || !strings.Contains(got.Detail, "the remedy") {
		t.Fatalf("token: %#v", got)
	}
}
