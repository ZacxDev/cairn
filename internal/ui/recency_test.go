package ui

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/authz"
	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/control/tokenfile"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/store"
)

// recencyNow is the render clock these guards inject through `Config.Now`. Year 2000, which is
// what `tests/leakscan.py` allows; every name below is invented.
var recencyNow = time.Date(2000, 6, 1, 12, 0, 0, 0, time.UTC)

// recencyFile is one entry file and how long before `recencyNow` it was last modified.
type recencyFile struct {
	scope, ref string
	age        time.Duration
}

// recencyFiles is the world. 🔴 ITS MTIMES ARE PAIRWISE DISTINCT EXCEPT FOR ONE DELIBERATE TIE
// PER LEVEL, AND THEIR ORDER IS NEITHER ALPHABETICAL NOR REVERSE-ALPHABETICAL, so no sort but the
// right one can produce the expected order:
//
//	orchard-notes rows, newest first:  birch 5m · dogwood 5m (TIE → by ref) · alder 3h · cedar 2d · elm 40d
//	alphabetical would be:             alder · birch · cedar · dogwood · elm
//
//	cards, by each scope's NEWEST entry:  orchard 5m · beta 6h · kestrel 6h (TIE → by name) · alpha 2d
//	alphabetical would be:                alpha · beta · kestrel · orchard
//
// ⚠ `beta-notes`' NEWEST entry is NOT its alphabetically-first one (`aster` is 40d old, `zinnia`
// 6h), so a card dated by `Entries[0]` — the index's first entry — would put beta LAST. And the
// one tag every orchard entry carries (`timber`) gives the tag listing something to list.
var recencyFiles = []recencyFile{
	{"orchard-notes", "alder", 3 * time.Hour},
	{"orchard-notes", "birch", 5 * time.Minute},
	{"orchard-notes", "cedar", 2 * 24 * time.Hour},
	{"orchard-notes", "dogwood", 5 * time.Minute},
	{"orchard-notes", "elm", 40 * 24 * time.Hour},
	{"alpha-notes", "anvil", 2 * 24 * time.Hour},
	{"beta-notes", "aster", 40 * 24 * time.Hour},
	{"beta-notes", "zinnia", 6 * time.Hour},
	{"kestrel-notes", "kite", 6 * time.Hour},
}

// recencyWorld writes `recencyFiles` to a store, stamps each file's mtime, and returns the store
// root, an identity that may read every scope in it (a bare token-file row) and the id of
// `orchard-notes`.
func recencyWorld(t *testing.T) (string, identity.Identity, control.ID) {
	t.Helper()
	root := t.TempDir()
	for _, f := range recencyFiles {
		dir := filepath.Join(root, f.scope)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := strings.Join([]string{
			"---",
			"service: " + f.ref,
			"scope: " + f.scope,
			"aliases:",
			"  - " + f.ref + "-alias",
			"refs:",
			"  - github:example-org/example-repo#7",
			"tags:",
			"  - timber",
			"---",
			"",
			"## What it is",
			"",
			"The " + f.ref + " notes.",
			"",
			store.NuanceHeading,
			"",
			"- 2000-05-01 the first history note",
			"- 2000-05-02 the second history note",
			"",
		}, "\n")
		path := filepath.Join(dir, f.ref+".md")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		at := recencyNow.Add(-f.age)
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}
	const token = "fixture-token-not-a-real-credential"
	src := tokenfile.Source{
		StoreRoot: root,
		Records:   func() []authz.TokenRecord { return []authz.TokenRecord{authz.LegacyRecord(token)} },
		Now:       func() time.Time { return recencyNow },
	}
	m, err := src.Model(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	p, auth, err := control.Authenticate(m, token)
	if err != nil {
		t.Fatal(err)
	}
	var orchard control.ID
	for _, n := range auth.NamedScopes(control.VerbRead) {
		if n.Name == "orchard-notes" {
			orchard = n.ID
		}
	}
	if orchard == "" {
		t.Fatal("the token-file world names no `orchard-notes`, so every assertion below is about nothing")
	}
	return root, identity.Identity{Principal: p, Auth: auth}, orchard
}

// recencyServer is the REAL `StoreSource` behind the REAL handlers, with the render clock
// injected — so the mtimes come off the files through the same reader recall uses.
func recencyServer(t *testing.T, root string, id identity.Identity) *Server {
	t.Helper()
	cfg := testConfig(t, staticAuth{id})
	cfg.Source = StoreSource{Root: root}
	cfg.Now = func() time.Time { return recencyNow }
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

// positionsOf is where each needle FIRST appears in body, failing on any that is absent — an
// absent needle would otherwise read as "position -1", which sorts first and passes.
func positionsOf(t *testing.T, body string, needles []string) []int {
	t.Helper()
	out := make([]int, len(needles))
	for i, n := range needles {
		out[i] = strings.Index(body, n)
		if out[i] < 0 {
			t.Fatalf("%q is not on the page at all, so its position cannot be compared", n)
		}
	}
	return out
}

func assertInOrder(t *testing.T, what string, body string, needles []string) {
	t.Helper()
	pos := positionsOf(t, body, needles)
	for i := 1; i < len(pos); i++ {
		if pos[i-1] >= pos[i] {
			t.Errorf("%s: %q does not come before %q (want the order %q)", what, needles[i-1], needles[i], needles)
		}
	}
}

// TestTheScopePageListsEntriesNewestFirstWithTiesByRef: newest file mtime first, the exact tie
// broken by ref — recall's own order (`report.NewerFirst`).
func TestTheScopePageListsEntriesNewestFirstWithTiesByRef(t *testing.T) {
	root, id, orchard := recencyWorld(t)
	rec := getAs(t, recencyServer(t, root, id), ScopePath+"?"+QueryID+"="+string(orchard))
	if rec.Code != http.StatusOK {
		t.Fatalf("the scope page answered %d: %s", rec.Code, rec.Body.String())
	}
	var links []string
	for _, ref := range []string{"birch", "dogwood", "alder", "cedar", "elm"} {
		links = append(links, `href="`+strings.ReplaceAll(entryHref(orchard, ref, false), "&", "&amp;")+`"`)
	}
	assertInOrder(t, "the scope page's rows", rec.Body.String(), links)
}

// TestTheRootPageOrdersScopeCardsByTheirNewestEntry: the card whose NEWEST entry is newest comes
// first, an exact tie by scope name.
func TestTheRootPageOrdersScopeCardsByTheirNewestEntry(t *testing.T) {
	root, id, _ := recencyWorld(t)
	rec := getAs(t, recencyServer(t, root, id), RootPath)
	if rec.Code != http.StatusOK {
		t.Fatalf("the root page answered %d: %s", rec.Code, rec.Body.String())
	}
	assertInOrder(t, "the root page's cards", rec.Body.String(), []string{
		">orchard-notes</a>", ">beta-notes</a>", ">kestrel-notes</a>", ">alpha-notes</a>",
	})
	// The card's preview lists the scope's NEWEST refs, in the scope page's own order.
	body := rec.Body.String()
	card := body[strings.Index(body, ">orchard-notes</a>"):]
	card = card[:strings.Index(card, "</section>")]
	assertInOrder(t, "orchard's card preview", card, []string{
		`<span class="ref">birch</span>`, `<span class="ref">dogwood</span>`, `<span class="ref">alder</span>`,
	})
}

// TestEveryTimestampIsRelativeToTheInjectedClockWithItsInstantPinned pins the `<time>` element
// LITERALLY — the `datetime` value, the absolute `title`, and the bucketed text — on the card,
// the row and the entry page's provenance block.
func TestEveryTimestampIsRelativeToTheInjectedClockWithItsInstantPinned(t *testing.T) {
	root, id, orchard := recencyWorld(t)
	srv := recencyServer(t, root, id)
	const birch = `<time class="updated" datetime="2000-06-01T11:55:00Z" title="2000-06-01 11:55:00 UTC">5m ago</time>`

	rootBody := getAs(t, srv, RootPath).Body.String()
	scopeBody := getAs(t, srv, ScopePath+"?"+QueryID+"="+string(orchard)).Body.String()
	entryBody := getAs(t, srv, entryHref(orchard, "birch", false)).Body.String()

	for where, want := range map[string][]string{
		"root card": {
			birch, // orchard's newest is birch, 5m
			`<time class="updated" datetime="2000-05-30T12:00:00Z" title="2000-05-30 12:00:00 UTC">2d ago</time>`,
			`<time class="updated" datetime="2000-06-01T06:00:00Z" title="2000-06-01 06:00:00 UTC">6h ago</time>`,
		},
		"scope rows": {
			birch,
			`<time class="updated" datetime="2000-06-01T09:00:00Z" title="2000-06-01 09:00:00 UTC">3h ago</time>`,
			// past 30 days the text is the DATE, not a distance
			`<time class="updated" datetime="2000-04-22T12:00:00Z" title="2000-04-22 12:00:00 UTC">2000-04-22</time>`,
		},
		"entry provenance": {
			`<dt class="prov-key">updated</dt><dd class="prov-val">` + birch + `</dd>`,
		},
	} {
		body := map[string]string{"root card": rootBody, "scope rows": scopeBody, "entry provenance": entryBody}[where]
		for _, w := range want {
			if !strings.Contains(body, w) {
				t.Errorf("%s: %s is not on the page", where, w)
			}
		}
	}
	// The scope page carries one timestamp per row and the root one per card — not one per page.
	if n := strings.Count(scopeBody, `<time class="updated"`); n != 5 {
		t.Errorf("the scope page carries %d timestamps, want one per row (5)", n)
	}
	if n := strings.Count(rootBody, `<time class="updated"`); n != 4 {
		t.Errorf("the root page carries %d timestamps, want one per card (4)", n)
	}
}
