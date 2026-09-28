package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	g "maragu.dev/gomponents"

	"github.com/ZacxDev/cairn/internal/store"
)

// A store with ONE entry whose `refs:` carries one ref of each shape the registry answers
// for, plus one it does not.
//
// ⚠ IT IS WRITTEN AS `refs:`, NOT `tasks:`, BECAUSE THIS SEAM IS WHAT THE KEY RENAME CHANGED
// AND A FIXTURE ON THE OLDER SPELLING WOULD PASS WITHOUT THE NEW ONE PARSING AT ALL.
func refStore(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "alpha-notes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("building the store: %v", err)
	}
	body := strings.Join([]string{
		"---",
		"service: runbook",
		"scope: alpha-notes",
		"refs:",
		"  - github:example-org/example-repo#428",
		"  - clickup:8600abcxyz",
		"  - tracker:662",
		"  - linear:ENG-441",
		"---",
		"",
		"## What it is",
		"",
		"The rollout runbook.",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "runbook.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	return root
}

func readTheOneEntry(t *testing.T, src StoreSource) Entry {
	t.Helper()
	index, err := store.LoadStore(src.Root, "recalled", store.ScopeSet{Unrestricted: true})
	if err != nil {
		t.Fatalf("loading the store: %v", err)
	}
	entries, err := index.Entries("alpha-notes")
	if err != nil {
		t.Fatalf("reading the scope: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("the fixture holds %d entries, want 1", len(entries))
	}
	item, err := src.readEntry("alpha-notes", entries[0])
	if err != nil {
		t.Fatalf("projecting the entry: %v", err)
	}
	return item
}

// TestResolvedRefsReachTheBrowserSurfaceAsLinks is criterion 5's positive half: the registry's
// URL reaches an `href`, and the LINK TEXT stays the ref the file carries.
//
// 🔴 WATCHED RED AT THE BASE COMMIT, where every one of these four rendered
// `class="task refused"`: `taskItem` handed the RAW ref to `safeHref`, which allowlists
// http(s), so `github:example-org/example-repo#428` matched no permitted scheme. That was the
// measured state of the criterion this closes — not an invariant nobody had violated.
func TestResolvedRefsReachTheBrowserSurfaceAsLinks(t *testing.T) {
	src := StoreSource{
		Root: refStore(t),
		RefBase: func(name string) string {
			if name == "CAIRN_REF_BASE_TRACKER" {
				return "https://tracker.invalid/tasks"
			}
			return ""
		},
	}
	entry := readTheOneEntry(t, src)

	var b strings.Builder
	if err := g.Group(g.Map(entry.Tasks, taskItem)).Render(&b); err != nil {
		t.Fatalf("rendering: %v", err)
	}
	out := b.String()

	linked := map[string]string{
		"github:example-org/example-repo#428": "https://github.com/example-org/example-repo/issues/428",
		"clickup:8600abcxyz":                  "https://app.clickup.com/t/8600abcxyz",
		"tracker:662":                         "https://tracker.invalid/tasks/662",
	}
	for raw, href := range linked {
		want := `<li class="task"><a href="` + href + `">` + escapeForTest(raw) + `</a></li>`
		if !strings.Contains(out, want) {
			t.Errorf("a resolved ref did not render as a link.\nwanted: %s\ngot:    %s", want, out)
		}
	}

	// 🔴 THE NON-VACUOUS OTHER HALF. An unregistered system resolves to no URL and must
	// render exactly as every ref did before the registry existed: visible and inert. A test
	// carrying only the three links above would pass on an implementation that linked
	// EVERYTHING, including a ref to a system nobody registered.
	if !strings.Contains(out, `<li class="task refused">linear:ENG-441</li>`) {
		t.Errorf("an unregistered system's ref did not render as inert text; got: %s", out)
	}
	if strings.Contains(out, `linear:ENG-441</a>`) {
		t.Errorf("an unregistered system's ref was LINKED; got: %s", out)
	}
}

// TestAJavascriptBaseIsRefusedRatherThanRendered is criterion 5's NEGATIVE CONTROL, and it is
// the one that had to be watched failing rather than assumed.
//
// 🔴 THE HAZARD IS REAL AND IT IS NEW. `store.RefURL` builds a self-hosted system's URL by
// appending a path to an OPERATOR-SUPPLIED base, and it deliberately does not scheme-check —
// its own comment says `safeHref` is the guard, and a second scheme check there would be the
// same predicate at two sites, wrong at one. So this test is what makes that comment a claim
// with something behind it: a base an operator can set, reaching a real render, refused.
//
// ⚠ IT WAS WATCHED FAILING. With `taskItem`'s `safeHref` call replaced by an unconditional
// `h.A(h.Href(ref.URL), …)`, this test reports
// `href="javascript:alert(document.domain)#/tasks/662"` on the page — the exact string the
// assertion below refuses. It is not a guard on a hazard that cannot occur.
func TestAJavascriptBaseIsRefusedRatherThanRendered(t *testing.T) {
	const hostileBase = `javascript:alert(document.domain)#`
	src := StoreSource{
		Root: refStore(t),
		RefBase: func(name string) string {
			if name == "CAIRN_REF_BASE_TRACKER" {
				return hostileBase
			}
			return ""
		},
	}
	entry := readTheOneEntry(t, src)

	// INSTRUMENT CONTROL, and it is the half that stops this test passing for the wrong
	// reason. If the registry declined to build the hostile URL at all — a scheme check
	// somebody added inside `store.RefURL` — the page below would carry no `javascript:`
	// whatever `safeHref` did, and the assertion would be measuring nothing. So the hostile
	// string must actually be sitting on the projected value.
	var hostile *EntryRef
	for i := range entry.Tasks {
		if entry.Tasks[i].Raw == "tracker:662" {
			hostile = &entry.Tasks[i]
		}
	}
	if hostile == nil {
		t.Fatal("INSTRUMENT CONTROL FAILED: the fixture's `tracker:662` ref did not survive projection")
	}
	if !strings.HasPrefix(hostile.URL, "javascript:") {
		t.Fatalf("INSTRUMENT CONTROL FAILED: the registry resolved the hostile base to %q, so this test "+
			"would pass whatever `safeHref` did. The refusal under test is `safeHref`'s; if a scheme check "+
			"moved into `store.RefURL`, that is a second predicate for the same rule and this control is "+
			"the thing that noticed.", hostile.URL)
	}

	var b strings.Builder
	if err := taskItem(*hostile).Render(&b); err != nil {
		t.Fatalf("rendering: %v", err)
	}
	out := b.String()

	if strings.Contains(strings.ToLower(out), "href=") {
		t.Errorf("a `javascript:`-producing template reached an href: %s", out)
	}
	if strings.Contains(strings.ToLower(out), "javascript:alert") && !strings.Contains(out, `class="task refused"`) {
		t.Errorf("the hostile URL rendered outside the refused branch: %s", out)
	}
	// It is REFUSED, and the ref's own text is still on the page — dropping it would hide a
	// fact the file carries.
	if !strings.Contains(out, `<li class="task refused">tracker:662</li>`) {
		t.Errorf("the refused ref did not render as inert text carrying the ref the file wrote: %s", out)
	}
}

// TestASelfHostedSystemWithNoBaseRendersExactlyAsBefore is the second point on the dimension
// the self-hosted base introduces — criterion 4's "an unknown system resolves to no URL and
// renders exactly as today", applied to the state an unconfigured deployment is actually in.
func TestASelfHostedSystemWithNoBaseRendersExactlyAsBefore(t *testing.T) {
	// `RefBase` nil, which is what `StoreSource` gets from any construction that does not
	// name it — every test in this package, and a `cmd/cairn-ui` before this change.
	src := StoreSource{Root: refStore(t)}
	entry := readTheOneEntry(t, src)
	for _, ref := range entry.Tasks {
		if ref.Raw == "tracker:662" {
			if ref.URL != "" {
				t.Fatalf("a self-hosted system resolved to %q with no base supplied", ref.URL)
			}
			var b strings.Builder
			if err := taskItem(ref).Render(&b); err != nil {
				t.Fatalf("rendering: %v", err)
			}
			if got := b.String(); got != `<li class="task refused">tracker:662</li>` {
				t.Fatalf("with no base supplied the ref rendered as %s", got)
			}
			return
		}
	}
	t.Fatal("the fixture's `tracker:662` ref did not survive projection")
}

// TestTheOlderKeyResolvesTheSameURLs pins that the accepted older spelling reaches the URL
// registry too: an entry on it gets the same links. The alias is PERMANENT — see
// `store.parseRefsField` — so this is a standing property of the page, not a window.
func TestTheOlderKeyResolvesTheSameURLs(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "alpha-notes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("building the store: %v", err)
	}
	body := "---\nservice: runbook\nscope: alpha-notes\ntasks: [github:example-org/example-repo#428]\n" +
		"---\n\n## What it is\n\nx\n"
	if err := os.WriteFile(filepath.Join(dir, "runbook.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	entry := readTheOneEntry(t, StoreSource{Root: root})
	if len(entry.Tasks) != 1 {
		t.Fatalf("the older key surfaced %d refs, want 1", len(entry.Tasks))
	}
	want := "https://github.com/example-org/example-repo/issues/428"
	if entry.Tasks[0].URL != want {
		t.Fatalf("the older key resolved to %q, want %q", entry.Tasks[0].URL, want)
	}
}
