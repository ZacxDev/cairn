package ui

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/ZacxDev/cairn/internal/arcs"
	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/plugins"
	"github.com/ZacxDev/cairn/internal/report"
	"github.com/ZacxDev/cairn/internal/transcript"
	"github.com/ZacxDev/cairn/internal/write"
)

// THE PLUGIN SURFACE — S5 of `claudedocs/plan-cairn-plugins.md` (decisions 9 and 10): `GET /plugins`
// lists what the operator registered, and three POST rows set a plugin's toggle at the three levels
// decision 10 names. The forms live on the pages whose subject they toggle — the scope page, the arc
// page and the session page — and post to these rows.
//
// 🔴 WHO MAY TOGGLE IS DECIDED BY LEVEL, FROM `control.Resolve` AND THE TRANSCRIPT'S OWNER ARM, AND
// NEVER FROM A FORM FIELD:
//   - a SCOPE toggle takes `admin` on that scope (the share flow's verb — consent to send a scope's
//     content to a third party is an administration act, and `write` is not enough);
//   - an ARC toggle takes `admin` on the arc's HOME scope — NOT the right to `PUT` the arc, which any
//     writer of the home scope has — and an arc speaks only for sessions whose visibility set holds its
//     home (`plugins.Effective`), so an arc homed elsewhere changes nothing for a session (T14). `on`
//     is read against the arc's members as they are NOW; `off` against them AND the members the arc
//     listed when the veto was saved — the handler snapshots their ids into the journal record
//     (`plugins.Record.Members`), so removing a session from an `off` arc lifts nothing. The arc
//     page lists the sessions a veto still covers although the arc no longer lists them;
//   - a SESSION opt-out takes the transcript's OWNER arm (un-narrowed, `(Kind, ID)`), and can only
//     say OFF: a session-level ON is refused 400 and journals nothing ([plugins.ErrSessionOn]).
//
// 🔴 EVERY TOGGLE ROW IS BEHIND BOTH CROSS-SITE GATES BY METHOD (class 0, the bell's and the memo
// rows' reason). Each answers 303 back to the page it came from on success, and ONE refusal
// ([pluginToggleRefusal], 403) for a scope, arc or session that does not exist, cannot be read, or is
// not the caller's to toggle — so a toggle form cannot be driven to ask what exists.
//
// 🔴 THE JOURNAL RECORDS `(Kind, ID)` AND THE POD CLOCK, NEVER `Display` AND NEVER A FORM VALUE.

// The plugin surface's paths.
const (
	// PluginsPath is the registry page.
	PluginsPath = "/plugins"
	// PluginScopePath, PluginArcPath and PluginSessionPath set a toggle at one level each — three paths
	// rather than a `level=` field, `UnsharePath`'s ruling: which authority a write is checked against
	// is something the ROUTE decides, never a value in a form body.
	PluginScopePath   = "/plugins/scope"
	PluginArcPath     = "/plugins/arc"
	PluginSessionPath = "/plugins/session"
)

// The toggle forms' fields. The scope, the arc's home and slug and the session reuse [FieldScope],
// [QueryHome]/[QuerySlug] and [FieldSession].
const (
	FieldPlugin = "plugin"
	FieldValue  = "value"
	// FieldMembers is the ARC toggle form's digest of the member list the page rendered
	// ([arcMembersDigest]): the POST is saved only over the list the admin was shown.
	FieldMembers = "members"
)

// arcMembersChanged answers an arc toggle POST whose page is stale: nothing is saved.
const arcMembersChanged = "this arc's members changed since the page you saved from was rendered, so the toggle was NOT " +
	"saved. Reload the arc page, read the member list, and save again."

// arcMembersDigest is a digest of an arc's member session ids, order-free. Not a secret and not an
// authority input: it only says WHICH list a form was rendered over.
func arcMembersDigest(members []string) string {
	sorted := slices.Clone(members)
	slices.Sort(sorted)
	sum := sha256.Sum256([]byte(strings.Join(sorted, "\n")))
	return hex.EncodeToString(sum[:16])
}

// NoPluginRegistry is the OFF state's sentence.
const NoPluginRegistry = "No plugin is registered on this deployment — it was started without -plugin-registry, so " +
	"there is nothing to toggle. This is the designed off state."

// pluginToggleRefusal is every toggle row's ONE authority refusal.
const pluginToggleRefusal = "no such target, or it is not yours to toggle"

// pluginJournalUnreadable is the BROKEN state: the journal is configured and could not be read or
// written. It carries no path (`sourcesUnreadable`'s rule).
const pluginJournalUnreadable = "the plugin toggle journal could not be read or written; this is NOT 'every toggle is off'"

// PluginToggleRow is one plugin's value at one level.
type PluginToggleRow struct {
	Name, Version string
	CallsModel    bool
	Value         string
	SetBy, SetAt  string
	// StillVetoed is an ARC row's whose value is `off`: the sessions the arc listed when the veto was
	// saved and no longer lists — still covered by it (`plugins.State.ArcSnapshot`).
	StillVetoed []string
}

// PluginToggles is the toggle block a scope, arc or session page carries.
type PluginToggles struct {
	Level plugins.Level
	// Action is the toggle row the form posts to; Hidden the fields naming the target.
	Action string
	Hidden [][2]string
	// Values are the options the form offers, in order.
	Values []string
	Rows   []PluginToggleRow
	// CanEdit is the level's authority, read off the request's own authorization.
	CanEdit bool
	// Unreadable is the journal's BROKEN state.
	Unreadable bool
}

// toggleBlock builds a block for one target, or nil when no registry is configured.
func (s *Server) toggleBlock(level plugins.Level, target string, canEdit bool, action string, hidden [][2]string, members ...string) *PluginToggles {
	if s.plugins == nil {
		return nil
	}
	values := []string{plugins.ValueOff, plugins.ValueAllowed, plugins.ValueOn}
	switch level {
	case plugins.LevelArc:
		values = []string{plugins.ValueUnset, plugins.ValueOn, plugins.ValueOff}
	case plugins.LevelSession:
		values = []string{plugins.ValueUnset, plugins.ValueOff}
	}
	block := &PluginToggles{Level: level, Action: action, Hidden: hidden, Values: values, CanEdit: canEdit}
	st, err := s.plugins.Journal.Read()
	if err != nil {
		s.logf("plugins: the toggle journal could not be read: %v", err)
		block.Unreadable = true
		return block
	}
	for _, e := range s.plugins.Registry.Entries() {
		row := PluginToggleRow{Name: e.Name, Version: e.Version, CallsModel: e.CallsModel}
		var key string
		switch level {
		case plugins.LevelScope:
			row.Value, key = st.Scope(e.Name, target), plugins.ScopeTarget(target)
		case plugins.LevelArc:
			row.Value = st.Value(e.Name, plugins.LevelArc, target, plugins.ValueUnset)
			key = target
		case plugins.LevelSession:
			row.Value, key = st.Session(e.Name, target), target
		}
		if rec, ok := st.Last(e.Name, level, key); ok {
			row.SetBy, row.SetAt = rec.By.Kind+":"+rec.By.ID, rec.At
			// An arc VETO keeps covering the members it was saved over, listed or not.
			if level == plugins.LevelArc && row.Value == plugins.ValueOff {
				for _, m := range rec.Members {
					if !slices.Contains(members, m) {
						row.StillVetoed = append(row.StillVetoed, m)
					}
				}
				slices.Sort(row.StillVetoed)
			}
		}
		block.Rows = append(block.Rows, row)
	}
	return block
}

// scopeToggles is the scope page's block: `admin` on the scope edits.
func (s *Server) scopeToggles(id identity.Identity, scope Scope) *PluginToggles {
	return s.toggleBlock(plugins.LevelScope, scope.Name, scope.ID != "" && id.Auth.Allows(scope.ID, control.VerbAdmin),
		PluginScopePath, [][2]string{{FieldScope, string(scope.ID)}})
}

// arcToggles is the arc page's block: `admin` on the HOME scope edits.
func (s *Server) arcToggles(id identity.Identity, home Scope, reg arcs.Registration) *PluginToggles {
	return s.toggleBlock(plugins.LevelArc, plugins.ArcTarget(reg.Key()), home.ID != "" && id.Auth.Allows(home.ID, control.VerbAdmin),
		PluginArcPath, [][2]string{{QueryHome, string(home.ID)}, {QuerySlug, reg.Slug},
			{FieldMembers, arcMembersDigest(memberSessions(reg))}}, memberSessions(reg)...)
}

// vetoSnapshot is the member ids an arc toggle record carries: the arc's members for an `off`, and
// none for any other value.
func vetoSnapshot(value string, reg arcs.Registration) []string {
	if value != plugins.ValueOff {
		return nil
	}
	return memberSessions(reg)
}

// memberSessions is an arc registration's member session ids, in the registration's order.
func memberSessions(reg arcs.Registration) []string {
	out := make([]string, 0, len(reg.Members))
	for _, m := range reg.Members {
		out = append(out, m.Session)
	}
	return out
}

// sessionToggles is the session page's block, for the transcript's OWNER only — nil for everybody
// else, so a refused viewer's page carries no trace of it.
func (s *Server) sessionToggles(id identity.Identity, session string) *PluginToggles {
	if s.plugins == nil || s.transcripts == nil {
		return nil
	}
	owns, err := s.transcripts.OwnedBy(transcript.Viewer{Principal: id.Principal, Auth: id.Auth}, session)
	if err != nil || !owns {
		return nil
	}
	return s.toggleBlock(plugins.LevelSession, session, true, PluginSessionPath, [][2]string{{FieldSession, session}})
}

// pluginTogglesNode renders a block. Every value is `g.Text`.
func pluginTogglesNode(t PluginToggles, csrf string) g.Node {
	heading := map[plugins.Level]string{
		plugins.LevelScope:   "Plugins on this scope",
		plugins.LevelArc:     "Plugins on this arc",
		plugins.LevelSession: "Plugins on this session",
	}[t.Level]
	note := map[plugins.Level]string{
		plugins.LevelScope: "A plugin runs on a session only when EVERY scope the session touched is at least `allowed`, " +
			"and either every one is `on` or an arc listing it — homed in a scope the session touched — is `on`. " +
			"`allowed` is consent that such an arc may turn the plugin on here: anyone who can WRITE to the arc's home " +
			"scope can add a session to the arc, so `allowed` extends to every session those writers enrol. Default: off.",
		plugins.LevelArc: "An arc can turn a plugin on only inside scopes whose admins already allowed it, and only for " +
			"sessions that touched this arc's home scope. `on` applies to the sessions the arc lists now — including " +
			"ones added later by anyone who can write to the home scope. `off` vetoes every session the arc lists, and " +
			"keeps vetoing the ones it listed when the veto was saved even if they are removed. Default: unset.",
		plugins.LevelSession: "Your opt-out for this one session. It can only turn a plugin OFF — never on.",
	}[t.Level]
	return h.Div(
		h.ID("plugin-toggles"), h.Data("level", string(t.Level)),
		h.H3(g.Text(heading)),
		h.P(h.Class("note"), g.Text(note)),
		g.If(t.Unreadable, h.P(h.Class("read-only"), h.Data("plugins", "unreadable"), g.Text(pluginJournalUnreadable))),
		g.If(!t.Unreadable && len(t.Rows) == 0, h.P(h.Class("empty"), g.Text("The registry holds no plugin."))),
		g.If(!t.Unreadable, h.Ul(g.Map(t.Rows, func(r PluginToggleRow) g.Node {
			return pluginToggleItem(t, r, csrf)
		}))),
	)
}

func pluginToggleItem(t PluginToggles, r PluginToggleRow, csrf string) g.Node {
	selectID := "plugin-" + string(t.Level) + "-" + r.Name
	return h.Li(
		h.Data("plugin", r.Name),
		h.Strong(g.Text(r.Name)), g.Text(" "+r.Version+" — "),
		h.Span(h.Data("toggle-value", r.Value), g.Text(r.Value)),
		g.If(r.CallsModel, h.Span(h.Class("badge"), g.Text("sends content to a model"))),
		g.If(r.SetBy != "", h.Span(h.Class("note"), g.Text(" set by "+r.SetBy+" at "+r.SetAt))),
		arcCoverage(t, r),
		g.If(t.CanEdit && csrf != "", h.FormEl(
			h.Class("invite"), h.Method("post"), h.Action(t.Action),
			h.Input(h.Type("hidden"), h.Name(FieldCSRF), h.Value(csrf)),
			h.Input(h.Type("hidden"), h.Name(FieldPlugin), h.Value(r.Name)),
			g.Map(t.Hidden, func(kv [2]string) g.Node {
				return h.Input(h.Type("hidden"), h.Name(kv[0]), h.Value(kv[1]))
			}),
			h.Label(h.For(selectID), g.Text("Set "+r.Name)),
			h.Select(h.ID(selectID), h.Name(FieldValue), g.Map(t.Values, func(v string) g.Node {
				return h.Option(h.Value(v), g.If(v == r.Value, h.Selected()), g.Text(v))
			})),
			h.Button(h.Type("submit"), g.Text("Save")),
		)),
	)
}

// arcCoverage says what an arc VETO still covers beyond the arc's current members — nothing on
// another level, for another value, or when every snapshotted member is still listed. Every id is
// `g.Text` with the whole id in `title=`, linked to its session page by this surface's own ledger
// path.
func arcCoverage(t PluginToggles, r PluginToggleRow) g.Node {
	if t.Level != plugins.LevelArc || r.Value != plugins.ValueOff || len(r.StillVetoed) == 0 {
		return nil
	}
	labels := shortIDsIn(r.StillVetoed)
	one := len(r.StillVetoed) == 1
	return g.Group([]g.Node{
		h.P(h.Class("note"), h.Data("still-vetoed", strconv.Itoa(len(r.StillVetoed))),
			g.Text("`off` still covers "+plural(len(r.StillVetoed), "session", "sessions")+" this arc listed when the veto was "+
				"saved and no longer lists — removing "+map[bool]string{true: "it", false: "them"}[one]+" from the arc did not lift it:")),
		h.Ul(h.Class("chips chips-alias"), g.Map(r.StillVetoed, func(id string) g.Node {
			return h.Li(h.TitleAttr(id), h.A(h.Href(sessionHref(id)), g.Text(labels[id])))
		})),
	})
}

// toggleForm parses a toggle POST: the form, a registered plugin and a value its level takes. A
// failure has been answered.
func (s *Server) toggleForm(w http.ResponseWriter, r *http.Request, level plugins.Level) (plugins.Entry, string, bool) {
	if err := r.ParseForm(); err != nil {
		writePlain(w, http.StatusBadRequest, "the form could not be read")
		return plugins.Entry{}, "", false
	}
	if s.plugins == nil {
		writePlain(w, http.StatusNotImplemented, NoPluginRegistry)
		return plugins.Entry{}, "", false
	}
	// The registry is PUBLIC to every signed-in caller (`GET /plugins`), so naming an unregistered
	// plugin is a malformed form, not a question about authority.
	e, ok := s.plugins.Registry.Lookup(r.PostFormValue(FieldPlugin))
	if !ok {
		writePlain(w, http.StatusBadRequest, "no such plugin is registered")
		return plugins.Entry{}, "", false
	}
	value := r.PostFormValue(FieldValue)
	if err := plugins.ValidValue(level, value); err != nil {
		// 🔴 DECIDED BEFORE ANY AUTHORITY IS ASKED, so a session-level ON is the same 400 for its owner
		// and for a stranger, and journals nothing for either (case 10).
		writePlain(w, http.StatusBadRequest, err.Error())
		return plugins.Entry{}, "", false
	}
	return e, value, true
}

// journal appends one toggle stamped with the AUTHENTICATED principal and the pod clock.
//
// `members` is an ARC VETO's snapshot — the session ids the arc lists at this moment, as the handler
// just read them — and nothing for any other level or value.
func (s *Server) journalToggle(w http.ResponseWriter, id identity.Identity, plugin string, level plugins.Level, target, value string, members ...string) bool {
	err := s.plugins.Journal.Append(plugins.Record{Plugin: plugin, Level: level, Target: target, Value: value, Members: members,
		By: plugins.By{Kind: string(id.Principal.Kind), ID: string(id.Principal.ID)}, At: s.now().UTC().Format(time.RFC3339)})
	if err != nil {
		s.logf("plugins: a toggle could not be journaled: %v", err)
		writePlain(w, http.StatusServiceUnavailable, pluginJournalUnreadable)
		return false
	}
	return true
}

// handlePluginScope is `POST /plugins/scope`.
func (s *Server) handlePluginScope(w http.ResponseWriter, r *http.Request, id identity.Identity) {
	e, value, ok := s.toggleForm(w, r, plugins.LevelScope)
	if !ok {
		return
	}
	scopes, err := s.source.Visible(id.Auth)
	if err != nil {
		writePlain(w, http.StatusInternalServerError, "the store could not be read")
		return
	}
	scope, found := pickScope(scopes, control.ID(r.PostFormValue(FieldScope)))
	if !found || !id.Auth.Allows(scope.ID, control.VerbAdmin) {
		writePlain(w, http.StatusForbidden, pluginToggleRefusal)
		return
	}
	if s.journalToggle(w, id, e.Name, plugins.LevelScope, plugins.ScopeTarget(scope.Name), value) {
		http.Redirect(w, r, scopeHref(scope), http.StatusSeeOther)
	}
}

// handlePluginArc is `POST /plugins/arc`.
func (s *Server) handlePluginArc(w http.ResponseWriter, r *http.Request, id identity.Identity) {
	e, value, ok := s.toggleForm(w, r, plugins.LevelArc)
	if !ok {
		return
	}
	scopes, err := s.source.Visible(id.Auth)
	if err != nil {
		writePlain(w, http.StatusInternalServerError, "the store could not be read")
		return
	}
	home, found := pickScope(scopes, control.ID(r.PostFormValue(QueryHome)))
	if !found || !id.Auth.Allows(home.ID, control.VerbAdmin) {
		writePlain(w, http.StatusForbidden, pluginToggleRefusal)
		return
	}
	rep, err := s.source.Arc(id.Auth, home.Name, r.PostFormValue(QuerySlug))
	if err != nil {
		var unreadable *arcs.JournalUnreadableError
		if errors.As(err, &unreadable) {
			writePlain(w, http.StatusServiceUnavailable, arcJournalUnreadable)
			return
		}
		writePlain(w, http.StatusInternalServerError, "the store could not be read")
		return
	}
	if rep.Status != report.StatusArcFound {
		writePlain(w, http.StatusForbidden, pluginToggleRefusal)
		return
	}
	// 🔴 THE TOGGLE IS SAVED ONLY OVER THE MEMBER LIST THE ADMIN WAS SHOWN (#273's audit, F6). An arc's
	// members are whatever a writer of the home last `PUT`, and an admin deciding on a page rendered a
	// minute ago is deciding about that page's list: a tab left open while a writer adds a session
	// would turn the plugin on for a session the admin never saw. The form carries a digest of the
	// list it was rendered over; a POST whose digest is not the CURRENT list's saves nothing.
	if r.PostFormValue(FieldMembers) != arcMembersDigest(memberSessions(rep.Reg)) {
		writePlain(w, http.StatusConflict, arcMembersChanged)
		return
	}
	// 🔴 A VETO'S SNAPSHOT: an `off` records the arc's members AS THIS ADMIN'S REQUEST FOUND THEM, so it
	// keeps covering them if they are later removed from the arc (`plugins.Record.Members`). Read from
	// the registration the authority just answered with, never from the form. `on` and `unset` carry
	// none: `on` is read against the arc's live members.
	if s.journalToggle(w, id, e.Name, plugins.LevelArc, plugins.ArcTarget(rep.Reg.Key()), value, vetoSnapshot(value, rep.Reg)...) {
		http.Redirect(w, r, arcHref(home.ID, rep.Reg.Slug), http.StatusSeeOther)
	}
}

// handlePluginSession is `POST /plugins/session`: the OWNER's opt-out.
func (s *Server) handlePluginSession(w http.ResponseWriter, r *http.Request, id identity.Identity) {
	e, value, ok := s.toggleForm(w, r, plugins.LevelSession)
	if !ok {
		return
	}
	session := r.PostFormValue(FieldSession)
	if !write.SessionComponent.MatchString(session) {
		writePlain(w, http.StatusForbidden, pluginToggleRefusal)
		return
	}
	owns, err := s.transcripts.OwnedBy(transcript.Viewer{Principal: id.Principal, Auth: id.Auth}, session)
	if err != nil {
		s.logf("plugins: a session's owner could not be read: %v", err)
	}
	if err != nil || !owns {
		writePlain(w, http.StatusForbidden, pluginToggleRefusal)
		return
	}
	if s.journalToggle(w, id, e.Name, plugins.LevelSession, session, value) {
		http.Redirect(w, r, sessionHref(session), http.StatusSeeOther)
	}
}

// PluginsView is the registry page's answer.
type PluginsView struct {
	// Configured is false in the OFF state.
	Configured bool
	Entries    []plugins.Entry
	// Scopes is, per plugin, the value on every scope this caller reads.
	Scopes map[string][]PluginScopeValue
	// Unreadable is the journal's BROKEN state.
	Unreadable bool
}

// PluginScopeValue is one readable scope's value for one plugin.
type PluginScopeValue struct {
	Scope Scope
	Value string
}

// handlePluginsPage is `GET /plugins`: the registry, and each plugin's value on every scope the caller
// reads. `content`: which scopes are listed is an answer about authority.
func (s *Server) handlePluginsPage(w http.ResponseWriter, r *http.Request, id identity.Identity) {
	scopes, err := s.source.Visible(id.Auth)
	if err != nil {
		writePlain(w, http.StatusInternalServerError, "the store could not be read")
		return
	}
	view := PageView{Viewer: id.Principal.Display, CSRF: csrfTokenFor(r), Scopes: scopes, Now: s.now(), App: s.app}
	pv := PluginsView{Configured: s.plugins != nil, Scopes: map[string][]PluginScopeValue{}}
	if s.plugins != nil {
		pv.Entries = s.plugins.Registry.Entries()
		st, err := s.plugins.Journal.Read()
		if err != nil {
			s.logf("plugins: the toggle journal could not be read: %v", err)
			pv.Unreadable = true
		} else {
			for _, e := range pv.Entries {
				for _, sc := range scopes {
					pv.Scopes[e.Name] = append(pv.Scopes[e.Name], PluginScopeValue{Scope: sc, Value: st.Scope(e.Name, sc.Name)})
				}
			}
		}
	}
	s.render(w, PluginsPage(view, pv))
}

// PluginsPage is the registry page.
func PluginsPage(v PageView, pv PluginsView) g.Node {
	return shell("plugins", v, []crumb{{Label: "plugins"}},
		h.Section(
			h.Class("card"), h.ID("plugins-index"),
			h.H2(g.Text("Plugins")),
			h.P(h.Class("note"), g.Text("Plugins are separate programs the operator registered. Each reads a session's "+
				"transcript only where it is ON for that session, and writes back derived records. Every toggle starts "+
				"off; a scope's admins consent on the scope page, an arc's on the arc page, and a session's owner can "+
				"opt one session out on its page.")),
			// 🔴 THE CONSENT SCREEN SAYS WHAT `allowed` DELEGATES, in the words the fold enforces.
			h.P(h.Class("note"), h.Data("plugins", "arc-consent"), g.Text("What `allowed` means: a scope set to `allowed` "+
				"lets an arc turn the plugin on for sessions that touched that scope. An arc that is `on` applies to "+
				"whatever sessions it lists, and anyone who can WRITE to the arc's home scope can add a session to it — "+
				"so such a writer can enrol a session, without an admin acting again, when the session touched the "+
				"arc's home scope and every other scope it touched is `allowed` or `on`. A scope left `off` is never "+
				"overridden by any arc.")),
			g.If(!pv.Configured, h.P(h.Class("read-only"), h.Data("plugins", "unconfigured"), g.Text(NoPluginRegistry))),
			g.If(pv.Unreadable, h.P(h.Class("read-only"), h.Data("plugins", "unreadable"), g.Text(pluginJournalUnreadable))),
			g.If(pv.Configured && len(pv.Entries) == 0, h.P(h.Class("empty"), g.Text("The registry holds no plugin."))),
			g.Map(pv.Entries, func(e plugins.Entry) g.Node { return pluginEntryNode(e, pv.Scopes[e.Name]) }),
		),
	)
}

func pluginEntryNode(e plugins.Entry, scopes []PluginScopeValue) g.Node {
	return h.Div(
		h.Data("plugin", e.Name),
		h.H3(g.Text(e.Name+" "+e.Version)),
		g.If(e.Description != "", h.P(g.Text(e.Description))),
		h.P(h.Class("note"), g.Text("Capabilities: "+strings.Join(e.Capabilities, ", ")+
			". Writes: "+orNone(e.OutputTypes())+".")),
		h.P(h.Class("note"), h.Data("calls-model", fmt.Sprint(e.CallsModel)), g.Text(map[bool]string{
			true:  "Sends what it reads to a model provider — outside this deployment once read.",
			false: "Does not call a model.",
		}[e.CallsModel])),
		// 🔴 AN `entry-row` WITH THE NAME IN A `.ref`, NOT A BARE `<li><a>`: a scope name can be one
		// unbreakable token longer than a phone is wide, and only the row classes carry the wrap rule
		// (and, under a coarse pointer, the 44px target). The bare list pushed this page sideways at
		// 390px — measured by `uiaudit` the first time its world registered a plugin (S6).
		g.If(len(scopes) > 0, h.Ul(h.Class("entry-list"), g.Map(scopes, func(sv PluginScopeValue) g.Node {
			return h.Li(h.Class("entry-row"),
				h.Span(h.Class("ref"), h.TitleAttr(sv.Scope.Name), h.A(h.Href(scopeHref(sv.Scope)), g.Text(sv.Scope.Name))),
				h.Span(h.Class("badge badge-quiet"), h.Data("toggle-value", sv.Value), g.Text(sv.Value)))
		}))),
	)
}

func orNone(xs []string) string {
	if len(xs) == 0 {
		return "nothing"
	}
	return strings.Join(xs, ", ")
}
