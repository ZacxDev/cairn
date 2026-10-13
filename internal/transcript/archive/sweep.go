package archive

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/ZacxDev/cairn/internal/write"
)

// Deletion reasons a journal record may carry. Every one is the SAME cascade — [Archive.deleteLocked]:
// the journaled fact, then `rm -r` of the session's directory (its streams, blobs and plugin
// outputs, ticket edges among them).
const (
	ReasonRetention = "retention"
	// ReasonOwnerDelete is the owner's "delete transcript" on the browser surface (S6, clause l).
	// The record's owner and host are the uploader's, and the owner arm IS that principal, so the
	// line says who deleted it without a second field.
	ReasonOwnerDelete = "owner-delete"
	// ReasonWithdrawn is the capture agent's withdrawal of a shipped prefix (S6, decision 16, clause
	// m), made with the capture token of the SAME (owner, host) that uploaded it — so, again, the
	// record's own owner and host are who withdrew it.
	ReasonWithdrawn = "withdrawn"
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
	if a.afterFrameSurvey != nil {
		a.afterFrameSurvey()
	}
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
	if !write.SessionComponent.MatchString(root) {
		return false, nil
	}
	l := a.rootLock(root)
	l.Lock()
	defer l.Unlock()
	m, err := a.readMeta(root)
	if err != nil || m == nil || (still != nil && !still(m)) {
		return false, err
	}
	return true, a.deleteLocked(m, reason)
}

// deleteLocked is THE DELETION CASCADE, and the only one: retention, the owner's delete and a
// withdrawal all end here. The caller holds the root's lock and read `m` under it.
//
// The fact is journaled FIRST: a crash between the two leaves a journal line for a directory that
// still exists (the next sweep, delete or withdrawal removes it and journals again), never a
// deletion nobody recorded.
//
// 🔴 EVERYTHING DERIVED GOES WITH IT, BECAUSE EVERYTHING DERIVED IS IN THE DIRECTORY: the streams,
// the blobs and `outputs.jsonl` — every summary, ticket edge and suggestion a plugin wrote. Two
// things live OUTSIDE it and are handled here: the plugin output TOTALS ([Archive.forgetOutputs]),
// and any continuation frames staged for the root (`frames/`, outside every session directory) —
// a half-sent record of a session being withdrawn is that session's content and must not sit on
// disk for [FrameIdle] after it.
func (a *Archive) deleteLocked(m *Meta, reason string) error {
	dir := a.sessionDir(m.Root)
	freed, err := treeBytes(dir)
	if err != nil {
		return err
	}
	if err := a.journal(journalRecord{Event: eventDeleted, Root: m.Root, Owner: m.Owner, Host: m.Host,
		Reason: reason, At: a.cfg.Now().UTC()}); err != nil {
		return err
	}
	if err := a.forgetOutputs(m.Root); err != nil {
		return err
	}
	a.dropRootFrames(m.Root)
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	a.adjust(-freed)
	return nil
}

// Withdraw is decision 16's withdrawal: the capture agent asking this instance to forget a prefix
// it shipped before the session's routing answer changed. It reports `deleted`:
//
//   - TRUE when `who` — the capture token's own (owner, host) — holds the root now (the cascade
//     runs, journaled [ReasonWithdrawn]), OR holds nothing under it and the journal records that a
//     root of that id uploaded by this same (owner, host) was already deleted. The second arm is
//     what makes the call IDEMPOTENT: a withdrawal whose acknowledgement was lost is repeated, the
//     root is gone, and the answer is `true` again with nothing changed. (Answering `false` to the
//     repeat turned a timed-out success into a permanent hold on the host; retracted in the plan.)
//   - FALSE for a root held by another owner, by the same owner under ANOTHER host label, and for
//     one this (owner, host) never had deleted here — ONE answer for all three, and nothing is
//     deleted, so the route is no oracle over other uploaders' roots: a `true` only ever speaks
//     about the caller's own.
//
// ⚠ THE SECOND ARM READS ANY DELETION OF THE CALLER'S OWN ROOT, NOT ONLY A WITHDRAWAL — a stated
// widening of the plan's "already withdrew it". The agent's question is "is my prefix gone from
// you", and a prefix the retention sweep or the owner's own delete removed is as gone as one a
// withdrawal removed; answering `false` there would hold the session on its host for good over a
// prefix that no longer exists anywhere. It still names only the caller's own (owner, host).
//
// 🔴 A ROOT HELD BY SOMEBODY ELSE IS `false` EVEN IF THE CALLER ONCE WITHDREW THE SAME ID: what is
// stored under it now is not the caller's, and `true` would claim a deletion that did not happen.
func (a *Archive) Withdraw(who Uploader, root string) (bool, error) {
	if !write.SessionComponent.MatchString(root) {
		return false, nil
	}
	l := a.rootLock(root)
	l.Lock()
	defer l.Unlock()
	m, err := a.readMeta(root)
	if err != nil {
		return false, err
	}
	if m != nil {
		// 🔴 `owned` IS THE UPLOAD PATH'S OWN PREDICATE — owner AND host — never a second spelling.
		// `m == nil` is its "nobody holds it yet" arm and cannot reach here.
		if !owned(m, who) {
			return false, nil
		}
		return true, a.deleteLocked(m, ReasonWithdrawn)
	}
	// Nothing is stored under the root. A record or blob still being reassembled from continuation
	// frames (staged OUTSIDE every session directory, before any upload commits) is that session's
	// content all the same, and goes now rather than after [FrameIdle].
	a.dropRootFrames(root)
	return a.deletedFor(who, root), nil
}

// deletedFor reports whether the journal records a deletion of `root` as uploaded by `who`.
func (a *Archive) deletedFor(who Uploader, root string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	was, ok := a.deleted[root]
	return ok && was == who && who.Owner != ""
}

// dropRootFrames discards every continuation-frame sequence staged for a root. The caller holds the
// root's lock.
func (a *Archive) dropRootFrames(root string) {
	a.mu.Lock()
	var staged []string
	for _, st := range a.frames {
		if st.root == root {
			staged = append(staged, st.target)
		}
	}
	a.mu.Unlock()
	for _, target := range staged {
		a.dropFrames(root, target)
	}
}

// readDeleted is every root the journal records a FINAL deletion of ([Archive.gone]), and whose it
// was. A line that does not decode is skipped: [CheckOwnership] already vetted the file, and a
// finality nobody can read must not stop the store from opening.
func readDeleted(dir string) (map[string]Uploader, error) {
	out := map[string]Uploader{}
	raw, err := os.ReadFile(filepath.Join(dir, journalName))
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	for _, line := range bytes.Split(raw, []byte("\n")) {
		var r journalRecord
		if json.Unmarshal(line, &r) != nil || r.Event != eventDeleted || r.Reason == ReasonRetentionNoMeta || r.Root == "" {
			continue
		}
		out[r.Root] = Uploader{Owner: r.Owner, Host: r.Host}
	}
	return out, nil
}

// journal appends one deletion fact and, once it is on disk, makes the deletion FINAL for the root.
//
// 🔴 UNDER ITS OWN LOCK: two roots' deletions hold two different root locks, and each measured the
// journal's size before and after its own append — so two concurrent ones each counted the other's
// line, and `used` drifted up by a line per collision.
func (a *Archive) journal(r journalRecord) error {
	a.journalMu.Lock()
	defer a.journalMu.Unlock()
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
	if r.Reason != ReasonRetentionNoMeta {
		a.mu.Lock()
		a.deleted[r.Root] = Uploader{Owner: r.Owner, Host: r.Host}
		a.mu.Unlock()
	}
	return nil
}
