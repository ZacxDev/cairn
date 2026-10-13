package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/plugins/internal/pluginapi"
)

// 🔴 THREE IN-PROCESS FAKES AND TWO HOST PROGRAMS, ALL SYNTHETIC. The fake POD answers the plugin API
// in S5's shapes and REFUSES what the pod refuses (a body outside its type's schema, a type the
// manifest does not write, a watermark past what is stored). The fake CLICKUP answers the one route
// this plugin calls and records every request's method and time. The fake PROVIDER answers the
// Anthropic Messages API with a configured text and records what it was sent. `git` is the REAL git,
// over a repository this test creates; the GitHub CLI is THIS TEST BINARY re-executed (TestMain), which
// answers `pr view` from files the test writes — so no shell and no network is involved.
//
// Because the fakes are written from the same spelling as the client they prove the PLUGIN, not the
// contract: what runs this binary against the REAL worker listener is the parent module's
// `internal/worker` `TestTheClickupPluginAgainstTheRealListener`. A real ClickUp workspace and a real
// provider are never called by CI.

const (
	fakeToken   = "plugin-token-synthetic-0002"
	fakeCUToken = "pk_synthetic_clickup_token_0002"
	fakeKey     = "provider-key-synthetic-0002"
	session     = "s-clk-0001"
	otherSess   = "s-clk-0002"
	team        = "9000001"
	listID      = "list-0001"
)

// fakeGHEnv names the directory the re-executed test binary answers `gh pr view` from.
const fakeGHEnv = "CAIRN_CLICKUP_TEST_FAKE_GH_DIR"

func TestMain(m *testing.M) {
	if dir := os.Getenv(fakeGHEnv); dir != "" {
		os.Exit(fakeGH(dir, os.Args[1:]))
	}
	os.Exit(m.Run())
}

// fakeGH is `gh pr view <n> --repo <owner/name> --json title,body,headRefName`, and nothing else.
func fakeGH(dir string, args []string) int {
	f, _ := os.OpenFile(filepath.Join(dir, "calls.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	fmt.Fprintln(f, strings.Join(args, " "))
	f.Close()
	if len(args) != 7 || args[0] != "pr" || args[1] != "view" || args[3] != "--repo" || args[5] != "--json" ||
		args[6] != "title,body,headRefName" {
		fmt.Fprintln(os.Stderr, "fake gh: unexpected arguments")
		return 2
	}
	raw, err := os.ReadFile(filepath.Join(dir, strings.ReplaceAll(args[4], "/", "_")+"#"+args[2]+".json"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake gh: no such pull request")
		return 1
	}
	os.Stdout.Write(raw)
	return 0
}

// fakePod is one stored session and its outputs.
type fakePod struct {
	mu      sync.Mutex
	records map[string][]string // stream → records, seq = index+1
	outputs []pluginapi.Output
	pageMax int
	// reads is every transcript request as "<stream> <view> <from_seq>"; limits every pending `limit`.
	reads  []string
	limits []int
	// failWrite makes the Nth output write (1-based) answer 500.
	failWrite int
	writes    int
	clock     time.Time
	// extra is further sessions (each one `main` stream), listed BEFORE `session`: they are older.
	extra map[string][]string
	// refuse answers every write for these sessions 507, as a pod whose quota is full does.
	refuse map[string]bool
	// titles is the instance's `-plugin-store-ticket-titles`: false refuses a body carrying a title.
	titles bool
}

// of is one session's streams; nil when the pod does not hold it.
func (p *fakePod) of(id string) map[string][]string {
	if id == session {
		return p.records
	}
	if recs, ok := p.extra[id]; ok {
		return map[string][]string{"main": recs}
	}
	return nil
}

// ids is every stored session, oldest first.
func (p *fakePod) ids() []string {
	var out []string
	for id := range p.extra {
		out = append(out, id)
	}
	slices.Sort(out)
	return append(out, session)
}

// validStream is the pod's `ValidStream` vocabulary (`internal/plugins`): what a watermark and a record
// locator may name, and what the pending query lists.
func validStream(s string) bool {
	return s == "main" || strings.HasPrefix(s, "subagent:") || strings.HasPrefix(s, "child:")
}

var manifestWrites = []string{TypeEdge, TypeSuggestion, TypeScan}

func strictInto(raw []byte, into any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(into)
}

// podAccepts is the pod's body schema for the three types this plugin writes (`internal/plugins`
// outputs.go), re-stated.
func podAccepts(typ string, raw []byte) error {
	switch typ {
	case TypeEdge:
		var e Edge
		if err := strictInto(raw, &e); err != nil {
			return err
		}
		shapes := 0
		if e.Locator.Stream != "" || e.Locator.Seq != 0 {
			if !validStream(e.Locator.Stream) || e.Locator.Seq < 1 {
				return fmt.Errorf("a record locator needs a stream and a seq")
			}
			shapes++
		}
		if e.Locator.Commit != "" {
			shapes++
		}
		if e.Locator.PR != 0 {
			shapes++
		}
		if e.Locator.Entry != "" {
			shapes++
		}
		signals := []string{SigBranch, SigCommitMsg, SigCommitTrlr, SigPRBody, SigPRLink, SigTranscript, SigEntryRef}
		if e.System != "clickup" || !canonicalID.MatchString(e.TicketID) || !slices.Contains(signals, e.Signal) || shapes != 1 ||
			(e.CustomID != "" && !customIDShape.MatchString(e.CustomID)) {
			return fmt.Errorf("not an edge the pod stores: %s", raw)
		}
	case TypeSuggestion:
		var s Suggestion
		if err := strictInto(raw, &s); err != nil {
			return err
		}
		if !canonicalID.MatchString(s.TicketID) || s.Confidence < 0 || s.Confidence > 1 || len(s.Rationale) > 280 {
			return fmt.Errorf("not a suggestion the pod stores: %s", raw)
		}
	case TypeScan:
		var s Scan
		if err := strictInto(raw, &s); err != nil {
			return err
		}
		if s.System != "clickup" || s.Edges < 0 || s.Suggestions < 0 {
			return fmt.Errorf("not a scan the pod stores: %s", raw)
		}
	default:
		return fmt.Errorf("type %q is not one this plugin's manifest writes", typ)
	}
	return nil
}

func writeJSON(rw http.ResponseWriter, status int, v any) {
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(status)
	_ = json.NewEncoder(rw).Encode(v)
}

func (p *fakePod) streams(id string) map[string]int64 {
	out := map[string]int64{}
	for s, recs := range p.of(id) {
		out[s] = int64(len(recs))
	}
	return out
}

func (p *fakePod) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+fakeToken {
		http.Error(rw, "unauthorized", http.StatusUnauthorized)
		return
	}
	q := r.URL.Query()
	switch {
	case r.URL.Path == "/plugin/v1/pending":
		items := []any{}
		limit, _ := strconv.Atoi(q.Get("limit"))
		p.limits = append(p.limits, limit)
		for _, id := range p.ids() {
			var wm map[string]int64
			var prev any
			for _, o := range p.outputs {
				if o.Plugin == Name && o.Session == id {
					wm = o.InputWatermark
					prev = map[string]any{"id": o.ID, "watermark": wm}
				}
			}
			grown := false
			pos := map[string]any{}
			for s, seq := range p.streams(id) {
				pos[s] = map[string]int64{"seq_to": seq}
				if validStream(s) && seq > wm[s] {
					grown = true
				}
			}
			if grown && len(items) < limit {
				items = append(items, map[string]any{"session": id, "streams": pos, "prev_output": prev})
			}
		}
		writeJSON(rw, 200, map[string]any{"items": items})
	case r.URL.Path == "/plugin/v1/transcript":
		recs, ok := p.of(q.Get("session"))[q.Get("stream")]
		if !ok {
			writeJSON(rw, 403, map[string]string{"refused": "plugin-forbidden"})
			return
		}
		view := q.Get("view")
		p.reads = append(p.reads, q.Get("stream")+" "+view+" "+q.Get("from_seq"))
		from, _ := strconv.Atoi(q.Get("from_seq"))
		page := []any{}
		next := from
		for i := max(from, 1); i <= len(recs) && len(page) < p.pageMax; i++ {
			rec := recs[i-1]
			if view != "raw" && strings.Contains(rec, `"type":"pr-link"`) {
				// The conversation view's placeholder for a bookkeeping record.
				page = append(page, map[string]any{"seq": i, "src": strconv.Itoa(i),
					"omitted": []any{map[string]any{"class": "bookkeeping", "path": "", "bytes": len(rec), "label": "pr-link"}}})
			} else {
				page = append(page, map[string]any{"seq": i, "src": strconv.Itoa(i), "rec": json.RawMessage(rec)})
			}
			next = i + 1
		}
		writeJSON(rw, 200, map[string]any{"records": page, "next_seq": next, "done": next > len(recs), "seq_to": len(recs)})
	case r.URL.Path == "/plugin/v1/outputs" && r.Method == http.MethodGet:
		outs := []pluginapi.Output{}
		for _, o := range p.outputs {
			if o.Type == q.Get("type") && o.Session == q.Get("session") {
				outs = append(outs, o)
			}
		}
		writeJSON(rw, 200, map[string]any{"outputs": outs})
	case r.URL.Path == "/plugin/v1/outputs" && r.Method == http.MethodPost:
		var env struct {
			Plugin         string           `json:"plugin"`
			PluginVersion  string           `json:"plugin_version"`
			Model          string           `json:"model"`
			Session        string           `json:"session"`
			InputWatermark map[string]int64 `json:"input_watermark"`
			Type           string           `json:"type"`
			Body           json.RawMessage  `json:"body"`
		}
		raw, _ := io.ReadAll(r.Body)
		if err := strictInto(raw, &env); err != nil || p.of(env.Session) == nil || env.Model == "" || len(env.InputWatermark) == 0 {
			http.Error(rw, "bad envelope", 400)
			return
		}
		if !slices.Contains(manifestWrites, env.Type) {
			writeJSON(rw, 403, map[string]string{"refused": "plugin-forbidden"})
			return
		}
		var titled struct {
			Title string `json:"title"`
		}
		if _ = json.Unmarshal(env.Body, &titled); titled.Title != "" && !p.titles {
			http.Error(rw, "this instance stores ticket ids only", 400)
			return
		}
		if err := podAccepts(env.Type, env.Body); err != nil {
			http.Error(rw, "body: "+err.Error(), 400)
			return
		}
		for s, seq := range env.InputWatermark {
			if top, ok := p.streams(env.Session)[s]; !ok || !validStream(s) || seq > top {
				http.Error(rw, "watermark past what is stored", 400)
				return
			}
		}
		if p.refuse[env.Session] {
			http.Error(rw, `{"refused":"quota","quota_bytes":1}`, http.StatusInsufficientStorage)
			return
		}
		p.writes++
		if p.writes == p.failWrite {
			http.Error(rw, "internal error", 500)
			return
		}
		id := fmt.Sprintf("o-%04d", len(p.outputs)+1)
		p.clock = p.clock.Add(time.Second)
		p.outputs = append(p.outputs, pluginapi.Output{ID: id, Plugin: Name, PluginVersion: Version, Model: env.Model,
			Session: env.Session, InputWatermark: env.InputWatermark, Type: env.Type, Body: env.Body,
			ProducedAt: p.clock.UTC().Format(time.RFC3339)})
		writeJSON(rw, 201, map[string]string{"id": id})
	default:
		http.NotFound(rw, r)
	}
}

// addSummary stores a `summary` output as plugin A would have written it.
func (p *fakePod) addSummary(text string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clock = p.clock.Add(time.Second)
	p.outputs = append(p.outputs, pluginapi.Output{ID: fmt.Sprintf("o-%04d", len(p.outputs)+1), Plugin: "summary",
		PluginVersion: "0.1.0", Model: "fake-model-1", Session: session, InputWatermark: map[string]int64{"main": 1},
		Type: TypeSummary, Body: json.RawMessage(`{"text":` + q(text) + `}`), ProducedAt: p.clock.UTC().Format(time.RFC3339)})
}

func (p *fakePod) grow(stream string, recs ...string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.records[stream] = append(p.records[stream], recs...)
}

// edges is every stored `ticket-edge` body, in write order.
func (p *fakePod) edges(t *testing.T) []Edge {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []Edge
	for _, o := range p.outputs {
		if o.Type == TypeEdge {
			var e Edge
			if err := json.Unmarshal(o.Body, &e); err != nil {
				t.Fatal(err)
			}
			out = append(out, e)
		}
	}
	return out
}

func (p *fakePod) ofType(typ string) []pluginapi.Output {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []pluginapi.Output
	for _, o := range p.outputs {
		if o.Type == typ {
			out = append(out, o)
		}
	}
	return out
}

// fakeClickUp answers `GET /api/v2/list/<id>/task`, a page at a time.
type fakeClickUp struct {
	mu      sync.Mutex
	tasks   []Ticket
	perPage int
	// limited answers the first N requests 429, naming a reset `resetIn` from now.
	limited int
	resetIn time.Duration
	noReset bool
	status  int // non-zero: answer this status, echoing the token it was sent
	// methods and times record every request; resets every reset time a 429 named.
	methods []string
	times   []time.Time
	resets  []time.Time
	auth    []string
	paths   []string
}

func (c *fakeClickUp) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.methods = append(c.methods, r.Method)
	c.times = append(c.times, time.Now())
	c.auth = append(c.auth, r.Header.Get("Authorization"))
	c.paths = append(c.paths, r.URL.Path+"?"+r.URL.RawQuery)
	if r.Method != http.MethodGet || r.URL.Path != "/api/v2/list/"+listID+"/task" || r.Header.Get("Authorization") != fakeCUToken {
		http.Error(rw, `{"err":"Token invalid","ECODE":"OAUTH_025"}`, 401)
		return
	}
	if c.status != 0 {
		http.Error(rw, `{"err":"upstream failed for `+r.Header.Get("Authorization")+`"}`, c.status)
		return
	}
	if len(c.methods) <= c.limited {
		reset := time.Now().Add(c.resetIn)
		// Whole seconds, rounded UP: what a Unix-seconds header can say.
		reset = time.Unix(reset.Unix()+1, 0)
		c.resets = append(c.resets, reset)
		if !c.noReset {
			rw.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))
		}
		rw.Header().Set("X-RateLimit-Remaining", "0")
		http.Error(rw, `{"err":"Rate limit reached","ECODE":"APP_002"}`, 429)
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	per := c.perPage
	if per == 0 {
		per = 100
	}
	lo, hi := min(page*per, len(c.tasks)), min((page+1)*per, len(c.tasks))
	tasks := []any{}
	for _, t := range c.tasks[lo:hi] {
		var custom any
		if t.CustomID != "" {
			custom = t.CustomID
		}
		// A description and an assignee are in every real answer; the plugin must read neither.
		tasks = append(tasks, map[string]any{"id": t.ID, "custom_id": custom, "name": t.Name,
			"description": "synthetic description " + t.ID, "assignees": []any{map[string]any{"username": "synthetic-user"}}})
	}
	writeJSON(rw, 200, map[string]any{"tasks": tasks, "last_page": hi >= len(c.tasks)})
}

// fakeProvider answers the Anthropic Messages API with `answer` and records each user message.
type fakeProvider struct {
	mu     sync.Mutex
	answer string
	status int
	users  []string
}

func (f *fakeProvider) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	raw, _ := io.ReadAll(r.Body)
	var body struct {
		Messages []struct{ Role, Content string } `json:"messages"`
	}
	if r.URL.Path != "/v1/messages" || r.Header.Get("x-api-key") != fakeKey || json.Unmarshal(raw, &body) != nil || len(body.Messages) == 0 {
		http.Error(rw, "unauthorized", 401)
		return
	}
	f.users = append(f.users, body.Messages[0].Content)
	if f.status != 0 {
		http.Error(rw, `{"error":{"message":"synthetic failure"}}`, f.status)
		return
	}
	writeJSON(rw, 200, map[string]any{"model": "fake-model-1", "stop_reason": "end_turn",
		"content": []any{map[string]string{"type": "text", "text": f.answer}},
		"usage":   map[string]int{"input_tokens": 10, "output_tokens": 5}})
}

func (f *fakeProvider) received() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.users...)
}

// world is the fakes and the files one run reads.
type world struct {
	pod                    *fakePod
	cu                     *fakeClickUp
	prov                   *fakeProvider
	dir                    string
	podURL, cuURL, provURL string
	clock                  time.Time
	// realClock runs the plugin on time.Now (the 429 wait is measured against the header's real time).
	realClock bool
	// args are flags every run of this world adds.
	args []string
}

// The synthetic workspace: three tickets, one with a custom id.
func tickets() []Ticket {
	return []Ticket{
		{ID: "clk0000a1", Name: "Synthetic ticket one: lorem export"},
		{ID: "clk0000b2", Name: "Synthetic ticket two: ipsum import"},
		{ID: "clk0000c3", CustomID: "ALPHA-12", Name: "Synthetic ticket three: dolor sync"},
	}
}

func newWorld(t *testing.T, main ...string) *world {
	t.Helper()
	w := &world{
		pod:  &fakePod{records: map[string][]string{"main": main}, pageMax: 3, clock: time.Date(2000, 1, 2, 3, 0, 0, 0, time.UTC)},
		cu:   &fakeClickUp{tasks: tickets()},
		prov: &fakeProvider{answer: "[]"},
		dir:  t.TempDir(), clock: time.Date(2000, 1, 2, 3, 4, 5, 0, time.UTC),
	}
	for _, s := range []struct {
		h   http.Handler
		url *string
	}{{w.pod, &w.podURL}, {w.cu, &w.cuURL}, {w.prov, &w.provURL}} {
		srv := httptest.NewServer(s.h)
		t.Cleanup(srv.Close)
		*s.url = srv.URL
	}
	for name, v := range map[string]string{"token": fakeToken, "clickup-token": fakeCUToken, "key": fakeKey} {
		if err := os.WriteFile(filepath.Join(w.dir, name), []byte(v+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return w
}

// run runs the plugin once with its real flag parsing. No model is asked unless `withModel` is among
// the world's args.
func (w *world) run(t *testing.T, extra ...string) (int, string, string) {
	t.Helper()
	args := []string{"-worker-url", w.podURL, "-token-file", filepath.Join(w.dir, "token"),
		"-clickup-url", w.cuURL, "-clickup-token-file", filepath.Join(w.dir, "clickup-token"), "-clickup-list", listID,
		"-state-file", filepath.Join(w.dir, "sessions.json")}
	args = append(append(args, w.args...), extra...)
	var out, errb strings.Builder
	now := func() time.Time { return w.clock }
	if w.realClock {
		now = time.Now
	}
	code := run(args, &out, &errb, now)
	return code, out.String(), errb.String()
}

// withModel is the flags that configure the fake provider.
func (w *world) withModel() []string {
	return []string{"-provider-url", w.provURL, "-key-file", filepath.Join(w.dir, "key"), "-model", "fake-model-1",
		"-spend-ledger", filepath.Join(w.dir, "spend.log")}
}

// Synthetic records.
func q(s string) string { raw, _ := json.Marshal(s); return string(raw) }

func human(text, branch string) string {
	b := ""
	if branch != "" {
		b = `"gitBranch":` + q(branch) + `,`
	}
	return `{"type":"user",` + b + `"message":{"role":"user","content":` + q(text) + `}}`
}

func assistant(text string) string {
	return `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":` + q(text) + `}]}}`
}

func prLink(n int, repo string) string {
	return fmt.Sprintf(`{"type":"pr-link","prNumber":%d,"prUrl":"https://github.com/%s/pull/%d","prRepository":%s,"timestamp":"2000-01-02T03:04:05Z"}`,
		n, repo, n, q(repo))
}

const cuURLPrefix = "https://app.clickup.com/t/"

// gitRepo creates a repository and returns its path and a function that commits a message and
// answers the commit's SHA.
func gitRepo(t *testing.T) (string, func(msg string) string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		// 🔴 NOT A SKIP: a skipped commit test is a commit matcher nobody ran.
		t.Fatalf("git is required by this suite and is not on PATH: %v", err)
	}
	dir := t.TempDir()
	git := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
			"GIT_AUTHOR_NAME=Synthetic Author", "GIT_AUTHOR_EMAIL=author@example.invalid",
			"GIT_COMMITTER_NAME=Synthetic Author", "GIT_COMMITTER_EMAIL=author@example.invalid",
			"GIT_AUTHOR_DATE=2000-01-02T03:04:05Z", "GIT_COMMITTER_DATE=2000-01-02T03:04:05Z")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q", "-b", "main")
	n := 0
	return dir, func(msg string) string {
		n++
		git("commit", "-q", "--allow-empty", "--no-gpg-sign", "-m", msg)
		return git("rev-parse", "HEAD")
	}
}

// ghDir arms the re-executed fake GitHub CLI for this test and returns its directory and binary.
func ghDir(t *testing.T) (dir, bin string) {
	t.Helper()
	dir = t.TempDir()
	t.Setenv(fakeGHEnv, dir)
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return dir, bin
}

func writePR(t *testing.T, dir, repo string, n int, title, body, head string) {
	t.Helper()
	raw, _ := json.Marshal(map[string]string{"title": title, "body": body, "headRefName": head})
	if err := os.WriteFile(filepath.Join(dir, strings.ReplaceAll(repo, "/", "_")+"#"+strconv.Itoa(n)+".json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

// want asserts the stored edges are exactly `edges`, in any order.
func wantEdges(t *testing.T, got []Edge, want ...Edge) {
	t.Helper()
	key := func(e Edge) string { raw, _ := json.Marshal(e); return string(raw) }
	var g, w []string
	for _, e := range got {
		g = append(g, key(e))
	}
	for _, e := range want {
		w = append(w, key(e))
	}
	slices.Sort(g)
	slices.Sort(w)
	if !slices.Equal(g, w) {
		t.Fatalf("the stored edges are\n  %s\nwant\n  %s", strings.Join(g, "\n  "), strings.Join(w, "\n  "))
	}
}

func rec(seq int64) Locator { return Locator{Stream: "main", Seq: seq} }
