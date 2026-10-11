package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/control"
)

func revokeFlagsFor(id string) *revokeCredentialFlags {
	enabled := true
	return &revokeCredentialFlags{enabled: &enabled, credential: &id}
}

// issueOne issues one credential through the real command and returns its token and id.
func issueOne(t *testing.T, env map[string]string, made control.Provisioned, label string) (token string, id control.ID) {
	t.Helper()
	var out, errOut bytes.Buffer
	if code := runIssueCredential(env, issueFlagsFor("user", string(made.User), label, ""), &out, &errOut); code != 0 {
		t.Fatalf("issuing: exit %d\n%s", code, errOut.String())
	}
	for _, field := range strings.Fields(errOut.String()) {
		if after, ok := strings.CutPrefix(field, "credential="); ok && strings.HasPrefix(after, "crd_") {
			return strings.TrimRight(out.String(), "\n"), control.ID(after)
		}
	}
	t.Fatalf("no credential id on the issue line:\n%s", errOut.String())
	return "", ""
}

func modelOf(t *testing.T, journal string) control.Model {
	t.Helper()
	s, err := control.OpenFileStore(journal)
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.Model(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// TestRevokeCredentialAppendsOneEventPrintsTheLabelAndTheTokenStopsAuthenticating — decision
// 16's revoke command: exactly ONE `credential-revoked` line, the label it revoked on stdout,
// and after the next read of the journal `Authenticate` refuses the token. RED with a mutant
// that reports success without appending (the token would still authenticate).
func TestRevokeCredentialAppendsOneEventPrintsTheLabelAndTheTokenStopsAuthenticating(t *testing.T) {
	journal, env, made := aJournalWithAnOwner(t)
	token, id := issueOne(t, env, made, "agents@host-a")
	otherToken, _ := issueOne(t, env, made, "agents@host-c")
	if _, _, err := control.Authenticate(modelOf(t, journal), token); err != nil {
		t.Fatalf("precondition: the issued token does not authenticate: %v", err)
	}
	before, _ := os.ReadFile(journal)

	var out, errOut bytes.Buffer
	if code := runRevokeCredential(env, revokeFlagsFor(string(id)), &out, &errOut); code != 0 {
		t.Fatalf("exit %d\n%s", code, errOut.String())
	}
	after, _ := os.ReadFile(journal)
	added := strings.TrimSpace(strings.TrimPrefix(string(after), string(before)))
	if !bytes.HasPrefix(after, before) || strings.Count(added, "\n") != 0 ||
		!strings.Contains(added, `"kind":"credential-revoked"`) || !strings.Contains(added, string(id)) {
		t.Fatalf("want exactly ONE appended credential-revoked line naming %s, got:\n%s", id, added)
	}
	if !strings.Contains(out.String(), `label="agents@host-a"`) {
		t.Errorf("stdout does not name the label it revoked:\n%s", out.String())
	}
	m := modelOf(t, journal)
	if _, _, err := control.Authenticate(m, token); !errors.Is(err, control.ErrNoCredential{}) {
		t.Fatalf("the revoked token still authenticates: %v", err)
	}
	// POSITIVE CONTROL: the same principal's OTHER credential keeps working.
	if _, _, err := control.Authenticate(m, otherToken); err != nil {
		t.Fatalf("revoking one credential cut off its sibling: %v", err)
	}
}

// TestRevokeCredentialRefusesUnknownAndAlreadyRevokedInTheirOwnWordsAndAppendsNothing.
func TestRevokeCredentialRefusesUnknownAndAlreadyRevokedInTheirOwnWordsAndAppendsNothing(t *testing.T) {
	journal, env, made := aJournalWithAnOwner(t)
	_, id := issueOne(t, env, made, "agents@host-a")
	var out, errOut bytes.Buffer
	if code := runRevokeCredential(env, revokeFlagsFor(string(id)), &out, &errOut); code != 0 {
		t.Fatalf("first revoke: exit %d\n%s", code, errOut.String())
	}
	for _, tc := range []struct{ name, id, want string }{
		{"an unknown id", "crd_nobodyissuedthis000000", "holds no credential"},
		{"an already-revoked id", string(id), "is ALREADY revoked"},
		{"not a credential id", "usr_notacredential", "is not a credential id"},
		{"no id", "", "-credential is required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, _ := os.ReadFile(journal)
			var out, errOut bytes.Buffer
			code := runRevokeCredential(env, revokeFlagsFor(tc.id), &out, &errOut)
			if code != exitConfig || !strings.Contains(errOut.String(), tc.want) {
				t.Fatalf("exit %d, want %d with %q:\n%s", code, exitConfig, tc.want, errOut.String())
			}
			if after, _ := os.ReadFile(journal); !bytes.Equal(before, after) {
				t.Fatal("a refused revocation changed the journal")
			}
			if out.Len() != 0 {
				t.Errorf("a refusal printed a record line on stdout: %q", out.String())
			}
		})
	}
}

// TestNarrowScopesWithoutNarrowVerbsIsRefusedAndWritesNothing — decision 17: a scope narrowing
// keeps each scope's full verbs, so `-narrow-scopes` alone would issue an `admin`-carrying
// agent credential. Refused before the mint.
func TestNarrowScopesWithoutNarrowVerbsIsRefusedAndWritesNothing(t *testing.T) {
	journal, env, made := aJournalWithAnOwner(t)
	before, _ := os.ReadFile(journal)
	f := issueFlagsFor("user", string(made.User), "agents@host-a", string(made.Scopes[0].ID))
	empty := ""
	f.narrowVerbs = &empty
	var out, errOut bytes.Buffer
	code := runIssueCredential(env, f, &out, &errOut)
	if code != exitConfig || !strings.Contains(errOut.String(), "-narrow-scopes needs -narrow-verbs") {
		t.Fatalf("exit %d, want %d naming the missing -narrow-verbs:\n%s", code, exitConfig, errOut.String())
	}
	if after, _ := os.ReadFile(journal); !bytes.Equal(before, after) {
		t.Fatal("the refused issue appended to the journal")
	}
	if out.Len() != 0 {
		t.Fatalf("a refused issue put %q on stdout, where a token goes", out.String())
	}
}

// TestNarrowVerbsIssuesACredentialWithoutAdmin — the owner holds admin on the scope; the
// credential issued `-narrow-verbs read,write` does not, and one issued with neither narrowing
// does (POSITIVE CONTROL). An unknown verb and a verb-less value are refused.
func TestNarrowVerbsIssuesACredentialWithoutAdmin(t *testing.T) {
	journal, env, made := aJournalWithAnOwner(t)
	scope := made.Scopes[0].ID
	var out, errOut bytes.Buffer
	if code := runIssueCredential(env, issueFlagsFor("user", string(made.User), "agents@host-a", string(scope)), &out, &errOut); code != 0 {
		t.Fatalf("exit %d\n%s", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "verbs=read,write") {
		t.Errorf("the record line does not carry the verb narrowing:\n%s", errOut.String())
	}
	m := modelOf(t, journal)
	_, a, err := control.Authenticate(m, strings.TrimSpace(out.String()))
	if err != nil {
		t.Fatal(err)
	}
	if a.Allows(scope, control.VerbAdmin) || !a.Allows(scope, control.VerbWrite) {
		t.Fatalf("a read,write agent credential has %s on its scope", a.VerbsOn(scope))
	}
	full, _ := issueOne(t, env, made, "operator laptop")
	if _, a, _ := control.Authenticate(modelOf(t, journal), full); !a.Allows(scope, control.VerbAdmin) {
		t.Fatal("POSITIVE CONTROL FAILED: the un-narrowed credential lacks admin, so the absence above proves nothing")
	}
	for _, bad := range []string{"read,wirte", " , "} {
		before, _ := os.ReadFile(journal)
		f := issueFlagsFor("user", string(made.User), "x", string(scope))
		v := bad
		f.narrowVerbs = &v
		var o, e bytes.Buffer
		if code := runIssueCredential(env, f, &o, &e); code != exitConfig {
			t.Errorf("-narrow-verbs %q: exit %d, want %d\n%s", bad, code, exitConfig, e.String())
		}
		if after, _ := os.ReadFile(journal); !bytes.Equal(before, after) {
			t.Errorf("-narrow-verbs %q appended to the journal", bad)
		}
	}
}
