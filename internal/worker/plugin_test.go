package worker

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/netid"
	"github.com/ZacxDev/cairn/internal/plugins"
	"github.com/ZacxDev/cairn/internal/redact"
	"github.com/ZacxDev/cairn/internal/transcript"
	"github.com/ZacxDev/cairn/internal/transcript/archive"
	"github.com/ZacxDev/cairn/internal/transcript/transcripttest"
)

// The plugin API over a REAL archive, a REAL store walk and a REAL toggle journal. Everything is
// SYNTHETIC: scopes `alpha-notes`/`beta-notes`, sessions `s-alpha` (WROTE `alpha-notes`, found only
// by the store walk — its upload declares nothing), `s-both` (declares both scopes) and `s-none`
// (touched nothing), the plugins `summary` and `lister`.
const (
	summaryToken = "fixture-plugin-token-summary-not-a-real-token"
	listerToken  = "fixture-plugin-token-lister-not-a-real-token"
	strangerTok  = "fixture-plugin-token-unregistered-not-a-real"
)

type pluginRig struct {
	*rig
	dir     string
	store   string
	journal plugins.Journal
	arc     interface{ Used() int64 }
	svc     *plugins.Service
}

func pluginRegistry(t *testing.T) *plugins.Registry {
	t.Helper()
	reg, err := plugins.ParseRegistry([]byte(`{"schema":1,"plugins":[
	 {"schema":1,"name":"summary","version":"0.1.0","description":"synthetic",	  "capabilities":["pending:list","transcript:read","output:write:summary"],"calls_model":true},
	 {"schema":1,"name":"lister","version":"0.0.1","description":"lists only",	  "capabilities":["pending:list"],"calls_model":false}]}`))
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func writeStoreWithTrailer(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for scope, nuance := range map[string]string{
		"alpha-notes": "- 2000-01-03: alpha noted the gadget [cairn: fixture-bot/s-alpha]",
		"beta-notes":  "- 2000-01-04: nobody signed this",
	} {
		dir := filepath.Join(root, scope)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := "---\nservice: gadget\nscope: " + scope + "\n---\n\n## What it is\nsynthetic.\n\n## Nuance / work-history\n\n" + nuance + "\n"
		if err := os.WriteFile(filepath.Join(dir, "gadget.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func newPluginRig(t *testing.T) *pluginRig { return newPluginRigQuota(t, 1<<30) }

func newPluginRigQuota(t *testing.T, quota int64) *pluginRig {
	t.Helper()
	dir := t.TempDir()
	red, err := redact.New(redact.SelfTestKey(43), nil)
	if err != nil {
		t.Fatal(err)
	}
	arc, err := archive.Open(archive.Config{Dir: dir, Retention: transcripttest.Retention, Quota: quota, Recheck: red,
		Now: func() time.Time { return transcripttest.Clock }})
	if err != nil {
		t.Fatal(err)
	}
	owner := string(ownerA.Kind) + ":" + string(ownerA.ID)
	user := func(text string) string {
		raw, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": text}})
		return string(raw)
	}
	transcripttest.Append(t, arc, transcripttest.Session{Root: "s-alpha", Owner: owner}, "main",
		[]string{user("alpha one"), user("alpha two"), user("alpha three")})
	transcripttest.Append(t, arc, transcripttest.Session{Root: "s-both", Owner: owner, Declared: []string{"alpha-notes", "beta-notes"}},
		"main", []string{user("both one")})
	transcripttest.Append(t, arc, transcripttest.Session{Root: "s-none", Owner: owner}, "main", []string{user("none one")})
	storeRoot := writeStoreWithTrailer(t)
	svc := &plugins.Service{
		Registry:    pluginRegistry(t),
		Journal:     plugins.Journal{Path: filepath.Join(t.TempDir(), "toggles.jsonl")},
		Transcripts: transcript.NewStore(arc, storeRoot),
		Known:       func(s string) bool { return s == "alpha-notes" || s == "beta-notes" },
		Now:         func() time.Time { return transcripttest.Clock.Add(time.Hour) },
	}
	g := &rig{t: t, arc: arc, tokens: filepath.Join(t.TempDir(), "worker-tokens")}
	writeRows(t, g.tokens, append(standardRows(), NewPluginTokenRow("summary", summaryToken), NewPluginTokenRow("lister", listerToken))...)
	w, _, err := New(Config{TokenFile: g.tokens, Owner: ownerA, OwnerHeld: func() bool { return true }, Archive: arc,
		Limiter: netid.NewRateLimiter(1000, time.Minute, time.Minute), Plugins: svc,
		Log: func(s string) { g.log = append(g.log, s) }})
	if err != nil {
		t.Fatal(err)
	}
	g.w = w
	svc.Log = func(s string) { g.log = append(g.log, s) }
	return &pluginRig{rig: g, dir: dir, store: storeRoot, journal: svc.Journal, arc: arc, svc: svc}
}

func (p *pluginRig) toggle(level plugins.Level, target, value string) {
	p.toggleFor("summary", level, target, value)
}

func (p *pluginRig) toggleFor(plugin string, level plugins.Level, target, value string) {
	p.t.Helper()
	if err := p.journal.Append(plugins.Record{Plugin: plugin, Level: level, Target: target, Value: value,
		By: plugins.By{Kind: "user", ID: "u-admin"}, At: "2000-01-02T03:04:05Z"}); err != nil {
		p.t.Fatal(err)
	}
}

func (p *pluginRig) pending(token string) []string {
	p.t.Helper()
	rec := p.do(http.MethodGet, PendingPattern, token, "")
	if rec.Code != http.StatusOK {
		p.t.Fatalf("pending answered %d %q", rec.Code, rec.Body.String())
	}
	var doc struct {
		Items []plugins.PendingItem `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		p.t.Fatal(err)
	}
	var out []string
	for _, it := range doc.Items {
		out = append(out, it.Session)
	}
	return out
}

// TestThePendingListIsEmptyUntilAToggleIsSet is clause (i) at unit level: nothing is pending with no
// toggle; with `alpha-notes` ON exactly the session whose EVERY scope in `V` is ON is listed —
// `s-alpha`, whose only scope is found by the STORE WALK (its upload declared nothing), and not
// `s-both` (beta is off) nor `s-none` (empty `V` is OFF). The mutant `plugins-toggle-default-on`
// turns the first assertion red.
func TestThePendingListIsEmptyUntilAToggleIsSet(t *testing.T) {
	p := newPluginRig(t)
	if got := p.pending(summaryToken); len(got) != 0 {
		t.Fatalf("with no toggle set the pending list is %v, want empty", got)
	}
	p.toggle(plugins.LevelScope, "alpha-notes", plugins.ValueOn)
	if got := p.pending(summaryToken); !slices.Equal(got, []string{"s-alpha"}) {
		t.Fatalf("with alpha-notes ON the pending list is %v, want exactly [s-alpha]", got)
	}
	p.toggle(plugins.LevelScope, "beta-notes", plugins.ValueAllowed)
	if got := p.pending(summaryToken); !slices.Equal(got, []string{"s-alpha"}) {
		t.Fatalf("with beta-notes only ALLOWED the pending list is %v, want [s-alpha] — allowed is consent, not run", got)
	}
	p.toggle(plugins.LevelScope, "beta-notes", plugins.ValueOn)
	if got := p.pending(summaryToken); !slices.Equal(got, []string{"s-alpha", "s-both"}) {
		t.Fatalf("with both scopes ON the pending list is %v, want [s-alpha s-both]", got)
	}
	p.toggle(plugins.LevelSession, "s-alpha", plugins.ValueOff)
	if got := p.pending(summaryToken); !slices.Equal(got, []string{"s-both"}) {
		t.Fatalf("after the owner's opt-out of s-alpha the pending list is %v, want [s-both]", got)
	}
}

// TestEveryPluginRefusalIsTheSameBytes is clause (j) at unit level: the summary token is refused,
// with ONE body, on a transcript where it is OFF, on a session that does not exist, and on writing a
// `ticket-edge`; a plugin without `transcript:read` is refused the same; a plugin without `output:read:summary` reading outputs is refused the same; the POSITIVE CONTROL is the same token reading a session where it is ON.
func TestEveryPluginRefusalIsTheSameBytes(t *testing.T) {
	p := newPluginRig(t)
	p.toggle(plugins.LevelScope, "alpha-notes", plugins.ValueOn)
	// `lister` is ON for s-alpha too, so its refusal below can ONLY be the missing capability — the
	// toggle check is not what answers it.
	p.toggleFor("lister", plugins.LevelScope, "alpha-notes", plugins.ValueOn)
	ok := p.do(http.MethodGet, TranscriptPattern+"?session=s-alpha", summaryToken, "")
	if ok.Code != http.StatusOK || !strings.Contains(ok.Body.String(), "alpha two") {
		t.Fatalf("POSITIVE CONTROL: reading s-alpha (ON) answered %d %q", ok.Code, ok.Body.String())
	}
	edge := `{"plugin":"summary","plugin_version":"0.1.0","model":"fake","session":"s-alpha","input_watermark":{"main":1},` +
		`"type":"ticket-edge","body":{"system":"clickup","ticket_id":"abc123","signal":"branch-name","locator":{"pr":1}}}`
	refusals := map[string]struct{ method, path, token, body string }{
		"a transcript where it is OFF":    {http.MethodGet, TranscriptPattern + "?session=s-both", summaryToken, ""},
		"a session that was never stored": {http.MethodGet, TranscriptPattern + "?session=s-absent", summaryToken, ""},
		"an id that is not a session id":  {http.MethodGet, TranscriptPattern + "?session=..%2Fescape", summaryToken, ""},
		"a ticket-edge it does not write": {http.MethodPost, OutputsPattern, summaryToken, edge},
		"a read it has no capability for": {http.MethodGet, TranscriptPattern + "?session=s-alpha", listerToken, ""},
		"outputs it may not read":         {http.MethodGet, OutputsPattern + "?session=s-alpha&type=summary", summaryToken, ""},
	}
	for name, c := range refusals {
		rec := p.do(c.method, c.path, c.token, c.body)
		if rec.Code != http.StatusForbidden || rec.Body.String() != ForbiddenBody {
			t.Errorf("%s answered %d %q, want 403 %q", name, rec.Code, rec.Body.String(), ForbiddenBody)
		}
	}
}

// TestAnOutputIsStoredUnderTheTokensNameAndAdvancesTheWatermark: a body naming another plugin is
// stored under the TOKEN's plugin (the mutant `plugins-plugin-name-from-body-not-token`), the write
// removes the session from the next pending query, and a watermark past what is stored is a 400.
func TestAnOutputIsStoredUnderTheTokensNameAndAdvancesTheWatermark(t *testing.T) {
	p := newPluginRig(t)
	p.toggle(plugins.LevelScope, "alpha-notes", plugins.ValueOn)
	body := func(seq int) string {
		return `{"plugin":"lister","plugin_version":"9.9.9","model":"fake-model","session":"s-alpha","input_watermark":{"main":` +
			strconv.Itoa(seq) + `},"type":"summary","body":{"text":"three synthetic turns"}}`
	}
	if rec := p.do(http.MethodPost, OutputsPattern, summaryToken, body(4)); rec.Code != http.StatusBadRequest {
		t.Fatalf("a watermark past the stored seq answered %d %q, want 400", rec.Code, rec.Body.String())
	}
	if got := p.pending(summaryToken); !slices.Equal(got, []string{"s-alpha"}) {
		t.Fatalf("before the write the pending list is %v", got)
	}
	rec := p.do(http.MethodPost, OutputsPattern, summaryToken, body(3))
	if rec.Code != http.StatusCreated || !strings.HasPrefix(rec.Body.String(), `{"id":"o-`) {
		t.Fatalf("the write answered %d %q, want 201 with an id", rec.Code, rec.Body.String())
	}
	if got := p.pending(summaryToken); len(got) != 0 {
		t.Fatalf("after an output at the newest record the pending list is %v, want empty", got)
	}
	v, err := p.svc.Open("summary", "s-alpha")
	if err != nil {
		t.Fatal(err)
	}
	var stored []plugins.Output
	_ = v.Outputs(func(line []byte) error {
		var o plugins.Output
		if err := json.Unmarshal(line, &o); err != nil {
			t.Fatal(err)
		}
		stored = append(stored, o)
		return nil
	})
	if len(stored) != 1 || stored[0].Plugin != "summary" || stored[0].PluginVersion != "0.1.0" {
		t.Fatalf("stored %+v, want ONE output attributed to the token's plugin, summary 0.1.0", stored)
	}
}

// TestAPluginRowIsAdmittedOnlyForARegisteredPlugin: at startup a row for a plugin the registry does
// not hold refuses the whole file; at request time such a token is the uniform 401. And the two
// kinds do not cross: a plugin token on a capture route and a capture token on a plugin route are
// each the uniform 401.
func TestAPluginRowIsAdmittedOnlyForARegisteredPlugin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens")
	writeRows(t, path, NewPluginTokenRow("stranger", strangerTok))
	reg := pluginRegistry(t)
	if _, err := LoadTokens(path, Wall{Owner: ownerA, Plugins: reg.Holds}); !errors.Is(err, ErrTokenFileContent) {
		t.Fatalf("an unregistered plugin's row loaded (%v)", err)
	}
	if _, err := LoadTokens(path, Wall{Owner: ownerA}); !errors.Is(err, ErrTokenFileContent) {
		t.Fatalf("a plugin row loaded on an instance with no registry (%v)", err)
	}
	writeRows(t, path, NewPluginTokenRow("summary", summaryToken))
	if rows, err := LoadTokens(path, Wall{Owner: ownerA, Plugins: reg.Holds}); err != nil || len(rows) != 1 || rows[0].Plugin != "summary" {
		t.Fatalf("POSITIVE CONTROL: a registered plugin's row: %v %v", rows, err)
	}
	p := newPluginRig(t)
	for name, c := range map[string]struct{ method, path, token string }{
		"a capture token on the pending route":    {http.MethodGet, PendingPattern, captureA},
		"a plugin token on a capture route":       {http.MethodPost, recordsPath, summaryToken},
		"an unregistered plugin's token":          {http.MethodGet, PendingPattern, strangerTok},
		"no token on the pending route":           {http.MethodGet, PendingPattern, ""},
		"a plugin token on the outputs write row": {http.MethodPost, OutputsPattern, garbage},
	} {
		if rec := p.do(c.method, c.path, c.token, "{}"); rec.Code != http.StatusUnauthorized || rec.Body.String() != unauthorizedBody {
			t.Errorf("%s answered %d %q, want the uniform 401", name, rec.Code, rec.Body.String())
		}
	}
	if got := p.pending(listerToken); got != nil {
		t.Fatalf("POSITIVE CONTROL: lister's pending is %v", got)
	}
}

// TestOneUndecidableSessionDoesNotStopThePendingQuery: a session whose meta.json no longer parses is
// LEFT OUT (OFF, fail closed) and every other session is still answered — before the fix it was a 500
// for every session, forever (audit round 1).
func TestOneUndecidableSessionDoesNotStopThePendingQuery(t *testing.T) {
	p := newPluginRig(t)
	p.toggle(plugins.LevelScope, "alpha-notes", plugins.ValueOn)
	p.toggle(plugins.LevelScope, "beta-notes", plugins.ValueOn)
	if got := p.pending(summaryToken); !slices.Equal(got, []string{"s-alpha", "s-both"}) {
		t.Fatalf("POSITIVE CONTROL: pending is %v", got)
	}
	if err := os.WriteFile(filepath.Join(p.dir, "sessions", "s-both", "meta.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := p.pending(summaryToken); !slices.Equal(got, []string{"s-alpha"}) {
		t.Fatalf("with s-both's meta damaged the pending list is %v, want [s-alpha]", got)
	}
	if !slices.ContainsFunc(p.log, func(l string) bool { return strings.Contains(l, "session s-both is left out") }) {
		t.Fatalf("the left-out session was not said: %v", p.log)
	}
}

// TestABrokenStoreIsAnErrorNotAnEmptyList: with the store root gone, a session that must be WALKED
// cannot be decided, and the query is a 500 — even when another session was ruled out without a walk
// (audit round 2: skipping every per-session error answered a removed store with a confident empty
// list). s-both is ruled out on its stored scopes (beta is off); s-alpha needs the walk.
func TestABrokenStoreIsAnErrorNotAnEmptyList(t *testing.T) {
	p := newPluginRig(t)
	p.toggle(plugins.LevelScope, "alpha-notes", plugins.ValueOn)
	if got := p.pending(summaryToken); !slices.Equal(got, []string{"s-alpha"}) {
		t.Fatalf("POSITIVE CONTROL: pending is %v", got)
	}
	if err := os.RemoveAll(p.store); err != nil {
		t.Fatal(err)
	}
	if rec := p.do(http.MethodGet, PendingPattern, summaryToken, ""); rec.Code != http.StatusInternalServerError {
		t.Fatalf("with the store removed the pending route answered %d %q, want 500", rec.Code, rec.Body.String())
	}
}

// TestThePluginTranscriptReadIsTheAgentReadModel: the plugin route pages through S8's ONE pager —
// `view=conversation` by default and `view=raw` on request, `next_seq` and `max_bytes` included —
// and an unknown view is a 400 decided before any session is looked at (so it is the same for a
// session the plugin is OFF for), while a stream the session does not hold is the one 403.
func TestThePluginTranscriptReadIsTheAgentReadModel(t *testing.T) {
	p := newPluginRig(t)
	p.toggle(plugins.LevelScope, "alpha-notes", plugins.ValueOn)
	var page struct {
		View    string `json:"view"`
		NextSeq int64  `json:"next_seq"`
		Done    bool   `json:"done"`
		Records []struct {
			Seq int64 `json:"seq"`
		} `json:"records"`
	}
	read := func(query string) {
		t.Helper()
		rec := p.do(http.MethodGet, TranscriptPattern+"?session=s-alpha"+query, summaryToken, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s answered %d %q", query, rec.Code, rec.Body.String())
		}
		page.Records = nil
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
	}
	read("")
	if page.View != "conversation" || len(page.Records) != 3 || !page.Done || page.NextSeq != 4 {
		t.Fatalf("the default read is %+v, want the conversation view of all 3 records, done, next_seq 4", page)
	}
	read("&view=raw&from_seq=2&max_bytes=1")
	if page.View != "raw" || len(page.Records) != 1 || page.Records[0].Seq != 2 || page.Done || page.NextSeq != 3 {
		t.Fatalf("a one-byte raw page from 2 is %+v, want record 2 alone (whole), next_seq 3", page)
	}
	for _, session := range []string{"s-alpha", "s-both"} {
		if rec := p.do(http.MethodGet, TranscriptPattern+"?session="+session+"&view=summary", summaryToken, ""); rec.Code != http.StatusBadRequest {
			t.Fatalf("an unknown view on %s answered %d, want 400 before the session is looked at", session, rec.Code)
		}
	}
	if rec := p.do(http.MethodGet, TranscriptPattern+"?session=s-alpha&stream=subagent:a1", summaryToken, ""); rec.Code != http.StatusForbidden ||
		rec.Body.String() != ForbiddenBody {
		t.Fatalf("a stream the session does not hold answered %d %q, want the one 403", rec.Code, rec.Body.String())
	}
}

// TestNothingIsWalkedWhenNoScopeConsents: with no scope `allowed` or `on` for the plugin the pending
// query reads no session and walks no store — measured by removing the store root, which any walk
// turns into a 500 (`TestABrokenStoreIsAnErrorNotAnEmptyList`), and getting 200 with no item. RED
// before #270 round 1 (every root was walked on every poll, toggles or not).
func TestNothingIsWalkedWhenNoScopeConsents(t *testing.T) {
	p := newPluginRig(t)
	p.toggle(plugins.LevelArc, "alpha-notes/arc-one", plugins.ValueOn) // an arc alone consents to nothing
	if err := os.RemoveAll(p.store); err != nil {
		t.Fatal(err)
	}
	if got := p.pending(summaryToken); len(got) != 0 {
		t.Fatalf("with no scope consenting the pending list is %v", got)
	}
}

// TestAPluginCannotFillTheCaptureQuota: one plugin's outputs stop at its share (1/20 of the quota) with
// a NAMED 507 — not the opaque 500 they were — said ONCE in the log however many are refused, and
// the instance is left with room for capture. RED before #270 round 1: 500 "internal error" once the
// whole quota was gone, a log line per request, and nothing reserved for capture.
func TestAPluginCannotFillTheCaptureQuota(t *testing.T) {
	const quota = 400 << 10 // the plugin's share is 20 KiB
	p := newPluginRigQuota(t, quota)
	p.toggle(plugins.LevelScope, "alpha-notes", plugins.ValueOn)
	text := strings.Repeat("synthetic summary text ", 400) // ~9 KiB a line
	body := `{"plugin":"summary","plugin_version":"0.1.0","model":"fake","session":"s-alpha","input_watermark":{"main":3},` +
		`"type":"summary","body":{"text":"` + text + `"}}`
	var codes []int
	for i := 0; i < 5; i++ {
		rec := p.do(http.MethodPost, OutputsPattern, summaryToken, body)
		codes = append(codes, rec.Code)
		if rec.Code == http.StatusInsufficientStorage && rec.Body.String() != fmt.Sprintf(`{"refused":"quota","quota_bytes":%d}`+"\n", quota/20) {
			t.Fatalf("the 507 body is %q", rec.Body.String())
		}
	}
	if !slices.Equal(codes, []int{201, 201, 507, 507, 507}) {
		t.Fatalf("five ~9 KiB outputs against a 20 KiB share answered %v, want 201 201 507 507 507", codes)
	}
	said := 0
	for _, l := range p.log {
		if strings.Contains(l, "share of the transcript quota") {
			said++
		}
	}
	if said != 1 {
		t.Fatalf("the refusal was logged %d times, want once: %v", said, p.log)
	}
	if used := p.arc.Used(); used > quota/2 {
		t.Fatalf("the archive holds %d of %d bytes after the plugin was stopped — capture's room is gone", used, quota)
	}
}
