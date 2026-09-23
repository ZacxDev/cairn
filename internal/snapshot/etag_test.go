package snapshot

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZacxDev/cairn/internal/store"
)

// inflate is the archive's own bytes, decompressed. Written here rather than reused from
// `members` because the CLAIM is about the bytes and not about the tree they parse into.
func inflate(t *testing.T, archive []byte) []byte {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatalf("the archive is not gzip: %v", err)
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("the archive did not inflate: %v", err)
	}
	return raw
}

// 🔴 THE VALIDATOR IS A DIGEST OF THE *UNCOMPRESSED* TAR, AND THIS IS THE ASSERTION THAT
// SAYS SO — every other test here would pass just as happily over a digest of the gzip
// stream. The expected value is computed from the inflated bytes INDEPENDENTLY of the
// code under test (the test calls `sha256` itself and spells the format itself), so it
// cannot be satisfied by whatever `Build` happens to do.
func TestTheETagDigestsTheUncompressedTarAndNotTheWireBytes(t *testing.T) {
	root := fixtureStore(t)
	result, err := Build(root, "", store.Unrestricted())
	if err != nil {
		t.Fatal(err)
	}
	raw := inflate(t, result.Archive)
	sum := sha256.Sum256(raw)
	want := fmt.Sprintf("%q", "sha256:"+hex.EncodeToString(sum[:]))
	if result.ETag != want {
		t.Fatalf("the ETag is not sha256 of the uncompressed tar:\n  got  %s\n  want %s",
			result.ETag, want)
	}
	// The negative control on the same claim: the digest of the COMPRESSED body must NOT
	// be what was emitted. Without this the assertion above would still hold for a
	// (broken) implementation that happened to compress to its own input.
	overWire := sha256.Sum256(result.Archive)
	if result.ETag == fmt.Sprintf("%q", "sha256:"+hex.EncodeToString(overWire[:])) {
		t.Fatal("the ETag digests the bytes on the wire, which cannot be equal across " +
			"two implementations — see ETagFor")
	}
	if len(raw) == len(result.Archive) {
		t.Fatal("the fixture did not compress at all, so this test cannot tell the two " +
			"digests apart; pick a fixture whose gzip envelope changes the length")
	}
}

// 🔴 THE LOAD-BEARING CONTROL. A bullet appended to an entry must move the validator,
// because the alternative key considered for this feature — the caller's principal plus
// the authorization epoch — would NOT have moved: the write path makes no authorization
// call, so the epoch is unchanged by a write and a conditional sync would have been
// answered 304 while the client was missing the new line. This is that exact failure,
// pinned against the key that was shipped.
func TestAnAppendMovesTheETag(t *testing.T) {
	root := fixtureStore(t)
	before, err := Build(root, "", store.Unrestricted())
	if err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(root, "alpha-notes", "gadget-one.md")
	body, err := os.ReadFile(entry)
	if err != nil {
		t.Fatal(err)
	}
	writeEntry(t, entry, string(body)+"- 2000-01-06: a bullet nobody had before.\n",
		946684800, 250000000)
	after, err := Build(root, "", store.Unrestricted())
	if err != nil {
		t.Fatal(err)
	}
	if before.ETag == after.ETag {
		t.Fatalf("an append left the validator at %s — a client holding it would be told "+
			"304 and never see the new bullet", after.ETag)
	}
	// ⚠ THE MTIME WAS DELIBERATELY RESTORED TO ITS OLD VALUE, so this is a claim about
	// the CONTENT and not about the clock: a validator keyed on mtimes alone would have
	// survived this edit.
	if !bytes.Contains(inflate(t, after.Archive), []byte("a bullet nobody had before")) {
		t.Fatal("the fixture edit did not reach the archive, so the assertion above is vacuous")
	}
}

// Two callers with different visible sets must not share a validator — otherwise a
// narrowed caller could present a wide caller's tag and be told its narrower cache is
// current.
func TestTheVisibleSetAndTheScopeFilterEachMoveTheETag(t *testing.T) {
	root := fixtureStore(t)
	wide, err := Build(root, "", store.Unrestricted())
	if err != nil {
		t.Fatal(err)
	}
	narrow, err := Build(root, "", store.VisibleScopeSet([]string{"beta-notes"}))
	if err != nil {
		t.Fatal(err)
	}
	filtered, err := Build(root, "alpha-notes", store.Unrestricted())
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range []struct {
		name string
		a, b Result
	}{
		{"a narrowed allowlist", wide, narrow},
		{"a ?scope= filter", wide, filtered},
		{"a narrowed allowlist against a filter", narrow, filtered},
	} {
		if pair.a.ETag == pair.b.ETag {
			t.Errorf("%s left the validator unchanged at %s", pair.name, pair.a.ETag)
		}
	}
	// The POSITIVE control: the same inputs twice give the same tag, so the three
	// inequalities above are about the inputs and not about the builder being
	// nondeterministic.
	again, err := Build(root, "", store.Unrestricted())
	if err != nil {
		t.Fatal(err)
	}
	if again.ETag != wide.ETag {
		t.Fatalf("two builds of one store disagreed: %s vs %s", wide.ETag, again.ETag)
	}
}

func TestETagMatchesIsTheDeclaredRule(t *testing.T) {
	const tag = `"sha256:abc"`
	cases := []struct {
		raw  string
		want bool
		why  string
	}{
		{"", false, "no header at all"},
		{tag, true, "the exact tag"},
		{"*", true, "RFC 9110 13.1.2's any-representation form"},
		{`"sha256:other"`, false, "a stale tag"},
		{`"sha256:other", "sha256:abc"`, true, "a LIST, matching on its second member"},
		{` ` + tag + ` `, true, "surrounding whitespace is stripped"},
		{`W/"sha256:abc"`, false,
			"a WEAK tag does NOT match: this server emits only strong ones, and the " +
				"narrowing costs a transfer rather than risking a stale cache"},
		{`"sha256:abc`, false, "a truncated tag is not the tag"},
		{`sha256:abc`, false, "an UNQUOTED value is not an entity-tag"},
		{"*x", false, "`*` is a whole value, never a prefix"},
	}
	for _, tc := range cases {
		if got := ETagMatches(tc.raw, tag); got != tc.want {
			t.Errorf("ETagMatches(%q) = %v, want %v — %s", tc.raw, got, tc.want, tc.why)
		}
	}
}
