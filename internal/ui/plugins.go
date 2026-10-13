package ui

import (
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
//     writer of the home scope has. It covers EXACTLY the members the arc listed when that admin set
//     it: the handler snapshots their ids into the journal record (`plugins.Record.Members`), and a
//     member a writer adds — or removes — afterwards changes nothing until an admin sets the toggle
//     again. And it speaks only for sessions whose visibility set holds its home
//     (`plugins.Effective`), so an arc homed elsewhere changes nothing for a session (T14). The arc
//     page says which members a toggle covers, and which it does not;
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
)

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
	// Covered and Uncovered are an ARC row's coverage, set when the value is `on` or `off`: Covered
	// is the member snapshot the toggle speaks for (the arc's members when an admin set it), and
	// Uncovered the arc's members NOW that are not in it — added since, and outside the toggle until
	// an admin saves it again.
	Covered, Uncovered []string
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
			// An arc toggle that says something covers its record's member snapshot, and only that.
			if level == plugins.LevelArc && row.Value != plugins.ValueUnset {
				row.Covered = slices.Clone(rec.Members)
				slices.Sort(row.Covered)
				for _, m := range members {
					if !slices.Contains(rec.Members, m) {
						row.Uncovered = append(row.Uncovered, m)
					}
				}
				slices.Sort(row.Uncovered)
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
		PluginArcPath, [][2]string{{QueryHome, string(home.ID)}, {QuerySlug, reg.Slug}}, memberSessions(reg)...)
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
			"and either every one is `on` or an arc — homed in a scope the session touched, and listing the session " +
			"when an admin of that scope turned the arc on — is `on`. `allowed` is consent to run where such an arc " +
			"turns it on. Default: off.",
		plugins.LevelArc: "An arc can turn a plugin on only inside scopes whose admins already allowed it, and only for " +
			"sessions that touched this arc's home scope. `on` and `off` cover exactly the members this arc listed when " +
			"the toggle was saved — shown below; a member added later is not covered until the toggle is saved again. " +
			"Default: unset.",
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

// arcCoverage says which members an arc toggle speaks for — nothing on another level, and nothing
// for an arc toggle that is `unset` (it covers nobody). Every id is `g.Text` with the whole id in
// `title=`, linked to its session page by this surface's own ledger path.
func arcCoverage(t PluginToggles, r PluginToggleRow) g.Node {
	if t.Level != plugins.LevelArc || r.Value == plugins.ValueUnset {
		return nil
	}
	list := func(ids []string) g.Node {
		labels := shortIDsIn(ids)
		return h.Ul(h.Class("chips chips-alias"), g.Map(ids, func(id string) g.Node {
			return h.Li(h.TitleAttr(id), h.A(h.Href(sessionHref(id)), g.Text(labels[id])))
		}))
	}
	covers := "`" + r.Value + "` covers " + plural(len(r.Covered), "member", "members") +
		" — the sessions this arc listed when the toggle was saved"
	one := len(r.Uncovered) == 1
	return g.Group([]g.Node{
		h.P(h.Class("note"), h.Data("covers", strconv.Itoa(len(r.Covered))), g.Text(covers+map[bool]string{true: ":", false: "."}[len(r.Covered) > 0])),
		g.If(len(r.Covered) > 0, list(r.Covered)),
		g.If(len(r.Uncovered) > 0, h.P(h.Class("read-only"), h.Data("uncovered", strconv.Itoa(len(r.Uncovered))),
			g.Text(plural(len(r.Uncovered), "member was", "members were")+" added to this arc since and "+
				map[bool]string{true: "is", false: "are"}[one]+" NOT covered — an admin of the home scope must save this "+
				"toggle again to cover "+map[bool]string{true: "it", false: "them"}[one]+":"))),
		g.If(len(r.Uncovered) > 0, list(r.Uncovered)),
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
// `members` is an ARC toggle's snapshot — the session ids the arc lists at this moment, as the
// handler just read them — and nothing on any other level.
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
	// 🔴 THE SNAPSHOT: the arc's members AS THIS ADMIN'S REQUEST FOUND THEM are what the toggle covers
	// (`plugins.Record.Members`). It is read from the registration the authority just answered with,
	// never from the form.
	if s.journalToggle(w, id, e.Name, plugins.LevelArc, plugins.ArcTarget(rep.Reg.Key()), value, memberSessions(rep.Reg)...) {
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
