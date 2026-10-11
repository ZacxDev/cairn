package capture

import (
	"github.com/ZacxDev/cairn/internal/client"
	"github.com/ZacxDev/cairn/internal/transcript/scopeuse"
)

// Decision is where one session may go on this run.
type Decision struct {
	// Instance is the alias to ship to, "" when Held.
	Instance string
	Held     bool
	Reason   string
}

// Decide is decision 16: the instance that routes EVERY scope in V — writes AND reads.
//
//   - V empty → the DEFAULT instance (where the owner-only rule applies, O10).
//   - every scope routes to one alias → that alias.
//   - scopes route to two aliases → HELD.
//   - `*`, or a scope the routing cannot place, with MORE than one instance configured → HELD.
//     With exactly one instance there is nowhere else it could go, so it ships to that one, where
//     `*` and unknown names make it owner-only.
//
// It uses `internal/client`'s routing (`Routing.AliasFor`), the same resolver every `cairn` write
// uses — never a second spelling of the table.
func Decide(v []string, routing client.Routing) Decision {
	if len(v) == 0 {
		return Decision{Instance: client.DefaultAlias, Reason: "no recorded scope: the default instance, owner-only"}
	}
	multi := routing.MultiInstance()
	target := ""
	for _, scope := range v {
		if scope == scopeuse.Star {
			if multi {
				return Decision{Held: true, Reason: "an unknown scope (*) with more than one instance configured"}
			}
			continue
		}
		alias, err := routing.AliasFor(scope)
		if err != nil {
			// 🔴 A SCOPE THE TABLE EXPLICITLY ROUTES ELSEWHERE IS HELD AT ANY INSTANCE COUNT. On a
			// one-instance host the "nowhere else it could go" argument holds only for a scope the
			// table says NOTHING about; a table naming an alias this host has no config for is a
			// statement that the scope lives on ANOTHER instance, and shipping it to the sole one
			// is the cross-instance leak decision 16 exists to stop (review round 1).
			if _, named := routing.Routes[scope]; multi || named {
				return Decision{Held: true, Reason: "scope " + scope + " routes to no configured instance"}
			}
			continue
		}
		if target != "" && alias != target {
			return Decision{Held: true, Reason: "its scopes route to two instances (" + target + ", " + alias + ")"}
		}
		target = alias
	}
	if target == "" {
		target = client.DefaultAlias
	}
	return Decision{Instance: target, Reason: "every scope routes to " + target}
}
