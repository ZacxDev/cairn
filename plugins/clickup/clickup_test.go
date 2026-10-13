package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestABranchTokenMatchesOnlyAFetchedIdOrOneNamedByURL — the plan's S10 branch case: `feature/clk0000a1-x`
// is an edge when `clk0000a1` is a FETCHED ticket id, and when the session names it by URL; with
// neither, the same branch is no edge (a token that merely looks like an id is never matched).
func TestABranchTokenMatchesOnlyAFetchedIdOrOneNamedByURL(t *testing.T) {
	const branch = "feature/clk0000a1-x"
	t.Run("a fetched id", func(t *testing.T) {
		w := newWorld(t, assistant("hello"), human("do lorem", branch), human("more", branch))
		if code, out, errs := w.run(t); code != 0 || !strings.Contains(out, "sessions=1 edges=1 suggestions=0 requests=0 failed=0") {
			t.Fatalf("exit %d\n%s%s", code, out, errs)
		}
		// The locator is the FIRST record that carried the branch.
		wantEdges(t, w.pod.edges(t), Edge{System: "clickup", TicketID: "clk0000a1", Signal: SigBranch, Locator: rec(2)})
	})
	t.Run("CONTROL: not fetched and not named by URL", func(t *testing.T) {
		w := newWorld(t, human("do lorem", branch))
		w.cu.tasks = tickets()[1:] // the list no longer holds clk0000a1
		if code, out, errs := w.run(t); code != 0 || !strings.Contains(out, "sessions=1 edges=0") {
			t.Fatalf("exit %d\n%s%s", code, out, errs)
		}
		wantEdges(t, w.pod.edges(t))
	})
	t.Run("not fetched, named by URL", func(t *testing.T) {
		w := newWorld(t, human("do lorem", branch), assistant("see "+cuURLPrefix+"clk0000a1 for the ask"))
		w.cu.tasks = tickets()[1:]
		if code, out, errs := w.run(t); code != 0 || !strings.Contains(out, "edges=2") {
			t.Fatalf("exit %d\n%s%s", code, out, errs)
		}
		wantEdges(t, w.pod.edges(t),
			Edge{System: "clickup", TicketID: "clk0000a1", Signal: SigTranscript, Locator: rec(2)},
			Edge{System: "clickup", TicketID: "clk0000a1", Signal: SigBranch, Locator: rec(1)})
	})
}

// TestCommitMessagesAndTrailersOfTheSessionsOwnCommits — real git, over a repository this test makes.
// A commit is the session's only when a TRAILER's value is the session id; its message's URL is a
// `commit-message` edge, another trailer's fetched id a `commit-trailer` edge, and both locators are
// SHAs the repository resolves.
func TestCommitMessagesAndTrailersOfTheSessionsOwnCommits(t *testing.T) {
	repo, commit := gitRepo(t)
	msgSHA := commit("lorem: export the thing\n\nSee " + cuURLPrefix + "clk0000a1 for the ask.\n\nClaude-Session-Id: " + session)
	trlSHA := commit("ipsum: import the other thing\n\nTicket: clk0000b2\nClaude-Session-Id: " + session)
	// CONTROLS, each naming a ticket that must NOT become an edge: another session's commit; a commit
	// that only QUOTES this session's id in prose; and a trailer value that merely looks like an id.
	commit("dolor: unrelated\n\n" + cuURLPrefix + "clk0000c3\n\nClaude-Session-Id: " + otherSess)
	commit("sit: mentions " + session + " in prose\n\n" + cuURLPrefix + "clk0000c3 was discussed.")
	commit("amet: a look-alike\n\nTicket: clk9999z9\nClaude-Session-Id: " + session)

	w := newWorld(t, human("do lorem", ""))
	w.args = []string{"-repo", repo}
	if code, out, errs := w.run(t); code != 0 || !strings.Contains(out, "edges=2 ") {
		t.Fatalf("exit %d\n%s%s", code, out, errs)
	}
	wantEdges(t, w.pod.edges(t),
		Edge{System: "clickup", TicketID: "clk0000a1", Signal: SigCommitMsg, Locator: Locator{Commit: msgSHA}},
		Edge{System: "clickup", TicketID: "clk0000b2", Signal: SigCommitTrlr, Locator: Locator{Commit: trlSHA}})
	for _, e := range w.pod.edges(t) {
		if out, err := exec.Command("git", "-C", repo, "cat-file", "-e", e.Locator.Commit+"^{commit}").CombinedOutput(); err != nil {
			t.Errorf("the locator %s does not resolve to a commit: %v %s", e.Locator.Commit, err, out)
		}
	}

	// CONTROL: with no `-repo` the same session has no commit signal at all.
	w2 := newWorld(t, human("do lorem", ""))
	if code, out, _ := w2.run(t); code != 0 || !strings.Contains(out, "edges=0") {
		t.Fatalf("with no -repo: exit %d %s", code, out)
	}
}

// TestACustomIdMatchesOnlyThroughTheFetchedListAndStoresTheCanonicalId — `ALPHA-12` is an edge only
// when the fetched list holds it, and the edge stores the CANONICAL id it resolves to; the
// `/t/<workspace>/ALPHA-12` URL resolves the same way. Control: the same strings with a list that does
// not hold the custom id → no edge.
func TestACustomIdMatchesOnlyThroughTheFetchedListAndStoresTheCanonicalId(t *testing.T) {
	repo, commit := gitRepo(t)
	sha := commit("lorem: closes ALPHA-12, not ALPHA-123 or XALPHA-12\n\nClaude-Session-Id: " + session)
	records := []string{
		human("please look at "+cuURLPrefix+team+"/ALPHA-12", "zach/ALPHA-12-fix"),
		assistant("ALPHA-12 in prose is not a transcript signal; " + cuURLPrefix + team + "/BETA-7 is not in the list"),
	}
	w := newWorld(t, records...)
	w.args = []string{"-repo", repo, "-clickup-team", team}
	if code, out, errs := w.run(t); code != 0 || !strings.Contains(out, "edges=3 ") {
		t.Fatalf("exit %d\n%s%s", code, out, errs)
	}
	wantEdges(t, w.pod.edges(t),
		Edge{System: "clickup", TicketID: "clk0000c3", CustomID: "ALPHA-12", Signal: SigTranscript, Locator: rec(1)},
		Edge{System: "clickup", TicketID: "clk0000c3", CustomID: "ALPHA-12", Signal: SigCommitMsg, Locator: Locator{Commit: sha}},
		Edge{System: "clickup", TicketID: "clk0000c3", CustomID: "ALPHA-12", Signal: SigBranch, Locator: rec(1)})

	// CONTROL: the list holds no custom id.
	w2 := newWorld(t, records...)
	w2.args = []string{"-repo", repo, "-clickup-team", team}
	w2.cu.tasks = tickets()[:2]
	if code, out, errs := w2.run(t); code != 0 || !strings.Contains(out, "edges=0") {
		t.Fatalf("with a list that does not hold the custom id: exit %d\n%s%s", code, out, errs)
	}
	wantEdges(t, w2.pod.edges(t))

	// Another workspace's URL is not this workspace's ticket, even though the list holds the custom id.
	w3 := newWorld(t, human("see "+cuURLPrefix+"9000002/ALPHA-12", ""))
	w3.args = []string{"-clickup-team", team}
	if code, out, _ := w3.run(t); code != 0 || !strings.Contains(out, "edges=0") {
		t.Fatalf("another workspace's URL: exit %d %s", code, out)
	}
}

// TestAPRLinkRecordPlusThePRsBody — a `pr-link` record names the PR; the host's GitHub CLI answers its
// body, and a ticket the BODY names is a `pr-body` edge located at the PR. The PR's title and head
// branch are the `pr-link` signal.
func TestAPRLinkRecordPlusThePRsBody(t *testing.T) {
	dir, gh := ghDir(t)
	writePR(t, dir, "example-org/alpha-notes", 7, "lorem export (ALPHA-12)",
		"Implements the export.\n\nTicket: "+cuURLPrefix+"clk0000a1\n", "zach/clk0000b2-export")
	records := []string{human("open the PR", ""), prLink(7, "example-org/alpha-notes"), prLink(7, "example-org/alpha-notes")}
	w := newWorld(t, records...)
	w.args = []string{"-gh-bin", gh}
	if code, out, errs := w.run(t); code != 0 || !strings.Contains(out, "edges=3 ") {
		t.Fatalf("exit %d\n%s%s", code, out, errs)
	}
	wantEdges(t, w.pod.edges(t),
		Edge{System: "clickup", TicketID: "clk0000a1", Signal: SigPRBody, Locator: Locator{PR: 7}},
		Edge{System: "clickup", TicketID: "clk0000c3", CustomID: "ALPHA-12", Signal: SigPRLink, Locator: Locator{PR: 7}},
		Edge{System: "clickup", TicketID: "clk0000b2", Signal: SigPRLink, Locator: Locator{PR: 7}})
	calls, _ := os.ReadFile(filepath.Join(dir, "calls.log"))
	if got := strings.TrimSpace(string(calls)); got != "pr view 7 --repo example-org/alpha-notes --json title,body,headRefName" {
		t.Errorf("the GitHub CLI was run as %q, want ONE read of PR 7", got)
	}

	// CONTROL: with no `-gh-bin` the CLI is never run and the PR is no signal.
	dir2, _ := ghDir(t)
	w2 := newWorld(t, records...)
	if code, out, _ := w2.run(t); code != 0 || !strings.Contains(out, "edges=0") {
		t.Fatalf("with no -gh-bin: exit %d %s", code, out)
	}
	if _, err := os.Stat(filepath.Join(dir2, "calls.log")); err == nil {
		t.Error("the GitHub CLI was run although no -gh-bin was given")
	}

	// A PR the CLI cannot read is a source error: the pass completes, and the run exits 1.
	w3 := newWorld(t, human("open the PR", ""), prLink(8, "example-org/alpha-notes"))
	w3.args = []string{"-gh-bin", gh}
	code, out, errs := w3.run(t)
	if code != 1 || !strings.Contains(out, "sessions=1 edges=0 suggestions=0 requests=0 failed=1") || !strings.Contains(errs, "could not be read") {
		t.Fatalf("an unreadable PR: exit %d\n%s%s", code, out, errs)
	}
	if n := len(w3.pod.ofType(TypeScan)); n != 1 {
		t.Errorf("an unreadable PR left %d scans, want the pass completed (1)", n)
	}
}

// TestAnEntryTheSessionWroteWithAClickupRef — an entry in the host's cairn cache whose bullet carries
// this session's attribution and whose front matter says `refs: [clickup:clk0000a1]` is an `entry-ref`
// edge located at the entry; an entry another session wrote is not.
func TestAnEntryTheSessionWroteWithAClickupRef(t *testing.T) {
	root := t.TempDir()
	write := func(rel, text string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	entry := func(refs, sess string) string {
		return "---\nslug: lorem\ntitle: Lorem\n" + refs + "\n---\n\n## Nuance / work-history\n\n" +
			"- 2000-01-02: did the export [cairn: host-a/" + sess + "] [cb:deadbeef]\n"
	}
	write("alpha-notes/lorem-export.md", entry("refs: [clickup:clk0000a1, github:example-org/alpha-notes#7]", session))
	write("alpha-notes/ipsum-import.md", entry("refs:\n  - clickup:ALPHA-12\n  - clickup:not/an-id", session))
	// CONTROLS: another session's entry; and an entry that only QUOTES the attribution mid-prose.
	write("alpha-notes/dolor.md", entry("refs: clickup:clk0000b2", otherSess))
	write("beta-notes/sit.md", "---\nslug: sit\nrefs: clickup:clk0000b2\n---\n\n- the trailer `[cairn: host-a/"+session+"]` is quoted here, mid-line\n")

	w := newWorld(t, human("do lorem", ""))
	w.args = []string{"-store-root", root}
	if code, out, errs := w.run(t); code != 0 || !strings.Contains(out, "edges=2 ") {
		t.Fatalf("exit %d\n%s%s", code, out, errs)
	}
	wantEdges(t, w.pod.edges(t),
		Edge{System: "clickup", TicketID: "clk0000a1", Signal: SigEntryRef, Locator: Locator{Entry: "alpha-notes/lorem-export"}},
		Edge{System: "clickup", TicketID: "clk0000c3", CustomID: "ALPHA-12", Signal: SigEntryRef, Locator: Locator{Entry: "alpha-notes/ipsum-import"}})
	for _, e := range w.pod.edges(t) {
		if _, err := os.Stat(filepath.Join(root, e.Locator.Entry+".md")); err != nil {
			t.Errorf("the locator %s does not resolve to an entry file: %v", e.Locator.Entry, err)
		}
	}
}

// TestATranscriptURLIsLocatedAtItsRecord — URLs anywhere in a record's values (a tool result included),
// in any stream; each edge's locator is the record that carried it, and the record does carry it.
func TestATranscriptURLIsLocatedAtItsRecord(t *testing.T) {
	w := newWorld(t,
		human("start", ""),
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"open `+cuURLPrefix+`clk0000b2 now"}]}}`,
		assistant("not a task URL: https://app.clickup.com/9000001/v/l/list-0001 and "+cuURLPrefix+"clk0000a1-draft"),
	)
	w.pod.records["subagent:a1"] = []string{assistant("the subagent read " + cuURLPrefix + "clk0000a1.")}
	if code, out, errs := w.run(t); code != 0 || !strings.Contains(out, "edges=2 ") {
		t.Fatalf("exit %d\n%s%s", code, out, errs)
	}
	wantEdges(t, w.pod.edges(t),
		Edge{System: "clickup", TicketID: "clk0000b2", Signal: SigTranscript, Locator: rec(2)},
		Edge{System: "clickup", TicketID: "clk0000a1", Signal: SigTranscript, Locator: Locator{Stream: "subagent:a1", Seq: 1}})
	for _, e := range w.pod.edges(t) {
		if got := w.pod.records[e.Locator.Stream][e.Locator.Seq-1]; !strings.Contains(got, cuURLPrefix+e.TicketID) {
			t.Errorf("the record %s/%d does not carry %s's URL: %s", e.Locator.Stream, e.Locator.Seq, e.TicketID, got)
		}
	}
	// Every output of the pass carries the pass's watermark: every stream at its end.
	for _, o := range w.pod.ofType(TypeEdge) {
		if o.InputWatermark["main"] != 3 || o.InputWatermark["subagent:a1"] != 1 || o.Model != noModel {
			t.Errorf("an edge was written with watermark %v model %q", o.InputWatermark, o.Model)
		}
	}
}

// TestSuggestionsAreNeverEdges — the plan's control: the fake provider answers a suggestion at
// confidence 1.0 and it is STILL written as a `ticket-suggestion`, never as an edge. It is made from
// plugin A's stored summary (the cross-plugin read), and a pick outside the candidate list is dropped.
func TestSuggestionsAreNeverEdges(t *testing.T) {
	w := newWorld(t, human("please export lorem", ""), assistant("done; see "+cuURLPrefix+"clk0000b2"))
	w.pod.addSummary("The human asked for a lorem export; the agent wrote it. SUMMARY-MARK")
	w.prov.answer = "```json\n" + `[{"ticket_id":"clk0000a1","confidence":1.0,"rationale":"the session is` + "\\n" + `the lorem export"},` +
		`{"ticket_id":"clk7777x7","confidence":0.99,"rationale":"not a candidate"},` +
		`{"ticket_id":"clk0000c3","confidence":0.2,"rationale":"below the floor"},` +
		`{"ticket_id":"clk0000b2","confidence":0.9,"rationale":"already an edge, so never a candidate"}]` + "\n```"
	w.args = w.withModel()
	code, out, errs := w.run(t)
	if code != 0 || !strings.Contains(out, "sessions=1 edges=1 suggestions=1 requests=1 failed=0 capped=false") {
		t.Fatalf("exit %d\n%s%s", code, out, errs)
	}
	// The ONLY edge is the deterministic one; the model's 1.0 is a suggestion.
	wantEdges(t, w.pod.edges(t), Edge{System: "clickup", TicketID: "clk0000b2", Signal: SigTranscript, Locator: rec(2)})
	sg := w.pod.ofType(TypeSuggestion)
	if len(sg) != 1 {
		t.Fatalf("%d suggestions stored, want 1", len(sg))
	}
	var s Suggestion
	_ = json.Unmarshal(sg[0].Body, &s)
	if s != (Suggestion{TicketID: "clk0000a1", Confidence: 1.0, Rationale: "the session is the lorem export"}) || sg[0].Model != "fake-model-1" {
		t.Errorf("the suggestion is %+v by model %q", s, sg[0].Model)
	}
	got := w.prov.received()
	if len(got) != 1 {
		t.Fatalf("the provider received %d requests, want 1", len(got))
	}
	// The cross-plugin read: the request carries plugin A's summary, names its author, and lists as
	// candidates exactly the tickets with no edge — by id AND name.
	for _, want := range []string{"SUMMARY-MARK", "SESSION (a summary written by the plugin summary)", "clk0000a1\tSynthetic ticket one: lorem export\n",
		"clk0000c3\tSynthetic ticket three: dolor sync\n"} {
		if !strings.Contains(got[0], want) {
			t.Errorf("the request does not carry %q:\n%s", want, got[0])
		}
	}
	if strings.Contains(got[0], "clk0000b2\t") || strings.Contains(got[0], "synthetic description") {
		t.Errorf("the request names a ticket the session already has an edge to, or a ticket's description:\n%s", got[0])
	}
	scans := w.pod.ofType(TypeScan)
	var sc Scan
	_ = json.Unmarshal(scans[len(scans)-1].Body, &sc)
	if len(scans) != 1 || sc != (Scan{System: "clickup", Edges: 1, Suggestions: 1, ModelAsked: true}) {
		t.Errorf("the scan is %+v (%d stored)", sc, len(scans))
	}
	// The scan is the LAST output of the pass.
	if all := w.pod.outputs; all[len(all)-1].Type != TypeScan {
		t.Errorf("the pass's last output is a %s, want the scan", all[len(all)-1].Type)
	}

	// The session grows, the summary has not changed: nothing new to describe, so no request.
	w.pod.grow("main", assistant("one more"))
	if code, out, errs := w.run(t); code != 0 || !strings.Contains(out, "sessions=1 edges=0 suggestions=0 requests=0") {
		t.Fatalf("a pass with the same summary: exit %d\n%s%s", code, out, errs)
	}
	// A NEW summary is asked about — and the ticket already suggested is no longer a candidate.
	w.pod.addSummary("An updated summary. SUMMARY-TWO")
	w.pod.grow("main", assistant("and another"))
	if code, out, errs := w.run(t); code != 0 || !strings.Contains(out, "requests=1") {
		t.Fatalf("a pass with a new summary: exit %d\n%s%s", code, out, errs)
	}
	if got := w.prov.received(); len(got) != 2 || !strings.Contains(got[1], "SUMMARY-TWO") || strings.Contains(got[1], "clk0000a1\t") {
		t.Errorf("the second request is not about the new summary, or re-offers a ticket already suggested:\n%v", got)
	}
}

// TestWithNoSummaryTheModelReadsTheSessionsOpening — and only on the session's first pass.
func TestWithNoSummaryTheModelReadsTheSessionsOpening(t *testing.T) {
	w := newWorld(t, human("please export lorem OPENING-MARK", ""), prLink(7, "example-org/alpha-notes"), assistant("done"))
	w.args = w.withModel()
	if code, out, errs := w.run(t); code != 0 || !strings.Contains(out, "requests=1") {
		t.Fatalf("exit %d\n%s%s", code, out, errs)
	}
	got := w.prov.received()
	if len(got) != 1 || !strings.Contains(got[0], "#1 [human] please export lorem OPENING-MARK") ||
		!strings.Contains(got[0], "its opening records 1–3, no summary being stored") || strings.Contains(got[0], "pr-link") {
		t.Fatalf("the request is not the labelled opening:\n%v", got)
	}
	w.pod.grow("main", assistant("more"))
	if code, out, _ := w.run(t); code != 0 || !strings.Contains(out, "sessions=1 edges=0 suggestions=0 requests=0") {
		t.Fatalf("a later pass with no summary asked again: exit %d %s", code, out)
	}
}

// TestASpendCapOfZeroSendsNothingAndAProviderFailureDoesNotHoldTheEdges.
func TestASpendCapOfZeroSendsNothingAndAProviderFailureDoesNotHoldTheEdges(t *testing.T) {
	w := newWorld(t, human("see "+cuURLPrefix+"clk0000a1", ""))
	w.args = append(w.withModel(), "-max-requests-per-day", "0")
	code, out, errs := w.run(t)
	if code != 0 || !strings.Contains(out, "sessions=1 edges=1 suggestions=0 requests=0 failed=0 capped=true") {
		t.Fatalf("cap 0: exit %d\n%s%s", code, out, errs)
	}
	if n := len(w.prov.received()); n != 0 {
		t.Fatalf("with a cap of 0 the provider received %d requests", n)
	}
	var sc Scan
	_ = json.Unmarshal(w.pod.ofType(TypeScan)[0].Body, &sc)
	if sc.ModelAsked {
		t.Error("the scan says a model was asked under a cap of 0")
	}
	// POSITIVE CONTROL for the count: the same world with the default cap reaches the provider.
	w1 := newWorld(t, human("see "+cuURLPrefix+"clk0000a1", ""))
	w1.args = w1.withModel()
	if w1.run(t); len(w1.prov.received()) != 1 {
		t.Fatalf("POSITIVE CONTROL: with the default cap the provider received %d requests, want 1", len(w1.prov.received()))
	}

	// A failing provider: the edge is written, the scan says no model answered, the run exits 1.
	w2 := newWorld(t, human("see "+cuURLPrefix+"clk0000a1", ""))
	w2.args = w2.withModel()
	w2.prov.status = 500
	code, out, errs = w2.run(t)
	if code != 1 || !strings.Contains(out, "edges=1 suggestions=0 requests=1 failed=1") || !strings.Contains(errs, "no suggestion was made") {
		t.Fatalf("a failing provider: exit %d\n%s%s", code, out, errs)
	}
	if len(w2.pod.edges(t)) != 1 || len(w2.pod.ofType(TypeScan)) != 1 {
		t.Error("a failing provider held the edge or the scan back")
	}
	// An answer that is no JSON array is a failure too, never a guess.
	w3 := newWorld(t, human("do lorem", ""))
	w3.args = w3.withModel()
	w3.prov.answer = "I think clk0000a1 fits."
	if code, _, errs := w3.run(t); code != 1 || !strings.Contains(errs, "holds no JSON array") || len(w3.pod.ofType(TypeSuggestion)) != 0 {
		t.Fatalf("a prose answer: exit %d %s", code, errs)
	}
}

// TestA429IsWaitedOutUntilTheResetTheHeaderNames — the plan's S10 case, asserted on the fake's own
// request timestamps: the request after a 429 arrives no earlier than the reset the 429 named.
func TestA429IsWaitedOutUntilTheResetTheHeaderNames(t *testing.T) {
	w := newWorld(t, human("see "+cuURLPrefix+"clk0000a1", ""))
	w.cu.limited, w.cu.resetIn = 1, 300*time.Millisecond
	// The plugin's clock is the real one here: the wait is measured against the header's real time.
	args := []string{"-worker-url", w.podURL, "-token-file", filepath.Join(w.dir, "token"),
		"-clickup-url", w.cuURL, "-clickup-token-file", filepath.Join(w.dir, "clickup-token"), "-clickup-list", listID}
	var out, errb strings.Builder
	if code := run(args, &out, &errb, time.Now); code != 0 || !strings.Contains(out.String(), "edges=1") {
		t.Fatalf("exit %d\n%s%s", code, out.String(), errb.String())
	}
	if len(w.cu.times) != 2 || len(w.cu.resets) != 1 {
		t.Fatalf("ClickUp saw %d requests and answered %d 429s, want 2 and 1", len(w.cu.times), len(w.cu.resets))
	}
	if w.cu.times[1].Before(w.cu.resets[0]) {
		t.Errorf("the retry arrived at %s, BEFORE the reset %s the 429 named", w.cu.times[1].Format(time.RFC3339Nano), w.cu.resets[0].Format(time.RFC3339Nano))
	}
	if !strings.Contains(errb.String(), "rate limited (429)") {
		t.Errorf("the wait was not logged:\n%s", errb.String())
	}

	// A window that reopens later than the run will wait fails the run, and NOTHING is written.
	w2 := newWorld(t, human("see "+cuURLPrefix+"clk0000a1", ""))
	w2.cu.limited, w2.cu.resetIn = 1, time.Hour
	args2 := []string{"-worker-url", w2.podURL, "-token-file", filepath.Join(w2.dir, "token"),
		"-clickup-url", w2.cuURL, "-clickup-token-file", filepath.Join(w2.dir, "clickup-token"), "-clickup-list", listID}
	out.Reset()
	errb.Reset()
	if code := run(args2, &out, &errb, time.Now); code != 1 || !strings.Contains(errb.String(), "past the 2m0s this run will wait") {
		t.Fatalf("an hour-long window: exit %d\n%s%s", code, out.String(), errb.String())
	}
	if len(w2.pod.outputs) != 0 {
		t.Errorf("a failed ticket fetch still wrote %d outputs", len(w2.pod.outputs))
	}
}

// TestClickUpIsOnlyEverReadAndItsTokenIsNeverPrinted — O6 (no write-back) asserted on what the fake
// saw: every request is a GET of the list route. The token is sent bare in `Authorization` and appears
// in neither stream, even when ClickUp's error body echoes it.
func TestClickUpIsOnlyEverReadAndItsTokenIsNeverPrinted(t *testing.T) {
	w := newWorld(t, human("see "+cuURLPrefix+"clk0000a1", "feature/clk0000b2"))
	w.cu.perPage = 2 // two pages
	w.args = w.withModel()
	code, out, errs := w.run(t)
	if code != 0 {
		t.Fatalf("exit %d\n%s%s", code, out, errs)
	}
	if len(w.cu.methods) != 2 {
		t.Fatalf("ClickUp saw %d requests, want the list's 2 pages", len(w.cu.methods))
	}
	for i, m := range w.cu.methods {
		if m != "GET" || !strings.HasPrefix(w.cu.paths[i], "/api/v2/list/"+listID+"/task?") || w.cu.auth[i] != fakeCUToken {
			t.Errorf("request %d was %s %s with Authorization %q", i, m, w.cu.paths[i], w.cu.auth[i])
		}
	}
	if !strings.Contains(w.cu.paths[1], "page=1") || !strings.Contains(w.cu.paths[0], "include_closed=true") {
		t.Errorf("the pages were asked as %v", w.cu.paths)
	}
	// Both pages' tickets are in the closed list: one from each matched.
	if len(w.pod.edges(t)) != 2 {
		t.Errorf("%d edges, want one per page's ticket", len(w.pod.edges(t)))
	}
	// Nothing ClickUp-side reached the pod but ids: no name, no description.
	for _, o := range w.pod.outputs {
		if s := string(o.Body); strings.Contains(s, "Synthetic ticket") || strings.Contains(s, "synthetic description") {
			t.Errorf("an output stores ClickUp text: %s", s)
		}
	}

	// A failing ClickUp whose body echoes the token: exit 1, nothing written, the token scrubbed.
	w2 := newWorld(t, human("see "+cuURLPrefix+"clk0000a1", ""))
	w2.cu.status = 500
	code, out, errs = w2.run(t)
	if code != 1 || !strings.Contains(errs, "answered 500") || !strings.Contains(errs, "[token]") {
		t.Fatalf("a failing ClickUp: exit %d\n%s%s", code, out, errs)
	}
	for _, secret := range []string{fakeCUToken, fakeToken, fakeKey} {
		if strings.Contains(out+errs, secret) {
			t.Errorf("a credential was printed:\n%s%s", out, errs)
		}
	}
	if len(w2.pod.outputs) != 0 {
		t.Errorf("a failed ticket fetch still wrote %d outputs", len(w2.pod.outputs))
	}

	// A run with nothing pending asks ClickUp nothing at all.
	w3 := newWorld(t)
	if code, out, _ := w3.run(t); code != 0 || !strings.Contains(out, "sessions=0") || len(w3.cu.methods) != 0 {
		t.Fatalf("nothing pending: exit %d %s, ClickUp saw %d requests", code, out, len(w3.cu.methods))
	}
}

// TestASessionThatNamesNoTicketStopsBeingPendingAndALaterPassReadsOnlyTheNewRecords — the scan is the
// watermark's carrier: a session with nothing to match is examined ONCE, and a pass after growth reads
// from the previous scan's watermark, not from record 1.
func TestASessionThatNamesNoTicketStopsBeingPendingAndALaterPassReadsOnlyTheNewRecords(t *testing.T) {
	w := newWorld(t, human("one", ""), assistant("two"), human("three", ""), assistant("four"))
	if code, out, errs := w.run(t); code != 0 || !strings.Contains(out, "sessions=1 edges=0 suggestions=0") {
		t.Fatalf("run 1: exit %d\n%s%s", code, out, errs)
	}
	scans := w.pod.ofType(TypeScan)
	if len(w.pod.outputs) != 1 || len(scans) != 1 || scans[0].InputWatermark["main"] != 4 {
		t.Fatalf("run 1 stored %d outputs (%d scans), want exactly one scan at main=4", len(w.pod.outputs), len(scans))
	}
	if code, out, _ := w.run(t); code != 0 || !strings.Contains(out, "sessions=0") {
		t.Fatalf("run 2 (nothing new): exit %d %s", code, out)
	}
	// Growth: only records 5… are read (the page size is 3, so a full read would start at 1).
	w.pod.mu.Lock()
	w.pod.reads = nil
	w.pod.mu.Unlock()
	w.pod.grow("main", human("five, see "+cuURLPrefix+"clk0000a1", ""))
	if code, out, errs := w.run(t); code != 0 || !strings.Contains(out, "edges=1") {
		t.Fatalf("run 3: exit %d\n%s%s", code, out, errs)
	}
	if !slices.Equal(w.pod.reads, []string{"main raw 5"}) {
		t.Errorf("run 3 read %v, want only the new record (main raw 5)", w.pod.reads)
	}
	wantEdges(t, w.pod.edges(t), Edge{System: "clickup", TicketID: "clk0000a1", Signal: SigTranscript, Locator: rec(5)})
}

// TestAnInterruptedPassIsRedoneFromTheFirstRecordWithoutDuplicates — a pass that died between two
// writes left an EDGE as the session's last output: the watermark moved, but no scan says the pass
// finished. The next pass (once the session grows) reads from record 1 and writes only what is missing.
func TestAnInterruptedPassIsRedoneFromTheFirstRecordWithoutDuplicates(t *testing.T) {
	w := newWorld(t, human("see "+cuURLPrefix+"clk0000a1", ""), assistant("and "+cuURLPrefix+"clk0000b2"))
	w.pod.failWrite = 2 // the second edge's write fails: one edge stored, no scan
	code, out, errs := w.run(t)
	if code != 1 || !strings.Contains(out, "failed=1") || len(w.pod.outputs) != 1 {
		t.Fatalf("the interrupted run: exit %d, %d outputs\n%s%s", code, len(w.pod.outputs), out, errs)
	}
	w.pod.grow("main", assistant("three"))
	w.pod.mu.Lock()
	w.pod.reads = nil
	w.pod.mu.Unlock()
	if code, out, errs := w.run(t); code != 0 || !strings.Contains(out, "edges=1") || !strings.Contains(errs, "full=true") {
		t.Fatalf("the redo: exit %d\n%s%s", code, out, errs)
	}
	if len(w.pod.reads) == 0 || w.pod.reads[0] != "main raw 1" {
		t.Errorf("the redo read %v, want it to start at record 1", w.pod.reads)
	}
	wantEdges(t, w.pod.edges(t),
		Edge{System: "clickup", TicketID: "clk0000a1", Signal: SigTranscript, Locator: rec(1)},
		Edge{System: "clickup", TicketID: "clk0000b2", Signal: SigTranscript, Locator: rec(2)})
}

// TestCredentialsComeFromFilesOnlyAndTheManifestDeclaresWhatTheCodeUses.
func TestCredentialsComeFromFilesOnlyAndTheManifestDeclaresWhatTheCodeUses(t *testing.T) {
	w := newWorld(t, human("x", ""))
	if err := os.Chmod(filepath.Join(w.dir, "clickup-token"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, errs := w.run(t); code != 2 || !strings.Contains(errs, "readable or writable by a group or other users") {
		t.Fatalf("a 0644 ClickUp token file: exit %d %s", code, errs)
	}
	if len(w.cu.methods) != 0 {
		t.Error("ClickUp was asked with a token from a loose file")
	}
	var out, errb strings.Builder
	if code := run([]string{"-worker-url", w.podURL, "-token-file", filepath.Join(w.dir, "token")}, &out, &errb, time.Now); code != 2 ||
		!strings.Contains(errb.String(), "-clickup-list is required") {
		t.Fatalf("no -clickup-list: exit %d %s", code, errb.String())
	}

	raw, err := os.ReadFile("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Name, Version string
		Capabilities  []string
		CallsModel    bool `json:"calls_model"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	want := []string{"pending:list", "transcript:read", "output:read:" + TypeSummary, "output:read:" + TypeEdge, "output:read:" + TypeScan,
		"output:read:" + TypeSuggestion, "output:write:" + TypeEdge, "output:write:" + TypeScan, "output:write:" + TypeSuggestion}
	got := append([]string(nil), m.Capabilities...)
	slices.Sort(got)
	slices.Sort(want)
	if m.Name != Name || m.Version != Version || !m.CallsModel || !slices.Equal(got, want) {
		t.Errorf("manifest.json is %+v, want name %s version %s, calls_model, and exactly the capabilities the code uses %v", m, Name, Version, want)
	}
}

// ---------------------------------------------------------------- the matchers and readers, directly

func TestTheURLMatcherTakesBothFormsAndNothingThatOnlyLooksLikeOne(t *testing.T) {
	m := &Matcher{Index: NewIndex(tickets()), Team: team}
	cases := map[string][]Hit{
		cuURLPrefix + "clk0000a1":                      {{ID: "clk0000a1", ByURL: true}},
		"(" + cuURLPrefix + "clk0000a1).":              {{ID: "clk0000a1", ByURL: true}},
		"_" + cuURLPrefix + "clk0000a1_":               {{ID: "clk0000a1", ByURL: true}},
		"http://app.clickup.com/t/zzz999":              {{ID: "zzz999", ByURL: true}}, // a URL needs no list
		cuURLPrefix + "clk0000a1-draft":                nil,
		cuURLPrefix + team + "/ALPHA-12":               {{ID: "clk0000c3", Custom: "ALPHA-12", ByURL: true}},
		cuURLPrefix + team + "/ALPHA-12_ and more":     {{ID: "clk0000c3", Custom: "ALPHA-12", ByURL: true}},
		cuURLPrefix + team + "/ALPHA-13":               nil, // not in the list — and the workspace id is no ticket
		cuURLPrefix + "9000002/ALPHA-12":               nil, // another workspace
		"https://app.clickup.com.example.invalid/t/x1": nil,
		"https://example.invalid/app.clickup.com/t/x1": nil,
		"clk0000a1 and ALPHA-12 bare":                  nil,
	}
	for text, want := range cases {
		if got := m.URLs(text); !slices.Equal(got, want) {
			t.Errorf("URLs(%q) = %+v, want %+v", text, got, want)
		}
	}
}

func TestACustomIdIsAWholeTokenOfTheClosedList(t *testing.T) {
	ts := append(tickets(), Ticket{ID: "clk0000d4", CustomID: "ALPHA-1"}, Ticket{ID: "clk0000e5", CustomID: "DUP-1"},
		Ticket{ID: "clk0000f6", CustomID: "DUP-1"}, Ticket{ID: "not/canonical", CustomID: "BAD-1"})
	ix := NewIndex(ts)
	if ix.Dropped != 1 || ix.Len() != 6 {
		t.Fatalf("the index holds %d tickets and dropped %d, want 6 and 1", ix.Len(), ix.Dropped)
	}
	m := &Matcher{Index: ix}
	cases := map[string][]string{
		"ALPHA-12":              {"clk0000c3"},
		"feature/ALPHA-12-fix":  {"clk0000c3"},
		"ALPHA-123":             nil,
		"XALPHA-12":             nil,
		"ALPHA-1 then ALPHA-12": {"clk0000d4", "clk0000c3"},
		"alpha-12":              nil, // exact case: the list's own spelling
		"DUP-1":                 nil, // two tasks claim it: it resolves to neither
		"BAD-1":                 nil, // its task's id is not canonical
	}
	for text, want := range cases {
		var got []string
		for _, h := range m.Customs(text) {
			got = append(got, h.ID)
		}
		if !slices.Equal(got, want) {
			t.Errorf("Customs(%q) = %v, want %v", text, got, want)
		}
	}
	// CONTROL: an empty list matches nothing.
	if got := (&Matcher{Index: NewIndex(nil)}).Field("feature/ALPHA-12-clk0000a1", nil); len(got) != 0 {
		t.Errorf("an empty list matched %+v", got)
	}
}

func TestATrailerBlockIsTheLastParagraphOfKeyValueLines(t *testing.T) {
	body, tr := splitMessage("subject\n\nbody text\n\nTicket: clk0000a1\nCo-Authored-By: Synthetic Author\n  <author@example.invalid>\n")
	if body != "subject\n\nbody text" || len(tr) != 2 || tr[0] != (Trailer{"Ticket", "clk0000a1"}) ||
		tr[1].Value != "Synthetic Author <author@example.invalid>" {
		t.Errorf("split into %q and %+v", body, tr)
	}
	for _, msg := range []string{"subject only", "Ticket: clk0000a1", "subject\n\nNote: this paragraph\nis prose, not trailers"} {
		if _, tr := splitMessage(msg); tr != nil {
			t.Errorf("splitMessage(%q) found trailers %+v", msg, tr)
		}
	}
}

func TestTheRefsKeyInItsThreeShapesAndItsOlderSpellings(t *testing.T) {
	cases := map[string][]string{
		"---\nrefs: clickup:clk0000a1\n---\n":                                                          {"clk0000a1"},
		"---\nrefs: [clickup:clk0000a1, \"clickup:ALPHA-12\", github:o/r#1]\n---\n":                    {"clk0000a1", "ALPHA-12"},
		"---\nrefs:\n  - clickup:clk0000a1\n\n  # a comment\n  - 'clickup:clk0000b2'\ntitle: x\n---\n": {"clk0000a1", "clk0000b2"},
		"---\ntasks: [clickup:clk0000a1]\n---\n":                                                       {"clk0000a1"},
		"---\nrefs: [github:o/r#1]\ntasks: [clickup:clk0000a1]\n---\n":                                 nil, // `refs:` wins; the older key is not read
		"refs: clickup:clk0000a1\n":                                                                    nil, // no front matter
		"---\ntitle: x\n---\n\nrefs: clickup:clk0000a1\n":                                              nil, // in the body
	}
	for text, want := range cases {
		if got := frontMatterRefs(text); !slices.Equal(got, want) {
			t.Errorf("frontMatterRefs(%q) = %v, want %v", text, got, want)
		}
	}
}

func TestTheModelsAnswerIsFilteredToWhatThePodStores(t *testing.T) {
	cands := map[string]bool{"clk0000a1": true, "clk0000b2": true, "clk0000c3": true}
	long := strings.Repeat("é", 200) // 400 bytes
	answer := `Here you go: [{"ticket_id":"clk0000a1","confidence":0.6,"rationale":"a` + "\\u0007" + `b"},` +
		`{"ticket_id":"clk0000a1","confidence":0.8,"rationale":"` + long + `"},` +
		`{"ticket_id":"clk0000b2","confidence":1.5,"rationale":"out of range"},` +
		`{"ticket_id":"clk0000b2","rationale":"no confidence"},` +
		`{"ticket_id":"clk0000c3","confidence":0.7,"rationale":"kept"}]`
	got, dropped, err := parseSuggestions(answer, cands, 0.5, 1)
	if err != nil || len(got) != 1 || dropped != 3 {
		t.Fatalf("got %+v dropped %d err %v, want the one best pick and 3 dropped", got, dropped, err)
	}
	if got[0].TicketID != "clk0000a1" || got[0].Confidence != 0.8 || len(got[0].Rationale) != 280 || !strings.HasPrefix(got[0].Rationale, "éé") {
		t.Errorf("the kept pick is %+v (rationale %d bytes), want clk0000a1 at 0.8 clipped to 280 bytes on a character", got[0], len(got[0].Rationale))
	}
	if err := podAccepts(TypeSuggestion, mustJSON(got[0])); err != nil {
		t.Errorf("the kept pick is not one the pod stores: %v", err)
	}
	if _, _, err := parseSuggestions(`{"ticket_id":"clk0000a1"}`, cands, 0.5, 3); err == nil {
		t.Error("an answer that is not an array was accepted")
	}
}

func mustJSON(v any) []byte { raw, _ := json.Marshal(v); return raw }
