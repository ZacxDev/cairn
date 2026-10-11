// Package archive is the transcript STORE on `cairn-ui` — slice S3 of
// `claudedocs/plan-cairn-plugins.md` (decision 15): every byte a capture agent SHIPS, in one
// directory per root session, appended under a compare-and-swap, owned by the first uploader,
// re-checked against the redaction table before a byte is written, bounded by a quota and swept by
// a required retention.
//
// 🔴 CAPTURE IS UNARMED ON EVERY INSTANCE (operator decision O16), AND THIS PACKAGE DOES NOT ARM
// IT. The refusal lives one layer up, in `internal/worker`, which answers every upload 503 unless
// `cairn-ui` was started with an explicit arming flag. This package is the storage that arming
// would expose; it has no switch of its own, so nothing here can arm anything.
//
// 🔴 THE POD RE-CHECKS AND REFUSES (decision 6, D4 kept). Every received record and every text
// blob is scanned with `internal/redact`'s table; a match refuses the WHOLE request (422, naming
// the record index or the blob, never the value) and nothing in it is stored. It catches a
// bypassed or out-of-date agent, NOT a redactor miss — the same table cannot catch what it does
// not recognise (T1). That refusal is only possible because the table is quiet on its own
// markers; see `internal/redact/marker.go` for the 1,502 re-hits it had before.
//
// 🔴 THE POD DOES NOT YET DERIVE `V` ITSELF, SO IT RECORDS `*` (fail closed). Decision 3 has the pod
// re-derive `R_header` and `F` over the stored records through `internal/transcript/scopeuse`,
// "so an old or lying agent cannot shrink `V`". That package is slice S2's and is not on `main`;
// re-implementing it here would be the second spelling decision 3 forbids ("one function, two
// callers"). Until it lands, every stored session's derived set is `{*}`, which decision 4 makes
// OWNER-ONLY — the narrow direction. See [derivedScopes].
//
// Layout under the configured directory (decision 15, with one deviation stated):
//
//	journal.jsonl                          the deletion facts — who, which root, why, when; no content
//	sessions/<root>/meta.json              owner, host, runtime, scope sets, positions (the COMMIT point)
//	sessions/<root>/stream-<name>.jsonl.gz append-only gzip members of `{seq, src, rec}` lines
//	sessions/<root>/blobs/<name>           a persisted tool-result file, redacted text or binary as is
//	sessions/<root>/frames/<target>        a record or blob being reassembled from continuation frames
//
// ⚠ THE DEVIATION: the plan writes `<dir>/<root-session>/`. A root id is any
// `write.SessionComponent` — `journal.jsonl` is one — so roots live under `sessions/` and nothing a
// caller names can collide with the journal.
package archive

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/ZacxDev/cairn/internal/arcs"
	"github.com/ZacxDev/cairn/internal/redact"
	"github.com/ZacxDev/cairn/internal/write"
)

// Star is decision 3's sentinel: "a scope nobody can name". Every non-owner is refused a set
// holding it (decision 4).
const Star = "*"

// Runtimes is the closed set of runtimes a session may be uploaded as.
var Runtimes = []string{"claude", "opencode"}

// Bounds on what one request may carry. A request BODY is bounded by the listener; these bound
// the strings inside it, so a log line or a page that renders one stays readable.
const (
	MaxCursorBytes   = 256
	MaxDeclared      = 256
	MaxRecords       = 10000
	MaxFrames        = 4096
	MaxNameBytes     = 128
	sessionsDir      = "sessions"
	journalName      = "journal.jsonl"
	metaName         = "meta.json"
	metaSchema       = 1
	framesDir        = "frames"
	blobsDir         = "blobs"
	streamPrefix     = "stream-"
	streamSuffix     = ".jsonl.gz"
	dirMode          = 0o700
	fileMode         = 0o600
	journalOpenFlags = os.O_WRONLY | os.O_APPEND | os.O_CREATE | syscall.O_NOFOLLOW
)

// idClass is the session-id class (`write.SessionComponent`'s), reused for a subagent's or a child
// session's id inside a stream name.
const idClass = `[A-Za-z0-9][A-Za-z0-9_.-]{0,63}`

var (
	// streamName is decision 7's stream vocabulary: the root's own `main`, a Claude Code subagent,
	// an opencode child session. `ledger` is S11's and is refused until S11 adds its fold — a
	// stream the pod stored and never read would be a `V` input nobody consumes.
	streamName = regexp.MustCompile(`^(main|subagent:` + idClass + `|child:` + idClass + `)$`)
	// blobName is a persisted tool-result file's base name (`toolu_….txt`, `….pdf`).
	blobName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
	// scopeName is a declared scope: a bounded identifier, or [Star]. An unknown NAME is not
	// refused here — decision 3: "unknown names fail closed" at READ time, owner-only.
	scopeName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
)

// Config is everything the archive needs. Every field is required.
type Config struct {
	// Dir is the transcript directory, already resolved by [ResolveDir].
	Dir string
	// Retention is how long a session is kept after its LAST accepted upload.
	Retention time.Duration
	// Quota is the instance's byte ceiling over everything under Dir.
	Quota int64
	// Recheck is the pod's own redactor. Its key is the pod's and never the host's: only hits are
	// read, never tags.
	Recheck *redact.Redactor
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

// Archive is the store. One per process: it assumes ONE `cairn-ui` replica (decision 1, Q12).
type Archive struct {
	cfg Config

	mu     sync.Mutex
	used   int64
	locks  map[string]*sync.Mutex
	frames map[string]*frameState
}

// Uploader is who an upload comes from: the capture token's bound owner (`<kind>:<id>`) and host.
type Uploader struct {
	Owner string
	Host  string
}

// Record is one record of a records upload: its source position (an offset, or an opencode part
// version) and its redacted raw JSON. In a framed upload `Rec` is that frame's CHUNK of the bytes.
type Record struct {
	Src string
	Rec []byte
}

// Frame says which continuation frame a request carries.
type Frame struct {
	Index, Count int
}

// Upload is the part of a request every upload kind shares.
type Upload struct {
	Root     string
	Runtime  string
	From, To string
	Declared []string
	Frame    *Frame
}

// Stored is a successful upload's answer.
type Stored struct {
	StoredTo string
	SeqTo    int64
	// Pending is true for a non-final frame: it was staged, nothing was stored.
	Pending    bool
	FrameIndex int
	FrameCount int
}

// ErrNotOwner is decision 15's ownership refusal: the root was first uploaded by another
// (owner, host). ONE value for every such case, so the answer names neither.
var ErrNotOwner = errors.New("this session is held by another uploader")

// StaleError is the compare-and-swap refusal: `from` is not the stored position. It names the
// stored position so the agent resumes from there — and it is only ever returned to the root's
// own (owner, host), after the ownership check.
type StaleError struct{ StoredTo string }

func (e *StaleError) Error() string {
	return fmt.Sprintf("from is not the stored position %q", e.StoredTo)
}

// RecheckError is the pod's refusing re-check: the table matched. It names the record index or
// the blob, never the value.
type RecheckError struct {
	Record int
	Blob   string
}

func (e *RecheckError) Error() string {
	if e.Blob != "" {
		return fmt.Sprintf("blob %s matched the redaction table; nothing was stored", e.Blob)
	}
	return fmt.Sprintf("record %d matched the redaction table; nothing was stored", e.Record)
}

// QuotaError is the instance quota refusal (decision 15: delayed, never dropped — the agent holds
// and retries).
type QuotaError struct{ Quota int64 }

func (e *QuotaError) Error() string {
	return fmt.Sprintf("the instance transcript quota of %d bytes is reached", e.Quota)
}

// InvalidError is a malformed request: a bad name, an unknown runtime, a frame out of order.
type InvalidError struct{ Reason string }

func (e *InvalidError) Error() string { return e.Reason }

func invalid(format string, a ...any) error { return &InvalidError{Reason: fmt.Sprintf(format, a...)} }

// ResolveDir is `-transcript-dir`'s startup check: the directory must exist, must be a directory,
// and must resolve OUTSIDE the store root — `internal/arcs`'s rule, for its reason (every directory
// at the store root is a scope to the token-file authority).
func ResolveDir(storeRoot, dir string) (string, error) {
	root, err := filepath.Abs(storeRoot)
	if err == nil {
		root, err = filepath.EvalSymlinks(root)
	}
	if err != nil {
		return "", fmt.Errorf("the store root %s does not resolve (%v), so the transcript directory cannot be checked against it", storeRoot, err)
	}
	abs, err := filepath.Abs(dir)
	if err == nil {
		abs, err = filepath.EvalSymlinks(abs)
	}
	if err != nil {
		return "", fmt.Errorf("the transcript directory %s does not exist or does not resolve (%v) — mount its volume first", dir, err)
	}
	if arcs.Inside(root, abs) {
		return "", fmt.Errorf("the transcript directory %s resolves to %s, which is INSIDE the store root %s. Every "+
			"directory at the store root is a scope to the token-file authority; put transcripts on their own volume", dir, abs, root)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("the transcript directory %s is not a directory", dir)
	}
	return abs, nil
}

// Open builds the archive and measures what the directory already holds, so the quota counts
// bytes from before a restart.
func Open(cfg Config) (*Archive, error) {
	if cfg.Dir == "" || cfg.Retention <= 0 || cfg.Quota <= 0 || cfg.Recheck == nil {
		return nil, errors.New("archive: a directory, a positive retention, a positive quota and a re-check redactor are all required")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if err := os.MkdirAll(filepath.Join(cfg.Dir, sessionsDir), dirMode); err != nil {
		return nil, err
	}
	// Staged frames from a previous process can never be continued — the index that would
	// continue them was in memory — so they are discarded rather than counted against the quota
	// forever. The agent restarts any such record at frame 0.
	if err := os.RemoveAll(filepath.Join(cfg.Dir, framesDir)); err != nil {
		return nil, err
	}
	used, err := treeBytes(cfg.Dir)
	if err != nil {
		return nil, err
	}
	return &Archive{cfg: cfg, used: used, locks: map[string]*sync.Mutex{}, frames: map[string]*frameState{}}, nil
}

// Used is the byte count the quota is measured against.
func (a *Archive) Used() int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.used
}

func treeBytes(dir string) (int64, error) {
	var n int64
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			n += info.Size()
		}
		return nil
	})
	return n, err
}

func (a *Archive) rootLock(root string) *sync.Mutex {
	a.mu.Lock()
	defer a.mu.Unlock()
	l, ok := a.locks[root]
	if !ok {
		l = &sync.Mutex{}
		a.locks[root] = l
	}
	return l
}

func (a *Archive) sessionDir(root string) string {
	return filepath.Join(a.cfg.Dir, sessionsDir, root)
}

// Meta is `meta.json`: the session's fixed identity, its grow-only scope sets and every stream's
// and blob's committed position. Writing it is the COMMIT of an upload.
type Meta struct {
	Schema    int       `json:"schema"`
	Root      string    `json:"root"`
	Owner     string    `json:"owner"`
	Host      string    `json:"host"`
	Runtime   string    `json:"runtime"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// Declared is `D(s)`: what the agent declared, grow-only.
	Declared []string `json:"declared_scopes"`
	// Derived is the pod's own `R_header ∪ F` — today `{*}` ([derivedScopes]).
	Derived []string `json:"derived_scopes"`
	// Children is every opencode child id uploaded as a `child:` stream (decision 7: trailers
	// naming the child count toward the root's `V`).
	Children []string            `json:"child_ids"`
	Streams  map[string]Position `json:"streams"`
	Blobs    map[string]Position `json:"blobs"`
}

// Position is one stream's or blob's committed state.
type Position struct {
	StoredTo string `json:"stored_to"`
	SeqTo    int64  `json:"seq_to,omitempty"`
	Bytes    int64  `json:"bytes"`
}

// ReadMeta reads a stored session's meta, or (nil, nil) when there is none.
func (a *Archive) ReadMeta(root string) (*Meta, error) {
	if !write.SessionComponent.MatchString(root) {
		return nil, nil
	}
	return a.readMeta(root)
}

func (a *Archive) readMeta(root string) (*Meta, error) {
	raw, err := os.ReadFile(filepath.Join(a.sessionDir(root), metaName))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var m Meta
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("archive: %s/%s does not parse: %w", root, metaName, err)
	}
	if m.Streams == nil {
		m.Streams = map[string]Position{}
	}
	if m.Blobs == nil {
		m.Blobs = map[string]Position{}
	}
	return &m, nil
}

// writeMeta commits: a temporary file, fsync, rename, fsync of the directory.
func writeMeta(dir string, m *Meta) error {
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, metaName+".tmp")
	if err := writeFileSync(tmp, append(raw, '\n')); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(dir, metaName)); err != nil {
		return err
	}
	return syncDir(dir)
}

func writeFileSync(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW, fileMode)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// derivedScopes is the pod's own derivation of `R_header ∪ F` over a session's stored records.
//
// 🔴 IT RETURNS `{*}`, AND THAT IS A DECISION, NOT A STUB TO BE READ AS "NOTHING FOUND". The
// derivation belongs to `internal/transcript/scopeuse` (decision 3: ONE function, called by the
// agent for routing and by the pod at upload), which is slice S2's and is not on `main`. An empty
// answer here would let the agent's DECLARATION be the whole read half of `V` — exactly what the
// pod-side re-derivation exists to refuse ("so an old or lying agent cannot shrink `V`"). `*`
// makes every stored session owner-only (decision 4), the direction that cannot widen visibility.
// ⚠ It is grow-only like every set in meta, so a session stored under this rule stays owner-only
// after `scopeuse` is wired; nothing is stored while capture is unarmed (O16).
func derivedScopes() []string { return []string{Star} }

func union(a, b []string) []string {
	out := append(append([]string(nil), a...), b...)
	slices.Sort(out)
	return slices.Compact(out)
}

func validateUpload(u Upload) error {
	if !write.SessionComponent.MatchString(u.Root) {
		return invalid("root %q is not a session id (%s)", u.Root, write.SessionComponentPattern)
	}
	if !slices.Contains(Runtimes, u.Runtime) {
		return invalid("runtime %q is not one of %v", u.Runtime, Runtimes)
	}
	if err := checkCursor("from", u.From, true); err != nil {
		return err
	}
	if err := checkCursor("to", u.To, false); err != nil {
		return err
	}
	if len(u.Declared) > MaxDeclared {
		return invalid("declared_scopes carries %d names, over the %d bound", len(u.Declared), MaxDeclared)
	}
	for _, s := range u.Declared {
		if s != Star && !scopeName.MatchString(s) {
			return invalid("declared scope %q is neither a scope name nor %q", s, Star)
		}
	}
	if f := u.Frame; f != nil {
		if f.Count < 2 || f.Count > MaxFrames || f.Index < 0 || f.Index >= f.Count {
			return invalid("frames {index %d, count %d} is not a frame of a 2–%d frame sequence", f.Index, f.Count, MaxFrames)
		}
	}
	return nil
}

// checkCursor bounds an opaque cursor: printable ASCII, no space. `from` may be "" (a stream
// never written); `to` may not.
func checkCursor(field, v string, mayBeEmpty bool) error {
	if v == "" && !mayBeEmpty {
		return invalid("%s is empty", field)
	}
	if len(v) > MaxCursorBytes {
		return invalid("%s is %d bytes, over the %d-byte bound", field, len(v), MaxCursorBytes)
	}
	for i := 0; i < len(v); i++ {
		if v[i] <= ' ' || v[i] > '~' {
			return invalid("%s carries a byte outside printable ASCII", field)
		}
	}
	return nil
}

// owned is decision 15's ownership rule: the first accepted upload fixes (owner, host).
func owned(m *Meta, who Uploader) bool {
	return m == nil || (m.Owner == who.Owner && m.Host == who.Host)
}

// AppendRecords appends one request's records to one stream, under the stream's CAS.
func (a *Archive) AppendRecords(who Uploader, u Upload, stream string, recs []Record) (Stored, error) {
	if err := validateUpload(u); err != nil {
		return Stored{}, err
	}
	if !streamName.MatchString(stream) {
		return Stored{}, invalid("stream %q is not main, subagent:<id> or child:<id>", stream)
	}
	if len(recs) == 0 || len(recs) > MaxRecords {
		return Stored{}, invalid("a records upload carries 1–%d records, not %d", MaxRecords, len(recs))
	}
	if u.Frame != nil && len(recs) != 1 {
		return Stored{}, invalid("a framed upload carries exactly one record's frame, not %d", len(recs))
	}
	for i, r := range recs {
		if err := checkCursor(fmt.Sprintf("record %d src", i), r.Src, false); err != nil {
			return Stored{}, err
		}
	}
	l := a.rootLock(u.Root)
	l.Lock()
	defer l.Unlock()

	m, err := a.readMeta(u.Root)
	if err != nil {
		return Stored{}, err
	}
	// 🔴 OWNERSHIP BEFORE THE CAS, so the stale answer — which names the stored position — is
	// only ever given to the root's own (owner, host).
	if !owned(m, who) {
		return Stored{}, ErrNotOwner
	}
	if m != nil && m.Runtime != u.Runtime {
		return Stored{}, invalid("runtime %q is not this session's %q", u.Runtime, m.Runtime)
	}
	pos := Position{}
	if m != nil {
		pos = m.Streams[stream]
	}
	if u.From != pos.StoredTo {
		a.dropFrames(u.Root, "stream:"+stream)
		return Stored{}, &StaleError{StoredTo: pos.StoredTo}
	}
	if u.Frame != nil {
		assembled, done, err := a.stageFrame(u, "stream:"+stream, recs[0].Src, recs[0].Rec)
		if err != nil || !done {
			return Stored{Pending: err == nil, FrameIndex: u.Frame.Index, FrameCount: u.Frame.Count}, err
		}
		recs = []Record{{Src: recs[0].Src, Rec: assembled}}
	}
	lines := make([][]byte, len(recs))
	for i, r := range recs {
		var c bytes.Buffer
		// Compact, never re-encode: insignificant whitespace is the only thing that changes, so a
		// stored line is one line and its values are the agent's bytes.
		if err := json.Compact(&c, r.Rec); err != nil {
			return Stored{}, invalid("record %d is not one JSON value", i)
		}
		// 🔴 THE REFUSING RE-CHECK (clause c). ANY hit refuses the whole request.
		if _, hits := a.cfg.Recheck.Record(c.Bytes()); len(hits) > 0 {
			return Stored{}, &RecheckError{Record: i}
		}
		lines[i] = c.Bytes()
	}

	var seg bytes.Buffer
	gz := gzip.NewWriter(&seg)
	seq := pos.SeqTo
	for i, line := range lines {
		seq++
		src, _ := json.Marshal(recs[i].Src)
		fmt.Fprintf(gz, `{"seq":%d,"src":%s,"rec":`, seq, src)
		gz.Write(line)
		gz.Write([]byte("}\n"))
	}
	if err := gz.Close(); err != nil {
		return Stored{}, err
	}
	if err := a.reserve(int64(seg.Len())); err != nil {
		return Stored{}, err
	}
	dir, m, err := a.ensureSession(m, who, u)
	if err != nil {
		a.release(int64(seg.Len()))
		return Stored{}, err
	}
	path := filepath.Join(dir, streamPrefix+stream+streamSuffix)
	size, delta, err := appendSegment(path, pos.Bytes, seg.Bytes())
	a.adjust(delta - int64(seg.Len()))
	if err != nil {
		return Stored{}, err
	}
	m.Streams[stream] = Position{StoredTo: u.To, SeqTo: seq, Bytes: size}
	if child, ok := cutPrefix(stream, "child:"); ok && !slices.Contains(m.Children, child) {
		m.Children = union(m.Children, []string{child})
	}
	if err := a.commit(dir, m, u); err != nil {
		return Stored{}, err
	}
	return Stored{StoredTo: u.To, SeqTo: seq}, nil
}

func cutPrefix(s, p string) (string, bool) {
	if len(s) > len(p) && s[:len(p)] == p {
		return s[len(p):], true
	}
	return "", false
}

// PutBlob stores one persisted tool-result file whole, under the blob's CAS.
func (a *Archive) PutBlob(who Uploader, u Upload, name string, data []byte) (Stored, error) {
	if err := validateUpload(u); err != nil {
		return Stored{}, err
	}
	if !blobName.MatchString(name) {
		return Stored{}, invalid("blob name %q is not a file base name", name)
	}
	l := a.rootLock(u.Root)
	l.Lock()
	defer l.Unlock()

	m, err := a.readMeta(u.Root)
	if err != nil {
		return Stored{}, err
	}
	if !owned(m, who) {
		return Stored{}, ErrNotOwner
	}
	if m != nil && m.Runtime != u.Runtime {
		return Stored{}, invalid("runtime %q is not this session's %q", u.Runtime, m.Runtime)
	}
	pos := Position{}
	if m != nil {
		pos = m.Blobs[name]
	}
	if u.From != pos.StoredTo {
		a.dropFrames(u.Root, "blob:"+name)
		return Stored{}, &StaleError{StoredTo: pos.StoredTo}
	}
	if u.Frame != nil {
		assembled, done, err := a.stageFrame(u, "blob:"+name, "", data)
		if err != nil || !done {
			return Stored{Pending: err == nil, FrameIndex: u.Frame.Index, FrameCount: u.Frame.Count}, err
		}
		data = assembled
	}
	// 🔴 THE REFUSING RE-CHECK OVER A TEXT BLOB (clause c). A binary blob is returned unmatched by
	// the table itself (decision 6a, O12: binary ships).
	if _, hits := a.cfg.Recheck.Blob(name, data); len(hits) > 0 {
		return Stored{}, &RecheckError{Blob: name}
	}
	if err := a.reserve(int64(len(data))); err != nil {
		return Stored{}, err
	}
	dir, m, err := a.ensureSession(m, who, u)
	if err != nil {
		a.release(int64(len(data)))
		return Stored{}, err
	}
	bdir := filepath.Join(dir, blobsDir)
	if err := os.MkdirAll(bdir, dirMode); err != nil {
		a.release(int64(len(data)))
		return Stored{}, err
	}
	tmp := filepath.Join(bdir, "."+name+".tmp")
	if err := writeFileSync(tmp, data); err != nil {
		a.release(int64(len(data)))
		return Stored{}, err
	}
	if err := os.Rename(tmp, filepath.Join(bdir, name)); err != nil {
		a.release(int64(len(data)))
		return Stored{}, err
	}
	// The previous version's bytes are gone with the rename.
	a.adjust(-pos.Bytes)
	m.Blobs[name] = Position{StoredTo: u.To, Bytes: int64(len(data))}
	if err := a.commit(dir, m, u); err != nil {
		return Stored{}, err
	}
	return Stored{StoredTo: u.To}, nil
}

// ensureSession creates the session directory and, for a first upload, the meta that FIXES its
// (owner, host). It is called only once a request has passed every check, so a refused first
// upload leaves no directory behind.
func (a *Archive) ensureSession(m *Meta, who Uploader, u Upload) (string, *Meta, error) {
	dir := a.sessionDir(u.Root)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return "", nil, err
	}
	if m == nil {
		now := a.cfg.Now().UTC()
		m = &Meta{Schema: metaSchema, Root: u.Root, Owner: who.Owner, Host: who.Host, Runtime: u.Runtime,
			CreatedAt: now, UpdatedAt: now, Declared: []string{}, Derived: []string{}, Children: []string{},
			Streams: map[string]Position{}, Blobs: map[string]Position{}}
	}
	return dir, m, nil
}

// commit folds the request's declarations and the pod's derivation into the grow-only sets and
// writes meta — the moment the upload exists.
func (a *Archive) commit(dir string, m *Meta, u Upload) error {
	m.Declared = union(m.Declared, u.Declared)
	m.Derived = union(m.Derived, derivedScopes())
	m.UpdatedAt = a.cfg.Now().UTC()
	before := fileSize(filepath.Join(dir, metaName))
	if err := writeMeta(dir, m); err != nil {
		return err
	}
	a.adjust(fileSize(filepath.Join(dir, metaName)) - before)
	return nil
}

func fileSize(p string) int64 {
	info, err := os.Stat(p)
	if err != nil {
		return 0
	}
	return info.Size()
}

// appendSegment truncates the segment to its COMMITTED size (dropping a member a crash appended
// before meta was written), appends one gzip member and syncs. It returns the new size and how
// many bytes the file grew by from what was on disk.
func appendSegment(path string, committed int64, member []byte) (size, delta int64, err error) {
	before := fileSize(path)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, fileMode)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	if err := f.Truncate(committed); err != nil {
		return 0, 0, err
	}
	if _, err := f.WriteAt(member, committed); err != nil {
		return 0, 0, err
	}
	if err := f.Sync(); err != nil {
		return 0, 0, err
	}
	size = committed + int64(len(member))
	return size, size - before, nil
}

// reserve claims n bytes against the quota, or refuses (decision 15: 507, named).
func (a *Archive) reserve(n int64) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.used+n > a.cfg.Quota {
		return &QuotaError{Quota: a.cfg.Quota}
	}
	a.used += n
	return nil
}

func (a *Archive) release(n int64) { a.adjust(-n) }

func (a *Archive) adjust(n int64) {
	a.mu.Lock()
	a.used += n
	if a.used < 0 {
		a.used = 0
	}
	a.mu.Unlock()
}

// StoredRecord is one stored line, as [Archive.ReadStream] returns it.
type StoredRecord struct {
	Seq int64           `json:"seq"`
	Src string          `json:"src"`
	Rec json.RawMessage `json:"rec"`
}

// ReadStream returns every COMMITTED record of one stream, in order. It is the read the later
// slices render from (S4, S8); here it is what the tests read back.
func (a *Archive) ReadStream(root, stream string) ([]StoredRecord, error) {
	if !write.SessionComponent.MatchString(root) || !streamName.MatchString(stream) {
		return nil, invalid("not a stored stream")
	}
	l := a.rootLock(root)
	l.Lock()
	defer l.Unlock()
	m, err := a.readMeta(root)
	if err != nil || m == nil {
		return nil, err
	}
	pos, ok := m.Streams[stream]
	if !ok {
		return nil, nil
	}
	f, err := os.Open(filepath.Join(a.sessionDir(root), streamPrefix+stream+streamSuffix))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	zr, err := gzip.NewReader(io.LimitReader(f, pos.Bytes))
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(zr)
	var out []StoredRecord
	for {
		var r StoredRecord
		if err := dec.Decode(&r); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// ReadBlob returns a stored blob's bytes.
func (a *Archive) ReadBlob(root, name string) ([]byte, error) {
	if !write.SessionComponent.MatchString(root) || !blobName.MatchString(name) {
		return nil, invalid("not a stored blob")
	}
	return os.ReadFile(filepath.Join(a.sessionDir(root), blobsDir, name))
}

// Roots lists every stored root session, sorted.
func (a *Archive) Roots() ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(a.cfg.Dir, sessionsDir))
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && write.SessionComponent.MatchString(e.Name()) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}
