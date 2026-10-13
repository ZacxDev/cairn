package ui

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/arcs"
	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/plugins"
	"github.com/ZacxDev/cairn/internal/transcript"
	"github.com/ZacxDev/cairn/internal/transcript/archive"
	"github.com/ZacxDev/cairn/internal/transcript/transcripttest"
)

// 🔴 PLUGIN OUTPUTS ON THE BROWSER SURFACE, AND THE OWNER'S DELETE (S6 of the transcripts plan),
// through the real dispatcher over a store on disk and a real transcript archive. Everything is
// SYNTHETIC: the S4 world's principals and sessions (`transcript_test.go`), the plugin `summary`
// 0.1.0 saying it used `fixture-model`, and text written here.

const (
	derivedSummary = "the session deployed the gadget and noted it"
	derivedSolo    = "a session that touched nothing, summarised"
)

func summaryBody(text string) string { return `{"text":"` + text + `"}` }

// TestAnOutputIsShownToExactlyTheViewersOfItsTranscript is clause (k): over S4's whole viewer
// matrix, the SET of viewers who see a session's output equals the SET who see its transcript — on
// the session page, and on the session's row in a list. A viewer the predicate refuses gets, on the
// page, the SAME BYTES it got before the output existed; on the list its row carries no derived
// line. Mutant `plugins-output-rendered-without-predicate`.
func TestAnOutputIsShownToExactlyTheViewersOfItsTranscript(t *testing.T) {
	w := transcriptWorld(t)
	a := w.archive(t)
	viewers := []string{"one", "none", "every", "owner"} // narrowest first: a walk is KEPT in the meta
	before := map[string]string{}
	for _, user := range viewers {
		for _, root := range []string{"s-full-01", "s-solo-02"} {
			before[user+"/"+root] = getAs(t, w.server(t, user, a), sessionURL(root)).Body.String()
		}
	}
	transcripttest.Output(t, a, "s-full-01", "summary", "0.1.0", "fixture-model", 4, plugins.TypeSummary, summaryBody(derivedSummary))
	transcripttest.Output(t, a, "s-solo-02", "summary", "0.1.0", "fixture-model", 2, plugins.TypeSummary, summaryBody(derivedSolo))

	for root, text := range map[string]string{"s-full-01": derivedSummary, "s-solo-02": derivedSolo} {
		var seeTranscript, seeOutput []string
		for _, user := range viewers {
			body := getAs(t, w.server(t, user, a), sessionURL(root)).Body.String()
			if strings.Contains(body, `id="transcript"`) {
				seeTranscript = append(seeTranscript, user)
			}
			if strings.Contains(body, `id="derived"`) || strings.Contains(body, text) {
				seeOutput = append(seeOutput, user)
			}
			if !strings.Contains(body, `id="transcript"`) && body != before[user+"/"+root] {
				t.Errorf("%s, refused %s's transcript, gets a page that CHANGED when an output was stored — it must be the "+
					"bytes of a session with no output", user, root)
			}
		}
		if !slices.Equal(seeOutput, seeTranscript) {
			t.Errorf("%s: its output is shown to %v and its transcript to %v — the two sets must be equal", root, seeOutput, seeTranscript)
		}
		want := map[string][]string{"s-full-01": {"every", "owner"}, "s-solo-02": {"owner"}}[root]
		if !slices.Equal(seeOutput, want) {
			t.Errorf("%s: its output is shown to %v, want %v", root, seeOutput, want)
		}
	}

	// THE ROWS. `/sessions` lists s-full-01 for everyone who reads a scope it wrote in — `one`
	// included — and only those the predicate admits get its derived line.
	for user, wantLine := range map[string]bool{"one": false, "every": true, "owner": true} {
		body := getAs(t, w.server(t, user, a), SessionsPath).Body.String()
		if !strings.Contains(body, `data-session="s-full-01"`) {
			t.Fatalf("CONTROL: %s's sessions list does not carry s-full-01's row", user)
		}
		if got := strings.Contains(body, derivedSummary); got != wantLine {
			t.Errorf("%s's sessions list: the derived line is shown=%v, want %v", user, got, wantLine)
		}
	}
	// "My sessions": the owner's rows carry it, the owner-only session's included.
	mine := getAs(t, w.server(t, "owner", a), MySessionsPath).Body.String()
	if !strings.Contains(mine, derivedSummary) || !strings.Contains(mine, derivedSolo) ||
		!strings.Contains(mine, `data-derived="summary"`) || !strings.Contains(visibleText(mine), "derived · summary 0.1.0: "+derivedSolo) {
		t.Errorf("the owner's My sessions rows do not carry each session's labelled derived line")
	}
	for _, user := range []string{"one", "every", "none"} {
		if body := getAs(t, w.server(t, user, a), MySessionsPath).Body.String(); strings.Contains(body, "derived ·") {
			t.Errorf("%s's My sessions carries a derived line", user)
		}
	}
	// The scope's sessions tab: the same rule, one page over.
	alpha := trID(control.PrefixScope, "alpha-notes")
	for user, wantLine := range map[string]bool{"one": false, "every": true} {
		body := getAs(t, w.server(t, user, a), scopeTabURL(alpha, TabSessions)).Body.String()
		if !strings.Contains(body, "s-full-01") {
			t.Fatalf("CONTROL: %s's alpha-notes sessions tab does not list s-full-01", user)
		}
		if got := strings.Contains(body, derivedSummary); got != wantLine {
			t.Errorf("%s's scope sessions tab: the derived line is shown=%v, want %v", user, got, wantLine)
		}
	}
	// A deployment with NO transcript store renders no derived line and no section anywhere.
	if body := getAs(t, w.server(t, "owner", nil), SessionsPath).Body.String(); strings.Contains(body, "derived ·") {
		t.Error("with no transcript store the sessions list carries a derived line")
	}
}

// TestAnOutputRendersAsALabelledClaim is decision 11: an output's block says `derived`, the plugin's
// name and version, the model, when it was produced and its watermark — "from turns 1–N of M", and
// "K newer turns not yet summarised" once the transcript grew past it. Only a plugin's LATEST
// summary is shown; earlier ones are counted.
func TestAnOutputRendersAsALabelledClaim(t *testing.T) {
	w := transcriptWorld(t)
	a := w.archive(t)
	transcripttest.Output(t, a, "s-solo-02", "summary", "0.1.0", "fixture-model", 2, plugins.TypeSummary, summaryBody("an EARLIER summary"))
	transcripttest.Output(t, a, "s-solo-02", "summary", "0.1.0", "fixture-model", 2, plugins.TypeSummary, summaryBody(derivedSolo))
	srv := w.server(t, "owner", a)
	body := getAs(t, srv, sessionURL("s-solo-02")).Body.String()
	text := visibleText(body)
	label := "derived · summary · by summary 0.1.0 · model fixture-model · 2000-01-02T03:04:05Z · from turn 1 of 1"
	if !strings.Contains(text, label) {
		t.Fatalf("the output's label is not %q:\n%s", label, text)
	}
	if strings.Contains(text, "newer turn") {
		t.Fatal("an output whose watermark is the transcript's end says turns are not yet summarised")
	}
	if !strings.Contains(body, derivedSolo) || strings.Contains(body, "an EARLIER summary") ||
		!strings.Contains(body, `data-superseded="1"`) || !strings.Contains(text, "1 earlier summary is stored and not shown") {
		t.Fatalf("the page does not show exactly the plugin's LATEST summary and count the earlier one")
	}
	if !strings.Contains(body, `data-output="o-000000000000000000000002"`) || !strings.Contains(body, `data-plugin="summary"`) ||
		!strings.Contains(text, "not part of the session's record") {
		t.Fatal("the block does not name its output and its plugin, or the section does not say an output is a claim")
	}
	// The transcript GROWS by two turns: the same output now says so.
	transcripttest.Append(t, a, w.session("s-solo-02"), "main", []string{trUser("a second message"), trAssistant("ok"), trUser("a third message")})
	text = visibleText(getAs(t, srv, sessionURL("s-solo-02")).Body.String())
	if want := "from turn 1 of 3; 2 newer turns not yet summarised"; !strings.Contains(text, want) {
		t.Fatalf("after the transcript grew by two turns the watermark does not read %q:\n%s", want, text)
	}
	// …and one summarising through the second turn (its first record is main 3) says one is left.
	transcripttest.Output(t, a, "s-solo-02", "summary", "0.1.0", "fixture-model", 4, plugins.TypeSummary, summaryBody("a later summary"))
	text = visibleText(getAs(t, srv, sessionURL("s-solo-02")).Body.String())
	if want := "from turns 1–2 of 3; 1 newer turn not yet summarised"; !strings.Contains(text, want) {
		t.Fatalf("the watermark does not read %q:\n%s", want, text)
	}
}

// TestAnOutputsTextIsNeverInterpreted: every byte a plugin wrote is rendered as text — markup is
// escaped, Markdown is not rendered, no link is built from it — with ONE exception: a ticket edge's
// CANONICAL id is linked through the entry pages' own ClickUp template. A stored body that does not
// validate (a `javascript:` ticket id, planted on disk past the API) is shown as its stored text and
// nothing in it is linked; the API half of that control is that the same body is REFUSED on write.
func TestAnOutputsTextIsNeverInterpreted(t *testing.T) {
	w := transcriptWorld(t)
	a := w.archive(t)
	hostile := `<script>alert(1)</script> [click](javascript:alert(2)) <a href=\"https://elsewhere.example.invalid\">x</a>`
	transcripttest.Output(t, a, "s-solo-02", "summary", "0.1.0", "fixture-model", 2, plugins.TypeSummary, summaryBody(hostile))
	transcripttest.Output(t, a, "s-solo-02", "clickup", "0.2.0", "none", 2, plugins.TypeTicketEdge,
		`{"system":"clickup","ticket_id":"abc123","custom_id":"GADGET-7","signal":"branch-name","locator":{"stream":"main","seq":1}}`)
	transcripttest.Output(t, a, "s-solo-02", "clickup", "0.2.0", "fixture-model", 2, plugins.TypeTicketSuggestion,
		`{"ticket_id":"def456","confidence":0.75,"rationale":"the session mentions the gadget"}`)
	planted := `{"system":"clickup","ticket_id":"javascript:alert(3)","signal":"pr-link","locator":{"pr":7}}`
	transcripttest.Output(t, a, "s-solo-02", "clickup", "0.2.0", "none", 2, plugins.TypeTicketEdge, planted)

	body := getAs(t, w.server(t, "owner", a), sessionURL("s-solo-02")).Body.String()
	if strings.Contains(body, "<script>alert(1)") || !strings.Contains(body, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Fatal("a summary's markup reached the page unescaped")
	}
	for _, bad := range []string{`href="javascript:`, `href="https://elsewhere.example.invalid`} {
		if strings.Contains(body, bad) {
			t.Fatalf("the page carries %s — a link built from plugin text", bad)
		}
	}
	text := visibleText(body)
	// The edge: its canonical id linked through the ClickUp template, its custom id as text, its signal
	// in words, its locator linking THIS surface's raw-record row.
	if !strings.Contains(body, `<a href="https://app.clickup.com/t/abc123">clickup:abc123</a>`) ||
		!strings.Contains(text, "(GADGET-7)") || !strings.Contains(text, "matched on branch name") ||
		!strings.Contains(body, `href="`+strings.ReplaceAll(transcriptRawHref("s-solo-02", "main", 1, 1), "&", "&amp;")+`"`) {
		t.Fatalf("the ticket edge does not render its templated link, custom id, signal and locator:\n%s", text)
	}
	if !strings.Contains(text, "derived · ticket edge · by clickup 0.2.0 · model none") {
		t.Fatal("the ticket edge is not labelled as derived, by its plugin")
	}
	// The suggestion says it is one, with its confidence, and is never worded as a match.
	if !strings.Contains(text, "suggested") || !strings.Contains(text, "confidence 0.75 · not a match") ||
		!strings.Contains(text, "the session mentions the gadget") || !strings.Contains(body, `https://app.clickup.com/t/def456`) {
		t.Fatalf("the suggestion does not render as a suggestion with its confidence:\n%s", text)
	}
	// The planted body: shown as stored, linked nowhere.
	if !strings.Contains(text, `"ticket_id":"javascript:alert(3)"`) || strings.Contains(body, `clickup.com/t/javascript`) ||
		!strings.Contains(text, "shown as stored and nothing in it is linked") {
		t.Fatalf("a stored body that does not validate is not rendered as inert text:\n%s", text)
	}
	// …and the write path refuses that body outright.
	if _, err := plugins.DecodeEnvelope([]byte(`{"plugin":"clickup","plugin_version":"0.2.0","model":"none","session":"s-solo-02",` +
		`"input_watermark":{"main":2},"type":"ticket-edge","body":` + planted + `}`)); err == nil {
		t.Fatal("CONTROL: the plugin API accepts a `javascript:` ticket id")
	}
}

// outputsRig is the share rig with a transcript store whose directory the test can read: rowan owns
// `s-rowan-01`'s transcript, which holds a summary and a ticket edge.
func outputsRig(t *testing.T) (*shareRig, *archive.Archive, string) {
	t.Helper()
	rig := newShareRig(t)
	arc, dir := transcripttest.OpenDir(t, func() time.Time { return transcripttest.Clock })
	transcripttest.Append(t, arc, transcripttest.Session{Root: toggleSession, Owner: "user:" + string(shareRowan)}, "main",
		[]string{trUser("a synthetic message"), trAssistant("a synthetic answer")})
	transcripttest.Output(t, arc, toggleSession, "summary", "0.1.0", "fixture-model", 2, plugins.TypeSummary, summaryBody(derivedSummary))
	transcripttest.Output(t, arc, toggleSession, "clickup", "0.2.0", "none", 2, plugins.TypeTicketEdge,
		`{"system":"clickup","ticket_id":"abc123","signal":"branch-name","locator":{"pr":7}}`)
	rig.srv.transcripts = transcript.NewStore(arc, rig.storeRoot)
	return rig, arc, dir
}

func deleteValues(session string) url.Values { return url.Values{FieldSession: {session}} }

// TestTheOwnerDeletesATranscriptAndEverythingDerivedGoesWithIt is clause (l) on the browser surface.
// The OWNER's delete removes the session's directory — the transcript, and with it the summary and
// the ticket edge — and journals the deletion (who, which root, why, when — no content); the plugin's
// next read of that session is its ONE refusal. Everybody else — a reader who is not the owner, and
// the owner asking about a transcript that is not stored or an id that is no session — gets the SAME
// refusal and deletes nothing.
func TestTheOwnerDeletesATranscriptAndEverythingDerivedGoesWithIt(t *testing.T) {
	rig, arc, dir := outputsRig(t)
	rowan, wren := rig.signIn(shareRowanToken), rig.signIn(shareWrenToken)
	sdir := filepath.Join(dir, "sessions", toggleSession)
	journal := filepath.Join(dir, "journal.jsonl")

	// The form is the owner's and nobody else's.
	if page := rowan.get(sessionURL(toggleSession)).Body.String(); !strings.Contains(page, `action="`+SessionDeletePath+`"`) ||
		!strings.Contains(page, `id="derived"`) {
		t.Fatalf("the owner's session page carries no delete form, or no derived section:\n%s", page)
	}
	if page := wren.get(sessionURL(toggleSession)).Body.String(); strings.Contains(page, SessionDeletePath) {
		t.Fatal("a non-owner's session page carries the delete form")
	}

	for name, c := range map[string]struct {
		who  *browser
		form url.Values
	}{
		"a principal who is not the owner":     {wren, deleteValues(toggleSession)},
		"the owner, for a session not stored":  {rowan, deleteValues("s-never-stored")},
		"the owner, for an id that is not one": {rowan, deleteValues("../" + toggleSession)},
	} {
		rec := c.who.post(SessionDeletePath, c.form)
		if rec.Code != http.StatusForbidden || rec.Body.String() != sessionDeleteRefusal {
			t.Errorf("%s: answered %d %q, want 403 %q", name, rec.Code, rec.Body.String(), sessionDeleteRefusal)
		}
	}
	if m, _ := arc.ReadMeta(toggleSession); m == nil || !arc.HasOutputs(toggleSession) || len(journalLines(t, journal)) != 0 {
		t.Fatal("a refused delete deleted or journaled something")
	}

	rec := rowan.post(SessionDeletePath, deleteValues(toggleSession))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != MySessionsPath {
		t.Fatalf("the owner's delete answered %d → %q, want 303 → %s: %s", rec.Code, rec.Header().Get("Location"), MySessionsPath, rec.Body.String())
	}
	if _, err := os.Stat(sdir); !os.IsNotExist(err) {
		t.Fatalf("the deleted session's directory is still there (%v)", err)
	}
	if arc.HasOutputs(toggleSession) {
		t.Fatal("the deleted session's outputs — its summary and its ticket edge — survived it")
	}
	lines := journalLines(t, journal)
	want := `{"event":"transcript-deleted","root":"` + toggleSession + `","owner":"user:` + string(shareRowan) +
		`","host":"host-a","reason":"owner-delete","at":"2000-01-02T03:04:05Z"}`
	if len(lines) != 1 || lines[0] != want {
		t.Fatalf("the journal holds\n%v\nwant the one fact\n%s", lines, want)
	}
	// The owner's page no longer carries the transcript, the output or the form; "My sessions" no row.
	if page := rowan.get(sessionURL(toggleSession)).Body.String(); strings.Contains(page, `id="transcript"`) ||
		strings.Contains(page, derivedSummary) || strings.Contains(page, SessionDeletePath) {
		t.Fatal("after the delete the owner's session page still carries the transcript, its output or the form")
	}
	if page := rowan.get(MySessionsPath).Body.String(); strings.Contains(page, `data-session="`+toggleSession+`"`) {
		t.Fatal("after the delete My sessions still lists the session")
	}
	// The plugin's next read: its ONE refusal, the bytes a never-stored id gets (not a 404 — `Open`).
	svc := &plugins.Service{Journal: plugins.Journal{Path: filepath.Join(t.TempDir(), "toggles.jsonl")},
		Transcripts: transcript.NewStore(arc, rig.storeRoot), Known: func(string) bool { return true }}
	for _, root := range []string{toggleSession, "s-never-stored"} {
		if _, err := svc.Open("summary", root); !errors.Is(err, plugins.ErrForbidden) {
			t.Fatalf("a plugin's read of %s after the delete: %v, want the one refusal", root, err)
		}
	}
	// A second delete of the same session is the refusal: there is nothing of the owner's there now.
	if rec := rowan.post(SessionDeletePath, deleteValues(toggleSession)); rec.Code != http.StatusForbidden || rec.Body.String() != sessionDeleteRefusal {
		t.Fatalf("deleting it again answered %d %q", rec.Code, rec.Body.String())
	}
	if n := len(journalLines(t, journal)); n != 1 {
		t.Fatalf("a refused repeat journaled: %d lines", n)
	}
}

// TestTheDeleteRowIsBehindBothCrossSiteGates: a cross-origin POST (gate 2) and a same-origin POST
// with no CSRF token (gate 6) are each refused by their own message — for the OWNER, with the
// confirmation ticked — and delete nothing.
func TestTheDeleteRowIsBehindBothCrossSiteGates(t *testing.T) {
	rig, arc, _ := outputsRig(t)
	rowan := rig.signIn(shareRowanToken)
	for name, setup := range map[string]func(*http.Request){
		"cross-origin": func(r *http.Request) { r.Header.Set("Origin", "https://elsewhere.example.invalid") },
		"no csrf":      func(r *http.Request) { r.Header.Set("Origin", "https://"+r.Host) },
	} {
		form := deleteValues(toggleSession)
		if name == "cross-origin" {
			form.Set(FieldCSRF, "irrelevant")
		}
		req := httptest.NewRequest(http.MethodPost, SessionDeletePath, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(rowan.cookie)
		setup(req)
		rec := httptest.NewRecorder()
		rig.srv.ServeHTTP(rec, req)
		want := map[string]string{"cross-origin": crossSiteRefusal, "no csrf": csrfRefusal}[name]
		if rec.Code != http.StatusForbidden || rec.Body.String() != want {
			t.Fatalf("%s POST %s answered %d %q, want 403 %q", name, SessionDeletePath, rec.Code, rec.Body.String(), want)
		}
	}
	if m, _ := arc.ReadMeta(toggleSession); m == nil || !arc.HasOutputs(toggleSession) {
		t.Fatal("a request refused by a cross-site gate deleted the transcript")
	}
	// With NO transcript store the row is the one refusal, never a crash.
	rig.srv.transcripts = nil
	if rec := rowan.post(SessionDeletePath, deleteValues(toggleSession)); rec.Code != http.StatusForbidden || rec.Body.String() != sessionDeleteRefusal {
		t.Fatalf("with no transcript store the row answered %d %q", rec.Code, rec.Body.String())
	}
}

// TestAnArcToggleSnapshotsItsMembersAndThePageSaysWhoIsCovered — #270's audit, closed in S6. When the
// arc home's ADMIN sets the toggle, the handler records the arc's members AT THAT MOMENT in the
// journal; a member a writer of the home adds afterwards is not in it. The arc page then says which
// members the toggle covers and which it does NOT, and the fold agrees with the page.
func TestAnArcToggleSnapshotsItsMembersAndThePageSaysWhoIsCovered(t *testing.T) {
	rig, journal := pluginsRig(t)
	wren := rig.signIn(shareWrenToken)
	arcForm := url.Values{FieldPlugin: {"summary"}, QueryHome: {string(shareOtherScope)}, QuerySlug: {"arc-one"}, FieldValue: {"on"}}
	if rec := wren.post(PluginArcPath, arcForm); rec.Code != http.StatusSeeOther {
		t.Fatalf("the home's admin setting the arc ON answered %d %q", rec.Code, rec.Body.String())
	}
	recs := toggleRecords(t, journal)
	if len(recs) != 1 || !slices.Equal(recs[0].Members, []string{toggleSession}) {
		t.Fatalf("the journaled arc toggle carries members %v, want the arc's members when it was set: [%s]", recs[0].Members, toggleSession)
	}
	// A scope toggle carries no snapshot.
	if rec := wren.post(PluginScopePath, toggle(shareOtherScope, "allowed")); rec.Code != http.StatusSeeOther {
		t.Fatalf("the scope toggle answered %d", rec.Code)
	}
	if recs = toggleRecords(t, journal); len(recs) != 2 || recs[1].Members != nil {
		t.Fatalf("a scope toggle journaled a member snapshot: %+v", recs)
	}
	page := wren.get(arcURL(shareOtherScope, "arc-one")).Body.String()
	if !strings.Contains(page, `data-covers="1"`) || strings.Contains(page, `data-uncovered`) ||
		!strings.Contains(visibleText(page), "`on` covers 1 member") {
		t.Fatalf("the arc page does not say the toggle covers its one member:\n%s", visibleText(page))
	}
	// A WRITER of the home re-registers the arc with a second member. No admin acted.
	rig.srv.source = StoreSource{Root: rig.storeRoot, ArcJournal: arcsJournal(t,
		arcReg(shareOtherScopeName, "arc-one", arcs.StatusOpen, []string{shareOtherScopeName},
			arcs.Member{Session: toggleSession, Role: "wrote"}, arcs.Member{Session: "s-added-02", Role: "wrote"}))}
	page = wren.get(arcURL(shareOtherScope, "arc-one")).Body.String()
	text := visibleText(page)
	if !strings.Contains(page, `data-covers="1"`) || !strings.Contains(page, `data-uncovered="1"`) ||
		!strings.Contains(text, "1 member was added to this arc since and is NOT covered") || !strings.Contains(page, `title="s-added-02"`) {
		t.Fatalf("the arc page does not say the member added since is NOT covered:\n%s", text)
	}
	st, err := journal.Read()
	if err != nil {
		t.Fatal(err)
	}
	if got := st.ArcsCovering("summary", []string{"s-added-02"}); len(got) != 0 {
		t.Fatalf("the fold covers the member added since: %v", got)
	}
	if got := st.ArcsCovering("summary", []string{toggleSession}); len(got) != 1 {
		t.Fatalf("the fold does not cover the member the admin saw: %v", got)
	}
	// The admin saves it again: both are covered, and the page says so.
	if rec := wren.post(PluginArcPath, arcForm); rec.Code != http.StatusSeeOther {
		t.Fatalf("saving the toggle again answered %d", rec.Code)
	}
	page = wren.get(arcURL(shareOtherScope, "arc-one")).Body.String()
	if !strings.Contains(page, `data-covers="2"`) || strings.Contains(page, `data-uncovered`) {
		t.Fatalf("after the admin saved the toggle again the page does not cover both members:\n%s", visibleText(page))
	}
}
