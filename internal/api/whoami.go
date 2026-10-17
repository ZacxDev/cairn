package api

import (
	"encoding/json"
	"net/url"
	"slices"

	"github.com/ZacxDev/cairn/internal/control"
)

// StatusWhoami is the `X-Store-Status` and audit `status=` of every `whoami` answer.
//
// It is the route's own name, one value for every caller, because the header says WHICH
// ANSWER this is and the body says what it found: a client tells a whoami from any other
// 200 (a proxy's page, another route's report) by the header, and branches on the body's
// `status` for the federation outcome.
const StatusWhoami = "whoami"

// WhoamiNotFederated is the body `status` of a caller that did NOT authenticate through
// group federation — a machine token, a user-mode signed token, a browser session, a
// proxy assertion. The three federated values are `control.Federation*`'s, spelled once
// there; this is the fourth, and the only one this package defines.
const WhoamiNotFederated = "not-federated"

// whoamiView is the JSON body. Field order is the declaration order, which is the
// contract's order: `principal`, `status`, `groups` {`mapped`, `unmapped`}, `scopes`.
type whoamiView struct {
	Principal string       `json:"principal"`
	Status    string       `json:"status"`
	Groups    whoamiGroups `json:"groups"`
	Scopes    []string     `json:"scopes"`
}

type whoamiGroups struct {
	Mapped   []string `json:"mapped"`
	Unmapped []string `json:"unmapped"`
}

// whoami is the Go-only `GET`/`HEAD whoami`: the caller's OWN authority, as data.
//
// 🔴 A SELF-VIEW, NEVER AN ENUMERATION. Every field is computed from THIS request's one
// authentication — `rq.principal` and `rq.auth`, the same match every other route on the
// request narrows with — so it can name only the groups this caller's token claimed and
// the scopes this caller can already read. It never consults the group map, the model's
// scope list, or another principal. The federation split is `control.Authorization.
// Federation`'s, which partitions the CLAIMED groups only (its own comment says why that
// is safe to show the caller).
//
// 🔴 `scopes` IS THE READ SET, AS NAMES. `NamedScopes(control.VerbRead)` is the one
// traversal every visibility answer comes from; under the federated read ceiling it is a
// federated principal's whole authority, and for any other principal it is exactly the set
// `recall` would answer for. Names, never ids: an id is what a grant addresses, and this is
// a reader's view.
//
// 🔴 NO NEW REFUSAL. The route is reached only after `authenticate` succeeded, so an
// unauthenticated or bad credential gets the uniform 401 every route gets — counted as
// every route counts it — and an authenticated caller always gets 200. A caller that
// reaches nothing (`no-groups`, `groups-unmapped`, or a principal with no grant) is
// answered with empty arrays, never a 401: authorization is never a 401 here.
//
// Nothing is written: no journal, no store, no limiter success beyond the one
// `handleRead` records for every authenticated read.
func (s *Server) whoami(rq *request, _ []string, _ url.Values) error {
	body, err := json.Marshal(whoamiOf(rq.principal, rq.auth))
	if err != nil {
		return err
	}
	rq.audit(200, StatusWhoami)
	rq.respond(200, append(body, '\n'), "application/json", map[string]string{
		"X-Store-Status": StatusWhoami,
	})
	return nil
}

// whoamiOf builds the view. Every list is non-nil (an empty list serialises as `[]`, never
// `null`) and sorted, so the body is a deterministic function of the authority.
func whoamiOf(p control.Principal, auth control.Authorization) whoamiView {
	view := whoamiView{
		Principal: p.Display,
		Status:    WhoamiNotFederated,
		Groups:    whoamiGroups{Mapped: []string{}, Unmapped: []string{}},
		Scopes:    []string{},
	}
	// `Federation` is set only by `control.ResolveFederated` (and carried through narrowing),
	// which is also the only producer of a `KindFederated` principal, so the report alone
	// decides: a second test on the principal's kind could never disagree with it.
	if fed, federated := auth.Federation(); federated {
		view.Status = string(fed.Status)
		view.Groups.Mapped = sortedNonNil(fed.Mapped)
		view.Groups.Unmapped = sortedNonNil(fed.Unmapped)
	}
	for _, scope := range auth.NamedScopes(control.VerbRead) {
		view.Scopes = append(view.Scopes, scope.Name)
	}
	slices.Sort(view.Scopes)
	return view
}

func sortedNonNil(in []string) []string {
	out := append([]string{}, in...)
	slices.Sort(out)
	return out
}
