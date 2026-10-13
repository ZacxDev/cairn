package refaudit

import (
	"errors"
	"strconv"
	"strings"

	"github.com/ZacxDev/cairn/internal/codesrc"
)

// GitReader is everything the auditor asks of a fetched source. `src` is the source's CANONICAL
// string; the implementation owns where its mirror lives. [GitCLI] is the real one.
type GitReader interface {
	// Fetch brings the source's mirror up to date and returns the declared branch's commit. An error
	// is "could not look": a [*HostNotMappedError] for a host the auditing host never contacts, any
	// other for a remote that could not be fetched.
	Fetch(src codesrc.Source) (commit string, err error)
	// HasPath is whether `path` names a blob or a tree at `commit` — decided on an exact compare of
	// the tree records' paths, never on the exit code (`ls-tree` exits 0 on a missing path).
	HasPath(src string, commit, path string) (bool, error)
	// Commits is every COMMIT object in the mirror whose name starts with `prefix`.
	Commits(src string, prefix string) ([]string, error)
	// ContainedBy is every ref under `refs/heads/` and `refs/pull/` — the two namespaces the fetch
	// wrote — that contains `sha`. Never a tag, never object presence.
	ContainedBy(src string, sha string) ([]string, error)
	// Grep is whether `symbol` occurs as a whole word in `path` at `commit`.
	Grep(src string, commit, path, symbol string) (bool, error)
}

// PRReader answers a GitHub PR's state: one of [PROpen], [PRMerged], [PRClosedUnmerged],
// [PRNotFound]; an error is [PRUnchecked].
type PRReader interface {
	PRState(owner, repo string, number int) (string, error)
}

// HostNotMappedError is a declared source on a host the auditing host's map does not name. That host
// is NEVER contacted (T1).
type HostNotMappedError struct{ Host string }

func (e *HostNotMappedError) Error() string {
	return "host " + e.Host + " is not in $" + EnvHosts + "; it was not contacted"
}

// MaxPRChecks bounds one run's API calls (T12); a PR claim past it is [PRUnchecked].
const MaxPRChecks = 50

// Audit checks every claim of every entry against the scope's declared sources. `sources` are
// canonical strings, in declaration order.
func Audit(sources []string, entries []EntryInput, git GitReader, prs PRReader) Result {
	res := Result{Sources: sources}
	type fetched struct {
		src    codesrc.Source
		canon  string
		commit string
	}
	var looked []fetched
	for _, canon := range sources {
		src, err := codesrc.Parse(canon)
		if err != nil {
			res.Fetched = append(res.Fetched, SourceResult{Source: canon, State: RemoteUnreachable, Detail: err.Error()})
			continue
		}
		commit, err := git.Fetch(src)
		var unmapped *HostNotMappedError
		switch {
		case errors.As(err, &unmapped):
			res.Fetched = append(res.Fetched, SourceResult{Source: canon, State: HostNotMapped, Detail: err.Error()})
		case err != nil:
			res.Fetched = append(res.Fetched, SourceResult{Source: canon, State: RemoteUnreachable, Detail: err.Error()})
		default:
			res.Fetched = append(res.Fetched, SourceResult{Source: canon, At: commit})
			looked = append(looked, fetched{src: src, canon: canon, commit: commit})
		}
	}

	prBudget := MaxPRChecks
	for _, e := range entries {
		ex := ExtractClaims(e)
		res.Findings = append(res.Findings, ex.ByDesign...)
		for _, c := range ex.Claims {
			if c.Kind == KindPR {
				res.Findings = append(res.Findings, checkPR(c, sources, prs, &prBudget))
				continue
			}
			if len(looked) == 0 {
				// No source could be looked at, so no claim was: the source rows say why, and the
				// claim is not counted at all — `checked=0` is the honest denominator.
				continue
			}
			var best Finding
			bestRank := -1
			unread := len(looked) < len(sources) // a declared source was never fetched
			for _, f := range looked {
				finding := Finding{Claim: c, Source: f.canon, At: f.commit}
				finding.State = checkAgainst(c, f.src, f.canon, f.commit, git)
				if finding.State == RemoteUnreachable {
					unread = true // fetched, but its git failed on THIS claim: not read either
				}
				if r := rank(finding.State); r > bestRank {
					best, bestRank = finding, r
				}
			}
			if unread && Severity[best.State] == ClassStale {
				// Some declared source was not read for this claim — never fetched, or its git failed on
				// it — and it may hold what no read one does: a stale verdict needs every source read
				// (operator decision on #259 r1 🟡2).
				best.State = NotInFetched
			}
			res.Findings = append(res.Findings, best)
		}
	}
	return res
}

// rank orders states within a kind, best first across sources: a claim holds if ANY looked-at
// source holds it.
func rank(state string) int {
	switch state {
	case HoldsRoot, OnBranch, SymbolPresent:
		return 5
	case HoldsSubpath, ExistsOffBranch:
		return 4
	case SymbolAbsent, Ambiguous:
		return 3
	case Missing, SymbolFileMissing, NotReachable:
		return 2
	default: // RemoteUnreachable: the source's git failed on this claim
		return 1
	}
}

func checkAgainst(c Claim, src codesrc.Source, canon, commit string, git GitReader) string {
	switch c.Kind {
	case KindPath:
		state, err := pathState(src, canon, commit, c.Value, git)
		if err != nil {
			return RemoteUnreachable
		}
		return state
	case KindSymbol:
		path, symbol, _ := strings.Cut(c.Value, "#")
		state, err := pathState(src, canon, commit, path, git)
		if err != nil {
			return RemoteUnreachable
		}
		if state == Missing {
			return SymbolFileMissing
		}
		full := path
		if state == HoldsSubpath {
			full = src.Subpath + "/" + path
		}
		found, err := git.Grep(canon, commit, full, symbol)
		if err != nil {
			return RemoteUnreachable
		}
		if found {
			return SymbolPresent
		}
		return SymbolAbsent
	case KindCommit:
		commits, err := git.Commits(canon, c.Value)
		if err != nil {
			return RemoteUnreachable
		}
		switch len(commits) {
		case 0:
			return NotReachable
		case 1:
		default:
			return Ambiguous
		}
		refs, err := git.ContainedBy(canon, commits[0])
		if err != nil {
			return RemoteUnreachable
		}
		for _, ref := range refs {
			if ref == "refs/heads/"+src.Branch {
				return OnBranch
			}
		}
		if len(refs) > 0 {
			return ExistsOffBranch
		}
		return NotReachable
	}
	return RemoteUnreachable
}

// pathState is the path check: repo-root-relative first, then subpath-relative (Q7), reporting which
// held.
func pathState(src codesrc.Source, canon, commit, path string, git GitReader) (string, error) {
	ok, err := git.HasPath(canon, commit, path)
	if err != nil {
		return "", err
	}
	if ok {
		return HoldsRoot, nil
	}
	if src.Subpath != "" {
		ok, err := git.HasPath(canon, commit, src.Subpath+"/"+path)
		if err != nil {
			return "", err
		}
		if ok {
			return HoldsSubpath, nil
		}
	}
	return Missing, nil
}

// checkPR is the PR row: `outside-sources` (a string comparison, no call) when the ref's owner/repo
// is none of the scope's GitHub sources; otherwise the API's answer, within the run's budget.
func checkPR(c Claim, sources []string, prs PRReader, budget *int) Finding {
	m := githubPR.FindStringSubmatch(c.Value)
	f := Finding{Claim: c}
	inside := false
	for _, canon := range sources {
		if src, err := codesrc.Parse(canon); err == nil && src.Host == "github.com" && src.RepoPath == m[1]+"/"+m[2] {
			inside = true
		}
	}
	if !inside {
		f.State = OutsideSources
		return f
	}
	if *budget <= 0 || prs == nil {
		f.State = PRUnchecked
		return f
	}
	*budget--
	n, _ := strconv.Atoi(m[3])
	state, err := prs.PRState(m[1], m[2], n)
	if err != nil {
		f.State = PRUnchecked
		return f
	}
	f.State = state
	return f
}
