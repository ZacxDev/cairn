package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ZacxDev/cairn/plugins/internal/llm"
	"github.com/ZacxDev/cairn/plugins/internal/pluginapi"
	"github.com/ZacxDev/cairn/plugins/internal/spend"
	"github.com/ZacxDev/cairn/plugins/internal/transcripttext"
)

// 🔴 CONTINUOUS = INCREMENTAL, AND THE ONLY STATE IS THE POD'S WATERMARK (decision 13). A run asks the
// pod which sessions have grown past this plugin's last output, reads ONLY the records after that
// output's watermark, and sends the model those records plus its own previous summary; writing the new
// output is what advances the watermark. Nothing about progress is kept on this host — a run that
// dies mid-session leaves the watermark where it was and the next run repeats that session's work.
//
// The main stream is what is READ: a subagent's work reaches it as the spawning call's result. The
// watermark still names EVERY stream the pending item listed, at the position it listed, once the main
// stream is read to its end — otherwise a subagent stream would count as grown from zero forever and
// the session would be pending on every run.

// Name and Version are the plugin's own, matching `manifest.json` (the pod stamps the registry's).
const (
	Name    = "summary"
	Version = "0.1.0"
)

// OutputType is the one output type this plugin writes.
const OutputType = "summary"

// writeTimeout bounds the write that keeps a failed chain's progress.
const writeTimeout = 30 * time.Second

// maxSummaryBytes is the pod's bound on a summary's text (`internal/plugins` outputs.go).
const maxSummaryBytes = 16 << 10

// Runner is one run's configuration.
type Runner struct {
	API      *pluginapi.Client
	Provider llm.Provider
	Spend    *spend.Ledger
	// Limit is how many pending sessions one run takes.
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
	Sessions, Written, Requests, Failed int
	Capped                              bool
}

// Run does one pass over the pending list.
func (r *Runner) Run(ctx context.Context) (Stats, error) {
	var st Stats
	items, err := r.API.Pending(ctx, r.Limit)
	if err != nil {
		return st, fmt.Errorf("the pending query: %w", err)
	}
	for _, it := range items {
		if st.Capped {
			break
		}
		st.Sessions++
		res, err := r.session(ctx, it)
		st.Requests += res.requests
		if res.written != "" {
			st.Written++
		}
		if res.capped {
			st.Capped = true
		}
		switch {
		case err != nil:
			st.Failed++
			r.Log(fmt.Sprintf("summary: session=%s FAILED after %d request(s): %v", it.Session, res.requests, err))
		default:
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

func (r *Runner) session(ctx context.Context, it pluginapi.PendingItem) (sessionResult, error) {
	var res sessionResult
	mainPos, ok := it.Streams["main"]
	if !ok {
		return res, errors.New("the pending item names no main stream")
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
			err = errors.New("the provider answered an empty summary")
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
			if _, held := it.Streams[s]; held && pluginapi.ValidWatermarkStream(s) {
				wm[s] = seq
			}
		}
	}
	if complete {
		for s, pos := range it.Streams {
			// 🔴 ONLY A STREAM THE POD ACCEPTS IN A WATERMARK: a session's `ledger` stream (the client
			// read ledger) is listed by the pending query but is no conversation, and naming it made
			// the pod refuse the WHOLE write — so the session was re-summarised, and re-paid for, on
			// every run.
			if pluginapi.ValidWatermarkStream(s) {
				wm[s] = pos.SeqTo
			}
		}
	}
	wm["main"] = covered
	if summary == "" {
		// Read to the end and found nothing a summary reads (only bookkeeping, or only placeholders).
		summary = "No conversation content in the records read so far."
	}
	if model == "" {
		model = "none (no model call was needed)"
	}
	keys := make([]string, 0, len(wm))
	for k := range wm {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	id, err := r.API.WriteOutput(ctx, pluginapi.Envelope{Plugin: Name, PluginVersion: Version, Model: model,
		Session: it.Session, InputWatermark: wm, Type: OutputType, Body: map[string]string{"text": summary}})
	if err != nil {
		return res, fmt.Errorf("writing the summary (watermark %s): %w", strings.Join(keys, ","), err)
	}
	res.written = id
	return res, nil
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
