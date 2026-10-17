package capture

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/ZacxDev/cairn/internal/redact"
	"github.com/ZacxDev/cairn/internal/transcript"
	"github.com/ZacxDev/cairn/internal/transcript/scopeuse"
)

// DryRun reads every session from offset 0 — writing nothing, and reading `a.State` (when the
// caller loaded one) only for where `--run` bound each session and the repo it kept — and prints
// one line per root session: record and blob COUNTS, unit counts by class, redaction counts by rule,
// the derived `V` and the routing decision.
//
// 🔴 IT PRINTS NO STRING VALUE OF ANY RECORD. Session ids come from file names (Claude Code) or
// the session listing (opencode); classes and rule names come from this repository's own tables;
// scope names are what `V` is made of. A tool name, a title, a path or a message never reaches the
// output — the test plants a sentinel in every string field and greps for it.
func (a *Agent) DryRun(w io.Writer) error {
	if a.ClaudeRoot != "" {
		sessions, err := discoverClaude(a.ClaudeRoot)
		if err != nil {
			return err
		}
		for _, cs := range sessions {
			a.dryClaude(w, cs)
		}
	}
	seen := map[string]bool{}
	for _, dir := range a.OpencodeDirs {
		raw, err := a.Runner.List(dir, a.listCap())
		if err != nil {
			return err
		}
		roots, err := listRoots(raw, a.listCap())
		if err != nil {
			return err
		}
		for _, root := range firstSeen(roots, seen) {
			a.dryOpencode(w, root)
		}
	}
	return nil
}

type tally map[string]int

func (t tally) String() string {
	keys := make([]string, 0, len(t))
	for k := range t {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s:%d", k, t[k]))
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, ",")
}

// decisionText is the routing decision a dry run prints for session `root`. A session with no
// recorded scope is placed by its repo: the one `--run`'s state kept for it, when there is one, else
// the repo in `dir`, asked of git exactly as `--run` asks it ([deriveRepo]).
//
// 🔴 IT READS WHERE `--run` BOUND THE SESSION, FROM THE STATE, READ-ONLY (#292's audit, round 3, F2).
// [RepoRules.place]'s answer depends on the session's binding, so a dry run that treated every
// session as unbound printed a decision `--run` would not make for a bound one. `a.State` is what
// the caller loaded with [LoadState] — never saved, never locked; nil (no state file) is every
// session unbound, and the caller says so on stderr.
func (a *Agent) decisionText(root string, v []string, dir string) string {
	var ss *SessionState
	if a.State != nil {
		ss = a.State.Sessions[root]
	}
	rules := RepoRules{Owners: a.OwnerRoutes, Uploadable: a.Uploadable}
	var repo Repo
	if ss != nil {
		rules.Bound = ss.Instance
	}
	if len(v) == 0 {
		switch {
		case ss != nil && ss.Repo != nil:
			repo = *ss.Repo
		case dir != "":
			repo, _ = deriveRepo(dir)
		case ss != nil:
			repo, _ = deriveRepo(ss.Dir)
		}
	}
	dec := Decide(v, repo, a.Routing, rules)
	switch {
	case dec.Held:
		return "held"
	case dec.Deferred:
		return "deferred"
	}
	return "ship:" + dec.Instance
}

func (a *Agent) dryClaude(w io.Writer, cs claudeSession) {
	d := scopeuse.NewDeriver()
	classes, hits := tally{}, tally{}
	records, refused := 0, 0
	for _, path := range cs.Streams {
		lines, _, _, _, err := readNewLines(path, &StreamState{}, -1)
		if err != nil {
			refused++
			continue
		}
		for _, l := range lines {
			records++
			feedClaudeLine(d, l)
			dec := json.NewDecoder(bytes.NewReader(l))
			dec.UseNumber()
			var rec map[string]any
			if dec.Decode(&rec) == nil {
				for _, u := range transcript.ClassifyClaude(rec) {
					classes[string(u.Class)]++
				}
			}
			_, hs := a.Redactor.Record(l)
			for _, h := range hs {
				hits[h.Rule]++
			}
		}
	}
	for name, path := range cs.Blobs {
		data, err := os.ReadFile(path)
		if err != nil {
			refused++
			continue
		}
		if !redact.IsBinary(data) {
			d.Content(string(data))
		}
		_, hs := a.Redactor.Blob(name, data)
		for _, h := range hs {
			hits[h.Rule]++
		}
	}
	a.foldLedger(d, cs.Root)
	v := d.Result().V
	dir := ""
	if len(v) == 0 {
		dir = claudeCwd(cs.Streams["main"])
	}
	fmt.Fprintf(w, "claude %s streams=%d records=%d blobs=%d refused=%d classes=%s redactions=%s v=[%s] decision=%s\n",
		cs.Root, len(cs.Streams), records, len(cs.Blobs), refused, classes, hits, strings.Join(v, ","), a.decisionText(cs.Root, v, dir))
}

func (a *Agent) dryOpencode(w io.Writer, lr listedRoot) {
	root := lr.ID
	d := scopeuse.NewDeriver()
	classes, hits := tally{}, tally{}
	units, streams := 0, 0
	queue := []string{root}
	seen := map[string]bool{root: true}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		raw, err := a.Runner.Export(id)
		if err != nil {
			fmt.Fprintf(w, "opencode %s refused: the export failed\n", root)
			return
		}
		us, kids, err := parseExport(raw)
		if errors.Is(err, ErrIncompleteExport) {
			fmt.Fprintf(w, "opencode %s refused: %d bytes that are not one complete JSON document\n", root, len(raw))
			return
		}
		streams++
		for _, u := range us {
			units++
			if u.Part != nil {
				d.OpencodePart(u.Part)
				for _, c := range transcript.ClassifyOpencodePart(u.Role, id != root, u.Part) {
					classes[string(c.Class)]++
				}
			} else {
				d.Content(string(u.Raw))
			}
			_, hs := a.Redactor.Record(u.Raw)
			for _, h := range hs {
				hits[h.Rule]++
			}
		}
		for _, k := range kids {
			if !seen[k] {
				seen[k] = true
				queue = append(queue, k)
				a.foldLedger(d, k)
			}
		}
	}
	a.foldLedger(d, root)
	v := d.Result().V
	fmt.Fprintf(w, "opencode %s streams=%d units=%d classes=%s redactions=%s v=[%s] decision=%s\n",
		root, streams, units, classes, hits, strings.Join(v, ","), a.decisionText(root, v, lr.Directory))
}
