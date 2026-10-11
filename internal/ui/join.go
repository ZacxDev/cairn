package ui

import (
	_ "embed"
	"net/http"
	"net/url"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/invite"
)

// joinScript moves a join link's token from the URL FRAGMENT into the accept form — the THIRD
// entry of the script allowlist (`script.go`), added deliberately by operator decision: a REUSABLE
// team-link token must never travel in a URL a server receives, so that no access log at any hop
// (a CDN, a relay, this pod's own gateway) can hold one. A fragment is never sent in a request, and
// moving it into a form needs script; see the header of `join.js` for exactly what it may touch,
// and `TestTheJoinScriptTouchesOnlyWhatItSays` for the spelling guard that holds it there.
//
// ⚠ THE SAME FLAKE RULE AS `filter.js` and `pwa.js`: `flake.nix`'s `onlyGo` filter names this file.
//
//go:embed join.js
var joinScript string

// JoinScriptPath is the content-hashed path the join page links — the stylesheet's mechanism, so a
// changed script is a NEW URL and the year-long `immutable` is licensed by the URL.
var JoinScriptPath = "/static/join." + hashAsset(joinScript) + ".js"

// joinScriptTag is the ONE way a page reaches the script, in the allowlisted shape (`defer`).
func joinScriptTag() g.Node {
	return h.Script(h.Src(JoinScriptPath), h.Defer())
}

// handleJoinScript serves the script's hashed row: PUBLIC (the join page is), `nosniff`,
// `immutable` — the filter script's handler exactly, for its reasons. It serves a Go variable.
func handleJoinScript(_ *Server, w http.ResponseWriter, _ *http.Request, _ identity.Identity) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", stylesheetCacheImmutable)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(joinScript))
}

// The script's row, a computed exact key in the same `routes` map — `pwa.js`'s argument. PUBLIC,
// because the page that links it is.
func init() {
	routes[routeKey{http.MethodGet, JoinScriptPath}] = route{handleJoinScript, classPublic}
}

// joinLink is the ONE spelling of a minted join link, for an invitation and a team link alike:
// `/join#invite=<token>`, a PATH plus a FRAGMENT. Both mint handlers call it, so no mint can drift
// back to the query string. The fragment is never sent in a request, so the token reaches no
// server's access log, no referrer and no request line; the page it opens moves it into the
// accept form's body (`join.js`).
func joinLink(token string) string {
	return JoinPath + "#" + url.Values{inviteTokenField: []string{token}}.Encode()
}

// admitQueryBorne answers the invitation token the callback may redeem: the flight's token, or ""
// when that token is a REUSABLE team link that travelled in a URL query string.
//
// # 🔴 THE TRANSITION RULE, STATED ONCE
//
// Every mint renders `/join#invite=<token>` now, and a fragment reaches no server. A link sent
// BEFORE that is `/join?invite=<token>`, and its token has already been written to every access log
// on the way. So, for a token whose flight is marked query-borne ([flightInvite.viaQuery]):
//
//   - a single-use INVITATION is redeemed — already-sent links keep working;
//   - a single-use TEAM LINK (reuse unticked) is redeemed too, for the same reason: one redemption
//     closes it, which is the exposure an invitation already has;
//   - a REUSABLE team link is REFUSED — it is open enrolment until it expires or is revoked, so a
//     logged copy is the leak the fragment exists to close, and accepting it would keep honest
//     browsers following and re-sending the logged shape.
//
// 🔴 THIS DOES NOT PROTECT A LOGGED TOKEN, AND MUST NOT BE READ AS IF IT DID. The query-borne mark
// is CLIENT-REPORTED (it exists only because the legacy page posts [inviteQueryTokenField]), so a
// holder of a logged token who posts it in [inviteTokenField], or opens `/join#invite=<token>`,
// arrives unmarked and redeems. What protects a reusable link that ever travelled in a query string
// is REVOCATION BY ITS MINTER — the only person who can revoke it ([ControlTeamLinks.Revoke]) — or
// its expiry, and the refusal's log line names that remedy and who holds it.
//
// 🔴 REFUSED BY DROPPING THE TOKEN, SO THE ANSWER IS THE UNIFORM ONE. With no token neither
// redemption arm runs: a stranger falls to `signInRefused`, byte-identical to a dead link's; a known
// user is signed in and joins nothing. A lookup FAILURE drops it too — fail closed, because the
// alternative redeems a link this function could not rule out.
func (s *Server) admitQueryBorne(carried flightInvite, client string) string {
	if carried.token == "" || !carried.viaQuery || s.teamLinks == nil {
		return carried.token
	}
	reusable, err := s.teamLinks.ReusableLink(carried.token)
	if err != nil {
		s.logf("github sign-in: a query-borne invitation token could not be classified, so it is NOT redeemed: %v (%s)",
			err, client)
		return ""
	}
	if reusable {
		s.logf(queryBorneReusableRefusedLog+": link=%s (%s)", shortDigest(invite.Digest(carried.token)), client)
		return ""
	}
	return carried.token
}

// queryBorneReusableRefusedLog is the fixed text of the line `admitQueryBorne` writes when it refuses a
// reusable link that arrived in a query string. 🔴 IT MUST NOT SAY THE LINK STOPS WORKING: the refusal
// binds only an honest browser, and the token still redeems through the fragment shape or a
// hand-built POST. It names the remedy and the one person who holds it — the link's minter.
const queryBorneReusableRefusedLog = "github sign-in: a REUSABLE team link arrived in a URL query string and " +
	"was refused for this browser, but its token is NOT retired: it has been in every access log on the way " +
	"and still redeems through /join#invite= or a hand-built POST until it expires or is revoked. Only the " +
	"user who minted it can revoke it (their Team page); tell them, and they should mint a new one"

// JoinFragmentNeedsScript is what the fragment join page says with script OFF — the link's token is
// in the fragment, which only script can move, so the page fails VISIBLY rather than silently.
const JoinFragmentNeedsScript = "This invitation link needs JavaScript to open: the invitation is in the " +
	"part of the link after the #, which your browser keeps to itself. Turn JavaScript on for this site " +
	"and open the link again."

// The element ids `join.js` reads. Spelled once for the renderer; the script spells them as literals,
// and `TestTheFragmentJoinPageMovesTheTokenThroughTheScript` pins the rendered markup they match.
const (
	joinAcceptID  = "join-accept"
	joinFieldID   = "join-invite"
	joinMissingID = "join-missing"
	joinFormID    = "join-form"
)
