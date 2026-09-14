package write

import (
	"os"
	"path/filepath"
	"syscall"
)

// entryLock is mutual exclusion for one entry file, held across a
// read-modify-write.
//
// 🔴 THE LOCK IS ON A SEPARATE FILE, AND THAT IS NOT AN OVERSIGHT. The write
// itself is a temp-file-plus-rename, so the entry file's INODE changes on every
// append: a second writer that had locked the entry file directly would be holding
// a lock on an inode nobody is looking at any more, and both writers would proceed.
// A stable side file is the only thing both writers can agree on across a rename.
//
// The lock file is named `.<entry>.lock` — a leading dot AND no `.md` suffix, so it
// is invisible to all three of the store's walkers twice over (the index loader
// takes `*.md`, `/snapshot` skips dotfiles and requires `.md`, and the freshness
// walk counts `.md` only).
type entryLock struct {
	file *os.File
}

func lockEntry(entryPath string) (*entryLock, error) {
	dir, name := filepath.Split(entryPath)
	lockPath := filepath.Join(dir, "."+name+".lock")
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		file.Close()
		return nil, err
	}
	return &entryLock{file: file}, nil
}

func (l *entryLock) unlock() {
	// The unlock is best-effort and the close is not: closing the descriptor
	// releases the flock either way, so a failed explicit unlock cannot leave the
	// lock held.
	_ = syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	_ = l.file.Close()
}

// fsyncDir syncs a DIRECTORY, so a rename inside it survives a crash.
//
// Best-effort: a filesystem that refuses a directory handle (or a store on one that
// has no such concept) must not turn a completed write into a 503. The bytes are
// already persisted by the file sync in replaceBytes; what is at risk here is only
// the rename's own metadata, and refusing to serve because we could not flush it
// would trade a rare durability gap for a certain availability one.
func fsyncDir(dir string) {
	handle, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = handle.Sync()
	_ = handle.Close()
}

// replaceBytes writes `data` to `path` so a reader sees the OLD file or the NEW one.
//
// 🔴 NEVER A TRUNCATE-THEN-WRITE. That leaves a window in which the entry is EMPTY
// or half-written, and a concurrent read then serves a truncated entry as a complete
// one — the silent under-report this whole design is built against, produced by the
// writer instead of the reader. `Rename` is atomic within a filesystem, and the temp
// file is created in the SAME directory precisely so it is on that filesystem.
//
// 🔴 TWO SYNCS, AND THE SECOND ONE IS NOT REDUNDANT. Syncing the FILE persists the
// bytes; the RENAME lives in the parent DIRECTORY and is a separate piece of
// metadata with its own writeback. Without the directory sync a node that loses
// power after the rename returns can come back with the old name still pointing at
// the old inode — the append is gone, and the client was told `200 appended`.
// Atomicity is a claim about what a concurrent READER can see; durability is a claim
// about what survives a crash, and only the first of those the rename gives you for
// free.
func replaceBytes(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".cairn-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() {
		// A failed write must not leave a `.cairn-*.tmp` behind: it is invisible to
		// every reader, so nothing would ever report it and nothing would ever
		// clean it up.
		_ = os.Remove(tmpName)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return err
	}
	fsyncDir(filepath.Dir(path))
	return nil
}

// createBytes materialises `path` with `data`, or reports that it already exists.
// It NEVER clobbers.
//
// 🔴 THE EXCLUSIVITY IS THE FILESYSTEM'S, NOT A CHECK THIS PROCESS MAKES. An
// existence test followed by a write is a TOCTOU: two concurrent creates both see
// nothing and the second silently destroys the first, which is the same lost update
// `If-Match` exists to refuse — arriving through the one verb that has no prior
// revision to name. `link` fails with EEXIST when the target is there, and it does so
// in the kernel, so exactly one of two racing creates can win. The per-entry lock is
// held as well; this is what holds when the other writer never took that lock (the
// seed script, an operator's editor, a second pod).
//
// 🔴 AND IT IS A LINK FROM A FULLY-WRITTEN TEMP FILE, NOT AN EXCLUSIVE CREATE
// DIRECTLY AT `path`. A direct exclusive create claims the name and only then starts
// writing, so a concurrent read can glob a `*.md` that is EMPTY or half-written and
// serve it as a MALFORMED entry — replaceBytes' partial-read hazard, reproduced by
// the create path. Linking a complete, synced file publishes the name and the bytes
// in one atomic step, so a reader sees the entry or sees nothing.
//
// ⚠ IT REQUIRES HARD LINKS, and that is a stated dependency rather than an
// assumption: creating the temp file proves exclusive create works on the store
// volume, `link` is not otherwise exercised there, and this has NOT been run against
// a deployed PVC. A filesystem that refuses it returns an error the route answers
// 503 with — loud, and never a silent overwrite.
//
// `interleave` is a deterministic seam called between "the bytes are ready" and "the
// name is claimed". It is a no-op in every deployment; a concurrent-append test
// driven by wall-clock timing proves nothing on the run where the two threads happen
// not to overlap, and the defect it guards against DESTROYS CONTENT rather than
// availability — so the overlap has to be forced, not hoped for.
func createBytes(path string, data []byte, interleave func()) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".cairn-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// The temp file is removed either way: it is a dotfile with no `.md` suffix, so
	// no walker would ever report one left behind.
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	if interleave != nil {
		interleave()
	}
	if err := os.Link(tmpName, path); err != nil {
		return err
	}
	fsyncDir(filepath.Dir(path))
	return nil
}
