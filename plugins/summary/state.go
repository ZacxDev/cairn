package main

import (
	"time"

	"github.com/ZacxDev/cairn/plugins/internal/hoststate"
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
//     write lands, when the pod's own last output for the session changes under it, when the pod
//     answers its one refusal for the session (deleted, or the plugin turned off), or after
//     [hoststate.StateTTL] unlisted — NEVER merely because one pending page did not list the session,
//     which is what a session that was only resumed looks like.
//
// ⚠ A CACHED SUMMARY IS DERIVED TRANSCRIPT CONTENT AT REST ON THIS HOST, until one of those happens.
// A session deleted on the pod is dropped by the next run: each entry the pod did not list is asked
// about (one read), and the pod's refusal is the signal.
//
// ⚠ BOTH CLOCKS HERE ARE THE HOST'S. A clock stepped FORWARD ends a backoff (and the spend ledger's
// 24-hour window) early; stepped BACK, it lengthens both.

// The backoff itself — the streak, the intervals, the file's atomic write — is `internal/hoststate`,
// shared with plugin B; what is THIS plugin's is the cached summary beside it.
const backoffBase = hoststate.BackoffBase

// cachedSummary is a produced summary whose write was refused.
type cachedSummary struct {
	// PrevID is the pod's last output id for the session when this was produced ("" for none): the
	// summary continues THAT output, so it is stale the moment the pod names another.
	PrevID    string           `json:"prev_id"`
	Summary   string           `json:"summary"`
	Model     string           `json:"model"`
	Watermark map[string]int64 `json:"watermark"`
}

// sessionState is what this host remembers about one session: its backoff, and a summary a refused
// write did not store. The backoff's fields are flattened into the session's own JSON object.
type sessionState struct {
	hoststate.Backoff
	Cached *cachedSummary `json:"cached,omitempty"`
}

type stateFile struct {
	path     string
	Sessions map[string]*sessionState `json:"sessions"`
}

// loadState reads the state file; a missing one is empty, an unreadable one an error ([hoststate.Load]).
func loadState(path string) (*stateFile, error) {
	st := &stateFile{path: path}
	if err := hoststate.Load(path, st); err != nil {
		return nil, err
	}
	if st.Sessions == nil {
		st.Sessions = map[string]*sessionState{}
	}
	return st, nil
}

func (st *stateFile) save() error { return hoststate.Save(st.path, st) }

// backingOff reports whether the session is inside a backoff taken at this record count. Nil-safe: a
// session this host remembers nothing about is not backing off.
func (s *sessionState) backingOff(now time.Time, records int64) bool {
	return s != nil && s.Backoff.Active(now, records)
}

// fail records one more failure at this record count and sets the next attempt.
func (s *sessionState) fail(now time.Time, records int64) time.Duration {
	return s.Backoff.Fail(now, records)
}
