// Package capture is the host-side capture agent behind `cmd/cairn-capture`: it reads Claude
// Code JSONL by byte offset and opencode through `opencode export`, derives each session's read
// scopes, decides where the session may go, redacts, and hands the redacted bytes to a [Sink].
//
// It uploads through [HTTPSink] (S3b of `claudedocs/plan-cairn-plugins.md`), the capture/v1
// contract `internal/worker` serves. A Sink write is the "acknowledgement": every watermark advances
// only after its write succeeds, which for [HTTPSink] means a 200.
//
// 🔴 STDLIB ONLY, AND MEASURED SO: `cmd/cairn-capture` is in `internal/depspolicy`'s
// `LinkedBinaryRoots`, so its whole import closure — this package included — is under the ban.
package capture

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/ZacxDev/cairn/internal/transcript/scopeuse"
)

// StreamState is one stream's watermark.
//
// A Claude Code stream is a byte OFFSET into its JSONL file plus a FINGERPRINT of the file's first
// bytes (up to 4 KiB): a file whose fingerprint changed was rewritten, not appended, and is refused
// rather than resumed into different bytes. An opencode stream is the version (digest) of every
// unit already shipped, because opencode MUTATES parts and its export carries no `time_updated`.
//
// `Cursor` is an opencode stream's position on the instance ([chainCursor]); a Claude Code
// stream's is its `Offset`. `Refused` is FINAL for the stream: a rewritten file, or a write whose
// 422 named a request FIELD (the stream's own name or a cursor matched the pod's table) — that
// stream is held and never resent. A 422 naming a RECORD does not hold the stream; see `Rechecked`.
type StreamState struct {
	Offset      int64             `json:"offset,omitempty"`
	Fingerprint string            `json:"fp,omitempty"`
	FPLen       int               `json:"fp_len,omitempty"`
	Cursor      string            `json:"cursor,omitempty"`
	Refused     string            `json:"refused,omitempty"`
	Units       map[string]string `json:"units,omitempty"`
	// Pending is THE write of an opencode stream that was sent and not acknowledged whole: the
	// units it carried and the cursor after each — saved BEFORE the request leaves.
	//
	// 🔴 WHY IT IS PERSISTED: an opencode cursor is a hash chain over the units as they were when
	// sent, and opencode MUTATES units in place. When an acknowledgement is lost (a dropped answer,
	// a kill before the state is saved) the instance holds a position the NEXT export can no longer
	// reproduce, so its 409 would name a cursor nothing recomputes — the stream wedged for good,
	// measured. The plan is what the instance's position is matched against instead
	// ([StreamState.reconcile]).
	//
	// 🔴 AND THERE IS AT MOST ONE. While a plan is pending NO new write is planned or sent: the
	// agent first ASKS the instance where the stream is ([Sink.Position], which stores nothing) and
	// settles the plan against the answer. A list of plans with an eviction bound was built first;
	// enough failed runs over a changing export evicted the one plan the instance held.
	Pending []PendingUnit `json:"pending,omitempty"`
	// Rechecked is every record of this stream the pod's re-check refused (422) and that is not
	// stored yet: its `src` → the digest of the bytes that were refused. Saved BEFORE the
	// placeholder is sent, so a later run sends the placeholder at once and never those bytes.
	Rechecked map[string]string `json:"rechecked,omitempty"`
}

// PendingUnit is one unit of a sent, unacknowledged write: its key, the version (digest of its
// raw bytes) that was sent, and the stream cursor once it is stored.
type PendingUnit struct {
	Key    string `json:"key"`
	Digest string `json:"digest"`
	End    string `json:"end"`
}

// reconcile settles the pending plan against the position the instance says it holds. At the
// cursor the agent already has, nothing of the plan landed; at the end of one of the plan's units,
// every unit up to it is stored AS IT WAS SENT and the cursor is that position. Either way the
// plan is settled; any other position is reported false and the plan is kept.
func (st *StreamState) reconcile(storedTo string) bool {
	if storedTo == st.Cursor {
		st.Pending = nil
		return true
	}
	for k, u := range st.Pending {
		if u.End != storedTo {
			continue
		}
		for _, done := range st.Pending[:k+1] {
			st.Units[done.Key] = done.Digest
		}
		st.Cursor, st.Pending = storedTo, nil
		return true
	}
	return false
}

// SessionState is everything the agent remembers about one ROOT session.
type SessionState struct {
	Runtime string `json:"runtime"`
	// Instance is where this session's prefix has been shipped, "" when nowhere yet.
	Instance string `json:"instance,omitempty"`
	// Held is FINAL: V only grows, so a held session stays held (decision 16).
	Held       bool                    `json:"held,omitempty"`
	HeldReason string                  `json:"held_reason,omitempty"`
	V          []string                `json:"v,omitempty"`
	Evidence   scopeuse.Evidence       `json:"evidence"`
	Streams    map[string]*StreamState `json:"streams,omitempty"`
	Blobs      map[string]BlobState    `json:"blobs,omitempty"`
}

// BlobState is what was shipped of one blob. Size and modification time are compared FIRST, so
// an unchanged blob is never re-read: hashing every persisted tool result on every 60-second run
// cost ~311 MB of reads per run on the measured host (review round 1); decision 5's "idle
// sessions cost one stat per file" is what this makes true.
//
// `Cursor` is the version the instance holds ([blobCursor]); `Refused` marks a version whose 422
// named a request FIELD (the blob's name matched the pod's table) — held, and not resent until the
// file changes. `Rechecked` is the digest of a version whose CONTENT the pod's re-check refused:
// saved before its placeholder is sent, so those bytes are not sent by a later run.
type BlobState struct {
	Size      int64  `json:"size"`
	MtimeNs   int64  `json:"mtime_ns"`
	Digest    string `json:"digest"`
	Cursor    string `json:"cursor,omitempty"`
	Refused   string `json:"refused,omitempty"`
	Rechecked string `json:"rechecked,omitempty"`
}

// State is the agent's whole local state: one 0600 file.
type State struct {
	Schema   int                      `json:"schema"`
	Sessions map[string]*SessionState `json:"sessions"`
}

// LoadState reads the state file, or returns an empty state when there is none.
//
// 🔴 AN UNREADABLE OR MALFORMED STATE IS AN ERROR, NOT A FRESH START. Starting over would re-ship
// every session from offset 0 under watermarks nobody can see — the duplicate the watermark exists
// to prevent.
func LoadState(path string) (*State, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if errors.Is(err, os.ErrNotExist) {
		return &State{Schema: 1, Sessions: map[string]*SessionState{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("capture: the state file %s could not be opened: %w", path, err)
	}
	defer f.Close()
	var s State
	dec := json.NewDecoder(f)
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("capture: the state file %s is malformed (%v); refusing rather than re-shipping everything", path, err)
	}
	if s.Schema != 1 {
		return nil, fmt.Errorf("capture: the state file %s has schema %d; this agent knows 1", path, s.Schema)
	}
	if s.Sessions == nil {
		s.Sessions = map[string]*SessionState{}
	}
	return &s, nil
}

// Save writes the state atomically (a 0600 temporary file, fsynced, renamed over the old one).
func (s *State) Save(path string) error {
	raw, err := json.MarshalIndent(s, "", " ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".state-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
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
	return os.Rename(tmp.Name(), path)
}

func (s *State) session(root, runtime string) *SessionState {
	ss := s.Sessions[root]
	if ss == nil {
		ss = &SessionState{Runtime: runtime}
		s.Sessions[root] = ss
	}
	if ss.Streams == nil {
		ss.Streams = map[string]*StreamState{}
	}
	if ss.Blobs == nil {
		ss.Blobs = map[string]BlobState{}
	}
	return ss
}

func (ss *SessionState) stream(name string) *StreamState {
	st := ss.Streams[name]
	if st == nil {
		st = &StreamState{}
		ss.Streams[name] = st
	}
	if st.Units == nil {
		st.Units = map[string]string{}
	}
	return st
}

// reset forgets every watermark, so the next ship starts from offset 0 (a re-ship, decision 16).
// Refusals are kept, a stream's and a blob's alike: a rewritten file is no less rewritten, and a
// name the re-check refuses no less refused, for moving instance.
func (ss *SessionState) reset() {
	for _, st := range ss.Streams {
		st.Offset, st.Fingerprint, st.FPLen, st.Cursor = 0, "", 0, ""
		st.Units = map[string]string{}
		st.Pending = nil
	}
	kept := map[string]BlobState{}
	for name, b := range ss.Blobs {
		if b.Refused != "" || b.Rechecked != "" {
			// Size and mtime go too unless it is held: a rechecked blob must be read again so its
			// placeholder ships to the new instance.
			if b.Refused == "" {
				b = BlobState{Rechecked: b.Rechecked}
			}
			b.Cursor = ""
			kept[name] = b
		}
	}
	ss.Blobs = kept
}

// ErrRunning is [LockState]'s answer when another run holds the lock.
var ErrRunning = errors.New("another capture run holds the lock")

// LockState takes the run lock — `<state>.lock`, an exclusive non-blocking `flock` — so two runs
// (a slow one and the next timer tick) never interleave writes of one session or overwrite each
// other's state. The lock goes with the process, so a crashed run leaves nothing to clean up.
func LockState(path string) (release func(), err error) {
	f, err := os.OpenFile(path+".lock", os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("capture: the run lock %s.lock cannot be opened: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrRunning
		}
		return nil, fmt.Errorf("capture: the run lock %s.lock cannot be taken: %w", path, err)
	}
	return func() { f.Close() }, nil
}

// CleanStateTemps removes the `.state-*` files a killed [State.Save] left beside the state file.
// Called under the run lock, so no live save owns one.
func CleanStateTemps(path string) {
	stale, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".state-*"))
	for _, p := range stale {
		_ = os.Remove(p)
	}
}
