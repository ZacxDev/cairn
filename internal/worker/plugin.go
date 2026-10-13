package worker

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/ZacxDev/cairn/internal/plugins"
	"github.com/ZacxDev/cairn/internal/transcript"
)

// THE PLUGIN API (slice S5 of `claudedocs/plan-cairn-plugins.md`, "## The contracts"): four routes
// a `plugin` worker token reaches, plain HTTP+JSON (O7 — never MCP).
//
//	GET  /plugin/v1/pending?limit=N                                   pending:list
//	GET  /plugin/v1/transcript?session=&stream=&from_seq=&max_bytes=  transcript:read + ON
//	GET  /plugin/v1/outputs?session=&type=                            output:read:<type> + ON
//	POST /plugin/v1/outputs  {envelope}                               output:write:<type> + ON → 201 {"id"}
//
// 🔴 EVERY REFUSAL OF A CAPABILITY, A TYPE OR A TOGGLE IS ONE 403 BODY ([ForbiddenBody]) — clause (j)
// and T10. A session the plugin is OFF for, one that was never stored, and an id that is not a
// session id are the same answer, so a plugin token cannot be used to ask which sessions exist. A
// MALFORMED request (an unparsable number, a stream name outside decision 7's vocabulary, a bad
// envelope) is a 400 that is decided from the request alone, before any session is looked at.

// The plugin routes.
const (
	PendingPattern    = "/plugin/v1/pending"
	TranscriptPattern = "/plugin/v1/transcript"
	OutputsPattern    = "/plugin/v1/outputs"
)

// ForbiddenBody is the plugin API's ONE refusal body.
const ForbiddenBody = `{"refused":"plugin-forbidden"}` + "\n"

// Transcript read bounds: a page carries records up to `max_bytes` of record bytes, and always at
// least one record when one is there (a record is bounded by `archive.MaxAssembledBytes`).
const (
	DefaultTranscriptPageBytes = 1 << 20
	MaxTranscriptPageBytes     = 8 << 20
)

func forbidden(rw http.ResponseWriter) { writeJSON(rw, http.StatusForbidden, ForbiddenBody) }

// entry resolves the token row to its registered plugin. A row the wall admitted always resolves; a
// registry that changed under a running process (it cannot — it is read once at startup) would
// answer 403 rather than serve a plugin nobody registered.
func (w *Worker) entry(rw http.ResponseWriter, row TokenRow) (plugins.Entry, bool) {
	if w.cfg.Plugins == nil {
		forbidden(rw)
		return plugins.Entry{}, false
	}
	e, ok := w.cfg.Plugins.Registry.Lookup(row.Plugin)
	if !ok {
		forbidden(rw)
		return plugins.Entry{}, false
	}
	return e, true
}

// refuse maps a service error onto the contract: [plugins.ErrForbidden] is the one 403; anything else
// is a 500 whose detail goes to the operator log only.
func (w *Worker) refuse(rw http.ResponseWriter, err error) {
	if errors.Is(err, plugins.ErrForbidden) {
		forbidden(rw)
		return
	}
	w.cfg.Log("worker: a plugin request failed: " + err.Error())
	rw.Header().Set("Content-Type", "text/plain; charset=utf-8")
	rw.WriteHeader(http.StatusInternalServerError)
	_, _ = io.WriteString(rw, "internal error\n")
}

func writeValue(rw http.ResponseWriter, status int, v any) {
	raw, err := json.Marshal(v)
	if err != nil {
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}
	writeJSON(rw, status, string(raw)+"\n")
}

// intParam reads an optional non-negative integer query parameter.
func intParam(r *http.Request, name string, def, lo, hi int64) (int64, error) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return def, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < lo || n > hi {
		return 0, fmt.Errorf("%s must be an integer in [%d, %d]", name, lo, hi)
	}
	return n, nil
}

func (w *Worker) handlePending(rw http.ResponseWriter, r *http.Request, row TokenRow) {
	e, ok := w.entry(rw, row)
	if !ok {
		return
	}
	if !e.Has(plugins.CapPendingList) {
		forbidden(rw)
		return
	}
	limit, err := intParam(r, "limit", plugins.DefaultPendingLimit, 1, plugins.MaxPendingLimit)
	if err != nil {
		badRequest(rw, err)
		return
	}
	items, err := w.cfg.Plugins.Pending(e, int(limit))
	if err != nil {
		w.refuse(rw, err)
		return
	}
	if items == nil {
		items = []plugins.PendingItem{}
	}
	writeValue(rw, http.StatusOK, map[string]any{"items": items})
}

// transcriptRecord is one record on the wire.
type transcriptRecord struct {
	Seq int64           `json:"seq"`
	Rec json.RawMessage `json:"rec"`
}

func (w *Worker) handleTranscript(rw http.ResponseWriter, r *http.Request, row TokenRow) {
	e, ok := w.entry(rw, row)
	if !ok {
		return
	}
	// 🔴 THE CAPABILITY FIRST, THEN THE REQUEST'S SHAPE, THEN THE SESSION'S TOGGLE. A plugin without
	// `transcript:read` learns nothing about its parameters either.
	if !e.Has(plugins.CapTranscriptRead) {
		forbidden(rw)
		return
	}
	q := r.URL.Query()
	stream := q.Get("stream")
	if stream == "" {
		stream = "main"
	}
	if !plugins.ValidStream(stream) {
		badRequest(rw, errors.New("stream is not a stream name"))
		return
	}
	if v := q.Get("view"); v != "" {
		// Decision 17's views are slice S8's; this build serves stored records as stored.
		badRequest(rw, errors.New("view is not served by this build: records are returned as stored"))
		return
	}
	from, err := intParam(r, "from_seq", 1, 0, 1<<62)
	if err != nil {
		badRequest(rw, err)
		return
	}
	budget, err := intParam(r, "max_bytes", DefaultTranscriptPageBytes, 1, MaxTranscriptPageBytes)
	if err != nil {
		badRequest(rw, err)
		return
	}
	v, err := w.cfg.Plugins.Open(e.Name, q.Get("session"))
	if err != nil {
		w.refuse(rw, err)
		return
	}
	out := []transcriptRecord{}
	var used int64
	next := from
	err = v.Records(stream, func(rec transcript.Record) error {
		if rec.Seq < from {
			return nil
		}
		if len(out) > 0 && used+int64(len(rec.Rec)) > budget {
			return transcript.StopRecords
		}
		out = append(out, transcriptRecord{Seq: rec.Seq, Rec: rec.Rec})
		used += int64(len(rec.Rec))
		next = rec.Seq + 1
		return nil
	})
	if err != nil {
		w.refuse(rw, err)
		return
	}
	writeValue(rw, http.StatusOK, map[string]any{"records": out, "next_seq": next,
		"seq_to": v.Meta.Streams[stream].SeqTo})
}

func (w *Worker) handleOutputsRead(rw http.ResponseWriter, r *http.Request, row TokenRow) {
	e, ok := w.entry(rw, row)
	if !ok {
		return
	}
	q := r.URL.Query()
	outs, err := w.cfg.Plugins.ReadOutputs(e, q.Get("session"), q.Get("type"))
	if err != nil {
		w.refuse(rw, err)
		return
	}
	writeValue(rw, http.StatusOK, map[string]any{"outputs": outs})
}

func (w *Worker) handleOutputsWrite(rw http.ResponseWriter, r *http.Request, row TokenRow) {
	e, ok := w.entry(rw, row)
	if !ok {
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(rw, r.Body, plugins.MaxEnvelopeBytes))
	if err != nil {
		badRequest(rw, errors.New("the envelope is over its bound or could not be read"))
		return
	}
	env, err := plugins.DecodeEnvelope(raw)
	if err != nil {
		// 🔴 A TYPE OUTSIDE THE CLOSED SET IS A 400 (the body cannot be validated), but a closed-set type
		// this plugin does not WRITE is the 403 below — the clause-(j) refusal is about authority.
		badRequest(rw, err)
		return
	}
	out, err := w.cfg.Plugins.WriteOutput(e, env)
	var wm *plugins.WatermarkError
	switch {
	case errors.As(err, &wm):
		badRequest(rw, wm)
	case err != nil:
		w.refuse(rw, err)
	default:
		writeValue(rw, http.StatusCreated, map[string]string{"id": out.ID})
	}
}
