package client

// THE `sources` VERB — S3 of `claudedocs/plan-cairn-scope-refs.md` (decision 8): "which code does
// this scope describe", read-only and Go-only (the `scope-sources` row of
// `tests/testlib/capability_ledger.LEDGER` declares it).
//
// 🔴 IT ASKS THE POD AND NEVER THE CACHE, AND PRINTS THE POD'S BODY VERBATIM — the arc verbs'
// shape and for their reason: declarations live in the sources journal OUTSIDE the store tree, so
// they are not in the snapshot, and `report.SourcesReport.RenderText` stays the ONLY rendering of
// a declaration anywhere. A second renderer here would be a second thing to keep byte-identical.
//
// 🔴 A PLAIN READ'S EXIT CODES, NOT THE AUDITOR'S. 0 for every answer the pod gave —
// `sources-declared`, `sources-undeclared`, `sources-unconfigured` and `scope-absent` alike, since
// each is a fact about the scope and the pod's `X-Store-Exit` is 0 for all of them. A pod that did
// not answer 200 — including the 503 `store-unreachable` an unreadable journal produces — is 3
// ("nothing was read at all"), through `FetchReport`'s `*StoreUnreachable` and `Run`'s read arm
// (decision 1, round 5 🟢1). The proposed auditor maps that same 503 to 10; this verb does not,
// because it measures nothing.

// SourcesShow is `cairn sources --scope X` (or `--repo P`): the pod's `GET sources/<scope>`.
func SourcesShow(env Env, opts Options) (int, error) {
	scope := ResolveScope(opts.Scope, opts.Repo, env.Stderr)
	if scope == "" {
		return ExitUsage, nil
	}
	alias, label, cfg, err := podFor(scope)
	if err != nil {
		return 0, err
	}
	headers, body, err := FetchReport(cfg, "/api/v1/sources/"+quoteAll(scope), opts.Timeout)
	if err != nil {
		return 0, err
	}
	env.recordRead(alias, scope)
	return printPodReport(env, label, headers, body), nil
}
