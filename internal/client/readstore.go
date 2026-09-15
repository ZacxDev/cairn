// Package client is the Go port of the `cairn` CLI — the read-through client.
//
// 🔴 IT EXISTS TO DELETE THE SECOND RENDERER, NOT TO BE A SECOND CLIENT. Rewriting the
// server alone would leave two renderers in two languages that must agree byte-for-byte
// forever, with drift arriving as "a different order that reads as a stale cache" — no
// error, no missing entry. `internal/report` is the one renderer; the pod calls it through
// `report.Reader` and this package calls `report.Recall` / `report.Search` /
// `RenderText` / `ExitFor` directly. Byte-identity between pod output and local output is
// then a property of there being ONE implementation, rather than a discipline two
// implementations are held to.
//
// 🔴 THE READER KEEPS ITS NO-NETWORK PROPERTY; THIS PACKAGE OWNS THE NETWORK. Nothing in
// `internal/report` or `internal/store` opens a socket, and nothing here patches or forks
// them. A recall must not become a live HTTP call: a DNS blip, a suspended laptop or a
// cluster reconcile would turn "orient me" into an error, or — worse — into an empty
// screen that reads like "nothing recorded".
//
// ⚠ THE PYTHON CLIENT IS STILL SHIPPED AND IS STILL THE ORACLE. `tests/parity/` runs both
// against ONE cache root and diffs stdout, stderr and exit code per verb; until that gate
// has held over real use, this package is the second implementation and `cairn` is the
// first. Do not delete the Python client, its `lib/`, or its packaging on the strength of
// a green parity run — that is P8's step, not this one's.
package client

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// 🔴 THE CACHE PATH AND THE STAMP NAME HAVE EXACTLY ONE DEFINITION EACH, HERE, and the
// reason is a measured one on the Python side: they used to live in the client script, which
// made it the only thing that knew where a host reads the store from — and two host-local
// read surfaces went on reading the FROZEN pre-cutover mirror for a day because nothing they
// imported could tell them otherwise.
const (
	// SyncStamp is the snapshot stamp `sync` writes into the cache root.
	SyncStamp = ".sync-stamp"
	// Remedy is the one-command fix, spelled once so every refusal quotes the same thing.
	Remedy = "cairn sync"
	// StampPrefix is how a rendered stamp line is introduced, wherever a reader sees one.
	StampPrefix = "  stamp: "
)

// DefaultCacheRoot is the synced read-through cache `cairn sync` installs.
//
// ⚠ IT IS A FUNCTION, NOT A PACKAGE-LEVEL VALUE, and the difference is load-bearing in the
// same way the oracle's `read_store_root()` accessor is: resolving `$HOME` at init time
// binds the path before a test (or a wrapper) can repoint it, and the Python module's own
// docstring names that spelling as the one that defeats a repoint.
//
// 🔴 THERE IS NO ENV OVERRIDE, AND AN EARLIER DRAFT OF THIS PORT ADDED ONE. A `CAIRN_CACHE_ROOT`
// looked free — the parity harness wanted a cache root it could set without varying argv — and it
// was a CAPABILITY THE ORACLE DOES NOT HAVE. The gate caught it immediately and in exactly the
// place it matters: `doctor` resolves the READER's store through this function and not through
// `--cache`, so the Go client reported a different `reader-resolution` root from the Python one on
// every doctor row. It is deleted rather than mirrored into the Python client, because `--cache`
// already exists on every verb and a second mechanism reaching the same value is the shape that
// leaves the first one silently dead.
func DefaultCacheRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		// The oracle's `Path.home()` RAISES here, and a client that guessed `/` instead
		// would write a cache into somebody's root. Naming the absence is the honest
		// answer; every caller turns an empty root into a refusal.
		return ""
	}
	return filepath.Join(home, ".cache", "subsystem-store")
}

// expandUser is `Path.expanduser()` for the one form that appears in configuration: a
// leading `~` or `~/`. A `~user` form is NOT expanded — Go has no portable passwd lookup
// and silently treating `~alice/x` as a relative directory named `~alice` is the shape of
// bug that writes a cache somewhere nobody looks.
func expandUser(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == "~" {
		return home
	}
	return filepath.Join(home, p[2:])
}

// ReadStore is the resolved host-local read store, and whether it can date itself.
//
// `Stamp` holds the stamp file's non-blank lines with TRAILING WHITESPACE STRIPPED, and
// nothing else done to them — no parsing, no reordering, no interpretation. The strip is
// what keeps a `\r` from a CRLF write out of a rendered header.
//
// `Reason` says why there is no stamp and is empty exactly when `Stamped` is true.
type ReadStore struct {
	Root    string
	Stamp   []string
	Stamped bool
	Reason  string
}

// ReadStamp is `(lines, reason-there-are-none)` — exactly one is meaningful.
//
// READ-ONLY and clock-free: it opens one file, splits it, drops blank lines and strips each
// line's TRAILING whitespace. That is the whole normalisation. An unreadable or EMPTY stamp
// is reported as ABSENT, never as a stamp with no fields: "the store is stamped" must not be
// satisfiable by a zero-byte file.
func ReadStamp(root string) (lines []string, reason string) {
	path := filepath.Join(root, SyncStamp)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Sprintf("no `%s` in %s", SyncStamp, filepath.Dir(path))
		}
		return nil, fmt.Sprintf("`%s` could not be read: %s", path, pyOSError(err))
	}
	// ⚠ NO DECODE ERROR ARM, AND THAT IS A DELIBERATE NARROWING. The oracle distinguishes
	// "could not be read" from "is not text" because `read_text` raises
	// `UnicodeDecodeError`; Go's `ReadFile` returns bytes and never fails on encoding. The
	// stamp is written by this program and is ASCII, so the arm is unreachable in practice
	// — but it IS a difference in what a hand-corrupted stamp prints, and it is named here
	// rather than papered over with a re-validation the oracle does not do.
	for _, line := range splitLines(string(data)) {
		if strings.TrimSpace(line) == "" {
			continue
		}
		lines = append(lines, strings.TrimRight(line, " \t\r\n\v\f"))
	}
	if len(lines) == 0 {
		return nil, fmt.Sprintf("`%s` is empty", path)
	}
	return lines, ""
}

// ResolveReadStore resolves the read store and reads its stamp in one call.
//
// An empty `root` is the DEFAULT resolution — the synced cache. Naming a root explicitly is
// the operator being deliberate; this function still reports whether that directory is
// stamped and leaves the decision to the caller, because "the default resolved somewhere
// undateable" and "you asked me to read this" warrant different answers.
func ResolveReadStore(root string) ReadStore {
	resolved := root
	if resolved == "" {
		resolved = DefaultCacheRoot()
	}
	lines, reason := ReadStamp(resolved)
	return ReadStore{Root: resolved, Stamp: lines, Stamped: lines != nil, Reason: reason}
}

// StampFields parses `key=value` stamp lines. The stamp's SCHEMA is owned by whoever writes
// it, so this is the one reader that interprets it, and it is used for the rendered
// freshness sentence only.
func StampFields(lines []string) map[string]string {
	out := map[string]string{}
	for _, line := range lines {
		key, value, _ := strings.Cut(line, "=")
		out[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return out
}

// splitLines is `str.splitlines()` for the subset that matters here: `\n`, `\r\n` and a
// trailing newline that does NOT produce a final empty element.
//
// ⚠ IT IS NOT `str.splitlines()` IN FULL — that splits on U+000B, U+000C, U+001C..U+001E,
// U+0085 and U+2028/9 as well. None of those can appear in a stamp this program wrote, and
// the difference is stated rather than silently inherited.
func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if s == "" {
		return nil
	}
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n")
}
