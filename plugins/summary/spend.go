package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// 🔴 THE PLUGIN'S OWN SPEND CAP, AND THE ONLY ONE THAT HOLDS ON THIS SIDE (decision 12, D1): the pod
// never sees the provider key, so a pod-side budget could only ask nicely. This one counts REQUESTS
// over a rolling 24 hours in a ledger file and refuses the request that would pass `-max-requests-per-day`
// — BEFORE it is sent, and the ledger line is written before the request too, so a crash mid-request
// still counts it. A request is bounded on both sides (`-chunk-bytes` of input plus the prompt,
// `-max-output-tokens` of output), so a request cap IS a cost cap: at most cap × (the most one request
// can cost). The other ceiling that holds is a spending limit set AT THE PROVIDER, which the operator
// owns.
//
// `-max-requests-per-day 0` is a real setting — the plugin sends nothing — and is what the S9 test
// asserts against a fake provider that counts what it receives.
//
// ⚠ ONE WRITER IS ASSUMED: the ledger is taken under an exclusive `flock` for the read-count-append, so
// two runs on one host cannot both take the last request; two HOSTS with two ledgers each get the
// whole cap. Decision 13 already assumes one worker per plugin.

type ledger struct {
	path string
	cap  int
	now  func() time.Time
}

// ErrCapped is the cap refusing a request.
var ErrCapped = errors.New("the spend cap is reached")

// take records one request if the cap allows it.
func (l *ledger) take() error {
	// No separate `cap <= 0` branch: a cap of 0 is refused by the same count below (0 ≥ 0) — measured,
	// a mutant deleting such a branch survived, so it was a guard nothing could reach.
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return fmt.Errorf("the spend ledger's directory: %w", err)
	}
	f, err := os.OpenFile(l.path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("the spend ledger %s: %w", l.path, err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("locking the spend ledger %s: %w", l.path, err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	now := l.now()
	since := now.Add(-24 * time.Hour)
	n := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(sc.Text()))
		if err != nil {
			// 🔴 AN UNREADABLE LINE COUNTS AS A REQUEST: a damaged ledger must not read as "nothing spent".
			n++
			continue
		}
		if t.After(since) {
			n++
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("reading the spend ledger %s: %w", l.path, err)
	}
	if n >= l.cap {
		return ErrCapped
	}
	if _, err := f.WriteString(now.UTC().Format(time.RFC3339Nano) + "\n"); err != nil {
		return fmt.Errorf("writing the spend ledger %s: %w", l.path, err)
	}
	return f.Sync()
}
