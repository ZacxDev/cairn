package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// 🔴 A HOST-LOCAL STATE FILE, FOR TWO THINGS THE STATELESS PENDING QUERY CANNOT DO (decision 13 keeps
// the POD stateless; this is the plugin's own, beside its spend ledger, mode 0600):
//
//  1. BACKOFF. The pod lists pending sessions oldest-first and knows nothing of a session this plugin
//     cannot finish — a provider that refuses or truncates the same records every time, a write the
//     pod refuses (a full quota, a 400). Without a memory of that, each run re-paid for the same
//     sessions FIRST and the cap was spent before a healthy session was reached (160 of 200 requests
//     in the audit's simulation). A failed session is skipped for an exponentially growing interval
//     ([backoffBase] doubling to [backoffMax]); a success clears it, and so does the session GROWING —
//     new records are new input, and the refusal was about the old.
//  2. THE SUMMARY A REFUSED WRITE DID NOT STORE. The provider was already paid for it; it is kept here
//     and re-POSTed on the next attempt WITHOUT calling the provider again. It is dropped when the
//     write lands, when the pod's own last output for the session changes under it, or when the pod
//     stops listing the session.
//
// ⚠ A CACHED SUMMARY IS DERIVED TRANSCRIPT CONTENT AT REST ON THIS HOST, until one of those three
// happens. A session deleted on the pod stops being listed, and the next run drops its entry.
//
// ⚠ BOTH CLOCKS HERE ARE THE HOST'S. A clock stepped FORWARD ends a backoff (and the spend ledger's
// 24-hour window) early; stepped BACK, it lengthens both.

const (
	backoffBase = 15 * time.Minute
	backoffMax  = 24 * time.Hour
)

// cachedSummary is a produced summary whose write was refused.
type cachedSummary struct {
	// PrevID is the pod's last output id for the session when this was produced ("" for none): the
	// summary continues THAT output, so it is stale the moment the pod names another.
	PrevID    string           `json:"prev_id"`
	Summary   string           `json:"summary"`
	Model     string           `json:"model"`
	Watermark map[string]int64 `json:"watermark"`
}

// sessionState is what this host remembers about one session.
type sessionState struct {
	Failures int    `json:"failures"`
	RetryAt  string `json:"retry_at,omitempty"`
	// Records is the session's record count (every listed stream) when it last failed.
	Records int64          `json:"records"`
	Cached  *cachedSummary `json:"cached,omitempty"`
}

type stateFile struct {
	path     string
	Sessions map[string]*sessionState `json:"sessions"`
}

// loadState reads the state file; a missing one is empty. An UNREADABLE one is an error: starting
// fresh would silently forget every backoff and re-pay for every refused session at once.
func loadState(path string) (*stateFile, error) {
	st := &stateFile{path: path, Sessions: map[string]*sessionState{}}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return nil, fmt.Errorf("the state file %s: %w", path, err)
	}
	if err := json.Unmarshal(raw, st); err != nil {
		return nil, fmt.Errorf("the state file %s does not parse (%v); move it aside to start without backoff state", path, err)
	}
	if st.Sessions == nil {
		st.Sessions = map[string]*sessionState{}
	}
	return st, nil
}

// save writes the file whole, through a temporary file and a rename, mode 0600.
func (st *stateFile) save() error {
	raw, err := json.MarshalIndent(st, "", " ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(st.path), 0o700); err != nil {
		return fmt.Errorf("the state file's directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(st.path), ".sessions-*")
	if err != nil {
		return fmt.Errorf("the state file %s: %w", st.path, err)
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err == nil {
		_, err = tmp.Write(append(raw, '\n'))
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), st.path)
	}
	if err != nil {
		return fmt.Errorf("writing the state file %s: %w", st.path, err)
	}
	return nil
}

// backingOff reports whether the session is inside a backoff taken at this record count.
func (s *sessionState) backingOff(now time.Time, records int64) bool {
	if s == nil || s.Failures == 0 || s.Records != records {
		return false
	}
	at, err := time.Parse(time.RFC3339, s.RetryAt)
	return err == nil && now.Before(at)
}

// fail records one more failure at this record count and sets the next attempt.
func (s *sessionState) fail(now time.Time, records int64) time.Duration {
	if s.Records != records {
		s.Failures = 0 // the session grew since the last failure: a new streak
	}
	s.Failures++
	s.Records = records
	wait := backoffMax
	if shift := s.Failures - 1; shift < 16 {
		if w := backoffBase << shift; w < backoffMax {
			wait = w
		}
	}
	s.RetryAt = now.Add(wait).UTC().Format(time.RFC3339)
	return wait
}
