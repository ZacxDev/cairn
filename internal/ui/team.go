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

// # 🔴 THE TEAM PAGE IS THE ONE PAGE — THE FORMS LIVE HERE (operator decision O-a)
//
// `GET /team` carries the whole of "who can get at my notes": the SHARE flow (`?scope=` picks
// a scope: who has access, what can be taken back, the grant form), the single-project
// INVITE flow (`?project=` picks a project: its outstanding invitations, revoke, the mint
// form, and its PROJECT-WIDE grants with revoke — O-b), and the multi-target TEAM LINK (its
// form, and every link this caller minted with its redemption log and revoke).
//
// `GET /share` and `GET /invite` answer a 303 here, carrying their query. Their POST rows
// (`/share`, `/unshare`, `/invite`, `/invite/revoke`) are UNCHANGED as routes, so both
// cross-site gates still reach them by METHOD and every refusal they answer is the same; only
// where they land afterwards moved, to this page. The sections are the same renderers the two
// old pages used (`shareIndex`, `shareScopeSection`, `inviteIndex`, `inviteProjectSection`,
// `mintedSection`), so what each guard over them read is what it still reads.

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

// teamOutcomeRevoked is the team-link flow's outcome code — a CODE from a closed set, never
// a sentence from the query string (`outcomeFrom`'s ruling). Distinct from the share flow's
// `revoked` and the invite flow's `invite-revoked`, because all three land on this one page.
const teamOutcomeRevoked = "link-revoked"

// The Team page's section anchors. A redirect into a flow lands at its section.
const (
	teamShareAnchor  = "share"
	teamInviteAnchor = "invite"
	teamLinksAnchor  = "links"
)

// redirectToTeam answers a GET with a bodiless 303 into the Team page.
//
// ⚠ NOT `http.Redirect`, WHICH WRITES A `text/html` BODY FOR A GET: that is an HTML response
// this surface did not render through `writeHTML`, so it carried no `Cache-Control: no-store`
// and `TestEveryNonPublicHTMLRowIsNoStore` measured it as a page. A 303 here has nothing to
// say beyond its Location.
func redirectToTeam(w http.ResponseWriter, href string) {
	w.Header().Set("Location", href)
	w.WriteHeader(http.StatusSeeOther)
}

// teamHref is the ONE builder of a URL into the Team page: the path, an already-encoded
// query (possibly empty) and a section anchor.
func teamHref(rawQuery, anchor string) string {
	out := TeamPath
	if rawQuery != "" {
		out += "?" + rawQuery
	}
	if anchor != "" {
		out += "#" + anchor
	}
	return out
}

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
	// Share is the share section: the index (zero `Scope`) or one scope — `shareSection`.
	Share ShareView
	// Invite is the invitation section: the index, one project, or a just-minted invitation
	// — `inviteSection` / `handleInvite`.
	Invite InviteView
	// ProjectGrants is the project-wide grants over `Invite.Project`, revocable here (O-b).
	ProjectGrants []GrantRow
	// NoInviteStore is true when the deployment has no database: there is then neither an
	// invitation half nor a link half (one is read from the other — `Inviting.TeamLinks`),
	// and both sections say `NoInviteStore`.
	NoInviteStore bool
	// Mintable is the link form's target chooser.
	Mintable []MintableTarget
	// Links is this caller's own links, newest first.
	Links []TeamLinkRow
	// Outcome is the banner a link revoke's redirect carries, or "".
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
	// Confirmed is whether the join it stands for was recorded — only a confirmed row is
	// rendered as a join (`invite.LinkRedemption.Confirmed`).
	Confirmed bool
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
//
// ⚠ `?scope=` AND `?project=` ARE REFUSED EXACTLY AS THE OLD PAGES REFUSED THEM — the
// uniform 404 `scopeRefusal` / `inviteRefusal` — because the sections are built by the same
// code (`shareSection`, `inviteSection`), which writes the refusal itself.
func (s *Server) handleTeamPage(w http.ResponseWriter, r *http.Request, id identity.Identity) {
	view, ok := s.teamView(w, r, id)
	if !ok {
		return
	}
	if r.URL.Query().Get(QueryOutcome) == teamOutcomeRevoked {
		view.Outcome = "Revoked. That link can no longer be redeemed. Anybody who already " +
			"joined with it keeps what it gave them."
	}
	s.render(w, TeamPage(view))
}

// teamView builds every section of the page from this request. ok=false means a refusal has
// been written.
func (s *Server) teamView(w http.ResponseWriter, r *http.Request, id identity.Identity) (TeamView, bool) {
	share, ok := s.shareSection(w, r, id)
	if !ok {
		return TeamView{}, false
	}
	inv, ok := s.inviteSection(w, r, id)
	if !ok {
		return TeamView{}, false
	}
	view := TeamView{
		Viewer:        id.Principal.Display,
		App:           s.app,
		CSRF:          csrfTokenFor(r),
		Share:         share,
		Invite:        inv,
		NoInviteStore: s.inviting == nil,
	}
	// 🔴 REACHED ONLY FOR A PROJECT `inviteSection` ALREADY NARROWED — `Invitable` through
	// `membershipActor` — because `ProjectGrants` performs no check of its own, exactly as
	// `Outstanding` performs none.
	if inv.Project.ID != "" {
		grants, err := s.sharing.ProjectGrants(inv.Project.ID)
		if err != nil {
			s.logf("the project-wide grants could not be read: %v", err)
			writePlain(w, http.StatusInternalServerError, "the authority could not be read")
			return TeamView{}, false
		}
		view.ProjectGrants = s.sharing.ForViewer(grants, membershipActor(id), id.Auth)
	}
	if s.teamLinks != nil {
		view.Mintable = s.teamLinks.Mintable(membershipActor(id))
		links, err := s.teamLinks.Links(membershipActor(id))
		if err != nil {
			s.logf("the team links could not be read: %v", err)
			writePlain(w, http.StatusInternalServerError, "the team links could not be read")
			return TeamView{}, false
		}
		view.Links = teamLinkRows(links, s.now())
	}
	return view, true
}

// renderTeamAfterMint renders the Team page as the response to a MINT (an invitation or a
// team link), with `set` adding the one-time value.
//
// ⚠ THE SECTIONS ARE BUILT FROM A COPY OF THE REQUEST WITH NO QUERY. A POST's URL may carry
// one (`POST /team/link?scope=…` is a string anybody can put in a form action), and a section
// refusing a scope it names would write a 404 AFTER the capability was minted — losing the
// one rendering of a token that cannot be recovered.
func (s *Server) renderTeamAfterMint(w http.ResponseWriter, r *http.Request, id identity.Identity, set func(*TeamView)) {
	bare := r.Clone(r.Context())
	bare.URL.RawQuery = ""
	view, ok := s.teamView(w, bare, id)
	if !ok {
		return
	}
	set(&view)
	s.render(w, TeamPage(view))
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

	s.renderTeamAfterMint(w, r, id, func(view *TeamView) {
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
	})
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
	http.Redirect(w, r, teamHref(QueryOutcome+"="+teamOutcomeRevoked, teamLinksAnchor), http.StatusSeeOther)
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
				Seq: red.Seq, Who: red.Who, At: red.At.UTC().Format(time.RFC3339),
				Provisioned: red.Provisioned, Confirmed: red.Confirmed,
			})
		}
		out = append(out, row)
	}
	return out
}

// TeamPage renders the Team page. Through `shell`, so it carries the frame.
//
// 🔴 ALL THREE NOTICES, ON EVERY SHAPE AND ABOVE EVERY ANSWER THEY QUALIFY — the rule each of
// the two old pages stated for its own: the replica-honesty notice qualifies the audience
// list, `InviteHonesty` the invitation link, `TeamHonesty` the team link. A notice shown only
// on the shape about to write would leave the read — far more common — unqualified.
func TeamPage(v TeamView) g.Node {
	title := "cairn — team"
	switch {
	case v.Share.Scope.Name != "":
		title = "cairn — team: sharing " + v.Share.Scope.Name
	case v.Invite.Project.Name != "":
		title = "cairn — team: inviting to " + v.Invite.Project.Name
	}
	share, inv := v.Share, v.Invite
	share.CSRF, inv.CSRF = v.CSRF, v.CSRF
	return shell(
		title,
		PageView{Viewer: v.Viewer, CSRF: v.CSRF, App: v.App},
		nil,
		h.H2(g.Text("Team")),
		h.P(h.Class("replica-honesty"), g.Text(ReplicaHonesty)),
		h.P(h.Class("invite-honesty"), g.Text(InviteHonesty)),
		h.P(h.Class("invite-honesty"), g.Text(TeamHonesty)),
		g.If(share.ReadOnly, h.P(h.Class("read-only"), g.Text(ReadOnlyAuthority))),
		g.If(share.Outcome != "", h.P(h.Class("outcome"), g.Text(share.Outcome))),
		g.If(inv.Outcome != "", h.P(h.Class("outcome"), g.Text(inv.Outcome))),
		g.If(v.Outcome != "", h.P(h.Class("outcome"), g.Text(v.Outcome))),
		// `g.Iff` for each pointer — the measured nil-deref the old invite page recorded:
		// `g.If` evaluates its argument before the condition.
		g.Iff(inv.Minted != nil, func() g.Node { return mintedSection(inv) }),
		g.Iff(v.Minted != nil, func() g.Node { return mintedLinkSection(v.Minted) }),
		h.Div(h.ID(teamShareAnchor),
			g.If(share.Scope.Name == "", shareIndex(share)),
			g.If(share.Scope.Name != "", shareScopeSection(share)),
		),
		h.Div(h.ID(teamInviteAnchor),
			g.If(v.NoInviteStore, h.P(h.Class("read-only"), g.Text(NoInviteStore))),
			g.If(!v.NoInviteStore && inv.Project.Name == "", inviteIndex(inv)),
			g.If(!v.NoInviteStore && inv.Project.Name != "", inviteProjectSection(inv)),
			g.If(!v.NoInviteStore && inv.Project.Name != "", projectGrantsSection(v)),
		),
		teamLinkSection(v),
	)
}

// projectGrantsSection lists the grants over the WHOLE selected project — what a team link's
// project-`reader` writes — each revocable through `POST /unshare` (operator decision O-b), so
// nothing a link confers needs the control CLI to take back.
func projectGrantsSection(v TeamView) g.Node {
	return h.Section(
		h.Class("invite-project"),
		h.H3(g.Text("Project-wide grants on "+v.Invite.Project.Name)),
		h.P(h.Class("note"), g.Text(
			"A project-wide grant reaches every scope this project owns. Revoking one withdraws "+
				"all of them from that grantee; it does not touch project membership.")),
		g.If(len(v.ProjectGrants) == 0, h.P(h.Class("empty"), g.Text("No project-wide grant names this project."))),
		h.Ul(h.Class("grants"), g.Map(v.ProjectGrants, func(row GrantRow) g.Node {
			// Its revoke lands back on THIS project's section, not the share section.
			return revocableItemReturningTo(row, v.CSRF, v.Invite.Project.ID)
		})),
	)
}

func teamLinkSection(v TeamView) g.Node {
	return h.Section(
		h.ID(teamLinksAnchor),
		h.Class("team-links"),
		h.H3(g.Text("Team links")),
		g.If(v.NoInviteStore, h.P(h.Class("read-only"), g.Text(NoInviteStore))),
		g.If(!v.NoInviteStore, g.Group([]g.Node{
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
		// The count is of SPENDS — attempts — and the confirmed share of them is the joins.
		h.Span(h.Class("at"), g.Text(strconv.Itoa(row.Redemptions)+" redemption attempt(s), "+
			strconv.Itoa(confirmedCount(row.Log))+" confirmed")),
		h.Span(h.Class("at"), g.Text("created "+row.Created)),
		h.Span(h.Class("at"), g.Text("expires "+row.Expires)),
		h.Ul(h.Class("link-targets"), g.Map(row.Targets, func(t string) g.Node { return h.Li(g.Text(t)) })),
		g.If(len(row.Log) > 0, h.Ul(h.Class("link-log"), g.Map(row.Log, func(red TeamRedemptionRow) g.Node {
			// 🔴 ONLY A CONFIRMED ROW IS A JOIN. An unconfirmed one is a spend whose authority
			// write never landed (the double-callback round 1 measured) — or, rarely, a join whose
			// confirmation was lost — and it is rendered as exactly that rather than as somebody
			// who joined.
			if !red.Confirmed {
				// ⚠ "NOT CONFIRMED", NOT "NOTHING WAS RECORDED": the rare lost-confirmation case is a
				// join that WAS recorded and whose row could not be confirmed (round 2 nit).
				return h.Li(g.Text("#" + strconv.Itoa(red.Seq) + " an attempt by " + red.Who + " at " + red.At +
					" — NOT confirmed: the join may not have been recorded (check the control journal)"))
			}
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

// confirmedCount is how many of a link's redemption rows are confirmed joins.
func confirmedCount(log []TeamRedemptionRow) int {
	n := 0
	for _, r := range log {
		if r.Confirmed {
			n++
		}
	}
	return n
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
