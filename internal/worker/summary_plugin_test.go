package worker

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/netid"
	"github.com/ZacxDev/cairn/internal/plugins"
	"github.com/ZacxDev/cairn/internal/redact"
	"github.com/ZacxDev/cairn/internal/transcript"
	"github.com/ZacxDev/cairn/internal/transcript/archive"
	"github.com/ZacxDev/cairn/internal/transcript/transcripttest"
)

// TestTheSummaryPluginAgainstTheRealListener — example plugin A (`plugins/summary`, S9) BUILT and RUN
// against THIS package's worker listener over real HTTP: the registry is parsed from the plugin's own
// committed `manifest.json`, the toggle is a real journal row, the transcript a real archive, and the
// only fake is the model provider. It is the one place the plugin's second spelling of the contract
// (`plugins/internal/pluginapi`, a stdlib-only module that cannot import this one) meets the first: the
// plugin module's own fakes are written from ITS spelling and so cannot see a drift.
//
// Asserted on the fake provider's RECEIVED bodies (the plan's S9 test, over the real pod): run 1
// sends records 1–3, the runtime's compaction summary labelled runtime-generated; after two more
// records, run 2 sends only 4–5 and run 1's summary; each run stores one `summary` output whose
// watermark is the main stream's end.
//
// ⚠ IT BUILDS A BINARY FROM `plugins/`, which the nix derivations' filtered source does not carry
// (that tier builds and tests the plugin as `packages.cairn-summary`). It skips there — and REFUSES to
// skip where `CAIRN_PLUGIN_BUILD_REQUIRED` is set, which the CI `go` job sets, because a skip nobody
// counts is a pass.
func TestTheSummaryPluginAgainstTheRealListener(t *testing.T) {
	src, err := filepath.Abs(filepath.Join("..", "..", "plugins"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(src, "summary", "main.go")); err != nil {
		if os.Getenv("CAIRN_PLUGIN_BUILD_REQUIRED") != "" {
			t.Fatalf("the plugin source is not here (%v) and CAIRN_PLUGIN_BUILD_REQUIRED is set: this tier must run it", err)
		}
		t.Skipf("the plugin source is not in this tree (%v) — the nix tier builds it as packages.cairn-summary", err)
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		if goBin, err = exec.LookPath("go"); err != nil {
			t.Fatalf("no go toolchain to build the plugin with: %v", err)
		}
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "cairn-summary")
	build := exec.Command(goBin, "-C", src, "build", "-o", bin, "./summary")
	build.Env = append(os.Environ(), "GOWORK=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the plugin: %v\n%s", err, out)
	}

	manifest, err := os.ReadFile(filepath.Join(src, "summary", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg, err := plugins.ParseRegistry([]byte(`{"schema":1,"plugins":[` + string(manifest) + `]}`))
	if err != nil {
		t.Fatalf("the plugin's committed manifest is refused by the registry: %v", err)
	}

	red, err := redact.New(redact.SelfTestKey(47), nil)
	if err != nil {
		t.Fatal(err)
	}
	arc, err := archive.Open(archive.Config{Dir: t.TempDir(), Retention: transcripttest.Retention, Quota: 1 << 30,
		Recheck: red, Now: func() time.Time { return transcripttest.Clock }})
	if err != nil {
		t.Fatal(err)
	}
	owner := string(ownerA.Kind) + ":" + string(ownerA.ID)
	rec := func(v map[string]any) string { raw, _ := json.Marshal(v); return string(raw) }
	user := func(text string) string {
		return rec(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": text}})
	}
	sess := transcripttest.Session{Root: "s-alpha", Owner: owner}
	transcripttest.Append(t, arc, sess, "main", []string{
		user("alpha mark-1"),
		rec(map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant",
			"content": []any{map[string]any{"type": "text", "text": "ok mark-2"}}}}),
		rec(map[string]any{"type": "user", "isCompactSummary": true, "message": map[string]any{"role": "user",
			"content": "earlier work mark-3"}}),
	})
	// The client read ledger the session wrote (S11): a stream the pending query must neither count nor
	// hand the plugin to name, since a watermark may not name it.
	transcripttest.Append(t, arc, sess, "ledger", []string{`{"scope":"alpha-notes"}`})
	svc := &plugins.Service{
		Registry:    reg,
		Journal:     plugins.Journal{Path: filepath.Join(t.TempDir(), "toggles.jsonl")},
		Transcripts: transcript.NewStore(arc, writeStoreWithTrailer(t)),
		Known:       func(s string) bool { return s == "alpha-notes" || s == "beta-notes" },
		Now:         func() time.Time { return transcripttest.Clock.Add(time.Hour) },
	}
	if err := svc.Journal.Append(plugins.Record{Plugin: "summary", Level: plugins.LevelScope, Target: "alpha-notes",
		Value: plugins.ValueOn, By: plugins.By{Kind: "user", ID: "u-admin"}, At: "2000-01-02T03:04:05Z"}); err != nil {
		t.Fatal(err)
	}
	const token = "fixture-plugin-token-summary-real-listener"
	tokens := filepath.Join(t.TempDir(), "worker-tokens")
	writeRows(t, tokens, append(standardRows(), NewPluginTokenRow("summary", token))...)
	var logMu sync.Mutex
	var podLog []string
	w, _, err := New(Config{TokenFile: tokens, Owner: ownerA, OwnerHeld: func() bool { return true }, Archive: arc,
		Limiter: netid.NewRateLimiter(1000, time.Minute, time.Minute), Plugins: svc,
		Log: func(s string) { logMu.Lock(); podLog = append(podLog, s); logMu.Unlock() }})
	if err != nil {
		t.Fatal(err)
	}
	pod := httptest.NewServer(w)
	defer pod.Close()

	const key = "fixture-provider-key-not-a-real-key"
	var mu sync.Mutex
	var bodies []string
	provider := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Messages []struct{ Content string } `json:"messages"`
		}
		if r.URL.Path != "/v1/messages" || r.Header.Get("x-api-key") != key || json.Unmarshal(raw, &body) != nil || len(body.Messages) != 1 {
			http.Error(rw, "bad request", http.StatusBadRequest)
			return
		}
		mu.Lock()
		bodies = append(bodies, body.Messages[0].Content)
		n := len(bodies)
		mu.Unlock()
		rw.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(rw).Encode(map[string]any{"model": "fake-model-1", "stop_reason": "end_turn",
			"content": []any{map[string]string{"type": "text", "text": "summary " + string(rune('0'+n))}},
			"usage":   map[string]int{"input_tokens": 1, "output_tokens": 1}})
	}))
	defer provider.Close()
	for name, v := range map[string]string{"token": token, "key": key} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(v+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runPlugin := func() string {
		t.Helper()
		cmd := exec.Command(bin, "-worker-url", pod.URL, "-token-file", filepath.Join(dir, "token"),
			"-provider-url", provider.URL, "-key-file", filepath.Join(dir, "key"), "-model", "fake-model-1",
			"-spend-ledger", filepath.Join(dir, "spend.log"))
		var out, errb strings.Builder
		cmd.Stdout, cmd.Stderr = &out, &errb
		if err := cmd.Run(); err != nil {
			logMu.Lock()
			defer logMu.Unlock()
			t.Fatalf("the plugin run failed: %v\nstdout: %s\nstderr: %s\npod log: %v", err, out.String(), errb.String(), podLog)
		}
		return out.String()
	}
	marks := func(s string) []string {
		var out []string
		for _, m := range regexp.MustCompile(`mark-\d+`).FindAllString(s, -1) {
			if !slices.Contains(out, m) {
				out = append(out, m)
			}
		}
		return out
	}
	outputs := func() []plugins.Output {
		t.Helper()
		outs, err := svc.ReadOutputs(plugins.Entry{Manifest: plugins.Manifest{Name: "summary",
			Capabilities: []string{plugins.CapOutputRead(plugins.TypeSummary)}}}, "s-alpha", plugins.TypeSummary)
		if err != nil {
			t.Fatal(err)
		}
		return outs
	}

	if out := runPlugin(); !strings.Contains(out, "sessions=1 written=1 requests=1 failed=0") {
		t.Fatalf("run 1: %s", out)
	}
	if len(bodies) != 1 || !slices.Equal(marks(bodies[0]), []string{"mark-1", "mark-2", "mark-3"}) {
		t.Fatalf("run 1 sent %q, want one body carrying records 1–3", bodies)
	}
	if !strings.Contains(bodies[0], "#3 [RUNTIME-GENERATED compaction summary") {
		t.Errorf("the compaction summary is not labelled runtime-generated in the prompt:\n%s", bodies[0])
	}
	outs := outputs()
	if len(outs) != 1 || outs[0].InputWatermark["main"] != 3 || outs[0].Plugin != "summary" || outs[0].Model != "fake-model-1" {
		t.Fatalf("after run 1 the pod holds %+v", outs)
	}

	transcripttest.Append(t, arc, sess, "main", []string{user("more mark-4"), user("more mark-5")})
	if out := runPlugin(); !strings.Contains(out, "sessions=1 written=1 requests=1 failed=0") {
		t.Fatalf("run 2: %s", out)
	}
	if len(bodies) != 2 || !slices.Equal(marks(bodies[1]), []string{"mark-4", "mark-5"}) ||
		!strings.Contains(bodies[1], "PREVIOUS SUMMARY (of records 1–3):\n<<<SUMMARY\nsummary 1\nSUMMARY>>>") {
		t.Fatalf("run 2: the provider holds %q, want a second body carrying only records 4–5 and the prior summary", bodies)
	}
	if outs := outputs(); len(outs) != 2 || outs[1].InputWatermark["main"] != 5 {
		t.Fatalf("after run 2 the pod holds %+v", outs)
	}
	// Nothing new — and the ledger growing is nothing new either: the pod lists nothing and the plugin
	// asks nothing.
	transcripttest.Append(t, arc, sess, "ledger", []string{`{"scope":"alpha-notes"}`})
	if out := runPlugin(); !strings.Contains(out, "sessions=0 written=0 requests=0") || len(bodies) != 2 {
		t.Fatalf("run 3 with nothing new: %s (%d bodies)", out, len(bodies))
	}
}

// TestALedgerStreamIsNeitherListedNorCountedAsPending — a session's `ledger` stream (the client read
// ledger, S11) is no conversation, and an envelope naming it is refused ([plugins.ValidStream]). So
// the pending query neither LISTS it nor counts its growth: an output covering every conversation
// stream takes the session off the list, and the ledger growing afterwards does not put it back. Red
// at S5's head: the ledger was listed and counted, so the session stayed pending forever. In-process,
// so it runs in every tier (the real-listener test above skips where `plugins/` is absent).
func TestALedgerStreamIsNeitherListedNorCountedAsPending(t *testing.T) {
	p := newPluginRig(t)
	p.toggle(plugins.LevelScope, "alpha-notes", plugins.ValueOn)
	owner := string(ownerA.Kind) + ":" + string(ownerA.ID)
	ledger := func() {
		transcripttest.Append(t, p.rig.arc, transcripttest.Session{Root: "s-alpha", Owner: owner}, "ledger",
			[]string{`{"scope":"alpha-notes"}`})
	}
	ledger()
	rec := p.do(http.MethodGet, PendingPattern, summaryToken, "")
	var doc struct {
		Items []plugins.PendingItem `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil || len(doc.Items) != 1 {
		t.Fatalf("pending answered %d %q", rec.Code, rec.Body.String())
	}
	if _, listed := doc.Items[0].Streams["ledger"]; listed {
		t.Errorf("the pending item lists the ledger stream: %+v", doc.Items[0].Streams)
	}
	body := `{"plugin":"summary","plugin_version":"0.1.0","model":"fake-model","session":"s-alpha","input_watermark":{"main":3},"type":"summary","body":{"text":"synthetic"}}`
	if rec := p.do(http.MethodPost, OutputsPattern, summaryToken, body); rec.Code != http.StatusCreated {
		t.Fatalf("the write answered %d %q", rec.Code, rec.Body.String())
	}
	if got := p.pending(summaryToken); len(got) != 0 {
		t.Errorf("after an output covering main the pending list is %v, want empty — the ledger counted as growth", got)
	}
	ledger()
	if got := p.pending(summaryToken); len(got) != 0 {
		t.Errorf("the ledger growing put the session back on the pending list: %v", got)
	}
}
