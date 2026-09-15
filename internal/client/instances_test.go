package client

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 🔴 WHAT THIS FILE GUARDS, IN ONE SENTENCE: that this port's routing answers the same three
// questions the oracle's does — what is configured, which instance a scope belongs to, and
// whether the two agree — and that the ONE predicate the oracle got wrong stays split here.

// configuredHost points the whole configuration at `dir` and returns it. One environment
// variable moves everything, which is `InstanceDir`'s stated contract.
func configuredHost(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(ConfigEnv, filepath.Join(dir, "env"))
	t.Setenv(RoutesEnv, "")
	return dir
}

func writeTable(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, RoutesFileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func addInstance(t *testing.T, dir, alias string) {
	t.Helper()
	instances := filepath.Join(dir, InstanceDirName)
	if err := os.MkdirAll(instances, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(instances, alias+InstanceSuffix),
		[]byte("SUBSYSTEM_STORE_URL=http://127.0.0.1:1\nSUBSYSTEM_STORE_TOKEN=x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestTheThreeRowsThatShareOneConfiguration is the whole point of splitting the predicate.
//
// 🔴 ONE INSTANCE WITH A TABLE PRESENT IS ONE CONFIGURATION AND THREE ANSWERS, so no single "is
// this host routing" boolean can decide it. Rows 1 and 2 must resolve to the sole instance; row 3
// must REFUSE. The oracle's `routes is not None or len(instances) > 1` refused row 2, and
// deleting that disjunct resolves row 3 — a silent misroute.
func TestTheThreeRowsThatShareOneConfiguration(t *testing.T) {
	dir := configuredHost(t, t.TempDir())

	// Row 1 — no table at all.
	routing, err := Discover(nil)
	if err != nil {
		t.Fatal(err)
	}
	if alias, aErr := routing.AliasFor("anything"); aErr != nil || alias != DefaultAlias {
		t.Fatalf("row 1: got (%q, %v), want the sole instance", alias, aErr)
	}

	// Row 2 — a table that says nothing about this scope.
	writeTable(t, dir, `{"some-other-scope": "personal"}`)
	routing, err = Discover(nil)
	if err != nil {
		t.Fatal(err)
	}
	if alias, aErr := routing.AliasFor("alpha-notes"); aErr != nil || alias != DefaultAlias {
		t.Fatalf("row 2: got (%q, %v), want the sole instance — a scope nobody has added to "+
			"the table yet must not become unreadable", alias, aErr)
	}

	// Row 3 — a table entry naming an alias this host has no config for.
	writeTable(t, dir, `{"alpha-notes": "no-such-instance"}`)
	routing, err = Discover(nil)
	if err != nil {
		t.Fatal(err)
	}
	alias, aErr := routing.AliasFor("alpha-notes")
	var unrouted *UnroutedScope
	if !errors.As(aErr, &unrouted) {
		t.Fatalf("row 3: got (%q, %v), want a refusal — resolving it to the sole instance is a "+
			"write landing in a store nobody decided on", alias, aErr)
	}
	if unrouted.Scope != "alpha-notes" || !strings.Contains(unrouted.Detail, "no-such-instance") {
		t.Fatalf("row 3's refusal must carry the scope as a FIELD and name the alias: %#v", unrouted)
	}
}

// TestTwoInstancesKeepTheFullRefusalSemantics is the ≥2 column, which must not move.
func TestTwoInstancesKeepTheFullRefusalSemantics(t *testing.T) {
	dir := configuredHost(t, t.TempDir())
	addInstance(t, dir, "secondary")

	routing, err := Discover(nil)
	if err != nil {
		t.Fatal(err)
	}
	_, aErr := routing.AliasFor("alpha-notes")
	if aErr == nil {
		t.Fatal("two instances and NO table must refuse rather than picking one")
	}
	// 🔴 AND IT MUST BE THE *NO TABLE* REFUSAL, NOT THE *NOT IN THE TABLE* ONE. Found by a
	// surviving mutant on the Python side: with the `Routes == nil` arm deleted this case
	// falls through to the missing-entry refusal, which is also an `UnroutedScope` at exit 11
	// and also contains the words "routing table" — so a test asserting only those passed
	// while the message told the operator to edit a file named `<nil>`. CREATE a table and
	// EDIT one are different remedies.
	if !strings.Contains(aErr.Error(), "Write a scope->alias table to") ||
		strings.Contains(aErr.Error(), "is not in the routing table") {
		t.Fatalf("the no-table refusal must say to WRITE one: %v", aErr)
	}
	writeTable(t, dir, `{"some-other-scope": "personal"}`)
	routing, err = Discover(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, aErr := routing.AliasFor("alpha-notes"); aErr == nil {
		t.Fatal("two instances and a table that does not name the scope must refuse")
	}
	// …and the positive control: a scope the table DOES name resolves to the named instance,
	// so the two refusals above are about the table rather than about routing being broken.
	writeTable(t, dir, `{"alpha-notes": "secondary"}`)
	routing, err = Discover(nil)
	if err != nil {
		t.Fatal(err)
	}
	if alias, aErr := routing.AliasFor("alpha-notes"); aErr != nil || alias != "secondary" {
		t.Fatalf("a routed scope must reach its instance: (%q, %v)", alias, aErr)
	}
}

// TestMultiInstanceAsksTheCountAndNotTheTable pins the LABELLING predicate.
//
// 🔴 A TABLE'S PRESENCE MUST NOT SWITCH LABELLING ON. That defect made every recall on a
// one-instance host assert "with more than one instance configured…", which is false on that
// host and printed on every call.
func TestMultiInstanceAsksTheCountAndNotTheTable(t *testing.T) {
	dir := configuredHost(t, t.TempDir())
	writeTable(t, dir, `{"alpha-notes": "personal"}`)
	routing, err := Discover(nil)
	if err != nil {
		t.Fatal(err)
	}
	if routing.MultiInstance() {
		t.Fatal("a table on a ONE-instance host must not make the host multi-instance")
	}
	if routing.Routes == nil {
		t.Fatal("…and the table must still have been LOADED, or the line above proves nothing")
	}
	addInstance(t, dir, "secondary")
	routing, err = Discover(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !routing.MultiInstance() {
		t.Fatal("two configured instances IS more than one place an answer can come from")
	}
}

// TestBannerNamedLabelsOnlyWhenAsked is the rendering half of the same rule.
func TestBannerNamedLabelsOnlyWhenAsked(t *testing.T) {
	plain := BannerNamed(StateLive, "fetched", "")
	if plain != "cairn: live — fetched" {
		t.Fatalf("the single-instance banner must be byte-identical to what it always was: %q", plain)
	}
	if Banner(StateLive, "fetched") != plain {
		t.Fatal("Banner must be BannerNamed with no label, or the two spellings can drift")
	}
	labelled := BannerNamed(StateNoCache, "gone", "secondary")
	if labelled != "🔴 cairn[secondary]: store-unreachable, no cache — gone" {
		t.Fatalf("the labelled banner keeps the marker ladder: %q", labelled)
	}
}

// TestCheckAsksTheResolverRatherThanSubtractingKeySets is finding 3 plus its one-instance
// correction.
func TestCheckAsksTheResolverRatherThanSubtractingKeySets(t *testing.T) {
	two := Routing{
		Instances: []Instance{{Alias: DefaultAlias}, {Alias: "secondary"}},
		Routes:    map[string]string{"alpha-notes": DefaultAlias, "retired-scope": "secondary"},
	}
	problems, err := two.Check([]string{"alpha-notes", "beta-notes"})
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 2 {
		t.Fatalf("both directions must fire: %q", problems)
	}
	if !strings.Contains(problems[0], "beta-notes") || !strings.Contains(problems[0], "REFUSE") {
		t.Fatalf("direction 1 first: %q", problems)
	}
	if !strings.Contains(problems[1], "retired-scope") {
		t.Fatalf("direction 2 second: %q", problems)
	}

	// 🔴 THE SAME TABLE ON A ONE-INSTANCE HOST REPORTS NEITHER THE REFUSAL NOR A FALSE ONE.
	// "a write to it will REFUSE" is a prediction about `AliasFor`, and there it does not.
	one := Routing{
		Instances: []Instance{{Alias: DefaultAlias}},
		Routes:    map[string]string{"alpha-notes": DefaultAlias},
	}
	problems, err = one.Check([]string{"alpha-notes", "beta-notes"})
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 {
		t.Fatalf("one instance: an unnamed scope resolves, so there is nothing to warn about: %q",
			problems)
	}
	// …but an UNCONFIGURED alias is still a problem at one instance, which is the row that
	// keeps the line above from being "the check was switched off".
	one.Routes = map[string]string{"alpha-notes": "ghost"}
	problems, err = one.Check([]string{"alpha-notes"})
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 || !strings.Contains(problems[0], "ghost") {
		t.Fatalf("one instance: an unconfigured alias still refuses: %q", problems)
	}
}

// TestCheckRefusesAHostWithNoTable — an absent table read as an empty one would report every
// scope as unrouted.
func TestCheckRefusesAHostWithNoTable(t *testing.T) {
	routing := Routing{Instances: []Instance{{Alias: DefaultAlias}}}
	if _, err := routing.Check([]string{"alpha-notes"}); err == nil {
		t.Fatal("grading a host with no table must refuse rather than report findings")
	}
}

// TestLoadRoutesRefusesEverythingButAFlatMapOfAliases. 🔴 A TABLE THAT PARSED TO NOTHING WOULD
// LEAVE EVERY SCOPE UNROUTED, WHICH LOOKS LIKE A DELIBERATE REFUSAL AND IS NOT.
func TestLoadRoutesRefusesEverythingButAFlatMapOfAliases(t *testing.T) {
	dir := t.TempDir()
	for _, body := range []string{`[]`, `"x"`, `{"a": 1}`, `{"a": {"b": "c"}}`,
		`{"a": "Bad Alias"}`, `not json`, `{"a": null}`, `{"a": true}`} {
		path := filepath.Join(dir, "routes.json")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadRoutes(path); err == nil {
			t.Fatalf("%s must be refused", body)
		}
	}
	// The control for the eight refusals above.
	path := filepath.Join(dir, "routes.json")
	if err := os.WriteFile(path, []byte(`{"alpha-notes": "personal"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	table, err := LoadRoutes(path)
	if err != nil || table["alpha-notes"] != DefaultAlias {
		t.Fatalf("a well-formed table must load: %v %v", table, err)
	}
}

// TestTheFirstBadEntryIsTheONEReported. 🔴 GO'S MAP ITERATION IS RANDOMISED, so a table with two
// malformed entries would name a different one run to run — nondeterminism in a diagnostic is how
// a fix gets applied to the wrong line.
func TestTheFirstBadEntryIsTheONEReported(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routes.json")
	body := `{"first-bad": 1, "second-bad": 2, "third-bad": 3}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		_, err := LoadRoutes(path)
		if err == nil || !strings.Contains(err.Error(), "first-bad") {
			t.Fatalf("iteration %d named %v, not the FIRST bad entry", i, err)
		}
	}
}

// TestDiscoveryRefusesAFileThatCannotBeAnInstance. 🔴 SKIPPING IT WOULD LEAVE NO MESSAGE
// ANYWHERE: the operator wrote a file, believes a store is configured, and the scopes they routed
// to it would later refuse naming the ALIAS — pointing at the table rather than at the file that
// was ignored.
func TestDiscoveryRefusesAFileThatCannotBeAnInstance(t *testing.T) {
	dir := configuredHost(t, t.TempDir())
	instances := filepath.Join(dir, InstanceDirName)
	if err := os.MkdirAll(instances, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Upper" + InstanceSuffix, DefaultAlias + InstanceSuffix} {
		path := filepath.Join(instances, name)
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Discover(nil); err == nil {
			t.Fatalf("%s must be an ERROR, not a skip", name)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	// The control: with the bad files gone, discovery succeeds with the default instance
	// alone — so the refusals above are about those files rather than about the directory.
	routing, err := Discover(nil)
	if err != nil || len(routing.Instances) != 1 {
		t.Fatalf("a clean host discovers exactly the default instance: %v %v", routing, err)
	}
}

// TestAnEXPLICITTablePathThatDoesNotExistIsAnError. 🔴 NOT "routing is off": ignoring a typo'd
// path turns a fail-loud configuration into a fail-open one.
func TestAnEXPLICITTablePathThatDoesNotExistIsAnError(t *testing.T) {
	dir := configuredHost(t, t.TempDir())
	t.Setenv(RoutesEnv, filepath.Join(dir, "no-such-table.json"))
	if _, err := Discover(nil); err == nil {
		t.Fatal("a named table that does not exist is an error, not an absence of routing")
	}
	// The control: the DEFAULT path not existing is the ordinary single-instance state.
	t.Setenv(RoutesEnv, "")
	if _, err := Discover(nil); err != nil {
		t.Fatalf("a missing DEFAULT table is not an error: %v", err)
	}
}

// TestCacheRootForKeepsTheDefaultAndSIBLINGSTheRest. 🔴 A CHILD WOULD LOOK LIKE A SCOPE to every
// reader that enumerates `<root>/<dir>`.
func TestCacheRootForKeepsTheDefaultAndSIBLINGSTheRest(t *testing.T) {
	root := DefaultCacheRoot()
	if root == "" {
		t.Skip("no resolvable home directory in this environment")
	}
	got, err := CacheRootFor(DefaultAlias)
	if err != nil || got != root {
		t.Fatalf("the default instance's cache root must not move: %q %v", got, err)
	}
	other, err := CacheRootFor("work-notes")
	if err != nil {
		t.Fatal(err)
	}
	if other == root || filepath.Dir(other) != filepath.Dir(root) {
		t.Fatalf("a second instance is a SIBLING of %q, got %q", root, other)
	}
	for _, bad := range []string{"../escape", "/abs", "Work", ".hidden", "", "a b"} {
		if _, err := CacheRootFor(bad); err == nil {
			t.Fatalf("%q is not a path segment and must be refused", bad)
		}
	}
}

// TestTheUnportedReadGuardFiresOnlyWithMoreThanOneInstance.
//
// 🔴 A HALF-PORTED FEATURE MUST FAIL LOUD, NOT DEGRADE — and it must be INVISIBLE where nothing
// is configured, which is every host today and every row of the parity gate. Both halves are
// asserted, because a guard that fired at one instance would change the bytes of every read.
func TestTheUnportedReadGuardFiresOnlyWithMoreThanOneInstance(t *testing.T) {
	dir := configuredHost(t, t.TempDir())
	var buf bytes.Buffer
	env := Env{Stdout: &buf, Stderr: os.Stderr}

	if code, stop := RefuseUnportedMultiInstance(env, "recall"); stop || code != 0 {
		t.Fatalf("one instance must not be refused: (%d, %v)", code, stop)
	}

	addInstance(t, dir, "secondary")
	stderr, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	code, stop := RefuseUnportedMultiInstance(Env{Stdout: &buf, Stderr: stderr}, "recall")
	if !stop || code != ExitUnrouted {
		t.Fatalf("two instances must stop the verb at exit %d, got (%d, %v)",
			ExitUnrouted, code, stop)
	}
	written, err := os.ReadFile(stderr.Name())
	if err != nil {
		t.Fatal(err)
	}
	// 🔴 THE MESSAGE MUST NAME THE VERB AND THE INSTANCES. A refusal that said only "cannot"
	// would send the reader hunting for a store outage.
	for _, want := range []string{"recall", "secondary", DefaultAlias, "REFUSING"} {
		if !strings.Contains(string(written), want) {
			t.Fatalf("the refusal must name %q: %s", want, written)
		}
	}
}

// TestLoadConfigForReadsTheENVIRONMENTForTheDefaultInstanceONLY.
//
// 🔴 IF THE ENVIRONMENT WON EVERYWHERE, A FAN-OUT WOULD MEASURE ONE STORE N TIMES and report
// agreement it never observed.
func TestLoadConfigForReadsTheENVIRONMENTForTheDefaultInstanceONLY(t *testing.T) {
	dir := configuredHost(t, t.TempDir())
	addInstance(t, dir, "secondary")
	t.Setenv("SUBSYSTEM_STORE_URL", "http://127.0.0.1:9999")
	t.Setenv("SUBSYSTEM_STORE_TOKEN", "from-the-environment")

	def, err := LoadConfigFor(DefaultAlias)
	if err != nil || def.URL != "http://127.0.0.1:9999" {
		t.Fatalf("the environment must win for the default instance: %#v %v", def, err)
	}
	other, err := LoadConfigFor("secondary")
	if err != nil {
		t.Fatal(err)
	}
	if other.URL == def.URL || other.Token == def.Token {
		t.Fatalf("a non-default instance reads its OWN file and nothing else: %#v", other)
	}
	// …and the refusal for a non-default instance says so, rather than blaming the
	// environment the caller was never allowed to use.
	if _, err := LoadConfigFor("absent-instance"); err == nil ||
		!strings.Contains(err.Error(), "NOT consulted") {
		t.Fatalf("the non-default refusal must explain the asymmetry: %v", err)
	}
}
