package capture

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"syscall"
	"testing"

	"github.com/ZacxDev/cairn/internal/client"
	"github.com/ZacxDev/cairn/internal/redact"
)

// TestMain doubles as an opencode STUB: when CAPTURE_OPENCODE_STUB names a directory, the test
// binary answers `export <id>` and `session list …` from files there and exits — so ExecRunner's
// real subprocess path (stdout to a FILE) is exercised without opencode installed.
func TestMain(m *testing.M) {
	if dir := os.Getenv("CAPTURE_OPENCODE_STUB"); dir != "" {
		args := os.Args[1:]
		var file string
		switch {
		case len(args) == 2 && args[0] == "export":
			file = filepath.Join(dir, "export-"+args[1]+".json")
		case len(args) >= 2 && args[0] == "session" && args[1] == "list":
			file = filepath.Join(dir, "list.json")
		default:
			os.Exit(64)
		}
		data, err := os.ReadFile(file)
		if err != nil {
			os.Exit(65)
		}
		os.Stdout.Write(data)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type world struct {
	Sessions map[string]struct {
		Runtime string `json:"runtime"`
		ID      string `json:"id"`
		Project string `json:"project"`
		Parent  string `json:"parent"`
	} `json:"sessions"`
	Claude struct {
		Files map[string]map[string]string `json:"files"`
	} `json:"claude"`
	Ledgers  map[string]string `json:"ledgers"`
	Opencode struct {
		Exports      map[string]map[string]json.RawMessage `json:"exports"`
		SessionList  json.RawMessage                       `json:"session_list"`
		ChangedParts []string                              `json:"changed_parts"`
		AddedParts   []string                              `json:"added_parts"`
	} `json:"opencode"`
}

func loadWorld(t *testing.T) world {
	t.Helper()
	raw, err := os.ReadFile("../transcript/testdata/synthetic_world.json")
	if err != nil {
		t.Fatalf("the synthetic world is missing: %v", err)
	}
	var w world
	if err := json.Unmarshal(raw, &w); err != nil {
		t.Fatal(err)
	}
	return w
}

func fileBytes(t *testing.T, f map[string]string) []byte {
	t.Helper()
	if s, ok := f["utf8"]; ok {
		return []byte(s)
	}
	b, err := base64.StdEncoding.DecodeString(f["base64"])
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type env struct {
	root, claude, ledger string
	sink                *memSink
}

// materialize writes the Claude Code layout and the ledgers of the sessions `keep` admits.
func materialize(t *testing.T, w world, keep ...string) env {
	t.Helper()
	root := t.TempDir()
	e := env{root: root, claude: filepath.Join(root, "projects"), ledger: filepath.Join(root, "ledger"),
		sink: newMemSink()}
	ids := map[string]bool{}
	for _, k := range keep {
		ids[w.Sessions[k].ID] = true
	}
	admitted := func(path string) bool {
		if len(keep) == 0 {
			return true
		}
		for id := range ids {
			if strings.Contains(path, id) {
				return true
			}
		}
		return false
	}
	for rel, f := range w.Claude.Files {
		if !admitted(rel) {
			continue
		}
		p := filepath.Join(e.claude, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, fileBytes(t, f), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(e.ledger, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, body := range w.Ledgers {
		if admitted(name) {
			if err := os.WriteFile(filepath.Join(e.ledger, name), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	return e
}

// fakeRunner serves recorded exports in-process.
type fakeRunner struct {
	exports map[string][]byte
	list    []byte
}

func (f *fakeRunner) List(string) ([]byte, error) { return f.list, nil }
func (f *fakeRunner) Export(id string) ([]byte, error) {
	b, ok := f.exports[id]
	if !ok {
		return nil, fmt.Errorf("no export %s", id)
	}
	return b, nil
}

func exportsOf(w world, which string) map[string][]byte {
	out := map[string][]byte{}
	for id, raw := range w.Opencode.Exports[which] {
		out[id] = raw
	}
	return out
}

var (
	oneInstance  = client.Routing{Instances: []client.Instance{{Alias: "personal"}}}
	twoInstances = client.Routing{
		Instances:    []client.Instance{{Alias: "personal"}, {Alias: "client"}},
		Routes:       map[string]string{"alpha-notes": "personal", "beta-notes": "client"},
		RoutesSource: "the test's table",
	}
)

func newAgent(t *testing.T, e env, routing client.Routing, runner OpencodeRunner, log *bytes.Buffer) *Agent {
	t.Helper()
	r, err := redact.New(redact.SelfTestKey(3), nil)
	if err != nil {
		t.Fatal(err)
	}
	a := &Agent{ClaudeRoot: e.claude, Runner: runner, LedgerDir: e.ledger, Routing: routing, Redactor: r,
		Sink: e.sink, State: &State{Schema: 1, Sessions: map[string]*SessionState{}}, Log: log}
	return a
}

func mustRun(t *testing.T, a *Agent) Summary {
	t.Helper()
	sum, err := a.Run()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return sum
}

// memSink is the in-memory Sink the agent is tested through (S2 ships no sink of its own).
type memSink struct {
	records   map[string][]Record // instance/root/stream
	blobs     map[string][]byte   // instance/root/name
	withdrawn map[string][]string // instance → roots
	failRoot  string              // a root whose writes fail, to test isolation
}

func newMemSink() *memSink {
	return &memSink{records: map[string][]Record{}, blobs: map[string][]byte{}, withdrawn: map[string][]string{}}
}

func (s *memSink) Records(instance, root, stream string, recs []Record) error {
	if root == s.failRoot {
		return fmt.Errorf("the sink refused %s", root)
	}
	k := instance + "/" + root + "/" + stream
	s.records[k] = append(s.records[k], recs...)
	return nil
}

func (s *memSink) Blob(instance, root, name string, data []byte) error {
	if root == s.failRoot {
		return fmt.Errorf("the sink refused %s", root)
	}
	s.blobs[instance+"/"+root+"/"+name] = append([]byte(nil), data...)
	return nil
}

func (s *memSink) Withdraw(instance, root string) error {
	s.withdrawn[instance] = append(s.withdrawn[instance], root)
	return nil
}

// has answers whether anything of root reached instance.
func (s *memSink) has(instance, root string) bool {
	p := instance + "/" + root + "/"
	for k := range s.records {
		if strings.HasPrefix(k, p) {
			return true
		}
	}
	for k := range s.blobs {
		if strings.HasPrefix(k, p) {
			return true
		}
	}
	return false
}

// spooled returns one stream's records, in order.
func spooled(t *testing.T, e env, instance, root, stream string) []Record {
	t.Helper()
	return e.sink.records[instance+"/"+root+"/"+stream]
}

func lineCount(b []byte) int { return bytes.Count(b, []byte("\n")) }

// TestTheOffsetReaderShipsCompleteLinesOnly: a file grown mid-line ships only complete lines; the
// next run ships the rest exactly once.
func TestTheOffsetReaderShipsCompleteLinesOnly(t *testing.T) {
	w := loadWorld(t)
	e := materialize(t, w, "cc-plain")
	id := w.Sessions["cc-plain"].ID
	path := filepath.Join(e.claude, w.Sessions["cc-plain"].Project, id+".jsonl")
	full, _ := os.ReadFile(path)
	lines := bytes.SplitAfter(full, []byte("\n"))
	last := lines[len(lines)-2] // the final complete line (SplitAfter leaves a trailing "")
	cut := len(full) - len(last) + len(last)/2
	if err := os.WriteFile(path, full[:cut], 0o600); err != nil {
		t.Fatal(err)
	}
	// CONTROL: the fragment a reader shipping partial lines would send does not parse.
	if json.Valid(full[len(full)-len(last) : cut]) {
		t.Fatal("control: the cut fragment parses, so this test cannot tell a partial line from a whole one")
	}
	a := newAgent(t, e, oneInstance, &fakeRunner{}, &bytes.Buffer{})
	mustRun(t, a)
	got := spooled(t, e, "personal", id, "main")
	if len(got) != lineCount(full)-1 {
		t.Fatalf("first run shipped %d records, want %d (every complete line, not the partial one)", len(got), lineCount(full)-1)
	}
	if err := os.WriteFile(path, full, 0o600); err != nil {
		t.Fatal(err)
	}
	mustRun(t, a)
	got = spooled(t, e, "personal", id, "main")
	if len(got) != lineCount(full) {
		t.Fatalf("after the line completed: %d records, want %d — exactly once", len(got), lineCount(full))
	}
	if string(got[len(got)-1].Rec) != string(bytes.TrimSuffix(last, []byte("\n"))) {
		t.Fatal("the completed line was not shipped whole")
	}
	mustRun(t, a)
	if again := spooled(t, e, "personal", id, "main"); len(again) != len(got) {
		t.Fatalf("a third run with nothing new shipped %d more records", len(again)-len(got))
	}
}

// TestARewrittenFileIsRefusedNotResumed: the first 4 KiB changed → refused and logged, nothing
// uploaded from an offset into different bytes.
func TestARewrittenFileIsRefusedNotResumed(t *testing.T) {
	w := loadWorld(t)
	e := materialize(t, w, "cc-plain")
	id := w.Sessions["cc-plain"].ID
	path := filepath.Join(e.claude, w.Sessions["cc-plain"].Project, id+".jsonl")
	var log bytes.Buffer
	a := newAgent(t, e, oneInstance, &fakeRunner{}, &log)
	mustRun(t, a)
	before := len(spooled(t, e, "personal", id, "main"))
	full, _ := os.ReadFile(path)
	rewritten := append([]byte(nil), full...)
	copy(rewritten[10:], []byte("REWRITTEN!"))
	rewritten = append(rewritten, []byte(`{"type":"mode","mode":"normal","sessionId":"x"}`+"\n")...)
	if err := os.WriteFile(path, rewritten, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := mustRun(t, a)
	if sum.Refused != 1 || !strings.Contains(log.String(), "refused "+id+"/main") {
		t.Fatalf("a rewritten file was not refused and logged: refused=%d log=%q", sum.Refused, log.String())
	}
	if after := len(spooled(t, e, "personal", id, "main")); after != before {
		t.Fatalf("a rewritten file shipped %d more records", after-before)
	}
}

// TestEveryByteShips: per stream, the shipped record count is the fixture's line count —
// bookkeeping and duplicate fields included — and every blob arrives: text without a secret and
// every binary byte-identical (O12), a planted text value replaced and nothing else changed.
func TestEveryByteShips(t *testing.T) {
	w := loadWorld(t)
	e := materialize(t, w, "cc-rich")
	rich := w.Sessions["cc-rich"]
	// A blob with a value planted at RUN time, beside the fixture's own blobs.
	val := "Zq" + strings.Repeat("x9", 11)
	planted := "LOG=1\nDB_PASSWORD=" + val + "\nEND=1\n"
	blobDir := filepath.Join(e.claude, rich.Project, rich.ID, "tool-results")
	if err := os.WriteFile(filepath.Join(blobDir, "toolu_planted.txt"), []byte(planted), 0o600); err != nil {
		t.Fatal(err)
	}
	a := newAgent(t, e, oneInstance, &fakeRunner{}, &bytes.Buffer{})
	mustRun(t, a)
	checked := 0
	for rel, f := range w.Claude.Files {
		if !strings.Contains(rel, rich.ID) {
			continue
		}
		data := fileBytes(t, f)
		switch {
		case strings.HasSuffix(rel, rich.ID+".jsonl"):
			if got := len(spooled(t, e, "personal", rich.ID, "main")); got != lineCount(data) {
				t.Errorf("main: %d records shipped, %d lines in the fixture", got, lineCount(data))
			}
			checked++
		case strings.Contains(rel, "/subagents/agent-") && strings.HasSuffix(rel, ".jsonl"):
			agent := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(rel), "agent-"), ".jsonl")
			if got := len(spooled(t, e, "personal", rich.ID, "subagent:"+agent)); got != lineCount(data) {
				t.Errorf("subagent %s: %d records shipped, %d lines", agent, got, lineCount(data))
			}
			checked++
		default:
			name := strings.SplitN(rel, rich.ID+"/", 2)[1]
			got, ok := e.sink.blobs["personal/"+rich.ID+"/"+name]
			if !ok {
				err := "missing"
				t.Errorf("blob %s was not shipped: %v", name, err)
				continue
			}
			if !bytes.Equal(got, data) {
				t.Errorf("blob %s (text=%v) is not byte-identical to its source", name, !redact.IsBinary(data))
			}
			checked++
		}
	}
	if checked < 10 {
		t.Fatalf("only %d fixture files were checked — the instrument read too little", checked)
	}
	got, ok := e.sink.blobs["personal/"+rich.ID+"/tool-results/toolu_planted.txt"]
	if !ok {
		t.Fatal("the planted blob was not shipped")
	}
	want := strings.Replace(planted, val, "[redacted:dotenv:"+a.Redactor.Tag(val)+"]", 1)
	if string(got) != want {
		t.Fatalf("the planted blob is not its source with exactly that span replaced:\n%s", got)
	}
}

// TestOpencodeUpsertsByPartVersion: 3 parts changed and 1 added between two exports → exactly 4
// upserts. Control: diffing by id alone finds 1.
func TestOpencodeUpsertsByPartVersion(t *testing.T) {
	w := loadWorld(t)
	e := materialize(t, w, "oc-root") // no Claude files match; opencode only
	root := w.Sessions["oc-root"].ID
	runner := &fakeRunner{exports: exportsOf(w, "before"), list: w.Opencode.SessionList}
	a := newAgent(t, e, oneInstance, runner, &bytes.Buffer{})
	a.ClaudeRoot = ""
	a.OpencodeDirs = []string{"/work/alpha"}
	mustRun(t, a)
	first := len(spooled(t, e, "personal", root, "main"))
	runner.exports = exportsOf(w, "after")
	mustRun(t, a)
	all := spooled(t, e, "personal", root, "main")
	upserts := all[first:]
	var keys []string
	for _, r := range upserts {
		keys = append(keys, strings.SplitN(r.Src, "@", 2)[0])
	}
	sort.Strings(keys)
	var want []string
	for _, id := range append(append([]string(nil), w.Opencode.ChangedParts...), w.Opencode.AddedParts...) {
		want = append(want, "prt:"+id)
	}
	// The session info's `time.updated` moved too: the export carries it, so it is a unit.
	want = append(want, "info")
	sort.Strings(want)
	if !slices.Equal(keys, want) {
		t.Fatalf("upserts = %v\nwant     %v", keys, want)
	}
	parts := 0
	for _, k := range keys {
		if strings.HasPrefix(k, "prt:") {
			parts++
		}
	}
	if parts != 4 {
		t.Fatalf("%d part upserts, want 4 (3 changed + 1 added)", parts)
	}
	// CONTROL: by id alone, only the ADDED part is new.
	before, _, _ := parseExport(w.Opencode.Exports["before"][root])
	after, _, _ := parseExport(w.Opencode.Exports["after"][root])
	seen := map[string]bool{}
	for _, u := range before {
		seen[u.Key] = true
	}
	byID := 0
	for _, u := range after {
		if !seen[u.Key] && strings.HasPrefix(u.Key, "prt:") {
			byID++
		}
	}
	if byID != 1 {
		t.Fatalf("control: diffing by id alone found %d parts, want 1", byID)
	}
	if child := spooled(t, e, "personal", root, "child:"+w.Sessions["oc-child"].ID); len(child) == 0 {
		t.Fatal("the child session, linked by its task part, was not shipped as a child stream")
	}
}

// TestATruncatedExportShipsNothing: the measured pipe failure — a document cut short, exit 0.
func TestATruncatedExportShipsNothing(t *testing.T) {
	w := loadWorld(t)
	e := materialize(t, w, "oc-root")
	root := w.Sessions["oc-root"].ID
	ex := exportsOf(w, "before")
	ex[root] = ex[root][:len(ex[root])/2]
	var log bytes.Buffer
	a := newAgent(t, e, oneInstance, &fakeRunner{exports: ex, list: w.Opencode.SessionList}, &log)
	a.ClaudeRoot = ""
	a.OpencodeDirs = []string{"/work/alpha"}
	sum := mustRun(t, a)
	if sum.Refused != 1 || sum.Records != 0 || !strings.Contains(log.String(), "not one complete JSON document") {
		t.Fatalf("a truncated export was not refused: %+v %q", sum, log.String())
	}
	if e.sink.has("personal", root) {
		t.Fatal("a truncated export left a spool directory")
	}
}

// TestTheOpencodeDatabaseIsNeverOpened runs the REAL subprocess path against a stub, with the
// database path a FIFO: anything that opened it for reading would block until the test times out.
func TestTheOpencodeDatabaseIsNeverOpened(t *testing.T) {
	w := loadWorld(t)
	home := t.TempDir()
	dbDir := filepath.Join(home, ".local", "share", "opencode")
	if err := os.MkdirAll(dbDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(dbDir, "opencode-stable.db"), 0o600); err != nil {
		t.Fatal(err)
	}
	stub := t.TempDir()
	for id, raw := range w.Opencode.Exports["before"] {
		_ = os.WriteFile(filepath.Join(stub, "export-"+id+".json"), raw, 0o600)
	}
	_ = os.WriteFile(filepath.Join(stub, "list.json"), w.Opencode.SessionList, 0o600)
	t.Setenv("HOME", home)
	t.Setenv("CAPTURE_OPENCODE_STUB", stub)
	e := materialize(t, w, "oc-root")
	a := newAgent(t, e, oneInstance, ExecRunner{Bin: os.Args[0], TempDir: t.TempDir()}, &bytes.Buffer{})
	a.ClaudeRoot = ""
	a.OpencodeDirs = []string{home}
	sum := mustRun(t, a)
	if sum.Records == 0 {
		t.Fatal("the stubbed export shipped nothing — the subprocess path did not run")
	}
}

// TestRoutingHonoursReads is THE fixture of clause (h) and decision 16.
func TestRoutingHonoursReads(t *testing.T) {
	w := loadWorld(t)
	cases := []struct {
		session, want string
	}{
		{"cc-routing-two-instances", "held"}, // wrote alpha (personal), read beta (client)
		{"cc-hook-recall", "client"},         // read beta-notes only
		{"cc-plain", "personal"},             // V empty → the default instance
		{"cc-hook-allscopes", "held"},        // `*` with two instances
	}
	for _, c := range cases {
		e := materialize(t, w, c.session)
		var log bytes.Buffer
		a := newAgent(t, e, twoInstances, &fakeRunner{}, &log)
		mustRun(t, a)
		id := w.Sessions[c.session].ID
		var shippedTo []string
		for _, inst := range []string{"personal", "client"} {
			if e.sink.has(inst, id) {
				shippedTo = append(shippedTo, inst)
			}
		}
		switch c.want {
		case "held":
			if len(shippedTo) != 0 || !strings.Contains(log.String(), "held "+id) {
				t.Errorf("%s: want held (nothing queued for either instance, logged); queued for %v, log %q",
					c.session, shippedTo, log.String())
			}
		default:
			if !slices.Equal(shippedTo, []string{c.want}) {
				t.Errorf("%s: queued for %v, want [%s]", c.session, shippedTo, c.want)
			}
		}
	}
	// CONTROL: routing on WRITES only queues the clause-(h) fixture for the personal instance —
	// the difference between that and "held" is the read half of V.
	if d := Decide([]string{"alpha-notes"}, twoInstances); d.Held || d.Instance != "personal" {
		t.Fatalf("control: writes-only routing = %+v", d)
	}
}

func appendTo(t *testing.T, path string, lines ...string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, l := range lines {
		if _, err := f.WriteString(l + "\n"); err != nil {
			t.Fatal(err)
		}
	}
}

const betaRead = `{"type":"user","sessionId":"x","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t","content":"subsystem-recall: status=recalled scope=beta-notes\n"}]}}`

// TestAMoveWithdrawsAndReshipsFromOffsetZero: run 1 with V empty ships to the default (personal);
// run 2 after a beta-notes read withdraws from personal and re-ships EVERYTHING to client.
func TestAMoveWithdrawsAndReshipsFromOffsetZero(t *testing.T) {
	w := loadWorld(t)
	e := materialize(t, w, "cc-plain")
	s := w.Sessions["cc-plain"]
	path := filepath.Join(e.claude, s.Project, s.ID+".jsonl")
	a := newAgent(t, e, twoInstances, &fakeRunner{}, &bytes.Buffer{})
	mustRun(t, a)
	if len(spooled(t, e, "personal", s.ID, "main")) == 0 {
		t.Fatal("run 1 did not ship the empty-V session to the default instance")
	}
	appendTo(t, path, betaRead)
	mustRun(t, a)
	if !slices.Contains(e.sink.withdrawn["personal"], s.ID) {
		t.Fatal("no withdrawal was queued for the personal instance")
	}
	client := spooled(t, e, "client", s.ID, "main")
	full, _ := os.ReadFile(path)
	first := bytes.SplitN(full, []byte("\n"), 2)[0]
	if len(client) != lineCount(full) || client[0].Src != "0" || string(client[0].Rec) != string(first) {
		t.Fatalf("the client instance did not receive the WHOLE session from offset 0: %d records, first src %q",
			len(client), client[0].Src)
	}
}

// TestAReadOnAnotherInstanceWithdrawsTheShippedPrefix is clause (m)'s agent half: a session whose
// first run shipped alpha-notes turns to the personal instance and whose second run reads
// beta-notes is HELD — the second run queues NO record and a withdrawal for the personal instance.
func TestAReadOnAnotherInstanceWithdrawsTheShippedPrefix(t *testing.T) {
	w := loadWorld(t)
	e := materialize(t, w, "cc-plain")
	s := w.Sessions["cc-plain"]
	path := filepath.Join(e.claude, s.Project, s.ID+".jsonl")
	appendTo(t, path, strings.Replace(betaRead, "beta-notes", "alpha-notes", 1))
	var log bytes.Buffer
	a := newAgent(t, e, twoInstances, &fakeRunner{}, &log)
	mustRun(t, a)
	shipped := len(spooled(t, e, "personal", s.ID, "main"))
	if shipped == 0 {
		t.Fatal("run 1 did not ship the alpha-notes session to the personal instance")
	}
	appendTo(t, path, betaRead)
	sum := mustRun(t, a)
	if sum.Records != 0 || sum.Withdrawn != 1 || sum.Held != 1 {
		t.Fatalf("run 2: %+v — want 0 records, 1 withdrawal, held", sum)
	}
	if got := len(spooled(t, e, "personal", s.ID, "main")); got != shipped {
		t.Fatalf("the record that READ beta-notes reached the personal instance (%d → %d)", shipped, got)
	}
	if e.sink.has("client", s.ID) {
		t.Fatal("a held session was queued for the client instance")
	}
	if !slices.Contains(e.sink.withdrawn["personal"], s.ID) {
		t.Fatal("no withdrawal was queued for the personal instance")
	}
}

const sentinel = "QZXSENTINELQZX"

func sentinelize(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, e := range t {
			t[k] = sentinelize(e)
		}
		return t
	case []any:
		for i, e := range t {
			t[i] = sentinelize(e)
		}
		return t
	case string:
		return t + sentinel
	default:
		return v
	}
}

// TestADryRunPrintsNoStringValue plants a sentinel in EVERY string field of every record and
// every text blob, runs --dry-run, and counts it in the output: 0. Positive control: the sentinel
// IS in the materialised input.
func TestADryRunPrintsNoStringValue(t *testing.T) {
	w := loadWorld(t)
	for rel, f := range w.Claude.Files {
		data := fileBytes(t, f)
		switch {
		case strings.HasSuffix(rel, ".jsonl"):
			var out []string
			for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
				var v any
				if err := json.Unmarshal([]byte(line), &v); err != nil {
					t.Fatal(err)
				}
				b, _ := json.Marshal(sentinelize(v))
				out = append(out, string(b))
			}
			w.Claude.Files[rel] = map[string]string{"utf8": strings.Join(out, "\n") + "\n"}
		case !redact.IsBinary(data):
			w.Claude.Files[rel] = map[string]string{"utf8": string(data) + sentinel + "\n"}
		}
	}
	for which, docs := range w.Opencode.Exports {
		for id, raw := range docs {
			var v any
			_ = json.Unmarshal(raw, &v)
			b, _ := json.Marshal(sentinelize(v))
			w.Opencode.Exports[which][id] = b
		}
	}
	e := materialize(t, w)
	inputs := 0
	_ = filepath.WalkDir(e.claude, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			b, _ := os.ReadFile(p)
			inputs += strings.Count(string(b), sentinel)
		}
		return nil
	})
	if inputs < 100 {
		t.Fatalf("positive control: only %d sentinels in the input — the planting did not reach the fixture", inputs)
	}
	a := newAgent(t, e, twoInstances, &fakeRunner{exports: exportsOf(w, "before"), list: w.Opencode.SessionList},
		&bytes.Buffer{})
	a.OpencodeDirs = []string{"/work/alpha"}
	var out bytes.Buffer
	if err := a.DryRun(&out); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(out.String(), sentinel); n != 0 {
		t.Fatalf("--dry-run printed %d string value(s) of a record:\n%s", n, out.String())
	}
	if lines := strings.Count(out.String(), "\n"); lines != len(w.Sessions)-1 {
		t.Fatalf("--dry-run printed %d session lines, want %d (one per root session)\n%s", lines, len(w.Sessions)-1, out.String())
	}
	t.Logf("%d sentinels planted in the input, 0 in the output", inputs)
}
