package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/plugins/internal/hoststate"
)

// The guards audit rounds 0–1 of the S9 pull request found with no test — each mutant survived — and
// the two behaviours that round added: the per-session backoff and the unwritten-summary cache.

// TestAnUnfinishedAnswerIsNeverStoredAsASummary — a refusal, or an answer cut at the output bound, is
// a 200 with text in it; storing it would file a claim the model did not finish making. Both
// providers, each with its own stop vocabulary.
func TestAnUnfinishedAnswerIsNeverStoredAsASummary(t *testing.T) {
	for _, c := range []struct{ provider, stop string }{
		{"anthropic", "refusal"}, {"anthropic", "max_tokens"}, {"openai", "length"}, {"openai", "content_filter"},
	} {
		t.Run(c.provider+"/"+c.stop, func(t *testing.T) {
			w := newWorld(t, numbered(1, 3))
			w.prov.openai, w.prov.stop = c.provider == "openai", c.stop
			code, out, errs := w.run(t, "-provider", c.provider)
			if code != 1 || len(w.pod.outputs) != 0 || !strings.Contains(errs, `stopped with "`+c.stop+`"`) {
				t.Fatalf("exit %d, %d outputs stored\n%s%s", code, len(w.pod.outputs), out, errs)
			}
			if len(w.prov.received()) != 1 {
				t.Fatalf("POSITIVE CONTROL: the provider received %d requests, want 1 — the answer must have been read", len(w.prov.received()))
			}
		})
	}
}

// TestTheAnswerAndTheModelAreMadeToFitThePod — a provider's answer longer than the pod's 16 KiB and a
// `model` string longer than its 128 bytes (or carrying a control character) are clipped before the
// write; the fake pod refuses either, as the real one does, and a refusal there would throw away an
// answer already paid for.
func TestTheAnswerAndTheModelAreMadeToFitThePod(t *testing.T) {
	w := newWorld(t, numbered(1, 3))
	w.prov.text = strings.Repeat("é", 10_000) // 20,000 bytes
	w.prov.model = "fake\x07-" + strings.Repeat("m", 200)
	if code, out, errs := w.run(t); code != 0 || len(w.pod.outputs) != 1 {
		t.Fatalf("exit %d, %d outputs\n%s%s", code, len(w.pod.outputs), out, errs)
	}
	text := w.pod.summaries(t)[0]
	if len(text) != maxSummaryBytes || !strings.HasPrefix(w.prov.text, text) {
		t.Errorf("the stored summary is %d bytes, want the answer's first %d", len(text), maxSummaryBytes)
	}
	model := w.pod.latest()["model"].(string)
	if len(model) != maxModelBytes || !strings.HasPrefix(model, "fake-mmm") {
		t.Errorf("the stored model is %d bytes %q, want %d with the control character gone", len(model), model, maxModelBytes)
	}
}

// TestARefusedWriteIsKeptAndRePostedWithoutTheProvider — the pod refuses the write (507, a full
// quota): the run exits non-zero, the produced summary is kept in the 0600 state file, the session is
// skipped while it backs off, and the next attempt POSTs the SAME summary without a second provider
// request. Red before the state file existed: every run re-bought the whole chain.
func TestARefusedWriteIsKeptAndRePostedWithoutTheProvider(t *testing.T) {
	for _, status := range []int{http.StatusInsufficientStorage, http.StatusBadRequest} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			w := newWorld(t, numbered(1, 3))
			w.pod.writeStatus = status
			code, out, errs := w.run(t)
			if code != 1 || !strings.Contains(out, "failed=1") || len(w.prov.received()) != 1 {
				t.Fatalf("a refused write: exit %d (want 1), %d provider requests\n%s%s", code, len(w.prov.received()), out, errs)
			}
			state := filepath.Join(w.dir, "sessions.json")
			st, err := os.Stat(state)
			if err != nil || st.Mode().Perm() != 0o600 {
				t.Fatalf("the state file: %v, mode %v, want 0600", err, st.Mode())
			}
			if raw, _ := os.ReadFile(state); !strings.Contains(string(raw), `"summary": "summary 1"`) {
				t.Fatalf("the refused summary is not kept:\n%s", raw)
			}
			// Still refusing, a minute later: skipped, nothing asked of anyone.
			w.clock = w.clock.Add(time.Minute)
			if code, out, _ := w.run(t); code != 0 || !strings.Contains(out, "sessions=0 written=0 requests=0 failed=0 capped=false skipped=1") {
				t.Fatalf("inside the backoff: exit %d %s", code, out)
			}
			// The quota is freed and the backoff has passed: the SAME summary lands, no provider call.
			w.pod.writeStatus = 0
			w.clock = w.clock.Add(backoffBase)
			if code, out, errs := w.run(t); code != 0 || !strings.Contains(out, "written=1 requests=0") {
				t.Fatalf("the retry: exit %d\n%s%s", code, out, errs)
			}
			if n := len(w.prov.received()); n != 1 {
				t.Errorf("the provider received %d requests in all, want 1 — the retry must not re-buy the summary", n)
			}
			if s := w.pod.summaries(t); !slices.Equal(s, []string{"summary 1"}) {
				t.Errorf("the pod holds %q", s)
			}
			if raw, _ := os.ReadFile(state); strings.Contains(string(raw), "summary 1") {
				t.Errorf("the written summary is still cached on the host:\n%s", raw)
			}
		})
	}
}

// TestARecordPastThePendingItemIsNotSummarised — the session grew between the pending query and the
// read: the page carries records past the item's `seq_to`. They are the NEXT run's; this one neither
// sends them nor claims them in its watermark.
func TestARecordPastThePendingItemIsNotSummarised(t *testing.T) {
	w := newWorld(t, numbered(1, 5))
	w.pod.pendingLag = 2
	w.pod.pageMax = 10 // ONE page carrying all five records, so records 4–5 arrive beside 1–3
	if code, out, errs := w.run(t); code != 0 {
		t.Fatalf("exit %d\n%s%s", code, out, errs)
	}
	if got := w.prov.received(); len(got) != 1 || !slices.Equal(marks(got[0]), []int{1, 2, 3}) {
		t.Fatalf("the request carried %q, want exactly records 1–3", got)
	}
	if wm := w.pod.latest()["input_watermark"].(map[string]int64); wm["main"] != 3 {
		t.Errorf("the watermark is %v, want main=3", wm)
	}
}

// TestBothCredentialFilesAreModeChecked — the plugin TOKEN's file as well as the provider key's, and
// group-readable (0640) as well as world-readable.
func TestBothCredentialFilesAreModeChecked(t *testing.T) {
	for _, name := range []string{"token", "key"} {
		for _, mode := range []os.FileMode{0o640, 0o604} {
			w := newWorld(t, numbered(1, 2))
			if err := os.Chmod(filepath.Join(w.dir, name), mode); err != nil {
				t.Fatal(err)
			}
			code, _, errs := w.run(t)
			if code != 2 || !strings.Contains(errs, filepath.Join(w.dir, name)) || len(w.prov.received()) != 0 {
				t.Errorf("the %s file at %o: exit %d, %d requests sent: %s", name, mode, code, len(w.prov.received()), errs)
			}
		}
	}
}

// TestACleartextURLOffTheHostIsRefused — `http://` is taken for a loopback address only, for both
// URLs, before any credential is sent anywhere. The control is every other test here: they run over
// `http://127.0.0.1`.
func TestACleartextURLOffTheHostIsRefused(t *testing.T) {
	for _, flag := range []string{"-worker-url", "-provider-url"} {
		w := newWorld(t, numbered(1, 2))
		code, _, errs := w.run(t, flag, "http://pod.example.invalid")
		if code != 2 || !strings.Contains(errs, "plain http to a non-loopback host") || len(w.prov.received()) != 0 {
			t.Errorf("%s http://… off-host: exit %d, %s", flag, code, errs)
		}
	}
}

// TestThePlaintextExceptionIsTheWorkerURLsAloneAndIsSaid — `-allow-plaintext-host` admits a plain-http
// worker URL to exactly the named host and says so on stderr every run; it never applies to the
// provider URL, and a host that is not the one named is still refused.
func TestThePlaintextExceptionIsTheWorkerURLsAloneAndIsSaid(t *testing.T) {
	w := newWorld(t, numbered(1, 2))
	// Accepted: the run gets PAST validation (exit 1 — the `.invalid` host cannot be reached — not 2).
	code, _, errs := w.run(t, "-worker-url", "http://mesh-host.invalid:8080", "-allow-plaintext-host", "mesh-host.invalid", "-timeout", "5s")
	if code != 1 || !strings.Contains(errs, "-worker-url is PLAIN HTTP to mesh-host.invalid, allowed by -allow-plaintext-host") ||
		strings.Contains(errs, "non-loopback") {
		t.Errorf("the named host: exit %d (want 1: accepted, then unreachable)\n%s", code, errs)
	}
	if code, _, errs := w.run(t, "-worker-url", "http://other-host.invalid", "-allow-plaintext-host", "mesh-host.invalid"); code != 2 ||
		!strings.Contains(errs, "plain http to a non-loopback host") {
		t.Errorf("a host that was not named: exit %d %s", code, errs)
	}
	if code, _, errs := w.run(t, "-provider-url", "http://mesh-host.invalid", "-allow-plaintext-host", "mesh-host.invalid"); code != 2 ||
		!strings.Contains(errs, "the provider URL is plain http to a non-loopback host") || len(w.prov.received()) != 0 {
		t.Errorf("the provider URL under the worker's exception: exit %d %s", code, errs)
	}
	// And a loopback run that names no exception says nothing about plaintext.
	if _, _, errs := w.run(t); strings.Contains(errs, "PLAIN HTTP") {
		t.Errorf("a loopback run logged the plaintext line: %s", errs)
	}
}

// TestEffortIsSentOnlyWhenSet — `output_config.effort` is refused (400) by models that do not take it,
// so the default sends none.
func TestEffortIsSentOnlyWhenSet(t *testing.T) {
	w := newWorld(t, numbered(1, 2))
	if code, _, errs := w.run(t); code != 0 {
		t.Fatal(errs)
	}
	if strings.Contains(w.prov.raws[0], "output_config") {
		t.Errorf("the default request carries output_config:\n%s", w.prov.raws[0])
	}
	w2 := newWorld(t, numbered(1, 2))
	if code, _, errs := w2.run(t, "-effort", "low"); code != 0 {
		t.Fatal(errs)
	}
	if !strings.Contains(w2.prov.raws[0], `"output_config":{"effort":"low"}`) {
		t.Errorf("POSITIVE CONTROL: -effort low is not in the request:\n%s", w2.prov.raws[0])
	}
}

// ---------------------------------------------------------------- the backoff, in the audit's shape

// manyPod is a pod holding several sessions, oldest first, each one human record. It answers the four
// routes for all of them.
type manyPod struct {
	mu       sync.Mutex
	order    []string
	recs     map[string][]string
	outputs  map[string][]map[string]any
	requests int
}

func (p *manyPod) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	q := r.URL.Query()
	id := q.Get("session")
	switch {
	case r.URL.Path == "/plugin/v1/pending":
		items := []any{}
		limit := 0
		fmt.Sscan(q.Get("limit"), &limit)
		for _, s := range p.order {
			if len(items) == limit {
				break // oldest-first, at most `limit` — the pod's rule, and what makes asking for few a trap
			}
			n := int64(len(p.recs[s]))
			var prev any
			var wm int64
			if outs := p.outputs[s]; len(outs) > 0 {
				o := outs[len(outs)-1]
				wm = o["input_watermark"].(map[string]int64)["main"]
				prev = map[string]any{"id": o["id"], "watermark": o["input_watermark"]}
			}
			if n > wm {
				items = append(items, map[string]any{"session": s, "streams": map[string]any{"main": map[string]int64{"seq_to": n}}, "prev_output": prev})
			}
		}
		writeJSON(rw, 200, map[string]any{"items": items})
	case r.URL.Path == "/plugin/v1/transcript":
		recs := []any{}
		var from int
		fmt.Sscan(q.Get("from_seq"), &from)
		for i := max(from, 1); i <= len(p.recs[id]); i++ {
			recs = append(recs, map[string]any{"seq": i, "src": fmt.Sprint(i), "rec": json.RawMessage(p.recs[id][i-1])})
		}
		writeJSON(rw, 200, map[string]any{"records": recs, "next_seq": len(p.recs[id]) + 1, "done": true, "seq_to": len(p.recs[id])})
	case r.URL.Path == "/plugin/v1/outputs" && r.Method == http.MethodGet:
		outs := []any{}
		for _, o := range p.outputs[id] {
			outs = append(outs, o)
		}
		writeJSON(rw, 200, map[string]any{"outputs": outs})
	case r.URL.Path == "/plugin/v1/outputs":
		var env struct {
			Model, Session string
			InputWatermark map[string]int64 `json:"input_watermark"`
			Body           json.RawMessage
		}
		_ = json.NewDecoder(r.Body).Decode(&env)
		oid := fmt.Sprintf("o-%s-%d", env.Session, len(p.outputs[env.Session])+1)
		p.outputs[env.Session] = append(p.outputs[env.Session], map[string]any{"id": oid, "plugin": "summary", "model": env.Model,
			"session": env.Session, "input_watermark": env.InputWatermark, "type": "summary", "body": env.Body})
		writeJSON(rw, 201, map[string]string{"id": oid})
	}
}

// TestOneRefusedSessionDoesNotStarveTheRest — the audit's simulation: the OLDEST sessions are ones the
// provider refuses every time, `-limit` is smaller than their number, and the pod lists oldest-first.
// Without a memory of the refusals every run re-paid for the same sessions and the healthy ones behind
// them were never reached. With it: each refused session is paid for once, then skipped; the healthy
// ones are written by the third run; an interval later the refused ones are tried again and their
// backoff DOUBLES; and a refused session that GROWS is tried at once.
func TestOneRefusedSessionDoesNotStarveTheRest(t *testing.T) {
	pod := &manyPod{recs: map[string][]string{}, outputs: map[string][]map[string]any{}}
	for i := 1; i <= 10; i++ {
		id := fmt.Sprintf("s-sim-%02d", i)
		text := "healthy lorem"
		if i <= 8 {
			text = "poison lorem"
		}
		pod.order = append(pod.order, id)
		pod.recs[id] = []string{human(text)}
	}
	ps := httptest.NewServer(pod)
	defer ps.Close()
	var mu sync.Mutex
	asked := 0
	prov := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		asked++
		mu.Unlock()
		stop := "end_turn"
		if strings.Contains(string(raw), "poison") {
			stop = "refusal"
		}
		writeJSON(rw, 200, map[string]any{"model": "fake-model-1", "stop_reason": stop,
			"content": []any{map[string]string{"type": "text", "text": "a summary"}}})
	}))
	defer prov.Close()
	w := &world{pod: &fakePod{}, prov: &fakeProvider{}, podURL: ps.URL, provURL: prov.URL, dir: t.TempDir(),
		clock: time.Date(2000, 1, 2, 3, 4, 5, 0, time.UTC)}
	for name, v := range map[string]string{"token": fakeToken, "key": fakeKey} {
		if err := os.WriteFile(filepath.Join(w.dir, name), []byte(v+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	written := func() int {
		pod.mu.Lock()
		defer pod.mu.Unlock()
		return len(pod.outputs["s-sim-09"]) + len(pod.outputs["s-sim-10"])
	}
	step := func(wantAsked, wantWritten int, wantOut string) {
		t.Helper()
		_, out, errs := w.run(t, "-limit", "4", "-max-requests-per-day", "200")
		w.clock = w.clock.Add(time.Minute)
		if asked != wantAsked || written() != wantWritten || !strings.Contains(out, wantOut) {
			t.Fatalf("provider requests %d (want %d), healthy sessions written %d (want %d), want %q in:\n%s%s",
				asked, wantAsked, written(), wantWritten, wantOut, out, errs)
		}
	}
	step(4, 0, "sessions=4 written=0 requests=4 failed=4 capped=false skipped=0")
	step(8, 0, "sessions=4 written=0 requests=4 failed=4 capped=false skipped=4")
	step(10, 2, "sessions=2 written=2 requests=2 failed=0 capped=false skipped=8")
	step(10, 2, "sessions=0 written=0 requests=0 failed=0 capped=false skipped=8")
	// A refused session GROWS: it is tried at once (and refused again, on its new input).
	pod.mu.Lock()
	pod.recs["s-sim-03"] = append(pod.recs["s-sim-03"], human("more poison"))
	pod.mu.Unlock()
	step(11, 2, "sessions=1 written=0 requests=1 failed=1 capped=false skipped=7")
	// Past the first interval the others are tried again — four a run — and their backoff doubles.
	w.clock = w.clock.Add(backoffBase)
	step(15, 2, "sessions=4 written=0 requests=4 failed=4")
	w.clock = w.clock.Add(backoffBase) // 15 minutes on: inside the doubled (30-minute) backoff of those four
	_, out, _ := w.run(t, "-limit", "4", "-max-requests-per-day", "200")
	if strings.Contains(out, "skipped=0") || asked > 19 {
		t.Fatalf("the backoff did not double: %d requests, %s", asked, out)
	}
}

// TestAFailedStateWriteLeavesTheGoodFileAlone — the state file is replaced by a rename of a temporary
// file (`internal/hoststate`, shared with plugin B); when that file's write FAILS part-way, save
// reports it and the existing state is untouched.
// Red before: `if err := tmp.Chmod(…); err == nil { _, err = tmp.Write(…) }` shadowed `err`, the
// failure was dropped, and the truncated temporary file was renamed over the good one.
func TestAFailedStateWriteLeavesTheGoodFileAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	st, err := loadState(path)
	if err != nil {
		t.Fatal(err)
	}
	st.Sessions["s-sum-0001"] = &sessionState{Backoff: hoststate.Backoff{Failures: 2, Records: 3}, Cached: &cachedSummary{Summary: "summary 1"}}
	if err := st.save(); err != nil {
		t.Fatal(err)
	}
	good, _ := os.ReadFile(path)
	if !strings.Contains(string(good), "summary 1") {
		t.Fatalf("POSITIVE CONTROL: a working save did not write the state:\n%s", good)
	}
	real := hoststate.Write
	defer func() { hoststate.Write = real }()
	hoststate.Write = func(f *os.File, raw []byte) (int, error) {
		n, _ := f.Write(raw[:len(raw)/2])
		return n, fmt.Errorf("no space left on device")
	}
	st.Sessions["s-sum-0002"] = &sessionState{Backoff: hoststate.Backoff{Failures: 1}}
	if err := st.save(); err == nil || !strings.Contains(err.Error(), "no space left") {
		t.Errorf("a failed write: save answered %v, want the failure", err)
	}
	if now, _ := os.ReadFile(path); string(now) != string(good) {
		t.Errorf("a failed write replaced the good state file:\n%s", now)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".sessions-*")); len(left) != 0 {
		t.Errorf("the temporary file was left behind: %v", left)
	}
	if again, err := loadState(path); err != nil || again.Sessions["s-sum-0001"] == nil {
		t.Errorf("the state no longer loads: %v", err)
	}
}

// TestAResumedSessionKeepsItsCachedSummaryAndADeletedOneLosesIt — S9 audit F1. A refused write leaves
// a paid-for summary and a backoff on this host. The session is then RESUMED: the pod stops listing
// it (not idle) though it is still stored. Red before: the entry was dropped on that absence, and when
// the session paused again the whole chain was re-bought. Now the entry survives the unlisted run and
// the SAME summary is re-posted with no provider request. The other half: once the pod answers its
// one refusal for the session (deleted), the cached text is removed from this host.
func TestAResumedSessionKeepsItsCachedSummaryAndADeletedOneLosesIt(t *testing.T) {
	w := newWorld(t, numbered(1, 3))
	state := filepath.Join(w.dir, "sessions.json")
	w.pod.writeStatus = http.StatusInsufficientStorage
	if code, _, _ := w.run(t); code != 1 || len(w.prov.received()) != 1 {
		t.Fatalf("the refused write: exit %d, %d provider requests", code, len(w.prov.received()))
	}
	// Resumed: not listed, still stored.
	w.pod.unlisted = true
	w.clock = w.clock.Add(time.Hour)
	if code, out, errs := w.run(t); code != 0 || !strings.Contains(out, "sessions=0") {
		t.Fatalf("the unlisted run: exit %d\n%s%s", code, out, errs)
	}
	if raw, _ := os.ReadFile(state); !strings.Contains(string(raw), `"summary": "summary 1"`) {
		t.Fatalf("a session the pod merely did not LIST lost its cached summary:\n%s", raw)
	}
	// Paused again, quota freed: the same summary lands, and nobody is paid twice.
	w.pod.unlisted, w.pod.writeStatus = false, 0
	if code, out, errs := w.run(t); code != 0 || !strings.Contains(out, "written=1 requests=0") {
		t.Fatalf("the re-post: exit %d\n%s%s", code, out, errs)
	}
	if n := len(w.prov.received()); n != 1 {
		t.Fatalf("the provider received %d requests across the pause/resume cycle, want 1", n)
	}

	// The cleanup: a session DELETED on the pod keeps no derived text here.
	w2 := newWorld(t, numbered(1, 3))
	state2 := filepath.Join(w2.dir, "sessions.json")
	w2.pod.writeStatus = http.StatusInsufficientStorage
	w2.run(t)
	if raw, _ := os.ReadFile(state2); !strings.Contains(string(raw), "summary 1") {
		t.Fatalf("POSITIVE CONTROL: the refused summary is not cached:\n%s", raw)
	}
	w2.pod.gone = true
	if code, _, errs := w2.run(t); code != 0 || !strings.Contains(errs, "is gone from the pod") {
		t.Fatalf("the run after the deletion: exit %d %s", code, errs)
	}
	if raw, _ := os.ReadFile(state2); strings.Contains(string(raw), "summary 1") || strings.Contains(string(raw), session) {
		t.Fatalf("a deleted session's summary is still at rest on this host:\n%s", raw)
	}

	// …and so does one unlisted for longer than the TTL, even if the pod cannot be asked.
	w3 := newWorld(t, numbered(1, 3))
	w3.pod.writeStatus = http.StatusInsufficientStorage
	w3.run(t)
	w3.pod.unlisted = true
	w3.clock = w3.clock.Add(hoststate.StateTTL + time.Hour)
	w3.run(t)
	if raw, _ := os.ReadFile(filepath.Join(w3.dir, "sessions.json")); strings.Contains(string(raw), "summary 1") {
		t.Fatalf("an entry past its TTL is still kept:\n%s", raw)
	}
}

// TestACachedSummaryIsRePostedOnlyWhileItStillContinuesThePodsLastOutput — S9 audit F2, the two
// conditions on re-posting. (1) The pod's last output for the session CHANGED under the cache (another
// run of this plugin wrote one): the cached summary continued an output that is no longer the last,
// so it is NOT posted; the session is summarised again from the new prior. (2) The cache's watermark
// is PAST what the pod now holds (the session was deleted and re-uploaded shorter under the same id):
// posting it would claim records nobody can read, so it is NOT posted either.
func TestACachedSummaryIsRePostedOnlyWhileItStillContinuesThePodsLastOutput(t *testing.T) {
	t.Run("the last output changed", func(t *testing.T) {
		w := newWorld(t, numbered(1, 3))
		w.pod.writeStatus = http.StatusInsufficientStorage
		w.run(t)
		w.pod.mu.Lock()
		w.pod.outputs = append(w.pod.outputs, map[string]any{"id": "o-other", "plugin": "summary", "plugin_version": "0.1.0",
			"model": "fake-model-1", "session": session, "input_watermark": map[string]int64{"main": 1}, "type": "summary",
			"body": json.RawMessage(`{"text":"written elsewhere"}`), "produced_at": "2000-01-01T00:00:00Z"})
		w.pod.writeStatus = 0
		w.pod.mu.Unlock()
		w.clock = w.clock.Add(backoffBase)
		if code, out, errs := w.run(t); code != 0 || !strings.Contains(out, "written=1 requests=1") {
			t.Fatalf("exit %d\n%s%s", code, out, errs)
		}
		got := w.prov.received()
		if len(got) != 2 || !strings.Contains(got[1], "written elsewhere") {
			t.Fatalf("the stale cached summary was posted instead of continuing the pod's new last output:\n%v", got)
		}
		if s := w.pod.summaries(t); slices.Contains(s, "summary 1") {
			t.Errorf("the pod holds the stale cached summary: %q", s)
		}
	})
	t.Run("the watermark is past what is stored", func(t *testing.T) {
		w := newWorld(t, numbered(1, 5))
		w.pod.writeStatus = http.StatusInsufficientStorage
		w.run(t)
		w.pod.mu.Lock()
		w.pod.records = w.pod.records[:2]
		w.pod.writeStatus = 0
		w.pod.mu.Unlock()
		w.clock = w.clock.Add(48 * time.Hour) // past any backoff, whatever the size
		if code, out, errs := w.run(t); code != 0 || !strings.Contains(out, "written=1 requests=1") {
			t.Fatalf("exit %d\n%s%s", code, out, errs)
		}
		w.pod.mu.Lock()
		wm := w.pod.outputs[len(w.pod.outputs)-1]["input_watermark"].(map[string]int64)
		w.pod.mu.Unlock()
		if wm["main"] != 2 {
			t.Errorf("the stored watermark is main=%d, want 2 — what the pod holds", wm["main"])
		}
	})
}

// TestOnlyARefusalBacksOff — S9 audit F2/F3. A provider 500 and a pod 500 on the write are TRANSIENT:
// they say nothing about this session, so the very next run tries again (no SKIPPED), and a summary
// already paid for is kept and re-posted. The control is the 507 in the tests above, which backs off.
func TestOnlyARefusalBacksOff(t *testing.T) {
	w := newWorld(t, numbered(1, 3))
	w.prov.status = 500
	if code, out, errs := w.run(t); code != 1 || strings.Contains(errs, "backing off") {
		t.Fatalf("a provider 500: exit %d\n%s%s", code, out, errs)
	}
	w.prov.status = 0
	if code, out, errs := w.run(t); code != 0 || !strings.Contains(out, "sessions=1 written=1") || strings.Contains(out, "skipped=1") {
		t.Fatalf("the run right after a provider 500: exit %d\n%s%s", code, out, errs)
	}

	w2 := newWorld(t, numbered(1, 3))
	w2.pod.writeStatus = http.StatusInternalServerError
	if code, out, errs := w2.run(t); code != 1 || strings.Contains(errs, "backing off") {
		t.Fatalf("a pod 500 on the write: exit %d\n%s%s", code, out, errs)
	}
	w2.pod.writeStatus = 0
	if code, out, errs := w2.run(t); code != 0 || !strings.Contains(out, "written=1 requests=0") {
		t.Fatalf("the run right after a pod 500: exit %d — want the kept summary re-posted, no backoff, no provider\n%s%s", code, out, errs)
	}
}
