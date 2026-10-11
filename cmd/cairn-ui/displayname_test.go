package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/ui"
)

// TestTheHeaderSaysWhoTheJournalSays is the SEAM between `control.displayOf` and the browser's
// "signed in as": a running `cairn-ui` over a journal in which the user was RENAMED after creation
// signs a credential in and renders the journal's display name — not the email it was created with.
// Each half is tested alone elsewhere; this is the one test that builds the combined state.
func TestTheHeaderSaysWhoTheJournalSays(t *testing.T) {
	_, journal := seededJournal(t, credentialLive)
	store, err := control.OpenFileStore(journal)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := control.RenameUser(context.Background(), store, control.NewUserDisplayName{
		UserID: control.DerivedID(control.PrefixUser, "startup-user"), DisplayName: "octocat-example",
	}); err != nil {
		t.Fatalf("renaming: %v", err)
	}

	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	port := aPortNothingIsListeningOn(t)
	// Generous on purpose: this child is a real process, and under a loaded host (the mutation
	// battery runs this package once per mutant) a 2-second single-shot request timed out at
	// sign-in and was scored as the killer of mutants it never saw.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	cmd := exec.CommandContext(ctx, self,
		"-control-journal", journal,
		"-store", t.TempDir(),
		"-session-file", filepath.Join(t.TempDir(), "sessions"),
		"-token-file", filepath.Join(t.TempDir(), "absent-token"),
		"-host", "127.0.0.1",
		"-port", fmt.Sprint(port))
	cmd.Env = []string{reexecEnv + "=1"}
	c := startChild(t, cmd, cancel, &syncBuffer{}, &syncBuffer{})
	c.waitFor(t, "the serving line", func() bool { return strings.Contains(c.out.String(), "serving") })

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	noRedirect := &http.Client{Timeout: 20 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, _ := http.NewRequest(http.MethodPost, base+ui.SignInPath,
		strings.NewReader(url.Values{ui.FieldToken: {"fixture-startup-credential-not-a-real-token"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", base)
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatalf("signing in: %v", err)
	}
	resp.Body.Close()
	var cookies []string
	for _, ck := range resp.Cookies() {
		cookies = append(cookies, ck.Name+"="+ck.Value)
	}
	if resp.StatusCode != http.StatusSeeOther || len(cookies) == 0 {
		t.Fatalf("POSITIVE CONTROL: sign-in answered %d with %d cookie(s) — nothing below is measured", resp.StatusCode, len(cookies))
	}
	page, _ := http.NewRequest(http.MethodGet, base+ui.RootPath, nil)
	page.Header.Set("Cookie", strings.Join(cookies, "; "))
	got, err := noRedirect.Do(page)
	if err != nil {
		t.Fatal(err)
	}
	defer got.Body.Close()
	body, _ := io.ReadAll(got.Body)
	if !strings.Contains(string(body), ">signed in as octocat-example<") {
		t.Fatalf("the header does not say the journal's display name:\n%s", body)
	}
	if strings.Contains(string(body), "startup@notes.example.invalid") {
		t.Errorf("the page still renders the email a later rename replaced")
	}
}
