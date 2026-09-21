package identity

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// FileSessionStore is the durable session table: one JSON object per line, rewritten
// whole on every mutation.
//
// 🔴 IT REWRITES WHERE `control.FileStore` APPENDS, AND THAT IS THE ONE PLACE THE TWO
// DIVERGE. The journal is append-only because the authority it holds IS append-only — "a
// grant log that could be UPDATE'd would not be a grant log". A session table is the
// opposite: its whole content is the set of sessions that are live RIGHT NOW, it is
// authoritative about nothing historical, and the highest-rate event it sees is a logout,
// which in an append-only file is a tombstone. Appending would mean a file that grows
// without bound at login rate, a replay that gets slower forever, and a revocation that
// is a later line SHADOWING an earlier one rather than an absence. Rewriting makes a
// revoked session GONE from disk, which is the property the whole storage decision was
// made for.
//
// 🔴 SO THE LOCK IS A SIDE FILE, AND THAT FOLLOWS FROM THE REWRITE RATHER THAN BEING A
// STYLE CHOICE. `control.FileStore` locks the journal itself and explains why it may: it
// is only ever appended to, so its inode is stable. A temp-file-plus-rename changes the
// inode, so a second writer holding a lock on the old one would be holding a lock on a
// file nobody is looking at any more and both writers would proceed. `internal/write`
// reached the same conclusion for entry writes, for the same reason, and this is the
// third site rather than a fourth answer.
//
// ⚠ WHAT IT IS NOT SUITED FOR, said plainly: a session table large enough that reading it
// per request costs something, and replicas that do not share a filesystem. Both are the
// same limits `control.FileStore` already declares, and both end at the same place — the
// second `control.Store` implementation.
type FileSessionStore struct {
	path     string
	lockPath string

	// Now is the clock. Injected so expiry is testable without sleeping; nil means
	// `time.Now().UTC()`.
	Now func() time.Time

	// mu serialises this PROCESS's writers before they queue on the file lock. The
	// file lock is what serialises across processes; this one is what keeps two
	// goroutines in one process from racing on the read-modify-write between the
	// `flock` and the rename.
	mu sync.Mutex
}

// OpenFileSessionStore opens (and creates if absent) a session table at path.
//
// The file is 0600 and its directory 0700, for the reason `control.OpenFileStore` gives
// about the journal: these are not secrets in the sense a token is — the ids are hashed —
// but the file names every principal with a live browser session, and that is not
// world-readable material.
func OpenFileSessionStore(path string) (*FileSessionStore, error) {
	if path == "" {
		return nil, fmt.Errorf("session store path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("session store directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("session store: %w", err)
	}
	f.Close()
	return &FileSessionStore{path: path, lockPath: path + ".lock"}, nil
}

// Path is where this store's table lives. For error messages an operator reads, never
// for one that reaches the wire.
func (s *FileSessionStore) Path() string { return s.path }

func (s *FileSessionStore) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}

var _ SessionStore = (*FileSessionStore)(nil)

// Lookup resolves a presented id to a live session.
//
// 🔴 IT RE-READS THE FILE ON EVERY CALL, WITH NO CACHE, AND THE COST IS ACCEPTED
// DELIBERATELY. `control.FileStore.Model` reached the same ruling after a measured
// defect: a process-local projection invalidated only by its OWN writes cannot see
// another process's write, and here that other process is the one that revoked a session.
// A cached session table would make logout work only for the replica that served it,
// which is precisely the property this design was chosen for.
//
// 🔴 IT TAKES NO LOCK, AND THAT IS SAFE ONLY BECAUSE WRITES ARE RENAMES. A reader opening
// the path gets one complete version of the file — the old one or the new one, never a
// half-written one — which is the second thing the rewrite-plus-rename buys beyond the
// bounded size. A reader against an appending writer would have no such guarantee.
//
// 🔴 THE SCAN DOES NOT SHORT-CIRCUIT. Returning at the first matching digest would make
// the response time carry the matched record's POSITION in the file, which is a fact
// about the store rather than about any one digest; see [digestsEqual] for why the
// comparison itself is the weaker of the two properties.
//
// ⚠ HOW THAT IS MEASURED, AND WHAT IT COSTS, BECAUSE THE ANSWER CHANGED. It used to be a
// per-comparison counter field on this struct, read by a test that probed three positions
// — an instrument in the serving path, carried by production for one test. It is now
// `TestTheLookupScanHasNoEarlyExit`, a STRUCTURAL guard that parses this file and refuses
// a `break`/`continue`/`goto`/`return` inside the loop below. It pins the loop's ITERATION
// COUNT, which is the leak the counter measured. ⚠ What it does NOT pin is the per-iteration
// COST: a body whose work depends on whether this record matched would leak the same fact
// with no branch statement anywhere. That half is [digestsEqual]'s, and its own guard's.
func (s *FileSessionStore) Lookup(presentedID string) (Session, bool, error) {
	if presentedID == "" {
		return Session{}, false, nil
	}
	records, err := s.read()
	if err != nil {
		return Session{}, false, err
	}
	want := SessionDigest(presentedID)
	now := s.now()

	var found Session
	hit := false
	for _, rec := range records {
		// No `break`, no early `return`: see the method comment. The match is recorded
		// and the loop runs to completion over every record in the file, and
		// `TestTheLookupScanHasNoEarlyExit` fails if a branch statement appears here.
		if digestsEqual(rec.Digest, want) && rec.Live(now) {
			found = rec
			hit = true
		}
	}
	return found, hit, nil
}

// Create stores one new session.
//
// Expired records are dropped in the same write. Pruning on write rather than on a timer
// means the file's size is bounded by LIVE sessions without a background goroutine whose
// failure would be silent.
func (s *FileSessionStore) Create(rec Session) error {
	if rec.Digest == "" {
		return fmt.Errorf("session store: a session with no digest would be unreachable and would never expire")
	}
	return s.mutate(func(records []Session) []Session {
		// A digest already present is replaced rather than duplicated. Two rows for one
		// digest would make `Revoke` remove one and leave the other — a logout that
		// reports success and revokes nothing, which is the exact failure this store
		// exists to prevent.
		out := make([]Session, 0, len(records)+1)
		for _, existing := range records {
			if !digestsEqual(existing.Digest, rec.Digest) {
				out = append(out, existing)
			}
		}
		return append(out, rec)
	})
}

// Revoke removes the session a presented id names. This is the logout.
//
// 🔴 AN ID WITH NO SESSION IS NOT AN ERROR. A browser holding a cookie whose session has
// already expired, or has been revoked from another tab, must still be able to complete a
// logout — otherwise the one action that clears a stale credential is the one action that
// fails while the credential is stale.
func (s *FileSessionStore) Revoke(presentedID string) error {
	if presentedID == "" {
		return nil
	}
	gone := SessionDigest(presentedID)
	return s.mutate(func(records []Session) []Session {
		out := make([]Session, 0, len(records))
		for _, rec := range records {
			if !digestsEqual(rec.Digest, gone) {
				out = append(out, rec)
			}
		}
		return out
	})
}

// mutate is the read-modify-write, and it is the ONE place the file is written.
//
// The sequence is the one `control.FileStore.Append` established: take the exclusive
// lock, re-read UNDER it rather than trusting anything in memory, apply, write, `Sync`
// before reporting success. The cache this store does not have is the reason the re-read
// is cheap to insist on.
func (s *FileSessionStore) mutate(apply func([]Session) []Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	lock, err := os.OpenFile(s.lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("session store lock: %w", err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("session store lock: %w", err)
	}
	// Best-effort unlock: closing the descriptor releases it regardless, so a failed
	// explicit unlock cannot leave it held.
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)

	// Re-read UNDER the lock. Anything read before it could have been replaced by
	// another process between the read and the write, and a session table written from
	// a stale read RESURRECTS every session revoked in that window.
	records, err := s.read()
	if err != nil {
		return err
	}

	now := s.now()
	live := make([]Session, 0, len(records))
	for _, rec := range records {
		if rec.Live(now) {
			live = append(live, rec)
		}
	}
	return s.write(apply(live))
}

// read parses the table. A line that will not parse is REFUSED rather than skipped.
//
// 🔴 SKIPPING A BAD LINE IS THE DANGEROUS DIRECTION HERE, WHICH IS THE OPPOSITE OF
// `control.FileStore`'s ruling and the difference is worth stating. There, an unreadable
// journal degrades to the last known-good model, because an empty authority authorises
// nobody and that turns a parse error into a total outage. Here the content is a set of
// LIVE CREDENTIALS and the next write rewrites the whole file: a skipped line is a
// session silently dropped from the table on the next write — a user signed out with no
// error anywhere — and, worse, a line skipped by the reader but still counted by nothing
// means a `Revoke` could report success having rewritten a file that never contained the
// record it was asked to remove.
func (s *FileSessionStore) read() ([]Session, error) {
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			// The file is created by `OpenFileSessionStore`; its absence here means it
			// was removed underneath us. An empty table is the correct reading — every
			// session is refused, which is the fail-closed direction — and it is not an
			// error, because the next `Create` recreates the file.
			return nil, nil
		}
		return nil, fmt.Errorf("session store: %w", err)
	}
	defer f.Close()

	var out []Session
	scanner := bufio.NewScanner(f)
	line := 0
	for scanner.Scan() {
		line++
		raw := scanner.Bytes()
		if len(raw) == 0 {
			continue
		}
		var rec Session
		if err := json.Unmarshal(raw, &rec); err != nil {
			// The line's CONTENT is not in the message: it is a session record, and the
			// message goes to an operator's log.
			return nil, fmt.Errorf("session store %s: line %d does not parse", s.path, line)
		}
		out = append(out, rec)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("session store %s: %w", s.path, err)
	}
	return out, nil
}

// write replaces the table atomically: temp file, `Sync`, rename, then `Sync` the
// directory so the rename itself is durable.
//
// 🔴 THE DIRECTORY `Sync` IS NOT REDUNDANT WITH THE FILE ONE. Syncing the temp file makes
// its CONTENTS durable; the rename is a directory metadata change, and on a crash between
// the two a filesystem is free to have the old file back with the new one's bytes
// unreferenced. `control.FileStore.Append` needs no equivalent because it appends to a
// file whose name never changes.
func (s *FileSessionStore) write(records []Session) error {
	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, ".sessions-*")
	if err != nil {
		return fmt.Errorf("session store: %w", err)
	}
	tmpName := tmp.Name()
	// If anything below fails the temp file must not be left behind: it is a second
	// copy of the live session table sitting in the same directory.
	defer os.Remove(tmpName)

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("session store: %w", err)
	}
	enc := json.NewEncoder(tmp)
	for _, rec := range records {
		if err := enc.Encode(rec); err != nil {
			tmp.Close()
			return fmt.Errorf("session store: %w", err)
		}
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("session store sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("session store: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("session store rename: %w", err)
	}
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("session store directory: %w", err)
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("session store directory sync: %w", err)
	}
	return nil
}
