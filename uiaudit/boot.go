package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ZacxDev/cairn/internal/arcs"
	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/store"
	"github.com/ZacxDev/cairn/internal/touch"
	"github.com/ZacxDev/cairn/internal/ui"
)

// 🔴 THE WORLD IS `tests/reader_fixtures.py`'s, AND NO FIXTURE CONTENT IS INVENTED HERE.
// That builder already produces six scopes and a spread of entries, is CI-green, and is
// leak-clean — its scope and entry names are the sanctioned synthetic vocabulary. A second
// synthetic world would be a second thing to keep leak-clean, and a name invented here
// could not be checked against `tests/leakscan.py`'s closed `denied-identifier` digest set
// by reading it, which is the one check a human cannot perform.
//
// 🔴 AND NOTHING IS ADDED TO `cmd/cairn-ui` FOR THIS. Every knob the boot needs is a flag
// that already exists, so the binary under audit is the binary that ships. A `-uiaudit`
// mode would make the thing measured differ from the thing deployed.

// healthBodyMirror is `internal/ui`'s health response, which is unexported there.
//
// ⚠ A SECOND SPELLING OF A TWO-BYTE LITERAL, AND THE DUPLICATION IS ACCEPTED RATHER THAN HIDDEN.
// `ui.healthBody` is not exported and exporting it would widen that package's surface for one
// assertion in a CI harness — a worse trade than this note. What makes the duplication safe is the
// direction it fails in: if `internal/ui` ever changes the string, `waitHealthy` stops seeing a match
// and the boot times out with "something else is listening on this port", which is loud, immediate,
// and in the one place a reader would look. It cannot fail SILENTLY, which is the only kind of
// duplication worth refusing.
//
// 🔴 AND IT IS NOT THE LOAD-BEARING CHECK. `refusePortInUse` is what establishes that the pod
// answering is the pod this boot started; this only catches a non-cairn listener that a port bind
// raced.
const healthBodyMirror = "ok"

// bindHost is the loopback address the pod is told to listen on, spelled once.
const bindHost = "127.0.0.1"

// fixtureToken is the credential the walk signs in with.
//
// 🔴 INVENTED, AND OBVIOUSLY SO. `internal/authz`'s floor is 43 characters; a value that
// LOOKED plausible would be worse than one that is real, because nobody could tell. This
// one is a single repeated character, which no generator produces.
const fixtureToken = "tttttttttttttttttttttttttttttttttttttttttttt"

// fixtureIdentity is the token file's identity field. `internal/authz` caps it at 32
// characters and requires it to be shorter than the token floor, so a credential pasted
// into the wrong field cannot be read as an identity.
const fixtureIdentity = "uiaudit"

// World is a booted pod plus everything needed to tear it down.
type World struct {
	BaseURL string
	Store   string
	Scopes  []string
	// Token is the credential the walk signs in with: [fixtureToken] in the token-file world,
	// and in the journal world the one `control.IssueCredential` minted (never printed).
	Token string

	cmd     *exec.Cmd
	dir     string
	logPath string
	// stopPushing ends the presence re-push loop (`fixturePresence.keepPushing`).
	stopPushing context.CancelFunc
}

// BootWorld materialises the fixture store, writes a token file granting the fixture
// identity every scope in it, and starts `cairn-ui` on loopback.
//
// The store is built by running `tests/reader_fixtures.py`'s own `build_store` through a
// three-line shim rather than by reimplementing it: the point of reusing that builder is
// that the world stays ONE definition, and a Go transcription of it would be a copy that
// drifts.
func BootWorld(ctx context.Context, repoRoot, uiBinary, dir string, port int) (*World, error) {
	store, scopes, err := buildFixtureStore(ctx, repoRoot, dir)
	if err != nil {
		return nil, err
	}

	arcJournal, err := writeArcJournal(dir, store, scopes)
	if err != nil {
		return nil, err
	}

	tokenPath := filepath.Join(dir, "tokens")
	row := fmt.Sprintf("%s %s %s\n", fixtureToken, fixtureIdentity, strings.Join(scopes, ","))
	if err := os.WriteFile(tokenPath, []byte(row), 0o600); err != nil {
		return nil, err
	}

	presenceSession, err := fixtureMembers(store)
	if err != nil {
		return nil, err
	}
	pres, err := mintPresence(ctx, uiBinary, dir, store, tokenPath)
	if err != nil {
		return nil, err
	}
	agentPort, err := aFreeLoopbackPort()
	if err != nil {
		return nil, err
	}
	pres.agentURL = fmt.Sprintf("http://%s:%d", bindHost, agentPort)
	pres.session = presenceSession[0].Session

	w := &World{
		BaseURL: fmt.Sprintf("http://%s:%d", bindHost, port),
		Store:   store,
		Scopes:  scopes,
		Token:   fixtureToken,
		dir:     dir,
		logPath: filepath.Join(dir, "cairn-ui.log"),
	}
	// ⚠ `-control-journal` IS LEFT AT ITS DEFAULT IN THIS WORLD AND ITS SHARE FLOW IS THEREFORE
	// READ-ONLY. That is the token-file deployment `internal/ui/README.md` describes, and it is
	// still a supported one, so it stays the world every existing signal is measured over. The
	// consequence is that `POST /share` has no effect to capture, which is fine — this walk never
	// navigates a non-GET row anyway. The journal-backed world is a SECOND boot beside it —
	// [BootJournalWorld] — and not a replacement; its doc says why the old objection ("a walk that
	// invented a journal would render a page no deployment serves") no longer holds.
	if err := w.start(ctx, uiBinary, port,
		"-store", store,
		"-token-file", tokenPath,
		"-session-file", filepath.Join(dir, "sessions.json"),
		"-arc-journal", arcJournal,
		"-host", bindHost,
		"-port", fmt.Sprint(port),
		"-presence-agent-addr", fmt.Sprintf("%s:%d", bindHost, agentPort),
		"-presence-tokens", pres.tokens,
		"-presence-owner", pres.owner,
	); err != nil {
		return nil, err
	}
	// A refused FIRST push is a boot failure, not a missing badge: the walk would otherwise capture
	// every page in its no-presence state and report the badged surfaces covered.
	if err := pres.push(ctx); err != nil {
		w.Stop()
		return nil, fmt.Errorf("%w\n--- cairn-ui log ---\n%s", err, w.Log())
	}
	pushCtx, stopPushing := context.WithCancel(ctx)
	w.stopPushing = stopPushing
	go pres.keepPushing(pushCtx)
	return w, nil
}

// start launches `cairn-ui` with `args`, its output to the world's log, and waits until it is
// healthy. It is the ONE launch sequence both worlds use.
func (w *World) start(ctx context.Context, uiBinary string, port int, args ...string) error {
	logFile, err := os.Create(w.logPath)
	if err != nil {
		return err
	}
	// 🔴 `CommandContext` RATHER THAN `Command`, AND THAT IS HOW AN ORPHAN SURVIVED. A plain
	// `exec.Command` ties the child to nothing: if the walk's context ends — a timeout, a cancelled
	// CI job, a panic before `Stop` — the pod keeps running and keeps the port. One was found alive
	// on a developer's machine from an aborted run, over a DIFFERENT store, and a subsequent walk
	// would have reported `pod up … over N scope(s)` from its own `listScopes` while the browser
	// talked to the survivor. `Cancel` and `WaitDelay` make the kill the context's job rather than a
	// `defer` somebody might not reach.
	w.cmd = exec.CommandContext(ctx, uiBinary, args...)
	w.cmd.Stdout = logFile
	w.cmd.Stderr = logFile
	// A clean environment: every `CAIRN_*` and `SUBSYSTEM_STORE_*` variable the ambient
	// shell happens to carry would otherwise reach the pod and the flags would be
	// competing with it. `PATH` is kept because `cairn-ui` resolves nothing by bare name
	// today and a future one might.
	w.cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + w.dir}

	w.cmd.Cancel = func() error { return w.cmd.Process.Kill() }
	// A grace period, then SIGKILL: `Wait` must not block forever on a child that ignores the first
	// signal, because the caller's `defer w.Stop()` would then hang the whole run.
	w.cmd.WaitDelay = 5 * time.Second

	// 🔴 THE PORT IS CLAIMED BEFORE THE POD IS STARTED, WHICH IS WHAT MAKES "THE POD ANSWERING IS THE
	// POD WE STARTED" A PROPERTY RATHER THAN A HOPE. `waitHealthy` accepts any 200 on
	// `/healthz` — and a survivor from an aborted run answers exactly that, over its own store. So
	// this binds the port first: if anything already holds it, the boot REFUSES and names the
	// collision instead of measuring somebody else's pod. The listener is closed immediately
	// afterwards, which leaves a window — but a window between two operations in this function is a
	// different risk from a survivor that has been running for hours, and it is the smaller one.
	if err := refusePortInUse(bindHost, port); err != nil {
		logFile.Close()
		return err
	}

	if err := w.cmd.Start(); err != nil {
		logFile.Close()
		return err
	}

	if err := waitHealthy(ctx, w.BaseURL+ui.HealthPath, 20*time.Second); err != nil {
		w.Stop()
		return fmt.Errorf("%w\n--- cairn-ui log ---\n%s", err, w.Log())
	}
	return nil
}

// buildFixtureStore materialises `tests/reader_fixtures.py`'s world under `dir` and lists its
// scopes. Both worlds call it, so both walk ONE fixture definition.
func buildFixtureStore(ctx context.Context, repoRoot, dir string) (string, []string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", nil, err
	}
	store := filepath.Join(dir, "store")

	shim := filepath.Join(dir, "buildstore.py")
	if err := os.WriteFile(shim, []byte(buildStoreShim), 0o600); err != nil {
		return "", nil, err
	}
	build := exec.CommandContext(ctx, "python3", shim, filepath.Join(repoRoot, "tests"), store)
	build.Stderr = os.Stderr
	if out, err := build.Output(); err != nil {
		return "", nil, fmt.Errorf("building the fixture store: %w", err)
	} else if len(out) > 0 {
		fmt.Printf("uiaudit: fixture store: %s", out)
	}

	scopes, err := listScopes(store)
	if err != nil {
		return "", nil, err
	}
	if len(scopes) == 0 {
		// A CONTENT-FLOOR refusal, not a pass. A walk over a store with no scopes would
		// capture the "no scope is visible to this credential" page on every route and
		// report a clean run about nothing.
		return "", nil, fmt.Errorf("the fixture store %s holds no scopes: a walk over it would capture an empty surface and report success", store)
	}
	return store, scopes, nil
}

// journalEpoch stamps every event the journal world's seed writes: an obviously-synthetic
// year-2000 instant, so no real time reaches the journal.
var journalEpoch = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// journalProvider is the provider half of the fixture user's (provider, subject) key. No
// identity backend is configured in this world, so nothing resolves a session by it; it exists
// because `EventUserCreated` refuses an empty one.
const journalProvider = "uiaudit"

// BootJournalWorld boots `cairn-ui` over the SAME fixture store with `-control-journal` pointing
// at a journal seeded through `internal/control`'s own writers, making the fixture user the OWNER
// of a project holding every fixture scope — so it holds `admin` over each, the share index
// publishes per-scope pages, and each renders its grant form.
//
// 🔴 THIS ANSWERS `BootWorld`'s OLD OBJECTION ON ITS OWN TERMS RATHER THAN OVERRULING IT. The
// token-file world left `-control-journal` unset because a walk that INVENTED a journal "would
// render a page no deployment serves". A journal-backed authority is what a deployed instance
// serves; the token-file one is the supported alternative. So both are booted, side by side, and
// neither replaces the other.
//
// 🔴 SEEDED THROUGH `control.ProvisionUser` AND `control.IssueCredential`, NEVER A HAND-WRITTEN
// JOURNAL LINE: a record shape the pod would DROP at replay could otherwise be captured as if it
// were an authority. Project ownership is `Resolve`'s membership path — the one an operator's
// `-create-user` takes — so this is the authority a deployment builds, not one built for a test.
//
// ⚠ NO `-db-dsn`, SO THE INVITE MINT FORM IS NOT REACHED: the invite rows render
// `ui.NoInviteStore`, which `refuseJournalWorldFellBack` asserts is the state captured.
// ⚠ NO `-arc-journal` AND NO PRESENCE: neither changes the two rows this world walks.
func BootJournalWorld(ctx context.Context, repoRoot, uiBinary, dir string, port int) (*World, error) {
	store, scopes, err := buildFixtureStore(ctx, repoRoot, dir)
	if err != nil {
		return nil, err
	}
	journal := filepath.Join(dir, "control", "journal.jsonl")
	fs, err := control.OpenFileStore(journal)
	if err != nil {
		return nil, err
	}
	prov, err := control.ProvisionUser(ctx, fs, control.NewUser{
		Provider: journalProvider, Subject: fixtureIdentity,
		ProjectName: fixtureIdentity, ScopeNames: scopes, At: journalEpoch,
	})
	if err != nil {
		return nil, fmt.Errorf("seeding the journal world's user: %w", err)
	}
	issued, err := control.IssueCredential(ctx, fs, control.NewCredential{
		SubjectKind: control.KindUser, SubjectID: prov.User, Label: fixtureIdentity, At: journalEpoch,
	})
	if err != nil {
		return nil, fmt.Errorf("issuing the journal world's credential: %w", err)
	}

	w := &World{
		BaseURL: fmt.Sprintf("http://%s:%d", bindHost, port),
		Store:   store,
		Scopes:  scopes,
		Token:   issued.Token(),
		dir:     dir,
		logPath: filepath.Join(dir, "cairn-ui.log"),
	}
	if err := w.start(ctx, uiBinary, port,
		"-store", store,
		"-control-journal", journal,
		"-session-file", filepath.Join(dir, "sessions.json"),
		"-host", bindHost,
		"-port", fmt.Sprint(port),
	); err != nil {
		return nil, err
	}
	return w, nil
}

// Stop kills the pod. It is called on every exit path including the failing ones; a pod
// left holding a port is how the NEXT run measures the PREVIOUS run's binary.
func (w *World) Stop() {
	if w.stopPushing != nil {
		w.stopPushing()
	}
	if w.cmd != nil && w.cmd.Process != nil {
		_ = w.cmd.Process.Kill()
		_, _ = w.cmd.Process.Wait()
	}
}

// Log is the pod's own output, read back so a failure report carries it.
func (w *World) Log() string {
	b, _ := os.ReadFile(w.logPath)
	return string(b)
}

// waitHealthy polls the readiness path.
//
// ⚠ AND READINESS IS EXACTLY THE SIGNAL THIS HARNESS EXISTS TO DISTRUST. `/healthz`
// answers before the authentication chain runs, so it answers `ok` on a pod whose session
// file is unwritable and whose every login therefore fails. Waiting on it is right —
// nothing else says "the listener is up" — but it is a precondition for the walk, never
// evidence about it. The sign-in click is what separates those two worlds.
func waitHealthy(ctx context.Context, url string, budget time.Duration) error {
	deadline := time.Now().Add(budget)
	var last error
	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			resp.Body.Close()
			// 🔴 THE BODY IS COMPARED, NOT JUST THE STATUS. `internal/ui` answers `/healthz` with a
			// fixed string; accepting any 200 accepts any HTTP server on that port — which is
			// precisely the survivor case this boot now refuses up front. Two cheap checks against
			// one class of confusion is not redundancy when the first one is a port bind and the
			// second is a payload.
			if resp.StatusCode == http.StatusOK {
				if got := strings.TrimSpace(string(body)); got != healthBodyMirror {
					last = fmt.Errorf("%s answered 200 but the body is %q, not %q — something else is "+
						"listening on this port", url, got, healthBodyMirror)
				} else {
					return nil
				}
			}
			last = fmt.Errorf("%s answered %s (%q)", url, resp.Status, strings.TrimSpace(string(body)))
		} else {
			last = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(150 * time.Millisecond):
		}
	}
	return fmt.Errorf("cairn-ui never became ready within %s: %v", budget, last)
}

// writeArcJournal writes one arc registration PER FIXTURE SCOPE into a journal OUTSIDE the store
// tree and returns its path, so the walk reaches the arcs card's LISTED state and the `/arc` page
// itself — neither exists on a pod with no journal, and the unconfigured card is what
// `internal/ui`'s own tests pin.
//
// 🔴 ONE PER SCOPE, NOT ONE, AND THAT IS MEASURED: the root publishes a link per scope and
// `ExpandLinks` keeps the first four by sorted PATH — i.e. by scope ID — so a single arc homed in
// one named scope was on a scope page the walk never captured, and the first run reached no
// `/arc?…` page at all. With every scope carrying one, every captured scope page links one, and
// `/arc?…` sorts ahead of `/entry?…` under that same per-page bound. ⚠ Since the scope page became tabs the
// arc links sit on the ARCS tab, reached through `roundRobinByRow`; the per-scope reasoning still holds.
//
// 🔴 NO NAME IS INVENTED: each arc is homed in a fixture scope and its slug IS that scope's name,
// the registrar is `fixtureIdentity`, and the one member is a session id the fixture STORE already
// carries in a trailer (`fixtureMembers`) — so every string in the record
// comes from the fixture world `tests/leakscan.py` already reads. The status is OMITTED, so the
// captured pages show `unknown` — the Q4 state a reader is likeliest to misread as `open`.
//
// ⚠ THIS IS A JOURNAL NO DEPLOYMENT HAS YET, AND THAT IS THE TRADE BOOT.GO OTHERWISE REFUSES (see
// `-control-journal` below). It is taken here because the registry's whole browser surface is
// unreachable without one, and the deployment it previews is the one the plan's mount change
// produces. The journal is written with `internal/arcs`' own record type, so a shape the pod would
// skip as unreadable cannot be captured as if it were a registration.
// fixtureMembers is ONE arc member: the byte-wise first session id the fixture store's trailers
// carry, read through `internal/touch` — the derivation the pages themselves render.
//
// 🔴 MEASURED NECESSARY: with no members, the session page was reachable only bare. The fixture's
// attributed bullets sit in a scope the walk's per-page bound never captures, so no sessions tab it
// reached listed a row; an arc's MEMBERS are the links to `/session?session=…` — as chips on each arc
// row of the scope page's ARCS tab, and as rows on the arc page's SESSIONS tab (since the arc page
// grew tabs, its default scopes tab carries none). Measured on one walk: the first `/session` page
// was published by a scope page's arcs tab. A store with no trailer at all is a CONTENT-FLOOR refusal, for `listScopes`'
// reason: the walk would capture no session page and report the row covered.
func fixtureMembers(storeRoot string) ([]arcs.Member, error) {
	index, err := store.LoadStore(storeRoot, "scanned", store.Unrestricted())
	if err != nil {
		return nil, err
	}
	first := ""
	for _, scope := range index.Scopes() {
		res, err := touch.Writes(storeRoot, index, scope)
		if err != nil {
			return nil, err
		}
		for _, s := range res.Sessions {
			if first == "" || s.ID < first {
				first = s.ID
			}
		}
	}
	if first == "" {
		return nil, fmt.Errorf("the fixture store %s carries no attributed bullet, so no session page can be reached", storeRoot)
	}
	return []arcs.Member{{Session: first, Role: "wrote", FirstSeen: "2000-01-01T00:00:00Z"}}, nil
}

func writeArcJournal(dir, storeRoot string, scopes []string) (string, error) {
	members, err := fixtureMembers(storeRoot)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, home := range scopes {
		// `validRecord` refuses a home that is not already normalized; such a scope gets no arc.
		if store.NormalizeRef(home) != home {
			continue
		}
		reg := arcs.Registration{Schema: arcs.Schema, Home: home, Slug: home, Status: arcs.StatusUnknown,
			ClosingKind: arcs.ClosingNone, DeclaredScopes: []string{home}, Members: members,
			ReportedAt: "2000-01-01T00:00:00Z", RegisteredBy: fixtureIdentity, RegisteredAt: "2000-01-01T00:00:00Z"}
		line, err := json.Marshal(reg)
		if err != nil {
			return "", err
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	if b.Len() == 0 {
		// A CONTENT-FLOOR refusal, for `listScopes`' reason: a journal with nothing in it would
		// capture the empty card everywhere and report the listed state as covered.
		return "", fmt.Errorf("no fixture scope name is already normalized, so no arc can be homed in one: %v", scopes)
	}
	// 🔴 THREE MORE ARCS FOR THE ARCS PAGE (S1 of the arcs/presence plan), all homed in the first
	// normalized fixture scope: one registered an hour before the walk (live by recency), one `open`
	// and old (live by status), one `closed` and old (hidden — so the "not live" count is non-zero
	// and the show-all toggle renders). Every per-scope arc above is `unknown` and dated in 2000, so
	// without these the live view would be EMPTY and the walk would capture only its empty state.
	// ⚠ The recent one is stamped from the WALK's clock, the one place this fixture is not a fixed
	// value: the page's live window is measured against the pod's clock, so a fixed date would age
	// out of it. Its slug and home are fixture strings; no real time reaches a committed file.
	home := ""
	for _, s := range scopes {
		if store.NormalizeRef(s) == s {
			home = s
			break
		}
	}
	for _, extra := range []struct{ slug, status, at string }{
		{"fixture-recent-arc", arcs.StatusClosed, time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)},
		{"fixture-open-old-arc", arcs.StatusOpen, "2000-01-01T00:00:00Z"},
		{"fixture-closed-old-arc", arcs.StatusClosed, "2000-01-01T00:00:00Z"},
	} {
		reg := arcs.Registration{Schema: arcs.Schema, Home: home, Slug: extra.slug, Status: extra.status,
			ClosingKind: arcs.ClosingCheck, DeclaredScopes: []string{home}, Members: members,
			ReportedAt: "2000-01-01T00:00:00Z", RegisteredBy: fixtureIdentity, RegisteredAt: extra.at}
		line, err := json.Marshal(reg)
		if err != nil {
			return "", err
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	arcDir := filepath.Join(dir, "arcs")
	if err := os.MkdirAll(arcDir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(arcDir, "journal.jsonl")
	return path, os.WriteFile(path, []byte(b.String()), 0o600)
}

// listScopes reads the store root the way `tokenfile.Source.storeDirs` does: a scope is a
// subdirectory, and the set is sorted so the token file's allowlist has one spelling.
func listScopes(store string) ([]string, error) {
	entries, err := os.ReadDir(store)
	if err != nil {
		return nil, fmt.Errorf("reading the fixture store: %w", err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

// buildStoreShim calls `tests/reader_fixtures.py`'s own builder. It is written to a temp
// dir rather than committed beside the fixture because it is an argument-shuffling
// adapter, not a second definition of the world — and because a committed file next to
// `reader_fixtures.py` would look like part of the fixture's own interface.
const buildStoreShim = `import pathlib, sys

sys.path.insert(0, sys.argv[1])
import reader_fixtures  # noqa: E402

dest = pathlib.Path(sys.argv[2])
reader_fixtures.build_store(dest)
print(f"{dest} built from reader_fixtures.build_store\n", end="")
`

// refusePortInUse refuses when anything already holds the loopback port this walk is about to use.
//
// 🔴 IT IS THE ONLY THING THAT DISTINGUISHES "MY POD IS UP" FROM "SOMEBODY'S POD IS UP". Every other
// signal in the boot — a 200 on `/healthz`, a scope count read from the store this function created —
// is equally true of a survivor from an aborted run listening on the same port over a different
// store. The failure that shape produces is the worst kind: a walk that reports the world it built
// while measuring a world it did not.
func refusePortInUse(host string, port int) error {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("%s is already in use, so this walk refuses to start: a pod already listening "+
			"there would answer /healthz and be measured as if it were the one this boot created, over "+
			"whatever store IT was given. Find the holder (`ss -lptn 'sport = :%d'`) and kill it by "+
			"RESOLVED PID rather than by a pattern: %w", addr, port, err)
	}
	return ln.Close()
}
