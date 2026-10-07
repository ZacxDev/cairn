package ui

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/store"
)

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
	titles := regexp.MustCompile(`<span class="title">([^<]*)</span>`).FindAllStringSubmatch(page, -1)
	var got []string
	for _, m := range titles {
		got = append(got, m[1])
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
