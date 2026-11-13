package capture

import (
	"time"

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
	// ([RepoRules.place]); or it is already bound and this run's F1 is live, inside its window
	// ([DecideRun], [Decision.F1Deferred]). Not final, unlike Held.
	Deferred bool
	// F1Deferred marks the second kind of deferral: counted `deferred=` on the run line, not
	// `refused=`, and logged once per episode rather than every pass ([Agent.route]).
	F1Deferred bool
	// F1Lapsed marks a decision made by the BASE rule because the session's F1 deferral window has
	// expired with F1 still live ([DecideRun]).
	F1Lapsed bool
	// OwnerOnly is "ship this session to the default instance, owner-only": it carries `*` (a
	// visibility statement, not a scope — see [Decide]) or it read scopes on TWO instances and has
	// been placed on the default one anyway. OwnerOnlyBy says which, and so whether [Agent.route]
	// adds `*` to the session's `Declared` set ([Decision.DeclaresStar]).
	OwnerOnly   bool
	OwnerOnlyBy OwnerOnlyCause
	Reason      string
}

// OwnerOnlyCause is WHY a [Decision] is owner-only. The causes differ in one thing that matters:
// whether the owner-only statement is a fact about the session that holds for good, and so belongs
// in the grow-only `Declared` set the pod keeps, or a fact about THIS RUN only.
type OwnerOnlyCause int

const (
	// NotOwnerOnly is the zero value: the decision is not owner-only.
	NotOwnerOnly OwnerOnlyCause = iota
	// OwnerOnlyUnledgered: the only `*` is F1's — the session's inputs name the program and its read
	// ledger is EMPTY on this run ([scopeuse.Result]'s F1). 🔴 NOT DECLARED (#309): the ledger line
	// that answers F1 lands later, and the pod applies F1 ITSELF at view time from the ledger it
	// holds, so it needs no `*` from here. Declared, `*` sat in the pod's grow-only
	// `declared_scopes` and kept the session owner-only after its ledger landed.
	OwnerOnlyUnledgered
	// OwnerOnlyStar: a `*` the session's own evidence records — a ledger record, a header, F2.
	// Declared.
	OwnerOnlyStar
	// OwnerOnlyCrossInstance: the session's recorded scopes route to two instances (#308). Declared:
	// it is the only thing keeping the other instance's bytes from a reader who could not see them
	// there.
	OwnerOnlyCrossInstance
)

// DeclaresStar is whether this decision's owner-only statement must be written into the session's
// grow-only `Declared` set — every owner-only cause but F1's ([OwnerOnlyUnledgered]).
func (d Decision) DeclaresStar() bool {
	return d.OwnerOnly && d.OwnerOnlyBy != OwnerOnlyUnledgered
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
//
// Every `*` in `v` is the session's own (a ledger record, a header, F2); this run's F1 is
// [DecideRun]'s `f1`.
func Decide(v []string, repo Repo, routing client.Routing, rules RepoRules) Decision {
	return DecideRun(v, false, repo, routing, rules)
}

// DecideRun is [Decide] over `v` plus, when `f1` holds, F1's `*` for THIS RUN — the session's inputs
// name the program and its read ledger is empty now ([scopeuse.Result]'s F1). For a session not yet
// bound (and on a one-instance host) it routes exactly as `v ∪ {*}` would; the one difference is the
// decision's [OwnerOnlyCause]: when F1's is the ONLY `*` and no two recorded scopes disagree, it is [OwnerOnlyUnledgered], which is not declared (#309).
//
// 🔴 TWO RECORDED SCOPES DISAGREE ONLY WHEN THEY ROUTE TO DIFFERENT INSTANCES THEMSELVES. A `*`
// forcing the default instance does not make a session whose only scope routes elsewhere
// "cross-instance": that answer would declare `*` for good on the strength of F1's one-run `*`.
//
// 🔴 F1 PLACES A NEW SESSION; A BOUND ONE IS DEFERRED — FOR AT MOST [F1DeferWindow] — WHILE F1 IS
// LIVE (operator decisions on #317). A session already bound to an instance (`rules.Bound`) on a host
// with more than one instance gets [Decision.Deferred] whenever `f1` holds and its window has not
// expired — whatever the answer with F1 or without it would be, a hold included — so nothing of it
// is shipped and nothing is withdrawn on that pass. The window runs from the first pass that saw F1
// live while the session was bound (`rules.F1Since`, persisted as [SessionState.F1Since]; zero means
// "this pass starts it") and is cleared on any pass where F1 is false. Once it has expired with F1
// still live, the session is routed by the BASE rule exactly as before #317 — F1's `*` forces the
// default instance, through the usual move and withdrawal ([Decision.F1Lapsed]).
//
// WHY DEFER: F1 reverts when the ledger line lands, and a move does not — the instance a session is
// withdrawn from keeps the withdrawal as a FINAL deletion, so when the ledger's scope routes the
// session back there the probe answers 410 and the session is HELD for good ([Agent.route]). That
// flip was measured on a two-instance host: a session the repo arm had bound to the second instance
// ran a `cairn` call, Claude Code wrote the call's tool input to the transcript seconds BEFORE the
// client appended its ledger line, a pass landing in between saw F1 and moved the session to the
// default, and the next pass held it there. That race clears within one pass, far inside the window.
//
// WHY BOUNDED: F1's evidence is sticky — any tool input naming the program, restored every pass —
// so a bound session that never makes a ledgered call has F1 live for good, and an unbounded
// deferral stopped its capture for good (measured at several percent of one workstation's sessions,
// most of their bytes after the first mention, while local transcripts are pruned).
//
// WHAT IT GUARANTEES, AND NO MORE: inside the window a bound session ships nothing and withdraws
// nothing, so an F1 that clears within the window can never flip it into a 410 hold. ⚠ TWO NAMED
// RESIDUALS:
//
//   - THE FLIP SURVIVES PAST THE WINDOW: a session whose F1 stays live longer than [F1DeferWindow]
//     is moved by the base rule, and if a ledger line later routes it back to the instance it left,
//     that move is refused 410 and held, exactly as before #317.
//   - T3 (pre-existing — the new-session path has it on the base too): the pod's view-time F1 hides
//     a session only while its ledger is EMPTY (`archive.Meta.LedgerScopes`). Once any ledger record
//     lands, output that reached the transcript with NO ledger record of its own — a call whose
//     ledger write was swallowed (`client.Env.recordRead`), or one run with no session id in its
//     environment (`sudo`, `env -i`, `ssh`), including the headerless verbs (`transcript`, `routes`,
//     `arcs`, `doctor`) whose served call would otherwise have recorded `*` — ships, deferred or not,
//     under the scope that LATER record names (`TestTheUnledgeredOutputResidualShipsUnderTheLaterScope`
//     pins it, so a fix flips it deliberately).
//
// ⚠ ONE INSTANCE IS EXEMPT: there F1 changes no routing answer and there is no other instance to be
// withdrawn from, so a deferral would only stall capture.
func DecideRun(v []string, f1 bool, repo Repo, routing client.Routing, rules RepoRules) Decision {
	if f1 && rules.Bound != "" && routing.MultiInstance() {
		if !rules.f1WindowExpired() {
			return Decision{Deferred: true, F1Deferred: true, Reason: "its inputs name the program and its read ledger " +
				"is empty this run (F1): a session bound to " + rules.Bound + " ships nothing and withdraws nothing until " +
				"its ledger answers, for at most " + F1DeferWindow.String() + " (then it is routed by the base rule)"}
		}
		dec := decideRun(v, f1, repo, routing, rules)
		dec.F1Lapsed = true
		dec.Reason = "F1 stayed live past the " + F1DeferWindow.String() + " deferral window, so the base rule routes it: " +
			dec.Reason
		return dec
	}
	return decideRun(v, f1, repo, routing, rules)
}

// F1DeferWindow is how long a BOUND session is deferred while F1 stays live ([DecideRun]) — the
// operator's decision on #317. `TestTheF1DeferWindowIsTheOperatorsDecision` pins it as a literal.
const F1DeferWindow = time.Hour

// f1WindowExpired is whether this F1 episode's deferral window has run out: never before it starts
// (`F1Since` zero — this pass starts it), and from `F1Since + F1DeferWindow` on. A start AFTER `Now`
// (stamped while the clock ran ahead) is not expired either, which is also what [Agent.route]'s
// restamp of it to `Now` answers — so a dry run reading the unrestamped state agrees with `--run`.
func (r RepoRules) f1WindowExpired() bool {
	return !r.F1Since.IsZero() && !r.Now.Before(r.F1Since.Add(F1DeferWindow))
}

func decideRun(v []string, f1 bool, repo Repo, routing client.Routing, rules RepoRules) Decision {
	if len(v) == 0 && !f1 {
		return decideByRepo(repo, routing, rules)
	}
	multi := routing.MultiInstance()
	target := ""
	recordedStar, cross, crossWhy := false, false, ""
	for _, scope := range v {
		if scope == scopeuse.Star {
			recordedStar = true
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
		switch {
		case target == "":
			target = alias
		case alias != target && !cross:
			// Two instances, one session: place it on the default one, owner-only.
			cross, crossWhy = true, "its scopes route to two instances ("+target+", "+alias+")"
		}
	}
	why, by := "", NotOwnerOnly
	switch {
	case cross:
		why, by = crossWhy, OwnerOnlyCrossInstance
	case multi && recordedStar:
		// See the doc comment: `*` narrows who may READ the session; it does not decide where the
		// session lives, and it does not "vote" for the instance its other scopes name either — it
		// forces the DEFAULT one. Otherwise the answer would depend on the ORDER of V
		// (`{*, beta-notes}` → personal, `{beta-notes, *}` → client), and the conservative direction
		// is the one that keeps content that may belong to either instance off the non-default one.
		why, by = "an unknown scope (*)", OwnerOnlyStar
	case multi && f1:
		why, by = "its inputs name the program and its read ledger is empty (F1's *, not declared)", OwnerOnlyUnledgered
	}
	if by != NotOwnerOnly || target == "" {
		target = client.DefaultAlias
	}
	if by != NotOwnerOnly {
		return Decision{Instance: target, OwnerOnly: true, OwnerOnlyBy: by,
			Reason: why + ": shipped to " + target + ", owner-only"}
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
	// F1Since is when the session's current F1 deferral window began ([SessionState.F1Since]), zero
	// for none yet; Now is this pass's clock. [DecideRun] reads both, and only for a bound session
	// under F1.
	F1Since, Now time.Time
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
