package control

import (
	"strings"
	"testing"
)

// TestAJournalWrittenBeforeDisplayNamesRendersTheSameDisplays pins that a journal carrying no
// `display_name` on any user replays to EXACTLY the displays it rendered before the field
// existed: the email when one was written, `<provider>:<subject>` when not.
//
// ⚠ AN INVARIANT GUARD, LABELLED AS ONE: it is green on the commit before `user-renamed`
// existed, by design — it uses only `ReadEvents`, `Replay` and `PrincipalFor`, so it compiles
// there. What it guards is that the new precedence did not move an old journal's answer. The
// lines are literal JSON in the shape `-create-user` wrote, not built from `Event`, so a
// change to the encoder cannot make the fixture agree with itself.
func TestAJournalWrittenBeforeDisplayNamesRendersTheSameDisplays(t *testing.T) {
	const journal = `{"kind":"user-created","at":"2000-01-01T00:00:00Z","user_id":"usr_withmail","provider":"notes-idp","subject":"subject-0101","email":"rowan@notes.example.test","narrowed_scopes":null}
{"kind":"user-created","at":"2000-01-01T00:01:00Z","user_id":"usr_nomail","provider":"notes-idp","subject":"subject-0202","narrowed_scopes":null}
{"kind":"project-created","at":"2000-01-01T00:02:00Z","project_id":"prj_quarry","name":"quarry","user_id":"usr_withmail","narrowed_scopes":null}
`
	events, err := ReadEvents(strings.NewReader(journal))
	if err != nil {
		t.Fatalf("reading the literal journal: %v", err)
	}
	m, err := Replay(events)
	if err != nil {
		t.Fatalf("an older journal no longer replays: %v", err)
	}
	for _, tc := range []struct {
		kind Kind
		id   ID
		want string
	}{
		{KindUser, "usr_withmail", "rowan@notes.example.test"},
		{KindUser, "usr_nomail", "notes-idp:subject-0202"},
		{KindProject, "prj_quarry", "quarry"},
	} {
		p, ok := m.PrincipalFor(tc.kind, tc.id)
		if !ok || p.Display != tc.want {
			t.Errorf("PrincipalFor(%s, %s) = %q, %v; want %q, true", tc.kind, tc.id, p.Display, ok, tc.want)
		}
	}
	if m.Epoch != 3 {
		t.Errorf("epoch %d, want 3 — every line applied", m.Epoch)
	}
}

// TestAJournalWithDisplayNamesRendersThem is the REGRESSION test for the feature, written against
// the journal's TEXT so it compiles on the commit before it and fails there by ASSERTION rather than
// by a missing symbol: that build ignores `display_name` on `user-created` (an unknown field to it)
// and refuses `user-renamed` as an unknown kind — refusing the journal whole, which is the
// documented behaviour for a journal written by a newer build.
func TestAJournalWithDisplayNamesRendersThem(t *testing.T) {
	const journal = `{"kind":"user-created","at":"2000-01-01T00:00:00Z","user_id":"usr_named","provider":"notes-idp","subject":"subject-0101","email":"rowan@notes.example.test","display_name":"octocat-example","narrowed_scopes":null}
{"kind":"user-created","at":"2000-01-01T00:01:00Z","user_id":"usr_later","provider":"notes-idp","subject":"subject-0202","email":"wren@notes.example.test","narrowed_scopes":null}
{"kind":"user-renamed","at":"2000-01-01T00:02:00Z","user_id":"usr_later","display_name":"hubot-example","narrowed_scopes":null}
`
	events, err := ReadEvents(strings.NewReader(journal))
	if err != nil {
		t.Fatalf("reading the literal journal: %v", err)
	}
	m, err := Replay(events)
	if err != nil {
		t.Fatalf("a journal carrying display names does not replay: %v", err)
	}
	for id, want := range map[ID]string{"usr_named": "octocat-example", "usr_later": "hubot-example"} {
		if p, ok := m.PrincipalFor(KindUser, id); !ok || p.Display != want {
			t.Errorf("PrincipalFor(user, %s) = %q, %v; want %q", id, p.Display, ok, want)
		}
	}
}
