// Package scopeuse derives the READ half of a session's visibility set — every scope it read
// through cairn, as far as its transcript and its client read ledger show — and the two
// fail-closed fallbacks (`claudedocs/plan-cairn-plugins.md`, decision 3).
//
// 🔴 ONE FUNCTION, TWO CALLERS, THE SAME ANSWER. `cmd/cairn-capture` computes it for ROUTING
// (decision 16) and `cairn-ui` will re-derive it over the STORED records (S3), so an old or lying
// agent cannot shrink `V`. Nothing here resolves a working directory or a variable, or runs a
// shell: a command line is read for one substring (F1), and for cache-root PATHS checked against a
// small allowlist grammar that only ever turns a named read into `*` (F2) — never for what a
// command it does not allow would do.
//
// 🔴 EVERYTHING HERE ONLY ADDS. A header quoted in prose, a spurious ledger record, an
// over-matching F1 or F2 — each adds a scope or `*`, which hides the session from MORE people. The
// sentinel `*` ("unknown scope") is readable by nobody but the owner, so every way this package
// can be wrong about a READ fails closed rather than open.
//
// The parts:
//
//   - R_header: every scope named by a rendered read header (`subsystem-recall:`) found in ANY
//     string of ANY record — tool results, duplicates, hook attachments, opencode outputs. A
//     header whose `scope=` is absent or not a valid scope name (the store-wide `(all scopes)`
//     search, the "all N entry files … MALFORMED" form) contributes `*`.
//   - R_memo (folded into R_header): every scope a MEMO BLOCK names (`<<<cairn-memo` …), found the
//     same way. The memo hook injects one with no header and no command line, and its previews are
//     that scope's text, so it is a read; a block that names no parseable scope contributes `*`.
//     It was missing until #261 round 2 ([MemoScopes]).
//   - L: the scopes the client read ledger names (decision 3a). A ledger line that does not parse,
//     or names an invalid scope, contributes `*`.
//   - F1: a tool INPUT names `cairn` or `subsystem-recall` (case-insensitive) AND the ledger holds
//     ZERO records → `*`. It asks "is the ledger empty", never "which record is this call's".
//   - F2 (named scopes folded into R_header): a tool input naming a path INTO the client cache
//     (`<…cache…>/subsystem-store/<scope>/…`) adds that scope, because a file read from the cache
//     never passes through the client and is never ledgered, so the path is the only record of it.
//     A cache path it cannot attribute to ONE default-instance scope adds `*`: the bare root, a
//     component that is not a valid scope (a glob, `$VAR`, `{a,b}`, `.sync-stamp`), ANOTHER
//     instance's root (`subsystem-store-<alias>`), a sync staging sibling (`subsystem-store.new-…`),
//     a relative `subsystem-store/…`, a root whose parent component is a spelling this cannot read
//     (`$C/subsystem-store/…`); and any input with the client's `--cache` flag adds `*`.
//     🔴 A NAMED SCOPE IS DERIVED ONLY WHEN IT IS PROVABLE — an ALLOWLIST over ONE TOOL INPUT
//     WHOLE, every string of it together (a command AND its `workdir`), and a failure anywhere
//     makes the input `*`. Each string is read by its key: a bash tool's `description` is skipped;
//     a `workdir`/`path`/`cwd`/`dir`/`directory` must be a clean absolute path that is not the
//     cache, under it, or an ancestor of it; a `file_path`/`filePath` must be one clean cache file
//     path; every other string is a shell line that must lex with spaces, tabs, `|`, `||`, `&&`,
//     `;` and newlines only (no other metacharacter outside single quotes; no `/` or `..` inside
//     them), run only `cat`/`cut`/`echo`/`grep`/`head`/`ls`/`nl`/`printf`/`tail`/`tr`/`uniq`/`wc`,
//     have no `.`/`..` word, no other word naming `cache` or `subsystem-store` (a relative
//     `.cache`, `subsystem-store`), no recursive or name-list flag (`grep -r`/`-R`/`-d`/`-NUM`,
//     `ls -R`, `wc --files0-from`, [readsBeyondItsWords]), and have every word holding `/` be one
//     clean, unquoted `~/`-, `/home/<u>/`- or `/root/`-`.cache/subsystem-store/<scope>/<file>` path
//     ([shellLineProvable], [cleanCachePath], [safeDir]). So `cat`, `grep` or `head` of named
//     cache files stays named, and almost every other shape that names the cache — a `$HOME`
//     spelling, a redirect, `sed`, a glob, a second non-cache path — is `*`. `subsystem-store`
//     that is NOT a cache path — a namespace (`-n subsystem-store`), a repo path
//     (`apps/subsystem-store/…`), the config dir (`~/.config/subsystem-store/`), a file name
//     (`check-subsystem-store.sh`), the `.lock` sibling — adds nothing ([eachCacheRead] has the
//     exact rule). And an input whose shell line changes directory to anywhere this cannot show
//     is outside the cache (`cd subsystem-store`, `cd ~`, `cd "$D"`) adds `*` whether or not it
//     names a read ([movesIntoUnseen]): the next call's relative words resolve there.
//
// ⚠ WHAT IT CANNOT SEE (threat T3): a session mixing a ledger-writing client with one that writes
// nothing (non-empty ledger, so F1 is silent); a file read of a store copy whose path lacks
// `subsystem-store`, or lacks a `cache`-named parent component — including an explicit `--cache`
// root under a non-cache parent read DIRECTLY (`cat /tmp/x/subsystem-store/<scope>/a.md` adds
// nothing; only the call carrying `--cache` is `*`); any read that names no cache path at all,
// such as an ancestor of the cache root spelled relatively in a call that names no cache path
// (`grep -r x .cache` or `grep -r x` from the home directory, alone). BESIDE a named read the
// grammar now rejects both relative shapes: a word naming `cache`/`subsystem-store` that is not a
// clean cache path, and a recursive flag, so `cat ~/.cache/subsystem-store/<scope>/a.md; grep -r x
// .cache` is `*` (it added only `<scope>` at #314 round 3). What a named read can still sit beside
// unseen is a RELATIVE, NON-RECURSIVE FILE word (`cat ~/.cache/subsystem-store/<scope>/a.md x.md`):
// it reads one file of the working directory, which the input does not fix — harmless unless that
// directory is inside the cache. In Claude Code it can be: Bash's working directory persists
// between calls, and an earlier call can step there through bare relative words that name no cache
// PATH (`cd ~/.cache`, then `cd subsystem-store`, then `cd <scope>`). So ANY input, named read or
// not, whose shell line runs `cd`/`pushd` (also as `builtin cd`, `command pushd`, after `&&`, `;`,
// `|` or a newline, inside `$(…)`) with an argument naming `cache`/`subsystem-store` (any case,
// quotes removed), or one that is not a plain relative path, a clean absolute path or a clean
// `~/…` path outside every home ancestor — `$D`, `"$D"`, `-`, `~`, `..`, nothing, a glob, `$(…)`,
// a flag — is `*` ([movesIntoUnseen]); `cd /work/repo`, `cd src`, `pushd /tmp/build` add nothing.
// What that still cannot see: a directory change it does not parse as one — through `eval`,
// `bash -c '…'`, `sudo`/`env`/`command -p` before `cd`, a function or alias wrapping it, `CDPATH`
// (a plain `cd <scope>` resolved against a `CDPATH` entry inside the cache), or a `popd` back to
// a directory pushed by a spelling it missed; a plain path that IS the cache through a symlink
// outside it (`cd /work/link` where `link` points into the cache); and a session that STARTS with
// its working directory inside the cache. In opencode a bash call's `workdir` is part of its
// input, which [safeDir] checks; with none it runs in "the current working directory" (the tool's
// own schema text, opencode 2.0.32), which no input names — the same `cd` rule applies to its
// commands, but whether an earlier call's `cd` can move that default directory at all is NOT
// established here (opencode 2.0 keeps shell ids; not read from its source). The same
// earlier-call gap covers a read through a variable set earlier, and a later `ls <scope>` or
// `grep x <file>` (one directory's names, one file); a call that names a cache path while setting
// a variable (`S=~/.cache/subsystem-store/notes1`) is itself `*`, since no assignment passes the
// grammar. Also unseen: a function or alias under an
// allowed command's name, defined in an earlier call or by the HOST (Claude Code's own `grep` is
// one, its embedded ugrep: the flags [readsBeyondItsWords] knows are GNU's plus ugrep's `-NUM` and
// `--depth`, and a host wrapper with any other mode is unseen); a bare relative `subsystem-store` with no `/` after
// it outside a `cd`/`pushd` argument (indistinguishable from the namespace); a symlink into the
// cache; a read that bypasses the
// client entirely; a memo shown by any path that does not print the renderer's block (a memo's
// text quoted without its fence, or the browser tab).
package scopeuse

import (
	"bufio"
	"bytes"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/ZacxDev/cairn/internal/authz"
)

// Star is the "unknown scope" sentinel.
const Star = "*"

// HeaderMarker is the prefix every rendered read header carries (`internal/report`).
const HeaderMarker = "subsystem-recall:"

var scopeField = regexp.MustCompile(`(?:^|\s)scope=(\S*)`)

// ValidScope is the scope-name grammar: the path-component rule the pod applies to a scope
// before it reaches the filesystem (`authz.SafePathComponent`) — one rule, imported, not copied.
func ValidScope(name string) bool { return authz.SafePathComponent.MatchString(name) }

// HeaderScopes returns every scope named by a rendered read header in s, `*` for a header that
// names none or names something that is not a scope.
//
// A header is a marker and the rest of ITS line: every `scope=` field from the marker to the end
// of the line counts toward it, and a marker followed by none adds `*`.
//
// 🔴 LINEAR, AND THAT IS A REQUIREMENT, NOT A TUNING: the pod runs this over every record it
// stores, while that session's uploads wait on its lock. It used to re-read the rest of the line
// after EVERY marker, so a line of back-to-back markers with no newline was quadratic — measured
// at 2.3 s for a 32 KB record and 2 min 39 s for 256 KB, under a 4 MiB cap (#261's audit). Now
// each line is scanned once: the fields after its FIRST marker are found in one pass (they are the
// union of every later marker's, since a field begins at whitespace and no field can swallow one),
// plus the one case a later marker adds by itself — `scope=` IMMEDIATELY after it, where the old
// per-marker scan matched at the start of its substring. That value's END is found by one forward
// cursor every marker shares; looking for it from each marker was still quadratic on a
// whitespace-free run of `subsystem-recall:scope=` (round 2: 691 ms at 128 KB).
// `TestHeaderScopesMatchesThePerMarkerScan` holds the scan to the per-marker answer over generated
// lines; `TestHeaderScopesIsLinearOnARunOfMarkers` holds both shapes to linear growth.
//
// ⚠ A SHORTER RULE WAS PROPOSED AND IS NOT EQUIVALENT: "the fields after the first marker, `*` if no
// field starts at or after the LAST marker, and any later marker's immediate `scope=` value". On
// `subsystem-recall:x subsystem-recall:scope=a` the first marker has no field of its own and the
// per-marker answer is {*, a}; the rule gives {a} if an immediate `scope=` counts as a field (a
// dropped `*` — fail OPEN), and on `subsystem-recall:scope=b subsystem-recall:scope=a` it gives
// {*, a, b} against {a, b} if it does not. Per-marker `found` is kept for that reason.
func HeaderScopes(s string) []string {
	var out []string
	eachHeaderScope(s, func(v string) { out = append(out, v) })
	return out
}

// eachHeaderScope is [HeaderScopes] without the slice: it hands each scope (or `*`) to emit as it
// is found. 🔴 IT ALLOCATES NOTHING PER LINE, and that is a MEMORY bound, not a tuning: the pod
// runs it over a record of up to 4 MiB inside a 256 Mi limit, and a record that is one long run of
// header lines made the slice-per-line form (a marker list, a regexp match list, the result)
// allocate more than the whole assembly's budget (`TestTheAssembledRecordCapFitsThePod`, the
// escaped-markers shape).
func eachHeaderScope(s string, emit func(string)) {
	for {
		i := strings.Index(s, HeaderMarker)
		if i < 0 {
			return
		}
		line := s[i:]
		rest := ""
		if nl := strings.IndexByte(line, '\n'); nl >= 0 {
			line, rest = line[:nl], line[nl:]
		}
		lineScopes(line, emit)
		s = rest
	}
}

// fieldEnd is RE2's `\s` — the characters a `scope=` value (`\S*`) runs up to.
const fieldEnd = "\t\n\f\r "

const scopeEq = "scope="

func isFieldEnd(c byte) bool { return strings.IndexByte(fieldEnd, c) >= 0 }

// valueEnd is the end of the `\S*` value starting at start.
func valueEnd(body string, start int) int {
	if e := strings.IndexAny(body[start:], fieldEnd); e >= 0 {
		return start + e
	}
	return len(body)
}

// lineScopes is [eachHeaderScope] for ONE line that begins with a marker.
//
// The fields are what `scopeField` (`(?:^|\s)scope=(\S*)`) finds in the body after the first
// marker, found here by hand so nothing is allocated: a `scope=` at the body's start or after a
// whitespace byte. Matches cannot overlap (a value ends AT whitespace, and the next match's
// whitespace is at or after it), so every such position is a match, and a match STARTS one byte
// before its `scope=` (the whitespace), or at 0 for `^`. All the per-marker loop needs from the
// match list is whether one starts at or after a marker — that is, the LAST match's start.
func lineScopes(line string, emit func(string)) {
	add := func(v string) {
		if ValidScope(v) {
			emit(v)
		} else {
			emit(Star)
		}
	}
	first := len(HeaderMarker) // line begins with a marker
	body := line[first:]
	lastStart := -1
	for p := 0; ; {
		j := strings.Index(body[p:], scopeEq)
		if j < 0 {
			break
		}
		at := p + j
		if at == 0 || isFieldEnd(body[at-1]) {
			start := at + len(scopeEq)
			end := valueEnd(body, start)
			add(body[start:end])
			lastStart = max(at-1, 0)
			p = end
			continue
		}
		p = at + 1
	}
	// end is where the field-end scan last stopped: the first field end at or after the position it
	// was asked about. Markers arrive in order, so a later marker whose value starts at or before
	// `end` ends at that same `end` and never rescans; only one starting past it scans again, from
	// its own start. Every byte is scanned at most once per line. (Scanning from each marker instead
	// was the round-2 quadratic: with no whitespace, each `scope=` value ran to the end of the line.)
	end := -1
	for rel := 0; ; {
		found := false
		if rel > 0 && strings.HasPrefix(body[rel:], scopeEq) {
			start := rel + len(scopeEq)
			if end < start {
				end = valueEnd(body, start)
			}
			add(body[start:end])
			found = true
		}
		if lastStart >= rel {
			found = true
		}
		if !found {
			emit(Star)
		}
		j := strings.Index(body[rel:], HeaderMarker)
		if j < 0 {
			return
		}
		rel += j + len(HeaderMarker)
	}
}

// Deriver accumulates one ROOT session's evidence across every stream. Feed it every string of
// every record ([Deriver.Content]), every tool input ([Deriver.ToolInputValue]) and every ledger file
// ([Deriver.Ledger]); then read [Deriver.Result].
type Deriver struct {
	headers       map[string]bool
	ledger        map[string]bool
	ledgerRecords int
	namesProgram  bool
	namesCache    bool
}

// NewDeriver returns an empty Deriver.
func NewDeriver() *Deriver {
	return &Deriver{headers: map[string]bool{}, ledger: map[string]bool{}}
}

// Content scans one string of any record for rendered read headers.
//
// A string that is ITSELF a JSON document (a tool printing a JSON response that embeds a rendered
// recall) is also decoded and its strings scanned: in the raw text the header's newline is the two
// characters `\n`, so the `scope=` value runs into the next field and parses as no scope at all —
// fail-closed `*`, but `*` where the real scope is readable (review round 1). So a string that
// decodes is scanned through EVERY string and EVERY key it holds instead of as raw text — nothing
// it carries goes unscanned — and one that does not decode is scanned raw.
//
// 🔴 THE DECODE IS NOT GATED ON THE MARKER BEING PRESENT RAW. It was, and JSON can ESCAPE any
// character of the marker (`subsystem-recall:`), so such a string was neither decoded nor matched
// raw and contributed NOTHING — not even `*` (#261 round 2).
//
// 🔴 IT IS GATED ON A MARKER BEING *POSSIBLE*, AND THAT GATE IS EXACT, NOT A HEURISTIC. Decoding
// rewrites only escapes, and the only JSON escape that can produce a character of either marker
// (letters, `-`, `:`, `<`) is `\uXXXX`; `\" \\ \/ \b \f \n \r \t` produce none of them. So a
// string holding neither marker raw and no `\u` decodes to strings holding no marker — and, by the
// same argument one level down, so does everything nested in it — and neither the decoded scan nor
// the raw one can add anything. Such a string is skipped whole. That matters for MEMORY: a 4 MiB
// record decoded token by token was most of what pushed one assembly at the cap over its budget
// (`TestTheAssembledRecordCapFitsThePod`, #261 round 3); `TestTheDecodeGateIsExact` holds the gated
// answer to the always-decode one over generated documents.
//
// The cost stays linear: the gate, json.Valid and the token walk are each one pass, the walk hands
// out substrings rather than copies (only an escaped token is unescaped), and the depth cap bounds
// how many times one byte is re-decoded through nesting.
func (d *Deriver) Content(s string) { d.content(s, 0) }

func (d *Deriver) content(s string, depth int) {
	if !MayHoldMarker(s) {
		return
	}
	if depth < 3 {
		t := strings.TrimSpace(s)
		if len(t) >= 2 && (t[0] == '{' || t[0] == '[') && json.Valid([]byte(t)) {
			eachJSONString(t, func(inner string) { d.content(inner, depth+1) })
			return
		}
	}
	eachHeaderScope(s, d.addHeader)
	eachMemoScope(s, d.addHeader)
}

func (d *Deriver) addHeader(sc string) { d.headers[sc] = true }

// MayHoldMarker reports whether s, or any string JSON-decoding s (at any depth) can produce, can
// hold a header or memo marker — see [Deriver.Content] for why `\u` is the only escape that counts.
func MayHoldMarker(s string) bool {
	return strings.Contains(s, HeaderMarker) || strings.Contains(s, MemoBlockOpen) || strings.Contains(s, `\u`)
}

// MemoBlockOpen and MemoBlockClose begin the first and last lines of a memo block as
// `internal/memo` renders it (`<<<cairn-memo untrusted nonce=… count=…>>>` … `<<<end cairn-memo
// nonce=…>>>`). They are spelled here rather than imported because `internal/memo` pulls the
// store and the redactor into every capture binary; `TestAMemoBlockIsARead` renders a REAL block
// through `memo.RenderPreview` and `memo.RenderFull`, so a renderer change that this parse no
// longer reads turns that test red instead of shrinking `V`.
const (
	MemoBlockOpen  = "<<<cairn-memo"
	MemoBlockClose = "<<<end cairn-memo"
)

// MemoScopes returns every scope a memo block in s names — `*` for a block that names none this
// can parse. A memo block is a READ: the memo hook injects one into a session (a hook attachment,
// with no `subsystem-recall:` header and no command line anywhere), and its previews are text OF
// that scope. Before this, a session that wrote only alpha-notes and was shown a gamma-notes memo
// was visible to an alpha-only viewer, memo text included (#261 round 2).
//
// A block runs from its open marker to its close marker or the next open marker, whichever comes
// first, so a string of unterminated opens is still scanned once. Within it, a scope is read from
// two line shapes, each after the block's `| ` content prefix:
//
//	[m-<id>] from <sender> · <scope> · <created> · expires <date>
//	and <n> more on <scope>: cairn memo-read --scope <word>
//
// The memo heading is split from the RIGHT, so a sender carrying ` · ` cannot move the scope. A
// scope that is not a valid name is `*`, like a header's.
func MemoScopes(s string) []string {
	var out []string
	eachMemoScope(s, func(v string) { out = append(out, v) })
	return out
}

// eachMemoScope is [MemoScopes] without the slices — no result list and no split of a block into
// lines — for the same memory reason as [eachHeaderScope].
func eachMemoScope(s string, emit func(string)) {
	for {
		i := strings.Index(s, MemoBlockOpen)
		if i < 0 {
			return
		}
		s = s[i+len(MemoBlockOpen):]
		// The next OPEN bounds the block before the close is looked for: searching for the close
		// first would scan to the end of s from every unterminated open — quadratic.
		block := s
		if j := strings.Index(block, MemoBlockOpen); j >= 0 {
			block = block[:j]
		}
		if j := strings.Index(block, MemoBlockClose); j >= 0 {
			block = block[:j]
		}
		named := false
		for rest := block; ; {
			line := rest
			nl := strings.IndexByte(rest, '\n')
			if nl >= 0 {
				line, rest = rest[:nl], rest[nl+1:]
			}
			line = strings.TrimPrefix(strings.TrimRight(line, "\r"), "| ")
			if sc, ok := memoLineScope(line); ok {
				named = true
				if ValidScope(sc) {
					emit(sc)
				} else {
					emit(Star)
				}
			}
			if nl < 0 {
				break
			}
		}
		if !named {
			emit(Star)
		}
		s = s[len(block):]
	}
}

const memoMoreCommand = ": cairn memo-read --scope "

func memoLineScope(line string) (string, bool) {
	if strings.HasPrefix(line, "[m-") && strings.Contains(line, "] from ") {
		// The scope is the third ` · `-separated part from the RIGHT. The separators are found as
		// `strings.Split` finds them — left to right, NOT overlapping (` · · ` holds one, not two) —
		// keeping only the last three, so nothing is allocated and the answer is Split's exactly
		// (`TestMemoHeadingSplitsFromTheRightWithoutSplitting`).
		const sep = " · "
		a, b, c := -1, -1, -1
		for off := 0; ; {
			j := strings.Index(line[off:], sep)
			if j < 0 {
				break
			}
			a, b, c = b, c, off+j
			off += j + len(sep)
		}
		if a >= 0 && strings.HasPrefix(line[c+len(sep):], "expires ") {
			return line[a+len(sep) : b], true
		}
		return "", true // a heading this cannot read: still a memo, so `*`
	}
	if strings.HasPrefix(line, "and ") {
		if k := strings.Index(line, " more on "); k >= 0 {
			if e := strings.LastIndex(line, memoMoreCommand); e > k {
				return line[k+len(" more on ") : e], true
			}
		}
	}
	return "", false
}

// eachJSONString hands emit every string TOKEN of one VALID JSON document t — every key and every
// value, DUPLICATE members included (a map decode keeps only the last of two equal keys, and a
// header in the first would be lost). The caller has checked validity, so a `"` outside a string
// always opens one; a token with no escape is handed out as a substring of t (no copy), and only an
// escaped one is decoded, by `encoding/json` itself so every escape and invalid-UTF-8 rule is the
// library's. It replaced a `json.Decoder` token walk, which first copied t into a buffer grown by
// doubling and then copied each token out again.
func eachJSONString(t string, emit func(string)) {
	for i := 0; i < len(t); i++ {
		if t[i] != '"' {
			continue
		}
		end, escaped := i+1, false
		for end < len(t) && t[end] != '"' {
			if t[end] == '\\' {
				escaped = true
				end++
			}
			end++
		}
		raw := t[i : end+1]
		if !escaped && utf8.ValidString(raw) {
			emit(raw[1 : len(raw)-1])
		} else {
			var v string
			_ = json.Unmarshal([]byte(raw), &v)
			emit(v)
		}
		i = end
	}
}

// ToolInputValue records ONE tool's whole decoded INPUT (a `tool_use` block's `input`, an opencode
// part's `state.input`) as ONE unit: every string it holds, at any depth, together.
//
// 🔴 F2 MUST SEE THE INPUT WHOLE, NOT STRING BY STRING. One input's strings act together: an
// opencode bash input `{"command":"cat ../bb/x.md","workdir":"…/subsystem-store/aa"}` reads `bb`,
// and neither string alone says so — the command has no cache path, the workdir no `..`. Fed one
// string at a time it derived `[aa]` (#314 round 1).
//
// The tool is not known here, so no string is read as free text ([role]); [Deriver.ClaudeRecord]
// and [Deriver.OpencodePart] pass the tool's name.
func (d *Deriver) ToolInputValue(v any) { d.toolInput("", v) }

// toolInput is [Deriver.ToolInputValue] for the named tool: each TOP-LEVEL string member of the
// input gets the role its key gives it ([keyRole]); every other string, at any depth, is a shell
// line.
func (d *Deriver) toolInput(tool string, v any) {
	var ss []string
	var roles []role
	add := func(r role) func(string) {
		return func(s string) { ss, roles = append(ss, s), append(roles, r) }
	}
	if m, ok := v.(map[string]any); ok {
		for k, e := range m {
			if s, ok := e.(string); ok {
				add(keyRole(tool, k))(s)
			} else {
				eachString(e, add(shellRole))
			}
		}
	} else {
		eachString(v, add(shellRole))
	}
	d.toolInputStrings(ss, roles)
}

// ToolInput records one tool input that IS one string (a raw command line). It is the
// one-string unit of [Deriver.ToolInputStrings].
func (d *Deriver) ToolInput(s string) { d.ToolInputStrings([]string{s}) }

// ToolInputStrings records ONE tool input given as all its strings (a command line, a file path,
// a working directory, an argument), every one of them read as a shell line ([role]). Inputs are
// content too, so the header scan runs on each string; F1 asks of each string; F2 decides over
// the strings TOGETHER ([eachCacheRead]).
func (d *Deriver) ToolInputStrings(ss []string) { d.toolInputStrings(ss, nil) }

// role is what F2's grammar ([provable]) reads one string of a tool input as.
type role int

const (
	// shellRole: a shell line, held to [shellLineProvable]. Every string whose key is not below.
	shellRole role = iota
	// freeRole: a bash tool's `description` — free text that is never run, so the grammar skips it
	// (it is still scanned for the cache name, and is still content).
	freeRole
	// dirRole: a working directory or a search root (`workdir`, `path`, `cwd`, `dir`,
	// `directory`), held to [safeDir].
	dirRole
	// fileRole: a file a file tool opens (`file_path`, `filePath`), held to [cleanCachePath].
	fileRole
)

// keyRole is the role of a top-level member key of tool's input. Only a bash tool's
// `description` (Claude Code's `Bash`, opencode's `bash`) is free text; no other key of any tool is
// skipped. The directory keys are read the same way for every tool: [safeDir] admits only an
// absolute path that cannot reach the cache, which is safe whatever the tool does with it.
func keyRole(tool, key string) role {
	switch key {
	case "description":
		if strings.EqualFold(tool, "bash") {
			return freeRole
		}
	case "workdir", "path", "cwd", "dir", "directory":
		return dirRole
	case "file_path", "filePath":
		return fileRole
	}
	return shellRole
}

func (d *Deriver) toolInputStrings(ss []string, roles []role) {
	lowers := make([]string, len(ss))
	for k, s := range ss {
		d.Content(s)
		lowers[k] = asciiLower(s)
		if strings.Contains(lowers[k], "cairn") || strings.Contains(lowers[k], "subsystem-recall") {
			d.namesProgram = true
		}
	}
	eachCacheRead(ss, lowers, roles, func(sc string) {
		if sc == Star {
			d.namesCache = true
		} else {
			d.addHeader(sc)
		}
	})
}

// CacheRootName is the default instance's cache directory name (`client.DefaultCacheRoot`:
// `$HOME/.cache/subsystem-store`; another instance's is the sibling `subsystem-store-<alias>`,
// `client.CacheRootFor`). Spelled here, not imported, for the same reason as [MemoBlockOpen]:
// `internal/client` would pull the whole client into every capture binary.
// `TestCacheScopesMatchesTheClientsCacheRoot` holds it to the client's resolution, so a moved
// root turns that test red instead of silently making F2 blind.
const CacheRootName = "subsystem-store"

// lockSuffix is the client's sync lock beside a cache root (`client/snapshot.go`): a sibling FILE,
// not a store, so naming it is not a read.
const lockSuffix = ".lock"

// CacheScopes returns what F2 adds for ONE tool input given as its strings, every one read as a
// shell line: the scope of every cache-root READ it names, `*` for one it cannot attribute to a
// single default-instance scope. See [eachCacheRead].
func CacheScopes(ss ...string) []string {
	lowers := make([]string, len(ss))
	for k, s := range ss {
		lowers[k] = asciiLower(s)
	}
	var out []string
	eachCacheRead(ss, lowers, nil, func(v string) { out = append(out, v) })
	return out
}

// eachCacheRead is F2, over ONE tool input: ss is every string it holds, lowers their asciiLower
// (passed in so the one copy [Deriver.ToolInputStrings] makes serves F1 and F2), roles what each
// string is ([role]; nil reads every string as a shell line). A file read from the client cache
// never passes through the client and is never ledgered, so the PATH is the only evidence of what
// was read. It finds every `subsystem-store` (case-insensitive) in every string, of every role, and
// classifies it — a NAME, not a substring:
//
//   - NOT A CACHE PATH, adds nothing: the name is part of a longer word (`test-check-subsystem-
//     store-phase1.sh`, `subsystem-stores`); it is a path component whose parent is a plain
//     literal word not containing `cache` (`clusters/…/apps/subsystem-store/…`,
//     `~/.config/subsystem-store/`, `deploy/subsystem-store`); it is a bare word with no `/` after it (`kubectl -n
//     subsystem-store`); or it is the sync lock `subsystem-store[-alias].lock` and nothing more.
//   - A CACHE ROOT: the parent component contains `cache` — `~/.cache`, `/home/<u>/.cache`,
//     `$HOME/.cache`, `$XDG_CACHE_HOME`, and any other spelling that ends in a component naming a
//     cache. A RELATIVE `subsystem-store/…` (preceded by whitespace, punctuation, `./` or `../`) is
//     `*`: the working directory is unknown. So is one whose parent component is neither a cache
//     nor a plain literal word (`$C/subsystem-store`, `"$C"/subsystem-store/…`,
//     `~/subsystem-store`): an unrecognised spelling of the root is `*`, never "not a cache"
//     ([rootContext]).
//   - A cache root is `*` unless it is the default instance (no `-<alias>` suffix: derived scopes
//     carry no instance), a `/` follows it, and the next component passes [ValidScope]. One that
//     passes is a NAMED read of that scope.
//
// 🔴 A NAMED READ IS ONLY A CANDIDATE. The named scopes are emitted only when the WHOLE input is
// [provable] — an allowlist grammar under which the input's text can read nothing under the cache
// but files of those scopes. Any input naming a read that is not provable adds `*` instead. And
// any input with a `--cache` word (the client's explicit cache root, `internal/client`) adds `*`
// whether or not it names `subsystem-store`: that root can be anywhere ([cacheFlag]). So does any
// input moving the working directory somewhere unseen ([movesIntoUnseen]), named read or not.
//
// LINEAR: every occurrence looks back over its parent component only (disjoint per occurrence)
// and forward over its scope component; [provable] is one pass over each string, and runs only for
// an input that names a read; [movesIntoUnseen] is one pass over each shell line.
func eachCacheRead(ss, lowers []string, roles []role, emit func(string)) {
	var named []string
	starred := false // a `*` already emitted, so the directory rule below does not repeat it
	emit = func(out func(string)) func(string) {
		return func(v string) { starred = starred || v == Star; out(v) }
	}(emit)
	for k, s := range ss {
		lower := lowers[k]
		if !strings.Contains(lower, CacheRootName) {
			continue
		}
		for off := 0; ; {
			j := strings.Index(lower[off:], CacheRootName)
			if j < 0 {
				break
			}
			i := off + j
			end := i + len(CacheRootName)
			// The alias suffix, `client.ValidAlias`'s `-[a-z0-9][a-z0-9-]*`.
			alias := false
			if end+1 < len(lower) && lower[end] == '-' && isAlnum(lower[end+1]) {
				alias = true
				end++
				for end < len(lower) && (isAlnum(lower[end]) || lower[end] == '-') {
					end++
				}
			}
			// 🔴 THE NEXT SEARCH STARTS PAST THE ALIAS, NOT PAST THE NAME: an occurrence inside it is the
			// same word (preceded by `-`, never a root), and starting inside it re-read the alias to its
			// end from every occurrence — quadratic, 52 s on a 2 MiB `subsystem-store-` run
			// (`TestCacheScopesIsLinear`).
			off = end
			var next byte // 0 = end of input
			if end < len(lower) {
				next = lower[end]
			}
			if isAlnum(next) {
				continue // a longer word
			}
			if strings.HasPrefix(lower[end:], lockSuffix) {
				after := end + len(lockSuffix)
				if after == len(lower) || !isPathByte(lower[after]) {
					continue // the sync lock, a file
				}
			}
			switch kind := rootContext(lower, i); {
			case kind == notRoot:
				continue
			case kind == relativeRoot && next != '/':
				continue // a bare word: a namespace, a service name
			case kind == relativeRoot, kind == unknownRoot:
				emit(Star)
				continue
			}
			if alias || next != '/' {
				emit(Star)
				continue
			}
			start := end + 1
			stop := start
			for stop < len(s) && s[stop] != '/' && !isShellSeparator(s[stop]) {
				stop++
			}
			sc := s[start:stop]
			if !ValidScope(sc) {
				emit(Star)
				continue
			}
			named = append(named, sc)
		}
	}
	if cacheFlag(lowers) {
		emit(Star)
		return
	}
	if movesIntoUnseen(ss, roles) {
		if !starred {
			emit(Star)
		}
		return
	}
	if len(named) == 0 {
		return
	}
	if !provable(ss, roles) {
		emit(Star)
		return
	}
	for _, sc := range named {
		emit(sc)
	}
}

// provable reports whether one tool input, every string of it by its role, passes F2's ALLOWLIST
// grammar — the only case in which the cache scopes it names are derived instead of `*`:
//
//   - a shell line passes [shellLineProvable];
//   - a working directory passes [safeDir];
//   - a file path is one [cleanCachePath];
//   - a bash tool's `description` is not read.
//
// 🔴 IT IS AN ALLOWLIST, NOT A LIST OF HAZARDS. Two audit rounds each found shell spellings that
// slipped past the denylist this replaced (an empty quote pair inside `..`, `{.,}`, `$'\x2e'`, a
// `cd` in another quoting); here a spelling this does not recognise FAILS, so the next one is
// over-poisoned, not missed.
func provable(ss []string, roles []role) bool {
	for k, s := range ss {
		r := shellRole
		if roles != nil {
			r = roles[k]
		}
		var ok bool
		switch r {
		case freeRole:
			ok = true
		case dirRole:
			ok = safeDir(s)
		case fileRole:
			ok = cleanCachePath(s)
		default:
			ok = shellLineProvable(s)
		}
		if !ok {
			return false
		}
	}
	return true
}

// provableCommands are the only commands a provable shell line may run: each reads the files its
// arguments name (or stdin) and nothing else, and none runs another program, changes the working
// directory, or takes a path it derives from its input. Not here, so `*`: `cd`/`pushd`/`popd`
// (bare `cd` is `$HOME`), `xargs`, `find`, `sed` (`e`, `r`, `w`), `awk` (`system`), `sort`
// (`--compress-program`), any shell or interpreter — one of them turns text into a path this cannot
// read (`… | sed s,a.x.md,, | xargs grep -r t` is the cache root).
var provableCommands = map[string]bool{
	"cat": true, "cut": true, "echo": true, "grep": true, "head": true, "ls": true, "nl": true,
	"printf": true, "tail": true, "tr": true, "uniq": true, "wc": true,
}

// shellLineProvable is the grammar for one shell line, read by a strict lexer in one pass:
//
//   - Words are separated by spaces and tabs; `|`, `||`, `&&`, `;` and a newline separate
//     commands. ANY other shell metacharacter outside single quotes — `$` a backtick `\ " { } * ?
//     [ ] ( ) < > & ~ ! # ^`, and any other control byte — fails; `~` is allowed only as the
//     leading `~/` of a word that is a [cleanCachePath].
//   - A single-quoted segment is literal text, but one holding `/` or `..` fails (it could be a
//     path), and an unterminated quote fails.
//   - The first word of every command must be one of [provableCommands], by its unquoted text.
//   - Every word holding `/` must be ONE [cleanCachePath], unquoted: no other path is provable.
//   - A word that is `.` or `..` once unquoted fails (with `/` it has already failed above).
//   - A word that, once unquoted, holds `cache` or [CacheRootName] (any case) and is not a
//     [cleanCachePath] fails: with no `/` it is RELATIVE (`.cache`, `subsystem-store`,
//     `--cached`), and from the home directory or `~/.cache` it is an ancestor of every scope.
//   - A flag putting an allowed command into a mode that reads more than the files its words
//     name fails ([readsBeyondItsWords]): `grep`'s recursion, `ls -R`, `wc --files0-from`.
//
// ⚠ A word with no `/` that passes is a FILE argument read non-recursively, and it resolves
// against a working directory this does not know — that is T3 (the package doc), not something
// this proves.
func shellLineProvable(s string) bool {
	cmd := true                // the next word is a command's first
	prog := ""                 // the current command's first word, unquoted
	start, quoted := -1, false // the current word's first byte, and whether it holds a quote
	var val []byte             // the current word unquoted, built only once it holds a quote
	endWord := func(i int) bool {
		if start < 0 {
			return true
		}
		raw := s[start:i]
		v := raw
		if quoted {
			v = string(val)
		}
		ok := true
		switch lv := asciiLower(v); {
		case strings.IndexByte(raw, '/') >= 0:
			ok = !quoted && cleanCachePath(raw)
		case v == "." || v == "..":
			ok = false
		case strings.Contains(lv, "cache") || strings.Contains(lv, CacheRootName):
			ok = false // a cache-named word that is not a clean cache path: relative, so `*`
		}
		if cmd {
			ok = ok && provableCommands[v]
			prog = v
			cmd = false
		} else if readsBeyondItsWords(prog, v) {
			ok = false
		}
		start, quoted, val = -1, false, val[:0]
		return ok
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == ' ' || c == '\t':
			if !endWord(i) {
				return false
			}
		case c == '\n' || c == ';' || c == '|':
			if !endWord(i) {
				return false
			}
			if c == '|' && i+1 < len(s) && s[i+1] == '|' {
				i++
			}
			cmd = true
		case c == '&':
			if i+1 >= len(s) || s[i+1] != '&' || !endWord(i) {
				return false
			}
			i++
			cmd = true
		case c == '\'':
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				return false
			}
			body := s[i+1 : i+1+j]
			if strings.IndexByte(body, '/') >= 0 || strings.Contains(body, "..") {
				return false
			}
			if start < 0 {
				start = i
			}
			if !quoted {
				val = append(val[:0], s[start:i]...)
				quoted = true
			}
			val = append(val, body...)
			i += j + 1
		case c == '~':
			if start >= 0 || i+1 >= len(s) || s[i+1] != '/' {
				return false
			}
			start = i
		case c < 0x20 || c == 0x7f || strings.IndexByte("$`\\\"{}*?[]()<>!#^", c) >= 0:
			return false
		default:
			if start < 0 {
				start = i
			}
			if quoted {
				val = append(val, c)
			}
		}
	}
	return endWord(len(s))
}

// readsBeyondItsWords reports whether w, an argument word of prog (both unquoted), is a flag that
// makes prog read files its words do not name: a directory tree, or a list of names read from a
// file. Of [provableCommands], only three have such a mode (each one's `--help`, GNU coreutils
// 9.11 and grep 3.12):
//
//   - `grep`: `-r`, `-R`, `--recursive`, `--dereference-recursive`, `-d recurse` and
//     `--directories=recurse`. Every short-flag cluster holding `r`, `R` or `d` fails, so
//     `-rn`, `-nR` and `-drecurse` do — and so does `-d skip`, and a cluster whose attached
//     argument happens to hold one of them (`-erate`): over-poisoned, never missed.
//     🔴 `grep` IS NOT ALWAYS GNU grep: Claude Code's Bash shadows it with a shell function
//     that runs its embedded ugrep, where `-NUM` and `--depth=…` RECURSE (measured: `grep -n2
//     needle sub` finds `sub/deep/f.txt`; GNU reads `-2` as `-C 2`). So a cluster holding a
//     digit fails too (`-m1`, `-A3` over-poisoned), and so does `--depth`.
//   - `ls`: `-R` and `--recursive`; every cluster holding `R` (`-laR`).
//   - `wc`: `--files0-from=F` reads the names of the files to count from F (or stdin, which
//     `printf '\057…'` can fill with a path no word spells).
//
// A long flag fails when its name (before any `=`) is a PREFIX of one of those, whole or
// abbreviated (`--rec`, `--direc=recurse`, `--files0`), because getopt_long accepts an
// abbreviation; an ambiguous one (`--re`, `--d`) fails too.
// The rest read only their words or stdin: `cat`, `cut`, `head`, `nl`, `tail`, `tr`, `uniq`
// have no recursive or name-list mode, and `echo`/`printf` read nothing. (`uniq IN OUT` WRITES
// `OUT`; that is not a read.)
func readsBeyondItsWords(prog, w string) bool {
	if len(w) < 2 || w[0] != '-' {
		return false
	}
	var short string
	var long []string
	switch prog {
	case "grep":
		short = "rRd0123456789"
		long = []string{"--recursive", "--dereference-recursive", "--directories", "--depth"}
	case "ls":
		short, long = "R", []string{"--recursive"}
	case "wc":
		long = []string{"--files0-from"}
	default:
		return false
	}
	if w[1] != '-' {
		return strings.ContainsAny(w[1:], short)
	}
	name, _, _ := strings.Cut(w, "=")
	if len(name) <= 2 {
		return false // `--`, the end of the flags
	}
	for _, l := range long {
		if strings.HasPrefix(l, name) {
			return true
		}
	}
	return false
}

// cleanCachePath reports whether w is exactly one file path under the default cache root as the
// client resolves it (`$HOME/.cache/subsystem-store`, [CacheRootName]), spelled `~/`, `/home/<u>/`
// or `/root/` + `.cache/subsystem-store/<scope>/<name>[/<name>…]`: a [ValidScope] scope, at least
// one component after it (a file, not the scope directory), and every component a [plainSegment].
// Exact bytes, case included — a spelling it does not match is not provable, never "probably it".
func cleanCachePath(w string) bool {
	var rest string
	switch {
	case strings.HasPrefix(w, "~/"):
		rest = w[2:]
	case strings.HasPrefix(w, "/root/"):
		rest = w[len("/root/"):]
	case strings.HasPrefix(w, "/home/"):
		u, r, ok := strings.Cut(w[len("/home/"):], "/")
		if !ok || !plainSegment(u) {
			return false
		}
		rest = r
	default:
		return false
	}
	rest, ok := strings.CutPrefix(rest, ".cache/"+CacheRootName+"/")
	if !ok {
		return false
	}
	sc, rest, ok := strings.Cut(rest, "/")
	if !ok || !ValidScope(sc) {
		return false
	}
	return eachSegment(rest, plainSegment)
}

// safeDir reports whether a working directory w is an absolute path the cache cannot be reached
// from by a relative word: every component a [plainSegment] (no `.`, `..` or empty component, no
// metacharacter, no trailing `/`), no component naming `cache` (so it is not a cache root's
// parent, the root, or under it — every cache root F2 recognises sits under one, [rootContext]),
// and not an ancestor of a home directory a cache path can be spelled under — `/`, `/home`,
// `/home/<u>`, `/root`, `/Users`, `/Users/<u>`.
func safeDir(w string) bool {
	if len(w) < 2 || w[0] != '/' {
		return false
	}
	n, first := 0, ""
	ok := eachSegment(w[1:], func(seg string) bool {
		if n == 0 {
			first = seg
		}
		n++
		return plainSegment(seg) && !strings.Contains(asciiLower(seg), "cache")
	})
	if !ok {
		return false
	}
	switch first {
	case "root":
		return n > 1
	case "home", "Users":
		return n > 2
	}
	return true
}

// eachSegment reports whether f holds for every `/`-separated component of p (an empty p is one
// empty component).
func eachSegment(p string, f func(string) bool) bool {
	for {
		seg, rest, more := strings.Cut(p, "/")
		if !f(seg) {
			return false
		}
		if !more {
			return true
		}
		p = rest
	}
}

// plainSegment is one path component of plain name bytes `[A-Za-z0-9._@+-]`, not empty and not
// `.` or `..`. A hidden name (`.notes`) passes: it is a file, not a step out of the directory.
func plainSegment(seg string) bool {
	if seg == "" || seg == "." || seg == ".." {
		return false
	}
	for i := 0; i < len(seg); i++ {
		if c := seg[i]; !isAlnum(c) && strings.IndexByte("._@+-", c) < 0 {
			return false
		}
	}
	return true
}

// cacheFlag reports whether any (lowered) string of one input holds the client's `--cache` flag as
// a word of its own: `--cache` or `--cache=…`, quoted or not.
func cacheFlag(lowers []string) bool {
	const flag = "--cache"
	for _, lower := range lowers {
		for off := 0; ; {
			j := strings.Index(lower[off:], flag)
			if j < 0 {
				break
			}
			i := off + j
			off = i + len(flag)
			before := i == 0 || isShellSeparator(lower[i-1]) || strings.IndexByte(`"'\`, lower[i-1]) >= 0
			after := off == len(lower) || isShellSeparator(lower[off]) || strings.IndexByte(`="'`, lower[off]) >= 0
			if before && after {
				return true
			}
		}
	}
	return false
}

// movesIntoUnseen reports whether any shell-line string of one input ([role]; a bash
// `description` is not one) runs `cd` or `pushd` with an argument this cannot show stays out of
// the cache ([cdArgUnseen]). In Claude Code the Bash working directory PERSISTS between calls, so
// `cd ~/.cache`, then `cd subsystem-store`, then `cd alder`, then `cat x.md` reads a scope no call
// names — and each `cd` alone names no cache path. So F2 asks of every input, named read or not.
func movesIntoUnseen(ss []string, roles []role) bool {
	for k, s := range ss {
		if (roles == nil || roles[k] == shellRole) && changesDirUnseen(s) {
			return true
		}
	}
	return false
}

// cdPrefixes are command-position words after which the next word is still a command's first:
// `builtin cd`, `command cd`, `{ cd`, `if cd`, `! cd` …
var cdPrefixes = map[string]bool{
	"builtin": true, "command": true, "{": true, "!": true, "if": true, "then": true, "else": true,
	"elif": true, "do": true, "while": true, "until": true, "time": true,
}

// changesDirUnseen is [movesIntoUnseen] for one shell line, one pass. A rough lexer: words are
// split by unquoted spaces and tabs; an unquoted newline, `;`, `|`, `&`, `(`, `)` or backtick
// ends a command (so `$(cd …)` and `(cd …)` are read too — a subshell's `cd` does not persist,
// which over-poisons); `'…'`, `"…"` and `\x` are unquoted. A command's first word, past any
// [cdPrefixes] and `NAME=value` assignments, that is `cd` or `pushd` (by its unquoted text) has
// its first argument checked by [cdArgUnseen]; one with no argument is `$HOME`, so unseen.
func changesDirUnseen(s string) bool {
	const (
		atCmd = iota // the next word is a command's first
		atArg        // the next word is a `cd`/`pushd` argument
		inCmd        // the rest of a command: skipped
	)
	state := atCmd
	var val []byte
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\r':
			i++
			continue
		case strings.IndexByte("\n;|&()`", c) >= 0:
			if state == atArg {
				return true // `cd` with no argument: the home directory
			}
			state = atCmd
			i++
			continue
		}
		// One word, unquoted into val.
		start := i
		val = val[:0]
		for i < len(s) {
			c := s[i]
			if c == ' ' || c == '\t' || c == '\r' || strings.IndexByte("\n;|&()`", c) >= 0 {
				break
			}
			switch c {
			case '\'':
				j := strings.IndexByte(s[i+1:], '\'')
				if j < 0 {
					j = len(s) - i - 1
				}
				val = append(val, s[i+1:i+1+j]...)
				i = min(i+j+2, len(s))
			case '"':
				i++
				for i < len(s) && s[i] != '"' {
					if s[i] == '\\' && i+1 < len(s) {
						i++
					}
					val = append(val, s[i])
					i++
				}
				i++
			case '\\':
				if i+1 < len(s) {
					i++
				}
				val = append(val, s[i])
				i++
			default:
				val = append(val, c)
				i++
			}
		}
		i = min(i, len(s))
		switch state {
		case atCmd:
			w := string(val)
			switch {
			case cdPrefixes[w] || isAssignment(w):
			case w == "cd" || w == "pushd":
				state = atArg
			default:
				state = inCmd
			}
		case atArg:
			if cdArgUnseen(string(val), strings.HasPrefix(s[start:], "~/")) {
				return true
			}
			state = inCmd
		}
	}
	return state == atArg
}

// isAssignment reports whether w is a `NAME=value` prefix word.
func isAssignment(w string) bool {
	name, _, ok := strings.Cut(w, "=")
	if !ok || name == "" || name[0] >= '0' && name[0] <= '9' {
		return false
	}
	for i := 0; i < len(name); i++ {
		if c := name[i]; !isAlnum(c) && c != '_' {
			return false
		}
	}
	return true
}

// cdArgUnseen reports whether v, a `cd`/`pushd` argument once unquoted, may move the working
// directory into the cache, under it, or to an ancestor of it: it names `cache` or [CacheRootName]
// (any case); or it is not ONE of a clean absolute path that [safeDir] admits, an unquoted `~/…`
// path that would be one under a home directory, or a relative path of [plainSegment]s not
// starting `-` or `+` — so `$D`, `-`, `~`, `..`, a glob, `$(…)`, a flag (`-P`), `+1` are unseen.
// One trailing `/` is allowed. (`..` is unseen on purpose: from a repo under the home directory
// it is an ancestor of the cache, as `cd ~` is.)
func cdArgUnseen(v string, tilde bool) bool {
	lv := asciiLower(v)
	if strings.Contains(lv, "cache") || strings.Contains(lv, CacheRootName) {
		return true
	}
	if v == "" || v[0] == '-' || v[0] == '+' {
		return true // `cd -` (the previous directory), a flag, a `pushd +N` stack entry
	}
	if len(v) > 1 && v[len(v)-1] == '/' {
		v = v[:len(v)-1]
	}
	switch {
	case tilde:
		return len(v) < 3 || !safeDir("/home/u/"+v[2:])
	case strings.HasPrefix(v, "/"):
		return !safeDir(v)
	}
	return !eachSegment(v, plainSegment)
}

type rootKind int

const (
	notRoot rootKind = iota
	cacheRoot
	relativeRoot
	unknownRoot
)

// rootContext classifies the occurrence of [CacheRootName] at i by what precedes it.
//
// A parent component is a cache (`cacheRoot`) when it contains `cache`; NOT one (`notRoot`) only
// when it is a plain LITERAL word — `[a-z0-9._-]+`, as a repo directory, `.config` or a kubectl
// `deploy/` is. A parent this cannot read as either — empty (`/subsystem-store`), or holding a
// shell expansion (`$C/…`, `"$C"/…`, `~/…`, `*/…`) — is `unknownRoot`, which is `*`: a spelling of
// the cache this does not recognise must never fall silently into "not a cache".
func rootContext(lower string, i int) rootKind {
	if i == 0 {
		return relativeRoot
	}
	switch c := lower[i-1]; {
	case c == '/':
		p := i - 1
		for p > 0 && lower[p-1] != '/' && !isShellSeparator(lower[p-1]) {
			p--
		}
		parent := lower[p : i-1]
		switch {
		case strings.Contains(parent, "cache"):
			return cacheRoot
		case parent == "." || parent == "..":
			return relativeRoot
		case parent != "" && strings.Trim(parent, "abcdefghijklmnopqrstuvwxyz0123456789._-") == "":
			return notRoot
		}
		return unknownRoot
	case isAlnum(c) || c == '-' || c == '_' || c == '.':
		return notRoot // inside a longer word
	}
	return relativeRoot // after whitespace, a quote, `=`, `(` …
}

// isShellSeparator ends a path token: whitespace and the shell's control and redirection bytes.
// Quotes, `$`, `{`, `*` and `\` are NOT separators, so a component holding them fails
// [ValidScope] instead of being cut short into a valid-looking prefix (`al"pha"` is `alpha` to
// the shell, and must not read as `al`).
func isShellSeparator(c byte) bool {
	return strings.IndexByte(" \t\n\r\f\v;&|<>()`", c) >= 0
}

// isPathByte is a byte that continues a file name.
func isPathByte(c byte) bool {
	return isAlnum(c) || c == '.' || c == '-' || c == '_' || c == '/'
}

func isAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// asciiLower lowers ASCII only, so every index into it is an index into s (`strings.ToLower` can
// change a string's length). F1's names and F2's are ASCII, so it finds what ToLower found.
// Like ToLower it returns s itself, allocating nothing, when s holds no upper-case byte — the
// common case, and the pod runs this over every tool input it stores.
func asciiLower(s string) string {
	first := strings.IndexFunc(s, func(r rune) bool { return r >= 'A' && r <= 'Z' })
	if first < 0 {
		return s
	}
	b := []byte(s)
	for i := first; i < len(b); i++ {
		if c := b[i]; c >= 'A' && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

// Ledger folds one session's read-ledger file (JSON Lines, decision 3a). An absent file and an
// empty one are both zero records, and that is what F1 asks.
func (d *Deriver) Ledger(data []byte) {
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		d.ledgerRecords++
		var rec struct {
			Scope *string `json:"scope"`
		}
		if err := json.Unmarshal(line, &rec); err != nil || rec.Scope == nil {
			d.ledger[Star] = true
			continue
		}
		if *rec.Scope == Star || !ValidScope(*rec.Scope) {
			d.ledger[Star] = true
			continue
		}
		d.ledger[*rec.Scope] = true
	}
	if sc.Err() != nil {
		d.ledger[Star] = true
	}
}

// LedgerUnreadable records that a ledger file EXISTS but could not be read. It fails closed: it
// counts as a record (so F1 cannot read it as "empty") naming `*`.
func (d *Deriver) LedgerUnreadable() {
	d.ledgerRecords++
	d.ledger[Star] = true
}

// Evidence is the part of a Deriver that must survive between capture runs, so a later run can
// feed only NEW records and still answer over the whole session. The ledger is not here: it is a
// file on the host and is re-read whole on every run.
type Evidence struct {
	Headers      []string `json:"headers,omitempty"`
	NamesProgram bool     `json:"names_program,omitempty"`
	NamesCache   bool     `json:"names_cache,omitempty"`
}

// Evidence returns what to persist.
func (d *Deriver) Evidence() Evidence {
	return Evidence{Headers: keys(d.headers), NamesProgram: d.namesProgram, NamesCache: d.namesCache}
}

// Restore folds persisted evidence back in. It only ADDS, like everything else here.
func (d *Deriver) Restore(e Evidence) {
	for _, h := range e.Headers {
		d.headers[h] = true
	}
	d.namesProgram = d.namesProgram || e.NamesProgram
	d.namesCache = d.namesCache || e.NamesCache
}

// Result is what one session's evidence adds to its visibility set.
type Result struct {
	Headers []string // R_header, with R_memo and F2's named scopes folded in
	Ledger  []string // L
	// F1 and F2 report whether each contributed `*` (F2's named scopes are in Headers).
	F1, F2 bool
	// V is R_header ∪ L ∪ F, sorted, with `*` present when any part contributed it.
	V []string
	// Declare is V WITHOUT F1's `*`: R_header ∪ L ∪ (F2 ? `*`). It is what an agent DECLARES to the
	// pod, which decides F1 itself, at view time, from the ledger it holds (`archive.Meta.
	// LedgerScopes`) — so F1 is the one part of `V` a declaration must not freeze (#266's audit).
	// A `*` from a header or a ledger record is still in it.
	Declare []string
}

// Result reads the accumulated evidence.
func (d *Deriver) Result() Result {
	r := Result{Headers: keys(d.headers), Ledger: keys(d.ledger)}
	r.F1 = d.namesProgram && d.ledgerRecords == 0
	r.F2 = d.namesCache
	all := map[string]bool{}
	for k := range d.headers {
		all[k] = true
	}
	for k := range d.ledger {
		all[k] = true
	}
	if r.F2 {
		all[Star] = true
	}
	r.Declare = keys(all)
	if r.F1 {
		all[Star] = true
	}
	r.V = keys(all)
	return r
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ClaudeRecord feeds one decoded Claude Code record: a `tool_use` block's `input` is a tool
// input; every other string, anywhere in the record, is content.
func (d *Deriver) ClaudeRecord(rec map[string]any) {
	msg, _ := rec["message"].(map[string]any)
	blocks, _ := msg["content"].([]any)
	inputs := map[int]bool{}
	for i, b := range blocks {
		if bm, ok := b.(map[string]any); ok && bm["type"] == "tool_use" {
			inputs[i] = true
			name, _ := bm["name"].(string)
			d.toolInput(name, bm["input"])
		}
	}
	for k, v := range rec {
		if k == "message" {
			continue
		}
		eachString(v, d.Content)
	}
	for k, v := range msg {
		if k == "content" {
			continue
		}
		eachString(v, d.Content)
	}
	if s, ok := msg["content"].(string); ok {
		d.Content(s)
	}
	for i, b := range blocks {
		if !inputs[i] {
			eachString(b, d.Content)
		} else if bm, ok := b.(map[string]any); ok {
			for k, v := range bm {
				if k != "input" {
					eachString(v, d.Content)
				}
			}
		}
	}
}

// OpencodePart feeds one decoded opencode part: a `tool` part's `state.input` is a tool input;
// everything else is content.
func (d *Deriver) OpencodePart(part map[string]any) {
	state, _ := part["state"].(map[string]any)
	if part["type"] == "tool" && state != nil {
		tool, _ := part["tool"].(string)
		d.toolInput(tool, state["input"])
		for k, v := range state {
			if k != "input" {
				eachString(v, d.Content)
			}
		}
		for k, v := range part {
			if k != "state" {
				eachString(v, d.Content)
			}
		}
		return
	}
	eachString(part, d.Content)
}

func eachString(v any, f func(string)) {
	switch t := v.(type) {
	case string:
		f(t)
	case map[string]any:
		for _, e := range t {
			eachString(e, f)
		}
	case []any:
		for _, e := range t {
			eachString(e, f)
		}
	}
}
