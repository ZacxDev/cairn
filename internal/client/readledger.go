package client

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/ZacxDev/cairn/internal/report"
	"github.com/ZacxDev/cairn/internal/store"
	"github.com/ZacxDev/cairn/internal/write"
)

// THE CLIENT READ LEDGER (`claudedocs/plan-cairn-plugins.md`, decision 3a, operator decision O11;
// slice S11): when this process runs inside an agent session — `CLAUDE_CODE_SESSION_ID` or
// `OPENCODE_SESSION_ID` is set — every scope a SERVED call resolved and touched is appended, one
// JSON line per scope, to `$XDG_STATE_HOME/cairn/read-ledger/<session>.jsonl`. The capture agent
// (`cmd/cairn-capture`) reads that file for routing and uploads it as the session's `ledger`
// stream, and `cairn-ui` folds it into the session's visibility set `V`. The parser on the other
// end is `internal/transcript/scopeuse.(*Deriver).Ledger`, which reads ONLY `scope`.
//
// 🔴 THE RECORD NAMES THE SCOPE THE CALL RESOLVED, NEVER THE FLAG AS TYPED. A bare `recall` in a
// repository records the scope `--repo` derived, `ls-entries` records every scope it listed, and a
// call whose reach this client cannot enumerate — `search --all-scopes`, and the content verbs that
// print what OTHER scopes hold (`doctor`, `routes`, `arcs`, `arc-show`) — records `*`, which the pod
// reads as "unknown scope": owner-only. A record that names too much only hides a session from MORE
// people; one that names too little shows its transcript to someone who cannot read what it read.
// So every call site records at the point the content is in hand, before it is printed, and a
// refused call (a usage error, an unroutable scope, an unreachable store with no cache) records
// nothing because it never reaches one.
//
// 🔴 INVISIBLE TO THE CLIENT'S CONTRACT. Every error here — no state directory, a read-only one, a
// symlink where the file should be, a full disk — is SWALLOWED: nothing on stdout or stderr, no
// change to the exit code. That is what keeps `tests/parity/` (which diffs stdout, stderr and the
// exit code byte for byte) unchanged by this file, and what keeps a broken ledger from breaking the
// tool an agent is using. The cost is stated in threat T3: a write that fails leaves the ledger
// SHORT, and the pod's F1 fallback covers it only when the ledger is entirely empty.
//
// ⚠ THE LEDGER IS SELF-REPORTED by the host, like a write trailer: anything on the host can edit or
// delete it, which can only make `V` smaller. It holds scope names only — no arguments, no query
// text, no content — so it needs no redaction.

// The two session-id variables (decision 3a). Claude Code sets the first for tool commands, with the
// ROOT session's id inside a subagent; opencode sets neither — the measured host exports the second
// from its own plugin, with the CHILD's id inside a subagent, and the capture agent maps a child id to
// its root (decision 7).
//
// 🔴 `OPENCODE_SESSION_ID` WINS WHEN BOTH ARE SET. An opencode run dispatched from a Claude Code
// Bash tool INHERITS the Claude id, so the Claude id names the OUTER session and the opencode id
// the one actually running this call (#266's audit). Preferring the Claude id filed the opencode
// session's reads under the session that only launched it.
const (
	ClaudeSessionEnv   = "CLAUDE_CODE_SESSION_ID"
	OpencodeSessionEnv = "OPENCODE_SESSION_ID"
)

// LedgerClass is what one verb's served call records (#266's audit, round 0): the Go verb table
// carries one per verb, the Python oracle mirrors the table (`lib/read_ledger.py`'s `VERB_CLASS`),
// and `tests/test_read_ledger.py` holds the two equal.
type LedgerClass string

const (
	// LedgerResolved: the scope the call RESOLVED and touched — never the flag as typed — plus
	// every OTHER scope its output names ([Env.recordNamed]; `*` for a store-wide listing). A
	// widening flag the client cannot enumerate (`search --all-scopes`) records `*` instead.
	LedgerResolved LedgerClass = "resolved"
	// LedgerListed: one record per scope the call LISTED.
	LedgerListed LedgerClass = "listed"
	// LedgerAll: exactly one `*` — output that names scopes this client does not enumerate.
	LedgerAll LedgerClass = "all"
	// LedgerNone: nothing; the verb prints no store content.
	LedgerNone LedgerClass = "none"
)

// LedgerSchema is the record's `schema`.
const LedgerSchema = 1

// LedgerAllScopes is the `*` a call records when it cannot enumerate the scopes its output names.
const LedgerAllScopes = "*"

// LedgerRecord is one line of the ledger. The field ORDER is the wire order, and the Python oracle
// (`lib/read_ledger.py`) writes the same keys in the same order; `tests/parity/` compares the two
// clients' files with `at`, `client` and `client_version` normalised.
type LedgerRecord struct {
	Schema        int    `json:"schema"`
	Session       string `json:"session"`
	Verb          string `json:"verb"`
	Instance      string `json:"instance"`
	Scope         string `json:"scope"`
	Client        string `json:"client"`
	ClientVersion string `json:"client_version"`
	At            string `json:"at"`
}

// readLedger is one call's ledger: which session, which verb, where, and what it already recorded.
// A nil *readLedger records nothing — no session id, or a session id that is not one.
type readLedger struct {
	path    string
	session string
	verb    string
	seen    map[[2]string]bool
	// now is the clock; tests pin it.
	now func() time.Time
}

// ledgerSession is the session id this process runs under, or "" — a value that is not a valid
// session component (`write.SessionComponent`, the pod's own root-id grammar) is no session: it
// could not name a file safely and the pod would refuse it as a root anyway.
func ledgerSession() string {
	v := getenv(OpencodeSessionEnv)
	if v == "" {
		v = getenv(ClaudeSessionEnv)
	}
	if !write.SessionComponent.MatchString(v) {
		return ""
	}
	return v
}

// LedgerDir is `$XDG_STATE_HOME/cairn/read-ledger`, `~/.local/state` when the variable is unset or
// not absolute — the directory `cairn-capture -ledger-dir` defaults to.
func LedgerDir() (string, error) {
	base, err := stateHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "cairn", "read-ledger"), nil
}

// stateHome is the XDG state base (`memoStateDir` reads the same rule).
func stateHome() (string, error) {
	base := getenv(StateHomeEnv)
	if base == "" || !filepath.IsAbs(base) {
		home, err := homeDir()
		if err != nil || home == "" {
			return "", os.ErrNotExist
		}
		base = filepath.Join(home, ".local", "state")
	}
	return base, nil
}

// openReadLedger is the ledger for one call of `verb`, or nil when there is no session.
func openReadLedger(verb string) *readLedger {
	session := ledgerSession()
	if session == "" {
		return nil
	}
	dir, err := LedgerDir()
	if err != nil {
		return nil
	}
	return &readLedger{path: filepath.Join(dir, session+".jsonl"), session: session, verb: verb,
		seen: map[[2]string]bool{}, now: time.Now}
}

// recordRead appends one record for (instance, scope), once per call.
//
// 🔴 EVERY ERROR IS SWALLOWED HERE, AND THIS IS THE ONE PLACE IT IS: the caller's stdout, stderr and
// exit code must not move because the ledger could not be written (decision 3a, "invisible to the
// client's contract"). `TestAFailedLedgerWriteIsInvisible` compares a broken-ledger run against a
// run with no session id, byte for byte.
func (e Env) recordRead(instance, scope string) {
	if e.ledger != nil {
		_ = e.ledger.record(instance, scope)
	}
}

// recordNamed records every OTHER scope a recall or search report NAMES in what it prints, beside
// the scope it read: a `scope-absent` report lists every scope the store holds ("scopes THIS
// HOST's store holds: …") — a store-wide listing, so `*` — and every status prints the
// "further malformed entries in OTHER scopes" line naming each scope that holds one. A scope name
// is store content too: printing one the session's viewer cannot read is a read of it, by the
// same rule `routes` records `*` under (#266's audit). A malformed entry with no scope is `*`.
func (e Env) recordNamed(instance, status string, elsewhere []store.MalformedEntry) {
	if status == report.StatusScopeAbsent {
		e.recordRead(instance, LedgerAllScopes)
	}
	for _, m := range elsewhere {
		scope := m.Scope
		if scope == "" {
			scope = LedgerAllScopes
		}
		e.recordRead(instance, scope)
	}
}

// recordReads is [Env.recordRead] for several scopes.
func (e Env) recordReads(instance string, scopes []string) {
	for _, s := range scopes {
		e.recordRead(instance, s)
	}
}

func (l *readLedger) record(instance, scope string) error {
	key := [2]string{instance, scope}
	if l.seen[key] {
		return nil
	}
	l.seen[key] = true
	line, err := json.Marshal(LedgerRecord{Schema: LedgerSchema, Session: l.session, Verb: l.verb,
		Instance: instance, Scope: scope, Client: "go", ClientVersion: clientVersion(),
		At: l.now().UTC().Format(time.RFC3339)})
	if err != nil {
		return err
	}
	return appendLedgerLine(l.path, append(line, '\n'))
}

// appendLedgerLine is ONE `O_APPEND` write of one line: directory 0700, file 0600, and a symlink at
// the file's name is refused rather than followed (`O_NOFOLLOW`), so a link planted there cannot
// redirect the append into another file.
//
// 🔴 AND ONLY INTO A REGULAR FILE, OPENED `O_NONBLOCK`. A FIFO at the path blocked the open until a
// reader appeared — the agent's tool call HUNG, the one way this write could become visible (#266's
// audit). Non-blocking, a FIFO with no reader fails at the open (`ENXIO`), and anything that opens
// but is not a regular file (a FIFO with a reader, a device) is refused before the write.
func appendLedgerLine(path string, line []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		return err
	}
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		f.Close()
		return errNotRegular
	}
	_, werr := f.Write(line)
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	return cerr
}

// errNotRegular is a ledger path that is not a regular file.
var errNotRegular = errors.New("the read ledger is not a regular file")

// clientVersion is the build's VCS revision when the toolchain stamped one, else the module
// version (`(devel)` for a local build). The nix build stamps neither, so it reads `(devel)` there;
// the field is informational and the parity row normalises it.
func clientVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" && s.Value != "" {
			return s.Value
		}
	}
	if info.Main.Version != "" {
		return info.Main.Version
	}
	return "unknown"
}
