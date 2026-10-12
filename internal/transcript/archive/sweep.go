package archive

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Deletion reasons a journal record may carry. S3 writes only [ReasonRetention]; the owner's
// delete and the agent's withdrawal (decision 16) arrive with S6 and reuse [Archive.Delete].
const (
	ReasonRetention = "retention"
	// ReasonRetentionNoMeta is a session directory no upload ever COMMITTED (its `meta.json` was
	// never written — a crash or a failed write between creating the directory and the commit).
	// Its owner is unknown, so the journal line names none.
	ReasonRetentionNoMeta = "retention-uncommitted"

	// eventDeleted is the ONE event this store journals; [checkJournal] refuses any other.
	eventDeleted = "transcript-deleted"
)

// journalRecord is one deletion fact: who uploaded the session, which root, why, when — never
// content (decision 15: "deletion is `rm -r` of one directory, plus the journaled fact").
type journalRecord struct {
	Event  string    `json:"event"`
	Root   string    `json:"root"`
	Owner  string    `json:"owner"`
	Host   string    `json:"host"`
	Reason string    `json:"reason"`
	At     time.Time `json:"at"`
}

// Sweep deletes every session whose LAST accepted upload is more than the retention ago, and
// returns the roots it deleted. A session exactly at the retention is kept: the boundary belongs
// to the session, so `-transcript-retention 90d` keeps a session for all of its ninetieth day.
//
// ⚠ RETENTION IS COUNTED FROM THE LAST UPLOAD, NOT THE FIRST — a session still being appended to
// is never swept out from under its own agent.
func (a *Archive) Sweep() ([]string, error) {
	roots, err := a.Roots()
	if err != nil {
		return nil, err
	}
	now := a.cfg.Now().UTC()
	// ONE predicate for the survey and for the re-check under the lock: two spellings let a
	// mutant in either be masked by the other.
	stale := func(last time.Time) bool { return now.Sub(last) > a.cfg.Retention }
	expired := func(m *Meta) bool { return stale(m.UpdatedAt) }
	a.sweepFrames(now)
	var deleted []string
	for _, root := range roots {
		m, err := a.ReadMeta(root)
		if err != nil {
			continue
		}
		var ok bool
		switch {
		case m == nil:
			// 🔴 A DIRECTORY NO UPLOAD COMMITTED IS SWEPT TOO (AP3). It used to be skipped here
			// forever — counted against the quota at every restart and never freed — because
			// retention was read only off `meta.json`. Its clock is the newest write inside it.
			ok, err = a.deleteUncommitted(root, stale)
		case expired(m):
			ok, err = a.Delete(root, ReasonRetention, expired)
		}
		if err != nil {
			return deleted, err
		}
		if ok {
			deleted = append(deleted, root)
		}
	}
	return deleted, nil
}

// deleteUncommitted removes a session directory that has NO meta, when its newest write is stale —
// both read UNDER the root's lock, so a first upload between the survey and the delete (which
// creates the directory before it commits) is seen and kept.
func (a *Archive) deleteUncommitted(root string, stale func(time.Time) bool) (bool, error) {
	l := a.rootLock(root)
	l.Lock()
	defer l.Unlock()
	m, err := a.readMeta(root)
	if err != nil || m != nil {
		return false, err
	}
	dir := a.sessionDir(root)
	var newest time.Time
	var freed int64
	err = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		if d.Type().IsRegular() {
			freed += info.Size()
		}
		return nil
	})
	if err != nil || !stale(newest) {
		return false, err
	}
	if err := a.journal(journalRecord{Event: eventDeleted, Root: root, Reason: ReasonRetentionNoMeta,
		At: a.cfg.Now().UTC()}); err != nil {
		return false, err
	}
	if err := os.RemoveAll(dir); err != nil {
		return false, err
	}
	a.adjust(-freed)
	return true, nil
}

// sweepFrames discards every staged frame sequence idle for more than [FrameIdle], returning its
// bytes to the quota (AP3). Each is re-checked under its root's lock, so a frame staged between the
// survey and the drop keeps its sequence.
func (a *Archive) sweepFrames(now time.Time) {
	type idle struct{ root, target string }
	var found []idle
	a.mu.Lock()
	for _, st := range a.frames {
		if now.Sub(st.touched) > FrameIdle {
			found = append(found, idle{st.root, st.target})
		}
	}
	a.mu.Unlock()
	for _, f := range found {
		l := a.rootLock(f.root)
		l.Lock()
		a.mu.Lock()
		st := a.frames[a.frameKey(f.root, f.target)]
		still := st != nil && now.Sub(st.touched) > FrameIdle
		a.mu.Unlock()
		if still {
			a.dropFrames(f.root, f.target)
		}
		l.Unlock()
	}
}

// Delete removes one session's directory when `still` holds for its meta read UNDER the root's
// lock — so an upload that lands between the survey and the delete is seen — and journals the
// fact. It reports whether anything was deleted.
func (a *Archive) Delete(root, reason string, still func(*Meta) bool) (bool, error) {
	l := a.rootLock(root)
	l.Lock()
	defer l.Unlock()
	m, err := a.readMeta(root)
	if err != nil || m == nil || (still != nil && !still(m)) {
		return false, err
	}
	dir := a.sessionDir(root)
	freed, err := treeBytes(dir)
	if err != nil {
		return false, err
	}
	// The fact is journaled FIRST: a crash between the two leaves a journal line for a directory
	// that still exists (the next sweep deletes it and journals again), never a deletion nobody
	// recorded.
	if err := a.journal(journalRecord{Event: eventDeleted, Root: root, Owner: m.Owner, Host: m.Host,
		Reason: reason, At: a.cfg.Now().UTC()}); err != nil {
		return false, err
	}
	if err := os.RemoveAll(dir); err != nil {
		return false, err
	}
	a.adjust(-freed)
	return true, nil
}

func (a *Archive) journal(r journalRecord) error {
	line, err := json.Marshal(r)
	if err != nil {
		return err
	}
	path := filepath.Join(a.cfg.Dir, journalName)
	before := fileSize(path)
	f, err := os.OpenFile(path, journalOpenFlags, fileMode)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	a.adjust(fileSize(path) - before)
	return nil
}
