package client

import (
	"fmt"

	"github.com/ZacxDev/cairn/internal/report"
	"github.com/ZacxDev/cairn/internal/store"
)

// Sessions is `cairn sessions`: which sessions wrote attributed bullets in a scope, read from the
// LOCAL cache — the Go-only verb of the arcs/sessions S2 slice, with no Python twin (decision 3 of
// `claudedocs/plan-cairn-arcs-sessions.md`; declared in `tests/testlib/capability_ledger.LEDGER`).
//
// 🔴 ONE RENDERER, SO POD AND CLI CARRY THE SAME REPORT BYTES. The report below is
// `report.Sessions` → `RenderText` → `Exit`, the exact functions the pod's `GET sessions/<scope>`
// reaches through `report.Reader`. What differs is only the one line ABOVE the report — the pod
// prints its snapshot freshness, this prints the cache-state banner — exactly as for `recall`.
// `TestTheSessionsReportIsByteIdenticalOnPodAndCLI` pins it over one synthetic store.
//
// 🔴 THE SAME PATH `recall` TAKES TO THE CACHE, step for step: the scope is derived (`--scope`, or
// `--repo`'s git common dir) BEFORE any sync because it decides which instance to read; an
// unrouted scope refuses before the network (`*UnroutedScope`, exit 11); the WHOLE store is synced,
// never a scope-filtered cache (which would answer `scope-absent` for every scope not fetched); a
// state with an exit hint (no cache, refused archive) prints its banner and exits with that hint.
// Authorisation is the pod's: the cache holds only what the snapshot shipped for this credential,
// so `store.Unrestricted()` here reads a set the pod already narrowed — the `recall` arrangement.
//
// Exit codes are the existing read set and nothing new: 0 for every answer (`sessions-listed`,
// `no-attributed-writes`, `scope-empty`, `scope-absent`), 3 when nothing in the scope could be
// scanned or there is no cache, 2 for usage, 4/5/11 from the shared state and routing paths.
func Sessions(env Env, opts Options) (int, error) {
	scope, scopeReason := ScopeOrReason(opts.Scope, opts.Repo)
	alias, label, cache := DefaultAlias, "", opts.Cache
	if scopeReason == "" {
		var routeErr error
		alias, label, cache, routeErr = readInstance(opts, scope)
		if routeErr != nil {
			return 0, routeErr
		}
	}
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
	rep, err := report.Sessions(cache, scope, store.Unrestricted())
	if err != nil {
		return 0, err
	}
	// The `scope-empty` promotion `recall` makes, for the same reason: a live sync that reached
	// the store and found nothing for this scope says so in the banner, keeping the stamp detail.
	if state.Name == StateLive && isEmptyStatus(rep.Status) {
		state = emptyState(state)
	}
	fmt.Fprintln(env.Stdout, BannerNamed(state.Name, state.Detail, label))
	fmt.Fprintln(env.Stdout)
	fmt.Fprintln(env.Stdout, rep.RenderText())
	code, warning := rep.Exit()
	if warning != "" {
		fmt.Fprintln(env.Stderr, warning)
	}
	return code, nil
}
