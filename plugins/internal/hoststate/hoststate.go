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
	// Seen is when the pod last LISTED the session to this plugin, or this entry was last written.
	Seen string `json:"seen,omitempty"`
}

// StateTTL is how long an entry is kept for a session the pod has stopped listing and still answers
// for. Past it the entry is dropped whatever it holds.
const StateTTL = 30 * 24 * time.Hour

// Touch records that the session was listed (or its entry written) now. It reports whether the
// stamp moved by a day or more (or was absent): the caller SAVES then, so the stamp of a session that
// is listed run after run without anything else changing is not only ever in memory — and is not
// rewritten on every run either.
func (b *Backoff) Touch(now time.Time) (moved bool) {
	at, err := time.Parse(time.RFC3339, b.Seen)
	if err == nil && now.Sub(at) < 24*time.Hour && !now.Before(at) {
		return false
	}
	b.Seen = now.UTC().Format(time.RFC3339)
	return true
}

// Stale reports whether the entry was last touched more than [StateTTL] ago. An entry with no stamp
// (written before the stamp existed) is not stale; the caller stamps it.
func (b *Backoff) Stale(now time.Time) bool {
	at, err := time.Parse(time.RFC3339, b.Seen)
	return err == nil && now.Sub(at) > StateTTL
}

// Unlisted decides what happens to the entry of a session the pending query did NOT list this run.
//
// 🔴 ABSENCE FROM ONE PENDING PAGE IS NOT A SIGNAL. The pod lists a session only when it is idle (or
// grew past a threshold) and within the page: a session that was merely RESUMED drops off the list
// and comes back when it pauses. Dropping its entry on absence forgot the backoff — and, for plugin A,
// the already-paid-for summary — once per pause/resume cycle, and a full quota was re-paid each
// time. An entry is dropped only on a POSITIVE signal: `gone` reports the pod answering its one
// refusal for the session (deleted, or the plugin turned off for it), or the entry is past
// [StateTTL]. A probe that FAILS for any other reason keeps the entry.
func (b *Backoff) Unlisted(now time.Time, gone func() (bool, error)) (drop bool) {
	// Stamped ONCE, when an entry has none (written before the stamp existed) — never refreshed
	// here: a stamp renewed on every unlisted run is a TTL that never expires.
	if b.Seen == "" {
		b.Seen = now.UTC().Format(time.RFC3339)
	}
	if b.Stale(now) {
		return true
	}
	isGone, err := gone()
	return err == nil && isGone
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

// Write is the temporary file's write — a seam, so a test can make it fail part-way.
var Write = func(f *os.File, raw []byte) (int, error) { return f.Write(raw) }

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
	// 🔴 PLAIN ASSIGNMENT, NOT `:=`: a shadowed `err` here dropped a failed write, and the truncated
	// temporary file was then renamed over the good state — every backoff and unwritten summary lost.
	if err = tmp.Chmod(0o600); err == nil {
		_, err = Write(tmp, append(raw, '\n'))
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
