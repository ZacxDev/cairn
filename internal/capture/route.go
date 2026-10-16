package capture

import (
	"github.com/ZacxDev/cairn/internal/client"
	"github.com/ZacxDev/cairn/internal/transcript/scopeuse"
)

// Decision is where one session may go on this run.
type Decision struct {
	// Instance is the alias to ship to, "" when Held.
	Instance string
	Held     bool
	Reason   string
}

// Decide is decision 16: the instance that routes EVERY scope in V — writes AND reads.
//
//   - V empty → placed by the REPOSITORY the session ran in ([decideByRepo]), and failing that the
//     DEFAULT instance (where the owner-only rule applies, O10).
//   - every scope routes to one alias → that alias.
//   - scopes route to two aliases → HELD.
//   - `*`, or a scope the routing cannot place, with MORE than one instance configured → HELD.
//     With exactly one instance there is nowhere else it could go, so it ships to that one, where
//     `*` and unknown names make it owner-only.
//
// 🔴 `repo` AND `rules` ARE READ ONLY WHEN V IS EMPTY. A session that recorded a scope is placed
// by what it READ, never by where it ran: a session in a repo the second instance's organisation
// owns that read only default-instance scopes still goes to the default instance, and the V
// branches below are unchanged by the repo arm (`TestARecordedScopeIsNeverOverruledByTheRepo`).
//
// It uses `internal/client`'s routing (`Routing.AliasFor`), the same resolver every `cairn` write
// uses — never a second spelling of the table.
func Decide(v []string, repo Repo, routing client.Routing, rules RepoRules) Decision {
	if len(v) == 0 {
		return decideByRepo(repo, routing, rules)
	}
	multi := routing.MultiInstance()
	target := ""
	for _, scope := range v {
		if scope == scopeuse.Star {
			if multi {
				return Decision{Held: true, Reason: "an unknown scope (*) with more than one instance configured"}
			}
			continue
		}
		alias, err := routing.AliasFor(scope)
		if err != nil {
			// 🔴 A SCOPE THE TABLE EXPLICITLY ROUTES ELSEWHERE IS HELD AT ANY INSTANCE COUNT. On a
			// one-instance host the "nowhere else it could go" argument holds only for a scope the
			// table says NOTHING about; a table naming an alias this host has no config for is a
			// statement that the scope lives on ANOTHER instance, and shipping it to the sole one
			// is the cross-instance leak decision 16 exists to stop (review round 1).
			if _, named := routing.Routes[scope]; multi || named {
				return Decision{Held: true, Reason: "scope " + scope + " routes to no configured instance"}
			}
			continue
		}
		if target != "" && alias != target {
			return Decision{Held: true, Reason: "its scopes route to two instances (" + target + ", " + alias + ")"}
		}
		target = alias
	}
	if target == "" {
		target = client.DefaultAlias
	}
	return Decision{Instance: target, Reason: "every scope routes to " + target}
}

// RepoRules is what [decideByRepo] places an empty-V session by, beside the routing table.
type RepoRules struct {
	// Owners is `-route-origin-owner`.
	Owners OwnerRoutes
	// Uploadable is every alias THIS RUN can upload to — under `--run`, the aliases with a `-worker`.
	// Nil places no restriction (a caller that uploads nowhere, such as a unit test of the rules).
	Uploadable map[string]bool
}

func (r RepoRules) uploadable(alias string) bool { return r.Uploadable == nil || r.Uploadable[alias] }

// decideByRepo places a session that recorded NO scope by the repository it ran in. First match
// wins:
//
//  1. the repo's SCOPE (derived as the client derives it — [client.ScopeForRepo], so a worktree
//     answers its base clone's) when the routing table NAMES it: the alias [client.Routing.AliasFor]
//     gives. 🔴 AN EXPLICITLY-NAMED SCOPE ALWAYS WINS, so a repo the operator pinned to the default
//     instance stays there whatever its origin's owner says.
//  2. the repo's `origin` OWNER when `-route-origin-owner OWNER=ALIAS` names it: that alias.
//  3. the DEFAULT instance — exactly what every empty-V session got before this arm existed.
//
// 🔴 AN ALIAS THIS RUN CANNOT UPLOAD TO IS NO MATCH (#292's audit, F1). A table naming the repo's
// scope for an instance with no `-worker` bound new sessions to an endpoint that does not exist —
// refused on every run — and left every already-shipped one in `withdrawal-pending` for good, the
// destination probe having nowhere to go; both at exit 0. Before this arm both shipped to the
// default. So the answer is the DEFAULT, as before: the session moves on the first run after the
// operator adds that instance's `-worker`.
//
// ⚠ AND A TABLE ENTRY WHOSE ALIAS IS UNUSABLE — not configured on this host, or not uploadable this
// run — GOES TO 3, NEVER TO 2. The table spoke about this repo, so the owner rule must not override
// it: an entry pinning the scope to the default would otherwise be overridden whenever the default
// had no `-worker`. Falling to 3 rather than HOLDING (the V arm holds a scope routed to an
// unconfigured alias) is deliberate: a hold is FINAL and withdraws a shipped prefix, and a session
// that READ nothing has only its directory against it — a stale table line would otherwise
// withdraw and freeze every session ever run in that repo.
//
// A session whose directory is unknown, gone, not a git repository, or has no `origin` carries
// the zero [Repo] and reaches 3.
func decideByRepo(repo Repo, routing client.Routing, rules RepoRules) Decision {
	if repo.Scope != "" {
		if _, named := routing.Routes[repo.Scope]; named {
			if alias, err := routing.AliasFor(repo.Scope); err == nil && rules.uploadable(alias) {
				return Decision{Instance: alias, Reason: "no recorded scope; the repo it ran in is scope " + repo.Scope +
					", which the routing table sends to " + alias}
			}
			return Decision{Instance: client.DefaultAlias, Reason: "no recorded scope; the routing table names the repo's " +
				"scope " + repo.Scope + " for an instance this run cannot upload to: the default instance, owner-only"}
		}
	}
	if alias, ok := rules.Owners.AliasFor(repo.Owner); ok && rules.uploadable(alias) {
		return Decision{Instance: alias, Reason: "no recorded scope; the repo it ran in has origin owner " + repo.Owner +
			", which -route-origin-owner sends to " + alias}
	}
	return Decision{Instance: client.DefaultAlias, Reason: "no recorded scope: the default instance, owner-only"}
}
