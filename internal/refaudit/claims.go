package refaudit

import (
	"regexp"
	"strings"

	"github.com/ZacxDev/cairn/internal/store"
)

// EntryInput is one entry as the auditor reads it: its ref, its file's text, and its `refs:`.
type EntryInput struct {
	Ref  string
	Text string
	Refs []store.TaskRef
}

// Extracted is an entry's claims, plus the rows it saw and could not check by design.
type Extracted struct {
	Claims   []Claim
	ByDesign []Finding // `unchecked: not path-shaped`
}

// pointerSpan is the first backticked span of a `## Pointers` row.
var pointerSpan = regexp.MustCompile("`([^`]+)`")

// githubPR is the structural PR claim: a `github:` ref of `<owner>/<repo>#<N>` (`refurl.go`'s shape).
var githubPR = regexp.MustCompile(`^([A-Za-z0-9._-]+)/([A-Za-z0-9._-]+)#([0-9]+)$`)

// ExtractClaims reads an entry's CHECKABLE claims, deterministically (decision 6):
//
//   - a PATH: the first backticked span of a `## Pointers` row, or its first token before ` — `,
//     when it is path-shaped (contains `/` or a `.<ext>`, no scheme, no whitespace, no leading `-`),
//     normalised by stripping one leading `./` and any trailing `/`. A row whose candidate is not
//     path-shaped is counted `unchecked: not path-shaped`, so the denominator is honest;
//   - a SYMBOL: ONLY the structural `path#Symbol` form in a Pointers row (Q8) — never a heuristic
//     over prose, the shape `openness.go` refuses for markers;
//   - a COMMIT: the sha of a `RESOLVED <sha>:` bullet (`store.BulletOpenness`, the ONE marker parser);
//   - a PR: a `github:` ref of `<owner>/<repo>#<N>`.
//
// Prose backticks, SHAs in prose, URLs and other ref systems are NOT claims (decision 6's
// "unchecked by design" list) and are not counted.
func ExtractClaims(e EntryInput) Extracted {
	var out Extracted
	sections := store.ExtractSections(e.Text, []string{store.PointersHeading, store.NuanceHeading})
	for _, line := range strings.Split(sections[store.PointersHeading], "\n") {
		row, ok := pointerRow(line)
		if !ok {
			continue
		}
		candidate := row
		if m := pointerSpan.FindStringSubmatch(row); m != nil {
			candidate = m[1]
		} else if before, _, found := strings.Cut(row, " — "); found {
			candidate = strings.TrimSpace(before)
		} else {
			candidate = strings.Fields(row)[0]
		}
		path, symbol, isSymbol := strings.Cut(candidate, "#")
		if isSymbol && symbol != "" && pathShaped(path) && symbolShaped(symbol) {
			out.Claims = append(out.Claims, Claim{Entry: e.Ref, Kind: KindSymbol, Value: NormalisePath(path) + "#" + symbol})
			continue
		}
		if !pathShaped(candidate) {
			out.ByDesign = append(out.ByDesign, Finding{Claim: Claim{Entry: e.Ref, Kind: KindPath, Value: candidate}, State: NotPathShaped})
			continue
		}
		out.Claims = append(out.Claims, Claim{Entry: e.Ref, Kind: KindPath, Value: NormalisePath(candidate)})
	}
	for _, b := range store.ParseJournalBullets(sections[store.NuanceHeading]) {
		if b.Openness == store.OpennessResolved && b.ResolvedBy != "" {
			out.Claims = append(out.Claims, Claim{Entry: e.Ref, Kind: KindCommit, Value: b.ResolvedBy})
		}
	}
	for _, ref := range e.Refs {
		if ref.System == "github" && githubPR.MatchString(ref.Ident) {
			out.Claims = append(out.Claims, Claim{Entry: e.Ref, Kind: KindPR, Value: ref.Ident})
		}
	}
	return out
}

// pointerRow is a top-level `- …` / `* …` row's text, trimmed; ok false for anything else.
func pointerRow(line string) (string, bool) {
	for _, marker := range []string{"- ", "* "} {
		if rest, ok := strings.CutPrefix(line, marker); ok {
			rest = strings.TrimSpace(rest)
			return rest, rest != ""
		}
	}
	return "", false
}

// pathShaped is decision 6's rule: contains `/` or a `.<ext>`, no scheme, no whitespace, no leading
// `-`.
func pathShaped(s string) bool {
	if s == "" || strings.HasPrefix(s, "-") || strings.Contains(s, "://") || strings.ContainsAny(s, " \t\r\n") {
		return false
	}
	if strings.Contains(s, "/") {
		return true
	}
	dot := strings.LastIndex(s, ".")
	return dot > 0 && dot < len(s)-1
}

// symbolShaped is an identifier: what `git grep -w -F` can match as a word.
var symbolShape = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*$`)

func symbolShaped(s string) bool { return symbolShape.MatchString(s) }

// NormalisePath strips ONE leading `./` and any trailing `/` (round 2's 🟡2): `./cmd/x.go` and
// `internal/` name what `cmd/x.go` and `internal` name.
func NormalisePath(p string) string {
	p = strings.TrimPrefix(p, "./")
	return strings.TrimRight(p, "/")
}
