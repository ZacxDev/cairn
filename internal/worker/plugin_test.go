package worker

import (
	"encoding/json"
	"errors"
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
	"github.com/ZacxDev/cairn/internal/transcript"
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
	journal plugins.Journal
	arc     interface{ Used() int64 }
	svc     *plugins.Service
}

func pluginRegistry(t *testing.T) *plugins.Registry {
	t.Helper()
	reg, err := plugins.ParseRegistry([]byte(`{"schema":1,"plugins":[
	 {"schema":1,"name":"summary","version":"0.1.0","description":"synthetic","toggle_key":"summary",
	  "capabilities":["pending:list","transcript:read","output:write:summary"],"output_types":["summary"],"calls_model":true},
	 {"schema":1,"name":"lister","version":"0.0.1","description":"lists only","toggle_key":"lister",
	  "capabilities":["pending:list"],"output_types":[],"calls_model":false}]}`))
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

func newPluginRig(t *testing.T) *pluginRig {
	t.Helper()
	arc := transcripttest.Open(t)
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
	svc := &plugins.Service{
		Registry:    pluginRegistry(t),
		Journal:     plugins.Journal{Path: filepath.Join(t.TempDir(), "toggles.jsonl")},
		Transcripts: transcript.NewStore(arc, writeStoreWithTrailer(t)),
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
	return &pluginRig{rig: g, journal: svc.Journal, arc: arc, svc: svc}
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
