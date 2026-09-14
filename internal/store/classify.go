package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"
)

// =============================================================================
// 🔴 THE TOTAL CLASSIFIER — why this exists instead of a fourth `if` arm.
//
// Four consecutive audit rounds found the same shape of defect in the Python
// `/snapshot`, and every fix added one more predicate to a sequence:
//
//	r1  entry symlinks followed          -> refuse symlinked ENTRIES
//	r2  symlinked SCOPE dirs filtered    -> silently omitted, read as scope-empty
//	r3  is_symlink() before is_dir()     -> a symlinked README 503'd everything
//	r4  is_dir() first                   -> a DANGLING scope link vanished,
//	                                        read as scope-empty at exit 0
//
// Each round the stated predicate ("refuse a thing that IS a scope but cannot be
// served safely; skip a thing that is not one") failed to DECIDE the next input
// class, because a broken pointer is neither. Adding an arm fixes the instance; it
// does not make the rule total, so the next class falls through the same gap.
//
// So: classify the path's TYPE exhaustively, in ONE place, and have each context
// map EVERY kind to an action explicitly. An unmapped kind is a BUG that raises
// rather than defaulting — a fallthrough is a test failure, not a silent skip.
// Name-based rules (dotfiles, the `.md` suffix) stay SEPARATE from type, because
// conflating them is what made the dotfile and symlink rules interfere.
// =============================================================================

// Kind is a path's type, as exactly one of the nine values below.
type Kind string

const (
	KindBrokenLink    Kind = "broken-link" // dangling target, or a symlink loop
	KindLinkToDir     Kind = "link-to-dir"
	KindLinkToFile    Kind = "link-to-file"
	KindLinkToOther   Kind = "link-to-other" // link to a fifo/socket/device
	KindDirectory     Kind = "directory"
	KindRegularFile   Kind = "regular-file"
	KindOther         Kind = "other" // fifo, socket, device…
	KindIndeterminate Kind = "indeterminate"
	KindAbsent        Kind = "absent"
)

// AllKinds is the closed set, so a table can be asserted complete over it.
//
// 🔴 `indeterminate` IS THE LAST CELL OF THE LOOP, and it is not decoration. The
// first version of this classifier was total over KIND STRINGS but not over
// KNOWLEDGE: "I could not determine what this is" is its own answer and must
// never share a bucket with "I know exactly what this is". Measured on the pinned
// interpreter: a child of a `0o600` directory makes every pathlib predicate RAISE
// PermissionError rather than return False, so a sequence built from them aborts
// mid-classification and takes the handler with it — no response, no audit line.
//
// `absent` is separate from `indeterminate` because the right ACTION differs: a
// file that vanished between the directory read and the stat is a benign race,
// not a hazard.
var AllKinds = []Kind{
	KindBrokenLink, KindLinkToDir, KindLinkToFile, KindLinkToOther,
	KindDirectory, KindRegularFile, KindOther, KindIndeterminate, KindAbsent,
}

// Action is what a context does with a kind.
//
// Skip  — not the thing we are looking for, so its absence is not a fact worth
//
//	reporting.
//
// Take  — use it.
// Refuse — it IS (or claims to be) the thing and we cannot serve it, which must
//
//	be REPORTED, never skipped: a skip renders as "nothing recorded".
type Action string

const (
	Skip   Action = "skip"
	Take   Action = "take"
	Refuse Action = "refuse"
)

// ClassifyPath returns the path's type, total by construction.
//
// 🔴 ONE `lstat`, THEN THE MODE BITS — not a sequence of convenience predicates.
// Those fail in TWO different ways and neither is usable here: they swallow some
// errnos (so "not a directory" and "no such path" become indistinguishable) and
// they surface the rest as errors (so a sequence can abort mid-classification).
// Reading the mode bits from one explicit `lstat` makes both cases answerable —
// a failure is an error we must CLASSIFY, not a false we might miss. It also
// halves the syscalls, which narrows the TOCTOU window between them.
func ClassifyPath(path string) Kind {
	st, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return KindAbsent
		}
		// EACCES on the parent, ESTALE, EIO… We do not know what this is, and
		// saying so is the point.
		return KindIndeterminate
	}
	if st.Mode()&fs.ModeSymlink == 0 {
		switch {
		case st.IsDir():
			return KindDirectory
		case st.Mode().IsRegular():
			return KindRegularFile
		}
		return KindOther
	}

	target, err := os.Stat(path) // follows the link
	if err != nil {
		// ENOENT = dangling, ELOOP = a cycle (measured on the Python side: a
		// self-loop, a mutual loop and a 45-link chain all give ELOOP). Both are
		// broken POINTERS, which is a fact we know; anything else means the stat
		// failed for a reason we cannot interpret, which is not.
		//
		// 🔴 THE SPLIT IS KILLABLE THROUGH THE LOADER and must not be collapsed:
		// `loaderEntryActions` REFUSES `broken-link` and TAKES `indeterminate`,
		// so a mutant that merges these two arms turns an editor lock file back
		// into a store-wide unreadable-store error. Errnos measured to land in
		// `indeterminate` rather than `broken-link`: ENOTDIR, ENAMETOOLONG,
		// EACCES.
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ELOOP) {
			return KindBrokenLink
		}
		return KindIndeterminate
	}
	switch {
	case target.IsDir():
		return KindLinkToDir
	case target.Mode().IsRegular():
		return KindLinkToFile
	}
	return KindLinkToOther
}

// ActionFor looks up the action, refusing to guess. An unmapped kind is a BUG,
// because a default is how the previous four rounds of this defect happened.
func ActionFor(kind Kind, actions map[Kind]Action) (Action, error) {
	action, mapped := actions[kind]
	if !mapped {
		return "", fmt.Errorf(
			"unclassified path kind %q: every kind must be mapped explicitly, because a default is how the last four rounds of this defect happened",
			string(kind))
	}
	return action, nil
}

// VisibleScopeSet turns one allowlist into the normalized set every narrowing
// site compares against.
//
// 🔴 ONE PLACE, because this predicate is spelled at three of them — the index
// loader, the result-shape narrowing in LoadStore, and `/snapshot`'s candidate
// list, which walks the store root directly and so cannot use the index at all.
// Open-coded at three sites it would be wrong at two of them in the same
// direction, and the direction that matters here is "wider than the caller's
// allowlist".
//
// 🔴 UNRESTRICTED IN, UNRESTRICTED OUT. `Unrestricted()` is the sentinel; an
// EMPTY allowlist returns an EMPTY SET, which is its OPPOSITE — nothing is
// visible. Every caller must ask `set.Unrestricted`, never "is the set empty";
// that asymmetry is the whole fail-closed direction of the scoped-token design.
type ScopeSet struct {
	Unrestricted bool
	names        map[string]struct{}
}

// Unrestricted is the ScopeSet a legacy (bare-token) principal resolves to.
func Unrestricted() ScopeSet { return ScopeSet{Unrestricted: true} }

// VisibleScopeSet folds an allowlist. A nil slice is NOT unrestricted — it is the
// empty set — because in Go a nil slice and an empty one are the same value and
// conflating "nobody set this" with "everything is visible" is the failure this
// type exists to make impossible. Unrestricted is reachable only by asking for it.
func VisibleScopeSet(scopes []string) ScopeSet {
	names := make(map[string]struct{}, len(scopes))
	for _, s := range scopes {
		names[NormalizeRef(s)] = struct{}{}
	}
	return ScopeSet{names: names}
}

// Allows answers whether one scope name is visible. The name is FOLDED before
// comparing, because the index key derived from a directory name is folded: a raw
// comparison drops a scope directory spelled `Kelp_Forest` out of an allowlist
// that names `kelp-forest` — the caller's OWN scope, silently emptied.
func (s ScopeSet) Allows(scope string) bool {
	if s.Unrestricted {
		return true
	}
	_, in := s.names[NormalizeRef(scope)]
	return in
}
