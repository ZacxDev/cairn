package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/presence"
	"github.com/ZacxDev/cairn/internal/store"
)

// The presence startup surface (S2). The in-process cases drive the predicates; the re-exec
// cases drive `main`, because whether `main` ACTS on a refusal — and whether it binds a second
// listener at all — is wiring no in-process test can see (`TestMain`'s comment says why).

var (
	startupUser    = control.DerivedID(control.PrefixUser, "startup-user")
	startupProject = control.DerivedID(control.PrefixProject, "startup-project")
	presenceOwnerA = presence.Owner{Kind: control.KindUser, ID: startupUser}
	presenceOwnerB = presence.Owner{Kind: control.KindProject, ID: startupProject}
)

// TestThePresenceFlagsAreAllOrNoneAndNeverBlank: none ⇒ off; every subset ⇒ a refusal naming
// what is missing; a blank value ⇒ refused rather than read as unset; an owner the authority
// does not hold ⇒ refused. All three, well-formed ⇒ on.
func TestThePresenceFlagsAreAllOrNoneAndNeverBlank(t *testing.T) {
	cache, _ := seededJournal(t, credentialLive)
	m := cache.Model()
	const addr, tokens = "127.0.0.1:9", "/nonexistent/presence-tokens"
	owner := presenceOwnerA.String()

	if _, on, err := presenceListener(presenceSettings{}, m); on || err != nil {
		t.Fatalf("no flags: on=%v err=%v, want off and no error", on, err)
	}
	if got, on, err := presenceListener(presenceSettings{addr: addr, tokens: tokens, owner: owner}, m); !on || err != nil || got != presenceOwnerA {
		t.Fatalf("POSITIVE CONTROL: all three: on=%v err=%v owner=%s", on, err, got)
	}
	for _, tc := range []struct {
		name string
		s    presenceSettings
		want string
	}{
		{"addr alone", presenceSettings{addr: addr}, "-presence-tokens and -presence-owner missing"},
		{"tokens alone", presenceSettings{tokens: tokens}, "-presence-agent-addr and -presence-owner missing"},
		{"owner alone", presenceSettings{owner: owner}, "-presence-agent-addr and -presence-tokens missing"},
		{"addr and tokens", presenceSettings{addr: addr, tokens: tokens}, "-presence-owner missing"},
		{"addr and owner", presenceSettings{addr: addr, owner: owner}, "-presence-tokens missing"},
		{"tokens and owner", presenceSettings{tokens: tokens, owner: owner}, "-presence-agent-addr missing"},
		{"whitespace addr", presenceSettings{addr: "   ", tokens: tokens, owner: owner}, "-presence-agent-addr is set to a value that reduces to nothing"},
		{"zero-width tokens", presenceSettings{addr: addr, tokens: "\u200b\u200b", owner: owner}, "-presence-tokens is set to a value that reduces to nothing"},
		{"whitespace owner", presenceSettings{addr: addr, tokens: tokens, owner: "\t"}, "-presence-owner is set to a value that reduces to nothing"},
		{"owner not kind:id", presenceSettings{addr: addr, tokens: tokens, owner: "startup@notes.example.invalid"}, "not <kind>:<id>"},
		{"owner unknown to the authority", presenceSettings{addr: addr, tokens: tokens, owner: "user:usr_nobody"}, "does not hold"},
		{"addr not host:port", presenceSettings{addr: "127.0.0.1", tokens: tokens, owner: owner}, "not host:port"},
		{"host without a mint", presenceSettings{host: "host-a"}, "means nothing without -issue-presence-token"},
		{"mint and listen at once", presenceSettings{issue: "push", addr: addr}, "Refusing to do both"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, on, err := presenceListener(tc.s, m)
			if on || err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("on=%v err=%v, want a refusal containing %q", on, err, tc.want)
			}
		})
	}
}

// TestTheAgentBindGetsItsOwnReachabilityVerdict mirrors the browser listener's refusal onto
// the agent's bind: reachable + no allowlist refuses; loopback, or an allowlist, admits.
func TestTheAgentBindGetsItsOwnReachabilityVerdict(t *testing.T) {
	noList := errors.New("no allowlist")
	for _, addr := range []string{"0.0.0.0:9", "[::]:9", "192.0.2.4:9", "agent.example.invalid:9", "not-an-address"} {
		if err := presenceBindRefusal(addr, noList); err == nil || !strings.Contains(err.Error(), "presence agent listener") {
			t.Errorf("reachable agent bind %s with no allowlist was admitted: %v", addr, err)
		}
		if err := presenceBindRefusal(addr, nil); err != nil {
			t.Errorf("POSITIVE CONTROL: reachable bind %s WITH an allowlist refused: %v", addr, err)
		}
	}
	for _, addr := range []string{"127.0.0.1:9", "[::1]:9"} {
		if err := presenceBindRefusal(addr, noList); err != nil {
			t.Errorf("POSITIVE CONTROL: loopback agent bind %s refused: %v", addr, err)
		}
	}
}

// TestTheMintResolvesAHumanOwnerOnce: `<kind>:<id>`, an email and a project name each resolve to
// the stable `(Kind, ID)`; an unknown value is refused.
func TestTheMintResolvesAHumanOwnerOnce(t *testing.T) {
	cache, _ := seededJournal(t, credentialLive)
	m := cache.Model()
	for raw, want := range map[string]presence.Owner{
		presenceOwnerA.String():             presenceOwnerA,
		"startup@notes.example.invalid":     presenceOwnerA,
		"startup":                           presenceOwnerB,
		"project:" + string(startupProject): presenceOwnerB,
	} {
		if got, err := resolvePresenceOwner(m, raw); err != nil || got != want {
			t.Errorf("resolvePresenceOwner(%q) = %s, %v; want %s", raw, got, err, want)
		}
	}
	for _, raw := range []string{"nobody@notes.example.invalid", "user:usr_nobody"} {
		if _, err := resolvePresenceOwner(m, raw); err == nil {
			t.Errorf("resolvePresenceOwner(%q) succeeded", raw)
		}
	}
}

// presenceChild runs this test binary as `cairn-ui` over a seeded journal with `extra` flags.
type presenceChild struct {
	cmd    *exec.Cmd
	out    *syncBuffer
	stdout *syncBuffer
	cancel context.CancelFunc
	// done closes once the child has exited and been reaped. It is the ONLY caller of
	// cmd.Wait, so `waitFor` can notice a child that died: a waiter blind to exit sat out its
	// full 30 s per boot when a mutant made startup refuse, which pushed the package past the
	// mutant battery's `-timeout=2m` before the row's named guard ever ran (scored
	// MISATTRIBUTED rather than killed).
	done chan struct{}
}

// reap starts the child's single cmd.Wait and registers the cleanup that kills and reaps it.
func (c *presenceChild) reap(t *testing.T) {
	c.done = make(chan struct{})
	go func() { _ = c.cmd.Wait(); close(c.done) }()
	t.Cleanup(func() { c.cancel(); <-c.done })
}

func startPresenceChild(t *testing.T, journal string, extra ...string) *presenceChild {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	args := append([]string{
		"-control-journal", journal,
		"-store", t.TempDir(),
		"-session-file", filepath.Join(t.TempDir(), "sessions"),
		"-token-file", filepath.Join(t.TempDir(), "absent-token"),
		"-host", "127.0.0.1", "-port", "0",
	}, extra...)
	cmd := exec.CommandContext(ctx, self, args...)
	cmd.Env = []string{reexecEnv + "=1"}
	c := &presenceChild{cmd: cmd, out: &syncBuffer{}, stdout: &syncBuffer{}, cancel: cancel}
	cmd.Stdout, cmd.Stderr = c.stdout, c.out
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	c.reap(t)
	return c
}

// exit waits for a child expected to REFUSE (or to finish a mint) and returns its exit code.
func (c *presenceChild) exit(t *testing.T) int {
	t.Helper()
	<-c.done
	st := c.cmd.ProcessState
	if st == nil || st.ExitCode() == -1 {
		t.Fatalf("the child did not exit by itself (it was killed at the deadline), so it SERVED:\n%s", c.out.String())
	}
	return st.ExitCode()
}

func (c *presenceChild) waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		select {
		case <-c.done:
			// One last look: the condition may have become true just before the exit.
			if cond() {
				return
			}
			t.Fatalf("the child EXITED (%s) while waiting for %s:\n%s", c.cmd.ProcessState, what, c.out.String())
		case <-time.After(10 * time.Millisecond):
		}
	}
	t.Fatalf("timed out waiting for %s:\n%s", what, c.out.String())
}

func writeRows(t *testing.T, path string, rows ...presence.TokenRow) {
	t.Helper()
	var b strings.Builder
	for _, r := range rows {
		b.WriteString(r.String() + "\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

const (
	childPushToken = "fixture-child-presence-push-token-host-a-not-real"
	childForeign   = "fixture-child-presence-push-token-owner-b-not-real"
)

// TestTheBinaryRefusesEachPresenceMisconfiguration: each arm exits 78 with ITS OWN refusal, so
// an exit code from some other `os.Exit(exitConfig)` cannot satisfy it.
func TestTheBinaryRefusesEachPresenceMisconfiguration(t *testing.T) {
	_, journal := seededJournal(t, credentialLive)
	tokens := filepath.Join(t.TempDir(), "presence-tokens")
	writeRows(t, tokens, presence.NewTokenRow(presence.KindPush, presenceOwnerA, "host-a", childPushToken))
	owner := presenceOwnerA.String()

	for _, arm := range []struct {
		name string
		args []string
		want string
	}{
		{"address alone", []string{"-presence-agent-addr", "127.0.0.1:0"}, "half-configuration"},
		{"blank owner", []string{"-presence-agent-addr", "127.0.0.1:0", "-presence-tokens", tokens, "-presence-owner", "  "},
			"reduces to nothing"},
		// An UNREADABLE path is a flag misconfiguration and still refuses the process; what a
		// readable file SAYS is `TestATokenFileContentProblemLeavesTheBrowserServingAndTheAgentStopped`.
		{"a token file that does not exist", []string{"-presence-agent-addr", "127.0.0.1:0",
			"-presence-tokens", tokens + ".typo", "-presence-owner", owner}, "cannot be read"},
		{"a reachable agent bind with no allowlist", []string{"-presence-agent-addr", "0.0.0.0:0",
			"-presence-tokens", tokens, "-presence-owner", owner}, "refusing to serve the presence agent listener"},
	} {
		t.Run(arm.name, func(t *testing.T) {
			c := startPresenceChild(t, journal, arm.args...)
			if code := c.exit(t); code != exitConfig {
				t.Fatalf("exit %d, want %d:\n%s", code, exitConfig, c.out.String())
			}
			if !strings.Contains(c.out.String(), arm.want) {
				t.Fatalf("the refusal does not say %q, so this arm cannot tell which refusal fired:\n%s",
					arm.want, c.out.String())
			}
			if strings.Contains(c.out.String(), childForeign) || strings.Contains(c.out.String(), childPushToken) {
				t.Fatal("a refusal printed a token")
			}
		})
	}
}

// TestATokenFileContentProblemLeavesTheBrowserServingAndTheAgentStopped: a readable token file
// whose CONTENT is refused at startup — a row for another owner (decision 15's wall), or a
// malformed row — does NOT take the process down. The browser surface answers `/healthz` 200, the
// agent port is connection-refused, and stderr names the file line, never a token.
func TestATokenFileContentProblemLeavesTheBrowserServingAndTheAgentStopped(t *testing.T) {
	_, journal := seededJournal(t, credentialLive)
	for _, arm := range []struct {
		name  string
		rows  string
		wants []string
	}{
		{"a row for another owner", presence.NewTokenRow(presence.KindPush, presenceOwnerA, "host-a", childPushToken).String() +
			"\n" + presence.NewTokenRow(presence.KindPush, presenceOwnerB, "host-a", childForeign).String() + "\n",
			[]string{"line 2", "names owner " + presenceOwnerB.String()}},
		{"a malformed row (a raw token in the kind column)", childForeign + " " + presenceOwnerA.String() + " host-a " +
			presence.NewTokenRow(presence.KindPush, presenceOwnerA, "host-a", childPushToken).Digest + "\n",
			[]string{"line 1", "field 1 (kind)"}},
	} {
		t.Run(arm.name, func(t *testing.T) {
			tokens := filepath.Join(t.TempDir(), "presence-tokens")
			if err := os.WriteFile(tokens, []byte(arm.rows), 0o600); err != nil {
				t.Fatal(err)
			}
			uiPort, agentPort := aPortNothingIsListeningOn(t), aPortNothingIsListeningOn(t)
			agentAddr := fmt.Sprintf("127.0.0.1:%d", agentPort)
			c := startPresenceChild(t, journal, "-port", fmt.Sprint(uiPort),
				"-presence-agent-addr", agentAddr, "-presence-tokens", tokens, "-presence-owner", presenceOwnerA.String())
			c.waitFor(t, "the serving line (the process must NOT exit)", func() bool {
				return strings.Contains(c.out.String(), "serving")
			})
			out := c.out.String()
			if !strings.Contains(out, "presence agent NOT started (token file refused at startup)") ||
				!strings.Contains(out, "WARNING the presence agent listener is NOT started") {
				t.Fatalf("the process did not come up with the agent listener stopped:\n%s", out)
			}
			for _, want := range arm.wants {
				if !strings.Contains(out, want) {
					t.Fatalf("stderr does not name %q:\n%s", want, out)
				}
			}
			if strings.Contains(out, childForeign) || strings.Contains(out, childPushToken) {
				t.Fatalf("stderr carries a token:\n%s", out)
			}
			// The "serving" line is printed BEFORE `ListenAndServe` binds the port (main.go), so a
			// single probe right after it races the bind and fails as connection-refused — which
			// it did in the publish workflow's nix build. Poll until the surface answers; the
			// waitFor deadline still fails a process that never serves.
			healthz := fmt.Sprintf("http://127.0.0.1:%d/healthz", uiPort)
			var lastErr error
			status := 0
			c.waitFor(t, "the browser surface to answer /healthz", func() bool {
				resp, err := (&http.Client{Timeout: time.Second}).Get(healthz)
				if err != nil {
					lastErr = err
					return false
				}
				resp.Body.Close()
				status = resp.StatusCode
				return true
			})
			if status != http.StatusOK {
				t.Fatalf("/healthz answered %d (last dial error before it answered: %v)", status, lastErr)
			}
			if conn, err := net.DialTimeout("tcp", agentAddr, time.Second); err == nil {
				_ = conn.Close()
				t.Fatalf("the agent listener is accepting on %s although its token file was refused", agentAddr)
			}
		})
	}
}

var agentLine = regexp.MustCompile(`presence agent on (127\.0\.0\.1:\d+) \(sole owner (\S+), (\d+) token row\(s\), 2 route\(s\)\)`)

// TestTheAgentListenerExistsOnlyWhenConfigured: with the three flags, a SECOND listener answers
// the agent routes (a push with a real token is `rows=1`, garbage is 401); without them the same
// port is connection-refused and the startup line says presence is off.
func TestTheAgentListenerExistsOnlyWhenConfigured(t *testing.T) {
	_, journal := seededJournal(t, credentialLive)
	tokens := filepath.Join(t.TempDir(), "presence-tokens")
	writeRows(t, tokens, presence.NewTokenRow(presence.KindPush, presenceOwnerA, "host-a", childPushToken))
	port := aPortNothingIsListeningOn(t)
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	on := startPresenceChild(t, journal, "-presence-agent-addr", addr, "-presence-tokens", tokens,
		"-presence-owner", presenceOwnerA.String())
	on.waitFor(t, "the serving line", func() bool { return strings.Contains(on.out.String(), "serving") })
	m := agentLine.FindStringSubmatch(on.out.String())
	if m == nil || m[1] != addr || m[2] != presenceOwnerA.String() || m[3] != "1" {
		t.Fatalf("the startup line does not announce the agent listener on %s:\n%s", addr, on.out.String())
	}
	post := func(token, body string) (int, string) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost, "http://"+addr+presence.PushPath, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		// A deadline, because a listener that is BOUND but never served accepts the connection
		// and answers nothing — a hang, which must read as this test's failure, not a timeout.
		resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
		if err != nil {
			t.Fatalf("POST to the agent listener: %v", err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	body := `{"schema":1,"host":"host-a","rows":[{"session":"s-0001","runtime":"claude","target":"notes:3",` +
		`"label":"notes","hotkey":"","last_activity":""}]}`
	if code, got := post(childPushToken, body); code != http.StatusOK || got != "rows=1" {
		t.Fatalf("POSITIVE CONTROL: a real push token answered %d %q", code, got)
	}
	if code, _ := post("fixture-garbage-token-for-the-child-xxxxxxxxxxx", body); code != http.StatusUnauthorized {
		t.Fatalf("a garbage token answered %d", code)
	}
	on.cancel()
	<-on.done

	off := startPresenceChild(t, journal)
	off.waitFor(t, "the serving line", func() bool { return strings.Contains(off.out.String(), "serving") })
	if !strings.Contains(off.out.String(), "presence off (no -presence-agent-addr)") {
		t.Fatalf("an unconfigured child does not say presence is off:\n%s", off.out.String())
	}
	if conn, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
		_ = conn.Close()
		t.Fatalf("something answers on %s with no presence flags set", addr)
	}
}

// TestTheMintModeMintsOnceStoresTheDigestAndExits drives `-issue-presence-token` through `main`:
// exit 0, the token on stdout ONCE and nowhere else, its digest in the file bound to the
// resolved owner and host. A second mint for ANOTHER owner into the same file is refused (the
// wall at mint time) and writes nothing.
func TestTheMintModeMintsOnceStoresTheDigestAndExits(t *testing.T) {
	_, journal := seededJournal(t, credentialLive)
	tokens := filepath.Join(t.TempDir(), "presence-tokens")

	c := startPresenceChild(t, journal, "-issue-presence-token", "claim",
		"-presence-owner", "startup@notes.example.invalid", "-presence-host", "host-a", "-presence-tokens", tokens)
	if code := c.exit(t); code != 0 {
		t.Fatalf("the mint exited %d:\n%s", code, c.out.String())
	}
	token := strings.TrimSuffix(c.stdout.String(), "\n")
	if len(token) != 43 || strings.Contains(token, "\n") {
		t.Fatalf("stdout is not exactly one 43-character token: %q", c.stdout.String())
	}
	raw, err := os.ReadFile(tokens)
	if err != nil {
		t.Fatal(err)
	}
	wantRow := presence.NewTokenRow(presence.KindClaim, presenceOwnerA, "host-a", token).String() + "\n"
	if string(raw) != wantRow {
		t.Fatalf("the token file is\n%q\nwant\n%q", raw, wantRow)
	}
	if bytes.Contains(raw, []byte(token)) || strings.Contains(c.out.String(), token) {
		t.Fatal("the token reached the file or stderr")
	}
	if info, err := os.Stat(tokens); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the token file mode is %v (%v), want 0600", info.Mode().Perm(), err)
	}

	other := startPresenceChild(t, journal, "-issue-presence-token", "push",
		"-presence-owner", "startup", "-presence-host", "host-a", "-presence-tokens", tokens)
	if code := other.exit(t); code != exitConfig || !strings.Contains(other.out.String(), "refusing to mint") {
		t.Fatalf("a mint for another owner into the same file exited %d:\n%s", code, other.out.String())
	}
	if after, _ := os.ReadFile(tokens); !bytes.Equal(after, raw) {
		t.Fatal("the refused mint wrote to the file")
	}
	if other.stdout.String() != "" {
		t.Fatalf("the refused mint printed a token: %q", other.stdout.String())
	}
}

// TestTheBrowserReadsTheStoreTheAgentListenerWrites drives the WIRING S4 added to `main`: the ONE
// presence service the agent listener writes is the one `ui.Config.Presence` reads. A push through
// the agent listener for `s-0001` on `host-a` makes the owner's session page — fetched with the
// owner's bearer credential from the BROWSER listener — carry `host-a · notes:3`; before the push
// the same page has no badge (the positive control that the page is reachable and the badge is
// not static). RED with the started listener's service never handed to the config
// (`ui-main-never-hands-presence-to-the-browser`): every in-process `internal/ui` test hands the
// service in itself and cannot see that line.
func TestTheBrowserReadsTheStoreTheAgentListenerWrites(t *testing.T) {
	const (
		credential = "fixture-startup-credential-not-a-real-token"
		scopeName  = "startup-notes"
	)
	user := control.DerivedID(control.PrefixUser, "startup-user")
	project := control.DerivedID(control.PrefixProject, "startup-project")
	at := time.Date(2000, 6, 1, 12, 0, 0, 0, time.UTC)
	journal := filepath.Join(t.TempDir(), "scoped.journal")
	js, err := control.OpenFileStore(journal)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := js.Append(context.Background(),
		control.Event{Kind: control.EventUserCreated, At: at, UserID: user, Provider: "fixture-provider",
			Subject: "00000000-0000-4000-8000-000000000031", Email: "startup@notes.example.invalid"},
		control.Event{Kind: control.EventProjectCreated, At: at, ProjectID: project, Name: "startup", UserID: user},
		control.Event{Kind: control.EventMemberSet, At: at, ProjectID: project, UserID: user, Role: control.RoleOwner},
		control.Event{Kind: control.EventScopeCreated, At: at, ScopeID: control.DerivedID(control.PrefixScope, scopeName),
			DisplayName: scopeName, ProjectID: project},
		control.Event{Kind: control.EventCredentialIssued, At: at, CredentialID: "crd_startup", SubjectKind: control.KindUser,
			SubjectID: user, TokenHash: control.HashToken(credential), Label: "fixture"},
	); err != nil {
		t.Fatal(err)
	}
	storeRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(storeRoot, scopeName), 0o755); err != nil {
		t.Fatal(err)
	}
	entry := "---\nservice: kettle\nscope: " + scopeName + "\n---\n\n## What it is\n\nsynthetic.\n\n" + store.NuanceHeading + "\n\n" +
		"- 2000-01-03: boiled the kettle [cairn: fixture-bot/s-0001]\n"
	if err := os.WriteFile(filepath.Join(storeRoot, scopeName, "kettle.md"), []byte(entry), 0o644); err != nil {
		t.Fatal(err)
	}
	tokens := filepath.Join(t.TempDir(), "presence-tokens")
	writeRows(t, tokens, presence.NewTokenRow(presence.KindPush, presenceOwnerA, "host-a", childPushToken))
	uiPort, agentPort := aPortNothingIsListeningOn(t), aPortNothingIsListeningOn(t)
	agentAddr := fmt.Sprintf("127.0.0.1:%d", agentPort)
	c := startPresenceChild(t, journal, "-store", storeRoot, "-port", fmt.Sprint(uiPort),
		"-presence-agent-addr", agentAddr, "-presence-tokens", tokens, "-presence-owner", presenceOwnerA.String())
	c.waitFor(t, "the serving line", func() bool { return strings.Contains(c.out.String(), "serving") })
	// The "serving" line is printed BEFORE the browser listener binds, so the first GET would race
	// the bind; poll /healthz until it answers (the agent listener is bound before that line).
	c.waitFor(t, "the browser surface to answer /healthz", func() bool {
		resp, err := (&http.Client{Timeout: time.Second}).Get(fmt.Sprintf("http://127.0.0.1:%d/healthz", uiPort))
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	})

	client := &http.Client{Timeout: 10 * time.Second}
	page := func() string {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/session?session=s-0001", uiPort), nil)
		req.Header.Set("Authorization", "Bearer "+credential)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("GET the session page: %v", err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("the owner's session page answered %d: %s\n%s", resp.StatusCode, b, c.out.String())
		}
		return string(b)
	}
	if before := page(); strings.Contains(before, `data-presence=`) {
		t.Fatalf("POSITIVE CONTROL: the page carries a presence badge before any push")
	}
	body := `{"schema":1,"host":"host-a","rows":[{"session":"s-0001","runtime":"claude","target":"notes:3",` +
		`"label":"notes","hotkey":"","last_activity":""}]}`
	req, _ := http.NewRequest(http.MethodPost, "http://"+agentAddr+presence.PushPath, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+childPushToken)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST to the agent listener: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the push answered %d", resp.StatusCode)
	}
	if after := page(); !strings.Contains(after, "host-a · notes:3 · claude · seen ") {
		t.Fatalf("after a push through the agent listener the owner's session page shows no badge — the browser "+
			"is not reading the store the listener writes:\n%s", after)
	}
}
