package ui

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/memo"
)

// 🔴 SCOPE MAIL'S BROWSER TAB (S5 of `claudedocs/plan-cairn-scope-mail.md`, decisions 10, 12, 15
// and 19): `?tab=memos` on the scope page, plus two gated writes, `POST /memo` and
// `POST /memo/retract`.
//
//   - 🔴 NO NEW READ ROW. The tab is a `?tab=` value on `GET /scope`, read AFTER that handler's
//     uniform refusal, by the scope NAME out of the narrowed answer — so a scope this viewer cannot
//     read never reaches the memo store from here.
//   - 🔴 BOTH WRITE ROWS ARE CLASS 0, so BOTH cross-site gates reach them by METHOD (same origin
//     before authentication, the CSRF token after), exactly as they reach `/ring` and `/team/link`.
//     `TestTheMemoRowsAreBehindBothCrossSiteGates` asserts each gate by its own message.
//   - 🔴 EVERY AUTHORISATION DECISION IS THE STORE'S. `pgstore.MemoStore` asks `memo.MaySend`,
//     `memo.MayRead` and `memo.MayRetract` itself, so a forged POST from a viewer this page showed no
//     form is refused by the same predicate an agent's send is. This file asks the predicates only to
//     decide what to RENDER — the form ([memo.MaySend]) and each retract button
//     ([memo.MayRetractMemo], the ONE retraction rule, Q19) — so the page never offers a control the
//     write refuses, and never becomes a second place the rule lives.
//   - 🔴 TEXT IS RENDERED FROM `memo.Rendered` ONLY (decision 19), THEN ESCAPED BY gomponents' TEXT
//     NODES. Both, because they stop different things: escaping stops markup; the render-side
//     replacement stops invisible and reordering code points escaping leaves intact. Nothing here
//     reaches stored text any other way — `memo.Stored` has no field and no method to reach it by.
//   - Plain text, never Markdown, and no link is rendered from memo content, so `safeHref` has no
//     memo input at all. No script and no new asset.
//
// ⚠ THE UI CANNOT IMPORT `internal/pgstore`: it would put the PostgreSQL driver into the nested
// `uiaudit` module's graph (which imports this package) and move `internal/depspolicy`'s nested
// allowlist. So [Memos] is this package's own interface and `cmd/cairn-ui` adapts the store to it.

// The memo rows' paths, spelled once for the dispatcher, the forms and the tests.
const (
	// MemoPath sends a memo to the scope named by the [FieldScope] form field.
	MemoPath = "/memo"
	// MemoRetractPath retracts one memo. A SEPARATE path rather than an action field on [MemoPath],
	// `UnsharePath`'s ruling: the difference between writing and withdrawing is a route, not a value
	// inside a form body.
	MemoRetractPath = "/memo/retract"
)

// TabMemos is the scope page's memos tab.
const TabMemos = "memos"

// The memo forms' fields. The scope is [FieldScope] (a `control.ID`, as on the share form) and the
// lifetime is [FieldTTLDays] (whole days, as on the team-link form) — one concept, one spelling.
//
// ⚠ NOT [FieldSubject]: that is the share flow's grant SUBJECT (a principal id), and one name for
// two concepts is the one somebody gets wrong.
const (
	FieldMemoSubject = "memo_subject"
	FieldMemoBody    = "memo_body"
	FieldMemoID      = "memo_id"
)

// Memos is what the browser needs of the memo store; `cmd/cairn-ui` adapts `pgstore.MemoStore` to it
// over the SAME authority the authentication chain resolves against.
//
// 🔴 THE STORE ASKS EVERY PREDICATE ITSELF (see the file comment): an implementation that trusted
// its caller to have asked would make this surface the place a refusal is forgotten.
type Memos interface {
	// Model is the authority model, read once per request: it resolves every sender's display and
	// label in [Memos.List], and decision 12's "no longer has write here" mark ([memo.SenderMaySend]).
	Model() control.Model
	// All is EVERY unexpired memo of the scope — live ones AND tombstones (decision 10) — oldest
	// first, in one call. A scope the caller cannot read is [memo.ErrNotFound].
	//
	// 🔴 ONE CALL, NOT PAGES, AND THAT IS A DELETION RATHER THAN A SHORTCUT. This interface used to
	// be the wire's paged listing, and this file paged it with a copy of the client's `memo-read`
	// boundary rule (one second early, ids deduplicated). A caller in the same process as the store
	// needs no pages, so the store answers the whole scope (`pgstore.MemoStore.All`, bounded by what
	// its own send rules can hold) and the paging rule has ONE spelling: `internal/client/memo.go`.
	All(ctx context.Context, m control.Model, auth control.Authorization, scope string) ([]memo.Stored, error)
	// Send and Retract are `pgstore.MemoStore`'s, unchanged: a refusal is a [*memo.Refusal], a miss
	// [memo.ErrNotFound].
	Send(ctx context.Context, caller control.Principal, auth control.Authorization, scope, subject, body string, ttl time.Duration) (int64, time.Time, error)
	Retract(ctx context.Context, caller control.Principal, auth control.Authorization, scope string, id int64) error
}

// NoMemoStore is what the tab says, and what the two writes answer (501), on a deployment with no
// memo store — no database, or no control journal (see `cmd/cairn-ui`). The `NoInviteStore` ruling:
// the rows stay in the ledger on every deployment and say so, rather than appearing with a flag.
const NoMemoStore = "This deployment has no memo store, so no memo can be read or sent here. " +
	"Memos need the surface's database and a control journal."

// MemoRetractionNote is decision 10's "the UI says so": a retraction cannot recall what a session was
// already shown.
const MemoRetractionNote = "Retracting a memo stops further delivery only: a session that was already " +
	"shown it keeps what it saw, and nothing already printed into a session or a transcript is recalled."

// memoFormRefusal is the answer to a memo form that is not well-formed.
const memoFormRefusal = "the memo form was not well-formed"

// The outcomes a memo write redirects back with. A CLOSED set read back by [memoOutcome]; anything
// else in the URL is ignored, so nothing the address bar carries is reflected into the page.
const (
	memoOutcomeSent      = "memo-sent"
	memoOutcomeRetracted = "memo-retracted"
	memoOutcomeRefused   = "memo-refused"
	memoOutcomeGone      = "memo-gone"
	// QueryMemoReason and QueryMemoRule carry a refusal's closed reason and, for a secret, the
	// redaction RULE NAME — never content (`memo.Refusal`).
	QueryMemoReason = "reason"
	QueryMemoRule   = "rule"
)

// memoReasons is the sentence for each refusal reason; a reason outside it renders the generic one.
var memoReasons = map[memo.RefusalReason]string{
	memo.RefusedNoWrite:      "you cannot write this scope, and sending a memo takes write access",
	memo.RefusedQuota:        fmt.Sprintf("this scope has had its %d memos for the day", memo.SendsPerScopePerWindow),
	memo.RefusedLiveLimit:    fmt.Sprintf("this scope already holds %d live memos", memo.MaxLivePerScope),
	memo.RefusedSubjectSize:  fmt.Sprintf("the subject is longer than %d bytes", memo.MaxSubjectBytes),
	memo.RefusedBodySize:     fmt.Sprintf("the text is longer than %d bytes", memo.MaxBodyBytes),
	memo.RefusedCharacters:   "it carries a control, direction or tag character that could disguise text",
	memo.RefusedSecret:       "it looks like it carries a credential",
	memo.RefusedTTL:          fmt.Sprintf("the lifetime must be 1 to %d days", int(memo.MaxTTL/(24*time.Hour))),
	memo.RefusedNotPermitted: "only the sender, or an admin of the scope, may retract it",
}

// MemoTab is the memos tab's answer, built by [Server.memoTab].
type MemoTab struct {
	// NoStore is true when this deployment has no memo store; nothing else is set then.
	NoStore bool
	// CanSend decides the send form: [memo.MaySend] over the viewer's authority.
	CanSend bool
	// Outcome is the banner a write redirected back with, "" for none.
	Outcome string
	// Rows is every unexpired memo of the scope, tombstones included, NEWEST FIRST.
	Rows []MemoRow
	// Draft is what a REFUSED send typed, handed back to the form so the refusal does not cost the
	// text; nil on every GET. Set only by [Server.handleMemoSend], for [memoDraftKept]'s reasons.
	Draft *MemoDraft
}

// MemoDraft is a refused send's own form values, re-rendered into the form it came from.
//
// 🔴 IT IS THE SENDER'S OWN TEXT, BACK TO THE SENDER, IN THE ANSWER TO THE POST THAT CARRIED IT —
// never stored, never in a URL, never shown to anyone else. It is NOT memo text in decision 19's
// sense (nothing was stored), and it reaches the page only as an attribute value and a textarea's
// text node, escaped by gomponents like every other string here.
type MemoDraft struct {
	Subject, Body string
	// TTLDays is the lifetime field as whole days, "" for the form's default.
	TTLDays string
}

// memoDraftKept is the CLOSED set of refusals whose answer re-renders the form with the draft: the
// ones a sender fixes by EDITING AND RESENDING THE SAME TEXT (a size, the lifetime) or by waiting
// (the day's quota, the live bound). A reason outside it redirects as every other outcome does.
//
// 🔴 THE TWO CONTENT REFUSALS ARE DELIBERATELY NOT IN IT. `secret`: the page would re-serve what
// looks like a credential. `characters`: the text carries a code point the render side exists to
// never emit raw, and a textarea would show it as nothing at all. A NEW reason is not kept until
// somebody adds it here — the set fails toward losing a draft, never toward echoing one.
//
// ⚠ WHY THIS EXISTS (#242 round 1): the form's `maxlength` counts UTF-16 code units and
// [memo.CheckSend] counts UTF-8 BYTES, so non-ASCII text the form accepted is refused by the store —
// and the redirect that used to answer every refusal threw the typed text away. No attribute can
// count bytes and this surface runs no script for it, so the limits stay the store's and the
// refusal stops costing the draft; the form's note says the limits are bytes.
var memoDraftKept = map[memo.RefusalReason]bool{
	memo.RefusedSubjectSize: true,
	memo.RefusedBodySize:    true,
	memo.RefusedTTL:         true,
	memo.RefusedQuota:       true,
	memo.RefusedLiveLimit:   true,
}

// MemoLimitsNote is the form's statement of its two size limits, in the unit the store counts.
var MemoLimitsNote = fmt.Sprintf("The subject holds %d bytes and the text %d — bytes, not characters: an accented "+
	"letter, a CJK character or an emoji counts for two to four. A memo over a limit is refused and your text is "+
	"kept in this form.", memo.MaxSubjectBytes, memo.MaxBodyBytes)

// MemoRow is one memo as the tab renders it: the RENDERED type (decision 19) plus two decisions
// asked of the stored memo, which the rendered one cannot answer.
type MemoRow struct {
	memo.Rendered
	// Retractable is [memo.MayRetractMemo] for this viewer, and false for a tombstone.
	Retractable bool
	// SenderLapsed is decision 12's mark: the sender no longer has write on the scope.
	SenderLapsed bool
}

// memoTab builds the tab for `scope`, which the caller has already found in the viewer's narrowed
// answer.
func (s *Server) memoTab(r *http.Request, id identity.Identity, scope Scope) (MemoTab, error) {
	if s.memos == nil {
		return MemoTab{NoStore: true}, nil
	}
	m := s.memos.Model()
	stored, err := s.memos.All(r.Context(), m, id.Auth, scope.Name)
	if errors.Is(err, memo.ErrNotFound) {
		// The scope page found this scope in the narrowed answer, so the store's read predicate
		// should agree; if a refresh moved between the two reads, the tab is empty, never an error
		// page that would say something the scope page did not.
		stored, err = nil, nil
	}
	if err != nil {
		return MemoTab{}, err
	}
	tab := MemoTab{CanSend: memo.MaySend(id.Auth, scope.Name), Outcome: memoOutcome(r.URL.Query())}
	for _, st := range stored {
		row := MemoRow{Rendered: memo.Render(st), SenderLapsed: !memo.SenderMaySend(m, st)}
		row.Retractable = row.Tombstone == "" && memo.MayRetractMemo(id.Principal, id.Auth, st)
		tab.Rows = append(tab.Rows, row)
	}
	// NEWEST FIRST (decision 15). The rendered time is one fixed RFC 3339 UTC layout, so it orders as
	// a string; the id breaks a tie within one second.
	slices.SortStableFunc(tab.Rows, func(a, b MemoRow) int {
		if c := strings.Compare(b.CreatedAt, a.CreatedAt); c != 0 {
			return c
		}
		switch {
		case a.ID > b.ID:
			return -1
		case a.ID < b.ID:
			return 1
		}
		return 0
	})
	return tab, nil
}

// memoOutcome is the banner for a redirect from a memo write, from the CLOSED set only.
func memoOutcome(q url.Values) string {
	switch q.Get(QueryOutcome) {
	case memoOutcomeSent:
		return "Memo sent."
	case memoOutcomeRetracted:
		return "Memo retracted. " + MemoRetractionNote
	case memoOutcomeGone:
		return "That memo is not here any more: it expired, was already retracted, or is not in this scope."
	case memoOutcomeRefused:
		return memoRefusalLine(memo.RefusalReason(q.Get(QueryMemoReason)), q.Get(QueryMemoRule))
	}
	return ""
}

// memoRefusalLine is the ONE sentence for a refused memo write — the redirect's banner and the
// re-rendered form's alike — from the closed reason set, with a secret's RULE NAME when it is one
// of the scan's own.
func memoRefusalLine(reason memo.RefusalReason, rule string) string {
	sentence, ok := memoReasons[reason]
	if !ok {
		return "The memo was refused."
	}
	line := "The memo was refused: " + sentence
	if rule != "" && slices.Contains(memo.ConfidentSecretRules(), rule) {
		line += " (rule: " + rule + ")"
	}
	return line + "."
}

// memoTabHref is the memos tab, with a write's outcome when there is one.
func memoTabHref(scope control.ID, outcome url.Values) string {
	q := url.Values{QueryID: {string(scope)}, QueryTab: {TabMemos}}
	for k, v := range outcome {
		q[k] = v
	}
	return ScopePath + "?" + q.Encode()
}

// memoScope resolves the form's [FieldScope] to a scope in the viewer's NARROWED answer — the
// scope page's own lookup, so a write can only name a scope the tab could have shown. A miss is the
// browse surface's ONE refusal, byte-identical for "not yours" and "does not exist".
func (s *Server) memoScope(w http.ResponseWriter, r *http.Request, id identity.Identity) ([]Scope, Scope, bool) {
	scopes, err := s.source.Visible(id.Auth)
	if err != nil {
		writePlain(w, http.StatusInternalServerError, "the store could not be read")
		return nil, Scope{}, false
	}
	scope, found := pickScope(scopes, control.ID(r.PostFormValue(FieldScope)))
	if !found {
		writePlain(w, http.StatusNotFound, browseRefusal)
		return nil, Scope{}, false
	}
	return scopes, scope, true
}

// memoWriteAnswer redirects a memo write back to the tab: the outcome for a success, a refusal's
// closed reason, the uniform miss — and a 500 for anything else, whose log line names the failure
// and never the memo.
func (s *Server) memoWriteAnswer(w http.ResponseWriter, r *http.Request, scope Scope, done string, err error) {
	q := url.Values{}
	var ref *memo.Refusal
	switch {
	case err == nil:
		q.Set(QueryOutcome, done)
	case errors.As(err, &ref):
		q.Set(QueryOutcome, memoOutcomeRefused)
		q.Set(QueryMemoReason, string(ref.Reason))
		if ref.Rule != "" {
			q.Set(QueryMemoRule, ref.Rule)
		}
	case errors.Is(err, memo.ErrNotFound):
		q.Set(QueryOutcome, memoOutcomeGone)
	default:
		s.logf("memo write failed: %v", err)
		writePlain(w, http.StatusInternalServerError, "the memo store could not be written")
		return
	}
	// 303, not 302: the browser must follow it with a GET (`handleSignIn`'s reason).
	http.Redirect(w, r, memoTabHref(scope.ID, q), http.StatusSeeOther)
}

// handleMemoSend is `POST /memo`. The sender is the AUTHENTICATED principal, never a form field
// (T4); the store asks [memo.MaySend] and runs the content rules and the secret scan.
func (s *Server) handleMemoSend(w http.ResponseWriter, r *http.Request, id identity.Identity) {
	if s.memos == nil {
		writePlain(w, http.StatusNotImplemented, NoMemoStore)
		return
	}
	if err := r.ParseForm(); err != nil {
		writePlain(w, http.StatusBadRequest, memoFormRefusal)
		return
	}
	draft := MemoDraft{Subject: r.PostFormValue(FieldMemoSubject), Body: r.PostFormValue(FieldMemoBody)}
	var ttl time.Duration
	if raw := r.PostFormValue(FieldTTLDays); raw != "" {
		// Any positive whole number of days; the store's `memo.CheckSend` owns the ceiling, so a
		// value past it is refused with the `ttl` reason rather than by a second copy of the rule —
		// and [memo.TTLOf] is the ONE conversion that cannot overflow on the way there, shared with
		// the client listener's `ttl_seconds`.
		days, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || days < 1 {
			writePlain(w, http.StatusBadRequest, memoFormRefusal)
			return
		}
		ttl = memo.TTLOf(days, 24*time.Hour)
		draft.TTLDays = strconv.FormatInt(days, 10)
	}
	scopes, scope, ok := s.memoScope(w, r, id)
	if !ok {
		return
	}
	_, _, err := s.memos.Send(r.Context(), id.Principal, id.Auth, scope.Name, draft.Subject, draft.Body, ttl)
	var ref *memo.Refusal
	if errors.As(err, &ref) && memoDraftKept[ref.Reason] {
		s.memoSendRefusedWithDraft(w, r, id, scopes, scope, ref, draft)
		return
	}
	s.memoWriteAnswer(w, r, scope, memoOutcomeSent, err)
}

// memoRefusedStatus is what a refused send answers when the form is re-rendered with the draft: the
// request was well-formed and understood, and its content was refused.
const memoRefusedStatus = http.StatusUnprocessableEntity

// memoSendRefusedWithDraft answers a refused send by rendering the memos tab ITSELF — the refusal's
// sentence, and the form holding what was typed — instead of redirecting to it ([memoDraftKept]).
//
// 🔴 NOT A REDIRECT, BECAUSE A REDIRECT CANNOT CARRY THE DRAFT WITHOUT PUTTING IT IN A URL, and
// nothing a URL carries is reflected into this page ([memoOutcome]'s closed set). The draft is read
// from the body of a POST that already passed BOTH cross-site gates, so only the sender's own
// browser can make this page show it. The page is the scope page's own ([Server.scopeView]): the
// same narrowed read, the same tab, `no-store` like every HTML answer here. A reload re-submits the
// POST, which the browser asks about first — the cost of not redirecting, accepted for the text.
//
// The tab is built AFTER the refused send, so a viewer whose write lapsed in between gets the tab's
// read-only note and no form: [memo.MaySend] still decides the form, never the draft.
func (s *Server) memoSendRefusedWithDraft(w http.ResponseWriter, r *http.Request, id identity.Identity,
	scopes []Scope, scope Scope, ref *memo.Refusal, draft MemoDraft) {
	view, ok := s.scopeView(w, r, id, scopes, scope, TabMemos)
	if !ok {
		return
	}
	view.Memos.Outcome = memoRefusalLine(ref.Reason, ref.Rule)
	view.Memos.Draft = &draft
	s.renderStatus(w, memoRefusedStatus, ScopePage(view))
}

// handleMemoRetract is `POST /memo/retract`: scope AND id, for the client listener's reason — an id
// alone does not say which scope it was asked under, and the store answers only when they match.
func (s *Server) handleMemoRetract(w http.ResponseWriter, r *http.Request, id identity.Identity) {
	if s.memos == nil {
		writePlain(w, http.StatusNotImplemented, NoMemoStore)
		return
	}
	if err := r.ParseForm(); err != nil {
		writePlain(w, http.StatusBadRequest, memoFormRefusal)
		return
	}
	memoID, err := strconv.ParseInt(r.PostFormValue(FieldMemoID), 10, 64)
	if err != nil || memoID < 1 {
		writePlain(w, http.StatusBadRequest, memoFormRefusal)
		return
	}
	_, scope, ok := s.memoScope(w, r, id)
	if !ok {
		return
	}
	s.memoWriteAnswer(w, r, scope, memoOutcomeRetracted,
		s.memos.Retract(r.Context(), id.Principal, id.Auth, scope.Name, memoID))
}

// memosPanel is the memos tab. Every memo string is a `g.Text` of a `memo.Rendered` field.
func memosPanel(s Scope, t MemoTab, csrf string) g.Node {
	return h.Div(
		h.ID("scope-memos"),
		h.P(h.Class("note"), g.Text("Memos are short messages to the agent sessions working in this scope. "+
			"Each session is shown each memo once, labelled as data from another party — never as an instruction "+
			"from its user.")),
		g.If(t.NoStore, h.P(h.Class("read-only"), g.Text(NoMemoStore))),
		g.If(t.Outcome != "", h.P(h.Class("outcome"), h.Data("memo", "outcome"), g.Text(t.Outcome))),
		g.If(!t.NoStore && t.CanSend && csrf != "", memoSendForm(s, csrf, t.Draft)),
		g.If(!t.NoStore && !t.CanSend, h.P(h.Class("note"), h.Data("memo", "read-only"), g.Text(
			"You can read this scope's memos but not send one: sending takes write access to the scope."))),
		g.If(!t.NoStore, h.P(h.Class("note"), h.Data("memo", "retraction-note"), g.Text(MemoRetractionNote))),
		g.If(!t.NoStore && len(t.Rows) == 0, h.P(h.Class("empty"), g.Text("No memo on this scope right now."))),
		g.If(len(t.Rows) > 0, h.Ul(h.Class("memo-list"), g.Map(t.Rows, func(row MemoRow) g.Node {
			return memoItem(s, row, csrf)
		}))),
	)
}

// memoSendForm is the send form: a POST, for `signOutForm`'s reason. `draft` is a refused send's own
// values ([MemoDraft]), nil for an empty form.
//
// ⚠ `maxlength` IS A CONVENIENCE, NOT THE LIMIT: it counts UTF-16 code units and the store counts
// UTF-8 bytes, so it is exact for ASCII and LOOSE for everything else. [MemoLimitsNote] says so on
// the form, and a refusal keeps the draft ([memoDraftKept]).
func memoSendForm(s Scope, csrf string, draft *MemoDraft) g.Node {
	d := MemoDraft{}
	if draft != nil {
		d = *draft
	}
	if d.TTLDays == "" {
		d.TTLDays = strconv.Itoa(int(memo.DefaultTTL / (24 * time.Hour)))
	}
	return h.FormEl(
		h.Class("invite memo-send"),
		h.Method("post"),
		h.Action(MemoPath),
		h.Input(h.Type("hidden"), h.Name(FieldCSRF), h.Value(csrf)),
		h.Input(h.Type("hidden"), h.Name(FieldScope), h.Value(string(s.ID))),
		h.Label(h.For("memo-subject"), g.Text("Subject")),
		h.Input(h.Type("text"), h.ID("memo-subject"), h.Name(FieldMemoSubject), h.Required(),
			h.MaxLength(strconv.Itoa(memo.MaxSubjectBytes)), h.Value(d.Subject)),
		h.Label(h.For("memo-body"), g.Text("Text")),
		h.Textarea(h.ID("memo-body"), h.Name(FieldMemoBody), h.Rows("5"),
			h.MaxLength(strconv.Itoa(memo.MaxBodyBytes)), g.Text(d.Body)),
		h.Label(h.For("memo-ttl"), g.Text("Expires after (days)")),
		h.Input(h.Type("number"), h.ID("memo-ttl"), h.Name(FieldTTLDays), h.Required(),
			h.Min("1"), h.Max(strconv.Itoa(int(memo.MaxTTL/(24*time.Hour)))),
			h.Value(d.TTLDays)),
		h.P(h.Class("note"), h.Data("memo", "limits"), g.Text(MemoLimitsNote)),
		h.P(h.Class("note"), g.Text("Sent as you; the sender line is the authority's, never what you type. "+
			"A memo that looks like it carries a credential is refused — but only the common token shapes are "+
			"recognised, so do not paste secrets.")),
		h.Button(h.Type("submit"), g.Text("Send memo")),
	)
}

// memoItem is one memo: the sender line, the times, then the subject and text — or the tombstone.
func memoItem(s Scope, row MemoRow, csrf string) g.Node {
	return h.Li(
		h.Class("invite-row"),
		h.Data("memo-id", strconv.FormatInt(row.ID, 10)),
		h.Span(h.Class("who"), g.Text(row.Sender)),
		g.If(row.SenderLapsed, h.Span(h.Class("state"), h.Data("memo", "sender-lapsed"),
			g.Text("no longer has write here"))),
		h.Span(h.Class("kind"), g.Text("sent "+row.CreatedAt+" · expires "+row.ExpiresAt)),
		g.If(row.Tombstone != "", h.P(h.Class("link-targets"), h.Data("memo", "tombstone"), g.Text(row.Tombstone))),
		g.If(row.Tombstone == "", h.P(h.Class("link-targets"),
			h.Strong(h.Data("memo", "subject"), g.Text(row.Subject)))),
		g.If(row.Tombstone == "", h.P(h.Class("link-targets"), h.Data("memo", "body"), g.Text(row.Body))),
		g.If(row.Retractable && csrf != "", h.FormEl(
			h.Class("revoke"),
			h.Method("post"),
			h.Action(MemoRetractPath),
			h.Input(h.Type("hidden"), h.Name(FieldCSRF), h.Value(csrf)),
			h.Input(h.Type("hidden"), h.Name(FieldScope), h.Value(string(s.ID))),
			h.Input(h.Type("hidden"), h.Name(FieldMemoID), h.Value(strconv.FormatInt(row.ID, 10))),
			h.Button(h.Type("submit"), g.Text("Retract")),
		)),
	)
}
