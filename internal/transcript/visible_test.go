package transcript

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/store"
	"github.com/ZacxDev/cairn/internal/transcript/archive"
	"github.com/ZacxDev/cairn/internal/transcript/transcripttest"
)

// 🔴 THE VISIBILITY RELATIONSHIPS, OVER ONE STORE, ONE CONTROL MODEL AND ONE ARCHIVE (the S4 test
// plan of `claudedocs/plan-cairn-plugins.md`; closing-condition clauses d–g). Everything is
// SYNTHETIC: scopes `alpha-notes`…`delta-notes`, sessions `s-…`, the host `host-a`, year 2000.
//
// The principals, by what `control.Resolve` gives each (one project per scope):
//
//	owner  reads alpha            — the capture owner, the uploader of every session below
//	every  reads alpha beta gamma — R: every scope any session here touched
//	one    reads alpha            — P: ONE written scope
//	wrote  reads alpha beta       — Q: every WRITTEN scope, not the READ one
//	none   reads delta            — a stranger
//
// The sessions, by their visibility set `V` (pairwise distinct shapes):
//
//	s-full-01  wrote alpha, beta; READ gamma (a rendered recall in a tool result)   V = {alpha, beta, gamma}
//	s-solo-02  no write, no read                                                    V = {}
//	s-star-03  wrote alpha; a tool input names the program, no ledger               V = {alpha, *}
//	s-only-04  wrote alpha                                                          V = {alpha}
//	s-tree-05  (opencode) root wrote alpha; its CHILD session wrote beta             V = {alpha, beta}
//	s-decl-06  wrote alpha; the agent DECLARED beta                                 V = {alpha, beta}
//	s-lost-07  wrote alpha; READ a scope no model holds (`ghost-notes`)             V = {alpha, ghost-notes}

var visClock = time.Date(2000, 1, 2, 3, 4, 5, 0, time.UTC)

type visWorld struct {
	store   string
	arc     *archive.Archive
	viewers map[string]Viewer
	model   control.Model
	ids     map[string]control.ID
}

func visID(prefix, name string) control.ID { return control.DerivedID(prefix, "vis-"+name) }

// visModel builds the control model. `rename` maps a scope's STORE name to the display name the
// model holds for it — the "renamed after the write" case; nil renames nothing.
func visModel(t *testing.T, rename map[string]string) control.Model {
	t.Helper()
	reads := map[string][]string{
		"owner": {"alpha-notes"}, "every": {"alpha-notes", "beta-notes", "gamma-notes"},
		"one": {"alpha-notes"}, "wrote": {"alpha-notes", "beta-notes"}, "none": {"delta-notes"},
	}
	var events []control.Event
	for i, user := range []string{"owner", "every", "one", "wrote", "none"} {
		events = append(events, control.Event{Kind: control.EventUserCreated, At: visClock, UserID: visID(control.PrefixUser, user),
			Provider: "fixture-provider", Subject: "00000000-0000-4000-8000-00000000007" + string(rune('0'+i)),
			Email: user + "@notes.example.invalid"})
	}
	for _, scope := range []string{"alpha-notes", "beta-notes", "gamma-notes", "delta-notes"} {
		project := visID(control.PrefixProject, scope)
		// The project's creator is a member of nothing else: `none` founds every project, and
		// membership below is what confers each read.
		events = append(events,
			control.Event{Kind: control.EventProjectCreated, At: visClock, ProjectID: project, Name: "p-" + scope,
				UserID: visID(control.PrefixUser, "none")})
		display := scope
		if to, ok := rename[scope]; ok {
			display = to
		}
		events = append(events, control.Event{Kind: control.EventScopeCreated, At: visClock,
			ScopeID: visID(control.PrefixScope, scope), DisplayName: display, ProjectID: project})
		for user, scopes := range reads {
			if slices.Contains(scopes, scope) {
				events = append(events, control.Event{Kind: control.EventMemberSet, At: visClock, ProjectID: project,
					UserID: visID(control.PrefixUser, user), Role: control.RoleOwner})
			}
		}
	}
	// `EventMemberSet` order follows map iteration; the model is the same whatever the order.
	m, err := control.Replay(events)
	if err != nil {
		t.Fatalf("building the visibility world: %v", err)
	}
	return m
}

func visViewer(t *testing.T, m control.Model, user string) Viewer {
	t.Helper()
	p, known := m.PrincipalFor(control.KindUser, visID(control.PrefixUser, user))
	if !known {
		t.Fatalf("no principal %s", user)
	}
	return Viewer{Principal: p, Auth: control.Resolve(m, p)}
}

func recallHeader(scope string) string {
	return "subsystem-recall: status=recalled scope=" + scope + " entries=1\n- synthetic"
}

func visBuild(t *testing.T) visWorld {
	t.Helper()
	root := t.TempDir()
	put := func(scope, service, nuance string) {
		dir := filepath.Join(root, scope)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := "---\nservice: " + service + "\nscope: " + scope + "\n---\n\n## What it is\n\nsynthetic.\n\n" +
			store.NuanceHeading + "\n\n" + nuance
		if err := os.WriteFile(filepath.Join(dir, service+".md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put("alpha-notes", "kettle", "- 2000-01-03: one [cairn: fixture-bot/s-full-01]\n"+
		"- 2000-01-03: two [cairn: fixture-bot/s-star-03]\n- 2000-01-03: three [cairn: fixture-bot/s-only-04]\n"+
		"- 2000-01-03: four [cairn: fixture-bot/s-tree-05]\n- 2000-01-03: five [cairn: fixture-bot/s-decl-06]\n"+
		"- 2000-01-03: six [cairn: fixture-bot/s-lost-07]\n")
	put("beta-notes", "ladle", "- 2000-01-04: seven [cairn: fixture-bot/s-full-01]\n"+
		"- 2000-01-04: eight [cairn: fixture-bot/ses_child05]\n")
	put("gamma-notes", "spoon", "- 2000-01-05: unsigned\n")
	put("delta-notes", "fork", "- 2000-01-06: unsigned\n")

	m := visModel(t, nil)
	w := visWorld{store: root, arc: transcripttest.Open(t), model: m, viewers: map[string]Viewer{}}
	for _, user := range []string{"owner", "every", "one", "wrote", "none"} {
		w.viewers[user] = visViewer(t, m, user)
	}
	owner := string(control.KindUser) + ":" + string(visID(control.PrefixUser, "owner"))
	plain := `{"type":"user","message":{"role":"user","content":"please note the gadget"}}`
	// The read is made through a variable, so no tool input names the program (F1 stays quiet) and
	// the ONLY evidence of it is the rendered header in the result.
	call := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_vis0001",` +
		`"name":"Bash","input":{"command":"\"$BIN\" recall --scope gamma-notes"}}]}}`
	result := func(scope string) string {
		return `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_vis0001",` +
			`"content":` + quoteJSON(recallHeader(scope)) + `}]}}`
	}
	names := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_vis0002",` +
		`"name":"Bash","input":{"command":"cairn recall"}}]}}`
	sess := func(rootID string) transcripttest.Session { return transcripttest.Session{Root: rootID, Owner: owner} }
	transcripttest.Append(t, w.arc, sess("s-full-01"), "main", []string{plain, call, result("gamma-notes")})
	transcripttest.Append(t, w.arc, sess("s-solo-02"), "main", []string{plain})
	transcripttest.Append(t, w.arc, sess("s-star-03"), "main", []string{plain, names})
	transcripttest.Append(t, w.arc, sess("s-only-04"), "main", []string{plain})
	tree := transcripttest.Session{Root: "s-tree-05", Owner: owner, Runtime: "opencode"}
	transcripttest.Append(t, w.arc, tree, "main", []string{`{"id":"s-tree-05"}`}, "info@000000000001")
	transcripttest.Append(t, w.arc, tree, "child:ses_child05", []string{`{"id":"ses_child05"}`}, "info@000000000002")
	decl := sess("s-decl-06")
	decl.Declared = []string{"beta-notes"}
	transcripttest.Append(t, w.arc, decl, "main", []string{plain})
	transcripttest.Append(t, w.arc, sess("s-lost-07"), "main", []string{plain, call, result("ghost-notes")})
	return w
}

func quoteJSON(s string) string {
	out := []byte{'"'}
	for _, c := range []byte(s) {
		switch c {
		case '"', '\\':
			out = append(out, '\\', c)
		case '\n':
			out = append(out, '\\', 'n')
		default:
			out = append(out, c)
		}
	}
	return string(append(out, '"'))
}

func (w visWorld) sees(t *testing.T, viewer Viewer, session string) bool {
	t.Helper()
	view, err := Store{Archive: w.arc, StoreRoot: w.store}.View(viewer, session)
	if err != nil {
		t.Fatalf("View(%s): %v", session, err)
	}
	return view != nil
}

// TestTranscriptVisibilityIsTheOwnerOrAReaderOfEveryScope is the matrix: each session's `V` is what
// the header above says (INSTRUMENT CONTROL — read off the stored meta and a whole-store walk, not
// assumed), and each viewer sees exactly the sessions decision 4 admits.
//
// What each row is the control for:
//   - s-full-01 / `one`: clause (e) — a reader of ONE written scope is refused. RED when the written
//     set is computed over the viewer's own readable scopes (`transcript-written-set-from-viewer-scopes`)
//     and when "every" becomes "any" (`transcript-any-scope-not-every-scope`).
//   - s-full-01 / `wrote`: clause (f) — every WRITTEN scope is not enough when the session READ
//     another. RED when the read half is dropped (`transcript-visibility-set-ignores-reads`).
//   - s-solo-02 / everyone but the owner: clause (g) — an empty `V` is owner-only. RED when the
//     empty set is vacuously readable (`transcript-empty-set-visible`).
//   - s-star-03 / `every`: `*` hides from everyone but the owner (`transcript-star-sentinel-readable`).
//   - s-tree-05 / `one`: a child session's write counts toward its root
//     (`transcript-child-visible-without-root`).
//   - s-lost-07 / `every`: a name no model holds is unreadable, never skipped
//     (`transcript-unknown-scope-name-readable`).
//   - every refusal: RED when the predicate is not asked (`transcript-section-without-predicate`).
func TestTranscriptVisibilityIsTheOwnerOrAReaderOfEveryScope(t *testing.T) {
	w := visBuild(t)
	wantV := map[string][]string{
		"s-full-01": {"alpha-notes", "beta-notes", "gamma-notes"},
		"s-solo-02": {},
		"s-star-03": {"*", "alpha-notes"},
		"s-only-04": {"alpha-notes"},
		"s-tree-05": {"alpha-notes", "beta-notes"},
		"s-decl-06": {"alpha-notes", "beta-notes"},
		"s-lost-07": {"alpha-notes", "ghost-notes"},
	}
	for session, want := range wantV {
		meta, err := w.arc.ReadMeta(session)
		if err != nil || meta == nil {
			t.Fatalf("%s is not stored (%v)", session, err)
		}
		written, err := WrittenScopes(w.store, append([]string{meta.Root}, meta.Children...))
		if err != nil {
			t.Fatal(err)
		}
		if got := VisibilitySet(meta, written); !slices.Equal(got, want) && !(len(got) == 0 && len(want) == 0) {
			t.Fatalf("INSTRUMENT CONTROL: V(%s) = %v, want %v — the matrix below would measure a different world", session, got, want)
		}
	}
	want := map[string][]string{ // session → the viewers who see it
		"s-full-01": {"every", "owner"},
		"s-solo-02": {"owner"},
		"s-star-03": {"owner"},
		"s-only-04": {"every", "one", "owner", "wrote"},
		"s-tree-05": {"every", "owner", "wrote"},
		"s-decl-06": {"every", "owner", "wrote"},
		"s-lost-07": {"owner"},
		"s-none-99": nil, // never stored
	}
	for session, viewers := range want {
		var got []string
		for name, viewer := range w.viewers {
			if w.sees(t, viewer, session) {
				got = append(got, name)
			}
		}
		slices.Sort(got)
		if !slices.Equal(got, viewers) {
			t.Errorf("%s is visible to %v, want exactly %v", session, got, viewers)
		}
	}
}

// TestStoreViewSkipsTheWalkOnlyWhereItCannotMatter: [Store.View] computes `V` lazily — no store
// walk for the owner, none for a viewer the stored half already refuses. For every viewer and
// session its answer must equal [Visible] over the FULL `V`, or the shortcut changed the predicate.
func TestStoreViewSkipsTheWalkOnlyWhereItCannotMatter(t *testing.T) {
	w := visBuild(t)
	roots, err := w.arc.Roots()
	if err != nil || len(roots) != 7 {
		t.Fatalf("the world holds %d stored sessions (%v), want 7", len(roots), err)
	}
	checked, admitted := 0, 0
	for _, session := range roots {
		meta, _ := w.arc.ReadMeta(session)
		written, err := WrittenScopes(w.store, append([]string{meta.Root}, meta.Children...))
		if err != nil {
			t.Fatal(err)
		}
		full := VisibilitySet(meta, written)
		for name, viewer := range w.viewers {
			checked++
			want := Visible(viewer, meta, full)
			if want {
				admitted++
			}
			if got := w.sees(t, viewer, session); got != want {
				t.Errorf("%s / %s: View says %v, Visible over the full V %v says %v", session, name, got, full, want)
			}
		}
	}
	if checked != 35 || admitted == 0 || admitted == checked {
		t.Fatalf("the comparison covered %d pairs with %d admitted — it needs both answers to mean anything", checked, admitted)
	}
}

// TestVisibleArms pins the predicate's arms on literal inputs, each a case the matrix cannot reach
// through a real authority alone.
func TestVisibleArms(t *testing.T) {
	m := visModel(t, nil)
	owner, every, one := visViewer(t, m, "owner"), visViewer(t, m, "every"), visViewer(t, m, "one")
	ownerKey := string(control.KindUser) + ":" + string(owner.Principal.ID)
	meta := &archive.Meta{Root: "s-0001", Owner: ownerKey}
	alpha, beta := visID(control.PrefixScope, "alpha-notes"), visID(control.PrefixScope, "beta-notes")

	// 🔴 THE OWNER IS A `(Kind, ID)`, NEVER A DISPLAY NAME. A principal whose DISPLAY is the owner's
	// id — and whose own id is another's — is not the owner; the owner under any display is.
	// RED under `transcript-owner-by-display-not-id`.
	impostor := one
	impostor.Principal.Display = string(owner.Principal.ID)
	if Visible(impostor, meta, nil) {
		t.Error("a principal whose DISPLAY NAME is the owner's id passed the owner arm")
	}
	renamed := owner
	renamed.Principal.Display = "somebody else entirely"
	if !Visible(renamed, meta, nil) {
		t.Error("the owner, under another display name, is refused its own session")
	}
	project := owner
	project.Principal.Kind = control.KindProject
	if Visible(project, meta, nil) {
		t.Error("a PROJECT principal with the owner's id passed the owner arm: the key is (Kind, ID)")
	}

	// 🔴 A NARROWED CREDENTIAL OF THE OWNER GETS THE SCOPE ARM ONLY: it sees a session whose `V` its
	// narrowing covers, and never an owner-only one. RED under `transcript-narrowed-owner-arm`.
	narrowed := owner
	narrowed.Auth = control.Narrow(owner.Auth, []control.ID{alpha}, nil)
	if !narrowed.Auth.Narrowed() {
		t.Fatal("INSTRUMENT CONTROL: the narrowed authority does not say it is narrowed")
	}
	if Visible(narrowed, meta, nil) {
		t.Error("a NARROWED credential of the owner sees an owner-only session (empty V)")
	}
	if Visible(narrowed, meta, []string{"alpha-notes", Star}) {
		t.Error("a NARROWED credential of the owner sees a session whose V holds `*`")
	}
	if !Visible(narrowed, meta, []string{"alpha-notes"}) {
		t.Error("a NARROWED credential of the owner is refused a session whose every scope it was narrowed to")
	}
	// A narrowing that covers nothing of V: the SAME principal, refused.
	if elsewhere := (Viewer{Principal: every.Principal, Auth: control.Narrow(every.Auth, []control.ID{beta}, nil)}); Visible(elsewhere, &archive.Meta{Owner: "user:nobody"}, []string{"alpha-notes"}) {
		t.Error("a credential narrowed to beta-notes sees a session that touched alpha-notes")
	}

	// A scope the authority HOLDS but may not READ (a credential narrowed to `write`) is not read.
	writeOnly := control.NewVerbSet(control.VerbWrite)
	writer := Viewer{Principal: every.Principal, Auth: control.Narrow(every.Auth, nil, &writeOnly)}
	other := &archive.Meta{Root: "s-0002", Owner: "user:nobody"}
	if !writer.holds("alpha-notes") || Visible(writer, other, []string{"alpha-notes"}) {
		t.Errorf("a write-only credential (holds alpha: %v) sees a session that touched alpha-notes", writer.holds("alpha-notes"))
	}
	// POSITIVE CONTROL: the un-narrowed reader of the same scopes does.
	if !Visible(every, other, []string{"alpha-notes", "beta-notes", "gamma-notes"}) {
		t.Fatal("POSITIVE CONTROL FAILED: the reader of every scope is refused")
	}
	if Visible(every, nil, []string{"alpha-notes"}) {
		t.Error("a nil meta — no stored session — is visible")
	}
}

// TestAScopeRenamedAfterTheWriteIsUnreadableUntilItIsRenamedBack: `V` holds STORE names. When the
// control model no longer holds `gamma-notes` under that name, the reader of every scope loses the
// transcript that read it (the name is unknown to its authority) — and gets it back when the name
// returns, which is the control that the refusal was the rename's doing.
func TestAScopeRenamedAfterTheWriteIsUnreadableUntilItIsRenamedBack(t *testing.T) {
	w := visBuild(t)
	if !w.sees(t, w.viewers["every"], "s-full-01") {
		t.Fatal("CONTROL: before the rename the reader of every scope does not see s-full-01")
	}
	renamed := visViewer(t, visModel(t, map[string]string{"gamma-notes": "gamma-renamed"}), "every")
	if w.sees(t, renamed, "s-full-01") {
		t.Error("after gamma-notes was renamed, a non-owner still sees the transcript that read it")
	}
	if !w.sees(t, visViewer(t, visModel(t, map[string]string{"gamma-notes": "gamma-renamed"}), "owner"), "s-full-01") {
		t.Error("the rename hid the transcript from its OWNER")
	}
	if !w.sees(t, visViewer(t, visModel(t, nil), "every"), "s-full-01") {
		t.Error("renaming the scope back did not restore the reader's view")
	}
}

// TestAnUnscannedEntryPutsItsScopeInV: a scope holding an entry the walk cannot scan is in `V` — a
// trailer there would be invisible, so the walk may not read "nothing found" as "nothing written".
func TestAnUnscannedEntryPutsItsScopeInV(t *testing.T) {
	w := visBuild(t)
	if !w.sees(t, w.viewers["one"], "s-only-04") {
		t.Fatal("CONTROL: before the damage the reader of alpha-notes does not see s-only-04")
	}
	// A file the index REJECTS (no front matter), in a scope `one` cannot read.
	if err := os.WriteFile(filepath.Join(w.store, "delta-notes", "broken.md"), []byte("no front matter here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	written, err := WrittenScopes(w.store, []string{"s-only-04"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(written.Scopes, []string{"alpha-notes"}) || !slices.Equal(written.Unscanned, []string{"delta-notes"}) {
		t.Fatalf("the walk found written=%v unscanned=%v, want [alpha-notes] and [delta-notes]", written.Scopes, written.Unscanned)
	}
	if w.sees(t, w.viewers["one"], "s-only-04") {
		t.Error("a viewer who cannot read the unscanned scope still sees the transcript")
	}
	if !w.sees(t, w.viewers["owner"], "s-only-04") {
		t.Error("the damage hid the transcript from its owner")
	}
}

// TestNoArchiveMeansNoView: a deployment without a transcript store answers nil for every id.
func TestNoArchiveMeansNoView(t *testing.T) {
	w := visBuild(t)
	view, err := Store{StoreRoot: w.store}.View(w.viewers["owner"], "s-full-01")
	if view != nil || err != nil {
		t.Fatalf("with no archive View answered %v, %v", view, err)
	}
	if view, err := (Store{Archive: w.arc, StoreRoot: w.store}).View(w.viewers["owner"], "not a session id"); view != nil || err != nil {
		t.Fatalf("an id the grammar cannot produce answered %v, %v", view, err)
	}
}
