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
func Sync(env Env, opts Options) (int, error) {
	// 🔴 `scope=""`, ALWAYS. `--scope` used to be threaded through here, which REPLACED THE
	// SHARED CACHE with a one-scope copy: measured, a scoped run took the cache from 305
	// entries to 2, after which an offline recall of any other scope printed "the store has
	// no 'other-scope/' directory" at exit 0 — a claim about the STORE derived from a
	// filtered cache. The server keeps `?scope=`; it is a legitimate API capability that may
	// not narrow THIS cache.
	state, err := ResolveState(opts.Cache, false, "", opts.Timeout)
	if err != nil {
		return 0, err
	}
	if state.Name == StateLive {
		fmt.Fprintln(env.Stdout, Banner(state.Name, state.Detail))
		return ExitOK, nil
	}
	fmt.Fprintln(env.Stderr, Banner(state.Name, state.Detail))
	if state.ExitHint != 0 {
		return state.ExitHint, nil
	}
	return ExitRefreshFailed, nil
}

// LsEntries prints one `<scope>/<entry>.md` per line.
func LsEntries(env Env, opts Options) (int, error) {
	state, err := ResolveState(opts.Cache, opts.NoSync, "", opts.Timeout)
	if err != nil {
		return 0, err
	}
	fmt.Fprintln(env.Stderr, Banner(state.Name, state.Detail))
	if state.ExitHint != 0 {
		return state.ExitHint, nil
	}
	matches, _ := filepath.Glob(filepath.Join(opts.Cache, "*", "*.md"))
	sort.Strings(matches)
	for _, path := range matches {
		fmt.Fprintf(env.Stdout, "%s/%s\n", filepath.Base(filepath.Dir(path)), filepath.Base(path))
	}
	return ExitOK, nil
}

// Report is `recall` and `search` — ONE function, because the state handling, the scope
// derivation, the banner and the exit passthrough are identical and three of them rendered
// differently when they were separate.
func Report(env Env, opts Options, isSearch bool) (int, error) {
	// 🔴 SYNC THE WHOLE STORE, NEVER `scope=opts.Scope`. A scope-filtered cache makes the
	// reader answer `scope-absent` for every scope that was simply not fetched —
	// indistinguishable, in the output, from a scope the store has never held.
	state, err := ResolveState(opts.Cache, opts.NoSync, "", opts.Timeout)
	if err != nil {
		return 0, err
	}
	if state.ExitHint != 0 {
		fmt.Fprintln(env.Stderr, Banner(state.Name, state.Detail))
		return state.ExitHint, nil
	}

	scope := ResolveScope(opts.Scope, opts.Repo, env.Stderr)
	if scope == "" {
		return ExitUsage, nil
	}

	var text, status, label string
	var malformed []store.MalformedEntry
	if isSearch {
		rep, searchErr := report.Search(opts.Cache, report.SearchOptions{
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
		text = rep.RenderText(env.host(), nil)
		status, malformed = rep.Status, rep.Malformed
		// 🔴 SEARCH USES ITS OWN LABEL. `SearchReport.Label()` names the scopes SEARCHED;
		// passing the query instead made the reader's failure sentence say "`lease` holds 1
		// entry file" — naming the search term as if it were a scope path.
		label = rep.Label()
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
		rep, recallErr := report.Recall(opts.Cache, recallOpts, store.Unrestricted())
		if recallErr != nil {
			return 0, recallErr
		}
		text = rep.RenderText(env.host(), nil)
		status, malformed = rep.Status, rep.Malformed
		// ⚠ `label` IS DERIVED, NOT AN ATTRIBUTE. The recall report has no label field, and
		// assuming it did was an AttributeError that took every recall to exit 1 on the
		// Python side. Derived exactly as the pod's own `Reader.Recall` derives it.
		label = rep.Scope + "/"
		if state.Name == StateLive && isEmptyStatus(rep.Status) {
			state = emptyState(state)
		}
	}

	fmt.Fprintln(env.Stdout, Banner(state.Name, state.Detail))
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
	code, warning := report.ExitFor(status, label, malformed)
	if warning != "" {
		fmt.Fprintln(env.Stderr, warning)
	}
	return code, nil
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
func Validate(env Env, opts Options) (int, error) {
	state, err := ResolveState(opts.Cache, opts.NoSync, "", opts.Timeout)
	if err != nil {
		return 0, err
	}
	fmt.Fprintln(env.Stderr, Banner(state.Name, state.Detail))
	if state.ExitHint != 0 {
		return state.ExitHint, nil
	}
	// 🔴 THE CACHE IS A MULTI-SCOPE STORE; VALIDATION IS SCOPE-BOUND. With no `--scope` a
	// repo-derived scope would validate a scope the cache does not hold and print "NOTHING WAS
	// CHECKED — a zero here is NOT a clean bill of health" while exiting 0. With no `--scope`
	// we validate EVERY scope in the cache.
	var held []string
	entries, readErr := os.ReadDir(opts.Cache)
	if readErr != nil {
		return 0, readErr
	}
	for _, e := range entries {
		info, statErr := os.Stat(filepath.Join(opts.Cache, e.Name()))
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
		fmt.Fprintf(env.Stderr, "cairn: nothing to validate — %s holds no scopes\n", opts.Cache)
		return ExitUnreachableNoCache, nil
	}

	worst := ExitOK
	for _, scope := range scopes {
		index, loadErr := store.LoadIndex(opts.Cache, store.Collect,
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
		checked, _ := filepath.Glob(filepath.Join(opts.Cache, scope, "*.md"))
		fmt.Fprintf(env.Stdout, "cairn: %s: %d of %d entry file(s) parse, %d malformed\n",
			scope, len(checked)-len(index.Malformed), len(checked), len(index.Malformed))
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
	cfg, err := LoadConfig()
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
	fmt.Fprintf(env.Stdout, "cairn: %s scope=%s ref=%s revision=%s\n",
		headerOr(headers, "X-Store-Status", "unknown"), scope, opts.Ref,
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
	if revision == "" {
		state, err := ResolveState(opts.Cache, false, "", opts.Timeout)
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
		matches, _ := filepath.Glob(filepath.Join(opts.Cache, scope, opts.Ref+".md"))
		sort.Strings(matches)
		if len(matches) == 0 {
			matches, _ = filepath.Glob(filepath.Join(opts.Cache, scope, opts.Ref+".*.md"))
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
	cfg, err := LoadConfig()
	if err != nil {
		return 0, err
	}
	path := fmt.Sprintf("/api/v1/entry/%s/%s", quoteAll(scope), quoteAll(opts.Ref))
	headers, body, err := SendWrite(cfg, "PUT", path, payload, opts.Timeout,
		map[string]string{"If-Match": `"` + revision + `"`})
	if err != nil {
		return 0, err
	}
	fmt.Fprintf(env.Stdout, "cairn: %s scope=%s ref=%s revision=%s\n",
		headerOr(headers, "X-Store-Status", "unknown"), scope, opts.Ref, etagOr(headers, "unknown"))
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
	cfg, err := LoadConfig()
	if err != nil {
		return 0, err
	}
	path := fmt.Sprintf("/api/v1/entry/%s/%s", quoteAll(scope), quoteAll(opts.Ref))
	headers, body, err := SendWrite(cfg, "PUT", path, payload, opts.Timeout,
		map[string]string{"If-None-Match": "*"})
	if err != nil {
		return 0, err
	}
	fmt.Fprintf(env.Stdout, "cairn: %s scope=%s ref=%s revision=%s\n",
		headerOr(headers, "X-Store-Status", "unknown"), scope, opts.Ref, etagOr(headers, "unknown"))
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
