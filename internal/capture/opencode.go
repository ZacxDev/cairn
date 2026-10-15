package capture

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"syscall"
)

// OpencodeRunner is how the agent asks opencode for its sessions. It NEVER opens opencode's
// SQLite database (decision 5): that file also holds `account`/`credential` tables, which are
// therefore structurally out of reach.
type OpencodeRunner interface {
	// List is `opencode session list --format json -n <limit>`, run IN dir: it lists that
	// project's ROOT sessions only (measured, R3), at most `limit` of them.
	List(dir string, limit int) ([]byte, error)
	// Export is `opencode export <id>`.
	Export(id string) ([]byte, error)
	// Projects is `opencode debug scrap`: every project opencode knows, as a JSON list. A CLI call
	// like the other two — the database stays unopened.
	Projects() ([]byte, error)
}

// SessionListCap is the `-n` every `session list` is run with.
//
// 🔴 MEASURED, AND THE REASON IT EXISTS: `opencode session list` (1.18.29) answers at most 100 rows
// unless `-n` is given — exit 0, nothing on stderr — so a project with more root sessions than that
// lost every one past the 100 most recent, on every run. `-n` lifts it (all 202 rows of a 202-root
// project at `-n 100000`). A list that comes back with this many rows is REFUSED rather than read
// ([ErrSessionListAtCap]): at the cap, "all of them" and "the first N of more" look the same.
const SessionListCap = 1000000

// ErrSessionListAtCap is a `session list` answer whose row count reached the `-n` it was asked
// with: it may be cut short, and nothing in it says whether it was.
var ErrSessionListAtCap = errors.New("capture: `opencode session list` answered as many rows as its -n cap; " +
	"could not vouch the list is complete")

// ExecRunner runs the opencode binary by name (default `opencode`, found on PATH — the
// `gitMinimal`-on-PATH precedent).
type ExecRunner struct {
	Bin string
	// TempDir holds the regular file each command's stdout is written to.
	TempDir string
}

// run executes the binary with stdout sent to a REGULAR FILE, never a pipe.
//
// 🔴 MEASURED, AND THE REASON THIS FUNCTION EXISTS: `opencode export` (1.18.29) TRUNCATES its
// output when stdout is a pipe — eight of eight sessions stopped at 8, 64 or 96 KiB with EXIT 0 —
// while the same export to a regular file was complete. So stdout is a temp file, and the caller
// still refuses any document that does not parse: the exit code is not evidence of a whole one.
func (r ExecRunner) run(dir string, args ...string) ([]byte, error) {
	bin := r.Bin
	if bin == "" {
		bin = "opencode"
	}
	out, err := os.CreateTemp(r.TempDir, "opencode-out-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(out.Name())
	defer out.Close()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Stdout = out
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("capture: %s %v: %w", bin, args, err)
	}
	if _, err := out.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	return io.ReadAll(out)
}

// List runs `opencode session list --format json -n <limit>` in dir.
func (r ExecRunner) List(dir string, limit int) ([]byte, error) {
	return r.run(dir, "session", "list", "--format", "json", "-n", strconv.Itoa(limit))
}

// Projects runs `opencode debug scrap`.
func (r ExecRunner) Projects() ([]byte, error) { return r.run("", "debug", "scrap") }

// Export runs `opencode export <id>`.
func (r ExecRunner) Export(id string) ([]byte, error) { return r.run("", "export", id) }

// ErrIncompleteExport is an export that is not ONE complete JSON document: the measured pipe
// truncation, or anything else that cut it short. Nothing from it is shipped.
var ErrIncompleteExport = errors.New("capture: the opencode export is not one complete JSON document; refusing to ship any of it")

// exportUnit is one versioned unit of an export: the session's own `info`, a message's `info`, or
// a part — each shipped as a record, keyed so a later export UPSERTS it.
type exportUnit struct {
	Key  string // "info", "msg:<id>", "prt:<id>"
	Raw  json.RawMessage
	Part map[string]any // decoded, for parts only
	Role string         // the owning message's role, for parts
}

// parseExport splits an export into units and finds the child sessions its `task` parts name.
func parseExport(raw []byte) ([]exportUnit, []string, error) {
	if !json.Valid(raw) {
		return nil, nil, ErrIncompleteExport
	}
	var doc struct {
		Info     json.RawMessage `json:"info"`
		Messages []struct {
			Info  json.RawMessage   `json:"info"`
			Parts []json.RawMessage `json:"parts"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil || doc.Info == nil {
		return nil, nil, ErrIncompleteExport
	}
	units := []exportUnit{{Key: "info", Raw: doc.Info}}
	var children []string
	for _, m := range doc.Messages {
		var mi struct {
			ID   string `json:"id"`
			Role string `json:"role"`
		}
		if err := json.Unmarshal(m.Info, &mi); err != nil || mi.ID == "" {
			return nil, nil, ErrIncompleteExport
		}
		units = append(units, exportUnit{Key: "msg:" + mi.ID, Raw: m.Info})
		for _, p := range m.Parts {
			dec := json.NewDecoder(bytes.NewReader(p))
			dec.UseNumber()
			var part map[string]any
			if err := dec.Decode(&part); err != nil {
				return nil, nil, ErrIncompleteExport
			}
			id, _ := part["id"].(string)
			if id == "" {
				return nil, nil, ErrIncompleteExport
			}
			units = append(units, exportUnit{Key: "prt:" + id, Raw: p, Part: part, Role: mi.Role})
			if part["type"] == "tool" && part["tool"] == "task" {
				state, _ := part["state"].(map[string]any)
				md, _ := state["metadata"].(map[string]any)
				if child, ok := md["sessionId"].(string); ok && child != "" {
					children = append(children, child)
				}
			}
		}
	}
	sort.Strings(children)
	return units, dedupe(children), nil
}

// diffUnits is the upsert set: every unit whose VERSION differs from the one already shipped.
//
// 🔴 THE VERSION IS A DIGEST OF THE UNIT'S BYTES, NOT `(id, time_updated)` AS THE PLAN FIRST SAID:
// the export carries no `time_updated` (measured, R3). A digest is also strictly stronger: it
// changes whenever any byte of the part does, which is exactly "the part was mutated".
func diffUnits(units []exportUnit, shipped map[string]string) []exportUnit {
	var out []exportUnit
	for _, u := range units {
		if shipped[u.Key] != digest(u.Raw) {
			out = append(out, u)
		}
	}
	return out
}

func dedupe(in []string) []string {
	var out []string
	for i, s := range in {
		if i == 0 || s != in[i-1] {
			out = append(out, s)
		}
	}
	return out
}

// listedRoot is one row of `session list`: the root's id and its `updated` (epoch milliseconds),
// 0 when the row carries none that parses as a non-negative integer — and 0 never lets an export
// be skipped ([Agent.exportSettled]).
type listedRoot struct {
	ID      string
	Updated int64
}

// listRoots parses `session list --format json`: `[{id, updated, …}]`, asked for at most `limit`
// rows. A list of `limit` rows or more is [ErrSessionListAtCap].
func listRoots(raw []byte, limit int) ([]listedRoot, error) {
	var rows []struct {
		ID      string          `json:"id"`
		Updated json.RawMessage `json:"updated"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("capture: `opencode session list` did not answer a JSON list: %w", err)
	}
	if len(rows) >= limit {
		return nil, fmt.Errorf("%w (%d rows at -n %d)", ErrSessionListAtCap, len(rows), limit)
	}
	var out []listedRoot
	for _, r := range rows {
		if r.ID == "" {
			continue
		}
		u, err := strconv.ParseInt(string(r.Updated), 10, 64)
		if err != nil || u < 0 {
			u = 0
		}
		out = append(out, listedRoot{ID: r.ID, Updated: u})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// ErrProjectsUnreadable is a `debug scrap` answer that is not a JSON list of projects each naming
// an absolute worktree. Discovery REFUSES on it; it never degrades to "no projects".
var ErrProjectsUnreadable = errors.New("capture: `opencode debug scrap` did not answer a JSON list of projects " +
	"with absolute worktrees")

// parseProjects reads `opencode debug scrap`'s `[{"id", "worktree", …}]` into cleaned worktree
// paths. The `global` project's worktree is `/`, and `session list` run there lists the global
// project's sessions (measured), so it is a project like any other.
//
// 🔴 ONLY A LIST IS AN ANSWER. `null`, an object, a row with no worktree or a relative one is
// [ErrProjectsUnreadable]: each would otherwise read as "fewer projects", the silent loss this
// discovery exists to end. `[]` IS an answer: opencode knows no project.
func parseProjects(raw []byte) ([]string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, ErrProjectsUnreadable
	}
	var rows []struct {
		Worktree *string `json:"worktree"`
	}
	if err := json.Unmarshal(trimmed, &rows); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProjectsUnreadable, err)
	}
	out := make([]string, 0, len(rows))
	for i, r := range rows {
		if r.Worktree == nil || !filepath.IsAbs(*r.Worktree) {
			return nil, fmt.Errorf("%w: row %d", ErrProjectsUnreadable, i)
		}
		out = append(out, filepath.Clean(*r.Worktree))
	}
	return out, nil
}

// DiscoverOpencodeDirs is the opencode project directories one run enumerates: the `explicit`
// ones (`-opencode-project`) UNION every project `opencode debug scrap` names, deduplicated by
// cleaned absolute path and sorted. `discovered` is how many projects scrap named.
//
// A discovered worktree that no longer exists is SKIPPED and counted in `skipped`: `session list`
// must run inside the directory, so its sessions cannot be listed through the CLI at all — the
// caller says how many there were, so the loss is never silent. Any other stat error keeps the
// directory, and the run's own `refused opencode project` line then names it. An explicit
// directory is never skipped.
//
// 🔴 A RUNNER THAT FAILS (a missing binary, a non-zero exit) OR AN ANSWER THAT DOES NOT PARSE IS AN
// ERROR — never zero projects.
func DiscoverOpencodeDirs(r OpencodeRunner, explicit []string) (dirs []string, discovered, skipped int, err error) {
	raw, err := r.Projects()
	if err != nil {
		return nil, 0, 0, fmt.Errorf("capture: opencode project discovery failed: %w", err)
	}
	found, err := parseProjects(raw)
	if err != nil {
		return nil, 0, 0, err
	}
	set := map[string]bool{}
	for _, d := range explicit {
		if abs, aerr := filepath.Abs(d); aerr == nil {
			d = abs
		}
		set[filepath.Clean(d)] = true
	}
	for _, d := range found {
		if set[d] {
			continue
		}
		if _, serr := os.Stat(d); errors.Is(serr, os.ErrNotExist) || errors.Is(serr, syscall.ENOTDIR) {
			skipped++
			continue
		}
		set[d] = true
	}
	for d := range set {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	return dirs, len(found), skipped, nil
}
