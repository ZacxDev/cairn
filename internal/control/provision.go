package control

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ZacxDev/cairn/internal/store"
)

// ProvisionUser is the OPERATOR-DRIVEN user-creation path: the first thing in this
// repository that writes to a journal-backed authority.
//
// 🔴 WHY IT EXISTS, STATED AS THE DEFECT IT CLOSES RATHER THAN AS A FEATURE. P4 shipped
// three identity backends and a durable `Store`, and nothing that could put a user INTO
// one: the only authority any binary wired was `tokenfile.Source`, which synthesizes a
// single user at provider `cairn-token-file` and subject `operator`, emits no
// `member-set`, and grants only `KindProject` subjects. So `SupabaseJWT`, whose default
// provider is `supabase`, could never match `UserByProviderSubject`, and a
// `TrustedHeader` aimed at the one pair that DID exist resolved to an EMPTY
// `Authorization`. An operator could configure a pod that fetched its JWKS, started
// clean, satisfied the partial-configuration ledger, passed its health check — and
// refused every sign-in. Both session backends were not untested; they were INERT.
//
// 🔴 AND IT IS AN OPERATOR ACTION, NOT SELF-SERVE SIGNUP. `UserByProviderSubject`'s own
// comment states the rule this function is the other half of: an IdP that vouches for
// somebody this control plane has never heard of has authenticated a stranger, and
// minting a row for them on the read path would make every route a user-creation
// endpoint for anybody with an account at the provider. Creation is therefore a
// deliberate act by somebody who can already write the journal. Quotas, rate limits,
// abuse handling and deletion/export belong to the signup path and are not here.
//
// 🔴 ONE BATCH, SO A HALF-CREATED USER IS NOT A STATE THIS PATH CAN PRODUCE. `Append`
// validates every event against a CLONE of the model before any byte reaches disk (see
// `FileStore.Append` and `Model.clone`), so a request whose third event conflicts leaves
// neither bytes nor state. A user row with no project, or a project with no membership,
// would authenticate somebody to an empty authority — which is the exact symptom above,
// reproduced one layer in.
//
// # What a provisioned user can actually reach, and why membership is what does it
//
// `Resolve` confers authority two ways: MEMBERSHIP in a project (its role's verbs over
// that project's own scopes) and a live GRANT naming the principal. This path uses the
// first: `project-created` + `member-set` at `RoleOwner`, which is read+write+admin over
// every scope the project holds. Sharing — a grant to a user or project that does not own
// the scope — is the later slice of P5 and is not here.
//
// ⚠ A ZERO-LENGTH `ScopeNames` IS ACCEPTED AND PRODUCES A USER WHO CAN REACH NOTHING.
// That is deliberate rather than an oversight: creating a person before deciding what
// they may see is a real operator sequence, and refusing it would force a scope to be
// invented to satisfy the call. The caller is handed the created scope list back so it can
// SAY so — `cmd/cairn-server` prints a line naming the state, because a user who
// authenticates to an empty authorization is indistinguishable, from the outside, from
// the defect this function closes.
func ProvisionUser(ctx context.Context, s Store, req NewUser) (Provisioned, error) {
	at := req.At
	if at.IsZero() {
		// `FileStore.Append` stamps a zero `At` too, so this is belt-and-braces there —
		// but `Store` is an interface and a backend is not required to stamp, while
		// `Event.validate` refuses a zero time from every writer. Stamping here means the
		// events this function builds are complete on their own.
		at = time.Now().UTC()
	}

	current, err := s.Model(ctx)
	if err != nil {
		return Provisioned{}, fmt.Errorf("reading the control journal: %w", err)
	}
	if err := checkScopeNamesAreFree(current, req.ScopeNames); err != nil {
		return Provisioned{}, err
	}

	userID, err := NewID(PrefixUser)
	if err != nil {
		return Provisioned{}, err
	}
	projectID, err := NewID(PrefixProject)
	if err != nil {
		return Provisioned{}, err
	}

	// 🔴 `NewID`, NOT `DerivedID`, AND THE CHOICE IS THE ONE `DerivedID`'s OWN COMMENT
	// FORBIDS GETTING WRONG. A derived id would have made re-running this path idempotent
	// for free — the second attempt would collide with the first on the id and be refused
	// by name. It is refused anyway, by the (provider, subject) uniqueness rule in
	// `apply`, and that rule is the one that matters: it holds for a journal written by
	// ANY writer, where an id derived from the pair only holds for writers that chose the
	// same derivation. `DerivedID` exists for adapters over an authority with no id
	// column; a journal has one.
	events := []Event{
		{
			Kind: EventUserCreated, At: at, Actor: req.Actor,
			UserID: userID, Provider: req.Provider, Subject: req.Subject, Email: req.Email,
		},
		{
			Kind: EventProjectCreated, At: at, Actor: req.Actor,
			ProjectID: projectID, Name: req.ProjectName, UserID: userID,
			// `Solo` is left false. It marks the project auto-created for a user at
			// SIGNUP so a UI can present it as "personal"; this project was named by an
			// operator. Nothing in authorization branches on it either way — see
			// `Project.Solo` — so the honest value is the one that describes how it was
			// made.
		},
		{
			Kind: EventMemberSet, At: at, Actor: req.Actor,
			ProjectID: projectID, UserID: userID, Role: RoleOwner,
		},
	}

	scopes := make([]ProvisionedScope, 0, len(req.ScopeNames))
	for _, name := range req.ScopeNames {
		scopeID, err := NewID(PrefixScope)
		if err != nil {
			return Provisioned{}, err
		}
		events = append(events, Event{
			Kind: EventScopeCreated, At: at, Actor: req.Actor,
			ScopeID: scopeID, DisplayName: name, ProjectID: projectID,
		})
		scopes = append(scopes, ProvisionedScope{Name: name, ID: scopeID})
	}

	// 🔴 NOTHING ELSE IS VALIDATED HERE, DELIBERATELY. A missing provider, a missing
	// subject, an empty project name and a duplicate scope name WITHIN this request are
	// all refused by `Event.validate` and `Model.apply`, which is where those rules
	// already live and where they apply to every writer. A second copy of them in this
	// function would be the one-rule-two-places shape that regenerates the same bug at
	// the second site — and the batch refusal already names the offending event and
	// field.
	after, err := s.Append(ctx, events...)
	if err != nil {
		return Provisioned{}, err
	}
	return Provisioned{User: userID, Project: projectID, Scopes: scopes, Epoch: after.Epoch}, nil
}

// NewUser is the one user an operator is creating, with the project and scopes created
// alongside them.
type NewUser struct {
	// Provider and Subject are the identity provider's own pair, and together they are
	// the natural key a session is looked up by — `supabase` and the Supabase user id for
	// the JWT backend, or whatever `CAIRN_TRUSTED_HEADER_PROVIDER` declares and the
	// subject header carries for the proxy backend. Both required.
	//
	// ⚠ THEY MUST MATCH THE BACKEND'S CONFIGURED PROVIDER EXACTLY, AND NOTHING HERE CAN
	// CHECK THAT. `SupabaseJWT` resolves against `DefaultSupabaseProvider` ("supabase")
	// unless `CAIRN_SUPABASE_PROVIDER` says otherwise; a user created under a different
	// string is a user that backend will never find. The mismatch is invisible to this
	// package — it holds no configuration — so it is a pod that refuses one person's
	// sign-in rather than a refusal here.
	Provider string
	Subject  string
	// Email is for display and for invites. It is NOT the key: an email is mutable at the
	// provider and reused across providers. It is what `Principal.Display` renders when
	// present, so the audit line names a person rather than a provider id.
	Email string
	// ProjectName is the project created for this user, who becomes its owner. Required —
	// `EventProjectCreated` refuses an unnamed project.
	ProjectName string
	// ScopeNames are the scopes created in that project.
	//
	// 🔴 A SCOPE'S DISPLAY NAME IS THE DIRECTORY NAME ON DISK, WHICH IS WHY THEY ARE
	// CHECKED AGAINST THE WHOLE JOURNAL AND NOT JUST THIS PROJECT — and checked by their
	// `store.NormalizeRef`'d form, because that is the name the reader and the writer both
	// resolve a directory by. The raw spelling is what gets STORED; it is never rewritten.
	// See `checkScopeNamesAreFree`.
	ScopeNames []string
	// Actor is recorded as the `actor` of every event in the batch. Empty is accepted:
	// `Event.validate` does not require it, and an operator running a command in a pod
	// has no `control.ID` of their own to name.
	Actor ID
	// At stamps every event in the batch. Zero means `time.Now().UTC()`, taken once so
	// the whole batch carries one instant.
	At time.Time
}

// Provisioned is what `ProvisionUser` created, so a caller can print the ids without
// reading the journal back.
type Provisioned struct {
	User    ID
	Project ID
	// Scopes is in the order `NewUser.ScopeNames` gave, so a printed line is reproducible.
	Scopes []ProvisionedScope
	// Epoch is the journal epoch after the batch — the number `control.Cache` compares to
	// decide whether a pod is serving this change yet.
	Epoch uint64
}

// ProvisionedScope pairs a created scope's display name with its id.
type ProvisionedScope struct {
	Name string
	ID   ID
}

// ErrScopeNameTaken refuses a scope display name that some other scope in the journal
// already carries.
var ErrScopeNameTaken = errors.New("control: that scope display name is already used in this journal")

// checkScopeNamesAreFree refuses a display name any existing scope carries, ANYWHERE in
// the journal — compared by its FOLDED form, which is what decides "one directory".
//
// 🔴 IT IS WIDER THAN THE MODEL'S OWN RULE, AND THE REASON IS THE PROJECTION ONTO DISK.
// `apply` refuses a duplicate scope name WITHIN a project and allows it across projects —
// correctly, because ids are what authorization is keyed on and two projects may each
// legitimately hold a scope called `notes`. But the pod does not serve ids: `Resolve`
// produces an `Authorization`, `Authorization.VisibleScopes` projects it onto
// `store.ScopeSet` — a set of NAMES — and the reader then narrows the store root's
// DIRECTORIES by those names. So two scope records sharing one display name resolve to
// ONE directory, and each project's members can read the other's entries. That is a
// cross-tenant read, and it is invisible to every id-keyed check in this package.
//
// 🔴 IT COMPARES `store.NormalizeRef`'d FORMS ON BOTH SIDES, BECAUSE A RAW `==` IS
// STRICTLY NARROWER THAN THE THING IT PROTECTS. The directory a name reaches is decided
// by that fold — lowercase, non-slug runes to `-`, runs collapsed, trimmed — on both
// sides of `store.ScopeSet.Allows` and again on the write path, where `createEntry` folds
// the scope before it resolves a filename. So `Quarry_Notes` and `quarry-notes` are ONE
// directory to every reader and every writer. Measured at `18df63d`, where the comparison
// was raw: two `-create-user` runs in different projects both succeeded, both owners held
// `RoleOwner`, and the second read AND wrote the first's entries. The RAW names are what
// the refusal reports, because the operator typed one of them and the journal holds the
// other; the folded form is printed beside them as the reason.
//
// ⚠ SO THIS IS A GUARD AT ONE PATH, NOT AN INVARIANT, AND SAYING WHICH IS THE POINT.
// Everything it does NOT see, enumerated rather than gestured at — a guard's description
// has to be as wide as its body:
//
//   - **Scope DIRECTORIES that already exist under the store root.** This reads
//     `m.Scopes`, the JOURNAL's scopes. The machine-token world's scopes are not journal
//     rows at all: `tokenfile.Source.storeDirs` enumerates the store root's
//     subdirectories, and both worlds are narrowed against the one `s.StoreRoot`. On
//     first use the journal is EMPTY, so this passes unconditionally while the store root
//     may already hold every existing tenant's directory. That is the hole that is live
//     in every deployment, and it is the one the other three are not. `cmd/cairn-server`
//     — which knows the store root, where this package does not — warns about it at
//     `-create-user`; see `warnScopesThatAlreadyExistOnDisk`. A warning rather than a
//     refusal because an existing directory is ALSO the ordinary sequence (seed the
//     store, then provision the person who owns it), and nothing on disk distinguishes
//     the two.
//   - A `scope-renamed` or `scope-moved` event, neither of which exists yet.
//   - A hand-edited journal.
//   - A CONCURRENT `-create-user`: `ProvisionUser` reads the model OUTSIDE the `flock`
//     that `Append` takes, so two runs can each read a journal without the name and both
//     pass here. `Append`'s own re-read under the lock is what still refuses the duplicate
//     (provider, subject) pair, but it carries no rule about names across projects — that
//     rule lives only here, and here is not under the lock.
//
// It is placed here rather than in `apply` because `apply` states the MODEL's rule, and
// widening that rule would make the model assert a property of a reader it deliberately
// knows nothing about — while covering only `scope-created`, leaving a rule that reads as
// global and is not.
//
// **CLOSING CONDITION:** it stops being needed when a scope's bytes are addressed by its
// id rather than by its display name, at which point two scopes may share a name and the
// reader cannot confuse them.
func checkScopeNamesAreFree(m Model, names []string) error {
	for _, name := range names {
		folded := store.NormalizeRef(name)
		for _, sc := range m.Scopes {
			if store.NormalizeRef(sc.DisplayName) == folded {
				return fmt.Errorf(
					"%w: %q folds to %q, which is scope %s (%q) in project %s. A display name is the DIRECTORY name a reader narrows on, and it narrows on the FOLDED name — so two scopes whose names fold alike resolve to one directory and each project's members could read the other's entries",
					ErrScopeNameTaken, name, folded, sc.ID, sc.DisplayName, sc.ProjectID)
			}
		}
	}
	return nil
}
