package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/arcs"
	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/plugins"
	"github.com/ZacxDev/cairn/internal/transcript"
	"github.com/ZacxDev/cairn/internal/transcript/transcripttest"
)

// The plugin toggles (S5 of the transcripts plan), driven through the share rig: a real control
// journal, a real store, real sessions, the real dispatcher, a real toggle journal. Rowan ADMINISTERS
// `quarry-notes` and is a plain MEMBER (read and write, no admin) of `commons-notes`, which wren owns.
// An arc homed in `commons-notes` lists rowan's session `s-rowan-01`, whose transcript rowan owns.

const toggleSession = "s-rowan-01"

func pluginsRig(t *testing.T) (*shareRig, plugins.Journal) {
	t.Helper()
	rig := newShareRig(t)
	reg, err := plugins.ParseRegistry([]byte(`{"schema":1,"plugins":[{"schema":1,"name":"summary","version":"0.1.0",` +
		`"description":"synthetic summaries","capabilities":["pending:list","transcript:read",` +
		`"output:write:summary"],"calls_model":true}]}`))
	if err != nil {
		t.Fatal(err)
	}
	journal := plugins.Journal{Path: filepath.Join(t.TempDir(), "toggles.jsonl")}
	arc := transcripttest.Open(t)
	transcripttest.Append(t, arc, transcripttest.Session{Root: toggleSession, Owner: "user:" + string(shareRowan)}, "main",
		[]string{trUser("a synthetic message")})
	store := transcript.NewStore(arc, rig.storeRoot)
	rig.srv.transcripts = store
	rig.srv.plugins = &plugins.Service{Registry: reg, Journal: journal, Transcripts: store}
	rig.srv.source = StoreSource{Root: rig.storeRoot, ArcJournal: arcsJournal(t,
		arcReg(shareOtherScopeName, "arc-one", arcs.StatusOpen, []string{shareOtherScopeName},
			arcs.Member{Session: toggleSession, Role: "wrote"}))}
	return rig, journal
}

func toggleRecords(t *testing.T, j plugins.Journal) []plugins.Record {
	t.Helper()
	var out []plugins.Record
	for _, line := range journalLines(t, j.Path) {
		var r plugins.Record
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func toggle(scope control.ID, value string) url.Values {
	return url.Values{FieldPlugin: {"summary"}, FieldScope: {string(scope)}, FieldValue: {value}}
}

// TestWhoMayToggleIsDecidedPerLevel is the S5 authorisation matrix: a scope's `write`-only member
// cannot toggle it and its admin can; a principal who can PUT an arc (write on its home) but is not
// admin of the home cannot toggle the arc and the home's admin can; a session's non-owner cannot opt
// it out and its owner can. Every refusal is the ONE body and journals nothing; every accepted toggle
// appends EXACTLY one record carrying `(Kind, ID)` and the pod clock (never `Display`).
func TestWhoMayToggleIsDecidedPerLevel(t *testing.T) {
	rig, journal := pluginsRig(t)
	rowan, wren := rig.signIn(shareRowanToken), rig.signIn(shareWrenToken)
	arcForm := func(value string) url.Values {
		return url.Values{FieldPlugin: {"summary"}, QueryHome: {string(shareOtherScope)}, QuerySlug: {"arc-one"}, FieldValue: {value},
			FieldMembers: {arcMembersDigest([]string{toggleSession})}}
	}
	sessionForm := func(value string) url.Values {
		return url.Values{FieldPlugin: {"summary"}, FieldSession: {toggleSession}, FieldValue: {value}}
	}
	steps := []struct {
		name   string
		who    *browser
		path   string
		form   url.Values
		status int
		by     control.ID
	}{
		{"a write-only member toggling the scope", rowan, PluginScopePath, toggle(shareOtherScope, "on"), http.StatusForbidden, ""},
		{"the scope's admin", rowan, PluginScopePath, toggle(shareScope, "allowed"), http.StatusSeeOther, shareRowan},
		{"a scope the caller cannot read", wren, PluginScopePath, toggle(shareScope, "on"), http.StatusForbidden, ""},
		{"an arc's writer who is not its home's admin", rowan, PluginArcPath, arcForm("on"), http.StatusForbidden, ""},
		{"the arc home's admin", wren, PluginArcPath, arcForm("off"), http.StatusSeeOther, shareWren},
		{"a session's non-owner", wren, PluginSessionPath, sessionForm("off"), http.StatusForbidden, ""},
		{"the session's owner", rowan, PluginSessionPath, sessionForm("off"), http.StatusSeeOther, shareRowan},
		{"a session-level ON, by its owner", rowan, PluginSessionPath, sessionForm("on"), http.StatusBadRequest, ""},
		{"a session-level ON, by a stranger", wren, PluginSessionPath, sessionForm("on"), http.StatusBadRequest, ""},
	}
	for _, s := range steps {
		before := len(toggleRecords(t, journal))
		rec := s.who.post(s.path, s.form)
		after := toggleRecords(t, journal)
		if rec.Code != s.status {
			t.Fatalf("%s answered %d %q, want %d", s.name, rec.Code, rec.Body.String(), s.status)
		}
		if s.status == http.StatusForbidden && rec.Body.String() != pluginToggleRefusal {
			t.Fatalf("%s answered %q, want the one refusal %q", s.name, rec.Body.String(), pluginToggleRefusal)
		}
		if s.by == "" {
			if len(after) != before {
				t.Fatalf("%s was refused and still journaled %d record(s)", s.name, len(after)-before)
			}
			continue
		}
		if len(after) != before+1 {
			t.Fatalf("%s journaled %d record(s), want exactly 1", s.name, len(after)-before)
		}
		got := after[len(after)-1]
		if got.By != (plugins.By{Kind: "user", ID: string(s.by)}) || got.At != "2000-06-01T12:00:00Z" || got.Plugin != "summary" {
			t.Fatalf("%s journaled %+v, want by user:%s at the pod clock", s.name, got, s.by)
		}
	}
	recs := toggleRecords(t, journal)
	if recs[0].Level != plugins.LevelScope || recs[0].Target != shareScopeName || recs[0].Value != "allowed" ||
		recs[1].Level != plugins.LevelArc || recs[1].Target != shareOtherScopeName+"/arc-one" ||
		recs[2].Level != plugins.LevelSession || recs[2].Target != toggleSession || recs[2].Value != "off" {
		t.Fatalf("the journal holds %+v", recs)
	}
}

// TestTheToggleRowsAreBehindBothCrossSiteGates: each toggle row refuses a cross-origin POST (gate 2)
// and a same-origin POST with no CSRF token (gate 6), each by its own message, and journals nothing.
func TestTheToggleRowsAreBehindBothCrossSiteGates(t *testing.T) {
	rig, journal := pluginsRig(t)
	rowan := rig.signIn(shareRowanToken)
	for _, path := range []string{PluginScopePath, PluginArcPath, PluginSessionPath} {
		for name, setup := range map[string]func(*http.Request){
			"cross-origin": func(r *http.Request) { r.Header.Set("Origin", "https://elsewhere.example.invalid") },
			"no csrf":      func(r *http.Request) { r.Header.Set("Origin", "https://"+r.Host) },
		} {
			form := toggle(shareScope, "on")
			if name == "cross-origin" {
				form.Set(FieldCSRF, "irrelevant")
			}
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.AddCookie(rowan.cookie)
			setup(req)
			rec := httptest.NewRecorder()
			rig.srv.ServeHTTP(rec, req)
			want := map[string]string{"cross-origin": crossSiteRefusal, "no csrf": csrfRefusal}[name]
			if rec.Code != http.StatusForbidden || rec.Body.String() != want {
				t.Fatalf("%s POST %s answered %d %q, want 403 %q", name, path, rec.Code, rec.Body.String(), want)
			}
		}
	}
	if n := len(toggleRecords(t, journal)); n != 0 {
		t.Fatalf("a refused toggle journaled %d record(s)", n)
	}
}

// TestTheToggleBlocksRenderWhereTheirAuthorityIs: the scope page carries the form for its admin and
// only the value for a write-only member; the session page carries the opt-out for the transcript's
// owner and NOTHING for anybody else; with no registry no page carries a block and the rows answer
// 501.
func TestTheToggleBlocksRenderWhereTheirAuthorityIs(t *testing.T) {
	rig, _ := pluginsRig(t)
	rowan, wren := rig.signIn(shareRowanToken), rig.signIn(shareWrenToken)
	admin := rowan.get(scopeURL(shareScope)).Body.String()
	member := rowan.get(scopeURL(shareOtherScope)).Body.String()
	if !strings.Contains(admin, `action="`+PluginScopePath+`"`) || !strings.Contains(admin, `data-toggle-value="off"`) {
		t.Fatalf("the admin's scope page carries no toggle form:\n%s", admin)
	}
	if strings.Contains(member, `action="`+PluginScopePath+`"`) || !strings.Contains(member, `id="plugin-toggles"`) {
		t.Fatal("a write-only member's scope page carries the toggle form, or no block at all")
	}
	owner := rowan.get(SessionPath + "?" + QuerySession + "=" + toggleSession).Body.String()
	if !strings.Contains(owner, `action="`+PluginSessionPath+`"`) {
		t.Fatalf("the owner's session page carries no opt-out:\n%s", owner)
	}
	if strings.Contains(owner, `<option value="on"`) {
		t.Fatal("the session opt-out offers ON")
	}
	if other := wren.get(SessionPath + "?" + QuerySession + "=" + toggleSession).Body.String(); strings.Contains(other, `plugin-toggles`) {
		t.Fatal("a non-owner's session page carries the plugin block")
	}
	if page := rowan.get(PluginsPath); page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `data-plugin="summary"`) {
		t.Fatalf("GET /plugins answered %d without the registered plugin", page.Code)
	}
	rig.srv.plugins = nil
	if strings.Contains(rowan.get(scopeURL(shareScope)).Body.String(), `plugin-toggles`) {
		t.Fatal("with no registry the scope page still carries a toggle block")
	}
	if rec := rowan.post(PluginScopePath, toggle(shareScope, "on")); rec.Code != http.StatusNotImplemented || rec.Body.String() != NoPluginRegistry {
		t.Fatalf("with no registry a toggle answered %d %q, want 501", rec.Code, rec.Body.String())
	}
	if page := rowan.get(PluginsPath); page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `data-plugins="unconfigured"`) {
		t.Fatal("with no registry GET /plugins does not say so")
	}
}
