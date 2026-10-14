package control

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/ZacxDev/cairn/internal/store"
)

// ErrScopeNameUnusable refuses a display name that cannot name a store directory a reader
// would reach.
//
// 🔴 THIS ONE IS NOT AN "EARLY REFUSAL" — NEITHER `Event.validate` NOR `apply` ENFORCES IT,
// for `ErrWouldOrphanProject`'s reason. `validate` asks only that `display_name` is
// non-empty; a class rule added there would govern REPLAY, and a journal already holding a
// scope `-create-user` accepted outside the class would stop loading whole. So the rule is
// a guard at `CreateScope` and nowhere else, and saying so is the point: see
// `checkScopeNameIsADirectoryName` for what it does not cover.
var ErrScopeNameUnusable = errors.New("control: that scope display name cannot name a store directory")

// scopeDirectoryName is the class a scope created by `CreateScope` must be spelled in.
//
// 🔴 IT IS `authz.SafePathComponent`, RESTATED AND PINNED RATHER THAN IMPORTED — the ruling
// `tokenChars` records: this package does not import `internal/authz`, because that would
// put the token FILE FORMAT on the authority model's dependency list. The two expressions
// are compared by `TestTheScopeNameClassIsThePathComponentClass`, a seam guard that imports
// both, so moving one alone is red.
//
// WHY THAT CLASS: a display name is the directory name under the store root AND the name a
// client spells in a URL path, where the pod refuses any component outside this class
// before it reaches the filesystem. It carries no `.` and no `/`, so `..` and a nested path
// are excluded by construction rather than by name.
var scopeDirectoryName = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// checkScopeNameIsADirectoryName refuses a name outside the class, and a name inside it
// that folds to nothing.
//
// 🔴 TWO CLAUSES, AND NEITHER IS A SPELLING OF THE OTHER. The class alone accepts `-` and
// `___`, which `store.NormalizeRef` folds to the EMPTY string — a scope every reader narrows
// to no directory at all. The fold alone accepts `quarry.notes` and `..`, because the fold
// keeps `.`. The empty name fails the class (`+`), so it needs no third clause.
//
// ⚠ A GUARD AT ONE PATH, NOT AN INVARIANT. `ProvisionUser` does NOT apply it: `-create-user
// -scopes` has always accepted any non-empty name, and refusing there would reject command
// lines that work today. Nor does a direct `Store.Append`. And it says nothing about
// LENGTH — a name longer than the filesystem's component limit matches the class.
func checkScopeNameIsADirectoryName(name string) error {
	if !scopeDirectoryName.MatchString(name) {
		return fmt.Errorf(
			"%w: %q is empty or carries a character outside %s. A display name is the DIRECTORY name under the store root and the name a client spells in a URL path, where the pod refuses anything outside that class — so this would be a scope no request could name. The journal is append-only and nothing emits `scope-renamed`: choose a name inside the class",
			ErrScopeNameUnusable, name, scopeDirectoryName)
	}
	if store.NormalizeRef(name) == "" {
		return fmt.Errorf(
			"%w: %q folds to nothing. Every reader and writer resolves a scope's directory by its FOLDED name — lowercase, `_` to `-`, runs collapsed, leading and trailing `-` trimmed — so this would be a scope that reaches no directory. Choose a name with a letter or digit in it",
			ErrScopeNameUnusable, name)
	}
	return nil
}

// NewScope is a request to create ONE scope in a project that already exists.
type NewScope struct {
	// ProjectID names the project that will OWN the scope. Required, and it must already
	// be in the journal: this path creates no project. An ID, not a name — project names
	// are not unique (`ProvisionUser` creates a new project per call, whatever it is called).
	ProjectID ID
	// Name is the display name, stored exactly as given. See
	// `checkScopeNameIsADirectoryName` and `checkScopeNamesAreFree` for the two rules it
	// must pass.
	//
	// ⚠ ONE NAME PER CALL, DELIBERATELY. `checkScopeNamesAreFree` already takes a list, so a
	// batch would cost nothing here; it is one because the operator's remedy for a wrong
	// scope is a hand edit of an append-only file, and a call that can only be wrong about
	// one name is the smaller thing to get wrong.
	Name string
	// Actor is recorded on the event. Optional, for `NewMembership.Actor`'s reason.
	Actor ID
	// At is the event's timestamp. Zero means now.
	At time.Time
}

// ScopeCreated is what `CreateScope` wrote.
type ScopeCreated struct {
	Scope   ProvisionedScope
	Project ID
	// Epoch is the journal epoch after the append — what an operator quotes when asking
	// whether a running pod has picked the change up yet.
	Epoch uint64
}

// CreateScope adds one scope to an existing project.
//
// 🔴 WHY IT EXISTS, STATED AS THE DEFECT IT CLOSES. `ProvisionUser` was the only thing that
// wrote a `scope-created` record, and it writes one only beside a NEW user and a NEW
// project. So a journal's scope set was frozen at provisioning: a project could never gain
// a scope, and a credential could never be narrowed to one that did not exist the day its
// owner was created. The remedy was a hand-appended line — the shape `SetMember` records
// this repository being burned by.
//
// 🔴 IT WIDENS EVERY MEMBER'S AUTHORITY, AND THAT IS WHAT A SCOPE IN A PROJECT MEANS.
// `Resolve` confers a member's role verbs over every scope their project owns, so the
// moment this record is read each member — through any credential NOT narrowed by scope —
// reaches the new scope. A scope-narrowed credential does not: a narrowing is a list of
// scope IDS fixed at issue, and this id did not exist then.
// `TestANewScopeReachesItsProjectsMembersAndNoNarrowedCredential` pins both halves.
//
// 🔴 THE NAME RULES ARE `ProvisionUser`'S OWN FUNCTION, NOT A COPY. `checkScopeNamesAreFree`
// is what refuses a name any scope in the journal already folds to; the fold lives there and
// not in `apply`, so a second writer that skipped it would be the hole that function's
// comment enumerates. `TestEveryWriterOfScopeCreatedChecksTheNameIsFree` is the ledger.
//
// ⚠ WHAT IT DOES NOT DO. It creates no directory — this package holds no path. It issues
// no credential and writes no grant. And it inherits `checkScopeNamesAreFree`'s
// check-then-act window: the model is read OUTSIDE the `flock` `Append` takes, so two
// concurrent calls naming one fold in two DIFFERENT projects can both pass. (In ONE project
// `apply`'s raw-name rule still refuses an identical spelling under the lock.)
func CreateScope(ctx context.Context, s Store, req NewScope) (ScopeCreated, error) {
	at := req.At
	if at.IsZero() {
		// For `ProvisionUser`'s reason: `Store` is an interface, a backend is not required
		// to stamp, and `Event.validate` refuses a zero time from every writer.
		at = time.Now().UTC()
	}

	current, err := s.Model(ctx)
	if err != nil {
		return ScopeCreated{}, fmt.Errorf("reading the control journal: %w", err)
	}
	if _, known := current.Projects[req.ProjectID]; !known {
		// An early refusal with a sentinel a caller can branch on; `apply` refuses the same
		// event under the lock ("names project …, which does not exist").
		return ScopeCreated{}, fmt.Errorf(
			"%w: %s — a scope names its owning project by id, and `apply` refuses a scope-created naming a project that does not exist",
			ErrNoSuchProject, req.ProjectID)
	}
	if err := checkScopeNameIsADirectoryName(req.Name); err != nil {
		return ScopeCreated{}, err
	}
	if err := checkScopeNamesAreFree(current, []string{req.Name}); err != nil {
		return ScopeCreated{}, err
	}

	// `NewID`, not `DerivedID`, for the reason `ProvisionUser` gives: a journal has an id
	// column, and a re-run is refused by the name rule above rather than by an id collision.
	scopeID, err := NewID(PrefixScope)
	if err != nil {
		return ScopeCreated{}, err
	}
	after, err := s.Append(ctx, Event{
		Kind: EventScopeCreated, At: at, Actor: req.Actor,
		ScopeID: scopeID, DisplayName: req.Name, ProjectID: req.ProjectID,
	})
	if err != nil {
		return ScopeCreated{}, err
	}
	return ScopeCreated{
		Scope:   ProvisionedScope{Name: req.Name, ID: scopeID},
		Project: req.ProjectID,
		Epoch:   after.Epoch,
	}, nil
}
