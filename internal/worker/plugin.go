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
//	GET  /plugin/v1/transcript?session=&stream=&from_seq=&max_bytes=&view=  transcript:read + ON
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

// handleTranscript is `GET /plugin/v1/transcript`: ONE page of a stored stream, through S8's read
// model — `transcript.ParseRecordsQuery` for the parameters (`stream`, `from_seq`, `to_seq`,
// `max_bytes`, `view` — `conversation` by default, or `raw` — and the class/role/tool filters) and
// `View.Page` for the page, so the plugin API and the agent read API have ONE pager (one rule, one
// place) and a plugin reads what decision 17 says an agent reads.
//
// 🔴 THE CAPABILITY FIRST, THEN THE REQUEST'S SHAPE, THEN THE SESSION'S TOGGLE. A plugin without
// `transcript:read` learns nothing about its parameters either; a malformed query is a 400 decided
// from the query alone; only then is the session looked at, and every miss after that — OFF, never
// stored, no such stream — is the ONE 403.
func (w *Worker) handleTranscript(rw http.ResponseWriter, r *http.Request, row TokenRow) {
	e, ok := w.entry(rw, row)
	if !ok {
		return
	}
	if !e.Has(plugins.CapTranscriptRead) {
		forbidden(rw)
		return
	}
	q := r.URL.Query()
	rq, err := transcript.ParseRecordsQuery(q)
	if err != nil {
		badRequest(rw, err)
		return
	}
	v, err := w.cfg.Plugins.Open(e.Name, q.Get("session"))
	if err != nil {
		w.refuse(rw, err)
		return
	}
	page, err := v.Page(rq)
	if errors.Is(err, transcript.ErrNotHeld) {
		forbidden(rw)
		return
	}
	if err != nil {
		w.refuse(rw, err)
		return
	}
	writeJSON(rw, http.StatusOK, string(page.JSON()))
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
	var quota *transcript.QuotaError
	switch {
	case errors.As(err, &wm):
		badRequest(rw, wm)
	case errors.As(err, &quota):
		// 🔴 507, NAMED, AND SAID ONCE — capture's answer (#270 round 1: it was an opaque 500 and a log
		// line per request). The plugin holds and retries; the operator sees one line.
		writeJSON(rw, http.StatusInsufficientStorage, fmt.Sprintf(`{"refused":"quota","quota_bytes":%d}`+"\n", quota.Quota))
		w.once(quotaLine(e.Name, quota))
	case err != nil:
		w.refuse(rw, err)
	default:
		writeValue(rw, http.StatusCreated, map[string]string{"id": out.ID})
	}
}

// quotaLine is the operator's line for a plugin write refused 507, and it says WHICH limit was hit.
//
// 🔴 THREE LIMITS, THREE REMEDIES, AND THE LINE NAMED ONE OF THEM FOR ALL THREE (#270's audit, closed
// in S6): it read "plugin X's outputs reached their share" whether X had filled its own share, all
// plugins together had filled theirs, or the INSTANCE quota was full of transcripts — and for the
// last, the operator was sent to look at a plugin that had written almost nothing.
func quotaLine(plugin string, q *transcript.QuotaError) string {
	switch q.Share {
	case transcript.SharePlugin:
		return fmt.Sprintf("worker: plugin %s's OWN outputs reached that plugin's share of the transcript quota "+
			"(%d bytes); its writes are refused 507 until sessions holding them are deleted", plugin, q.Quota)
	case transcript.ShareOutputs:
		return fmt.Sprintf("worker: ALL plugins' outputs together reached their share of the transcript quota "+
			"(%d bytes); plugin %s's write — and every other plugin's — is refused 507 until sessions holding outputs "+
			"are deleted", q.Quota, plugin)
	}
	return fmt.Sprintf("worker: the INSTANCE transcript quota (%d bytes) is reached — by everything stored, not by "+
		"plugin %s's outputs; its write is refused 507 until retention frees space", q.Quota, plugin)
}
