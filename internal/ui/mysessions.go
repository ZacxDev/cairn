package ui

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/transcript"
)

// 🔴 "MY SESSIONS" (S7 of `claudedocs/plan-cairn-plugins.md`, operator decision O10): the stored
// sessions the VIEWER uploaded, newest upload first, each linking its session page. It is the one way
// in to an owner-only session — a session with no recorded write or read (clause g) is named by no
// trailer, so no scope tab, no sessions list and no arc chip can reach it.
//
// 🔴 IT ASKS `transcript.Store.Mine` AND NOTHING ELSE, AND `Mine` IS THE OWNER ARM ONLY — ownership,
// never visibility. A reader of every scope of somebody else's session may OPEN it from a link (the
// session page asks `transcript.Store.View`), but it is not listed on that reader's page: the page is
// "mine", not "what I can see". A narrowed credential lists nothing (the owner arm refuses it).
//
// 🔴 BOUNDED BY PAGES, BECAUSE THE STORE CAN HOLD THOUSANDS OF SESSIONS. [mySessionsPerPage] rows a
// page, `?page=N` selects one; an unrecognised value is page one and a page past the end is the last,
// so every request renders at most one page of rows.
//
// 🔴 NO TRANSCRIPT STORE IS AN EMPTY STATE, NOT A 404 — S4's convention: a deployment started without
// `-transcript-dir` answers what a deployment with nothing stored answers. `GET /session/transcript`
// named nothing gets the navigation page (a 200) on either, and the session page renders no section
// on either; this page has no operand to miss, so its answer is the empty state, BYTE-IDENTICAL on
// the two (`TestMySessionsWithNoStoreIsTheEmptyState`). The empty sentence names both causes rather
// than telling them apart.
//
// Every href is this surface's own ledger paths with encoded operands (`sessionHref`,
// [mySessionsPageHref]); session ids, runtimes and host labels are uploader text and render through
// `g.Text` and attribute values only. No script.

// MySessionsPath is the viewer's own stored sessions.
const MySessionsPath = "/my-sessions"

// QueryPage selects a page of a paged list (`/my-sessions?page=N`). A view selector, never an
// authority input.
const QueryPage = "page"

// mySessionsPerPage bounds one page.
const mySessionsPerPage = 50

// mySessionsEmpty is the empty state's sentence — one for "you uploaded nothing", "this deployment
// stores no transcripts" and "your credential is narrowed", which the page does not tell apart.
const mySessionsEmpty = "No stored session transcript is listed for this credential: none was uploaded under it, " +
	"this deployment stores none, or the credential is narrowed — a narrowed credential lists none, even its owner's."

// MySessions is one page of the viewer's owned sessions.
type MySessions struct {
	Sessions []transcript.Owned
	// Total is how many the viewer owns; Page and Pages say where this page is (Pages is 0 when
	// Total is).
	Total, Page, Pages int
}

// handleMySessions renders one page of the viewer's own stored sessions.
//
// ⚠ `Visible` IS ASKED FIRST, for `handleSessionsPage`'s reason: the shell is built from it. The
// listing itself is the transcript store's owner arm, read off the request's own identity.
func (s *Server) handleMySessions(w http.ResponseWriter, r *http.Request, id identity.Identity) {
	scopes, err := s.source.Visible(id.Auth)
	if err != nil {
		writePlain(w, http.StatusInternalServerError, "the store could not be read")
		return
	}
	view := PageView{Viewer: id.Principal.Display, CSRF: csrfTokenFor(r), Scopes: scopes, Now: s.now(), App: s.app}
	mine, err := s.transcripts.Mine(transcript.Viewer{Principal: id.Principal, Auth: id.Auth})
	if err != nil {
		fmt.Fprintf(s.log, "ui: the transcript store could not be listed: %v\n", err)
		writePlain(w, http.StatusInternalServerError, "the transcript store could not be read")
		return
	}
	if mine.Unreadable > 0 {
		fmt.Fprintf(s.log, "ui: %d stored transcript session(s) have a meta.json that could not be read or parsed; listed for nobody\n",
			mine.Unreadable)
	}
	page := pageOf(mine.Sessions, r.URL.Query().Get(QueryPage))
	// Each row's one-line plugin summary (`outputs.go`) — for the page's rows only. Every row here is
	// the viewer's own, so the predicate's owner arm admits each and no store walk is made.
	listed := make([]string, 0, len(page.Sessions))
	for _, o := range page.Sessions {
		listed = append(listed, o.Root)
	}
	view.Derived = s.derivedFor(id, listed)
	s.render(w, MySessionsPage(view, page))
}

// pageOf cuts one page out of the whole sorted list. `raw` is `?page=`: not a positive integer —
// a wider one than `int` included — is page one, and past the end is the last page.
func pageOf(all []transcript.Owned, raw string) MySessions {
	out := MySessions{Total: len(all), Page: 1}
	if len(all) == 0 {
		return out
	}
	out.Pages = (len(all) + mySessionsPerPage - 1) / mySessionsPerPage
	if n, err := strconv.Atoi(raw); err == nil && n > 1 {
		out.Page = min(n, out.Pages)
	}
	from := (out.Page - 1) * mySessionsPerPage
	out.Sessions = all[from:min(from+mySessionsPerPage, len(all))]
	return out
}

// mySessionsPageHref is page `n` of the list; page one is the bare path.
func mySessionsPageHref(n int) string {
	if n <= 1 {
		return MySessionsPath
	}
	return MySessionsPath + "?" + url.Values{QueryPage: []string{strconv.Itoa(n)}}.Encode()
}

// MySessionsPage renders one page of the viewer's own stored sessions.
func MySessionsPage(v PageView, mine MySessions) g.Node {
	ids := make([]string, 0, len(mine.Sessions))
	for _, o := range mine.Sessions {
		ids = append(ids, o.Root)
	}
	labels := shortIDsIn(ids)
	return shell("my sessions", v, []crumb{{Label: "my sessions"}},
		h.Section(
			h.Class("card"),
			h.ID("my-sessions"),
			h.H2(g.Text("My sessions")),
			h.P(h.Class("card-stats"),
				stat(plural(mine.Total, "stored session", "stored sessions"),
					"every session whose transcript was uploaded under your sign-in, newest upload first — including "+
						"those only you can open"),
				g.If(mine.Pages > 1, stat("page "+strconv.Itoa(mine.Page)+" of "+strconv.Itoa(mine.Pages), "")),
			),
			g.If(mine.Total == 0, h.P(h.Class("empty"), g.Text(mySessionsEmpty))),
			g.If(len(mine.Sessions) > 0, h.Ul(h.Class("entry-list"), h.ID("my-sessions-list"),
				g.Map(mine.Sessions, func(o transcript.Owned) g.Node { return mySessionRow(o, labels[o.Root], v) }))),
			mySessionsPager(mine),
		),
	)
}

// mySessionRow is one owned session: its session page's link, what it was uploaded as and from, and
// when it was last uploaded to.
func mySessionRow(o transcript.Owned, label string, v PageView) g.Node {
	return h.Li(
		h.Class("entry-row session-row"),
		h.Data("session", o.Root),
		h.Span(h.Class("ref"), h.TitleAttr(o.Root),
			h.A(h.Class("row-link"), h.Href(sessionHref(o.Root)), g.Text(label))),
		h.Span(h.Class("title"), g.Text(o.Runtime+" · from "+o.Host)),
		dateAgo(o.UpdatedAt.UTC().Format("2006-01-02"), v.Now),
		v.Derived.line(o.Root),
	)
}

// mySessionsPager links the previous and next page, and renders nothing for a list that fits one.
func mySessionsPager(mine MySessions) g.Node {
	if mine.Pages <= 1 {
		return nil
	}
	var links []g.Node
	if mine.Page > 1 {
		links = append(links, h.A(h.Href(mySessionsPageHref(mine.Page-1)), g.Text("← newer")))
	}
	if mine.Page < mine.Pages {
		links = append(links, h.A(h.Href(mySessionsPageHref(mine.Page+1)), g.Text("older →")))
	}
	return h.Nav(h.Class("pager"), g.Attr("aria-label", "my sessions pages"), g.Group(links))
}
