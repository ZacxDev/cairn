package store

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ZacxDev/cairn/internal/pytext"
)

// The two malformed-entry POLICIES. `Raise` is right for a WRITER, which is
// about to modify a curated store and must not act on a partial picture of it;
// `Collect` is right for a READER, where aborting spends every good entry in the
// scope to report one bad one.
//
// 🔴 COLLECT IS NOT "SKIP". A collected entry is carried on the index, counted,
// and every reader that uses this mode is OBLIGED to print it: silently serving a
// short index would be a worse failure than the collapse it replaces, because a
// missing entry is indistinguishable from an entry that was never written.
type OnMalformed int

const (
	Raise OnMalformed = iota
	Collect
)

// UnknownScopeError is "the requested scope is not in the index". Sentinel:
// `unknown scope`.
//
// 🔴 A REFUSED SCOPE AND A NEVER-EXISTED ONE PRODUCE THIS SAME ERROR, and that
// is the whole enumeration property: the allowlist is applied to the INDEX, so a
// scope the caller may not see is simply not in `byScope` and asking for it is
// indistinguishable from asking for one nobody ever created.
type UnknownScopeError struct{ message string }

func (e *UnknownScopeError) Error() string { return e.message }

// AmbiguousRefError is a ref that resolved to more than one entry within a tier.
// Sentinel: `ambiguous ref`. The resolver NEVER picks — `Candidates` is the list
// for a human to choose from.
type AmbiguousRefError struct {
	Ref        string
	Tier       string
	Scope      string
	Candidates []string
	message    string
}

func (e *AmbiguousRefError) Error() string { return e.message }

// Index holds entries grouped by scope. A scope may legitimately be present but
// EMPTY, and that distinction is load-bearing: an existing-but-empty scope
// directory yields an honest empty result while a scope the index has never heard
// of is an UnknownScopeError. Collapsing the two would turn a typo'd scope into
// "0 subsystems", the exact silent zero this store exists to avoid.
type Index struct {
	byScope map[string][]Entry

	// Malformed holds the entries that were REJECTED, when the index was built
	// with Collect. Always empty under Raise. It is a field on the index rather
	// than a second return value so that "the entries" and "what could not become
	// an entry" cannot be separated by a caller that only takes the first thing.
	Malformed []MalformedEntry
}

// Scopes is every scope the index knows, sorted.
func (ix *Index) Scopes() []string {
	out := make([]string, 0, len(ix.byScope))
	for k := range ix.byScope {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// Entries is the entries of ONE scope, or UnknownScopeError.
func (ix *Index) Entries(scope string) ([]Entry, error) {
	key := NormalizeRef(scope)
	entries, present := ix.byScope[key]
	if !present {
		known := strings.Join(ix.Scopes(), ", ")
		if known == "" {
			known = "(none)"
		}
		return nil, &UnknownScopeError{message: fmt.Sprintf(
			"unknown scope %s (normalized %s); the index holds: %s",
			PyRepr(scope), PyRepr(key), known)}
	}
	return entries, nil
}

// HasScope answers the membership question without minting an error, for the one
// caller that treats an unknown scope as an ordinary outcome.
func (ix *Index) HasScope(scope string) bool {
	_, present := ix.byScope[NormalizeRef(scope)]
	return present
}

// MalformedIn is the rejected entries belonging to ONE scope, normalized like
// every ref. No UnknownScopeError: this answers "what is broken here", and an
// unknown scope has nothing broken in it.
func (ix *Index) MalformedIn(scope string) []MalformedEntry {
	key := NormalizeRef(scope)
	var out []MalformedEntry
	for _, m := range ix.Malformed {
		if m.Scope == key {
			out = append(out, m)
		}
	}
	return out
}

// MalformedOutside is the rejected entries in EVERY OTHER scope — the store-wide
// defect count. A reader is scope-scoped, so without this a broken entry in a
// scope nobody happens to recall today is invisible until someone recalls it.
func (ix *Index) MalformedOutside(scopes []string) []MalformedEntry {
	keys := map[string]struct{}{}
	for _, s := range scopes {
		keys[NormalizeRef(s)] = struct{}{}
	}
	var out []MalformedEntry
	for _, m := range ix.Malformed {
		if _, in := keys[m.Scope]; !in {
			out = append(out, m)
		}
	}
	return out
}

// Len is the total entry count across every scope.
func (ix *Index) Len() int {
	n := 0
	for _, v := range ix.byScope {
		n += len(v)
	}
	return n
}

// BuildIndex validates and groups entry mappings.
//
// `extraScopes` registers scopes that exist but hold no entries (a scope
// directory the loader found empty), so they resolve to an empty result rather
// than UnknownScopeError.
//
// 🔴 BOTH REJECTION SITES COLLECT — the per-entry validator AND the duplicate
// check. A duplicate is a relationship between two files, and the one recorded is
// the LATER of the pair in the loader's sorted order, so the first spelling of a
// ref keeps serving and the collision is still named. Collecting only the first
// site would leave Collect able to raise, which is the shape a caller cannot
// defend against because it looks handled.
func BuildIndex(mappings []FrontMatter, extraScopes []string, onMalformed OnMalformed) (*Index, error) {
	collecting := onMalformed == Collect
	byScope := map[string][]Entry{}
	var malformedRows []MalformedEntry
	type dupKey struct{ scope, slug, kind string }
	seen := map[dupKey]string{}

	for _, mapping := range mappings {
		source := mappingSource(mapping)
		entry, err := EntryFromMapping(mapping, source)
		if err != nil {
			if !collecting {
				return nil, err
			}
			me, _ := err.(*MalformedEntryError)
			malformedRows = append(malformedRows, rejection(mapping, source, me))
			continue
		}
		key := dupKey{entry.Scope, entry.Slug, entry.Kind}
		if first, dup := seen[key]; dup {
			why := fmt.Sprintf("duplicate %s in scope %s — already defined by %s",
				PyRepr(entry.Ref()), PyRepr(entry.Scope), PyRepr(first))
			dupErr := malformed(source, why)
			if !collecting {
				return nil, dupErr
			}
			malformedRows = append(malformedRows, rejection(mapping, source, dupErr))
			continue
		}
		seen[key] = entry.Filename
		byScope[entry.Scope] = append(byScope[entry.Scope], entry)
	}

	// 🔴 A SCOPE THAT HOLDS ONLY BROKEN ENTRIES STILL EXISTS. Without this the
	// scope would be unknown to the index and a reader would answer
	// `scope-absent` — "nothing recorded yet" — about a directory full of content
	// it simply could not parse.
	for _, m := range malformedRows {
		if m.Scope != "" {
			if _, present := byScope[m.Scope]; !present {
				byScope[m.Scope] = nil
			}
		}
	}
	for _, scope := range extraScopes {
		key := NormalizeRef(scope)
		if key == "" {
			continue
		}
		if _, present := byScope[key]; !present {
			byScope[key] = nil
		}
	}
	return &Index{byScope: byScope, Malformed: malformedRows}, nil
}

// mappingSource is the label a rejection names: the filename, else `service:`,
// else `<unnamed>` — the same precedence Python's `build_index` uses.
func mappingSource(mapping FrontMatter) string {
	if fn, ok := mapping.String("filename"); ok && fn != "" {
		return fn
	}
	if svc, ok := mapping.String("service"); ok && svc != "" {
		return svc
	}
	return "<unnamed>"
}

// rejection turns one refusal into one row. The scope comes from the MAPPING,
// deliberately: on a disk load `scope` was set from the directory name before
// validation ran, so it is known even for an entry too broken to construct —
// which is what lets a rejected entry be reported against the scope it lives in
// instead of against the store at large.
func rejection(mapping FrontMatter, source string, err *MalformedEntryError) MalformedEntry {
	rawScope, isStr := mapping.String("scope")
	if !isStr || pytext.StripWhitespace(rawScope) == "" {
		rawScope, isStr = mapping.String("repo")
	}
	scope := ""
	if isStr {
		scope = NormalizeRef(rawScope)
	}
	filename := source
	if fn, isStr := mapping.String("filename"); isStr && fn != "" {
		filename = fn
	}
	why := err.Error()
	if err.Why != "" {
		why = err.Why
	}
	return MalformedEntry{Scope: scope, Filename: filename, Reason: why}
}

// ResolveRefTiered resolves one ref within one scope, reporting WHICH tier hit.
//
// Two tiers; an alias can never outrank a filename:
//  1. FILENAME — the normalized ref against `<slug>.md` and every
//     `<slug>.<kind>.md`. A ref naming its own kind matches ONLY that qualified
//     file.
//  2. ALIAS — the normalized `aliases:` across the scope, consulted ONLY if tier
//     1 returned zero hits.
//
// One hit returns the entry and its tier. More than one in a tier is
// AmbiguousRefError listing the candidates, never a pick. Zero in both is
// `(nil, "", nil)` — an honest miss, not an error.
//
// The tier is RETURNED rather than re-derived by the caller, because a second
// expression computing "was that a filename or an alias hit?" is the same
// predicate twice, one edit from disagreeing with the branch that chose the entry.
func ResolveRefTiered(ref string, ix *Index, scope string) (*Entry, string, error) {
	entries, err := ix.Entries(scope)
	if err != nil {
		return nil, "", err
	}
	nref := NormalizeRef(ref)
	// ⚠ REDUNDANT-BUT-KEPT, and labelled so a mutation sweep does not re-derive
	// it: no entry can have an empty slug (validation rejects one), so an empty
	// ref would miss both tiers anyway. A cheap short-circuit that states the
	// intent, not a guard.
	if nref == "" {
		return nil, "", nil
	}

	slug, kind := SplitKind(nref)
	var hits []Entry
	for _, e := range entries {
		if kind != "" {
			if e.Slug == slug && e.Kind == kind {
				hits = append(hits, e)
			}
		} else if e.Slug == nref {
			hits = append(hits, e)
		}
	}
	if len(hits) > 1 {
		return nil, "", ambiguous(nref, "filename", hits, scope)
	}
	if len(hits) == 1 {
		hit := hits[0]
		return &hit, "filename", nil
	}

	hits = nil
	for _, e := range entries {
		if slices.Contains(e.Aliases, nref) {
			hits = append(hits, e)
		}
	}
	if len(hits) > 1 {
		return nil, "", ambiguous(nref, "alias", hits, scope)
	}
	if len(hits) == 1 {
		hit := hits[0]
		return &hit, "alias", nil
	}
	return nil, "", nil
}

func ambiguous(ref, tier string, hits []Entry, scope string) error {
	names := make([]string, 0, len(hits))
	for _, e := range hits {
		names = append(names, e.Filename)
	}
	slices.Sort(names)
	return &AmbiguousRefError{
		Ref:        ref,
		Tier:       tier,
		Scope:      scope,
		Candidates: names,
		message: fmt.Sprintf(
			"ambiguous ref %s in scope %s: %d candidates in the %s tier (%s). The resolver never picks — disambiguate the ref or the index.",
			PyRepr(ref), PyRepr(scope), len(names), tier, strings.Join(names, ", ")),
	}
}
