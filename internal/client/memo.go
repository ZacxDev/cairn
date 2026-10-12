package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ZacxDev/cairn/internal/memo"
	"github.com/ZacxDev/cairn/internal/memowire"
	"github.com/ZacxDev/cairn/internal/store"
	"github.com/ZacxDev/cairn/internal/write"
)

// THE MEMO VERBS — `memo-check`, `memo-read`, `memo-send`, `memo-retract` — S3 of
// `claudedocs/plan-cairn-scope-mail.md` (decision 8), Go-only and declared (the `go_only` rows of
// `tests/testlib/capability_ledger.LEDGER`, `flake.nix`'s `want-go-only-verbs.txt`, residual 11 of
// `tests/parity/README.md`).
//
// 🔴 THEY TALK TO `cairn-ui`'S CLIENT LISTENER, NEVER TO THE POD. Memos live in `cairn-ui`'s
// database (decision 2), so the base URL is the instance's `CAIRN_UI_URL` and the credential is
// its `CAIRN_UI_TOKEN` — the agent's OWN narrowed journal credential (O9, decision 16) — read from
// the same per-instance env file as the pod's pair (`LoadUIConfigFor`). The scope is ROUTED exactly
// as every other verb routes it (`Routing.AliasFor`, exit 11 when unrouted), which is what makes an
// id meaningful: an id is unique only within ONE instance's database, so both id verbs REQUIRE
// `--scope`/`--repo` and send the scope with the id (round 1 🔴3).
//
// 🔴 EVERY MEMO IS PRINTED THROUGH `internal/memo`'s ONE RENDERER — `memo.RenderPreview` for the
// check, `memo.RenderFull` for a read — over `memo.FromRendered`, which re-applies the render-side
// replacement to every field the wire carried. A real listener's text is already rendered (a
// no-op); a RAW one's is not, and e2e clause (d)'s stub is what keeps that half honest (Guard D).
// No memo verb has a JSON output in v1.
//
// 🔴 NO NEW EXIT CODE (decision 8). `memo-check` and `memo-read` use the READ bucket (0, 3, 5) and
// `memo-send`/`memo-retract` the WRITE bucket (6, 7), plus 2 for usage and 11 for unrouted.

// memoStatusPrefix begins `memo-check`'s FIRST stderr line (decision 7).
const memoStatusPrefix = "memo-status="

// memoSendScanNotes is `memo-send --help`'s statement of what the send-time secret scan CANNOT
// catch (decision 11; `memo.CheckSend`'s doc says the same). The scan refuses only the confident
// rules' shapes, so a sender reading only `--help` would otherwise take a clean send for a clean
// memo. Pinned as one whole string by `TestMemoSendHelpSaysWhatTheScanCannotCatch`.
const memoSendScanNotes = "The send-time secret scan refuses only a credential recognisable by its own shape\n" +
	"(a PEM private key, a vendor-prefixed token, a JWT, an Authorization or Bearer value,\n" +
	"a password in a URL). Everything else goes through: a password in prose, a short or\n" +
	"unnamed token, an unprefixed hex secret, any novel format. Never put a secret in a\n" +
	"memo; if one gets through, `cairn memo-retract` it at once."

// memoTTLDays is the `<n>d` spelling `--ttl` accepts beside Go's own durations.
const memoTTLDays = 24 * time.Hour

// memoListPage is the listing's page size: the store caps a page at `memo.MaxLivePerScope`.
const memoListPage = memo.MaxLivePerScope

// memoAnswer is one HTTP exchange with the listener: the status (0 = no answer at all) and the
// body, or the transport failure that prevented one.
type memoAnswer struct {
	status int
	body   []byte
	err    error
}

// memoRequest makes ONE bounded request to the client listener with the agent credential.
func memoRequest(cfg UIConfig, method, path string, body any, timeout int) memoAnswer {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return memoAnswer{err: err}
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, cfg.URL+path, reader)
	if err != nil {
		return memoAnswer{err: err}
	}
	applyStandardHeaders(req, cfg.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := httpClient(timeout).Do(req)
	if err != nil {
		return memoAnswer{err: errors.New(urlErrorReason(err))}
	}
	defer resp.Body.Close()
	// The listener's own answers are small; a body past 4 MiB is not one of them.
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return memoAnswer{status: resp.StatusCode, err: err}
	}
	return memoAnswer{status: resp.StatusCode, body: data}
}

// errorBody decodes a non-2xx answer's `{"error": …}`; ok is false when it is not one.
func (a memoAnswer) errorBody() (memowire.ErrorBody, bool) {
	var e memowire.ErrorBody
	if json.Unmarshal(a.body, &e) != nil || e.Error == "" {
		return e, false
	}
	return e, true
}

func (a memoAnswer) notFound() bool {
	e, ok := a.errorBody()
	return a.status == http.StatusNotFound && ok && e.Error == memowire.ErrorNotFound
}

// decodeAnswer decodes exactly ONE JSON object of `into`'s shape from a listener ANSWER, and
// requires each of `required` to be present and non-null in it.
//
// 🔴 A RESPONSE IS READ TOLERANTLY, AND THE TOLERANCE IS ONE-SIDED ON PURPOSE (the rule is stated
// once, beside the wire, in `internal/memowire`'s package doc). An UNKNOWN field is ignored: the
// list's memos are `memo.Rendered`'s fields, so refusing extras (as S3 first did) made a listener
// release that ADDS a field break every older client's `memo-check` at exit 5 `malformed` — two
// releases that must deploy in lockstep across every host. A MISSING field this client reads is
// still `malformed`: without that half, a field renamed on the listener would decode here as the
// empty string with no error, which is the drift `internal/memowire` exists to prevent.
func decodeAnswer(data []byte, into any, required ...string) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(into); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("trailing data after the JSON value")
	}
	return requireKeys(data, required)
}

// requireKeys refuses a JSON object lacking any of `keys`, or carrying one as `null`.
func requireKeys(object []byte, keys []string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(object, &fields); err != nil {
		return err
	}
	for _, k := range keys {
		if v, ok := fields[k]; !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return fmt.Errorf("the answer has no %q field", k)
		}
	}
	return nil
}

// renderedKeys is every JSON key of a wire memo — `memo.Rendered`'s Go field names (the type IS the
// wire format, `internal/memowire`) — read off the type, so a field added there is required here
// without a second list to forget.
//
// 🔴 "REQUIRED WITHOUT A LIST" IS ALSO A DEPLOY ORDER: a client built after a field joins
// `memo.Rendered` requires it from EVERY listener, so the listener deploys first. The rule is stated
// once, in `internal/memowire`'s package doc; `TestTheMemoKeysAClientRequiresAreExactlyRenderedsFields`
// pins that this set is exactly the type's.
var renderedKeys = func() []string {
	t := reflect.TypeOf(memo.Rendered{})
	keys := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		keys = append(keys, t.Field(i).Name)
	}
	return keys
}()

// decodeList reads a `ListBody` answer: the `memos` key is required, and so is every key of every
// memo in it.
func decodeList(data []byte) ([]memo.Rendered, error) {
	var list memowire.ListBody
	if err := decodeAnswer(data, &list, "memos"); err != nil {
		return nil, err
	}
	var raw struct {
		Memos []json.RawMessage `json:"memos"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	for i, m := range raw.Memos {
		if err := requireKeys(m, renderedKeys); err != nil {
			return nil, fmt.Errorf("memo %d of the list: %w", i+1, err)
		}
	}
	return list.Memos, nil
}

// decodeOne reads a `MemoBody` answer: the `memo` key and every key of the memo are required.
func decodeOne(data []byte) (memo.Rendered, error) {
	var one memowire.MemoBody
	if err := decodeAnswer(data, &one, "memo"); err != nil {
		return memo.Rendered{}, err
	}
	var raw struct {
		Memo json.RawMessage `json:"memo"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return memo.Rendered{}, err
	}
	if err := requireKeys(raw.Memo, renderedKeys); err != nil {
		return memo.Rendered{}, err
	}
	return one.Memo, nil
}

// toStored turns wire memos into the renderer's input; one that does not parse is MALFORMED.
func toStored(rs []memo.Rendered) ([]memo.Stored, error) {
	out := make([]memo.Stored, 0, len(rs))
	for _, r := range rs {
		s, err := memo.FromRendered(r)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// memoRoute is `(alias, label, config)` for a memo verb about `scope`: the SAME routing every verb
// uses, then the instance's memo listener configuration. A routing failure is returned as the
// error `Run` maps to exit 11; a missing key is `*UIConfigMissing`.
func memoRoute(scope string) (string, string, UIConfig, error) {
	routing, err := Discover(nil)
	if err != nil {
		return "", "", UIConfig{}, err
	}
	alias, err := routing.AliasFor(scope)
	if err != nil {
		return "", "", UIConfig{}, err
	}
	cfg, err := LoadUIConfigFor(alias)
	return alias, instanceLabel(routing, alias), cfg, err
}

func isRoutingError(err error) bool {
	var unrouted *UnroutedScope
	var badConfig *RoutingConfigError
	return errors.As(err, &unrouted) || errors.As(err, &badConfig)
}

// ---------------------------------------------------------------- memo-check

// MemoCheck is `cairn memo-check --scope S | --repo P --session ID [--timeout N]`: the hook's
// verb (decision 7). It prints ONE fenced preview block of the memos THIS session has not seen —
// or NOTHING — and writes `memo-status=<token>` as its FIRST stderr line, from the closed set
// `memo.Statuses()` (`--statuses` prints it).
//
// 🔴 THE STATUS LINE IS FIRST ON EVERY RUN THAT REACHES A VERDICT, AND THE HOOK BRANCHES ON IT,
// NEVER ON REASON TEXT. So this verb holds back `Run`'s deprecation warnings (`deferMemoWarnings`)
// until the token is out, and every reason line follows it. ⚠ Exits 2 (usage) and 11 (unrouted)
// print NO token: the closed set has none for "the command line or the routing table must
// change", and inventing one here would grow a contract the hook carries a copy of. The hook reads a
// token-less run as `malformed` (once per throttle interval), which is loud in the right direction.
//
// The order is decision 7's: derive the scope (a directory that is not a repo has NO scope → silent
// `no-scope`); ask the LOCAL caches whether any configured instance holds it (none → `no-scope`,
// exit 0, NO request — most repos are like that, and the cache already says which scopes exist);
// route; read the listener configuration (missing → `unconfigured`, and NO request); then ONE
// request (a 401 → `refused`).
func MemoCheck(env Env, opts Options) (int, error) {
	if opts.Statuses {
		for _, s := range memo.Statuses() {
			fmt.Fprintln(env.Stdout, s)
		}
		return ExitOK, nil
	}
	if opts.Session == "" {
		flushMemoWarnings(env)
		fmt.Fprintln(env.Stderr, "cairn: memo-check requires --session <id> (there is NO environment fallback: a cursor "+
			"keyed by whatever the shell last exported would hand one session's memos to another)")
		return ExitUsage, nil
	}
	if !write.SessionComponent.MatchString(opts.Session) {
		flushMemoWarnings(env)
		fmt.Fprintf(env.Stderr, "cairn: --session %s is not a session id (%s)\n", store.PyRepr(opts.Session),
			write.SessionComponent.String())
		return ExitUsage, nil
	}
	if bad := UnboundedTimeoutReason(opts.Timeout); bad != "" {
		flushMemoWarnings(env)
		fmt.Fprintln(env.Stderr, "cairn: memo-check: "+bad)
		return ExitUsage, nil
	}
	status := func(token string, code int, lines ...string) (int, error) {
		fmt.Fprintln(env.Stderr, memoStatusPrefix+token)
		flushMemoWarnings(env)
		for _, l := range lines {
			fmt.Fprintln(env.Stderr, "cairn: memo-check: "+l)
		}
		return code, nil
	}

	scope := opts.Scope
	if scope == "" {
		derived, err := ScopeForRepo(opts.Repo)
		// 🔴 THE TOKEN AND THE EXIT STAY `no-scope`/0 ON AN ERROR — that is the hook's contract (a
		// directory that is not a repository is the common case, and silent) — but the ERROR is not
		// dropped: a missing or failing `git` reads exactly like "not a repository" otherwise, and
		// only this line, after the token, can tell the two apart.
		if err != nil {
			return status("no-scope", ExitOK, "no scope could be derived from --repo, so there is nothing to check: "+err.Error())
		}
		if derived == "" {
			return status("no-scope", ExitOK, "no scope could be derived from --repo, so there is nothing to check")
		}
		scope = derived
	}
	routing, err := Discover(nil)
	if err != nil {
		flushMemoWarnings(env)
		return 0, err
	}
	held, err := memoScopeIsCached(opts, routing, scope)
	if err != nil {
		flushMemoWarnings(env)
		return 0, err
	}
	if !held {
		return status("no-scope", ExitOK)
	}
	alias, err := routing.AliasFor(scope)
	if err != nil {
		flushMemoWarnings(env)
		return 0, err
	}
	cfg, err := LoadUIConfigFor(alias)
	if err != nil {
		return status("unconfigured", ExitUnreachableNoCache, err.Error())
	}

	dir, dirErr := memoStateDir(alias)
	cursorPath := ""
	var cursor memoCursor
	var notes []string
	if dirErr == nil {
		cursorPath = filepath.Join(dir, opts.Session+".json")
		var problem string
		cursor, problem = loadCursor(cursorPath)
		if problem != "" {
			notes = append(notes, problem)
		}
	} else {
		cursor = memoCursor{Schema: memoCursorSchema, Seen: map[string]string{}}
	}

	q := url.Values{"scope": {scope}}
	if after := cursor.after(); after != "" {
		q.Set("after", after)
	}
	a := memoRequest(cfg, http.MethodGet, memowire.MemosPath+"?"+q.Encode(), nil, opts.Timeout)
	switch {
	case a.err != nil && a.status == 0:
		return status("unreachable", ExitUnreachableNoCache, fmt.Sprintf("%s unreachable: %v", cfg.URL, a.err))
	case a.notFound():
		return status("scope-unreadable", ExitUnreachableNoCache, fmt.Sprintf(
			"this host's cache holds %s, but the memo listener answers not-found for it with this credential — "+
				"it is narrowed away from the scope, or cannot read it", store.PyRepr(scope)))
	case a.status == http.StatusUnauthorized:
		// `refused`, never `unconfigured`: this request reached the listener and COUNTED toward its
		// per-host lockout, which is the one thing the hook backs a whole host off for.
		return status("refused", ExitUnreachableNoCache, fmt.Sprintf(
			"%s refused the credential in %s (HTTP 401) — revoked, mistyped, or for another listener", cfg.URL, EnvUIToken))
	case a.err != nil || a.status != http.StatusOK:
		return status("malformed", ExitCorrupt, fmt.Sprintf("%s answered HTTP %d%s", cfg.URL, a.status, oneLineSuffix(a.body)))
	}
	memos, err := decodeList(a.body)
	if err != nil {
		return status("malformed", ExitCorrupt, fmt.Sprintf("%s answered an unreadable list: %v", cfg.URL, err))
	}
	fresh := cursor.unseen(memos)
	stored, err := toStored(fresh)
	if err != nil {
		return status("malformed", ExitCorrupt, fmt.Sprintf("%s answered an unreadable memo: %v", cfg.URL, err))
	}
	cursor.observe(memos)
	save := func() string {
		if dirErr != nil {
			return dirErr.Error()
		}
		if err := saveCursor(cursorPath, cursor); err != nil {
			return err.Error()
		}
		return ""
	}
	if len(fresh) == 0 {
		// Nothing is printed, so there is no delivery to order the save against; a failed save here
		// costs nothing a reader would see (the next check asks the same question again).
		_ = save()
		return status("none", ExitOK, notes...)
	}
	// 🔴 DELIVERY BEATS BOOKKEEPING, SO THE BLOCK IS PRINTED *BEFORE* THE CURSOR IS SAVED. A process
	// killed between the two then re-delivers these memos at the next check (a DUPLICATE) instead of
	// recording them as seen without ever having shown them (a LOSS) — S3 first saved first, which
	// contradicted this very sentence (#239 round 1). The status token is still the FIRST stderr line:
	// it is written before the block, and the save's own note follows the block.
	code, _ := status("new", ExitOK, notes...)
	fmt.Fprint(env.Stdout, memo.RenderPreview(stored))
	if problem := save(); problem != "" {
		// The cost is said out loud: the exit is still 0, and the next check shows the same memos
		// again because nothing recorded them.
		fmt.Fprintln(env.Stderr, "cairn: memo-check: the cursor was NOT saved ("+problem+"); the next check re-delivers these memos")
	}
	return code, nil
}

// memoScopeIsCached answers "does any configured instance's LOCAL cache hold this scope" — the
// question decision 7 asks BEFORE any request, so a repo cairn does not know is silent. A scope
// directory under an instance's cache root, matched by `store.NormalizeRef` on both sides. No cache
// at all is "no" (it cannot tell — a stated limit).
func memoScopeIsCached(opts Options, routing Routing, scope string) (bool, error) {
	want := store.NormalizeRef(scope)
	if want == "" {
		return false, nil
	}
	roots := map[string]bool{}
	for _, instance := range routing.Instances {
		root, err := instanceCache(opts, instance.Alias)
		if err != nil {
			return false, err
		}
		roots[root] = true
	}
	for root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() && store.NormalizeRef(e.Name()) == want {
				return true, nil
			}
		}
	}
	return false, nil
}

func oneLineSuffix(body []byte) string {
	if line := oneLine(body, 300); line != "" {
		return " — " + line
	}
	return ""
}

// ---------------------------------------------------------------- memo-read

// MemoRead is `cairn memo-read --scope S | --repo P [--id N]`: ONE memo, or — without `--id` — every
// live memo of the scope with its FULL body, retracted ones as tombstones (decision 10), through the
// same fenced renderer as the preview (`memo.RenderFull`).
//
// Exits: 0 (printed, or a scope with no memos — empty output); 3 when nothing was read — the
// listener unreachable, unconfigured, the credential refused, or `not-found` (an id that does not
// exist and one this credential cannot read are ONE answer, T12; not 6, which is a WRITE outcome);
// 5 malformed; 2 usage; 11 unrouted.
func MemoRead(env Env, opts Options) (int, error) {
	scope := ResolveScope(opts.Scope, opts.Repo, env.Stderr)
	if scope == "" {
		return ExitUsage, nil
	}
	var id int64
	if opts.HasID {
		n, err := strconv.ParseInt(opts.ID, 10, 64)
		if err != nil || n < 1 {
			fmt.Fprintf(env.Stderr, "cairn: memo-read --id expects a positive integer, got %s\n", store.PyRepr(opts.ID))
			return ExitUsage, nil
		}
		id = n
	}
	_, label, cfg, err := memoRoute(scope)
	if isRoutingError(err) {
		return 0, err
	}
	var missing *UIConfigMissing
	if errors.As(err, &missing) {
		fmt.Fprintln(env.Stderr, "🔴 cairn: memo-read: nothing was read — "+missing.Error())
		return ExitUnreachableNoCache, nil
	}
	if err != nil {
		return 0, err
	}
	refused := func(a memoAnswer) (int, bool) {
		switch {
		case a.err != nil && a.status == 0:
			fmt.Fprintf(env.Stderr, "🔴 cairn: memo-read: nothing was read — %s unreachable: %v\n", cfg.URL, a.err)
			return ExitUnreachableNoCache, true
		case a.notFound():
			what := "scope " + store.PyRepr(scope)
			if opts.HasID {
				what = fmt.Sprintf("memo %d on scope %s", id, store.PyRepr(scope))
			}
			fmt.Fprintf(env.Stderr, "cairn: memo-read: not-found — no %s that this credential can read "+
				"(an id or scope that does not exist and one you cannot read are the same answer)\n", what)
			return ExitUnreachableNoCache, true
		case a.status == http.StatusUnauthorized:
			fmt.Fprintf(env.Stderr, "🔴 cairn: memo-read: nothing was read — %s refused the credential in %s (HTTP 401)\n",
				cfg.URL, EnvUIToken)
			return ExitUnreachableNoCache, true
		case a.err != nil || a.status != http.StatusOK:
			fmt.Fprintf(env.Stderr, "🔴 cairn: memo-read: %s answered HTTP %d%s\n", cfg.URL, a.status, oneLineSuffix(a.body))
			return ExitCorrupt, true
		}
		return 0, false
	}
	var memos []memo.Rendered
	if opts.HasID {
		q := url.Values{"scope": {scope}, "id": {strconv.FormatInt(id, 10)}}
		a := memoRequest(cfg, http.MethodGet, memowire.MemoPath+"?"+q.Encode(), nil, opts.Timeout)
		if code, done := refused(a); done {
			return code, nil
		}
		one, err := decodeOne(a.body)
		if err != nil {
			fmt.Fprintf(env.Stderr, "🔴 cairn: memo-read: %s answered an unreadable memo: %v\n", cfg.URL, err)
			return ExitCorrupt, nil
		}
		memos = []memo.Rendered{one}
	} else {
		// 🔴 PAGED, AND THE PAGE BOUNDARY IS ONE SECOND EARLY ON PURPOSE. A listing WITH tombstones can
		// exceed one page (≤ 50 a day for up to 30 days), and the wire's times are whole seconds, so a
		// page's last time is ≤ the true one: asking `after` = that time would skip any memo sharing its
		// second. The next page asks from one second before it, and ids already held are dropped.
		seen := map[int64]bool{}
		after := ""
		for {
			q := url.Values{"scope": {scope}, "tombstones": {"1"}, "limit": {strconv.Itoa(memoListPage)}}
			if after != "" {
				q.Set("after", after)
			}
			a := memoRequest(cfg, http.MethodGet, memowire.MemosPath+"?"+q.Encode(), nil, opts.Timeout)
			if code, done := refused(a); done {
				return code, nil
			}
			page, err := decodeList(a.body)
			if err != nil {
				fmt.Fprintf(env.Stderr, "🔴 cairn: memo-read: %s answered an unreadable list: %v\n", cfg.URL, err)
				return ExitCorrupt, nil
			}
			added := 0
			for _, m := range page {
				if !seen[m.ID] {
					seen[m.ID] = true
					memos = append(memos, m)
					added++
				}
			}
			if len(page) < memoListPage || added == 0 {
				break
			}
			last, err := time.Parse(time.RFC3339, page[len(page)-1].CreatedAt)
			if err != nil {
				fmt.Fprintf(env.Stderr, "🔴 cairn: memo-read: %s answered an unreadable time: %v\n", cfg.URL, err)
				return ExitCorrupt, nil
			}
			after = last.Add(-time.Second).UTC().Format(time.RFC3339Nano)
		}
	}
	stored, err := toStored(memos)
	if err != nil {
		fmt.Fprintf(env.Stderr, "🔴 cairn: memo-read: %s answered an unreadable memo: %v\n", cfg.URL, err)
		return ExitCorrupt, nil
	}
	if label != "" && len(stored) > 0 {
		fmt.Fprintf(env.Stderr, "cairn: instance=%s\n", label)
	}
	fmt.Fprint(env.Stdout, memo.RenderFull(stored))
	return ExitOK, nil
}

// ---------------------------------------------------------------- memo-send

// MemoSend is `cairn memo-send --scope S | --repo P --subject T (--body B | --body-file F) [--ttl D]`.
//
// Exits: 0 sent; 6 REFUSED — no write on the scope, the quota, the live bound, a size, the
// characters (`memo.BreaksFence`, decided by the listener), a confident secret (decided by the
// listener, naming the RULE, never the text), the TTL, or the credential refused; 7 the send did
// NOT happen and a retry is the right response (unreachable, a server error, or no listener
// configured — the same code `append` gives a missing `CAIRN_TOKEN`); 2 usage; 11 unrouted.
//
// ⚠ ONE CHARACTER CHECK RUNS HERE, AND ONLY BECAUSE THE WIRE WOULD HIDE IT: invalid UTF-8. JSON
// encoding silently replaces an invalid byte with U+FFFD, so a body the listener would refuse
// (`BreaksFence` refuses invalid UTF-8) would arrive as a different, ACCEPTABLE body. Every other
// rule is the listener's, applied once, server-side.
func MemoSend(env Env, opts Options) (int, error) {
	if opts.HasBody == (opts.BodyFile != "") {
		fmt.Fprintln(env.Stderr, "cairn: memo-send needs exactly one of --body or --body-file")
		return ExitUsage, nil
	}
	body := opts.Body
	if opts.BodyFile != "" {
		var data []byte
		var err error
		if opts.BodyFile == "-" {
			data, err = io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
		} else {
			data, err = os.ReadFile(opts.BodyFile)
		}
		if err != nil {
			fmt.Fprintf(env.Stderr, "cairn: memo-send --body-file %s: %s\n", store.PyRepr(opts.BodyFile), pyOSError(err))
			return ExitUsage, nil
		}
		body = string(data)
	}
	ttl, ok := parseMemoTTL(opts.TTL)
	if !ok {
		fmt.Fprintf(env.Stderr, "cairn: memo-send --ttl expects a duration (72h, 90m) or whole days (3d), got %s\n",
			store.PyRepr(opts.TTL))
		return ExitUsage, nil
	}
	// 🔴 AN EXPLICIT TTL THAT ROUNDS TO 0 SECONDS IS REFUSED, NOT SENT. The wire carries whole
	// seconds and `ttl_seconds` 0 is the listener's DEFAULT (3 days), so `--ttl 500ms` or `--ttl 0`
	// asked for "almost no time" and stored a memo for three days at exit 0. Refused rather than
	// rounded up to 1 s: a TTL nobody can read in time is a mistake to name, not a value to guess.
	if strings.TrimSpace(opts.TTL) != "" && ttl/time.Second == 0 {
		fmt.Fprintf(env.Stderr, "cairn: memo-send --ttl %s is less than one second, and the wire carries whole seconds — "+
			"0 would be the listener's DEFAULT TTL. Give at least 1s, or omit --ttl for the default.\n", store.PyRepr(opts.TTL))
		return ExitUsage, nil
	}
	scope := ResolveScope(opts.Scope, opts.Repo, env.Stderr)
	if scope == "" {
		return ExitUsage, nil
	}
	if !utf8.ValidString(opts.Subject) || !utf8.ValidString(body) {
		fmt.Fprintln(env.Stderr, "🔴 cairn: the memo was REFUSED [characters] — the subject or body is not valid UTF-8 "+
			"(refused here, because JSON would silently replace the invalid bytes). Nothing was sent.")
		return ExitWriteRefused, nil
	}
	alias, _, cfg, err := memoRoute(scope)
	if isRoutingError(err) {
		return 0, err
	}
	var missing *UIConfigMissing
	if errors.As(err, &missing) {
		fmt.Fprintln(env.Stderr, "🔴 cairn: the memo was NOT sent — "+missing.Error())
		return ExitWriteUnreachable, nil
	}
	if err != nil {
		return 0, err
	}
	a := memoRequest(cfg, http.MethodPost, memowire.MemosPath,
		memowire.SendRequest{Scope: scope, Subject: opts.Subject, Body: body, TTLSeconds: int64(ttl / time.Second)}, opts.Timeout)
	if code, done := memoWriteRefused(env, "send", cfg, a); done {
		return code, nil
	}
	var sent memowire.SentBody
	if a.status != http.StatusCreated || decodeAnswer(a.body, &sent, "id", "created_at") != nil || sent.ID < 1 {
		fmt.Fprintf(env.Stderr, "🔴 cairn: the memo listener answered the send HTTP %d%s — whether it was stored is "+
			"UNKNOWN; read the scope before re-sending\n", a.status, oneLineSuffix(a.body))
		return ExitWriteUnreachable, nil
	}
	fmt.Fprintf(env.Stdout, "cairn: memo sent — id=%d scope=%s instance=%s created=%s\n", sent.ID, scope, alias, sent.CreatedAt)
	return ExitOK, nil
}

// parseMemoTTL reads `--ttl`: "" is the listener's default (sent as 0), Go's duration syntax, or a
// whole number of days with `d`. A value the listener's ceiling refuses is ITS refusal (exit 6); an
// explicit value under one second is refused by `MemoSend` itself (exit 2), since it would be sent
// as that same 0.
func parseMemoTTL(raw string) (time.Duration, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, true
	}
	if days, ok := strings.CutSuffix(raw, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil || n > 3650 || n < -3650 {
			return 0, false
		}
		return time.Duration(n) * memoTTLDays, true
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, false
	}
	return d, true
}

// memoWriteRefused classifies a send's or retraction's NON-success answer: 6 when the listener
// answered no (a refusal, a 4xx, the credential refused), 7 when it did not answer or failed
// (retry is right). `done` is false for a 2xx, which the caller decodes.
func memoWriteRefused(env Env, what string, cfg UIConfig, a memoAnswer) (int, bool) {
	switch {
	case a.err != nil && a.status == 0:
		fmt.Fprintf(env.Stderr, "🔴 cairn: the memo %s did NOT happen — %s unreachable: %v. Nothing was queued; "+
			"re-run when the listener is reachable.\n", what, cfg.URL, a.err)
		return ExitWriteUnreachable, true
	case a.status >= 200 && a.status < 300:
		return 0, false
	// A refused credential (401, the listener's `{"error":"unauthorized"}`) is one of these: the
	// listener answered no, and resending the same token cannot help.
	case a.status >= 400 && a.status < 500:
		e, _ := a.errorBody()
		reason := e.Reason
		if reason == "" {
			reason = e.Error
		}
		if reason == "" {
			reason = "HTTP " + strconv.Itoa(a.status)
		}
		detail := ""
		if e.Rule != "" {
			detail = " (rule " + e.Rule + ")"
		}
		if e.Detail != "" {
			detail += " — " + e.Detail
		}
		fmt.Fprintf(env.Stderr, "🔴 cairn: the memo %s was REFUSED [%s]%s. Change the request; a retry as-is cannot help.\n",
			what, reason, detail)
		return ExitWriteRefused, true
	default:
		fmt.Fprintf(env.Stderr, "🔴 cairn: the memo %s did NOT happen — %s answered HTTP %d%s\n",
			what, cfg.URL, a.status, oneLineSuffix(a.body))
		return ExitWriteUnreachable, true
	}
}

// ---------------------------------------------------------------- memo-retract

// MemoRetract is `cairn memo-retract --scope S | --repo P --id N`: the sender, or a scope admin who
// can read the scope, retracts one memo (O8 as S2 narrowed it; `memo.MayRetract` decides,
// server-side). Exits: 0 retracted; 6 refused — not-found (absent, another scope's, or unreadable:
// one answer), not permitted, already retracted, the credential refused; 7 not done; 2; 11.
func MemoRetract(env Env, opts Options) (int, error) {
	id, err := strconv.ParseInt(opts.ID, 10, 64)
	if err != nil || id < 1 {
		fmt.Fprintf(env.Stderr, "cairn: memo-retract --id expects a positive integer, got %s\n", store.PyRepr(opts.ID))
		return ExitUsage, nil
	}
	scope := ResolveScope(opts.Scope, opts.Repo, env.Stderr)
	if scope == "" {
		return ExitUsage, nil
	}
	alias, _, cfg, err := memoRoute(scope)
	if isRoutingError(err) {
		return 0, err
	}
	var missing *UIConfigMissing
	if errors.As(err, &missing) {
		fmt.Fprintln(env.Stderr, "🔴 cairn: the memo was NOT retracted — "+missing.Error())
		return ExitWriteUnreachable, nil
	}
	if err != nil {
		return 0, err
	}
	a := memoRequest(cfg, http.MethodPost, memowire.RetractPath, memowire.RetractRequest{Scope: scope, ID: id}, opts.Timeout)
	if code, done := memoWriteRefused(env, "retraction", cfg, a); done {
		return code, nil
	}
	var done memowire.RetractedBody
	if decodeAnswer(a.body, &done, "retracted") != nil || done.Retracted != id {
		fmt.Fprintf(env.Stderr, "🔴 cairn: the memo listener answered the retraction HTTP %d%s — whether it happened "+
			"is UNKNOWN; read the memo to see\n", a.status, oneLineSuffix(a.body))
		return ExitWriteUnreachable, nil
	}
	fmt.Fprintf(env.Stdout, "cairn: memo retracted — id=%d scope=%s instance=%s\n", id, scope, alias)
	return ExitOK, nil
}

// ---------------------------------------------------------------- deferred warnings

// memoWarnings holds `Run`'s deprecation warnings while `memo-check` runs, so its status token can
// be the FIRST stderr line (decision 7). nil when nothing is deferred.
var memoWarnings *[]string

// deferMemoWarnings points the deprecation sink at a buffer. `Run` calls it for `memo-check` only:
// the other verbs' stderr is diffed byte-for-byte against the oracle (`tests/parity/`), so their
// warning position is part of a contract `memo-check`, which the oracle does not have, is not in.
func deferMemoWarnings(env Env) {
	buf := []string{}
	memoWarnings = &buf
	SetDeprecationSink(func(line string) { *memoWarnings = append(*memoWarnings, "cairn: "+line) })
}

// flushMemoWarnings writes and clears whatever was deferred, then points the sink back at stderr.
// Idempotent, so `Run` can call it after any return.
func flushMemoWarnings(env Env) {
	if memoWarnings == nil {
		return
	}
	for _, line := range *memoWarnings {
		fmt.Fprintln(env.Stderr, line)
	}
	memoWarnings = nil
	SetDeprecationSink(func(line string) { fmt.Fprintln(env.Stderr, "cairn: "+line) })
}
