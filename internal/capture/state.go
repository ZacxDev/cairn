// Package capture is the host-side capture agent behind `cmd/cairn-capture`: it reads Claude
// Code JSONL by byte offset and opencode through `opencode export`, derives each session's read
// scopes, decides where the session may go, redacts, and hands the redacted bytes to a [Sink].
//
// 🔴 IT UPLOADS NOTHING (S2 of `claudedocs/plan-cairn-plugins.md`). This slice ships no [Sink]: the
// agent is exercised through an in-memory one in its tests, and S3 adds the upload. A Sink write
// is the "acknowledgement": every watermark advances only after its write succeeds.
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
type StreamState struct {
	Offset      int64             `json:"offset,omitempty"`
	Fingerprint string            `json:"fp,omitempty"`
	FPLen       int               `json:"fp_len,omitempty"`
	Refused     string            `json:"refused,omitempty"`
	Units       map[string]string `json:"units,omitempty"`
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
type BlobState struct {
	Size    int64  `json:"size"`
	MtimeNs int64  `json:"mtime_ns"`
	Digest  string `json:"digest"`
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
// Refusals are kept: a rewritten file is no less rewritten for moving instance.
func (ss *SessionState) reset() {
	for _, st := range ss.Streams {
		st.Offset, st.Fingerprint, st.FPLen = 0, "", 0
		st.Units = map[string]string{}
	}
	ss.Blobs = map[string]BlobState{}
}
