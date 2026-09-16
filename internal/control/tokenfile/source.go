// Package tokenfile projects the static token file into a `control.Model`.
//
// 🔴 AN ADAPTER, NOT A MIGRATION, AND THE THREE REASONS ARE THESE. Wiring
// `internal/api` to authorise from `internal/control` needs principals to exist; a
// one-time conversion of the deployed token file into a journal would answer that.
// Reading the SAME file the pod already reads and synthesizing the equivalent users,
// projects, scopes, grants and credentials on every refresh answers it differently:
//
//  1. the DEPLOYED SECRET keeps working unchanged, so shipping this is a code change
//     rather than a coordinated cutover against a live pod whose only principals are
//     rows in that file;
//  2. the ROLLBACK is one commit. Nothing is written: `Events` is read-only, no caller
//     appends it to a `FileStore`, so reverting leaves no converted data behind to
//     restore;
//  3. the JOURNAL FORMAT IS NOT SETTLED. `go.mod` has no `require` block, which blocks
//     every Postgres driver and every embedded SQL engine, so P4 owes a decision about
//     stdlib-only before there is a durable authority to convert INTO — and piece (d)
//     changes what a scope IS, from a directory this file enumerates to a
//     `scope-created` event. A conversion today writes a format the next phase is
//     about to move.
//
// 🔴 AND THE REASON THIS USED TO GIVE FIRST IS FALSE — MEASURED, NOT RECONSIDERED. It
// read "because it makes the conformance corpus a DIFFERENTIAL gate over the
// migration". The corpus's world holds four scopes and its `wide-reader` principal's
// allowlist names all four, so no row anywhere in it exercises unrestricted-ness: the
// corpus cannot tell "unrestricted" from "allowlisted everything", which is precisely
// the property the adapter has to reproduce. Measured rather than argued — deleting the
// store-directory half of the enumeration below leaves `run_go.sh` at **116 PASS /
// 0 failures**. The corpus does gate that the served contract did not move for the
// principals it declares, and that is worth having; what gates the unrestricted half is
// `internal/api/authority_test.go`, whose `gamma-notes` is reachable by the BARE row
// alone.
//
// 🔴 THE HARD CASE IS THE LEGACY BARE ROW, AND IT IS WHY THIS FILE IS LONGER THAN
// ITS CODE. A bare token is UNRESTRICTED — `store.Unrestricted()`, a sentinel that
// answers yes to every scope name including one that appears after the check was
// written. `control.Authorization` has no such value by construction
// (`Authorization.VisibleScopes` never returns `store.Unrestricted()`, and its
// comment says exactly why: a wildcard serves a directory the model does not know
// about, which is the cross-tenant read the package exists to prevent). So an
// adapter has to ENUMERATE, and an enumeration is a claim about a moment. What that
// costs, measured rather than reasoned about, is the next paragraph and the matching
// section of `internal/control/README.md`.
//
// # THE DIVERGENCE
//
// 🔴 A SCOPE DIRECTORY CREATED AFTER THE LAST MATERIALIZATION IS NOT VISIBLE TO A BARE
// (LEGACY) ROW UNTIL THE NEXT ONE. Today `store.Unrestricted()` is a sentinel evaluated
// per request, so a directory that appeared one millisecond ago is readable by the next
// request. Here the scope set is a snapshot, so it is readable after the next refresh.
//
// ⚠ IT IS BOUNDED. IT IS NOT REPORTED, AND THIS LINE CLAIMED IT WAS. `control.Cache`
// refreshes on a timer and on every token reload, so the window is bounded by
// `api.AuthorityRefreshInterval`; `Cache.Staleness()` renders the epoch and its age as a
// value, and **no deployed program calls it** — `git grep 'Staleness()'` finds its own
// definition, two comments and `_test.go` files, and nothing that runs in the pod. So the
// correct sentence is BOUNDED AND SILENT: an operator inside the
// window has no way to see it, and an operator inside a `degraded` or `stale` window has
// no way to see that either. The surface is deferred because the one place to print it is
// the startup banner, which `tests/dualrun/harness.py` compares between the two servers —
// a Go-only field there moves a gate in the same change that most needs it.
// **CLOSING CONDITION:** a render (a `doctor` section, a status route, or a banner field
// declared in `wire.NORMALIZATIONS`) that a `tests/dualrun/` run exits 0 with. A mapped
// row is UNAFFECTED by the divergence itself — its allowlist is in the file, so a scope it
// names is covered whether or not the directory exists.
//
// ⚠ AND IT IS NARROWER THAN "A NEW SCOPE", because the two ways a scope appears do not
// both reach it. A scope created THROUGH THIS SERVER (`PUT` with `If-None-Match: *`,
// the first-entry case) is already in the enumeration: the creating row is mapped, so
// the scope is in its allowlist, so it was in the model before the directory existed.
// What is left is a directory created OUT OF BAND — `server/seed.sh`, which seeds
// through `kubectl exec … tar -xf -`.
//
// 🔴 SO THE MITIGATION IS THE TIMER, AND NOT AN OPERATOR RELOAD — THIS PARAGRAPH SAID
// OTHERWISE AND THE REPOSITORY'S OWN RUNBOOK REFUTES IT. It read "which is exactly the
// operation an operator already follows with a reload". `server/README.md`'s seeding
// procedure is `build-push.sh` → `seed.sh --push` → `port-forward` →
// `verify-byte-identity.sh`, and there is no SIGHUP anywhere in it; the only documented
// SIGHUP is the token-ROTATION procedure, a different operation with a different
// trigger. A mitigation resting on a habit nobody documented is not a mitigation, and
// the timer — gated by `cmd/cairn-server`'s
// `TestTheBinarysOwnTimerIsWhatClosesTheDivergence`, which runs the binary and watches
// the divergence heal with nobody asking — is the whole of it.
//
// CLOSING CONDITION, stated so it is checkable rather than aspirational: it closes when
// scopes stop being discovered from the filesystem at all — when a scope is created by
// an event in the control plane's own journal, which is the authority piece (d) and P4
// build. At that point `storeDirs` has no reason to exist and the enumeration is
// complete by construction. It does NOT close by widening this adapter, and it must not
// be closed by reintroducing an unrestricted principal.
//
// ⚠ IT WAS ALSO A `const Divergence` STRING, AND THE STRING IS DELETED RATHER THAN
// KEPT. Every reference to it in this repository was a comment or the NAME inside an
// error message: exported API with no reader, which is the shape this package's own
// rules refuse elsewhere ("minting a verb that no call site can branch on"). The claim
// belongs where a reader of the package meets it, which is here.
package tokenfile

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/ZacxDev/cairn/internal/authz"
	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/store"
)

// Provider is the `User.Provider` of the one synthetic user this adapter mints.
//
// It is not an identity provider and does not pretend to be: a token file vouches
// for nobody. The string exists so that a `User` row which came from a file is
// distinguishable from one an IdP created once P4 lands, in a journal, a log line or
// a UI, without anybody having to infer it from the absence of an email.
const Provider = "cairn-token-file"

// OperatorSubject names the synthetic user every synthesized project is owned by.
//
// 🔴 IT HOLDS NO CREDENTIAL AND IS A MEMBER OF NOTHING, DELIBERATELY. `apply`
// refuses a `project-created` whose owner does not exist, so a project needs one;
// giving that user a membership would confer `roleVerbs` over the project's scopes
// on a principal no token can authenticate as. An owner that can be reached by
// nobody is the narrow answer, and the narrow answer is the safe one.
const OperatorSubject = "operator"

// ScopesProjectName is the project that holds every scope this adapter knows about.
//
// 🔴 ONE PROJECT FOR ALL SCOPES, WITH THE PRINCIPALS OUTSIDE IT. A token file has no
// projects, so any project structure here is invented; inventing one project per
// identity and putting its scopes inside would give that identity authority through
// MEMBERSHIP, which is authority that never appears in the grant log. Every
// authority this adapter confers is an explicit `granted` event instead — which is
// the same ruling `internal/control/README.md` records for a project's own scopes,
// applied one layer out.
const ScopesProjectName = "token-file scopes"

// Source is a `control.Source` over a token file's records and a store root.
//
// ⚠ IT READS THE STORE ROOT, AND THAT IS NOT INCIDENTAL. The token file names the
// scopes a MAPPED row may see and names none at all for a bare row, so the only
// place "which scopes exist" is written down is the filesystem the pod serves. A
// `Source` that consulted the file alone would leave every legacy row authorised
// over nothing.
type Source struct {
	// StoreRoot is the directory whose subdirectories are scopes.
	//
	// 🔴 AN UNREADABLE ROOT IS AN ERROR — `Model` refuses, `ErrStoreRootUnreadable`
	// classifies it, and a cache that has already materialized keeps its last-known-good
	// credential table. This comment asserted the OPPOSITE for one round after the
	// behaviour changed, and pointed at the very function that contradicts it; the
	// argument for swallowing it, and why it is true at one instant and false across
	// time, is written out at `Model`.
	StoreRoot string

	// Records returns the token table currently in force. It is a FUNCTION rather
	// than a slice so that a SIGHUP reload has one place to publish to: the server
	// swaps its table, the next refresh reads the new one, and no second copy of
	// the records exists to go stale.
	Records func() []authz.TokenRecord

	// Now is the clock stamped onto every synthesized event. nil means
	// `time.Now().UTC()`.
	//
	// ⚠ A TOKEN FILE RECORDS NO TIMES AT ALL, so `Model.At` for a synthesized world
	// means "when this projection was built", not "when the authority last
	// changed". That is stated rather than hidden because `Staleness.ModelAt`
	// renders it beside `MaterializedAt`, and a reader comparing the two on a real
	// control plane is comparing two different facts.
	Now func() time.Time
}

func (s Source) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}

// compile-time proof that this satisfies the read half. There is no write half:
// the authority is a file an operator edits, and a `Writer` here would be a second
// way to change it that the file itself would not know about.
var _ control.Source = Source{}

// ErrStoreRootUnreadable is the projection refusing to describe a world it could not
// enumerate.
//
// 🔴 IT IS EXPORTED SO A CALLER CAN CLASSIFY THE REFUSAL, NOT SO ONE CAN RECOVER FROM
// IT. `errors.Is` against this is how `cmd/cairn-server` tells "the store volume is not
// mounted" from "the token file synthesizes an inconsistent world" in the one message an
// operator reads at startup. Nothing branches on it to keep serving, and the two places
// this error travels through treat it as they treat every other one:
// `control.Cache.refresh` keeps its last-known-good model, and `api.New` — whose first
// `SetTokens` is the materialization — refuses to build a server. Only the message at the
// top of `main` reads the sentinel.
var ErrStoreRootUnreadable = errors.New("the store root cannot be enumerated")

// Model synthesizes the world the current token table describes.
//
// 🔴 IT IS A PURE FUNCTION OF (records, scope names) APART FROM THE CLOCK. The same
// table over the same store produces the same ids, the same grants and the same
// epoch, because every id is `control.DerivedID` over a stable key. That is what
// makes re-materializing a no-op instead of a rename, and it is what lets a test
// assert that two refreshes agree.
//
// 🔴 AN UNREADABLE STORE ROOT IS AN ERROR, AND THE ARGUMENT THAT IT SHOULD NOT BE WAS
// TRUE AT ONE INSTANT AND FALSE ACROSS TIME. This comment used to say: every read route
// answers 503 `store-unreachable` through an unreadable root — `LoadStore` stats the root
// and `snapshot.Build` reads it, both before any narrowing is consulted — so a
// principal's visible set cannot be observed through a root nobody can read, and
// refusing to project would convert an unreadable store into an unreadable CREDENTIAL
// TABLE, which is strictly worse. Every clause of that is still true AT THE INSTANT OF
// THE FAILURE. What it missed is that the projection is CACHED: a Model built from an
// empty enumeration is committed by `control.Cache.refresh`, `materializedAt` moves, the
// status reports `fresh`, and that world is then served THROUGH A ROOT THAT IS READABLE
// AGAIN until the next refresh. Measured end-to-end: a bare row reading a scope that
// exists on disk answered `200 scope-absent` after recovery — a healthy pod, a readable
// store, and every scope gone.
//
// 🔴 SO THE ARGUMENT KEEPS ITS FORCE EXACTLY WHERE IT APPLIES, WHICH IS A CACHE THAT HAS
// ALREADY MATERIALIZED. There the refusal is what PRESERVES the credential table:
// `Cache.refresh` keeps its last-known-good model on any error, so every row goes on
// authenticating with the authority it had, and `Staleness` moves to `degraded`. The
// unreadable-store-becomes-unreadable-credential-table failure is what the cache
// prevents, and it needs this function to fail rather than to succeed with a lie.
//
// 🔴 AND THE OTHER HALF OF THAT TRADE, DECLARED RATHER THAN LEFT FOR THE NEXT READER:
// THE SAME REFUSAL PRESERVES A CREDENTIAL THE OPERATOR IS DELETING. "Keeps the table it
// had" and "cannot revoke" are one mechanism, not two. Measured at both ends over one
// probe — a materialized cache, the store root renamed away, the compromised row removed
// from the table, then the SIGHUP path (`api.Server.SetTokens` publishes the table and
// calls `Refresh`):
//
//	at fb7e788: the reload SUCCEEDS and the revoked credential stops authenticating.
//	at ca632e3: the reload REFUSES (`the store root cannot be enumerated`) and the
//	            revoked credential STILL AUTHENTICATES — across that reload and a second
//	            refresh attempt inside the same outage. It stops only once the root is
//	            readable again.
//
// ⚠ THE WINDOW IS THE OUTAGE, NOT THE REFRESH INTERVAL. Every other lag this package
// declares is bounded by `api.AuthorityRefreshInterval`; this one is bounded by however
// long the volume stays unmountable, because every attempt inside it fails the same way.
// And SIGHUP is the ONLY revocation path in the deployed binary: `Source` has no write
// half, so `Cache.ApplyNow` over this authority returns `ErrAuthorityReadOnly` and the
// "revoke now" bypass does not exist here.
//
// It is recorded as 🟡 rather than 🔴 because it is not silent. `cmd/cairn-server`'s
// refused-reload line says the table IS already swapped, names the new fingerprints,
// says the AUTHORITY is unchanged, and names the fingerprints still serving — so the
// operator is told, in the one message they are reading, that the revocation has not
// landed. What is missing is a surface that says so WITHOUT a SIGHUP — the
// `Staleness`-is-reportable-but-unreported gap the package doc records, whose closing
// condition covers this too.
//
// 🔴 AND THE COLD START IS A DECISION RATHER THAN A CONSEQUENCE: IT REFUSES. A cache with
// no last-known-good has nothing to keep, so the only two answers are "serve an
// enumeration known to be wrong" and "do not come up". `api.New` returns an error
// wrapping this one, `cmd/cairn-server` exits 78 (EX_CONFIG) naming the root, and the pod
// crash-loops until
// the volume is there. That loses nothing served, and "nothing" is checked rather than
// assumed: every read route answers 503 through such a root, and a write cannot land
// either, because `internal/write` creates a scope directory with `os.Mkdir` and NOT
// `MkdirAll` — so it fails on the absent parent rather than inventing a store. Refusing
// also removes the window rather than moving it to startup, which is where an unmounted
// volume actually puts it.
//
// ⚠ IT IS A DIVERGENCE FROM `server/server.py`, DECLARED RATHER THAN HIDDEN. The oracle
// starts over an unreadable root and answers 503 on every read; this binary does not
// start. Nothing else moves: after a materialization, the served answers are the oracle's
// for a root that is readable and 503 for one that is not. **CLOSING CONDITION,** and it
// is the same one the divergence above carries: it closes when scopes stop being
// discovered from the filesystem at all, because the store root is then not an input to
// the authority and an unreadable one is a read-path 503 again. It does NOT close by
// making this function lenient.
func (s Source) Model(ctx context.Context) (control.Model, error) {
	events, err := s.Events(ctx)
	if err != nil {
		return control.Model{}, err
	}
	m, err := control.Replay(events)
	if err != nil {
		// 🔴 RETURNED, NEVER PARTIALLY APPLIED. `Replay` fails whole; the cache above
		// keeps its last-known-good model, so a token file that somehow synthesizes an
		// inconsistent world leaves the previously-serving authority in place rather
		// than emptying it. The only shapes that can reach THIS branch are refused by
		// `authz.LoadTokens` upstream (two rows sharing one token, an identity claimed
		// twice), which is why this wraps rather than recovers. ⚠ "Returned" rather than
		// "reported": the error reaches `Staleness.LastError`, which no deployed program
		// renders — see the package doc.
		return control.Model{}, fmt.Errorf("token-file authority: %w", err)
	}
	return m, nil
}

// Events is the synthesized journal, exposed so a migration can WRITE it.
//
// ⚠ NOTHING WRITES IT TODAY, AND THAT IS THE POINT OF EXPOSING IT RATHER THAN A
// PROMISE THAT SOMETHING WILL. When the control plane gains a real authority, the
// one-time conversion is `Append`-ing exactly these events to a journal — so the
// conversion and the adapter cannot describe two different worlds, because they are
// one function. Until then this is read-only.
func (s Source) Events(_ context.Context) ([]control.Event, error) {
	at := s.now()
	records := s.Records()

	operator := control.DerivedID(control.PrefixUser, OperatorSubject)
	scopesProject := control.DerivedID(control.PrefixProject, ScopesProjectName)

	events := []control.Event{
		{
			Kind: control.EventUserCreated, At: at, UserID: operator,
			Provider: Provider, Subject: OperatorSubject,
		},
		{
			Kind: control.EventProjectCreated, At: at, ProjectID: scopesProject,
			Name: ScopesProjectName, UserID: operator,
		},
	}

	names, err := s.scopeNames(records)
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		events = append(events, control.Event{
			Kind: control.EventScopeCreated, At: at,
			ScopeID: scopeID(name), DisplayName: name, ProjectID: scopesProject,
		})
	}

	// 🔴 ONE PRINCIPAL PER DISTINCT IDENTITY, N CREDENTIALS. Two bare rows are an
	// overlap rotation of ONE unrestricted holder — `authz.LoadTokens` guard 12
	// exempts `legacy` for exactly that reason — so minting a project per ROW would
	// try to create the same project twice and fail the replay. The credential is
	// per row; the authority is per identity, which is the shape the file already
	// has.
	//
	// 🔴 AND THE DEDUPE IS KEYED ON THE AUTHORITY, NOT ON THE IDENTITY ALONE, BECAUSE
	// THE JUSTIFICATION ABOVE IS STRUCTURAL AND THE OLD KEY ONLY SPELLED IT. `seen[identity]`
	// makes the rows after the first CONTRIBUTE NOTHING BUT A CREDENTIAL: a mapped row
	// named `legacy` ahead of a real bare row collapsed both into the mapped row's
	// grants, and the bare row then GAINED `write` on one scope and LOST `read` on
	// another — a wrong authority produced by a projection that reported success. What
	// stops that today is `authz.LoadTokens` guards 8 and 12, in a DIFFERENT package;
	// a guard in another package being the only thing between this and a wrong authority
	// is the shape this repository refuses. So the key is `authorityKey`, and two records
	// that share an identity while describing different authorities are REFUSED here.
	//
	// ⚠ REFUSED RATHER THAN SPLIT, and that is forced rather than chosen: the principal
	// id is derived from the identity, so minting a second principal for the second
	// authority would emit `project-created` twice for ONE id and `Replay` would fail
	// whole anyway — with a message about a duplicate project instead of about the token
	// file. Failing here names the actual input.
	seen := map[string]string{}
	for _, r := range records {
		identity := r.Identity
		principal := control.DerivedID(control.PrefixProject, "principal\x00"+identity)
		key := authorityKey(r)
		if prior, minted := seen[identity]; minted {
			if prior != key {
				return nil, fmt.Errorf(
					"token-file authority: two rows share the identity %q and describe DIFFERENT authorities (%q, then %q); "+
						"one identity is one principal here, so the later row would contribute only a credential and silently "+
						"inherit the earlier row's grants", identity, prior, key)
			}
		} else {
			seen[identity] = key
			events = append(events, control.Event{
				Kind: control.EventProjectCreated, At: at, ProjectID: principal,
				Name: identity, UserID: operator,
			})
			events = append(events, s.grantsFor(at, r, principal, scopesProject)...)
		}
		events = append(events, control.Event{
			Kind: control.EventCredentialIssued, At: at,
			CredentialID: control.DerivedID(control.PrefixCredential, r.Fingerprint()),
			SubjectKind:  control.KindProject, SubjectID: principal,
			TokenHash: control.HashToken(r.Token), Label: identity,
		})
	}
	return events, nil
}

// authorityKey is what two rows sharing an identity must AGREE on.
//
// 🔴 IT IS DERIVED FROM THE SAME TWO FACTS `grantsFor` BRANCHES ON — `IsLegacy()` and the
// FOLDED scope list — so "the key says these rows are interchangeable" and "these rows
// would produce the same grants" cannot come apart. Keying on the raw `Scopes` slice
// instead would make `Alpha_Notes` and `alpha-notes` look like different authorities
// while `grantsFor` folds them into one, which is a refusal of a file that is fine.
//
// The scopes are SORTED, because a rotation that reorders one row's allowlist is the
// same authority and must not be refused, and joined on NUL, which no scope name can
// contain: `store.NormalizeRef` cannot produce one, so two lists cannot be spelled into
// the same key.
func authorityKey(r authz.TokenRecord) string {
	if r.IsLegacy() {
		return "legacy"
	}
	folded := make([]string, 0, len(r.Scopes))
	for _, raw := range r.Scopes {
		if name := foldScope(raw); name != "" {
			folded = append(folded, name)
		}
	}
	sort.Strings(folded)
	return "mapped\x00" + strings.Join(folded, "\x00")
}

// grantsFor is the ONE place a token row's authority becomes grants.
//
// 🔴 A MAPPED ROW GETS `read,write` AND A BARE ROW GETS `read`, AND THAT IS THE
// TOKEN FILE'S OWN RULE RESTATED STRUCTURALLY RATHER THAN A NEW POLICY. Today the
// write path asks `TokenRecord.IsLegacy()` — a fact about the row's SHAPE — and
// refuses. Here the same fact is carried as the absence of a verb, so the write path
// asks the predicate every other narrowing site asks. `admin` is conferred on
// nobody: the token file has no sharing to administer, and minting a verb that no
// call site can branch on is the spelled-rather-than-structural guard shape
// `internal/control/model.go` refuses.
//
// 🔴 THE BARE ROW IS A PROJECT-AS-OBJECT GRANT, NOT A LIST OF SCOPES, AND THAT
// NARROWS THE DIVERGENCE IT WOULD OTHERWISE CARRY. `Resolve` expands an
// `ObjectProject` grant against `m.ScopesIn(project)` at RESOLVE time, so every
// scope the model holds is covered — including one added to the model after this
// grant was synthesized. What remains uncovered is a scope the MODEL does not hold,
// which is a re-materialization away rather than a re-grant away. See `Divergence`.
func (s Source) grantsFor(at time.Time, r authz.TokenRecord, principal, scopesProject control.ID) []control.Event {
	if r.IsLegacy() {
		return []control.Event{{
			Kind: control.EventGranted, At: at,
			GrantID:     control.DerivedID(control.PrefixGrant, "legacy\x00"+string(principal)),
			SubjectKind: control.KindProject, SubjectID: principal,
			ObjectKind: control.ObjectProject, ObjectID: scopesProject,
			Verbs: control.NewVerbSet(control.VerbRead),
		}}
	}
	out := make([]control.Event, 0, len(r.Scopes))
	for _, raw := range r.Scopes {
		name := foldScope(raw)
		if name == "" {
			// Unreachable through `authz.ParseTokenRow`, whose guard 10 refuses a
			// scope that folds away. A record built programmatically can still carry
			// one, and a grant naming a scope with no record would fail the replay —
			// so it is dropped in the NARROWING direction rather than crashing the
			// authority.
			continue
		}
		out = append(out, control.Event{
			Kind: control.EventGranted, At: at,
			GrantID:     control.DerivedID(control.PrefixGrant, string(principal)+"\x00"+name),
			SubjectKind: control.KindProject, SubjectID: principal,
			ObjectKind: control.ObjectScope, ObjectID: scopeID(name),
			Verbs: control.NewVerbSet(control.VerbRead, control.VerbWrite),
		})
	}
	return out
}

// scopeNames is the enumeration that replaces the unrestricted sentinel.
//
// 🔴 THE UNION OF TWO SOURCES, AND NEITHER IS SUFFICIENT ALONE. The store root
// answers "which scopes exist", which is what a bare row needs and what a mapped row
// does not. The token file answers "which scopes may be written to", which includes
// a scope with NO DIRECTORY YET — the first-entry create that is how the store gained
// every scope it has. Enumerating only directories would silently take that verb away
// from a mapped row; enumerating only the file would leave a bare row blind.
//
// 🔴 FOLDED THROUGH `foldScope`, AND DEDUPED ON THE FOLDED FORM. Two directories that
// fold together (`Alpha_Notes` and `alpha-notes`) are ONE scope to every reader
// downstream — `store.ScopeSet.Allows` folds its probe — so recording two would be
// recording a distinction nothing can act on.
//
// ⚠ THE LOAD-BEARING PART IS THAT THIS FOLDS THE SAME WAY `grantsFor` DOES, NOT THAT IT
// FOLDS AT ALL. See `foldScope`: a mutation sweep measured which half of that sentence
// is the hazard, and this comment used to name the wrong one.
func (s Source) scopeNames(records []authz.TokenRecord) ([]string, error) {
	names := map[string]struct{}{}
	add := func(raw string) {
		if folded := foldScope(raw); folded != "" {
			names[folded] = struct{}{}
		}
	}
	for _, r := range records {
		for _, scope := range r.Scopes {
			add(scope)
		}
	}
	dirs, err := s.storeDirs()
	if err != nil {
		return nil, err
	}
	for _, dir := range dirs {
		add(dir)
	}
	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, name)
	}
	// Sorted so the event sequence — and therefore the epoch every id and grant is
	// reported against — is the same for the same world. Go's map range is
	// deliberately randomised; a projection whose epoch moved on every refresh would
	// make `Staleness` unreadable.
	sort.Strings(out)
	return out, nil
}

// storeDirs lists the store root the way `store.LoadIndex` does.
//
// 🔴 THE SAME RULE AS THE LOADER, NOT A SECOND ONE: every entry that STATS as a
// directory, dot-named ones included. The loader has no dotfile filter at the root —
// `snapshot.Build` does, and it is the narrower of the two — so enumerating the
// loader's rule makes this set a superset of both narrowing sites' candidates.
// Enumerating the snapshot's rule instead would silently take a dot-named scope away
// from a bare row that can read it today.
//
// 🔴 THE ROOT READ AND THE PER-CHILD STAT FAIL DIFFERENTLY, AND THE COMMENT HERE USED TO
// COVER ONLY THE SECOND. A failed `Stat` on one child drops ONE candidate that no route
// could render anyway; a failed `ReadDir` on the ROOT drops EVERY candidate, and on the
// deployed shape — where the principals are bare rows and the allowlist half of the union
// contributes nothing — that is the whole enumeration. It is an error, and `Model`'s
// comment is where the cost of that is argued.
//
// ⚠ A STAT THAT FAILS IS STILL SKIPPED, AND THE SKIP STILL CANNOT BE OBSERVED.
// `LoadIndex` returns an error for any errno outside `pathlib`'s ignored four, which the
// route answers `503 store-unreachable` — before visibility is consulted. So a scope this
// function could not classify is a scope no route will render either way. That argument
// is about ONE name and does not widen to the root, which is the confusion the paragraph
// above exists to end.
//
// ⚠ AN EMPTY `StoreRoot` IS NOT AN UNREADABLE ONE. It says no root was configured, which
// no deployed path can reach — `cmd/cairn-server`'s flag defaults to `/data` — and it is
// kept separable so that "nobody told us where the store is" cannot be read back as "the
// store would not open".
func (s Source) storeDirs() ([]string, error) {
	if s.StoreRoot == "" {
		return nil, nil
	}
	dirents, err := os.ReadDir(s.StoreRoot)
	if err != nil {
		return nil, fmt.Errorf("token-file authority: %w: %w", ErrStoreRootUnreadable, err)
	}
	var out []string
	for _, d := range dirents {
		info, statErr := os.Stat(s.StoreRoot + string(os.PathSeparator) + d.Name())
		if statErr != nil || !info.IsDir() {
			continue
		}
		out = append(out, d.Name())
	}
	return out, nil
}

// foldScope is the ONE fold from a raw name — a directory, or a word in an allowlist —
// to the name this projection records.
//
// 🔴 ONE FUNCTION, TWO CALLERS, AND THE HAZARD IS THE SPLIT RATHER THAN THE FOLD.
// `scopeNames` decides which `scope-created` events exist and `grantsFor` decides which
// scope id each grant names. If those two fold DIFFERENTLY — one of them inlined,
// reverted, or "simplified" at the site somebody happened to be reading — a grant names
// a scope the model does not hold, `apply` refuses it, `Replay` fails whole, and the pod
// authenticates NOBODY. That is why this is a function rather than two calls to
// `store.NormalizeRef`.
//
// ⚠ AND THE FOLD ITSELF IS A NORMALISATION, NOT A DECISION — SAID HERE BECAUSE THIS
// COMMENT CLAIMED OTHERWISE AND A MUTATION SWEEP MEASURED IT WRONG. Removing the fold
// from BOTH sites at once changes nothing observable: `store.VisibleScopeSet` folds the
// names it is given and `store.ScopeSet.Allows` folds the probe, so a scope recorded in
// raw form is still reachable under its folded name. What the fold buys is that the
// model holds ONE scope per ADDRESSABLE name — two directories that fold together are
// one scope to every reader downstream, and recording two would be recording a
// distinction nothing can act on. `tests/control_mutants.py` carries the corrected row.
func foldScope(raw string) string { return store.NormalizeRef(raw) }

// scopeID is the ONE derivation from a folded scope name to its id. Spelled once so
// the `scope-created` event and every grant naming it cannot disagree.
func scopeID(foldedName string) control.ID {
	return control.DerivedID(control.PrefixScope, foldedName)
}
