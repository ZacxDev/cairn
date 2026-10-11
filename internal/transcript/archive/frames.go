package archive

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"syscall"
)

// frameState is one record (or blob) being reassembled from continuation frames (decision 15:
// "a record larger than one request is sent as numbered continuation frames and reassembled
// before it is stored").
//
// 🔴 FRAMES ARRIVE IN ORDER OR NOT AT ALL. Frame 0 starts (or restarts) a sequence; frame i is
// accepted only when frame i−1 was the last one staged under the same `from`, `to`, `src` and
// count. Anything else drops the staged bytes and is refused, so a missing middle frame stores
// NOTHING — the agent restarts the record from frame 0. A reassembled record is then exactly one
// ordinary upload: the same CAS, the same re-check, the same quota.
//
// The staged bytes live on disk under `<dir>/frames/`, outside every session directory, because
// nothing is owned until an upload is accepted — a squatter's half-sent record must not create a
// session. They count against the quota while staged. The in-memory index is lost on a restart,
// which only means the next frame is refused and the agent starts again at 0.
type frameState struct {
	from, to, src string
	count, next   int
	path          string
	bytes         int64
}

func (a *Archive) frameKey(root, target string) string { return root + "\x00" + target }

func (a *Archive) framePath(root, target string) string {
	sum := sha256.Sum256([]byte(a.frameKey(root, target)))
	return filepath.Join(a.cfg.Dir, framesDir, hex.EncodeToString(sum[:16]))
}

// stageFrame stages one frame. It returns the assembled bytes and done=true on the final frame.
// The caller holds the root lock.
func (a *Archive) stageFrame(u Upload, target, src string, chunk []byte) ([]byte, bool, error) {
	f := u.Frame
	key := a.frameKey(u.Root, target)
	a.mu.Lock()
	st := a.frames[key]
	a.mu.Unlock()
	if f.Index == 0 {
		a.dropFrames(u.Root, target)
		if err := os.MkdirAll(filepath.Join(a.cfg.Dir, framesDir), dirMode); err != nil {
			return nil, false, err
		}
		st = &frameState{from: u.From, to: u.To, src: src, count: f.Count, path: a.framePath(u.Root, target)}
	} else if st == nil || st.next != f.Index || st.count != f.Count || st.from != u.From || st.to != u.To || st.src != src {
		a.dropFrames(u.Root, target)
		return nil, false, invalid("frame %d of %d does not continue a staged sequence (a frame is missing or "+
			"out of order); nothing is stored — restart the record at frame 0", f.Index, f.Count)
	}
	if f.Index == f.Count-1 {
		staged, err := os.ReadFile(st.path)
		if err != nil {
			a.dropFrames(u.Root, target)
			return nil, false, err
		}
		a.dropFrames(u.Root, target)
		return append(staged, chunk...), true, nil
	}
	if err := a.reserve(int64(len(chunk))); err != nil {
		a.dropFrames(u.Root, target)
		return nil, false, err
	}
	flags := os.O_WRONLY | os.O_CREATE | os.O_APPEND | syscall.O_NOFOLLOW
	if f.Index == 0 {
		flags |= os.O_TRUNC
	}
	file, err := os.OpenFile(st.path, flags, fileMode)
	if err == nil {
		_, err = file.Write(chunk)
		if cerr := file.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		a.release(int64(len(chunk)))
		a.dropFrames(u.Root, target)
		return nil, false, err
	}
	st.bytes += int64(len(chunk))
	st.next = f.Index + 1
	a.mu.Lock()
	a.frames[key] = st
	a.mu.Unlock()
	return nil, false, nil
}

// dropFrames discards a staged sequence and returns its bytes to the quota.
func (a *Archive) dropFrames(root, target string) {
	key := a.frameKey(root, target)
	a.mu.Lock()
	st := a.frames[key]
	delete(a.frames, key)
	if st != nil {
		a.used -= st.bytes
		if a.used < 0 {
			a.used = 0
		}
	}
	a.mu.Unlock()
	_ = os.Remove(a.framePath(root, target))
}
