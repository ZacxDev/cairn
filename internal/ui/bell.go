package ui

import (
	"net/http"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/write"
)

// 🔴 THE BELL (S5 of `claudedocs/plan-cairn-arcs-presence.md`): A BUTTON BESIDE THE SESSION PAGE'S
// PRESENCE BADGE THAT QUEUES ONE TERMINAL-BELL RING FOR THE PANE THAT BADGE NAMES. The host's own
// ring-claim service picks the ring up and re-resolves the pane locally (decision 9); nothing here
// knows a pane, a tty or a byte to send, and the ring itself carries none.
//
//   - 🔴 ONE PREDICATE, AND THE HANDLER NEVER ASKS ANOTHER. `presence.Service.Ring` queues only when
//     `presence.Store.For(viewer, session)` returns the target row — the SAME call the badge renders
//     from, with the SAME whole `identity.Identity` (narrowing rides on its `Auth`). Another owner's
//     presence, expired presence, a narrowed viewer, no presence and presence OFF all queue nothing.
//   - 🔴 ONE ANSWER, WHATEVER HAPPENED: 303 to the session's page. Queued, deduplicated against a ring
//     already pending (O4/D3: one per `(owner, session)` for 60 s), refused by the predicate, presence
//     off, a session id the trailer grammar cannot produce — every one is the same status, the same
//     `Location` and the same empty body, so `POST /ring` is not an oracle over whose panes exist.
//     The owner learns nothing from the response either; the terminal is the feedback.
//     `TestEveryRingAnswerIsTheSameRedirect` holds that as a relationship over one store.
//   - 🔴 THE HOST IS DECIDED AT ENQUEUE TIME by decision 7 (newest `last_activity`, ties to the smaller
//     label) — `Service.Ring` reads it off the target row, so the ring goes where the badge says.
//   - No script and no new asset (decision 14): a plain `<form method="post">` carrying the CSRF field,
//     rendered only when the page has a token to carry. Both cross-site gates reach the row by METHOD.
//
// ⚠ IT DOES NOT ASK WHETHER THE VIEWER CAN READ ANY WRITE OF THE SESSION. The session page's uniform
// 404 means the bell is never RENDERED for such a session (presence decorates a page that was found,
// the plan's P5), but a hand-built POST naming the owner's own live session is queued: the predicate is
// about the PANE, which is the owner's, and a store walk on every ring would buy nothing a non-owner
// could not already be refused by the predicate.

// handleRing is `POST /ring`. See the file comment; every exit but a failed queue write is the one
// redirect.
func (s *Server) handleRing(w http.ResponseWriter, r *http.Request, id identity.Identity) {
	session := r.PostFormValue(FieldSession)
	// The bound first, so an id no writer could have produced never reaches the store — and gets the
	// same answer as every other request, because a 400 here would be a second thing a ring can say.
	if s.presence != nil && write.SessionComponent.MatchString(session) {
		if _, err := s.presence.Ring(id, session); err != nil {
			// Reachable only for a viewer the predicate SHOWED a pane (the error is the ring id's
			// randomness failing), so it discloses nothing the owner's own page does not.
			s.logf("ring refused: the ring could not be queued: %v", err)
			writePlain(w, http.StatusInternalServerError, "the ring could not be queued")
			return
		}
	}
	// 303, not 302: the browser must follow it with a GET (`handleSignIn`'s reason).
	http.Redirect(w, r, sessionHref(session), http.StatusSeeOther)
}

// bellForm is the bell button for `session`: a POST, for `signOutForm`'s reason — a ring behind a
// `GET` would be rung by every link prefetcher and every `<img src>` on any page in the world.
func bellForm(session, csrf string) g.Node {
	return h.FormEl(
		h.Class("bell"),
		h.Method("post"),
		h.Action(RingPath),
		h.Data("presence", "bell"),
		h.Input(h.Type("hidden"), h.Name(FieldCSRF), h.Value(csrf)),
		h.Input(h.Type("hidden"), h.Name(FieldSession), h.Value(session)),
		h.Button(h.Type("submit"), h.TitleAttr("ring the terminal bell in this pane, so its window is easy to find"),
			g.Text("Ring")),
	)
}
