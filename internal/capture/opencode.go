package capture

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
)

// OpencodeRunner is how the agent asks opencode for its sessions. It NEVER opens opencode's
// SQLite database (decision 5): that file also holds `account`/`credential` tables, which are
// therefore structurally out of reach.
type OpencodeRunner interface {
	// List is `opencode session list --format json`, run IN dir: it lists that project's ROOT
	// sessions only (measured, R3).
	List(dir string) ([]byte, error)
	// Export is `opencode export <id>`.
	Export(id string) ([]byte, error)
}

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

// List runs `opencode session list --format json` in dir.
func (r ExecRunner) List(dir string) ([]byte, error) {
	return r.run(dir, "session", "list", "--format", "json")
}

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

// listRoots parses `session list --format json`: `[{id, …}]`.
func listRoots(raw []byte) ([]string, error) {
	var rows []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("capture: `opencode session list` did not answer a JSON list: %w", err)
	}
	var out []string
	for _, r := range rows {
		if r.ID != "" {
			out = append(out, r.ID)
		}
	}
	sort.Strings(out)
	return out, nil
}
