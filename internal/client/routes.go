package client

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Routes prints the routing table and, with `--check`, grades it against reality.
//
// 🔴 THE TABLE IS AN INPUT, NOT A CONSTANT IN THIS REPOSITORY, AND THAT IS A DELIBERATE
// SEPARATION. A client that shipped somebody's scope→instance map would be publishing their
// taxonomy; this program knows how to route and nothing about who routes where. So the mechanism
// lives here and the table lives wherever the operator keeps it — which makes THIS command the
// only place the two meet, and the reason it grades rather than merely prints.
//
// 🔴 IT GRADES IN BOTH DIRECTIONS. A scope with no entry refuses its next write — loud. An entry
// naming a scope that exists nowhere is the silent one: it reads as coverage and survives a
// rename. See `Routing.Check`, which asks `Routing.AliasFor` for the two directions that are
// predictions about the resolver rather than re-deriving them.
func Routes(env Env, opts Options) (int, error) {
	routing, err := Discover(nil)
	if err != nil {
		return 0, err
	}
	fmt.Fprintf(env.Stdout, "instances: %s\n", joinComma(routing.Aliases()))
	if routing.Routes == nil {
		if !routing.MultiInstance() {
			fmt.Fprintf(env.Stdout,
				"routes: NONE configured — every scope resolves to `%s`\n", DefaultAlias)
		} else {
			fmt.Fprintln(env.Stdout, "routes: NONE configured, and this host has more than one "+
				"instance, so EVERY scope refuses until a table exists")
		}
		if !opts.Check {
			return ExitOK, nil
		}
		if !routing.MultiInstance() {
			return ExitOK, nil
		}
		return ExitUnrouted, nil
	}
	fmt.Fprintf(env.Stdout, "routes: %s\n", routing.RoutesSource)
	var scopesInTable []string
	for scope := range routing.Routes {
		scopesInTable = append(scopesInTable, scope)
	}
	sort.Strings(scopesInTable)
	for _, scope := range scopesInTable {
		fmt.Fprintf(env.Stdout, "  %s -> %s\n", scope, routing.Routes[scope])
	}
	if !opts.Check {
		return ExitOK, nil
	}

	if refusal := refuseSharedCache(env, routing, opts, "routes --check"); refusal != 0 {
		return refusal, nil
	}
	// 🔴 THE SCOPE SET IS READ FROM THE INSTANCES THEMSELVES, REFRESHED FIRST. Grading a table
	// against a stale cache invents both kinds of finding: a scope added elsewhere reads as
	// "the table does not name it", and a scope deleted elsewhere reads as "the table names a
	// scope that does not exist". An instance that cannot be refreshed is therefore named, and
	// the check REFUSES rather than grading against what happens to be on this disk.
	seen := map[string]bool{}
	for _, instance := range routing.Instances {
		cache, cacheErr := instanceCache(opts, instance.Alias)
		if cacheErr != nil {
			return 0, cacheErr
		}
		// 🔴 THE ALIAS IS THREADED, AND THE CACHE ALONE IS NOT ENOUGH. `ResolveState` fetches
		// as well as unpacks, so handing it instance `N`'s cache root while it loads instance
		// `personal`'s credentials writes the DEFAULT instance's snapshot into every other
		// instance's cache — the exact damage `refuseSharedCache` exists to prevent, arriving
		// through the code path rather than through `--cache`. The grader then reads a scope
		// set that belongs to one store and reports the others' scopes as stale.
		state, stateErr := ResolveState(cache, opts.NoSync, "", opts.Timeout, instance.Alias)
		if stateErr != nil {
			return 0, stateErr
		}
		fmt.Fprintln(env.Stderr, bannerFor(state.Name, state.Detail, routing, instance.Alias))
		if state.ExitHint != 0 || state.Name != StateLive {
			fmt.Fprintf(env.Stderr, "🔴 cairn: REFUSING to grade the table — instance `%s` was "+
				"not read LIVE, and a table graded against a stale or absent cache reports "+
				"findings in both directions that are facts about this disk rather than about "+
				"the table.\n", instance.Alias)
			return ExitUnrouted, nil
		}
		entries, readErr := os.ReadDir(cache)
		if readErr != nil {
			return 0, readErr
		}
		for _, entry := range entries {
			info, statErr := os.Stat(filepath.Join(cache, entry.Name()))
			if statErr != nil || !info.IsDir() {
				continue
			}
			seen[entry.Name()] = true
		}
	}
	var scopes []string
	for scope := range seen {
		scopes = append(scopes, scope)
	}
	sort.Strings(scopes)
	problems, notes, checkErr := routing.Check(scopes)
	if checkErr != nil {
		return 0, checkErr
	}
	for _, problem := range problems {
		fmt.Fprintf(env.Stderr, "🔴 cairn: %s\n", problem)
	}
	// ⚠ NOTES CARRY A DIFFERENT MARKER AND DO NOT MOVE THE EXIT CODE. A finding the available
	// evidence cannot decide must not fail a gate — see `Routing.Check`. Printed AFTER the
	// problems so a real finding is never buried.
	for _, note := range notes {
		fmt.Fprintf(env.Stderr, "⚠ cairn: %s\n", note)
	}
	fmt.Fprintf(env.Stdout,
		"routes: %d entr%s, %d scope(s) across %d instance(s), %d problem(s), %d note(s)\n",
		len(routing.Routes), plural(len(routing.Routes)), len(scopes), len(routing.Instances),
		len(problems), len(notes))
	if len(problems) > 0 {
		return ExitUnrouted, nil
	}
	return ExitOK, nil
}

// plural is the oracle's `'y' if n == 1 else 'ies'`, spelled once.
func plural(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

func joinComma(items []string) string {
	out := ""
	for i, item := range items {
		if i > 0 {
			out += ", "
		}
		out += item
	}
	return out
}

// bannerFor is `Banner`, labelled when this host has more than one instance.
//
// 🔴 THE LABEL ASKS THE INSTANCE COUNT, NEVER THE TABLE. `cairn[alias]:` on a host with one
// instance would change the bytes of output on every host that has merely written a routing
// table, which is the compatibility guarantee the whole design rests on.
func bannerFor(state, detail string, routing Routing, alias string) string {
	return BannerNamed(state, detail, instanceLabel(routing, alias))
}

// instanceCache is the cache root for `alias`, honouring an explicit `--cache`.
//
// 🔴 AN EXPLICIT `--cache` WINS, AND THE DEFAULT IS RESOLVED PER INSTANCE. A second instance must
// not share one directory with the first: two stores unpacked over each other would interleave
// scopes, and `.sync-stamp` would date whichever synced last while the entries came from both.
func instanceCache(opts Options, alias string) (string, error) {
	if opts.CacheExplicit {
		return opts.Cache, nil
	}
	return CacheRootFor(alias)
}

// readInstance is `(alias, label, cache)` for a READ of `scope` — the Go spelling of the
// oracle's `_instance_for`. The error is an `*UnroutedScope`, and it NEVER guesses.
//
// 🔴 THE EMPTY `label` IS THE COMPATIBILITY GUARANTEE, NOT A FORMATTING NICETY. It is what
// keeps a one-instance host's output byte-for-byte what it was, so the routing machinery ships
// and is provably inert before any data moves: every rendered caveat, every banner and every
// `ls-entries` line is unchanged where nothing is configured.
//
// 🔴 AND IT IS GATED ON THE INSTANCE COUNT, NEVER ON THE TABLE. A one-instance host that has
// written a routing table is still a host with one place an answer can come from, so it is
// still unlabelled — while `AliasFor` has already consulted that table and may already have
// refused. The two questions are separate; see `Routing.MultiInstance` and `Routing.AliasFor`.
//
// ⚠ `alias` AND `label` ARE DIFFERENT VALUES AND BOTH ARE RETURNED ON PURPOSE. `alias` names
// which instance to read — credentials, cache root, state resolver — and is never empty;
// `label` is what gets PRINTED and is empty at one instance. Collapsing them would either
// label a single-instance host or read the wrong store.
func readInstance(opts Options, scope string) (alias, label, cache string, err error) {
	routing, err := Discover(nil)
	if err != nil {
		return "", "", "", err
	}
	alias, err = routing.AliasFor(scope)
	if err != nil {
		return "", "", "", err
	}
	cache, err = instanceCache(opts, alias)
	if err != nil {
		return "", "", "", err
	}
	return alias, instanceLabel(routing, alias), cache, nil
}

// defaultInstance is `readInstance` where there is no scope to route — the oracle's
// `_default_instance`.
//
// 🔴 THE LABEL STILL APPEARS. "Which of my instances did that read" is exactly as pressing
// when the COMMAND picked one as when a table did.
func defaultInstance(opts Options) (alias, label, cache string, err error) {
	routing, err := Discover(nil)
	if err != nil {
		return "", "", "", err
	}
	cache, err = instanceCache(opts, DefaultAlias)
	if err != nil {
		return "", "", "", err
	}
	return DefaultAlias, instanceLabel(routing, DefaultAlias), cache, nil
}

// instanceLabel is the alias when this host has MORE THAN ONE instance, and "" when it has
// one. ONE spelling, because every read verb needs it and a second copy is a second place for
// the count-versus-table confusion to come back.
func instanceLabel(routing Routing, alias string) string {
	if !routing.MultiInstance() {
		return ""
	}
	return alias
}

// refuseSharedCache refuses an explicit `--cache` that would make N instances share one
// directory. Returns 0 when there is nothing to refuse.
//
// 🔴 THE DAMAGE IS SILENT AND IT IS TO THE CACHE, NOT TO THE OUTPUT. Two snapshots unpacked into
// one root interleave their scopes, and `.sync-stamp` then dates whichever synced last while the
// entries beneath it came from both — a store that can neither say what it holds nor how old it
// is.
func refuseSharedCache(env Env, routing Routing, opts Options, verb string) int {
	if len(routing.Instances) > 1 && opts.CacheExplicit {
		fmt.Fprintf(env.Stderr, "cairn: `%s` walks all %d configured instances (%s), so an "+
			"explicit --cache would make them share one directory and overwrite each other. "+
			"Drop --cache, or name one instance's scope.\n",
			verb, len(routing.Instances), joinComma(routing.Aliases()))
		return ExitUsage
	}
	return 0
}
