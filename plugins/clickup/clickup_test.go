package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/plugins/internal/hoststate"
	"github.com/ZacxDev/cairn/plugins/internal/pluginapi"
)

// TestABranchTokenMatchesOnlyAFetchedId — the plan's S10 branch case: `feature/clk0000a1-x` is an edge
// when `clk0000a1` is a FETCHED ticket id, and no edge otherwise (a token that merely looks like an id
// is never matched). ⚠ The plan's "OR appears as a ClickUp URL" is NOT built: since #274's audit a URL
// names a ticket only when the list holds its id too, so a URL can vouch for nothing the list does not.
func TestABranchTokenMatchesOnlyAFetchedId(t *testing.T) {
	const branch = "feature/clk0000a1-x"
	t.Run("a fetched id", func(t *testing.T) {
		w := newWorld(t, assistant("hello"), human("do lorem", branch), human("more", branch))
		if code, out, errs := w.run(t); code != 0 || !strings.Contains(out, "sessions=1 edges=1 suggestions=0 requests=0 failed=0") {
			t.Fatalf("exit %d\n%s%s", code, out, errs)
		}
		// The locator is the FIRST record that carried the branch.
		wantEdges(t, w.pod.edges(t), Edge{System: "clickup", TicketID: "clk0000a1", Signal: SigBranch, Locator: rec(2)})
	})
	t.Run("CONTROL: not fetched", func(t *testing.T) {
		w := newWorld(t, human("do lorem", branch))
		w.cu.tasks = tickets()[1:] // the list no longer holds clk0000a1
		if code, out, errs := w.run(t); code != 0 || !strings.Contains(out, "sessions=1 edges=0") {
			t.Fatalf("exit %d\n%s%s", code, out, errs)
		}
		wantEdges(t, w.pod.edges(t))
	})
	t.Run("CONTROL: not fetched, though the session names it by URL", func(t *testing.T) {
		w := newWorld(t, human("do lorem", branch), assistant("see "+cuURLPrefix+"clk0000a1 for the ask"))
		w.cu.tasks = tickets()[1:]
		if code, out, errs := w.run(t); code != 0 || !strings.Contains(out, "edges=0") {
			t.Fatalf("exit %d\n%s%s", code, out, errs)
		}
		wantEdges(t, w.pod.edges(t))
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

	// Another workspace's URL is not this workspace's ticket, even though the list holds the custom id
	// — in the transcript, in a commit message, and in a branch alike: the bare-token matchers must
	// not re-read the inside of a URL the URL matcher refused.
	repo3, commit3 := gitRepo(t)
	commit3("lorem: see " + cuURLPrefix + "9000002/ALPHA-12\n\nRefs: " + cuURLPrefix + "9000002/ALPHA-12\nClaude-Session-Id: " + session)
	w3 := newWorld(t, human("see "+cuURLPrefix+"9000002/ALPHA-12", "x/"+cuURLPrefix+"9000002/ALPHA-12"))
	w3.args = []string{"-clickup-team", team, "-repo", repo3}
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
	// …and the pass is REDONE once its backoff passes, though the pod no longer lists the session: the
	// PR that could not be read then is readable now, and its edge is written.
	if code, out, _ := w3.run(t); code != 0 || !strings.Contains(out, "sessions=0") || !strings.Contains(out, "skipped=1") {
		t.Fatalf("inside the backoff: exit %d %s", code, out)
	}
	writePR(t, dir2, "example-org/alpha-notes", 8, "ipsum", "see "+cuURLPrefix+"clk0000b2", "main")
	w3.clock = w3.clock.Add(15 * time.Minute)
	if code, out, errs := w3.run(t); code != 0 || !strings.Contains(out, "sessions=1 edges=1") {
		t.Fatalf("the redo after the PR became readable: exit %d\n%s%s", code, out, errs)
	}
	wantEdges(t, w3.pod.edges(t), Edge{System: "clickup", TicketID: "clk0000b2", Signal: SigPRBody, Locator: Locator{PR: 8}})
	// A PR that NEVER becomes readable is given up after a bounded number of attempts.
	w4 := newWorld(t, human("open the PR", ""), prLink(9, "example-org/alpha-notes"))
	w4.args = []string{"-gh-bin", gh}
	attempts := 0
	for range 12 {
		_, out, _ := w4.run(t)
		if strings.Contains(out, "sessions=1") {
			attempts++
		}
		w4.clock = w4.clock.Add(48 * time.Hour)
	}
	if attempts != maxRedoFailures {
		t.Errorf("a PR that is never readable was attempted %d times, want exactly %d and then given up", attempts, maxRedoFailures)
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
	write("alpha-notes/dolor.md", entry("refs: [clickup:clk0000b2]", otherSess))
	write("beta-notes/sit.md", "---\nslug: sit\nrefs: [clickup:clk0000b2]\n---\n\n- the trailer `[cairn: host-a/"+session+"]` is quoted here, mid-line\n")

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
	if s != (Suggestion{TicketID: "clk0000a1", Confidence: 1.0, Rationale: rationaleFromSummary}) || sg[0].Model != "fake-model-1" {
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
	w.realClock = true
	code, out, errs := w.run(t)
	if code != 0 || !strings.Contains(out, "edges=1") {
		t.Fatalf("exit %d\n%s%s", code, out, errs)
	}
	if len(w.cu.times) != 2 || len(w.cu.resets) != 1 {
		t.Fatalf("ClickUp saw %d requests and answered %d 429s, want 2 and 1", len(w.cu.times), len(w.cu.resets))
	}
	if w.cu.times[1].Before(w.cu.resets[0]) {
		t.Errorf("the retry arrived at %s, BEFORE the reset %s the 429 named", w.cu.times[1].Format(time.RFC3339Nano), w.cu.resets[0].Format(time.RFC3339Nano))
	}
	if !strings.Contains(errs, "rate limited (429)") {
		t.Errorf("the wait was not logged:\n%s", errs)
	}

	// A window that reopens later than the run will wait fails the run, and NOTHING is written.
	w2 := newWorld(t, human("see "+cuURLPrefix+"clk0000a1", ""))
	w2.cu.limited, w2.cu.resetIn, w2.realClock = 1, time.Hour, true
	if code, out, errs := w2.run(t); code != 1 || !strings.Contains(errs, "past the 2m0s this run will wait") {
		t.Fatalf("an hour-long window: exit %d\n%s%s", code, out, errs)
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
// writes left an EDGE as the session's last output: the watermark moved, the pod stopped listing the
// session, and no scan says the pass finished. The next run redoes it from record 1 and writes only
// what is missing.
func TestAnInterruptedPassIsRedoneFromTheFirstRecordWithoutDuplicates(t *testing.T) {
	w := newWorld(t, human("see "+cuURLPrefix+"clk0000a1", ""), assistant("and "+cuURLPrefix+"clk0000b2"))
	w.pod.failWrite = 2 // the second edge's write fails: one edge stored, no scan
	code, out, errs := w.run(t)
	if code != 1 || !strings.Contains(out, "failed=1") || len(w.pod.outputs) != 1 {
		t.Fatalf("the interrupted run: exit %d, %d outputs\n%s%s", code, len(w.pod.outputs), out, errs)
	}
	// 🔴 THE SESSION DOES NOT GROW. The one edge written carries the pass's watermark, so the pod no
	// longer lists the session — and a session that has ended never grows. What brings the pass
	// back is the redo marker on this host. (RED before the marker: this run found nothing pending,
	// exited 0, and the second edge and the scan were never written.)
	// A 500 is transient — it says nothing about this session — so the redo is the very next run.
	w.pod.mu.Lock()
	w.pod.reads = nil
	w.pod.mu.Unlock()
	if code, out, errs := w.run(t); code != 0 || !strings.Contains(out, "sessions=1 edges=1") || !strings.Contains(errs, "full=true") {
		t.Fatalf("the redo: exit %d\n%s%s", code, out, errs)
	}
	if scans := w.pod.ofType(TypeScan); len(scans) != 1 {
		t.Fatalf("the redo left %d scans, want the pass finished (1)", len(scans))
	}
	if raw, _ := os.ReadFile(filepath.Join(w.dir, "sessions.json")); strings.Contains(string(raw), "redo") {
		t.Errorf("a finished pass left its redo marker:\n%s", raw)
	}
	if code, out, _ := w.run(t); code != 0 || !strings.Contains(out, "sessions=0") {
		t.Fatalf("a run after the pass finished worked on it again: exit %d %s", code, out)
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
		cuURLPrefix + "clk0000a1":            {{ID: "clk0000a1"}},
		"(" + cuURLPrefix + "clk0000a1).":    {{ID: "clk0000a1"}},
		"_" + cuURLPrefix + "clk0000a1_":     {{ID: "clk0000a1"}},
		"http://app.clickup.com/t/clk0000a1": {{ID: "clk0000a1"}},
		// 🔴 A URL NEEDS THE LIST TOO. The five shapes #274's audit measured writing an edge to a ticket
		// that does not exist — a well-formed URL whose id is not one the list holds:
		"http://app.clickup.com/t/zzz999":              nil, // an id of some other workspace, or a typo
		cuURLPrefix + "clk00":                          nil, // cut short with nothing after it
		cuURLPrefix + "clk00 and more":                 nil, // cut short, then a space
		cuURLPrefix + "clk0000a":                       nil, // one character short
		cuURLPrefix + "clk0000a12":                     nil, // one character long
		cuURLPrefix + team:                             nil, // the workspace's own id is no ticket…
		cuURLPrefix + "clk0000a1x/" + team:             nil,
		cuURLPrefix + "clk0000a1-draft":                nil,
		cuURLPrefix + "clk0000a1_draft":                nil,
		cuURLPrefix + "clk0000a1%2Ddraft":              nil,
		cuURLPrefix + "clk00...":                       nil, // an id a tool cut short
		cuURLPrefix + "clk00…":                         nil,
		cuURLPrefix + "clk0000a1. Next sentence":       {{ID: "clk0000a1"}},
		cuURLPrefix + "clk0000a1?comment=1":            {{ID: "clk0000a1"}},
		cuURLPrefix + team + "/ALPHA-12":               {{ID: "clk0000c3", Custom: "ALPHA-12"}},
		cuURLPrefix + team + "/ALPHA-12_ and more":     {{ID: "clk0000c3", Custom: "ALPHA-12"}},
		cuURLPrefix + team + "/ALPHA-13":               nil, // not in the list — and the workspace id is no ticket
		cuURLPrefix + team + "/":                       nil, // the workspace form with no custom id: never ticket 9000001
		cuURLPrefix + team + "/{custom_id}":            nil,
		cuURLPrefix + "9000002/":                       nil,
		cuURLPrefix + team + "/ALPHA-12...":            nil, // a custom id cut short is not the id it now spells
		cuURLPrefix + team + "/ALPHA-12…":              nil,
		cuURLPrefix + "clk0000a1/":                     {{ID: "clk0000a1"}}, // a trailing slash on a canonical URL
		cuURLPrefix + "9000002/ALPHA-12":               nil,                 // another workspace
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
		"ALPHA-12":             {"clk0000c3"},
		"feature/ALPHA-12-fix": {"clk0000c3"},
		"ALPHA-123":            nil,
		// A neighbouring id is another id: joined by `-`, `_` or `.` to a digit on the right, or to
		// a word on the left.
		"PRE-ALPHA-12":   nil,
		"ALPHA-12-13":    nil,
		"ALPHA-12_3":     nil,
		"ALPHA-12.5":     nil,
		"ALPHA-12. Next": {"clk0000c3"},
		"(ALPHA-12)":     {"clk0000c3"},
		// …and the boundary is not ASCII's: a letter or digit of any script is part of the word.
		"éALPHA-12":             nil,
		"ALPHA-12é":             nil,
		"ALPHA-12\u0663":        nil, // an Arabic-Indic digit
		"«ALPHA-12»":            {"clk0000c3"},
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
	// A URL is judged ONCE, as a URL — to its END as written, not just the part the pattern read: the
	// rest of another workspace's URL, or of a URL the matcher refused, is not a bare token.
	for _, text := range []string{cuURLPrefix + "9000002/x/ALPHA-12", cuURLPrefix + "abc-ALPHA-12", "(" + cuURLPrefix + "9000002/ALPHA-12)"} {
		if got := (&Matcher{Index: ix, Team: team}).Field(text); len(got) != 0 {
			t.Errorf("Field(%q) = %+v, want nothing", text, got)
		}
	}
	if got := m.Prose("see " + cuURLPrefix + "clk0000a1 (ALPHA-12)"); len(got) != 2 {
		t.Errorf("a custom id BESIDE a URL was blanked with it: %+v", got)
	}
	// CONTROL: an empty list matches nothing.
	if got := (&Matcher{Index: NewIndex(nil)}).Field("feature/ALPHA-12-clk0000a1 " + cuURLPrefix + "clk0000a1"); len(got) != 0 {
		t.Errorf("an empty list matched %+v", got)
	}
}

func TestATrailerBlockIsTheLastParagraphOfKeyValueLines(t *testing.T) {
	body, tr := splitMessage("subject\n\nbody text\n\nTicket: clk0000a1\nCo-Authored-By: Synthetic Author\n  <author@example.invalid>\n")
	if body != "subject\n\nbody text" || len(tr) != 2 || tr[0] != (Trailer{"Ticket", "clk0000a1"}) ||
		tr[1].Value != "Synthetic Author <author@example.invalid>" {
		t.Errorf("split into %q and %+v", body, tr)
	}
	// What git itself adds to a last paragraph, and what a conflicted merge appends, hide no trailer;
	// a bare URL line beside the trailers stays PROSE (it is no `https:` trailer).
	for name, msg := range map[string]string{
		"a cherry-pick -x note":   "subject\n\nTicket: clk0000a1\n(cherry picked from commit 0123456789abcdef0123456789abcdef01234567)",
		"a conflicts paragraph":   "subject\n\nTicket: clk0000a1\n\n# Conflicts:\n#\tsome/file.go",
		"a URL line in the block": "subject\n\n" + cuURLPrefix + "zzz999\nTicket: clk0000a1",
	} {
		body, tr := splitMessage(msg)
		if len(tr) != 1 || tr[0] != (Trailer{"Ticket", "clk0000a1"}) {
			t.Errorf("%s: trailers %+v, want the one Ticket trailer", name, tr)
		}
		if name == "a URL line in the block" && !strings.Contains(body, cuURLPrefix+"zzz999") {
			t.Errorf("%s: the URL line left the message: %q", name, body)
		}
	}
	// A whitespace-only line under the trailers is no continuation of the session id; an uncommented
	// `Conflicts:` list hides nothing; and a paragraph looked PAST stays in the message.
	if _, tr := splitMessage("subject\n\nClaude-Session-Id: s-1\n  "); len(tr) != 1 || tr[0].Value != "s-1" {
		t.Errorf("a whitespace-only last line changed the trailer: %+v", tr)
	}
	if _, tr := splitMessage("subject\n\nTicket: clk0000a1\n\nConflicts:\n\tsome/file.go"); len(tr) != 1 || tr[0].Key != "Ticket" {
		t.Errorf("an uncommented Conflicts list hid the trailer block: %+v", tr)
	}
	// …but a conflict list is PATHS git wrote, not something the author said: it is not message text.
	for _, list := range []string{"Conflicts:\n\tALPHA-12/file.go", "# Conflicts:\n#\tALPHA-12/file.go"} {
		if body, tr := splitMessage("subject\n\nTicket: clk0000a1\n\n" + list); len(tr) != 1 || strings.Contains(body, "ALPHA-12") {
			t.Errorf("a conflict list's path is in the message: %q %+v", body, tr)
		}
	}
	if body, tr := splitMessage("subject\n\nTicket: clk0000a1\n\n#42 is the PR for ALPHA-12"); len(tr) != 1 || !strings.Contains(body, "#42 is the PR for ALPHA-12") {
		t.Errorf("a paragraph after the trailers left the message: %q %+v", body, tr)
	}
	for _, msg := range []string{"subject only", "Ticket: clk0000a1", "subject\n\nNote: this paragraph\nis prose, not trailers"} {
		if _, tr := splitMessage(msg); tr != nil {
			t.Errorf("splitMessage(%q) found trailers %+v", msg, tr)
		}
	}
}

func TestTheRefsKeyInItsThreeShapesAndItsOlderSpellings(t *testing.T) {
	cases := map[string][]string{
		"---\nrefs: clickup:clk0000a1\n---\n":                                                          nil, // a scalar `refs:` — the store refuses the entry
		"---\ntask: clickup:clk0000a1\n---\n":                                                          {"clk0000a1"},
		"---\nrefs: clickup:clk0000b2\ntasks: [clickup:clk0000a1]\n---\n":                              nil, // `refs:` wins even unreadable
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

// TestARefusedSessionBacksOffAndDoesNotStarveTheRest — the pod lists pending sessions oldest first and
// knows nothing of one this plugin cannot finish. Twelve older sessions whose every write the pod
// refuses (507, a full quota) sit ahead of a healthy one with `-limit 10`: run 1 works on ten of them
// and each is paid for once; run 2 SKIPS those ten and reaches the other two and the healthy session,
// whose edge is written; no refused session is paid for twice inside its backoff.
func TestARefusedSessionBacksOffAndDoesNotStarveTheRest(t *testing.T) {
	w := newWorld(t, human("see "+cuURLPrefix+"clk0000a1", ""))
	w.pod.extra, w.pod.refuse = map[string][]string{}, map[string]bool{}
	for i := range 12 {
		id := "s-bad-" + string(rune('a'+i))
		w.pod.extra[id] = []string{human("please do lorem", "")}
		w.pod.refuse[id] = true
	}
	w.args = w.withModel()
	code, out, errs := w.run(t)
	if code != 1 || !strings.Contains(out, "sessions=10 edges=0 suggestions=0 requests=10 failed=10 capped=false skipped=0") {
		t.Fatalf("run 1: exit %d\n%s%s", code, out, errs)
	}
	if !slices.Equal(w.pod.limits, []int{100}) {
		t.Errorf("the pending query asked for %v, want the pod's maximum (100) — `-limit` is what a run WORKS ON", w.pod.limits)
	}
	if !strings.Contains(errs, "backing off 15m0s") {
		t.Errorf("run 1 did not say it backs off:\n%s", errs)
	}
	code, out, errs = w.run(t)
	if code != 1 || !strings.Contains(out, "sessions=3 edges=1 suggestions=0 requests=3 failed=2 capped=false skipped=10") {
		t.Fatalf("run 2: exit %d\n%s%s", code, out, errs)
	}
	wantEdges(t, w.pod.edges(t), Edge{System: "clickup", TicketID: "clk0000a1", Signal: SigTranscript, Locator: rec(1)})
	// Inside the backoff nothing is asked again; past it, the refused sessions are tried again.
	if code, out, _ := w.run(t); code != 0 || !strings.Contains(out, "sessions=0 edges=0 suggestions=0 requests=0 failed=0 capped=false skipped=12") {
		t.Fatalf("run 3, inside every backoff: exit %d %s", code, out)
	}
	if n := len(w.prov.received()); n != 13 {
		t.Fatalf("the provider received %d requests over three runs, want 13 (one per session)", n)
	}
	w.clock = w.clock.Add(15 * time.Minute)
	if code, out, _ := w.run(t); code != 1 || !strings.Contains(out, "sessions=10 ") || !strings.Contains(out, "skipped=0") {
		t.Fatalf("run 4, past the backoff: exit %d %s", code, out)
	}
	// A session that GROWS leaves its backoff: new records are new input.
	w.pod.mu.Lock()
	w.pod.extra["s-bad-a"] = append(w.pod.extra["s-bad-a"], assistant("more"))
	w.pod.refuse["s-bad-a"] = false
	w.pod.mu.Unlock()
	// (The two sessions run 4's limit did not reach are due as well; the nine others stay skipped.)
	if code, out, errs := w.run(t); !strings.Contains(out, "sessions=3 ") || !strings.Contains(out, "skipped=9") ||
		!strings.Contains(errs, "session=s-bad-a full=true") {
		t.Fatalf("run 5, one session grew: exit %d\n%s%s", code, out, errs)
	}
}

// TestNoCredentialFollowsARedirectOrCrossesTheNetworkInCleartext — the module's two rules for a client
// that carries a credential, applied to the ClickUp token: a 3xx is an error and the token is NOT
// re-sent to its target, and a plain-http URL off the host is refused before anything is sent.
func TestNoCredentialFollowsARedirectOrCrossesTheNetworkInCleartext(t *testing.T) {
	var got []string
	target := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Get("Authorization"))
		writeJSON(rw, 200, map[string]any{"tasks": []any{}, "last_page": true})
	}))
	defer target.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		http.Redirect(rw, r, target.URL+r.URL.Path, http.StatusFound)
	}))
	defer redirector.Close()
	w := newWorld(t, human("see "+cuURLPrefix+"clk0000a1", ""))
	w.cuURL = redirector.URL
	code, out, errs := w.run(t)
	if code != 1 || !strings.Contains(errs, "answered 302") || len(got) != 0 {
		t.Fatalf("a redirecting ClickUp: exit %d, the redirect target received %q\n%s%s", code, got, out, errs)
	}
	if len(w.pod.outputs) != 0 {
		t.Errorf("%d outputs were written without a ticket list", len(w.pod.outputs))
	}
	w.cuURL = "http://clickup.example.invalid"
	if code, _, errs := w.run(t); code != 2 || !strings.Contains(errs, "cleartext") {
		t.Fatalf("a plain-http ClickUp URL off the host: exit %d %s", code, errs)
	}
}

// TestTicketTitlesAreSentOnlyWhenAskedForAndStoredOnlyWhereTheInstanceAllows — Q8. By default no
// output carries a ticket's name (the read-only test above asserts that over a whole run). With
// `-store-ticket-titles` against an instance that stores titles, an edge and a suggestion carry the
// fetched name. Against
// an instance that stores ids only the titled write is REFUSED and nothing is stored.
func TestTicketTitlesAreSentOnlyWhenAskedForAndStoredOnlyWhereTheInstanceAllows(t *testing.T) {
	records := []string{human("see "+cuURLPrefix+"clk0000a1", "")}
	w := newWorld(t, records...)
	w.pod.titles = true
	w.prov.answer = `[{"ticket_id":"clk0000b2","confidence":0.9,"rationale":"the model's own words"}]`
	w.args = append(w.withModel(), "-store-ticket-titles")
	if code, out, errs := w.run(t); code != 0 || !strings.Contains(out, "edges=1 suggestions=1") {
		t.Fatalf("exit %d\n%s%s", code, out, errs)
	}
	wantEdges(t, w.pod.edges(t),
		Edge{System: "clickup", TicketID: "clk0000a1", Title: "Synthetic ticket one: lorem export", Signal: SigTranscript, Locator: rec(1)})
	var s Suggestion
	_ = json.Unmarshal(w.pod.ofType(TypeSuggestion)[0].Body, &s)
	// With titles ON the model's own rationale is stored too: both are the operator's one decision.
	if s.Title != "Synthetic ticket two: ipsum import" || s.Rationale != "the model's own words" {
		t.Errorf("the suggestion is %+v", s)
	}

	// A ticket NAME carrying a bidirectional control (ClickUp stores what its users type) is sent
	// without it: the pod refuses such a title, and one refused title would fail the whole pass.
	w3 := newWorld(t, records...)
	w3.pod.titles = true
	w3.cu.tasks = []Ticket{{ID: "clk0000a1", Name: "gadget \u202eexport\u2066 now"}}
	w3.args = []string{"-store-ticket-titles"}
	if code, out, errs := w3.run(t); code != 0 || !strings.Contains(out, "edges=1") {
		t.Fatalf("a ticket name with a bidirectional control: exit %d\n%s%s", code, out, errs)
	}
	wantEdges(t, w3.pod.edges(t),
		Edge{System: "clickup", TicketID: "clk0000a1", Title: "gadget export now", Signal: SigTranscript, Locator: rec(1)})

	w2 := newWorld(t, records...)
	w2.args = []string{"-store-ticket-titles"}
	code, out, errs := w2.run(t)
	if code != 1 || !strings.Contains(errs, "stores ticket ids only") || len(w2.pod.outputs) != 0 {
		t.Fatalf("titles sent to an instance that stores ids only: exit %d, %d outputs\n%s%s", code, len(w2.pod.outputs), out, errs)
	}
}

// TestAPassThatStopsAtTheEdgeCapIsContinuedByTheNextRun — a session naming more tickets than one pass
// writes is not cut off at the cap: the pass says there is more, keeps its marker, and the next run
// writes the rest, each exactly once.
func TestAPassThatStopsAtTheEdgeCapIsContinuedByTheNextRun(t *testing.T) {
	var text strings.Builder
	const n = maxEdgesPerPass + 30
	var listed []Ticket
	for i := range n {
		fmt.Fprintf(&text, "%sclkcap%04d ", cuURLPrefix, i)
		listed = append(listed, Ticket{ID: fmt.Sprintf("clkcap%04d", i), Name: "synthetic"})
	}
	w := newWorld(t, human(text.String(), ""))
	w.cu.tasks = listed
	w.cu.perPage = 100
	if code, out, errs := w.run(t); code != 0 || !strings.Contains(out, fmt.Sprintf("edges=%d ", maxEdgesPerPass)) || !strings.Contains(errs, "more=true") {
		t.Fatalf("run 1: exit %d\n%s", code, out)
	}
	if code, out, _ := w.run(t); code != 0 || !strings.Contains(out, "sessions=1 edges=30 ") {
		t.Fatalf("run 2: exit %d %s", code, out)
	}
	seen := map[string]int{}
	for _, e := range w.pod.edges(t) {
		seen[e.TicketID]++
	}
	if len(seen) != n {
		t.Fatalf("%d distinct tickets have an edge, want all %d", len(seen), n)
	}
	for id, c := range seen {
		if c != 1 {
			t.Fatalf("ticket %s has %d edges", id, c)
		}
	}
	if code, out, _ := w.run(t); code != 0 || !strings.Contains(out, "sessions=0") {
		t.Fatalf("run 3: exit %d %s", code, out)
	}
}

// TestOneRecordNamingATicketBothWaysAlwaysStoresItsCustomId — a record carrying the canonical URL and
// the custom-id URL of ONE ticket writes one edge, and it always carries the custom id (the two hits
// came out of a map walk in either order: 36 runs of 40 dropped it).
func TestOneRecordNamingATicketBothWaysAlwaysStoresItsCustomId(t *testing.T) {
	for range 25 {
		w := newWorld(t, `{"type":"user","a":"`+cuURLPrefix+`clk0000c3","b":"`+cuURLPrefix+team+`/ALPHA-12","c":"`+cuURLPrefix+`clk0000c3"}`)
		if code, out, _ := w.run(t); code != 0 || !strings.Contains(out, "edges=1 ") {
			t.Fatalf("exit %d %s", code, out)
		}
		wantEdges(t, w.pod.edges(t), Edge{System: "clickup", TicketID: "clk0000c3", CustomID: "ALPHA-12", Signal: SigTranscript, Locator: rec(1)})
	}
}

// TestAResetTheClientDoesNotBelieveIsNotAZeroWait — a 429 whose reset is absurdly far (or overflows a
// duration) must read as "past what this run will wait", never as "no wait at all".
func TestAResetTheClientDoesNotBelieveIsNotAZeroWait(t *testing.T) {
	now := time.Date(2000, 1, 2, 3, 4, 5, 0, time.UTC)
	c := &ClickUp{Now: func() time.Time { return now }}
	h := func(k, v string) http.Header { return http.Header{k: {v}} }
	for name, tc := range map[string]struct {
		h    http.Header
		want time.Duration
	}{
		"seconds, 30s ahead":      {h("X-Ratelimit-Reset", fmt.Sprint(now.Unix()+30)), 31 * time.Second},
		"milliseconds, 30s ahead": {h("X-Ratelimit-Reset", fmt.Sprint(now.UnixMilli()+30000)), 31 * time.Second},
		"already past":            {h("X-Ratelimit-Reset", fmt.Sprint(now.Unix()-5)), 0},
		"absurdly far":            {h("X-Ratelimit-Reset", "99999999999"), maxResetAhead},
		"overflowing":             {h("X-Ratelimit-Reset", "9223372036854775807"), maxResetAhead},
		"a huge Retry-After":      {h("Retry-After", "9300000000"), maxResetAhead},
		"Retry-After 7":           {h("Retry-After", "7"), 7 * time.Second},
		"no header":               {http.Header{}, fallbackWait},
	} {
		if got := c.resetWait(tc.h); got != tc.want {
			t.Errorf("%s: wait %s, want %s", name, got, tc.want)
		}
	}
}

// TestASymlinkedStoreRootIsRead — a cache root given as a symlink indexes what the directory holds
// (a walk does not follow a symlinked root: it found nothing, in silence).
func TestASymlinkedStoreRootIsRead(t *testing.T) {
	real := t.TempDir()
	if err := os.MkdirAll(filepath.Join(real, "alpha-notes"), 0o700); err != nil {
		t.Fatal(err)
	}
	entry := "---\nrefs: [clickup:clk0000a1]\n---\n\n- 2000-01-02: did it [cairn: host-a/" + session + "]\n"
	if err := os.WriteFile(filepath.Join(real, "alpha-notes", "lorem.md"), []byte(entry), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "cache")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	w := newWorld(t, human("do lorem", ""))
	w.args = []string{"-store-root", link}
	if code, out, errs := w.run(t); code != 0 || !strings.Contains(out, "edges=1 ") {
		t.Fatalf("exit %d\n%s%s", code, out, errs)
	}
}

// TestAnInterruptedPassOfASessionThatThenGrewIsStillReadFromTheFirstRecord — the other half of the
// interrupted pass: the session DID grow, so the pod lists it again, naming the lone edge as its
// previous output. That edge's watermark is not proof the records before it were examined: no scan
// follows it, so the pass reads from record 1 and the edge the interrupted pass never wrote is found.
// The redo marker is removed first, so this is the plugin's rule with no host state to lean on.
func TestAnInterruptedPassOfASessionThatThenGrewIsStillReadFromTheFirstRecord(t *testing.T) {
	w := newWorld(t, human("see "+cuURLPrefix+"clk0000a1", ""), assistant("and "+cuURLPrefix+"clk0000b2"))
	w.pod.failWrite = 2
	if code, _, _ := w.run(t); code != 1 || len(w.pod.outputs) != 1 {
		t.Fatalf("the interrupted run: exit %d, %d outputs", code, len(w.pod.outputs))
	}
	if err := os.Remove(filepath.Join(w.dir, "sessions.json")); err != nil {
		t.Fatal(err)
	}
	w.pod.grow("main", assistant("three"))
	if code, out, errs := w.run(t); code != 0 || !strings.Contains(errs, "full=true") {
		t.Fatalf("exit %d\n%s%s", code, out, errs)
	}
	wantEdges(t, w.pod.edges(t),
		Edge{System: "clickup", TicketID: "clk0000a1", Signal: SigTranscript, Locator: rec(1)},
		Edge{System: "clickup", TicketID: "clk0000b2", Signal: SigTranscript, Locator: rec(2)})
}

// TestABackoffSurvivesASessionThatIsMerelyNotListedAndEndsWhenThePodRefusesIt — the pod stops listing
// a refused session (it was resumed) and then lists it again: its backoff is still there, so it is
// SKIPPED rather than paid for again. A session the pod answers its one 403 for keeps nothing.
func TestABackoffSurvivesASessionThatIsMerelyNotListedAndEndsWhenThePodRefusesIt(t *testing.T) {
	w := newWorld(t)
	w.pod.extra = map[string][]string{"s-bad-a": {human("please do lorem", "")}}
	w.pod.refuse = map[string]bool{"s-bad-a": true}
	w.args = w.withModel()
	if code, out, _ := w.run(t); code != 1 || !strings.Contains(out, "sessions=1") || !strings.Contains(out, "requests=1") {
		t.Fatalf("the refused pass: exit %d %s", code, out)
	}
	state := filepath.Join(w.dir, "sessions.json")
	w.pod.mu.Lock()
	w.pod.hidden = map[string]bool{"s-bad-a": true}
	w.pod.mu.Unlock()
	// The marker makes the session due whether listed or not; inside its backoff it is skipped.
	if code, out, _ := w.run(t); code != 0 || !strings.Contains(out, "sessions=0") || !strings.Contains(out, "skipped=1") {
		t.Fatalf("unlisted, inside the backoff: exit %d %s", code, out)
	}
	if raw, _ := os.ReadFile(state); !strings.Contains(string(raw), "s-bad-a") {
		t.Fatalf("a session the pod merely did not list lost its backoff:\n%s", raw)
	}
	w.pod.mu.Lock()
	w.pod.hidden = nil
	w.pod.mu.Unlock()
	if code, out, _ := w.run(t); code != 0 || !strings.Contains(out, "skipped=1") || len(w.prov.received()) != 1 {
		t.Fatalf("listed again inside the backoff: exit %d %s (%d provider requests, want 1)", code, out, len(w.prov.received()))
	}
	// Deleted on the pod: the next attempt gets the one 403 and the entry is dropped.
	w.pod.mu.Lock()
	delete(w.pod.extra, "s-bad-a")
	w.pod.mu.Unlock()
	w.clock = w.clock.Add(15 * time.Minute)
	if _, _, errs := w.run(t); !strings.Contains(errs, "no longer serves this session") {
		t.Fatalf("the pass after the deletion did not drop the session: %s", errs)
	}
	if raw, _ := os.ReadFile(state); strings.Contains(string(raw), "s-bad-a") {
		t.Fatalf("a deleted session is still remembered:\n%s", raw)
	}
}

// TestAModelIsPaidOncePerPassHoweverOftenThePassIsRedone — delta audit 🔴. The model answers, and the
// pod then FAILS the scan's write with a 500 on every run (transient: no backoff for a listed
// session). Red before: each run asked the provider again — one paid request per run, forever. Now
// the answer is saved in the redo marker before any of it is written, and a redo asks nobody.
func TestAModelIsPaidOncePerPassHoweverOftenThePassIsRedone(t *testing.T) {
	t.Run("a session the pod still lists", func(t *testing.T) {
		w := newWorld(t, human("please do the lorem export", ""))
		w.args = w.withModel()
		w.prov.answer = `[{"ticket_id":"clk0000a1","confidence":0.9,"rationale":"synthetic"}]`
		w.pod.failTypes = map[string]bool{TypeSuggestion: true} // nothing is written, so it stays listed
		for range 5 {
			if code, _, errs := w.run(t); code != 1 || strings.Contains(errs, "backing off") {
				t.Fatalf("a run against a failing pod: exit %d %s", code, errs)
			}
		}
		if n := len(w.prov.received()); n != 1 {
			t.Fatalf("the provider received %d requests over 5 runs of one pass, want 1", n)
		}
		w.pod.failTypes = nil
		if code, out, errs := w.run(t); code != 0 || !strings.Contains(out, "suggestions=1 requests=0") {
			t.Fatalf("the redo once the pod recovered: exit %d\n%s%s", code, out, errs)
		}
		if n := len(w.prov.received()); n != 1 || len(w.pod.ofType(TypeSuggestion)) != 1 || len(w.pod.ofType(TypeScan)) != 1 {
			t.Fatalf("after recovery: %d provider requests, %d suggestions, %d scans — want 1, 1, 1", n,
				len(w.pod.ofType(TypeSuggestion)), len(w.pod.ofType(TypeScan)))
		}
		if raw, _ := os.ReadFile(filepath.Join(w.dir, "sessions.json")); strings.Contains(string(raw), "asked") {
			t.Errorf("a finished pass left the model's answer on the host:\n%s", raw)
		}
	})
	t.Run("a session only the marker brings back is bounded", func(t *testing.T) {
		// The suggestion LANDS (so the pod stops listing the session) and the scan's write fails, on
		// every run for 60 days. Red before: one request per run and a marker that never expired.
		w := newWorld(t, human("please do the lorem export", ""))
		w.args = w.withModel()
		w.prov.answer = `[{"ticket_id":"clk0000a1","confidence":0.9,"rationale":"synthetic"}]`
		w.pod.failTypes = map[string]bool{TypeScan: true}
		attempts := 0
		for range 30 {
			_, out, _ := w.run(t)
			if strings.Contains(out, "sessions=1") {
				attempts++
			}
			w.clock = w.clock.Add(48 * time.Hour)
		}
		if n := len(w.prov.received()); n != 1 {
			t.Fatalf("the provider received %d requests, want 1", n)
		}
		// The pass itself, then maxRedoFailures redos (each backed off, the session being unlisted).
		if attempts != 1+maxRedoFailures {
			t.Fatalf("the unfinished pass was attempted %d times, want exactly %d and then given up", attempts, 1+maxRedoFailures)
		}
		if raw, _ := os.ReadFile(filepath.Join(w.dir, "sessions.json")); strings.Contains(string(raw), session) {
			t.Errorf("the given-up pass is still on the host:\n%s", raw)
		}
		if n := len(w.pod.ofType(TypeSuggestion)); n != 1 {
			t.Errorf("%d suggestions stored, want the one — a redo must not write it again", n)
		}
	})
}

// TestASessionThePodStillListsIsNeverGivenUp — the give-up is for a session NOTHING else will bring
// back. One the pod still lists keeps its backoff, which grows to its ceiling: 400 is a refusal.
func TestASessionThePodStillListsIsNeverGivenUp(t *testing.T) {
	w := newWorld(t)
	w.pod.extra = map[string][]string{"s-bad-a": {human("see "+cuURLPrefix+"clk0000a1", "")}}
	w.pod.refuseStatus = 400
	w.pod.refuse = map[string]bool{"s-bad-a": true}
	for i := range maxRedoFailures + 3 {
		code, out, errs := w.run(t)
		if code != 1 || !strings.Contains(out, "sessions=1") || !strings.Contains(errs, "backing off") {
			t.Fatalf("attempt %d on a listed, refused session: exit %d\n%s%s", i+1, code, out, errs)
		}
		if strings.Contains(errs, "GIVEN UP") {
			t.Fatalf("a session the pod still lists was given up on attempt %d", i+1)
		}
		w.clock = w.clock.Add(48 * time.Hour)
	}
}

// TestAMarkerPastWhatThePodHoldsIsDroppedBeforeAnythingIsPaid — the session was deleted and uploaded
// again, shorter, under the same id: the marker names positions that no longer exist. The pass stops
// at its first transcript page — no model request, no refused write — and the marker is dropped.
func TestAMarkerPastWhatThePodHoldsIsDroppedBeforeAnythingIsPaid(t *testing.T) {
	w := newWorld(t, human("one", ""), human("two", ""), human("three", ""))
	w.args = w.withModel()
	w.pod.failTypes = map[string]bool{TypeScan: true}
	w.pod.failStatus = 400
	w.prov.answer = `[{"ticket_id":"clk0000a1","confidence":0.9,"rationale":"synthetic"}]`
	if code, _, _ := w.run(t); code != 1 || len(w.prov.received()) != 1 {
		t.Fatalf("the interrupted pass: exit %d, %d requests", code, len(w.prov.received()))
	}
	w.pod.mu.Lock()
	w.pod.records["main"] = w.pod.records["main"][:1]
	w.pod.hidden = map[string]bool{session: true}
	w.pod.mu.Unlock()
	w.clock = w.clock.Add(time.Hour)
	_, _, errs := w.run(t)
	if !strings.Contains(errs, "fewer records than the pending item names") {
		t.Fatalf("the redo over a shrunk session: %s", errs)
	}
	if raw, _ := os.ReadFile(filepath.Join(w.dir, "sessions.json")); strings.Contains(string(raw), session) {
		t.Errorf("the stale marker is still kept:\n%s", raw)
	}
	if n := len(w.prov.received()); n != 1 {
		t.Errorf("%d provider requests, want no new one", n)
	}
}

// TestAnEntryWithNoUnfinishedPassIsDroppedWhenThePodRefusesTheSession — a backoff entry carrying no
// marker (a state file written before the marker existed) for a session the pod does not list: kept
// while the pod still answers for it, dropped when the pod answers its one 403.
func TestAnEntryWithNoUnfinishedPassIsDroppedWhenThePodRefusesTheSession(t *testing.T) {
	w := newWorld(t)
	w.pod.extra = map[string][]string{"s-old-a": {human("x", "")}}
	w.pod.hidden = map[string]bool{"s-old-a": true}
	state := filepath.Join(w.dir, "sessions.json")
	write := func() {
		if err := os.WriteFile(state, []byte(`{"sessions":{"s-old-a":{"failures":2,"retry_at":"2000-01-03T00:00:00Z","records":1}}}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write()
	w.run(t)
	if raw, _ := os.ReadFile(state); !strings.Contains(string(raw), "s-old-a") || !strings.Contains(string(raw), `"seen"`) {
		t.Fatalf("an unlisted entry the pod still answers for was dropped, or not stamped:\n%s", raw)
	}
	w.pod.mu.Lock()
	delete(w.pod.extra, "s-old-a")
	w.pod.mu.Unlock()
	w.run(t)
	if raw, _ := os.ReadFile(state); strings.Contains(string(raw), "s-old-a") {
		t.Fatalf("an entry for a session the pod refuses is still kept:\n%s", raw)
	}
}

// TestUnfinishedPassesAreWorkedOnBeforeNewOnes — with `-limit 1`, the pass that was started is
// finished before a new session is begun.
func TestUnfinishedPassesAreWorkedOnBeforeNewOnes(t *testing.T) {
	w := newWorld(t, human("see "+cuURLPrefix+"clk0000a1", ""))
	w.pod.failTypes = map[string]bool{TypeScan: true}
	if code, _, _ := w.run(t); code != 1 {
		t.Fatalf("the interrupted pass: exit %d", code)
	}
	w.pod.mu.Lock()
	w.pod.failTypes = nil
	w.pod.extra = map[string][]string{"s-aaa-new": {human("see "+cuURLPrefix+"clk0000b2", "")}}
	w.pod.mu.Unlock()
	w.clock = w.clock.Add(time.Hour)
	if _, _, errs := w.run(t, "-limit", "1"); !strings.Contains(errs, "session="+session+" full=true") || strings.Contains(errs, "session=s-aaa-new") {
		t.Fatalf("with -limit 1 the run did not finish the unfinished pass first:\n%s", errs)
	}
}

// TestAListedSessionInBackoffIsStampedAndAnUnlistedRedoIsNot — the stamp the state TTL counts from.
// A session the pod LISTS is stamped on disk even when the run only skips it. A session only the redo
// marker brings back is NOT re-stamped by being retried — otherwise the TTL could never end a pass
// that keeps failing without a refusal (the run's own deadline, here a cancelled context).
func TestAListedSessionInBackoffIsStampedAndAnUnlistedRedoIsNot(t *testing.T) {
	w := newWorld(t, human("x", ""))
	state := filepath.Join(w.dir, "sessions.json")
	retry := w.clock.Add(time.Hour).UTC().Format(time.RFC3339)
	if err := os.WriteFile(state, []byte(`{"sessions":{"`+session+`":{"failures":1,"retry_at":"`+retry+`","records":1}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, out, _ := w.run(t); !strings.Contains(out, "skipped=1") {
		t.Fatalf("expected the session inside its backoff: %s", out)
	}
	if raw, _ := os.ReadFile(state); !strings.Contains(string(raw), `"seen": "`+w.clock.UTC().Format(time.RFC3339)+`"`) {
		t.Fatalf("a listed, skipped session was not stamped on disk:\n%s", raw)
	}

	const old = "2000-01-01T00:00:00Z"
	item := pluginapi.PendingItem{Session: session, Streams: map[string]pluginapi.StreamPos{"main": {SeqTo: 1}}}
	for _, unlisted := range []bool{true, false} {
		st := &stateFile{path: filepath.Join(t.TempDir(), "s.json"), Sessions: map[string]*sessionState{
			session: {Backoff: hoststate.Backoff{Seen: old}, Redo: &item}}}
		r := &Runner{API: &pluginapi.Client{Base: w.podURL, Token: fakeToken}, State: st, Now: func() time.Time { return w.clock },
			Matcher: &Matcher{Index: NewIndex(nil)}, Log: func(string) {}}
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // the run's deadline: every request fails, and it is no refusal
		var stats Stats
		if err := r.Run(ctx, []work{{item: item, redo: true, unlisted: unlisted}}, &stats); err != nil || stats.Failed != 1 {
			t.Fatalf("unlisted=%t: %v %+v", unlisted, err, stats)
		}
		ss := st.Sessions[session]
		if ss == nil || ss.Failures != 0 {
			t.Fatalf("unlisted=%t: a cancelled run dropped or backed off the pass: %+v", unlisted, ss)
		}
		if got := ss.Seen == old; got != unlisted {
			t.Errorf("unlisted=%t: the stamp is %s (was %s)", unlisted, ss.Seen, old)
		}
	}

	// …and that is what lets the TTL end it: an unfinished pass the pod has not listed for longer
	// than the TTL is dropped rather than retried; one inside the TTL is still due.
	for _, tc := range []struct {
		seen string
		due  int
	}{{w.clock.Add(-hoststate.StateTTL - time.Hour).UTC().Format(time.RFC3339), 0}, {w.clock.Add(-time.Hour).UTC().Format(time.RFC3339), 1}} {
		st := &stateFile{path: filepath.Join(t.TempDir(), "s.json"), Sessions: map[string]*sessionState{
			session: {Backoff: hoststate.Backoff{Seen: tc.seen}, Redo: &item}}}
		r := &Runner{State: st, Now: func() time.Time { return w.clock }, Limit: 10, Log: func(string) {}}
		var stats Stats
		todo, err := r.due(context.Background(), nil, &stats)
		if err != nil || len(todo) != tc.due || len(st.Sessions) != tc.due {
			t.Errorf("an unfinished pass last listed at %s: %d due, %d kept (err %v), want %d", tc.seen, len(todo), len(st.Sessions), err, tc.due)
		}
	}
}

// TestAQuestionIsPutToTheModelOnceWhateverItAnswered — round-3 audit 🔴. The model answers PROSE (a
// paid request with no usable answer) and the pod then fails the scan's write on every run. Red
// before: only a usable answer was remembered, so each redo bought the request again — five runs,
// five requests. The record of the question is now saved whether the answer was usable or not.
func TestAQuestionIsPutToTheModelOnceWhateverItAnswered(t *testing.T) {
	for name, arm := range map[string]func(w *world){
		"an unusable answer": func(w *world) { w.prov.answer = "this is not json" },
		"a provider 400":     func(w *world) { w.prov.status = 400 },
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, human("please do the lorem export", ""))
			w.args = w.withModel()
			arm(w)
			w.pod.failTypes = map[string]bool{TypeScan: true}
			w.pod.hidden = nil
			for range 5 {
				w.run(t)
				w.clock = w.clock.Add(48 * time.Hour)
			}
			if n := len(w.prov.received()); n != 1 {
				t.Fatalf("the provider received %d requests over 5 attempts at one pass, want 1", n)
			}
		})
	}
}

// TestTheSameQuestionIsNotBoughtAgainWhenTheSessionGrows — what identifies a question is the
// DESCRIPTION the model was sent, not the session's size. A suggestion whose write the pod refuses (a
// full quota) while the session keeps growing under the SAME summary is one request, not one per
// growth; a NEW summary is a new question. And a saved suggestion for a ticket the session has since
// been found to name is not written beside its edge.
func TestTheSameQuestionIsNotBoughtAgainWhenTheSessionGrows(t *testing.T) {
	w := newWorld(t, human("please do the lorem export", ""))
	w.pod.addSummary("The human asked for a lorem export. SUMMARY-ONE")
	w.args = w.withModel()
	w.prov.answer = `[{"ticket_id":"clk0000a1","confidence":0.9,"rationale":"synthetic"},{"ticket_id":"clk0000b2","confidence":0.8,"rationale":"synthetic"}]`
	w.pod.failTypes = map[string]bool{TypeSuggestion: true}
	w.pod.failStatus = http.StatusInsufficientStorage
	for i := range 4 {
		w.run(t)
		w.pod.grow("main", assistant(fmt.Sprintf("more %d", i)))
		w.clock = w.clock.Add(time.Minute)
	}
	if n := len(w.prov.received()); n != 1 {
		t.Fatalf("the provider received %d requests while the session grew under one summary, want 1", n)
	}
	// The session now NAMES one of the suggested tickets, and the quota is freed.
	w.pod.grow("main", assistant("see "+cuURLPrefix+"clk0000a1"))
	w.pod.mu.Lock()
	w.pod.failTypes = nil
	w.pod.mu.Unlock()
	w.clock = w.clock.Add(48 * time.Hour)
	if code, out, errs := w.run(t); code != 0 || !strings.Contains(out, "edges=1 suggestions=1 requests=0") {
		t.Fatalf("the redo: exit %d\n%s%s", code, out, errs)
	}
	var s Suggestion
	sg := w.pod.ofType(TypeSuggestion)
	_ = json.Unmarshal(sg[0].Body, &s)
	if len(sg) != 1 || s.TicketID != "clk0000b2" {
		t.Fatalf("stored suggestions: %d, the first for %s — want only clk0000b2 (clk0000a1 has an edge now)", len(sg), s.TicketID)
	}
	// A NEW summary is a new question.
	w.pod.addSummary("An updated summary. SUMMARY-TWO")
	w.pod.grow("main", assistant("and more"))
	if code, out, _ := w.run(t); code != 0 || !strings.Contains(out, "requests=1") {
		t.Fatalf("a new summary was not asked about: exit %d %s", code, out)
	}

	// …and it is a new question WHILE a pass is unfinished too: the saved answer was to another one.
	w2 := newWorld(t, human("please do the lorem export", ""))
	w2.pod.addSummary("The first summary. SUMMARY-ONE")
	w2.args = w2.withModel()
	w2.prov.answer = `[{"ticket_id":"clk0000a1","confidence":0.9,"rationale":"synthetic"}]`
	w2.pod.failTypes = map[string]bool{TypeSuggestion: true}
	w2.run(t)
	w2.run(t)
	if n := len(w2.prov.received()); n != 1 {
		t.Fatalf("the same question was asked %d times", n)
	}
	w2.pod.addSummary("A different summary. SUMMARY-TWO")
	w2.run(t)
	if got := w2.prov.received(); len(got) != 2 || !strings.Contains(got[1], "SUMMARY-TWO") {
		t.Fatalf("a new summary during an unfinished pass was not asked about (%d requests)", len(got))
	}
}

// TestASavedAnswerSurvivesARedoThatCannotReadTheSummary — an answer was paid for and its write failed;
// on the redo the pod fails the SUMMARY read. The pass must not finish (and drop the answer): it fails,
// is redone, and the saved suggestion is written with no second request. Red before: the scan was
// written `model_asked:false`, the marker deleted, and the paid-for suggestion never stored.
func TestASavedAnswerSurvivesARedoThatCannotReadTheSummary(t *testing.T) {
	w := newWorld(t, human("please do the lorem export", ""))
	w.pod.addSummary("The human asked for a lorem export.")
	w.args = w.withModel()
	w.prov.answer = `[{"ticket_id":"clk0000a1","confidence":0.9,"rationale":"synthetic"}]`
	w.pod.failTypes = map[string]bool{TypeSuggestion: true}
	w.run(t)
	w.pod.mu.Lock()
	w.pod.failTypes = nil
	w.pod.failSummaryReads = 1
	w.pod.mu.Unlock()
	if code, _, errs := w.run(t); code != 1 || !strings.Contains(errs, "reading what to ask the model about") {
		t.Fatalf("the redo whose summary read failed: exit %d %s", code, errs)
	}
	if n := len(w.pod.ofType(TypeScan)); n != 0 {
		t.Fatalf("a pass that could not read its summary finished (%d scans) and dropped the saved answer", n)
	}
	if code, out, errs := w.run(t); code != 0 || !strings.Contains(out, "suggestions=1 requests=0") {
		t.Fatalf("the next redo: exit %d\n%s%s", code, out, errs)
	}
	if n := len(w.prov.received()); n != 1 {
		t.Fatalf("%d provider requests, want 1", n)
	}
}

// TestATicketNameDoesNotReachAnIdsOnlyPodThroughTheRationale — #274's audit. The model is sent the
// candidates' NAMES and asked what points at its pick; here it quotes the name, as models do. On an
// instance that stores ticket ids only, NOTHING the pod stores may carry it: the rationale stored is
// this program's own fixed sentence. Red before: the model's sentence — name included — was stored as
// the rationale, past a title gate that only looks at `title`.
func TestATicketNameDoesNotReachAnIdsOnlyPodThroughTheRationale(t *testing.T) {
	const name = "Synthetic ticket one: lorem export"
	for desc, withSummary := range map[string]bool{"from a summary": true, "from the opening": false} {
		t.Run(desc, func(t *testing.T) {
			w := newWorld(t, human("please do the lorem export", ""))
			want := rationaleFromOpening
			if withSummary {
				w.pod.addSummary("The human asked for a lorem export.")
				want = rationaleFromSummary
			}
			w.args = w.withModel()
			w.prov.answer = `[{"ticket_id":"clk0000a1","confidence":0.9,"rationale":"this is clearly '` + name + `'"}]`
			if code, out, errs := w.run(t); code != 0 || !strings.Contains(out, "suggestions=1") {
				t.Fatalf("exit %d\n%s%s", code, out, errs)
			}
			if got := w.prov.received(); len(got) != 1 || !strings.Contains(got[0], name) {
				t.Fatal("POSITIVE CONTROL: the model was not sent the ticket's name, so this proves nothing")
			}
			for _, o := range w.pod.outputs {
				if strings.Contains(string(o.Body), "Synthetic ticket") || strings.Contains(string(o.Body), "clearly") {
					t.Fatalf("an output on an ids-only pod carries the ticket's name or the model's sentence: %s", o.Body)
				}
			}
			var s Suggestion
			_ = json.Unmarshal(w.pod.ofType(TypeSuggestion)[0].Body, &s)
			if s.Rationale != want {
				t.Errorf("the stored rationale is %q, want the fixed %q", s.Rationale, want)
			}
		})
	}
}

// TestAnEntryRefNeedsTheListToo — the attribution trailer that makes an entry "the session's" is text
// anyone who can write the cached scope can forge, so an `entry-ref` is held to the rule every other
// source is: the id must be one the fetched list holds. An explicit `clickup:<id>` naming anything
// else — which used to need no list — is no edge.
func TestAnEntryRefNeedsTheListToo(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "alpha-notes"), 0o700); err != nil {
		t.Fatal(err)
	}
	entry := "---\nrefs: [clickup:zzz999, clickup:clk0000b2, clickup:BETA-7]\n---\n\n- 2000-01-02: did it [cairn: host-a/" + session + "]\n"
	if err := os.WriteFile(filepath.Join(root, "alpha-notes", "lorem.md"), []byte(entry), 0o600); err != nil {
		t.Fatal(err)
	}
	w := newWorld(t, human("do lorem", ""))
	w.args = []string{"-store-root", root}
	if code, out, errs := w.run(t); code != 0 || !strings.Contains(out, "edges=1 ") {
		t.Fatalf("exit %d\n%s%s", code, out, errs)
	}
	wantEdges(t, w.pod.edges(t), Edge{System: "clickup", TicketID: "clk0000b2", Signal: SigEntryRef, Locator: Locator{Entry: "alpha-notes/lorem"}})
}

// fakeGHMode makes the re-executed fake GitHub CLI misbehave: "hang" never answers; "orphan" answers
// and exits 0 while a child it started keeps its output open.
const fakeGHMode = "CAIRN_CLICKUP_TEST_FAKE_GH_MODE"

// TestAHungHostProgramIsKilledAndTheSessionBacksOff — #274's audit. `gh` (or `git`) that never
// answers used to hold the whole run, past `-timeout`; and one that exits while a child it started
// holds its output kept `Wait` blocked just the same. Each is now bounded by its own deadline, its
// PROCESS GROUP is killed, and the session takes a backoff so the next run works on others first.
func TestAHungHostProgramIsKilledAndTheSessionBacksOff(t *testing.T) {
	oldTimeout, oldDelay := execTimeout, execWaitDelay
	execTimeout, execWaitDelay = 500*time.Millisecond, 300*time.Millisecond
	defer func() { execTimeout, execWaitDelay = oldTimeout, oldDelay }()
	for _, mode := range []string{"hang", "orphan"} {
		t.Run(mode, func(t *testing.T) {
			dir, gh := ghDir(t)
			t.Setenv(fakeGHMode, mode)
			writePR(t, dir, "example-org/alpha-notes", 7, "lorem", "see "+cuURLPrefix+"clk0000a1", "main")
			w := newWorld(t, human("open the PR", ""), prLink(7, "example-org/alpha-notes"))
			w.args = []string{"-gh-bin", gh}
			start := time.Now()
			code, out, errs := w.run(t)
			if took := time.Since(start); took > 10*time.Second {
				t.Fatalf("the run took %s: the hung program was not bounded", took)
			}
			if code != 1 || !strings.Contains(out, "sessions=1 edges=0") || !strings.Contains(errs, "could not be read") {
				t.Fatalf("exit %d\n%s%s", code, out, errs)
			}
			if mode == "hang" && !strings.Contains(errs, "did not finish in time and was killed") {
				t.Errorf("the deadline is not what failed it:\n%s", errs)
			}
			if !strings.Contains(errs, "backing off") {
				t.Errorf("a host program that failed did not back the session off:\n%s", errs)
			}
			// Nothing the program started is left running.
			raw, _ := os.ReadFile(filepath.Join(dir, "child.pid"))
			if pid, err := strconv.Atoi(strings.TrimSpace(string(raw))); mode == "orphan" && (err != nil || pid < 2) {
				t.Fatalf("POSITIVE CONTROL: the fake started no child (%q)", raw)
			} else if err == nil {
				deadline := time.Now().Add(3 * time.Second)
				for syscall.Kill(pid, 0) == nil && time.Now().Before(deadline) {
					time.Sleep(20 * time.Millisecond)
				}
				if alive := syscall.Kill(pid, 0) == nil && !zombie(pid); alive {
					_ = syscall.Kill(pid, syscall.SIGKILL)
					t.Errorf("the program's child (pid %d) outlived it", pid)
				}
			}
			// The next run does not work on the session again: it is inside its backoff.
			if code, out, _ := w.run(t); code != 0 || !strings.Contains(out, "sessions=0") || !strings.Contains(out, "skipped=1") {
				t.Fatalf("the next run: exit %d %s", code, out)
			}
		})
	}
}

// zombie reports whether a pid is a defunct process awaiting its parent's wait (it answers signal 0).
func zombie(pid int) bool {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return true // gone
	}
	i := strings.LastIndexByte(string(raw), ')')
	return i >= 0 && i+2 < len(raw) && raw[i+2] == 'Z'
}
