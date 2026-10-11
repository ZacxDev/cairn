package codesrc

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/ZacxDev/cairn/internal/arcs"
	"github.com/ZacxDev/cairn/internal/identity"
)

// 🔴 THE JOURNAL: ONE JSON OBJECT PER LINE, APPEND-ONLY, NEVER COMPACTED — the arc registry's
// pattern (`internal/arcs/journal.go`), which the operator chose for this (the plan's O5). The
// file IS the history; the fold below turns it into "each scope's current declaration".
//
// 🔴 IT LIVES OUTSIDE THE STORE TREE, AND `ResolveJournalPath` IS WHAT MAKES THAT A FACT. The
// pod serves a COPY of the store that a re-seed overwrites wholesale, so a declaration written
// inside it would be silently reverted; and the token-file authority enumerates every directory
// at the store root as a scope.

// EnvJournal is the ONE configuration of the journal's path. 🔴 AN ENVIRONMENT VARIABLE AND NO
// FLAG, ON PURPOSE: a pre-feature binary handed an unknown FLAG refuses to start, but it ignores
// an unknown variable — so the variable is the rollback story. There is no default: unset is the
// designed OFF state (`sources-unconfigured`).
const EnvJournal = "CAIRN_SOURCE_JOURNAL"

// journalNoun is what every refusal about this file calls it. Never "arc journal": a refusal
// naming the wrong journal sends the operator to edit the wrong variable.
const journalNoun = "sources journal ($" + EnvJournal + ")"

// Schema is the one record schema this package reads and writes.
const Schema = 1

// RevisionNone is the revision of a scope with no record. A form rendered for an undeclared
// scope carries it, so the FIRST declaration also goes through the compare.
const RevisionNone = "none"

// Record is one line of the journal.
//
// 🔴 UNKNOWN FIELDS ARE IGNORED, NOT REFUSED (the plan's decision 1): a record written by a newer
// build must still fold in an older one — the opposite of arcs' `DisallowUnknownFields`, because
// here an old reader refusing a new line would turn "a newer UI wrote this" into "undeclared".
type Record struct {
	Schema   int      `json:"schema"`
	Scope    string   `json:"scope"`
	Sources  []string `json:"sources"`
	SetBy    string   `json:"set_by"`
	SetAt    string   `json:"set_at"`
	Revision string   `json:"revision"`
}

// Revision is the digest of a scope key and its canonical sources — what a form carries and
// `Set` compares. Content-derived, so two writers that converge on the same list agree on it.
func Revision(scope string, sources []string) string {
	if sources == nil {
		sources = []string{}
	}
	// The COMPACT JSON of a two-element array, `["<scope>",["<source>",…]]`: unambiguous for any
	// strings, with no separator to escape. HTML escaping is OFF so the digest is the plain spelling
	// any JSON encoder produces (a test fixture written in another language can compute it).
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode([]any{scope, sources})
	sum := sha256.Sum256(bytes.TrimSuffix(buf.Bytes(), []byte("\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// valid is the read rule's per-record check. 🔴 THE FOLD RE-VALIDATES EVERYTHING IT APPLIES —
// the key, every source through the ONE parser (and canonical as written), the cap, the stamp and
// the revision — so a hand-edited line can be skipped and counted but never served.
func (r Record) valid() bool {
	if r.Schema != Schema || r.Scope == "" || r.Scope != Key(r.Scope) || r.SetBy == "" || r.Sources == nil {
		return false
	}
	if _, err := time.Parse(time.RFC3339, r.SetAt); err != nil {
		return false
	}
	srcs, err := ParseList(r.Sources)
	if err != nil || !slices.Equal(Canonicals(srcs), r.Sources) {
		return false
	}
	return r.Revision == Revision(r.Scope, r.Sources)
}

// JournalUnreadableError is "the journal is configured and could NOT be read" — the read rule's
// BROKEN state, answered as `store-unreachable` (503) and never as "undeclared".
type JournalUnreadableError struct {
	Path string
	Err  error
}

func (e *JournalUnreadableError) Error() string {
	return fmt.Sprintf("code sources journal unreadable: %s (%v) — this is NOT 'no sources declared'", e.Path, e.Err)
}

func (e *JournalUnreadableError) Unwrap() error { return e.Err }

// StaleRevisionError is a lost race: the scope's revision is no longer the one the writer read.
// It carries the CURRENT revision so the form can re-render the current list.
type StaleRevisionError struct {
	Scope, Want, Current string
}

func (e *StaleRevisionError) Error() string {
	return fmt.Sprintf("the sources of %q changed since they were read (expected revision %s, now %s); nothing was written",
		e.Scope, e.Want, e.Current)
}

// Snapshot is one read of the journal, folded.
type Snapshot struct {
	// Latest is the LAST VALID record per scope key. History is in the file.
	Latest map[string]Record
	// Records is how many complete, valid lines were read.
	Records int
	// Skipped is how many COMPLETE lines did not parse or validate — counted, never applied. One
	// bad line does not poison the rest: refusing the whole journal would turn one damaged record
	// into "every scope is undeclared".
	Skipped int
	// TornTail is true when the file did not end in a newline: the last fragment is a partial
	// write and is IGNORED, not parsed.
	TornTail bool
	// Missing is true when the file does not exist — configured, never written. EMPTY, not broken.
	Missing bool
}

// Damaged is true when something in the journal was not applied.
func (s Snapshot) Damaged() bool { return s.Skipped > 0 || s.TornTail }

// For is the scope's current record, if it has one. `scope` is a NAME; it is keyed here.
func (s Snapshot) For(scopeName string) (Record, bool) {
	r, ok := s.Latest[Key(scopeName)]
	return r, ok
}

// RevisionFor is the scope's current revision, `RevisionNone` when it has no record. Like `For`,
// it takes a scope NAME and keys it here — one rule, in one place, for both lookups.
func (s Snapshot) RevisionFor(scopeName string) string {
	if r, ok := s.Latest[Key(scopeName)]; ok {
		return r.Revision
	}
	return RevisionNone
}

// Journal is the sources file. `Path` must be one `ResolveJournalPath` returned.
type Journal struct{ Path string }

// FromEnv is the ONE reader of `EnvJournal`: every binary that configures the journal calls it,
// so they cannot disagree about a value. `ok` false is the designed OFF state (unset, or set to
// the empty string); any error is a refusal to start:
//   - a value that REDUCES TO NOTHING (whitespace, or nothing graphic — `identity`'s one rule for a
//     blank setting): an operator who wrote the line meant a journal, and `"   "` would otherwise
//     resolve to a file named `   ` in the working directory;
//   - a path resolving inside the store root (`ResolveJournalPath`);
//   - a path resolving to `arcJournal` — the arc registry's journal, already resolved, or "" when
//     none is configured. Each reader would read the other's lines as damaged records, and the
//     sources writer would append into the arc registry.
func FromEnv(storeRoot string, lookup func(string) (string, bool), arcJournal string) (j Journal, ok bool, err error) {
	v, set := lookup(EnvJournal)
	if !set || v == "" {
		return Journal{}, false, nil
	}
	if identity.ValueReducesToNothing(v) {
		return Journal{}, false, &BlankError{}
	}
	resolved, err := ResolveJournalPath(storeRoot, v)
	if err != nil {
		return Journal{}, false, err
	}
	if arcJournal != "" && resolved == arcJournal {
		return Journal{}, false, &SharedWithArcJournalError{Path: resolved}
	}
	return Journal{Path: resolved}, true, nil
}

// BlankError is `EnvJournal` set to a value that reduces to nothing.
type BlankError struct{}

func (*BlankError) Error() string {
	return "$" + EnvJournal + " is set to a value that reduces to nothing; set a path or remove the line"
}

// SharedWithArcJournalError is the sources journal resolving to the arc registry's journal.
type SharedWithArcJournalError struct{ Path string }

func (e *SharedWithArcJournalError) Error() string {
	return fmt.Sprintf("$%s resolves to %s, which is the ARC journal: each reader would read the other's "+
		"records as damaged lines. Give the sources journal its own file", EnvJournal, e.Path)
}

// ResolveJournalPath is `arcs.ResolveJournalPath`'s resolution — ONE rule for "outside the store
// tree" on every journal — with every refusal naming the SOURCES journal and `EnvJournal`.
func ResolveJournalPath(storeRoot, journal string) (string, error) {
	return arcs.ResolveJournalPathNamed(journalNoun, storeRoot, journal)
}

// Read reads and folds the whole journal: absent → an empty snapshot flagged `Missing`; any
// other failure → `*JournalUnreadableError`. It opens `O_RDONLY|O_NOFOLLOW`: the reader never
// needs write access (the pod mounts the file read-only), and a symlink appearing at the
// resolved path after startup is refused rather than followed somewhere the startup check
// never saw.
func (j Journal) Read() (Snapshot, error) {
	f, err := os.OpenFile(j.Path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return Snapshot{Latest: map[string]Record{}, Missing: true}, nil
	}
	if err != nil {
		return Snapshot{}, &JournalUnreadableError{Path: j.Path, Err: err}
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		// A DIRECTORY at the path opens and then fails here (EISDIR): broken, not empty.
		return Snapshot{}, &JournalUnreadableError{Path: j.Path, Err: err}
	}
	return fold(data), nil
}

// fold is the read rule over bytes. LATEST VALID WINS per scope key.
//
// 🔴 THE TORN TAIL IS DECIDED BY THE LAST BYTE, NOT BY WHETHER THE LAST LINE PARSES — arcs'
// rule, for arcs' reason: every complete record is written with its newline in ONE `write(2)`,
// so "no trailing newline" is exactly "the final write did not finish", and a fragment that
// happened to parse is still never applied.
func fold(data []byte) Snapshot {
	snap := Snapshot{Latest: map[string]Record{}}
	if len(data) == 0 {
		return snap
	}
	if data[len(data)-1] != '\n' {
		snap.TornTail = true
		data = data[:bytes.LastIndexByte(data, '\n')+1]
	}
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var r Record
		dec := json.NewDecoder(bytes.NewReader(line))
		if err := dec.Decode(&r); err != nil || !r.valid() {
			snap.Skipped++
			continue
		}
		// ONE object per line and nothing after it, which is also what makes `tornSeal` work.
		if _, err := dec.Token(); err != io.EOF {
			snap.Skipped++
			continue
		}
		snap.Records++
		snap.Latest[r.Scope] = r
	}
	return snap
}

// tornSeal is what `Set` appends after a torn tail before its own record — arcs' marker, for
// arcs' measured reason: a bare newline would turn a fragment that is a whole object missing
// only its newline into an APPLIED line. This one carries no `"`, `}` or `]`, so it can never
// close a fragment into valid JSON; the sealed line is always skipped and counted.
const tornSeal = " !torn-tail-sealed\n"

// Set declares `scope`'s sources, if and only if the scope's CURRENT revision is `ifRevision`
// (`RevisionNone` for a scope with no record), stamping `by` and `now`. It returns the record it
// appended.
//
// `scope` must already be a KEY (`Key(name)`); `sources` are re-validated through `ParseList`, so
// the writer accepts nothing the reader would skip (`TestEveryAcceptedSourceRoundTripsThroughTheJournal`).
//
// 🔴 THE REVISION IS COMPARED *INSIDE* THE LOCK, AND THAT IS THE WHOLE GUARANTEE: exactly one of
// two writes carrying the same revision lands; the other gets `*StaleRevisionError`. An exclusive
// `flock` covers the read-merge-append, the re-read goes through the LOCKED DESCRIPTOR (never the
// path, which a rename could have repointed), then ONE `write(2)` under `O_APPEND`, then `fsync`.
// `TestTwoWritesCarryingOneRevisionLandExactlyOnce` drives the race through `interleave`, which
// runs inside the lock between the compare and the append — the window a compare outside the lock
// leaves open. ⚠ `flock` is advisory-only across hosts on a network filesystem; the reference
// deployment has one writer on a node-local volume (arcs' stated scope, unchanged).
//
// ⚠ A resubmitted UNCHANGED list carrying the now-stale revision is refused the same way. That is
// a harmless no-op the user sees as "already current", not a lost update.
//
// `O_NOFOLLOW`: a symlink appearing at the resolved path after startup is refused, not followed.
func (j Journal) Set(scope string, sources []string, ifRevision, by string, now time.Time, interleave func()) (Record, error) {
	if scope == "" || scope != Key(scope) {
		return Record{}, fmt.Errorf("codesrc: %q is not a scope key; pass Key(<scope name>)", scope)
	}
	if by == "" {
		return Record{}, errors.New("codesrc: set_by is empty; the caller stamps the signed-in principal")
	}
	if !utf8.ValidString(by) {
		// The same reason `Parse` refuses invalid UTF-8: the JSON encoder would rewrite it, and the
		// record read back would not carry the principal that was stamped.
		return Record{}, errors.New("codesrc: set_by is not valid UTF-8")
	}
	srcs, err := ParseList(sources)
	if err != nil {
		return Record{}, err
	}
	canon := Canonicals(srcs)
	rec := Record{
		Schema:   Schema,
		Scope:    scope,
		Sources:  canon,
		SetBy:    by,
		SetAt:    now.UTC().Format(time.RFC3339),
		Revision: Revision(scope, canon),
	}

	f, err := os.OpenFile(j.Path, os.O_CREATE|os.O_RDWR|os.O_APPEND|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return Record{}, &JournalUnreadableError{Path: j.Path, Err: err}
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return Record{}, &JournalUnreadableError{Path: j.Path, Err: err}
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return Record{}, &JournalUnreadableError{Path: j.Path, Err: err}
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return Record{}, &JournalUnreadableError{Path: j.Path, Err: err}
	}
	current := fold(data).RevisionFor(scope)
	if current != ifRevision {
		return Record{}, &StaleRevisionError{Scope: scope, Want: ifRevision, Current: current}
	}
	if interleave != nil {
		interleave()
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return Record{}, err
	}
	var buf bytes.Buffer
	if len(data) > 0 && data[len(data)-1] != '\n' {
		buf.WriteString(tornSeal)
	}
	buf.Write(line)
	buf.WriteByte('\n')
	if _, err := f.Write(buf.Bytes()); err != nil {
		return Record{}, &JournalUnreadableError{Path: j.Path, Err: err}
	}
	if err := f.Sync(); err != nil {
		return Record{}, &JournalUnreadableError{Path: j.Path, Err: err}
	}
	return rec, nil
}
