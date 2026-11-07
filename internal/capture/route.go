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
	// OwnerOnly is "ship this session, but declare it owner-only": it carries `*` (a visibility
	// statement, not a scope — see [Decide]) or it read scopes on TWO instances and has been placed
	// on the default one anyway. [Agent.route] adds `*` to the session's `Declared` set so the pod
	// stores it owner-only.
	OwnerOnly bool
	Reason    string
}

// Decide is decision 16: the instance that routes EVERY scope in V — writes AND reads.
//
//   - V empty → placed by the REPOSITORY the session ran in ([decideByRepo]), and failing that the
//     DEFAULT instance (where the owner-only rule applies, O10).
//   - every scope routes to one alias → that alias.
//   - scopes route to two aliases → the DEFAULT instance, OWNER-ONLY (OwnerOnly).
//   - `*` with MORE than one instance configured → the DEFAULT instance, OWNER-ONLY (OwnerOnly).
//     `*` forces the default however V is ordered, and however few other scopes it carries: it is
//     the conservative direction, and an order-dependent answer is not a policy.
//   - a scope the routing names but this host does not configure → HELD (unchanged): the table says
//     that scope lives on ANOTHER instance, and shipping its records to the one this host runs is
//     the cross-instance leak decision 16 exists to stop.
//
// 🔴 `*` IS A VISIBILITY STATEMENT, NOT AN UNPLACEABLE SCOPE — and it used to HOLD the session for
// good, which was measured to be the largest capture gap on a two-instance host: the client records
// `*` for every verb whose output names scopes it cannot enumerate (`routes`, `doctor`, `arcs`, the
// `transcript` verbs) and for any header that named no scope, so one `cairn routes` condemned a
// whole session. `*` says "this may be anything, so nobody but the owner may read it"; it does not
// say the session has no home. Routing it to the default instance while DECLARING `*` gives the pod
// exactly that visibility, which is the same rule a one-instance host has always applied.
//
// 🔴 CROSS-INSTANCE SESSIONS ARE PLACED, NOT HELD (operator decision): a session that read BOTH
// instances' scopes is shipped to the default instance OWNER-ONLY. The trade is explicit and was
// accepted with it: bytes read from the other instance now sit on this one, and the ONLY thing
// keeping them away from a reader who could not see them there is the owner-only declaration. A
// session that must move is withdrawn from where it was, exactly as any other move (S6, clause m),
// so nothing is duplicated.
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
	ownerOnly, ownerOnlyWhy := false, ""
	for _, scope := range v {
		if scope == scopeuse.Star {
			if multi {
				// See the doc comment: `*` narrows who may READ the session; it does not decide
				// where the session lives, and it does not "vote" for the instance its other scopes
				// name either — it forces the DEFAULT one. Otherwise the answer would depend on the
				// ORDER of V (`{*, beta-notes}` → personal, `{beta-notes, *}` → client), and the
				// conservative direction is the one that keeps content that may belong to either
				// instance off the non-default one.
				ownerOnly, ownerOnlyWhy = true, "an unknown scope (*)"
				target = client.DefaultAlias
			}
			continue
		}
		alias, err := routing.AliasFor(scope)
		if err != nil {
			// 🔴 A SCOPE THE TABLE EXPLICITLY ROUTES ELSEWHERE IS HELD AT ANY INSTANCE COUNT. On a
			// one-instance host the "nowhere else it could go" argument holds only for a scope the
			// table says NOTHING about; a table naming an alias this host has no config for is a
			// statement that the scope lives on ANOTHER instance, and shipping it to the sole one
			// is the cross-instance leak decision 16 exists to stop (review round 1). Unchanged by
			// the owner-only decision above: THAT one places a session whose scopes this host DOES
			// serve, and says so with `*`; this one would move records to an instance the table
			// says nothing about.
			if _, named := routing.Routes[scope]; multi || named {
				return Decision{Held: true, Reason: "scope " + scope + " routes to no configured instance"}
			}
			continue
		}
		if target == "" {
			target = alias
			continue
		}
		if alias != target {
			// Two instances, one session: place it on the default one, owner-only.
			ownerOnly, ownerOnlyWhy = true, "its scopes route to two instances ("+target+", "+alias+")"
			target = client.DefaultAlias
		}
	}
	if target == "" {
		target = client.DefaultAlias
	}
	if ownerOnly {
		return Decision{Instance: target, OwnerOnly: true,
			Reason: ownerOnlyWhy + ": shipped to " + target + ", owner-only"}
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
