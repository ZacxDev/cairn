package capture

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// claudeSession is one ROOT Claude Code session on disk (R1): its own JSONL, each subagent's JSONL
// as a child stream, and every other file under its directory (persisted tool results, subagent
// metadata) as a blob.
type claudeSession struct {
	Root    string
	Streams map[string]string // stream name → path ("main", "subagent:<id>")
	Blobs   map[string]string // blob name (relative to the session dir) → path
}

// discoverClaude lists every root session under a Claude Code projects root.
//
// ⚠ `<project>/memory/` and anything else that is not `<project>/<id>.jsonl` or under
// `<project>/<id>/` is NOT transcript content and is never read (R1).
func discoverClaude(root string) ([]claudeSession, error) {
	projects, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []claudeSession
	for _, p := range projects {
		if !p.IsDir() {
			continue
		}
		pdir := filepath.Join(root, p.Name())
		entries, err := os.ReadDir(pdir)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".jsonl") {
				continue
			}
			id := strings.TrimSuffix(name, ".jsonl")
			cs := claudeSession{Root: id, Streams: map[string]string{"main": filepath.Join(pdir, name)},
				Blobs: map[string]string{}}
			sdir := filepath.Join(pdir, id)
			_ = filepath.WalkDir(sdir, func(path string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() || !d.Type().IsRegular() {
					return nil
				}
				rel, _ := filepath.Rel(sdir, path)
				rel = filepath.ToSlash(rel)
				if dir, file := filepath.Split(rel); dir == "subagents/" && strings.HasPrefix(file, "agent-") &&
					strings.HasSuffix(file, ".jsonl") {
					cs.Streams["subagent:"+strings.TrimSuffix(strings.TrimPrefix(file, "agent-"), ".jsonl")] = path
					return nil
				}
				cs.Blobs[rel] = path
				return nil
			})
			out = append(out, cs)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Root < out[j].Root })
	return out, nil
}

// FingerprintBytes is how much of a file's head the rewrite check remembers.
const FingerprintBytes = 4096

// ErrRewritten is a file whose already-shipped head changed, or which shrank below the watermark:
// it was rewritten, not appended, and an offset into it now points into different bytes.
var ErrRewritten = errors.New("capture: the file was rewritten, not appended; refusing to resume into different bytes")

// readNewLines returns every COMPLETE line after st.Offset, and the offset after the last one.
//
// 🔴 COMPLETE LINES ONLY. The runtime appends while this reads; a last line without its newline is
// a record still being written, and shipping it ships a fragment that does not parse. It waits
// for the next run.
//
// 🔴 AND IT REFUSES A REWRITE. Append-only was observed once (R2), not proven, so the head of the
// file already shipped is fingerprinted and re-checked before every resume.
//
// `limit` >= 0 stops at that offset: a re-ship from 0 reads only up to what the derivation saw.
func readNewLines(path string, st *StreamState, limit int64) ([][]byte, int64, string, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, "", 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, 0, "", 0, err
	}
	size := info.Size()
	if size < st.Offset || (limit >= 0 && size < limit) {
		return nil, 0, "", 0, ErrRewritten
	}
	if st.FPLen > 0 {
		head := make([]byte, st.FPLen)
		if _, err := io.ReadFull(f, head); err != nil {
			return nil, 0, "", 0, ErrRewritten
		}
		if digest(head) != st.Fingerprint {
			return nil, 0, "", 0, ErrRewritten
		}
	}
	if limit >= 0 {
		size = limit
	}
	if _, err := f.Seek(st.Offset, io.SeekStart); err != nil {
		return nil, 0, "", 0, err
	}
	buf, err := io.ReadAll(io.LimitReader(f, size-st.Offset))
	if err != nil {
		return nil, 0, "", 0, err
	}
	end := bytes.LastIndexByte(buf, '\n')
	if end < 0 {
		return nil, st.Offset, st.Fingerprint, st.FPLen, nil
	}
	buf = buf[:end+1]
	var lines [][]byte
	for _, l := range bytes.SplitAfter(buf, []byte("\n")) {
		if len(l) == 0 {
			continue
		}
		lines = append(lines, bytes.TrimSuffix(l, []byte("\n")))
	}
	newOffset := st.Offset + int64(len(buf))
	fp, fpLen := st.Fingerprint, st.FPLen
	if fpLen < FingerprintBytes {
		n := newOffset
		if n > FingerprintBytes {
			n = FingerprintBytes
		}
		head := make([]byte, n)
		if _, err := f.ReadAt(head, 0); err != nil {
			return nil, 0, "", 0, err
		}
		fp, fpLen = digest(head), int(n)
	}
	return lines, newOffset, fp, fpLen, nil
}

func digest(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
