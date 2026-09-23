package client

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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
	problems, notes, err := two.Check([]string{"alpha-notes", "beta-notes"})
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 {
		t.Fatalf("direction 1 is the only PROBLEM here: %q", problems)
	}
	if !strings.Contains(problems[0], "beta-notes") || !strings.Contains(problems[0], "REFUSE") {
		t.Fatalf("direction 1: %q", problems)
	}
	// Direction 2 still FIRES — it is reported, it is simply not a verdict. Asserting the note
	// rather than only the problem count is what keeps "demoted" from becoming "deleted".
	if len(notes) != 1 || !strings.Contains(notes[0], "retired-scope") {
		t.Fatalf("direction 2 must still be REPORTED, as a note: %q", notes)
	}

	// 🔴 THE SAME TABLE ON A ONE-INSTANCE HOST REPORTS NEITHER THE REFUSAL NOR A FALSE ONE.
	// "a write to it will REFUSE" is a prediction about `AliasFor`, and there it does not.
	one := Routing{
		Instances: []Instance{{Alias: DefaultAlias}},
		Routes:    map[string]string{"alpha-notes": DefaultAlias},
	}
	problems, notes, err = one.Check([]string{"alpha-notes", "beta-notes"})
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 || len(notes) != 0 {
		t.Fatalf("one instance: an unnamed scope resolves, so there is nothing to say: %q %q",
			problems, notes)
	}
	// …but an UNCONFIGURED alias is still a problem at one instance, which is the row that
	// keeps the line above from being "the check was switched off".
	one.Routes = map[string]string{"alpha-notes": "ghost"}
	problems, _, err = one.Check([]string{"alpha-notes"})
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 || !strings.Contains(problems[0], "ghost") {
		t.Fatalf("one instance: an unconfigured alias still refuses: %q", problems)
	}
}

// TestAScopeThatEXISTSBUTIsEMPTYIsANoteAndNOTAVerdict is finding 4.
//
// 🔴 THE TWO STATES ARE INDISTINGUISHABLE IN THE INPUT, WHICH IS WHY THE VERDICT HAD TO GO. The
// caller's scope set is a cache directory listing, and a snapshot ships entry FILES, so a table
// entry for a scope holding nothing is absent from that set whether it is stale, pre-registered
// before its first write, or pruned back to empty. All three produce the identical note and NO
// problem, so `routes --check` exits 0 — which is the two-way registry the verb exists to be.
func TestAScopeThatEXISTSBUTIsEMPTYIsANoteAndNOTAVerdict(t *testing.T) {
	one := Routing{
		Instances: []Instance{{Alias: DefaultAlias}},
		Routes:    map[string]string{"alpha-notes": DefaultAlias, "hollow-set": DefaultAlias},
	}
	problems, notes, err := one.Check([]string{"alpha-notes"})
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 {
		t.Fatalf("a pre-registered scope must not be a VERDICT — that is what made the check "+
			"refuse every table written before its first write: %q", problems)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "hollow-set") {
		t.Fatalf("…and it must still be REPORTED: %q", notes)
	}
	// 🔴 THE NOTE MUST NOT TELL THE READER TO DELETE THE LINE, because with more than one
	// instance that makes the next write to the scope REFUSE. Pinned as text because the
	// wrong remedy is the harm, and it is only expressible in the sentence.
	if !strings.Contains(notes[0], "rather than deleting") || !strings.Contains(notes[0], "REFUSE") {
		t.Fatalf("the note must name the remedy trap: %q", notes[0])
	}
	// The control for all of the above: a table that names only scopes that DO hold entries
	// produces neither. Without it every assertion here is satisfied by a `Check` that
	// returns nothing at all.
	one.Routes = map[string]string{"alpha-notes": DefaultAlias}
	problems, notes, err = one.Check([]string{"alpha-notes"})
	if err != nil || len(problems) != 0 || len(notes) != 0 {
		t.Fatalf("an exactly-correct table says nothing: %q %q %v", problems, notes, err)
	}
}

// TestCheckRefusesAHostWithNoTable — an absent table read as an empty one would report every
// scope as unrouted.
func TestCheckRefusesAHostWithNoTable(t *testing.T) {
	routing := Routing{Instances: []Instance{{Alias: DefaultAlias}}}
	if _, _, err := routing.Check([]string{"alpha-notes"}); err == nil {
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

// =============================================================================
// THE TWO-INSTANCE BEHAVIOURAL TEST. Findings 1 and 2.
// =============================================================================

// twoInstanceWorld stands up two real HTTP stores, points a HOME at both, and returns the
// per-instance snapshot request counters.
//
// 🔴 A SIGNATURE ASSERTION WOULD NOT HAVE CAUGHT THIS. "`ResolveState` now takes an alias"
// type-checks while the caller passes the wrong one, so what is measured here is the two
// OBSERVABLES a misroute actually moves: WHICH URL was fetched, and WHICH cache directory was
// written. Both were wrong before the fix and neither is visible from a message check.
func twoInstanceWorld(t *testing.T) (home string, hits map[string]*int32, urls map[string]string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	// 🔴 EVERY INHERITED POINTER IS CLEARED. `SUBSYSTEM_STORE_URL` overrides the DEFAULT
	// instance — the one path where the environment wins — so a developer's own value would
	// point `personal` at their live store and this test would measure their machine.
	for _, key := range clientConfigNames() {
		t.Setenv(key, "")
	}

	stamp := time.Unix(946684800, 0)
	hits = map[string]*int32{DefaultAlias: new(int32), "secondary": new(int32)}
	urls = map[string]string{}
	bodies := map[string][]byte{
		DefaultAlias: gzTar(t, func(tw *tar.Writer) {
			regular(tw, "alpha-notes/one.md", []byte("alpha\n"), stamp)
		}),
		"secondary": gzTar(t, func(tw *tar.Writer) {
			regular(tw, "gamma-notes/two.md", []byte("gamma\n"), stamp)
		}),
	}
	for _, alias := range []string{DefaultAlias, "secondary"} {
		alias, counter, body := alias, hits[alias], bodies[alias]
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/snapshot" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			atomic.AddInt32(counter, 1)
			w.Header().Set("X-Store-Entries", "1")
			w.Header().Set("X-Store-Snapshot", "snap-"+alias)
			_, _ = w.Write(body)
		}))
		t.Cleanup(srv.Close)
		urls[alias] = srv.URL
	}

	configDir := filepath.Join(home, ".config", "subsystem-store")
	if err := os.MkdirAll(filepath.Join(configDir, InstanceDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path, url, token string) {
		t.Helper()
		body := "SUBSYSTEM_STORE_URL=" + url + "\nSUBSYSTEM_STORE_TOKEN=" + token + "\n"
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(configDir, "env"), urls[DefaultAlias], "token-default")
	write(filepath.Join(configDir, InstanceDirName, "secondary"+InstanceSuffix),
		urls["secondary"], "token-secondary")
	return home, hits, urls
}

func writeRoutesIn(t *testing.T, home string, body string) {
	t.Helper()
	path := filepath.Join(home, ".config", "subsystem-store", RoutesFileName)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// scopeDirs is the scope directories a cache root actually holds, sorted.
func scopeDirs(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// TestRoutesCheckReadsEACHInstanceFromITSOWNConfig is the Go twin of the oracle's
// `test_a_CORRECT_table_passes_the_CLI_check`, with the two observables a misroute moves.
//
// 🔴 MEASURED BEFORE THE FIX, on exactly this world: `routes --check` fetched `personal`'s URL
// TWICE and `secondary`'s ZERO times, overwrote `<cache>-secondary` with `personal`'s snapshot,
// and then reported `gamma-notes` as "a scope that exists on no configured instance" — a FALSE
// finding in the direction the design calls the silent one, at exit 11, with the second
// instance's real scope set never read at all.
func TestRoutesCheckReadsEACHInstanceFromITSOWNConfig(t *testing.T) {
	home, hits, urls := twoInstanceWorld(t)
	writeRoutesIn(t, home, `{"alpha-notes": "personal", "gamma-notes": "secondary"}`)

	stderr, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	code, runErr := Routes(Env{Stdout: &stdout, Stderr: stderr},
		Options{Cache: DefaultCacheRoot(), Timeout: 5, Check: true})
	if runErr != nil {
		t.Fatal(runErr)
	}
	errText, err := os.ReadFile(stderr.Name())
	if err != nil {
		t.Fatal(err)
	}

	// OBSERVABLE 1 — which URL was fetched. One request per instance, to ITS OWN store.
	for _, alias := range []string{DefaultAlias, "secondary"} {
		if n := atomic.LoadInt32(hits[alias]); n != 1 {
			t.Fatalf("instance %q served %d snapshot request(s), want exactly 1 — a client "+
				"reading every instance from the default config fetches `%s` N times and the "+
				"others never", alias, n, DefaultAlias)
		}
	}
	// …and the banner says so, which is the operator-visible half of the same fact.
	if !strings.Contains(string(errText), "cairn[secondary]: live — fetched from "+urls["secondary"]) {
		t.Fatalf("the `secondary` banner must name SECONDARY's URL (%s), not %s:\n%s",
			urls["secondary"], urls[DefaultAlias], errText)
	}

	// OBSERVABLE 2 — which cache directory was written. Each instance's snapshot lands in its
	// own root, and the sibling root is NOT overwritten with the default instance's store.
	defaultRoot, err := CacheRootFor(DefaultAlias)
	if err != nil {
		t.Fatal(err)
	}
	secondRoot, err := CacheRootFor("secondary")
	if err != nil {
		t.Fatal(err)
	}
	if got := scopeDirs(t, defaultRoot); !reflect.DeepEqual(got, []string{"alpha-notes"}) {
		t.Fatalf("the default cache root holds %q, want [alpha-notes]", got)
	}
	if got := scopeDirs(t, secondRoot); !reflect.DeepEqual(got, []string{"gamma-notes"}) {
		t.Fatalf("`secondary`'s cache root holds %q, want [gamma-notes] — holding "+
			"[alpha-notes] is the default instance's snapshot unpacked over it, which is the "+
			"damage `refuseSharedCache` exists to prevent arriving through the code path", got)
	}

	// …and therefore the GRADE is right: the table matches reality on both instances.
	if code != ExitOK {
		t.Fatalf("a table that matches both instances must exit %d, got %d\nstdout: %s\nstderr: %s",
			ExitOK, code, stdout.String(), errText)
	}
	if !strings.Contains(stdout.String(), "0 problem(s)") {
		t.Fatalf("stdout must report a clean grade: %s", stdout.String())
	}
	if strings.Contains(string(errText), "gamma-notes") {
		t.Fatalf("`gamma-notes` lives on `secondary` and was found there — no finding may "+
			"name it:\n%s", errText)
	}
	if got := len(scopeDirs(t, defaultRoot)) + len(scopeDirs(t, secondRoot)); got != 2 {
		t.Fatalf("the graded scope set must be the UNION of both instances, saw %d dirs", got)
	}
}

// TestAPutDerivesItsPreconditionFromTheROUTEDStore is finding 2.
//
// 🔴 THE DANGEROUS CASE IS A REF THAT EXISTS ON BOTH STORES, so this world gives the two
// instances the SAME scope and ref with DIFFERENT bytes. A `put` that derived its `If-Match`
// from the default instance's cache would send a well-formed precondition computed from the
// wrong store's bytes, print "derived If-Match … from the live snapshot", and be wrong about
// both halves. The observable is the `If-Match` value on the wire.
func TestAPutDerivesItsPreconditionFromTheROUTEDStore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, key := range clientConfigNames() {
		t.Setenv(key, "")
	}
	stamp := time.Unix(946684800, 0)
	// 🔴 THE SAME SCOPE AND THE SAME REF ON BOTH STORES, DIFFERENT BYTES. With different refs
	// the defect merely exits 2 ("cannot derive a revision"), which is the BENIGN outcome and
	// would make this test pass against the bug for the wrong reason.
	defaultBytes := []byte("the DEFAULT instance's copy\n")
	routedBytes := []byte("the ROUTED instance's copy\n")
	bodies := map[string][]byte{
		DefaultAlias: gzTar(t, func(tw *tar.Writer) {
			regular(tw, "shared-scope/thing.md", defaultBytes, stamp)
		}),
		"secondary": gzTar(t, func(tw *tar.Writer) {
			regular(tw, "shared-scope/thing.md", routedBytes, stamp)
		}),
	}
	urls := map[string]string{}
	var sentIfMatch string
	var sentTo string
	for _, alias := range []string{DefaultAlias, "secondary"} {
		alias, body := alias, bodies[alias]
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v1/snapshot" {
				w.Header().Set("X-Store-Entries", "1")
				_, _ = w.Write(body)
				return
			}
			sentIfMatch, sentTo = r.Header.Get("If-Match"), alias
			w.Header().Set("X-Store-Status", "replaced")
			w.Header().Set("ETag", `"newrevision0000"`)
			_, _ = w.Write([]byte("ok\n"))
		}))
		t.Cleanup(srv.Close)
		urls[alias] = srv.URL
	}
	configDir := filepath.Join(home, ".config", "subsystem-store")
	if err := os.MkdirAll(filepath.Join(configDir, InstanceDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	for path, alias := range map[string]string{
		filepath.Join(configDir, "env"):                                       DefaultAlias,
		filepath.Join(configDir, InstanceDirName, "secondary"+InstanceSuffix): "secondary",
	} {
		body := "SUBSYSTEM_STORE_URL=" + urls[alias] + "\nSUBSYSTEM_STORE_TOKEN=t-" + alias + "\n"
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeRoutesIn(t, home, `{"shared-scope": "secondary"}`)

	replacement := filepath.Join(t.TempDir(), "new.md")
	if err := os.WriteFile(replacement, []byte("replacement\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stderr, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	code, runErr := Put(Env{Stdout: &stdout, Stderr: stderr}, Options{
		Cache: DefaultCacheRoot(), Timeout: 5,
		Scope: "shared-scope", Ref: "thing", File: replacement,
	})
	if runErr != nil || code != ExitOK {
		body, _ := os.ReadFile(stderr.Name())
		t.Fatalf("put failed: code=%d err=%v stderr=%s", code, runErr, body)
	}
	if sentTo != "secondary" {
		t.Fatalf("the write went to %q, and the table routes `shared-scope` to `secondary`", sentTo)
	}
	sum := sha256.Sum256(routedBytes)
	want := `"` + hex.EncodeToString(sum[:])[:16] + `"`
	wrong := sha256.Sum256(defaultBytes)
	if sentIfMatch == `"`+hex.EncodeToString(wrong[:])[:16]+`"` {
		t.Fatalf("If-Match was derived from the DEFAULT instance's bytes and sent to the "+
			"ROUTED one: %s", sentIfMatch)
	}
	if sentIfMatch != want {
		t.Fatalf("If-Match %s, want %s (sha256 of the ROUTED store's bytes)", sentIfMatch, want)
	}
	// …and the ROUTED cache is what was refreshed, not the default one.
	secondRoot, err := CacheRootFor("secondary")
	if err != nil {
		t.Fatal(err)
	}
	if !StampExists(secondRoot) {
		t.Fatal("the routed instance's cache must be the one that was synced")
	}
}

// TestAPutLoadsTheROUTEDCredentialsLAZILY pins WHEN the credentials are read, not whose.
//
// 🔴 A BYTE DIVERGENCE THAT EVERY EXISTING GATE WAS BLIND TO, BECAUSE THE EXIT CODES AGREE.
// On a routed instance whose config file is INCOMPLETE, the oracle's `cmd_put` reaches
// `resolve_state` first, which reports the missing credential as a non-live state, and `put`
// refuses in its OWN words — "refusing to PUT — could not refresh the cache … (config
// incomplete: …)". This port loaded the config eagerly in `writeInstance`, so the error escaped
// to `cli.go`'s write-unreachable arm and refused in ITS words — "the write did NOT happen —
// config incomplete: … Re-run when the store is reachable." Both are rc 7, so no exit-code
// comparison could see it, and the only `<MULTICFG>` parity rows were `routes --check`.
//
// ⚠ AND IT IS NOT "GO IS EAGER" — `append` is byte-identical on this same input, because the
// oracle loads the config eagerly there too. The divergence was one verb wide, which is why the
// fix splits `writeInstance` rather than reordering it.
//
// ⚠ THE `runErr == nil` ASSERTION IS THE LOAD-BEARING ONE. An escaping error is how the eager
// version fails, and it arrives as `(0, err)` — a code this test would otherwise never compare.
func TestAPutLoadsTheROUTEDCredentialsLAZILY(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, key := range clientConfigNames() {
		t.Setenv(key, "")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("no request may reach any pod: the routed instance has no token, so this "+
			"write must refuse before the network (%s %s)", r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)

	configDir := filepath.Join(home, ".config", "subsystem-store")
	if err := os.MkdirAll(filepath.Join(configDir, InstanceDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	// The DEFAULT instance is complete; only the ROUTED one is missing its token. A host where
	// BOTH are broken cannot tell "it read the routed config" from "it read the default one".
	if err := os.WriteFile(filepath.Join(configDir, "env"),
		[]byte("SUBSYSTEM_STORE_URL="+srv.URL+"\nSUBSYSTEM_STORE_TOKEN=t-default\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, InstanceDirName, "secondary"+InstanceSuffix),
		[]byte("SUBSYSTEM_STORE_URL="+srv.URL+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeRoutesIn(t, home, `{"shared-scope": "secondary"}`)

	payload := filepath.Join(t.TempDir(), "new.md")
	if err := os.WriteFile(payload, []byte("replacement\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stderr, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	code, runErr := Put(Env{Stdout: &stdout, Stderr: stderr}, Options{
		Cache: DefaultCacheRoot(), Timeout: 5,
		Scope: "shared-scope", Ref: "thing", File: payload,
	})
	text, _ := os.ReadFile(stderr.Name())
	if runErr != nil {
		t.Fatalf("the config error ESCAPED instead of being reported by `put`: %v\n"+
			"that is the eager load, and `cli.go` renders it in a different sentence than the "+
			"oracle's", runErr)
	}
	if code != ExitWriteUnreachable {
		t.Fatalf("code %d, want %d (a write that could not refresh its cache)",
			code, ExitWriteUnreachable)
	}
	if !strings.Contains(string(text), "refusing to PUT — could not refresh the cache") {
		t.Fatalf("`put` must refuse in ITS OWN words, which is what the oracle prints:\n%s", text)
	}
	if !strings.Contains(string(text), "config incomplete") {
		t.Fatalf("the refusal must carry the state resolver's detail, which is where the "+
			"missing credential is named:\n%s", text)
	}
}

// TestAnEDITORLockFileDoesNotTakeEveryVerbToExit11 is finding 6.
//
// 🔴 EVERY VERB, NOT JUST `routes`. `Discover` runs on the routing path of every write AND of
// every read (`readInstance`, and the fan-out in `sync`/`ls-entries`/`doctor`), so a hard
// `*RoutingConfigError` here refused the whole tool while one buffer was open. Emacs writes
// `.#secondary.env` as a DANGLING SYMLINK, so `IsDir()` is false and the name ends in `.env` —
// it reached the refusal.
func TestAnEDITORLockFileDoesNotTakeEveryVerbToExit11(t *testing.T) {
	dir := configuredHost(t, t.TempDir())
	instances := filepath.Join(dir, InstanceDirName)
	if err := os.MkdirAll(instances, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(instances, "secondary"+InstanceSuffix), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	// Two shapes, because the skip must not depend on the file's TYPE: what Emacs actually
	// creates (a dangling symlink) and a regular file with the same `.#` name shape.
	lock := filepath.Join(instances, ".#secondary"+InstanceSuffix)
	if err := os.Symlink("an-editor@a-host.12345:946684800", lock); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(lock); err == nil {
		t.Fatal("the fixture must be a DANGLING link, as Emacs writes it")
	}
	routing, err := Discover(nil)
	if err != nil {
		t.Fatalf("a dangling editor lock file must not refuse: %v", err)
	}
	if !reflect.DeepEqual(routing.Aliases(), []string{DefaultAlias, "secondary"}) {
		t.Fatalf("…and it must not become an instance either: %q", routing.Aliases())
	}
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}

	regular := filepath.Join(instances, ".#vim-style"+InstanceSuffix)
	if err := os.WriteFile(regular, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Discover(nil); err != nil {
		t.Fatalf("a regular `.#` file must not refuse either: %v", err)
	}
	if err := os.Remove(regular); err != nil {
		t.Fatal(err)
	}

	// 🔴 THE CONTROL: a NON-dotted file the operator really did write is still an ERROR. The
	// rule is NARROWED to names a human did not choose, not removed — and without this line
	// the test is satisfied by deleting the refusal outright.
	if err := os.WriteFile(filepath.Join(instances, "Upper"+InstanceSuffix), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Discover(nil); err == nil {
		t.Fatal("a file the operator wrote that cannot be an alias is still an ERROR")
	}
}

// TestADottedFileThatIsNotAnEditorLockIsStillAnError is the skip's OTHER arm.
//
// 🔴 A ONE-SIDED TEST PASSES WHILE THE HOLE IS OPEN. The first cut of the narrowing above
// skipped every name beginning with `.`, which silently swallowed `instances/.env` — the dotted
// name an operator is MOST likely to write there, and whose stem is the empty string. Measured
// on that predicate: a complete, valid `instances/.env` produced `instances: personal` at exit 0
// with no message on either client, the exact outcome the `Upper.env` refusal exists to forbid.
// So the skip arm and this arm together are the predicate; either alone is a direction.
//
// ⚠ `.env` IS THE LOAD-BEARING CASE. The other two are here so the guard pins `.#` rather than
// "an empty stem": a non-empty dotted stem that is also an unusable alias must refuse too.
func TestADottedFileThatIsNotAnEditorLockIsStillAnError(t *testing.T) {
	dir := configuredHost(t, t.TempDir())
	instances := filepath.Join(dir, InstanceDirName)
	if err := os.MkdirAll(instances, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".env", ".Upper.env", ".hidden.env"} {
		probe := filepath.Join(instances, name)
		body := "SUBSYSTEM_STORE_URL=http://127.0.0.1:1\nSUBSYSTEM_STORE_TOKEN=synthetic\n"
		if err := os.WriteFile(probe, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := Discover(nil)
		if err == nil {
			t.Fatalf("`instances/%s` was SKIPPED: an operator who wrote it gets no message "+
				"anywhere, which is the hole the `.` predicate opened", name)
		}
		if !strings.Contains(err.Error(), "does not name a usable instance alias") {
			t.Fatalf("`instances/%s` refused for the WRONG reason — this test would be green "+
				"off another guard's error: %v", name, err)
		}
		if !strings.Contains(err.Error(), name) {
			t.Fatalf("the refusal must name the FILE the operator wrote, not just the rule: %v",
				err)
		}
		if err := os.Remove(probe); err != nil {
			t.Fatal(err)
		}
	}
}

// TestDirectionOneCountsAScopeONCEHoweverOftenTheCallerNamesIt is finding 7.
//
// ⚠ AN ALIGNMENT, NOT REGRESSION COVERAGE, AND LABELLED AS ONE. The oracle subtracts SETS
// (`set(scopes) - set(self.routes)`); this port walked the caller's SLICE, so a repeated scope
// produced a repeated finding. `Routes` builds its scope set from a map and therefore cannot
// hand `Check` a duplicate today — no input reachable from the CLI observes this. It is pinned
// because the two implementations are supposed to be one rule, and a divergence nobody can
// reach today is reachable the moment a second caller appears.
func TestDirectionOneCountsAScopeONCEHoweverOftenTheCallerNamesIt(t *testing.T) {
	two := Routing{
		Instances: []Instance{{Alias: DefaultAlias}, {Alias: "secondary"}},
		Routes:    map[string]string{"alpha-notes": DefaultAlias},
	}
	problems, _, err := two.Check([]string{"alpha-notes", "beta-notes", "beta-notes", "beta-notes"})
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 {
		t.Fatalf("one unnamed scope is one finding however often it is named: %q", problems)
	}
}
