package client

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// tinyArchive is a one-entry gzipped tar, which is all `InstallSnapshot` needs.
func tinyArchive(t *testing.T, name, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{
		Name: name, Mode: 0o644, Size: int64(len(body)),
		ModTime: time.Unix(946684800, 0), Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	for _, closer := range []func() error{tw.Close, gz.Close} {
		if err := closer(); err != nil {
			t.Fatal(err)
		}
	}
	return buf.Bytes()
}

func TestStorableETagIsTheDeclaredFilter(t *testing.T) {
	// 🔴 THE VALIDATOR COMES FROM A SERVER THIS CLIENT DOES NOT FULLY TRUST, and it is
	// written to a file AND re-sent as a header. Every rejected case below is a value
	// that would break one of those two.
	for _, tc := range []struct {
		in, want, why string
	}{
		{`"sha256:abc"`, `"sha256:abc"`, "the ordinary case"},
		{"", "", "no tag at all"},
		{"\"a\nb\"", "", "a newline would add a second line to the stored file"},
		{"\"a\rb\"", "", "a CR would split the outgoing header"},
		{`"a b"`, "", "RFC 9110's etagc excludes the space"},
		{"\"a\tb\"", "", "and the tab"},
		{"\"café\"", "", "non-ASCII cannot be carried identically by two header writers"},
		{`"` + strings.Repeat("x", 200) + `"`, "", "over the length cap"},
	} {
		if got := StorableETag(tc.in); got != tc.want {
			t.Errorf("StorableETag(%q) = %q, want %q — %s", tc.in, got, tc.want, tc.why)
		}
	}
	// The POSITIVE control on the length cap: one byte under it is KEPT, so the rejection
	// above is the cap firing rather than the whole branch being dead.
	long := `"` + strings.Repeat("x", maxETagBytes-2) + `"`
	if StorableETag(long) != long {
		t.Fatalf("a tag of exactly the cap must be kept, got %q", StorableETag(long))
	}
}

func TestTheValidatorIsInstalledWithTheContentItDescribes(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "store")
	headers := http.Header{}
	headers.Set("ETag", `"sha256:abc"`)
	headers.Set("X-Store-Entries", "1")
	if _, err := InstallSnapshot(tinyArchive(t, "alpha/one.md", "x\n"), cache, headers); err != nil {
		t.Fatal(err)
	}
	if got := StoredETag(cache); got != `"sha256:abc"` {
		t.Fatalf("the validator was not recorded beside the cache: %q", got)
	}
	// 🔴 AND NOT IN `.sync-stamp`, WHICH IS RENDERED LINE BY LINE IN EVERY REPORT HEADER.
	// This is the reason it is its own file; a stamp field would print a digest on every
	// recall a human reads.
	stamp, err := os.ReadFile(filepath.Join(cache, SyncStamp))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stamp), "sha256:") {
		t.Fatalf("the validator leaked into the rendered stamp:\n%s", stamp)
	}

	// A response with NO usable validator leaves none behind — and it must CLEAR the
	// previous one, which the staging-directory install does for free.
	headers.Set("ETag", "a bad one")
	if _, err := InstallSnapshot(tinyArchive(t, "alpha/one.md", "y\n"), cache, headers); err != nil {
		t.Fatal(err)
	}
	if got := StoredETag(cache); got != "" {
		t.Fatalf("an unusable validator must leave the cache with none, got %q", got)
	}
}

// 🔴 A 304 IS NOT AN OUTAGE, AND THIS IS THE CLAIM THE WHOLE CLIENT CHANGE RESTS ON.
// Before the handling existed, `urllib`/`net-http` hand a 304 to the "not a 200" arm,
// which reports `answered HTTP 304` and degrades to `⚠ cached` — a confirmed-current
// cache rendered as a failure to reach the store.
func TestA304RendersAsLiveAndTouchesNothing(t *testing.T) {
	var sawValidator string
	var served int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served++
		sawValidator = r.Header.Get("If-None-Match")
		w.Header().Set("X-Store-Snapshot", "seeded=2000-01-04T00:00:00Z newest=NONE entry-files=0")
		w.Header().Set("ETag", `"sha256:abc"`)
		if sawValidator == `"sha256:abc"` {
			w.Header().Set("X-Store-Status", "not-modified")
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("X-Store-Entries", "1")
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(tinyArchive(t, "alpha/one.md", "x\n"))
	}))
	defer srv.Close()
	t.Setenv("CAIRN_URL", srv.URL)
	t.Setenv("CAIRN_TOKEN", "t")
	cache := filepath.Join(t.TempDir(), "store")

	// First sync: no validator held, so none is offered and the archive arrives.
	first, err := ResolveState(cache, false, "", 5, "")
	if err != nil {
		t.Fatal(err)
	}
	if first.Name != StateLive || !strings.Contains(first.Detail, "fetched from") {
		t.Fatalf("the first sync: %#v", first)
	}
	if sawValidator != "" {
		t.Fatalf("a host with no cache must offer no validator, sent %q", sawValidator)
	}
	stampBefore, err := os.Stat(filepath.Join(cache, SyncStamp))
	if err != nil {
		t.Fatal(err)
	}

	// Second sync: the stored validator is offered and the pod says nothing changed.
	time.Sleep(10 * time.Millisecond)
	second, err := ResolveState(cache, false, "", 5, "")
	if err != nil {
		t.Fatal(err)
	}
	if sawValidator != `"sha256:abc"` {
		t.Fatalf("the stored validator was not offered, sent %q", sawValidator)
	}
	if second.Name != StateLive {
		t.Fatalf("a 304 is not an outage: %#v", second)
	}
	if second.ExitHint != 0 {
		t.Fatalf("a 304 is a successful read: exit hint %d", second.ExitHint)
	}
	// 🔴 IT READS AS ITSELF. Not `fetched … just now` (no bytes arrived), not `SERVED
	// FROM CACHE` (the pod answered), and never an empty or unreachable store.
	if !strings.Contains(second.Detail, "already current at "+srv.URL) ||
		!strings.Contains(second.Detail, "not modified") {
		t.Fatalf("a 304 must say what it is: %q", second.Detail)
	}
	if strings.Contains(second.Detail, "fetched from") ||
		strings.Contains(second.Detail, "SERVED FROM CACHE") {
		t.Fatalf("a 304 borrowed another state's sentence: %q", second.Detail)
	}
	if !strings.Contains(second.Detail, "snapshot seeded=") {
		t.Fatalf("the 304's freshness block must reach the banner: %q", second.Detail)
	}
	// Nothing was re-extracted: the stamp is the same file, untouched.
	stampAfter, err := os.Stat(filepath.Join(cache, SyncStamp))
	if err != nil {
		t.Fatal(err)
	}
	if !stampAfter.ModTime().Equal(stampBefore.ModTime()) {
		t.Fatal("a 304 must not rewrite the cache")
	}
	if served != 2 {
		t.Fatalf("two syncs, %d requests", served)
	}
	// The banner a caller actually prints, end to end.
	line := Banner(second.Name, second.Detail)
	if strings.HasPrefix(line, "⚠") || strings.HasPrefix(line, "🔴") {
		t.Fatalf("a 304 must not raise a degradation marker: %q", line)
	}
}

// A pod that sends no `ETag` at all is the pre-change server, and a client that cannot
// store a validator simply never sends one. No conditional, no 304, no change.
//
// ⚠ AN **INVARIANT GUARD**, NOT REGRESSION COVERAGE: measured GREEN at the pre-change
// base, where no client sent a conditional at all. What it pins is BACKWARD
// COMPATIBILITY — a new client against an old pod — which is the state of every
// deployment between this change landing and the pod being rolled.
func TestAPodThatSendsNoValidatorIsUnaffected(t *testing.T) {
	var conditionals int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") != "" {
			conditionals++
		}
		w.Header().Set("X-Store-Entries", "1")
		_, _ = w.Write(tinyArchive(t, "alpha/one.md", "x\n"))
	}))
	defer srv.Close()
	t.Setenv("CAIRN_URL", srv.URL)
	t.Setenv("CAIRN_TOKEN", "t")
	cache := filepath.Join(t.TempDir(), "store")
	for i := 0; i < 2; i++ {
		state, err := ResolveState(cache, false, "", 5, "")
		if err != nil {
			t.Fatal(err)
		}
		if state.Name != StateLive || !strings.Contains(state.Detail, "fetched from") {
			t.Fatalf("sync %d: %#v", i, state)
		}
	}
	if conditionals != 0 {
		t.Fatalf("a client holding no validator must send no conditional, sent %d", conditionals)
	}
}

// 🔴 THE DOCTOR PROBE MUST NEVER BE CONDITIONAL. Its job is to count what the POD would
// send; an answer of "nothing changed" would make it report the cache as the pod and
// find the two in agreement by construction.
func TestTheDoctorProbeOffersNoValidator(t *testing.T) {
	var offered string
	var asked int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked++
		offered = r.Header.Get("If-None-Match")
		w.Header().Set("X-Store-Entries", "1")
		w.Header().Set("ETag", `"sha256:abc"`)
		_, _ = w.Write(tinyArchive(t, "alpha/one.md", "x\n"))
	}))
	defer srv.Close()
	facts := ProbeStore(Config{URL: srv.URL, Token: "t"}, 5)
	if !facts.Reached {
		t.Fatalf("the probe did not reach the fake pod: %+v", facts)
	}
	if asked != 1 || offered != "" {
		t.Fatalf("the probe offered %q over %d request(s)", offered, asked)
	}
	if facts.VisibleEntries == nil || *facts.VisibleEntries != 1 {
		t.Fatalf("the probe must still read the count: %+v", facts)
	}
}

// A validator whose tree was deleted underneath it is not offered: `StampExists` is the
// discriminator, and offering one here would let a 304 confirm an absence.
func TestNoValidatorIsOfferedForACacheThatIsNotThere(t *testing.T) {
	var offered string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offered = r.Header.Get("If-None-Match")
		w.Header().Set("X-Store-Entries", "1")
		_, _ = w.Write(tinyArchive(t, "alpha/one.md", "x\n"))
	}))
	defer srv.Close()
	t.Setenv("CAIRN_URL", srv.URL)
	t.Setenv("CAIRN_TOKEN", "t")
	cache := filepath.Join(t.TempDir(), "store")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	// A validator on disk with NO stamp beside it — a half-deleted cache root.
	if err := os.WriteFile(filepath.Join(cache, SyncETagFile), []byte("\"sha256:abc\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if StoredETag(cache) == "" {
		t.Fatal("the fixture did not write a readable validator, so the claim below is vacuous")
	}
	if _, err := ResolveState(cache, false, "", 5, ""); err != nil {
		t.Fatal(err)
	}
	if offered != "" {
		t.Fatalf("a cache with no stamp must offer no validator, sent %q", offered)
	}
}

// The declared-count cross-check is unchanged for the 200 path, and a 304 cannot reach
// it — there is no extracted count to compare. Pinned so a later edit cannot start
// comparing a header against a cache this response never described.
func TestTheEntryCountCrossCheckStillFiresOnThe200Path(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "store")
	headers := http.Header{}
	headers.Set("X-Store-Entries", strconv.Itoa(9))
	headers.Set("ETag", `"sha256:abc"`)
	_, err := InstallSnapshot(tinyArchive(t, "alpha/one.md", "x\n"), cache, headers)
	if err == nil || !strings.Contains(err.Error(), "server declared 9 entries") {
		t.Fatalf("the count cross-check must still refuse: %v", err)
	}
}
