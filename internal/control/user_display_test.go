package control

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/authz"
)

// Every value below is a literal, and each user's three candidate displays are pairwise
// distinct, so a `displayOf` that returned the wrong one of them cannot coincide with the right.

// TestDisplayOfPrefersTheDisplayNameThenTheEmailThenProviderSubject pins the precedence over
// all four reachable shapes, through `Replay` rather than a hand-built model.
func TestDisplayOfPrefersTheDisplayNameThenTheEmailThenProviderSubject(t *testing.T) {
	m, err := Replay([]Event{
		{Kind: EventUserCreated, At: at(1), UserID: "usr_all", Provider: "notes-idp", Subject: "subject-0001",
			Email: "rowan@notes.example.test", DisplayName: "octocat-example"},
		{Kind: EventUserCreated, At: at(2), UserID: "usr_mail", Provider: "notes-idp", Subject: "subject-0002",
			Email: "wren@notes.example.test"},
		{Kind: EventUserCreated, At: at(3), UserID: "usr_bare", Provider: "notes-idp", Subject: "subject-0003"},
		{Kind: EventUserCreated, At: at(4), UserID: "usr_nameonly", Provider: "notes-idp", Subject: "subject-0004",
			DisplayName: "hubot-example"},
		// A rename AFTER creation wins over the email it was created with.
		{Kind: EventUserRenamed, At: at(5), UserID: "usr_mail", DisplayName: "wren-example"},
	})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	for id, want := range map[ID]string{
		"usr_all":      "octocat-example",
		"usr_mail":     "wren-example",
		"usr_bare":     "notes-idp:subject-0003",
		"usr_nameonly": "hubot-example",
	} {
		p, ok := m.PrincipalFor(KindUser, id)
		if !ok || p.Display != want {
			t.Errorf("PrincipalFor(user, %s) = %q, %v; want %q", id, p.Display, ok, want)
		}
	}
	// The email is not lost — it is still on the row, for invites.
	if got := m.Users["usr_mail"].Email; got != "wren@notes.example.test" {
		t.Errorf("a rename changed the stored email to %q", got)
	}
	// And a SECOND rename replaces the first: the latest event wins.
	m2, err := Replay([]Event{
		{Kind: EventUserCreated, At: at(1), UserID: "usr_a", Provider: "notes-idp", Subject: "subject-0001",
			DisplayName: "first-name"},
		{Kind: EventUserRenamed, At: at(2), UserID: "usr_a", DisplayName: "second-name"},
	})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if p, _ := m2.PrincipalFor(KindUser, "usr_a"); p.Display != "second-name" {
		t.Errorf("after two renames the display is %q, want the later one", p.Display)
	}
}

// TestTheDisplayNameShapeRefusesEverySpoofingSpelling: every refusal is the shape sentinel, on
// BOTH kinds that write a display name, and every acceptance is accepted on both. The refused
// set is the spoofing argument in `user_display.go`, one row per excluded shape.
func TestTheDisplayNameShapeRefusesEverySpoofingSpelling(t *testing.T) {
	accepted := []string{
		"a", "octocat-example", "A.b_c-d", "0lead-digit",
		strings.Repeat("x", UserDisplayNameMax), // exactly at the cap
	}
	refused := map[string]string{
		"notes-idp:subject-0001":  "the <provider>:<subject> fallback (under the cap, so the alphabet refuses it)",
		"wren@notes.example.test": "an email display",
		"octocat (usr_other)":     "Principal.String's (<display>) suffix",
		"two words":               "a space, which the audit line rewrites to _",
		"line\nbreak":             "a newline",
		"tab\there":               "a control character",
		"rtl\u202eoverride":       "a bidi override (format character)",
		"zero\u200bwidth":         "a zero-width space",
		"caf\u00e9":               "non-ASCII, which the audit line rewrites to ?",
		"-leading":                "a leading hyphen",
		".leading":                "a leading dot",
		strings.Repeat("x", UserDisplayNameMax+1): "one past the cap",
	}
	for _, name := range accepted {
		for _, e := range displayEvents(name) {
			if err := e.validate(); err != nil {
				t.Errorf("%s with display_name %q refused: %v", e.Kind, name, err)
			}
		}
	}
	for name, why := range refused {
		for _, e := range displayEvents(name) {
			if err := e.validate(); !errors.Is(err, ErrUserDisplayNameShape) {
				t.Errorf("%s with display_name %q (%s) = %v, want ErrUserDisplayNameShape", e.Kind, name, why, err)
			}
		}
		if err := ValidUserDisplayName(name); !errors.Is(err, ErrUserDisplayNameShape) {
			t.Errorf("ValidUserDisplayName(%q) (%s) = %v, want ErrUserDisplayNameShape", name, why, err)
		}
	}
	// A rename with NO name is refused as a missing field; a creation with none is the old shape.
	if err := (Event{Kind: EventUserRenamed, At: at(1), UserID: "usr_a"}).validate(); err == nil ||
		!strings.Contains(err.Error(), "display_name is required") {
		t.Errorf("a user-renamed with no display_name = %v, want display_name is required", err)
	}
	if err := (Event{Kind: EventUserCreated, At: at(1), UserID: "usr_a", Provider: "p", Subject: "s"}).validate(); err != nil {
		t.Errorf("a user-created with no display_name — every journal before the field — was refused: %v", err)
	}
}

func displayEvents(name string) []Event {
	return []Event{
		{Kind: EventUserCreated, At: at(1), UserID: "usr_a", Provider: "notes-idp", Subject: "subject-0001", DisplayName: name},
		{Kind: EventUserRenamed, At: at(1), UserID: "usr_a", DisplayName: name},
	}
}

// TestUserDisplayNameMaxIsTheAuditFieldCap pins the cap to the field it mirrors: if the audit
// line's identity cap moved, a display name could be truncated there into another one.
func TestUserDisplayNameMaxIsTheAuditFieldCap(t *testing.T) {
	if UserDisplayNameMax != authz.MaxIdentityChars {
		t.Fatalf("UserDisplayNameMax = %d, authz.MaxIdentityChars = %d — they must move together",
			UserDisplayNameMax, authz.MaxIdentityChars)
	}
}

// TestTwoUsersCannotRenderTheSameDisplay: the uniqueness rule, at `apply`, against each of the
// three things another user can render as — and NOT against the user themself.
func TestTwoUsersCannotRenderTheSameDisplay(t *testing.T) {
	base := []Event{
		{Kind: EventUserCreated, At: at(1), UserID: "usr_named", Provider: "notes-idp", Subject: "subject-0001",
			Email: "rowan@notes.example.test", DisplayName: "octocat-example"},
		// An email WITHOUT `@` — free text the creation path never validated — is the one email
		// a display name can spell, so it is the case that proves the comparison reads emails.
		{Kind: EventUserCreated, At: at(2), UserID: "usr_oddmail", Provider: "notes-idp", Subject: "subject-0002",
			Email: "wren-example"},
		{Kind: EventUserCreated, At: at(3), UserID: "usr_other", Provider: "notes-idp", Subject: "subject-0003"},
	}
	for _, tc := range []struct {
		name string
		next Event
	}{
		{"a rename onto another user's display name", Event{Kind: EventUserRenamed, At: at(9), UserID: "usr_other", DisplayName: "octocat-example"}},
		{"…differing only in case", Event{Kind: EventUserRenamed, At: at(9), UserID: "usr_other", DisplayName: "OctoCat-Example"}},
		{"a rename onto another user's rendered email", Event{Kind: EventUserRenamed, At: at(9), UserID: "usr_other", DisplayName: "Wren-Example"}},
		{"a creation carrying a taken name", Event{Kind: EventUserCreated, At: at(9), UserID: "usr_new", Provider: "notes-idp", Subject: "subject-0009", DisplayName: "octocat-example"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Replay(append(append([]Event(nil), base...), tc.next))
			if !errors.Is(err, ErrUserDisplayNameTaken) {
				t.Fatalf("replay = %v, want ErrUserDisplayNameTaken", err)
			}
		})
	}
	// Re-asserting one's OWN name, in another case, is not a clash.
	m, err := Replay(append(append([]Event(nil), base...),
		Event{Kind: EventUserRenamed, At: at(9), UserID: "usr_named", DisplayName: "Octocat-Example"}))
	if err != nil {
		t.Fatalf("a user re-asserting their own name was refused: %v", err)
	}
	if p, _ := m.PrincipalFor(KindUser, "usr_named"); p.Display != "Octocat-Example" {
		t.Errorf("display %q after a self-rename", p.Display)
	}
	// And a rename naming a user the model does not hold is refused, not created.
	if _, err := Replay(append(append([]Event(nil), base...),
		Event{Kind: EventUserRenamed, At: at(9), UserID: "usr_ghost", DisplayName: "ghost-example"})); err == nil ||
		!strings.Contains(err.Error(), "does not exist") {
		t.Errorf("a rename of an unknown user = %v, want a does-not-exist refusal", err)
	}
}

// TestADisplayNameOnceHeldIsNotReissuedToAnotherUser: a bullet's ACTOR and an audit line's
// `identity=` are STORED text, so a name a user has given up still attributes everything
// written under it. Handing it to a different user later would make one string mean two
// people across time — the hazard the uniqueness rule exists for, arriving by reuse.
func TestADisplayNameOnceHeldIsNotReissuedToAnotherUser(t *testing.T) {
	base := []Event{
		// Mixed case on purpose: an all-lowercase fixture cannot see a history keyed WITHOUT the
		// case fold, since the lowercase spelling would then be the key either way.
		{Kind: EventUserCreated, At: at(1), UserID: "usr_first", Provider: "notes-idp", Subject: "subject-0001",
			DisplayName: "Octocat-Example"},
		{Kind: EventUserRenamed, At: at(2), UserID: "usr_first", DisplayName: "octocat-renamed"},
		{Kind: EventUserRenamed, At: at(3), UserID: "usr_first", DisplayName: "octocat-current"},
		{Kind: EventUserCreated, At: at(4), UserID: "usr_second", Provider: "notes-idp", Subject: "subject-0002"},
	}
	for _, tc := range []struct {
		name string
		next Event
	}{
		{"a rename onto a name another user gave up", Event{Kind: EventUserRenamed, At: at(9), UserID: "usr_second", DisplayName: "octocat-example"}},
		{"…one held only by an earlier RENAME", Event{Kind: EventUserRenamed, At: at(9), UserID: "usr_second", DisplayName: "octocat-renamed"}},
		{"…differing only in case", Event{Kind: EventUserRenamed, At: at(9), UserID: "usr_second", DisplayName: "OCTOCAT-EXAMPLE"}},
		{"a creation carrying a name another user gave up", Event{Kind: EventUserCreated, At: at(9), UserID: "usr_third", Provider: "notes-idp", Subject: "subject-0003", DisplayName: "Octocat-Example"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Replay(append(append([]Event(nil), base...), tc.next))
			if !errors.Is(err, ErrUserDisplayNameTaken) {
				t.Fatalf("replay = %v, want ErrUserDisplayNameTaken", err)
			}
		})
	}
	// The user who HELD it may take it back: their own history is one person, not two.
	m, err := Replay(append(append([]Event(nil), base...),
		Event{Kind: EventUserRenamed, At: at(9), UserID: "usr_first", DisplayName: "Octocat-Example"}))
	if err != nil {
		t.Fatalf("a user reclaiming their own former name was refused: %v", err)
	}
	if p, _ := m.PrincipalFor(KindUser, "usr_first"); p.Display != "Octocat-Example" {
		t.Errorf("display %q after reclaiming a former name", p.Display)
	}
}

// TestAReleasedEmailDisplayIsNotReissued: the history is of what a user RENDERED, not only of
// display names. A user shown as an `@`-less free-text email (and, through the audit line's
// space rewrite, as that email with `_` for each space) attributed everything written before
// their rename under that string, so it is held for them exactly as a released display name is.
func TestAReleasedEmailDisplayIsNotReissued(t *testing.T) {
	for _, tc := range []struct{ email, taken string }{
		{"wren_example", "WREN_example"},
		{"wren example", "Wren_Example"},
	} {
		t.Run(tc.email, func(t *testing.T) {
			_, err := Replay([]Event{
				{Kind: EventUserCreated, At: at(1), UserID: "usr_first", Provider: "notes-idp", Subject: "subject-0001", Email: tc.email},
				{Kind: EventUserRenamed, At: at(2), UserID: "usr_first", DisplayName: "wren"},
				{Kind: EventUserCreated, At: at(3), UserID: "usr_second", Provider: "notes-idp", Subject: "subject-0002"},
				{Kind: EventUserRenamed, At: at(4), UserID: "usr_second", DisplayName: tc.taken},
			})
			if !errors.Is(err, ErrUserDisplayNameTaken) {
				t.Fatalf("replay = %v, want ErrUserDisplayNameTaken", err)
			}
		})
	}
}

// TestTheHeldNameHistorySurvivesSeparateAppends drives the history rule through a real
// `FileStore`, one `Append` per event. The last write goes to `Append` DIRECTLY, past
// `RenameUser`'s early refusal (which reads a fresh replay and so cannot see the defect), so
// what refuses it is `apply` on `Append`'s clone — a clone that dropped the history would
// accept it.
func TestTheHeldNameHistorySurvivesSeparateAppends(t *testing.T) {
	store, _ := journalAt(t)
	owner, err := ProvisionUser(context.Background(), store, aUser("quarry-notes"))
	if err != nil {
		t.Fatalf("provisioning: %v", err)
	}
	second := aSecondUser(t, store)
	for _, name := range []string{"octocat-example", "octocat-renamed"} {
		if _, err := RenameUser(context.Background(), store, NewUserDisplayName{UserID: owner.User, DisplayName: name}); err != nil {
			t.Fatalf("RenameUser(%s): %v", name, err)
		}
	}
	if _, err := store.Append(context.Background(), Event{Kind: EventUserRenamed, UserID: second.User,
		DisplayName: "octocat-example"}); !errors.Is(err, ErrUserDisplayNameTaken) {
		t.Fatalf("reissuing a released name through Append = %v, want ErrUserDisplayNameTaken", err)
	}
}

// TestADisplayNameCannotEqualAnotherUsersAuditRendering: the audit line writes every space as
// `_`, so an `@`-less free-text email `wren example` is `identity=wren_example` there. A display
// name `wren_example` passes the alphabet and differs from the email as a string — and is the
// same string in the one place an attribution is read after the fact.
func TestADisplayNameCannotEqualAnotherUsersAuditRendering(t *testing.T) {
	base := []Event{
		{Kind: EventUserCreated, At: at(1), UserID: "usr_spaced", Provider: "notes-idp", Subject: "subject-0001",
			Email: "wren example"},
		{Kind: EventUserCreated, At: at(2), UserID: "usr_other", Provider: "notes-idp", Subject: "subject-0002"},
	}
	_, err := Replay(append(append([]Event(nil), base...),
		Event{Kind: EventUserRenamed, At: at(9), UserID: "usr_other", DisplayName: "Wren_Example"}))
	if !errors.Is(err, ErrUserDisplayNameTaken) {
		t.Fatalf("replay = %v, want ErrUserDisplayNameTaken", err)
	}
}

// TestRenameUserWritesOneRecordAndRefusalsWriteNothing drives the library path the command
// uses, against a real `FileStore`, and re-reads from DISK.
func TestRenameUserWritesOneRecordAndRefusalsWriteNothing(t *testing.T) {
	store, path := journalAt(t)
	owner, err := ProvisionUser(context.Background(), store, aUser("quarry-notes"))
	if err != nil {
		t.Fatalf("provisioning: %v", err)
	}
	second := aSecondUser(t, store)

	got, err := RenameUser(context.Background(), store, NewUserDisplayName{UserID: owner.User, DisplayName: "octocat-example"})
	if err != nil {
		t.Fatalf("RenameUser: %v", err)
	}
	if got.Previous != "rowan@notes.example.test" || got.Display != "octocat-example" || got.UserID != owner.User {
		t.Errorf("RenameUser reported %+v", got)
	}
	reopened, err := OpenFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	m, err := reopened.Model(context.Background())
	if err != nil {
		t.Fatalf("re-reading: %v", err)
	}
	if p, _ := m.PrincipalFor(KindUser, owner.User); p.Display != "octocat-example" {
		t.Errorf("after a rename, the journal on disk renders %q", p.Display)
	}
	if got.Epoch != m.Epoch {
		t.Errorf("reported epoch %d, journal epoch %d", got.Epoch, m.Epoch)
	}

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		req  NewUserDisplayName
		want error
	}{
		{"a taken name", NewUserDisplayName{UserID: second.User, DisplayName: "OCTOCAT-EXAMPLE"}, ErrUserDisplayNameTaken},
		{"a spoofing shape", NewUserDisplayName{UserID: second.User, DisplayName: "notes-idp:subject-0001"}, ErrUserDisplayNameShape},
		{"an unknown user", NewUserDisplayName{UserID: "usr_ghost", DisplayName: "ghost-example"}, ErrNoSuchMember},
	} {
		if _, err := RenameUser(context.Background(), store, tc.req); !errors.Is(err, tc.want) {
			t.Errorf("%s: RenameUser = %v, want %v", tc.name, err, tc.want)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("a refused rename changed the journal on disk")
	}
}

// TestProvisionUserCarriesTheDisplayName: `-create-user -display-name` reaches the row.
func TestProvisionUserCarriesTheDisplayName(t *testing.T) {
	store, _ := journalAt(t)
	req := aUser("quarry-notes")
	req.DisplayName = "octocat-example"
	made, err := ProvisionUser(context.Background(), store, req)
	if err != nil {
		t.Fatalf("provisioning: %v", err)
	}
	m, err := store.Model(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := m.PrincipalFor(KindUser, made.User); p.Display != "octocat-example" {
		t.Errorf("a user provisioned with a display name renders %q", p.Display)
	}
	req2 := aUser()
	req2.Subject, req2.Email, req2.ProjectName, req2.DisplayName = "subject-0002", "wren@notes.example.test", "harbour", "bad name"
	if _, err := ProvisionUser(context.Background(), store, req2); !errors.Is(err, ErrUserDisplayNameShape) {
		t.Errorf("provisioning with a refused shape = %v, want ErrUserDisplayNameShape", err)
	}
}
