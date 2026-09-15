// Package doctor is `cairn doctor` — one call that answers what a reader otherwise checks by
// hand.
//
// 🔴 WHY IT EXISTS. Every fact below was previously established by a human running four or
// five commands and holding the results in their head: is the pod up, is my token any good, is
// my cache stamped, does my cache agree with the pod, is anything on this disk invisible to my
// credential, and does this host's READER resolve to the synced cache or to the frozen
// pre-cutover mirror. Nothing joined them, so the answers were assembled differently every
// time and the joins that matter — cache count vs pod count, local scopes vs visible scopes —
// were the ones nobody made.
//
// 🔴 FOUR STATES, AND THE FOURTH IS THE WHOLE POINT. This subsystem's recurring defect is a
// reassuring zero: an output that cannot distinguish "there is nothing there" from "I could not
// look". `NOT-OBSERVABLE` is separate from `UNMEASURED` on purpose — folding the two would make
// the exit code permanently non-zero, and a permanently-red gate is worse than no gate because
// it trains everyone to click through.
//
// 🔴 DOCTOR NEVER INSTALLS A SNAPSHOT. It fetches, reads the headers and the member list, and
// throws the bytes away. A diagnostic that repaired the cache as a side effect would destroy
// the staleness it was run to measure — and it would be the one command you must not run twice.
//
// 🔴 THE HOLE IT CANNOT CLOSE, STATED RATHER THAN LEFT TO BE FOUND. A scope this host holds but
// the pod's snapshot does not contain is EITHER a scope the store has never held OR a scope
// outside this token's allowlist. The API answers those two identically **by design**, so no
// client can tell them apart and this package does not pretend to. It reports the set and names
// both readings.
package doctor

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The four verdicts a check may carry. Written out once; `Render` and `ExitCode` both read
// `States` rather than restating the list.
const (
	OK            = "OK"
	Problem       = "PROBLEM"
	Unmeasured    = "UNMEASURED"
	NotObservable = "NOT-OBSERVABLE"
)

// States is the vocabulary, in the order `Render`'s count line prints it.
var States = []string{OK, Problem, Unmeasured, NotObservable}

// TokenFingerprintChars is how many hex characters of `sha256(token)` identify a credential.
//
// 🔴 IT MIRRORS THE SERVER'S `token_id`, WHICH IS WHAT THE POD'S AUDIT LOG CARRIES. The value
// is what makes the fingerprint printed here matchable against a `token=<id>` line in the pod's
// log, so the two agreeing is a wire fact and not an implementation detail.
const TokenFingerprintChars = 12

// TokenFingerprint is a stable, non-reversible handle for a credential. NEVER the token.
func TokenFingerprint(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])[:TokenFingerprintChars]
}

// Check is one diagnosed fact.
//
// `Detail` is mandatory and is not decoration: for `UNMEASURED` it carries the reason, and for
// `NOT-OBSERVABLE` it carries the command that CAN answer it. 🔴 A STATE WITH AN EMPTY DETAIL
// IS REFUSED AT CONSTRUCTION, because a bare `UNMEASURED` is the reassuring zero wearing a
// different word.
type Check struct {
	Name   string
	State  string
	Detail string
}

func newCheck(name, state, detail string) Check {
	if !contains(States, state) {
		panic(fmt.Sprintf("unknown check state %q; expected one of %v", state, States))
	}
	if strings.TrimSpace(detail) == "" {
		panic(fmt.Sprintf("check %q has an empty detail — a state with no reason cannot "+
			"be acted on", name))
	}
	return Check{Name: name, State: state, Detail: detail}
}

// PodFacts is what one non-installing snapshot fetch established, or why it did not.
//
// 🔴 `Reached` IS NOT DERIVED FROM THE COUNTS. A store that genuinely holds zero entries and a
// fetch that never happened both leave the counts at 0, so the counts may only be read when
// `Reached` is true — and every consumer below branches on `Reached` first.
//
// 🔴 THE REASON IS ENFORCED, NOT MERELY DOCUMENTED. A `PodFacts{Reached: false}` with no reason
// renders `the store's state could not be established — .` and walks straight past `Check`'s
// own guard, because the empty string is wrapped in literal text before it gets there.
type PodFacts struct {
	Reached bool
	Reason  string
	// HTTPStatus is the status when the server ANSWERED but refused. Zero when there was no
	// answer at all, which is what separates unauthorised from unreachable without parsing a
	// message.
	HTTPStatus int
	// VisibleEntries is `X-Store-Entries` — what THIS token's snapshot contained. A nil
	// pointer is UNMEASURED; a pointer to 0 is a measured zero.
	VisibleEntries *int
	// StoreWideEntries is `entry-files=` out of `X-Store-Snapshot` — the STORE-WIDE total,
	// which the server emits unfiltered (a documented, deliberate residual count leak).
	StoreWideEntries *int
	VisibleScopes    []string
	SnapshotHeader   string
}

// Validate enforces the mandatory reason. It returns an error rather than panicking because its
// one caller is a command, and a command that crashed here would be a diagnostic tool dying on
// its own diagnosis.
func (p PodFacts) Validate() error {
	if !p.Reached && strings.TrimSpace(p.Reason) == "" {
		return errors.New("PodFacts{Reached: false} requires a reason — an unmeasured " +
			"store with no stated cause is the silent zero this module exists to prevent")
	}
	return nil
}

// --------------------------------------------------------------------------- //
// Disk facts. Each one is a plain function so a test can drive it directly.
// --------------------------------------------------------------------------- //

// scopeDirs is `<root>/<scope>/` directories, dot-directories excluded.
//
// It returns an error rather than an empty list on an unreadable root: an empty list and an
// unreadable directory are the two things this whole package exists to keep apart, so the
// failure has to reach a caller that can name it.
func scopeDirs(root string) ([]string, error) {
	items, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, item := range items {
		if strings.HasPrefix(item.Name(), ".") {
			continue
		}
		full := filepath.Join(root, item.Name())
		// `os.Stat`, not `item.IsDir()`: a SYMLINK to a directory is a directory to the
		// oracle's `Path.is_dir()` and is not to `DirEntry.IsDir()`. That difference decides
		// whether a symlinked scope is counted.
		info, statErr := os.Stat(full)
		if statErr != nil || !info.IsDir() {
			continue
		}
		out = append(out, full)
	}
	sort.Strings(out)
	return out, nil
}

// StoreScopes is the scope names a store root holds.
func StoreScopes(root string) ([]string, error) {
	dirs, err := scopeDirs(root)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(dirs))
	for _, d := range dirs {
		out = append(out, filepath.Base(d))
	}
	return out, nil
}

// StoreEntryFiles is `<scope>/<entry>.md` files under a store root.
//
// The SAME shape the server's freshness stamp counts — depth 2, `*.md`, no dot-directories and
// no dot-files — so this number and the pod's `entry-files=` are answers to one question rather
// than two.
func StoreEntryFiles(root string) (int, error) {
	dirs, err := scopeDirs(root)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, dir := range dirs {
		items, err := os.ReadDir(dir)
		if err != nil {
			return 0, err
		}
		for _, item := range items {
			name := item.Name()
			if strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".md") {
				continue
			}
			info, statErr := os.Stat(filepath.Join(dir, name))
			if statErr != nil || !info.Mode().IsRegular() {
				continue
			}
			total++
		}
	}
	return total, nil
}

// WritableEntryFiles is the entry files under `root` that any mode bit still allows WRITING.
//
// 🔴 THE FREEZE IS A PROPERTY OF EVERY FILE, NOT OF THE DIRECTORY. A partially-frozen mirror
// still accepts a write, and that write then lives on one host and is invisible to the pod —
// the exact stranding the cutover exists to prevent. So this counts FILES, and a non-empty
// answer is a PROBLEM even when the directory looks frozen.
func WritableEntryFiles(root string) ([]string, error) {
	dirs, err := scopeDirs(root)
	if err != nil {
		return nil, err
	}
	var loose []string
	for _, dir := range dirs {
		items, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(items))
		for _, item := range items {
			names = append(names, item.Name())
		}
		sort.Strings(names)
		for _, name := range names {
			if !strings.HasSuffix(name, ".md") {
				continue
			}
			info, statErr := os.Stat(filepath.Join(dir, name))
			if statErr != nil || !info.Mode().IsRegular() {
				continue
			}
			if info.Mode().Perm()&0o222 != 0 {
				loose = append(loose, filepath.Base(dir)+"/"+name)
			}
		}
	}
	return loose, nil
}

// reading is one disk fact, or the structured reason there is none.
//
// 🔴 `absent` IS A FLAG, NOT A SENTENCE. The first version had callers ask whether the reason
// ended in "does not exist" to tell a missing directory from an unreadable one — a guard
// SPELLED rather than STRUCTURAL: reword the message and the branch silently stops firing, with
// an unreadable mirror then reported as "nothing pre-cutover on this host".
type reading struct {
	scopes []string
	count  int
	loose  []string
	reason string
	absent bool
}

func (r reading) ok() bool { return r.reason == "" }

// describe reads one fact off disk, or says why not. Never a silent zero.
//
// 🔴 `absent` MEANS THE ROOT IS ABSENT — it is RE-CHECKED, not inferred from the error. Mapping
// any not-exist error to `absent` is wrong in a way that produces a confident OK: a file
// vanishing MID-WALK raises the same error, and this store has two writers that do exactly that
// — an hourly autocommit, and `cairn sync`, which replaces the cache root by rename. The report
// would have read `frozen-mirror OK … does not exist` for a directory that was right there.
func describe(root string, what func(string) (reading, error)) reading {
	r, err := what(root)
	if err == nil {
		return r
	}
	if errors.Is(err, fs.ErrNotExist) {
		if _, statErr := os.Stat(root); statErr != nil && errors.Is(statErr, fs.ErrNotExist) {
			return reading{reason: root + " does not exist", absent: true}
		}
		return reading{reason: fmt.Sprintf("%s exists but something under it vanished "+
			"while it was being read (%s) — a concurrent writer, not an absent store",
			root, pyOSError(err))}
	}
	return reading{reason: fmt.Sprintf("%s could not be read: %s", root, pyOSError(err))}
}

func readScopes(root string) (reading, error) {
	scopes, err := StoreScopes(root)
	return reading{scopes: scopes}, err
}

func readEntryFiles(root string) (reading, error) {
	n, err := StoreEntryFiles(root)
	return reading{count: n}, err
}

func readWritable(root string) (reading, error) {
	loose, err := WritableEntryFiles(root)
	return reading{loose: loose}, err
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
