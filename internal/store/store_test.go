package store

import (
	"os"
	"path/filepath"
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
	}
	for _, tc := range cases {
		if got := NormalizeRef(tc.in); got != tc.want {
			t.Fatalf("NormalizeRef(%q) = %q, want %q", tc.in, got, tc.want)
		}
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
