package arcs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
	"strings"
	"syscall"
	"time"
)

// 🔴 THE JOURNAL: ONE JSON OBJECT PER LINE, APPEND-ONLY, NEVER COMPACTED (operator decision Q7).
// Closed arcs, superseded registrations and unreadable lines all stay in the file forever this
// phase; the fold below is what turns history into "the current state of each arc".
//
// 🔴 IT LIVES OUTSIDE THE STORE TREE (Q2), AND `ResolveJournalPath` IS WHAT MAKES THAT A FACT
// RATHER THAN A REQUEST — the token-file adapter enumerates every DIRECTORY at the store root as
// a scope, dot-prefixed or not, so a journal or its directory under the root could become a scope
// a bare row reads.

// The status tokens a registration answers with.
const (
	OutcomeRegistered = "arc-registered"
	OutcomeUnchanged  = "arc-unchanged"
)

// JournalUnreadableError is "the journal is configured and could NOT be read" — the four-state
// rule's "could not look", answered 503 and never as "nothing registered".
type JournalUnreadableError struct {
	Path string
	Err  error
}

func (e *JournalUnreadableError) Error() string {
	return fmt.Sprintf("arc registration journal unreadable: %s (%v) — this is NOT 'no arc registered'", e.Path, e.Err)
}

func (e *JournalUnreadableError) Unwrap() error { return e.Err }

// Snapshot is one read of the journal, folded.
type Snapshot struct {
	// Latest is the LAST VALID record per key — the arc's current state. History is in the file.
	Latest map[Key]Registration
	// Records is how many complete, valid lines were read (every registration ever applied).
	Records int
	// Skipped is how many COMPLETE lines did not parse or validate. They are counted and never
	// applied; one bad line does not poison the rest, because the alternative — refusing the
	// whole journal — would turn one corrupt record into "no arc is registered anywhere".
	Skipped int
	// TornTail is true when the file did not end in a newline: the last fragment is a partial
	// write (a crash between `write` and its completion) and is IGNORED, not parsed.
	TornTail bool
	// Missing is true when the file does not exist yet — a configured journal nobody has
	// registered into. It reads as empty, not as unreadable.
	Missing bool
}

// Damaged is true when something in the journal was not applied — the one predicate the
// renderer's warning reads.
func (s Snapshot) Damaged() bool { return s.Skipped > 0 || s.TornTail }

// Sorted is the latest registrations ordered by (home, slug), byte-wise.
func (s Snapshot) Sorted() []Registration {
	out := make([]Registration, 0, len(s.Latest))
	for _, r := range s.Latest {
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b Registration) int {
		if c := strings.Compare(a.Home, b.Home); c != 0 {
			return c
		}
		return strings.Compare(a.Slug, b.Slug)
	})
	return out
}

// Journal is the registry's file. The zero value is not usable; `Path` must be a path
// `ResolveJournalPath` returned.
type Journal struct{ Path string }

// Read reads and folds the whole journal. A missing file is an empty snapshot; any other
// failure to read is a `*JournalUnreadableError`.
func (j Journal) Read() (Snapshot, error) {
	data, err := os.ReadFile(j.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return Snapshot{Latest: map[Key]Registration{}, Missing: true}, nil
	}
	if err != nil {
		return Snapshot{}, &JournalUnreadableError{Path: j.Path, Err: err}
	}
	return fold(data), nil
}

// fold is the read rule, over bytes, so it can be tested without a file.
//
// 🔴 THE TORN TAIL IS DECIDED BY THE LAST BYTE, NOT BY WHETHER THE LAST LINE PARSES. A crash can
// leave a prefix of a record that happens to be valid JSON only if it is complete — and every
// complete record is written with its newline in the SAME `write(2)` — so "no trailing newline"
// is exactly "the final write did not finish". A fragment that parsed by accident would be
// applied by a parse-based rule; this one never applies it.
func fold(data []byte) Snapshot {
	snap := Snapshot{Latest: map[Key]Registration{}}
	if len(data) == 0 {
		return snap
	}
	if data[len(data)-1] != '\n' {
		snap.TornTail = true
		cut := bytes.LastIndexByte(data, '\n')
		data = data[:cut+1] // cut == -1 leaves nothing, which is right: one partial line, no record
	}
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var r Registration
		dec := json.NewDecoder(bytes.NewReader(line))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&r); err != nil || !validRecord(r) {
			snap.Skipped++
			continue
		}
		// ONE object per line and nothing after it — which is also what makes `tornSeal` work:
		// a sealed fragment that happened to be a whole object is invalid by its suffix.
		if _, err := dec.Token(); err != io.EOF {
			snap.Skipped++
			continue
		}
		snap.Records++
		snap.Latest[r.Key()] = r
	}
	return snap
}

// tornSeal is what `Register` appends after a torn tail before its own record.
//
// 🔴 NOT A BARE NEWLINE — MEASURED. A bare newline turns the fragment into a complete line, and a
// fragment that is a whole object missing only its newline then PARSES and is applied on every
// later read, though it was ignored as a torn tail before the append: the same bytes, two
// verdicts (`TestATornTailIsIgnoredOnReadAndSealedBeforeTheNextAppend`'s second case went red
// exactly that way). The marker carries no `"`, `}` or `]`, so it can never close an unterminated
// fragment into valid JSON, and the fold's one-object-per-line check refuses it after a complete
// one. The sealed line is therefore always SKIPPED and counted, and the marker says why in the file.
const tornSeal = " !torn-tail-sealed\n"

// Outcome is what one registration did.
type Outcome struct {
	Status string       // OutcomeRegistered or OutcomeUnchanged
	Record Registration // the state now displayed for the key, merge rule 7 applied
}

// Register merges `reg` onto the key's current state and appends the result — unless that would
// change nothing a reader sees, in which case nothing is written (`arc-unchanged`, so a retried
// PUT after a timeout cannot grow the journal).
//
// 🔴 CONCURRENCY: AN EXCLUSIVE `flock` ON THE JOURNAL FOR THE WHOLE READ-MERGE-APPEND, AND ONE
// `write(2)` UNDER `O_APPEND`, THEN `fsync` BEFORE SUCCESS — `control.FileStore.Append`'s
// mechanism, for its reasons. `O_APPEND` alone makes each append land at the end, but two
// registrations of one key could each merge against the same previous state and the second would
// silently discard the first's carried members; the lock makes the sequence atomic. The lock is
// per open file description, so it serialises goroutines in this process AND any other process
// appending to the same file on the same host's filesystem. ⚠ That is its scope: a journal on a
// network filesystem whose `flock` is advisory-only across hosts is not covered, and the
// reference deployment has one pod writing a read-write-once volume.
//
// 🔴 A TORN TAIL IS SEALED BEFORE APPENDING. Writing after a fragment with no newline would glue
// the new record onto it, and the fold would then skip BOTH — a crash on one registration
// silently eating the next. So `tornSeal` is written first, in the same `write`, turning the
// fragment into one skipped line of its own (never an applied one — see `tornSeal`).
//
// `O_NOFOLLOW`: the path was fully resolved at startup; a symlink appearing at it afterwards is
// refused here rather than followed somewhere `ResolveJournalPath` never checked.
func (j Journal) Register(reg Registration, now time.Time) (Outcome, error) {
	f, err := os.OpenFile(j.Path, os.O_CREATE|os.O_RDWR|os.O_APPEND|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return Outcome{}, &JournalUnreadableError{Path: j.Path, Err: err}
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return Outcome{}, &JournalUnreadableError{Path: j.Path, Err: err}
	}
	// Best-effort: closing the descriptor releases the lock regardless.
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)

	// Re-read UNDER THE LOCK, through the LOCKED DESCRIPTOR rather than the path, so the merge is
	// against the bytes of the inode this process holds the lock on — never a file a rename put at
	// the same name in between. `O_APPEND` moves every WRITE to the end whatever the read offset.
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return Outcome{}, &JournalUnreadableError{Path: j.Path, Err: err}
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return Outcome{}, &JournalUnreadableError{Path: j.Path, Err: err}
	}
	snap := fold(data)
	var prev *Registration
	if p, ok := snap.Latest[reg.Key()]; ok {
		prev = &p
	}
	next := Merge(prev, reg)
	next.RegisteredAt = now.UTC().Format(time.RFC3339)
	if prev != nil && sameState(*prev, next) {
		return Outcome{Status: OutcomeUnchanged, Record: *prev}, nil
	}
	line, err := json.Marshal(next)
	if err != nil {
		return Outcome{}, err
	}
	var buf bytes.Buffer
	if len(data) > 0 && data[len(data)-1] != '\n' {
		buf.WriteString(tornSeal)
	}
	buf.Write(line)
	buf.WriteByte('\n')
	if _, err := f.Write(buf.Bytes()); err != nil {
		return Outcome{}, &JournalUnreadableError{Path: j.Path, Err: err}
	}
	if err := f.Sync(); err != nil {
		return Outcome{}, &JournalUnreadableError{Path: j.Path, Err: err}
	}
	return Outcome{Status: OutcomeRegistered, Record: next}, nil
}
