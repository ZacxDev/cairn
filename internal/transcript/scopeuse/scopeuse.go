// Package scopeuse derives the READ half of a session's visibility set — every scope it read
// through cairn, as far as its transcript and its client read ledger show — and the two
// fail-closed fallbacks (`claudedocs/plan-cairn-plugins.md`, decision 3).
//
// 🔴 ONE FUNCTION, TWO CALLERS, THE SAME ANSWER. `cmd/cairn-capture` computes it for ROUTING
// (decision 16) and `cairn-ui` will re-derive it over the STORED records (S3), so an old or lying
// agent cannot shrink `V`. Nothing here resolves a working directory or a variable, or runs a
// shell: a command line is read for one substring (F1), and for cache-root PATHS checked against a
// small allowlist grammar that can only replace F2's `*` with the scopes those paths name — never
// for what a command it does not allow would do.
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
//   - F2 (named scopes folded into R_header). 🔴 NEVER NARROWER THAN THE RULE IT REPLACED, BY
//     CONSTRUCTION ([eachCacheRead]). Before #314 F2 was one predicate: a tool input any string of
//     which contains `subsystem-store` (the client cache root's directory name, any case) → `*`.
//     That predicate still decides first, unchanged. An input that does not satisfy it adds
//     NOTHING to F2 — there is no other F2 rule. One that does adds `*`, with ONE exception, which
//     exists because a file read from the cache never passes through the client and is never
//     ledgered, so the path is the only record of it: when the WHOLE input passes an allowlist
//     grammar AND every occurrence of `subsystem-store` in it lies inside a clean cache path that
//     grammar accepted, it adds exactly those paths' scopes instead, and no `*` ([provenScopes]).
//     The grammar reads each top-level string of the input by its key: a bash tool's
//     `description` is skipped (so a mention there is never inside an accepted path); a
//     `workdir`/`path`/`cwd`/`dir`/`directory` must be a clean absolute path with no component
//     naming `cache` that is not `/`, `/home`, `/home/<u>`, `/root`, `/Users` or `/Users/<u>`
//     ([safeDir]); a `file_path`/`filePath` must be one clean cache path; every other string is a
//     shell line ([shellLineProvable]) that lexes with spaces, tabs, `|`, `||`, `&&`, `;` and
//     newlines only (no other metacharacter outside single quotes; no `/` or `..` inside them),
//     runs only `cat`/`cut`/`echo`/`grep`/`head`/`ls`/`nl`/`printf`/`tail`/`tr`/`uniq`/`wc`, gives
//     each only flags on that command's own allowlist ([commandFlags]; for `grep`: `-n -c -l -L -i
//     -v -w -x -o -h -H -E -F -s -q` and clusters of them, `-e PAT`, `-m`/`-A`/`-B`/`-C` with a
//     SEPARATE all-digit word, and `--count --line-number --ignore-case --invert-match
//     --word-regexp --line-regexp --only-matching --no-filename --with-filename --extended-regexp
//     --fixed-strings --files-with-matches --files-without-match --quiet --silent --no-messages`;
//     no recursion for any command, no `wc --files0-from`; an unknown flag fails), has no `.`/`..`
//     word and no other word naming `cache`, and has every word holding `/` be one unquoted
//     `~/`-, `/home/<u>/`- or `/root/`-`.cache/subsystem-store/<scope>/<file>` path with a
//     [ValidScope] scope and plain components ([cleanCachePath]). So `cat`, `grep -n` or `head`
//     of named cache files stays named, and EVERYTHING else naming `subsystem-store` is `*` as it
//     was before #314: a namespace (`-n subsystem-store`), a repo path, `~/.config/subsystem-store/`,
//     a longer word, the `.lock` sibling, another instance's `subsystem-store-<alias>`, the bare
//     root, a `$HOME` spelling, a glob, a redirect, `sed`, `cd`, `find`, `tar`, an unknown flag.
//
// ⚠ WHAT IT CANNOT SEE (threat T3): a session mixing a ledger-writing client with one that writes
// nothing (non-empty ledger, so F1 is silent); and every read of the cache through tool inputs
// none of which contains `subsystem-store` — the bare-substring rule's own blind spots, kept as
// they were: a store copy at a path without the name (a `--cache <dir>` root, a mirror) read by
// file tools; a relative spelling that never writes the name (`grep -r x .cache` from the home
// directory, `cat */a.md` from inside the root); a working directory moved into the cache by
// calls that never write the name (`cd ~/.cache`, then `cd s*`), and a session that starts inside
// it; a read through a variable, function or alias set in an earlier call; a symlink into the
// cache; a read that bypasses the client entirely. The named-scope exception adds one gap the
// bare-substring rule did not have: a named read beside a RELATIVE file word
// (`cat ~/.cache/subsystem-store/<scope>/a.md x.md`) derives `<scope>`, but `x.md` resolves
// against a working directory the input does not fix — harmless unless an earlier call moved it
// into the cache by a spelling without the name (one WITH it is `*` for the session). Not
// established here: whether the host's `grep` (Claude Code's embedded ugrep) recurses when it is
// given NO file word and its stdin is a terminal. Also unseen: a memo shown by any path that does
// not print the renderer's block (a memo's text quoted without its fence, or the browser tab).
package scopeuse

import (
	"bufio"
	"bytes"
	"encoding/json"
	"regexp"
	"slices"
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
	ss, roles := inputStrings(tool, v)
	d.toolInputStrings(ss, roles)
}

// inputStrings is every string of one tool input — the same strings [eachString] collects — each
// with its [role].
func inputStrings(tool string, v any) ([]string, []role) {
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
	return ss, roles
}

// ToolInput records one tool input that IS one string (a raw command line). It is the
// one-string unit of [Deriver.ToolInputStrings].
func (d *Deriver) ToolInput(s string) { d.ToolInputStrings([]string{s}) }

// ToolInputStrings records ONE tool input given as all its strings (a command line, a file path,
// a working directory, an argument), every one of them read as a shell line ([role]). Inputs are
// content too, so the header scan runs on each string; F1 asks of each string; F2 decides over
// the strings TOGETHER ([eachCacheRead]).
func (d *Deriver) ToolInputStrings(ss []string) { d.toolInputStrings(ss, nil) }

// role is what F2's grammar ([provenScopes]) reads one string of a tool input as.
type role int

const (
	// shellRole: a shell line, held to [shellLineProvable]. Every string whose key is not below.
	shellRole role = iota
	// freeRole: a bash tool's `description` — free text that is never run, so the grammar skips it
	// (it is still content, and a mention of the cache root in it makes the input `*`).
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

// CacheScopes returns what F2 adds for ONE tool input given as its strings, every one read as a
// shell line: nothing, `[*]`, or the named scopes of its clean cache paths. See [eachCacheRead].
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
// string is ([role]; nil reads every string as a shell line).
//
// 🔴 NEVER NARROWER THAN THE BARE-SUBSTRING RULE, BY CONSTRUCTION. That rule (F2 before #314) was
// [namesCacheRoot] → `*`, and it is still the first test here, unchanged:
//
//   - no string of the input contains `subsystem-store` (any case) → F2 adds NOTHING. No other F2
//     rule exists, so on such an input F2 is exactly what it was;
//   - some string does → `*`, UNLESS [provenScopes] proves the whole input reads only files of
//     named scopes — then exactly those scopes, and no `*`.
//
// So the one way this differs from the bare-substring rule is a `*` replaced by named scopes, and
// only for an input the allowlist grammar accepts. `TestF2NeverNarrowerThanMain` pins it.
//
// LINEAR: [namesCacheRoot] and [provenScopes] are each one pass over every string.
func eachCacheRead(ss, lowers []string, roles []role, emit func(string)) {
	if !namesCacheRoot(lowers) {
		return
	}
	named, ok := provenScopes(ss, lowers, roles)
	if !ok {
		emit(Star)
		return
	}
	for _, sc := range named {
		emit(sc)
	}
}

// namesCacheRoot is the bare-substring rule: some (asciiLower'd) string of the input contains
// [CacheRootName]. (It lowered with `strings.ToLower`; for this ASCII name the two find the same
// occurrences, since no non-ASCII rune lowers to any of its bytes.)
func namesCacheRoot(lowers []string) bool {
	for _, l := range lowers {
		if strings.Contains(l, CacheRootName) {
			return true
		}
	}
	return false
}

// provenScopes reports whether one tool input, every string of it by its role, passes F2's
// ALLOWLIST grammar AND every occurrence of [CacheRootName] (any case) in it lies inside a clean
// cache file path that grammar accepted — and if so, the scopes of those paths:
//
//   - a bash tool's `description` is not read by the grammar, so a mention of the name there is
//     never inside an accepted path: `*`;
//   - a working directory passes [safeDir] — never a cache path, so one naming the name is `*`;
//   - a file path is one [cleanCachePath];
//   - every other string passes [shellLineProvable].
//
// 🔴 THE OCCURRENCE COUNT IS THE RULE, NOT A SECOND FENCE. The grammar accepts plenty of words that
// hold the name without being cache paths (`kubectl`'s namespace is not one, but `echo
// subsystem-store` lexes fine); what makes them `*` is that the count of occurrences in the input
// exceeds the count inside accepted paths. A namespace, a repo path, `~/.config/subsystem-store`, a
// longer word, the `.lock` sibling, another instance's `subsystem-store-<alias>`: each is an
// occurrence no accepted path covers. ([CacheRootName] cannot overlap itself, so
// `strings.Count` counts every occurrence.)
//
// 🔴 IT IS AN ALLOWLIST, NOT A LIST OF HAZARDS. Two audit rounds each found shell spellings that
// slipped past the denylist this replaced; here a spelling this does not recognise FAILS, so the
// next one is over-poisoned (`*`, as before #314), not missed.
func provenScopes(ss, lowers []string, roles []role) ([]string, bool) {
	var named []string
	total, covered := 0, 0
	accept := func(p string) bool {
		sc, ok := cleanCachePath(p)
		if ok {
			named = append(named, sc)
			covered += strings.Count(asciiLower(p), CacheRootName)
		}
		return ok
	}
	for k, s := range ss {
		total += strings.Count(lowers[k], CacheRootName)
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
			ok = accept(s)
		default:
			ok = shellLineProvable(s, accept)
		}
		if !ok {
			return nil, false
		}
	}
	return named, covered == total
}

// flagSet is one allowed command's FLAG ALLOWLIST. Any word that starts `-` once unquoted (`-`
// alone and `'-r'` included), and is not a listed flag's argument, is a flag, and one it does not
// list fails; a long flag must match exactly, so an
// abbreviation getopt_long would expand (`--rec`) fails too.
type flagSet struct {
	bare   string   // short flags taking no argument; a cluster of only these passes (`-in`)
	arg    string   // short flags taking an argument: alone, then the NEXT word (`-e PAT`), or …
	attach bool     // … attached to the letter (`-c1-180`); false: attached fails (`-m1`)
	num    string   // of arg, the flags whose separate argument must be all digits (`-m 5`)
	digits bool     // `-NUM` alone passes (`head -20`)
	long   []string // long flags taking no argument
	vals   []string // long flags taking `=VALUE` (`--lines=5`)
}

// commandFlags are the only commands a provable shell line may run, with each one's flags. Each
// command reads the files its words name (or stdin) and nothing else; none runs another program,
// changes the working directory, or takes a path it derives from its input. Not here, so `*`:
// `cd`/`pushd`/`popd`, `xargs`, `find`, `sed` (`e`, `r`, `w`), `awk` (`system`), `sort`
// (`--compress-program`), any shell or interpreter. And no flag here reads beyond the words:
// no recursion (`grep -r/-R/-d/--recursive`, `ls -R/--recursive`), no name list (`wc
// --files0-from`, `grep -f`), no ugrep-only mode — 🔴 Claude Code's `grep` is a shell function
// running its embedded ugrep, where an attached digit RECURSES (`grep -n2 x sub` finds
// `sub/deep/f.txt`; GNU reads it as `-C 2`), so grep's `-m`/`-A`/`-B`/`-C` pass only with a
// separate all-digit word, and every flag not listed fails, `--index` and `--depth` included.
var commandFlags = map[string]flagSet{
	"cat": {bare: "AbeEnstTuv", long: []string{"--show-all", "--number-nonblank", "--show-ends",
		"--number", "--squeeze-blank", "--show-tabs", "--show-nonprinting"}},
	"cut":  {bare: "sz", arg: "bcdf", attach: true, long: []string{"--complement", "--only-delimited"}},
	"echo": {bare: "neE"},
	"grep": {bare: "nclLivwxohHEFsq", arg: "emABC", num: "mABC", long: []string{"--count",
		"--line-number", "--ignore-case", "--invert-match", "--word-regexp", "--line-regexp",
		"--only-matching", "--no-filename", "--with-filename", "--extended-regexp", "--fixed-strings",
		"--files-with-matches", "--files-without-match", "--quiet", "--silent", "--no-messages"}},
	"head":   {bare: "qv", arg: "nc", attach: true, digits: true, long: []string{"--quiet", "--silent", "--verbose"}, vals: []string{"--lines", "--bytes"}},
	"ls":     {bare: "1aAdFhilnrSt", long: []string{"--all", "--almost-all", "--directory", "--human-readable", "--reverse"}},
	"nl":     {arg: "bnsvw", attach: true},
	"printf": {},
	"tail":   {bare: "qv", arg: "nc", attach: true, digits: true, long: []string{"--quiet", "--silent", "--verbose"}, vals: []string{"--lines", "--bytes"}},
	"tr":     {bare: "cdsCt", long: []string{"--complement", "--delete", "--squeeze-repeats", "--truncate-set1"}},
	"uniq":   {bare: "cdDiuz", arg: "fsw", attach: true, long: []string{"--count", "--repeated", "--ignore-case", "--unique"}},
	"wc":     {bare: "lwcmL", long: []string{"--lines", "--words", "--bytes", "--chars", "--max-line-length"}},
}

// argKind is what the word after a flag must be.
type argKind int

const (
	noArg  argKind = iota
	anyArg         // any word the line's other rules accept
	numArg         // all digits
)

// flag reports whether w (unquoted, starting `-`) is on f's allowlist, and what the next word
// must be.
func (f flagSet) flag(w string) (argKind, bool) {
	if len(w) < 2 {
		return noArg, false
	}
	if w[1] == '-' {
		name, _, val := strings.Cut(w, "=")
		if val {
			return noArg, slices.Contains(f.vals, name)
		}
		return noArg, slices.Contains(f.long, name)
	}
	if strings.IndexByte(f.arg, w[1]) >= 0 {
		switch {
		case len(w) > 2:
			return noArg, f.attach
		case strings.IndexByte(f.num, w[1]) >= 0:
			return numArg, true
		}
		return anyArg, true
	}
	if f.digits && allDigits(w[1:]) {
		return noArg, true
	}
	for i := 1; i < len(w); i++ {
		if strings.IndexByte(f.bare, w[i]) < 0 {
			return noArg, false
		}
	}
	return noArg, true
}

func allDigits(s string) bool {
	return s != "" && strings.Trim(s, "0123456789") == ""
}

// shellLineProvable is the grammar for one shell line, read by a strict lexer in one pass;
// accept is called with every word holding `/` and must accept it:
//
//   - Words are separated by spaces and tabs; `|`, `||`, `&&`, `;` and a newline separate
//     commands. ANY other shell metacharacter outside single quotes — `$` a backtick `\ " { } * ?
//     [ ] ( ) < > & ~ ! # ^`, and any other control byte — fails; `~` is allowed only as the
//     leading `~/` of a word.
//   - A single-quoted segment is literal text, but one holding `/` or `..` fails (it could be a
//     path), and an unterminated quote fails.
//   - The first word of every command must be one of [commandFlags], by its unquoted text, and
//     every word of it starting `-` must be on that command's allowlist ([flagSet.flag]); a flag's
//     argument word follows it (and must be all digits where the flag says so). A flag left
//     waiting for its argument at the end of a command fails.
//   - Every word holding `/` must be one that accept takes ([cleanCachePath]), unquoted.
//   - A word that is `.` or `..` once unquoted fails (with `/` it has already failed above).
//   - A word that, once unquoted, holds `cache` (any case) fails: with no `/` it is RELATIVE
//     (`.cache`, `--cached`), and from the home directory it is the cache's parent.
//
// ⚠ A word with no `/` that passes is read RELATIVE to a working directory this does not know —
// T3 (the package doc), not something this proves.
func shellLineProvable(s string, accept func(string) bool) bool {
	cmd := true                // the next word is a command's first
	var flags flagSet          // the current command's flag allowlist
	want := noArg              // what the next word must be: a flag's argument, or nothing
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
		switch {
		case strings.IndexByte(raw, '/') >= 0:
			ok = !quoted && accept(raw)
		case v == "." || v == "..":
			ok = false
		case strings.Contains(asciiLower(v), "cache"):
			ok = false // a cache-named word that is not a clean cache path: relative, so `*`
		}
		switch {
		case cmd:
			var known bool
			flags, known = commandFlags[v]
			ok = ok && known
			cmd = false
		case want == numArg:
			ok = ok && allDigits(v)
			want = noArg
		case want == anyArg:
			want = noArg
		case strings.HasPrefix(v, "-"):
			var fok bool
			want, fok = flags.flag(v)
			ok = ok && fok
		}
		start, quoted, val = -1, false, val[:0]
		return ok
	}
	endCmd := func(i int) bool {
		ok := endWord(i) && want == noArg
		cmd = true
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
			if !endCmd(i) {
				return false
			}
			if c == '|' && i+1 < len(s) && s[i+1] == '|' {
				i++
			}
		case c == '&':
			if i+1 >= len(s) || s[i+1] != '&' || !endCmd(i) {
				return false
			}
			i++
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
	return endCmd(len(s))
}

// cleanCachePath reports whether w is exactly one file path under the default cache root as the
// client resolves it (`$HOME/.cache/subsystem-store`, [CacheRootName]), spelled `~/`, `/home/<u>/`
// or `/root/` + `.cache/subsystem-store/<scope>/<name>[/<name>…]`: a [ValidScope] scope, at least
// one component after it (a file, not the scope directory), and every component a [plainSegment].
// It returns the scope. Exact bytes, case included — a spelling it does not match is not provable,
// never "probably it". (So another instance's root, the bare root, a scope directory, a `$HOME`
// or `.CACHE` spelling are not clean.)
func cleanCachePath(w string) (string, bool) {
	var rest string
	switch {
	case strings.HasPrefix(w, "~/"):
		rest = w[2:]
	case strings.HasPrefix(w, "/root/"):
		rest = w[len("/root/"):]
	case strings.HasPrefix(w, "/home/"):
		u, r, ok := strings.Cut(w[len("/home/"):], "/")
		if !ok || !plainSegment(u) {
			return "", false
		}
		rest = r
	default:
		return "", false
	}
	rest, ok := strings.CutPrefix(rest, ".cache/"+CacheRootName+"/")
	if !ok {
		return "", false
	}
	sc, rest, ok := strings.Cut(rest, "/")
	if !ok || !ValidScope(sc) || !eachSegment(rest, plainSegment) {
		return "", false
	}
	return sc, true
}

// safeDir reports whether a working directory w is an absolute path the cache cannot be reached
// from by a relative word: every component a [plainSegment] (no `.`, `..` or empty component, no
// metacharacter, no trailing `/`), no component naming `cache` (so it is not the cache root's
// parent, the root, or under it — every clean cache path sits under a `.cache`), and not an
// ancestor of a home directory a cache path can be spelled under — `/`, `/home`, `/home/<u>`,
// `/root`, `/Users`, `/Users/<u>`.
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
