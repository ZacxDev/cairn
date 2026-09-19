package control

import (
	"fmt"
	"sort"

	"github.com/ZacxDev/cairn/internal/store"
)

// Principal is who a request is.
//
// 🔴 A REQUEST RESOLVES TO EXACTLY ONE OF THESE, COMPUTED ONCE. The property being
// preserved from the token-file design: authentication and authorization must come
// out of the SAME match, so no route can be authenticated against one credential
// and authorised against another. `Authenticate` is the only constructor that a
// server path may use, and it returns the Principal and the Authorization together.
type Principal struct {
	Kind Kind
	ID   ID
	// Display is what the audit line carries. Never a token, never a digest.
	Display string
	// CredentialID is the credential this principal authenticated with, for the
	// audit trail. Empty for a principal resolved without one (the UI's session
	// path, which P4 adds).
	CredentialID ID
}

// String renders a principal for an audit line.
func (p Principal) String() string {
	if p.Display == "" {
		return fmt.Sprintf("%s:%s", p.Kind, p.ID)
	}
	return fmt.Sprintf("%s:%s (%s)", p.Kind, p.ID, p.Display)
}

// Authorization is one principal's materialized authority over scopes.
//
// 🔴 THE ZERO VALUE PERMITS NOTHING, AND THAT IS THE FAIL-CLOSED DIRECTION THE
// WHOLE DESIGN RESTS ON. There is no "unrestricted" Authorization — the control
// plane has no principal that sees everything, by construction. The legacy bare
// token that DID is a property of the token file, not of this model; the migration
// that reconciles the two is a separate change, and it must convert that authority
// into explicit grants rather than reintroduce a wildcard here.
type Authorization struct {
	// byScope maps a scope id to what this principal may do to it. A scope absent
	// from the map is a scope this principal cannot see at all — which is the same
	// observable as a scope that does not exist, deliberately, because a refusal
	// that discriminates is an enumeration API.
	byScope map[ID]VerbSet
	// names is the scope id to display-name projection, carried alongside so that
	// `VisibleScopes` does not need the Model again. Built at resolve time from
	// the same Model, so the two cannot disagree.
	names map[ID]string
	// Epoch is the Model epoch this was computed from. It travels with the
	// authority so that a materialized copy can report how stale it is instead of
	// asserting it is current.
	Epoch uint64
}

// Allows is THE PREDICATE. Everything that narrows anything consults this.
//
// 🔴 ONE FUNCTION, BECAUSE THE SAME QUESTION IS ASKED AT FOUR SITES — the index
// loader's visible set, the result-shape narrowing, the snapshot candidate filter
// and the write path. Open-coded at four sites it would be wrong at three of them
// in the same direction, and the direction that matters is "wider than the
// principal's authority". `store.VisibleScopeSet` already carries that lesson for
// the name-based narrowing; this is the same lesson one level up, where the answer
// depends on the verb as well as the scope.
func (a Authorization) Allows(scope ID, verb Verb) bool {
	return a.byScope[scope].Has(verb)
}

// VerbsOn returns everything this principal may do to one scope.
func (a Authorization) VerbsOn(scope ID) VerbSet { return a.byScope[scope] }

// ScopeIDs lists every scope this principal can reach with the given verb, sorted.
func (a Authorization) ScopeIDs(verb Verb) []ID {
	var out []ID
	for id, vs := range a.byScope {
		if vs.Has(verb) {
			out = append(out, id)
		}
	}
	sortIDs(out)
	return out
}

// VisibleScopes projects this authority onto the name-based set the reader already
// narrows with.
//
// 🔴 THIS IS THE SEAM, AND IT IS DELIBERATELY THE ONLY ONE. `store.ScopeSet` is
// what `LoadIndex`, `LoadStore`, `report.Recall`, `report.Search` and
// `snapshot.Build` all take today, and none of them learns about the control plane.
// Converting here — ids to display names, once, at the edge — means the control
// plane can grow projects, grants and memberships without a second visibility
// decision appearing inside the reader.
//
// 🔴 AND IT NEVER RETURNS `store.Unrestricted()`. A principal with authority over
// every scope in the model gets the ENUMERATION of those scopes, not the wildcard.
// The two are observably identical today and diverge the instant a scope exists
// that the model does not know about — a directory on disk with no scope record —
// where the wildcard would serve it and the enumeration will not. Serving an
// unmodelled directory is exactly the cross-tenant read this package exists to
// prevent.
func (a Authorization) VisibleScopes(verb Verb) store.ScopeSet {
	var names []string
	for _, id := range a.ScopeIDs(verb) {
		if name, known := a.names[id]; known {
			names = append(names, name)
		}
	}
	return store.VisibleScopeSet(names)
}

// Resolve computes a principal's authority from the model.
//
// 🔴 TWO SOURCES, ONE FUNCTION, AND THE DISTINCTION IS REAL RATHER THAN A
// DUPLICATION. Authority arrives two ways:
//
//  1. OWNERSHIP — a user is a member of a project, so they reach the project's own
//     scopes at their role's verbs.
//  2. SHARING — a live grant names the principal (or a project they belong to) as
//     its subject.
//
// Those are different facts about the world and both have to be consulted. What
// one-rule-one-place requires is not one TABLE but one FUNCTION that computes the
// union — this one. Nothing else in the codebase may decide what a principal can
// see, and the matrix test pins that as a relationship rather than per component.
//
// 🔴 A PROJECT GRANT EXPANDS AT RESOLVE TIME, NOT AT GRANT TIME. Granting a project
// and storing the expansion would freeze the membership as it stood that day: every
// later join would silently miss the share and every later departure would silently
// keep it. Expanding here means the share follows the membership, which is what a
// team share means to the person who clicked it.
func Resolve(m Model, p Principal) Authorization {
	a := Authorization{
		byScope: map[ID]VerbSet{},
		names:   map[ID]string{},
		Epoch:   m.Epoch,
	}

	// The set of subjects whose grants count as this principal's. A user brings
	// every project they are a member of; a project principal brings only itself.
	subjects := map[subjectRef]struct{}{
		{p.Kind, p.ID}: {},
	}
	if p.Kind == KindUser {
		for projectID, ms := range m.userProject[p.ID] {
			subjects[subjectRef{KindProject, projectID}] = struct{}{}
			// Source 1: ownership. Membership in a project confers the role's
			// verbs over that project's OWN scopes.
			for _, scopeID := range m.ScopesIn(projectID) {
				a.add(scopeID, roleVerbs[ms.Role])
			}
		}
	}

	// Source 2: sharing.
	for _, g := range sortedGrants(m) {
		if !g.Live() {
			continue
		}
		if _, mine := subjects[subjectRef{g.SubjectKind, g.SubjectID}]; !mine {
			continue
		}
		switch g.ObjectKind {
		case ObjectScope:
			a.add(g.ObjectID, g.Verbs)
		case ObjectProject:
			for _, scopeID := range m.ScopesIn(g.ObjectID) {
				a.add(scopeID, g.Verbs)
			}
		default:
			// Unreachable: `apply` refuses an unknown object kind. Skipping rather
			// than panicking keeps the narrowing direction safe if that ever stops
			// being true — an object kind nobody understands confers nothing.
			continue
		}
	}

	// Drop any scope that ended up with an empty verb set, and attach names. A
	// zero-verb entry and an absent one must be the same observable; keeping both
	// shapes would give `ScopeIDs` and `Allows` two ways to say no.
	for id, vs := range a.byScope {
		if vs.Empty() {
			delete(a.byScope, id)
			continue
		}
		if sc, known := m.Scopes[id]; known {
			a.names[id] = sc.DisplayName
		} else {
			// A grant naming a scope the model does not hold cannot happen —
			// `apply` checks the object exists. If it ever does, the scope has no
			// name to narrow on, so it is dropped rather than carried as an id
			// nothing can resolve.
			delete(a.byScope, id)
		}
	}
	return a
}

func (a Authorization) add(scope ID, verbs VerbSet) {
	if verbs.Empty() {
		return
	}
	a.byScope[scope] = a.byScope[scope].Union(verbs)
}

type subjectRef struct {
	kind Kind
	id   ID
}

// sortedGrants iterates grants in a fixed order.
//
// The union is order-independent, so this is not needed for correctness — it is
// needed for REPRODUCIBILITY of anything derived from the iteration, and for a test
// that compares whole authorizations to be comparing a stable thing. Map range
// order in Go is deliberately randomised; a guard that passes because the range
// happened to be favourable is the shape this repository's rules call a green for
// the wrong reason.
func sortedGrants(m Model) []Grant {
	out := make([]Grant, 0, len(m.Grants))
	for _, g := range m.Grants {
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// ErrNoCredential is the ONE authentication failure this package produces.
//
// It carries nothing — no reason, no near-miss, no hint about whether the token was
// unknown, revoked, or bound to a principal that no longer exists. The same
// discipline as `authz.ErrRejected`: a reason that reaches the wire is an
// enumeration API, and here it would additionally distinguish "revoked" from
// "never existed", which tells an attacker whether they found a real credential.
type ErrNoCredential struct{}

func (ErrNoCredential) Error() string { return "unauthorized" }

// Authenticate resolves a presented bearer token to a principal and its authority.
//
// 🔴 ONE MATCH, THREE FACTS — the same rule `authz.Authorize` states: who this is,
// what it may see, and which credential said so all come out of the SAME lookup, so
// no caller can authenticate against one row and authorise against another.
//
// 🔴 NO EARLY EXIT. The loop runs to completion whether or not it has matched, so
// the response time does not encode WHICH credential was presented. During a
// rotation overlap, "you used the old one" is precisely the fact an attacker wants
// and a `break` on first hit makes it measurable from outside.
//
// 🔴 A REVOKED CREDENTIAL IS SKIPPED, NOT REPORTED. It fails exactly as an unknown
// token does, with the same error and the same shape.
func Authenticate(m Model, token string) (Principal, Authorization, error) {
	if token == "" {
		return Principal{}, Authorization{}, ErrNoCredential{}
	}
	presented := HashToken(token)
	var matched *Credential
	for _, c := range sortedCredentials(m) {
		if !c.Live() {
			continue
		}
		if EqualHash(presented, c.TokenHash) {
			found := c
			matched = &found
		}
	}
	if matched == nil {
		return Principal{}, Authorization{}, ErrNoCredential{}
	}

	p, known := m.PrincipalFor(matched.PrincipalKind, matched.PrincipalID)
	if !known {
		// The credential names a principal the model no longer holds. That is not
		// an authenticated request: resolving it would produce an Authorization
		// with no owner, and the safe answer to "who is this" is nobody.
		return Principal{}, Authorization{}, ErrNoCredential{}
	}
	p.CredentialID = matched.ID
	return p, Narrow(Resolve(m, p), matched.NarrowedScopes), nil
}

// PrincipalFor builds the Principal for an entity the model holds, or reports that
// it holds none.
//
// 🔴 ONE CONSTRUCTOR, BECAUSE P4 ADDED A SECOND CALLER AND A SECOND SPELLING WOULD
// HAVE BEEN A SECOND ANSWER TO "WHO IS THIS". Until identity became an interface,
// `Authenticate` was the only path that turned an entity id into a Principal and it
// built one inline. `internal/identity`'s JWT and trusted-header backends resolve a
// principal with NO credential behind it, so they need the same construction — and
// the field that matters is `Display`, which the audit line's `identity=` and every
// written bullet's ACTOR carry. Two functions filling that field would be two
// answers to what a bullet says about who wrote it.
//
// 🔴 `false` MEANS THE MODEL HOLDS NO SUCH ENTITY, AND EVERY CALLER MUST REFUSE ON
// IT. `displayOf` returns the empty string for an unknown id, and a Principal with
// no display is a credential whose use cannot be attributed. The boolean is returned
// rather than left for the caller to re-derive from `Display == ""`, because that
// re-derivation is the shape this repository's rules name: one predicate, two
// spellings, wrong at one of them.
func (m Model) PrincipalFor(kind Kind, id ID) (Principal, bool) {
	display := displayOf(m, kind, id)
	if display == "" {
		return Principal{}, false
	}
	return Principal{Kind: kind, ID: id, Display: display}, true
}

// UserByProviderSubject resolves an identity provider's own (provider, subject) pair
// to the user this control plane holds for it.
//
// 🔴 `Provider`+`Subject` IS THE NATURAL KEY AND `Email` IS NOT — the rule `User`'s
// own comment states, asked here rather than restated by each identity backend. An
// email is mutable at the provider and is reused across providers, so a lookup keyed
// on it merges two people who share an address at two IdPs. Both the Supabase JWT
// backend and the trusted-header backend ask exactly this question, which is why it
// is one function and not two.
//
// 🔴 AND A MISS IS A REFUSAL, NEVER A CREATION. An IdP that vouches for somebody this
// control plane has never heard of has authenticated a stranger, not provisioned an
// account; just-in-time provisioning is signup, it is P6, and doing it here would make
// every read route a user-creation endpoint for anyone with an account at the IdP.
//
// 🔴 AT MOST ONE ROW CAN MATCH, AND THAT IS ENFORCED IN `apply` RATHER THAN ASSUMED
// HERE. The loop below returns the FIRST match and Go randomises map range order, so two
// user rows carrying one (provider, subject) pair would make this function answer a
// different user — with a different authorization — on different calls in one process.
// `EventUserCreated` refuses the second row for exactly that reason; without that rule
// this scan would be a coin flip rather than a lookup.
//
// ⚠ IT IS A LINEAR SCAN, AND THAT IS A DELIBERATE NON-OPTIMISATION RATHER THAN AN
// OVERSIGHT. `Resolve` already sorts every grant in the model on every authentication,
// so a scan over users is not a new complexity class here; adding a third index to
// `Model` would have to be maintained by `apply`, which is where the two membership
// indexes already cost a `clone` that is one nesting level deep. Revisit it when the
// model is big enough to measure, with the measurement in hand.
func (m Model) UserByProviderSubject(provider, subject string) (User, bool) {
	if provider == "" || subject == "" {
		// Not a lookup that can succeed: a user row with an empty provider or subject
		// cannot be created (`apply` refuses one), so an empty probe here would be
		// asking whether the model holds something it cannot hold. Refusing keeps the
		// caller from reading "no match" as "the IdP sent nothing and that was fine".
		return User{}, false
	}
	for _, u := range m.Users {
		if u.Provider == provider && u.Subject == subject {
			return u, true
		}
	}
	return User{}, false
}

// Narrow applies a credential's scope restriction.
//
// 🔴 IT INTERSECTS AND CANNOT WIDEN, AND THAT IS ENFORCED STRUCTURALLY RATHER THAN
// VALIDATED. The implementation keeps only scopes the principal ALREADY has, so a
// narrowing naming a scope the principal has since lost — or never had — confers
// nothing at all. Validating the list at issue time instead would be a claim about
// the authority as it stood then; this keeps being true as the authority changes,
// which is the only version that survives a revocation.
//
// ⚠ `nil` MEANS NO NARROWING; A NON-NIL EMPTY SLICE MEANS NOTHING IS VISIBLE. They
// are opposites. The journal format preserves the distinction on purpose (see
// `Event.NarrowedScopes`), and `copyIDs` preserves it across every copy.
func Narrow(a Authorization, only []ID) Authorization {
	if only == nil {
		return a
	}
	keep := make(map[ID]struct{}, len(only))
	for _, id := range only {
		keep[id] = struct{}{}
	}
	out := Authorization{
		byScope: map[ID]VerbSet{},
		names:   map[ID]string{},
		Epoch:   a.Epoch,
	}
	for id, vs := range a.byScope {
		if _, wanted := keep[id]; !wanted {
			continue
		}
		out.byScope[id] = vs
		out.names[id] = a.names[id]
	}
	return out
}

func sortedCredentials(m Model) []Credential {
	out := make([]Credential, 0, len(m.Credentials))
	for _, c := range m.Credentials {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func displayOf(m Model, kind Kind, id ID) string {
	switch kind {
	case KindUser:
		if u, known := m.Users[id]; known {
			if u.Email != "" {
				return u.Email
			}
			return u.Provider + ":" + u.Subject
		}
	case KindProject:
		if p, known := m.Projects[id]; known {
			return p.Name
		}
	}
	return ""
}
