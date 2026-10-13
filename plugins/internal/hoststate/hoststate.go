// Package hoststate is what an example plugin remembers ON ITS HOST, beside its spend ledger: a small
// JSON file, mode 0600, written whole through a temporary file and a rename — and the per-session
// BACKOFF both plugins keep in it.
//
// 🔴 WHY A PLUGIN KEEPS ANY STATE (decision 13 keeps the POD stateless): the pod lists pending sessions
// oldest-first and knows nothing of a session a plugin cannot finish — a provider that refuses the same
// records every time, a write the pod refuses (a full quota, a 400). Without a memory of that, each
// run works on the same sessions FIRST, re-pays for them, and never reaches a healthy session behind
// them. A failed session is skipped for an exponentially growing interval ([BackoffBase] doubling to
// [BackoffMax]); a success clears it, and so does the session GROWING — new records are new input, and
// the refusal was about the old.
//
// ⚠ THE CLOCK IS THE HOST'S. Stepped FORWARD it ends a backoff early; stepped BACK it lengthens one.
package hoststate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// The backoff's first interval and its ceiling.
const (
	BackoffBase = 15 * time.Minute
	BackoffMax  = 24 * time.Hour
)

// Backoff is one session's failure streak.
type Backoff struct {
	Failures int    `json:"failures"`
	RetryAt  string `json:"retry_at,omitempty"`
	// Records is the session's record count (every listed stream) when it last failed.
	Records int64 `json:"records"`
}

// Active reports whether the session is inside a backoff taken at this record count. A nil Backoff is
// not.
func (b *Backoff) Active(now time.Time, records int64) bool {
	if b == nil || b.Failures == 0 || b.Records != records {
		return false
	}
	at, err := time.Parse(time.RFC3339, b.RetryAt)
	return err == nil && now.Before(at)
}

// Fail records one more failure at this record count and sets the next attempt.
func (b *Backoff) Fail(now time.Time, records int64) time.Duration {
	if b.Records != records {
		b.Failures = 0 // the session grew since the last failure: a new streak
	}
	b.Failures++
	b.Records = records
	wait := BackoffMax
	if shift := b.Failures - 1; shift < 16 {
		if w := BackoffBase << shift; w < BackoffMax {
			wait = w
		}
	}
	b.RetryAt = now.Add(wait).UTC().Format(time.RFC3339)
	return wait
}

// Load reads the state file into `into`; a missing file leaves it untouched. An UNREADABLE one is an
// error: starting fresh would silently forget every backoff and re-pay for every refused session at
// once.
func Load(path string, into any) error {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("the state file %s: %w", path, err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("the state file %s does not parse (%v); move it aside to start without backoff state", path, err)
	}
	return nil
}

// Save writes the file whole, through a temporary file and a rename, mode 0600.
func Save(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("the state file's directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".sessions-*")
	if err != nil {
		return fmt.Errorf("the state file %s: %w", path, err)
	}
	defer os.Remove(tmp.Name())
	// 🔴 ONE `err`, ASSIGNED — an earlier spelling declared a second one inside the `if`, so a failed
	// write was dropped and a truncated file was renamed over the good one.
	if err = tmp.Chmod(0o600); err == nil {
		_, err = tmp.Write(append(raw, '\n'))
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		return fmt.Errorf("writing the state file %s: %w", path, err)
	}
	return nil
}
