package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/store"
	"github.com/ZacxDev/cairn/internal/transcript"
	"github.com/ZacxDev/cairn/internal/transcript/archive"
	"github.com/ZacxDev/cairn/internal/transcript/transcripttest"
)

// 🔴 THE TRANSCRIPT ON THE SESSION PAGE, THROUGH THE REAL DISPATCHER OVER THE REAL `StoreSource`, A
// STORE ON DISK AND A REAL TRANSCRIPT ARCHIVE (S4 of the transcripts plan). Everything is SYNTHETIC.
//
// The principals (one project per scope, authority through `control.Resolve`):
//
//	owner  reads alpha            — the capture owner: every stored session below is its
//	every  reads alpha beta gamma — reads every scope any session touched
//	one    reads alpha            — reads ONE scope the session wrote
//	none   reads delta            — a stranger
//
// The sessions:
//
//	s-full-01  wrote alpha and beta, READ gamma (a rendered recall in a tool result) — V = {alpha, beta, gamma}
//	s-solo-02  no recorded write or read                                           — V = {}
//	s-bare-09  wrote alpha; NO transcript stored — the "session with no transcript" every refusal is compared with

type trWorld struct {
	store string
	ids   map[string]identity.Identity
	owner string
	clock *trClock
}

type trClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *trClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *trClock) Add(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func trID(prefix, name string) control.ID { return control.DerivedID(prefix, "tr-"+name) }

func transcriptWorld(t *testing.T) trWorld {
	t.Helper()
	reads := map[string][]string{
		"owner": {"alpha-notes"}, "every": {"alpha-notes", "beta-notes", "gamma-notes"},
		"one": {"alpha-notes"}, "none": {"delta-notes"},
	}
	users := []string{"owner", "every", "one", "none"}
	at := browseClock
	var events []control.Event
	for i, user := range users {
		events = append(events, control.Event{Kind: control.EventUserCreated, At: at, UserID: trID(control.PrefixUser, user),
			Provider: "fixture-provider", Subject: fmt.Sprintf("00000000-0000-4000-8000-0000000000%02d", 80+i),
			Email: user + "@notes.example.invalid"})
	}
	for _, scope := range []string{"alpha-notes", "beta-notes", "gamma-notes", "delta-notes"} {
		project := trID(control.PrefixProject, scope)
		events = append(events,
			control.Event{Kind: control.EventProjectCreated, At: at, ProjectID: project, Name: "p-" + scope,
				UserID: trID(control.PrefixUser, "none")},
			control.Event{Kind: control.EventScopeCreated, At: at, ScopeID: trID(control.PrefixScope, scope),
				DisplayName: scope, ProjectID: project})
		for _, user := range users {
			if slices.Contains(reads[user], scope) {
				events = append(events, control.Event{Kind: control.EventMemberSet, At: at, ProjectID: project,
					UserID: trID(control.PrefixUser, user), Role: control.RoleOwner})
			}
		}
	}
	m, err := control.Replay(events)
	if err != nil {
		t.Fatalf("building the transcript world: %v", err)
	}
	w := trWorld{store: t.TempDir(), ids: map[string]identity.Identity{},
		owner: string(control.KindUser) + ":" + string(trID(control.PrefixUser, "owner")),
		clock: &trClock{now: transcripttest.Clock}}
	for _, user := range users {
		p, known := m.PrincipalFor(control.KindUser, trID(control.PrefixUser, user))
		if !known {
			t.Fatalf("no principal %s", user)
		}
		w.ids[user] = identity.Identity{Principal: p, Auth: control.Resolve(m, p)}
	}
	put := func(scope, service, nuance string) {
		dir := filepath.Join(w.store, scope)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := "---\nservice: " + service + "\nscope: " + scope + "\n---\n\n## What it is\n\nsynthetic.\n\n" +
			store.NuanceHeading + "\n\n" + nuance
		if err := os.WriteFile(filepath.Join(dir, service+".md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put("alpha-notes", "kettle", "- 2000-01-03: boiled [cairn: fixture-bot/s-full-01]\n"+
		"- 2000-01-03: steeped [cairn: fixture-bot/s-bare-09]\n- 2000-01-03: poured [cairn: fixture-bot/s-render-03]\n")
	put("beta-notes", "ladle", "- 2000-01-04: stirred [cairn: fixture-bot/s-full-01]\n")
	put("gamma-notes", "spoon", "- 2000-01-05: unsigned\n")
	put("delta-notes", "fork", "- 2000-01-06: unsigned\n")
	return w
}

const trSecretText = "the owner asked for the gadget to be deployed"

func (w trWorld) session(root string) transcripttest.Session {
	return transcripttest.Session{Root: root, Owner: w.owner}
}

// archive returns an archive holding s-full-01 and s-solo-02 (and nothing for s-bare-09).
func (w trWorld) archive(t *testing.T) *archive.Archive {
	t.Helper()
	a := transcripttest.OpenAt(t, w.clock.Now)
	header, _ := json.Marshal("subsystem-recall: status=recalled scope=gamma-notes entries=1\n- synthetic")
	transcripttest.Append(t, a, w.session("s-full-01"), "main", []string{
		trUser(trSecretText),
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_tr0001","name":"Bash",` +
			`"input":{"command":"\"$BIN\" recall --scope gamma-notes"}}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_tr0001","content":` +
			string(header) + `}]}}`,
		trAssistant("deployed it"),
	})
	transcripttest.Append(t, a, w.session("s-solo-02"), "main", []string{trUser("a session that touched nothing"), trAssistant("noted")})
	return a
}

func trUser(text string) string {
	b, _ := json.Marshal(text)
	return `{"type":"user","message":{"role":"user","content":` + string(b) + `}}`
}

func trAssistant(text string) string {
	b, _ := json.Marshal(text)
	return `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":` + string(b) + `}]}}`
}

// server builds the surface for one viewer. A nil archive is a deployment with NO transcript store.
func (w trWorld) server(t *testing.T, user string, a *archive.Archive) *Server {
	t.Helper()
	cfg := testConfig(t, staticAuth{w.ids[user]})
	cfg.Source = StoreSource{Root: w.store}
	cfg.Now = func() time.Time { return sessNow }
	if a != nil {
		cfg.Transcripts = transcript.NewStore(a, w.store)
	}
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func rawURL(session string, more ...string) string {
	q := url.Values{QuerySession: []string{session}}
	for i := 0; i+1 < len(more); i += 2 {
		q.Set(more[i], more[i+1])
	}
	return SessionTranscriptPath + "?" + q.Encode()
}

// TestTheTranscriptSectionIsShownToExactlyThoseThePredicateAdmits — clauses (d), (e) and (f) on the
// page: the owner and the reader of every scope see the section and the content; the reader of ONE
// written scope gets the PAGE (it reads a scope the session wrote in) and no section, no content,
// no link to it; a stranger gets the unseen answer.
func TestTheTranscriptSectionIsShownToExactlyThoseThePredicateAdmits(t *testing.T) {
	w := transcriptWorld(t)
	a := w.archive(t)
	for _, user := range []string{"owner", "every"} {
		rec := getAs(t, w.server(t, user, a), sessionURL("s-full-01"))
		body := rec.Body.String()
		if rec.Code != http.StatusOK || !strings.Contains(body, `id="transcript"`) || !strings.Contains(body, trSecretText) {
			t.Errorf("%s: the session page (%d) does not show the transcript section with its content", user, rec.Code)
		}
		if !strings.Contains(body, `href="#transcript"`) || !strings.Contains(visibleText(body), "transcript · 1 turn") {
			t.Errorf("%s: the summary card does not link the transcript", user)
		}
	}
	rec := getAs(t, w.server(t, "one", a), sessionURL("s-full-01"))
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `id="session-summary"`) || !strings.Contains(body, `data-scope="alpha-notes"`) {
		t.Fatalf("CONTROL: the reader of one written scope does not get the session PAGE (%d) — the refusal below "+
			"would be the page's, not the transcript's", rec.Code)
	}
	for _, leak := range []string{`id="transcript"`, trSecretText, "#transcript", "transcript", "gamma-notes", "deployed it", SessionTranscriptPath} {
		if strings.Contains(body, leak) {
			t.Errorf("THE REFUSED VIEWER'S PAGE CONTAINS %q", leak)
		}
	}
	if rec := getAs(t, w.server(t, "none", a), sessionURL("s-full-01")); rec.Code != http.StatusNotFound || rec.Body.String() != sessionUnseenBody {
		t.Errorf("a stranger's request answered %d %q, want the unseen answer", rec.Code, rec.Body.String())
	}
}

// TestARefusedViewerGetsTheBytesOfASessionWithNoTranscript — the byte identity decision 4 states,
// measured three ways for each refused viewer and BOTH rows: over an archive that holds the
// transcript, over an archive that never held it, over a deployment with no archive at all, and
// over an archive the retention sweep deleted it from. Hidden, absent, off and deleted are one
// response, byte for byte. RED under `transcript-section-without-predicate`.
func TestARefusedViewerGetsTheBytesOfASessionWithNoTranscript(t *testing.T) {
	w := transcriptWorld(t)
	holds := w.archive(t)
	empty := transcripttest.OpenAt(t, w.clock.Now)
	swept := w.archive(t)

	// POSITIVE CONTROL FIRST: with the transcript stored, an ADMITTED viewer's page differs from the
	// no-transcript page — so equal bytes below are the predicate's doing, not a section that never
	// renders.
	with := getAs(t, w.server(t, "owner", holds), sessionURL("s-full-01")).Body.String()
	without := getAs(t, w.server(t, "owner", empty), sessionURL("s-full-01")).Body.String()
	if with == without || !strings.Contains(with, trSecretText) {
		t.Fatal("POSITIVE CONTROL FAILED: the owner's page is the same with and without a stored transcript")
	}

	w.clock.Add(transcripttest.Retention + time.Hour)
	if deleted, err := swept.Sweep(); err != nil || !slices.Contains(deleted, "s-full-01") {
		t.Fatalf("the sweep deleted %v (%v), want s-full-01 among them", deleted, err)
	}
	w.clock.Add(-(transcripttest.Retention + time.Hour))

	for _, user := range []string{"one", "none"} {
		for _, path := range []string{sessionURL("s-full-01"), rawURL("s-full-01"), rawURL("s-full-01", QueryStream, "main", QuerySeq, "1")} {
			hidden := getAs(t, w.server(t, user, holds), path)
			for name, a := range map[string]*archive.Archive{"an archive that never held it": empty, "no archive": nil, "an archive it was swept from": swept} {
				other := getAs(t, w.server(t, user, a), path)
				if hidden.Code != other.Code || hidden.Body.String() != other.Body.String() {
					t.Errorf("%s GET %s: with the transcript stored and hidden the answer is %d (%d bytes); over %s it is %d (%d bytes). "+
						"A refused viewer must get the bytes of a session with no transcript.",
						user, path, hidden.Code, hidden.Body.Len(), name, other.Code, other.Body.Len())
				}
			}
		}
	}
	// And the same viewer's page for a session that never had a transcript carries no trace either.
	bare := getAs(t, w.server(t, "one", holds), sessionURL("s-bare-09"))
	if bare.Code != http.StatusOK || strings.Contains(bare.Body.String(), "transcript") {
		t.Errorf("the no-transcript session's page (%d) mentions a transcript", bare.Code)
	}
	// The owner's own page after the sweep is the no-transcript page too.
	if got := getAs(t, w.server(t, "owner", swept), sessionURL("s-full-01")).Body.String(); got != without {
		t.Error("after the retention sweep the owner's page is not the no-transcript page")
	}
}

// TestASessionWithNoRecordedWriteIsFoundForItsOwnerAlone — clause (g), the visibility half (O10):
// a session with no write and no read has an EMPTY visibility set. Its owner gets a page — found by
// the transcript, since no trailer names it — and everyone else, the reader of every scope
// included, gets the bytes of a session nobody ever wrote. RED under `transcript-empty-set-visible`.
func TestASessionWithNoRecordedWriteIsFoundForItsOwnerAlone(t *testing.T) {
	w := transcriptWorld(t)
	a := w.archive(t)
	rec := getAs(t, w.server(t, "owner", a), sessionURL("s-solo-02"))
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `id="transcript"`) || !strings.Contains(body, "a session that touched nothing") {
		t.Fatalf("the owner's page for its own no-write session answered %d without the transcript", rec.Code)
	}
	text := strings.Join(strings.Fields(visibleText(body)), " ")
	if !strings.Contains(text, "no write by this session is visible to you") || strings.Contains(text, "0 bullets") {
		t.Errorf("the transcript-only page does not say it found no write (or still counts bullets):\n%.600s", text)
	}
	never := getAs(t, w.server(t, "every", a), sessionURL("s-never-00"))
	for _, user := range []string{"every", "one", "none"} {
		for _, path := range []string{sessionURL("s-solo-02"), rawURL("s-solo-02")} {
			got := getAs(t, w.server(t, user, a), path)
			if got.Code != http.StatusNotFound {
				t.Errorf("%s GET %s answered %d, want 404: an empty visibility set is owner-only", user, path, got.Code)
			}
		}
		if got := getAs(t, w.server(t, user, a), sessionURL("s-solo-02")); got.Body.String() != never.Body.String() {
			t.Errorf("%s: the owner-only session's page is not byte-identical to a never-written one", user)
		}
	}
	// A page the WRITES found is untouched by an owner-only transcript existing elsewhere: no
	// transcript-only note on it.
	if rec := getAs(t, w.server(t, "one", a), sessionURL("s-bare-09")); strings.Contains(rec.Body.String(), "no write by this session") {
		t.Error("a page found by its writes carries the transcript-only note")
	}
}

// renderSession stores the session decision 18's rendering rules are measured on, and returns its
// distinct pieces.
type renderFixture struct {
	typed, long, lines, big string
}

func (w trWorld) renderArchive(t *testing.T) (*archive.Archive, renderFixture) {
	t.Helper()
	f := renderFixture{
		typed: strings.Repeat("a human typed this, in full. ", 20_000/29+1)[:20_000],
		long:  strings.Repeat("the assistant goes on. ", 5_000/23+1)[:5_000],
		lines: strings.TrimSuffix(strings.Repeat("a short line\n", 20), "\n"),
		big:   strings.Repeat("Q", 600_000),
	}
	a := transcripttest.OpenAt(t, w.clock.Now)
	q := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	transcripttest.Append(t, a, w.session("s-render-03"), "main", []string{
		`{"type":"permission-mode","permissionMode":"default"}`,
		trUser(f.typed),
		trAssistant(f.long),
		trAssistant(f.lines),
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"weighing it up"},` +
			`{"type":"tool_use","id":"toolu_rn0001","name":"Bash","input":{"command":"false"}}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_rn0001",` +
			`"content":"Exit code 1","is_error":true}]},"toolUseResult":{"stdout":"","stderr":""}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_rn0002","name":"Bash",` +
			`"input":{"command":"seq 1 99999"}}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_rn0002","content":` +
			q(f.big) + `}]},"toolUseResult":{"stdout":` + q(f.big) + `}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_rn0003","name":"Agent",` +
			`"input":{"description":"count the gadgets","subagent_type":"general-purpose","prompt":"count them"}}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_rn0003",` +
			`"content":"counted"}]},"toolUseResult":{"agentId":"a1b2c3d4"}}`,
		`{"type":"system","subtype":"compact_boundary","content":"Conversation compacted"}`,
		`{"type":"user","isCompactSummary":true,"message":{"role":"user","content":"This session is being continued."}}`,
		`{"type":"ai-title","aiTitle":"gadgets"}`,
		trUser("and a second message"),
		trAssistant("a short answer"),
	})
	transcripttest.Append(t, a, w.session("s-render-03"), "subagent:a1b2c3d4", []string{
		`{"isSidechain":true,"agentId":"a1b2c3d4","type":"user","message":{"role":"user","content":"count them"}}`,
		`{"isSidechain":true,"agentId":"a1b2c3d4","type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"there are seven"}]}}`,
	})
	return a, f
}

// TestTheTranscriptRendersCollapsed is decision 18, by its own numbers:
//
//   - a 20,000-character user message is rendered WHOLE (asserted by byte count — a renderer that
//     clips user text fails it);
//   - a 5,000-character assistant message renders its first 1,200 characters, then
//     `read more (3800 more characters)` holding the rest; twenty short lines render fifteen;
//   - a tool call is a `<details>` summarised `Bash · exit 1 · 11 B`;
//   - a 600 KB tool result inside a `user` record is NOT a user message: it shows its first 64 KiB
//     and links the raw record (RED under `transcript-render-classifies-by-record-type`, which
//     renders all 600,000 bytes);
//   - a subagent nests under the call that spawned it; bookkeeping and duplicate copies are ONE
//     `<details>` per turn; the compaction summary is labelled runtime-generated;
//   - and the page carries no script.
func TestTheTranscriptRendersCollapsed(t *testing.T) {
	w := transcriptWorld(t)
	a, f := w.renderArchive(t)
	rec := getAs(t, w.server(t, "owner", a), sessionURL("s-render-03"))
	if rec.Code != http.StatusOK {
		t.Fatalf("the page answered %d", rec.Code)
	}
	body := rec.Body.String()

	// The user message, whole and once, in a user block.
	if n := strings.Count(body, f.typed); n != 1 {
		t.Errorf("the 20,000-character user message appears whole %d time(s), want exactly 1 — a user message is never clipped", n)
	}
	if !regexp.MustCompile(`<div class="t-user" data-class="human-text"><p class="t-label">user</p><div class="t-text">a human typed this`).MatchString(body) {
		t.Error("the user message is not in a `t-user` block")
	}

	// Assistant text: the head, then the reveal holding the rest.
	head, rest := f.long[:1200], f.long[1200:]
	want := `<div class="t-text">` + head + `</div><details class="t-more"><summary>read more (3800 more characters)</summary>` +
		`<div class="t-text">` + rest + `</div></details>`
	if !strings.Contains(body, want) {
		t.Error("the 5,000-character assistant message is not rendered as its first 1,200 characters and a " +
			"`read more (3800 more characters)` reveal holding the other 3,800")
	}
	fifteen := strings.TrimSuffix(strings.Repeat("a short line\n", 15), "\n")
	if !strings.Contains(body, `<div class="t-text">`+fifteen+`</div><details class="t-more"><summary>read more (65 more characters)</summary>`) {
		t.Error("twenty short lines are not rendered as fifteen and a reveal of the other five (65 characters)")
	}
	if strings.Contains(body, `a short answer</div><details`) || !strings.Contains(body, `<div class="t-text">a short answer</div></div>`) {
		t.Error("a short assistant message carries a reveal, or is not rendered plainly")
	}

	// Tool calls, collapsed.
	if !strings.Contains(body, `<details class="t-block" data-class="tool-call"><summary>Bash · exit 1 · 11 B</summary>`) {
		t.Error("the failed Bash call is not a `<details>` summarised `Bash · exit 1 · 11 B`")
	}
	// The 600 KB result: 64 KiB of it, and the raw record one link away.
	if n := strings.Count(body, "Q"); n < toolResultInline || n > toolResultInline+64 {
		t.Errorf("the page carries %d bytes of the 600,000-byte tool result, want its first %d", n, toolResultInline)
	}
	if !strings.Contains(body, `<summary>Bash · 600.0 KB</summary>`) || !strings.Contains(visibleText(body), "the first 65.5 KB of 600.0 KB") ||
		!strings.Contains(body, `href="/session/transcript?seq=8&amp;session=s-render-03&amp;stream=main&amp;to=8"`) {
		t.Error("the 600 KB result does not say how much is shown or link its raw record (seq 8)")
	}
	if rec.Body.Len() > 200_000 {
		t.Errorf("the page is %d bytes; with one 20 KB message and 64 KiB of one result it should be well under 200,000", rec.Body.Len())
	}

	// The subagent, nested inside its spawning call's block.
	agent := strings.Index(body, `<summary>Agent · 7 B</summary>`)
	child := strings.Index(body, `<details class="t-block" data-stream="subagent:a1b2c3d4"><summary>subagent · general-purpose · count the gadgets · 2 items</summary>`)
	if agent < 0 || child < agent || strings.Contains(body[agent:child], "</details>") {
		t.Errorf("the subagent is not nested inside the Agent call that spawned it (call at %d, child at %d)", agent, child)
	}
	if !strings.Contains(body, `data-class="agent-prompt"><p class="t-label">prompt (written by the parent agent)</p><div class="t-text">count them</div>`) ||
		!strings.Contains(body, "there are seven") {
		t.Error("the subagent's prompt and answer are not inside it, the prompt labelled as the parent agent's")
	}

	// One collapsed line per turn for what is not shown: turn 1 has the mode and title records and
	// three duplicate copies; turn 2 has nothing to count and so no line.
	if n := strings.Count(body, "bookkeeping record"); n != 1 || !strings.Contains(body, `<summary>2 bookkeeping records, 3 duplicate tool-result copies</summary>`) {
		t.Errorf("the page carries %d bookkeeping lines; want ONE, `2 bookkeeping records, 3 duplicate tool-result copies`", n)
	}
	if !strings.Contains(body, `data-class="runtime"><summary>runtime-generated · compaction summary</summary>`) ||
		!strings.Contains(body, `<summary>runtime-generated · compact boundary</summary>`) {
		t.Error("the compaction summary and its boundary are not labelled runtime-generated")
	}
	if !strings.Contains(body, `<summary>thinking · 14 characters</summary>`) {
		t.Error("the thinking block is not collapsed behind its size")
	}
	if n := strings.Count(body, `class="turn"`); n != 2 || !strings.Contains(body, `id="turn-2"`) {
		t.Errorf("the page renders %d turns, want 2", n)
	}
	if strings.Contains(body, "<script") {
		t.Error("the session page with a transcript carries a script element; every reveal must be a <details>")
	}
	if len(AllowedScriptSources()) != 3 {
		t.Errorf("AllowedScriptSources has %d entries; the transcript must add none to the three it had", len(AllowedScriptSources()))
	}
}

// TestHostileTranscriptTextIsEscaped — T9: a record whose every string is markup renders as text on
// the session page and on the raw-record page; nothing a transcript carries becomes an element, an
// attribute or a link target.
func TestHostileTranscriptTextIsEscaped(t *testing.T) {
	const hostile = `<script>alert(1)</script><img src=x onerror=alert(2)>"'><a href="javascript:alert(3)">x</a>`
	w := transcriptWorld(t)
	a := transcripttest.OpenAt(t, w.clock.Now)
	// Encoded WITHOUT JSON's HTML escaping, so the STORED bytes hold the literal markup — the raw
	// view shows stored bytes, and `\u003c` there would prove nothing about `<`.
	var enc strings.Builder
	e := json.NewEncoder(&enc)
	e.SetEscapeHTML(false)
	if err := e.Encode(hostile); err != nil {
		t.Fatal(err)
	}
	h := strings.TrimSpace(enc.String())
	transcripttest.Append(t, a, w.session("s-full-01"), "main", []string{
		`{"type":"user","message":{"role":"user","content":` + h + `}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":` + h + `}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":` + h + `},` +
			`{"type":"tool_use","id":"toolu_hx0001","name":` + h + `,"input":{"command":` + h + `,"subagent_type":` + h + `,"description":` + h + `}}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_hx0001","content":[` +
			`{"type":"text","text":` + h + `},{"type":"image","source":{"media_type":` + h + `,"data":"aGk="}}],"is_error":true}]},` +
			`"toolUseResult":{"agentId":"hx01"}}`,
		`{"type":"system","subtype":` + h + `,"content":` + h + `}`,
		`{"type":` + h + `,"payload":` + h + `}`,
	})
	transcripttest.Append(t, a, w.session("s-full-01"), "subagent:hx01", []string{
		`{"isSidechain":true,"type":"user","message":{"role":"user","content":` + h + `}}`})
	srv := w.server(t, "owner", a)
	for _, path := range []string{sessionURL("s-full-01"), rawURL("s-full-01"), rawURL("s-full-01", QueryStream, "subagent:hx01")} {
		rec := getAs(t, srv, path)
		body := rec.Body.String()
		if rec.Code != http.StatusOK || !strings.Contains(body, "&lt;script&gt;alert(1)&lt;/script&gt;") {
			t.Fatalf("POSITIVE CONTROL FAILED: GET %s (%d) does not carry the hostile text at all, so its absence as markup proves nothing", path, rec.Code)
		}
		for _, live := range []string{"<script>alert", "<img src=x", `href="javascript:`, "onerror=alert(2)>\"'><a"} {
			if strings.Contains(body, live) {
				t.Errorf("GET %s renders transcript text as markup: found %q", path, live)
			}
		}
		for _, m := range regexp.MustCompile(`href="([^"]*)"`).FindAllStringSubmatch(body, -1) {
			if !strings.HasPrefix(m[1], "/") && !strings.HasPrefix(m[1], "#") {
				t.Errorf("GET %s carries a link that is not this surface's own path: %q", path, m[1])
			}
		}
	}
}

// TestTheTranscriptPagesByFiftyTurns — `?turn=`: 120 turns are three pages; a page starts at the
// turn asked for and links the page fifty turns back and the next page's first turn; a turn past the
// end is an empty page that links back to the session's last turns.
func TestTheTranscriptPagesByFiftyTurns(t *testing.T) {
	w := transcriptWorld(t)
	a := transcripttest.OpenAt(t, w.clock.Now)
	var lines []string
	for i := 1; i <= 120; i++ {
		lines = append(lines, trUser(fmt.Sprintf("message number %d.", i)))
	}
	transcripttest.Append(t, a, w.session("s-full-01"), "main", lines)
	srv := w.server(t, "owner", a)
	for _, tc := range []struct {
		query       string
		first, last int
		links       []string
	}{
		{"", 1, 50, []string{"turn=51#transcript"}},
		{"&turn=1", 1, 50, []string{"turn=51#transcript"}},
		{"&turn=nonsense", 1, 50, []string{"turn=51#transcript"}},
		// A page starts AT the turn asked for (#261 round 3); the previous link is fifty turns back.
		{"&turn=60", 60, 109, []string{"turn=10#transcript", "turn=110#transcript"}},
		{"&turn=51", 51, 100, []string{`session=s-full-01#transcript`, "turn=101#transcript"}},
		{"&turn=101", 101, 120, []string{"turn=51#transcript"}},
	} {
		body := getAs(t, srv, sessionURL("s-full-01")+tc.query).Body.String()
		if n := strings.Count(body, `class="turn"`); n != tc.last-tc.first+1 {
			t.Errorf("?%s renders %d turns, want %d–%d", tc.query, n, tc.first, tc.last)
		}
		for _, n := range []int{tc.first, tc.last} {
			if !strings.Contains(body, fmt.Sprintf(`id="turn-%d"`, n)) || !strings.Contains(body, fmt.Sprintf("message number %d.", n)) {
				t.Errorf("?%s does not render turn %d", tc.query, n)
			}
		}
		for _, n := range []int{tc.first - 1, tc.last + 1} {
			if n >= 1 && n <= 120 && strings.Contains(body, fmt.Sprintf("message number %d.", n)) {
				t.Errorf("?%s renders turn %d, which is on another page", tc.query, n)
			}
		}
		for _, link := range tc.links {
			if !strings.Contains(body, link) {
				t.Errorf("?%s carries no pager link %q", tc.query, link)
			}
		}
	}
	// Past the end, up to the largest number `?turn=` parses as (a wider one does not parse and is
	// page one, like any other unrecognised value — the `nonsense` row above).
	for _, beyond := range []string{"5000", "9223372036854775807"} {
		past := getAs(t, srv, sessionURL("s-full-01")+"&turn="+beyond).Body.String()
		if strings.Contains(past, `class="turn"`) || !strings.Contains(past, "turn=71#transcript") || !strings.Contains(visibleText(past), "of 120") ||
			strings.Contains(visibleText(past), "turns -") {
			t.Errorf("?turn=%s is not an empty page linking back to the last real one (or it renders a negative page)", beyond)
		}
	}
}

// TestTheRawRecordViewAsksThePredicate: the raw row serves the stored lines to exactly the viewers
// the predicate admits — byte-exact, bounded, by stream and record range — and answers ONE body for
// every miss: a refused viewer, a session with no transcript, a stream the session does not hold, a
// record past the end, an id that is no session, and a deployment with no archive.
func TestTheRawRecordViewAsksThePredicate(t *testing.T) {
	w := transcriptWorld(t)
	a := w.archive(t)
	for _, user := range []string{"owner", "every"} {
		rec := getAs(t, w.server(t, user, a), rawURL("s-full-01"))
		body := rec.Body.String()
		if rec.Code != http.StatusOK || !strings.Contains(body, `id="transcript-raw"`) {
			t.Fatalf("%s: the raw view answered %d", user, rec.Code)
		}
		// The first stored line, as stored (HTML-escaped by the one text path).
		first := `{&#34;seq&#34;:1,&#34;src&#34;:&#34;0&#34;,&#34;rec&#34;:{&#34;type&#34;:&#34;user&#34;,&#34;message&#34;:{&#34;role&#34;:&#34;user&#34;,&#34;content&#34;:&#34;` + trSecretText + `&#34;}}}`
		if !strings.Contains(body, `<pre class="entry-raw" data-seq="1"><code>`+first+`</code></pre>`) {
			t.Errorf("%s: the raw view does not carry record 1 as stored", user)
		}
		if n := strings.Count(body, `class="entry-raw"`); n != 4 {
			t.Errorf("%s: the raw view carries %d records, want the stream's 4", user, n)
		}
	}
	owner := w.server(t, "owner", a)
	if body := getAs(t, owner, rawURL("s-full-01", QuerySeq, "2", QueryTo, "3")).Body.String(); strings.Count(body, `class="entry-raw"`) != 2 ||
		!strings.Contains(body, `data-seq="2"`) || !strings.Contains(body, `data-seq="3"`) || strings.Contains(body, trSecretText) {
		t.Error("?seq=2&to=3 does not carry exactly records 2 and 3")
	}

	misses := map[string]struct {
		user string
		a    *archive.Archive
		path string
	}{
		"a reader of one written scope":      {"one", a, rawURL("s-full-01")},
		"a stranger":                         {"none", a, rawURL("s-full-01")},
		"a session with no transcript":       {"owner", a, rawURL("s-bare-09")},
		"a never-written id":                 {"owner", a, rawURL("s-never-00")},
		"an id the grammar cannot produce":   {"owner", a, rawURL("<s>")},
		"a stream the session does not hold": {"owner", a, rawURL("s-full-01", QueryStream, "subagent:nope")},
		"a stream name that is no stream":    {"owner", a, rawURL("s-full-01", QueryStream, "../../meta.json")},
		"a record past the end":              {"owner", a, rawURL("s-full-01", QuerySeq, "99")},
		"a deployment with no archive":       {"owner", nil, rawURL("s-full-01")},
	}
	for name, m := range misses {
		rec := getAs(t, w.server(t, m.user, m.a), m.path)
		if rec.Code != http.StatusNotFound || rec.Body.String() != transcriptUnseenBody {
			t.Errorf("%s: answered %d %q, want 404 and the one miss body", name, rec.Code, rec.Body.String())
		}
	}
	// A request naming no session is the navigation page, not a refusal.
	if rec := getAs(t, owner, SessionTranscriptPath); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Pick a scope") {
		t.Errorf("a bare request answered %d, want the navigation page", rec.Code)
	}
}

// TestTheRawRecordViewIsBounded: a page stops at [rawPageBytes] and links the next record.
func TestTheRawRecordViewIsBounded(t *testing.T) {
	w := transcriptWorld(t)
	a := transcripttest.OpenAt(t, w.clock.Now)
	var lines []string
	for i := 0; i < 10; i++ {
		lines = append(lines, trUser(strings.Repeat("x", 100_000)))
	}
	transcripttest.Append(t, a, w.session("s-full-01"), "main", lines)
	srv := w.server(t, "owner", a)
	body := getAs(t, srv, rawURL("s-full-01")).Body.String()
	if n := strings.Count(body, `class="entry-raw"`); n != 5 || !strings.Contains(body, `seq=6&amp;session=s-full-01&amp;stream=main&amp;to=10`) {
		t.Fatalf("the first raw page carries %d records, want 5 (five 100 KB records fit %d bytes) and a link to record 6", n, rawPageBytes)
	}
	next := getAs(t, srv, rawURL("s-full-01", QueryStream, "main", QuerySeq, "6", QueryTo, "10")).Body.String()
	if n := strings.Count(next, `class="entry-raw"`); n != 5 || strings.Contains(next, "next records") {
		t.Errorf("the second raw page carries %d records and a next link: %v", n, strings.Contains(next, "next records"))
	}
}

// TestSplitAssistantTextCutsAtTheFirstLimit pins the display rule at its boundaries, and that
// nothing is lost: head + rest is the text.
func TestSplitAssistantTextCutsAtTheFirstLimit(t *testing.T) {
	line := strings.Repeat("line\n", 40)
	for _, tc := range []struct {
		name, text string
		head, rest int // in characters
	}{
		{"empty", "", 0, 0},
		{"exactly the character limit", strings.Repeat("é", 1200), 1200, 0},
		{"one character over", strings.Repeat("é", 1201), 1200, 1},
		{"fifteen lines", strings.TrimSuffix(line[:15*5], "\n"), 74, 0},
		{"sixteen lines", strings.TrimSuffix(line[:16*5], "\n"), 74, 5},
		{"many lines", line, 74, 126},
		{"long lines: the character limit ends first", strings.Repeat(strings.Repeat("w", 500)+"\n", 5), 1200, 1305},
	} {
		head, rest := splitAssistantText(tc.text)
		if head+rest != tc.text {
			t.Errorf("%s: head + rest is not the text", tc.name)
		}
		if h, r := len([]rune(head)), len([]rune(rest)); h != tc.head || r != tc.rest {
			t.Errorf("%s: head %d and rest %d characters, want %d and %d", tc.name, h, r, tc.head, tc.rest)
		}
	}
}

// TestAPromptTypedMidTurnRendersAsAUserMessageWhole — a human message typed while a turn ran is stored
// by the runtime as a `queued_command` attachment (`commandMode: "prompt"`), not as a `user` record.
// It renders as a user message, WHOLE — the one arm with no head and no reveal (O9) — and opens a
// turn; a queued TASK NOTIFICATION beside it stays counted bookkeeping. The records are the
// generator's `cc-queued-prompt` session (`tests/transcripts/gen.py`), stored under this world's
// session; its prompt is 21 lines, past the assistant-text head, so "whole" is measurable.
func TestAPromptTypedMidTurnRendersAsAUserMessageWhole(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "transcript", "testdata", "synthetic_world.json"))
	if err != nil {
		t.Fatalf("the synthetic world is missing (%v) — written by tests/transcripts/gen.py, named in flake.nix's onlyGo filter", err)
	}
	var world struct {
		Sessions map[string]struct{ ID string } `json:"sessions"`
		Claude   struct {
			Files map[string]map[string]string `json:"files"`
		} `json:"claude"`
	}
	if err := json.Unmarshal(raw, &world); err != nil {
		t.Fatal(err)
	}
	file := world.Claude.Files["-work-alpha/"+world.Sessions["cc-queued-prompt"].ID+".jsonl"]["utf8"]
	lines := strings.Split(strings.TrimRight(file, "\n"), "\n")
	var queued struct{ Attachment struct{ Prompt, CommandMode string } }
	if len(lines) != 6 || json.Unmarshal([]byte(lines[3]), &queued) != nil || queued.Attachment.CommandMode != "prompt" {
		t.Fatalf("cc-queued-prompt is not the 6-record layout with the queued prompt at record 4; regenerate the world")
	}
	prompt := queued.Attachment.Prompt
	if strings.Count(prompt, "\n") < assistantHeadLines {
		t.Fatalf("the fixture prompt has %d lines, not past the %d-line head, so it cannot show whole-vs-clipped", strings.Count(prompt, "\n"), assistantHeadLines)
	}
	w := transcriptWorld(t)
	a := transcripttest.OpenAt(t, w.clock.Now)
	transcripttest.Append(t, a, w.session("s-full-01"), "main", lines)
	rec := getAs(t, w.server(t, "owner", a), sessionURL("s-full-01"))
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("GET the session page: %d", rec.Code)
	}
	whole := `<div class="t-user" data-class="human-text"><p class="t-label">user</p><div class="t-text">` + prompt + `</div></div>`
	if !strings.Contains(body, whole) {
		t.Errorf("the prompt typed mid-turn is not rendered as a user message, whole (%d bytes)", len(prompt))
	}
	if n := strings.Count(body, `<div class="t-user" data-class="human-text">`); n != 2 {
		t.Errorf("the page carries %d user messages, want 2 (the first prompt and the queued one)", n)
	}
}
