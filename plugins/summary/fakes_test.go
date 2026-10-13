package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode"
)

// 🔴 TWO IN-PROCESS FAKES, BOTH SYNTHETIC, BOTH WRITTEN FROM THE CONTRACTS THIS MODULE SPELLS. The fake
// POD answers the plugin API's four routes in S5's shapes (the transcript route in the `view=` page
// shape of `internal/transcript`'s `Page.JSON`); the fake PROVIDER answers the Anthropic Messages API
// (or the OpenAI chat-completions API) and RECORDS every body it receives — the S9 assertions are on
// those bodies. Because both are written from the same spelling as the client, they prove the PLUGIN's
// behaviour, not the contract's: the real worker listener is what `internal/worker`'s
// `TestTheSummaryPluginAgainstTheRealListener` runs this binary against.

const (
	fakeToken = "plugin-token-synthetic-0001"
	fakeKey   = "provider-key-synthetic-0001"
	session   = "s-sum-0001"
)

// fakePod is one stored session's main stream (and, optionally, one subagent stream) and its outputs.
type fakePod struct {
	mu       sync.Mutex
	t        *testing.T
	records  []string // main, seq = index+1
	sub      int64    // a subagent stream's seq_to; 0 is no such stream
	outputs  []map[string]any
	pageMax  int // records per page, to exercise paging
	views    []string
	refuseTr bool // answer 403 on the transcript route (the toggle turned OFF)
	plugin   string // the registered name the pod stamps on outputs ("" is "summary")
	srcs     []string // per-record src (opencode `prt:…`); nil is the record's index
	// writeStatus, when non-zero, is what the POST answers instead of storing (507 a full quota, 400).
	writeStatus int
	// pendingLag makes the pending item name a main `seq_to` this many records SHORT of what the
	// transcript route then serves — a session that grew between the two requests.
	pendingLag int
	// unlisted makes the pending query list nothing though the session is still stored (a session
	// that was resumed and is not idle); gone makes the pod answer its one 403 for it as well.
	unlisted, gone bool
}

// streamWire is the pod's `ValidStream` vocabulary (`internal/plugins`): the streams an
// `input_watermark` may name. The fake REFUSES any other, as the pod does.
var streamWire = regexp.MustCompile(`^(main|subagent:[A-Za-z0-9][A-Za-z0-9_.-]{0,63}|child:[A-Za-z0-9][A-Za-z0-9_.-]{0,63})$`)

func (p *fakePod) streams() map[string]int64 {
	s := map[string]int64{"main": int64(len(p.records) - p.pendingLag)}
	if p.sub > 0 {
		s["subagent:a1"] = p.sub
	}
	return s
}

func (p *fakePod) latest() map[string]any {
	if len(p.outputs) == 0 {
		return nil
	}
	return p.outputs[len(p.outputs)-1]
}

func writeJSON(rw http.ResponseWriter, status int, v any) {
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(status)
	_ = json.NewEncoder(rw).Encode(v)
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
		if p.unlisted || p.gone {
			writeJSON(rw, 200, map[string]any{"items": []any{}})
			return
		}
		items := []any{}
		streams := p.streams()
		var wm map[string]int64
		var prev any
		if o := p.latest(); o != nil {
			wm = o["input_watermark"].(map[string]int64)
			prev = map[string]any{"id": o["id"], "watermark": wm}
		}
		grown := false
		pos := map[string]any{}
		for s, seq := range streams {
			pos[s] = map[string]int64{"seq_to": seq}
			if seq > wm[s] {
				grown = true
			}
		}
		if grown {
			items = append(items, map[string]any{"session": session, "streams": pos, "prev_output": prev})
		}
		writeJSON(rw, 200, map[string]any{"items": items})
	case r.URL.Path == "/plugin/v1/transcript":
		if p.refuseTr || q.Get("session") != session {
			writeJSON(rw, 403, map[string]string{"refused": "plugin-forbidden"})
			return
		}
		p.views = append(p.views, q.Get("view"))
		from, _ := strconv.Atoi(q.Get("from_seq"))
		recs := []any{}
		next := from
		for i := max(from, 1); i <= len(p.records) && len(recs) < p.pageMax; i++ {
			rec := p.records[i-1]
			var obj map[string]any
			_ = json.Unmarshal([]byte(rec), &obj)
			if obj["type"] == "attachment" && !strings.Contains(rec, `"commandMode":"prompt"`) {
				// The conversation view's placeholder for a bookkeeping record.
				recs = append(recs, map[string]any{"seq": i, "src": strconv.Itoa(i),
					"omitted": []any{map[string]any{"class": "bookkeeping", "path": "", "bytes": len(rec), "label": "attachment"}}})
			} else {
				src := strconv.Itoa(i)
				if p.srcs != nil {
					src = p.srcs[i-1]
				}
				recs = append(recs, map[string]any{"seq": i, "src": src, "rec": json.RawMessage(rec)})
			}
			next = i + 1
		}
		writeJSON(rw, 200, map[string]any{"session": session, "stream": "main", "view": q.Get("view"), "records": recs,
			"next_seq": next, "done": next > len(p.records), "seq_to": len(p.records)})
	case r.URL.Path == "/plugin/v1/outputs" && r.Method == http.MethodGet:
		if p.gone {
			writeJSON(rw, 403, map[string]string{"refused": "plugin-forbidden"})
			return
		}
		outs := []any{}
		for _, o := range p.outputs {
			outs = append(outs, o)
		}
		writeJSON(rw, 200, map[string]any{"outputs": outs})
	case r.URL.Path == "/plugin/v1/outputs" && r.Method == http.MethodPost:
		var env struct {
			Model          string           `json:"model"`
			Session        string           `json:"session"`
			InputWatermark map[string]int64 `json:"input_watermark"`
			Type           string           `json:"type"`
			Body           json.RawMessage  `json:"body"`
		}
		dec := json.NewDecoder(r.Body)
		if err := dec.Decode(&env); err != nil || env.Type != "summary" || env.Session != session {
			http.Error(rw, "bad envelope", 400)
			return
		}
		if p.writeStatus != 0 {
			http.Error(rw, `{"refused":"quota","quota_bytes":1}`, p.writeStatus)
			return
		}
		// The pod's own bounds (`internal/plugins` outputs.go): model 1–128 bytes with no control
		// character, a summary's text 1–16 KiB.
		var body struct{ Text string }
		_ = json.Unmarshal(env.Body, &body)
		if env.Model == "" || len(env.Model) > 128 || strings.ContainsFunc(env.Model, unicode.IsControl) ||
			body.Text == "" || len(body.Text) > 16<<10 {
			http.Error(rw, "the envelope is outside the pod's bounds", 400)
			return
		}
		for s, seq := range env.InputWatermark {
			if !streamWire.MatchString(s) {
				http.Error(rw, "input_watermark holds a bad stream or a negative seq", 400)
				return
			}
			if top, ok := p.streams()[s]; !ok || seq > top {
				http.Error(rw, "watermark past what is stored", 400)
				return
			}
		}
		id := fmt.Sprintf("o-%04d", len(p.outputs)+1)
		name := p.plugin
		if name == "" {
			name = "summary"
		}
		p.outputs = append(p.outputs, map[string]any{"id": id, "plugin": name, "plugin_version": "0.1.0",
			"model": env.Model, "session": session, "input_watermark": env.InputWatermark, "type": "summary",
			"body": env.Body, "produced_at": "2000-01-01T00:00:00Z"})
		writeJSON(rw, 201, map[string]string{"id": id})
	default:
		http.NotFound(rw, r)
	}
}

func (p *fakePod) summaries(t *testing.T) []string {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for _, o := range p.outputs {
		var b struct{ Text string }
		if err := json.Unmarshal(o["body"].(json.RawMessage), &b); err != nil {
			t.Fatal(err)
		}
		out = append(out, b.Text)
	}
	return out
}

// fakeProvider answers the Anthropic Messages API and records each request's user message.
type fakeProvider struct {
	mu     sync.Mutex
	users  []string
	system []string
	status int    // non-zero: answer this status instead
	echo   string // with status: a body echoing this (the key, to prove it is scrubbed)
	openai bool
	// emptyOn and hangOn make request number N (1-based) answer an empty text, or hang for hang.
	emptyOn, hangOn int
	hang            time.Duration
	sizes           []int // each request body's size
	raws            []string
	// stop, model and text override the answer's stop/finish reason, model and text ("" keeps the default).
	stop, model, text string
}

func (f *fakeProvider) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	raw, _ := io.ReadAll(r.Body)
	f.sizes = append(f.sizes, len(raw))
	f.raws = append(f.raws, string(raw))
	if f.hangOn == len(f.users)+1 {
		f.mu.Unlock()
		select {
		case <-time.After(f.hang):
		case <-r.Context().Done():
		}
		f.mu.Lock()
	}
	if f.status != 0 {
		http.Error(rw, `{"error":{"message":"bad key `+f.echo+`"}}`, f.status)
		return
	}
	var body struct {
		System   string `json:"system"`
		Messages []struct {
			Role, Content string
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &body); err != nil || len(body.Messages) == 0 {
		http.Error(rw, "bad request", 400)
		return
	}
	if f.openai {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer "+fakeKey {
			http.Error(rw, "unauthorized", 401)
			return
		}
		f.system = append(f.system, body.Messages[0].Content)
		f.users = append(f.users, body.Messages[len(body.Messages)-1].Content)
		writeJSON(rw, 200, map[string]any{"model": "fake-chat-1", "choices": []any{map[string]any{
			"message":       map[string]string{"role": "assistant", "content": fmt.Sprintf("summary %d", len(f.users))},
			"finish_reason": orStr(f.stop, "stop")}}, "usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 5}})
		return
	}
	if r.URL.Path != "/v1/messages" || r.Header.Get("x-api-key") != fakeKey || r.Header.Get("anthropic-version") == "" {
		http.Error(rw, "unauthorized", 401)
		return
	}
	f.system = append(f.system, body.System)
	f.users = append(f.users, body.Messages[0].Content)
	writeJSON(rw, 200, map[string]any{"model": orStr(f.model, "fake-model-1"), "stop_reason": orStr(f.stop, "end_turn"),
		"content": []any{map[string]string{"type": "thinking", "thinking": ""},
			map[string]string{"type": "text", "text": f.answer()}},
		"usage": map[string]int{"input_tokens": 10, "output_tokens": 5}})
}

func orStr(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func (f *fakeProvider) answer() string {
	if len(f.users) == f.emptyOn {
		return ""
	}
	if f.text != "" {
		return f.text
	}
	return fmt.Sprintf("summary %d", len(f.users))
}

func (f *fakeProvider) received() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.users...)
}

// world is one fake pod, one fake provider, and the credential files a run reads.
type world struct {
	pod      *fakePod
	prov     *fakeProvider
	podURL   string
	provURL  string
	dir      string
	clock    time.Time
	extraArg []string
}

func newWorld(t *testing.T, records []string) *world {
	t.Helper()
	w := &world{pod: &fakePod{t: t, records: records, pageMax: 3}, prov: &fakeProvider{}, dir: t.TempDir(),
		clock: time.Date(2000, 1, 2, 3, 4, 5, 0, time.UTC)}
	ps := httptest.NewServer(w.pod)
	t.Cleanup(ps.Close)
	vs := httptest.NewServer(w.prov)
	t.Cleanup(vs.Close)
	w.podURL, w.provURL = ps.URL, vs.URL
	for name, v := range map[string]string{"token": fakeToken, "key": fakeKey} {
		if err := os.WriteFile(filepath.Join(w.dir, name), []byte(v+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return w
}

// run runs the plugin once with its real flag parsing, and answers the exit code and both streams.
func (w *world) run(t *testing.T, extra ...string) (int, string, string) {
	t.Helper()
	args := []string{"-worker-url", w.podURL, "-token-file", filepath.Join(w.dir, "token"),
		"-provider-url", w.provURL, "-key-file", filepath.Join(w.dir, "key"), "-model", "fake-model-1",
		"-spend-ledger", filepath.Join(w.dir, "spend.log")}
	args = append(append(args, w.extraArg...), extra...)
	var out, errb strings.Builder
	code := run(args, &out, &errb, func() time.Time { return w.clock })
	return code, out.String(), errb.String()
}

// Synthetic records.
func human(text string) string {
	return `{"type":"user","message":{"role":"user","content":` + q(text) + `}}`
}
func assistant(text string) string {
	return `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":` + q(text) + `}]}}`
}
func toolCall(id, cmd string) string {
	return `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"` + id + `","name":"Bash","input":{"command":` + q(cmd) + `}}]}}`
}
func toolResult(id, out string) string {
	return `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"` + id + `","content":` + q(out) + `}]},"toolUseResult":{"stdout":` + q(out) + `}}`
}
func q(s string) string { raw, _ := json.Marshal(s); return string(raw) }

// numbered is n records, each carrying its own marker word `mark-<i>`, so a body can be searched for
// exactly which records it carried.
func numbered(from, n int) []string {
	var out []string
	for i := from; i < from+n; i++ {
		if i%2 == 1 {
			out = append(out, human(fmt.Sprintf("please do lorem mark-%d", i)))
		} else {
			out = append(out, assistant(fmt.Sprintf("done ipsum mark-%d", i)))
		}
	}
	return out
}
