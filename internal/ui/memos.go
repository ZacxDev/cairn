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
	// List is the scope's unexpired memos — live ones AND tombstones (decision 10) — created strictly
	// after `after`, oldest first, at most `limit`. A scope the caller cannot read is
	// [memo.ErrNotFound].
	List(ctx context.Context, m control.Model, auth control.Authorization, scope string, after time.Time, limit int) ([]memo.Stored, error)
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
}

// MemoRow is one memo as the tab renders it: the RENDERED type (decision 19) plus two decisions
// asked of the stored memo, which the rendered one cannot answer.
type MemoRow struct {
	memo.Rendered
	// Retractable is [memo.MayRetractMemo] for this viewer, and false for a tombstone.
	Retractable bool
	// SenderLapsed is decision 12's mark: the sender no longer has write on the scope.
	SenderLapsed bool
}

// memoListPage is one listing page: the store caps a page at the live bound.
const memoListPage = memo.MaxLivePerScope

// memoListMaxPages bounds the paging loop. A listing WITH tombstones holds at most
// `SendsPerScopePerWindow` a day for `MaxTTL` (the quota is per scope), so this is that bound in
// pages, plus slack — a store that returned more is refused rather than silently truncated.
const memoListMaxPages = memo.SendsPerScopePerWindow*int(memo.MaxTTL/memo.QuotaWindow)/memoListPage + 4

// listAllMemos pages the scope's memos until the store runs out.
//
// 🔴 THE PAGE BOUNDARY IS ONE SECOND EARLY ON PURPOSE, AND IDS ALREADY HELD ARE DROPPED — the S3
// client's `memo-read` rule (`internal/client/memo.go`), for its reason: `memo.Rendered`'s time is
// whole seconds, so a page's last time is ≤ the true one, and asking strictly after it would skip a
// memo sharing that second. Two spellings of one paging rule in two layers — the client's is over
// HTTP and this one is in process — and stated rather than hidden.
func listAllMemos(ctx context.Context, store Memos, m control.Model, auth control.Authorization, scope string) ([]memo.Stored, error) {
	var (
		out   []memo.Stored
		seen  = map[int64]bool{}
		after time.Time
	)
	for page := 0; page < memoListMaxPages; page++ {
		got, err := store.List(ctx, m, auth, scope, after, memoListPage)
		if err != nil {
			return nil, err
		}
		added := 0
		for _, st := range got {
			id := memo.Render(st).ID
			if seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, st)
			added++
		}
		if len(got) < memoListPage || added == 0 {
			return out, nil
		}
		last, err := time.Parse(time.RFC3339, memo.Render(got[len(got)-1]).CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("a memo's created time is not RFC 3339: %w", err)
		}
		after = last.Add(-time.Second)
	}
	return nil, fmt.Errorf("the memo listing did not end within %d pages", memoListMaxPages)
}

// memoTab builds the tab for `scope`, which the caller has already found in the viewer's narrowed
// answer.
func (s *Server) memoTab(r *http.Request, id identity.Identity, scope Scope) (MemoTab, error) {
	if s.memos == nil {
		return MemoTab{NoStore: true}, nil
	}
	m := s.memos.Model()
	stored, err := listAllMemos(r.Context(), s.memos, m, id.Auth, scope.Name)
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
		reason, ok := memoReasons[memo.RefusalReason(q.Get(QueryMemoReason))]
		if !ok {
			return "The memo was refused."
		}
		line := "The memo was refused: " + reason
		if rule := q.Get(QueryMemoRule); rule != "" && slices.Contains(memo.ConfidentSecretRules(), rule) {
			line += " (rule: " + rule + ")"
		}
		return line + "."
	}
	return ""
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
func (s *Server) memoScope(w http.ResponseWriter, r *http.Request, id identity.Identity) (Scope, bool) {
	scopes, err := s.source.Visible(id.Auth)
	if err != nil {
		writePlain(w, http.StatusInternalServerError, "the store could not be read")
		return Scope{}, false
	}
	scope, found := pickScope(scopes, control.ID(r.PostFormValue(FieldScope)))
	if !found {
		writePlain(w, http.StatusNotFound, browseRefusal)
		return Scope{}, false
	}
	return scope, true
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
	var ttl time.Duration
	if raw := r.PostFormValue(FieldTTLDays); raw != "" {
		// Any positive whole number of days; the store's `memo.CheckSend` owns the ceiling, so a
		// value past it is refused with the `ttl` reason rather than by a second copy of the rule.
		days, err := strconv.Atoi(raw)
		if err != nil || days < 1 {
			writePlain(w, http.StatusBadRequest, memoFormRefusal)
			return
		}
		// Past the ceiling is handed on as just past it — `clientapi`'s `ttlFrom` shape — so a huge
		// value cannot overflow the multiplication and still meets the store's own refusal.
		ttl = memo.MaxTTL + 24*time.Hour
		if days <= int(memo.MaxTTL/(24*time.Hour)) {
			ttl = time.Duration(days) * 24 * time.Hour
		}
	}
	scope, ok := s.memoScope(w, r, id)
	if !ok {
		return
	}
	_, _, err := s.memos.Send(r.Context(), id.Principal, id.Auth, scope.Name,
		r.PostFormValue(FieldMemoSubject), r.PostFormValue(FieldMemoBody), ttl)
	s.memoWriteAnswer(w, r, scope, memoOutcomeSent, err)
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
	scope, ok := s.memoScope(w, r, id)
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
		g.If(!t.NoStore && t.CanSend && csrf != "", memoSendForm(s, csrf)),
		g.If(!t.NoStore && !t.CanSend, h.P(h.Class("note"), h.Data("memo", "read-only"), g.Text(
			"You can read this scope's memos but not send one: sending takes write access to the scope."))),
		g.If(!t.NoStore, h.P(h.Class("note"), h.Data("memo", "retraction-note"), g.Text(MemoRetractionNote))),
		g.If(!t.NoStore && len(t.Rows) == 0, h.P(h.Class("empty"), g.Text("No memo on this scope right now."))),
		g.If(len(t.Rows) > 0, h.Ul(h.Class("memo-list"), g.Map(t.Rows, func(row MemoRow) g.Node {
			return memoItem(s, row, csrf)
		}))),
	)
}

// memoSendForm is the send form: a POST, for `signOutForm`'s reason.
func memoSendForm(s Scope, csrf string) g.Node {
	return h.FormEl(
		h.Class("invite memo-send"),
		h.Method("post"),
		h.Action(MemoPath),
		h.Input(h.Type("hidden"), h.Name(FieldCSRF), h.Value(csrf)),
		h.Input(h.Type("hidden"), h.Name(FieldScope), h.Value(string(s.ID))),
		h.Label(h.For("memo-subject"), g.Text("Subject")),
		h.Input(h.Type("text"), h.ID("memo-subject"), h.Name(FieldMemoSubject), h.Required(),
			h.MaxLength(strconv.Itoa(memo.MaxSubjectBytes))),
		h.Label(h.For("memo-body"), g.Text("Text")),
		h.Textarea(h.ID("memo-body"), h.Name(FieldMemoBody), h.Rows("5"),
			h.MaxLength(strconv.Itoa(memo.MaxBodyBytes))),
		h.Label(h.For("memo-ttl"), g.Text("Expires after (days)")),
		h.Input(h.Type("number"), h.ID("memo-ttl"), h.Name(FieldTTLDays), h.Required(),
			h.Min("1"), h.Max(strconv.Itoa(int(memo.MaxTTL/(24*time.Hour)))),
			h.Value(strconv.Itoa(int(memo.DefaultTTL/(24*time.Hour))))),
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
