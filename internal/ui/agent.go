package ui

import (
	"strconv"
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/hostid"
	"github.com/ZacxDev/cairn/internal/report"
)

// 🔴 "WHAT AN AGENT SEES" — the scope page's fourth tab: BYTE FOR BYTE the text `cairn recall --scope
// <scope>` renders, through the SAME `internal/report` renderer the CLI and the pod run. Nothing here
// re-renders: `report.Recall` builds the report and `RecallReport.RenderText` prints it, with the
// options the CLI builds for exactly that command line (no `--list`, `--limit` or `--page`, so the
// default digest mode, the default entry limit and page 1; a scope named, so no focus window).
// `TestTheAgentTabIsByteForByteTheCLIRecall` pins the bytes against that call over the same store.
//
//   - 🔴 AUTHORISED LIKE THE SCOPE PAGE, TWICE. `handleScopePage` refuses a scope this caller cannot
//     read before any tab is chosen, with the one `browseRefusal` every miss gets; and the recall read
//     itself is narrowed by `auth.NamedScopes(VerbRead)` — the set every other read here uses — so a
//     name that reached it by any other route would answer the renderer's own scope-absent text.
//   - ⚠ IT IS THE POD'S SPELLING OF THE HEADER, NOT AN AGENT'S, AND THE TAB SAYS SO. Four differences
//     from what an agent's own run shows are NAMED on the tab rather than papered over: the `--repo`
//     featured pick (the resume skills run `cairn recall --repo`, which features the entry its repo's
//     newest handoff doc names; `--scope` cannot, and this server has no repo to read), the client's
//     state banner (one line, plus a blank one, above the recall), and the `store:`/`host:` lines,
//     which name whoever rendered it — here, this server's store root and host.
//   - ⚠ THE `head -60` MARK. Agents almost always truncate (measured by the operator: most standalone
//     recalls were piped through `head`/`grep`/`sed`, most often `head -60`), so the tab draws where a
//     60-line cut falls. The client prints its banner and a blank line first, so the cut falls after
//     line 58 of the recall text; the two `<pre>` halves concatenate to the exact bytes.
//   - The size is the text's UTF-8 byte count; the token figure is bytes ÷ 4, labelled an ESTIMATE —
//     no tokenizer runs here.

// agentHeadLines is the `head -N` the tab marks, and agentBannerLines what the client prints above the
// recall text (`client.BannerNamed`'s one line, then `fmt.Fprintln(env.Stdout)`'s blank one).
const (
	agentHeadLines   = 60
	agentBannerLines = 2
)

// AgentRecall is the agent tab's data: the rendered recall text, exactly.
type AgentRecall struct {
	Text string
}

// Recall renders `cairn recall --scope <scope>` over the caller's visible set.
func (s StoreSource) Recall(auth control.Authorization, scope string) (AgentRecall, error) {
	visible := scopeSetOf(auth.NamedScopes(control.VerbRead))
	opts := report.RecallOptions{Scope: scope, Mode: report.DefaultMode, Limit: report.DefaultEntryLimit, Page: 1}
	if err := report.ValidateRecall(opts); err != nil {
		return AgentRecall{}, err
	}
	rep, err := report.Recall(s.Root, opts, visible)
	if err != nil {
		return AgentRecall{}, err
	}
	return AgentRecall{Text: rep.RenderText(s.host(), nil, "")}, nil
}

// host is `Host` with the nil case folded in: this machine's identity, the pod's own default.
func (s StoreSource) host() string {
	if s.Host == nil {
		return hostid.ThisHost()
	}
	return s.Host()
}

// agentNote is the tab's ONE note: the three ways an agent's own run differs from this text.
const agentNote = "This is `cairn recall --scope` for this scope, rendered by the same renderer the CLI runs; " +
	"an agent's own run differs in four places, by where it ran. (1) The resume and handoff skills run " +
	"`cairn recall --repo`, which features the entry the repo's newest handoff doc names — `--scope` cannot. " +
	"(2) The client prints a state banner (sync and caveat) above the text. (3) The `store:` line is the " +
	"renderer's store root: this server's here, the agent's local cache path there. (4) The `host:` line " +
	"names the machine that rendered it."

// agentPanel is the agent tab: the note, the numbers, and the text split where `head -60` cuts it.
func agentPanel(a AgentRecall) g.Node {
	text := a.Text
	cut := agentHeadLines - agentBannerLines
	above, below := splitAfterLines(text, cut)
	lines := strings.Count(text, "\n")
	if text != "" && !strings.HasSuffix(text, "\n") {
		lines++
	}
	return h.Div(
		h.ID("scope-agent"),
		h.P(h.Class("note"), g.Text(agentNote)),
		h.P(h.Class("card-stats"),
			h.Span(h.Class("stat"), h.Data("agent", "bytes"), g.Text(plural(len(text), "byte", "bytes"))),
			h.Span(h.Class("stat"), h.Data("agent", "lines"), g.Text(plural(lines, "line", "lines"))),
			h.Span(h.Class("stat"), h.Data("agent", "tokens"), h.TitleAttr("bytes ÷ 4 — an estimate; no tokenizer ran"),
				g.Text("≈"+strconv.Itoa((len(text)+3)/4)+" tokens (estimate)")),
		),
		h.Pre(h.Class("entry-raw"), h.Data("agent", "above"), h.Code(g.Text(above))),
		g.If(below != "", g.Group([]g.Node{
			h.P(h.Class("note"), h.Data("agent", "cut"), g.Text(
				"An agent piping this through `head -"+strconv.Itoa(agentHeadLines)+"` stops here (the client's banner "+
					"and a blank line take its first "+strconv.Itoa(agentBannerLines)+"): "+
					plural(cut, "line", "lines")+" / "+plural(len(above), "byte", "bytes")+" above, "+
					plural(lines-cut, "line", "lines")+" / "+plural(len(below), "byte", "bytes")+" below.")),
			h.Pre(h.Class("entry-raw"), h.Data("agent", "below"), h.Code(g.Text(below))),
		})),
	)
}

// splitAfterLines splits `text` after its first `n` lines (each with its newline). `above + below` is
// `text`, always — the tab's byte-equality rests on it.
func splitAfterLines(text string, n int) (above, below string) {
	at := 0
	for i := 0; i < n; i++ {
		next := strings.IndexByte(text[at:], '\n')
		if next < 0 {
			return text, ""
		}
		at += next + 1
	}
	return text[:at], text[at:]
}
