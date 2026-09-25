package client

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ZacxDev/cairn/internal/hostid"
	"github.com/ZacxDev/cairn/internal/report"
	"github.com/ZacxDev/cairn/internal/store"
	"github.com/ZacxDev/cairn/internal/write"
)

// Env is the streams and the process identity a verb needs, injected so every branch below is
// reachable from a test with no terminal.
type Env struct {
	Stdout io.Writer
	Stderr *os.File
	// Host is "whose disk is this", threaded to the renderer. nil means hostid.ThisHost.
	Host func() string
}

func (e Env) host() string {
	if e.Host != nil {
		return e.Host()
	}
	return hostid.ThisHost()
}

// Sync refreshes the local cache.
//
// 🔴 IT FAILS WHEN IT COULD NOT REFRESH, EVEN THOUGH A CACHE SURVIVES. This is deliberately
// different from `recall`, and the difference is the whole contract. `recall`'s job is to
// answer, so a stale answer is a success that says it is stale (exit 0). `sync`'s job is to
// REFRESH, so "I did not reach the store" is a failed operation no matter how good the cache is
// — exit non-zero, naming the host. A `sync` that exited 0 on an outage is how a timer reports
// success forever while the cache silently ages out.
//
// 🔴 EVERY CONFIGURED INSTANCE, NOT THE ROUTED ONE. `sync` DOES declare `--scope` — `Verbs()`
// says so, and the oracle's subparser does too — and passes `scope=""` regardless, so it has
// nothing to route. ⚠ "`sync` TAKES NO SCOPE" IS WHAT THIS SENTENCE USED TO SAY AND `--help`
// FALSIFIES IT; the conclusion is the same and the reason is not.
// Refreshing one of two instances while reporting success would leave the other
// silently ageing, which is the failure this function's own contract exists to prevent, one
// instance over. The exit code is the WORST of the walk: one reachable store must never make a
// second one that is down look measured.
func Sync(env Env, opts Options) (int, error) {
	routing, err := Discover(nil)
	if err != nil {
		return 0, err
	}
	if refusal := refuseSharedCache(env, routing, opts, "sync"); refusal != 0 {
		return refusal, nil
	}
	worst := ExitOK
	for _, instance := range routing.Instances {
		label := instanceLabel(routing, instance.Alias)
		cache, cacheErr := instanceCache(opts, instance.Alias)
		if cacheErr != nil {
			return 0, cacheErr
		}
		// 🔴 `scope=""`, ALWAYS. `--scope` used to be threaded through here, which REPLACED
		// THE SHARED CACHE with a one-scope copy: measured, a scoped run took the cache from
		// 305 entries to 2, after which an offline recall of any other scope printed "the
		// store has no 'other-scope/' directory" at exit 0 — a claim about the STORE derived
		// from a filtered cache. The server keeps `?scope=`; it is a legitimate API
		// capability that may not narrow THIS cache.
		//
		// 🔴 AND THE ALIAS IS THREADED, NOT JUST THE CACHE PATH. `ResolveState` FETCHES as
		// well as unpacks, so handing it instance N's cache root while it loads the DEFAULT
		// instance's credentials writes the default instance's snapshot into every other
		// instance's cache — the exact damage `refuseSharedCache` exists to prevent,
		// arriving through the code path rather than through `--cache`.
		state, stateErr := ResolveState(cache, false, "", opts.Timeout, instance.Alias)
		if stateErr != nil {
			return 0, stateErr
		}
		if state.Name == StateLive {
			fmt.Fprintln(env.Stdout, BannerNamed(state.Name, state.Detail, label))
			continue
		}
		fmt.Fprintln(env.Stderr, BannerNamed(state.Name, state.Detail, label))
		if state.ExitHint != 0 {
			worst = max(worst, state.ExitHint)
		} else {
			worst = max(worst, ExitRefreshFailed)
		}
	}
	return worst, nil
}

// LsEntries prints one `<scope>/<entry>.md` per line — per instance when there is more than
// one.
//
// 🔴 THE INSTANCE PREFIX IS A `[alias] ` ON THE LINE, NOT A THIRD PATH SEGMENT. A consumer
// splits these on `/` expecting exactly two parts; making it `alias/scope/entry.md` would
// silently re-point every such split at the wrong field. The prefix is absent entirely on a
// single-instance host.
func LsEntries(env Env, opts Options) (int, error) {
	routing, err := Discover(nil)
	if err != nil {
		return 0, err
	}
	if refusal := refuseSharedCache(env, routing, opts, "ls-entries"); refusal != 0 {
		return refusal, nil
	}
	worst := ExitOK
	for _, instance := range routing.Instances {
		label := instanceLabel(routing, instance.Alias)
		cache, cacheErr := instanceCache(opts, instance.Alias)
		if cacheErr != nil {
			return 0, cacheErr
		}
		state, stateErr := ResolveState(cache, opts.NoSync, "", opts.Timeout, instance.Alias)
		if stateErr != nil {
			return 0, stateErr
		}
		fmt.Fprintln(env.Stderr, BannerNamed(state.Name, state.Detail, label))
		if state.ExitHint != 0 {
			worst = max(worst, state.ExitHint)
			continue
		}
		prefix := ""
		if label != "" {
			prefix = "[" + label + "] "
		}
		// 🔴 THE CACHE ROOT IS ENUMERATED, NOT PATTERN-MATCHED — AND THAT IS A FIX, NOT A
		// REFACTOR. This was `filepath.Glob(filepath.Join(cache, "*", "*.md"))`, which puts
		// the CACHE ROOT inside the pattern: every metacharacter in the operator's own
		// directory name (`[`, `?`, `*`, `\`) is interpreted rather than matched. MEASURED
		// at `a41dd02` with a cache root `…/wid[get` holding `alpha-notes/y.md`:
		// `filepath.Match` returned `ErrBadPattern`, `matches` was nil, the error was
		// DISCARDED, and `ls-entries` printed NOTHING at exit 0 while the oracle printed
		// `alpha-notes/y.md` — the verb that advertises itself as "what the cache actually
		// holds" making a FALSE CLAIM OF ABSENCE. The oracle never had it: `cache.glob(
		// "*/*.md")` treats its anchor literally. This branch closed the identical hazard in
		// `Validate` (by moving to `os.ReadDir`) in the same commit that left it open here.
		//
		// ✅ THE CLASS IS NOW CLOSED, AND THIS PARAGRAPH USED TO SAY IT WAS NOT. It declared
		// two further sites live in this file — `Put`'s pair of `filepath.Glob` calls — and
		// named a third outside it; all of them are fixed, and the one rule they now share is
		// `anchor.go`. The measurements that were recorded here have moved beside the code
		// that carries them (`Put` below, `Focus` in `focus.go`, `ReapOrphans` in
		// `snapshot.go`), because a comment describing a hazard as OPEN is a claim like any
		// other and this one would now be false. Residual 9 in `tests/parity/README.md` is
		// retired with it; what remains there are the two NARROW divergences the fix chose
		// deliberately, which are a different statement.
		//
		// 🔴 A SCOPE'S `README.md` IS ITS POLICY SHEET, NOT AN ENTRY. Until the filter
		// existed `ls-entries` listed every scope's sheet as `<scope>/README.md` — measured
		// twelve of them on a populated cache, under a banner naming the store they came
		// from. The rule is `store.EntryFileNames`, the set helper `store.LoadIndex` and
		// `Validate` enumerate with, so all three sites now answer from ONE walk and ONE
		// predicate rather than from a glob filtered with a re-spelled rule.
		//
		// ⚠ THE SET AND THE ORDER ARE UNCHANGED, AND BOTH WERE MEASURED RATHER THAN
		// ARGUED. Same SET: the glob's middle `*` matched dot-named scope directories and
		// followed symlinked ones, and `os.ReadDir`+`os.Stat` does both (`Stat` follows the
		// link; a symlink to a non-directory, a broken link and a plain file each yielded
		// nothing under the glob too, because `Glob` could not read them as directories).
		// Same ORDER: every path the glob produced shared the `cache` prefix, so
		// `sort.Strings` over the full paths is byte-for-byte the same comparison as
		// `sort.Strings` over the joined `scope/name` strings sorted here. The parity row
		// compares this listing against the oracle's line for line, which is what makes
		// "unchanged" a claim worth stating.
		//
		// ⚠ BOTH READ ERRORS ARE DISCARDED, WHICH IS THE PRE-EXISTING BEHAVIOUR KEPT ON
		// PURPOSE. The glob swallowed its error too, and the oracle's `Path.glob` yields no
		// paths rather than raising — MEASURED on the pinned interpreter (CPython 3.12.14)
		// for BOTH shapes: `Path("<mode-000 root>").glob("*/*.md")` gives `[]`, and with a
		// mode-000 SCOPE directory the same call gives `['alpha/y.md']` — the unreadable
		// child is skipped while its siblings still list. So surfacing either error here
		// would be a divergence, not an improvement.
		//
		// 🔴 THAT IS A MEASUREMENT ABOUT `Path.glob`, NOT ABOUT THE CLIENTS, AND A SENTENCE
		// HERE GENERALISED IT INTO "an unreadable cache root or scope directory contributes
		// no lines, ON BOTH CLIENTS" — TRUE OF ONE SHAPE AND FALSE OF THE OTHER. Re-measured
		// end to end with both real binaries over one cache root holding `alpha/y.md` and
		// `beta/z.md`, `ls-entries --no-sync`:
		//
		//	mode-000 SCOPE dir (`beta`)  go → `alpha/y.md`, exit 0   py → `alpha/y.md`, exit 0
		//	mode-000 CACHE ROOT          go → exit 3, no lines       py → exit 1, TRACEBACK
		//
		// The cache-root row does not reach this walk on EITHER client. `ResolveState` runs
		// first and reads `.sync-stamp`: the Go client turns that into a state whose
		// `ExitHint` is `ExitUnreachableNoCache`, banners `store-unreachable, no cache`, and
		// the loop `continue`s above — so this code never runs. The oracle raises an UNCAUGHT
		// `PermissionError` out of `resolve_state`'s `(cache / SYNC_STAMP).exists()` and
		// exits 1 with a traceback, which is `tests/parity/README.md` residual 4's route, not
		// a quiet empty listing.
		//
		// So the claim this paragraph is entitled to is the narrow one: an unreadable SCOPE
		// DIRECTORY contributes no lines and its siblings still list, on both clients. An
		// unreadable CACHE ROOT is not this walk's case at all, and the two clients diverge
		// on it — above this code rather than in it.
		//
		// 🔴 AND THAT NARROW CLAIM IS STILL TRUE WHILE ITS *JUSTIFICATION* HAS EXPIRED — THIS
		// VERB IS NOW THE ONLY READ THAT STILL SERVES A FALSE ABSENCE FOR A MODE-000 SCOPE
		// DIRECTORY. "Surfacing either error here would be a divergence, not an improvement"
		// rested on the oracle's `Path.glob` swallowing `EACCES`. `cmd_ls_entries` still
		// globs, so the two clients still AGREE — but #119 made exactly that swallow
		// unacceptable one level over: `recall`, `search` and `validate` all answer 3 with
		// `index entry unreadable` for the same directory, and this verb — the one the
		// top-level `README.md` advertises as *"what the cache actually holds"* — answers
		// **exit 0 with the siblings** and says nothing. Agreeing with the oracle is no longer
		// a reason when the oracle's own other verbs disagree with this one.
		//
		// ⬜ **FOLLOW-UP, DELIBERATELY NOT CLOSED HERE** — it changes a verb's stdout and exit
		// code for a state, on both clients, which is its own change with its own reasoning
		// (and `ls-entries` fans out over every instance, so "fail closed" has to decide
		// whether one bad scope refuses the whole listing or just that instance's). **CLOSING
		// CONDITION:** a merged PR after which both clients answer a mode-000 scope directory
		// under `ls-entries` with the reader's own `index entry unreadable` sentence at exit
		// 3, pinned by a PARITY ROW — this case IS statically expressible, unlike the
		// vanished-directory one, so the gate can own it. Mechanically:
		// `python3 tests/parity/harness.py | grep -q '^PASS ls-entries-unreadable-scope-dir'`
		// exits 0, and that row is shown RED at this head. ⚠ A zero-match grep exits 1, so
		// unlike a `go test -run` filter this command cannot read as met when the row is
		// absent — but check the run's own `SUMMARY … failures=0` line too, because a row that
		// FAILED still prints a name.
		//
		// 🔴 THE TWO CLIENTS STILL DIVERGE ON SOME PREFIXED SCOPE NAMES, AND THE CONDITION
		// IS NARROWER THAN "A PREFIX" — A SENTENCE HERE SAID "any cache where one scope name
		// is a prefix of another" AND THAT IS MEASURABLY FALSE. The mechanism is BYTE-WISE vs
		// COMPONENT-WISE: `sort.Strings` here compares the joined `scope/name` byte by byte,
		// while the oracle sorts `Path` objects, whose `__lt__` compares `_parts_normcase` —
		// a tuple, component by component (CPython 3.12.14). Where scope `S` is a proper
		// prefix of scope `T`, the oracle always puts all of `S`'s files first (`S` < `T` as
		// strings); this client compares `/` (0x2f) against `T`'s first byte PAST the prefix,
		// so it agrees when that byte sorts ABOVE `/` and diverges when it sorts BELOW.
		// MEASURED end to end on both clients over a two-scope cache: `a`/`a0` (`0`, 0x30)
		// agree, `a`/`a_b` (`_`, 0x5f) agree, `a`/`aZ` (`Z`, 0x5a) agree; `a`/`a-b` (`-`,
		// 0x2d), `a`/`a.b` (`.`, 0x2e) and `a`/`a+b` (`+`, 0x2b) diverge. A non-prefixed pair
		// cannot diverge at all: the first differing byte then lies inside both scope names,
		// where the two comparisons agree. Seeding `a`/`a0` — which the old sentence invites
		// — yields a GREEN and leaves the gate exactly as blind. PRE-EXISTING (both sorts
		// predate this branch) and deliberately NOT fixed here: it needs a corpus pair AND a
		// decided direction, because agreeing means changing one client's stdout.
		//
		// ⚠ AN UNCLAIMED, MEASURED CONSEQUENCE OF THE WALK, RECORDED BECAUSE IT NAMES THE
		// ONE-LINE REMEDY: `sort.Strings` below is now the ONLY thing producing the byte-wise
		// order. `os.ReadDir` returns scope directories sorted and `EntryFileNames` returns
		// names sorted, so the lines are ALREADY in component-wise (`scope`, then `name`)
		// order — the ORACLE's. MEASURED by deleting that one line and re-running both
		// clients over `a`/`a-b`, `a`/`a.b`, `a`/`a0` and an ordinary six-entry cache: the Go
		// listing was byte-identical to the oracle's in all four. NOT applied here, for the
		// reason in the paragraph above — it is a change to a verb's stdout.
		var lines []string
		scopeDirs, _ := os.ReadDir(cache)
		for _, d := range scopeDirs {
			scopePath := filepath.Join(cache, d.Name())
			info, statErr := os.Stat(scopePath)
			if statErr != nil || !info.IsDir() {
				continue
			}
			names, entryErr := store.EntryFileNames(scopePath)
			if entryErr != nil {
				continue
			}
			for _, name := range names {
				lines = append(lines, d.Name()+"/"+name)
			}
		}
		sort.Strings(lines)
		for _, line := range lines {
			fmt.Fprintf(env.Stdout, "%s%s\n", prefix, line)
		}
	}
	return worst, nil
}

// Report is `recall` and `search` — ONE function, because the state handling, the scope
// derivation, the banner and the exit passthrough are identical and three of them rendered
// differently when they were separate.
func Report(env Env, opts Options, isSearch bool) (int, error) {
	routing, err := Discover(nil)
	if err != nil {
		return 0, err
	}
	// 🔴 `--all-scopes` NAMES NO SCOPE, SO IT IS NOT ROUTED — IT FANS OUT. Asking the routing
	// table about this repo's scope here would refuse a fleet-wide search because the scope
	// the caller did not name is unregistered, which is a refusal about a question nobody
	// asked.
	if isSearch && opts.AllScopes && routing.MultiInstance() {
		return searchEveryInstance(env, opts, routing)
	}

	// 🔴 THE SCOPE IS DERIVED BEFORE THE SYNC, BECAUSE IT DECIDES WHICH STORE TO SYNC. The
	// failure ORDER is preserved deliberately: a store that cannot be read is still reported
	// before a scope that cannot be derived, because that is what this command has always
	// done and the two are independent. That is why `ScopeOrReason` returns the sentence
	// instead of printing it.
	scope, scopeReason := ScopeOrReason(opts.Scope, opts.Repo)

	// No scope means nothing to route: the default instance is the one this host has always
	// read, the default cache root is what `--cache` already resolved, and the usage error
	// below is the real answer. 🔴 UNLABELLED EVEN ON A MULTI-INSTANCE HOST — the run is
	// about to refuse, and naming an instance it did not choose would be a claim about a
	// read that never happened.
	alias, label, cache := DefaultAlias, "", opts.Cache
	if scopeReason == "" {
		// 🔴 BEFORE THE NETWORK. Nothing is fetched, nothing is written and no cache is
		// touched: the client does not know where this scope lives, so any store it
		// contacted would be a guess. The `*UnroutedScope` escapes to `Run`, which is the
		// ONE spelling of the refusal.
		var routeErr error
		alias, label, cache, routeErr = readInstance(opts, scope)
		if routeErr != nil {
			return 0, routeErr
		}
	}

	// 🔴 SYNC THE WHOLE STORE, NEVER `scope=opts.Scope`. A scope-filtered cache makes the
	// reader answer `scope-absent` for every scope that was simply not fetched —
	// indistinguishable, in the output, from a scope the store has never held.
	state, err := ResolveState(cache, opts.NoSync, "", opts.Timeout, alias)
	if err != nil {
		return 0, err
	}
	if state.ExitHint != 0 {
		fmt.Fprintln(env.Stderr, BannerNamed(state.Name, state.Detail, label))
		return state.ExitHint, nil
	}
	if scopeReason != "" {
		fmt.Fprintln(env.Stderr, scopeReason)
		return ExitUsage, nil
	}

	var text, status, exitLabel string
	var malformed []store.MalformedEntry
	if isSearch {
		rep, searchErr := report.Search(cache, report.SearchOptions{
			Scope:     scope,
			Query:     opts.Query,
			Context:   report.ContextBullet,
			Threshold: report.DefaultThreshold,
			MaxHits:   report.DefaultMaxHits,
			AllScopes: opts.AllScopes,
		}, store.Unrestricted())
		if searchErr != nil {
			return 0, searchErr
		}
		text = rep.RenderText(env.host(), nil, label)
		status, malformed = rep.Status, rep.Malformed
		// 🔴 SEARCH USES ITS OWN EXIT LABEL. `SearchReport.Label()` names the scopes SEARCHED;
		// passing the query instead made the reader's failure sentence say "`lease` holds 1
		// entry file" — naming the search term as if it were a scope path.
		// ⚠ IT IS NOT THE INSTANCE LABEL. Two things called `label` met here when the read
		// verbs learned to route; they are the reader's exit operand and the printed alias,
		// and swapping them would put a scope path in a caveat and a scope set in an alias.
		exitLabel = rep.Label()
		if state.Name == StateLive && isEmptyStatus(rep.Status) {
			state = emptyState(state)
		}
	} else {
		// 🔴 THE DIGEST'S OWN FOOTER PRESCRIBES THESE FLAGS, so the client has to have
		// them. The mapping and the refusals are NOT re-derived per caller.
		if refusal := RejectRecallFlags(opts.HasRef, opts.List, opts.Limit, opts.Page); refusal != "" {
			fmt.Fprintf(env.Stderr, "cairn: %s\n", refusal)
			return ExitUsage, nil
		}
		selection := RecallSelectionFor(opts.List, opts.Limit, opts.Page)
		// `--mode` stays authoritative when the caller passed it explicitly: it predates
		// these flags and something may already drive it.
		mode := opts.Mode
		if mode == report.DefaultMode {
			mode = selection.Mode
		}
		// 🔴 THE FEATURED-ENTRY PICK NEEDS A FOCUS WINDOW, AND A CLIENT THAT NEVER BUILT ONE
		// COULD ONLY EVER PRINT `most-recent fallback`, whatever was actually relevant.
		// Measured on a real store, same repo and same moment: the wrapper said "most-recent
		// fallback … (no handoff doc to read a path window from)" while the module said
		// "resolved via claudedocs/handoff-<topic>.md — 11 of 48 quoted path(s) name it".
		// The parenthetical was the tell and it was WRONG about the world.
		//
		// The condition mirrors the reader's own: a window is a claim about THIS repo's
		// newest handoff doc, so it is meaningless once the caller names a scope directly or
		// asks for a non-default mode.
		var window FocusWindow
		if mode == report.DefaultMode && opts.Scope == "" {
			window = Focus(opts.Repo)
		}
		recallOpts := report.RecallOptions{
			Scope:       scope,
			Ref:         opts.Ref,
			HasRef:      opts.HasRef,
			Limit:       selection.Limit,
			Mode:        mode,
			Page:        selection.Page,
			FocusPaths:  window.Paths,
			FocusSource: window.Source,
		}
		// 🔴 THE OPTION LADDER IS THE READER'S, RUN HERE. `report.Recall` does not re-run it
		// (one rule, one place), so a caller that skipped it would hand the renderer an
		// unvalidated option — and `--limit 0` used to raise an UNCAUGHT error at rc 1 where
		// the module's own entrypoint answered 2 with the message alone.
		if vErr := report.ValidateRecall(recallOpts); vErr != nil {
			fmt.Fprintf(env.Stderr, "cairn: %s\n", vErr)
			return ExitUsage, nil
		}
		rep, recallErr := report.Recall(cache, recallOpts, store.Unrestricted())
		if recallErr != nil {
			return 0, recallErr
		}
		text = rep.RenderText(env.host(), nil, label)
		status, malformed = rep.Status, rep.Malformed
		// ⚠ THE EXIT LABEL IS DERIVED, NOT AN ATTRIBUTE. The recall report has no label field,
		// and assuming it did was an AttributeError that took every recall to exit 1 on the
		// Python side. Derived exactly as the pod's own `Reader.Recall` derives it.
		exitLabel = rep.Scope + "/"
		if state.Name == StateLive && isEmptyStatus(rep.Status) {
			state = emptyState(state)
		}
	}

	fmt.Fprintln(env.Stdout, BannerNamed(state.Name, state.Detail, label))
	fmt.Fprintln(env.Stdout)
	fmt.Fprintln(env.Stdout, text)
	// 🔴 THE READER'S OWN EXIT CODE, PASSED THROUGH. Hardcoding 0 here was a measured defect:
	// on an all-malformed scope the reader exited 3 and the client exited 0. The stdout text
	// was loud either way, so a human was not deceived — but a machine consumer branches on
	// the CODE, and it was.
	//
	// 🔴 AND THE WARNING SENTENCE IS FORWARDED. `report.ExitFor` RETURNS it instead of
	// writing to stderr from inside the library (the one deliberate difference from the
	// oracle in that path), so the caller that drops it is the caller that loses the signal.
	code, warning := report.ExitFor(status, exitLabel, malformed)
	if warning != "" {
		fmt.Fprintln(env.Stderr, warning)
	}
	return code, nil
}

// searchEveryInstance is `search --all-scopes` across every configured instance.
//
// 🔴 A PARTIAL RESULT IS A LIE, SO AN UNREAD INSTANCE IS LOUD AND NON-ZERO. "No matches" from
// a fan-out that silently skipped a store is the same silent zero this client was built to
// prevent, one layer up: the caller concludes the thing is not recorded anywhere. Hits that
// WERE found are still printed — throwing away real answers to report a defect is its own harm
// — but the run exits non-zero and names the instance it could not read.
//
// 🔴 AND EACH SECTION NAMES ITS INSTANCE UNCONDITIONALLY. This function only runs when there
// is more than one, so there is no single-instance case to keep unlabelled here — and without
// the name, two hits with the same ref from two stores are indistinguishable, which is the
// question a second instance creates.
func searchEveryInstance(env Env, opts Options, routing Routing) (int, error) {
	if refusal := refuseSharedCache(env, routing, opts, "search --all-scopes"); refusal != 0 {
		return refusal, nil
	}
	worst := ExitOK
	var unread []string
	for _, instance := range routing.Instances {
		cache, cacheErr := instanceCache(opts, instance.Alias)
		if cacheErr != nil {
			return 0, cacheErr
		}
		state, stateErr := ResolveState(cache, opts.NoSync, "", opts.Timeout, instance.Alias)
		if stateErr != nil {
			return 0, stateErr
		}
		if state.ExitHint != 0 {
			fmt.Fprintln(env.Stderr, BannerNamed(state.Name, state.Detail, instance.Alias))
			unread = append(unread, instance.Alias)
			worst = max(worst, state.ExitHint)
			continue
		}
		// 🔴 `Scope: ""` WITH `AllScopes`, WHICH IS THE ORACLE'S CALL EXACTLY. The caller
		// named no scope; passing one derived from the repo would narrow the "elsewhere"
		// accounting of a fleet-wide search to a scope nobody asked about.
		rep, searchErr := report.Search(cache, report.SearchOptions{
			Scope:     "",
			Query:     opts.Query,
			Context:   report.ContextBullet,
			Threshold: report.DefaultThreshold,
			MaxHits:   report.DefaultMaxHits,
			AllScopes: true,
		}, store.Unrestricted())
		if searchErr != nil {
			return 0, searchErr
		}
		fmt.Fprintln(env.Stdout, BannerNamed(state.Name, state.Detail, instance.Alias))
		fmt.Fprintln(env.Stdout)
		fmt.Fprintln(env.Stdout, rep.RenderText(env.host(), nil, instance.Alias))
		fmt.Fprintln(env.Stdout)
		code, warning := report.ExitFor(rep.Status, rep.Label(), rep.Malformed)
		if warning != "" {
			fmt.Fprintln(env.Stderr, warning)
		}
		worst = max(worst, code)
	}
	if len(unread) > 0 {
		fmt.Fprintf(env.Stderr, "🔴 cairn: PARTIAL — %d of %d instance(s) could not be read "+
			"(%s). Anything printed above is what the instances that ANSWERED hold; it is "+
			"NOT an answer about the ones that did not, and 'no matches' above does not mean "+
			"the query matches nothing.\n", len(unread), len(routing.Instances),
			joinComma(unread))
	}
	return worst, nil
}

func isEmptyStatus(status string) bool {
	return status == report.StatusScopeEmpty || status == report.StatusScopeAbsent
}

// emptyState is the `scope-empty` promotion. 🔴 IT KEEPS `Detail`, IT DOES NOT REPLACE IT. The
// pre-fix version overwrote it wholesale, which threw away the server's freshness stamp exactly
// when it mattered most: if the pod could not read a scope, that stamp is where
// `newest=UNREADABLE` appears, and discarding it turned an unreadable scope into a confident
// "nothing recorded".
func emptyState(state State) State {
	return State{
		Name:   StateEmpty,
		Detail: "reached the store; nothing recorded for this scope — " + state.Detail,
	}
}

// Validate is the POST-WRITE check over the cached entries, and the parse count is
// only its first half. It parse-checks with the READER'S OWN parser, then reports the
// write-protocol advisories — entry shape, dropped lines, open actions and marker
// reachability — which answer a different question: not "would the loader accept this
// file?" but "does it hold text no reader will ever surface?". No advisory moves the
// exit code — the write protocol branches on that code to mean "write NOTHING", and
// failing here would stop a session recording anything into an entry whose only defect
// is that an OLDER write lost a line. ⚠ "It does not move the exit code" is a claim
// about the ADVISORY, never about the command: a non-regular path in the cache is
// still a malformed entry and still exits 5, and a round of this PR briefly made that
// a crash instead — see `store.nuanceBody`.
//
// 🔴 THE RESOLVER IS THE PARSER, so `validate` and `recall` cannot disagree about what
// "malformed" means. The Python version once shelled a separate authoring tool's `--validate`,
// which was a second implementation of the same predicate — exactly the shape that lets a file
// validate clean and then fail to render.
// 🔴 ONE INSTANCE, ROUTED BY `--scope`/`--repo` LIKE A READ. Validation is a claim about the
// BYTES of a particular cache, so it names which one rather than merging several — a merged
// verdict could not say where the malformed file is. With no scope at all the default instance
// is the answer, which is what `RepoScope` swallowing its error is for.
func Validate(env Env, opts Options) (int, error) {
	scope := opts.Scope
	if scope == "" {
		scope = RepoScope(opts.Repo)
	}
	var alias, label, cache string
	var err error
	if scope != "" {
		alias, label, cache, err = readInstance(opts, scope)
	} else {
		alias, label, cache, err = defaultInstance(opts)
	}
	if err != nil {
		return 0, err
	}
	state, err := ResolveState(cache, opts.NoSync, "", opts.Timeout, alias)
	if err != nil {
		return 0, err
	}
	fmt.Fprintln(env.Stderr, BannerNamed(state.Name, state.Detail, label))
	if state.ExitHint != 0 {
		return state.ExitHint, nil
	}
	// 🔴 THE CACHE IS A MULTI-SCOPE STORE; VALIDATION IS SCOPE-BOUND. With no `--scope` a
	// repo-derived scope would validate a scope the cache does not hold and print "NOTHING WAS
	// CHECKED — a zero here is NOT a clean bill of health" while exiting 0. With no `--scope`
	// we validate EVERY scope in the cache.
	var held []string
	entries, readErr := os.ReadDir(cache)
	if readErr != nil {
		return 0, readErr
	}
	for _, e := range entries {
		info, statErr := os.Stat(filepath.Join(cache, e.Name()))
		if statErr != nil || !info.IsDir() {
			continue
		}
		held = append(held, e.Name())
	}
	sort.Strings(held)

	var scopes []string
	if opts.Scope != "" {
		// 🔴 THE SILENT ZERO THE NO-SCOPE PATH WAS REWRITTEN TO CLOSE, WHICH THE EXPLICIT
		// `--scope` PATH THEN COMMITTED ANYWAY: passing the value straight through made
		// `validate --scope no-such-scope` print the writer's own "NOTHING WAS CHECKED"
		// and exit 0. The fix covered the case somebody was looking at, not the predicate.
		if !containsString(held, opts.Scope) {
			shown := strings.Join(held, ", ")
			if shown == "" {
				shown = "(none)"
			}
			fmt.Fprintf(env.Stderr, "cairn: cache holds no scope %s — nothing was "+
				"validated. Held: %s\n", store.PyRepr(opts.Scope), shown)
			return ExitUsage, nil
		}
		scopes = []string{opts.Scope}
	} else {
		scopes = held
	}
	if len(scopes) == 0 {
		fmt.Fprintf(env.Stderr, "cairn: nothing to validate — %s holds no scopes\n", cache)
		return ExitUnreachableNoCache, nil
	}

	worst := ExitOK
	for _, scope := range scopes {
		// 🔴 THROUGH `LoadStore`, NOT `LoadIndex` DIRECTLY, AND THE ORACLE'S
		// `cmd_validate` MOVED IN THE SAME COMMIT. `LoadIndex`'s residual ledger says a
		// `Take` kind whose read fails "fails closed into a store-wide
		// EntryUnreadableError" — that wrap is `LoadStore`'s, and this line bypassed it,
		// so a mode-000 entry reached the CLI as a bare `*os.PathError` and printed
		// `open <path>: permission denied` where every other reader prints the named
		// `index entry unreadable: under <root> (PermissionError: …)` sentence. The exit
		// code was already 3 via `cli.go`'s reader-error arm; the TEXT was not the
		// oracle's — and the oracle's own answer was a traceback at exit 1. Both sides
		// now read the store through the one function that owns the policy. #111.
		index, loadErr := store.LoadStore(cache, "validated",
			store.VisibleScopeSet([]string{scope}))
		if loadErr != nil {
			return 0, loadErr
		}
		for _, bad := range index.Malformed {
			fmt.Fprintf(env.Stderr, "cairn: %s: malformed: %s\n", scope, pyMalformedRepr(bad))
		}
		// 🔴 REPORT WHAT WAS CHECKED, NOT ONLY WHAT WAS WRONG. Until this line a CLEAN scope
		// printed NOTHING and exited 0, which is byte-identical to a validate that parsed no
		// files at all — and this command is the post-write check the write protocol
		// MANDATES, so that zero was being read as "the entry I just wrote is fine". A count
		// that MOVES with the store is what makes the zero mean something.
		//
		// 🔴 `README.md` IS NOT AN ENTRY, AND THE NUMERATOR ALREADY KNEW THAT. The rejections
		// come from `store.LoadIndex`, which skips `README.md` in every scope ("each scope
		// directory carries one as its store-policy sheet"); this count came from a bare
		// `*.md` glob that did not. Two walks, one line — and `/snapshot` ships those
		// READMEs, so this fired against the real store: a scope holding two entries beside
		// its policy sheet printed `3 of 3`, a scope holding ONLY a policy sheet printed
		// `1 of 1`, and — the direction that misleads — one BROKEN entry beside a README
		// printed `1 of 2 … 1 malformed`, asserting that a file parsed when none had. This is
		// the command whose whole job is making a zero mean something.
		//
		// 🔴 THE DENOMINATOR NOW APPLIES THE LOADER'S OWN RULE FOR WHAT AN ENTRY IS, THROUGH
		// THE LOADER'S OWN WALK FUNCTION — AND THAT IS ALL IT IS. The first fix filtered a
		// `*.md` glob with an open-coded `!= "README.md"`, which closed the symptom and left
		// the mechanism — two spellings of one rule behind one line — intact.
		// `store.EntryFileNames` is the function `store.LoadIndex` enumerates with, so the
		// numerator and the denominator cannot come to disagree about what an ENTRY is.
		//
		// 🔴 THEY CAN STILL DISAGREE ABOUT WHICH DIRECTORIES TO COUNT, AND AN EARLIER FORM OF
		// THIS COMMENT CLAIMED OTHERWISE — IT SAID "THE DENOMINATOR IS NOW THE LOADER'S OWN
		// WALK", WHICH IS FALSE. It is the loader's RULE over a DIFFERENT DIRECTORY SET:
		// `LoadIndex` selects scope directories through `ScopeSet.Allows`, which compares
		// `NormalizeRef(name)` — FOLDED — so every directory whose name normalizes to
		// `--scope`'s value feeds the numerator, while this line walks the ONE LITERAL
		// `<cache>/<scope>` directory. MEASURED at this commit on both clients over a cache
		// holding `kelp-forest/a.md` and `Kelp_Forest/b.md`, both unparseable:
		//
		//	cairn --cache <root> validate --scope kelp-forest --no-sync
		//	  → cairn: kelp-forest: -1 of 1 entry file(s) parse, 2 malformed   (exit 5)
		//
		// A NEGATIVE count — which the oracle's own sibling block (`cairn`, the
		// `cache`-not-`args.cache` paragraph) names as the thing a contract cannot include.
		// PRE-EXISTING and IDENTICAL in both clients, so the parity gate is structurally blind
		// to it. Deliberately NOT closed here: deciding whether `--scope kelp-forest` means
		// the folded set or the literal directory changes a verb's stdout and needs its own
		// change. CLOSING CONDITION — a merged PR carrying a test per client that seeds those
		// two directories and asserts the printed line, shown RED at this commit; mechanically,
		// `go test ./internal/client/ -run FoldVsLiteral -count=1 -v` and
		// `python3 -m pytest tests -q -p no:randomly -k fold_vs_literal` each SELECT at least
		// one test and exit 0.
		//
		// 🔴 A ZERO-SELECTION RUN IS **NOT** THE MET STATE, AND ON THE GO HALF IT IS
		// INDISTINGUISHABLE FROM ONE BY EXIT CODE ALONE. MEASURED at this head, with no such
		// test in the tree: `go test ./internal/client/ -run FoldVsLiteral -count=1 -v` prints
		// `testing: warning: no tests to run`, `PASS`, `ok … [no tests to run]` and EXITS 0 —
		// so an operator checking `$?` reads this row as already closed. The condition's text
		// says "SELECT at least one test AND exit 0", which is well formed; the exit code
		// alone cannot witness the first half. Require a `--- PASS: TestFoldVsLiteral…` line
		// in the `-v` output, or run `go test -json` and require at least one `"Action":"pass"`
		// carrying a `"Test"` field. `[no tests to run]` is the UNMET state.
		// ⚠ The PYTHON half does not share the hazard — measured the same way, the `-k` filter
		// selecting nothing prints `2050 deselected` and exits **5**, not 0. The two commands
		// therefore need different checks, which is why this paragraph names both.
		//
		// 🔴 THE READ ERROR IS NO LONGER DISCARDED, AND THE JUSTIFICATION FOR DISCARDING IT
		// WAS VOIDED BY THE SAME COMMIT THAT MADE THIS READ ABLE TO FAIL. This line was
		// `entryNames, _ := store.EntryFileNames(...)`, and the reason recorded here was "it
		// is still swallowed because the oracle swallows it rather than raising, so surfacing
		// it would be a divergence with nothing behind it — MEASURED on the pinned
		// interpreter (CPython 3.12.14), `Path("<mode-000 dir>").glob("*.md")` yields `[]`
		// rather than a `PermissionError`". #119 replaced that glob with `iterdir()`, so the
		// measurement was FALSE at that very head: the oracle raises, and "a divergence with
		// nothing behind it" became a divergence with the whole of #111 behind it. The rest of
		// the old sentence was right and is kept — `LoadIndex` above has ALREADY walked this
		// directory and RETURNED on any error, so ABSENT CONCURRENT MUTATION a failure here is
		// unreachable; a scope directory removed, renamed or chmod'd BETWEEN the two walks, or
		// an `EMFILE`/`ENOMEM` at this call, is what reaches it.
		//
		// 🔴 AND THE SWALLOWED OUTCOME WAS NOT MERELY DIVERGENT, IT WAS A FALSE COUNT.
		// `checked = 0` beside a non-zero malformed count is the negative-count nonsense the
		// block above names, and `0 of 0 entry file(s) parse, 0 malformed` over a directory
		// nothing read is the confident zero this verb exists to prevent — the same false
		// ABSENCE as the `scope-empty` defect one level up. MEASURED at `e162746` over one
		// cache holding two scopes, the second removed after the first scope's line was
		// printed (`validate --no-sync`, no `--scope`): this client printed that line at exit
		// **0** while the oracle died with a `FileNotFoundError` TRACEBACK at exit **1**.
		// `EntryFilesOrUnreadable` is `LoadStore`'s OWN wrap (`store.StoreUnreadable`, one
		// writer for both sites), so the error reaches `cli.go`'s reader-error arm and both
		// clients answer 3 with identical bytes.
		// `TestAVanishedScopeDirectoryIsNotCountedAsZeroEntries` gates it here;
		// `tests/test_cairn_cli.py::TestAScopeThatVANISHESMidRunIsNotServedAsZeroOfZero` gates
		// the oracle. The parity harness cannot: no STATIC world reaches this read, because
		// both walks resolve the same directory from the same listing — see
		// `tests/parity/README.md`, "What the gate structurally cannot see".
		//
		// ⚠ AN UNCLAIMED CONSEQUENCE, RECORDED BECAUSE NO FIXTURE COVERS IT: reading the
		// directory (`EntryFileNames` → `os.ReadDir`) instead of globbing
		// `<cache>/<scope>/*.md` also removes a latent divergence for scope names carrying
		// glob metacharacters. The scope name used to be part of the PATTERN, while the
		// oracle's `Path(scope_dir).glob("*.md")` globs only the pattern and treats the
		// directory literally. MEASURED: a directory literally named `wid[get` gave Go `[]`
		// plus `syntax error in pattern` — `validate` would have printed `0 of 0` — where the
		// oracle listed the file. NOT claimed as a fix: nothing here exercises such a scope
		// name, and whether one can reach a cache at all is not established.
		entryNames, walkErr := store.EntryFilesOrUnreadable(cache, filepath.Join(cache, scope))
		if walkErr != nil {
			return 0, walkErr
		}
		checked := len(entryNames)
		fmt.Fprintf(env.Stdout, "cairn: %s: %d of %d entry file(s) parse, %d malformed\n",
			scope, checked-len(index.Malformed), checked, len(index.Malformed))
		// 🔴 THE PARSE COUNT IS NOT THE WRITE-PROTOCOL CHECK, AND UNTIL THESE
		// BLOCKS IT WAS THE WHOLE OF WHAT THIS COMMAND REPORTED. "Would the loader
		// accept this file?" is answered by the line above; an entry can pass it while
		// holding text NO reader will ever surface. `dropped lines:` is the half that
		// means content is ALREADY LOST — the file holds it, the store holds it, and
		// `--ref`, `--search`, the digest and every openness count skip it. A
		// post-write check that cannot see that is checking the parser, not the write.
		//
		// 🔴 THEY SCAN THE MALFORMED FILES TOO, and that is not an oversight: every
		// scanner is tolerant by construction (an unreadable file or a missing nuance
		// section contributes nothing), and a file the loader rejected can still hold
		// lost content that a later fix to its front matter would not restore. The
		// rejection above is still the finding that matters, which is why these print
		// BELOW it and change no verdict.
		//
		// 🔴 NEITHER MOVES `worst`. The write protocol branches on this command's EXIT
		// CODE to mean "write NOTHING", so failing here would stop a session recording
		// anything into an entry whose only defect is that an OLDER write lost a line —
		// which makes the store lossier, not safer.
		entryPaths := make([]string, 0, len(entryNames))
		for _, name := range entryNames {
			entryPaths = append(entryPaths, filepath.Join(cache, scope, name))
		}
		// 🔴 THE ADVISORIES' DENOMINATOR IS `ScannedEntryCount`, NOT `checked`, AND
		// THAT IS A THIRD SET. `checked` is the LISTING — every `*.md` name in the
		// scope directory, which is the right denominator for the parse line above
		// because the loader tries every one of them. The scanners do not: they read
		// only the kinds the loader's own table TAKES, refusing a FIFO, a device, a
		// directory or a dangling link before `open()`. Handing them `checked`
		// therefore printed a zero over files nothing had opened — MEASURED on a scope
		// holding one entry beside a FIFO: `dropped lines: 0 across 2 entry file(s)`,
		// with one of the two never read. The FIFO is not lost from the output; it is
		// reported malformed on stderr by the line above and drives the exit to 5.
		//
		// 🔴 FOUR BLOCKS, AND `entry shape:` PRINTS FIRST FOR A REASON THE OTHER THREE
		// CANNOT STATE FOR THEMSELVES. All three of the others read only the nuance
		// heading, so an entry whose heading is RENAMED contributes zero to every one of
		// them — `open actions: 0 declared` over a section no parser ever reached. The
		// shape block is the one that says why, and it has to be ABOVE them to be read
		// as the reason rather than as an afterthought. None of the four moves `worst`.
		for _, line := range store.ValidationAdvisoryLines(
			store.ScannedEntryCount(entryPaths),
			store.ScanEntryShape(entryPaths),
			store.ScanDroppedLines(entryPaths),
			store.ScanOpenActions(entryPaths),
			store.ScanUnreachableMarkers(entryPaths),
		) {
			// A blank separator stays blank — prefixing it would print a trailing
			// `cairn: <scope>: ` with nothing after it, and the parity gate would then
			// pin that noise in both clients forever.
			if line == "" {
				fmt.Fprintln(env.Stdout)
				continue
			}
			fmt.Fprintf(env.Stdout, "cairn: %s: %s\n", scope, line)
		}
		if len(index.Malformed) > 0 && ExitCorrupt > worst {
			worst = ExitCorrupt
		}
	}
	return worst, nil
}

// pyMalformedRepr is the DATACLASS repr of the oracle's `MalformedEntry`, because `validate`
// interpolates the object itself (`f"… malformed: {entry}"`) rather than its `.line`.
//
// ⚠ THAT IS THE ORACLE'S CHOICE AND ARGUABLY THE WRONG ONE — `.line` carries the sentinel
// phrase a reader greps for — but it is the printed contract, and reproducing it is not the same
// as endorsing it. Changing it is a change to BOTH clients in one commit, not a difference for
// one of them to introduce.
func pyMalformedRepr(m store.MalformedEntry) string {
	return fmt.Sprintf("MalformedEntry(scope=%s, filename=%s, reason=%s)",
		store.PyRepr(m.Scope), store.PyRepr(m.Filename), store.PyRepr(m.Reason))
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

// writeInstance is `(alias, config, cache)` for a write, or an `*UnroutedScope` that never
// guesses. It is the Go spelling of the oracle's `_instance_for`.
//
// 🔴 A WRITE NAMES ITS INSTANCE UNCONDITIONALLY, UNLIKE A READ'S LABEL. "Where did that bullet
// go" is a question about a DURABLE record, asked later, by someone who no longer has the
// terminal — so the alias is printed at one instance and at many, and the oracle does the same.
//
// 🔴 AND THE CONFIG COMES FROM THE ROUTED INSTANCE, NOT FROM THE DEFAULT ONE. Printing the right
// alias while sending to the wrong store is the shape of bug a message check cannot see.
//
// 🔴 THE CACHE IS RESOLVED FOR EVERY WRITE, AND ITS TWO CALLERS HERE **DISCARD** IT — that is
// deliberate, not an oversight, so do not "simplify" this to `(alias, config)`. `CacheRootFor`
// can FAIL (an unresolvable HOME), and the oracle's `cmd_append`/`cmd_create` reach that failure
// too because `_instance_for` calls `_instance_cache` unconditionally. Dropping the value would
// drop the error check with it and make the two clients disagree on that host.
//
// ⚠ `put` no longer comes through here at all — it calls `writeRoute` and loads the credentials
// LATE, for the reason written there. The cache-ordering hazard this comment used to carry (a
// routed write deriving its `If-Match` from `opts.Cache`, i.e. the DEFAULT instance's root) is
// recorded at `Put`'s own call site, where the ordering it constrains now lives.
// ⚠ `cacheRoot` RATHER THAN `cache`, AND THE NAME IS LOAD-BEARING FOR A REASON OUTSIDE THE CODE:
// `tests/routing_mutants.py` anchors its mutants on exact source lines and asserts each anchor
// occurs EXACTLY ONCE. Spelling this destructuring identically to `Put`'s made the battery REFUSE
// — correctly, because an anchor matching twice cannot be attributed — so the two differ by one
// name. That refusal is the harness working; do not resolve it by loosening the anchor. (And do
// not restate the anchor text in a comment: a comment that quotes it can become the second
// match.)
func writeInstance(opts Options, scope string) (string, Config, string, error) {
	alias, cacheRoot, err := writeRoute(opts, scope)
	if err != nil {
		return "", Config{}, "", err
	}
	cfg, err := LoadConfigFor(alias)
	if err != nil {
		return "", Config{}, "", err
	}
	return alias, cfg, cacheRoot, nil
}

// writeRoute is the ROUTE half of `writeInstance` — `(alias, cache)`, with NO credential load.
//
// 🔴 IT EXISTS BECAUSE *WHEN* THE CREDENTIALS ARE LOADED IS AN OBSERVABLE, AND THE TWO CLIENTS
// DISAGREED ABOUT IT. The oracle's `cmd_put` calls `resolve_state` and only then `load_config`,
// so a routed instance whose config file is INCOMPLETE surfaces inside the state resolver, as a
// non-live state, and `put` refuses with its own sentence naming the cache it could not refresh.
// This port loaded the config EAGERLY in `writeInstance`, so the same host produced the same exit
// code (7) with different BYTES: the error escaped to `cli.go`'s write-unreachable arm — "the
// write did NOT happen — config incomplete: … Re-run when the store is reachable." Exit codes
// agreed, which is why every gate stayed green, and `append` is byte-identical on the same input
// because the oracle loads the config eagerly THERE too. So the divergence is not "Go is eager";
// it is "Go was eager on the ONE verb where the oracle is lazy".
//
// ⚠ `Append` and `Create` keep using `writeInstance`, deliberately: both need the credentials
// before anything else can happen, and both oracle verbs load them at the same point. Splitting
// the helper rather than reordering it is what keeps those two unmoved.
func writeRoute(opts Options, scope string) (string, string, error) {
	routing, err := Discover(nil)
	if err != nil {
		return "", "", err
	}
	alias, err := routing.AliasFor(scope)
	if err != nil {
		return "", "", err
	}
	cache, err := instanceCache(opts, alias)
	if err != nil {
		return "", "", err
	}
	return alias, cache, nil
}

// Append appends ONE dated bullet — `POST /api/v1/entry/<scope>/<ref>/bullets`.
//
// 🔴 DO NOT DATE-PREFIX `--text`. The server prepends `- <date>: ` itself; the first production
// append through this route read `- <date>: <date>: …` because a caller did.
func Append(env Env, opts Options) (int, error) {
	scope := ResolveScope(opts.Scope, opts.Repo, env.Stderr)
	if scope == "" {
		return ExitUsage, nil
	}
	// 🔴 REFUSE LOCALLY, BEFORE THE NETWORK, AND SAY THE OVERAGE. The server enforces this cap
	// and always did; what it could not do is tell a caller the limit before they had composed
	// something over it. Measured cost of that asymmetry: one bullet took three full
	// recompose-and-retry cycles, with nothing written on any of the three.
	//
	// 🔴 THE SAME CONSTANT AS THE SERVER'S, IMPORTED — not a second 2000 typed here. A
	// client-side copy that drifted LOW would refuse writes the store would have accepted.
	//
	// ⚠ IT IS A CONVENIENCE, NOT AN AUTHORITY. The server re-checks; a client that skipped
	// this could not widen what the store accepts.
	if runes := len([]rune(opts.Text)); runes > write.BulletTextMax {
		fmt.Fprintf(env.Stderr, "cairn: refusing to send — `--text` is %d characters, max "+
			"%d — %d over. Nothing was written, and the store was not contacted.\n",
			runes, write.BulletTextMax, runes-write.BulletTextMax)
		return ExitUsage, nil
	}
	alias, cfg, _, err := writeInstance(opts, scope)
	if err != nil {
		return 0, err
	}
	payload, err := pyJSONObject([][2]string{{"text", opts.Text}, {"session", opts.Session}})
	if err != nil {
		return 0, err
	}
	path := fmt.Sprintf("/api/v1/entry/%s/%s/bullets", quoteAll(scope), quoteAll(opts.Ref))
	headers, body, err := SendWrite(cfg, "POST", path, payload, opts.Timeout, nil)
	if err != nil {
		return 0, err
	}
	// 🔴 `duplicate` IS PRINTED, NOT SWALLOWED, and it exits 0. The server recognises a bullet
	// by CONTENT HASH, so a re-POST after a timeout is idempotent — which is the property that
	// makes a retry safe. But a caller told nothing would read "appended" into a run that wrote
	// nothing. Saying which of the two happened is the whole difference.
	fmt.Fprintf(env.Stdout, "cairn: %s instance=%s scope=%s ref=%s revision=%s\n",
		headerOr(headers, "X-Store-Status", "unknown"), alias, scope, opts.Ref,
		etagOr(headers, "unknown"))
	fmt.Fprint(env.Stdout, store.DecodeReplace(body))
	return ExitOK, nil
}

// refVariantName is `fnmatch(name, ref + ".*.md")` with `ref` taken LITERALLY — the one
// wildcard `Put`'s revision derivation keeps after `anchor.go`'s rule took the anchor out of
// the pattern. `<ref>.runbook.md` is a member; `<ref>.md` is not, because the `*` sits between
// two literal dots.
//
// 🔴 IT IS A NAMED FUNCTION SO IT CAN BE MUTATED AND PINNED ON ITS OWN, and the length floor is
// why that matters. Inline, the floor is UNREACHABLE in production: the exact-name arm runs
// first over the SAME directory and this arm is only reached when no `<ref>.md` exists, so
// nothing in `Put` can ever present the floor with the one name it excludes. A condition no
// caller can execute cannot be watched to fail, and an untestable condition that READS as a
// guard is worse than no condition at all. Here the model it implements — "exactly the names
// `filepath.Match(ref+".*.md", …)` accepts, for a `ref` with no metacharacter in it" — is a
// claim a test can make against the glob itself, which is what `anchor_test.go` does.
func refVariantName(name, ref string) bool {
	return strings.HasPrefix(name, ref+".") &&
		strings.HasSuffix(name, ".md") &&
		len(name) >= len(ref)+len(".")+len(".md")
}

// Put replaces a whole entry behind an `If-Match` precondition.
//
// 🔴 THE REVISION IS DERIVED FROM A **LIVE** SYNC, NEVER FROM `--no-sync`. The entry revision is
// `sha256(file bytes)[:16]`, so the cache can compute it offline — which is exactly what makes a
// stale cache dangerous here in a way it is not for a read: an edit based on bytes that moved is
// a lost update, and the `If-Match` is what turns that into a 412 instead of a silent overwrite.
func Put(env Env, opts Options) (int, error) {
	scope := ResolveScope(opts.Scope, opts.Repo, env.Stderr)
	if scope == "" {
		return ExitUsage, nil
	}
	// 🔴 READ THE FILE BEFORE THE NETWORK — AND "BEFORE THE NETWORK" MEANS ABOVE
	// `ResolveState`, NOT ABOVE `LoadConfig`. The first attempt at this fix on the Python side
	// moved the read up only as far as the config load, which is a local file read, so the full
	// snapshot download still ran first: a missing `--file` went on reporting the STORE AS
	// UNREACHABLE (rc 7) instead of "cannot read --file" (rc 2).
	payload, readErr := os.ReadFile(opts.File)
	if readErr != nil {
		fmt.Fprintf(env.Stderr, "cairn: cannot read --file %s: %s\n", opts.File, pyOSError(readErr))
		return ExitUsage, nil
	}
	revision := opts.IfMatch
	// 🔴 THE ROUTE IS RESOLVED BEFORE THE REVISION IS DERIVED, AND THE ORDER IS THE WHOLE
	// POINT. The precondition is `sha256(<the ROUTED store's bytes>)`, so a sync and a glob
	// against the DEFAULT instance's cache — which is what `opts.Cache` is — computes it from
	// a store this write is not addressing. The oracle resolves the route first
	// (`cmd_put` -> `_instance_for`) and syncs the routed cache; this is the port following.
	// ⚠ It stays BELOW the `--file` read: "cannot read --file" is rc 2 and must not be
	// preceded by a routing refusal or a network round trip.
	//
	// 🔴 `writeRoute`, NOT `writeInstance` — THE CREDENTIALS ARE LOADED BELOW, AFTER THE
	// REVISION. That ordering is the oracle's (`cmd_put` resolves the route, resolves the
	// STATE, then calls `load_config`), and loading them here instead produced a measured byte
	// divergence on a routed instance whose config is incomplete: `ResolveState` reports that
	// as a non-live state and `put` refuses in its own words, where an eager load escapes to
	// `cli.go` and refuses in `cli.go`'s. Same exit code, different sentence — see `writeRoute`.
	alias, cache, err := writeRoute(opts, scope)
	if err != nil {
		return 0, err
	}
	if revision == "" {
		state, err := ResolveState(cache, false, "", opts.Timeout, alias)
		if err != nil {
			return 0, err
		}
		if state.Name != StateLive {
			// 🔴 NOT "served from cache". A read may degrade; deriving a precondition from
			// bytes we could not confirm is the one case where the cache is worse than
			// nothing.
			//
			// 🔴 AND NOT `state.ExitHint`. That is a READ verdict, and it is
			// `ExitUnreachableNoCache` (3) exactly when there is NO cache — the FIRST run on
			// a fresh host, i.e. the commonest way to get here. So the one code this whole
			// design insists a write must never return was returned by a write, on its
			// likeliest path, while four comments and a design doc said it could not happen.
			fmt.Fprintf(env.Stderr, "🔴 cairn: refusing to PUT — could not refresh the "+
				"cache, so the revision would be derived from bytes that may have moved "+
				"(%s). Nothing was queued and nothing was written locally. Pass --if-match "+
				"explicitly if you already hold it.\n", state.Detail)
			return ExitWriteUnreachable, nil
		}
		// 🔴 THE SCOPE DIRECTORY IS ENUMERATED AND THE REF IS MATCHED LITERALLY — AND THAT
		// IS THE FIX THE `LsEntries` COMMENT ABOVE DECLARED AS RESIDUAL 9. This was
		// `filepath.Glob(filepath.Join(cache, scope, opts.Ref+".md"))` and, on no match,
		// `…+".*.md"`, which put the CACHE ROOT inside the pattern. Measured end to end
		// against one pod with both real binaries: over a root with no metacharacter both
		// clients answered `replaced` at exit 0 off the same derived `If-Match`; over a root
		// named `cache[bad` `filepath.Glob` returned `ErrBadPattern` and n=0 for BOTH
		// patterns, so the `!= 1` arm fired and this client refused —
		// `cannot derive a revision — 0 cached file(s) match …`, exit 2 — while the oracle
		// still answered `replaced` at exit 0. See `anchor.go` for the class.
		//
		// ⚠ `opts.Ref` IS NOW LITERAL TOO, AND THAT IS A SECOND DECLARED DIVERGENCE
		// (`tests/parity/README.md` residual 9). The oracle interpolates the ref into its
		// pattern, so `--ref 'wid*'` is a WILDCARD there: it can match exactly one file and
		// derive a precondition from `widget-cfg.md` while the `PUT` that follows addresses
		// an entry literally named `wid*` — a precondition taken from bytes the request is
		// not addressing, which is the one thing this block exists to prevent. Here it finds
		// nothing and refuses with the count. Unreachable from the parity corpus (no row
		// passes a metacharacter `--ref`), reachable from a keyboard, and the refusal is the
		// safe side.
		//
		// The `<ref>.*.md` family keeps its ONE wildcard, in `refVariantName` above.
		scopeDir := filepath.Join(cache, scope)
		exact := opts.Ref + ".md"
		matches := anchoredNames(scopeDir, func(name string) bool { return name == exact })
		if len(matches) == 0 {
			matches = anchoredNames(scopeDir, func(name string) bool {
				return refVariantName(name, opts.Ref)
			})
		}
		for i, name := range matches {
			matches[i] = filepath.Join(scopeDir, name)
		}
		// `os.ReadDir` already returns sorted names and they all share one directory, so this
		// is a no-op today. It stays because the ORACLE sorts, the index below is `[0]`, and
		// an ordering that holds only by accident of another package's documented behaviour is
		// not the kind of claim the parity gate should rest on.
		sort.Strings(matches)
		if len(matches) != 1 {
			fmt.Fprintf(env.Stderr, "cairn: cannot derive a revision — %d cached file(s) "+
				"match %s/%s. Pass --if-match, or use the entry's exact filename stem as "+
				"--ref.\n", len(matches), scope, opts.Ref)
			return ExitUsage, nil
		}
		data, err := os.ReadFile(matches[0])
		if err != nil {
			return 0, err
		}
		sum := sha256.Sum256(data)
		revision = hex.EncodeToString(sum[:])[:16]
		fmt.Fprintf(env.Stderr, "cairn: derived If-Match %s from the live snapshot\n", revision)
	}
	// 🔴 HERE, NOT ABOVE — the oracle's `cmd_put` loads the config on this line too, and the
	// position is the whole content of the fix above.
	cfg, err := LoadConfigFor(alias)
	if err != nil {
		return 0, err
	}
	path := fmt.Sprintf("/api/v1/entry/%s/%s", quoteAll(scope), quoteAll(opts.Ref))
	headers, body, err := SendWrite(cfg, "PUT", path, payload, opts.Timeout,
		map[string]string{"If-Match": `"` + revision + `"`})
	if err != nil {
		return 0, err
	}
	fmt.Fprintf(env.Stdout, "cairn: %s instance=%s scope=%s ref=%s revision=%s\n",
		headerOr(headers, "X-Store-Status", "unknown"), alias, scope, opts.Ref,
		etagOr(headers, "unknown"))
	fmt.Fprint(env.Stdout, store.DecodeReplace(body))
	return ExitOK, nil
}

// Create makes a NEW entry — `PUT` with `If-None-Match: *`.
//
// 🔴 WHY IT EXISTS, since `append` and `put` already write: neither can make an entry that is not
// there, because both resolve an EXISTING ref and 404 when it is absent. So the protocol told a
// session to write a brand-new entry into the local store directly — true and safe while that
// tree WAS the store, and content loss the moment reads moved to the pod cache.
//
// 🔴 NO `--if-match`, AND NO SYNC. A create has no prior bytes and its precondition is the
// constant `*`. The server decides absence under its own lock, so there is nothing a local cache
// read could add except a stale answer and a wasted round trip.
//
// 🔴 EXIT 9 IS NOT A FAILURE TO RETRY. It means the entry already exists and NOTHING was
// written — the remedy is `append` or `put`, never the same `create` again.
func Create(env Env, opts Options) (int, error) {
	scope := ResolveScope(opts.Scope, opts.Repo, env.Stderr)
	if scope == "" {
		return ExitUsage, nil
	}
	payload, readErr := os.ReadFile(opts.File)
	if readErr != nil {
		fmt.Fprintf(env.Stderr, "cairn: cannot read --file %s: %s\n", opts.File, pyOSError(readErr))
		return ExitUsage, nil
	}
	alias, cfg, _, err := writeInstance(opts, scope)
	if err != nil {
		return 0, err
	}
	path := fmt.Sprintf("/api/v1/entry/%s/%s", quoteAll(scope), quoteAll(opts.Ref))
	headers, body, err := SendWrite(cfg, "PUT", path, payload, opts.Timeout,
		map[string]string{"If-None-Match": "*"})
	if err != nil {
		return 0, err
	}
	fmt.Fprintf(env.Stdout, "cairn: %s instance=%s scope=%s ref=%s revision=%s\n",
		headerOr(headers, "X-Store-Status", "unknown"), alias, scope, opts.Ref,
		etagOr(headers, "unknown"))
	fmt.Fprint(env.Stdout, store.DecodeReplace(body))
	return ExitOK, nil
}

// etagOr is the entry revision the server echoed, with its quotes stripped.
func etagOr(headers http.Header, fallback string) string {
	raw := strings.Trim(headerOr(headers, "ETag", ""), `"`)
	if raw == "" {
		return fallback
	}
	return raw
}

// quoteAll is `urllib.parse.quote(s, safe="")` — EVERY reserved character escaped, `/`
// included.
//
// 🔴 `safe=""` IS THE POINT AND `url.PathEscape` IS NOT IT. `PathEscape` leaves `/`, `:`, `@`
// and more alone, so a scope or ref containing a slash would silently address a DIFFERENT route
// — which is how a path component becomes a path.
func quoteAll(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		// `quote`'s always-safe set: letters, digits and `_.-~`.
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			c == '_' || c == '.' || c == '-' || c == '~' {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}

var _ = url.PathEscape // referenced by the comment above; kept so the import documents the trap

// pyJSONObject is `json.dumps({...})` for a flat string→string object, with CPython's
// separators (`, ` and `: `) and its `ensure_ascii=True` default.
//
// 🔴 `ensure_ascii` IS LOAD-BEARING ON THIS ROUTE. A bullet containing an emoji travels as a
// `😀` surrogate PAIR, and the server's own guard once could not tell a pair from a
// LONE surrogate and 400'd every astral character — a defect found by reading the port against
// the oracle, not by any suite, because no corpus row carries that shape. Sending raw UTF-8
// here instead would mean the two clients exercise different halves of that guard.
func pyJSONObject(pairs [][2]string) ([]byte, error) {
	var b strings.Builder
	b.WriteByte('{')
	for i, kv := range pairs {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(store.PyJSONString(kv[0]))
		b.WriteString(": ")
		b.WriteString(store.PyJSONString(kv[1]))
	}
	b.WriteByte('}')
	return []byte(b.String()), nil
}
