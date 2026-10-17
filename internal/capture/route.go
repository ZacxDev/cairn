package capture

import (
	"github.com/ZacxDev/cairn/internal/client"
	"github.com/ZacxDev/cairn/internal/transcript/scopeuse"
)

// Decision is where one session may go on this run.
type Decision struct {
	// Instance is the alias to ship to, "" when Held or Deferred.
	Instance string
	Held     bool
	// Deferred is "send nothing of this session THIS run, and ask again next run": its repo routes
	// to an instance this run has no `-worker` for, and it is bound to a DIFFERENT non-default one
	// ([RepoRules.place]). Not final, unlike Held.
	Deferred bool
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
	// Bound is the instance the session is bound to now ([SessionState.Instance]), "" for a new one.
	Bound string
}

func (r RepoRules) uploadable(alias string) bool { return r.Uploadable == nil || r.Uploadable[alias] }

// place is a rule's answer `alias` — configured on this host — given what this run can upload to.
//
// 🔴 A CONFIGURED ALIAS WITH NO `-worker` THIS RUN NEVER RE-PLACES A BOUND SESSION (#292's audit,
// round 2). Uploadability is a property of ONE RUN, not of where a session belongs. A session the
// arm moved to `client` on an earlier run was WITHDRAWN from the default, and the archive keeps a
// withdrawal as a deletion; answering "default" on a run that merely lacks client's `-worker` made
// that a move back, the default's probe answered 410, and the session was HELD — final, so restoring
// the `-worker` changed nothing. So nothing here ever MOVES a bound session; the three answers when
// `alias` has no `-worker`:
//
//   - bound to `alias` itself: it stays bound — its uploads are refused and retried, as before the arm;
//   - new, or bound to the DEFAULT: the default, where a new session goes, so the two agree from one run
//     to the next;
//   - 🔴 bound to some OTHER instance: DEFERRED — nothing is sent this run, counted `refused=` and asked
//     again next run (#292's audit, round 3). Keeping that binding shipped the session's NEW records
//     to an instance the table no longer routes its repo to, silently (exit 0, `refused=0`); moving
//     it to the default is the round-2 hold. Only a host with three or more instances reaches it.
func (r RepoRules) place(alias, reason string) Decision {
	switch {
	case r.uploadable(alias):
		return Decision{Instance: alias, Reason: reason}
	case r.Bound == alias:
		return Decision{Instance: r.Bound, Reason: reason + ", which has no -worker this run: it stays bound to " + r.Bound}
	case r.Bound == "" || r.Bound == client.DefaultAlias:
		return Decision{Instance: client.DefaultAlias, Reason: reason + ", which has no -worker this run: the default instance, owner-only"}
	}
	return Decision{Deferred: true, Reason: reason + ", which has no -worker this run, while the session is bound to " + r.Bound +
		": nothing of it is sent until " + alias + " has a -worker (asked again next run)"}
}

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
// 🔴 A CONFIGURED ALIAS THIS RUN HAS NO `-worker` FOR NEVER RECEIVES A NEW SESSION (#292's audit,
// F1). A table naming the repo's scope for such an instance bound new sessions to an endpoint that
// does not exist — refused on every run — and left every already-shipped one in
// `withdrawal-pending` for good; both at exit 0. A NEW session goes to the default, as before the
// arm; a session already BOUND stays where it is ([RepoRules.place]).
//
// ⚠ AND A TABLE ENTRY WHOSE ALIAS IS UNUSABLE NEVER REACHES RULE 2. The table spoke about this
// repo, so the owner rule must not override it: an entry pinning the scope to the default would
// otherwise be overridden whenever the default had no `-worker`. An entry naming an alias this host
// does not CONFIGURE goes to the default rather than HOLDING (the V arm holds such a scope)
// deliberately: a hold is FINAL and withdraws a shipped prefix, and a session that READ nothing has
// only its directory against it — a stale table line would otherwise withdraw and freeze every
// session ever run in that repo.
//
// A session whose directory is unknown, gone, not a git repository, or has no `origin` carries
// the zero [Repo] and reaches 3.
func decideByRepo(repo Repo, routing client.Routing, rules RepoRules) Decision {
	if repo.Scope != "" {
		if _, named := routing.Routes[repo.Scope]; named {
			alias, err := routing.AliasFor(repo.Scope)
			if err != nil {
				return Decision{Instance: client.DefaultAlias, Reason: "no recorded scope; the routing table names the repo's " +
					"scope " + repo.Scope + " for an instance this host does not configure: the default instance, owner-only"}
			}
			return rules.place(alias, "no recorded scope; the repo it ran in is scope "+repo.Scope+
				", which the routing table sends to "+alias)
		}
	}
	if alias, ok := rules.Owners.AliasFor(repo.Owner); ok {
		return rules.place(alias, "no recorded scope; the repo it ran in has origin owner "+repo.Owner+
			", which -route-origin-owner sends to "+alias)
	}
	return Decision{Instance: client.DefaultAlias, Reason: "no recorded scope: the default instance, owner-only"}
}
