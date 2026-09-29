package report

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ZacxDev/cairn/internal/hostid"
	"github.com/ZacxDev/cairn/internal/pytext"
	"github.com/ZacxDev/cairn/internal/store"
)

// listingLine is ONE index line: `  <ref>   N nuance  <sensitivity>[  <badges>]`. ~60
// bytes.
//
// The ref is what `?ref=` takes, the count is the size signal that says whether a `?ref=`
// is worth spending, and the sensitivity has to travel WITH the entry it describes — a
// sensitivity stated once at the top of a block is a sensitivity that gets copied away
// from.
//
// 🔴 A FIELD EARNS THIS LINE ONLY IF IT CHANGES WHAT THE READER DOES, because the index is
// the one thing printed for every entry and the cost is paid on every read. `OPEN` clears
// that bar where size does not — an entry with unfinished business may be describing a
// remedy that has since landed, and the reader cannot tell from the body. Every badge is
// CONDITIONAL, so entries with nothing to report render byte-identical to a row with no
// badges at all.
//
// ⚠ THE COUNT IS `## Nuance / work-history` BULLETS ONLY, and the word says so rather than
// leaving it to be assumed. It was `N bullets`, which reads as ENTRY SIZE to anyone
// scanning the index — an entry with 5 pointers and 7 nuance bullets showed `7 bullets`
// and has 12.
//
// ⚠ ORDER IS THE VALIDATOR'S ORDER (declared → near-miss → unverifiable), so a reader who
// has seen one surface can read the other without re-learning it. The `NO <heading>` badge
// sits last because it is not a bullet population at all — it says the parser never
// reached the bullets.
func listingLine(entry RecalledEntry, width int) string {
	base := "  " + ljustRunes(entry.Ref, width) + "  " +
		fmt.Sprintf("%3d", entry.BulletCount) + " nuance   " +
		SensitivityLabel(entry.Sensitivity, entry.DeclaredSensitivity)
	var badges []string
	if entry.OpenCount != 0 {
		badges = append(badges, "🔴 "+strconv.Itoa(entry.OpenCount)+" OPEN")
	}
	if entry.NearMissCount != 0 {
		badges = append(badges, "🔴 "+strconv.Itoa(entry.NearMissCount)+" NEAR-MISS")
	}
	if entry.UnverifiableCount != 0 {
		badges = append(badges, "⚠ "+strconv.Itoa(entry.UnverifiableCount)+" UNVERIFIABLE")
	}
	if len(entry.MissingSections) != 0 {
		short := make([]string, 0, len(entry.MissingSections))
		for _, h := range entry.MissingSections {
			short = append(short, shortHeading(h))
		}
		badges = append(badges, "🔴 NO "+strings.Join(short, ", "))
	}
	if len(entry.Tasks) != 0 {
		// 🔴 A COUNT AND NOT THE REFS, on the same bar: an entry joined to a task is an
		// entry whose work has a tracked owner and a closing condition somewhere else,
		// which decides whether to spend a `?ref=`. What it does NOT do is print the refs
		// — one can be 36 characters, three of them would triple the row, and the index's
		// whole contract is one line per entry. The refs are printed in the BODY.
		badges = append(badges, "🔗 "+strconv.Itoa(len(entry.Tasks))+" task"+plural(len(entry.Tasks)))
	}
	if len(badges) == 0 {
		return base
	}
	return base + "   " + strings.Join(badges, "   ")
}

// ljustRunes is `str.ljust(width)`: padding is counted in CODE POINTS, not bytes. Refs are
// normalized to `[a-z0-9.-]` so the two agree today; the function does not rely on that,
// because the day a normalizer widens is the day every index row silently reflows.
func ljustRunes(s string, width int) string {
	n := len([]rune(s))
	if n >= width {
		return s
	}
	return s + strings.Repeat(" ", width-n)
}

// renderListing is the index block, its ORDER stated, and its remainder counted.
//
// 🔴 The single-page wording is unchanged from the pre-pagination default ("ALL N … none
// omitted"), because on every scope that fits in one page the claim is exactly true and
// weakening it would train the reader to skim past the page notice on the day it is not.
// The multi-page wording shares no phrase with it.
//
// 🔴 …AND IT IS WITHDRAWN THE MOMENT A REJECT EXISTS. "ALL N entries, none omitted" is a
// COMPLETENESS CLAIM, and it is simply false about a scope whose third file could not be
// indexed — the index really does omit it. A comment the implementation contradicts is a
// defect, and so is a header: the clean branch keeps its exact historical bytes and a scope
// with rejects gets its OWN wording, sharing no phrase with either the clean or the
// paginated one.
func renderListing(r RecallReport) []string {
	width := 0
	for _, e := range r.Listing {
		if n := len([]rune(e.Ref)); n > width {
			width = n
		}
	}
	const order = "newest-first by file mtime"
	shown := len(r.Listing)
	nBad := len(r.Malformed)

	var head string
	switch {
	case r.PageIsPastTheEnd():
		// 🔴 NO ARITHMETIC ON A PAGE THAT DOES NOT EXIST. The oracle's first shipped
		// version computed the range unconditionally and printed
		// `entries 801–800 of 150 … (page 9 of 2)` — an inverted range and an impossible
		// page-of-page — directly above a correct guidance line. A header is a claim like
		// any other; it does not get to be wrong because something below it is right.
		head = "INDEX (" + RecallLabel + ") — no entries: page " + strconv.Itoa(r.ListingPage) +
			" is past the end of `" + r.Scope + "/`, which holds " +
			strconv.Itoa(r.ListingTotal) + " in " + strconv.Itoa(r.ListingPages) + " page" +
			plural(r.ListingPages) + " (" + order + "):"
	case r.ListingPages <= 1 && nBad > 0:
		head = "INDEX (" + RecallLabel + ") — the " + strconv.Itoa(shown) + " READABLE entr" +
			entryPlural(shown) + " in `" + r.Scope + "/` (" + order + "). NOT complete: " +
			strconv.Itoa(nBad) + " further file" + plural(nBad) + " in this scope could not be " +
			"indexed and " + isAre(nBad) + " named above, never here:"
	case r.ListingPages <= 1:
		head = "INDEX (" + RecallLabel + ") — ALL " + strconv.Itoa(shown) + " entr" +
			entryPlural(shown) + " in `" + r.Scope + "/`, none omitted (" + order + "):"
	default:
		first := r.ListingBeforePage() + 1
		// The paginated header already declines to claim completeness, so a reject only
		// needs the count corrected: ListingTotal is READABLE entries, and without this
		// the reader would take it for the file count.
		rejects := ""
		if nBad > 0 {
			rejects = ", plus " + strconv.Itoa(nBad) + " that could NOT be indexed (named above)"
		}
		head = "INDEX (" + RecallLabel + ") — entries " + strconv.Itoa(first) + "–" +
			strconv.Itoa(first+shown-1) + " of " + strconv.Itoa(r.ListingTotal) + " in `" +
			r.Scope + "/`" + rejects + ", " + order + " (page " + strconv.Itoa(r.ListingPage) +
			" of " + strconv.Itoa(r.ListingPages) + "):"
	}

	out := []string{"", head}
	for _, e := range r.Listing {
		out = append(out, listingLine(e, width))
	}

	switch {
	case r.PageIsPastTheEnd():
		// Its own branch and its own words: a page past the end lists nothing, and
		// "nothing on this page" must not be readable as "nothing on record".
		out = append(out, "  (PAGE "+strconv.Itoa(r.ListingPage)+" IS PAST THE END — `"+
			r.Scope+"/` holds "+strconv.Itoa(r.ListingTotal)+" entr"+entryPlural(r.ListingTotal)+
			" across "+strconv.Itoa(r.ListingPages)+" page"+plural(r.ListingPages)+" of "+
			strconv.Itoa(ListingPageSize)+". Nothing was listed here and nothing is missing "+
			"from the store; re-run with `--page 1` … `--page "+strconv.Itoa(r.ListingPages)+"`.)")
	case r.ListingAfterPage() > 0:
		// 🔴 GATED ON WHAT COMES AFTER THIS PAGE, never on "this page is not the whole
		// index". On the LAST page this is 0 and nothing prints, because there is nothing
		// left to announce and a notice pointing at a page that does not exist is worse
		// than none.
		remaining := r.ListingAfterPage()
		next := r.ListingPage + 1
		tail := ""
		if next < r.ListingPages {
			tail = " … `--page " + strconv.Itoa(r.ListingPages) + "`"
		}
		out = append(out, "  (… "+strconv.Itoa(remaining)+" more entr"+entryPlural(remaining)+
			" NOT LISTED on this page — the index is capped at "+strconv.Itoa(ListingPageSize)+
			" lines per page. Nothing is hidden and nothing was filtered: `--page "+
			strconv.Itoa(next)+"`"+tail+" lists the rest, oldest last.)")
	}
	return out
}

// RenderText is the agent-facing recall block.
//
// Deterministic in the REPORT with ONE exception: the host line names THIS machine's
// identity, which is the entire point of it — a recall that does not name whose disk it
// read states one host's store as the fleet's.
//
// `extraHeader` is how a CLI puts the read store's snapshot stamp WITH the `store:`/`host:`
// lines instead of somewhere else on the page. The POD passes none: its `/data` has no
// stamp to print.
//
// `instance` is the alias of the cairn instance this report was read from, and `""` — the
// single-instance case — is what the POD passes and what every client on a host with one
// instance configured passes. It adds a clause to the caveat naming which instance was
// consulted; see `hostid.StoreCaveat`.
//
// 🔴 THAT IS THE WHOLE INSTANCE AWARENESS THIS PACKAGE HAS, AND THE NARROWNESS IS THE POINT.
// It is a string that reaches two sentences. This package does not DISCOVER instances, does
// not know what a routing table is, and takes no server or client config — the renderer is a
// library both the pod and the CLI import, and instance discovery belongs to
// `internal/client`, which is the only caller that can answer "how many stores can this
// machine reach".
func (r RecallReport) RenderText(host string, extraHeader []string, instance string) string {
	out := []string{
		"subsystem-recall: status=" + r.Status + " scope=" + r.Scope,
		"  store: " + r.StoreRoot,
		hostid.StoreHostLine(host, "  ", instance),
	}
	out = append(out, extraHeader...)
	out = append(out, "  caveat: "+r.Caveat())

	// 🔴 THE NARROWING ANNOUNCES ITSELF, BECAUSE EVERY COUNT BELOW IT IS ABOUT THE NARROWED
	// SET. Without this line a `--ref-to` digest is byte-indistinguishable from a digest of a
	// scope that happens to hold exactly those entries — the reader would take a filtered
	// index for the whole one. It carries BOTH numbers so what the filter removed is visible.
	//
	// ⚠ EMITTED ONLY WHEN THE FILTER WAS SENT, so no existing golden moves. `HasRefTo` is
	// false on every request that does not carry the parameter, and the line is absent then.
	if r.HasRefTo {
		// ⚠ THE LAST ARGUMENT IS THE RENDER DECISION, NOT A STATUS. "Everything below is
		// about those N" is a claim about the BODY, so the body is what has to answer it:
		// `RendersNarrowedSet()` is true exactly when a matched entry is printed below,
		// and its own header records the three shapes a status-name derivation got wrong
		// plus the digest-mode case that rules out the obvious second guess.
		// ⚠ THE LAST ARGUMENT IS WHAT THE TAG FILTER MADE NECESSARY. `RefToMatched` is the
		// entries that carry the ref; with a `--tag` also in force the body below reports on a
		// SUBSET of them, so "everything below is about those N" would be false — see
		// `reachClause`.
		//
		// ⚠ AND IT IS "THE TAG FILTER REMOVED SOMETHING", NOT "A TAG FILTER WAS SENT". A `--tag`
		// that kept every entry narrows nothing, so the original clause is still TRUE there and
		// saying otherwise would send a reader looking for a subset that is the whole set.
		out = append(out, refToLine(r.RefTo, r.RefToMatched, r.RefToScopeTotal, r.Scope+"/",
			r.RendersNarrowedSet(), r.TagScopeTotal > r.TagMatched))
	}

	// The CATEGORY narrowing announces itself for the same reason, and AFTER the `ref-to` line
	// because that is the order the two filters ran in — so a report carrying both reads as the
	// composition it is rather than as two independent claims about one index.
	//
	// ⚠ EMITTED ONLY WHEN THE FILTER WAS SENT, so no existing golden moves: `Tag` is empty on
	// every request that does not carry the parameter.
	if r.Tag != "" {
		// The same render decision, not a status — `RendersNarrowedSet()`'s own header records
		// the shapes a status-name derivation got wrong, and every one of them is reachable
		// with `--tag` in place of `--ref-to`.
		out = append(out, tagLine(r.Tag, r.TagMatched, r.TagScopeTotal, r.Scope+"/",
			r.RendersNarrowedSet()))
	}

	// 🔴 BEFORE EVERY STATUS BRANCH, INCLUDING THE ONES THAT RETURN IMMEDIATELY. A reject
	// reported only on the paths somebody remembered is a reject that will be missed on
	// the path they did not — and `scope-absent`, the most common status in most repos, is
	// precisely where a store-wide defect would otherwise never be mentioned at all.
	out = append(out, renderMalformed(r.Malformed, r.MalformedElsewhere, r.Scope+"/")...)

	switch r.Status {
	case StatusScopeUnreadable:
		out = append(out, "", unreadableSummary(r.Scope+"/", r.Malformed))
		return strings.Join(out, "\n")

	case StatusScopeAbsent:
		out = append(out, "")
		out = append(out, "NOTHING RECORDED YET ON THIS HOST — "+host+"'s store has no `"+
			r.Scope+"/` directory. This is the ordinary case in most repos (the store is "+
			"young and its scopes are few), NOT an error and NOT an absence of drift: "+
			"nothing was checked, so nothing can be concluded from it. Carry on with the "+
			"resume and say plainly that the index had nothing for this repo ON THIS MACHINE.")
		// 🔴 THE SECOND SENTENCE IS THE ONE THE OLD WORDING LACKED. "The store" reads as
		// one thing; the CACHES are two. Measured on the oracle's fleet before the
		// cutover: seven scopes existed only on one machine and ten only on the other, so
		// "not recorded" was routinely false of the fleet while true of the disk that was
		// read. Post-cutover the caches converge — but only when each syncs, so a read
		// still sees one disk at one time.
		out = append(out, "  NOT A FACT ABOUT THE FLEET — "+hostid.StoreCaveat(instance)+". The "+
			"other host syncs the SAME hosted store through its own cache, and may already "+
			"hold `"+r.Scope+"/` where this one has not synced it yet.")
		out = append(out, "  scopes THIS HOST's store holds: "+joinOrNone(r.KnownScopes))
		return strings.Join(out, "\n")

	case StatusScopeEmpty:
		out = append(out, "")
		out = append(out, "NOTHING RECORDED YET — `"+r.Scope+"/` exists but holds no entries. "+
			"Same conclusion as an absent scope and a DIFFERENT mechanism: the directory was "+
			"made and never filled, or was pruned to nothing. Not an error.")
		return strings.Join(out, "\n")

	case StatusRefAmbiguous:
		out = append(out, "")
		out = append(out, "AMBIGUOUS REF `"+r.Ref+"` — it names more than one entry, so "+
			"nothing was surfaced. The resolver never picks; neither does this. Candidates: "+
			strings.Join(r.Candidates, ", ")+". Re-run naming one of them.")
		return strings.Join(out, "\n")

	case StatusRefToAbsent:
		out = append(out, "")
		// 🔴 TWO SENTENCES FOR ONE STATUS, BECAUSE `ref-to-absent` IS REACHED TWO WAYS AND
		// THE SECOND ONE IS NOT AN ABSENCE AT ALL. Nothing in the scope matched (the filter's
		// own zero) and "the `--ref` operand is not among the entries that DID match" are
		// different facts, and the second shipped wearing the first's words: both clients
		// printed "NO ENTRY REFERENCES <X>" over a scope where other entries carried it,
		// because the numerator had been pinned to 0 whenever the status was this one. The
		// discriminator is `RefToMatched` — see its field header for both directions of that
		// pendulum.
		if r.RefToMatched > 0 {
			// The `--ref` operand loaded fine, so the malformed rows cannot make THIS claim
			// wrong; they can only understate the count of entries that DO carry the ref,
			// which is what the qualification names.
			extra := ""
			if n := len(r.Malformed); n > 0 {
				extra = " ⚠ AND THAT COUNT IS ONLY OF ENTRIES THAT LOADED: " + strconv.Itoa(n) +
					" entry file" + plural(n) + " in this scope could not be indexed (listed " +
					"above), and an entry that never loaded carries no refs a filter can see."
			}
			out = append(out, "`"+r.Ref+"` DOES NOT REFERENCE `"+r.RefTo+"` — it was read and "+
				"carries no such ref, so the two narrowings compose to nothing and no body is "+
				"printed. "+strconv.Itoa(r.RefToMatched)+" of the "+
				strconv.Itoa(r.RefToScopeTotal)+" entr"+entryPlural(r.RefToScopeTotal)+" in `"+
				r.Scope+"/` "+doesOrDo(r.RefToMatched)+" reference it — re-run without `--ref` "+
				"to see "+themOrIt(r.RefToMatched)+
				". The comparison is on THIS SCOPE's `refs:` keys and "+
				"the id half is matched byte-for-byte, so a different spelling of it would not "+
				"be found here either."+extra)
			return strings.Join(out, "\n")
		}
		// 🔴 IT SAYS WHAT WAS LOOKED AT AND WHAT WAS NOT, because a reverse lookup's zero is
		// the most misreadable answer this reader produces: "no entry references X" and "X is
		// not a thing anybody tracks" are different facts, and only the first is in evidence.
		// The malformed rows are already above; this sentence is what stops the reader
		// concluding from them in the wrong direction, exactly as `ref-absent`'s does.
		extra := ""
		if n := len(r.Malformed); n > 0 {
			extra = " ⚠ BUT " + strconv.Itoa(n) + " entry file" + plural(n) + " in this scope " +
				"could not be indexed (listed above), and an entry that never loaded carries no " +
				"refs a filter can see — one of them may reference this. Check those before " +
				"concluding nothing does."
		}
		out = append(out, "NO ENTRY REFERENCES `"+r.RefTo+"` — the "+
			strconv.Itoa(r.RefToScopeTotal)+" entr"+entryPlural(r.RefToScopeTotal)+" in `"+
			r.Scope+"/` were read and none of them carries that ref. This is a fact about "+
			"THIS SCOPE's `refs:` keys and NOT about whether the reference exists: an entry "+
			"may point at it under a different spelling of the id half, which is compared "+
			"byte-for-byte."+extra)
		return strings.Join(out, "\n")

	case StatusTagAbsent:
		out = append(out, "")
		// 🔴 TWO SENTENCES FOR ONE STATUS, FOR THE REASON `ref-to-absent` ABOVE HAS TWO, AND
		// WRITTEN THAT WAY FROM THE START RATHER THAN AFTER A ROUND MEASURED IT WRONG. This
		// status is reached TWO ways — nothing in the set carried the tag (the filter's own
		// zero), and "the `--ref` operand is not among the entries that DID" — and a status
		// cannot tell them apart. The discriminator is `TagMatched`, taken from the filter's own
		// result; see its field header.
		if r.TagMatched > 0 {
			// The `--ref` operand loaded fine, so the malformed rows cannot make THIS claim
			// wrong; they can only understate the count of entries that DO carry the tag.
			extra := ""
			if n := len(r.Malformed); n > 0 {
				extra = " ⚠ AND THAT COUNT IS ONLY OF ENTRIES THAT LOADED: " + strconv.Itoa(n) +
					" entry file" + plural(n) + " in this scope could not be indexed (listed " +
					"above), and an entry that never loaded carries no tags a filter can see."
			}
			out = append(out, "`"+r.Ref+"` IS NOT TAGGED `"+r.Tag+"` — it was read "+
				"and does not carry it, so the two narrowings compose "+
				"to nothing and no body is printed. "+strconv.Itoa(r.TagMatched)+" of the "+
				strconv.Itoa(r.TagScopeTotal)+" entr"+entryPlural(r.TagScopeTotal)+" in `"+
				r.Scope+"/` "+doesOrDo(r.TagMatched)+" — re-run without `--ref` to see "+
				themOrIt(r.TagMatched)+"."+extra)
			return strings.Join(out, "\n")
		}
		// 🔴 IT SAYS WHAT WAS LOOKED AT AND WHAT WAS NOT, because a category filter's zero is as
		// misreadable as a reverse lookup's: "no entry is tagged X" and "X is not a category
		// anybody uses" are different facts, and only the first is in evidence. The vocabulary
		// is OPEN, so a typo in either the file or the query makes a silently separate category
		// — that is what the last clause names.
		extra := ""
		if n := len(r.Malformed); n > 0 {
			extra = " ⚠ BUT " + strconv.Itoa(n) + " entry file" + plural(n) + " in this scope " +
				"could not be indexed (listed above), and an entry that never loaded carries no " +
				"tags a filter can see — one of them may be tagged this. Check those before " +
				"concluding nothing is."
		}
		out = append(out, "NO ENTRY IS TAGGED `"+r.Tag+"` — the "+
			strconv.Itoa(r.TagScopeTotal)+" entr"+entryPlural(r.TagScopeTotal)+" in `"+
			r.Scope+"/` were read and none of them carries it. Both "+
			"sides of the comparison are FOLDED, so a differently-cased spelling would have "+
			"been found — but the tag vocabulary is OPEN and nothing declares it, so a typo in "+
			"the file or in this query is a category of one that no check can see."+extra)
		return strings.Join(out, "\n")

	case StatusRefAbsent:
		out = append(out, "")
		// 🔴 THE ABSENCE IS QUALIFIED WHEN THE SCOPE HAS REJECTS. "Nothing recorded under
		// that name yet" is a claim about the STORE, and it is false if the entry exists in
		// a file the loader refused. The malformed rows are already above; this sentence is
		// what stops the reader concluding from them in the wrong direction.
		extra := " Nothing recorded under that name yet; re-run without `--ref` to see what " +
			"IS recorded."
		if n := len(r.Malformed); n > 0 {
			extra = " ⚠ BUT " + strconv.Itoa(n) + " entry file" + plural(n) + " in this scope " +
				"could not be indexed (listed above) — this ref may name one of them, in which " +
				"case it IS recorded and merely invisible. Check those before concluding the " +
				"name is new; re-run without `--ref` to see what IS readable."
		}
		out = append(out, "NO SUCH ENTRY — `"+r.Ref+"` resolves to nothing in `"+r.Scope+
			"/`, which holds "+strconv.Itoa(r.TotalInScope)+" entr"+entryPlural(r.TotalInScope)+
			"."+extra)
		return strings.Join(out, "\n")
	}

	// ListingTotal, not Listing: a `?page=` past the end has an EMPTY slice and still has
	// an index block to render — the notice saying so is the whole point. Gating on the
	// slice would make that page render as `full` mode.
	if r.ListingTotal != 0 {
		out = append(out, renderListing(r)...)
	}

	if len(r.Entries) == 0 {
		// `list` mode. LOUD about what it did not do: an index with no bodies is not an
		// empty scope, and the two must not read the same.
		//
		// 🔴 IT DESCRIBES THE PAGE, NOT THE SCOPE. This line originally asserted "the N
		// entries above are the complete index", with N the SCOPE total — false on every
		// paginated page (100 were above, not 150) and flatly self-contradictory past the
		// end (zero were). The scope total still appears, because "this is not an empty
		// scope" is the other half of the sentence and has to stay true.
		shown := len(r.Listing)
		var what string
		switch {
		case r.PageIsPastTheEnd():
			what = "NO entries were listed — see the page notice above. `" + r.Scope +
				"/` holds " + strconv.Itoa(r.TotalInScope)
		case r.ListingPages > 1:
			what = "The " + strconv.Itoa(shown) + " entr" + entryPlural(shown) + " above " +
				isAre(shown) + " page " + strconv.Itoa(r.ListingPage) + " of " +
				strconv.Itoa(r.ListingPages) + " of the index for `" + r.Scope +
				"/`, which holds " + strconv.Itoa(r.TotalInScope) + " in all"
		case len(r.Malformed) > 0:
			// 🔴 The same withdrawn completeness claim as the header above, in the other
			// place it is spelled. "the complete index" is false when a file in the scope
			// could not be indexed, and this line is the one a reader quotes when they say
			// "the store has N of them".
			nBad := len(r.Malformed)
			what = "The " + strconv.Itoa(r.TotalInScope) + " entr" +
				entryAboveIs(r.TotalInScope) + " every READABLE entry in `" + r.Scope +
				"/` — NOT the complete index: " + strconv.Itoa(nBad) + " further file" +
				plural(nBad) + " could not be indexed"
		default:
			what = "The " + strconv.Itoa(r.TotalInScope) + " entr" +
				entryAboveIs(r.TotalInScope) + " the complete index for `" + r.Scope + "/`"
		}
		out = append(out, "")
		out = append(out, "NO ENTRY BODIES WERE PRINTED (--list). "+what+" — this is not an "+
			"empty scope. Run `--ref <name>` for one entry's `"+store.WhatHeading+"` + `"+
			store.PointersHeading+"` + `"+store.NuanceHeading+"`.")
		return strings.Join(out, "\n")
	}

	out = append(out, "")
	if r.FeaturedBasis != "" {
		// 🔴 THE BASIS, NEVER THE PICK ALONE.
		out = append(out, "FEATURED IN FULL ("+RecallLabel+") — 1 of "+
			strconv.Itoa(r.TotalInScope)+", "+r.FeaturedBasis+":")
	} else {
		out = append(out, "RECALL ("+RecallLabel+") — "+strconv.Itoa(len(r.Entries))+" of "+
			strconv.Itoa(r.TotalInScope)+" entr"+entryPlural(r.TotalInScope)+" in `"+
			r.Scope+"/`:")
	}
	for _, e := range r.Entries {
		out = append(out, "")
		out = append(out, "  ### "+e.Ref+"  ("+r.Scope+"/"+e.Filename+", sensitivity="+
			SensitivityLabel(e.Sensitivity, e.DeclaredSensitivity)+")")
		if len(e.Tasks) != 0 {
			// 🔴 THE REFS THEMSELVES, AND ONLY IN A BODY. Above the sections deliberately:
			// "which task does this answer" is identity, like the ref and the sensitivity
			// on the line above, not content.
			//
			// ⚠ THE LABEL STILL READS `tasks:` WHILE THE FRONT-MATTER KEY IS `refs:` AND THE
			// BROWSER SURFACE SAYS "Refs", AND THAT IS DEFERRED RATHER THAN OVERLOOKED. The
			// Go type's field and the Python dataclass's each got a paragraph explaining the
			// name; this line had none, so a reader could not tell the mismatch from an
			// omission. Changing it is not a rename: it re-bases every recall golden in
			// `tests/conformance/`, the reader fixture `internal/report/testdata/` replays,
			// and the parity harness's byte diffs — so it belongs in a change whose whole
			// subject is that re-base, not in one that happens to touch this function.
			out = append(out, "    tasks: "+strings.Join(e.Tasks, ", "))
		}
		if len(e.Tags) != 0 {
			// Identity, like the refs line above and for the same reason: "what category is
			// this" is not content.
			//
			// ⚠ THE LABEL MATCHES THE KEY HERE, WHICH THE LINE ABOVE DOES NOT — and saying so
			// is the point rather than leaving a reader to wonder whether this one is also
			// deferred. `tags:` is the key an operator writes and `tags:` is what this prints;
			// there is no older spelling, no rename in flight, and nothing to defer.
			out = append(out, "    tags: "+strings.Join(e.Tags, ", "))
		}
		for _, heading := range SurfacedHeadings {
			body := e.Sections[heading]
			if body == "" {
				continue
			}
			out = append(out, "    "+heading)
			for _, line := range pytext.SplitLines(body) {
				out = append(out, "      "+line)
			}
		}
		if e.Sections[store.WhatHeading] == "" {
			// 🔴 SAID, NOT LEFT BLANK — and BODY-ONLY, never on the index row. Absent and
			// present-but-empty are folded together on purpose: both render as nothing
			// above, and the reader's question ("what IS this thing?") is unanswered either
			// way.
			//
			// 🔴 IT CLAIMS A PARSE, NEVER A FACT ABOUT THE ENTRY. The notice used to read
			// "this entry never says what the subsystem IS" — which the extractor cannot
			// know. A heading the parser does not match parses to nothing and produced that
			// same sentence while the answer sat on disk.
			//
			// 🔴 THE CAUSE LIST IS EXPLICITLY NON-EXHAUSTIVE ("among others"), and it has to
			// be: headings match at column 0 and fenced regions are skipped, so a RENAME, an
			// INDENTED heading and one inside a fence all reach this same branch — and only
			// the first is literally a "rename". An enumeration that reads as closed is a
			// narrower claim than the branch.
			out = append(out, "    (no parsable `"+store.WhatHeading+"` — absent, empty, or "+
				"not parsed as a heading [renamed, indented, fenced, among others], so this "+
				"read cannot say what the subsystem IS; re-derive it live)")
		}
		if e.IsBare() {
			// 🔴 Said, not left blank. An entry that exists with nothing under either
			// COUNTED heading is a real state (the writer's own template ships a stub), and
			// printing nothing for it is indistinguishable from an extractor that failed to
			// find the sections.
			out = append(out, "    (no `"+store.PointersHeading+"` or `"+store.NuanceHeading+
				"` content — the entry exists but has not been filled in)")
		} else if len(e.MissingSections) != 0 {
			quoted := make([]string, 0, len(e.MissingSections))
			for _, h := range e.MissingSections {
				quoted = append(quoted, "`"+h+"`")
			}
			out = append(out, "    (no "+strings.Join(quoted, ", ")+" section)")
		}
	}

	if omitted := r.Omitted(); omitted > 0 && r.ListingTotal != 0 {
		// The digest's own words. It is NOT the `--limit` truncation below: every one of
		// these entries IS listed above and is one `?ref=` away, so borrowing the
		// truncation wording would train the reader to read a complete index as a lossy
		// one — and then to ignore the real notice.
		out = append(out, "")
		out = append(out, "… "+strconv.Itoa(omitted)+" further entr"+entryPlural(omitted)+
			" in `"+r.Scope+"/` LISTED ABOVE but NOT shown in full. Nothing is hidden: this "+
			"is the default digest, not a judgement about relevance. `--ref <name>` prints "+
			"any one of them in full; `--limit "+strconv.Itoa(r.TotalInScope)+"` prints them all.")
	} else if omitted > 0 {
		out = append(out, "")
		out = append(out, "… "+strconv.Itoa(omitted)+" more entr"+entryPlural(omitted)+" in `"+
			r.Scope+"/` NOT shown (--limit "+strconv.Itoa(r.Limit)+"). This is a display cap, "+
			"not a judgement about relevance — raise it to see the rest.")
	}
	return strings.Join(out, "\n")
}

// refToLine is the ONE spelling of the reverse-lookup header, shared by both renderers.
//
// 🔴 ONE FUNCTION, TWO CALLERS, BECAUSE THE TWO REPORT TYPES HAVE DIFFERENT COUNTS TO PUT IN
// IT AND THE SENTENCE MUST NOT DIFFER. `CaveatText`'s own header gives the rule: a package
// with two report types spells a shared sentence once, or it is wrong in one of them.
//
// `matched`/`total` are the narrowed and pre-filter counts. `narrowedSetShown` says whether
// the report BELOW this line reports on the matched entries — a PREDICATE each caller
// answers for itself, and deliberately not a status test: `RecallReport` passes
// `RendersNarrowedSet()` (see its header for the statuses a status test got wrong) and
// `SearchReport` passes a constant, justified at its own call site. It is a separate clause
// rather than a second function so the COUNT half stays spelled once.
//
// ⚠ `label` IS A FORMED LABEL AND CARRIES ITS OWN TRAILING `/`, so do not append one here.
// The draft passed `SearchReport.Scope`, which on a store-wide search is the literal
// `(all scopes)`, plus a `/` — printing “ `(all scopes)/` “. `SearchReport.Label()` is what it
// passes now: `ScopeLabel` over the SEARCHED scopes, which cannot return `(all scopes)` at all
// (it returns `(no scope)` for an empty set, and a `/`-suffixed join otherwise). Caught by
// reading the regenerated golden, not by a test: both implementations agreed, and both were
// wrong.
func refToLine(refTo string, matched, total int, label string, narrowedSetShown, narrowedFurther bool) string {
	return "  ref-to: `" + refTo + "` — " + strconv.Itoa(matched) + " of " +
		strconv.Itoa(total) + " entr" + entryPlural(total) + " in `" + label +
		"` reference it, " + reachClause(matched, narrowedSetShown, narrowedFurther) +
		narrowingCaveat
}

// reachClause is the middle clause of BOTH filter headers: what the body below the line is
// about. THREE states, spelled once, because there are three and two of them were once one.
//
// 🔴 THE THIRD STATE IS WHAT A SECOND FILTER MADE NECESSARY, AND ITS ABSENCE WOULD HAVE BEEN A
// FALSE SENTENCE RATHER THAN A MISSING ONE. With `--ref-to X --tag Y`, the `ref-to` line's
// numerator is the entries that reference X — which is correct and is NOT what the body renders,
// because the tag filter then removes some of them. "Everything below is about those N" was
// therefore false about the store in exactly the way #141's round 2 measured on four other
// shapes: a count of matched entries above a body that reports on fewer. Naming the further
// narrowing is the honest clause.
//
// 🔴 `narrowedSetShown` WINS OVER `narrowedFurther`, AND THE ORDER IS LOAD-BEARING. When nothing
// below reports on a matched entry at all, "a further filter narrows those N again" would be
// true and useless — it invites the reader to look for the narrowed subset, and there is no
// subset on screen. The "NOTHING below" clause points at the sentence that explains the empty
// body, which is the only thing there is to read.
func reachClause(matched int, narrowedSetShown, narrowedFurther bool) string {
	switch {
	case !narrowedSetShown:
		return "and NOTHING below is about them — the sentence below says why."
	case narrowedFurther:
		return "and a FURTHER filter below narrows those " + strconv.Itoa(matched) + " again."
	default:
		return "and everything below is about those " + strconv.Itoa(matched) + "."
	}
}

// tagLine is the ONE spelling of the category-filter header, shared by both renderers for
// exactly the reason `refToLine` above is shared: two report types, one sentence.
//
// 🔴 IT IS A SEPARATE FUNCTION AND NOT A `refToLine` PARAMETERISED BY A LABEL, EVEN THOUGH BOTH
// OPERANDS ARE NOW SCALAR. The two sentences differ in every clause but the trailing NARROWING
// caveat — "carry it" against "reference it", and two different pairs of counts — so folding them
// would put a conditional on each clause to save one shared word. That caveat is the half neither
// renderer may spell twice, so it is spelled once in `narrowingCaveat`.
//
// ⚠ `label` CARRIES ITS OWN TRAILING `/` — the trap `refToLine`'s header records, inherited
// here because this function is copied from it. Pass `SearchReport.Label()`, never
// `SearchReport.Scope`, which is the literal `(all scopes)` on a store-wide search.
// ⚠ IT TAKES NO `narrowedFurther`, AND THAT IS A PROPERTY OF THE FILTER ORDER RATHER THAN AN
// OMISSION. The tag filter runs LAST at both call sites, so nothing narrows its result again and
// the third reach state is unreachable here. A third filter added after it would have to pass
// one — and would also have to re-derive this line's denominator, which is the set the tag
// filter SAW rather than the readable total.
func tagLine(tag string, matched, total int, label string, narrowedSetShown bool) string {
	return "  tag: `" + tag + "` — " + strconv.Itoa(matched) + " of " +
		strconv.Itoa(total) + " entr" + entryPlural(total) + " in `" + label + "` carry it, " +
		reachClause(matched, narrowedSetShown, false) + narrowingCaveat
}

// narrowingCaveat is the clause both filter headers end with, spelled ONCE.
//
// 🔴 IT IS A CONSTANT RATHER THAN A REPEATED LITERAL BECAUSE IT IS THE PART THAT MUST NOT
// DIVERGE. The counts and the verbs differ between the two lines by design; this sentence is
// the reader's instruction — "the rest were read and did not match", which is what stops a
// narrowed index reading as a truncated one — and a second spelling of it is the one that would
// go stale in whichever line was edited second.
const narrowingCaveat = " This is a NARROWING, not a truncation: the rest were read and " +
	"did not match."

// themOrIt and doesOrDo agree with a COUNT the reader is being pointed at, not with the
// entry-total beside it in the same sentence. Spelled here beside their siblings because the
// `ref-to-absent` sentence puts both numbers in one clause — "1 of the 2 entries … DOES" — and
// an idiom inlined there would be the one that agrees with the wrong one.
func themOrIt(n int) string {
	if n == 1 {
		return "it"
	}
	return "them"
}

func doesOrDo(n int) string {
	if n == 1 {
		return "DOES"
	}
	return "DO"
}

// entryAboveIs is `entr{'y above is' if n == 1 else 'ies above are'}` — one idiom, spelled
// once, because it is the tail of two sentences that must agree.
func entryAboveIs(n int) string {
	if n == 1 {
		return "y above is"
	}
	return "ies above are"
}

func joinOrNone(values []string) string {
	if len(values) == 0 {
		return "(none)"
	}
	return strings.Join(values, ", ")
}
