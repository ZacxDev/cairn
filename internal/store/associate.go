package store

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// This file is the port of the WRITER's path→subsystem matcher
// (`subsystem_resolver.associate_paths` and `path_refs`).
//
// 🔴 IT IS HERE BECAUSE P2's CLI REACHES IT, AND `report.ErrFocusSelectorUnported`
// EXISTED ONLY UNTIL IT DID. P1b shipped the featured-entry selector with its
// most-recent fallback and refused a non-empty focus window by name, on the stated
// grounds that the store API never sends one — true of the pod, and false of the
// CLI. `cairn recall` with no `--scope` and the default `--mode` builds a window out
// of the repo's newest handoff doc (`focus.Window`), so the refusal was reachable
// from the commonest invocation of the commonest verb. A window that silently took
// the fallback would print a basis claiming a resolved pick, so the only two honest
// options were "refuse" and "port the matcher"; the CLI cannot refuse, so this is
// the port. The closing condition AGENTS.md recorded for the error — the matcher
// ported with a red-at-baseline differential test — is met by
// `TestAssociatePathsMatchesTheOracle` in `associate_test.go`.
//
// ⚠ IT IS A PORT OF THE WRITER'S MATCHER AND NOT A SECOND RANKING. `report`'s
// selector consumes `Matched` and nothing else decides which entry is featured; the
// ordering rule (path count, then mtime, then ref) lives at the one call site, as it
// does on the oracle.

// InvalidPathError is a path that is not usable as a repo-relative path. Sentinel:
// `invalid repo-relative path`.
//
// Absolute paths and `..` traversal are REJECTED rather than normalized away: an
// absolute path drags its whole prefix into the component set, so accepting one
// silently converts a caller bug into plausible-looking associations.
type InvalidPathError struct{ message string }

func (e *InvalidPathError) Error() string { return e.message }

// Evidence is why ONE path was attributed to ONE entry.
type Evidence struct {
	Path string
	// Component is the RAW path component (or filename stem) that produced the ref.
	Component string
	// Ref is the component after normalization — what was actually compared.
	Ref string
	// Tier is "filename" or "alias".
	Tier string
	// MatchedAlias is the alias as WRITTEN in the entry, when Tier == "alias".
	MatchedAlias string
}

// SubsystemMatch is one entry and the paths that named it.
type SubsystemMatch struct {
	Entry    Entry
	Paths    []string
	Evidence []Evidence
}

// PathCount is how many DISTINCT paths named this entry — the ranking signal.
func (m SubsystemMatch) PathCount() int { return len(m.Paths) }

// AmbiguousRef is a ref that could not be resolved because it named more than one
// entry. It contributes to no subsystem and is recorded rather than raised: one
// undecidable ref must not blind the whole window.
type AmbiguousRef struct {
	Ref        string
	Tier       string
	Candidates []string
	// Paths are the paths that produced it, so a human can see what would have
	// been tagged.
	Paths []string
}

// Association is the full, accounted-for result. Nothing is dropped silently:
// Matched + BelowThreshold + Ambiguous + UnmatchedPaths is the complete story of
// the input.
//
// 🔴 `len(Matched) == 0` CONFLATES THE TWO ZEROS — it is empty both when no paths
// were supplied and when paths were supplied and matched nothing, and those mean
// opposite things downstream. `ConsideredPaths` is the discriminator, and
// `LookedAtNothing` names it so no caller re-derives the test.
type Association struct {
	Scope    string
	MinPaths int

	// Matched are the entries that cleared MinPaths, in canonical ref order.
	Matched        []SubsystemMatch
	BelowThreshold []SubsystemMatch
	Ambiguous      []AmbiguousRef
	UnmatchedPaths []string
	// ConsideredPaths is every path actually examined, deduped, in input order.
	ConsideredPaths []string
}

// LookedAtNothing is true when NO paths were supplied — as distinct from "matched
// nothing".
//
// 🔴 IT IS AN AFFORDANCE, NOT A GATE, exactly as on the oracle. Nothing can force a
// consumer to consult it; that is the one thing raising an exception did buy, and it
// was traded away because it made the ordinary case exceptional.
func (a Association) LookedAtNothing() bool { return len(a.ConsideredPaths) == 0 }

// SubsystemRefs are the canonical refs of the entries that CLEARED the threshold,
// already in canonical order — `AssociatePaths` establishes that order in ONE place.
// A second `sort` here would mean neither site could be observed to be wrong.
func (a Association) SubsystemRefs() []string {
	out := make([]string, 0, len(a.Matched))
	for _, m := range a.Matched {
		out = append(out, m.Entry.Ref())
	}
	return out
}

// PathRefs are the candidate `(raw component, normalized ref)` pairs for one
// repo-relative path.
//
// Every path component contributes its normalized self. The FINAL component
// additionally contributes its STEM (one extension stripped), so
// `apps/widget/values.yaml` offers `values` as well as `values.yaml`.
//
// 🔴 ONLY ONE EXTENSION IS STRIPPED: `foo.tar.gz` offers `foo.tar`, not `foo`.
// Stripping greedily would let any dotted filename impersonate a short slug.
//
// Duplicate pairs are collapsed with insertion order preserved, so `a/a/values.yaml`
// counts once per distinct (component, ref).
func PathRefs(path string) [][2]string {
	var parts []string
	for _, p := range strings.Split(path, "/") {
		if p != "" && p != "." {
			parts = append(parts, p)
		}
	}
	var out [][2]string
	seen := map[[2]string]struct{}{}
	add := func(component, ref string) {
		if ref == "" {
			return
		}
		key := [2]string{component, ref}
		if _, dup := seen[key]; dup {
			return
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	for i, part := range parts {
		add(part, NormalizeRef(part))
		// `strings.Trim(part, ".")` mirrors `part.strip(".")`: a component that is
		// ALL dots offers no stem, and `.gitignore` offers none either because the
		// dot is leading rather than separating.
		if i == len(parts)-1 && strings.Contains(strings.Trim(part, "."), ".") {
			stem := part[:strings.LastIndex(part, ".")]
			add(stem, NormalizeRef(stem))
		}
	}
	return out
}

// ValidateRepoRelativePath is the per-path guard. Its two refusal sentences are the
// oracle's, transcribed.
func ValidateRepoRelativePath(path string) error {
	if strings.TrimSpace(path) == "" {
		return &InvalidPathError{message: fmt.Sprintf(
			"invalid repo-relative path %s: empty or not a string", PyRepr(path))}
	}
	if strings.HasPrefix(path, "/") {
		return &InvalidPathError{message: fmt.Sprintf(
			"invalid repo-relative path %s: absolute paths drag their prefix "+
				"into the component set (home, zach, workspace, …) and manufacture matches",
			PyRepr(path))}
	}
	if slices.Contains(strings.Split(path, "/"), "..") {
		return &InvalidPathError{message: fmt.Sprintf(
			"invalid repo-relative path %s: `..` escapes the repo root", PyRepr(path))}
	}
	return nil
}

// AssociatePaths maps changed repo-relative paths onto the subsystems they touch.
//
// Pure: no I/O, no clock, no globals. `ix` is injected.
//
// Guard order — each reachable by an input no earlier guard rejects:
//  1. `minPaths` sanity   → a plain error
//  2. scope known         → UnknownScopeError
//  3. each path relative  → InvalidPathError
//
// An ambiguous ref does NOT fail here: it lands in `Ambiguous`, contributes to no
// subsystem, and a caller that ignores that field is discarding a known unknown.
//
// 🔴 AN EMPTY PATH SET IS NOT AN ERROR. It returns a fully empty, fully accounted
// Association — see `LookedAtNothing` for the structural discriminator that replaced
// the exception the oracle removed.
func AssociatePaths(paths []string, ix *Index, scope string, minPaths int) (Association, error) {
	if minPaths < 1 {
		return Association{}, fmt.Errorf("min_paths must be an int >= 1, got %d", minPaths)
	}
	// Scope guard, before a single path is counted: an unknown scope must not be
	// able to produce a well-formed empty result. (An empty path set may; a
	// typo'd scope may not.)
	if _, err := ix.Entries(scope); err != nil {
		return Association{}, err
	}

	var ordered []string
	for _, raw := range paths {
		if err := ValidateRepoRelativePath(raw); err != nil {
			return Association{}, err
		}
		if !slices.Contains(ordered, raw) {
			ordered = append(ordered, raw)
		}
	}

	pathsByEntry := map[string][]string{}
	evidenceByEntry := map[string][]Evidence{}
	entryByRef := map[string]Entry{}
	ambiguousByKey := map[[2]string]AmbiguousRef{}
	var ambiguousOrder [][2]string
	matchedPaths := map[string]struct{}{}

	for _, path := range ordered {
		for _, pair := range PathRefs(path) {
			component, ref := pair[0], pair[1]
			entry, tier, err := ResolveRefTiered(ref, ix, scope)
			if err != nil {
				var amb *AmbiguousRefError
				if !errors.As(err, &amb) {
					return Association{}, err
				}
				key := [2]string{amb.Ref, amb.Tier}
				prev, had := ambiguousByKey[key]
				seen := append([]string{}, prev.Paths...)
				if !slices.Contains(seen, path) {
					seen = append(seen, path)
				}
				if !had {
					ambiguousOrder = append(ambiguousOrder, key)
				}
				ambiguousByKey[key] = AmbiguousRef{
					Ref:        amb.Ref,
					Tier:       amb.Tier,
					Candidates: amb.Candidates,
					Paths:      seen,
				}
				continue
			}
			// `tier` is "" exactly when `entry` is nil — they are returned
			// together — so testing both is defensiveness no input can
			// distinguish.
			if entry == nil {
				continue
			}

			matchedAlias := ""
			if tier == "alias" {
				for _, a := range entry.RawAliases {
					if NormalizeRef(a) == ref {
						matchedAlias = a
						break
					}
				}
			}

			key := entry.Ref()
			entryByRef[key] = *entry
			if !slices.Contains(pathsByEntry[key], path) {
				pathsByEntry[key] = append(pathsByEntry[key], path)
			}
			evidenceByEntry[key] = append(evidenceByEntry[key], Evidence{
				Path:         path,
				Component:    component,
				Ref:          ref,
				Tier:         tier,
				MatchedAlias: matchedAlias,
			})
			matchedPaths[path] = struct{}{}
		}
	}

	// 🔴 THE ONE ordering site. Output order is by canonical ref, NOT by the order
	// paths happened to arrive: a re-run over the same window must produce the same
	// rows in the same order, or a diff of two runs shows churn that is not there.
	keys := make([]string, 0, len(pathsByEntry))
	for k := range pathsByEntry {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	var matched, below []SubsystemMatch
	for _, key := range keys {
		m := SubsystemMatch{
			Entry:    entryByRef[key],
			Paths:    pathsByEntry[key],
			Evidence: evidenceByEntry[key],
		}
		if m.PathCount() >= minPaths {
			matched = append(matched, m)
		} else {
			below = append(below, m)
		}
	}

	ambSorted := append([][2]string{}, ambiguousOrder...)
	slices.SortFunc(ambSorted, func(a, b [2]string) int {
		if a[0] != b[0] {
			return strings.Compare(a[0], b[0])
		}
		return strings.Compare(a[1], b[1])
	})
	ambiguous := make([]AmbiguousRef, 0, len(ambSorted))
	for _, k := range ambSorted {
		ambiguous = append(ambiguous, ambiguousByKey[k])
	}

	var unmatched []string
	for _, p := range ordered {
		if _, hit := matchedPaths[p]; !hit {
			unmatched = append(unmatched, p)
		}
	}

	return Association{
		Scope:           NormalizeRef(scope),
		MinPaths:        minPaths,
		Matched:         matched,
		BelowThreshold:  below,
		Ambiguous:       ambiguous,
		UnmatchedPaths:  unmatched,
		ConsideredPaths: ordered,
	}, nil
}
