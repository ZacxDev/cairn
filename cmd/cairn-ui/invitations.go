package main

import (
	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/invite"
	"github.com/ZacxDev/cairn/internal/ui"
)

// wireInvitations builds the invitation half AND the team-link half from one database's two
// stores, over the one authority the chain authenticates against.
//
// 🔴 ONE FUNCTION, SO THE WIRING HAS A TEST THAT DOES NOT NEED A DATABASE. The two halves
// used to be built inline in `main`'s DSN branch, which only the Postgres tier reaches — and
// deleting `Links: links` there left BOTH tiers green, because nothing minted a link and
// redeemed it through the object `main` built. `TestTheWiredInvitationHalfRedeemsATeamLink`
// drives this function with in-memory stores (the ordinary tier, and the mutation battery's
// killer for the missing-wiring row); `TestWithADatabaseTheSurfaceMovesItsStateThereAndHoldsInvitations`'
// sibling in the Postgres tier drives it with the real ones.
//
// 🔴 ONE `ControlTeamLinks`, HANDED TO `ControlInviting.Links`: the Team page reads it back
// through `Inviting.TeamLinks()`, and `ControlInviting` hands it every token its own store does
// not know — so a link minted on the page is redeemed through the same object, against the
// same authority.
func wireInvitations(authority *control.Cache, invites invite.Store, links invite.LinkStore) ui.Inviting {
	team := &ui.ControlTeamLinks{Authority: authority, Store: links}
	return ui.ControlInviting{Authority: authority, Invites: invites, Links: team}
}
