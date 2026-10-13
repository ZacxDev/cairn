package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// marks is the set of `mark-<i>` words a body carries, sorted.
func marks(body string) []int {
	var out []int
	for _, m := range regexp.MustCompile(`mark-(\d+)\b`).FindAllStringSubmatch(body, -1) {
		var n int
		fmt.Sscan(m[1], &n)
		if !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	slices.Sort(out)
	return out
}

func span(from, to int) []int {
	var out []int
	for i := from; i <= to; i++ {
		out = append(out, i)
	}
	return out
}

// TestTheFirstRunSummarisesOneToNAndTheSecondOnlyTheNewRecordsPlusThePriorSummary — the plan's S9
// test, asserted on the fake provider's RECEIVED bodies: run 1 sends records 1–N and no prior summary;
// after M new records, run 2 sends exactly records N+1–N+M and run 1's summary, and none of 1–N.
func TestTheFirstRunSummarisesOneToNAndTheSecondOnlyTheNewRecordsPlusThePriorSummary(t *testing.T) {
	const n, m = 7, 5
	w := newWorld(t, numbered(1, n))
	code, out, errs := w.run(t)
	if code != 0 || !strings.Contains(out, "sessions=1 written=1 requests=1 failed=0 capped=false") {
		t.Fatalf("run 1: exit %d\n%s%s", code, out, errs)
	}
	got := w.prov.received()
	if len(got) != 1 || !slices.Equal(marks(got[0]), span(1, n)) || !strings.Contains(got[0], "PREVIOUS SUMMARY: (none") {
		t.Fatalf("run 1 sent %d request(s); the first carried records %v, want exactly 1–%d and no prior summary", len(got), marks(got[0]), n)
	}
	w.pod.mu.Lock()
	first := w.pod.outputs[0]
	w.pod.mu.Unlock()
	if wm := first["input_watermark"].(map[string]int64); wm["main"] != n || first["model"] != "fake-model-1" {
		t.Errorf("run 1 wrote watermark %v model %v, want main=%d and the model the provider named", wm, first["model"], n)
	}

	// A run with nothing new asks nothing and writes nothing.
	if code, out, _ := w.run(t); code != 0 || !strings.Contains(out, "sessions=0 written=0 requests=0") {
		t.Fatalf("a run with nothing pending: exit %d %s", code, out)
	}

	w.pod.mu.Lock()
	w.pod.records = append(w.pod.records, numbered(n+1, m)...)
	w.pod.mu.Unlock()
	if code, out, errs := w.run(t); code != 0 || !strings.Contains(out, "written=1 requests=1") {
		t.Fatalf("run 2: exit %d\n%s%s", code, out, errs)
	}
	got = w.prov.received()
	if len(got) != 2 {
		t.Fatalf("the provider received %d requests over both runs, want 2", len(got))
	}
	if !slices.Equal(marks(got[1]), span(n+1, n+m)) {
		t.Errorf("run 2 carried records %v, want exactly %d–%d (only the new ones)", marks(got[1]), n+1, n+m)
	}
	if !strings.Contains(got[1], fmt.Sprintf("PREVIOUS SUMMARY (of records 1–%d):\n<<<SUMMARY\nsummary 1\nSUMMARY>>>", n)) {
		t.Errorf("run 2 does not carry run 1's summary as the prior summary:\n%s", got[1])
	}
	if s := w.pod.summaries(t); !slices.Equal(s, []string{"summary 1", "summary 2"}) {
		t.Errorf("the outputs are %q", s)
	}
	for _, v := range w.pod.views {
		if v != "conversation" {
			t.Errorf("a transcript read asked view=%q; the plugin reads view=conversation", v)
		}
	}
}

// TestACompactionSummaryIsLabelledRuntimeGenerated — the runtime's own summary of earlier turns is a
// `user` record; the model must be told it is the runtime's, never the human's.
func TestACompactionSummaryIsLabelledRuntimeGenerated(t *testing.T) {
	compaction := `{"type":"user","isCompactSummary":true,"isVisibleInTranscriptOnly":true,"message":{"role":"user","content":"earlier the agent did lorem mark-2"}}`
	boundary := `{"type":"system","subtype":"compact_boundary","content":"Conversation compacted"}`
	w := newWorld(t, []string{human("please mark-1"), boundary, compaction, assistant("ok mark-4")})
	if code, out, errs := w.run(t); code != 0 {
		t.Fatalf("exit %d\n%s%s", code, out, errs)
	}
	body := w.prov.received()[0]
	if !strings.Contains(body, "#3 [RUNTIME-GENERATED compaction summary — written by the agent runtime, not by the human] earlier the agent did lorem mark-2") {
		t.Errorf("the compaction summary is not labelled runtime-generated in the prompt:\n%s", body)
	}
	if !strings.Contains(body, "#2 [RUNTIME-GENERATED compact boundary] Conversation compacted") {
		t.Errorf("the compaction boundary is not labelled runtime-generated:\n%s", body)
	}
	// The control: the human's own message IS labelled the human's.
	if !strings.Contains(body, "#1 [human] please mark-1") || strings.Contains(body, "[human] earlier the agent") {
		t.Errorf("the labels do not tell the human from the runtime:\n%s", body)
	}
	if !strings.Contains(w.prov.system[0], "RUNTIME-GENERATED") {
		t.Error("the system prompt does not explain the runtime-generated label")
	}
}

// TestTheSpendCapAtZeroSendsNothing — the plugin's OWN cap (decision 12): at 0 the fake provider
// receives 0 requests and nothing is written; the positive control is the same world with the cap high,
// which sends ≥ 1. And the cap holds ACROSS runs: a cap of 1 lets the first run's request through and
// refuses the next run's, because the ledger is read, not the run's own count.
func TestTheSpendCapAtZeroSendsNothing(t *testing.T) {
	w := newWorld(t, numbered(1, 4))
	code, out, errs := w.run(t, "-max-requests-per-day", "0")
	if code != 0 || len(w.prov.received()) != 0 || len(w.pod.outputs) != 0 || !strings.Contains(out, "capped=true") {
		t.Fatalf("cap 0: exit %d, the provider received %d requests, %d outputs written\n%s%s",
			code, len(w.prov.received()), len(w.pod.outputs), out, errs)
	}
	if code, out, errs := w.run(t, "-max-requests-per-day", "50"); code != 0 || len(w.prov.received()) < 1 {
		t.Fatalf("POSITIVE CONTROL: cap 50 sent %d requests (exit %d)\n%s%s", len(w.prov.received()), code, out, errs)
	}

	w2 := newWorld(t, numbered(1, 4))
	if code, _, _ := w2.run(t, "-max-requests-per-day", "1"); code != 0 || len(w2.prov.received()) != 1 {
		t.Fatalf("cap 1, run 1: exit %d, %d requests, want 1", code, len(w2.prov.received()))
	}
	w2.pod.mu.Lock()
	w2.pod.records = append(w2.pod.records, numbered(5, 2)...)
	w2.pod.mu.Unlock()
	if code, out, _ := w2.run(t, "-max-requests-per-day", "1"); code != 0 || len(w2.prov.received()) != 1 || !strings.Contains(out, "capped=true") {
		t.Fatalf("cap 1, run 2: exit %d, %d requests in all, want still 1 (the ledger holds run 1's)\n%s", code, len(w2.prov.received()), out)
	}
	// A day later the window has moved and the request goes.
	w2.clock = w2.clock.Add(25 * time.Hour)
	if code, _, _ := w2.run(t, "-max-requests-per-day", "1"); code != 0 || len(w2.prov.received()) != 2 {
		t.Fatalf("cap 1, a day later: %d requests in all, want 2", len(w2.prov.received()))
	}
}

// TestASessionLargerThanOneChunkIsSummarisedInARollingChain — each request carries at most
// `-chunk-bytes` of records and the previous request's summary; the cap stopping the chain part-way
// writes what WAS summarised, with the watermark at the last record it covered.
func TestASessionLargerThanOneChunkIsSummarisedInARollingChain(t *testing.T) {
	var recs []string
	for i := 1; i <= 6; i++ {
		recs = append(recs, human(fmt.Sprintf("mark-%d %s", i, strings.Repeat("lorem ", 500))))
	}
	w := newWorld(t, recs)
	if code, out, errs := w.run(t, "-chunk-bytes", "6500", "-max-requests-per-day", "2"); code != 0 || !strings.Contains(out, "capped=true") {
		t.Fatalf("exit %d\n%s%s", code, out, errs)
	}
	got := w.prov.received()
	if len(got) != 2 || !slices.Equal(marks(got[0]), []int{1, 2}) || !slices.Equal(marks(got[1]), []int{3, 4}) {
		t.Fatalf("the chain sent %d requests carrying %v / %v, want [1 2] then [3 4]", len(got), marks(got[0]), marks(got[1]))
	}
	if !strings.Contains(got[1], "PREVIOUS SUMMARY (of records 1–2):\n<<<SUMMARY\nsummary 1") {
		t.Errorf("the second request does not carry the first's summary")
	}
	w.pod.mu.Lock()
	wm := w.pod.outputs[0]["input_watermark"].(map[string]int64)
	w.pod.mu.Unlock()
	if len(w.pod.outputs) != 1 || wm["main"] != 4 {
		t.Errorf("the capped chain wrote watermark %v, want main=4 — the last record it summarised", wm)
	}
}

// TestASubagentStreamIsCoveredByTheWatermark — once main is read to its end, the watermark names every
// stream the pending item listed; otherwise a subagent stream would keep the session pending forever.
// A later run where ONLY the subagent grew asks the provider nothing and re-states the prior summary
// under the model that wrote it.
func TestASubagentStreamIsCoveredByTheWatermark(t *testing.T) {
	w := newWorld(t, numbered(1, 3))
	w.pod.sub = 4
	if code, out, errs := w.run(t); code != 0 {
		t.Fatalf("exit %d\n%s%s", code, out, errs)
	}
	if wm := w.pod.latest()["input_watermark"].(map[string]int64); wm["main"] != 3 || wm["subagent:a1"] != 4 {
		t.Fatalf("watermark %v, want main=3 and subagent:a1=4", wm)
	}
	if code, out, _ := w.run(t); !strings.Contains(out, "sessions=0") || code != 0 {
		t.Fatalf("the session is still pending after its summary covered every stream: %s", out)
	}
	w.pod.sub = 6
	if code, out, errs := w.run(t); code != 0 || !strings.Contains(out, "written=1 requests=0") {
		t.Fatalf("a subagent-only growth: exit %d\n%s%s", code, out, errs)
	}
	last := w.pod.latest()
	if last["input_watermark"].(map[string]int64)["subagent:a1"] != 6 || last["model"] != "fake-model-1" {
		t.Errorf("the re-stated summary is %v", last)
	}
	if s := w.pod.summaries(t); s[len(s)-1] != "summary 1" {
		t.Errorf("the re-stated summary text is %q, want the prior one", s[len(s)-1])
	}
}

// TestWhatTheModelReadsIsLabelledByWhoWroteIt — tool calls and results are labelled and CLIPPED; a
// bookkeeping placeholder renders as nothing; a prompt the human typed mid-turn is the human's.
func TestWhatTheModelReadsIsLabelledByWhoWroteIt(t *testing.T) {
	long := strings.Repeat("x", toolClip+100)
	queued := `{"type":"attachment","attachment":{"type":"queued_command","prompt":"also mark-5","commandMode":"prompt","origin":{"kind":"human"}}}`
	hook := `{"type":"attachment","attachment":{"type":"hook_success","content":"hook mark-6"}}`
	w := newWorld(t, []string{human("go mark-1"), toolCall("toolu_1", "ls mark-2"), toolResult("toolu_1", long+" mark-3"),
		assistant("ok mark-4"), queued, hook})
	if code, out, errs := w.run(t); code != 0 {
		t.Fatalf("exit %d\n%s%s", code, out, errs)
	}
	body := w.prov.received()[0]
	for _, want := range []string{
		`#2 [tool call Bash] {"command":"ls mark-2"}`,
		"#3 [tool result] " + long[:toolClip] + " […",
		"#5 [human, typed while the agent worked] also mark-5",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the prompt lacks %q", want)
		}
	}
	if strings.Contains(body, "mark-3") || strings.Contains(body, "mark-6") {
		t.Errorf("the prompt carries the clipped tail of a tool result or a bookkeeping record:\n%s", body)
	}
}

// TestTheKeyIsReadFromAFileAndNeverPrinted — a credential file other users can read is refused before
// anything is sent; a provider whose error body ECHOES the key has it scrubbed from what is printed.
func TestTheKeyIsReadFromAFileAndNeverPrinted(t *testing.T) {
	w := newWorld(t, numbered(1, 2))
	if err := os.Chmod(filepath.Join(w.dir, "key"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, errs := w.run(t); code != 2 || !strings.Contains(errs, "readable or writable by a group or other users") {
		t.Fatalf("a 0644 key file: exit %d %s", code, errs)
	}
	if err := os.Chmod(filepath.Join(w.dir, "key"), 0o600); err != nil {
		t.Fatal(err)
	}
	w.prov.status, w.prov.echo = 401, fakeKey
	code, out, errs := w.run(t)
	if code != 1 || !strings.Contains(errs, "answered 401") {
		t.Fatalf("a refusing provider: exit %d\n%s%s", code, out, errs)
	}
	if strings.Contains(out+errs, fakeKey) || strings.Contains(out+errs, fakeToken) {
		t.Errorf("a credential reached the output:\n%s%s", out, errs)
	}
	if !strings.Contains(errs, "bad key [key]") {
		t.Errorf("POSITIVE CONTROL: the echoed body is not in the output at all, so its scrubbing proves nothing:\n%s", errs)
	}
	if len(w.pod.outputs) != 0 {
		t.Errorf("a failed request wrote %d outputs", len(w.pod.outputs))
	}
}

// TestTheOpenAIProviderIsTheSameRun — the provider is configurable; the OpenAI-compatible spelling
// carries the same prompt and its answer is the summary.
func TestTheOpenAIProviderIsTheSameRun(t *testing.T) {
	w := newWorld(t, numbered(1, 3))
	w.prov.openai = true
	if code, out, errs := w.run(t, "-provider", "openai"); code != 0 {
		t.Fatalf("exit %d\n%s%s", code, out, errs)
	}
	if got := w.prov.received(); len(got) != 1 || !slices.Equal(marks(got[0]), []int{1, 2, 3}) || !strings.Contains(w.prov.system[0], "RUNTIME-GENERATED") {
		t.Fatalf("the openai request carried %v", got)
	}
	if last := w.pod.latest(); last["model"] != "fake-chat-1" {
		t.Errorf("the output names model %v, want the one the provider answered", last["model"])
	}
}

// TestARefusedTranscriptIsAFailureNotASummary — the toggle turned OFF between the pending query and
// the read: the 403 fails that session, nothing is sent and nothing is written.
func TestARefusedTranscriptIsAFailureNotASummary(t *testing.T) {
	w := newWorld(t, numbered(1, 3))
	w.pod.refuseTr = true
	code, out, errs := w.run(t)
	if code != 1 || !strings.Contains(errs, "403") || len(w.prov.received()) != 0 || len(w.pod.outputs) != 0 {
		t.Fatalf("exit %d, %d requests, %d outputs\n%s%s", code, len(w.prov.received()), len(w.pod.outputs), out, errs)
	}
}
