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
	// Projects reads every project opencode knows, preferring the v2 API and falling back to the
	// v1 CLI command when the v2 API command fails. Both are CLI calls; the database stays unopened.
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

// Projects runs the v2 standalone API command, falling back to the v1 CLI command when it fails.
// A successful command's output is returned unchanged; malformed output is rejected by parseProjects
// rather than hidden by trying a second protocol.
func (r ExecRunner) Projects() ([]byte, error) {
	projects, err := r.run("", "api", "--standalone", "GET", "/api/project")
	if err == nil {
		return projects, nil
	}
	return r.run("", "debug", "scrap")
}

// Export runs the v2 `opencode session export <id>`, falling back to the v1 top-level
// `opencode export <id>` when it fails — the same compatibility shape as Projects.
//
// 🔴 v2 MOVED `export` UNDER `session`, and this function was left on the old form. The failure
// is not a clean "unknown command" an operator would notice: the top-level form prints HELP and
// exits non-zero, so cairn-capture refused every opencode session with
// `capture: opencode [export ses_…]: exit status 1`, none reaching the store — while
// `opencode session export <ses_…>` returned the session JSON with exit 0. Same migration that
// `List` and `Projects` already carry.
func (r ExecRunner) Export(id string) ([]byte, error) {
	out, err := r.run("", "session", "export", id)
	if err == nil {
		return out, nil
	}
	return r.run("", "export", id)
}

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
//
// 🔴 TWO DOCUMENT SHAPES, ONE UNIT MODEL. opencode v2 moved the verb (`session export`) AND changed
// the document: `messages[]` is FLAT — each element being the message itself (`id`, `type`, `agent`,
// `model`, `cost`, `tokens`) with its parts under `content`, and no `{info, parts}` wrapper. Against
// that document the v1 reader refused every session as "not one complete JSON document" while the
// exit code was 0 and the bytes were valid JSON. Both shapes normalize to the SAME units here,
// because the store and the transcript reader consume one model: a message unit must carry `role`
// and each part unit must carry `messageID`.
//
// The v1 path is deliberately LEFT BYTE-FOR-BYTE as it was. A unit's shipped bytes ARE its version,
// so re-encoding what v1 already parsed would re-version every known unit for no gain.
func parseExport(raw []byte) ([]exportUnit, []string, error) {
	if !json.Valid(raw) {
		return nil, nil, ErrIncompleteExport
	}
	var doc struct {
		Info     json.RawMessage   `json:"info"`
		Messages []json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil || doc.Info == nil {
		return nil, nil, ErrIncompleteExport
	}
	units := []exportUnit{{Key: "info", Raw: doc.Info}}
	var children []string
	for _, mraw := range doc.Messages {
		var m struct {
			Info    json.RawMessage   `json:"info"`
			Parts   []json.RawMessage `json:"parts"`
			ID      string            `json:"id"`
			Type    string            `json:"type"`
			Content []json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(mraw, &m); err != nil {
			return nil, nil, ErrIncompleteExport
		}
		if m.Info != nil {
			var mi struct {
				ID   string `json:"id"`
				Role string `json:"role"`
			}
			if err := json.Unmarshal(m.Info, &mi); err != nil || mi.ID == "" {
				return nil, nil, ErrIncompleteExport
			}
			units = append(units, exportUnit{Key: "msg:" + mi.ID, Raw: m.Info})
			for _, p := range m.Parts {
				part, err := decodePart(p)
				if err != nil {
					return nil, nil, err
				}
				id, _ := part["id"].(string)
				if id == "" {
					return nil, nil, ErrIncompleteExport
				}
				units = append(units, exportUnit{Key: "prt:" + id, Raw: p, Part: part, Role: mi.Role})
				if c := taskChild(part); c != "" {
					children = append(children, c)
				}
			}
			continue
		}
		// ---- v2: the message object IS the info, and its parts are `content`.
		if m.ID == "" {
			return nil, nil, ErrIncompleteExport
		}
		meta, err := v2MessageInfo(mraw, m.Type)
		if err != nil {
			return nil, nil, ErrIncompleteExport
		}
		units = append(units, exportUnit{Key: "msg:" + m.ID, Raw: meta})
		for i, p := range m.Content {
			u, child, err := v2Part(p, m.ID, m.Type, strconv.Itoa(i))
			if err != nil {
				return nil, nil, err
			}
			units = append(units, u)
			if child != "" {
				children = append(children, child)
			}
		}
	}
	sort.Strings(children)
	return units, dedupe(children), nil
}

// decodePart decodes ONE part with numbers kept as written (`UseNumber`), so a shipped digest is a
// digest of the part's own bytes rather than of a float round-trip.
func decodePart(raw json.RawMessage) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var part map[string]any
	if err := dec.Decode(&part); err != nil {
		return nil, ErrIncompleteExport
	}
	return part, nil
}

// taskChild is the child session a subagent tool part names, or "". ONE predicate, shared by both
// shapes — the tool's own name field is `tool` on v1 and `name` on v2, normalized by v2Part.
//
// 🔴 v2 RENAMED BOTH HALVES (measured on opencode 2.0.32): the tool is `subagent` (v1: `task`) and
// the child id is `state.metadata.sessionID` (v1: `sessionId`). Asking for the v1 spellings alone
// found zero children on every v2 root. A RESUMED v2 call repeats the id (also in
// `state.input.sessionID`); [dedupe] collapses the repeat.
func taskChild(part map[string]any) string {
	if part["type"] != "tool" || (part["tool"] != "task" && part["tool"] != "subagent") {
		return ""
	}
	state, _ := part["state"].(map[string]any)
	md, _ := state["metadata"].(map[string]any)
	for _, key := range []string{"sessionID", "sessionId"} {
		if child, ok := md[key].(string); ok && child != "" {
			return child
		}
	}
	return ""
}

// v2MessageInfo is the message unit for a v2 message: the message object itself, WITHOUT its
// `content` (each part is its own unit) and WITH a `role`, which v2 spells `type` and which the
// reader keys the transcript's classification off.
func v2MessageInfo(raw json.RawMessage, typ string) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return nil, ErrIncompleteExport
	}
	delete(fields, "content")
	if _, ok := fields["role"]; !ok && typ != "" {
		role, err := json.Marshal(typ)
		if err != nil {
			return nil, ErrIncompleteExport
		}
		fields["role"] = role
	}
	return json.Marshal(fields)
}

// v2Part normalizes ONE part of a v2 message into a unit, adding the three fields the v1 shape
// carried and v2 does not:
//
//   - `messageID` — what links a part to its message's role;
//   - `tool`      — v2 names a tool call's tool `name`;
//   - `state.output` — v2 keeps a completed call's result in `state.content`, which `unitText`
//     already renders, so it is aliased rather than reformatted.
//
// 🔴 A PART WITH NO `id` IS STILL A UNIT. v2 leaves its `text`/`reasoning` parts id-less, so the key
// falls back to the message id plus the part's INDEX within it. That is stable for the case that
// matters: mutating a part's text changes its bytes, hence its digest, under the SAME key.
func v2Part(raw json.RawMessage, msgID, role, index string) (exportUnit, string, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return exportUnit{}, "", ErrIncompleteExport
	}
	id := ""
	if v, ok := fields["id"]; ok {
		_ = json.Unmarshal(v, &id)
	}
	if id == "" {
		id = msgID + "-" + index
	}
	if _, ok := fields["messageID"]; !ok {
		v, err := json.Marshal(msgID)
		if err != nil {
			return exportUnit{}, "", ErrIncompleteExport
		}
		fields["messageID"] = v
	}
	if _, ok := fields["tool"]; !ok {
		if name, ok := fields["name"]; ok {
			fields["tool"] = name
		}
	}
	if st, ok := fields["state"]; ok {
		var state map[string]json.RawMessage
		if err := json.Unmarshal(st, &state); err == nil && state != nil {
			if _, hasOut := state["output"]; !hasOut {
				if content, hasContent := state["content"]; hasContent {
					state["output"] = content
					if b, err := json.Marshal(state); err == nil {
						fields["state"] = b
					}
				}
			}
		}
	}
	out, err := json.Marshal(fields)
	if err != nil {
		return exportUnit{}, "", ErrIncompleteExport
	}
	part, err := decodePart(out)
	if err != nil {
		return exportUnit{}, "", err
	}
	return exportUnit{Key: "prt:" + id, Raw: out, Part: part, Role: role}, taskChild(part), nil
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
// be skipped ([Agent.exportSettled]) — and the `directory` the session ran in, "" when the row
// carries none (measured on 1.18.29: 162 of 162 rows, over two projects, carried one).
type listedRoot struct {
	ID        string
	Updated   int64
	Directory string
}

// listRoots parses `session list --format json`: `[{id, updated, …}]`, asked for at most `limit`
// rows. A list of `limit` rows or more is [ErrSessionListAtCap].
//
// 🔴 AN EMPTY ANSWER IS ZERO ROOTS: measured on opencode 1.18.29, a project with no sessions makes
// `session list` write nothing at all (exit 0) rather than `[]`. Only all-whitespace counts; a
// truncated list is still an error. Stdout goes to a regular file, so empty is not a pipe cut short.
func listRoots(raw []byte, limit int) ([]listedRoot, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	var rows []struct {
		ID        string          `json:"id"`
		Updated   json.RawMessage `json:"updated"`
		Directory json.RawMessage `json:"directory"`
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
		// A `directory` that is not a string is no directory — never a refused list: the row's
		// other fields are read as before, and the session is placed as one with no repo.
		var dir string
		_ = json.Unmarshal(r.Directory, &dir)
		out = append(out, listedRoot{ID: r.ID, Updated: u, Directory: dir})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// ErrProjectsUnreadable is a project-list answer that is not a JSON list of projects each naming
// an absolute v2 canonical path or legacy v1 worktree. Discovery REFUSES on it; it never degrades to "no projects".
var ErrProjectsUnreadable = errors.New("capture: opencode project discovery did not answer a JSON list of projects " +
	"with absolute canonical paths or legacy worktrees")

// projectInfo is one discovered project: its canonical path and sandboxes — further directories
// of the SAME project — all cleaned absolute paths.
type projectInfo struct {
	Path      string
	Sandboxes []string
}

// parseProjects reads project rows with a v2 `canonical` path or legacy v1 `worktree`, plus optional sandboxes. The
// `global` project's path is `/`, and `session list` run there lists the global project's
// sessions (measured), so it is a project like any other.
//
// 🔴 ONLY A LIST IS AN ANSWER. `null`, an object, a row with no canonical/worktree path or a relative one, or a
// sandbox that is not an absolute path string, is [ErrProjectsUnreadable]: each would otherwise
// read as "fewer projects", the silent loss this discovery exists to end. `[]` IS an answer:
// opencode knows no project. A row with no `sandboxes` has none.
func parseProjects(raw []byte) ([]projectInfo, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, ErrProjectsUnreadable
	}
	var rows []struct {
		Canonical json.RawMessage   `json:"canonical"`
		Worktree  json.RawMessage   `json:"worktree"`
		Sandboxes []json.RawMessage `json:"sandboxes"`
	}
	if err := json.Unmarshal(trimmed, &rows); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProjectsUnreadable, err)
	}
	out := make([]projectInfo, 0, len(rows))
	for i, r := range rows {
		pathField := r.Canonical
		if pathField == nil {
			pathField = r.Worktree
		}
		var projectPath string
		if len(pathField) == 0 || json.Unmarshal(pathField, &projectPath) != nil || !filepath.IsAbs(projectPath) {
			return nil, fmt.Errorf("%w: row %d", ErrProjectsUnreadable, i)
		}
		p := projectInfo{Path: filepath.Clean(projectPath)}
		for _, s := range r.Sandboxes {
			var dir string
			if err := json.Unmarshal(s, &dir); err != nil || !filepath.IsAbs(dir) {
				return nil, fmt.Errorf("%w: row %d's sandboxes", ErrProjectsUnreadable, i)
			}
			p.Sandboxes = append(p.Sandboxes, filepath.Clean(dir))
		}
		out = append(out, p)
	}
	return out, nil
}

// Discovery is [DiscoverOpencodeDirs]'s answer: the directories to enumerate, and the counts the
// caller prints — how many projects the CLI named, how many of those whose canonical path is gone were
// reached through an existing sandbox instead, and how many could be reached through neither.
type Discovery struct {
	Dirs                          []string
	Projects, ViaSandbox, Skipped int
}

// gone reports whether a directory does not exist (or a component of its path is not one).
func gone(dir string) bool {
	_, err := os.Stat(dir)
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOTDIR)
}

// DiscoverOpencodeDirs is the opencode project directories one run enumerates: the `explicit`
// ones (`-opencode-project`) UNION every project discovered from opencode, deduplicated by
// cleaned absolute path and sorted.
//
// `session list` runs IN a directory and answers that directory's PROJECT — it filters by project,
// not by directory (measured: a base checkout and a worktree of it answered identical id sets; a
// project whose primary path was gone answered all 10 of its sessions from either of its 2 sandboxes).
// So a project whose primary path no longer exists is reached through its FIRST existing sandbox
// (`ViaSandbox`), and only a project with neither is SKIPPED (`Skipped`) — the caller prints both
// counts, so the loss is never silent. Any other stat error keeps the directory, and the run's own
// `refused opencode project` line then names it. An explicit directory is never skipped. Two
// directories of one project list the same roots; the pass processes each root once
// ([firstSeen]).
//
// 🔴 A RUNNER THAT FAILS (a missing binary, a non-zero exit) OR AN ANSWER THAT DOES NOT PARSE IS AN
// ERROR — never zero projects.
func DiscoverOpencodeDirs(r OpencodeRunner, explicit []string) (Discovery, error) {
	raw, err := r.Projects()
	if err != nil {
		return Discovery{}, fmt.Errorf("capture: opencode project discovery failed: %w", err)
	}
	found, err := parseProjects(raw)
	if err != nil {
		return Discovery{}, err
	}
	d := Discovery{Projects: len(found)}
	set := map[string]bool{}
	for _, dir := range explicit {
		if abs, aerr := filepath.Abs(dir); aerr == nil {
			dir = abs
		}
		set[filepath.Clean(dir)] = true
	}
	for _, p := range found {
		if set[p.Path] || !gone(p.Path) {
			set[p.Path] = true
			continue
		}
		reached := false
		for _, s := range p.Sandboxes {
			if !gone(s) {
				set[s], reached = true, true
				break
			}
		}
		if reached {
			d.ViaSandbox++
		} else {
			d.Skipped++
		}
	}
	for dir := range set {
		d.Dirs = append(d.Dirs, dir)
	}
	sort.Strings(d.Dirs)
	return d, nil
}
