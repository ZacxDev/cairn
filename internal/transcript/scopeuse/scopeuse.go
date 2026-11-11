// Package scopeuse derives the READ half of a session's visibility set — every scope it read
// through cairn, as far as its transcript and its client read ledger show — and the two
// fail-closed fallbacks (`claudedocs/plan-cairn-plugins.md`, decision 3).
//
// 🔴 ONE FUNCTION, TWO CALLERS, THE SAME ANSWER. `cmd/cairn-capture` computes it for ROUTING
// (decision 16) and `cairn-ui` will re-derive it over the STORED records (S3), so an old or lying
// agent cannot shrink `V`. Neither caller resolves a working directory or parses a shell line: a
// command line is read for one substring (F1) and for cache-root PATHS (F2) — never decomposed
// into commands, and never resolved against a working directory or a variable.
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
//     A cache path it cannot attribute to ONE default-instance scope adds `*`: the bare root
//     (`S=~/.cache/subsystem-store;`, `ls ~/.cache/subsystem-store`), a component that is not a
//     valid scope (a glob, `$VAR`, `{a,b}`, `.sync-stamp`), ANOTHER instance's root
//     (`subsystem-store-<alias>` — derived scopes carry no instance), a sync staging sibling
//     (`subsystem-store.new-…`), a relative `subsystem-store/…`, a root whose parent component is
//     a spelling this cannot read (`$C/subsystem-store/…`), and any input that also holds a
//     `..` component or `cd`s into the cache. `subsystem-store` that is NOT a cache path — a
//     namespace (`-n subsystem-store`), a repo path (`apps/subsystem-store/…`), the config dir
//     (`~/.config/subsystem-store/`), a file name (`check-subsystem-store.sh`), the `.lock`
//     sibling — adds nothing ([eachCacheRead] has the exact rule).
//
// ⚠ WHAT IT CANNOT SEE (threat T3): a session mixing a ledger-writing client with one that writes
// nothing (non-empty ledger, so F1 is silent); a file read of a store copy whose path lacks
// `subsystem-store`, or lacks a `cache`-named parent component; a read through a variable or a
// working directory set in an EARLIER call (`$S/notes1/x.md` after `S=~/.cache/subsystem-store`,
// or a relative path after `cd ~/.cache/subsystem-store/notes1`) — the call that SETS a bare root
// or `cd`s into the cache is itself `*`, so such a session is still owner-only, but one that sets
// `S=~/.cache/subsystem-store/notes1` adds only `notes1`, and a later `$S/../alpha/x.md` is
// unseen; a bare relative `subsystem-store` with no `/` after it (indistinguishable from the
// namespace); a symlink into the cache; a read that bypasses the client entirely; a memo shown by
// any path that does not print the renderer's block (a memo's text quoted without its fence, or
// the browser tab).
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
// every record ([Deriver.Content]), every tool input ([Deriver.ToolInput]) and every ledger file
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

// ToolInput records one string of a tool's INPUT (a command line, a file path, an argument).
// Inputs are content too, so the header scan runs on them as well.
func (d *Deriver) ToolInput(s string) {
	d.Content(s)
	lower := asciiLower(s)
	if strings.Contains(lower, "cairn") || strings.Contains(lower, "subsystem-recall") {
		d.namesProgram = true
	}
	eachCacheRead(s, lower, func(sc string) {
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

// CacheScopes returns what F2 adds for one tool input: the scope of every cache-root READ it names,
// `*` for one it cannot attribute to a single default-instance scope. See [eachCacheRead].
func CacheScopes(s string) []string {
	var out []string
	eachCacheRead(s, asciiLower(s), func(v string) { out = append(out, v) })
	return out
}

// eachCacheRead is F2. A file read from the client cache never passes through the client and is
// never ledgered, so the PATH is the only evidence of what was read. It finds every
// `subsystem-store` (case-insensitive) in s and classifies it — a NAME, not a substring:
//
//   - NOT A CACHE PATH, adds nothing: the name is part of a longer word (`test-check-subsystem-
//     store-phase1.sh`, `subsystem-stores`); it is a path component whose parent is a plain
//     literal word not containing `cache` (`clusters/…/apps/subsystem-store/…`,
//     `~/.config/subsystem-store/`, `deploy/subsystem-store`); it is a bare word with no `/` after it (`kubectl -n
//     subsystem-store`); or it is the sync lock `subsystem-store[-alias].lock` and nothing more.
//   - A CACHE ROOT: the parent component contains `cache` — `~/.cache`, `/home/<u>/.cache`,
//     `$HOME/.cache`, `"${HOME}/.cache"`, `$XDG_CACHE_HOME`, `${XDG_CACHE_HOME:-~/.cache}`, and any
//     other spelling that ends in a component naming a cache (the client resolves only
//     `$HOME/.cache`, so the XDG spellings are caught to fail closed, not because they are real).
//     A RELATIVE `subsystem-store/…` (preceded by whitespace, punctuation, `./` or `../`) is `*`:
//     the working directory is unknown. So is one whose parent component is neither a cache nor a
//     plain literal word (`$C/subsystem-store`, `"$C"/subsystem-store/…`, `~/subsystem-store`):
//     an unrecognised spelling of the root is `*`, never "not a cache" ([rootContext]).
//   - A cache root adds `*` unless EVERYTHING below holds, and then adds the scope:
//     it is the default instance (no `-<alias>` suffix: derived scopes carry no instance, so a
//     scope read from ANOTHER instance's cache cannot be attributed — routing would send it by name
//     to whatever instance the table says); a `/` follows it; the next component, up to `/` or a
//     shell separator, passes [ValidScope] (a bare root, `*`, `$S`, `{a,b}`, a quoted or empty
//     component all fail); the input holds no `..` path component ([hasParentComponent]); and the input does not `cd`/`pushd`
//     into a path naming the cache (the working directory then persists into calls this cannot
//     connect to it). Anything else after the root name — `-*`, `.new-…`, `_old` — is `*`.
//
// LINEAR: every occurrence looks back over its parent component only (disjoint per occurrence)
// and forward over its scope component; the `..` and `cd` checks are one pass each per input.
//
// lower is asciiLower(s), passed in so the one copy [Deriver.ToolInput] makes serves F1 and F2.
func eachCacheRead(s, lower string, emit func(string)) {
	if !strings.Contains(lower, CacheRootName) {
		return
	}
	poisoned := -1 // lazily: 1 if the input holds `..` or a cd into the cache, else 0
	isPoisoned := func() bool {
		if poisoned < 0 {
			poisoned = 0
			if hasParentComponent(s) || cdsIntoCache(lower) {
				poisoned = 1
			}
		}
		return poisoned == 1
	}
	for off := 0; ; {
		j := strings.Index(lower[off:], CacheRootName)
		if j < 0 {
			return
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
		if alias || next != '/' || isPoisoned() {
			emit(Star)
			continue
		}
		start := end + 1
		stop := start
		for stop < len(s) && s[stop] != '/' && !isShellSeparator(s[stop]) {
			stop++
		}
		if sc := s[start:stop]; ValidScope(sc) {
			emit(sc)
		} else {
			emit(Star)
		}
	}
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

var cdIntoCache = regexp.MustCompile(`(?:^|[\s;&|(])(?:cd|pushd)(?:\s+-\S*)*\s+\S*` + CacheRootName)

// hasParentComponent reports whether s holds a `..` path component: `..` with a path boundary on
// both sides (`/`, a shell separator, a quote, `{`, `,`, `=` or the end), so `a/../b`, `$S/..`
// and `".."` count and a git range `main..HEAD` does not.
func hasParentComponent(s string) bool {
	boundary := func(c byte) bool { return c == '/' || isShellSeparator(c) || strings.IndexByte(`"'{},=`, c) >= 0 }
	for off := 0; ; {
		j := strings.Index(s[off:], "..")
		if j < 0 {
			return false
		}
		i := off + j
		off = i + 1
		if (i == 0 || boundary(s[i-1])) && (i+2 == len(s) || boundary(s[i+2])) {
			return true
		}
	}
}

// cdsIntoCache reports whether the (lowered) input changes directory to a path naming the cache.
func cdsIntoCache(lower string) bool { return cdIntoCache.MatchString(lower) }

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
			eachString(bm["input"], d.ToolInput)
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
		eachString(state["input"], d.ToolInput)
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
