package client

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/ZacxDev/cairn/internal/pytext"
)

// 🔴 THIS IS THE ONLY THING IN THE READ PATH THAT LOOKS OUTSIDE THE STORE, and it reads
// exactly one file. It runs no git, no subprocess and no network.

// HandoffGlobs is the resolution order: lowercase family first, caps family second, newest
// within each.
//
// ⚠ THAT IS A NARROWER GUARANTEE THAN IT LOOKS. This takes no topic, so a resume run given a
// topic slug reconciles one initiative while recalling against the NEWEST doc, which is a
// different initiative exactly when it matters. Aligning the two needs a topic parameter and
// a decision about what a miss means; it is NOT done here, and this comment states what the
// code does rather than what would be nice.
var HandoffGlobs = []string{"claudedocs/handoff-*.md", "claudedocs/*HANDOFF*.md"}

// backticked is the span extractor. 🔴 PATH-SHAPED TOKENS COME FROM BACKTICKED SPANS ONLY.
// Handoff docs are prose and the convention is that a real path is code-quoted; harvesting
// bare prose would mint tokens out of ordinary English. The failure direction matters: a
// missed path costs the fallback, a fabricated one costs a wrong featured entry with a basis
// that claims it was quoted.
var backticked = regexp.MustCompile("`([^`\n]+)`")

// pathTokenStrip is `_PATH_TOKEN_STRIP`, character for character.
const pathTokenStrip = "`,;:()[]{}<>\"'*_"

// FocusWindow is repo-relative paths standing in for "what is being worked on", plus where
// they came from. `Source` is empty exactly when `Paths` is, and the renderer says which of
// the two selectors fired either way — an unattributed pick is the failure this type exists
// to prevent.
type FocusWindow struct {
	Paths  []string
	Source string
}

// isRepoRelative is a SUPERSET of the resolver's own rejections, plus shape.
//
// Kept a hair STRICTER than `store.ValidateRepoRelativePath` on purpose: anything this lets
// through is handed to `store.AssociatePaths`, which REFUSES a path it considers malformed. A
// reader that failed because a handoff doc quoted a URL would be a resume step that breaks on
// ordinary prose.
func isRepoRelative(token string) bool {
	if token == "" || !strings.Contains(token, "/") {
		return false
	}
	for _, r := range token {
		// `str.isspace()`'s own predicate, not Go's: `pytext.IsSpace` is true for
		// U+001C..U+001F where `unicode.IsSpace` is not, and this is a rejection filter, so
		// the WIDER predicate is the safe one.
		if pytext.IsSpace(r) {
			return false
		}
	}
	if strings.ContainsRune("/~-", rune(token[0])) { // absolute, home-relative, a CLI flag
		return false
	}
	if strings.Contains(token, "$") || strings.Contains(token, "://") ||
		strings.Contains(token, "@") { // a var, a URL, a host
		return false
	}
	for _, part := range strings.Split(token, "/") {
		if part == ".." {
			return false
		}
	}
	return true
}

// FocusPathsFromText is the repo-relative path tokens quoted in `text`, deduped, in order of
// first use.
func FocusPathsFromText(text string) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, match := range backticked.FindAllStringSubmatch(text, -1) {
		for _, raw := range strings.Fields(match[1]) {
			token := strings.Trim(raw, pathTokenStrip)
			token = strings.TrimRight(token, ".")
			token = strings.TrimRight(token, "/")
			if !isRepoRelative(token) {
				continue
			}
			if _, dup := seen[token]; dup {
				continue
			}
			seen[token] = struct{}{}
			out = append(out, token)
		}
	}
	return out
}

// ⚠ `strings.Fields` SPLITS ON GO'S `unicode.IsSpace`, WHERE PYTHON'S `str.split()` SPLITS ON
// `str.isspace()`. The two differ at U+001C..U+001F, so a backticked span containing one of
// those four would yield one token here and two there. Named rather than hand-rolled: those
// code points cannot appear in a markdown handoff doc anybody wrote, and `isRepoRelative`
// rejects a token containing one anyway — so the difference is bounded to "one rejected token
// instead of two", which changes nothing observable.
var _ = unicode.IsSpace

// Focus is the repo's newest handoff doc, as a path window. READ-ONLY, one file read.
//
// An absent or unreadable doc is an ORDINARY outcome — most repos have no handoff at the
// moment they are resumed — and returns an empty window rather than an error: the caller's
// fallback is a real answer, not a degraded one.
//
// 🔴 THE REPO PATH IS AN ANCHOR, NOT PART OF THE PATTERN — AND THAT IS A FIX, NOT A REFACTOR.
// This was `filepath.Glob(filepath.Join(repo, pattern))`, which put the caller's own `--repo`
// value inside a glob: a repo under a directory called `wid[get` made `filepath.Match` return
// `ErrBadPattern`, the error was discarded, `matches` was nil, and this function answered
// `FocusWindow{}` — which the caller renders as *"most-recent fallback … (no handoff doc to
// read a path window from)"* while the doc is sitting in `claudedocs/`. A FALSE CLAIM OF
// ABSENCE on the DEFAULT path of `cairn recall --repo <path>`, and the failure the type's own
// doc comment says it exists to prevent. The oracle never had it: `Path(repo).glob(pattern)`
// treats its anchor literally. `anchoredNames` is the one mechanism; see `anchor.go`.
//
// 🔴 THE PATTERN'S DIRECTORY PREFIX IS **JOINED**, NOT ENUMERATED, AND THE DIFFERENCE IS
// OBSERVABLE. `filepath.Glob` splits its argument at the LAST separator and only reads the
// directory it ends up with, so `<repo>/claudedocs/handoff-*.md` never lists `<repo>` — it
// lists `<repo>/claudedocs`. A repo that is SEARCHABLE but not READABLE (mode `--x`) therefore
// resolved fine before this change, and still does. Enumerating `<repo>` to find `claudedocs`
// would find nothing there — a silent narrowing, in the same "empty result" shape as the defect
// this function fixed. `TestFocusDoesNotREADADirectoryTheGlobOnlyDESCENDSTHROUGH` is the row.
//
// 🔴 THAT CHOICE HAS **NO PARITY JUSTIFICATION**, AND EARLIER FORMS OF THIS COMMENT INVENTED
// ONE TWICE — EACH TIME BY NAMING A CPython INTERNAL, EACH TIME WRONGLY. The first said the
// `--x` case *"resolves fine on the oracle, whose `_PreciseSelector` asks `is_dir()` rather
// than scandir'ing the parent"*; that selector does not exist in the pinned `pathlib`. The
// second said that at mode `0111` *"`os.listdir` raises `PermissionError`, `focus_window`'s
// own `except OSError` swallows it"*; also false, see below.
//
// 🔴 SO THIS COMMENT NOW MAKES ONE CLAIM, AND IT IS BEHAVIOURAL: **FOR A REPO AT MODE `0111`,
// THIS FUNCTION RETURNS THE DOC AND THE ORACLE RETURNS AN EMPTY WINDOW.** MEASURED end to end
// on one synthetic fixture, same repo, mode flipped between the two reads: readable → both
// sides `claudedocs/handoff-demo.md`; at `0111` → `Focus` →
// `Source="claudedocs/handoff-demo.md"`, `focus_window` → `FocusWindow(paths=(), source=None)`.
// It is CPython's own globbing that produces the empty side: `Path(repo).glob(pattern)` returns
// `[]` and does **not** raise. That sentence is version-independent; a selector name is not,
// which is why every wording of this paragraph that named a mechanism has been wrong.
//
// 🔴 A CONSEQUENCE THE PREVIOUS WORDING GOT BACKWARDS: `focus_window`'s OWN `except OSError`
// ARM IS **NOT** WHAT TURNS THIS INTO AN ORDINARY EMPTY ANSWER — IT NEVER EXECUTES IN THIS
// SCENARIO. Measured with an `os.scandir`/`os.listdir` spy on the pinned interpreter
// (`flake.nix` → `python312`, **3.12.14**), not read: at `0111`, `os.listdir` is called **0**
// times and `Path.glob` raises nothing; control, the same fixture readable, finds the doc. So
// an auditor asking whether that arm is dead code gets no evidence from here, and anyone
// trying to close `tests/parity/README.md` residual 10 by adjusting it would change nothing.
// Do not restate a mechanism you have not measured yourself; if you keep one, pin the
// interpreter version beside it and say it was measured.
//
// ⚠ SO THE `--x` REPO IS A REAL DIVERGENCE, NOW DECLARED RATHER THAN ASSERTED AWAY —
// `tests/parity/README.md` residual **10**, which also records that the parity harness
// cannot build such a world. The Go behaviour is kept because it is the NON-NARROWING
// direction and because it is what `filepath.Glob` did here before the fix: the same "answer
// the doc rather than claim absence" direction this function exists to protect. It is NOT
// kept because the oracle agrees. It does not.
//
// ⚠ THAT JOIN IS CORRECT ONLY WHILE THE PREFIX CARRIES NO METACHARACTER, AND THE PRECONDITION
// IS PINNED RATHER THAN ASSUMED: `TestHandoffGlobsKeepTheLiteralDIRECTORYPrefixThatFocusJOINS`
// goes red if a pattern with a `*`, `?`, `[` or `\` before its last `/` is added to
// `HandoffGlobs`, because this loop would then treat it literally and quietly match nothing.
func Focus(repo string) FocusWindow {
	var doc string
	var docInfo os.FileInfo
	for _, pattern := range HandoffGlobs {
		dir, base := path.Split(pattern)
		anchor := filepath.Join(repo, filepath.FromSlash(dir))
		type candidate struct {
			path string
			info os.FileInfo
		}
		var found []candidate
		for _, name := range anchoredNames(anchor, func(name string) bool {
			// A `Match` error cannot happen for `HandoffGlobs`' members and is still
			// checked: `base` comes from a pattern this package wrote, but "no match" is
			// the answer `Glob` gave for an ill-formed one too.
			ok, err := filepath.Match(base, name)
			return err == nil && ok
		}) {
			m := filepath.Join(anchor, name)
			info, err := os.Stat(m)
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			found = append(found, candidate{m, info})
		}
		if len(found) == 0 {
			continue
		}
		// mtime, then name: two docs written in the same second must still resolve
		// identically on every run. `max` over `(mtime, name)` on the oracle, so the LAST
		// of an ascending sort is the same element.
		sort.SliceStable(found, func(i, j int) bool {
			ai, aj := found[i].info.ModTime(), found[j].info.ModTime()
			if !ai.Equal(aj) {
				return ai.Before(aj)
			}
			return filepath.Base(found[i].path) < filepath.Base(found[j].path)
		})
		doc, docInfo = found[len(found)-1].path, found[len(found)-1].info
		break
	}
	if doc == "" || docInfo == nil {
		return FocusWindow{}
	}
	data, err := os.ReadFile(doc)
	if err != nil {
		return FocusWindow{}
	}
	rel, err := filepath.Rel(repo, doc)
	if err != nil {
		return FocusWindow{}
	}
	rel = filepath.ToSlash(rel)
	// `errors="replace"`: a handoff doc with an invalid byte is still a handoff doc, and
	// refusing to read one would turn a cosmetic corruption into a lost selector.
	paths := []string{rel}
	for _, p := range FocusPathsFromText(pytext.DecodeUTF8Replace(data)) {
		if p != rel {
			paths = append(paths, p)
		}
	}
	return FocusWindow{Paths: paths, Source: rel}
}
