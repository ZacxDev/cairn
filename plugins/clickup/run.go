package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ZacxDev/cairn/plugins/internal/hoststate"
	"github.com/ZacxDev/cairn/plugins/internal/llm"
	"github.com/ZacxDev/cairn/plugins/internal/pluginapi"
	"github.com/ZacxDev/cairn/plugins/internal/spend"
	"github.com/ZacxDev/cairn/plugins/internal/transcripttext"
)

// 🔴 ONE PASS OVER A SESSION WRITES ITS EDGES, THEN ITS SUGGESTIONS, THEN ONE `ticket-scan`. The pod
// holds the progress state (decision 13): a session is pending while it holds records past this
// plugin's LAST-WRITTEN output's watermark, and every output of a pass carries the pass's watermark —
// so the FIRST one written already takes the session off the pending list. What makes the rest of the
// pass happen anyway is on THIS host: the redo marker ([stateFile]), set before the first write and
// cleared when the scan is written. A pass that is being redone, and a pass whose previous output is
// not a scan, read the session from its first record; duplicates are impossible, because an edge the
// session already holds for the same ticket and signal is never written again.
//
// 🔴 THE SCAN IS ALSO WHY A SESSION THAT MATCHES NOTHING STOPS BEING PENDING. Most sessions name no
// ticket. Without something to write, such a session would stay at the head of the pending list
// (oldest first) forever, and a page of them would starve every session behind it.
//
// ⚠ SUGGESTIONS ARE BEST-EFFORT, AND EDGES ARE NOT HELD HOSTAGE TO THEM: when the spend cap is
// reached, no key is configured, or the provider fails, the pass still completes and its scan says no
// model was asked (`model_asked: false`). A failed request is counted and makes the run exit 1.

// Name and Version are the plugin's own, matching `manifest.json` (the pod stamps the registry's).
const (
	Name    = "clickup"
	Version = "0.1.0"
)

// The output types this plugin writes, and the one it reads from another plugin.
const (
	TypeEdge       = "ticket-edge"
	TypeSuggestion = "ticket-suggestion"
	TypeScan       = "ticket-scan"
	TypeSummary    = "summary"
)

// The signals (decision 14's closed set, `internal/plugins` `Signals`).
const (
	SigBranch     = "branch-name"
	SigCommitMsg  = "commit-message"
	SigCommitTrlr = "commit-trailer"
	SigPRBody     = "pr-body"
	SigPRLink     = "pr-link"
	SigTranscript = "transcript-url"
	SigEntryRef   = "entry-ref"
)

// maxTitle is the pod's bound on a stored ticket title (`internal/plugins` outputs.go).
const maxTitle = 256

// noModel is the `model` an output carries when no model made it: the envelope requires the field.
const noModel = "none (deterministic match)"

// maxEdgesPerPass bounds the edges one pass writes: a session that LISTED a workspace's tickets names
// every one of them by URL. A pass that stops here is continued by the next run (the redo marker).
const maxEdgesPerPass = 100

// Locator is where a match was found — exactly one of its shapes (`internal/plugins` `Locator`).
type Locator struct {
	Stream string `json:"stream,omitempty"`
	Seq    int64  `json:"seq,omitempty"`
	Commit string `json:"commit,omitempty"`
	PR     int    `json:"pr,omitempty"`
	Entry  string `json:"entry,omitempty"`
}

// Edge is one `ticket-edge` body.
type Edge struct {
	System   string  `json:"system"`
	TicketID string  `json:"ticket_id"`
	CustomID string  `json:"custom_id,omitempty"`
	Title    string  `json:"title,omitempty"`
	Signal   string  `json:"signal"`
	Locator  Locator `json:"locator"`
}

// Scan is one `ticket-scan` body.
type Scan struct {
	System      string `json:"system"`
	Edges       int    `json:"edges"`
	Suggestions int    `json:"suggestions"`
	ModelAsked  bool   `json:"model_asked"`
}

// Runner is one run's configuration.
type Runner struct {
	API     *pluginapi.Client
	Matcher *Matcher
	// Provider is nil when no key was configured: no suggestion is asked for.
	Provider llm.Provider
	Spend    *spend.Ledger
	// Repos is the host-local repository list; GitBin the git to run in them.
	Repos  []string
	GitBin string
	// GHBin is the GitHub CLI; "" reads no PR.
	GHBin string
	// Entries is the cache roots' index; nil when none is configured.
	Entries EntryIndex
	// State is the host-local backoff; Now its clock. Limit is how many sessions one run WORKS ON
	// (sessions skipped in backoff do not count).
	State *stateFile
	Now   func() time.Time
	Limit int
	// PageBytes is the `max_bytes` of one transcript page.
	PageBytes int
	// ExcerptBytes bounds the opening excerpt sent when no summary is stored.
	ExcerptBytes    int
	MaxOutputTokens int
	// MaxCandidates bounds the ticket list one request carries; MaxSuggestions what one pass writes.
	MaxCandidates  int
	MaxSuggestions int
	MinConfidence  float64
	// StoreTitles sends each fetched ticket's NAME as the output's `title` (Q8) — only useful against
	// an instance started with `-plugin-store-ticket-titles`; any other refuses the write.
	StoreTitles bool
	Log         func(string)
}

// Stats is what a run did.
type Stats struct {
	Sessions, Edges, Suggestions, Requests, Failed, Skipped int
	Capped                                                  bool
}

// pendingAsk is how many pending sessions one run ASKS for — the pod's maximum, not `-limit`: sessions
// in backoff still occupy the oldest-first list, so asking for only `-limit` would let that many
// failing sessions hide every healthy one behind them. ⚠ More than this many in backoff at once
// still hides the rest.
const pendingAsk = 100

// stateFile is this plugin's host-local memory (`internal/hoststate`): per session, a failure backoff
// and the REDO marker.
//
// 🔴 THE REDO MARKER IS WHAT MAKES A PASS ALL-OR-RETRIED. Every output of a pass carries the pass's
// watermark, so the FIRST one written already takes the session off the pod's pending list (decision
// 13). A pass that then stops — a write the pod refuses, a 500, the run's deadline, a killed process —
// would leave a session that never grows again short of its remaining edges FOREVER: the pod no
// longer lists it, and nothing else would ask. So the pending item is saved here BEFORE the pass's
// first write and cleared only when the pass finished; a session carrying the marker is worked on
// again whether or not the pod lists it, from its first record (an edge the session already holds is
// never written twice). The same marker keeps a pass that stopped at the edge cap, or whose host
// source (a repository, the GitHub CLI) could not be read, coming back.
//
// ⚠ BOUNDED, FOR A SESSION ONLY THE MARKER BRINGS BACK: a failure the pod or a host source ANSWERED
// backs it off, and after [maxRedoFailures] failed attempts at the same size the marker is dropped and
// a line says so; it is dropped at once when the pod answers its one 403 (deleted, or toggled off) or
// holds fewer records than the marker names; and it is never re-stamped by being retried, so
// [hoststate.StateTTL] ends it whatever else happens — that is the ONLY bound on a pass the run's own
// deadline keeps cutting short. A session the pod still lists keeps backing off instead; and in every
// case the model is not asked the same question twice ([sessionState.Asked]).
type stateFile struct {
	path     string
	Sessions map[string]*sessionState `json:"sessions"`
}

// sessionState is one session's entry. The backoff's fields are flattened into its JSON object.
type sessionState struct {
	hoststate.Backoff
	// Redo is the pending item of a pass that started and has not finished.
	Redo *pluginapi.PendingItem `json:"redo,omitempty"`
	// Asked is what the model was asked, and answered, for the unfinished pass.
	//
	// 🔴 A MODEL IS PAID ONCE FOR ONE QUESTION, HOWEVER OFTEN THE PASS IS REDONE — AND WHATEVER IT
	// ANSWERED. The record is saved here the moment the provider answers OR FAILS, before anything is
	// written to the pod; a redo that would ask the SAME question (the same description of the
	// session) writes what is saved and asks nobody. Without it every redo — after a 500 on the
	// scan's write, a refused write, a failed host source — bought the same request again; and with
	// only the successes remembered, a model that answered prose was re-bought on every redo.
	Asked *asked `json:"asked,omitempty"`
}

// asked is one question put to the provider and what came of it.
type asked struct {
	// Digest identifies the QUESTION: the description of the session the model was sent. A session
	// that grew without its description changing (the same stored summary) is the same question; a
	// new summary is a new one.
	Digest string `json:"digest"`
	// Model is the model that answered; "" when the request failed or its answer could not be used.
	Model       string       `json:"model"`
	Suggestions []Suggestion `json:"suggestions"`
}

// errShrunk is a session whose stored streams end BEFORE the positions a pending item names: it was
// deleted and uploaded again under the same id. A watermark at the old positions would be refused,
// so the pass stops before anything is paid for and the stale marker is dropped.
var errShrunk = errors.New("the session holds fewer records than the pending item names (deleted and re-uploaded); the pass is dropped")

// maxRedoFailures bounds how often an unfinished pass is retried at one size.
const maxRedoFailures = 6

func loadState(path string) (*stateFile, error) {
	st := &stateFile{path: path}
	if err := hoststate.Load(path, st); err != nil {
		return nil, err
	}
	if st.Sessions == nil {
		st.Sessions = map[string]*sessionState{}
	}
	return st, nil
}

func (st *stateFile) save() error { return hoststate.Save(st.path, st) }

// records is a pending item's record count over every listed stream.
func records(it pluginapi.PendingItem) int64 {
	var n int64
	for _, pos := range it.Streams {
		n += pos.SeqTo
	}
	return n
}

// work is one session a run works on: its pending item, and whether an earlier pass over it did not
// finish (so this one reads from the first record).
type work struct {
	item pluginapi.PendingItem
	redo bool
	// unlisted reports that the pod no longer lists the session: only the redo marker brought it here.
	unlisted bool
}

// due is the sessions this run works on: every unfinished pass first — listed by the pod or not —
// then the pending list, without the sessions in backoff, at most Limit.
func (r *Runner) due(ctx context.Context, items []pluginapi.PendingItem, st *Stats) ([]work, error) {
	listed := map[string]pluginapi.PendingItem{}
	for _, it := range items {
		listed[it.Session] = it
	}
	changed := false
	var unlisted []string
	for id, ss := range r.State.Sessions {
		if _, ok := listed[id]; ok {
			changed = ss.Touch(r.Now()) || changed
			continue
		}
		// 🔴 NOT LISTED IS NOT GONE ([hoststate.Backoff.Unlisted]): a session that was only resumed
		// drops off the pending list and comes back. Its backoff is kept until the pod answers its
		// one refusal for it, or the entry is past its TTL.
		seen := ss.Seen
		if ss.Redo == nil && ss.Unlisted(r.Now(), func() (bool, error) {
			_, err := r.API.Outputs(ctx, id, TypeScan)
			if errors.Is(err, pluginapi.ErrForbidden) {
				return true, nil // the pod's one refusal: the positive signal
			}
			return false, err
		}) || ss.Redo != nil && ss.Stale(r.Now()) {
			delete(r.State.Sessions, id)
			changed = true
			continue
		}
		changed = changed || ss.Seen != seen
		if ss.Redo != nil {
			unlisted = append(unlisted, id)
		}
	}
	if changed {
		if err := r.State.save(); err != nil {
			return nil, err
		}
	}
	slices.Sort(unlisted)
	var all []work
	for _, id := range unlisted {
		all = append(all, work{item: *r.State.Sessions[id].Redo, redo: true, unlisted: true})
	}
	for _, it := range items {
		ss := r.State.Sessions[it.Session]
		all = append(all, work{item: it, redo: ss != nil && ss.Redo != nil})
	}
	var out []work
	for _, w := range all {
		if ss := r.State.Sessions[w.item.Session]; ss != nil && ss.Active(r.Now(), records(w.item)) {
			st.Skipped++
			r.Log(fmt.Sprintf("clickup: session=%s SKIPPED: %d failure(s) at this size, next attempt %s", w.item.Session, ss.Failures, ss.RetryAt))
			continue
		}
		if len(out) < r.Limit {
			out = append(out, w)
		}
	}
	return out, nil
}

// refused reports whether a pass failed because the POD REFUSED a request — a full quota (507), a
// malformed or over-bound request (400), the one 403 — which repeats for the same records. A 5xx, a
// 3xx, the pod not being reached and the run's own deadline say nothing about THIS session: those are
// retried by the next run with no backoff.
func refused(err error) bool {
	var se *pluginapi.StatusError
	if errors.As(err, &se) {
		return se.Status == 400 || se.Status == 507
	}
	return errors.Is(err, pluginapi.ErrForbidden)
}

// Run does one pass over the sessions [Runner.due] chose.
func (r *Runner) Run(ctx context.Context, items []work, st *Stats) error {
	for _, w := range items {
		it := w.item
		st.Sessions++
		// The marker BEFORE the first write: see [stateFile].
		ss := r.State.Sessions[it.Session]
		if ss == nil {
			ss = &sessionState{}
			r.State.Sessions[it.Session] = ss
		}
		ss.Redo = &it
		if !w.unlisted {
			// 🔴 NOT for a session only the marker brought here: its stamp stays where the pod last
			// listed it, so the state TTL bounds how long an unfinished pass is retried.
			ss.Touch(r.Now())
		}
		if err := r.State.save(); err != nil {
			return err
		}
		res, err := r.session(ctx, it, w.redo, ss)
		st.Edges += res.edges
		st.Suggestions += res.suggestions
		st.Requests += res.requests
		if res.capped {
			st.Capped = true
		}
		note := ""
		switch {
		case err != nil && w.redo && errors.Is(err, pluginapi.ErrForbidden):
			// The pod's one 403 on a pass being redone: the session was deleted or the plugin turned
			// off for it. There is nothing left to finish.
			delete(r.State.Sessions, it.Session)
			note = " — the pod no longer serves this session to the plugin; the unfinished pass is dropped"
		case errors.Is(err, errShrunk):
			delete(r.State.Sessions, it.Session)
		case err != nil && ctx.Err() == nil && (refused(err) || w.unlisted), err == nil && res.sourceErrs > 0:
			// A session ONLY the marker brings back backs off on ANY failure, transient ones included:
			// nothing else is waiting on it, and an unbounded every-run retry is what it would be.
			// 🔴 NOT RETRIED NEXT RUN: a refusal by the pod repeats for the same records, and a model
			// may already have been paid for the suggestion whose write was refused.
			wait := ss.Fail(r.Now(), records(it))
			note = fmt.Sprintf(" — backing off %s", wait)
			// Given up only where nothing else will ever bring the session back with a longer wait: a
			// session the pod still lists keeps its backoff, which grows to its ceiling.
			if w.unlisted && ss.Failures >= maxRedoFailures {
				delete(r.State.Sessions, it.Session)
				note = fmt.Sprintf(" — %d failed attempts at this size; the unfinished pass is GIVEN UP until the session grows", maxRedoFailures)
			}
		case err == nil && !res.more:
			delete(r.State.Sessions, it.Session) // the pass finished
		}
		// Anything else — for a session the pod lists: the pod failing (5xx) or not reached, the
		// run's deadline; for any session: the edge cap — keeps the marker with no backoff, and the
		// next run continues. It asks no model again: the answer is in the marker ([sessionState.Asked]).
		if serr := r.State.save(); serr != nil {
			return serr
		}
		if err != nil {
			st.Failed++
			r.Log(fmt.Sprintf("clickup: session=%s FAILED: %v%s", it.Session, err, note))
			continue
		}
		if res.sourceErrs > 0 || res.modelErr {
			st.Failed++
		}
		r.Log(fmt.Sprintf("clickup: session=%s full=%t edges=%d suggestions=%d requests=%d capped=%t source-errors=%d more=%t%s",
			it.Session, res.full, res.edges, res.suggestions, res.requests, res.capped, res.sourceErrs, res.more, note))
	}
	return nil
}

type sessionResult struct {
	edges, suggestions, requests int
	capped, full                 bool
	// sourceErrs counts host sources (a repository, the GitHub CLI) that could not be read: the pass
	// completes and is redone later. modelErr is a provider failure: suggestions are best-effort, so
	// the pass completes and is NOT redone for it.
	sourceErrs int
	modelErr   bool
	// more reports that the pass stopped at the edge cap with edges still unwritten.
	more bool
}

// found is one candidate edge, before it is checked against what the session already holds.
type found struct {
	hit Hit
	sig string
	loc Locator
}

// hitOrder sorts one record's hits so which is written does not vary between runs: by ticket, and
// for one ticket the hit that spelled a custom id first — the edge then carries it.
func hitOrder(a, b Hit) int {
	if c := strings.Compare(a.ID, b.ID); c != 0 {
		return c
	}
	return strings.Compare(b.Custom, a.Custom)
}

// eachString calls fn for every string VALUE in a decoded JSON value — never a key.
func eachString(v any, fn func(string)) {
	switch t := v.(type) {
	case string:
		fn(t)
	case []any:
		for _, e := range t {
			eachString(e, fn)
		}
	case map[string]any:
		for _, e := range t {
			eachString(e, fn)
		}
	}
}

// scanned is what one pass read off the transcript.
type scanned struct {
	// branches is each distinct `gitBranch`, at the first record that carried it.
	branches map[string]Locator
	urls     []found
	prs      []PR
}

// scanStream reads one stream's records in (after, to] with `view=raw` — a matcher reads EVERY byte
// the record holds, bookkeeping included: `gitBranch` and the `pr-link` record ARE bookkeeping, which
// the conversation view leaves out.
func (r *Runner) scanStream(ctx context.Context, session, stream string, after, to int64, into *scanned) error {
	from := after + 1
	for from <= to {
		p, err := r.API.Transcript(ctx, pluginapi.TranscriptQuery{Session: session, Stream: stream, FromSeq: from,
			View: "raw", MaxBytes: r.PageBytes})
		if err != nil {
			return err
		}
		if p.SeqTo < to {
			return errShrunk
		}
		if len(p.Records) == 0 {
			break
		}
		last := from - 1
		for _, rec := range p.Records {
			if rec.Seq < from || rec.Seq <= last {
				return fmt.Errorf("the transcript page went backwards (record %d after %d)", rec.Seq, last)
			}
			last = rec.Seq
			if rec.Seq > to {
				break
			}
			var v any
			if len(rec.Rec) == 0 || json.Unmarshal(rec.Rec, &v) != nil {
				continue
			}
			loc := Locator{Stream: stream, Seq: rec.Seq}
			if obj, ok := v.(map[string]any); ok {
				if b, _ := obj["gitBranch"].(string); b != "" {
					if _, seen := into.branches[b]; !seen {
						into.branches[b] = loc
					}
				}
				if pr, ok := prOf(obj); ok {
					pr.Stream, pr.Seq = stream, rec.Seq
					into.prs = append(into.prs, pr)
				}
			}
			// One record's map is walked in no fixed order; the hits are sorted so which ticket's
			// edge is written first does not vary between runs.
			var hits []Hit
			eachString(v, func(s string) { hits = append(hits, r.Matcher.URLs(s)...) })
			slices.SortStableFunc(hits, hitOrder)
			for _, h := range hits {
				into.urls = append(into.urls, found{hit: h, sig: SigTranscript, loc: loc})
			}
		}
		from = last + 1
	}
	return nil
}

// streamsOf is the item's streams, `main` first. The pod lists only streams a watermark may name (a
// session's `ledger` stream is not listed), so no second filter is kept here.
func streamsOf(it pluginapi.PendingItem) []string {
	var out []string
	for s := range it.Streams {
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b string) int {
		if (a == "main") != (b == "main") {
			if a == "main" {
				return -1
			}
			return 1
		}
		return strings.Compare(a, b)
	})
	return out
}

// held is what a session already holds from earlier passes.
type held struct {
	// edges is every (ticket, signal) an edge exists for; edged every ticket with any edge.
	edges, edged map[string]bool
	// suggested is every ticket already suggested.
	suggested map[string]bool
	// byURL is every ticket an EARLIER pass found named by URL in the transcript — what lets a bare
	// token in a branch count when the URL and the branch are read by two different passes.
	byURL map[string]bool
	// prevScan is the previous pass's scan when the session's LAST output is one; nil otherwise.
	prevScan *pluginapi.Output
}

func (r *Runner) heldBy(ctx context.Context, it pluginapi.PendingItem, redo bool) (held, error) {
	h := held{edges: map[string]bool{}, edged: map[string]bool{}, suggested: map[string]bool{}, byURL: map[string]bool{}}
	edges, err := r.API.Outputs(ctx, it.Session, TypeEdge)
	if err != nil {
		return h, fmt.Errorf("reading the session's edges: %w", err)
	}
	for _, o := range edges {
		var e Edge
		if json.Unmarshal(o.Body, &e) == nil && e.TicketID != "" {
			h.edges[e.TicketID+"\x00"+e.Signal] = true
			h.edged[e.TicketID] = true
			if e.Signal == SigTranscript {
				h.byURL[e.TicketID] = true
			}
		}
	}
	suggs, err := r.API.Outputs(ctx, it.Session, TypeSuggestion)
	if err != nil {
		return h, fmt.Errorf("reading the session's suggestions: %w", err)
	}
	for _, o := range suggs {
		var s Suggestion
		if json.Unmarshal(o.Body, &s) == nil {
			h.suggested[s.TicketID] = true
		}
	}
	if it.PrevOutput == nil && !redo {
		return h, nil
	}
	scans, err := r.API.Outputs(ctx, it.Session, TypeScan)
	if err != nil {
		return h, fmt.Errorf("reading the session's scans: %w", err)
	}
	for i := range scans {
		switch {
		case redo:
			// A pass being redone reads from record 1 whatever the last output is; the latest scan
			// is kept only so the model is not asked again about a summary it already saw.
			h.prevScan = &scans[i]
		case scans[i].ID == it.PrevOutput.ID:
			h.prevScan = &scans[i]
		}
	}
	return h, nil
}

func (r *Runner) session(ctx context.Context, it pluginapi.PendingItem, redo bool, ss *sessionState) (sessionResult, error) {
	var res sessionResult
	streams := streamsOf(it)
	if len(streams) == 0 {
		return res, errors.New("the pending item names no stream")
	}
	if !sessionShape.MatchString(it.Session) {
		return res, errors.New("the pending item's session is not a session id")
	}
	h, err := r.heldBy(ctx, it, redo)
	if err != nil {
		return res, err
	}
	res.full = redo || h.prevScan == nil

	// --- the transcript: branches, PR links and URLs.
	sc := scanned{branches: map[string]Locator{}}
	wm := map[string]int64{}
	for _, s := range streams {
		after := int64(0)
		if !res.full {
			after = min(it.PrevOutput.Watermark[s], it.Streams[s].SeqTo)
		}
		if err := r.scanStream(ctx, it.Session, s, after, it.Streams[s].SeqTo, &sc); err != nil {
			return res, fmt.Errorf("reading the transcript (%s): %w", s, err)
		}
		wm[s] = it.Streams[s].SeqTo
	}
	all := sc.urls

	// --- commits: messages now, trailers below — from the host-local repository list.
	var commits []Commit
	for _, repo := range r.Repos {
		cs, err := SessionCommits(ctx, r.GitBin, repo, it.Session)
		if err != nil {
			res.sourceErrs++
			r.Log(fmt.Sprintf("clickup: session=%s repository %s could not be read: %v", it.Session, repo, err))
			continue
		}
		commits = append(commits, cs...)
	}
	for _, c := range commits {
		for _, hit := range r.Matcher.Prose(c.Message) {
			all = append(all, found{hit: hit, sig: SigCommitMsg, loc: Locator{Commit: c.SHA}})
		}
	}

	// --- PRs: the body (`pr-body`), and the title (`pr-link`), through the host's GitHub CLI.
	type prText struct {
		pr   PR
		text PRText
	}
	var fetched []prText
	if r.GHBin != "" {
		seen := map[string]bool{}
		for _, pr := range sc.prs {
			key := fmt.Sprintf("%s#%d", pr.Repo, pr.Number)
			if seen[key] {
				continue
			}
			if len(seen) == maxPRs {
				r.Log(fmt.Sprintf("clickup: session=%s names more than %d PRs; the rest are not read", it.Session, maxPRs))
				break
			}
			seen[key] = true
			text, err := FetchPR(ctx, r.GHBin, pr)
			if err != nil {
				res.sourceErrs++
				r.Log(fmt.Sprintf("clickup: session=%s PR %s could not be read: %v", it.Session, key, err))
				continue
			}
			fetched = append(fetched, prText{pr, text})
			for _, hit := range r.Matcher.Prose(text.Body) {
				all = append(all, found{hit: hit, sig: SigPRBody, loc: Locator{PR: pr.Number}})
			}
			for _, hit := range r.Matcher.Prose(text.Title) {
				all = append(all, found{hit: hit, sig: SigPRLink, loc: Locator{PR: pr.Number}})
			}
		}
	}

	// --- entries the session wrote: explicit `clickup:<id>` refs.
	for _, e := range r.Entries[it.Session] {
		for _, ident := range e.Idents {
			switch t, ok := r.Matcher.Index.Resolve(ident); {
			case ok:
				all = append(all, found{hit: Hit{ID: t.ID, Custom: ident}, sig: SigEntryRef, loc: Locator{Entry: e.Ref}})
			case canonicalID.MatchString(ident):
				// An explicit ref names the canonical id itself; it needs no list to vouch for it.
				all = append(all, found{hit: Hit{ID: ident}, sig: SigEntryRef, loc: Locator{Entry: e.Ref}})
			}
		}
	}

	// --- structured fields, LAST: a bare token counts when the list holds it or a URL above named it.
	byURL := h.byURL
	for _, f := range all {
		if f.hit.ByURL {
			byURL[f.hit.ID] = true
		}
	}
	names := make([]string, 0, len(sc.branches))
	for b := range sc.branches {
		names = append(names, b)
	}
	slices.Sort(names)
	for _, b := range names {
		for _, hit := range r.Matcher.Field(b, byURL) {
			all = append(all, found{hit: hit, sig: SigBranch, loc: sc.branches[b]})
		}
	}
	for _, c := range commits {
		for _, t := range c.Trailers {
			for _, hit := range r.Matcher.Field(t.Value, byURL) {
				all = append(all, found{hit: hit, sig: SigCommitTrlr, loc: Locator{Commit: c.SHA}})
			}
		}
	}
	for _, f := range fetched {
		for _, hit := range r.Matcher.Field(f.text.Head, byURL) {
			all = append(all, found{hit: hit, sig: SigPRLink, loc: Locator{PR: f.pr.Number}})
		}
	}

	// --- write the edges this session does not already hold: one per (ticket, signal).
	for _, f := range all {
		key := f.hit.ID + "\x00" + f.sig
		if h.edges[key] {
			continue
		}
		if res.edges == maxEdgesPerPass {
			r.Log(fmt.Sprintf("clickup: session=%s names more than %d new tickets; the rest are written by the next run", it.Session, maxEdgesPerPass))
			res.more = true
			break
		}
		h.edges[key] = true
		h.edged[f.hit.ID] = true
		body := Edge{System: "clickup", TicketID: f.hit.ID, CustomID: f.hit.Custom, Title: r.title(f.hit.ID), Signal: f.sig, Locator: f.loc}
		if _, err := r.API.WriteOutput(ctx, pluginapi.Envelope{Plugin: Name, PluginVersion: Version, Model: noModel,
			Session: it.Session, InputWatermark: wm, Type: TypeEdge, Body: body}); err != nil {
			return res, fmt.Errorf("writing a %s edge: %w", f.sig, err)
		}
		res.edges++
	}

	// --- suggestions: one request, and only ever written as suggestions.
	scan := Scan{System: "clickup", Edges: res.edges}
	model := noModel
	a, err := r.suggest(ctx, it, &res, h, ss)
	if err != nil {
		return res, err
	}
	if a != nil && a.Model != "" {
		m := a.Model
		scan.ModelAsked, model = true, m
		for _, s := range a.Suggestions {
			// A redo writes only what the interrupted pass did not get to — and nothing for a ticket
			// the session has since been found to NAME: an edge needs no suggestion beside it.
			if h.suggested[s.TicketID] || h.edged[s.TicketID] {
				continue
			}
			h.suggested[s.TicketID] = true
			s.Title = r.title(s.TicketID)
			if _, err := r.API.WriteOutput(ctx, pluginapi.Envelope{Plugin: Name, PluginVersion: Version, Model: m,
				Session: it.Session, InputWatermark: wm, Type: TypeSuggestion, Body: s}); err != nil {
				return res, fmt.Errorf("writing a suggestion: %w", err)
			}
			res.suggestions++
		}
	}
	scan.Suggestions = res.suggestions

	// --- the scan, LAST: the pass finished.
	if _, err := r.API.WriteOutput(ctx, pluginapi.Envelope{Plugin: Name, PluginVersion: Version, Model: model,
		Session: it.Session, InputWatermark: wm, Type: TypeScan, Body: scan}); err != nil {
		return res, fmt.Errorf("writing the scan: %w", err)
	}
	return res, nil
}

// title is the ticket's name as an output may carry it: "" unless the operator asked for titles and
// the fetched list holds the ticket (a ticket named only by URL has no name here).
func (r *Runner) title(id string) string {
	if !r.StoreTitles {
		return ""
	}
	return oneLine(r.Matcher.Index.byID[id].Name, maxTitle)
}

// suggest is this pass's answer from the model: nil when nobody was (or is to be) asked — no provider,
// no candidate, nothing new to describe, the spend cap — and otherwise the record of the ONE request
// for this question, made now or by an earlier attempt at this pass ([sessionState.Asked]). A request
// that failed is such a record too, with no model: it is counted, logged and NOT made again.
func (r *Runner) suggest(ctx context.Context, it pluginapi.PendingItem, res *sessionResult, h held, ss *sessionState) (*asked, error) {
	if r.Provider == nil {
		return nil, nil
	}
	var cands []Ticket
	allowed := map[string]bool{}
	for _, id := range r.Matcher.Index.order {
		// A ticket the session already has an edge to, or was already suggested, is not asked about.
		if h.edged[id] || h.suggested[id] {
			continue
		}
		if len(cands) == r.MaxCandidates {
			break
		}
		cands = append(cands, r.Matcher.Index.byID[id])
		allowed[id] = true
	}
	if len(cands) == 0 {
		return nil, nil
	}
	source, text, err := r.describe(ctx, it, h.prevScan)
	if err != nil {
		// 🔴 THE DESCRIPTION COULD NOT BE READ, SO THE PASS DOES NOT FINISH. Carrying on would write
		// the scan and delete the marker — and with it an answer already paid for and not yet
		// written. The pass fails here and is redone.
		return nil, fmt.Errorf("reading what to ask the model about: %w", err)
	}
	if text == "" {
		// Nothing NEW to describe — but an answer saved for this unfinished pass is still owed.
		if ss.Asked != nil && ss.Asked.Model != "" {
			return ss.Asked, nil
		}
		return nil, nil
	}
	sum := sha256.Sum256([]byte(source + "\x00" + text))
	digest := hex.EncodeToString(sum[:])
	if ss.Asked != nil && ss.Asked.Digest == digest {
		return ss.Asked, nil // this question was already put: no second request
	}
	if err := r.Spend.Take(); err != nil {
		if errors.Is(err, spend.ErrCapped) {
			res.capped = true
			return nil, nil
		}
		res.modelErr = true
		r.Log(fmt.Sprintf("clickup: session=%s no suggestion was made: %v", it.Session, err))
		return nil, nil
	}
	res.requests++
	a := &asked{Digest: digest}
	ans, err := r.Provider.Complete(ctx, llm.Request{System: suggestSystem, User: suggestPrompt(source, text, cands),
		MaxTokens: r.MaxOutputTokens})
	if err == nil {
		var dropped int
		if a.Suggestions, dropped, err = parseSuggestions(ans.Text, allowed, r.MinConfidence, r.MaxSuggestions); err == nil {
			a.Model = ans.Model
			if dropped > 0 {
				r.Log(fmt.Sprintf("clickup: session=%s %d of the model's picks were dropped (not a candidate, no usable confidence, below the floor, or past the limit)",
					it.Session, dropped))
			}
		}
	}
	if err != nil {
		res.modelErr = true
		r.Log(fmt.Sprintf("clickup: session=%s no suggestion was made: %v", it.Session, err))
	}
	// Saved BEFORE anything of it is written, success or failure — except when the run's own deadline
	// cut the request short: that says nothing about the question, and the next run may ask it.
	if err == nil || ctx.Err() == nil {
		ss.Asked = a
		if serr := r.State.save(); serr != nil {
			return nil, serr
		}
	}
	return a, nil
}

// describe is what the model is told about the session: the latest stored `summary` (plugin A's — the
// cross-plugin read, `output:read:summary`), or, when none is stored, the session's OPENING records
// rendered as labelled lines. "" when there is nothing NEW to describe since the previous pass: the
// same summary, or — with no summary — any pass after the first, because the opening does not change.
func (r *Runner) describe(ctx context.Context, it pluginapi.PendingItem, prevScan *pluginapi.Output) (source, text string, err error) {
	sums, err := r.API.Outputs(ctx, it.Session, TypeSummary)
	if err != nil {
		return "", "", fmt.Errorf("reading the session's summary: %w", err)
	}
	for i := len(sums) - 1; i >= 0; i-- {
		var body struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(sums[i].Body, &body) != nil || strings.TrimSpace(body.Text) == "" {
			continue
		}
		if prevScan != nil && sums[i].ProducedAt <= prevScan.ProducedAt {
			return "", "", nil // the summary this plugin already asked about
		}
		return "a summary written by the plugin " + oneLine(sums[i].Plugin, 64), body.Text, nil
	}
	if prevScan != nil {
		return "", "", nil
	}
	mainPos, ok := it.Streams["main"]
	if !ok {
		return "", "", nil
	}
	var entries []transcripttext.Entry
	size := 0
	for from := int64(1); from <= mainPos.SeqTo && size < r.ExcerptBytes; {
		p, err := r.API.Transcript(ctx, pluginapi.TranscriptQuery{Session: it.Session, Stream: "main", FromSeq: from,
			View: "conversation", MaxBytes: r.PageBytes})
		if err != nil {
			return "", "", fmt.Errorf("reading the session's opening: %w", err)
		}
		if len(p.Records) == 0 {
			break
		}
		for _, rec := range p.Records {
			if rec.Seq < from {
				return "", "", errors.New("the transcript page went backwards")
			}
			from = rec.Seq + 1
			if t := transcripttext.Record(rec); t != "" && size < r.ExcerptBytes {
				entries = append(entries, transcripttext.Entry{Seq: rec.Seq, Text: t})
				size += len(t)
			}
		}
	}
	chunks := transcripttext.Chunks(entries, r.ExcerptBytes)
	if len(chunks) == 0 {
		return "", "", nil
	}
	var b strings.Builder
	for _, e := range chunks[0] {
		fmt.Fprintf(&b, "#%d %s\n", e.Seq, e.Text)
	}
	last := chunks[0][len(chunks[0])-1].Seq
	return fmt.Sprintf("its opening records 1–%d, no summary being stored", last), strings.TrimRight(b.String(), "\n"), nil
}
