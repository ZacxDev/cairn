package main

import (
	"context"
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

	cmd     *exec.Cmd
	dir     string
	logPath string
}

// BootWorld materialises the fixture store, writes a token file granting the fixture
// identity every scope in it, and starts `cairn-ui` on loopback.
//
// The store is built by running `tests/reader_fixtures.py`'s own `build_store` through a
// three-line shim rather than by reimplementing it: the point of reusing that builder is
// that the world stays ONE definition, and a Go transcription of it would be a copy that
// drifts.
func BootWorld(ctx context.Context, repoRoot, uiBinary, dir string, port int) (*World, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	store := filepath.Join(dir, "store")

	shim := filepath.Join(dir, "buildstore.py")
	if err := os.WriteFile(shim, []byte(buildStoreShim), 0o600); err != nil {
		return nil, err
	}
	build := exec.CommandContext(ctx, "python3", shim, filepath.Join(repoRoot, "tests"), store)
	build.Stderr = os.Stderr
	if out, err := build.Output(); err != nil {
		return nil, fmt.Errorf("building the fixture store: %w", err)
	} else if len(out) > 0 {
		fmt.Printf("uiaudit: fixture store: %s", out)
	}

	scopes, err := listScopes(store)
	if err != nil {
		return nil, err
	}
	if len(scopes) == 0 {
		// A CONTENT-FLOOR refusal, not a pass. A walk over a store with no scopes would
		// capture the "no scope is visible to this credential" page on every route and
		// report a clean run about nothing.
		return nil, fmt.Errorf("the fixture store %s holds no scopes: a walk over it would capture an empty surface and report success", store)
	}

	tokenPath := filepath.Join(dir, "tokens")
	row := fmt.Sprintf("%s %s %s\n", fixtureToken, fixtureIdentity, strings.Join(scopes, ","))
	if err := os.WriteFile(tokenPath, []byte(row), 0o600); err != nil {
		return nil, err
	}

	logPath := filepath.Join(dir, "cairn-ui.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return nil, err
	}

	w := &World{
		BaseURL: fmt.Sprintf("http://%s:%d", bindHost, port),
		Store:   store,
		Scopes:  scopes,
		dir:     dir,
		logPath: logPath,
	}
	// ⚠ `-control-journal` IS LEFT AT ITS DEFAULT AND THE SHARE FLOW IS THEREFORE
	// READ-ONLY. That is the deployment `internal/ui/README.md` describes for the
	// token-file world, so it is the state worth capturing: a walk that invented a
	// journal would render a page no deployment serves. The consequence is that
	// `POST /share` has no effect to capture, which is fine — this walk never navigates
	// a non-GET row anyway.
	// 🔴 `CommandContext` RATHER THAN `Command`, AND THAT IS HOW AN ORPHAN SURVIVED. A plain
	// `exec.Command` ties the child to nothing: if the walk's context ends — a timeout, a cancelled
	// CI job, a panic before `Stop` — the pod keeps running and keeps the port. One was found alive
	// on a developer's machine from an aborted run, over a DIFFERENT store, and a subsequent walk
	// would have reported `pod up … over N scope(s)` from its own `listScopes` while the browser
	// talked to the survivor. `Cancel` and `WaitDelay` make the kill the context's job rather than a
	// `defer` somebody might not reach.
	w.cmd = exec.CommandContext(ctx, uiBinary,
		"-store", store,
		"-token-file", tokenPath,
		"-session-file", filepath.Join(dir, "sessions.json"),
		"-host", bindHost,
		"-port", fmt.Sprint(port),
	)
	w.cmd.Stdout = logFile
	w.cmd.Stderr = logFile
	// A clean environment: every `CAIRN_*` and `SUBSYSTEM_STORE_*` variable the ambient
	// shell happens to carry would otherwise reach the pod and the flags above would be
	// competing with it. `PATH` is kept because `cairn-ui` resolves nothing by bare name
	// today and a future one might.
	w.cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir}

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
		return nil, err
	}

	if err := w.cmd.Start(); err != nil {
		logFile.Close()
		return nil, err
	}

	if err := waitHealthy(ctx, w.BaseURL+ui.HealthPath, 20*time.Second); err != nil {
		w.Stop()
		body, _ := os.ReadFile(logPath)
		return nil, fmt.Errorf("%w\n--- cairn-ui log ---\n%s", err, body)
	}
	return w, nil
}

// Stop kills the pod. It is called on every exit path including the failing ones; a pod
// left holding a port is how the NEXT run measures the PREVIOUS run's binary.
func (w *World) Stop() {
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
