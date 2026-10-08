package ui

import (
	"strconv"
	"strings"
	"time"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/presence"
)

// 🔴 PRESENCE BADGES (S4 of `claudedocs/plan-cairn-arcs-presence.md`): WHERE A SESSION THE VIEWER
// OWNS IS RUNNING, AND NOTHING FOR ANYBODY ELSE. Decision 5: every surface asks ONE predicate,
// `presence.Store.For(viewer, session)`, and nothing else — so another owner's presence, expired
// presence, a narrowed viewer and no presence are one answer, and this file turns that answer into
// NO NODE AT ALL. A page whose viewer is not shown presence is therefore BYTE-IDENTICAL to the page
// with no presence anywhere; `TestPresenceIsInvisibleToEveryoneButItsOwner` measures that as a
// relationship over one store on every surface that renders a badge.
//
//   - 🔴 THE VIEWER IS THE REQUEST'S OWN `identity.Identity`, WHOLE. [Server.panesFor] binds the
//     predicate to it once per request; the narrowing bit rides on its `Auth`, so rebuilding a
//     viewer out of its principal would read a narrowed bearer credential as its owner.
//   - 🔴 PRESENCE NEVER DECIDES WHETHER A PAGE EXISTS (the plan's P5). Every handler asks its
//     refusal first; the badge is a decoration of a page that was already found, and the session
//     page's uniform 404 is unchanged for a session the owner has a live pane for.
//   - No script, no new asset (decision 14): the badges reuse `.badge`. The badges add no route; the
//     bell beside the session page's badge (S5) is ONE route, `POST /ring` — see `bell.go`.
//
// What a badge says: `host · target · hotkey · runtime · seen Ns ago`, where "seen" is the UI's own
// clock against the push that installed the row (`Located.PushedAt`) — never the host's
// `last_activity`, which is the HOST's clock and is shown only in the tooltip. A second live host
// adds "also on <host>". On the arc pages a member with a live pane makes the arc show "live pane".

// livePanes is "where is this session running", for ONE request's viewer — `presence.Store.For`
// bound to that viewer, or nil when this deployment has no presence. Built only by
// [Server.panesFor].
type livePanes func(session string) (presence.Presence, bool)

// panesFor binds the ONE predicate to the viewer this request resolved to.
func (s *Server) panesFor(id identity.Identity) livePanes {
	if s.presence == nil {
		return nil
	}
	store := s.presence.Store
	return func(session string) (presence.Presence, bool) { return store.For(id, session) }
}

// at is [livePanes] that answers "nothing" when presence is off.
func (l livePanes) at(session string) (presence.Presence, bool) {
	if l == nil {
		return presence.Presence{}, false
	}
	return l(session)
}

// badge is the session badge for `session`, or NO node when the predicate says nothing.
func (l livePanes) badge(session string, now time.Time) g.Node {
	p, ok := l.at(session)
	if !ok {
		return nil
	}
	return paneBadge(p, now)
}

// anyLive is whether any of `sessions` has a pane the predicate shows this viewer.
func (l livePanes) anyLive(sessions []string) bool {
	for _, session := range sessions {
		if _, ok := l.at(session); ok {
			return true
		}
	}
	return false
}

// liveBadge is the arc's "live pane" badge when any member is live, else no node.
func (l livePanes) liveBadge(sessions []string) g.Node {
	if !l.anyLive(sessions) {
		return nil
	}
	return h.Span(h.Class("badge"), h.Data("presence", "live-pane"),
		h.TitleAttr("a member session is running in a pane on one of your machines (visible only to you)"),
		g.Text("live pane"))
}

// paneBadge renders one [presence.Presence]. Every value is the owner's own host's word, so each
// goes through `g.Text` or a quoted attribute, like every other user string on these pages.
func paneBadge(p presence.Presence, now time.Time) g.Node {
	t := p.Target
	parts := []string{t.Host, t.Target}
	if t.Hotkey != "" {
		parts = append(parts, t.Hotkey)
	}
	parts = append(parts, t.Runtime, "seen "+secondsAgo(t.PushedAt, now))
	tip := "where this session is running, pushed by your own machine — visible only to you"
	if t.Label != "" {
		tip += "\nlabel: " + t.Label
	}
	if t.LastActivity != "" {
		tip += "\nlast activity: " + t.LastActivity + " (that host's clock)"
	}
	return g.Group([]g.Node{
		h.Span(h.Class("badge"), h.Data("presence", "pane"), h.TitleAttr(tip), g.Text(strings.Join(parts, " · "))),
		g.If(len(p.AlsoOn) > 0, h.Span(h.Class("badge badge-quiet"), h.Data("presence", "also-on"),
			h.TitleAttr("the same session is live on another of your machines; the badge shows the most recently active one"),
			g.Text("also on "+strings.Join(p.AlsoOn, ", ")))),
	})
}

// secondsAgo is "Ns ago" at second precision — presence lives minutes, so a coarser bucket would
// hide the only difference between two pushes. A push in the future (a skewed clock) is "0s ago".
func secondsAgo(at, now time.Time) string {
	d := now.Sub(at)
	if d < 0 {
		d = 0
	}
	return strconv.FormatInt(int64(d/time.Second), 10) + "s ago"
}
