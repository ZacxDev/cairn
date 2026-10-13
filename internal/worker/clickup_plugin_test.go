package worker

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
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

// TestTheClickupPluginAgainstTheRealListener — example plugin B (`plugins/clickup`, S10) BUILT and RUN
// against THIS package's worker listener over real HTTP, beside plugin A's manifest in ONE registry:
// both manifests are the committed files, both toggles real journal rows, the transcript a real
// archive, and the only fakes are ClickUp and the model provider. It is where the plugin's second
// spelling of the contract — the three body shapes it writes, the stream vocabulary, `view=raw` — meets
// the first, which its own module's fakes (written from ITS spelling) cannot show.
//
// 🔴 IT IS ALSO THE CROSS-PLUGIN TEST OF THE ABSTRACTION (the plan's S10 row): a `summary` output is
// stored through plugin A's registry entry, and plugin B — a different token, a different manifest —
// reads it back through `output:read:summary` on the real pod and sends it to the provider.
//
// Asserted on what the POD holds afterwards: a `branch-name` edge and a `transcript-url` edge, each
// with the record it was found in; the model's pick stored as a `ticket-suggestion` and NOT as an
// edge; one `ticket-scan`, last; every one stamped `clickup` by the pod. A second run finds nothing
// pending.
//
// ⚠ It builds a binary from `plugins/`, so it skips in the nix derivations' filtered source and
// REFUSES to skip where `CAIRN_PLUGIN_BUILD_REQUIRED` is set — `TestTheSummaryPluginAgainstTheRealListener`'s
// rule.
func TestTheClickupPluginAgainstTheRealListener(t *testing.T) {
	src, err := filepath.Abs(filepath.Join("..", "..", "plugins"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(src, "clickup", "main.go")); err != nil {
		if os.Getenv("CAIRN_PLUGIN_BUILD_REQUIRED") != "" {
			t.Fatalf("the plugin source is not here (%v) and CAIRN_PLUGIN_BUILD_REQUIRED is set: this tier must run it", err)
		}
		t.Skipf("the plugin source is not in this tree (%v) — the nix tier builds it as packages.cairn-clickup", err)
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		if goBin, err = exec.LookPath("go"); err != nil {
			t.Fatalf("no go toolchain to build the plugin with: %v", err)
		}
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "cairn-clickup")
	build := exec.Command(goBin, "-C", src, "build", "-o", bin, "./clickup")
	build.Env = append(os.Environ(), "GOWORK=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the plugin: %v\n%s", err, out)
	}

	var manifests []string
	for _, name := range []string{"summary", "clickup"} {
		raw, err := os.ReadFile(filepath.Join(src, name, "manifest.json"))
		if err != nil {
			t.Fatal(err)
		}
		manifests = append(manifests, string(raw))
	}
	reg, err := plugins.ParseRegistry([]byte(`{"schema":1,"plugins":[` + strings.Join(manifests, ",") + `]}`))
	if err != nil {
		t.Fatalf("the plugins' committed manifests are refused by the registry: %v", err)
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
	sess := transcripttest.Session{Root: "s-alpha", Owner: owner}
	transcripttest.Append(t, arc, sess, "main", []string{
		rec(map[string]any{"type": "user", "gitBranch": "feature/clk0000a1-x",
			"message": map[string]any{"role": "user", "content": "please do the lorem export"}}),
		rec(map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant",
			"content": []any{map[string]any{"type": "text", "text": "done, see https://app.clickup.com/t/clk0000b2"}}}}),
	})
	transcripttest.Append(t, arc, sess, "ledger", []string{`{"scope":"alpha-notes"}`})
	svc := &plugins.Service{
		Registry:    reg,
		Journal:     plugins.Journal{Path: filepath.Join(t.TempDir(), "toggles.jsonl")},
		Transcripts: transcript.NewStore(arc, writeStoreWithTrailer(t)),
		Known:       func(s string) bool { return s == "alpha-notes" || s == "beta-notes" },
		Now:         func() time.Time { return transcripttest.Clock.Add(time.Hour) },
	}
	for _, name := range []string{"summary", "clickup"} {
		if err := svc.Journal.Append(plugins.Record{Plugin: name, Level: plugins.LevelScope, Target: "alpha-notes",
			Value: plugins.ValueOn, By: plugins.By{Kind: "user", ID: "u-admin"}, At: "2000-01-02T03:04:05Z"}); err != nil {
			t.Fatal(err)
		}
	}
	// Plugin A's output, stored through plugin A's own registry entry.
	summaryEntry, _ := reg.Lookup("summary")
	env, err := plugins.DecodeEnvelope([]byte(`{"plugin":"summary","plugin_version":"0.1.0","model":"fake-model-1","session":"s-alpha",` +
		`"input_watermark":{"main":2},"type":"summary","body":{"text":"The human asked for the lorem export. SUMMARY-MARK"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.WriteOutput(summaryEntry, env); err != nil {
		t.Fatalf("storing plugin A's summary: %v", err)
	}

	const token = "fixture-plugin-token-clickup-real-listener"
	tokens := filepath.Join(t.TempDir(), "worker-tokens")
	writeRows(t, tokens, append(standardRows(), NewPluginTokenRow("clickup", token))...)
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

	const cuToken = "fixture-clickup-token-not-a-real-token"
	var mu sync.Mutex
	var cuMethods []string
	clickup := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		mu.Lock()
		cuMethods = append(cuMethods, r.Method+" "+r.URL.Path)
		mu.Unlock()
		if r.Method != http.MethodGet || r.URL.Path != "/api/v2/list/list-0001/task" || r.Header.Get("Authorization") != cuToken {
			http.Error(rw, "unauthorized", http.StatusUnauthorized)
			return
		}
		rw.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(rw, `{"tasks":[{"id":"clk0000a1","custom_id":null,"name":"Synthetic ticket one"},`+
			`{"id":"clk0000b2","custom_id":null,"name":"Synthetic ticket two"},`+
			`{"id":"clk0000c3","custom_id":"ALPHA-12","name":"Synthetic ticket three"}],"last_page":true}`)
	}))
	defer clickup.Close()

	const key = "fixture-provider-key-not-a-real-key"
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
		mu.Unlock()
		rw.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(rw).Encode(map[string]any{"model": "fake-model-1", "stop_reason": "end_turn",
			"content": []any{map[string]string{"type": "text",
				"text": `[{"ticket_id":"clk0000c3","confidence":1.0,"rationale":"synthetic rationale"}]`}},
			"usage": map[string]int{"input_tokens": 1, "output_tokens": 1}})
	}))
	defer provider.Close()
	for name, v := range map[string]string{"token": token, "clickup-token": cuToken, "key": key} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(v+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runPlugin := func() string {
		t.Helper()
		cmd := exec.Command(bin, "-worker-url", pod.URL, "-token-file", filepath.Join(dir, "token"),
			"-clickup-url", clickup.URL, "-clickup-token-file", filepath.Join(dir, "clickup-token"), "-clickup-list", "list-0001",
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

	if out := runPlugin(); !strings.Contains(out, "sessions=1 edges=2 suggestions=1 requests=1 failed=0") {
		t.Fatalf("run 1: %s", out)
	}
	// Read back off the ARCHIVE, in write order — what the pod stored, not what the plugin says it sent.
	view, err := svc.Open("clickup", "s-alpha")
	if err != nil {
		t.Fatal(err)
	}
	var stored []plugins.Output
	if err := view.Outputs(func(line []byte) error {
		var o plugins.Output
		if err := json.Unmarshal(line, &o); err != nil {
			return err
		}
		stored = append(stored, o)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, o := range stored[1:] { // [0] is plugin A's summary
		if o.Plugin != "clickup" || o.PluginVersion != "0.1.0" || o.InputWatermark["main"] != 2 || len(o.InputWatermark) != 1 {
			t.Errorf("an output is stamped %s %s with watermark %v, want clickup 0.1.0 at main=2 and no ledger stream",
				o.Plugin, o.PluginVersion, o.InputWatermark)
		}
		got = append(got, o.Type+" "+string(o.Body))
	}
	want := []string{
		`ticket-edge {"system":"clickup","ticket_id":"clk0000b2","signal":"transcript-url","locator":{"stream":"main","seq":2}}`,
		`ticket-edge {"system":"clickup","ticket_id":"clk0000a1","signal":"branch-name","locator":{"stream":"main","seq":1}}`,
		`ticket-suggestion {"ticket_id":"clk0000c3","confidence":1,"rationale":"synthetic rationale"}`,
		`ticket-scan {"system":"clickup","edges":2,"suggestions":1,"model_asked":true}`,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("the pod stored, in order:\n  %s\nwant:\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
	// The cross-plugin read: plugin A's summary reached the provider through plugin B.
	if len(bodies) != 1 || !strings.Contains(bodies[0], "SUMMARY-MARK") || !strings.Contains(bodies[0], "clk0000c3\tSynthetic ticket three") {
		t.Fatalf("the provider received %q, want one request carrying plugin A's summary and the candidate tickets", bodies)
	}
	for _, m := range cuMethods {
		if m != "GET /api/v2/list/list-0001/task" {
			t.Errorf("ClickUp received %q — it is only ever read", m)
		}
	}
	// Nothing new: the scan took the session off the pending list, and ClickUp is not asked again.
	asked := len(cuMethods)
	if out := runPlugin(); !strings.Contains(out, "sessions=0 edges=0") || len(cuMethods) != asked {
		t.Fatalf("run 2 with nothing new: %s (ClickUp saw %d more requests)", out, len(cuMethods)-asked)
	}
}
