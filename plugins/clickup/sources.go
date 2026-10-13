package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// 🔴 THE HOST-LOCAL SOURCES (the plan's Q13): commit messages and trailers from a list of repository
// paths the OPERATOR names, PR titles and bodies through the host's own GitHub CLI login, and the refs
// of entries the session wrote, read from a cairn cache this host already syncs. Nothing here is on
// the pod and nothing is configured by default: with no `-repo`, no `-gh-bin` and no `-store-root`
// the plugin matches on the transcript alone.
//
// 🔴 EVERY VALUE THAT REACHES AN ARGV IS VALIDATED FIRST, AND NONE CAN BE READ AS AN OPTION. A session
// id comes from the pod and a PR's number and repository from a transcript record — both are data.
// The session id is checked against the store's own session class and passed as `--grep=<id>`; the PR
// number is an integer and the repository must be `owner/name`. No shell is involved.
//
// ⚠ WHICH COMMITS ARE A SESSION'S: only those whose message carries a TRAILER whose value is the
// session id (any key — `Claude-Session-Id: <id>` is the usual spelling). A session that stamps no
// such trailer has no commit signal; guessing from a branch or a time window would not be
// deterministic.

// sessionShape is the store's session class (`internal/write` `sessionClass`) — a second spelling.
var sessionShape = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

var (
	repoShape   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}/[A-Za-z0-9_.][A-Za-z0-9_.-]{0,99}$`)
	shaShape    = regexp.MustCompile(`^[0-9a-f]{40,64}$`)
	trailerLine = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9-]*):[ \t]*(.*)$`)
)

// Bounds on what one session's sources may cost.
const (
	maxCommits   = 200
	maxExecBytes = 8 << 20
	maxPRs       = 20
	maxEntries   = 20000
	maxEntryFile = 1 << 20
)

// Trailer is one `Key: value` line of a commit message's last paragraph.
type Trailer struct{ Key, Value string }

// Commit is one commit a session's trailer names.
type Commit struct {
	SHA string
	// Message is the message WITHOUT its trailer block.
	Message  string
	Trailers []Trailer
}

// gitNote is a line git itself adds to a message's last paragraph that is no trailer.
var gitNote = regexp.MustCompile(`^\(cherry picked from commit [0-9a-f]{7,64}\)$`)

// gitComment reports whether a paragraph is one git appends that is no part of the message's trailer
// block: `#` comment lines, or a `Conflicts:` list (commented or not) with its tab-indented paths.
func gitComment(par string) bool {
	lines := strings.Split(par, "\n")
	if !strings.HasPrefix(lines[0], "#") && strings.TrimSpace(lines[0]) != "Conflicts:" {
		return false
	}
	for _, line := range lines[1:] {
		if !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "\t") {
			return false
		}
	}
	return true
}

// splitMessage separates a commit message from its trailer block: the LAST paragraph — looking past
// any trailing paragraphs git itself appended ([gitComment]) — when it is not the only one and every
// line of it is a trailer, a continuation of the line above, a line git adds itself
// (`(cherry picked from commit …)`), or a bare URL, which is prose. The message returned is
// EVERYTHING ELSE: only the trailer lines leave it.
func splitMessage(msg string) (string, []Trailer) {
	msg = strings.TrimRight(strings.ReplaceAll(msg, "\r\n", "\n"), "\n")
	pars := strings.Split(msg, "\n\n")
	at := len(pars) - 1
	for at > 0 && gitComment(pars[at]) {
		at--
	}
	if at == 0 {
		return msg, nil // the subject paragraph is never a trailer block
	}
	var out []Trailer
	var prose []string
	for _, line := range strings.Split(pars[at], "\n") {
		m := trailerLine.FindStringSubmatch(line)
		switch {
		case strings.TrimSpace(line) == "":
			// A whitespace-only line is no continuation: appended to the value above it made a
			// session id that was never equal to itself.
		case m != nil && strings.HasPrefix(m[2], "//"):
			prose = append(prose, line) // `https://…` on its own line: a URL, not a `https:` trailer
		case m != nil:
			out = append(out, Trailer{Key: m[1], Value: strings.TrimSpace(m[2])})
		case len(out) > 0 && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")):
			out[len(out)-1].Value += " " + strings.TrimSpace(line)
		case gitNote.MatchString(line):
		default:
			return msg, nil // prose in the last paragraph: it is not a trailer block
		}
	}
	if len(out) == 0 {
		return msg, nil
	}
	rest := append(append([]string{}, pars[:at]...), strings.Join(prose, "\n"))
	for _, p := range pars[at+1:] {
		// A conflict list is a list of PATHS git wrote, not something the author said: a path named
		// for a ticket is no mention of it. Any other paragraph looked past stays in the message.
		if first := strings.TrimSpace(strings.TrimLeft(strings.SplitN(p, "\n", 2)[0], "# ")); first != "Conflicts:" {
			rest = append(rest, p)
		}
	}
	var kept []string
	for _, p := range rest {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, "\n\n"), out
}

// The bounds on ONE run of a host program. Variables, so a test can shorten them.
var (
	// execTimeout bounds the program itself.
	execTimeout = 60 * time.Second
	// execWaitDelay bounds how long its output pipes are waited on after it exits or is killed.
	execWaitDelay = 2 * time.Second
)

// errExecDeadline marks a host program that did not finish inside [execTimeout].
var errExecDeadline = errors.New("it did not finish in time and was killed")

// runProgram executes one host program with a bounded answer AND a bounded life. The environment is
// this process's, so the programs use the operator's own configuration and login.
//
// 🔴 A HOST PROGRAM CAN HANG, AND IT MUST NOT TAKE THE RUN WITH IT (#274's audit). `gh` waiting on a
// network that never answers, or `git` on a lock, used to hold the whole run — and past `-timeout`,
// because a child that inherited the output pipe keeps `Wait` blocked after the program itself is
// killed. Three things bound it: its own deadline ([execTimeout], inside the run's); its PROCESS
// GROUP is killed, not just the program, so a helper it spawned goes too; and [execWaitDelay] closes
// the pipes if anything still holds them. A program that exits 0 while something it started still
// holds its output is refused too: its answer cannot be known complete.
func runProgram(ctx context.Context, env []string, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	killGroup := func() error {
		if cmd.Process == nil {
			return nil
		}
		// The group the child leads (its pid), so whatever it started dies with it.
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.Cancel = killGroup
	cmd.WaitDelay = execWaitDelay
	var out, errb bytes.Buffer
	cmd.Stdout = &limitWriter{w: &out, left: maxExecBytes}
	cmd.Stderr = &limitWriter{w: &errb, left: 4 << 10}
	err := cmd.Run()
	_ = killGroup() // nothing it started outlives it, however it ended
	switch {
	case ctx.Err() != nil:
		return nil, fmt.Errorf("%s: %w (after %s)", filepath.Base(name), errExecDeadline, execTimeout)
	case err != nil:
		return nil, fmt.Errorf("%s: %w: %s", filepath.Base(name), err, strings.TrimSpace(errb.String()))
	}
	return out.Bytes(), nil
}

// limitWriter keeps the first `left` bytes and discards the rest, so a program cannot fill memory.
type limitWriter struct {
	w    *bytes.Buffer
	left int
}

func (l *limitWriter) Write(p []byte) (int, error) {
	n := len(p)
	if len(p) > l.left {
		p = p[:l.left]
	}
	l.left -= len(p)
	l.w.Write(p)
	return n, nil
}

// SessionCommits is every commit in one repository whose trailer block names the session.
func SessionCommits(ctx context.Context, gitBin, repo, session string) ([]Commit, error) {
	if !sessionShape.MatchString(session) {
		return nil, fmt.Errorf("session id %q is not a session id", session)
	}
	// %x1f and %x1e: separators no commit message carries.
	raw, err := runProgram(ctx, []string{"GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0"}, gitBin, "-C", repo, "--no-pager",
		"log", "--all", "--no-show-signature", "-n", strconv.Itoa(maxCommits), "--fixed-strings", "--grep="+session, "--format=%H%x1f%B%x1e")
	if err != nil {
		return nil, err
	}
	var out []Commit
	for _, rec := range strings.Split(string(raw), "\x1e") {
		sha, msg, ok := strings.Cut(strings.TrimLeft(rec, "\n"), "\x1f")
		if !ok || !shaShape.MatchString(sha) {
			continue
		}
		body, trailers := splitMessage(msg)
		named := false
		var rest []Trailer
		for _, t := range trailers {
			if t.Value == session {
				named = true
				continue
			}
			rest = append(rest, t)
		}
		// 🔴 A TRAILER, NOT A MENTION: `--grep` finds the id anywhere in a message, and a commit that
		// merely QUOTES another session's id is not that session's commit.
		if named {
			out = append(out, Commit{SHA: sha, Message: body, Trailers: rest})
		}
	}
	return out, nil
}

// PR is one pull request a session's `pr-link` record names.
type PR struct {
	Number int
	Repo   string
	// Seq and Stream locate the `pr-link` record itself.
	Stream string
	Seq    int64
}

// PRText is what the CLI answered for a PR.
type PRText struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Head  string `json:"headRefName"`
}

// FetchPR reads one PR's title, body and head branch through the host's GitHub CLI.
func FetchPR(ctx context.Context, ghBin string, pr PR) (PRText, error) {
	var t PRText
	if pr.Number < 1 || !repoShape.MatchString(pr.Repo) {
		return t, fmt.Errorf("PR %d of %q is not a PR number and an owner/name repository", pr.Number, pr.Repo)
	}
	raw, err := runProgram(ctx, []string{"GH_PROMPT_DISABLED=1", "NO_COLOR=1"}, ghBin, "pr", "view", strconv.Itoa(pr.Number),
		"--repo", pr.Repo, "--json", "title,body,headRefName")
	if err != nil {
		return t, err
	}
	if err := json.Unmarshal(raw, &t); err != nil {
		return t, fmt.Errorf("the GitHub CLI's answer for PR %d is not the documented JSON: %w", pr.Number, err)
	}
	return t, nil
}

// prURL is a GitHub pull-request URL, for a `pr-link` record that carries no separate fields.
var prURL = regexp.MustCompile(`^https://github\.com/([^/\s]+/[^/\s]+)/pull/([0-9]{1,9})(?:[/?#].*)?$`)

// prOf reads a Claude Code `pr-link` record (R1: `{prNumber, prUrl, prRepository}`).
func prOf(rec map[string]any) (PR, bool) {
	if rec["type"] != "pr-link" {
		return PR{}, false
	}
	var pr PR
	if n, ok := rec["prNumber"].(float64); ok && n >= 1 && n == float64(int(n)) {
		pr.Number = int(n)
	}
	pr.Repo, _ = rec["prRepository"].(string)
	if u, _ := rec["prUrl"].(string); pr.Number == 0 || pr.Repo == "" {
		if m := prURL.FindStringSubmatch(u); m != nil {
			pr.Repo = m[1]
			pr.Number, _ = strconv.Atoi(m[2])
		}
	}
	return pr, pr.Number >= 1 && repoShape.MatchString(pr.Repo)
}

// ---------------------------------------------------------------- entries the session wrote

// 🔴 A DELIBERATELY NARROW SECOND READER OF AN ENTRY, NOT A SECOND PARSER OF THE STORE. This module is
// stdlib-only and cannot import `internal/store`, so it reads exactly TWO facts off a cached entry
// file and nothing else: whether a line ends in a ` [cairn: <actor>/<session>]` attribution (the
// trailer `internal/write` stamps on an appended bullet), and the entry's `refs:` front-matter key
// (with its older spellings `tasks:` / `task:`, read only when `refs:` is absent — the store's rule).
// A file it misreads costs an `entry-ref` edge; it decides nothing about visibility or authority.
//
// ⚠ `put`/`create` stamp no trailer, so an entry a session wrote ONLY that way is not found — the
// same blind spot `internal/touch` states.

var (
	attribution = regexp.MustCompile(`\[cairn: [a-z0-9][a-z0-9-]{0,31}/([A-Za-z0-9][A-Za-z0-9_.-]{0,63})\]`)
	// trailerRun is what may follow an attribution on its line: more bracketed trailer pieces only.
	trailerRun = regexp.MustCompile(`^(?:\s*\[[a-z]+:[^\[\]]*\])*\s*$`)
	// entryRefShape is the pod's rule for an entry locator.
	entryRefShape = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,255}$`)
)

// EntryRefs is one cached entry and the `clickup:` idents its front matter carries.
type EntryRefs struct {
	// Ref is `<scope>/<entry>` — the file's path under the cache root, without `.md`.
	Ref    string
	Idents []string
}

// EntryIndex is every session's attributed entries under the configured cache roots, built ONCE per
// run.
type EntryIndex map[string][]EntryRefs

// sessionsOf is every session id a file's bullets are attributed to.
func sessionsOf(text string) map[string]bool {
	out := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		if !strings.Contains(line, "[cairn: ") {
			continue
		}
		for _, loc := range attribution.FindAllStringSubmatchIndex(line, -1) {
			// Only an attribution in the line's trailing run counts: one quoted mid-prose is no edge.
			if trailerRun.MatchString(line[loc[1]:]) {
				out[line[loc[2]:loc[3]]] = true
			}
		}
	}
	return out
}

// frontMatterRefs is the `clickup:` idents of an entry's `refs:` key, in its two list shapes —
// `refs: [clickup:x, github:o/r#1]` and a block list of `- item` lines — and of the older `task:`
// key's one scalar. A scalar `refs:` is read as nothing: the store refuses such an entry as malformed.
func frontMatterRefs(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return nil
	}
	end := strings.Index(text[4:], "\n---")
	if end < 0 {
		return nil
	}
	lines := strings.Split(text[4:4+end], "\n")
	values := map[string][]string{}
	hasRefs := false
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		key, val, ok := strings.Cut(line, ":")
		if !ok || strings.HasPrefix(line, "-") || strings.HasPrefix(line, "#") {
			continue
		}
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		if key != "refs" && key != "tasks" && key != "task" {
			continue
		}
		hasRefs = hasRefs || (key == "refs" && val != "")
		var items []string
		switch {
		case strings.HasPrefix(val, "[") && strings.HasSuffix(val, "]"):
			items = strings.Split(val[1:len(val)-1], ",")
		case val != "" && key == "task":
			items = []string{val}
		case val != "":
		default:
			for j := i + 1; j < len(lines); j++ {
				item := strings.TrimSpace(lines[j])
				if item == "" || strings.HasPrefix(item, "#") {
					continue
				}
				if item != "-" && !strings.HasPrefix(item, "- ") {
					break
				}
				items = append(items, item[1:])
			}
		}
		for _, it := range items {
			if it = strings.Trim(strings.TrimSpace(it), `'"`); it != "" {
				values[key] = append(values[key], it)
			}
		}
	}
	items := values["refs"]
	// `refs:` WINS BY NOT CONSULTING THE OLDER KEYS AT ALL (the store's rule) — even when what it
	// holds is unreadable here.
	if len(items) == 0 && !hasRefs {
		items = append(values["tasks"], values["task"]...)
	}
	var out []string
	for _, it := range items {
		system, ident, ok := strings.Cut(it, ":")
		if ok && strings.TrimSpace(system) == "clickup" && strings.TrimSpace(ident) != "" {
			out = append(out, strings.TrimSpace(ident))
		}
	}
	return out
}

// BuildEntryIndex walks the cache roots once. A root that cannot be walked is an error; a file that
// cannot be read is skipped and counted.
func BuildEntryIndex(roots []string) (EntryIndex, int, error) {
	ix := EntryIndex{}
	skipped, seen := 0, 0
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				if path == root {
					return err
				}
				skipped++
				return nil
			}
			if d.IsDir() {
				if path != root && strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if !d.Type().IsRegular() || !strings.HasSuffix(d.Name(), ".md") {
				return nil
			}
			if seen++; seen > maxEntries {
				return errTooManyEntries
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return nil
			}
			ref := strings.TrimSuffix(filepath.ToSlash(rel), ".md")
			if !entryRefShape.MatchString(ref) || strings.Contains(ref, "..") {
				return nil
			}
			info, err := d.Info()
			if err != nil || info.Size() > maxEntryFile {
				skipped++
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				skipped++
				return nil
			}
			text := string(raw)
			idents := frontMatterRefs(text)
			if len(idents) == 0 {
				return nil
			}
			for s := range sessionsOf(text) {
				ix[s] = append(ix[s], EntryRefs{Ref: ref, Idents: idents})
			}
			return nil
		})
		if err != nil {
			return nil, skipped, fmt.Errorf("the cache root %s: %w", root, err)
		}
	}
	return ix, skipped, nil
}

var errTooManyEntries = errors.New("it holds more entry files than this plugin will read")
