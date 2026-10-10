package ui

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/invite"
)

// # 🔴 THE TEAM PAGE CONSOLIDATES; IT DOES NOT REPLACE
//
// `GET /team` is the one page for "who can get at my notes": the scopes this caller can
// SHARE (linking `/share?scope=…`), the projects it can INVITE into (linking
// `/invite?project=…`), and the new multi-target TEAM LINK — its form, and every link this
// caller minted with its redemption log and a revoke button.
//
// The `/share` and `/invite` rows are KEPT, not redirected, and that was the choice the
// operator left open: every guard over them — the uniform refusals, the CSRF and
// same-origin gates on their POST rows, the narrowed-actor tests, the mutation rows naming
// their handlers — stays meaningful only while the rows keep answering what they answer
// today. A 303 to `/team` would have turned each of those into a test of a redirect.

// TeamHonesty is the notice the Team page carries on every shape of it.
//
// 🔴 PINNED AS ONE NORMALISED STRING — `InviteHonesty`'s ruling, for its reason: the clause
// most worth dropping is the one that makes the product sound weakest, and here that is
// the REUSE clause. Every clause is a property measured elsewhere:
//
//   - "shown once and cannot be recovered" — `invite.NewToken`; only the digest is stored;
//   - "joins every project and scope it names" — `linkEvents` writes one record per target;
//   - "a link that allows reuse … any number of times … until it expires or is revoked" —
//     `TeamLink.StateAt` never closes a reusable link on its count (the operator decision);
//   - "stops working if whoever created it loses the authority it confers" — `reCheckMinter`;
//   - "revoking it … takes nothing back" — redemption writes `member-set`/`granted` records
//     on an append-only journal, which a revoke of the LINK does not touch.
const TeamHonesty = "A team link is a link, and the link is the authority: it is shown once " +
	"and cannot be recovered, and anyone who holds it can use it to join every project and " +
	"scope it names. A link that allows reuse can be redeemed by any number of people, any " +
	"number of times, until it expires or is revoked. A link stops working if whoever created " +
	"it loses the authority it confers, and revoking it stops further redemptions but takes " +
	"nothing back from anybody who already joined."

// teamOutcomeRevoked is the one outcome a redirect into the Team page may carry — a CODE
// from a closed set, never a sentence from the query string (`outcomeFrom`'s ruling).
const teamOutcomeRevoked = "link-revoked"

// teamWriteRefusal is the uniform refusal for both team-link writes: unknown target, not
// yours, role too high, unknown link and somebody else's link are one answer.
const teamWriteRefusal = "that team link cannot be recorded"

// The Team page's form fields.
//
// ⚠ `FieldTarget` IS SINGULAR AND REPEATS, for `FieldVerb`'s reason: a checkbox group posts
// one name many times, and only `r.PostForm[FieldTarget]` sees every box.
const (
	// FieldTarget is `<kind>:<control.ID>` — `project:prj_…` or `scope:scp_…`.
	FieldTarget = "target"
	// FieldTTLDays is the link's lifetime in whole days, 1 to `invite.MaxLinkTTL` in days.
	FieldTTLDays = "ttl_days"
	// FieldReuse is the "allow reuse" checkbox: present means reusable.
	FieldReuse = "reuse"
)

// TeamView is everything the Team page renders. Plain values, so the renderer needs no
// store and no `internal/invite` type beyond the role list.
type TeamView struct {
	Viewer string
	CSRF   string
	App    App
	// Shareable is the scopes this caller administers — `Sharing.Administrable`.
	Shareable []control.NamedScope
	// Invitable is the projects this caller may invite into — `Inviting.Invitable`. Nil
	// with NoInviteStore when there is no invitation store.
	Invitable []control.NamedProject
	// NoInviteStore is true when there is no INVITATION store: the invite list says
	// `NoInviteStore` instead.
	NoInviteStore bool
	// NoLinkStore is true when there is no TEAM-LINK store: the link half says `NoInviteStore`
	// (the same database is what is missing). Separate from NoInviteStore because a server may
	// hold invitations without links, and folding the two made the invite list claim "no
	// invitation store" on a server that had one.
	NoLinkStore bool
	// Mintable is the link form's target chooser.
	Mintable []MintableTarget
	// Links is this caller's own links, newest first.
	Links []TeamLinkRow
	// Outcome is the banner a revoke's redirect carries, or "".
	Outcome string
	// Minted is the link this request just created, shown ONCE.
	Minted *MintedTeamLink
}

// TeamLinkRow is one link as the Team page shows it.
type TeamLinkRow struct {
	Digest   string
	Short    string
	Role     string
	State    string
	Reusable bool
	// Targets are display strings — a name, or the id when the caller can no longer
	// confer that target (the link is then dead, and the row says why).
	Targets     []string
	Redemptions int
	Created     string
	Expires     string
	Log         []TeamRedemptionRow
}

// TeamRedemptionRow is one redemption — the audit record, rendered.
type TeamRedemptionRow struct {
	Seq         int
	Who         string
	At          string
	Provisioned bool
}

// MintedTeamLink is a freshly created link, rendered exactly once — `MintedInvite`'s rule.
type MintedTeamLink struct {
	Link     string
	Role     string
	Expires  string
	Reusable bool
	Targets  []string
}

// handleTeamPage renders the Team page.
//
// 🔴 EVERY LIST ON IT IS AN AUTHORITY ANSWER, ASKED OF THE AUTHORITY THAT OWNS IT: the
// share list from `Sharing.Administrable(id.Auth)` (the request's own frozen
// authorization), the invite list from `Inviting.Invitable` and the link chooser from
// `TeamLinking.Mintable` (both through `membershipActor`, so a narrowed credential sees
// neither — `membershipActor`'s ruling).
func (s *Server) handleTeamPage(w http.ResponseWriter, r *http.Request, id identity.Identity) {
	view := s.teamView(r, id)
	if s.teamLinks != nil {
		links, err := s.teamLinks.Links(membershipActor(id))
		if err != nil {
			s.logf("the team links could not be read: %v", err)
			writePlain(w, http.StatusInternalServerError, "the team links could not be read")
			return
		}
		view.Links = teamLinkRows(links, s.now())
	}
	if r.URL.Query().Get(QueryOutcome) == teamOutcomeRevoked {
		view.Outcome = "Revoked. That link can no longer be redeemed. Anybody who already " +
			"joined with it keeps what it gave them."
	}
	s.render(w, TeamPage(view))
}

// teamView fills the parts of the page every shape of it carries.
func (s *Server) teamView(r *http.Request, id identity.Identity) TeamView {
	view := TeamView{
		Viewer:        id.Principal.Display,
		App:           s.app,
		CSRF:          csrfTokenFor(r),
		Shareable:     s.sharing.Administrable(id.Auth),
		NoInviteStore: s.inviting == nil,
		NoLinkStore:   s.teamLinks == nil,
	}
	if s.inviting != nil {
		view.Invitable = s.inviting.Invitable(membershipActor(id))
	}
	if s.teamLinks != nil {
		view.Mintable = s.teamLinks.Mintable(membershipActor(id))
	}
	return view
}

// handleTeamLink mints one team link and renders it ONCE.
//
// ⚠ IT DOES NOT REDIRECT — `handleInvite`'s ruling, for its reason: the token survives no
// hop. A reload re-submits the form and mints a second link; the extra one is listed and
// revocable.
func (s *Server) handleTeamLink(w http.ResponseWriter, r *http.Request, id identity.Identity) {
	if s.teamLinks == nil {
		s.refuseWithoutInviteStore(w)
		return
	}
	if err := r.ParseForm(); err != nil {
		writePlain(w, http.StatusBadRequest, teamWriteRefusal)
		return
	}
	targets, ok := parseTargets(r.PostForm[FieldTarget])
	if !ok {
		writePlain(w, http.StatusBadRequest, teamWriteRefusal)
		return
	}
	ttl, ok := parseTTLDays(r.PostFormValue(FieldTTLDays))
	if !ok {
		writePlain(w, http.StatusBadRequest, teamWriteRefusal)
		return
	}
	role := invite.LinkRole(r.PostFormValue(FieldRole))
	reusable := r.PostFormValue(FieldReuse) != ""

	// 🔴 THE TARGETS GO TO `Mint` AS THE FORM NAMED THEM AND `Mint` IS THE AUTHORITY CHECK.
	// The chooser constrains a browser and nothing else; `Mint` asks `mayLink` of every
	// target, and refuses the whole link on one miss.
	token, link, err := s.teamLinks.Mint(r.Context(), membershipActor(id), targets, role, ttl, reusable)
	if err != nil {
		s.refuseTeamWrite(w, err, "mint")
		return
	}
	s.logf("a team link was minted: targets=%d role=%s reusable=%v expires=%s by=%s",
		len(link.Targets), link.Role, link.Reusable, link.ExpiresAt.UTC().Format(time.RFC3339), link.Inviter)

	view := s.teamView(r, id)
	names := map[invite.Target]string{}
	for _, m := range view.Mintable {
		names[m.Target] = m.Name
	}
	var shown []string
	for _, t := range link.Targets {
		shown = append(shown, targetLabel(t, names[t]))
	}
	view.Minted = &MintedTeamLink{
		// A PATH AND A QUERY, NEVER AN ABSOLUTE URL — `handleInvite`'s ruling. And the SAME
		// join path and field as an invitation: see `ControlInviting.Links`.
		Link:     JoinPath + "?" + url.Values{inviteTokenField: []string{token}}.Encode(),
		Role:     string(link.Role),
		Expires:  link.ExpiresAt.UTC().Format(time.RFC3339),
		Reusable: link.Reusable,
		Targets:  shown,
	}
	s.render(w, TeamPage(view))
}

// handleTeamLinkRevoke withdraws one of the caller's own links. The authority is
// `ControlTeamLinks.Revoke`'s, from the stored row: only the minter.
func (s *Server) handleTeamLinkRevoke(w http.ResponseWriter, r *http.Request, id identity.Identity) {
	if s.teamLinks == nil {
		s.refuseWithoutInviteStore(w)
		return
	}
	if err := r.ParseForm(); err != nil {
		writePlain(w, http.StatusBadRequest, teamWriteRefusal)
		return
	}
	digest := r.PostFormValue(FieldDigest)
	if digest == "" {
		writePlain(w, http.StatusBadRequest, teamWriteRefusal)
		return
	}
	if err := s.teamLinks.Revoke(r.Context(), membershipActor(id), digest); err != nil {
		s.refuseTeamWrite(w, err, "revoke")
		return
	}
	http.Redirect(w, r, TeamPath+"?"+QueryOutcome+"="+teamOutcomeRevoked, http.StatusSeeOther)
}

// refuseTeamWrite is the ONE status mapping for both team-link writes.
func (s *Server) refuseTeamWrite(w http.ResponseWriter, err error, what string) {
	s.logf("a team link %s was refused: %v", what, err)
	switch {
	case errors.Is(err, ErrNotLinkable), errors.Is(err, invite.ErrNotRedeemable):
		writePlain(w, http.StatusForbidden, teamWriteRefusal)
	case errors.Is(err, invite.ErrBadTarget), errors.Is(err, ErrBadLinkTTL):
		writePlain(w, http.StatusBadRequest, teamWriteRefusal)
	default:
		writePlain(w, http.StatusInternalServerError, "the team link could not be recorded")
	}
}

// parseTargets reads the repeated target field. Any malformed value refuses the whole form
// rather than being dropped: a dropped box would mint a link over less than was ticked.
func parseTargets(raw []string) ([]invite.Target, bool) {
	var out []invite.Target
	for _, v := range raw {
		kind, id, found := strings.Cut(v, ":")
		t := invite.Target{Kind: invite.TargetKind(kind), ID: control.ID(id)}
		if !found || !t.Kind.Valid() || t.ID == "" {
			return nil, false
		}
		out = append(out, t)
	}
	return out, invite.ValidateTargets(out) == nil
}

// parseTTLDays reads the lifetime field: whole days, from 1 to the ceiling. Empty is the
// default.
func parseTTLDays(raw string) (time.Duration, bool) {
	if raw == "" {
		return invite.DefaultTTL, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || time.Duration(n)*24*time.Hour > invite.MaxLinkTTL {
		return 0, false
	}
	return time.Duration(n) * 24 * time.Hour, true
}

// maxLinkTTLDays is the ceiling in the unit the form speaks.
var maxLinkTTLDays = int(invite.MaxLinkTTL / (24 * time.Hour))

// defaultLinkTTLDays is the form's pre-filled lifetime.
var defaultLinkTTLDays = int(invite.DefaultTTL / (24 * time.Hour))

func targetValue(t invite.Target) string { return string(t.Kind) + ":" + string(t.ID) }

func targetLabel(t invite.Target, name string) string {
	if name == "" {
		return string(t.Kind) + " " + string(t.ID) + " (no longer yours to confer)"
	}
	return string(t.Kind) + " " + name
}

// teamLinkRows projects links onto rows at ONE instant — `inviteRows`' rule.
func teamLinkRows(links []LinkWithLog, now time.Time) []TeamLinkRow {
	out := make([]TeamLinkRow, 0, len(links))
	for _, l := range links {
		row := TeamLinkRow{
			Digest:      l.Link.Digest,
			Short:       shortDigest(l.Link.Digest),
			Role:        string(l.Link.Role),
			State:       string(l.Link.StateAt(now)),
			Reusable:    l.Link.Reusable,
			Redemptions: l.Link.Redemptions,
			Created:     l.Link.CreatedAt.UTC().Format(time.RFC3339),
			Expires:     l.Link.ExpiresAt.UTC().Format(time.RFC3339),
			Targets:     l.TargetLabels,
		}
		for _, red := range l.Log {
			row.Log = append(row.Log, TeamRedemptionRow{
				Seq: red.Seq, Who: red.Who, At: red.At.UTC().Format(time.RFC3339), Provisioned: red.Provisioned,
			})
		}
		out = append(out, row)
	}
	return out
}

// TeamPage renders the Team page. Through `shell`, so it carries the frame.
func TeamPage(v TeamView) g.Node {
	return shell(
		"cairn — team",
		PageView{Viewer: v.Viewer, CSRF: v.CSRF, App: v.App},
		nil,
		h.H2(g.Text("Team")),
		h.P(h.Class("invite-honesty"), g.Text(TeamHonesty)),
		g.If(v.Outcome != "", h.P(h.Class("outcome"), g.Text(v.Outcome))),
		// `g.Iff` for the pointer — `InvitePage`'s measured panic.
		g.Iff(v.Minted != nil, func() g.Node { return mintedLinkSection(v.Minted) }),
		teamShareSection(v),
		teamInviteSection(v),
		teamLinkSection(v),
	)
}

func teamShareSection(v TeamView) g.Node {
	return h.Section(
		h.Class("team-share"),
		h.H3(g.Text("Share one scope")),
		g.If(len(v.Shareable) == 0, h.P(h.Class("empty"), g.Text(
			"No scope is administrable by this credential. That is an authority answer, not an "+
				"empty store."))),
		h.Ul(g.Map(v.Shareable, func(sc control.NamedScope) g.Node {
			return h.Li(h.A(h.Href(SharePath+"?"+QueryScope+"="+string(sc.ID)), g.Text(sc.Name)))
		})),
		h.P(h.Class("note"), h.A(h.Href(SharePath), g.Text("All sharing"))),
	)
}

func teamInviteSection(v TeamView) g.Node {
	return h.Section(
		h.Class("team-invite"),
		h.H3(g.Text("Invite one person to one project")),
		g.If(v.NoInviteStore, h.P(h.Class("read-only"), g.Text(NoInviteStore))),
		g.If(!v.NoInviteStore && len(v.Invitable) == 0, h.P(h.Class("empty"), g.Text(
			"No project is yours to invite into. That is an authority answer, not an empty "+
				"control plane: inviting somebody needs the owner or admin role in a project."))),
		h.Ul(g.Map(v.Invitable, func(p control.NamedProject) g.Node {
			return h.Li(
				h.A(h.Href(InvitePath+"?"+QueryProject+"="+string(p.ID)), g.Text(p.Name)),
				h.Span(h.Class("kind"), g.Text("you are "+string(p.HeldRole))),
			)
		})),
		h.P(h.Class("note"), h.A(h.Href(InvitePath), g.Text("All invitations"))),
	)
}

func teamLinkSection(v TeamView) g.Node {
	return h.Section(
		h.Class("team-links"),
		h.H3(g.Text("Team links")),
		g.If(v.NoLinkStore, h.P(h.Class("read-only"), g.Text(NoInviteStore))),
		g.If(!v.NoLinkStore, g.Group([]g.Node{
			h.H4(g.Text("Create a team link")),
			g.If(len(v.Mintable) == 0, h.P(h.Class("empty"), g.Text(
				"No project or scope is yours to put on a link. That is an authority answer: a "+
					"link needs the owner or admin role in a project, or admin on a scope."))),
			g.If(v.CSRF == "" && len(v.Mintable) > 0, h.P(h.Class("note"), g.Text(
				"This credential has no browser session, so no form is rendered. Sign in to create a link."))),
			g.If(v.CSRF != "" && len(v.Mintable) > 0, teamLinkForm(v)),
			h.H4(g.Text("Links you have created")),
			g.If(len(v.Links) == 0, h.P(h.Class("empty"), g.Text("You have not created a team link."))),
			h.Ul(h.Class("invites"), g.Map(v.Links, func(row TeamLinkRow) g.Node {
				return teamLinkItem(row, v.CSRF)
			})),
		})),
	)
}

// teamLinkForm is the multi-target mint form.
//
// 🔴 THE ROLE DEFAULT IS THE LEAST PRIVILEGED (`reader`), FOR `inviteForm`'s REASON, AND
// THE REUSE BOX IS UNTICKED, FOR THE SAME ONE: the unread default must be the safe one.
func teamLinkForm(v TeamView) g.Node {
	return h.FormEl(
		h.Class("invite team-link"),
		h.Method("post"),
		h.Action(TeamLinkPath),
		h.Input(h.Type("hidden"), h.Name(FieldCSRF), h.Value(v.CSRF)),
		h.FieldSet(
			h.Legend(g.Text("Join these")),
			// The LABEL wraps the box, `shareForm`'s shape, so at a coarse pointer the whole
			// row is the target (`tailwind.css`'s touch block) rather than a 13px square.
			g.Map(v.Mintable, func(m MintableTarget) g.Node {
				roles := make([]string, 0, len(m.Roles))
				for _, r := range m.Roles {
					roles = append(roles, string(r))
				}
				return h.Label(
					h.Input(h.Type("checkbox"), h.Name(FieldTarget), h.Value(targetValue(m.Target))),
					g.Text(targetLabel(m.Target, m.Name)+" (up to "+strings.Join(roles, ", ")+")"),
				)
			}),
		),
		h.Label(h.For("link-role"), g.Text("Join as")),
		h.Select(h.ID("link-role"), h.Name(FieldRole), h.Required(),
			g.Map(invite.AllLinkRoles, func(r invite.LinkRole) g.Node {
				return h.Option(h.Value(string(r)), g.If(r == invite.LinkReader, h.Selected()), g.Text(string(r)))
			}),
		),
		h.Label(h.For("link-ttl"), g.Text("Expires after (days)")),
		h.Input(h.Type("number"), h.ID("link-ttl"), h.Name(FieldTTLDays), h.Required(),
			h.Min("1"), h.Max(strconv.Itoa(maxLinkTTLDays)), h.Value(strconv.Itoa(defaultLinkTTLDays))),
		h.FieldSet(
			h.Legend(g.Text("Reuse")),
			h.Label(
				h.Input(h.Type("checkbox"), h.Name(FieldReuse), h.Value("1")),
				g.Text("Allow reuse — anyone with the link can join, any number of times, until it expires or you revoke it"),
			),
		),
		h.P(h.Class("note"), g.Text(
			"The link this creates is shown once. Every box ticked must be one you may confer at "+
				"the role chosen, or no link is created.")),
		h.Button(h.Type("submit"), g.Text("Create a team link")),
	)
}

func teamLinkItem(row TeamLinkRow, csrf string) g.Node {
	reuse := "single use"
	if row.Reusable {
		reuse = "reusable"
	}
	return h.Li(
		h.Class("invite-row"),
		h.Span(h.Class("who"), g.Text(row.Short)),
		h.Span(h.Class("kind"), g.Text(row.Role)),
		h.Span(h.Class("state"), g.Text(row.State)),
		h.Span(h.Class("kind"), g.Text(reuse)),
		h.Span(h.Class("at"), g.Text("redeemed "+strconv.Itoa(row.Redemptions)+" time(s)")),
		h.Span(h.Class("at"), g.Text("created "+row.Created)),
		h.Span(h.Class("at"), g.Text("expires "+row.Expires)),
		h.Ul(h.Class("link-targets"), g.Map(row.Targets, func(t string) g.Node { return h.Li(g.Text(t)) })),
		g.If(len(row.Log) > 0, h.Ul(h.Class("link-log"), g.Map(row.Log, func(red TeamRedemptionRow) g.Node {
			what := "joined"
			if red.Provisioned {
				what = "joined (account created by this link)"
			}
			return h.Li(g.Text("#" + strconv.Itoa(red.Seq) + " " + red.Who + " " + what + " at " + red.At))
		}))),
		g.If(csrf != "" && row.State == string(invite.StateOpen), h.FormEl(
			h.Class("revoke"),
			h.Method("post"),
			h.Action(TeamLinkRevokePath),
			h.Input(h.Type("hidden"), h.Name(FieldCSRF), h.Value(csrf)),
			h.Input(h.Type("hidden"), h.Name(FieldDigest), h.Value(row.Digest)),
			h.Button(h.Type("submit"), g.Text("Revoke")),
		)),
	)
}

// mintedLinkSection renders a fresh link ONCE, as TEXT — `mintedSection`'s three reasons.
func mintedLinkSection(m *MintedTeamLink) g.Node {
	reuse := "Single use: the first person to redeem it spends it."
	if m.Reusable {
		reuse = "Reusable: anyone holding it can join, any number of times, until it expires or is revoked."
	}
	return h.Section(
		h.Class("invite-minted"),
		h.H3(g.Text("Team link created")),
		h.P(h.Class("note"), g.Text(
			"This is the only time this link is shown. It is not stored and cannot be "+
				"recovered; if it is lost, revoke it and create another.")),
		h.P(h.Class("invite-link"), g.Text(m.Link)),
		h.P(h.Class("note"), g.Text(
			"Put this deployment's own address in front of that path before sending it. "+
				"cairn cannot know the address you reach it by, so it does not guess one.")),
		h.P(h.Class("note"), g.Text(reuse)),
		h.Ul(h.Class("invites"), h.Li(
			h.Class("invite-row"),
			h.Span(h.Class("kind"), g.Text(m.Role)),
			h.Span(h.Class("at"), g.Text("expires "+m.Expires)),
			h.Ul(h.Class("link-targets"), g.Map(m.Targets, func(t string) g.Node { return h.Li(g.Text(t)) })),
		)),
	)
}
