package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ZacxDev/cairn/plugins/internal/llm"
	"github.com/ZacxDev/cairn/plugins/internal/pluginapi"
	"github.com/ZacxDev/cairn/plugins/internal/spend"
	"github.com/ZacxDev/cairn/plugins/internal/transcripttext"
)

// 🔴 CONTINUOUS = INCREMENTAL, AND THE PROGRESS STATE IS THE POD'S WATERMARK (decision 13). A run asks
// the pod which sessions have grown past this plugin's last output, reads ONLY the records after that
// output's watermark, and sends the model those records plus its own previous summary; writing the new
// output is what advances the watermark. No PROGRESS is kept on this host — a run that dies mid-session
// leaves the watermark where it was and the next run repeats that session's work. What IS kept here
// (`state.go`) is what the pod cannot know: which sessions keep failing, and a summary whose write was
// refused.
//
// The main stream is what is READ: a subagent's work reaches it as the spawning call's result. The
// watermark still names EVERY stream the pending item listed, at the position it listed, once the main
// stream is read to its end — otherwise a subagent stream would count as grown from zero forever and
// the session would be pending on every run. (The pod lists only streams a watermark may name.)

// Name and Version are the plugin's own, matching `manifest.json` (the pod stamps the registry's).
const (
	Name    = "summary"
	Version = "0.1.0"
)

// OutputType is the one output type this plugin writes.
const OutputType = "summary"

// writeTimeout bounds the write that keeps a failed chain's progress.
const writeTimeout = 30 * time.Second

// The pod's bounds on an output (`internal/plugins` outputs.go): a summary's text, and `model`.
const (
	maxSummaryBytes = 16 << 10
	maxModelBytes   = 128
)

// pendingAsk is how many pending sessions one run ASKS for — the pod's maximum, not `-limit`: sessions
// in backoff still occupy the oldest-first list, so asking for only `-limit` would let that many
// failing sessions hide every healthy one behind them. ⚠ More than this many sessions in backoff at
// once still hides the rest.
const pendingAsk = 100

// Runner is one run's configuration.
type Runner struct {
	API      *pluginapi.Client
	Provider llm.Provider
	Spend    *spend.Ledger
	State    *stateFile
	Now      func() time.Time
	// Limit is how many pending sessions one run WORKS ON (sessions skipped in backoff do not count).
	Limit int
	// ChunkBytes bounds the rendered records one request carries.
	ChunkBytes int
	// MaxOutputTokens bounds one answer.
	MaxOutputTokens int
	// PageBytes is the `max_bytes` of one transcript page.
	PageBytes int
	Log       func(string)
}

// Stats is what a run did.
type Stats struct {
	Sessions, Written, Requests, Failed, Skipped int
	Capped                                       bool
}

// errEmptyAnswer is a provider answering 200 with no text.
var errEmptyAnswer = errors.New("the provider answered an empty summary")

// refusedWrite is the pod REFUSING the write — a full quota (507), a 400, the one 403 — as opposed to
// failing (a 5xx), redirecting, or not being reached, which are transient.
type refusedWrite struct{ err error }

func (e *refusedWrite) Error() string { return e.err.Error() }
func (e *refusedWrite) Unwrap() error { return e.err }

// deterministic reports whether a failure will repeat for the same records, so that retrying next
// run only re-pays for it: a provider's 4xx (not 429, which is about the account's rate), a refusal
// or truncated answer, an empty answer, and a write the pod refused. A provider's 5xx, an unreachable
// endpoint and the run's own deadline are not — they say nothing about THIS session.
func deterministic(err error) bool {
	var ps *llm.StatusError
	var stop *llm.StopError
	var rw *refusedWrite
	switch {
	case errors.As(err, &rw), errors.As(err, &stop), errors.Is(err, errEmptyAnswer):
		return true
	case errors.As(err, &ps):
		return ps.Status >= 400 && ps.Status < 500 && ps.Status != 429
	}
	return false
}

// records is a pending item's record count over every listed stream.
func records(it pluginapi.PendingItem) int64 {
	var n int64
	for _, pos := range it.Streams {
		n += pos.SeqTo
	}
	return n
}

// Run does one pass over the pending list.
func (r *Runner) Run(ctx context.Context) (Stats, error) {
	var st Stats
	items, err := r.API.Pending(ctx, pendingAsk)
	if err != nil {
		return st, fmt.Errorf("the pending query: %w", err)
	}
	// 🔴 A SESSION THE POD DID NOT LIST THIS RUN KEEPS ITS ENTRY ([hoststate.Backoff.Unlisted]): not
	// listed is what a session merely RESUMED looks like, and dropping its backoff and its cached
	// summary there re-paid a full quota once per pause/resume cycle. The entry goes only when the pod
	// answers its one refusal for the session — DELETED, or this plugin turned off for it — at which
	// point the cached summary is transcript-derived text with no reason left to exist; or when it is
	// past its TTL.
	listed := map[string]bool{}
	for _, it := range items {
		listed[it.Session] = true
	}
	changed := false
	for id, ss := range r.State.Sessions {
		if listed[id] {
			ss.Touch(r.Now())
			continue
		}
		seen := ss.Seen
		if ss.Unlisted(r.Now(), func() (bool, error) {
			_, err := r.API.Outputs(ctx, id, OutputType)
			if errors.Is(err, pluginapi.ErrForbidden) {
				return true, nil // the pod's one refusal: the positive signal
			}
			return false, err
		}) {
			delete(r.State.Sessions, id)
			changed = true
			r.Log(fmt.Sprintf("summary: session=%s is gone from the pod (or past its state TTL); what this host kept for it is dropped", id))
		}
		changed = changed || ss.Seen != seen
	}
	if changed {
		if err := r.State.save(); err != nil {
			return st, err
		}
	}
	for _, it := range items {
		if st.Capped || st.Sessions >= r.Limit {
			break
		}
		ss := r.State.Sessions[it.Session]
		total := records(it)
		if ss.backingOff(r.Now(), total) {
			st.Skipped++
			r.Log(fmt.Sprintf("summary: session=%s SKIPPED: %d failure(s) at this size, next attempt %s", it.Session, ss.Failures, ss.RetryAt))
			continue
		}
		st.Sessions++
		var cache *cachedSummary
		if ss != nil {
			cache = ss.Cached
		}
		res, err := r.session(ctx, it, cache)
		st.Requests += res.requests
		if res.written != "" {
			st.Written++
		}
		if res.capped {
			st.Capped = true
		}
		if err == nil && !res.capped {
			delete(r.State.Sessions, it.Session)
		} else if err != nil {
			st.Failed++
			if ss == nil {
				ss = &sessionState{}
				r.State.Sessions[it.Session] = ss
			}
			ss.Touch(r.Now())
			ss.Cached = res.unwritten
			note := ""
			if deterministic(err) && ctx.Err() == nil {
				note = fmt.Sprintf(" — backing off %s", ss.fail(r.Now(), total))
			} else if ss.Cached == nil && ss.Failures == 0 {
				delete(r.State.Sessions, it.Session)
			}
			r.Log(fmt.Sprintf("summary: session=%s FAILED after %d request(s): %v%s", it.Session, res.requests, err, note))
		}
		if serr := r.State.save(); serr != nil {
			return st, serr
		}
		if err == nil {
			r.Log(fmt.Sprintf("summary: session=%s records=%d..%d requests=%d written=%s capped=%t",
				it.Session, res.from, res.to, res.requests, orNone(res.written), res.capped))
		}
	}
	return st, nil
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

type sessionResult struct {
	from, to int64
	requests int
	written  string
	capped   bool
	// unwritten is a produced summary whose write did not land — what the state file keeps.
	unwritten *cachedSummary
}

func prevID(it pluginapi.PendingItem) string {
	if it.PrevOutput == nil {
		return ""
	}
	return it.PrevOutput.ID
}

// prior finds this plugin's previous summary text: the output the pending item names, read back
// through `output:read:summary`. "" and 0 when there is none — or when it cannot be found, which
// restarts the session from its first record rather than trusting a watermark with no summary behind it.
func (r *Runner) prior(ctx context.Context, it pluginapi.PendingItem) (text string, to int64, model string, err error) {
	if it.PrevOutput == nil {
		return "", 0, "", nil
	}
	outs, err := r.API.Outputs(ctx, it.Session, OutputType)
	if err != nil {
		return "", 0, "", fmt.Errorf("reading the previous summary: %w", err)
	}
	for _, o := range outs {
		// Matched on the id ALONE: the pending item names this token's own last output, and a
		// name check would lose the prior summary whenever the registry names the plugin otherwise.
		if o.ID != it.PrevOutput.ID {
			continue
		}
		var body struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(o.Body, &body) != nil || body.Text == "" {
			break
		}
		return body.Text, o.InputWatermark["main"], o.Model, nil
	}
	r.Log(fmt.Sprintf("summary: session=%s its previous output %s is not a readable summary; summarising from record 1",
		it.Session, it.PrevOutput.ID))
	return "", 0, "", nil
}

// read is every record of `main` in (after, to], rendered.
func (r *Runner) read(ctx context.Context, session string, after, to int64) ([]transcripttext.Entry, error) {
	var out []transcripttext.Entry
	from := after + 1
	for from <= to {
		p, err := r.API.Transcript(ctx, pluginapi.TranscriptQuery{Session: session, Stream: "main", FromSeq: from,
			View: "conversation", MaxBytes: r.PageBytes})
		if err != nil {
			return nil, err
		}
		if len(p.Records) == 0 {
			break
		}
		last := from - 1
		for _, rec := range p.Records {
			if rec.Seq < from || rec.Seq <= last {
				return nil, fmt.Errorf("the transcript page went backwards (record %d after %d)", rec.Seq, last)
			}
			last = rec.Seq
			if rec.Seq > to {
				break
			}
			if text := transcripttext.Record(rec); text != "" {
				out = append(out, transcripttext.Entry{Seq: rec.Seq, Text: text})
			}
		}
		from = last + 1
	}
	return out, nil
}

func (r *Runner) session(ctx context.Context, it pluginapi.PendingItem, cache *cachedSummary) (sessionResult, error) {
	var res sessionResult
	mainPos, ok := it.Streams["main"]
	if !ok {
		return res, errors.New("the pending item names no main stream")
	}
	// 🔴 A SUMMARY WHOSE WRITE WAS REFUSED IS RE-POSTED, NOT RE-BOUGHT. It is still good exactly when
	// the pod's last output for the session is the one it continued; then it is written as it was,
	// and whatever the session has grown since is the NEXT run's (the pod keeps listing it).
	if cache != nil && cache.PrevID == prevID(it) && cache.Watermark["main"] <= mainPos.SeqTo {
		res.from, res.to = 0, cache.Watermark["main"]
		return r.post(ctx, it, res, *cache)
	}
	// A run that makes no model call (nothing new to read) re-states the prior summary under the
	// model that WROTE it, never under a model that did not.
	summary, after, model, err := r.prior(ctx, it)
	if err != nil {
		return res, err
	}
	res.from, res.to = after+1, mainPos.SeqTo
	if after >= mainPos.SeqTo {
		// Grown only in another stream: nothing new to read in main; the write below advances the rest.
		after = mainPos.SeqTo
	}
	entries, err := r.read(ctx, it.Session, after, mainPos.SeqTo)
	if err != nil {
		return res, fmt.Errorf("reading the transcript: %w", err)
	}
	covered := after
	for _, chunk := range transcripttext.Chunks(entries, r.ChunkBytes) {
		if err := r.Spend.Take(); err != nil {
			if errors.Is(err, spend.ErrCapped) {
				res.capped = true
				break
			}
			return res, err
		}
		res.requests++
		ans, err := r.Provider.Complete(ctx, llm.Request{System: systemPrompt, User: userPrompt(summary, covered, chunk),
			MaxTokens: r.MaxOutputTokens})
		if err == nil && ans.Text == "" {
			err = errEmptyAnswer
		}
		if err != nil {
			if covered > after {
				// 🔴 KEEP WHAT WAS SUMMARISED, AND ON A CONTEXT THE FAILURE DID NOT CANCEL: when the run's
				// own deadline is what failed the request, writing on the same context would fail too,
				// and a backlog larger than one run would restart from the same record forever.
				wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), writeTimeout)
				defer cancel()
				res, werr := r.write(wctx, it, res, summary, model, covered, false)
				return res, errors.Join(err, werr)
			}
			return res, err
		}
		summary, model = clipSummary(ans.Text), ans.Model
		covered = chunk[len(chunk)-1].Seq
	}
	if res.capped && covered == after {
		return res, nil // nothing new summarised; the watermark stays where it was
	}
	complete := !res.capped
	if complete {
		covered = mainPos.SeqTo
	}
	return r.write(ctx, it, res, summary, model, covered, complete)
}

// write stores the summary as of `covered` in main. When the main stream was read to its end the
// watermark names every listed stream at its listed position; otherwise only main moves.
func (r *Runner) write(ctx context.Context, it pluginapi.PendingItem, res sessionResult, summary, model string, covered int64, complete bool) (sessionResult, error) {
	wm := map[string]int64{}
	if it.PrevOutput != nil {
		for s, seq := range it.PrevOutput.Watermark {
			if _, held := it.Streams[s]; held {
				wm[s] = seq
			}
		}
	}
	if complete {
		for s, pos := range it.Streams {
			wm[s] = pos.SeqTo
		}
	}
	wm["main"] = covered
	if summary == "" {
		// Read to the end and found nothing a summary reads (only bookkeeping, or only placeholders).
		summary = "No conversation content in the records read so far."
	}
	return r.post(ctx, it, res, cachedSummary{PrevID: prevID(it), Summary: summary, Model: cleanModel(model), Watermark: wm})
}

// post is the one write. A summary that does not land is handed back in `unwritten`.
func (r *Runner) post(ctx context.Context, it pluginapi.PendingItem, res sessionResult, c cachedSummary) (sessionResult, error) {
	keys := make([]string, 0, len(c.Watermark))
	for k := range c.Watermark {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	id, err := r.API.WriteOutput(ctx, pluginapi.Envelope{Plugin: Name, PluginVersion: Version, Model: c.Model,
		Session: it.Session, InputWatermark: c.Watermark, Type: OutputType, Body: map[string]string{"text": c.Summary}})
	if err != nil {
		res.unwritten = &c
		err = fmt.Errorf("writing the summary (watermark %s): %w", strings.Join(keys, ","), err)
		// 🔴 ONLY A REFUSAL IS A REFUSAL: 507 (the quota), 400 (the request) and the one 403. A 5xx or
		// a 3xx says nothing about this session — it is retried next run, the summary kept, no backoff.
		var status *pluginapi.StatusError
		if (errors.As(err, &status) && (status.Status == 400 || status.Status == 507)) || errors.Is(err, pluginapi.ErrForbidden) {
			return res, &refusedWrite{err}
		}
		return res, err
	}
	res.written = id
	return res, nil
}

// cleanModel makes a provider's `model` string one the pod accepts: no control character, at most
// [maxModelBytes] bytes on a character boundary, never empty. The provider chooses this string, and
// a pod refusal over it would throw away a summary that was already paid for.
func cleanModel(m string) string {
	m = strings.Map(func(c rune) rune {
		if unicode.IsControl(c) || c == utf8.RuneError {
			return -1
		}
		return c
	}, m)
	if len(m) > maxModelBytes {
		cut := maxModelBytes
		for cut > 0 && !utf8.RuneStart(m[cut]) {
			cut--
		}
		m = m[:cut]
	}
	if m == "" {
		return "none (no model call was needed)"
	}
	return m
}

// clipSummary keeps a model's answer within the pod's bound, on a character boundary.
func clipSummary(s string) string {
	if len(s) <= maxSummaryBytes {
		return s
	}
	cut := maxSummaryBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
