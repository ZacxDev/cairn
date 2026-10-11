package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/ZacxDev/cairn/internal/memo"
)

// THE MEMO CURSOR (scope-mail decision 6): what ONE session has already been shown, kept LOCALLY,
// one file per (instance, session) at `$XDG_STATE_HOME/cairn/memo/<instance>/<session>.json`
// (0600, written atomically). The server stores no read state at all.
//
// 🔴 A TIMESTAMP HIGH-WATER MARK PLUS A SEEN-SET OF IDS, NEVER AN ID HIGH-WATER. `BIGSERIAL` order is
// ALLOCATION order, not commit order, so `id > cursor` skips a memo that committed late. The check
// asks for `created_at > high_water − memo.CursorMargin`; the margin re-delivers an overlap, and the
// seen-set drops it. `high_water` is ALWAYS a `created_at` the SERVER returned — never this host's
// clock, never an id.
//
// 🔴 THE WIRE CARRIES `created_at` IN WHOLE SECONDS, AND THE SEEN-SET'S PRUNE IS ONE SECOND WIDER
// THAN THE MARGIN BECAUSE OF IT (S1/S2 corrections: `memo.Rendered.CreatedAt` is whole-second UTC).
// A truncated time is ≤ the true one, so a high-water read off the wire is LOW — the safe direction:
// the next check asks from a little earlier and the seen-set drops the repeats. But the PRUNE must
// keep every id the next check can still return: the server returns a memo whose TRUE `created_at`
// is > `hw − margin`, and its TRUNCATED time can then be as low as `hw − margin − 1s` (exclusive).
// So an id is kept while its wire time is > `hw − margin − 1s`. A prune at exactly `hw − margin`
// forgets a memo the next check still returns, and that memo is shown TWICE — measured by
// `TestTheSeenSetOutlivesTheWholeSecondTruncation`.
//
// ⚠ BACKWARD WALL-CLOCK STEPS on the `cairn-ui` host larger than the margin's slack can still let a
// memo be skipped; decision 6 states that residual, and nothing here can close it.

// memoCursor is the on-disk cursor. `Seen` maps a memo id to the wire `created_at` it arrived with.
type memoCursor struct {
	Schema    int               `json:"schema"`
	HighWater string            `json:"high_water,omitempty"`
	Seen      map[string]string `json:"seen"`
}

const memoCursorSchema = 1

// memoStateDir is `$XDG_STATE_HOME/cairn/memo/<instance>`, with `~/.local/state` when the variable
// is unset or not absolute (the XDG rule: a relative value is ignored).
func memoStateDir(alias string) (string, error) {
	base := getenv(StateHomeEnv)
	if base == "" || !filepath.IsAbs(base) {
		home, err := homeDir()
		if err != nil || home == "" {
			return "", fmt.Errorf("no state directory: $%s is unset and $%s cannot be resolved", StateHomeEnv, HomeEnv)
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "cairn", "memo", alias), nil
}

// loadCursor reads the session's cursor. A MISSING file is the empty cursor (a new session sees
// every live memo, O1); an UNREADABLE or CORRUPT one is ALSO the empty cursor, with `problem` saying
// why — treated as absent and REWRITTEN by the next save, never a crash (S3 test plan).
func loadCursor(path string) (c memoCursor, problem string) {
	c = memoCursor{Schema: memoCursorSchema, Seen: map[string]string{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return c, ""
	}
	if err != nil {
		return c, fmt.Sprintf("the cursor %s could not be read (%v); treated as absent", path, err)
	}
	var read memoCursor
	if err := json.Unmarshal(data, &read); err != nil || read.Schema != memoCursorSchema {
		return c, fmt.Sprintf("the cursor %s is corrupt; treated as absent and rewritten", path)
	}
	if read.HighWater != "" {
		if _, err := time.Parse(time.RFC3339, read.HighWater); err != nil {
			return c, fmt.Sprintf("the cursor %s carries an unreadable high-water mark; treated as absent and rewritten", path)
		}
	}
	if read.Seen == nil {
		read.Seen = map[string]string{}
	}
	return read, ""
}

// after is the `after` parameter the next check sends: `high_water − margin`, or "" for no cursor.
func (c memoCursor) after() string {
	if c.HighWater == "" {
		return ""
	}
	hw, _ := time.Parse(time.RFC3339, c.HighWater)
	return hw.Add(-memo.CursorMargin).UTC().Format(time.RFC3339Nano)
}

// observe records one check's answer: every memo returned is SEEN (the backlog past the five
// previews included — O1's "seen" is a pointer there, decision 6 states it), `high_water` moves to
// the largest `created_at` returned, and seen ids the next check can no longer return are pruned.
func (c *memoCursor) observe(memos []memo.Rendered) {
	var hw time.Time
	if c.HighWater != "" {
		hw, _ = time.Parse(time.RFC3339, c.HighWater)
	}
	for _, m := range memos {
		c.Seen[strconv.FormatInt(m.ID, 10)] = m.CreatedAt
		if at, err := time.Parse(time.RFC3339, m.CreatedAt); err == nil && at.After(hw) {
			hw = at
		}
	}
	if hw.IsZero() {
		return
	}
	c.HighWater = hw.UTC().Format(time.RFC3339)
	keepAfter := hw.Add(-memo.CursorMargin - time.Second)
	for id, at := range c.Seen {
		t, err := time.Parse(time.RFC3339, at)
		if err != nil || !t.After(keepAfter) {
			delete(c.Seen, id)
		}
	}
}

// unseen is the memos whose ids the cursor has not recorded, in the order given.
func (c memoCursor) unseen(memos []memo.Rendered) []memo.Rendered {
	var out []memo.Rendered
	for _, m := range memos {
		if _, ok := c.Seen[strconv.FormatInt(m.ID, 10)]; !ok {
			out = append(out, m)
		}
	}
	return out
}

// saveCursor writes the cursor ATOMICALLY at 0600 under a 0700 directory: a temp file in the same
// directory, then a rename, so a reader never sees half a cursor and a crash leaves the old one.
func saveCursor(path string, c memoCursor) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(c) // map keys are emitted sorted, so equal cursors are equal bytes
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".cursor-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
