package ui

import (
	"fmt"
	"html"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/client"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/report"
	"github.com/ZacxDev/cairn/internal/store"
)

// agentHost is the `host:` line both sides render — the renderer's one machine-dependent input, fixed.
const agentHost = "fixture-host"

// agentStore is alpha with enough entries that the recall text runs past the `head -60` mark, and
// beta with one entry whose service name appears nowhere else.
func agentStore(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	put := func(scope, service, nuance string) {
		dir := filepath.Join(root, scope)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := "---\nservice: " + service + "\nscope: " + scope + "\naliases:\n  - " + service + "-alias\n---\n\n" +
			"## What it is\n\nThe " + service + " notes, synthetic.\n\n## Pointers\n\n- `docs/" + service + ".md`\n\n" +
			store.NuanceHeading + "\n\n" + nuance
		if err := os.WriteFile(filepath.Join(dir, service+".md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 48; i++ {
		put("alpha-notes", fmt.Sprintf("lamp-%02d", i),
			fmt.Sprintf("- 2000-01-%02d: OPEN: trimmed wick %d [cairn: tallow-bot/s-lamp-%04d]\n- 2000-01-01: an older note\n", 1+i%28, i, i))
	}
	put("beta-notes", "chandlery-secret", "- 2000-01-04: poured the wax [cairn: wax-bot/s-wick-0002]\n")
	return root
}

func agentServer(t *testing.T, root string, id identity.Identity) *Server {
	t.Helper()
	return arcsServer(t, StoreSource{Root: root, Host: func() string { return agentHost }}, id)
}

// cliRecall is what `cairn recall --scope <scope>` renders below its banner: the CLI's OWN option
// builder (`client.RecallSelectionFor` with no `--list`, `--limit` or `--page`), the reader's own
// validation, `report.Recall` over the store as the CLI reads its cache (unrestricted — the cache is
// already what the pod let this token see), and `RenderText` with the CLI's nil extra header and
// single-instance label. Built HERE, from the client package, never from `agent.go`.
func cliRecall(t *testing.T, root, scope string) string {
	t.Helper()
	sel := client.RecallSelectionFor(false, nil, nil)
	opts := report.RecallOptions{Scope: scope, Mode: sel.Mode, Limit: sel.Limit, Page: sel.Page}
	if err := report.ValidateRecall(opts); err != nil {
		t.Fatal(err)
	}
	rep, err := report.Recall(root, opts, store.Unrestricted())
	if err != nil {
		t.Fatal(err)
	}
	return rep.RenderText(agentHost, nil, "")
}

var agentPreRE = regexp.MustCompile(`(?s)<pre class="entry-raw" data-agent="(above|below)"><code>(.*?)</code></pre>`)

// agentText is the tab's two `<pre>` halves, unescaped and concatenated — what a reader copies.
func agentText(t *testing.T, body string) (whole, above string) {
	t.Helper()
	m := agentPreRE.FindAllStringSubmatch(body, -1)
	if len(m) == 0 {
		t.Fatal("the agent tab renders no recall <pre>")
	}
	for _, part := range m {
		whole += html.UnescapeString(part[2])
		if part[1] == "above" {
			above = html.UnescapeString(part[2])
		}
	}
	return whole, above
}

// TestTheAgentTabIsByteForByteTheCLIRecall pins the tab's text to the CLI renderer's output for the
// same scope over the same store, BYTE FOR BYTE, and the `head -60` mark to the line it claims.
func TestTheAgentTabIsByteForByteTheCLIRecall(t *testing.T) {
	readsA, _, _ := arcsWorld(t)
	root := agentStore(t)
	want := cliRecall(t, root, "alpha-notes")
	// INSTRUMENT CONTROL: the expectation is a real recall over real entries, long enough to be cut.
	if !strings.HasPrefix(want, "subsystem-recall: status=") || !strings.Contains(want, "lamp-") {
		t.Fatalf("the CLI-side recall is not a recall of alpha's entries, so equality below proves nothing:\n%s", want)
	}
	if n := strings.Count(want, "\n"); n <= agentHeadLines {
		t.Fatalf("the CLI-side recall is %d lines, not past the head -%d mark this test also pins", n, agentHeadLines)
	}

	rec := getAs(t, agentServer(t, root, readsA), scopeTabURL(browseScopeA, TabAgent))
	if rec.Code != http.StatusOK {
		t.Fatalf("the agent tab answered %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	got, above := agentText(t, body)
	if got != want {
		t.Errorf("the agent tab's text is not the CLI's recall, byte for byte.\n--- tab (%d bytes)\n%s\n--- cli (%d bytes)\n%s",
			len(got), got, len(want), want)
	}
	// The cut: what an agent's `head -60` keeps of the recall text is 60 minus the lines the CLIENT prints
	// above it — `client.RecallPreamble`, the one string `recall` prints there. Read here off the CLIENT
	// package, with a non-live state, never off `agent.go`: a banner that grew a line moves this
	// expectation and the mark must follow.
	want60 := 60 - strings.Count(client.RecallPreamble(client.StateCached, "the cache is a day old", ""), "\n")
	if want60 != 58 {
		t.Logf("the client's preamble is now %d lines (it was 2)", 60-want60)
	}
	if n := strings.Count(above, "\n"); n != want60 {
		t.Errorf("the head -60 mark falls after %d lines of the recall, want %d", n, want60)
	}
	// The text carries no final newline (the CLI's `Fprintln` adds it), so its last line is a line.
	lines := strings.Count(want, "\n")
	if !strings.HasSuffix(want, "\n") {
		lines++
	}
	for _, s := range []string{
		`data-agent="bytes">` + strconv.Itoa(len(want)) + ` bytes<`,
		`data-agent="lines">` + strconv.Itoa(lines) + ` lines<`,
		`data-agent="tokens"`, `≈` + strconv.Itoa((len(want)+3)/4) + ` tokens (estimate)<`,
		"stops here", strconv.Itoa(len(above)) + " bytes above",
		"four places", "`cairn recall --repo`", "state banner", "`store:` line", "local cache path", "`host:` line",
		`<span class="view-tab view-tab-here" data-tab="agent">What an agent sees</span>`,
	} {
		if !strings.Contains(body, s) {
			t.Errorf("the agent tab lacks %q", s)
		}
	}
	// Only this tab reads the recall: the entries tab carries none of it.
	if entries := getAs(t, agentServer(t, root, readsA), scopeURL(browseScopeA)).Body.String(); strings.Contains(entries, `data-agent=`) {
		t.Error("the entries tab renders the agent panel")
	}
}

// TestTheAgentTabRefusesAScopeTheViewerCannotRead: the agent tab of a scope this viewer cannot read is
// the scope page's one refusal — the same bytes as an id that names nothing — and W, who reads beta,
// gets the recall at the SAME URL (the positive control).
func TestTheAgentTabRefusesAScopeTheViewerCannotRead(t *testing.T) {
	readsA, _, readsW := arcsWorld(t)
	root := agentStore(t)
	hidden := getAs(t, agentServer(t, root, readsA), scopeTabURL(browseScopeB, TabAgent))
	absent := getAs(t, agentServer(t, root, readsA), scopeTabURL("scp_nosuchscope000000000000000", TabAgent))
	if hidden.Code != http.StatusNotFound || hidden.Body.String() != browseRefusal {
		t.Errorf("A's agent tab on beta answered %d %q, want 404 %q", hidden.Code, hidden.Body.String(), browseRefusal)
	}
	if hidden.Body.String() != absent.Body.String() || hidden.Code != absent.Code {
		t.Error("the refusal for an unreadable scope differs from the one for an absent scope")
	}
	wide := getAs(t, agentServer(t, root, readsW), scopeTabURL(browseScopeB, TabAgent))
	if got, _ := agentText(t, wide.Body.String()); wide.Code != http.StatusOK || !strings.Contains(got, "chandlery-secret") {
		t.Errorf("POSITIVE CONTROL: W's agent tab on beta answered %d without beta's recall", wide.Code)
	}
}

// TestTheRecallReadIsNarrowedByTheViewersAuthority is the SECOND half of the agent tab's
// authorisation, below the page's refusal: `StoreSource.Recall` asked about a scope the viewer cannot
// read answers what the renderer answers for a scope that is not there — never its entries — so a
// name that reached the read some other way could not widen it.
func TestTheRecallReadIsNarrowedByTheViewersAuthority(t *testing.T) {
	readsA, _, readsW := arcsWorld(t)
	root := agentStore(t)
	src := StoreSource{Root: root, Host: func() string { return agentHost }}
	got, err := src.Recall(readsA.Auth, "beta-notes")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.Text, "chandlery-secret") || strings.Contains(got.Text, "wax-bot") {
		t.Errorf("A's recall of beta-notes carries beta's entry:\n%s", got.Text)
	}
	wide, err := src.Recall(readsW.Auth, "beta-notes")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(wide.Text, "chandlery-secret") {
		t.Fatalf("POSITIVE CONTROL: W's recall of beta-notes lacks beta's entry, so A's absence proves nothing:\n%s", wide.Text)
	}
}
