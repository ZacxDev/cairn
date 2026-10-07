package ui

import (
	"fmt"
	"html"
	"maps"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/store"
)

// shortIDsInPairwise is the ORACLE: the first, O(N²) implementation, kept here only so the
// sorted-neighbour rewrite can be held to identical labels.
func shortIDsInPairwise(ids []string) map[string]string {
	out := make(map[string]string, len(ids))
	for _, id := range ids {
		n := min(8, len(id))
		for ; n < len(id); n++ {
			clash := false
			for _, other := range ids {
				if other != id && strings.HasPrefix(other, id[:n]) {
					clash = true
					break
				}
			}
			if !clash {
				break
			}
		}
		out[id] = id[:n]
	}
	return out
}

// adversarialIDs is a seeded mix built to make the neighbour shortcut wrong if it can be: long shared
// prefixes, ids that are prefixes of others, ids shorter than the floor, exact duplicates, and
// near-misses that differ at byte 8, 9 and deep inside.
func adversarialIDs(r *rand.Rand, n int) []string {
	alphabet := "ab-.0_Z"
	stems := []string{"", "s", "sess-000", "sess-0000000000", "s-shared-aaaa", "x"}
	var ids []string
	for len(ids) < n {
		stem := stems[r.Intn(len(stems))]
		tail := make([]byte, r.Intn(12))
		for i := range tail {
			tail[i] = alphabet[r.Intn(len(alphabet))]
		}
		id := stem + string(tail)
		if id == "" {
			continue
		}
		ids = append(ids, id)
		switch r.Intn(5) {
		case 0:
			ids = append(ids, id) // a duplicate
		case 1:
			ids = append(ids, id+string(alphabet[r.Intn(len(alphabet))])) // id is a prefix of this one
		}
	}
	return ids
}

// TestShortIDsInMatchesThePairwiseOracle holds the O(N log N) labelling to the O(N²) oracle's labels,
// exactly, over 400 seeded adversarial sets plus the hand-written edge cases.
func TestShortIDsInMatchesThePairwiseOracle(t *testing.T) {
	cases := [][]string{
		nil, {"only"}, {"dup", "dup"}, {"sess-0000000000000001", "sess-0000000000000002"},
		{"abcdefgh", "abcdefghi", "abcdefghij"}, {"abcdefgh", "abcdefgh", "abcdefghX"},
		{"s-pre-12345", "s-pre-123456789", "short"},
	}
	r := rand.New(rand.NewSource(20000101))
	for i := 0; i < 400; i++ {
		cases = append(cases, adversarialIDs(r, 1+r.Intn(60)))
	}
	differed := 0
	for _, ids := range cases {
		got, want := shortIDsIn(ids), shortIDsInPairwise(ids)
		if !maps.Equal(got, want) {
			differed++
			if differed <= 3 {
				t.Errorf("labels differ for %q:\n got  %v\n want %v", ids, got, want)
			}
		}
	}
	if differed > 0 {
		t.Errorf("%d of %d sets labelled differently from the oracle", differed, len(cases))
	}
	// INSTRUMENT: the sets really do exercise lengthening — some label is longer than the floor.
	lengthened := 0
	for _, ids := range cases {
		for id, l := range shortIDsInPairwise(ids) {
			if len(l) > 8 && len(l) < len(id) {
				lengthened++
			}
		}
	}
	if lengthened == 0 {
		t.Fatal("INSTRUMENT: no label was lengthened past 8 bytes, so the comparison never exercised the rule")
	}
}

// BenchmarkShortIDsIn measures both implementations at 4,000 and 16,000 ids (uuid-shaped, a quarter
// sharing a long prefix):
//
//	go test ./internal/ui/ -run '^$' -bench BenchmarkShortIDsIn -benchtime 5x
func BenchmarkShortIDsIn(b *testing.B) {
	for _, n := range []int{4000, 16000} {
		r := rand.New(rand.NewSource(int64(n)))
		ids := make([]string, n)
		for i := range ids {
			prefix := ""
			if i%4 == 0 {
				prefix = "s-shared-prefix-"
			}
			ids[i] = fmt.Sprintf("%s%08x-%04x-%04x", prefix, r.Uint32(), r.Intn(1<<16), r.Intn(1<<16))
		}
		for name, fn := range map[string]func([]string) map[string]string{
			"sorted-neighbour": shortIDsIn, "pairwise-oracle": shortIDsInPairwise} {
			b.Run(fmt.Sprintf("%s/n=%d", name, n), func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					fn(ids)
				}
			})
		}
	}
}

// TestShortIDsAreTheShortestUniquePrefixOfAtLeastEight pins the labelling rule as literal values,
// including a set whose result does not depend on input order.
func TestShortIDsAreTheShortestUniquePrefixOfAtLeastEight(t *testing.T) {
	for _, ids := range [][]string{
		{"s-shared-aaaa1", "s-shared-aaab2", "s-other-zzz9", "short", "s-pre-12345", "s-pre-123456789"},
		{"s-pre-123456789", "short", "s-other-zzz9", "s-shared-aaab2", "s-pre-12345", "s-shared-aaaa1"},
	} {
		got := shortIDsIn(ids)
		want := map[string]string{
			"s-shared-aaaa1":  "s-shared-aaaa", // collides through 12 bytes, unique at 13
			"s-shared-aaab2":  "s-shared-aaab",
			"s-other-zzz9":    "s-other-", // no collision: the floor of eight
			"short":           "short",    // shorter than the floor: in full
			"s-pre-12345":     "s-pre-12345",
			"s-pre-123456789": "s-pre-123456", // the other is ITS prefix: unique one byte past it
		}
		for id, w := range want {
			if got[id] != w {
				t.Errorf("label for %q = %q, want %q (input order %v)", id, got[id], w, ids)
			}
		}
	}
}

// TestCollidingSessionIdsRenderAsDistinguishableRows — the screenshot case: two ids sharing their
// first eight bytes rendered as two identical `sess-000` rows. RED with the label back to a fixed
// eight-byte `shortID`.
func TestCollidingSessionIdsRenderAsDistinguishableRows(t *testing.T) {
	readsA, _, _ := arcsWorld(t)
	root := t.TempDir()
	dir := filepath.Join(root, "alpha-notes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nservice: lamphouse\nscope: alpha-notes\n---\n\n" + store.NuanceHeading + "\n\n" +
		"- 2000-01-03: one [cairn: tallow-bot/s-shared-aaaa1]\n" +
		"- 2000-01-04: two [cairn: tallow-bot/s-shared-aaab2]\n" +
		"- 2000-01-05: three [cairn: tallow-bot/s-other-zzz9]\n"
	if err := os.WriteFile(filepath.Join(dir, "lamphouse.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := getAs(t, arcsServer(t, StoreSource{Root: root}, readsA), scopeTabURL(browseScopeA, TabSessions))
	if rec.Code != http.StatusOK {
		t.Fatalf("sessions tab answered %d", rec.Code)
	}
	labels := map[string]string{}
	for _, m := range regexp.MustCompile(`<span class="ref" title="([^"]+)"><a class="row-link" href="[^"]*">([^<]*)</a>`).
		FindAllStringSubmatch(rec.Body.String(), -1) {
		labels[m[1]] = m[2]
	}
	want := map[string]string{"s-shared-aaaa1": "s-shared-aaaa", "s-shared-aaab2": "s-shared-aaab", "s-other-zzz9": "s-other-"}
	if len(labels) != len(want) {
		t.Fatalf("INSTRUMENT: found %d session rows (%v), want %d", len(labels), labels, len(want))
	}
	for id, w := range want {
		if labels[id] != w {
			t.Errorf("row %q is labelled %q, want %q", id, labels[id], w)
		}
	}
}

// TestTheSessionPageExcerptDropsTheTrailerAndKeepsAMidProseToken — the session page shows a bullet
// WITHOUT its `[cairn: actor/session]` trailer (the header already says who wrote it), a `[cairn:`
// token in the middle of the prose is kept, and the ENTRY page still shows the trailer as written.
// RED with `bulletExcerpt` returning the body unstripped.
func TestTheSessionPageExcerptDropsTheTrailerAndKeepsAMidProseToken(t *testing.T) {
	readsA, _, _ := arcsWorld(t)
	root := t.TempDir()
	dir := filepath.Join(root, "alpha-notes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nservice: lamphouse\nscope: alpha-notes\n---\n\n" + store.NuanceHeading + "\n\n" +
		"- 2000-01-03: trimmed the wick [cairn: tallow-bot/s-lamp-0001]\n" +
		"- 2000-01-04: quoted [cairn: quoted-bot/s-quoted-9] in prose, then\n" +
		"  wrapped onto a second line [cairn: tallow-bot/s-lamp-0001]\n"
	if err := os.WriteFile(filepath.Join(dir, "lamphouse.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := arcsServer(t, StoreSource{Root: root}, readsA)
	page := getAs(t, srv, sessionURL("s-lamp-0001")).Body.String()
	// Each excerpt is CLAMPED (class `excerpt`, the stylesheet's `line-clamp`) and carries its whole
	// text — trailer still stripped — in `title=`, byte-equal to the visible text.
	titles := regexp.MustCompile(`<span class="title excerpt" title="([^"]*)">([^<]*)</span>`).FindAllStringSubmatch(page, -1)
	var got []string
	for _, m := range titles {
		if m[1] != m[2] {
			t.Errorf("an excerpt's title %q is not its full text %q", m[1], m[2])
		}
		got = append(got, html.UnescapeString(m[2]))
	}
	if !strings.Contains(stylesheet, ".excerpt {") || !strings.Contains(stylesheet, "-webkit-line-clamp: 3;") {
		t.Error("the served stylesheet has no three-line clamp for `.excerpt`")
	}
	want := []string{
		"quoted [cairn: quoted-bot/s-quoted-9] in prose, then wrapped onto a second line",
		"trimmed the wick",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("session-page excerpts %q, want %q", got, want)
	}
	// The ENTRY page is unchanged: the trailer is still on the bullet as the file has it.
	entry := getAs(t, srv, entryHref(browseScopeA, "lamphouse", false)).Body.String()
	if !strings.Contains(entry, "trimmed the wick [cairn: tallow-bot/s-lamp-0001]") {
		t.Errorf("the entry page lost the trailer — only the session page strips it:\n%s", entry)
	}
}
