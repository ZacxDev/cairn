package ui

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/authz"
	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/control/tokenfile"
	"github.com/ZacxDev/cairn/internal/identity"
)

// The entry page's `provenance` block, per view. The raw view drops the rows whose values
// are PARSED OUT OF THE FRONT MATTER (`scope`, `service:`) — the file's own text, directly
// below, shows them verbatim — and keeps the two the file's text CANNOT show: `file` (the
// name is not in the contents) and `updated` (the mtime).
//
// ⚠ THE FIXTURE'S THREE VALUES ARE PAIRWISE UNEQUAL, so a row cannot be satisfied by
// another row's value: scope `quillmoor-notes`, file `lanternfish.md`, `service:`
// `lanternfish`. The `service:` value IS a substring of the filename and cannot be made
// otherwise — the loader pins `service:` equal to the filename's slug — which is why every
// value below is compared by EQUALITY against its own `dd`, never by `strings.Contains`.
const (
	rawProvScope   = "quillmoor-notes"
	rawProvService = "lanternfish"
	rawProvFile    = rawProvService + ".md"
)

func rawProvenanceWorld(t *testing.T) (*Server, control.ID) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, rawProvScope)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := strings.Join([]string{
		"---",
		"service: " + rawProvService,
		"scope: " + rawProvScope,
		"---",
		"",
		"## What it is",
		"",
		"The lanternfish notes.",
		"",
	}, "\n")
	path := filepath.Join(dir, rawProvFile)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// A real mtime, so the `updated` row is rendered at all (it is omitted when the stat
	// failed) — without it, "updated is kept on the raw view" would be unfalsifiable.
	at := recencyNow.Add(-5 * time.Minute)
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
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
	var id control.ID
	for _, n := range auth.NamedScopes(control.VerbRead) {
		if n.Name == rawProvScope {
			id = n.ID
		}
	}
	if id == "" {
		t.Fatalf("the token-file world names no %q, so every assertion below is about nothing", rawProvScope)
	}
	return recencyServer(t, root, identity.Identity{Principal: p, Auth: auth}), id
}

var (
	provenanceDL = regexp.MustCompile(`(?s)<dl class="provenance">(.*?)</dl>`)
	provenanceKV = regexp.MustCompile(`(?s)<dt class="prov-key">(.*?)</dt><dd class="prov-val">(.*?)</dd>`)
)

// provenanceRows returns the ONE provenance block's rows, in order, as key → value. It
// fails on zero or several blocks: a page with no block would otherwise read as "every row
// absent" and pass the raw-view half for the wrong reason.
func provenanceRows(t *testing.T, view, body string) ([]string, map[string]string) {
	t.Helper()
	blocks := provenanceDL.FindAllStringSubmatch(body, -1)
	if len(blocks) != 1 {
		t.Fatalf("the %s view carries %d provenance blocks, want exactly 1", view, len(blocks))
	}
	var keys []string
	vals := map[string]string{}
	for _, kv := range provenanceKV.FindAllStringSubmatch(blocks[0][1], -1) {
		keys = append(keys, kv[1])
		vals[kv[1]] = kv[2]
	}
	return keys, vals
}

// TestTheRawViewDropsTheFrontMatterProvenanceRowsAndKeepsTheRest pins the row SET of each
// view, structurally (the `dt` keys of the one `dl.provenance`), as a pair: the rendered
// half is what catches deleting the rows from BOTH views, which the raw half alone would
// pass.
func TestTheRawViewDropsTheFrontMatterProvenanceRowsAndKeepsTheRest(t *testing.T) {
	srv, id := rawProvenanceWorld(t)
	ref := rawProvService

	for _, tc := range []struct {
		view     string
		raw      bool
		wantKeys []string
	}{
		{"rendered", false, []string{"scope", "file", "service:", "updated"}},
		{"raw", true, []string{"file", "updated"}},
	} {
		rec := getAs(t, srv, entryHref(id, ref, tc.raw))
		if rec.Code != http.StatusOK {
			t.Fatalf("the %s view answered %d, want 200: %s", tc.view, rec.Code, rec.Body.String())
		}
		keys, vals := provenanceRows(t, tc.view, rec.Body.String())
		if !slices.Equal(keys, tc.wantKeys) {
			t.Errorf("the %s view's provenance rows are %q, want %q", tc.view, keys, tc.wantKeys)
		}
		// Each surviving row carries ITS OWN value, so a row cannot be kept in name while
		// showing a neighbour's.
		for k, want := range map[string]string{"scope": rawProvScope, "file": rawProvFile, "service:": rawProvService} {
			if got, ok := vals[k]; ok && got != want {
				t.Errorf("the %s view's %q row reads %q, want %q", tc.view, k, got, want)
			}
		}
		if !strings.Contains(vals["updated"], `<time class="updated"`) {
			t.Errorf("the %s view's updated row is %q, want a <time> element", tc.view, vals["updated"])
		}
	}
}
