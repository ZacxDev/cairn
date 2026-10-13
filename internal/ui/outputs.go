package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/plugins"
	"github.com/ZacxDev/cairn/internal/store"
	"github.com/ZacxDev/cairn/internal/transcript"
	"github.com/ZacxDev/cairn/internal/write"
)

// 🔴 PLUGIN OUTPUTS ON THE BROWSER SURFACE, AND THE OWNER'S "DELETE TRANSCRIPT" — S6 of
// `claudedocs/plan-cairn-plugins.md` (decisions 11, 14 and 15; clauses k and l).
//
// 🔴 AN OUTPUT IS A CLAIM, AND IT IS RENDERED AS ONE (decision 11). Every output sits inside a block
// labelled `derived` that states the plugin's NAME and VERSION (the pod stamped both from the token's
// registry entry — a plugin cannot claim to be another), the MODEL it says it used, when the pod
// accepted it, and its WATERMARK: "from turns 1–N of M", and "K newer turns not yet summarised" once
// the transcript has grown past it. Nothing a plugin wrote is rendered as part of the session's own
// record, and nothing here says an output is true.
//
// 🔴 IT IS SHOWN TO EXACTLY THE PRINCIPALS WHO CAN SEE THE TRANSCRIPT IT WAS DERIVED FROM (clause k)
// — and that is not re-derived here. An output is read only through a `transcript.View`, and a View
// exists only where `transcript.Visible` said yes: the session page's comes from `Store.View` (the
// ONE call that also decides the transcript section), a session ROW's from `Store.ViewsWithOutputs`.
// So a viewer the predicate refuses gets, on the page and on every row, the bytes a session with no
// output gets.
//
// 🔴 EVERY BYTE OF AN OUTPUT IS SOMEBODY'S TEXT — a model's words, or a plugin's — AND IT IS RENDERED
// AS TEXT: `g.Text` and attribute values only, no Markdown. NO LINK IS BUILT FROM AN OUTPUT, with ONE
// exception: a ClickUp task link, built by the entry pages' own `store.RefURL` template from a ticket
// id that `plugins.DecodeBody` validated as CANONICAL (alphanumeric — it can carry no scheme, no `/`
// and no markup) and then passed through `safeHref` like every other external URL on this surface.
// A stored body that does NOT validate against this build's schema is shown as its stored text, with
// no link at all.
//
// 🔴 DELETION IS THE OWNER'S, AND IT CASCADES (clause l). `POST /session/delete` asks the transcript
// store's OWNER ARM and nothing else; the store removes the session's directory — the transcript,
// every blob, and every output and ticket edge derived from it, which live in that directory — and
// journals the fact. A reader of every scope the session touched can SEE it and cannot delete it.

// SessionDeletePath is the owner's "delete transcript" row. A FIXED path with the session in a form
// field, and its own path rather than an action on another row — `UnsharePath`'s ruling.
const SessionDeletePath = "/session/delete"

// sessionDeleteRefusal is the delete row's ONE authority refusal: no transcript by that id, a
// transcript that is somebody else's, a narrowed credential, an id that is no session id, and a
// deployment with no transcript store.
const sessionDeleteRefusal = "no such transcript, or it is not yours to delete"

// The derived section's display bounds. Storage is untouched by both.
const (
	// maxDerivedBlocks bounds how many output blocks ONE session page renders; the rest are counted.
	maxDerivedBlocks = 60
	// derivedLineRunes bounds a session row's one-line summary.
	derivedLineRunes = 160
)

// DerivedBlock is one stored output as the session page renders it.
type DerivedBlock struct {
	Output plugins.Output
	// Body is the output's body, decoded and VALIDATED by `plugins.DecodeBody`; Valid is false when
	// the stored body does not validate against this build's schema, and then only Raw is shown.
	Body  plugins.Body
	Valid bool
	Raw   string
	// Watermark is the sentence saying what the output was derived FROM.
	Watermark string
}

// DerivedView is the session page's "derived by plugins" section.
type DerivedView struct {
	Session string
	Blocks  []DerivedBlock
	// Superseded counts a plugin's EARLIER summaries: each new summary is written over the new
	// records plus the plugin's own prior one, so only the latest is shown.
	Superseded int
	// More counts outputs past [maxDerivedBlocks].
	More int
}

// storedOutput decodes one `outputs.jsonl` line. ok is false for a line this build cannot read at
// all — it is skipped, never guessed at.
func storedOutput(line []byte) (plugins.Output, bool) {
	var o plugins.Output
	if err := json.Unmarshal(line, &o); err != nil || o.ID == "" || !plugins.ValidName(o.Plugin) || !plugins.IsOutputType(o.Type) {
		return plugins.Output{}, false
	}
	return o, true
}

// derivedOf builds the section from a View — a stored session the ONE predicate already admitted
// this viewer to — and the reading of its transcript (for "turns 1–N of M"). Nil when the session
// holds no output.
func derivedOf(view *transcript.View, rd transcript.Reading) (*DerivedView, error) {
	out := &DerivedView{Session: view.Meta.Root}
	latest := map[string]plugins.Output{} // plugin → its latest summary
	scans := map[string]plugins.Output{}  // plugin → its latest ticket scan
	seen := map[string]int{}              // an edge or suggestion's identity → its index in `rest`
	var rest []plugins.Output
	err := view.Outputs(func(line []byte) error {
		o, ok := storedOutput(line)
		if !ok {
			return nil
		}
		if o.Type == plugins.TypeSummary {
			if _, had := latest[o.Plugin]; had {
				out.Superseded++
			}
			latest[o.Plugin] = o
			return nil
		}
		// A scan says how far a matcher has examined the session; every pass writes one, and only
		// the latest says anything the earlier ones do not.
		if o.Type == plugins.TypeTicketScan {
			scans[o.Plugin] = o
			return nil
		}
		// A plugin that re-derives the same edge on every run writes the same body again: shown once,
		// as its latest.
		key := o.Plugin + "\x00" + o.Type + "\x00" + string(o.Body)
		if i, dup := seen[key]; dup {
			rest[i] = o
			return nil
		}
		seen[key] = len(rest)
		rest = append(rest, o)
		return nil
	})
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(latest))
	for name := range latest {
		names = append(names, name)
	}
	sort.Strings(names)
	all := make([]plugins.Output, 0, len(names)+len(rest))
	for _, name := range names {
		all = append(all, latest[name])
	}
	// Scans BEFORE the edges and suggestions: the page shows a bounded number of blocks, and "how far
	// this was examined" must not be the block a long list of edges cuts off.
	scanners := make([]string, 0, len(scans))
	for name := range scans {
		scanners = append(scanners, name)
	}
	sort.Strings(scanners)
	for _, name := range scanners {
		all = append(all, scans[name])
	}
	all = append(all, rest...)
	if len(all) == 0 {
		return nil, nil
	}
	if len(all) > maxDerivedBlocks {
		out.More = len(all) - maxDerivedBlocks
		all = all[:maxDerivedBlocks]
	}
	for _, o := range all {
		b := DerivedBlock{Output: o, Raw: string(o.Body), Watermark: watermarkSentence(o, rd)}
		if body, err := plugins.DecodeBody(o.Type, o.Body); err == nil {
			b.Body, b.Valid = body, true
		}
		out.Blocks = append(out.Blocks, b)
	}
	return out, nil
}

// watermarkSentence says what an output was derived from (decision 11): "from turns 1–N of M", and
// "K newer turns not yet summarised" when the transcript has grown past the watermark. The turns are
// the MAIN stream's — the conversation; a watermark that names no `main` is stated by stream and
// record number instead.
func watermarkSentence(o plugins.Output, rd transcript.Reading) string {
	seq, hasMain := o.InputWatermark["main"]
	if !hasMain {
		names := make([]string, 0, len(o.InputWatermark))
		for name := range o.InputWatermark {
			names = append(names, name)
		}
		sort.Strings(names)
		parts := make([]string, 0, len(names))
		for _, name := range names {
			parts = append(parts, name+" through record "+strconv.FormatInt(o.InputWatermark[name], 10))
		}
		return "from " + strings.Join(parts, ", ")
	}
	n, m := rd.TurnsThrough(seq), rd.TotalTurns
	var s string
	switch n {
	case 0:
		s = "from no turn of " + strconv.Itoa(m)
	case 1:
		s = "from turn 1 of " + strconv.Itoa(m)
	default:
		s = "from turns 1–" + strconv.Itoa(n) + " of " + strconv.Itoa(m)
	}
	if m > n {
		verb := "examined"
		if o.Type == plugins.TypeSummary {
			verb = "summarised"
		}
		s += "; " + plural(m-n, "newer turn", "newer turns") + " not yet " + verb
	}
	return s
}

// typeWords is an output type as a reader sees it.
var typeWords = map[string]string{
	plugins.TypeSummary:          "summary",
	plugins.TypeTicketEdge:       "ticket edge",
	plugins.TypeTicketSuggestion: "ticket suggestion",
	plugins.TypeTicketScan:       "ticket scan",
}

// signalWords is decision 14's closed signal set, as words.
var signalWords = map[string]string{
	"branch-name":    "branch name",
	"commit-message": "commit message",
	"commit-trailer": "commit trailer",
	"pr-body":        "PR body",
	"pr-link":        "PR link",
	"transcript-url": "URL in the transcript",
	"entry-ref":      "entry ref",
}

// derivedSection renders the section. Every value is `g.Text` or an attribute.
func derivedSection(d DerivedView) g.Node {
	return h.Section(
		h.Class("card"),
		h.ID("derived"),
		h.Div(h.Class("card-head"),
			h.H2(g.Text("Derived by plugins")),
			h.Span(h.Class("entry-count"), g.Text(plural(len(d.Blocks)+d.More, "output", "outputs"))),
		),
		h.P(h.Class("note"), g.Text("Each block below is a CLAIM a plugin derived from this session's transcript — it is "+
			"not part of the session's record, and nothing here checked it. It is shown to exactly the people who can "+
			"see the transcript, and it is deleted with it.")),
		g.Map(d.Blocks, func(b DerivedBlock) g.Node { return derivedBlock(d.Session, b) }),
		g.If(d.Superseded > 0, h.P(h.Class("t-line"), h.Data("superseded", strconv.Itoa(d.Superseded)),
			g.Text(plural(d.Superseded, "earlier summary is", "earlier summaries are")+
				" stored and not shown: each summary is written over the plugin's previous one."))),
		g.If(d.More > 0, h.P(h.Class("t-line"),
			g.Text(plural(d.More, "more output is", "more outputs are")+" stored and not shown on this page."))),
	)
}

// derivedLabel is the line every output carries: `derived`, what it is, by which plugin and version,
// with which model, when, and from what.
func derivedLabel(b DerivedBlock) string {
	o := b.Output
	return "derived · " + typeWords[o.Type] + " · by " + o.Plugin + " " + o.PluginVersion + " · model " + plainText(o.Model) +
		" · " + o.ProducedAt + " · " + b.Watermark
}

func derivedBlock(session string, b DerivedBlock) g.Node {
	o := b.Output
	var body g.Node
	switch {
	case !b.Valid:
		body = g.Group([]g.Node{
			h.P(h.Class("t-line"), g.Text("This output's stored body does not match what this version of cairn accepts "+
				"for its type, so it is shown as stored and nothing in it is linked.")),
			h.Pre(h.Class("t-pre"), g.Text(plainText(b.Raw))),
		})
	case b.Body.Summary != nil:
		body = h.Div(h.Class("t-text"), g.Text(plainText(b.Body.Summary.Text)))
	case b.Body.Edge != nil:
		e := b.Body.Edge
		body = h.P(h.Class("t-line"),
			g.Text("ticket "), ticketNode(e.TicketID),
			g.If(e.CustomID != "", g.Text(" ("+e.CustomID+")")),
			g.If(e.Title != "", g.Text(" — "+e.Title)),
			g.Text(" · matched on "+signalWords[e.Signal]+" · "), locatorNode(session, e.Locator))
	case b.Body.Scan != nil:
		// 🔴 A SCAN IS NOT A TICKET AND NOT A SUGGESTION: it says a matcher EXAMINED the session through
		// the watermark in its label, what that pass wrote, and whether a model was asked — so "no
		// suggestion" from a model that found none is told apart from a pass that asked nobody.
		sc := b.Body.Scan
		asked := "no model was asked"
		if sc.ModelAsked {
			asked = "a model was asked"
		}
		body = h.P(h.Class("t-line"), g.Text("examined for tickets · this pass wrote "+
			plural(sc.Edges, "ticket edge", "ticket edges")+" and "+plural(sc.Suggestions, "suggestion", "suggestions")+" · "+asked))
	case b.Body.Suggestion != nil:
		sg := b.Body.Suggestion
		body = g.Group([]g.Node{
			h.P(h.Class("t-line"),
				h.Span(h.Class("badge badge-quiet"), g.Text("suggested")),
				g.Text(" ticket "), ticketNode(sg.TicketID),
				g.If(sg.Title != "", g.Text(" — "+sg.Title)),
				g.Text(" · confidence "+strconv.FormatFloat(sg.Confidence, 'f', 2, 64)+" · not a match")),
			g.If(sg.Rationale != "", h.Div(h.Class("t-text"), g.Text(plainText(sg.Rationale)))),
		})
	}
	return h.Article(
		h.Class("turn"),
		h.Data("output", o.ID), h.Data("type", o.Type), h.Data("plugin", o.Plugin),
		h.P(h.Class("t-label"), g.Text(derivedLabel(b))),
		body,
	)
}

// ticketNode is a CANONICAL ClickUp task id, linked through the entry pages' own ref template.
//
// 🔴 THE ID WAS VALIDATED BEFORE THIS IS REACHED (`plugins.DecodeBody` — an unvalidated body never
// gets here), THE URL IS THE TEMPLATE'S, AND IT STILL GOES THROUGH `safeHref`: one path to an
// external href on this surface, the entry pages' ruling. A ref the template resolves to nothing
// renders as text.
func ticketNode(id string) g.Node {
	url, _ := store.RefURL(store.TaskRef{System: "clickup", Ident: id, Raw: "clickup:" + id}, func(string) string { return "" })
	href, ok := safeHref(url)
	if !ok {
		return g.Text("clickup:" + id)
	}
	return h.A(h.Href(href), g.Text("clickup:"+id))
}

// locatorNode says WHERE a deterministic match was found. A transcript record links this surface's
// own raw-record row (a ledger path with encoded operands); everything else is text.
func locatorNode(session string, l plugins.Locator) g.Node {
	switch {
	case l.Stream != "":
		return h.A(h.Href(transcriptRawHref(session, l.Stream, l.Seq, l.Seq)),
			g.Text(l.Stream+" record "+strconv.FormatInt(l.Seq, 10)))
	case l.Commit != "":
		return g.Text("commit " + l.Commit)
	case l.PR != 0:
		return g.Text("PR #" + strconv.Itoa(l.PR))
	}
	return g.Text("entry " + l.Entry)
}

// DerivedLine is a session row's one-line summary: the session's LATEST stored summary, clipped.
type DerivedLine struct {
	Plugin, Version, Model, ProducedAt string
	Text                               string
}

// derivedLines is "what one-liner does this viewer see for this session", for ONE request's viewer —
// nil when this deployment has no transcript store. Built only by [Server.derivedFor]; read only
// through [derivedLines.line], so a row the predicate shows nothing renders no node at all.
type derivedLines map[string]DerivedLine

// line is the row's node, or NO node.
func (d derivedLines) line(session string) g.Node {
	l, ok := d[session]
	if !ok {
		return nil
	}
	return h.Span(h.Class("title excerpt"), h.Data("derived", l.Plugin),
		h.TitleAttr("derived by the plugin "+l.Plugin+" "+l.Version+" (model "+plainText(l.Model)+") at "+l.ProducedAt+
			" — a claim about this session's transcript, not part of its record"),
		g.Text("derived · "+l.Plugin+" "+l.Version+": "+l.Text))
}

// plainText is a plugin's text made safe to sit INSIDE a page's own sentences: every Unicode
// BIDIRECTIONAL CONTROL is removed.
//
// 🔴 ESCAPING DOES NOT COVER THIS. `g.Text` neutralises markup; an override or isolate character
// (U+202E and its family) is not markup — it REORDERS how the following characters are displayed,
// across the element's own label, so a summary could make "derived · summary · by …" read as
// something else, or hide what follows it. Model output is exactly where such a character arrives
// unasked. The stored bytes are untouched; only the rendering drops them.
func plainText(text string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069, r == 0x200E, r == 0x200F, r == 0x061C:
			return -1
		}
		return r
	}, text)
}

// oneLine is a summary's text as ONE clipped line.
func oneLine(text string) string {
	text = strings.Join(strings.Fields(plainText(text)), " ")
	if utf8.RuneCountInString(text) <= derivedLineRunes {
		return text
	}
	n := 0
	for i := range text {
		if n == derivedLineRunes {
			return text[:i] + "…"
		}
		n++
	}
	return text
}

// derivedFor is the ONE question a page that lists sessions asks about their outputs: for each of
// `sessions`, the one-liner this viewer may see.
//
// 🔴 THROUGH `transcript.Store.ViewsWithOutputs`, WHICH ENDS IN THE ONE PREDICATE — a session whose
// transcript this viewer cannot see contributes nothing, so its row is the row of a session with no
// output. A failure to decide is logged and shows nothing, for `transcriptFor`'s reason.
func (s *Server) derivedFor(id identity.Identity, sessions []string) derivedLines {
	if s.transcripts == nil || len(sessions) == 0 {
		return nil
	}
	views, err := s.transcripts.ViewsWithOutputs(transcript.Viewer{Principal: id.Principal, Auth: id.Auth}, sessions)
	if err != nil {
		fmt.Fprintf(s.log, "ui: the sessions' derived lines could not be decided, so none is shown: %v\n", err)
		return nil
	}
	out := derivedLines{}
	for root, view := range views {
		var last *plugins.Output
		err := view.Outputs(func(line []byte) error {
			if o, ok := storedOutput(line); ok && o.Type == plugins.TypeSummary {
				last = &o
			}
			return nil
		})
		if err != nil || last == nil {
			continue
		}
		body, err := plugins.DecodeBody(last.Type, last.Body)
		if err != nil {
			continue
		}
		out[root] = DerivedLine{Plugin: last.Plugin, Version: last.PluginVersion, Model: last.Model,
			ProducedAt: last.ProducedAt, Text: oneLine(body.Summary.Text)}
	}
	return out
}

// deleteForm is the owner's "delete transcript" control. NO SCRIPT: it is COLLAPSED in a `<details>`,
// so deleting takes two deliberate acts — opening the reveal, which states what goes, and pressing
// the button under it — and a slip on the page presses nothing.
//
// ⚠ NO CONFIRMATION FIELD. A "tick to confirm" checkbox was built first and removed: it is a third
// control with a touch target of its own, and it guards nothing the reveal does not (the cross-site
// gates, not a checkbox, are what stop a request the owner did not make).
func deleteForm(session, csrf string) g.Node {
	return h.Details(h.Class("t-block"), h.ID("transcript-delete"),
		h.Summary(g.Text("Delete this transcript")),
		h.P(h.Class("note"), g.Text("Deletes this session's stored transcript from this deployment for everyone: every "+
			"stored record and file, and every plugin output and ticket edge derived from it. It cannot be undone "+
			"here. The session's notes in the store are not touched, and what a plugin already sent to a model "+
			"provider cannot be recalled.")),
		h.FormEl(
			h.Class("invite"), h.Method("post"), h.Action(SessionDeletePath),
			h.Input(h.Type("hidden"), h.Name(FieldCSRF), h.Value(csrf)),
			h.Input(h.Type("hidden"), h.Name(FieldSession), h.Value(session)),
			h.Button(h.Type("submit"), g.Text("Delete transcript")),
		),
	)
}

// handleSessionDelete is `POST /session/delete`: the OWNER deletes a stored transcript.
//
// 🔴 CLASS 0, SO BOTH CROSS-SITE GATES REACH IT BY METHOD (same origin before auth, the CSRF token
// after) — a deletion a third-party page could trigger is the worst write on this surface.
//
// 🔴 THE OWNER ARM, ASKED BY THE STORE (`transcript.Store.Delete`), AND ONE REFUSAL FOR EVERYTHING
// ELSE: not stored, somebody else's, a narrowed credential, not a session id, no transcript store.
// Nothing in the answer says which, so the row cannot be driven to ask what exists.
func (s *Server) handleSessionDelete(w http.ResponseWriter, r *http.Request, id identity.Identity) {
	if err := r.ParseForm(); err != nil {
		writePlain(w, http.StatusBadRequest, "the form could not be read")
		return
	}
	session := r.PostFormValue(FieldSession)
	deleted := false
	if write.SessionComponent.MatchString(session) {
		var err error
		if deleted, err = s.transcripts.Delete(transcript.Viewer{Principal: id.Principal, Auth: id.Auth}, session); err != nil {
			s.logf("transcript: a deletion failed: %v", err)
			writePlain(w, http.StatusInternalServerError, "the transcript store could not be written")
			return
		}
	}
	if !deleted {
		writePlain(w, http.StatusForbidden, sessionDeleteRefusal)
		return
	}
	// Who and which session — the store's journal holds the same fact; never content.
	s.logf("transcript: session %s deleted by its owner %s:%s — its transcript, plugin outputs and ticket edges are gone",
		session, id.Principal.Kind, id.Principal.ID)
	http.Redirect(w, r, MySessionsPath, http.StatusSeeOther)
}
