package arcs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// EnvJournal is the variable `-arc-journal` defaults from. A NEW name with no old spelling, so
// `internal/envalias` gains no pair (design decision 4). There is NO default path: unset means
// the arc routes answer `registrations-unconfigured`, the designed off state, and no image or
// Dockerfile changes.
const EnvJournal = "CAIRN_ARC_JOURNAL"

// InsideStoreError is the refusal to start: the journal path resolves inside the store root.
//
// `Noun` names WHICH journal, because more than one journal is placed by this rule (the sources
// journal, `internal/codesrc`, reuses it): a refusal naming the wrong journal sends the operator
// to edit the wrong variable. Empty is "arc journal", so every existing message is unchanged.
type InsideStoreError struct {
	Journal, Resolved, StoreRoot string
	Noun                         string
}

// defaultNoun is the journal `ResolveJournalPath` (no noun) is about.
const defaultNoun = "arc journal"

func (e *InsideStoreError) noun() string {
	if e.Noun == "" {
		return defaultNoun
	}
	return e.Noun
}

func (e *InsideStoreError) Error() string {
	return fmt.Sprintf("the "+e.noun()+" %s resolves to %s, which is INSIDE the store root %s. "+
		"Every directory at the store root — dot-prefixed or not — is enumerated as a scope by the "+
		"token-file authority, so a journal there (or its directory) could become a scope a bare row "+
		"reads. Put it on its own volume outside the store tree", e.Journal, e.Resolved, e.StoreRoot)
}

// ResolveJournalPath is the startup check behind operator decision Q2: the journal lives OUTSIDE
// the store tree, enforced rather than requested. It returns the fully symlink-resolved path the
// pod must use from then on, or an error the pod refuses to start on.
//
// 🔴 THE PREMISE IS MEASURED, NOT CITED: `tokenfile`'s
// `TestADotDirectoryAtTheStoreRootIsEnumeratedAsAScope` shows a `.arcs/` directory at the root
// becoming a scope a bare row reads. So "inside" is decided on RESOLVED paths — a journal named
// outside the root that reaches it through a symlink (on the file or on any parent) is inside —
// and the store root itself, as the journal path, is inside too.
//
// The rules, each a refusal: the store root must resolve; the journal's PARENT directory must
// exist and resolve (a path whose directory is missing is a typo, and creating directories is not
// this pod's job); an existing journal must resolve (a DANGLING symlink would make the first
// append create a file wherever it points, a place this check never saw) and must not be a
// directory; and the result must not be the root or under it.
//
// ⚠ WHAT IT CANNOT SEE: a symlink created at the resolved path AFTER startup. `Register` opens
// with `O_NOFOLLOW`, which refuses that case at the write rather than following it.
func ResolveJournalPath(storeRoot, journal string) (string, error) {
	return ResolveJournalPathNamed(defaultNoun, storeRoot, journal)
}

// ResolveJournalPathNamed is `ResolveJournalPath` for a journal other than the arc registry's —
// the SAME resolution (one rule for "inside the store tree", on both binaries and for every
// journal), with every message naming `noun` instead of "arc journal".
func ResolveJournalPathNamed(noun, storeRoot, journal string) (string, error) {
	root, err := filepath.Abs(storeRoot)
	if err == nil {
		root, err = filepath.EvalSymlinks(root)
	}
	if err != nil {
		return "", fmt.Errorf("the store root %s does not resolve (%v), so the "+noun+" cannot be checked against it", storeRoot, err)
	}
	abs, err := filepath.Abs(journal)
	if err != nil {
		return "", fmt.Errorf("the "+noun+" %s does not resolve: %v", journal, err)
	}
	var resolved string
	isDir := false
	_, lerr := os.Lstat(abs)
	switch {
	case lerr == nil:
		resolved, err = filepath.EvalSymlinks(abs)
		if err != nil {
			return "", fmt.Errorf("the "+noun+" %s is a symlink that does not resolve (%v) — the first registration would create a file wherever it points, which this check never saw", journal, err)
		}
		if target, statErr := os.Stat(resolved); statErr == nil && target.IsDir() {
			isDir = true
		}
	case errors.Is(lerr, fs.ErrNotExist):
		parent, perr := filepath.EvalSymlinks(filepath.Dir(abs))
		if perr != nil {
			return "", fmt.Errorf("the "+noun+"'s directory %s does not exist or does not resolve (%v) — mount its volume first", filepath.Dir(abs), perr)
		}
		resolved = filepath.Join(parent, filepath.Base(abs))
	default:
		return "", fmt.Errorf("the "+noun+" %s cannot be inspected: %v", journal, lerr)
	}
	// INSIDE IS DECIDED BEFORE "IS A DIRECTORY", so the store root named as the journal is
	// refused for the reason that matters rather than for being a directory.
	if inside(root, resolved) {
		return "", &InsideStoreError{Journal: journal, Resolved: resolved, StoreRoot: root, Noun: noun}
	}
	if isDir {
		return "", fmt.Errorf("the "+noun+" %s is a directory; it must name a FILE", journal)
	}
	return resolved, nil
}

// Inside is [inside], exported so the transcript store's directory check
// (`internal/transcript/archive`, plan decision 15: "refused inside the store root, the
// `internal/arcs/path.go` rule") is this rule rather than a second spelling of it.
func Inside(root, path string) bool { return inside(root, path) }

// inside is "path is root or a descendant of it", over two CLEAN absolute paths.
func inside(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
