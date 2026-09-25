package store

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"syscall"
	"testing"
)

func TestNormalizeRef(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Alpha_Notes", "alpha-notes"},
		{"  spaced  ", "spaced"},
		{"a--b", "a-b"},
		{"___", ""},
		{"--", ""},
		{"", ""},
		// 🔴 `.` SURVIVES. It is inside the character class, and that is what lets
		// kind qualification (`<slug>.<kind>`) and a dotted slug work at all.
		{"forgejo.example.com", "forgejo.example.com"},
		{"repo-cos.process", "repo-cos.process"},
		// 🔴 U+0130 — THE ONE CODE POINT WHOSE `str.lower()` EXPANDS, AND EVERY `want`
		// BELOW IS TRANSCRIBED FROM `lib/subsystem_resolver.normalize_ref` RUN ON THE
		// PINNED INTERPRETER, never from this implementation. `İ`.lower() is `i` + U+0307,
		// and U+0307 is outside `[a-z0-9.-]`, so it folds to a SEPARATOR.
		//
		// 🔴 THE FIRST TWO ROWS ARE THE CONTROL THAT MAKES THE OTHER THREE MEAN SOMETHING.
		// They AGREED before the fix, because a trailing dash is trimmed — so a table
		// containing only them measured the code point and concluded there was no
		// divergence, which is exactly what the old comment here claimed. Position on the
		// dimension is what decides the answer; these rows name both ends and the middle.
		{"İ", "i"},      // alone: the dash is trailing, so it is trimmed
		{"xİ", "xi"},    // trailing: same
		{"İa", "i-a"},   // LEADING: the dash survives between `i` and `a`
		{"aİb", "ai-b"}, // interior
		{"İİ", "i-i"},   // two of them
		// …and an ordinary capital I must NOT gain a separator, or the fix has widened
		// into every ASCII ref in the store.
		{"Ia", "ia"},
		{"aIb", "aib"},
	}
	for _, tc := range cases {
		if got := NormalizeRef(tc.in); got != tc.want {
			t.Fatalf("NormalizeRef(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// 🔴 THE FOLD IS REACHABLE FROM A REQUEST **BODY**, AND THE OLD COMMENT'S DISMISSAL RESTED
// ON IT NOT BEING. "A scope or ref that is not ASCII cannot be named in a URL path at all"
// is true and does not cover this: entry front matter goes through the same fold, and front
// matter arrives in a `PUT` body. This test is the reachability half — without it the row
// above is a claim about a function nobody can reach.
func TestTheFoldIsReachableFromEntryFrontMatter(t *testing.T) {
	entry, err := EntryFromMapping(map[string]any{
		"service":  "gadget-İone",
		"scope":    "alpha-notes",
		"filename": "gadget-i̇one.md",
	}, "gadget-i̇one.md")
	if err != nil {
		t.Fatalf("front matter carrying U+0130 must validate the way the oracle validates it: %v", err)
	}
	// Spelled by hand from the oracle's fold: `gadget-İone` -> `gadget-i` + U+0307 + `one`
	// -> `gadget-i-one`.
	if entry.Slug != "gadget-i-one" {
		t.Fatalf("`service:` folded to %q, oracle folds it to %q", entry.Slug, "gadget-i-one")
	}
}

func TestSplitKind(t *testing.T) {
	cases := []struct{ ref, slug, kind string }{
		{"repo-cos.process", "repo-cos", "process"},
		{"thing.doc", "thing", "doc"},
		// A trailing dot-segment is a kind ONLY if it is in the enum; otherwise the
		// whole ref is the slug, which is what keeps a dotted slug intact.
		{"forgejo.example.com", "forgejo.example.com", ""},
		{"values.yaml", "values.yaml", ""},
		// A leading-dot ref has no slug, so it is not a kind qualification either.
		{".process", ".process", ""},
		{"plain", "plain", ""},
	}
	for _, tc := range cases {
		slug, kind := SplitKind(tc.ref)
		if slug != tc.slug || kind != tc.kind {
			t.Fatalf("SplitKind(%q) = (%q, %q), want (%q, %q)", tc.ref, slug, kind, tc.slug, tc.kind)
		}
	}
}

func TestParseFrontMatter(t *testing.T) {
	t.Run("a block list binds to its key rather than becoming phantom keys", func(t *testing.T) {
		// 🔴 THE CORRUPTION THIS PARSER WAS WIDENED TO FIX. Before the block form was
		// parsed, `tasks:` followed by two ref-shaped items produced the key silently
		// EMPTY and EVERY item promoted to a front-matter key by its own internal
		// colon. A caller then saw keys nobody wrote and lost the data that was
		// written.
		fm := ParseFrontMatter("---\nservice: thing\ntasks:\n  - alpha:1\n  - beta:2\nscope: s\n---\n")
		tasks, isList := fm.List("tasks")
		if !isList || len(tasks) != 2 || tasks[0] != "alpha:1" || tasks[1] != "beta:2" {
			t.Fatalf("tasks did not bind as a list: %#v", fm)
		}
		for key := range fm {
			if strings.HasPrefix(key, "-") {
				t.Fatalf("a `-`-led line became a phantom key %q: %#v", key, fm)
			}
		}
		if scope, _ := fm.String("scope"); scope != "s" {
			t.Fatalf("the key after the block must stay readable, got %#v", fm)
		}
	})

	t.Run("a BARE key with no items under it is an empty scalar, not a list", func(t *testing.T) {
		// 🔴 RECOGNISED BY LOOKAHEAD, so no existing key changes type. Handing a list
		// to a consumer that has always had a string is an error raised from a file the
		// operator would have to guess at.
		fm := ParseFrontMatter("---\nsensitivity:\nservice: thing\n---\n")
		value, isString := fm.String("sensitivity")
		if !isString || value != "" {
			t.Fatalf("a bare key must read as an empty scalar, got %#v", fm["sensitivity"])
		}
	})

	t.Run("an EMPTY item does not truncate the block", func(t *testing.T) {
		// 🔴 THE NARROWER `startswith("- ")` RESURRECTED THE CORRUPTION: a bare `-`
		// does not satisfy it, so the scan terminated there and every item below was
		// promoted to a phantom key — and the owning key then read as falsy, so the
		// entry LOADED CLEAN reporting no tasks.
		fm := ParseFrontMatter("---\ntasks:\n  -\n  - beta:2\nservice: thing\n---\n")
		tasks, _ := fm.List("tasks")
		if len(tasks) != 1 || tasks[0] != "beta:2" {
			t.Fatalf("an empty item must contribute nothing and stop nothing: %#v", fm)
		}
		for key := range fm {
			if strings.HasPrefix(key, "-") {
				t.Fatalf("a phantom key survived: %q", key)
			}
		}
	})

	t.Run("a BLANK line inside a list does not end it", func(t *testing.T) {
		// Measured against a real YAML parser: a list separated from its key by a blank
		// line is ordinary, valid YAML, and breaking there dropped the whole list back
		// out of the scan.
		fm := ParseFrontMatter("---\ntasks:\n\n  - alpha:1\nservice: thing\n---\n")
		tasks, _ := fm.List("tasks")
		if len(tasks) != 1 || tasks[0] != "alpha:1" {
			t.Fatalf("a blank line must not end the block: %#v", fm)
		}
	})

	t.Run("an inline flow list and quotes", func(t *testing.T) {
		fm := ParseFrontMatter("---\naliases: [a, 'b', \"c\"]\nservice: 'thing'\n---\n")
		aliases, _ := fm.List("aliases")
		if strings.Join(aliases, ",") != "a,b,c" {
			t.Fatalf("inline list: %#v", aliases)
		}
		if svc, _ := fm.String("service"); svc != "thing" {
			t.Fatalf("quotes are stripped: %q", svc)
		}
	})

	t.Run("no front matter is an empty mapping and not an error", func(t *testing.T) {
		if len(ParseFrontMatter("no front matter here\n")) != 0 {
			t.Fatal("a file with no front matter yields nothing")
		}
	})
}

func TestEntryValidation(t *testing.T) {
	// 🔴 THESE SENTENCES ARE THE HTTP CONTRACT, NOT DIAGNOSTICS. A write whose body
	// the loader would reject answers `422 unprocessable: the index loader would reject
	// these bytes: <this message>`, and the conformance goldens pin it byte for byte.
	// The full string is spelled out for the one message a golden carries.
	t.Run("the missing-service sentence is pinned WHOLE", func(t *testing.T) {
		_, err := EntryFromMapping(EntryMapping("no front matter\n", "spare-six.md", "beta-notes"), "spare-six.md")
		if err == nil {
			t.Fatal("bytes with no front matter are not an entry")
		}
		want := "malformed index entry 'spare-six.md': missing or empty `service:` — " +
			"an entry with no name cannot be addressed"
		if err.Error() != want {
			t.Fatalf("the 422 body quotes this verbatim.\n got: %s\nwant: %s", err, want)
		}
	})

	t.Run("the source is quoted the way CPython's repr quotes it", func(t *testing.T) {
		// Single quotes, not Go's `%q` double quotes — a golden records the oracle's
		// `{source!r}` and would fail on the quote character alone.
		_, err := EntryFromMapping(EntryMapping("x\n", "a.md", "s"), "a.md")
		if err == nil || !strings.Contains(err.Error(), "'a.md'") {
			t.Fatalf("want a single-quoted source, got %v", err)
		}
	})

	t.Run("the filename slug and service: must agree", func(t *testing.T) {
		text := "---\nservice: other\nscope: s\n---\n"
		_, err := EntryFromMapping(EntryMapping(text, "widget.md", "s"), "widget.md")
		if err == nil || !strings.Contains(err.Error(), "the two must agree or a ref reaches the wrong file") {
			t.Fatalf("want the slug/service disagreement refused, got %v", err)
		}
	})

	t.Run("a well-formed entry loads with folded aliases", func(t *testing.T) {
		text := "---\nservice: widget-three\nscope: Beta_Notes\naliases: [Widget_Three, widget-three]\n---\n"
		entry, err := EntryFromMapping(EntryMapping(text, "widget-three.md", "beta-notes"), "widget-three.md")
		if err != nil {
			t.Fatal(err)
		}
		if entry.Scope != "beta-notes" {
			t.Fatalf("the DIRECTORY NAME is the authority on scope, got %q", entry.Scope)
		}
		// Two spellings of one alias on ONE entry are a single address, deduped — that
		// distinction is why ambiguity is measured per ENTRY and never per occurrence.
		if strings.Join(entry.Aliases, ",") != "widget-three" {
			t.Fatalf("aliases should fold and dedupe to one, got %v", entry.Aliases)
		}
		if entry.Ref() != "widget-three" || entry.Filename != "widget-three.md" {
			t.Fatalf("unexpected entry %+v", entry)
		}
	})

	t.Run("a kind that contradicts the filename is refused", func(t *testing.T) {
		text := "---\nservice: thing\nscope: s\nkind: doc\n---\n"
		_, err := EntryFromMapping(EntryMapping(text, "thing.process.md", "s"), "thing.process.md")
		if err == nil || !strings.Contains(err.Error(), "contradicts the filename's kind") {
			t.Fatalf("want the kind disagreement refused, got %v", err)
		}
	})

	t.Run("aliases as a bare string is refused, and an EMPTY scalar is not", func(t *testing.T) {
		bare := "---\nservice: thing\nscope: s\naliases: one\n---\n"
		if _, err := EntryFromMapping(EntryMapping(bare, "thing.md", "s"), "thing.md"); err == nil ||
			!strings.Contains(err.Error(), "must be a list, not a bare string") {
			t.Fatalf("want a bare-string aliases refusal, got %v", err)
		}
		// 🔴 THE `or ()` IS PART OF THE RULE: an empty scalar is an ABSENT key, so the
		// refusal fires only on a key that actually carries a value.
		empty := "---\nservice: thing\nscope: s\naliases:\n---\n"
		if _, err := EntryFromMapping(EntryMapping(empty, "thing.md", "s"), "thing.md"); err != nil {
			t.Fatalf("an empty `aliases:` is an absent key: %v", err)
		}
	})
}

func TestTheActionTablesAreTotal(t *testing.T) {
	// 🔴 AN UNMAPPED KIND IS A BUG THAT RAISES, BECAUSE A DEFAULT IS HOW THE PREVIOUS
	// FOUR ROUNDS OF THIS DEFECT HAPPENED. This asserts the loader's table is complete
	// over the CLOSED kind set, in both directions.
	if len(AllKinds) != 9 {
		t.Fatalf("the closed kind set has nine members; got %d — a table asserted "+
			"complete over a set that SHRANK is complete over nothing", len(AllKinds))
	}
	for _, kind := range AllKinds {
		if _, mapped := LoaderAction(kind); !mapped {
			t.Fatalf("the loader's table has no row for kind %q", kind)
		}
	}
	// 🔴 THE CELL LEDGER RUNS BEFORE THE REASON LEDGER, AND THE ORDER IS WHAT MAKES
	// THE SPECIFIC DIAGNOSIS REACHABLE. Measured: with the reason check first, flipping
	// `link-to-file` to Refuse failed with "kind link-to-file is refused with no reason
	// to report" — true, and about the wrong thing. The cell that moved is the finding;
	// the missing reason is a consequence, and a reader sent to the reason table would
	// go looking for prose instead of for the ruling.
	//
	// The five REFUSE cells the narrow ruling decided are named so a widening has to
	// change this list and a narrowing cannot pass unseen.
	for kind, want := range map[Kind]Action{
		KindBrokenLink:  Refuse,
		KindOther:       Refuse,
		KindLinkToOther: Refuse,
		KindDirectory:   Refuse,
		KindLinkToDir:   Refuse,
		// 🔴 `link-to-file` IS TAKE, AND THAT IS THE POINT OF THE NARROW FORM. A
		// symlink to a regular entry is read today and keeps being read; a mutant
		// flipping this cell is the over-broad regression the ruling exists to prevent.
		KindRegularFile:   Take,
		KindLinkToFile:    Take,
		KindIndeterminate: Take,
		KindAbsent:        Take,
	} {
		if got, _ := LoaderAction(kind); got != want {
			t.Fatalf("loader action for %q: got %q want %q", kind, got, want)
		}
	}
	// …and every REFUSE cell carries a reason, while no TAKE cell does: a refused entry
	// has to read like every other unusable one, and a reason attached to a kind that is
	// never refused is a sentence nothing can print.
	for _, kind := range AllKinds {
		action, _ := LoaderAction(kind)
		_, hasReason := LoaderRefusalReason(kind)
		if action == Refuse && !hasReason {
			t.Fatalf("kind %q is refused with no reason to report", kind)
		}
		if action != Refuse && hasReason {
			t.Fatalf("kind %q is not refused but carries a refusal reason", kind)
		}
	}
}

// 🔴 AN UNREADABLE STORE MUST NOT RENDER AS AN EMPTY ONE. `p.is_dir()` returns False for
// four errnos and RAISES for every other one, so the oracle answers
// `503 store-unreachable` for a store root it could not walk. The Go loader `continue`d on
// ANY stat error, which produced a clean, empty index and no error — "nothing recorded yet"
// for a store it never read.
//
// The shape: a store root that is READABLE but not SEARCHABLE (mode 0o444). `ReadDir`
// succeeds and lists the scope names; `Stat` on each child fails EACCES.
func TestAnUnsearchableStoreRootIsREPORTEDAndNotRenderedEmpty(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the search bit, so this shape is unreachable as root")
	}
	root := buildStore(t)

	// The POSITIVE CONTROL FIRST, and it is not decoration: if this load came back empty
	// for an unrelated reason, every assertion below would pass with the guard deleted.
	before, err := LoadIndex(root, Collect, Unrestricted())
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Scopes()) == 0 {
		t.Fatal("the fixture store must have scopes, or this test cannot see the defect")
	}

	if err := os.Chmod(root, 0o444); err != nil {
		t.Fatal(err)
	}
	// Restored so `t.TempDir()` can remove the tree — an unsearchable directory cannot be
	// emptied, and the cleanup failure would be reported as a test failure elsewhere.
	defer func() { _ = os.Chmod(root, 0o755) }()

	index, err := LoadIndex(root, Collect, Unrestricted())
	if err == nil {
		t.Fatalf("an unsearchable store root produced a CLEAN index of %d scope(s) and no "+
			"error — the oracle raises PermissionError here and the route answers 503",
			len(index.Scopes()))
	}
	// 🔴 AND IT MUST BE THE ERRNO-CARRYING ERROR, not a generic one: `LoadStore` maps it
	// through `osErrorTypeName`, and the oracle's sentence names the exception CLASS.
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("the error must carry the permission errno so the 503 can name it: %v", err)
	}

	// The end-to-end half, through the function every route actually calls: the error has
	// to arrive as `EntryUnreadableError`, because that is the type `finish` maps to
	// `503 store-unreachable`. A raw errno would fall through to a 500.
	_, storeErr := LoadStore(root, "recalled", Unrestricted())
	var unreadable *EntryUnreadableError
	if !errors.As(storeErr, &unreadable) {
		t.Fatalf("LoadStore must report this as EntryUnreadableError (which maps to 503), got %T: %v",
			storeErr, storeErr)
	}
	if !strings.Contains(unreadable.Error(), "the store was not fully read, so this report would be INCOMPLETE") {
		t.Fatalf("the 503 body must say the store was not fully read: %s", unreadable)
	}
}

// The other side of the same predicate: the four errnos CPython swallows must still be
// skipped, or every store with a dangling scope symlink starts answering 503. This is the
// control that stops the fix above from becoming "fail on everything".
func TestTheFourIgnoredErrnosAreStillSkipped(t *testing.T) {
	root := t.TempDir()
	// A dangling symlink at scope level: `Stat` follows it and fails ENOENT, which
	// `is_dir()` reports as plain False.
	if err := os.Symlink(filepath.Join(root, "nowhere"), filepath.Join(root, "dangling")); err != nil {
		t.Fatal(err)
	}
	// …and a symlink loop, which fails ELOOP.
	if err := os.Symlink(filepath.Join(root, "loop"), filepath.Join(root, "loop")); err != nil {
		t.Fatal(err)
	}
	index, err := LoadIndex(root, Collect, Unrestricted())
	if err != nil {
		t.Fatalf("ENOENT and ELOOP are in _IGNORED_ERRNOS and must not fail the load: %v", err)
	}
	if len(index.Scopes()) != 0 {
		t.Fatalf("neither broken pointer is a scope, got %v", index.Scopes())
	}

	// And the predicate itself, at both ends, so "is this errno ignored" is pinned rather
	// than inferred from the two shapes above.
	for _, e := range []syscall.Errno{syscall.ENOENT, syscall.ENOTDIR, syscall.EBADF, syscall.ELOOP} {
		if !isIgnoredStatErrno(e) {
			t.Fatalf("%v is in pathlib._IGNORED_ERRNOS and must be skipped", e)
		}
	}
	for _, e := range []syscall.Errno{syscall.EACCES, syscall.EIO, syscall.ESTALE} {
		if isIgnoredStatErrno(e) {
			t.Fatalf("%v is NOT in pathlib._IGNORED_ERRNOS — CPython raises, so this must report", e)
		}
	}
}

// 🔴 A LEDGER, NOT AN ASSERTION ABOUT ONE FIELD — AND WRITING IT IS WHAT FOUND THE BUG.
// The first version of this test declared all three of `Entry`'s slice fields `nil` when
// empty, on the strength of reading `parseTasksField`. It went RED on `Aliases`, which is an
// empty slice because `sortedKeys` always allocates. So the three fields gave TWO different
// answers to one question, in one struct, undecided — the `nil`-versus-`[]` split that
// `encoding/json` turns into `null`-versus-`[]` the moment anything marshals an entry. They
// are all empty slices now, matching the oracle's tuples.
//
// The ledger does two things a per-field assertion cannot: it RE-MEASURES the claim written
// on the struct, so that comment cannot drift from the code, and it fails when the set of
// slice fields GROWS or SHRINKS, so a field added later cannot inherit this silently.
func TestTheEmptySliceLedgerIsComplete(t *testing.T) {
	// The declared ledger: every slice-typed field of `Entry`, and what it is when empty.
	// `nil` here is a STATEMENT OF FACT about today's code, re-measured below, not a wish.
	ledger := map[string]string{
		"Aliases":    "empty slice",
		"RawAliases": "empty slice",
		"Tasks":      "empty slice",
	}

	typ := reflect.TypeOf(Entry{})
	found := map[string]bool{}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Type.Kind() != reflect.Slice {
			continue
		}
		found[field.Name] = true
		if _, listed := ledger[field.Name]; !listed {
			t.Fatalf("`Entry.%s` is a slice field and is NOT in the empty-slice ledger. "+
				"Decide what it is when empty — nil marshals as JSON `null`, an empty slice "+
				"as `[]` — record it on the struct, and add it here.", field.Name)
		}
	}
	for name := range ledger {
		if !found[name] {
			t.Fatalf("the ledger names `Entry.%s`, which no longer exists — a stale ledger "+
				"reads as coverage while providing none", name)
		}
	}

	// 🔴 AND THE LEDGER'S CLAIM IS RE-MEASURED, not trusted. An entry with no aliases and
	// no tasks must actually produce nil for each field the ledger says is nil — otherwise
	// the comment on the struct is a claim the code contradicts, which is the thing this
	// repo keeps finding.
	entry, err := EntryFromMapping(map[string]any{
		"service":  "gadget-one",
		"scope":    "alpha-notes",
		"filename": "gadget-one.md",
	}, "gadget-one.md")
	if err != nil {
		t.Fatal(err)
	}
	value := reflect.ValueOf(entry)
	for name, want := range ledger {
		got := "nil"
		if !value.FieldByName(name).IsNil() {
			got = "empty slice"
		}
		if got != want {
			t.Fatalf("`Entry.%s` on an entry with none: ledger says %s, measured %s",
				name, want, got)
		}
	}

	// `tasks: []` and an absent `tasks:` key mean the same thing on the oracle
	// (`lib/subsystem_resolver` says so explicitly), so they must agree here too — which is
	// the behavioural property the nil/empty question is usually reaching for.
	withEmpty, err := EntryFromMapping(map[string]any{
		"service":  "gadget-one",
		"scope":    "alpha-notes",
		"filename": "gadget-one.md",
		"tasks":    []string{},
	}, "gadget-one.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(withEmpty.Tasks) != 0 || len(entry.Tasks) != 0 {
		t.Fatalf("`tasks: []` and an absent key must both be zero-length, got %d and %d",
			len(withEmpty.Tasks), len(entry.Tasks))
	}
}

func TestTheLoaderITSELFTakesTheAllowlist(t *testing.T) {
	// 🔴 TWO FILTERS, TWO DIFFERENT QUESTIONS, AND ONLY THIS TEST CAN TELL THEM APART.
	// `LoadIndex` decides what is OPENED; `LoadStore` decides what the RESULT SHAPE is.
	// Measured: mutating the loader's filter away SURVIVES every assertion driven
	// through `LoadStore`, because that function's rebuild drops the key again on the
	// way out — so the loader's own filter reads as redundant while it is the half that
	// keeps a denied scope's NAME off `Scopes()`, which is the `known_scopes`
	// enumeration channel, for a directory nothing ever opened.
	root := buildStore(t)
	index, err := LoadIndex(root, Collect, VisibleScopeSet([]string{"beta-notes"}))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(index.Scopes(), ","); got != "beta-notes" {
		t.Fatalf("the loader must not even REGISTER a denied scope, got %q", got)
	}
	// The positive control: an unrestricted load of the same store sees all four, so a
	// pass above is the filter and not an empty store.
	all, err := LoadIndex(root, Collect, Unrestricted())
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Scopes()) != 4 {
		t.Fatalf("the control failed: the store holds four scopes, the loader saw %v",
			all.Scopes())
	}
}

func TestClassifyPath(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	link := func(target, name string) string {
		path := filepath.Join(dir, name)
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
		return path
	}

	regular := write("plain.md", "x")
	subdir := filepath.Join(dir, "adir")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(dir, "afifo")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skipf("this filesystem has no fifos: %v", err)
	}

	cases := []struct {
		path string
		want Kind
	}{
		{regular, KindRegularFile},
		{subdir, KindDirectory},
		{fifo, KindOther},
		{link(regular, "to-file"), KindLinkToFile},
		{link(subdir, "to-dir"), KindLinkToDir},
		{link(fifo, "to-fifo"), KindLinkToOther},
		// 🔴 A DANGLING LINK AND A LOOP ARE BOTH BROKEN POINTERS. The loader REFUSES
		// `broken-link` and TAKES `indeterminate`, so collapsing the two arms turns an
		// editor lock file back into a store-wide unreadable-store error — which is
		// what makes this split killable rather than decorative.
		{link(filepath.Join(dir, "nothing-here"), "dangling"), KindBrokenLink},
		{filepath.Join(dir, "absent"), KindAbsent},
	}
	// A self-referential symlink is the loop half of `broken-link`.
	loop := filepath.Join(dir, "loop")
	if err := os.Symlink(loop, loop); err == nil {
		cases = append(cases, struct {
			path string
			want Kind
		}{loop, KindBrokenLink})
	}
	for _, tc := range cases {
		if got := ClassifyPath(tc.path); got != tc.want {
			t.Fatalf("ClassifyPath(%s) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestActionForRefusesAnUnmappedKind(t *testing.T) {
	// The negative control on the lookup itself: a table missing a cell must be an
	// error, never a silent skip.
	if _, err := ActionFor(KindOther, map[Kind]Action{KindRegularFile: Take}); err == nil {
		t.Fatal("an unmapped kind must be an error")
	}
}

// buildStore materialises a small store: two scopes, one malformed entry and one
// empty scope.
func buildStore(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	entry := func(scope, name, service string) {
		dir := filepath.Join(root, scope)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := "---\nservice: " + service + "\nscope: " + scope +
			"\n---\n\n## Nuance / work-history\n- 2000-01-02: a note.\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	entry("alpha-notes", "gadget-one.md", "gadget-one")
	entry("beta-notes", "widget-three.md", "widget-three")
	if err := os.WriteFile(filepath.Join(root, "rubble-heap", "broken.md"), nil, 0o644); err != nil {
		if err := os.MkdirAll(filepath.Join(root, "rubble-heap"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "rubble-heap", "broken.md"),
			[]byte("no front matter\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "hollow-set"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestLoadStoreNarrowsByAllowlist(t *testing.T) {
	root := buildStore(t)

	t.Run("an unrestricted caller sees every scope", func(t *testing.T) {
		index, err := LoadStore(root, "recalled", Unrestricted())
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(index.Scopes(), ",") != "alpha-notes,beta-notes,hollow-set,rubble-heap" {
			t.Fatalf("scopes: %v", index.Scopes())
		}
	})

	t.Run("a refused scope is INDISTINGUISHABLE from one that never existed", func(t *testing.T) {
		// 🔴 THE ENUMERATION PROPERTY, AND IT IS CLOSED AT THE INDEX RATHER THAN AT
		// EACH ROUTE. A scope the caller may not see is simply not in the index, so
		// asking for it produces the SAME error a never-existed scope produces — the
		// only thing the two messages differ in is the name the caller itself supplied.
		index, err := LoadStore(root, "recalled", VisibleScopeSet([]string{"beta-notes"}))
		if err != nil {
			t.Fatal(err)
		}
		_, refused := index.Entries("alpha-notes")
		_, absent := index.Entries("ghost-void")
		if refused == nil || absent == nil {
			t.Fatal("both a refused and an absent scope must be unknown to this index")
		}
		normalise := func(err error, scope string) string {
			return strings.ReplaceAll(err.Error(), scope, "<SCOPE>")
		}
		if normalise(refused, "alpha-notes") != normalise(absent, "ghost-void") {
			t.Fatalf("a refusal is distinguishable from an absence:\n %s\n %s", refused, absent)
		}
		// …and the scope it CAN see is still there, so this is not "the index is empty".
		if _, err := index.Entries("beta-notes"); err != nil {
			t.Fatalf("the allowed scope must remain visible: %v", err)
		}
	})

	t.Run("an EMPTY allowlist sees nothing, which is the opposite of unrestricted", func(t *testing.T) {
		index, err := LoadStore(root, "recalled", VisibleScopeSet(nil))
		if err != nil {
			t.Fatal(err)
		}
		if len(index.Scopes()) != 0 {
			t.Fatalf("an empty allowlist must see no scope, got %v", index.Scopes())
		}
	})

	t.Run("a malformed entry is COLLECTED and its scope still EXISTS", func(t *testing.T) {
		// 🔴 A SCOPE THAT HOLDS ONLY BROKEN ENTRIES STILL EXISTS. Without that, a
		// reader would answer "nothing recorded yet" about a directory full of content
		// it simply could not parse — the exact conflation this store guards against.
		index, err := LoadStore(root, "recalled", Unrestricted())
		if err != nil {
			t.Fatal(err)
		}
		bad := index.MalformedIn("rubble-heap")
		if len(bad) != 1 {
			t.Fatalf("want one collected rejection, got %v", bad)
		}
		entries, err := index.Entries("rubble-heap")
		if err != nil {
			t.Fatalf("the scope must be known: %v", err)
		}
		if len(entries) != 0 {
			t.Fatalf("…and hold no entries, got %v", entries)
		}
		// A rejection in another scope must not make THIS scope look broken.
		if len(index.MalformedIn("alpha-notes")) != 0 {
			t.Fatal("a rejection must be reported against the scope it lives in")
		}
	})

	t.Run("an absent store root is store-missing and NOT nothing-recorded", func(t *testing.T) {
		_, err := LoadStore(filepath.Join(root, "nope"), "recalled", Unrestricted())
		var missing *StoreMissingError
		if err == nil {
			t.Fatal("an absent store root must fail")
		}
		if !asStoreMissing(err, &missing) {
			t.Fatalf("want a store-missing error, got %T", err)
		}
		if !strings.Contains(err.Error(), "NOT 'nothing recorded yet'") {
			t.Fatalf("the sentence must say what did NOT happen: %s", err)
		}
	})
}

func asStoreMissing(err error, target **StoreMissingError) bool {
	got, ok := err.(*StoreMissingError)
	if ok {
		*target = got
	}
	return ok
}

func TestResolveRefTiered(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "s")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	put := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put("thing.md", "---\nservice: thing\nscope: s\naliases: [handle]\n---\n")
	put("other.md", "---\nservice: other\nscope: s\naliases: [handle]\n---\n")
	put("thing.process.md", "---\nservice: thing\nscope: s\nkind: process\n---\n")

	index, err := LoadStore(root, "recalled", Unrestricted())
	if err != nil {
		t.Fatal(err)
	}

	t.Run("a BARE ref beside a qualified file is AMBIGUOUS, not a preference", func(t *testing.T) {
		// 🔴 THE FILENAME TIER MATCHES `<slug>.md` AND EVERY `<slug>.<kind>.md`, so a
		// bare ref against both is >1 in ONE tier and the resolver refuses rather than
		// picking. Measured against the oracle rather than assumed: the message below is
		// byte-identical to what `subsystem_resolver.resolve_ref_tiered` produces for
		// the same three entries. ⚠ AN EARLIER VERSION OF THIS TEST EXPECTED THE BARE
		// FILE TO WIN — the implementation was right and the expectation was wrong,
		// which is the direction that only shows up when the expectation is checked
		// against the thing being ported instead of against intuition.
		_, _, err := ResolveRefTiered("thing", index, "s")
		want := "ambiguous ref 'thing' in scope 's': 2 candidates in the filename tier " +
			"(thing.md, thing.process.md). The resolver never picks — disambiguate the ref or the index."
		if err == nil || err.Error() != want {
			t.Fatalf("\n got: %v\nwant: %s", err, want)
		}
	})

	t.Run("a kind-qualified ref matches ONLY that file", func(t *testing.T) {
		entry, _, err := ResolveRefTiered("thing.process", index, "s")
		if err != nil {
			t.Fatal(err)
		}
		if entry == nil || entry.Filename != "thing.process.md" {
			t.Fatalf("got %+v", entry)
		}
	})

	t.Run("an alias shared by two entries is AMBIGUOUS and never picked", func(t *testing.T) {
		_, _, err := ResolveRefTiered("handle", index, "s")
		var ambiguous *AmbiguousRefError
		got, ok := err.(*AmbiguousRefError)
		if !ok {
			t.Fatalf("want an ambiguity, got %v", err)
		}
		ambiguous = got
		if strings.Join(ambiguous.Candidates, ",") != "other.md,thing.md" {
			t.Fatalf("the candidates must be listed for a human to choose from: %v", ambiguous.Candidates)
		}
		if !strings.Contains(ambiguous.Error(), "The resolver never picks") {
			t.Fatalf("unexpected message: %s", ambiguous)
		}
	})

	t.Run("an alias can never outrank a filename", func(t *testing.T) {
		// `other` is a FILENAME and also nobody's alias; `thing` is a filename while
		// `handle` is ambiguous as an alias. The tier order is what stops an alias
		// collision from breaking an unambiguous filename hit.
		entry, tier, err := ResolveRefTiered("other", index, "s")
		if err != nil || entry == nil || tier != "filename" {
			t.Fatalf("got %+v tier=%q err=%v", entry, tier, err)
		}
	})

	t.Run("a miss in both tiers is not an error", func(t *testing.T) {
		entry, tier, err := ResolveRefTiered("ghost", index, "s")
		if err != nil || entry != nil || tier != "" {
			t.Fatalf("an honest miss: got %+v tier=%q err=%v", entry, tier, err)
		}
	})
}

func TestNuanceBlockAndJournalBullets(t *testing.T) {
	t.Run("the FIRST heading wins and the section ends at the next heading", func(t *testing.T) {
		// 🔴 ONE WALK, BECAUSE THE INSERTION SCOPE AND THE DEDUPE SCOPE MUST BE THE
		// SAME SECTION. When they were not, an entry carrying the heading TWICE
		// answered `duplicate` — writing nothing — for a genuinely new bullet that
		// merely matched one sitting in the SECOND section.
		lines := []string{
			"## Nuance / work-history",
			"- first",
			"## Pointers",
			"- not a bullet of ours",
			"## Nuance / work-history",
			"- second",
		}
		insertAt, body, ok := NuanceBlock(lines)
		if !ok {
			t.Fatal("the heading is right there")
		}
		if insertAt != 1 {
			t.Fatalf("insert immediately under the FIRST heading, got %d", insertAt)
		}
		if body != "- first" {
			t.Fatalf("the body must be the FIRST block only, got %q", body)
		}
	})

	t.Run("a heading INSIDE a fence is not the heading", func(t *testing.T) {
		lines := []string{
			"```",
			"## Nuance / work-history",
			"```",
			"## Nuance / work-history",
			"- real",
		}
		insertAt, body, ok := NuanceBlock(lines)
		if !ok || insertAt != 4 || body != "- real" {
			t.Fatalf("got insertAt=%d body=%q ok=%v", insertAt, body, ok)
		}
	})

	t.Run("no heading at all is reported and not invented", func(t *testing.T) {
		if _, _, ok := NuanceBlock([]string{"## Pointers", "- x"}); ok {
			t.Fatal("an entry with no nuance heading has nowhere to put a bullet")
		}
	})

	t.Run("a continuation line attaches to the bullet above it", func(t *testing.T) {
		// Measured over the real corpus: every bullet is at indent 0 and every
		// continuation at indent 2, so an indented `-` is a CONTINUATION — folding the
		// two together would report a history longer than the entry has.
		bullets := ParseJournalBullets("- one\n  wrapped prose\n  - nested\n- two\n")
		if len(bullets) != 2 {
			t.Fatalf("want two bullets, got %d: %#v", len(bullets), bullets)
		}
		if len(bullets[0].Lines) != 3 {
			t.Fatalf("the first bullet must carry its continuations: %#v", bullets[0].Lines)
		}
	})

	t.Run("a `- ` line inside a fence is sample text, not history", func(t *testing.T) {
		bullets := ParseJournalBullets("- one\n```\n- not a bullet\n```\n")
		if len(bullets) != 1 {
			t.Fatalf("promoting fenced text invents history: %#v", bullets)
		}
	})

	t.Run("text BEFORE the first bullet is dropped from the bullet list", func(t *testing.T) {
		bullets := ParseJournalBullets("prose with no bullet\n")
		if len(bullets) != 0 {
			t.Fatalf("got %#v", bullets)
		}
		// …and a caller must not read that as "the section is empty": a non-empty body
		// yielding no bullets is its own state, which is why the body is returned too.
	})

	t.Run("trailing blank lines do not inflate a bullet", func(t *testing.T) {
		bullets := ParseJournalBullets("- one\n\n\n- two\n")
		if len(bullets[0].Lines) != 1 {
			t.Fatalf("a blank separator is not part of the bullet: %#v", bullets[0].Lines)
		}
	})
}

func TestPyRepr(t *testing.T) {
	cases := []struct{ in, want string }{
		{"spare-six.md", "'spare-six.md'"},
		{"it's", `"it's"`},
		{`a"b`, `'a"b'`},
		{"tab\there", `'tab\there'`},
		{"nl\n", `'nl\n'`},
		{"\x00", `'\x00'`},
		{"plain space", "'plain space'"},
	}
	for _, tc := range cases {
		if got := PyRepr(tc.in); got != tc.want {
			t.Fatalf("PyRepr(%q) = %s, want %s", tc.in, got, tc.want)
		}
	}
}

// THE TWO "the store was not fully read" SENTENCES MUST SPELL THEIR CAUSE THE ORACLE'S WAY.
//
// 🔴 THIS EXISTS BECAUSE A MUTATION SWEEP FOUND THE SURVIVOR AND SAID SO. `EntryUnreadable`'s
// cause was moved from `%s` on the raw Go error to `PyOSError` in the same change as
// `LoadStore`'s — one rule, one place, exactly as `EntryUnreadable`'s own header demands ("so
// the two … sentences cannot drift apart"). But reverting THIS one alone left `internal/client`,
// `internal/store` and `internal/report` all green: the per-entry sentence is raised from
// `report.ReadEntry`/`search`, which run AFTER the index loaded, and an entry that was readable
// at index time and unreadable at body time is a TOCTOU no test can stage. So the guard has to
// call the function DIRECTLY rather than reach it through a verb.
//
// ⚠ AND IT ASSERTS THE WHOLE NORMALISED SENTENCE, not that `[Errno` appears somewhere. A
// substring check on the tail alone passes for a sentence that lost its path, its type name or
// its "INCOMPLETE" clause — and the parity gate compares these bytes.
func TestBothUnreadableSentencesRenderTheirCauseAsCPythonWould(t *testing.T) {
	cause := &os.PathError{Op: "open", Path: "/w/alpha-notes/one.md", Err: syscall.EACCES}
	// 🔴 THE NEGATIVE CONTROL ON THE FIXTURE: Go's OWN rendering, which is what the mutant
	// produces. If these two were ever equal the assertions below could not tell the two
	// spellings apart and would pass either way.
	if cause.Error() == PyOSError(cause) {
		t.Fatalf("the fixture cannot discriminate: Go and CPython render %q identically",
			cause.Error())
	}

	perEntry := EntryUnreadable("/w/alpha-notes/one.md", cause).Error()
	wantPerEntry := "index entry unreadable: /w/alpha-notes/one.md " +
		"(PermissionError: [Errno 13] Permission denied: '/w/alpha-notes/one.md') — " +
		"the store was not fully read, so this report is INCOMPLETE; nothing was written"
	if perEntry != wantPerEntry {
		t.Fatalf("EntryUnreadable:\n got %q\nwant %q", perEntry, wantPerEntry)
	}

	// The store-wide twin, reached the way a reader reaches it: a scope holding one entry
	// nothing can open. Same spelling, same function, so the two cannot drift.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "alpha-notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(root, "alpha-notes", "one.md")
	if err := os.WriteFile(entry, []byte("---\nservice: one\nscope: alpha-notes\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(entry, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(entry, 0o644) })
	_, err := LoadStore(root, "recalled", Unrestricted())
	if err == nil {
		t.Fatal("an unreadable entry must fail the store CLOSED, not load a short index")
	}
	wantWide := "index entry unreadable: under " + root +
		" (PermissionError: [Errno 13] Permission denied: '" + entry + "') — " +
		"the store was not fully read, so this report would be INCOMPLETE"
	if err.Error() != wantWide {
		t.Fatalf("LoadStore:\n got %q\nwant %q", err.Error(), wantWide)
	}
}

// AN UNREADABLE SCOPE DIRECTORY FAILS THE STORE CLOSED, AND A GENUINELY EMPTY ONE DOES NOT.
//
// 🔴 AN INVARIANT GUARD, NOT REGRESSION COVERAGE, AND LABELLED AS ONE. This client has always
// answered a mode-000 scope directory with `index entry unreadable` at exit 3 — `mdNamesIn`
// walks with `os.ReadDir`, which has no suppression to remove — so this test is GREEN at
// `278b8df` and green at HEAD. It exists because the ORACLE's twin of this walk used
// `pathlib.Path.glob`, which SUPPRESSES the `OSError` its directory scan raises and yields
// nothing: measured over one cached scope at `chmod 000`, the oracle answered 0 with
// `status=scope-empty` ("NOTHING RECORDED YET … Not an error.") where this client answered 3.
// The fix is the oracle's; what this pins is the side the fix had to MATCH, so a later
// "simplification" of `mdNamesIn` into a suppressing walk cannot make the two agree by
// regressing this one instead.
//
// ⚠ THE EMPTY HALF IS THE OTHER DIRECTION AND IS WHY BOTH LIVE IN ONE TEST. A walk that
// refused every directory it could not fully account for would satisfy the first assertion
// while destroying the ordinary outcome, which is a scope somebody made and never filled.
func TestAnUnreadableScopeDirFailsClosedWhileAnEmptyOneLoads(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions; the guard is unreachable")
	}
	root := t.TempDir()
	locked := filepath.Join(root, "alpha-notes")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(locked, "one.md")
	if err := os.WriteFile(entry, []byte("---\nservice: one\nscope: alpha-notes\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The negative control on the fixture: over an EMPTY directory a suppressing walk and a
	// raising one agree, so the mode could not be the variable.
	if names, err := mdNamesIn(locked); err != nil || len(names) != 1 {
		t.Fatalf("the fixture scope must hold exactly one entry file: %v %v", names, err)
	}
	empty := filepath.Join(root, "made-never-filled")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}

	// THE STATE THAT MUST NOT MOVE: readable and genuinely empty loads, and the scope is
	// REGISTERED rather than dropped.
	index, err := LoadStore(root, "recalled", Unrestricted())
	if err != nil {
		t.Fatalf("a readable store with an empty scope must load: %v", err)
	}
	if !slices.Contains(index.Scopes(), "made-never-filled") {
		t.Fatalf("an empty scope must be REGISTERED, not dropped: %v", index.Scopes())
	}

	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	_, err = LoadStore(root, "recalled", Unrestricted())
	if err == nil {
		t.Fatal("an unreadable SCOPE DIRECTORY must fail the store CLOSED, not register it empty")
	}
	want := "index entry unreadable: under " + root +
		" (PermissionError: [Errno 13] Permission denied: '" + locked + "') — " +
		"the store was not fully read, so this report would be INCOMPLETE"
	if err.Error() != want {
		t.Fatalf("LoadStore:\n got %q\nwant %q", err.Error(), want)
	}
}
