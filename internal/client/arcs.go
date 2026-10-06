package client

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"

	"github.com/ZacxDev/cairn/internal/doctor"
	"github.com/ZacxDev/cairn/internal/report"
	"github.com/ZacxDev/cairn/internal/store"
)

// THE ARC-REGISTRY VERBS — `arcs`, `arc-show`, `arc-register` — the Go-only surface of the
// arcs/sessions S3 slice (decision 3 of `claudedocs/plan-cairn-arcs-sessions.md`; declared by the
// `go_only` rows of `tests/testlib/capability_ledger.LEDGER`).
//
// 🔴 ALL THREE ASK THE POD, NONE READS THE CACHE. Registrations live in the pod's journal and are
// deliberately NOT in the snapshot (which `tests/dualrun/` byte-compares against the oracle, so a
// Go-only member there would be a divergence by construction). So the two read verbs print the
// pod's body VERBATIM — `internal/report`'s arc renderer is the only rendering of these answers —
// and a pod that cannot be reached is exit 3 ("nothing was read at all"), not a stale answer.
//
// 🔴 NO NEW EXIT CODE (operator decision Q5). Reads: 0 for every answer the pod gave (including
// `arc-unregistered`, `no-arc-registered` and `registrations-unconfigured`), 3 when the pod did
// not answer one, 2 for usage, 11 unrouted. `arc-register`: 0 registered or unchanged, 6 refused
// (malformed payload, a scope the credential may not write, a credential that writes nowhere, a pod
// with no journal), 7 the write did not happen, 2 usage. `arcs --check` is the exception to the read
// codes, and still no new one: it exits on DOCTOR'S 0/9/10 at its own call site (see `arcsCheck`).

// maxArcPayloadBytes is the pod's own body cap (`api.maxBodyBytes`); a larger payload is refused
// here, before the network, rather than sent to be refused there.
const maxArcPayloadBytes = 1 << 20

// podFor is `(alias, label, config)` for a request about `scope`: the SAME routing every verb
// uses (`Routing.AliasFor`), so an arc homed in a scope another instance serves is asked of that
// instance, and an unrouted scope refuses before the network (`*UnroutedScope`, exit 11).
func podFor(scope string) (string, string, Config, error) {
	routing, err := Discover(nil)
	if err != nil {
		return "", "", Config{}, err
	}
	alias, err := routing.AliasFor(scope)
	if err != nil {
		return "", "", Config{}, err
	}
	cfg, err := LoadConfigFor(alias)
	if err != nil {
		return "", "", Config{}, err
	}
	return alias, instanceLabel(routing, alias), cfg, nil
}

// FetchReport GETs one rendered report from the pod. A 200 returns its headers and body; any
// other answer, or none, is a `*StoreUnreachable` naming the host and carrying the body's first
// line — the read path's rule (`FetchSnapshot`), so a refusal is never printed as if it were the
// report.
func FetchReport(cfg Config, path string, timeout int) (http.Header, []byte, error) {
	if bad := UnboundedTimeoutReason(timeout); bad != "" {
		return nil, nil, unreachable("refusing to fetch %s: %s", cfg.URL, bad)
	}
	req, err := http.NewRequest(http.MethodGet, cfg.URL+path, nil)
	if err != nil {
		return nil, nil, unreachable("%s unreachable: %s", cfg.URL, err)
	}
	applyStandardHeaders(req, cfg.Token)
	resp, err := httpClient(timeout).Do(req)
	if err != nil {
		return nil, nil, unreachable("%s unreachable: %s", cfg.URL, urlErrorReason(err))
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		detail := ""
		if readErr == nil {
			if line := oneLine(body, 300); line != "" {
				detail = " — " + line
			}
		}
		return nil, nil, &StoreUnreachable{
			Reason:     fmt.Sprintf("%s answered HTTP %d%s", cfg.URL, resp.StatusCode, detail),
			HTTPStatus: resp.StatusCode,
		}
	}
	if readErr != nil {
		return nil, nil, unreachable("%s unreachable: %s", cfg.URL, readErr)
	}
	return resp.Header, body, nil
}

// printPodReport writes one pod report to stdout — preceded by the instance label on a host with
// more than one — and returns the exit code the pod declared in `X-Store-Exit`.
func printPodReport(env Env, label string, headers http.Header, body []byte) int {
	if label != "" {
		fmt.Fprintf(env.Stdout, "cairn: instance=%s\n", label)
	}
	fmt.Fprint(env.Stdout, store.DecodeReplace(body))
	code, err := strconv.Atoi(headers.Get("X-Store-Exit"))
	if err != nil {
		return ExitOK
	}
	return code
}

// ArcsList is `cairn arcs --scope X` (or `--repo P`): the pod's `GET arcs/<scope>`, verbatim.
// With `--check` it is the orphan check instead — see `arcsCheck`.
func ArcsList(env Env, opts Options) (int, error) {
	if opts.AllScopes && !opts.Check {
		// Refused rather than ignored: `--all-scopes` on a LISTING would be a flag that parses and
		// changes nothing, which a caller reads as "it listed every arc".
		fmt.Fprintln(env.Stderr, "cairn: arcs --all-scopes only widens --check; a listing is always about one scope")
		return ExitUsage, nil
	}
	scope := ResolveScope(opts.Scope, opts.Repo, env.Stderr)
	if scope == "" {
		return ExitUsage, nil
	}
	if opts.Check {
		return arcsCheck(env, opts, scope)
	}
	_, label, cfg, err := podFor(scope)
	if err != nil {
		return 0, err
	}
	headers, body, err := FetchReport(cfg, "/api/v1/arcs/"+quoteAll(scope), opts.Timeout)
	if err != nil {
		return 0, err
	}
	return printPodReport(env, label, headers, body), nil
}

// arcsCheck is `cairn arcs --check [--all-scopes]`: the pod's `GET arcs/<scope>?check=1`, printed
// verbatim, exiting on DOCTOR'S legend (operator decision Q5) — `doctor.ExitOK` / `ExitProblem` /
// `ExitUnmeasured`, read from `internal/doctor`, no new constant.
//
// 🔴 THE EXIT IS RECOMPUTED FROM `X-Store-Status` BY `report.ArcsCheckExit`, NOT READ FROM
// `X-Store-Exit`. A pod that predates the check ignores `?check=1` and answers an `arcs/<scope>`
// LISTING with `X-Store-Exit: 0`; trusting the header would print that listing as a passed check.
// A status the mapping does not know is "could not look" (10), said on stderr.
//
// 🔴 "THE POD DID NOT ANSWER" IS ALSO 10, NOT 3. On every other read verb 3 means "nothing was
// read"; on a check the question is whether anything was MEASURED, and doctor's 10 is that answer.
// Two refusals keep their own codes because they happen before any look: usage (2) and an unrouted
// scope (11, the routing contract every verb shares).
//
// ⚠ ONE INSTANCE PER RUN: the scope (`--scope`, `--repo`, or the working directory's repo) picks
// the pod, and `--all-scopes` means every arc visible on THAT pod. A multi-instance host is not
// fanned out; run it once per instance's scope.
func arcsCheck(env Env, opts Options, scope string) (int, error) {
	_, label, cfg, err := podFor(scope)
	if err != nil {
		var unrouted *UnroutedScope
		var badConfig *RoutingConfigError
		if errors.As(err, &unrouted) || errors.As(err, &badConfig) {
			return 0, err
		}
		fmt.Fprintf(env.Stderr, "🔴 cairn: arcs --check could NOT look — %s\n", err)
		return doctor.ExitUnmeasured, nil
	}
	path := "/api/v1/arcs/" + quoteAll(scope) + "?check=1"
	if opts.AllScopes {
		path += "&all_scopes=1"
	}
	headers, body, err := FetchReport(cfg, path, opts.Timeout)
	if err != nil {
		fmt.Fprintf(env.Stderr, "🔴 cairn: arcs --check could NOT look — %s\n", err)
		return doctor.ExitUnmeasured, nil
	}
	printPodReport(env, label, headers, body)
	status := headers.Get("X-Store-Status")
	code, ok := report.ArcsCheckExit(status)
	if !ok {
		fmt.Fprintf(env.Stderr, "🔴 cairn: arcs --check could NOT look — the pod answered status %s, which is not a check answer "+
			"(a pod that predates `arcs --check` ignores ?check=1 and lists instead)\n", store.PyRepr(status))
	}
	return code, nil
}

// ArcShow is `cairn arc-show --slug S` (home from `--scope`/`--repo`): `GET arc/<home>/<slug>`.
func ArcShow(env Env, opts Options) (int, error) {
	home := ResolveScope(opts.Scope, opts.Repo, env.Stderr)
	if home == "" {
		return ExitUsage, nil
	}
	_, label, cfg, err := podFor(home)
	if err != nil {
		return 0, err
	}
	headers, body, err := FetchReport(cfg,
		"/api/v1/arc/"+quoteAll(home)+"/"+quoteAll(opts.Slug), opts.Timeout)
	if err != nil {
		return 0, err
	}
	return printPodReport(env, label, headers, body), nil
}

// ArcRegister is `cairn arc-register --slug S --from F` (home from `--scope`/`--repo`).
//
// 🔴 THE PAYLOAD IS A FILE, NOT A FLAG PER FIELD, AND THE CLIENT DOES NOT PARSE IT. The tooling
// already holds its report as JSON, a member list does not fit in flags, and a file is what a
// script passes. The bytes go to the pod unchanged because the pod's `arcs.DecodePayload` is the
// ONE validator — a client-side copy would be a second answer to "what is a valid registration"
// that drifts, and this client could only ever be stricter or wronger than the pod.
//
// The HOME SCOPE IS DERIVED HERE (`--repo` → `DeriveScope`, the worktree-stable rule `recall
// --repo` uses — design decision 1), never taken from the payload, so one repo cannot key one arc
// under two names depending on which worktree ran the tooling.
func ArcRegister(env Env, opts Options) (int, error) {
	home := ResolveScope(opts.Scope, opts.Repo, env.Stderr)
	if home == "" {
		return ExitUsage, nil
	}
	info, err := os.Stat(opts.From)
	if err != nil {
		fmt.Fprintf(env.Stderr, "cairn: cannot read the registration payload %s: %v. Nothing was sent.\n",
			store.PyRepr(opts.From), err)
		return ExitUsage, nil
	}
	if info.Size() > maxArcPayloadBytes {
		fmt.Fprintf(env.Stderr, "cairn: refusing to send — the payload is %d bytes, max %d. Nothing was sent.\n",
			info.Size(), maxArcPayloadBytes)
		return ExitUsage, nil
	}
	payload, err := os.ReadFile(opts.From)
	if err != nil {
		fmt.Fprintf(env.Stderr, "cairn: cannot read the registration payload %s: %v. Nothing was sent.\n",
			store.PyRepr(opts.From), err)
		return ExitUsage, nil
	}
	alias, cfg, _, err := writeInstance(opts, home)
	if err != nil {
		return 0, err
	}
	path := "/api/v1/arc/" + quoteAll(home) + "/" + quoteAll(opts.Slug)
	headers, body, err := SendWrite(cfg, "PUT", path, payload, opts.Timeout, nil)
	if err != nil {
		return 0, err
	}
	fmt.Fprintf(env.Stdout, "cairn: %s instance=%s home=%s slug=%s\n",
		headerOr(headers, "X-Store-Status", "unknown"), alias, home, opts.Slug)
	fmt.Fprint(env.Stdout, store.DecodeReplace(body))
	return ExitOK, nil
}
