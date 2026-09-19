package control

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// Source is the READ half of an authority backend: a consistent point-in-time
// Model.
//
// 🔴 THE INTERFACE EXISTS BECAUSE A SECOND BACKEND IS PLANNED, NOT BECAUSE
// ABSTRACTION IS A VIRTUE. The control plane's durable authority is intended to
// move to Postgres once identity does — the two arrive together, because a user row
// is what an identity provider resolves to. Retrofitting a seam around a vendor
// client after the fact is the expensive version; declaring the seam now, with one
// implementation behind it, is the cheap one. The shape is deliberately the
// smallest thing both an append-only file and a set of SQL tables can satisfy: give
// me the whole world, and let me append to it.
//
// ⚠ WHAT THIS SEAM DOES NOT PROMISE: a partial or streaming read. Every backend
// hands over the entire Model. That is affordable because the control plane is
// small by construction — users, projects, scopes and grants, not entries — and it
// is what makes the materialized cache a single comparable value with an epoch
// rather than a set of independently-stale queries.
type Source interface {
	Model(ctx context.Context) (Model, error)
}

// Writer is the MUTATING half.
//
// It returns the Model the append produced, so a caller never has to do a
// read-after-write to learn the new epoch — a read-after-write against a backend
// with any replication at all is the classic way to observe a state older than the
// one you just created.
type Writer interface {
	Append(ctx context.Context, events ...Event) (Model, error)
}

// Store is both halves.
type Store interface {
	Source
	Writer
}

// FileStore is the append-only journal on local disk.
//
// 🔴 A FILE RATHER THAN AN EMBEDDED DATABASE, AND THE CONSTRAINT IS `go.mod`. This
// module has no `require` block and `flake.nix` passes `vendorHash = null`, which
// together make a new dependency in the serving path a BUILD FAILURE rather than a
// silent addition. Every embedded SQL engine for Go is a third-party dependency, so
// the choice was between relaxing a stated property of the deployed pod and writing
// the smallest durable thing the standard library can express. An append-only
// journal is that thing — and it is not a compromise here, because the authority
// this stores is append-only by design: a grant log that could be UPDATE'd would
// not be a grant log.
//
// ⚠ WHAT IT IS NOT SUITED FOR, said plainly so nobody discovers it later: many
// concurrent writers across many processes, and a journal large enough that
// replaying it on every open becomes a cost. Neither is true of a control plane
// with one pod and human-rate writes. Both become true at a scale where the
// Postgres backend behind `Store` is the answer, which is why the seam exists
// before the pressure does.
type FileStore struct {
	path string

	// Now is the clock. Injected so tests can pin timestamps; nil means
	// `time.Now`.
	Now func() time.Time

	mu     sync.RWMutex
	cached Model
	loaded bool
}

// OpenFileStore opens (and creates if absent) a journal at path.
//
// The file is created with mode 0600: it holds credential DIGESTS, which are not
// secrets in the sense a token is, but it also holds the complete membership and
// sharing graph of every tenant, and that is not world-readable material.
func OpenFileStore(path string) (*FileStore, error) {
	if path == "" {
		return nil, fmt.Errorf("control journal path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("control journal directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("control journal: %w", err)
	}
	f.Close()
	return &FileStore{path: path}, nil
}

// Path is where this store's journal lives. For `doctor` and for error messages.
func (s *FileStore) Path() string { return s.path }

func (s *FileStore) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}

// Model returns the current world, re-reading the journal every time.
//
// 🔴 IT IS `Reload`, UNCONDITIONALLY, AND THE SHORT-CIRCUIT THAT USED TO BE HERE WAS
// DELETED RATHER THAN DOCUMENTED. This method served a process-local projection
// invalidated only by THIS VALUE'S own appends, and the comment above it claimed that was
// "correct for the deployed shape — one pod owns the file". Measured: the deployed shape
// has TWO processes — the pod reads, `cairn-server -create-user` writes through
// `kubectl exec` — so a pod reading through the short-circuit would materialize the
// journal once at startup and never see a provisioned user. The journal correct, the
// command successful, the sign-in still refused.
//
// 🔴 THE PREVIOUS ANSWER WAS A SECOND TYPE, AND THAT IS WHAT THIS DELETES. A one-line
// `ReloadingSource{Store: *FileStore}` existed only to make `Reload` satisfy `Source`, so
// every caller had to know which of two read paths it wanted and the wrong one was the
// default spelling. One rule, one place: there is now one read path and it cannot be
// stale.
//
// ⚠ AND THE SHORT-CIRCUIT SAVED NOTHING, MEASURED RATHER THAN ARGUED. Its only
// beneficiary was a caller that owns the file and reads it more than once; the only such
// caller is `ProvisionUser`, which reads once and appends once. Counted with
// `strace -e trace=openat` over a real `cairn-server -create-user`, at `e11c3a7` and
// again with this change: **4 opens of the journal either way** — the `OpenFileStore`
// create, this read, the `O_APPEND` write, and `Append`'s own re-read under the lock.
// The branch was never taken, because the `FileStore` value is fresh when `ProvisionUser`
// reaches it.
//
// The `cached`/`loaded` fields stay: `Reload` writes them and `lastKnownGood` reads them,
// which is what keeps an unreadable journal degrading to a STALE authority rather than to
// an empty one.
func (s *FileStore) Model(ctx context.Context) (Model, error) {
	return s.Reload(ctx)
}

// Reload re-reads the journal from disk unconditionally.
//
// 🔴 ON FAILURE THE PREVIOUSLY LOADED MODEL IS KEPT. A journal that has become
// unreadable — a truncated line, a disk error, an event kind from a newer build —
// must not empty the authority: an empty Model authorises nobody, so replacing a
// good one with it turns a parse error into a total outage that looks like a
// permissions problem. The error is returned so the caller can shout; the served
// authority stays the last one that was known-good, and its epoch says how old that
// is.
func (s *FileStore) Reload(_ context.Context) (Model, error) {
	f, err := os.Open(s.path)
	if err != nil {
		return s.lastKnownGood(), fmt.Errorf("control journal: %w", err)
	}
	defer f.Close()

	events, err := ReadEvents(f)
	if err != nil {
		return s.lastKnownGood(), fmt.Errorf("control journal %s: %w", s.path, err)
	}
	m, err := Replay(events)
	if err != nil {
		return s.lastKnownGood(), fmt.Errorf("control journal %s: %w", s.path, err)
	}

	s.mu.Lock()
	s.cached = m
	s.loaded = true
	s.mu.Unlock()
	return m, nil
}

func (s *FileStore) lastKnownGood() Model {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.loaded {
		return s.cached
	}
	return Model{}
}

// Append validates, writes and re-projects — in that order.
//
// 🔴 THE EVENTS ARE REPLAYED AGAINST A COPY OF THE MODEL **BEFORE** ANY BYTE
// REACHES DISK. A journal is append-only, so an event written and then found
// inconsistent cannot be taken back: it would poison every subsequent replay, and
// the process that next starts would refuse to load its own authority. Validating
// first means the file only ever contains events that replay cleanly.
//
// 🔴 ONE `write(2)` OF THE WHOLE BATCH, UNDER `O_APPEND` AND AN EXCLUSIVE `flock`.
// `O_APPEND` makes the offset update atomic against other appenders, and a single
// write of a buffer this size will not be split by the kernel for a regular file —
// together that means a concurrent appender cannot interleave INSIDE a line. The
// `flock` is what makes the read-validate-write sequence atomic as a whole, which
// `O_APPEND` alone does not: without it two processes can each validate against the
// same model and both write, producing a journal whose second event was never
// checked against the first.
//
// ⚠ THE LOCK IS ON THE JOURNAL ITSELF, NOT A SIDE FILE — and that differs from
// `internal/write`'s entry lock on purpose. That one needs a side file because its
// write is a temp-file-plus-rename, so the entry's inode changes and a lock on it
// would be held on an inode nobody is looking at. This file is only ever appended
// to, so its inode is stable and it can hold its own lock.
func (s *FileStore) Append(ctx context.Context, events ...Event) (Model, error) {
	if len(events) == 0 {
		return s.Model(ctx)
	}

	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return Model{}, fmt.Errorf("control journal: %w", err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return Model{}, fmt.Errorf("control journal lock: %w", err)
	}
	// Best-effort unlock: closing the descriptor releases the lock regardless, so
	// a failed explicit unlock cannot leave it held.
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)

	// Re-read UNDER THE LOCK rather than trusting the cache. The cache can be
	// stale with respect to another process, and validating an append against a
	// stale model is how an event that conflicts with a committed one gets
	// written.
	current, err := s.Reload(ctx)
	if err != nil {
		return Model{}, err
	}
	// 🔴 `clone`, NOT `current`. A Model is six maps behind a struct header, so
	// applying to a struct copy applies to the LIVE CACHE — and a batch rejected
	// at its third event would leave the first two applied to an authority no
	// journal line records. See `Model.clone`.
	next := current.clone()

	stamped := make([]Event, len(events))
	for i, e := range events {
		if e.At.IsZero() {
			e.At = s.now()
		}
		stamped[i] = e
		if err := next.apply(e); err != nil {
			return Model{}, fmt.Errorf("event %d (%s) would not replay: %w", i+1, e.Kind, err)
		}
	}

	var buf bytes.Buffer
	if err := WriteEvents(&buf, stamped); err != nil {
		return Model{}, err
	}
	if _, err := f.Write(buf.Bytes()); err != nil {
		return Model{}, fmt.Errorf("control journal append: %w", err)
	}
	// 🔴 `Sync` BEFORE REPORTING SUCCESS. The caller is about to tell somebody
	// their access was revoked; returning before the bytes are durable makes that
	// a claim about a page cache. A revocation that does not survive a restart is
	// the one failure this whole table exists to prevent.
	if err := f.Sync(); err != nil {
		return Model{}, fmt.Errorf("control journal sync: %w", err)
	}

	s.mu.Lock()
	s.cached = next
	s.loaded = true
	s.mu.Unlock()
	return next, nil
}

// compile-time proof that the file backend satisfies the seam a second backend
// will have to. A stub, but the cheap kind: it fails at BUILD time if either half
// drifts, which is earlier than any test.
var _ Store = (*FileStore)(nil)
