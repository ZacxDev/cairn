package plugins

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

	"github.com/ZacxDev/cairn/internal/arcs"
	"github.com/ZacxDev/cairn/internal/store"
	"github.com/ZacxDev/cairn/internal/write"
)

// 🔴 TOGGLES: DEFAULT OFF, SET PER SCOPE AND PER ARC (O5), AND EVERY FINER LEVEL CAN ONLY NARROW
// (decision 10). Revision 1's fold, under which an arc or a session ON overrode a scope OFF, is
// RETRACTED: arc membership is whatever an arc `PUT` names, with no admin needed, so an arc that
// could widen would hand the consent of a scope's admins to whoever registers arcs. For the same
// reason an arc toggle covers the members SNAPSHOTTED when an admin set it ([Record.Members]),
// never the arc's registration as it reads later.

// Level is where a toggle is set.
type Level string

const (
	// LevelScope is set by `admin` on the scope (`control.Resolve`).
	LevelScope Level = "scope"
	// LevelArc is set by `admin` on the arc's HOME scope.
	LevelArc Level = "arc"
	// LevelSession is the session OWNER's opt-out. It can only say OFF.
	LevelSession Level = "session"
)

// The values each level takes.
const (
	// ValueOff is a scope's default, an arc's veto and the owner's opt-out.
	ValueOff = "off"
	// ValueAllowed is a scope's CONSENT without running: nothing runs because of it alone.
	ValueAllowed = "allowed"
	// ValueOn is a scope's consent AND run, or an arc's run inside consenting scopes.
	ValueOn = "on"
	// ValueUnset is an arc's or a session's default: the level says nothing.
	ValueUnset = "unset"
)

// ErrSessionOn is the refusal of a session-level ON (decision 10, case 10): the owner's level can
// only narrow, so an ON there would be a way to enable something no scope admin consented to.
var ErrSessionOn = errors.New("a session-level toggle can only be off or unset — it can never turn a plugin on")

// ValidValue reports whether v is a value `level` takes. A session-level ON is [ErrSessionOn].
func ValidValue(level Level, v string) error {
	ok := false
	switch level {
	case LevelScope:
		ok = v == ValueOff || v == ValueAllowed || v == ValueOn
	case LevelArc:
		ok = v == ValueUnset || v == ValueOn || v == ValueOff
	case LevelSession:
		if v == ValueOn {
			return ErrSessionOn
		}
		ok = v == ValueUnset || v == ValueOff
	default:
		return fmt.Errorf("level %q is not scope, arc or session", level)
	}
	if !ok {
		return fmt.Errorf("%q is not a value the %s level takes", v, level)
	}
	return nil
}

// ScopeTarget is the key a scope toggle is journaled under: the scope's STORE name, folded the
// way every reader folds it — the names in a visibility set are store names (`codesrc.Key`'s rule,
// and for its reason: a control id is not what a stored session's `V` holds).
func ScopeTarget(name string) string { return store.NormalizeRef(name) }

// ArcTarget is the key an arc toggle is journaled under.
func ArcTarget(k arcs.Key) string { return k.Home + "/" + k.Slug }

// By is the principal that set a toggle: `(Kind, ID)`, never `Display`.
type By struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// Record is ONE journal line (decision 10): `{plugin, level, target, value, by, at}`, `at` the
// pod's clock — and, on an ARC record, `members`.
type Record struct {
	Plugin string `json:"plugin"`
	Level  Level  `json:"level"`
	Target string `json:"target"`
	Value  string `json:"value"`
	By     By     `json:"by"`
	At     string `json:"at"`
	// Members is an ARC record's SNAPSHOT: the session ids the arc listed at the moment an admin of
	// its home set this value. Arc level only; absent on every other level.
	//
	// 🔴 AN ARC TOGGLE COVERS EXACTLY THIS LIST, AND NOTHING THE ARC'S REGISTRATION SAYS LATER (#270's
	// audit, closed in S6). Arc membership is whatever an arc `PUT` names, and any WRITER of the home
	// may `PUT`: read live, a writer added a session to an ON arc and it went ON with no admin act —
	// and REMOVED one from an OFF arc and lifted the admin's veto. "It must have touched the home" did
	// not close that either: a trailer naming a session id is self-declared, by the same writer. So
	// the admin's act speaks for the members it could see, a later registration changes nothing in
	// either direction, and covering a member added since takes an admin setting the toggle again.
	// A record with NO snapshot covers nobody.
	//
	// ⚠ A build older than this field refuses the line (unknown key) and counts it `Skipped`, so
	// after a rollback an arc toggle reads `unset` — fail closed for an ON, and an OFF veto is lost
	// until the toggle is saved again.
	Members []string `json:"members,omitempty"`
}

type toggleKey struct {
	plugin string
	level  Level
	target string
}

// State is the journal folded: last write wins per `(plugin, level, target)`.
type State struct {
	latest map[toggleKey]Record
	// Skipped counts complete lines that did not parse or validate — counted and never applied.
	Skipped int
	// TornTail is a final line with no newline: a crash mid-append, ignored.
	TornTail bool
}

// Value is the latest value set at `(plugin, level, target)`, or `fallback`.
func (s State) Value(plugin string, level Level, target, fallback string) string {
	if r, ok := s.latest[toggleKey{plugin, level, target}]; ok {
		return r.Value
	}
	return fallback
}

// Scope is a scope's value for a plugin: `off` unless set.
func (s State) Scope(plugin, name string) string {
	return s.Value(plugin, LevelScope, ScopeTarget(name), ValueOff)
}

// Arc is an arc's value for a plugin: `unset` unless set.
func (s State) Arc(plugin string, k arcs.Key) string {
	return s.Value(plugin, LevelArc, ArcTarget(k), ValueUnset)
}

// Session is a session's opt-out for a plugin: `unset` unless set.
func (s State) Session(plugin, root string) string {
	return s.Value(plugin, LevelSession, root, ValueUnset)
}

// ArcMembers is the member snapshot the latest record for `(plugin, arc)` carries, and whether any
// record exists for it. An `unset` record covers nothing whatever it carries.
func (s State) ArcMembers(plugin string, k arcs.Key) ([]string, bool) {
	r, ok := s.latest[toggleKey{plugin, LevelArc, ArcTarget(k)}]
	if !ok {
		return nil, false
	}
	return r.Members, true
}

// ArcsCovering is every arc whose toggle for `plugin` COVERS one of `ids` (a root session and its
// children) — whose latest record, `on` or `off`, snapshotted one of them as a member — sorted. It
// is what [Subject.Arcs] is built from: the journal's own snapshots, never the live arc registry.
func (s State) ArcsCovering(plugin string, ids []string) []arcs.Key {
	var out []arcs.Key
	for k, r := range s.latest {
		if k.plugin != plugin || k.level != LevelArc || r.Value == ValueUnset {
			continue
		}
		if !slices.ContainsFunc(r.Members, func(m string) bool { return slices.Contains(ids, m) }) {
			continue
		}
		// An arc target is `<home>/<slug>` ([ArcTarget]); a home is a folded scope name and holds no `/`.
		home, slug, ok := strings.Cut(k.target, "/")
		if !ok {
			continue
		}
		out = append(out, arcs.Key{Home: home, Slug: slug})
	}
	slices.SortFunc(out, func(a, b arcs.Key) int {
		if c := strings.Compare(a.Home, b.Home); c != 0 {
			return c
		}
		return strings.Compare(a.Slug, b.Slug)
	})
	return out
}

// AnyConsent reports whether ANY scope holds `allowed` or `on` for the plugin. Without one, rule (1)
// of [Effective] refuses every session with a non-empty `V`, and an empty `V` is OFF — so no session
// can be ON and a caller need not look at any (the pending query's zero-walk short-circuit).
//
// ⚠ IT ASKS THE DEFAULT THROUGH [State.Scope] TOO, so it can never disagree with the fold about a
// scope nobody toggled: a default that read anything but `off` makes every session a candidate.
func (s State) AnyConsent(plugin string) bool {
	if v := s.Scope(plugin, "never-toggled-scope-default-probe"); v == ValueAllowed || v == ValueOn {
		return true
	}
	for k, r := range s.latest {
		if k.plugin == plugin && k.level == LevelScope && (r.Value == ValueAllowed || r.Value == ValueOn) {
			return true
		}
	}
	return false
}

// Last is the record that set a value, for "set by … at …" on a page.
func (s State) Last(plugin string, level Level, target string) (Record, bool) {
	r, ok := s.latest[toggleKey{plugin, level, target}]
	return r, ok
}

// Subject is what [Effective] decides about: one stored root session.
type Subject struct {
	// Root is the root session id (the session level's target).
	Root string
	// V is the session's WHOLE visibility set (decision 3) — written AND read scopes, `*` included.
	V []string
	// Arcs is every arc whose toggle for the plugin COVERS the root or one of its child sessions:
	// the member snapshot an admin's act recorded ([State.ArcsCovering], [Record.Members]) — never
	// the arc registry as it reads now.
	Arcs []arcs.Key
}

// Effective is decision 10's fold: is `plugin` ON for this session?
//
// ON iff (1) EVERY scope in `V` is a scope this authority still holds AND is `allowed` or `on` —
// the consent AND, applied regardless of every other level; AND (2) EITHER every scope in `V` is
// `on`, OR some arc in `sub.Arcs` HOMED IN A SCOPE OF `V` is `on`; AND (3) no such arc is `off`; AND
// (4) the owner has not opted out. An EMPTY `V` resolves OFF. An arc homed outside `V` says nothing
// about the session (#270 round 1 — the plan's decision 10 named no such limit).
//
// 🔴 `sub.Arcs` IS THE ARCS WHOSE TOGGLE COVERS THE SESSION — the member list an ADMIN's act
// snapshotted ([Record.Members]) — so BOTH limits hold together: the session was a member when the
// home's admin set the toggle, AND its `V` holds the home.
//
// 🔴 `known` IS ASKED OF EVERY NAME, AND A NAME IT DOES NOT KNOW IS OFF WHATEVER THE JOURNAL SAYS.
// A scope deleted or renamed after its admins consented keeps its journal line; the session's
// content from it is still in the transcript, and nobody who could administer it is left to say no.
// ⚠ Keyed by NAME: a scope RECREATED under that name is known again and inherits the old line.
// `*` is "a scope nobody can name" and is OFF the same way.
func Effective(st State, plugin string, sub Subject, known func(string) bool) bool {
	// 🔴 (9) EMPTY `V` IS OFF — "every scope of the empty set" is vacuously true, and an owner-only
	// session is exactly one no scope admin was ever asked about.
	if len(sub.V) == 0 {
		return false
	}
	consented, everyOn := true, true
	inV := map[string]bool{}
	for _, scope := range sub.V {
		inV[ScopeTarget(scope)] = true
		if scope == "*" || !known(scope) {
			return false
		}
		switch st.Scope(plugin, scope) {
		case ValueOn:
		case ValueAllowed:
			everyOn = false
		default:
			consented, everyOn = false, false
		}
	}
	arcOn, arcOff := false, false
	for _, k := range sub.Arcs {
		// 🔴 AN ARC SPEAKS ONLY FOR A SESSION THAT TOUCHED ITS HOME (#270 round 1). An arc whose home
		// is outside `V` would let an admin of an unrelated scope turn the plugin ON (or OFF) for
		// sessions that scope never saw, by listing their ids in their own arc before setting it.
		// Inside `V`, the home's admin set the arc and the home's admins (the same authority) are
		// among those whose consent rule (1) already requires.
		if !inV[ScopeTarget(k.Home)] {
			continue
		}
		switch st.Arc(plugin, k) {
		case ValueOff:
			arcOff = true
		case ValueOn:
			arcOn = true
		}
	}
	// 🔴 THE CONSENT AND IS NOT INSIDE THE OR. Revision 1 read `(consented || arcOn) && …` in effect —
	// an arc ON overrode a scope OFF — and arc membership is whatever an arc `PUT` names.
	return consented && (everyOn || arcOn) && !arcOff && st.Session(plugin, sub.Root) != ValueOff
}

// Settled reports that `plugin` is OFF for `sub` AND FOR EVERY SUPERSET of `sub.V` — the only case
// in which a caller holding part of a session's visibility set may stop looking for the rest
// (`transcript.Gate.Settled`). `sub.Arcs` is [State.ArcsCovering], as for [Effective].
//
// 🔴 [Effective] SAYING OFF IS NOT ENOUGH, BECAUSE THE FOLD IS NOT MONOTONE IN `V`: an arc counts
// only when its home is in `V`, so a scope ADDED to `V` can be the home of an ON arc. What cannot
// be undone by adding scopes is exactly:
//   - a scope already in the set that is `*`, unknown, or not consented (rule 1 fails for good);
//   - the owner's opt-out (rule 4);
//   - an `off` arc covering the session whose home is ALREADY in the set (rule 3);
//   - some scope in the set merely `allowed` AND no `on` arc covering the session at all, whatever
//     its home (rule 2 can never be met: "every scope on" is already false, and no arc can turn on).
//
// Anything else — in particular an `on` arc covering the session whose home the set does not hold
// YET — is not settled, and the caller must compute the whole set. An empty set is never settled.
func Settled(st State, plugin string, sub Subject, known func(string) bool) bool {
	if len(sub.V) == 0 {
		return false
	}
	if st.Session(plugin, sub.Root) == ValueOff {
		return true
	}
	everyOn := true
	inV := map[string]bool{}
	for _, scope := range sub.V {
		inV[ScopeTarget(scope)] = true
		if scope == "*" || !known(scope) {
			return true
		}
		switch st.Scope(plugin, scope) {
		case ValueOn:
		case ValueAllowed:
			everyOn = false
		default:
			return true
		}
	}
	anArcIsOn := false
	for _, k := range sub.Arcs {
		switch st.Arc(plugin, k) {
		case ValueOff:
			if inV[ScopeTarget(k.Home)] {
				return true
			}
		case ValueOn:
			anArcIsOn = true
		}
	}
	return !everyOn && !anArcIsOn
}

// Journal is `-plugin-journal`: one JSON record per line, append-only — the arc journal's write
// discipline (decision 10): `flock`, ONE `O_APPEND|O_NOFOLLOW` write, `fsync`, 0600, a torn tail
// sealed before the next append. The path is resolved by `arcs.ResolveJournalPathNamed` at startup,
// which refuses it inside the store root.
type Journal struct{ Path string }

// ErrJournal marks a journal that is configured and could not be read or written — answered as
// "could not look", never as "every toggle is off".
var ErrJournal = errors.New("the plugin toggle journal could not be read or written")

// Read folds the whole journal. A missing file is an empty state (every toggle at its default).
func (j Journal) Read() (State, error) {
	data, err := os.ReadFile(j.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return State{latest: map[toggleKey]Record{}}, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("%w: %s: %v", ErrJournal, j.Path, err)
	}
	return foldToggles(data), nil
}

// foldToggles is the read rule over bytes.
func foldToggles(data []byte) State {
	st := State{latest: map[toggleKey]Record{}}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		st.TornTail = true
		data = data[:bytes.LastIndexByte(data, '\n')+1]
	}
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var r Record
		dec := json.NewDecoder(bytes.NewReader(line))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&r); err != nil || validRecord(r) != nil {
			st.Skipped++
			continue
		}
		if _, err := dec.Token(); err != io.EOF {
			st.Skipped++
			continue
		}
		st.latest[toggleKey{r.Plugin, r.Level, r.Target}] = r
	}
	return st
}

func validRecord(r Record) error {
	if !ValidName(r.Plugin) || r.Target == "" || r.By.Kind == "" || r.By.ID == "" {
		return errors.New("incomplete record")
	}
	if _, err := time.Parse(time.RFC3339, r.At); err != nil {
		return err
	}
	// A member snapshot belongs to an ARC record and to nothing else, and is the arc registry's own
	// shape: at most [arcs.MaxMembers] session ids.
	if len(r.Members) > 0 && r.Level != LevelArc {
		return errors.New("only an arc record carries a member snapshot")
	}
	if len(r.Members) > arcs.MaxMembers {
		return fmt.Errorf("a member snapshot holds at most %d sessions", arcs.MaxMembers)
	}
	for _, m := range r.Members {
		if !write.SessionComponent.MatchString(m) {
			return errors.New("a member snapshot holds a value that is not a session id")
		}
	}
	return ValidValue(r.Level, r.Value)
}

// tornSeal is the arc journal's seal, for its reason (`arcs.tornSeal`).
const tornSeal = " !torn-tail-sealed\n"

// Append records ONE toggle. It is the journal's only writer.
func (j Journal) Append(r Record) error {
	if err := validRecord(r); err != nil {
		return fmt.Errorf("refusing to journal a malformed toggle: %w", err)
	}
	line, err := json.Marshal(r)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(j.Path, os.O_CREATE|os.O_RDWR|os.O_APPEND|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrJournal, j.Path, err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrJournal, j.Path, err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	var buf bytes.Buffer
	if info, err := f.Stat(); err == nil && info.Size() > 0 {
		last := make([]byte, 1)
		if _, err := f.ReadAt(last, info.Size()-1); err == nil && last[0] != '\n' {
			buf.WriteString(tornSeal)
		}
	}
	buf.Write(line)
	buf.WriteByte('\n')
	if _, err := f.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrJournal, j.Path, err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrJournal, j.Path, err)
	}
	return nil
}
