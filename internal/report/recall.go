package report

import (
	"errors"
	"sort"

	"github.com/ZacxDev/cairn/internal/store"
)

// LISTING_PAGE_SIZE — 🔴 THE INDEX IS CAPPED, AND THE CAP IS LOUD. Every line is ~60
// bytes, so an index that grows with the store forever is a per-session cost with no
// ceiling; 100 lines is ~6 KB, still smaller than the ONE featured body it sits above on
// a large entry. Past the cap the remainder is COUNTED and `?page=N` reaches it — nothing
// is dropped and nothing is silent.
//
// ⚠ IT IS A LISTING CAP AND NOT A SELECTION RULE: the scope total still counts every
// entry, and the truncation notice names both the count and the parameter.
const ListingPageSize = 100

// ErrFocusSelectorUnported is returned when a caller supplies a focus path window.
//
// 🔴 A DELIBERATE REFUSAL, NOT AN OVERSIGHT, AND IT IS THE FAIL-LOUD DIRECTION. The
// oracle's featured-entry pick has TWO selectors: a path window resolved through the
// WRITER's own `associate_paths` matcher, and the most-recent fallback. Only the fallback
// is ported, because the store API never sends a path window — a pod has no repo to read
// a handoff doc out of — so porting the matcher here would ship an unexercised second
// implementation of the writer's ranking, which is exactly the "free to drift" shape this
// codebase refuses.
//
// The CLI (P2) does read a window, and it must not silently get the fallback while the
// report PRINTS a basis claiming a resolved pick. So a non-empty window is refused by
// name until the matcher is ported with its own tests. Closing condition: `associate_paths`
// ported into `internal/store` with a red-at-baseline differential test against the
// oracle, at which point this error and its guard are deleted together.
var ErrFocusSelectorUnported = errors.New(
	"the focus-path featured-entry selector is not ported: pass no focus paths, or port " +
		"`associate_paths` first — a window that silently fell back would print a basis it did not use")

// RecallReport is one deterministic answer to "what does the index already record here?".
type RecallReport struct {
	Status    string
	Scope     string
	StoreRoot string

	// Entries are the bodies this report PRINTS — one in digest mode, one for a `?ref=`,
	// up to Limit in full mode, none in list mode.
	Entries []RecalledEntry

	// TotalInScope is entries the scope holds, BEFORE any cap. The truncation
	// discriminator.
	TotalInScope int

	Limit int

	// Ref/HasRef is the `?ref=` narrowing. HasRef separates "no ref was sent" from
	// "`?ref=` with an empty value", which still narrows and finds nothing.
	Ref    string
	HasRef bool

	// Candidates are the filenames an ambiguous ref named. The resolver never picks; nor
	// does this.
	Candidates []string

	// KnownScopes is every scope the (already narrowed) index holds — printed on
	// `scope-absent` so a typo'd or unexpectedly-normalized scope is visible instead of
	// reading as "nothing recorded yet".
	KnownScopes []string

	// Malformed are entry files in THIS scope that could not be indexed. Rendered on
	// every status, before anything else. These are NOT in Entries, Listing or
	// TotalInScope: they never became entries, and counting them as if they had would be
	// the silent short index the collecting loader exists to avoid.
	Malformed []store.MalformedEntry

	// MalformedElsewhere is the same, in every OTHER scope of the store.
	MalformedElsewhere []store.MalformedEntry

	Mode string

	// Listing is ONE PAGE of the index, newest-first by file mtime. It is a PAGE and not
	// a filter: ListingTotal counts every entry in the scope and the renderer prints the
	// remainder with the `?page=` that reaches it.
	Listing      []RecalledEntry
	ListingTotal int
	ListingPage  int
	ListingPages int

	// FeaturedBasis is which selector chose Entries[0], in words. Set in digest mode
	// only, and never empty there: a featured entry with no stated basis is the implicit
	// pick this package refuses to make.
	FeaturedBasis string
}

// PageIsPastTheEnd is 🔴 THE ONE PLACE THIS QUESTION IS ASKED. Three renderer branches
// need it — the index header, the page notice and list mode's completeness line — and the
// oracle's first shipped version answered it separately in each, which is how a header
// printed `entries 801–800 of 150 (page 9 of 2)` while the notice directly under it was
// correct.
func (r RecallReport) PageIsPastTheEnd() bool { return r.ListingPage > r.ListingPages }

// ListingBeforePage is index rows on EARLIER pages. 0 on a page past the end: nothing was
// listed there, so "before" describes no position.
func (r RecallReport) ListingBeforePage() int {
	if r.PageIsPastTheEnd() {
		return 0
	}
	return (r.ListingPage - 1) * ListingPageSize
}

// ListingAfterPage is index rows on LATER pages — what a truncation notice is about.
//
// 🔴 THE BUG THIS EXISTS FOR. The notice originally fired on "this page does not hold the
// whole index", which is TRUE ON THE LAST PAGE TOO. On page 2 of 2 it therefore announced
// page 1's 100 entries as still unseen and routed the reader to a `page=3` that does not
// exist: a false truncation notice contradicting the correct header on the line above it.
func (r RecallReport) ListingAfterPage() int {
	if r.PageIsPastTheEnd() {
		return 0
	}
	if n := r.ListingTotal - r.ListingBeforePage() - len(r.Listing); n > 0 {
		return n
	}
	return 0
}

// Omitted is entries in the scope whose BODY was not printed.
//
// A `?ref=` run reports 0: one of four is a NARROWING, not a truncation, and calling it
// an omission trains the reader to ignore the real one.
func (r RecallReport) Omitted() int {
	if r.HasRef {
		return 0
	}
	if n := r.TotalInScope - len(r.Entries); n > 0 {
		return n
	}
	return 0
}

// Caveat is what this window can and cannot see — CaveatText, and nothing local.
//
// 🔴 `Listing`, NOT `Entries`. Badges are rendered by the index row, which iterates
// Listing; Entries holds only the FEATURED bodies and is a different, usually smaller
// set. Reading Entries here looked right and was silently wrong on the oracle — the
// explanation vanished on an index whose visible `🔴 2 NEAR-MISS` row simply was not among
// the featured entries. Caught by running it, not by reading it.
func (r RecallReport) Caveat() string {
	return CaveatText(r.Scope+"/", badgesPresent(r.Listing))
}

// Recall surfaces an entry's `## What it is` + `## Pointers` +
// `## Nuance / work-history`. READ-ONLY: no clock, no network, no git, no prompt.
//
// Guard order — each reachable by an input no earlier guard rejects: the option ladder
// (ValidateRecall, which the caller has already run and which is NOT re-run here, so the
// rule has one site), then the store root, then the store's readability, then the scope,
// then whether anything in it could be read.
//
// 🔴 AN ABSENT SCOPE IS A STATUS, NOT AN ERROR, and that is the single most load-bearing
// decision here. A store holds few scopes while work spans many repos, so "this repo has
// nothing recorded yet" is the ORDINARY outcome. Returning an error would make the common
// case the exceptional one, every caller would wrap the call, and a wrapped call is how
// the genuine errors above get swallowed too.
//
// 🔴 `visible` IS PASSED, NEVER RE-DERIVED. A scope the caller may not see must be absent
// from the INDEX, not filtered out of each answer — see store.LoadStore. Once it is
// absent, `scope-absent` (and its KnownScopes list) is what a refused scope produces,
// which is byte-for-byte what a scope that never existed produces.
func Recall(storeRoot string, opts RecallOptions, visible store.ScopeSet) (RecallReport, error) {
	if len(opts.FocusPaths) > 0 {
		return RecallReport{}, ErrFocusSelectorUnported
	}
	index, err := store.LoadStore(storeRoot, "recalled", visible)
	if err != nil {
		return RecallReport{}, err
	}
	// Computed ONCE, before any status branch, and handed to every one of them — the
	// MALFORMED block renders on all of them, so deriving it per branch would be the same
	// predicate at six sites, wrong at five.
	bad := index.MalformedIn(opts.Scope)
	badElsewhere := index.MalformedOutside([]string{opts.Scope})

	base := RecallReport{
		Scope:              store.NormalizeRef(opts.Scope),
		StoreRoot:          storeRoot,
		Limit:              opts.Limit,
		Mode:               opts.Mode,
		KnownScopes:        index.Scopes(),
		Malformed:          bad,
		MalformedElsewhere: badElsewhere,
		ListingPage:        1,
		ListingPages:       1,
	}

	entries, scopeErr := index.Entries(opts.Scope)
	if scopeErr != nil {
		var unknown *store.UnknownScopeError
		if !errors.As(scopeErr, &unknown) {
			return RecallReport{}, scopeErr
		}
		out := base
		out.Status = StatusScopeAbsent
		// ⚠ THE RAW REF, UNNORMALIZED, ON THIS BRANCH ONLY — the oracle's own asymmetry.
		// Nothing renders it here (the `scope-absent` branch prints no ref), so
		// normalizing would be a change with no observable effect and one more place for
		// the two to disagree.
		out.Ref, out.HasRef = opts.Ref, opts.HasRef
		return out, nil
	}

	// 🔴 BEFORE `?ref=`, AND BEFORE `scope-empty`. A scope whose every file was rejected
	// reaches both of those branches looking identical to a scope that holds nothing —
	// `ref-absent` would say "nothing recorded under that name yet" and `scope-empty`
	// would say "NOTHING RECORDED YET", and both would be false about a directory full of
	// content. This is the discriminator, and it is the ONLY thing standing between a
	// broken store and a status a consumer reports as an ordinary non-finding.
	if len(entries) == 0 && len(bad) > 0 {
		out := base
		out.Status = StatusScopeUnreadable
		if opts.HasRef {
			out.Ref, out.HasRef = store.NormalizeRef(opts.Ref), true
		}
		return out, nil
	}

	if opts.HasRef {
		entry, _, refErr := store.ResolveRefTiered(opts.Ref, index, opts.Scope)
		if refErr != nil {
			var ambiguous *store.AmbiguousRefError
			if !errors.As(refErr, &ambiguous) {
				return RecallReport{}, refErr
			}
			out := base
			out.Status = StatusRefAmbiguous
			out.TotalInScope = len(entries)
			out.Ref, out.HasRef = ambiguous.Ref, true
			out.Candidates = ambiguous.Candidates
			return out, nil
		}
		if entry == nil {
			out := base
			out.Status = StatusRefAbsent
			out.TotalInScope = len(entries)
			out.Ref, out.HasRef = store.NormalizeRef(opts.Ref), true
			return out, nil
		}
		// A `?ref=` run is a NARROWING and prints its one entry in full whatever the mode
		// says — no index, no featured basis.
		recalled, readErr := ReadEntry(storeRoot, *entry)
		if readErr != nil {
			return RecallReport{}, readErr
		}
		out := base
		out.Status = StatusRecalled
		out.Entries = []RecalledEntry{recalled}
		out.TotalInScope = len(entries)
		out.Ref, out.HasRef = store.NormalizeRef(opts.Ref), true
		return out, nil
	}

	if len(entries) == 0 {
		out := base
		out.Status = StatusScopeEmpty
		return out, nil
	}

	// THE ONE ordering site for bodies: canonical ref ascending, so two runs over an
	// unchanged store produce identical bytes and a diff of them shows only real movement.
	ordered := make([]store.Entry, len(entries))
	copy(ordered, entries)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Ref() < ordered[j].Ref() })

	if opts.Mode == "full" {
		capped := ordered
		if opts.Limit < len(capped) {
			capped = capped[:opts.Limit]
		}
		read, readErr := readAll(storeRoot, capped)
		if readErr != nil {
			return RecallReport{}, readErr
		}
		out := base
		out.Status = StatusRecalled
		out.Entries = read
		out.TotalInScope = len(ordered)
		return out, nil
	}

	// `digest` and `list` both read EVERY entry — the index line carries a bullet count,
	// which only the file can answer, and the featured pick needs every entry's mtime.
	// The PAGE cap is applied to the rendered listing only, never to the read.
	read, readErr := readAll(storeRoot, ordered)
	if readErr != nil {
		return RecallReport{}, readErr
	}
	pageSlice, pages := ListingPageOf(read, opts.Page)

	out := base
	out.Status = StatusRecalled
	out.TotalInScope = len(ordered)
	out.Listing = pageSlice
	out.ListingTotal = len(read)
	out.ListingPage = opts.Page
	out.ListingPages = pages

	if opts.Mode == "list" {
		return out, nil
	}

	featuredRef, basis := selectFeatured(read, opts.Scope)
	for _, e := range read {
		if e.Ref == featuredRef {
			out.Entries = append(out.Entries, e)
		}
	}
	out.FeaturedBasis = basis
	return out, nil
}

func readAll(storeRoot string, entries []store.Entry) ([]RecalledEntry, error) {
	out := make([]RecalledEntry, 0, len(entries))
	for _, e := range entries {
		recalled, err := ReadEntry(storeRoot, e)
		if err != nil {
			return nil, err
		}
		out = append(out, recalled)
	}
	return out, nil
}

// ListingOrder is the index's OWN order: newest-first by file mtime, tie-broken by ref.
//
// 🔴 A SECOND ORDERING SITE, ON PURPOSE, and the only one that is not ref-ascending. It
// exists because the index is CAPPED and a cap makes the order load-bearing: cutting an
// alphabetical list at 100 hides entries by an accident of their names, while cutting a
// recency list hides the stale ones — the only cut that means anything to a session
// re-entering work.
//
// 🔴 AND THE TIE-BREAK IS NOT DECORATION. Two entries written in the same whole second
// tie on a truncated mtime and fall through to the ref; the conformance fixture carries
// exactly such a pair, differing only in the fraction, because getting this wrong
// produces a different order with no error and no missing entry — which reads as a stale
// cache.
// ⚠ `SliceStable` IS BELT-AND-BRACES AND NOT A GUARD — LABELLED SO A SWEEP DOES NOT
// RE-DERIVE IT. The comparator is a TOTAL order (refs are unique within a scope, so no two
// entries compare equal), which makes stability unobservable: a mutant swapping it for
// `sort.Slice` SURVIVED the whole battery, correctly. It stays because "equal elements keep
// their order" is the property a reader assumes of an ordering function, and the day a
// future field makes the comparator partial is the day it starts mattering.
func ListingOrder(entries []RecalledEntry) []RecalledEntry {
	out := make([]RecalledEntry, len(entries))
	copy(out, entries)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].MTime != out[j].MTime {
			return out[i].MTime > out[j].MTime
		}
		return out[i].Ref < out[j].Ref
	})
	return out
}

// ListingPageOf is one page of the ordered index, and how many pages there are in total.
//
// A page PAST the end returns an EMPTY slice rather than clamping to the last one:
// clamping would answer a question the caller did not ask and print a full page under a
// heading saying `page=9`. The renderer says the page is past the end and names the valid
// range.
func ListingPageOf(entries []RecalledEntry, page int) ([]RecalledEntry, int) {
	ordered := ListingOrder(entries)
	pages := (len(ordered) + ListingPageSize - 1) / ListingPageSize
	if pages < 1 {
		pages = 1
	}
	start := (page - 1) * ListingPageSize
	if start >= len(ordered) {
		return nil, pages
	}
	end := start + ListingPageSize
	if end > len(ordered) {
		end = len(ordered)
	}
	return ordered[start:end], pages
}

// selectFeatured picks the ONE entry to print in full, and says why.
//
// 🔴 THE BASIS IS RETURNED, NOT LOGGED, so no caller can render a pick without also
// rendering how it was made. An entry featured without saying which selector chose it is
// indistinguishable from an entry the tool thinks is IMPORTANT, and that is a claim the
// store cannot support.
//
// 🔴 WHY MTIME AND NOT GIT RECENCY, which would otherwise be the obvious choice: mtime is
// trustworthy for THIS store specifically because the store has no remote and is never
// cloned, checked out or rebased — the only thing that ever touches those files is a
// session editing them in place, so mtime IS the edit time. Git recency is not usable as
// the primary signal: the store's history is mostly bulk autocommits, so several entries
// share one commit timestamp and the intra-commit order is unrecoverable. The ordinary
// git-recency argument (a checkout rewrites mtime and git does not) is exactly backwards
// for a repo nobody ever checks out.
//
// ⚠ `scope` IS THE CALLER'S RAW SPELLING AND NOT THE NORMALIZED ONE, because that is what
// the oracle interpolates into the printed basis. Every other sentence in the report uses
// the normalized scope; this one does not, and the difference is observable the moment a
// caller asks for `Alpha_Notes`.
func selectFeatured(entries []RecalledEntry, scope string) (ref, basis string) {
	best := entries[0]
	for _, e := range entries[1:] {
		if e.MTime > best.MTime || (e.MTime == best.MTime && e.Ref > best.Ref) {
			best = e
		}
	}
	return best.Ref, "most-recent fallback — newest entry file in `" + scope +
		"/` (no handoff doc to read a path window from)"
}
