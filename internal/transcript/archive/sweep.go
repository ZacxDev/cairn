package archive

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Deletion reasons a journal record may carry. S3 writes only [ReasonRetention]; the owner's
// delete and the agent's withdrawal (decision 16) arrive with S6 and reuse [Archive.Delete].
const (
	ReasonRetention = "retention"
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
	var deleted []string
	for _, root := range roots {
		m, err := a.ReadMeta(root)
		if err != nil || m == nil {
			continue
		}
		if now.Sub(m.UpdatedAt) <= a.cfg.Retention {
			continue
		}
		ok, err := a.Delete(root, ReasonRetention, func(cur *Meta) bool {
			return now.Sub(cur.UpdatedAt) > a.cfg.Retention
		})
		if err != nil {
			return deleted, err
		}
		if ok {
			deleted = append(deleted, root)
		}
	}
	return deleted, nil
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
	if err := a.journal(journalRecord{Event: "transcript-deleted", Root: root, Owner: m.Owner, Host: m.Host,
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
