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
func (r RecallReport) RenderText(host string, extraHeader []string) string {
	out := []string{
		"subsystem-recall: status=" + r.Status + " scope=" + r.Scope,
		"  store: " + r.StoreRoot,
		hostid.StoreHostLine(host, "  "),
	}
	out = append(out, extraHeader...)
	out = append(out, "  caveat: "+r.Caveat())

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
		out = append(out, "  NOT A FACT ABOUT THE FLEET — "+hostid.StoreIsPerHost+". The "+
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
			out = append(out, "    tasks: "+strings.Join(e.Tasks, ", "))
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
