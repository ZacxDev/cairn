package ui

import (
	"net/http"
	"slices"
	"strings"

	"github.com/ZacxDev/cairn/internal/identity"
)

// 🔴 THE ROUTE LEDGER, AND THE DISPATCHER READS IT — the same shape as
// `api.DeclaredRoutes`, and for the same reason. A path that is not a key below is
// not dispatched, so a route that is not in the ledger cannot exist. A hand-written
// list beside the table is a second spelling that goes stale in the direction
// nobody notices: the ledger reporting full coverage over a set that has grown.
//
// ⚠ THIS IS A DIFFERENT LEDGER FROM THE POD'S, AND NEITHER MOVES THE OTHER.
// `api.DeclaredRoutes()` is the SERVED CONTRACT the conformance corpus replays;
// this one is a browser surface with no corpus. Adding a row here does not touch
// `tests/conformance/requests.json`, and adding one there does not touch this.
type routeKey struct {
	method string
	path   string
}

type handler func(*Server, http.ResponseWriter, *http.Request, identity.Identity)

// routeClass is what a row DECLARES about itself. There are exactly two classes and
// both had to be invented to add the sign-in flow; everything else a route is subject
// to is DERIVED from the method instead — see [Server.ServeHTTP].
//
// 🔴 A CLASS IS A HOLE IN A GUARD BY CONSTRUCTION, WHICH IS WHY THERE ARE ONLY TWO AND
// WHY NEITHER OF THEM IS `csrf`. A per-row opt-in to a security check is a check
// somebody can forget to opt a new row into, and the forgetting is silent. So the CSRF
// token requirement and the same-origin requirement are not classes: they are asked of
// every state-changing request the dispatcher sees, derived from the METHOD, and a new
// row inherits them without anybody remembering. What a class can only do is make a
// route LESS protected, so each one is spelled into the hand-written ledger in
// `routes_test.go` and adding a row means writing its class out by hand.
type routeClass uint8

const (
	// classPublic is dispatched BEFORE the authentication chain runs. Exactly the
	// sign-in pair carries it, because a surface whose only way in is behind its own
	// authentication has no way in.
	classPublic routeClass = 1 << iota
	// classContent renders an answer about the caller's authority, so it MUST consult AN
	// authority before rendering — which one is per route, declared in
	// `contentAuthority` in `routes_test.go`, and a content route missing from that map
	// fails. ⚠ This said "MUST consult `Source.Visible`" while the share flow's content
	// route consults `Sharing` instead: a class doc naming one authority, three lines from
	// where somebody adds the next row.
	classContent
)

type route struct {
	handle handler
	class  routeClass
}

// routes is the dispatch table. It is a package-level var rather than a method so
// that `DeclaredRoutes` derives from the same data the dispatcher uses — there is
// no second copy to disagree with.
//
// 🔴 `HealthPath` IS THE ONE PATH THAT IS NOT A ROW. It is answered before the chain
// runs, it says nothing but "ok", and it is not in the ledger because it serves no
// content. That mirrors `internal/api`, where the health path is likewise outside
// `DeclaredRoutes()`.
//
// 🔴 ONE ROW WAS DELETED HERE FOR A REASON WORTH KEEPING. This table briefly held
// `GET /` beside `GET /entries`: the first rendered the page with NO store read, the
// second over the scopes the credential may see. The first was a measured lie. [Page]
// renders "No scope is visible to this credential. That is an authority answer, not an
// empty store." whenever it is handed an empty slice — and the handler behind `GET /`
// handed it `nil` without ever calling [Source.Visible], so an operator with authority
// over every scope was told, in a sentence whose whole purpose is to distinguish an
// authority answer from an empty one, that they had none. A page that has not asked may
// not answer.
//
// 🔴 THE PUBLIC ROWS ARE PUBLIC BECAUSE A SURFACE WHOSE ONLY WAY IN IS BEHIND ITS OWN
// AUTHENTICATION HAS NO WAY IN. The sign-in pair, the two GitHub rows and the stylesheet
// carry `classPublic`: each is reached before anybody has a credential. `GET /sign-in`
// must answer an anonymous caller or the door is locked from the inside; the GitHub pair
// must, because the flow starts and finishes before a session exists; and the stylesheet
// must, because the sign-in page links it.
//
// 🔴 AND THE "THE URL SPACE IS NOT MAPPABLE" PROPERTY IS GONE — RETRACTED RATHER THAN
// NARROWED AGAIN. This comment said the space was "mappable TO THE EXTENT OF THE PUBLIC
// ROWS" and that the property "still holds in full for every authenticated row". Both
// sentences described a guard that was never buying anything: THIS REPOSITORY IS PUBLIC,
// and the map is this file. Anybody who wants the ledger reads `routes` — or runs
// `ui.DeclaredRoutes()`, which the startup line already counts — so an unknown path is now
// answered **404**, honestly. What survives, and is the half that was always worth its
// cost, is that a BAD CREDENTIAL is answered uniformly: no refusal on this surface says
// which part of a credential was wrong, which is the oracle over the CREDENTIAL TABLE
// rather than over the URL space. ⚠ The gate ORDER incidentally keeps an unauthenticated
// caller from telling most routes from a typo — but NOT the root, which answers a browser
// 303 where a typo gets 401. See [Server.ServeHTTP] for the scope of that one exception.
// 🔴 AND THE SHARE FLOW IS THREE ROWS ON FIXED PATHS, WITH THE SCOPE IN A QUERY
// PARAMETER RATHER THAN IN THE PATH. `routes` is an EXACT-MATCH map, so a path
// parameter (`/share/{scope}`) would mean a prefix match in the dispatcher — and a
// prefix match is a second way for a request to reach a handler, one that
// `TestEveryServedPathComesFromTheLedger` structurally cannot probe, because there is
// no longer a finite set of paths to probe. The query parameter keeps every served
// path a literal key in this map, which is the property the whole ledger rests on.
//
// 🔴 THE BROWSE PAIR IS TWO MORE FIXED PATHS WITH THEIR OPERANDS IN QUERY PARAMETERS,
// FOR THE REASON THE PARAGRAPH ABOVE GIVES AND NOT BY ANALOGY WITH IT. `/scope/{id}` and
// `/entry/{scope}/{ref}` are the obvious spellings and both are refused: each needs a
// prefix match, a prefix match is a second way for a request to reach a handler, and
// `TestEveryServedPathComesFromTheLedger` structurally cannot probe one because there is
// no finite set of paths left to enumerate. `/entry` makes the cost of the alternative
// concrete — the entry REF is a filename stem out of a file somebody else wrote, so a
// path-segment spelling would put unvalidated user text in a path position and make
// `..` a routing question. In a query parameter it is a value the handler matches
// against the narrowed answer and never resolves. See `handleEntryPage`.
var routes = map[routeKey]route{
	// 🔴 THE ROOT IS THE HUB, AND THE SCOPE LIST MOVED TO `/scopes` (an operator decision). The hub is
	// four cards — arcs, scopes, sessions, team — only the scopes card carries a count (`len(Visible)`, an answer
	// this caller is authorised to see); the scope cards, the search box and the tag filter are
	// `/scopes`'s. `/?q=` and `/?tag=` still ANSWER, with a 303 to `/scopes` carrying the query, so a
	// bookmark or a home-screen shortcut is never a dead link. See `handleHub`.
	{"GET", "/"}:       {(*Server).handleHub, classContent},
	{"GET", "/scopes"}: {(*Server).handlePage, classContent},
	// 🔴 THE SESSIONS LIST: every session this caller can see anything of — the SAME predicate the
	// session page is found by (`report.SessionsAcross` and `report.SessionAcross` read one walk), so a
	// listed session's page never 404s and a hidden one is never listed. GET only. See
	// `handleSessionsPage`.
	{"GET", "/sessions"}: {(*Server).handleSessionsPage, classContent},
	{"GET", "/scope"}:    {(*Server).handleScopePage, classContent},
	{"GET", "/entry"}:    {(*Server).handleEntryPage, classContent},
	// 🔴 THE ARC PAGE IS A FOURTH FIXED BROWSE PATH WITH ITS OPERANDS IN QUERY PARAMETERS, for the
	// browse pair's reason above: `/arc/{home}/{slug}` would need a prefix match, and the slug is a
	// value a registration supplied. GET only — the UI never registers an arc (its journal mount is
	// read-only), so neither cross-site gate gains a case; both would apply by METHOD if a write row
	// were ever added. See `handleArcPage`.
	{"GET", "/arc"}: {(*Server).handleArcPage, classContent},
	// 🔴 THE ARCS-FIRST PAGE (S1 of the arcs/presence plan): every arc homed in a readable scope, live
	// ones first. A fixed path whose only operand (`?all=1`) is a view toggle, never an authority input;
	// `content`, because which arcs exist is itself an answer about authority (Q1). GET only — the UI
	// never registers an arc. `/` is NOT replaced (plan decision 13, open question P1). See
	// `handleArcsPage`.
	{"GET", "/arcs"}: {(*Server).handleArcsPage, classContent},
	// 🔴 THE SESSION PAGE IS A FIFTH FIXED BROWSE PATH WITH ITS OPERAND IN A QUERY PARAMETER, for the
	// browse pair's reason: `/session/{id}` would need a prefix match, and a session id is a value a
	// WRITER declared in a trailer. It is the one browse row keyed by a value that is NOT scoped — a
	// session id is global — so it is the one page that AGGREGATES across scopes, and it does so only
	// over the caller's narrowed set. GET only. See `handleSessionPage`.
	{"GET", "/session"}: {(*Server).handleSessionPage, classContent},
	// 🔴 THE BELL (S5 of the arcs/presence plan): queue a terminal-bell ring at the pane a session the
	// VIEWER owns is running in. CLASS 0, ON PURPOSE — it declares nothing, so BOTH cross-site gates
	// reach it by METHOD (same origin before auth, the CSRF token after), exactly as they reach
	// `/sign-out`; `TestTheRingRowIsBehindBothCrossSiteGates` asserts both by their own messages so a
	// future class cannot quietly exempt it. Not `classContent`: it renders no answer about authority —
	// every request is answered with the same 303 back to the session page, whether or not a ring was
	// queued, so the response cannot say whose pane exists. See `handleRing`.
	{"POST", "/ring"}:     {(*Server).handleRing, 0},
	{"GET", "/share"}:     {(*Server).handleSharePage, classContent},
	{"POST", "/share"}:    {(*Server).handleShare, 0},
	{"POST", "/unshare"}:  {(*Server).handleUnshare, 0},
	{"GET", "/sign-in"}:   {(*Server).handleSignInForm, classPublic},
	{"POST", "/sign-in"}:  {(*Server).handleSignIn, classPublic},
	{"POST", "/sign-out"}: {(*Server).handleSignOut, 0},

	// 🔴 THE INVITE FLOW IS THREE AUTHENTICATED ROWS AND ONE PUBLIC ONE, AND THE PUBLIC ONE
	// IS THE WHOLE REASON THE FLOW EXISTS. `GET /join` is what an invited person opens, and
	// they are by definition somebody this control plane may never have heard of — so a row
	// behind the authentication chain would be a door that only opens for people who are
	// already inside. It carries `classPublic` for exactly `GET /sign-in`'s reason, and the
	// two are the same page shape for the same reason: no viewer, no session, no CSRF token.
	//
	// 🔴 `GET /join` NEVER LOOKS THE TOKEN UP, WHICH IS WHY IT IS SAFE TO SERVE PUBLICLY. See
	// `handleJoinPage`: it renders one page for every token, valid or not, so driving it is
	// not a way to ask which invitations exist. That is `flights.start`'s ruling one level
	// over — the token is resolved once, at the callback, where it is being redeemed anyway.
	//
	// 🔴 AND REVOKE IS ITS OWN PATH RATHER THAN AN `action=` FIELD ON `POST /invite`, WHICH IS
	// `UnsharePath`'s RULING RESTATED BECAUSE IT IS THE SAME HAZARD. A hidden field would make
	// the difference between MINTING a capability and withdrawing one a value inside a form
	// body — chosen by whoever gets one request past both cross-site gates — rather than
	// something the route decides. Two paths are two rows in this ledger, which is where
	// somebody reads them.
	{"GET", "/invite"}:         {(*Server).handleInvitePage, classContent},
	{"POST", "/invite"}:        {(*Server).handleInvite, 0},
	{"POST", "/invite/revoke"}: {(*Server).handleInviteRevoke, 0},
	{"GET", "/join"}:           {(*Server).handleJoinPage, classPublic},

	// 🔴 THE PROVIDER PAIR, AND THE METHODS ARE NOT INTERCHANGEABLE. The START is a POST so
	// that gate (2) — same origin, derived from the method — refuses a cross-site request to
	// it: as a GET it would be reachable by any `<img src>` and by every link prefetcher,
	// each of which would mint a flight and overwrite the visitor's flight cookie. The
	// CALLBACK is a GET because the PROVIDER chooses the method and a redirect is a GET;
	// that makes it the one state-changing handler on this surface behind a safe method, and
	// `handleOAuthCallback` names what guards it instead.
	{"POST", "/sign-in/github"}:         {(*Server).handleOAuthStart, classPublic},
	{"GET", "/sign-in/github/callback"}: {(*Server).handleOAuthCallback, classPublic},

	// 🔴 THE STYLESHEET IS A ROUTE, AND THE POLICY THAT FIRST MADE IT ONE IS GONE — THE ROUTE
	// IS NOT. `style-src 'self'` forbade an inline `<style>` element in a conforming browser,
	// so the stylesheet that was a `<style>` in every page's head moved here; that header has
	// since been deleted by operator decision (see `writeHTML`). The route stays because the
	// bytes are now GENERATED — `tailwind.css` compiled to `app.css`, ~29 KB — and inlining
	// them into every response would send that on every page load. Both stylesheet rows are
	// `classPublic` because the SIGN-IN page links the hashed one and that page answers an
	// anonymous caller; a stylesheet behind the chain would render the way in as unstyled
	// text. Neither is `classContent`: they consult no authority, and they answer the same
	// bytes to everybody, which is exactly what makes them safe to serve before
	// authentication.
	//
	// 🔴 THERE ARE TWO STYLESHEET ROWS, AND ONLY THE HASHED ONE IS EVER LINKED. The hashed row
	// is the one `stylesheetLink` emits and the one that carries `immutable`; the constant row
	// is kept, still at a short `max-age`, for requests that already exist in the world — an
	// HTML page a browser rendered before this deploy, a bookmark, a log-derived URL. Keeping
	// it costs one ledger row and turns a 404 into the current bytes. Nothing may LINK it:
	// `TestNoPageLinksTheUnversionedStylesheetPath` is what holds that, because a page that
	// linked it would put every visitor back on the unversioned path and reinstate the exact
	// stale-cache failure the hashed row exists to close.
	{"GET", StylesheetPath}: {(*Server).handleStylesheet, classPublic},

	// 🔴 A COMPUTED KEY, AND IT IS STILL AN EXACT MATCH — WHICH IS THE PROPERTY, NOT THE
	// SPELLING. `routes` is an exact-match map and a prefix match is refused here for the
	// share flow's sake three paragraphs up: a prefix is a second way for a request to reach a
	// handler and `TestEveryServedPathComesFromTheLedger` structurally cannot probe it,
	// because there is no finite set of paths left to probe. A key computed at init is none of
	// that. The set of served paths is still FINITE, still enumerable, still exactly the keys
	// of this map, and the probe list in that test carries the near-misses of this row —
	// `/static/app..css`, a wrong digest, a suffixed digest — each of which a prefix match
	// would serve and this map answers 404.
	{"GET", StylesheetHashedPath}: {(*Server).handleHashedStylesheet, classPublic},

	// 🔴 THE FILTER SCRIPT, AND IT IS THE ONLY SCRIPT ROW. A computed, content-hashed EXACT key for
	// the stylesheet row's reasons above, and `classPublic` for the stylesheet's reason: it consults
	// no authority and answers identical bytes to everybody. Only the scope page links it, and only
	// `AllowedScriptSources` may name it — see `script.go` for the allowlist that replaced this
	// surface's zero-script property, and the three guards that hold it.
	{"GET", FilterScriptPath}: {(*Server).handleFilterScript, classPublic},

	// 🔴 THE WEB APP MANIFEST (S2 of the mobile plan, `pwa.go`). A FIXED path, and `classPublic` because
	// Chromium fetches a manifest without credentials — a manifest behind the chain would leave the
	// sign-in page uninstallable. It consults no authority: it says only what the deployment's
	// `-app-*` flags configured. UNARMED it answers `404 no such route` — the AUTHENTICATED no-route
	// answer; an anonymous caller, whose unrouted paths get 401/303, can tell it apart (`pwa.go`) — so
	// the row is in the ledger on every deployment and the ledger never depends on configuration. The icon rows it
	// links are added beside it by `pwa.go`'s `init`, one computed exact key per committed file.
	{"GET", ManifestPath}: {(*Server).handleManifest, classPublic},
}

// SignInPath and SignOutPath are spelled once and read by the dispatcher, by the
// renderer's form actions and by the redirects. A form posting to a literal string
// would be a second spelling of a route, which is how a form outlives its handler.
const (
	SignInPath  = "/sign-in"
	SignOutPath = "/sign-out"
	RootPath    = "/"
	// ScopesPath is the scope list: one card per readable scope, the search box (`?q=`) and the tag
	// filter (`?tag=`). It was the root until the root became the hub.
	ScopesPath = "/scopes"
	// SessionsPath is every session this caller can see anything of, newest first.
	SessionsPath = "/sessions"
	// ScopePath is one scope's entry list, keyed by `?id=<control.ID>`.
	// EntryPath is one entry, keyed by `?scope=<control.ID>&ref=<stem>`.
	ScopePath = "/scope"
	EntryPath = "/entry"
	// ArcPath is one registered arc, keyed by `?home=<control.ID>&slug=<slug>`.
	ArcPath = "/arc"
	// ArcsPath is every arc this caller can see, live ones first; `?all=1` lists every one.
	ArcsPath = "/arcs"
	// SessionPath is one writing session across every readable scope, keyed by `?session=<id>`.
	SessionPath = "/session"
	// RingPath queues a terminal-bell ring for the session named by the [FieldSession] form field,
	// and answers 303 back to that session's page whatever happened (`handleRing`).
	RingPath = "/ring"
	// SharePath answers the share flow's read AND its grant write, split by method.
	SharePath = "/share"
	// UnsharePath is a SEPARATE path rather than an action field on `SharePath`,
	// deliberately. A hidden `action=revoke` would make the difference between
	// granting and revoking a value inside a form body — something chosen by whoever
	// gets one request past both cross-site gates, rather than something the route
	// decides. Two paths make the two writes two rows in the ledger, which is where
	// somebody reads them.
	UnsharePath = "/unshare"

	// InvitePath answers the invite flow's read AND its mint, split by method — the same
	// shape as [SharePath], keyed by `?project=<control.ID>`.
	InvitePath = "/invite"
	// InviteRevokePath withdraws an outstanding invitation. A SEPARATE path rather than an
	// action field on [InvitePath], for the reason `UnsharePath` gives and `routes` restates.
	InviteRevokePath = "/invite/revoke"
	// JoinPath is the page an invitation LINK points at, and it is the one path on this
	// surface whose URL legitimately carries a capability token.
	//
	// 🔴 SO IT IS ALSO THE ONE PATH WHOSE FULL URL AN OPERATOR HAS TO HAND OUT, AND THIS
	// PROCESS CANNOT BUILD IT. The origin half is configuration — the same reason
	// `OAuthCallbackPath` is spelled here and its full URL is not (see
	// `identity.ErrSupabaseOAuthNoRedirect`) — so the mint page renders the PATH plus the
	// token and says so, rather than guessing a host out of a request header that a proxy
	// chooses.
	JoinPath = "/join"

	// OAuthStartPath and OAuthCallbackPath are the provider pair.
	//
	// 🔴 THE CALLBACK PATH IS ALSO WHAT THE OPERATOR MUST PUT IN THE PROVIDER'S
	// `GOTRUE_URI_ALLOW_LIST`, AND IT IS SPELLED HERE ONCE FOR THAT REASON TOO. The full
	// URL is configuration (this process cannot know its own external origin — see
	// `identity.ErrSupabaseOAuthNoRedirect`), but the PATH half is this file's, and a
	// deployment whose allow-list names a different path gets a flow that completes at the
	// provider and lands nowhere.
	OAuthStartPath    = "/sign-in/github"
	OAuthCallbackPath = "/sign-in/github/callback"

	// StylesheetPath is the UNVERSIONED stylesheet path. It is served, it is NOT linked, and
	// the difference is the whole point — see `routes` for both halves and `stylesheet.go`
	// for the failure that split one row into two.
	StylesheetPath = "/static/app.css"
)

// StylesheetHashedPath is the path every page links, and it carries a digest of the bytes it
// serves. See `stylesheet.go` for why the digest is the mechanism and `routes` for why a
// computed key is not a prefix match.
//
// 🔴 A `var`, NOT A `const`, AND EVERY READER OF IT MUST STAY A READER. It is derived from the
// embedded stylesheet at package initialisation, so it changes whenever the theme does —
// which is exactly the property being bought. Anything that writes the current value down as a
// literal (a ledger row, a test, a deployment's cache rule, a page template) re-creates the
// unversioned path under a longer name and goes silently wrong on the next theme change.
var StylesheetHashedPath = hashedStylesheetPathFor(stylesheet)

// The query parameters this surface reads, spelled once each.
//
// ⚠ `QueryScope` IS SHARED BY THE SHARE FLOW AND BY `GET /entry`, AND THAT IS ON PURPOSE
// RATHER THAN A COLLISION. Both mean "the `control.ID` of a scope", so two spellings
// would be two names for one concept and the second would be the one somebody gets wrong.
// `GET /scope` uses `QueryID` instead because on that page the scope is the SUBJECT, not
// a qualifier on something else — `?scope=` reading as "some other thing, in this scope"
// is what makes `?id=` the honest spelling there.
const (
	QueryScope = "scope"
	QueryID    = "id"
	// QueryRef is an entry ref: USER TEXT, percent-encoded on the way out by `entryHref`
	// and matched against the narrowed answer on the way in.
	QueryRef = "ref"
	// QueryQuery is the search box. It rides on the SCOPE LIST row (`/scopes`, the root before the hub) rather than on a route of
	// its own — see `routes`.
	QueryQuery = "q"
	// QueryTag is the CATEGORY filter: `/scopes?tag=<name>`, the target every rendered tag links
	// to. It rides on the SCOPE LIST row for exactly the reason [QueryQuery] and [QueryView] do,
	// and this is the one place that reason is a REQUIREMENT rather than a preference.
	//
	// 🔴 A QUERY PARAMETER AND NEVER A PATH SEGMENT, BECAUSE EVERY SERVED PATH IS A LITERAL
	// KEY IN `routes`. A `/tag/<name>` shape would make the last segment USER TEXT — a tag
	// out of a file somebody else wrote — so the dispatcher would have to match a PREFIX,
	// and `DeclaredRoutes()`, `TestEveryServedPathComesFromTheLedger` and the
	// `stateChanging` classification all read exact keys. One prefix route would make the
	// ledger's own claim ("every served path is in this map") false, which is the claim the
	// session layer derives its cross-site gates from.
	//
	// ⚠ IT SHARES THE POD'S SPELLING, `tag`, on purpose: the same word names the same filter
	// on `/api/v1/recall/{scope}` and here.
	//
	// 🔴 AND THE SPELLING IS THE CHEAP HALF — A DRAFT OF THIS COMMENT SAID A READER "does not
	// have to learn that they disagree" WHILE THEY DID. `url.Values.Get` is FIRST-wins and the
	// pod's `lastValue` is LAST-wins, so `?tag=a&tag=b` answered `a` here and `b` there. Making
	// the operand scalar settled HOW MANY values are read and says nothing about WHICH, which
	// is the step that draft skipped. `lastTagValue` in `server.go` is what makes the sentence
	// true; it is the only parameter on this surface that agrees with the pod, because it is
	// the only one a reader carries between them by hand.
	QueryTag = "tag"
	// QueryView selects WHICH VIEW of an entry `GET /entry` answers, and it rides on that
	// row rather than on a route of its own for the reason `routes` gives about the share
	// flow and the browse pair: a second path would be a second row in the ledger for one
	// answer about one entry, and `/entry/raw` in particular would be a path whose last
	// segment is a view name sitting where a ref used to be.
	//
	// QueryProject is the `control.ID` of a project, on `GET /invite`. It is a THIRD
	// spelling beside [QueryScope] and [QueryID] because it names a different KIND of
	// object: ids are namespaced by prefix (`prj_` against `scp_`), so reusing `scope=`
	// for a project would put two types under one parameter name and the handler would be
	// the only thing that knew which.
	QueryProject = "project"
	// 🔴 EXACTLY ONE VALUE IS RECOGNISED AND EVERYTHING ELSE IS THE RENDERED VIEW. That is
	// `handlePage`'s ruling for `?q=` restated: a view selector is not an authority
	// question, so an unrecognised value is answered with the page rather than with a
	// refusal. The cost is that a typo is silent — stated here rather than discovered,
	// and the reason the recognised spelling is pinned as a literal in
	// `rawview_test.go` instead of being read back off this constant.
	QueryView = "view"
	// QueryHome is the `control.ID` of an arc's HOME scope on `GET /arc` — an id and never a name,
	// for the reason [Scope.ID] gives. A fourth spelling beside [QueryScope], [QueryID] and
	// [QueryProject] because it names a different ROLE: not "the scope under view" but "the scope
	// this arc belongs to", and a page about an arc is not a page about that scope.
	QueryHome = "home"
	// QuerySlug is the arc's slug on `GET /arc`: MATCHED against the registered set inside
	// `report.Arc`, never resolved.
	QuerySlug = "slug"
	// QuerySession is a writing session's id on `GET /session` — a FIFTH spelling, because a session
	// id is not a `control.ID` of anything: it is a value a writer declared in a trailer, opaque and
	// byte-exact, and `write.SessionComponent` is its whole grammar.
	QuerySession = "session"
	// QueryTab selects WHICH TAB of `GET /scope` (and of `GET /arc`) is rendered, for [QueryView]'s reason on the entry
	// page: three views of one scope behind one row, server-rendered, so a tab is a shareable URL and
	// the back button works with no script. See [TabSessions].
	QueryTab = "tab"
)

// TabSessions and TabArcs are the two recognised [QueryTab] values on `GET /scope`. On `GET /arc`
// only [TabSessions] is recognised (`arcTab`) and the default is the SCOPES tab — the same rule, one
// page over.
//
// ⚠ THERE IS NO `TabEntries` CONSTANT, AND THE ABSENCE IS [ViewRaw]'s RULING RESTATED. The entries
// tab is what the row answers when nothing selects otherwise — `?tab=entries`, `?tab=`, a typo and
// no parameter at all render it — so it has no spelling to get wrong and `scopeTabHref` emits the
// plain scope URL for it. An unrecognised value is answered with the DEFAULT TAB rather than a 400:
// a view selector is not an authority question (`handlePage`'s ruling for `?q=`), and a stale or
// hand-edited tab link landing on the entries is more useful than a refusal page.
const (
	TabSessions = "sessions"
	TabArcs     = "arcs"
	// TabAgent is the scope page's "What an agent sees" tab (`agent.go`). Scope page only.
	TabAgent = "agent"
)

// ViewRaw is the one recognised [QueryView] value: the entry's file, as bytes.
//
// ⚠ THERE IS NO `ViewRendered` CONSTANT, AND THE ABSENCE IS THE DESIGN. The rendered view
// is what the row answers when nothing selects otherwise, so it has no spelling to get
// wrong and no URL of its own to keep working — `entryHref` emits the plain entry URL
// for it. A second constant would invite a second recognised value, and then two URLs
// would render one state.
const ViewRaw = "raw"

// The share flow's form fields, spelled once for the renderer and the handlers.
//
// ⚠ `FieldVerb` IS SINGULAR AND REPEATS. A checkbox group posts one name many times,
// and `r.PostForm[FieldVerb]` is the only read that sees all of them:
// `PostFormValue` returns the FIRST, which would silently narrow every multi-verb
// grant to whichever box the browser happened to serialise first.
const (
	FieldScope   = "scope"
	FieldSubject = "subject"
	FieldVerb    = "verb"
	FieldGrant   = "grant"
)

// FieldSession is the bell form's session id on `POST /ring`. The same string as [QuerySession], for
// [FieldProject]'s reason: one concept, reached on the read through a URL and on the write through a
// body.
const FieldSession = "session"

// The invite flow's form fields.
//
// ⚠ `FieldProject` IS A FORM FIELD WHILE [QueryProject] IS A QUERY PARAMETER, AND THE TWO
// ARE DELIBERATELY THE SAME STRING. They name one concept — the `control.ID` of a project —
// reached on the read through a URL and on the write through a body, which is exactly the
// pair [FieldScope] and [QueryScope] already are. Two different spellings would be two names
// for one thing and the second is the one somebody gets wrong.
//
// ⚠ AND THE INVITATION'S OWN TOKEN IS NOT HERE. It travels in `inviteTokenField`, spelled
// in `inviting.go` beside the argument for why it is a form field and never a query
// parameter — the value is a bearer capability, so its spelling lives next to that reason
// rather than in a list of ordinary field names.
const (
	FieldProject = "project"
	FieldRole    = "role"
	// FieldDigest is how the revoke form names the invitation it withdraws. A DIGEST and
	// never a token: the mint page does not hold the tokens (see `invite.Store`), and a
	// digest is not a credential, which is what makes it safe to render into a page.
	FieldDigest = "digest"
)

// HealthPath is the readiness probe: before authentication, before rate limiting,
// and outside the ledger. A readiness probe broken by a security guard is how the
// guard gets deleted.
const HealthPath = "/healthz"

const healthBody = "ok"

// DeclaredRoutes is every `<METHOD> <path>` this server dispatches, sorted.
//
// ⚠ WHAT IT CANNOT SEE, stated rather than assumed away: a route dispatched from
// anywhere other than `routes`. `TestEveryServedPathComesFromTheLedger` is what
// keeps that true — it drives a path in no row and requires the no-route answer.
//
// ⚠ AND IT IS READ IN ONE PLACE PLUS A LOG LINE, WHICH IS WEAKER THAN THE POD'S
// EQUIVALENT ON PURPOSE. `api.DeclaredRoutes()` is checked against an external
// corpus and read back out of the running binary because the corpus builder is
// Python and cannot see a compiled program. This surface has no corpus, so the
// strongest available claim is that the ledger and the dispatcher read one map.
func DeclaredRoutes() []string {
	out := make([]string, 0, len(routes))
	for key := range routes {
		out = append(out, key.method+" "+key.path)
	}
	slices.Sort(out)
	return out
}

// DeclaredRouteLedger is [DeclaredRoutes] with each row's CLASS spelled out, and it is
// the one the hand-written expectation is compared against.
//
// 🔴 TWO VIEWS OF ONE MAP, NEVER TWO COPIES, AND THE TEST PINS THEM TO EACH OTHER. A
// classed ledger is what makes "this row is public" a thing somebody has to write down
// when they add the row; a plain one is what `cmd/cairn-ui` counts at startup. They
// derive from the same `routes` map, and `TestTheRouteLedgerMatchesTheDispatchTable`
// asserts that stripping the classes off this one reproduces the other exactly — so the
// two cannot drift even though there are two functions.
func DeclaredRouteLedger() []string {
	out := make([]string, 0, len(routes))
	for key, r := range routes {
		line := key.method + " " + key.path
		if classes := classNames(r.class); len(classes) > 0 {
			line += " " + strings.Join(classes, ",")
		}
		out = append(out, line)
	}
	slices.Sort(out)
	return out
}

// classNames renders a class set, sorted by the constants' own order so the ledger's
// spelling cannot depend on anything but the bits.
func classNames(c routeClass) []string {
	var out []string
	if c&classPublic != 0 {
		out = append(out, "public")
	}
	if c&classContent != 0 {
		out = append(out, "content")
	}
	return out
}
