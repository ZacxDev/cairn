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
		matches, _ := filepath.Glob(filepath.Join(cache, "*", "*.md"))
		sort.Strings(matches)
		for _, path := range matches {
			// 🔴 A SCOPE'S `README.md` IS ITS POLICY SHEET, NOT AN ENTRY — AND THIS
			// VERB IS THE ONE THAT ADVERTISES ITSELF AS "what the cache actually
			// holds". The glob above is the bare `*.md` walk the loader deliberately
			// does NOT use, so until this line existed `ls-entries` listed every
			// scope's sheet as `<scope>/README.md` — measured twelve of them on a
			// populated cache, under a banner naming the store they came from. The
			// predicate is `store.IsEntryFileName`, the LOADER'S rule imported rather
			// than respelled, because respelling it is how the two answers came apart.
			//
			// ⚠ FILTERED AFTER THE GLOB RATHER THAN BY WALKING SCOPE DIRECTORIES,
			// BECAUSE THE ORDER IS THE CLAIM. Sorting full paths is NOT the same order
			// as sorting scope names and then entry names — `/` sorts above `-`, so
			// `a-b/x.md` precedes `a/y.md` — and this verb's parity row compares the
			// listing line for line.
			if !store.IsEntryFileName(filepath.Base(path)) {
				continue
			}
			fmt.Fprintf(env.Stdout, "%s%s/%s\n", prefix,
				filepath.Base(filepath.Dir(path)), filepath.Base(path))
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

// Validate parse-checks the cached entries with the READER'S OWN parser.
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
		index, loadErr := store.LoadIndex(cache, store.Collect,
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
		// 🔴 THE DENOMINATOR IS NOW THE LOADER'S OWN WALK, NOT A SECOND ONE SPELLED THE SAME
		// WAY. The first fix filtered a `*.md` glob with an open-coded `!= "README.md"`, which
		// closed the symptom and left the mechanism — two walks behind one line — intact.
		// `store.EntryFileNames` is the function `store.LoadIndex` enumerates with, so the
		// numerator and the denominator cannot come to disagree about what an entry is.
		//
		// ⚠ THE READ ERROR IS DISCARDED, AND THAT IS THE PRE-EXISTING BEHAVIOUR KEPT
		// DELIBERATELY. `LoadIndex` above has ALREADY walked this directory and RETURNED on
		// any error, so a failure here is unreachable; and the oracle swallows it rather than
		// raising, so surfacing it would be a divergence with nothing behind it — MEASURED on
		// the pinned interpreter (CPython 3.12.14), `Path("<mode-000 dir>").glob("*.md")`
		// yields `[]` rather than a `PermissionError`.
		entryNames, _ := store.EntryFileNames(filepath.Join(cache, scope))
		checked := len(entryNames)
		fmt.Fprintf(env.Stdout, "cairn: %s: %d of %d entry file(s) parse, %d malformed\n",
			scope, checked-len(index.Malformed), checked, len(index.Malformed))
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
		matches, _ := filepath.Glob(filepath.Join(cache, scope, opts.Ref+".md"))
		sort.Strings(matches)
		if len(matches) == 0 {
			matches, _ = filepath.Glob(filepath.Join(cache, scope, opts.Ref+".*.md"))
			sort.Strings(matches)
		}
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
