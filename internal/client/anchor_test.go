package client

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 🔴 WHAT THIS FILE GUARDS, IN ONE SENTENCE: that no site in this package puts an ANCHOR — a
// path somebody else chose — inside a `filepath.Glob` pattern, where `filepath.Match` parses it
// instead of matching it.
//
// The three regression rows below were each RED at `e293c6e` and each failed the same way: an
// unterminated `[` in a directory name took `filepath.Match` to `ErrBadPattern`, the error was
// discarded at the call site, and the empty match set was reported as an ordinary answer. The
// oracle has never had any of them, so each was also a silent cross-client divergence — which
// is why `tests/parity/harness.py` now names its world root with a `[` in it, and why three of
// its rows go red at that commit.
//
// ⚠ THE INVARIANT ROWS BESIDE THEM ARE LABELLED AS SUCH AND ARE NOT REGRESSION COVERAGE. They
// pass at `e293c6e` too. They exist because each fix replaced a pattern match with an
// enumeration, and "same results for ordinary paths" is the half a metacharacter fixture
// structurally cannot assert.
//
// ⚠ AND THEY ARE AIMED AT THE THREE CALLERS, NOT AT A SHARED HELPER. A round-1 audit removed a
// general `anchoredGlob` walker whose only production caller was `Focus` and whose only
// multi-component-wildcard case had none at all; the two rows that had covered it moved onto
// `Focus`, where the property is on a path the program takes. `anchor.go` carries the argument.

// metacharacterHome is a `$HOME` whose own name carries an unterminated `[`.
//
// 🔴 `t.TempDir()` CANNOT BE MADE TO CARRY ONE, which is why every row here builds a
// subdirectory of it rather than asking for a temp dir with a funny name. Making `$HOME` the
// metacharacter-bearing directory is also what puts the DEFAULT cache root
// (`$HOME/.cache/subsystem-store`) under test with no flag involved.
func metacharacterHome(t *testing.T) string {
	t.Helper()
	home := filepath.Join(t.TempDir(), "wid[get")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	return home
}

// writeHandoff writes one handoff doc and stamps its mtime, so the mtime ordering a row asserts
// is the row's own rather than the filesystem's idea of "now".
func writeHandoff(t *testing.T, repo, name, body string, mtime time.Time) string {
	t.Helper()
	docs := filepath.Join(repo, "claudedocs")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(docs, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestFocusResolvesUnderARepoPathCarryingAGlobMetacharacter is the severe member of the class.
//
// 🔴 A FALSE CLAIM OF ABSENCE ON THE DEFAULT READ PATH. `Report` calls `Focus(opts.Repo)`, so
// `cairn recall --repo <path>` with no `--scope` renders the featured entry's basis from
// whatever this returns. RED at `e293c6e` with this fixture: `filepath.Glob` was handed
// `<repo>/claudedocs/handoff-*.md` with a `[` in `<repo>`, returned `ErrBadPattern` with a nil
// slice, the error was discarded, and the window came back EMPTY — which the renderer prints as
// `most-recent fallback … (no handoff doc to read a path window from)` while the doc is sitting
// in `claudedocs/`. Not a refusal: a confident, wrong sentence about the repo.
//
// ⚠ The oracle's `focus_window` is unaffected — `Path(repo).glob(pattern)` treats its anchor
// literally — so this was a divergence as well as a defect.
func TestFocusResolvesUnderARepoPathCarryingAGlobMetacharacter(t *testing.T) {
	repo := metacharacterHome(t)
	writeHandoff(t, repo, "handoff-parity.md",
		"# handoff\n\nthe work is in `apps/widget-cfg/values.yaml` today.\n",
		time.Unix(946684800, 0))

	// 🔴 THE REACHABILITY CONTROL, NAMING THE MECHANISM RATHER THAN THE SYMPTOM. A fixture
	// that merely contains a `[` proves nothing unless the pattern the deleted code built is
	// one `filepath.Match` actually refuses; otherwise this row is a second sample of the
	// ordinary case and would pass at the broken commit.
	if _, err := filepath.Glob(filepath.Join(repo, HandoffGlobs[0])); !errors.Is(err, filepath.ErrBadPattern) {
		t.Fatalf("the fixture does not reach the defect — globbing %q gave err=%v, so the "+
			"deleted code would have found the doc anyway", repo, err)
	}

	got := Focus(repo)

	if got.Source != "claudedocs/handoff-parity.md" {
		t.Fatalf("Source=%q, want %q — a glob metacharacter in the REPO PATH emptied the "+
			"window, so `recall` reports there is no handoff doc to read a path window from "+
			"while the doc is sitting in claudedocs/", got.Source, "claudedocs/handoff-parity.md")
	}
	want := []string{"claudedocs/handoff-parity.md", "apps/widget-cfg/values.yaml"}
	if strings.Join(got.Paths, "|") != strings.Join(want, "|") {
		t.Fatalf("Paths=%v, want %v", got.Paths, want)
	}
}

// TestFocusKeepsItsFamilyOrderAndItsTieBreakForAnOrdinaryRepo is an INVARIANT GUARD, NOT
// REGRESSION COVERAGE, and is labelled as one: it PASSES at `e293c6e`.
//
// It is what the metacharacter row cannot assert. `anchoredGlob` replaced `filepath.Glob`, and
// the SET and the ORDER it produces are the thing a replacement is most likely to move quietly:
// the lowercase family must still be consulted before the caps family (so a NEWER `*HANDOFF*.md`
// loses to an older `handoff-*.md`), and within a family the pick must still be `max` over
// `(mtime, name)` — the oracle's own key, which is what makes two docs written in the same
// second resolve identically on every run.
func TestFocusKeepsItsFamilyOrderAndItsTieBreakForAnOrdinaryRepo(t *testing.T) {
	repo := t.TempDir()
	sameSecond := time.Unix(946684800, 0)
	writeHandoff(t, repo, "handoff-alpha.md", "# a\n\n`apps/a/values.yaml`\n", sameSecond)
	writeHandoff(t, repo, "handoff-beta.md", "# b\n\n`apps/b/values.yaml`\n", sameSecond)
	// NEWER by a full day, and in the OTHER family — so it wins on mtime and must still lose
	// on family order.
	writeHandoff(t, repo, "PROJECT-HANDOFF-NOTES.md", "# c\n\n`apps/c/values.yaml`\n",
		sameSecond.Add(24*time.Hour))

	got := Focus(repo)

	if got.Source != "claudedocs/handoff-beta.md" {
		t.Fatalf("Source=%q, want %q — the lowercase family is consulted first and `max` over "+
			"(mtime, name) breaks a same-second tie by NAME", got.Source,
			"claudedocs/handoff-beta.md")
	}
	want := []string{"claudedocs/handoff-beta.md", "apps/b/values.yaml"}
	if strings.Join(got.Paths, "|") != strings.Join(want, "|") {
		t.Fatalf("Paths=%v, want %v", got.Paths, want)
	}
}

// stagingTree makes a `<cache><suffix>` directory holding one file and stamps its own mtime.
// The directory's mtime is set LAST, because writing the child bumps it to now.
func stagingTree(t *testing.T, cache, suffix string, mtime time.Time) string {
	t.Helper()
	path := cache + suffix
	if err := os.MkdirAll(filepath.Join(path, "alpha-notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "alpha-notes", "left.md"),
		[]byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestReapOrphansCollectsUnderACacheParentCarryingAGlobMetacharacter is the same class at the
// sync path's own housekeeping.
//
// 🔴 A LEAK NOTHING PRINTS. `ReapOrphans` returns a count no caller renders, so the only
// evidence it ever ran is the filesystem. RED at `e293c6e`: with a `[` above the cache root
// `filepath.Glob` returned `ErrBadPattern`, the call site's `if err != nil { continue }` skipped
// both prefixes, and every interrupted sync's staging tree stayed on disk for ever — on exactly
// the hosts whose directory names are least ordinary. The oracle collects them.
func TestReapOrphansCollectsUnderACacheParentCarryingAGlobMetacharacter(t *testing.T) {
	parent := metacharacterHome(t)
	cache := filepath.Join(parent, "subsystem-store")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := time.Unix(946684800, 0)
	staged := []string{
		stagingTree(t, cache, ".new-abandoned", stale),
		stagingTree(t, cache, ".old-retired", stale),
	}

	// The reachability control: the pattern the deleted code built is one `Match` refuses.
	if _, err := filepath.Glob(filepath.Join(parent, "subsystem-store.new-*")); !errors.Is(err, filepath.ErrBadPattern) {
		t.Fatalf("the fixture does not reach the defect — globbing under %q gave err=%v", parent, err)
	}

	if n := ReapOrphans(cache); n != 2 {
		t.Fatalf("reaped %d, want 2 — a glob metacharacter in the cache's PARENT skipped both "+
			"prefixes, so every abandoned staging tree survives every later sync", n)
	}
	for _, path := range staged {
		if _, err := os.Lstat(path); err == nil {
			t.Fatalf("%s is still on disk", filepath.Base(path))
		}
	}
}

// TestReapOrphansStillDiscriminatesByPrefixAgeAndKind is an INVARIANT GUARD, NOT REGRESSION
// COVERAGE, and is labelled as one: it PASSES at `e293c6e`.
//
// The fix replaced `filepath.Match(name+".new-*", …)` with `strings.HasPrefix`, and the four
// things that predicate must not have lost are the prefix BOUNDARY (`…old-` is not `…older-`),
// the EMPTY suffix (`*` matches nothing, and so does `HasPrefix`, so a bare `<cache>.old-` is
// still a staging tree), the one-hour GRACE (a young tree may belong to a CONCURRENT sync), and
// the sibling cache whose trees are not this cache's to remove.
//
// ⚠ `TestOrphanStagingTreesAreReapedOnlyOnceTheyCannotBeLIVE` in `client_test.go` already
// covers the grace pair and an unrelated directory; this row is the PREFIX ALGEBRA, which is
// the part the rewrite could have moved and that one cannot see.
func TestReapOrphansStillDiscriminatesByPrefixAgeAndKind(t *testing.T) {
	parent := t.TempDir()
	cache := filepath.Join(parent, "subsystem-store")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := time.Unix(946684800, 0)
	reapable := []string{
		stagingTree(t, cache, ".old-retired", stale),
		// The EMPTY suffix. `Glob`'s `*` matched it and `HasPrefix` does too; a predicate
		// written as "prefix AND something after it" would quietly leak this one.
		stagingTree(t, cache, ".old-", stale),
	}
	survivors := []string{
		// The boundary: `.older-` is not `.old-`.
		stagingTree(t, cache, ".older-thing", stale),
		// The grace.
		stagingTree(t, cache, ".new-inflight", time.Now()),
		// Another cache's staging tree, in the same parent directory.
		stagingTree(t, filepath.Join(parent, "subsystem-store-other"), ".old-retired", stale),
		cache,
	}
	// A plain FILE with a staging name: the kind ladder's `default` arm leaves it alone, and it
	// is here because an enumeration that stopped checking the kind would count it.
	plain := cache + ".old-notadir"
	if err := os.WriteFile(plain, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(plain, stale, stale); err != nil {
		t.Fatal(err)
	}
	survivors = append(survivors, plain)

	if n := ReapOrphans(cache); n != len(reapable) {
		t.Fatalf("reaped %d, want %d — exactly the stale directories whose name carries this "+
			"cache's own `.new-`/`.old-` prefix", n, len(reapable))
	}
	for _, gone := range reapable {
		if _, err := os.Lstat(gone); err == nil {
			t.Fatalf("%s is still on disk", filepath.Base(gone))
		}
	}
	for _, survivor := range survivors {
		if _, err := os.Lstat(survivor); err != nil {
			t.Fatalf("%s was removed and must not have been: %v", filepath.Base(survivor), err)
		}
	}
	// …and the sibling cache kept its CONTENTS, not merely its directory entry.
	if _, err := os.Lstat(filepath.Join(parent, "subsystem-store-other.old-retired",
		"alpha-notes", "left.md")); err != nil {
		t.Fatalf("another cache's staging tree lost its contents: %v", err)
	}
}

// TestPutDerivesARevisionUnderAMetacharacterCacheRoot is the third site.
//
// 🔴 THE NAME IS PINNED BY `tests/parity/README.md`'s RETIRED RESIDUAL 9, WHICH IS WHY IT IS
// NOT SPELLED LIKE ITS TWO SIBLINGS. That row's closing condition is a command —
// `go test ./internal/client/ -run PutDerivesARevisionUnderAMetacharacterCacheRoot -count=1 -v`
// must print a `--- PASS:` line — and the row spends a paragraph warning that a zero-selection
// `-run` exits 0, so the exit code cannot tell an unwritten test from a met condition. Measured
// at `6696ad1`: the first cut of this row was called
// `…UnderACacheRootCarryingAGlobMetacharacter`, that filter selected NOTHING, printed
// `testing: warning: no tests to run` / `PASS` / `ok … [no tests to run]` and exited **0** —
// the exact state the row warned about, with the row retired over it. Renaming was the cheap
// half of keeping the prior round's check honest. Do not rename it back without moving the
// pinned command in the same commit.
//
// ⚠ THAT SHA WAS CITED AS `6696a17` IN THE ROUND THAT WROTE THIS, AND IT RESOLVES TO NOTHING —
// `git rev-parse --verify 6696a17` is `fatal: Needed a single revision`, so the whole
// bookkeeping argument was unreproducible. The commit is
// `6696ad17ea094be1f49a666eab77c7f8b0008381`. Corrected here and in residual 9, each verified
// with `git cat-file -e <sha>^{commit}`, with `deadbee` as the control that the check can fail.
// The same lesson is already written down at `internal/envalias/envalias_test.go`; quote a sha
// only after resolving it.
//
// 🔴 A REFUSAL RATHER THAN A WRONG ANSWER, WHICH IS WHY IT WAS RECORDED BEFORE IT WAS FIXED —
// and it is still a write the operator cannot make. RED at `e293c6e`: `put` derived its
// `If-Match` with `filepath.Glob(filepath.Join(cache, scope, ref+".md"))`, so a `[` in the cache
// root took BOTH that pattern and the `<ref>.*.md` fallback to `ErrBadPattern` and n=0, the
// `!= 1` arm fired, and this client exited 2 with `cannot derive a revision — 0 cached file(s)
// match …` over a cache that holds exactly one. The oracle answers `replaced` at exit 0.
func TestPutDerivesARevisionUnderAMetacharacterCacheRoot(t *testing.T) {
	home := metacharacterHome(t)
	t.Setenv("HOME", home)
	t.Setenv("CAIRN_MIRROR_ROOT", "")

	stamp := time.Unix(946684800, 0)
	cached := []byte("---\nservice: gauge-api\nscope: alpha-notes\n---\n\nthe cached copy.\n")
	body := gzTar(t, func(tw *tar.Writer) {
		regular(tw, "alpha-notes/gauge-api.md", cached, stamp)
	})
	var sentIfMatch string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/snapshot" {
			w.Header().Set("X-Store-Entries", "1")
			_, _ = w.Write(body)
			return
		}
		sentIfMatch = r.Header.Get("If-Match")
		w.Header().Set("X-Store-Status", "replaced")
		w.Header().Set("ETag", `"newrevision0000"`)
		_, _ = w.Write([]byte("replaced\n"))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SUBSYSTEM_STORE_URL", srv.URL)
	t.Setenv("SUBSYSTEM_STORE_TOKEN", "t-synthetic")
	dir := configuredHost(t, filepath.Join(home, "config"))
	writeTable(t, dir, `{"alpha-notes": "`+DefaultAlias+`"}`)

	cache := DefaultCacheRoot()
	if !strings.Contains(cache, "[") {
		t.Fatalf("the fixture is not under test: the default cache root is %q", cache)
	}
	// The reachability control, on BOTH patterns the deleted code built.
	for _, pattern := range []string{"gauge-api.md", "gauge-api.*.md"} {
		if _, err := filepath.Glob(filepath.Join(cache, "alpha-notes", pattern)); !errors.Is(err, filepath.ErrBadPattern) {
			t.Fatalf("the fixture does not reach the defect — globbing %q under %q gave err=%v",
				pattern, cache, err)
		}
	}

	replacement := filepath.Join(t.TempDir(), "new.md")
	if err := os.WriteFile(replacement, []byte("replacement\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := capture(t, Put, Options{
		Cache: cache, Timeout: 5, Repo: ".",
		Scope: "alpha-notes", Ref: "gauge-api", File: replacement,
	})

	if code != ExitOK {
		t.Fatalf("put exited %d, want 0 — a glob metacharacter in the cache root made the "+
			"revision underivable over a cache holding exactly one match\nstderr: %s", code, stderr)
	}
	sum := sha256.Sum256(cached)
	want := `"` + hex.EncodeToString(sum[:])[:16] + `"`
	if sentIfMatch != want {
		t.Fatalf("If-Match %s, want %s (sha256 of the cached entry's bytes)", sentIfMatch, want)
	}
	if !strings.Contains(stdout, "replaced") {
		t.Fatalf("stdout=%q, want the server's status line", stdout)
	}
}

// TestPutStillResolvesTheDottedVariantAndItsBoundaries is an INVARIANT GUARD, NOT REGRESSION
// COVERAGE, and is labelled as one: it PASSES at `e293c6e`.
//
// 🔴 IT IS THE ONLY THING PINNING THE ONE WILDCARD THE FIX KEPT. `<ref>.*.md` was a glob and is
// now three bounds — prefix, suffix and a length floor — and the floor is the half a reader will
// not re-derive: `fnmatch` does NOT match `<ref>.md` with `<ref>.*.md`, because the `*` sits
// between two literal dots. Measured on CPython 3.12.14 against `filepath.Match`, which agrees.
// Getting the floor wrong would make an exact entry match the FAMILY as well, so a cache holding
// `gauge-api.md` alone would count TWO and refuse.
func TestPutStillResolvesTheDottedVariantAndItsBoundaries(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CAIRN_MIRROR_ROOT", "")

	stamp := time.Unix(946684800, 0)
	// 🔴 NO `gauge-api.md` IN THIS STORE. The exact-name arm is tried first and would shadow
	// the family arm entirely; the cache has to hold ONLY the dotted spelling for the second
	// arm to be the thing under test.
	variant := []byte("---\nservice: gauge-api\nscope: alpha-notes\n---\n\nthe runbook copy.\n")
	body := gzTar(t, func(tw *tar.Writer) {
		regular(tw, "alpha-notes/gauge-api.runbook.md", variant, stamp)
	})
	var sentIfMatch string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/snapshot" {
			w.Header().Set("X-Store-Entries", "1")
			_, _ = w.Write(body)
			return
		}
		sentIfMatch = r.Header.Get("If-Match")
		w.Header().Set("X-Store-Status", "replaced")
		w.Header().Set("ETag", `"newrevision0000"`)
		_, _ = w.Write([]byte("replaced\n"))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SUBSYSTEM_STORE_URL", srv.URL)
	t.Setenv("SUBSYSTEM_STORE_TOKEN", "t-synthetic")
	dir := configuredHost(t, filepath.Join(home, "config"))
	writeTable(t, dir, `{"alpha-notes": "`+DefaultAlias+`"}`)

	replacement := filepath.Join(t.TempDir(), "new.md")
	if err := os.WriteFile(replacement, []byte("replacement\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := capture(t, Put, Options{
		Cache: DefaultCacheRoot(), Timeout: 5, Repo: ".",
		Scope: "alpha-notes", Ref: "gauge-api", File: replacement,
	})
	if code != ExitOK {
		t.Fatalf("put exited %d, want 0 — the `<ref>.*.md` family is what resolves an entry "+
			"whose filename carries a kind\nstderr: %s", code, stderr)
	}
	sum := sha256.Sum256(variant)
	if want := `"` + hex.EncodeToString(sum[:])[:16] + `"`; sentIfMatch != want {
		t.Fatalf("If-Match %s, want %s", sentIfMatch, want)
	}
	// 🔴 …AND THE PREDICATE ITSELF, AGAINST THE GLOB IT REPLACED, WHICH IS THE ONLY PLACE
	// `refVariantName`'s LENGTH FLOOR IS OBSERVABLE. Inside `Put` that floor is unreachable:
	// the exact-name arm runs first over the same directory, so the family arm never sees a
	// `<ref>.md`. Two independent operands here — this table, which says what the family
	// MEANS, and `filepath.Match`, which is the pattern the code used to build — so a wrong
	// bound has to be wrong in the same direction twice to pass.
	const ref = "gauge-api"
	for _, row := range []struct {
		name   string
		member bool
	}{
		{"gauge-api.md", false},         // the exact name is NOT a member
		{"gauge-api..md", true},         // `*` matching the empty string IS
		{"gauge-api.runbook.md", true},  //
		{"gauge-api.a.b.md", true},      // `*` spans dots
		{"gauge-apis.md", false},        // the `.` after the ref is literal
		{"gauge-api.runbook.txt", false} /* the suffix is literal too */, {"gauge-api", false},
	} {
		if got := refVariantName(row.name, ref); got != row.member {
			t.Fatalf("refVariantName(%q, %q) = %v, want %v", row.name, ref, got, row.member)
		}
		glob, err := filepath.Match(ref+".*.md", row.name)
		if err != nil {
			t.Fatal(err)
		}
		if glob != row.member {
			t.Fatalf("`%s.*.md` vs %q: Match says %v where this table says %v — the table is "+
				"the model `refVariantName` implements, so one of the two is wrong",
				ref, row.name, glob, row.member)
		}
	}
}

// TestHandoffGlobsKeepTheLiteralDIRECTORYPrefixThatFocusJOINS pins the ONE precondition
// `Focus`'s collapse rests on, so it cannot be lost by editing a variable in another file.
//
// 🔴 `Focus` SPLITS EACH PATTERN AT ITS LAST `/` AND **JOINS** THE LEFT HALF ONTO THE REPO,
// interpreting only the right half. That is exactly `filepath.Glob`'s own behaviour — and
// exactly WRONG for a pattern whose directory prefix is itself a wildcard, which `Glob` would
// have expanded and this would match literally. The failure direction is the one this whole
// file exists to refuse: no error, an empty match set, and a confident sentence saying the repo
// has no handoff doc. A general component-by-component walk used to cover that case; it had no
// caller and was deleted (`anchor.go`), so this row is what stops the case arriving unnoticed.
//
// 🔴 BUT THE METACHARACTER ARM REFUSES AN **AMBIGUITY**, NOT A DEFECT, AND AN EARLIER FORM OF
// ITS MESSAGE GOT THAT BACKWARDS — it claimed `Focus` "would match nothing" and prescribed
// restoring the walk, which is the change that would BREAK the other reading. MEASURED on
// go1.25.14 against a repo holding a directory literally named `claudedocs[v2]`:
//
//	walker:  filepath.Match("claudedocs[v2]", "claudedocs[v2]")            = false, err=<nil>
//	pre-fix: filepath.Glob(<repo>/claudedocs[v2]/handoff-*.md)             = [],    err=<nil>
//	today:   Focus with HandoffGlobs=["claudedocs[v2]/handoff-*.md"]       = the doc
//
// i.e. the literal-directory reading is the one the JOIN gets RIGHT and a component walk gets
// WRONG. The two intents are indistinguishable from the string, they want opposite remedies,
// so this arm refuses rather than guesses and its message now says which is which.
//
// ⚠ IT IS A GUARD ON THE DATA, NOT ON A SPELLING: it reads `HandoffGlobs` itself and asks
// `strings.ContainsAny` over the prefix, so any pattern added in any wording is measured.
//
// 🔴 AND THE WELL-FORMEDNESS HALF PROBES A **SET** OF NAMES, BECAUSE ONE FIXED NAME IS BLIND TO
// HALF THE PATTERNS. `filepath.Match` reports `ErrBadPattern` only for a chunk it actually
// REACHES. MEASURED on go1.25.14:
//
//	Match("*HANDOFF*[.md", "handoff-x.md")             = false, err=<nil>          ← BLIND
//	Match("*HANDOFF*[.md", "PROJECT-HANDOFF-NOTES.md") = false, err=syntax error
//	Match("handoff-*[.md", "handoff-x.md")             = false, err=syntax error
//	Match("handoff-*[.md", "PROJECT-HANDOFF-NOTES.md") = false, err=<nil>          ← BLIND
//
// The first cut probed `"handoff-x.md"` alone, which cannot see the CAPS family — the member
// actually spelled `*HANDOFF*.md`, where a stray `[` is the realistic typo. Demonstrated by
// mutation at this head: `HandoffGlobs[1] = "claudedocs/*HANDOFF*[.md"` left this row PASSING
// while production silently lost that family.
//
// ⚠ REACHABILITY IS ASSERTED, NOT ASSUMED, AND THAT IS WHAT MAKES THE CHECK SUFFICIENT RATHER
// THAN MERELY TWO-FAMILY: a probe that MATCHES (`ok == true`, `err == nil`) proves `Match`
// consumed the whole pattern, so every chunk was scanned and none was ill-formed. A member no
// probe name reaches is therefore a FAILURE here, not a quiet pass — it means this row measured
// NOTHING about that member, and the fix is to add a name the new family matches.
func TestHandoffGlobsKeepTheLiteralDIRECTORYPrefixThatFocusJOINS(t *testing.T) {
	if len(HandoffGlobs) == 0 {
		t.Fatal("HandoffGlobs is empty — this row would pass while measuring nothing")
	}
	for _, pattern := range HandoffGlobs {
		dir, base := path.Split(pattern)
		if strings.ContainsAny(dir, `*?[\`) {
			t.Fatalf("HandoffGlobs member %q has a glob metacharacter in its DIRECTORY prefix "+
				"%q, and `Focus` JOINS that prefix onto the repo literally. Which of the two "+
				"intents this is cannot be read off the string, and they want OPPOSITE "+
				"remedies: if the prefix was meant as a WILDCARD (`*/handoff-*.md`), `Focus` "+
				"matches nothing and reports the repo has no handoff doc — drop it, or give "+
				"`Focus` back a component-by-component walk. If it names a directory "+
				"LITERALLY (`claudedocs[v2]`), `Focus` already resolves it and a walk would "+
				"BREAK it — measured, the walk misses that directory and `Focus` finds it. "+
				"This row refuses the ambiguity rather than guessing; decide which you meant.",
				pattern, dir)
		}
		// …and the other half: the part `Focus` DOES interpret has to be interpretable, or
		// the pattern silently matches nothing for the opposite reason. One probe name cannot
		// do this — see the reachability note above.
		probes := []string{"handoff-x.md", "PROJECT-HANDOFF-NOTES.md"}
		reached := false
		for _, name := range probes {
			ok, err := filepath.Match(base, name)
			if err != nil {
				t.Fatalf("HandoffGlobs member %q has an ill-formed final component %q: "+
					"filepath.Match(%q, %q) = %v. `Focus` discards that error, so this "+
					"family would match nothing and the repo would be reported as having "+
					"no handoff doc.", pattern, base, base, name, err)
			}
			if ok {
				reached = true
			}
		}
		if !reached {
			t.Fatalf("no probe name in %v matches HandoffGlobs member %q's final component "+
				"%q, so `filepath.Match` never scanned the whole pattern and this row "+
				"measured NOTHING about that member's well-formedness. Add a name the new "+
				"family matches to `probes`.", probes, pattern, base)
		}
	}
}

// TestFocusDoesNotREADADirectoryTheGlobOnlyDESCENDSTHROUGH is the row for the one branch "same
// results for ordinary paths" rests on that no other row reaches.
//
// 🔴 `filepath.Glob` SPLITS AT THE LAST SEPARATOR AND READS ONE DIRECTORY. For
// `<repo>/claudedocs/handoff-*.md` that is `<repo>/claudedocs`; `<repo>` itself is never listed.
// So a repo that is SEARCHABLE but not READABLE (mode `--x`) resolved fine before this change,
// and still does. A `Focus` that enumerated `<repo>` to find `claudedocs` would find nothing
// there — a silent narrowing, in the same "empty result" shape as the defect this branch fixed.
//
// 🔴 THE ORACLE DOES **NOT** SHARE THE PROPERTY, AND AN EARLIER FORM OF THIS COMMENT SAID IT
// DID, ON A CPython INTERNAL THAT DOES NOT EXIST. It read *"resolves fine on the oracle, whose
// `_PreciseSelector` asks `is_dir()` rather than scandir'ing the parent."* MEASURED on the
// pinned interpreter (`flake.nix` → `python312`, 3.12.14): `_PreciseSelector` is absent from
// `pathlib` — `_make_selector` falls through to `_WildcardSelector` even for a literal
// component, and that selector `scandir`s its parent — all three re-measured at 3.12.14 before
// this sentence was allowed to stay: `hasattr(pathlib, …)` for the two names, a direct
// `_make_selector(("claudedocs", "handoff-*.md"), …)` call returning `_WildcardSelector` for
// the fall-through, and an `os.scandir`/`os.listdir` spy for the read (1 `scandir` at `0111`,
// 2 when readable).
//
// 🔴 THE CLAIM THIS ROW ACTUALLY RESTS ON IS THE BEHAVIOUR, NOT THAT MECHANISM. End to end on
// one fixture, repo at mode `0111`: `Focus` → `Source="claudedocs/handoff-demo.md"`,
// `focus_window` → `FocusWindow(paths=(), source=None)`. So this row pins a GO property and a
// DECLARED divergence (`tests/parity/README.md` residual 10), not a parity property. The
// direction is still the right one — the Go side answers the doc where the oracle claims
// absence — but that is a choice, not agreement.
//
// 🔴 AND THE SIBLING COPIES WENT WRONG A SECOND TIME, WHICH IS WHY THE MECHANISM HALF IS KEPT
// SHORT HERE. Round 3 (`39e3977`) replaced the dead `_PreciseSelector` sentence in `focus.go`
// and in `tests/parity/README.md` with *"at mode `0111` `os.listdir` raises `PermissionError`,
// `focus_window`'s own `except OSError` turns that into an ordinary empty answer"* — also
// false: measured with the spy above, `os.listdir` is called **0** times and `Path.glob` does
// not raise, so that arm never executes in this scenario. Both sites are corrected to the
// behavioural claim. Every wording of this paragraph that named a mechanism has been wrong —
// two distinct false mechanisms, each of which travelled between files before anyone measured
// it (provenance of both is below and was derived, not recalled) — so do not write a third. If
// you keep a
// mechanism detail anywhere in this family, measure it with a spy, pin the interpreter version
// beside it, and say it was measured rather than read.
//
// 🔴 PROVENANCE OF THE FIRST ONE, MEASURED with `git log -S_PreciseSelector` and a
// per-commit `git show <c>:<file> | grep -c`, because the round that fixed it guessed: the
// sentence entered in `anchor.go` at `7348820`, was COPIED into this file at `6696ad1`, and
// round 1 (`b91c4ed`) deleted the `anchor.go` copy while writing a THIRD into `focus.go` — a
// false claim MOVED, not removed, twice. No copy of it survives as an assertion anywhere in the
// tree; the remaining occurrences are quoted retractions, and a tree-wide grep for `listdir`,
// `except OSError` and `_WildcardSelector` is how that was checked.
//
// 🔴 PROVENANCE OF THE SECOND ONE, DERIVED THE SAME WAY rather than taken from the round that
// wrote it: `git show <c>:<file> | grep -c` for the `os.listdir`-raises sentence over
// `anchor.go`, `anchor_test.go`, `focus.go` and `tests/parity/README.md` at `6696ad1`,
// `b91c4ed` and `39e3977` answers 0 everywhere until `39e3977`, where it is 1 in `focus.go`
// and 1 in `tests/parity/README.md` and still 0 here. So the second false mechanism was born
// in two files at once, in the very commit that retracted the first.
//
// ⚠ IT IS AIMED AT `Focus` RATHER THAN AT A HELPER, AND THAT IS THE POINT. An earlier cut
// asserted this against `anchoredGlob`, a general walker with no general caller; the property is
// only worth anything on the path `cairn recall --repo <path>` actually takes, so it is asserted
// there.
//
// ⚠ IT SKIPS UNDER EUID 0, AND THE SKIP IS LOUD BUT **NOT COUNTED** — AN EARLIER FORM OF THIS
// COMMENT SAID "THE SKIP IS COUNTED RATHER THAN LEFT TO LOOK LIKE A PASS", AND NOTHING COUNTS
// IT. Root bypasses the missing read bit, so the fixture cannot construct the state at all; the
// skip is LOUD in the sense that its reason is printed under `-v` and the row is not silently
// green. That is all it is. The `go` job's only floor is `ok=$(grep -c '^ok  ' …)` over
// PACKAGES (`.github/workflows/ci.yml`), and `go test` without `-v` prints nothing about skips,
// so this row disappearing into a skip moves no number CI reads. The repo does own a real skip
// counter — twice, over the pytest ledger job and the conformance corpus, both reading a
// `skipped=` count and REFUSING on it — which is precisely why "COUNTED" read as a mechanism
// that exists here. It does not.
//
// ⚠ THE GAP IS PRE-EXISTING AND REPO-WIDE, AND IS NOT CLOSED HERE. Measured at this head: ten
// `t.Skip*` call sites in the tree, nine of them other than this one, four `os.Geteuid() == 0`
// guards of which three are other rows. A counter for the Go tier is a whole-tier change
// (`go test -json`, or `-v` plus a parser, on the one invocation the `ok` floor covers) and
// does not belong in this file. What BOUNDS the exposure is that this arm cannot fire in CI:
// every job in `ci.yml` is a bare `runs-on: ubuntu-latest` with no `container:`, and a hosted
// runner's job user is not root. ⚠ That last half is read from the workflow file and from
// GitHub's documented runner user — it is not a measurement taken on a runner.
func TestFocusDoesNotREADADirectoryTheGlobOnlyDESCENDSTHROUGH(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("euid 0 bypasses the missing read bit, so the fixture cannot be built — this " +
			"row measures nothing here and says so rather than passing")
	}
	root := t.TempDir()
	writeHandoff(t, root, "handoff-a.md", "# a\n\nthe work is in `apps/a/values.yaml`.\n",
		time.Unix(946684800, 0))
	if err := os.Chmod(root, 0o111); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) })

	// The reachability control: the state really is "cannot list, can traverse".
	if _, err := os.ReadDir(root); err == nil {
		t.Fatalf("the fixture does not reach the branch — %q is still readable", root)
	}

	got := Focus(root)

	want := []string{"claudedocs/handoff-a.md", "apps/a/values.yaml"}
	if got.Source != "claudedocs/handoff-a.md" ||
		strings.Join(got.Paths, "|") != strings.Join(want, "|") {
		t.Fatalf("Focus over a SEARCHABLE-but-not-READABLE repo gave Source=%q Paths=%v, want "+
			"%q and %v — the directory prefix a pattern only DESCENDS through must be JOINED "+
			"onto the repo, not found by listing the repo", got.Source, got.Paths,
			"claudedocs/handoff-a.md", want)
	}
}
