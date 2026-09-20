package client

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ZacxDev/cairn/internal/store"
)

// WHICH INSTANCE does a scope live on, and where is each instance configured?
//
// The Go port of `lib/cairn_instances.py` and of `subsystem_read_store`'s alias half. A
// deployment may point one client at more than one cairn; this file owns what is configured,
// which instance a scope belongs to, and whether the two agree — for a client that knows
// NOTHING about who routes where. The scope→alias table is an INPUT (a file), never a constant
// in this repository: a public tool that shipped somebody's taxonomy would be publishing their
// org chart.
//
// 🔴 EVERY VERB ROUTES NOW, AND THE READ/WRITE SPLIT IS A DIFFERENCE IN *LABELLING* RATHER
// THAN IN ROUTING. This paragraph has twice said something false about that split — first that
// the writes did not consult `AliasFor` (they always did), then that the reads refused
// outright (they did, until declared difference 8 in `tests/parity/README.md` was closed) — so
// it states the mechanism rather than a status:
//
//   - **The WRITE verbs ROUTE and ALWAYS PRINT THE ALIAS.** `append`, `put` and `create`
//     resolve the scope through `AliasFor`, load the ROUTED instance's credentials, sync the
//     ROUTED instance's cache, and print `instance=<alias>` unconditionally — at one instance
//     and at many. "Where did that bullet go" is a question about a DURABLE record, asked
//     later, by someone who no longer has the terminal.
//   - **The READ verbs ROUTE and label CONDITIONALLY.** `recall`/`search` and `validate`
//     resolve their scope through `AliasFor`; `sync`, `ls-entries` and `doctor` route NOTHING
//     and walk EVERY configured instance. ⚠ Not because none of them takes a scope: only
//     `doctor` has no `--scope`, and `sync`/`ls-entries` declare one and pass `scope=""`
//     regardless — see the retraction below. All six print the alias only when
//     `MultiInstance()` — see `readInstance`/`instanceLabel` in `routes.go`. That emptiness is
//     the compatibility guarantee: a one-instance host's bytes are unchanged.
//   - **`routes` reports rather than routes.** It prints what is configured and, with
//     `--check`, grades the table against every instance. It is the verb an operator runs
//     WHILE STANDING UP a second instance, so it must keep working before any table is right.
//
// 🔴 "A ONE-INSTANCE HOST'S BYTES ARE UNCHANGED" IS TRUE OF *LABELLING* AND WAS READ AS COVERING
// *ROUTING* TOO. `recall`/`search`/`validate` now resolve their scope through `AliasFor`, which
// they did not do before, and `AliasFor` row 3 REFUSES at one instance exactly as at many. So
// there IS a single-instance behaviour change, and it is exactly one case: a host with ONE
// instance whose table routes the scope to an alias it has no config for — a stale or typo'd
// entry, the kind `routes --check` exists to find. Measured over one world with this client and
// `routes.json = {"alpha-notes": "nowhere"}`: `recall`, `search` and `validate` each answered
// **exit 0** off the default instance's cache at `d8b858a`, and each answers **exit 11,
// refusing** at HEAD. HEAD is the correct answer — the old one read a store the table said was
// somewhere else — but it is not "unchanged", and it belongs in whatever announcement the
// `packages.default` flip carries. `sync`, `ls-entries` and `doctor` route NOTHING, so none of
// this reaches them. The same clause is on `lib/README.md`'s bullet, in `tests/parity/README.md`,
// in `cairn`'s `_instance_for` and on `Sync`.
//
// 🔴 AND THAT LAST CLAUSE USED TO GIVE A DIFFERENT, FALSE REASON — "take no scope, so none of
// this reaches them" — WHICH `--help` FALSIFIES FOR TWO OF THE THREE. `cairn sync` and
// `cairn ls-entries` both declare `--scope`; only `doctor` has none. What makes them untouched
// is that they pass `scope=""` and fan out over every instance (`Sync`, `LsEntries`), not that
// there is no scope to route. Re-measured over the same `{"alpha-notes": "nowhere"}` world:
// `sync --scope alpha-notes` exits 4 and `ls-entries --scope alpha-notes` exits 0 on BOTH
// clients, byte-identical to the unscoped runs — so the CONCLUSION stands and only the reason
// moved. 🔴 The difference is load-bearing because this paragraph is what the flip's
// announcement is written from: a later change that routed `ls-entries --scope` through
// `AliasFor` would falsify the conclusion while the old reason still read as covering it.

const (
	// ConfigEnv names the DEFAULT instance's config file. It predates instances and keeps
	// its meaning exactly.
	ConfigEnv = "SUBSYSTEM_STORE_CONFIG"
	// RoutesEnv names the routing table. 🔴 SET IT AND THE TABLE IS MANDATORY — a missing
	// file is an error, never "routing is off". An operator who named a table meant to use
	// one, and silently ignoring the name is how a typo'd path turns a fail-loud design into
	// a fail-open one.
	RoutesEnv = "CAIRN_ROUTES"
	// InstanceDirName is where additional instances live, beside the default config file.
	InstanceDirName = "instances"
	// InstanceSuffix is one file per instance.
	InstanceSuffix = ".env"
	// RoutesFileName is the table's default name, beside the config file. Absent => no table.
	RoutesFileName = "routes.json"
	// DefaultAlias is the alias `DefaultCacheRoot` belongs to.
	//
	// 🔴 A WIRE FACT, NOT A LABEL. It names a config file, a cache directory and a
	// routing-table value, so moving it strands a cache and silently unroutes every scope a
	// table sent here.
	DefaultAlias = "personal"
)

// aliasPattern is what an alias may be SPELLED. An alias becomes a directory name beside the
// cache root, so this is the guard between a routing table (an input, possibly hand-edited) and
// a path: `..`, `/`, a leading dot and an absolute path are all refused here rather than at the
// filesystem, where the failure would be a cache written somewhere nobody looks.
var aliasPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// ValidAlias is `subsystem_read_store.valid_alias`.
//
// 🔴 `DefaultAlias` IS VALID HERE AND RESERVED SOMEWHERE ELSE, and confusing the two broke this
// once on the oracle: a table routing a scope to `personal` — the ordinary case — was refused as
// a malformed alias. What is reserved is a FILE in `instances/` claiming the default alias,
// because then one alias would have two sources; that is `Discover`'s rule, not this one.
func ValidAlias(alias string) bool { return aliasPattern.MatchString(alias) }

// CacheRootFor is the cache root for ONE instance. A function, because there are now N.
//
// 🔴 THE DEFAULT INSTANCE KEEPS `DefaultCacheRoot()` EXACTLY, AND THAT IS A COMPATIBILITY
// GUARANTEE RATHER THAN AN ACCIDENT OF THE FORMULA. Every existing host has a populated cache at
// that path, and the path is PRINTED — it is the `store:` line of every recall.
//
// 🔴 A SIBLING DIRECTORY, NOT A CHILD. Putting instance caches UNDER the default root would make
// each alias look exactly like a SCOPE to every reader that enumerates `<root>/<dir>`.
func CacheRootFor(alias string) (string, error) {
	root := DefaultCacheRoot()
	if alias == DefaultAlias {
		return root, nil
	}
	if !ValidAlias(alias) {
		return "", fmt.Errorf("`%s` is not a usable instance alias: an alias becomes a "+
			"directory name beside the cache root, so it is lowercase letters, digits and "+
			"hyphens only, and `%s` names the default instance's own cache", alias, DefaultAlias)
	}
	return filepath.Join(filepath.Dir(root), filepath.Base(root)+"-"+alias), nil
}

// RoutingConfigError is "the instance configuration or the routing table cannot be read".
//
// Separate from `UnroutedScope` because the remedies differ: this one means a FILE is wrong
// (malformed JSON, an alias that cannot be a directory name, two sources claiming one alias),
// and no scope is involved.
type RoutingConfigError struct{ Detail string }

func (e *RoutingConfigError) Error() string { return e.Detail }

// UnroutedScope is "this scope cannot be resolved to an instance, and nothing was guessed".
//
// 🔴 THE SCOPE IS A FIELD, NOT ONLY A SUBSTRING OF THE MESSAGE. A caller that has to parse a
// sentence to learn which scope failed has pinned prose.
type UnroutedScope struct {
	Scope  string
	Detail string
}

func (e *UnroutedScope) Error() string { return e.Detail }

// Instance is one configured cairn: its alias and the file its URL and token come from.
//
// 🔴 NO URL AND NO TOKEN HERE. This file enumerates and routes; it never reads a credential.
// Keeping the secret out of the value that gets printed, logged and rendered is what stops it
// being printed, logged and rendered.
type Instance struct {
	Alias      string
	ConfigPath string
}

// IsDefault reports whether this is the instance the legacy config file describes.
func (i Instance) IsDefault() bool { return i.Alias == DefaultAlias }

// Routing is what is configured on this host, and the routing decision it supports.
type Routing struct {
	Instances []Instance
	// Routes is nil when there is NO table, which is a different fact from an empty one.
	Routes       map[string]string
	RoutesSource string
}

// Aliases is the configured aliases, in discovery order (default first).
func (r Routing) Aliases() []string {
	out := make([]string, 0, len(r.Instances))
	for _, i := range r.Instances {
		out = append(out, i.Alias)
	}
	return out
}

// MultiInstance reports whether there is more than one PLACE on this host an answer could come
// from.
//
// 🔴 THE LABELLING PREDICATE, AND IT IS DELIBERATELY NOT THE ROUTING ONE. It asks the instance
// COUNT and not the table: a table is a statement about where scopes live, not about how many
// stores this machine can reach. The oracle's predicate used to be
// `routes is not None or len(instances) > 1`, so writing a table on a one-instance host switched
// every label on and made each recall assert "with more than one instance configured…" — false
// on that host, on every call. See `AliasFor` for the routing half and the three-row table.
func (r Routing) MultiInstance() bool { return len(r.Instances) > 1 }

// Get is the instance with this alias, or an error naming what IS configured.
func (r Routing) Get(alias string) (Instance, error) {
	for _, i := range r.Instances {
		if i.Alias == alias {
			return i, nil
		}
	}
	return Instance{}, &RoutingConfigError{Detail: fmt.Sprintf(
		"no instance `%s` is configured on this host (configured: %s)",
		alias, strings.Join(r.Aliases(), ", "))}
}

// AliasFor is the instance `scope` belongs to, or an `*UnroutedScope`. NEVER a guess.
//
// 🔴 THE TABLE IS CONSULTED FIRST, WHATEVER `MultiInstance` SAYS, AND THE ORDER IS THE WHOLE
// POINT. Three cases share one configuration — a one-instance host with a table — and two of
// them must answer DIFFERENTLY, so a single "is this host routing" boolean cannot decide them:
//
//	table state for this scope    | today  | `active = len(instances)>1`
//	------------------------------|--------|----------------------------
//	(1) no table at all           | sole   | sole
//	(2) table, scope ABSENT       | sole   | sole
//	(3) table -> UNCONFIGURED     | REFUSE | sole  <- SILENT MISROUTE
//	    alias                     |        |
//
// Row 3 is a table explicitly routing a scope to an alias this host has no config for.
// Resolving it to the sole instance is a write landing in a store nobody decided on, so it
// refuses at ONE instance and at many.
func (r Routing) AliasFor(scope string) (string, error) {
	alias, named := "", false
	if r.Routes != nil {
		alias, named = r.Routes[scope]
	}
	if named {
		if !containsString(r.Aliases(), alias) {
			dir, _ := InstanceDir(nil)
			return "", &UnroutedScope{Scope: scope, Detail: fmt.Sprintf(
				"the routing table `%s` routes scope `%s` to instance `%s`, which is not "+
					"configured on this host (configured: %s). REFUSING rather than falling "+
					"back: the table names where this scope lives, and this host cannot reach "+
					"it. Add `%s`.",
				r.RoutesSource, scope, alias, strings.Join(r.Aliases(), ", "),
				filepath.Join(dir, alias+InstanceSuffix))}
		}
		return alias, nil
	}
	// The table decided NOTHING about this scope. With one instance that has one answer.
	if !r.MultiInstance() {
		return DefaultAlias, nil
	}
	if r.Routes == nil {
		path, _, _ := RoutesFile(nil)
		return "", &UnroutedScope{Scope: scope, Detail: fmt.Sprintf(
			"%d instances are configured (%s) and no routing table says which one scope `%s` "+
				"belongs to. REFUSING rather than picking one: a write to the wrong instance "+
				"lands in a store nobody reads. Write a scope->alias table to `%s`, or set $%s "+
				"to one.",
			len(r.Instances), strings.Join(r.Aliases(), ", "), scope, path, RoutesEnv)}
	}
	return "", &UnroutedScope{Scope: scope, Detail: fmt.Sprintf(
		"scope `%s` is not in the routing table `%s`. REFUSING rather than defaulting to an "+
			"instance: an unregistered scope is a scope nobody decided about, and guessing is "+
			"how a write lands in a store nobody reads. Add `\"%s\": \"<alias>\"` to that table "+
			"(configured instances: %s).",
		scope, r.RoutesSource, scope, strings.Join(r.Aliases(), ", "))}
}

// Check grades this host's routing table against reality, IN BOTH DIRECTIONS.
//
// It returns `(problems, notes, err)`. A PROBLEM is a defect this check can actually decide,
// and it is what takes `routes --check` to exit 11. A NOTE is worth printing and is NOT
// decidable from the evidence available, so it never moves the exit code.
//
// 🔴 BOTH DIRECTIONS, BECAUSE EACH MISSES A DIFFERENT DEFECT. A scope with no entry is a scope
// whose next write REFUSES — annoying but loud. An entry naming a scope that does not exist is
// the silent one: it reads as coverage, survives the scope being renamed or retired, and is
// exactly what a table accumulates when it is edited by hand.
//
// 🔴 THE TWO DIRECTIONS THAT ARE PREDICTIONS ABOUT THE RESOLVER ASK THE RESOLVER. "a write to it
// will REFUSE" and "that alias is not configured" are both claims about `AliasFor`; re-deriving
// them from the table's key set gave the first one a confident warning on every unnamed scope of
// every ONE-instance host, where no refusal happens.
//
// 🔴 AND DIRECTION TWO IS A NOTE, NOT A PROBLEM, BECAUSE ITS SCOPE SET CANNOT SEE AN EMPTY
// SCOPE. `scopes` is a cache's DIRECTORY LISTING and a snapshot ships entry FILES — no directory
// member — so a scope holding no entries is missing from it whether it was retired,
// pre-registered before its first write, or pruned back to nothing. Measured at one instance
// with a table naming `hollow-set`: `routes --check` exited 11 saying "exists on no configured
// instance" while `recall --scope hollow-set` exited 0 saying `scope-empty — reached the store`.
// Failing there makes the check unusable as the TWO-WAY REGISTRY it exists to be, and the
// remedy the sentence implies — delete the line — makes the next write to that scope REFUSE.
//
// ⚠ THAT COSTS REAL TEETH. **Closing condition**: the discriminator exists and is simply not in
// the snapshot — the SERVER reads the real store, where `store.BuildIndex` registers a scope
// directory holding no entries (`extraScopes`), so `GET /api/v1/recall/<scope>` answers
// `scope-empty` for a live-but-empty scope and `scope-absent` for one that does not exist. When
// this check probes the ROUTED instance per table entry, identically on both clients, with a
// parity row over it, direction two can be a problem again. Not done here: it turns a
// two-request command into an O(table) one, which is a separate decision.
func (r Routing) Check(scopes []string) ([]string, []string, error) {
	if r.Routes == nil {
		return nil, nil, &RoutingConfigError{Detail: "there is no routing table on this host to " +
			"grade. A caller that reached here read an absent table as an empty one, which " +
			"would report every scope as unrouted."}
	}
	inScopes := map[string]bool{}
	for _, s := range scopes {
		inScopes[s] = true
	}
	var problems []string
	// ⚠ THE UNNAMED SET IS BUILT FROM `inScopes`, NOT FROM THE SLICE. The oracle takes
	// `set(scopes) - set(self.routes)`, so a scope appearing twice in the caller's list
	// produces ONE finding there and produced as many as the slice held here. `Routes`
	// deduplicates upstream, so nothing reachable today could observe it — an alignment, not a
	// fix for a live defect, and labelled as one rather than counted as regression coverage.
	var unnamed []string
	for s := range inScopes {
		if _, ok := r.Routes[s]; !ok {
			unnamed = append(unnamed, s)
		}
	}
	sort.Strings(unnamed)
	for _, scope := range unnamed {
		if _, err := r.AliasFor(scope); err != nil {
			problems = append(problems, fmt.Sprintf(
				"scope `%s` exists but the routing table does not name it — a write to it "+
					"will REFUSE", scope))
		}
	}
	// ⚠ DIRECTION TWO IS NOT A RESOLVER QUESTION, DELIBERATELY. `AliasFor` resolves a stale
	// entry perfectly well — it names a configured alias — so asking it here would grade this
	// direction clean. What the resolver cannot see is whether the scope EXISTS; what the
	// scope set cannot see is whether it exists but holds nothing. Hence a NOTE.
	var notes []string
	var stale []string
	for scope := range r.Routes {
		if !inScopes[scope] {
			stale = append(stale, scope)
		}
	}
	sort.Strings(stale)
	for _, scope := range stale {
		notes = append(notes, fmt.Sprintf(
			"the routing table names scope `%s`, which holds no entry on any configured "+
				"instance — a stale entry reads as coverage, but a scope pre-registered before "+
				"its first write or pruned back to nothing looks IDENTICAL here, because a "+
				"snapshot ships entry files and not directories. Check it rather than deleting "+
				"the line: with more than one instance, deleting it makes the next write to "+
				"`%s` REFUSE.", scope, scope))
	}
	var named []string
	for scope := range r.Routes {
		named = append(named, scope)
	}
	sort.Strings(named)
	for _, scope := range named {
		if _, err := r.AliasFor(scope); err != nil {
			problems = append(problems, fmt.Sprintf(
				"the routing table routes `%s` to instance `%s`, which is not configured on "+
					"this host", scope, r.Routes[scope]))
		}
	}
	return problems, notes, nil
}

// envLookup is the environment as a function, so a test can supply one without touching the
// process. nil means the real environment.
type envLookup func(string) string

func lookup(env envLookup) envLookup {
	if env != nil {
		return env
	}
	return os.Getenv
}

// ConfigPath is the DEFAULT instance's config file — `$SUBSYSTEM_STORE_CONFIG` or the
// long-standing `~/.config/subsystem-store/env`.
func ConfigPath(env envLookup) string {
	if raw := strings.TrimSpace(lookup(env)(ConfigEnv)); raw != "" {
		return expandUser(raw)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "subsystem-store", "env")
}

// InstanceDir is where additional instances live: `instances/` beside the config file.
//
// 🔴 DERIVED FROM THE CONFIG PATH, NOT A SECOND ENVIRONMENT VARIABLE. One variable moves the
// whole configuration — which is what a test needs, and what keeps `$SUBSYSTEM_STORE_CONFIG`
// pointing somewhere while the instances it should sit beside are read from the operator's real
// home directory.
func InstanceDir(env envLookup) (string, error) {
	cfg := ConfigPath(env)
	if cfg == "" {
		return "", &RoutingConfigError{Detail: "cannot resolve a config path — neither $" +
			ConfigEnv + " nor $HOME is set, so there is nowhere to look for instances"}
	}
	return filepath.Join(filepath.Dir(cfg), InstanceDirName), nil
}

// RoutesFile is `(path, explicitly-configured, err)` for the routing table.
//
// The second value is the difference between "the operator named this file" and "this is where a
// table would live if there were one". An explicit path that does not exist is an ERROR; a
// default path that does not exist means there is no routing table, which is the ordinary
// single-instance state.
func RoutesFile(env envLookup) (string, bool, error) {
	if raw := strings.TrimSpace(lookup(env)(RoutesEnv)); raw != "" {
		return expandUser(raw), true, nil
	}
	cfg := ConfigPath(env)
	if cfg == "" {
		return "", false, &RoutingConfigError{Detail: "cannot resolve a config path — neither $" +
			ConfigEnv + " nor $HOME is set, so there is nowhere a routing table could live"}
	}
	return filepath.Join(filepath.Dir(cfg), RoutesFileName), false, nil
}

// LoadRoutes reads the scope→alias table, as a flat JSON object of strings.
//
// 🔴 ONE SHAPE, AND EVERYTHING ELSE IS REFUSED. A table is read by a machine to decide where a
// durable write lands, so "we accepted something odd and did our best with it" is the wrong
// failure mode: a nested object, a list, a non-string value or a non-object document each raise
// rather than silently contributing zero routes. A table that parsed to nothing would leave
// every scope unrouted, which LOOKS like a deliberate refusal and is not.
func LoadRoutes(path string) (map[string]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, &RoutingConfigError{Detail: fmt.Sprintf(
			"the routing table `%s` could not be read: %s", path, pyOSError(err))}
	}
	var document any
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, &RoutingConfigError{Detail: fmt.Sprintf(
			"the routing table `%s` is not valid JSON: %s", path, err)}
	}
	object, ok := document.(map[string]any)
	if !ok {
		return nil, &RoutingConfigError{Detail: fmt.Sprintf(
			"the routing table `%s` must be a JSON object mapping each scope to an instance "+
				"alias, e.g. {\"alpha-notes\": \"personal\"} — got %s",
			path, pyTypeName(document))}
	}
	// 🔴 THE KEYS ARE WALKED IN DOCUMENT ORDER, NOT MAP ORDER, BECAUSE THE FIRST BAD ENTRY IS
	// WHAT GETS REPORTED. `json.loads` hands the oracle an insertion-ordered dict, so a table
	// with two malformed entries names the first one; Go's map iteration is deliberately
	// randomised, which would make the SAME file produce a different message run to run —
	// nondeterminism in a diagnostic is how a fix gets applied to the wrong line.
	table := map[string]string{}
	for _, scope := range jsonObjectKeyOrder(raw) {
		value := object[scope]
		alias, isString := value.(string)
		if !isString || scope == "" {
			return nil, &RoutingConfigError{Detail: fmt.Sprintf(
				"the routing table `%s` maps scope `%s` to %s, which is not an instance alias. "+
					"Every value must be the alias of a configured instance.",
				path, scope, pyValueRepr(value))}
		}
		if !ValidAlias(alias) {
			return nil, &RoutingConfigError{Detail: fmt.Sprintf(
				"the routing table `%s` maps scope `%s` to the alias `%s`, which is not a "+
					"usable alias. An alias becomes a directory name under the cache root, so "+
					"it is lowercase letters, digits and hyphens only.", path, scope, alias)}
		}
		table[scope] = alias
	}
	return table, nil
}

// jsonObjectKeyOrder is the top-level object's keys in DOCUMENT order, deduplicated to the LAST
// occurrence's position — which is where `json.loads` leaves a repeated key, because it
// overwrites in place and a Python dict keeps the FIRST insertion's position.
//
// ⚠ IT RE-SCANS BYTES THE DECODER ALREADY READ, and that is the cheap half of the trade: the
// alternative is a hand-rolled object decoder, which is a second JSON implementation in a program
// whose whole point is not having two of anything.
func jsonObjectKeyOrder(raw []byte) []string {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	if _, err := decoder.Token(); err != nil { // the opening `{`
		return nil
	}
	var order []string
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return order
		}
		key, ok := token.(string)
		if !ok {
			return order
		}
		if !seen[key] {
			seen[key] = true
			order = append(order, key)
		}
		var discard json.RawMessage
		if err := decoder.Decode(&discard); err != nil {
			return order
		}
	}
	return order
}

// pyTypeName is `type(x).__name__` for the six shapes `json.loads` can produce.
func pyTypeName(v any) string {
	switch t := v.(type) {
	case nil:
		return "NoneType"
	case bool:
		return "bool"
	case string:
		return "str"
	case []any:
		return "list"
	case map[string]any:
		return "dict"
	case float64:
		// 🔴 `json.loads` PRODUCES AN `int` FOR AN INTEGER LITERAL AND A `float` OTHERWISE,
		// and Go's decoder collapses both to float64. The literal's own shape is what
		// decides, so a whole number reports `int` — which is the only one of the two a
		// hand-written table is at all likely to contain.
		if t == float64(int64(t)) {
			return "int"
		}
		return "float"
	}
	return "object"
}

// pyValueRepr is `repr(x)` for the same six shapes — enough for the one message that prints a
// rejected value.
//
// ⚠ CONTAINERS ARE NOT RECURSED INTO, AND THAT IS A DECLARED NARROWING RATHER THAN AN OVERSIGHT.
// The oracle prints `repr` of the nested structure; reproducing that is a second Python
// serialiser, in a program whose whole point is not having two of anything. The scope name — the
// part that tells the operator WHICH line to fix — is identical either way, and no parity row
// sends a malformed table.
func pyValueRepr(v any) string {
	switch t := v.(type) {
	case nil:
		return "None"
	case bool:
		if t {
			return "True"
		}
		return "False"
	case string:
		return store.PyRepr(t)
	case []any:
		return "a list"
	case map[string]any:
		return "a dict"
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%v", t)
	}
	return "an unknown value"
}

// Discover is everything configured on this host: instances first, then the table.
//
// 🔴 THE DEFAULT INSTANCE ALWAYS EXISTS, FILE OR NO FILE. `LoadConfig` has always accepted a URL
// and token from the environment with no file at all, and every test in this repository does
// exactly that. Making the default instance conditional on a file would report "no instances
// configured" on a host that is perfectly well configured — a statement about a file rather than
// about the store.
func Discover(env envLookup) (Routing, error) {
	routing := Routing{Instances: []Instance{{Alias: DefaultAlias, ConfigPath: ConfigPath(env)}}}

	directory, err := InstanceDir(env)
	if err != nil {
		return Routing{}, err
	}
	entries, readErr := os.ReadDir(directory)
	if readErr != nil && !os.IsNotExist(readErr) {
		return Routing{}, &RoutingConfigError{Detail: fmt.Sprintf(
			"the instance directory `%s` could not be read: %s. It is not the same fact as "+
				"'no extra instances are configured', so this refuses rather than reporting "+
				"one instance.", directory, pyOSError(readErr))}
	}
	// `os.ReadDir` already sorts by filename, which is the oracle's `sorted(iterdir())`.
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), InstanceSuffix) {
			continue
		}
		// 🔴 AN EDITOR LOCK FILE IS NOT A FILE THE OPERATOR WROTE, AND THE REFUSAL BELOW
		// TOOK EVERY VERB TO EXIT 11 WHILE ONE WAS OPEN. Emacs' lock file for
		// `secondary.env` is `.#secondary.env`: it ends in `.env`, its stem `.#secondary`
		// is not a usable alias, and it is a DANGLING SYMLINK, so `IsDir()` is false and it
		// reached the hard error — meaning every `cairn` invocation on that host refused to
		// run until the buffer was closed. `internal/snapshot` already learned this exact
		// lesson one suffix over (`.#entry.md` 503'd the whole store); the rule there is the
		// rule here — **name rules are separate from type rules**.
		//
		// 🔴 `.#`, NOT `.` — AND THE WIDER PREDICATE WAS SHIPPED AND MEASURED WRONG.
		// Skipping every dotted name also skipped `instances/.env`, whose stem is the EMPTY
		// string: an operator who wrote that file got `instances: personal` at exit 0 and no
		// message anywhere, which is precisely the outcome the refusal below exists to
		// prevent. `.#` is the only in-suffix name a TOOL writes (a vim swapfile is
		// `.secondary.env.swp` and an Emacs autosave is `#secondary.env#` — both fail the
		// `.env` suffix test above and never reach here), so every other dotted name is one
		// a human could have chosen and stays an ERROR. The refusal's stated intent — "a
		// file the operator wrote and would otherwise get no message about" — is what picks
		// the boundary, and the `.` version did not honour it.
		if strings.HasPrefix(entry.Name(), ".#") {
			continue
		}
		alias := strings.TrimSuffix(entry.Name(), InstanceSuffix)
		// 🔴 A FILE THAT CANNOT BE AN INSTANCE IS AN ERROR, NOT A SKIP. Skipping it would
		// leave the operator with a file they wrote, a store they think is configured, and
		// no message anywhere — and the scopes they routed to it would refuse later naming
		// the ALIAS, which points at the table rather than at the file that was ignored.
		if !ValidAlias(alias) || alias == DefaultAlias {
			return Routing{}, &RoutingConfigError{Detail: fmt.Sprintf(
				"`%s` does not name a usable instance alias. An alias becomes a directory "+
					"name under the cache root, so it is lowercase letters, digits and "+
					"hyphens only, and `%s` is reserved for the default instance's own config "+
					"file — one alias with two config sources is a store nobody can point at.",
				filepath.Join(directory, entry.Name()), DefaultAlias)}
		}
		routing.Instances = append(routing.Instances,
			Instance{Alias: alias, ConfigPath: filepath.Join(directory, entry.Name())})
	}

	path, explicit, err := RoutesFile(env)
	if err != nil {
		return Routing{}, err
	}
	if _, statErr := os.Stat(path); statErr == nil {
		table, loadErr := LoadRoutes(path)
		if loadErr != nil {
			return Routing{}, loadErr
		}
		routing.Routes, routing.RoutesSource = table, path
	} else if explicit {
		return Routing{}, &RoutingConfigError{Detail: fmt.Sprintf(
			"$%s names `%s`, which does not exist. A configured table that cannot be read is "+
				"an error, not an absence of routing — treating it as 'no table' would "+
				"silently turn a fail-loud configuration into a fail-open one.",
			RoutesEnv, path)}
	}
	return routing, nil
}
