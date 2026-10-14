package control

import (
	"bytes"
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/authz"
)

func journalBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the journal: %v", err)
	}
	return data
}

// refusedAndWroteNothing asserts a `CreateScope` refusal carries `sentinel` AND `fragment`
// — a piece of THAT guard's own message, so a neighbouring refusal cannot satisfy it — and
// that the journal file is byte-identical afterwards.
func refusedAndWroteNothing(t *testing.T, store *FileStore, path string, req NewScope, sentinel error, fragment string) {
	t.Helper()
	before := journalBytes(t, path)
	_, err := CreateScope(context.Background(), store, req)
	if err == nil {
		t.Fatalf("CreateScope(%q in %s) was ACCEPTED", req.Name, req.ProjectID)
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("CreateScope(%q in %s) was refused, but not as %v: %v", req.Name, req.ProjectID, sentinel, err)
	}
	if !strings.Contains(err.Error(), fragment) {
		t.Fatalf("CreateScope(%q) was refused by a DIFFERENT guard — its message lacks %q: %v", req.Name, fragment, err)
	}
	if after := journalBytes(t, path); !bytes.Equal(before, after) {
		t.Fatalf("a refused CreateScope(%q) changed the journal:\n%s", req.Name, after[len(before):])
	}
}

// TestCreatingAScopeInAnExistingProjectIsOneRecordTheOwnerReaches is the end-to-end claim,
// read back through a SECOND `FileStore` so nothing is answered from this process's memory.
//
// RED at the base commit by construction — `CreateScope` does not exist there, so the file
// does not compile. The behavioural half is the authorization check: an implementation that
// appended a record no resolver honoured fails it.
func TestCreatingAScopeInAnExistingProjectIsOneRecordTheOwnerReaches(t *testing.T) {
	store, path, owner := aProvisionedOwner(t)
	if owner.Epoch != 4 {
		t.Fatalf("the fixture's epoch is %d, not the 4 the literal below is written against", owner.Epoch)
	}

	got, err := CreateScope(context.Background(), store, NewScope{
		ProjectID: owner.Project, Name: "Quarry_Inbox", At: provisionClock,
	})
	if err != nil {
		t.Fatalf("creating a scope in an existing project: %v", err)
	}
	// A LITERAL: one event after a four-event fixture. A path that also wrote a project or
	// a membership would move it.
	if got.Epoch != 5 {
		t.Fatalf("epoch = %d after one scope-created over a four-event journal", got.Epoch)
	}
	if got.Project != owner.Project || got.Scope.Name != "Quarry_Inbox" || !MustPrefix(got.Scope.ID, PrefixScope) {
		t.Fatalf("the result does not describe what was asked for: %+v", got)
	}

	reread, err := OpenFileStore(path)
	if err != nil {
		t.Fatalf("re-opening the journal: %v", err)
	}
	m, err := reread.Model(context.Background())
	if err != nil {
		t.Fatalf("replaying: %v", err)
	}
	sc, held := m.Scopes[got.Scope.ID]
	if !held {
		t.Fatalf("the journal holds no scope %s", got.Scope.ID)
	}
	// The RAW spelling is what is stored; the fold is a comparison, never a rewrite.
	if sc.DisplayName != "Quarry_Inbox" || sc.ProjectID != owner.Project {
		t.Fatalf("the stored scope is %+v", sc)
	}
	if len(m.Projects) != 1 || len(m.Users) != 1 {
		t.Fatalf("creating a scope created something else too: %d project(s), %d user(s)", len(m.Projects), len(m.Users))
	}

	principal, _ := m.PrincipalFor(KindUser, owner.User)
	auth := Resolve(m, principal)
	for _, verb := range AllVerbs {
		if !auth.Allows(got.Scope.ID, verb) {
			t.Fatalf("the project's owner cannot %s the scope just created in it", verb)
		}
	}
	// The NAME projection, by the folded spelling a client would type.
	visible := auth.VisibleScopes(VerbWrite)
	if !visible.Allows("quarry-inbox") {
		t.Fatal("the new scope is authorised by id and absent from the name projection a reader narrows by")
	}
	if visible.Allows("a-scope-nobody-created") {
		t.Fatal("the visible set answers yes to a name no scope carries, so the line above proved nothing")
	}
}

// TestANewScopeReachesItsProjectsMembersAndNoNarrowedCredential pins what creating a scope
// CHANGES for credentials that already exist — the operator-facing consequence the command
// prints. Its name says "members"; the GRANTEE arm is what makes it as wide as the claim.
//
// 🔴 THE GRANTEE ARM IS THE ONE THE FIRST DRAFT OF THIS PATH GOT WRONG. The command, the doc
// comment and both READMEs said the scope reaches "every member of the project"; `Resolve`'s
// second source expands every live grant whose OBJECT is the project over `m.ScopesIn` at
// resolve time, so a NON-member holding a project grant — a team-link redeemer, a share-flow
// grantee — gains the new scope at the grant's verbs. The grant here is recorded BEFORE the
// scope exists, which is the shape that makes it a widening nobody re-approved.
//
// ⚠ AN INVARIANT GUARD, NOT REGRESSION COVERAGE: no line of `CreateScope` decides this; it
// is `Resolve` and `Narrow` behaving as they did, measured over the state this path makes.
func TestANewScopeReachesItsProjectsMembersAndNoNarrowedCredential(t *testing.T) {
	store, _, owner := aProvisionedOwner(t)
	outsider := aSecondUser(t, store)
	old := owner.Scopes[0].ID
	wide := issueTo(t, store, KindUser, owner.User, nil)
	narrowedToOld := issueTo(t, store, KindUser, owner.User, []ID{old})
	foreign := issueTo(t, store, KindUser, outsider.User, nil)

	// A third user in their OWN project, holding a READ grant whose object is the owner's
	// PROJECT — the shape a reader team link and a project share both record.
	granteeReq := aUser("ledger-notes")
	granteeReq.Subject, granteeReq.Email, granteeReq.ProjectName = "subject-0003", "ash@notes.example.test", "ledger"
	grantee, err := ProvisionUser(context.Background(), store, granteeReq)
	if err != nil {
		t.Fatalf("provisioning the grantee: %v", err)
	}
	if _, err := store.Append(context.Background(), Event{
		Kind: EventGranted, At: provisionClock, GrantID: "grt_projectreader",
		SubjectKind: KindUser, SubjectID: grantee.User,
		ObjectKind: ObjectProject, ObjectID: owner.Project, Verbs: NewVerbSet(VerbRead),
	}); err != nil {
		t.Fatalf("granting the project: %v", err)
	}
	granted := issueTo(t, store, KindUser, grantee.User, nil)

	// An hour AFTER the grant: the grant predates the scope, which is the case that makes the
	// expansion a widening nobody re-approved.
	made, err := CreateScope(context.Background(), store, NewScope{
		ProjectID: owner.Project, Name: "quarry-inbox", At: provisionClock.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("creating the scope: %v", err)
	}
	inbox := made.Scope.ID

	readWrite := NewVerbSet(VerbRead, VerbWrite)
	fresh, err := IssueCredential(context.Background(), store, NewCredential{
		SubjectKind: KindUser, SubjectID: owner.User, Label: "inbox agent",
		NarrowedScopes: []ID{inbox}, NarrowedVerbs: &readWrite, At: issueClock,
	})
	if err != nil {
		t.Fatalf("issuing a credential narrowed to the new scope: %v", err)
	}

	m, err := store.Model(context.Background())
	if err != nil {
		t.Fatalf("reading the journal: %v", err)
	}
	reach := func(tok string) Authorization {
		t.Helper()
		_, auth, err := Authenticate(m, tok)
		if err != nil {
			t.Fatalf("a fixture credential does not authenticate: %v", err)
		}
		return auth
	}

	if a := reach(wide.Token()); !a.Allows(inbox, VerbRead) || !a.Allows(inbox, VerbAdmin) {
		t.Error("an UN-narrowed credential of a project member does not reach the project's new scope")
	}
	if a := reach(narrowedToOld.Token()); a.Allows(inbox, VerbRead) || !a.Allows(old, VerbRead) {
		t.Error("a credential narrowed to ANOTHER scope before this one existed now reaches it — " +
			"or lost the scope it was narrowed to, which would make this assertion vacuous")
	}
	if a := reach(foreign.Token()); a.Allows(inbox, VerbRead) {
		t.Error("a user in a DIFFERENT project, holding no grant, reaches the new scope")
	}
	if a := reach(granted.Token()); !a.Allows(inbox, VerbRead) || a.Allows(inbox, VerbWrite) {
		t.Errorf("a NON-member holding a READ grant on the PROJECT has read=%v write=%v on its new "+
			"scope; want read=true write=false — project grants are expanded over the project's "+
			"scopes at resolve time, so a new scope reaches every project grantee at the grant's verbs",
			a.Allows(inbox, VerbRead), a.Allows(inbox, VerbWrite))
	}
	a := reach(fresh.Token())
	if !a.Allows(inbox, VerbRead) || !a.Allows(inbox, VerbWrite) {
		t.Error("a credential narrowed to the new scope with read,write cannot read and write it")
	}
	if a.Allows(inbox, VerbAdmin) || a.Allows(old, VerbRead) {
		t.Error("a credential narrowed to the new scope with read,write carries admin, or reaches a sibling scope")
	}
}

// TestCreateScopeRefusesAProjectTheJournalDoesNotHold — by the sentinel a caller branches
// on. `apply` refuses the same event in its own words, so asserting only "an error" would
// stay green with this path's check deleted.
func TestCreateScopeRefusesAProjectTheJournalDoesNotHold(t *testing.T) {
	store, path, owner := aProvisionedOwner(t)
	for _, project := range []ID{"prj_notinthisjournal", ID(owner.User), "quarry", ""} {
		refusedAndWroteNothing(t, store, path,
			NewScope{ProjectID: project, Name: "quarry-inbox", At: provisionClock},
			ErrNoSuchProject, "a scope names its owning project by id")
	}
}

// TestCreateScopeRefusesANameOutsideThePathComponentClass covers the CLASS clause with
// names that all fold to something — so only that clause can refuse them — plus the empty
// name, whose refusal must be this clause's and not the fold's.
func TestCreateScopeRefusesANameOutsideThePathComponentClass(t *testing.T) {
	store, path, owner := aProvisionedOwner(t)
	for _, name := range []string{
		"quarry.inbox", "..", "quarry inbox", " quarry-inbox", "quarry/inbox",
		"quarry-inbox,quarry-outbox", "quarry-inbóx", "",
	} {
		refusedAndWroteNothing(t, store, path,
			NewScope{ProjectID: owner.Project, Name: name, At: provisionClock},
			ErrScopeNameUnusable, "carries a character outside")
	}
	// The positive control: every character the class allows, in one name. Without it a
	// guard that refused EVERYTHING satisfies the loop above.
	if _, err := CreateScope(context.Background(), store, NewScope{
		ProjectID: owner.Project, Name: "Quarry_Inbox-2", At: provisionClock,
	}); err != nil {
		t.Fatalf("a name inside the class was refused: %v", err)
	}
}

// TestCreateScopeRefusesANameThatFoldsToNothing covers the FOLD clause with names the class
// accepts — the reason it is a second clause rather than a spelling of the first.
func TestCreateScopeRefusesANameThatFoldsToNothing(t *testing.T) {
	store, path, owner := aProvisionedOwner(t)
	for _, name := range []string{"-", "___", "-_-"} {
		if !scopeDirectoryName.MatchString(name) {
			t.Fatalf("%q is outside the class, so the class clause would refuse it and this test would not reach the fold", name)
		}
		refusedAndWroteNothing(t, store, path,
			NewScope{ProjectID: owner.Project, Name: name, At: provisionClock},
			ErrScopeNameUnusable, "folds to nothing")
	}
}

// TestCreateScopeRefusesANameTheJournalAlreadyFoldsTo is the reuse claim: the name is
// checked by `checkScopeNamesAreFree`, FOLDED and across the WHOLE journal.
//
// Both arms are shapes `apply` ACCEPTS — a different spelling in the same project (its rule
// is a raw `==`), and the same spelling in a different project (its rule is per project) —
// so each is refused only if this path called the function.
func TestCreateScopeRefusesANameTheJournalAlreadyFoldsTo(t *testing.T) {
	store, path, owner := aProvisionedOwner(t) // holds `quarry-notes`
	other := aSecondUser(t, store)             // holds `harbour-notes`, in another project

	refusedAndWroteNothing(t, store, path,
		NewScope{ProjectID: owner.Project, Name: "Quarry_Notes", At: provisionClock},
		ErrScopeNameTaken, `folds to "quarry-notes"`)
	refusedAndWroteNothing(t, store, path,
		NewScope{ProjectID: other.Project, Name: "quarry-notes", At: provisionClock},
		ErrScopeNameTaken, `folds to "quarry-notes"`)
}

// TestTheScopeNameClassIsThePathComponentClass is a SEAM GUARD, on
// `TestTheMintedWidthAgreesWithTheTokenFileFloor`'s pattern: this package does not import
// `internal/authz`, so nothing in the production graph makes the class a new scope is held
// to equal the class the pod's URL router and the token-file parser hold a scope name to.
func TestTheScopeNameClassIsThePathComponentClass(t *testing.T) {
	if got, want := scopeDirectoryName.String(), authz.SafePathComponent.String(); got != want {
		t.Fatalf("control.scopeDirectoryName is %s and authz.SafePathComponent is %s. A scope "+
			"created inside one and outside the other is a scope no URL can name, or a name the "+
			"pod accepts and this path refuses; move them together", got, want)
	}
}

// TestEveryWriterOfScopeCreatedChecksTheNameIsFree is the LEDGER behind
// `checkScopeNamesAreFree`'s claim that the fold "lives only here": every function in
// non-test code that USES the identifier `EventScopeCreated`, by file and name, failing when
// the set GROWS or SHRINKS — and, for each one that writes a journal, that the same function
// calls the check.
//
// 🔴 ANY USE OF THE IDENTIFIER, NOT THE `Kind: EventScopeCreated` LITERAL, BECAUSE THE FIRST
// DRAFT MATCHED ONLY THAT SHAPE AND AN ASSIGNMENT (`e.Kind = …`) OR A KIND PASSED AS AN
// ARGUMENT WALKED STRAIGHT PAST IT. The exemptions are EVERY non-function declaration in
// journal.go — which covers the kind's own `const` block and `AllEventKinds`, but is wider
// than them: a package-level `var`, `type` or `const` IN journal.go that used the kind would
// be skipped too — and journal.go's `case` lists, where the kind is dispatched on rather
// than built. A package-level declaration in any OTHER file is reported as `(package-level)`.
// ⚠ What it still cannot see: the string `"scope-created"` spelled as a literal `EventKind`,
// and a hand-written JSON line — neither uses the identifier.
//
// ⚠ STRUCTURAL, SO IT TYPE-CHECKS PAST A WRONG ARGUMENT: a writer calling the check with
// `nil` satisfies it. The behavioural halves are
// `TestCreateScopeRefusesANameTheJournalAlreadyFoldsTo` and
// `TestAScopeNameThatFOLDSOntoOneAlreadyHeldIsRefused`.
func TestEveryWriterOfScopeCreatedChecksTheNameIsFree(t *testing.T) {
	// writer → whether it must call the check.
	want := map[string]bool{
		"internal/control/provision.go:ProvisionUser":  true,
		"internal/control/scope_create.go:CreateScope": true,
		// A PROJECTION, not a journal writer: it rebuilds events from the store root's
		// directory listing on every refresh and appends them nowhere.
		"internal/control/tokenfile/source.go:Events": false,
	}
	assertWriterLedger(t, "EventScopeCreated", "checkScopeNamesAreFree", want)
}

// assertWriterLedger is the walker behind every event-kind WRITER LEDGER here: the set of
// non-test functions that use the identifier `kind` (outside journal.go's declaration and
// `case` lists) must equal `want`'s keys, in both directions, and each writer `want` marks
// true must call `check` in the same function. Shared rather than copied, so a fix to what
// counts as a use reaches every ledger at once.
func assertWriterLedger(t *testing.T, kind, check string, want map[string]bool) {
	t.Helper()
	root := filepath.Join("..", "..")
	got := map[string]bool{}
	scanned := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// `.claude/` holds nested CHECKOUTS of this repository, each of which would
			// report every writer a second time.
			if name := d.Name(); path != root && strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		scanned++
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		isJournal := rel == "internal/control/journal.go"
		for _, decl := range file.Decls {
			owner := "(package-level)"
			if fn, isFunc := decl.(*ast.FuncDecl); isFunc {
				owner = fn.Name.Name
			} else if isJournal {
				// EVERY non-function declaration in journal.go, not only the kind's own
				// `const` declaration and `AllEventKinds` (the declaration this ledger is
				// about, not a use of it) — any other package-level declaration there is
				// skipped by this same branch.
				continue
			}
			builds, checks := false, false
			ast.Inspect(decl, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.CaseClause:
					// `validate`'s and `apply`'s arms DISPATCH on the kind; they build
					// nothing. Exempt in journal.go only, and only the case LIST: the arm's
					// body is still walked.
					if isJournal {
						for _, stmt := range node.Body {
							ast.Inspect(stmt, func(inner ast.Node) bool {
								if e, ok := inner.(ast.Expr); ok && identName(e) == kind {
									builds = true
								}
								return true
							})
						}
						return false
					}
				case *ast.Ident:
					if node.Name == kind {
						builds = true
					}
				case *ast.CallExpr:
					if identName(node.Fun) == check {
						checks = true
					}
				}
				return true
			})
			if builds {
				got[rel+":"+owner] = checks
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the tree: %v", err)
	}
	if scanned < 50 {
		t.Fatalf("only %d non-test Go files were scanned from %s — the walk is wired to the wrong root", scanned, root)
	}

	names := func(m map[string]bool) []string {
		out := make([]string, 0, len(m))
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	if g, w := strings.Join(names(got), "\n"), strings.Join(names(want), "\n"); g != w {
		t.Fatalf("the functions that build a %s event are\n%s\nand this ledger says\n%s\n"+
			"A new writer must call %s — that rule is NOT in `apply` — and then be added here; a "+
			"missing one means the ledger is describing code that is gone", kind, g, w, check)
	}
	for writer, must := range want {
		if must && !got[writer] {
			t.Errorf("%s builds a %s event and does not call %s, the rule `apply` does not carry",
				writer, kind, check)
		}
	}
}

// identName is the rightmost identifier of `X` or `pkg.X`, and "" for anything else.
func identName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return x.Sel.Name
	}
	return ""
}
