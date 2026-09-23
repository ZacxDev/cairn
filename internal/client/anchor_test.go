package client

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
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

// TestPutDerivesARevisionUnderACacheRootCarryingAGlobMetacharacter is the third site.
//
// 🔴 A REFUSAL RATHER THAN A WRONG ANSWER, WHICH IS WHY IT WAS RECORDED BEFORE IT WAS FIXED —
// and it is still a write the operator cannot make. RED at `e293c6e`: `put` derived its
// `If-Match` with `filepath.Glob(filepath.Join(cache, scope, ref+".md"))`, so a `[` in the cache
// root took BOTH that pattern and the `<ref>.*.md` fallback to `ErrBadPattern` and n=0, the
// `!= 1` arm fired, and this client exited 2 with `cannot derive a revision — 0 cached file(s)
// match …` over a cache that holds exactly one. The oracle answers `replaced` at exit 0.
func TestPutDerivesARevisionUnderACacheRootCarryingAGlobMetacharacter(t *testing.T) {
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
	// …and the boundary the length floor draws, asserted directly rather than inferred from
	// the run above: `gauge-api.md` is NOT a member of the `gauge-api.*.md` family.
	for name, member := range map[string]bool{
		"gauge-api.md":         false,
		"gauge-api..md":        true,
		"gauge-api.runbook.md": true,
		"gauge-apis.md":        false,
	} {
		ok, err := filepath.Match("gauge-api.*.md", name)
		if err != nil {
			t.Fatal(err)
		}
		if ok != member {
			t.Fatalf("`gauge-api.*.md` vs %q: Match says %v, this test's model says %v — the "+
				"length floor in `Put` is derived from that model", name, ok, member)
		}
	}
}

// TestAnchoredGlobTreatsItsAnchorLiterallyAndItsPatternAsAPattern is the helper's own row.
//
// 🔴 IT IS NOT REDUNDANT WITH THE THREE ABOVE, AND THE DIRECTION IT ADDS IS THE SECOND HALF. A
// fix that made the anchor literal by making the PATTERN literal too would satisfy every
// metacharacter row in this file and would silently break `Focus`, whose patterns are globs by
// design. This asserts both halves at once, in one tree.
func TestAnchoredGlobTreatsItsAnchorLiterallyAndItsPatternAsAPattern(t *testing.T) {
	root := metacharacterHome(t)
	for _, rel := range []string{
		filepath.Join("claudedocs", "handoff-a.md"),
		filepath.Join("claudedocs", "handoff-b.md"),
		filepath.Join("claudedocs", "notes.md"),
		filepath.Join("elsewhere", "handoff-c.md"),
	} {
		if err := os.MkdirAll(filepath.Join(root, filepath.Dir(rel)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, rel), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got := anchoredGlob(root, "claudedocs/handoff-*.md")
	want := []string{
		filepath.Join(root, "claudedocs", "handoff-a.md"),
		filepath.Join(root, "claudedocs", "handoff-b.md"),
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %v, want %v — the anchor must be a path and the pattern must still be a "+
			"pattern, in `os.ReadDir`'s sorted order", got, want)
	}

	// A pattern whose DIRECTORY component is a wildcard, which `Focus`'s own patterns are not
	// and a future one could be: the walk has to descend through every match.
	got = anchoredGlob(root, "*/handoff-c.md")
	want = []string{filepath.Join(root, "elsewhere", "handoff-c.md")}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %v, want %v — an intermediate component is a pattern too", got, want)
	}

	// An anchor that is not a directory at all yields nothing rather than an error, which is
	// what every caller's `Glob` did.
	if got = anchoredGlob(filepath.Join(root, "claudedocs", "notes.md"), "*.md"); got != nil {
		t.Fatalf("got %v, want nil for an anchor that is not a directory", got)
	}
}
