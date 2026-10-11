package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The client listener's startup surface (S2 of the scope-mail plan). The in-process case drives the
// predicate; the re-exec case drives `main`, because whether `main` ACTS on a refusal is wiring no
// in-process test can see. What needs a database — the listener actually serving, a cookie signed in
// next door, the uniform miss over real rows — is `clientapi_pgtest_test.go`.

// TestTheClientAPIFlagNeedsAJournalADatabaseAndAnAllowlist: unset ⇒ off; set without
// -control-journal ⇒ refused naming it (O9: never the token-file projection); without -db-dsn ⇒
// refused naming it; blank ⇒ refused; not host:port ⇒ refused; a reachable bind with no allowlist ⇒
// refused. POSITIVE CONTROLS: a loopback bind with both, and a reachable bind WITH an allowlist.
func TestTheClientAPIFlagNeedsAJournalADatabaseAndAnAllowlist(t *testing.T) {
	const journal, dsn = "/var/lib/cairn/control.journal", "postgres://db.example.invalid/cairn"
	noList := errors.New("no allowlist")
	if err := clientAPIRefusal("", "", "", noList); err != nil {
		t.Fatalf("no flag: %v, want the off state", err)
	}
	for _, ok := range []struct {
		addr     string
		proxyErr error
	}{
		{"127.0.0.1:0", noList}, {"[::1]:9", noList}, {"0.0.0.0:9", nil}, {"192.0.2.4:9", nil},
	} {
		if err := clientAPIRefusal(ok.addr, journal, dsn, ok.proxyErr); err != nil {
			t.Errorf("POSITIVE CONTROL: %s (allowlist error %v) refused: %v", ok.addr, ok.proxyErr, err)
		}
	}
	for _, tc := range []struct {
		name, addr, journal, dsn string
		want                     string
	}{
		{"no journal", "127.0.0.1:0", "", dsn, "needs -control-journal"},
		{"no database", "127.0.0.1:0", journal, "", "needs -db-dsn"},
		{"neither (the journal is named first)", "127.0.0.1:0", "", "", "needs -control-journal"},
		{"whitespace addr", "   ", journal, dsn, "reduces to nothing"},
		{"zero-width addr", "\u200b\u200b", journal, dsn, "reduces to nothing"},
		{"not host:port", "127.0.0.1", journal, dsn, "not host:port"},
		{"reachable wildcard, no allowlist", "0.0.0.0:9", journal, dsn, "refusing to serve the client API listener"},
		{"reachable address, no allowlist", "192.0.2.4:9", journal, dsn, "refusing to serve the client API listener"},
		{"a hostname reads as reachable", "agents.example.invalid:9", journal, dsn, "refusing to serve the client API listener"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := clientAPIRefusal(tc.addr, tc.journal, tc.dsn, noList)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v, want a refusal containing %q", err, tc.want)
			}
		})
	}
}

// TestTheBinaryRefusesEachClientAPIMisconfiguration: each arm exits 78 with ITS OWN refusal, BEFORE
// any database is dialled (the DSN below names a host that does not resolve, so a child that got as
// far as the database would refuse with a DIFFERENT message, which the arm's text check rejects).
//
// 🔴 THE NO-JOURNAL ARM IS THE ONE THE PLAN NAMES (round 1 🟡1), and its token file is VALID on
// purpose: on a build without the refusal the child takes `openAuthority`'s token-file branch and
// proceeds — it reaches the database and refuses there, which this arm's message check reads as RED.
// The Postgres tier's `TestTheClientListenerNeverServesTheTokenFileProjection` is the same mutant
// with a database that ANSWERS, where the child then comes up serving token-file rows.
func TestTheBinaryRefusesEachClientAPIMisconfiguration(t *testing.T) {
	_, journal := seededJournal(t, credentialLive)
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte(strings.Repeat("s", 43)), 0o600); err != nil {
		t.Fatal(err)
	}
	const unreachableDSN = "postgres://cairn@db.example.invalid:5432/cairn?sslmode=disable&connect_timeout=1"
	for _, arm := range []struct {
		name string
		args []string
		want string
	}{
		{"no -control-journal (a valid token file present)", []string{"-token-file", tokenFile,
			"-db-dsn", unreachableDSN, "-client-api-addr", "127.0.0.1:0"}, "needs -control-journal"},
		{"no -db-dsn", []string{"-control-journal", journal, "-client-api-addr", "127.0.0.1:0"}, "needs -db-dsn"},
		{"a blank address", []string{"-control-journal", journal, "-db-dsn", unreachableDSN, "-client-api-addr", " "},
			"reduces to nothing"},
		{"a reachable bind with no allowlist", []string{"-control-journal", journal, "-db-dsn", unreachableDSN,
			"-client-api-addr", "0.0.0.0:0"}, "refusing to serve the client API listener"},
	} {
		t.Run(arm.name, func(t *testing.T) {
			self, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			args := append([]string{"-store", t.TempDir(), "-session-file", filepath.Join(t.TempDir(), "sessions"),
				"-host", "127.0.0.1", "-port", "0"}, arm.args...)
			cmd := exec.CommandContext(ctx, self, args...)
			cmd.Env = []string{reexecEnv + "=1"}
			c := startChild(t, cmd, cancel, &syncBuffer{}, &syncBuffer{})
			if code := c.exit(t); code != exitConfig {
				t.Fatalf("exit %d, want %d:\n%s", code, exitConfig, c.out.String())
			}
			if !strings.Contains(c.out.String(), arm.want) {
				t.Fatalf("the refusal does not say %q, so this arm cannot tell which refusal fired:\n%s",
					arm.want, c.out.String())
			}
		})
	}
}
