package store

import (
	"reflect"
	"strings"
	"testing"
)

// TestUnknownFrontMatterKeysAreIgnored is the measurement the `refs:` key's whole
// backward-compatibility claim rests on, taken on THIS implementation rather than inferred
// from the oracle's.
//
// ⚠ IT IS AN INVARIANT GUARD, NOT REGRESSION COVERAGE, AND IT IS LABELLED AS ONE. No bug ever
// made `EntryFromMapping` refuse an unknown key — it reads only the keys it names and never
// enumerates the mapping. What this pins is the property an older reader handed a NEWER file
// depends on: a `refs:` file loads on a binary that has never heard of `refs:`, reporting no
// refs, instead of refusing the file. A code reading said so; the oracle was probed and
// agreed; this is the second point.
//
// 🔴 THE NEGATIVE CONTROL IS IN THE SAME TEST FUNCTION ON PURPOSE. Four IGNOREDs from a probe
// that cannot observe a refusal are four facts about the probe. The no-`service:` mapping
// MUST be refused, so if the assertion loop below were wired to nothing that case would fail
// and the whole test with it.
func TestUnknownFrontMatterKeysAreIgnored(t *testing.T) {
	valid := func(extra map[string]any) FrontMatter {
		fm := FrontMatter{"service": "alpha", "scope": "zone-one"}
		for k, v := range extra {
			fm[k] = v
		}
		return fm
	}
	ignored := map[string]FrontMatter{
		"tags-as-a-list":   valid(map[string]any{"tags": []string{"a", "b"}}),
		"tags-as-a-scalar": valid(map[string]any{"tags": "a"}),
		"a-nonsense-key":   valid(map[string]any{"zzz-no-such-key": "whatever"}),
	}
	for name, fm := range ignored {
		entry, err := EntryFromMapping(fm, "probe.md")
		if err != nil {
			t.Fatalf("%s: unknown key REFUSED, which breaks the forward-compatibility claim: %v", name, err)
		}
		if entry.Slug != "alpha" || entry.Scope != "zone-one" {
			t.Fatalf("%s: loaded but wrong: slug=%q scope=%q", name, entry.Slug, entry.Scope)
		}
	}
	// The control: a mapping this function MUST refuse.
	if _, err := EntryFromMapping(FrontMatter{"scope": "zone-one"}, "probe.md"); err == nil {
		t.Fatal("the control passed: a mapping with no `service:` loaded, so the three IGNOREDs " +
			"above are facts about a probe that cannot observe a refusal")
	}
}

// TestTheRefsKeyIsRead is criterion 1: `refs:` is accepted with the same rules `tasks:` has.
//
// 🔴 WATCHED RED AT THE BASE COMMIT, where an entry carrying only `refs:` surfaced ZERO refs —
// the key was unknown, so `Tasks` came back empty and the entry loaded clean. That is the
// symptom this pins, and it is the reason the guard above is labelled an invariant while this
// one is regression coverage.
func TestTheRefsKeyIsRead(t *testing.T) {
	entry, err := EntryFromMapping(FrontMatter{
		"service": "alpha",
		"scope":   "zone-one",
		"refs":    []string{"GitHub:example-org/example-repo#428", "clickup:8600xyz", "github:example-org/example-repo#428"},
	}, "alpha.md")
	if err != nil {
		t.Fatalf("`refs:` refused: %v", err)
	}
	// Every rule `tasks:` has, in one assertion: the system half NORMALIZED (`GitHub` →
	// `github`), the id half BYTE-IDENTICAL, DEDUPED across spellings of the system half,
	// and FILE ORDER preserved.
	want := []TaskRef{
		{System: "github", Ident: "example-org/example-repo#428", Raw: "GitHub:example-org/example-repo#428"},
		{System: "clickup", Ident: "8600xyz", Raw: "clickup:8600xyz"},
	}
	if !reflect.DeepEqual(entry.Tasks, want) {
		t.Fatalf("refs = %#v, want %#v", entry.Tasks, want)
	}
	// Non-nil when empty, the same as `tasks:` — see the ledger on `Entry`.
	empty, err := EntryFromMapping(FrontMatter{"service": "alpha", "scope": "zone-one", "refs": []string{}}, "alpha.md")
	if err != nil {
		t.Fatalf("`refs: []` refused: %v", err)
	}
	if empty.Tasks == nil {
		t.Fatal("`refs: []` produced a nil slice; every slice field on Entry is non-nil when empty")
	}
}

// TestTheOlderRefKeysStillParseAndRefsWins is criterion 2, pinned in BOTH directions on
// one fixture each.
//
// 🔴 IT IS NOW THE WHOLE GUARD ON THE `tasks:`/`task:` ALIAS ON THIS SIDE. The warning
// machinery that used to sit beside it is deleted; the ACCEPTANCE is not, and nothing else in
// this package would go red if `parseRefsField` stopped reading the old spellings. Its Python
// twin is `test_the_older_spellings_still_parse`, and `tests/conformance/`'s
// `linked-set/linked-old-key.md` exercises the same path over the wire on both servers.
func TestTheOlderRefKeysStillParseAndRefsWins(t *testing.T) {
	idents := func(e Entry) []string {
		out := []string{}
		for _, r := range e.Tasks {
			out = append(out, r.String())
		}
		return out
	}
	cases := []struct {
		name string
		fm   FrontMatter
		want []string
	}{
		{"tasks-alone-still-parses",
			FrontMatter{"service": "alpha", "scope": "zone-one", "tasks": []string{"clickup:old"}},
			[]string{"clickup:old"}},
		{"task-alone-still-parses",
			FrontMatter{"service": "alpha", "scope": "zone-one", "task": "clickup:old"},
			[]string{"clickup:old"}},
		// 🔴 BOTH DIRECTIONS ON ONE FIXTURE: the same entry with `refs:` beside each
		// older spelling, and the answer is the NEW key's value both times. A test
		// that only checked `refs:` + `tasks:` would pass on an implementation that read
		// `task:` in preference to `refs:`.
		{"refs-beats-tasks",
			FrontMatter{"service": "alpha", "scope": "zone-one",
				"refs": []string{"clickup:new"}, "tasks": []string{"clickup:old"}},
			[]string{"clickup:new"}},
		{"refs-beats-task",
			FrontMatter{"service": "alpha", "scope": "zone-one",
				"refs": []string{"clickup:new"}, "task": "clickup:old"},
			[]string{"clickup:new"}},
		// 🔴 AND IT BEATS BOTH AT ONCE WITHOUT TRIPPING THEIR MUTUAL-EXCLUSION REFUSAL.
		// See `parseRefsField`: that refusal is about two spellings DISAGREEING over the
		// entry's refs, and when `refs:` is present neither is read, so there is no
		// disagreement to resolve.
		{"refs-beats-both-and-is-not-refused",
			FrontMatter{"service": "alpha", "scope": "zone-one",
				"refs": []string{"clickup:new"}, "tasks": []string{"clickup:old"}, "task": "clickup:older"},
			[]string{"clickup:new"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			entry, err := EntryFromMapping(c.fm, "alpha.md")
			if err != nil {
				t.Fatalf("refused: %v", err)
			}
			if !reflect.DeepEqual(idents(entry), c.want) {
				t.Fatalf("refs = %v, want %v", idents(entry), c.want)
			}
		})
	}
	// The older pair's mutual exclusion is UNCHANGED for the entries that reach it,
	// which is every file with no `refs:` — and that is the half a golden pins.
	if _, err := EntryFromMapping(FrontMatter{"service": "alpha", "scope": "zone-one",
		"tasks": []string{"clickup:a"}, "task": "clickup:b"}, "alpha.md"); err == nil {
		t.Fatal("`tasks:` + `task:` with no `refs:` must still be refused")
	}
}

// TestTheRefURLRegistryIsComplete is criterion 4: the BUILT-IN tier's key set asserted
// EXACTLY, so it fails when the set GROWS or SHRINKS, plus the resolution table under it and
// the OPERATOR tier measured at both of its two states.
//
// ⚠ THE SELF-HOSTED SYSTEM IN THESE FIXTURES IS SYNTHETIC, AND THAT IS NOT COSMETIC. The
// system this tier was built for is a self-hosted tool of the deployment this repo was
// extracted from, and `tests/leakscan.py` gates its NAME under `denied-identifier` — measured,
// 17 findings across three files, REFUSING. So the fixtures name `tracker`, which exercises
// the same code path: the operator tier keys on nothing but the system half.
func TestTheRefURLRegistryIsComplete(t *testing.T) {
	want := []string{"clickup", "github"}
	if !reflect.DeepEqual(RefSystems(), want) {
		t.Fatalf("built-in systems = %v, want exactly %v — a template was added or removed "+
			"without a decision about its URL shape. A SELF-HOSTED system belongs in the "+
			"OPERATOR tier, not in this list; see refurl.go.", RefSystems(), want)
	}
	// Every built-in row must carry a base. A row with none resolves nothing, which is what
	// the operator tier is for.
	for _, tpl := range RefURLTemplates {
		if tpl.Base == "" {
			t.Fatalf("%s: a built-in template with no Base resolves nothing", tpl.System)
		}
		if !strings.HasPrefix(tpl.Base, "https://") {
			t.Fatalf("%s: Base is %q, want an absolute https origin", tpl.System, tpl.Base)
		}
	}

	// The operator-registered tier, measured at TWO POINTS because that is the dimension its
	// answer depends on.
	registered := map[string]string{"CAIRN_REF_BASE_TRACKER": "https://tracker.invalid/tasks/"}
	get := func(name string) string { return registered[name] }
	absent := func(string) string { return "" }

	cases := []struct {
		name    string
		ref     string
		get     func(string) string
		wantURL string
	}{
		{"github issue or pr", "github:example-org/example-repo#428", absent,
			"https://github.com/example-org/example-repo/issues/428"},
		{"github repo without a number", "github:example-org/example-repo", absent,
			"https://github.com/example-org/example-repo"},
		{"clickup task", "clickup:8600abcxyz", absent, "https://app.clickup.com/t/8600abcxyz"},
		{"an operator-registered system", "tracker:662", get, "https://tracker.invalid/tasks/662"},
		// Point two on the operator dimension: the SAME ref with nothing registered.
		{"the same system with nothing registered", "tracker:662", absent, ""},
		// 🔴 THE BUILT-IN TIER WINS. An operator base for `github` must NOT redirect a
		// GitHub ref, because this tier does not know that system's path shape and would
		// downgrade `owner/repo#428` to one escaped segment.
		{"a built-in system is not overridable", "github:example-org/example-repo#428",
			func(string) string { return "https://mirror.invalid" },
			"https://github.com/example-org/example-repo/issues/428"},
		// A nil getter is the same answer as an unset variable, not a panic.
		{"a nil getter", "tracker:662", nil, ""},
		// Shapes a BUILT-IN system cannot address resolve to no URL rather than a guess.
		{"github with no owner", "github:example-repo", absent, ""},
		{"github with a non-numeric number", "github:example-org/example-repo#four", absent, ""},
		{"clickup id carrying a slash", "clickup:a/b", absent, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ref, err := ParseTaskRef(c.ref)
			if err != nil {
				t.Fatalf("fixture ref %q does not parse: %v", c.ref, err)
			}
			got, ok := RefURL(ref, c.get)
			if c.wantURL == "" {
				if ok {
					t.Fatalf("resolved to %q, want NO url", got)
				}
				return
			}
			if !ok {
				t.Fatalf("resolved to no url, want %q", c.wantURL)
			}
			if got != c.wantURL {
				t.Fatalf("url = %q, want %q", got, c.wantURL)
			}
		})
	}

	// The id half is attacker-influenced, so it must be ESCAPED into the path rather than
	// interpolated. `..` is the shape that would otherwise climb out of the path, and the
	// operator tier is where a WHOLE id half — `/` and `#` included — becomes one segment.
	for _, c := range []struct{ ref, want string }{
		{"clickup:..", "https://app.clickup.com/t/.."},
		{"tracker:../../etc", "https://tracker.invalid/tasks/..%2F..%2Fetc"},
		{"tracker:a#b", "https://tracker.invalid/tasks/a%23b"},
	} {
		ref, err := ParseTaskRef(c.ref)
		if err != nil {
			t.Fatalf("fixture %q does not parse: %v", c.ref, err)
		}
		got, ok := RefURL(ref, get)
		if !ok || got != c.want {
			t.Fatalf("escaping %q: got %q ok=%v, want %q", c.ref, got, ok, c.want)
		}
	}
}

// TestRefBaseEnvIsAPureFunctionOfTheSystemHalf pins that the variable name does NOT depend on
// the registry — a lookup would answer "" for exactly the systems the operator tier exists to
// serve, so a caller printing "set $X to resolve these" would print nothing.
func TestRefBaseEnvIsAPureFunctionOfTheSystemHalf(t *testing.T) {
	for in, want := range map[string]string{
		"tracker":        "CAIRN_REF_BASE_TRACKER",
		"two-words":      "CAIRN_REF_BASE_TWO_WORDS",
		"github":         "CAIRN_REF_BASE_GITHUB",
		"no-such-system": "CAIRN_REF_BASE_NO_SUCH_SYSTEM",
	} {
		if got := RefBaseEnv(in); got != want {
			t.Errorf("RefBaseEnv(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestEntryReferencesAppliesTheSchemasAsymmetry pins the reverse-lookup predicate at BOTH ends
// of the one dimension it is asymmetric on.
func TestEntryReferencesAppliesTheSchemasAsymmetry(t *testing.T) {
	entry, err := EntryFromMapping(FrontMatter{
		"service": "alpha", "scope": "zone-one",
		"refs": []string{"GitHub:example-org/example-repo#428", "clickup:ABC"},
	}, "alpha.md")
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	match := func(q string) bool {
		ref, parseErr := ParseTaskRef(q)
		if parseErr != nil {
			t.Fatalf("query %q does not parse: %v", q, parseErr)
		}
		return EntryReferences(entry, ref)
	}
	// The SYSTEM half folds: a query in another casing finds the entry.
	if !match("github:example-org/example-repo#428") {
		t.Error("a query whose system half differs only in case did not match")
	}
	if !match("GITHUB:example-org/example-repo#428") {
		t.Error("a query whose system half differs only in case did not match")
	}
	// The ID half does NOT: two ids differing in case are two different tasks.
	if !match("clickup:ABC") {
		t.Error("the exact id half did not match")
	}
	if match("clickup:abc") {
		t.Error("an id half differing in case MATCHED — the schema preserves it byte-for-byte, " +
			"and folding it makes two different tasks compare equal")
	}
	if match("clickup:AB") {
		t.Error("a PREFIX of the id half matched; the comparison is equality")
	}
}
