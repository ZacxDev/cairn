package report

import (
	"strconv"
	"strings"

	"github.com/ZacxDev/cairn/internal/hostid"
	"github.com/ZacxDev/cairn/internal/store"
)

// RenderText is the agent-facing search block. Deterministic: same report in, same bytes
// out, with the single exception every report shares — the host line names THIS machine.
//
// `extraHeader` has the same contract as the recall renderer's: a CLI's read-store stamp
// belongs in the header block, and a search is a read like any other, so it carries its own
// freshness too. So does `instance`: `""` is the single-instance case the pod and every
// one-instance host pass, and an alias adds the caveat's multi-instance clause. See
// `RecallReport.RenderText` for why that string is the whole of this package's instance
// awareness.
func (r SearchReport) RenderText(host string, extraHeader []string, instance string) string {
	ctx := "bullet"
	if r.Context != ContextBullet {
		ctx = "±" + strconv.Itoa(r.Context) + " raw lines"
	}
	out := []string{
		"subsystem-recall: status=" + r.Status + " scope=" + r.Scope +
			" query=" + store.PyRepr(r.Query) +
			" threshold=" + twoPlaces(r.Threshold) + " context=" + ctx,
		"  store: " + r.StoreRoot,
		hostid.StoreHostLine(host, "  ", instance),
	}
	out = append(out, extraHeader...)
	out = append(out, "  caveat: "+r.Caveat())

	// Same reasoning as the recall renderer's: the narrowing announces itself, because the
	// "searched N entries" count below is about the narrowed set and a zero from a filter that
	// matched nothing is otherwise indistinguishable from a zero from an empty store.
	// `RefToSkipped` + `EntriesSearched` is the pre-filter total, so the line does not need a
	// third field to carry it.
	if r.HasRefTo {
		// A CONSTANT `true`. THE NUMERATOR IS WHY IT CAN BE ONE FOR THE COUNT; THE SECOND
		// CLAUSE IS CARRIED BY AN ENUMERATION AND BY NOTHING SHORTER.
		//
		// The count: `EntriesSearched` is incremented once per entry the filter KEPT, so the
		// number printed here is the kept count by construction. The recall renderer's
		// problem — a count of matched entries rendered over a body about none of them — has
		// no spelling here on the count side.
		//
		// The clause "everything below is about those N" is a claim about the BODY, and the
		// only thing that carries it is an enumeration of what can BE below it. This line
		// sits above exactly three statuses — `scope-absent` returns before the operand is
		// parsed, so it never carries one — and `search-no-match` has THREE distinguishable
		// bodies, which makes FIVE cases. ⚠ IT SAID "two bodies / FOUR cases" AND THAT WAS
		// MEASURED INCOMPLETE: `N == 0` is TWO bodies, not one, and the second is live in this
		// change's own `tests/dualrun/` gate. The paragraph below says a new body must be added
		// to this list and checked BY HAND — so an incomplete list returns a false clean, which
		// is why the fourth row exists rather than being folded into the third. Each was
		// re-derived rather than covered by an absolute:
		//
		//   - `search-hit` — the hunks below come from kept entries only, because
		//     `entryHunks` is called on the kept slice and on nothing else.
		//   - `search-no-match` with N > 0 — the body is "searched N entries in <scopes>,
		//     and nothing cleared the threshold", plus a near miss that is itself a hunk
		//     from a kept entry. A statement about exactly those N.
		//   - `search-no-match` with N == 0 and `RefToSkipped > 0` — VACUOUS, not false: the
		//     body says the scan found nothing because the FILTER removed every readable entry
		//     (the `RefToSkipped > 0` case of the no-match branch below). Nothing under the
		//     line reports on a REMOVED entry as though it had been kept, which is what the
		//     clause would have to be false about.
		//   - `search-no-match` with N == 0 and `RefToSkipped == 0` — a PRESENT-BUT-EMPTY scope
		//     asked with `?ref-to=`. Nothing existed to keep OR to skip, so the `switch` falls
		//     through to the DEFAULT `near` and prints "No candidate scored above zero at all,
		//     so this is an absent term rather than a weak one." over a "0 of 0" line. Vacuous
		//     for the same reason as the row above — there is no removed entry for anything
		//     below to misreport — but it reaches the branch by a DIFFERENT mechanism, and the
		//     ⚠ note at the no-match branch below is what owns that route. Measured live on
		//     both servers, identical bytes: `genstore.py`'s empty scope is one of the
		//     `search-ref-to` dualrun targets.
		//
		// ⚠ THE FIFTH CASE IS `search-unreadable`, and after the fix it requires
		// `RefToSkipped == 0` as well as `EntriesSearched == 0` — so the line there reads
		// "0 of 0", and the clause is about the empty set over a pre-filter set that was
		// also empty. ⚠ "0 of 0" IS THEREFORE NOT THAT STATUS'S SIGNATURE: the fourth row
		// prints it too, and the STATUS is what tells them apart. 🔴 NONE OF THAT IS WHY THE
		// PRE-FIX COMMENT WAS WRONG, AND THE DIFFERENCE IS THE POINT: it reasoned only about
		// the CLAUSE ("about the empty set rather than false about a non-empty one") and was
		// literally true while the STATUS it annotated was false about the store. Re-deriving
		// a clause says nothing about the branch that reaches it.
		//
		// 🔴 AND THE ENUMERATION IS THE WHOLE JUSTIFICATION, WHICH IS WEAKER THAN A GUARD AND
		// IS SAID SO RATHER THAN DRESSED UP. It is PROSE: nothing mechanical asserts that the
		// set of statuses reachable under this line is still those three, the way
		// `renders_narrowed_set`'s table is now asserted over all four render shapes. A new
		// `search-*` status, or a new body under an existing one, has to be added to this list
		// and checked BY HAND today — or the constant has to become a predicate the way the
		// recall renderer's `RendersNarrowedSet` is.
		//
		// 🔴 BOTH NUMBERS MOVED WHEN THE TAG FILTER LANDED, AND LEAVING THEM WOULD HAVE BEEN A
		// FALSE SENTENCE IN BOTH HALVES. The numerator is "entries that reference this", which is
		// the set the `ref-to` filter KEPT — `EntriesSearched` alone once WAS that set and is not
		// any more, because the tag filter removes some of it downstream. The denominator is the
		// readable set, which now partitions three ways. So: kept-by-ref-to is
		// `EntriesSearched + TagSkipped`, and readable is all three counters. With no tag filter
		// `TagSkipped` is 0 and this line is byte-identical to what it was, which is what keeps
		// `search-ref-to-*` goldens still.
		out = append(out, refToLine(r.RefTo, r.EntriesSearched+r.TagSkipped,
			r.EntriesSearched+r.RefToSkipped+r.TagSkipped, r.Label(), true, r.TagSkipped > 0))
	}
	// The CATEGORY narrowing, after the reverse-lookup line because that is the order the
	// filters ran in.
	//
	// 🔴 THE CONSTANT `true` IS JUSTIFIED BY THE SAME ENUMERATION AND THE ENUMERATION IS NOW
	// WIDER, WHICH IS THE COST THIS LINE ADDS TO A PROSE JUSTIFICATION. The count half is safe
	// by construction for the same reason: `EntriesSearched` counts entries the filters KEPT,
	// so the numerator here is the kept count whatever removed the rest. The clause half —
	// "everything below is about those N" — is carried by the list above, and a `tag` filter
	// adds no new STATUS and no new BODY to it: every shape it can produce is a shape
	// `ref-to` can already produce, because the two filters differ only in their predicate and
	// both run over `ordered` inside the same scope loop. That is why this line does not
	// lengthen the list; it is also exactly the kind of claim the list's own last paragraph
	// says nothing mechanical checks.
	//
	// 🔴 THE DENOMINATOR IS THE SET THIS FILTER SAW, NOT THE READABLE TOTAL, AND THE TWO DIFFER
	// EXACTLY WHEN A `ref-to` FILTER ALSO RAN. `EntriesSearched + TagSkipped` is the set the tag
	// filter was handed; `+ RefToSkipped` would be the readable set, and using it would print
	// "2 of 7 entries carry it" while the filter had only ever LOOKED at 3 — a claim about five
	// entries whose tags were never read, in the direction that understates the category. The
	// recall side has the same shape and says so: `TagScopeTotal` is assigned AFTER the
	// reverse-lookup narrowing, not before it.
	if r.Tag != "" {
		out = append(out, tagLine(r.Tag, r.EntriesSearched, r.EntriesSearched+r.TagSkipped,
			r.Label(), true))
	}

	// Same rule as the recall renderer: before every branch, on every status.
	out = append(out, renderMalformed(r.Malformed, r.MalformedElsewhere, r.Label())...)

	switch r.Status {
	case StatusSearchUnreadable:
		out = append(out, "", unreadableSummary(r.Label(), r.Malformed))
		out = append(out, "  The query "+store.PyRepr(r.Query)+
			" was never run against anything — this is NOT 'no matches'.")
		return strings.Join(out, "\n")

	case StatusScopeAbsent:
		out = append(out, "")
		out = append(out, "NOTHING RECORDED YET ON THIS HOST — "+host+"'s store has no `"+
			r.Scope+"/` directory, so the query was never run. This is NOT 'no matches': "+
			"nothing was searched, so nothing can be concluded from it.")
		out = append(out, "  NOT A FACT ABOUT THE FLEET — "+hostid.StoreCaveat(instance)+". The "+
			"other host syncs the SAME hosted store through its own cache, and may already "+
			"hold `"+r.Scope+"/` where this one has not synced it yet.")
		out = append(out, "  scopes THIS HOST's store holds: "+joinOrNone(r.KnownScopes))
		return strings.Join(out, "\n")
	}

	quoted := make([]string, 0, len(r.ScopesSearched))
	for _, s := range r.ScopesSearched {
		quoted = append(quoted, "`"+s+"/`")
	}
	scanned := strconv.Itoa(r.EntriesSearched) + " entr" + entryPlural(r.EntriesSearched) +
		" in " + joinOrNoneWith(quoted)

	if r.Status == StatusSearchNoMatch {
		out = append(out, "")
		// 🔴 THE ZERO CARRIES ITS OWN EVIDENCE: how much was scanned (so a zero from an
		// empty scan is visible), and the best NEAR miss with its score (so "matched
		// nothing" and "just missed" are distinguishable).
		near := " No candidate scored above zero at all, so this is an absent term rather " +
			"than a weak one."
		switch {
		case r.BestBelow != nil:
			// `max(0.0, score - 0.01)` — the suggested threshold never goes negative, and
			// the subtraction happens on the ALREADY-ROUNDED score, which is what the oracle
			// does and therefore what the suggested value has to be derived from.
			suggest := r.BestBelow.Score - 0.01
			if suggest < 0.0 {
				suggest = 0.0
			}
			near = " The closest candidate was `" + r.BestBelow.Ref + "` at " +
				twoPlaces(r.BestBelow.Score) + ", below the " + twoPlaces(r.Threshold) +
				" threshold — re-run with `--threshold " + twoPlaces(suggest) +
				"` to see it, or rephrase."

		case r.EntriesSearched == 0 && (r.RefToSkipped > 0 || r.TagSkipped > 0):
			// 🔴 THE FILTER'S ZERO, SAID AS THE FILTER'S. With nothing scanned, "an absent
			// term rather than a weak one" is a claim about the QUERY that no comparison was
			// made to support — the same defect `BestBelow` exists to refuse, one branch
			// over. This is the shape the `ref-to` filter introduced: every readable entry
			// was read and indexed, and then removed by the narrowing.
			//
			// ⚠ ORDERED AFTER `BestBelow` AND THE ORDER CANNOT MATTER: a near miss is a
			// hunk from a scanned entry, so `BestBelow != nil` and `EntriesSearched == 0`
			// cannot both hold. A `switch` rather than two `if`s so a later edit does not
			// have to re-derive that.
			//
			// ⚠ REACHABLE ONLY WITH A FILTER THAT REMOVED SOMETHING, DELIBERATELY. The
			// zero-scanned sentence a present-but-empty scope prints is a DIFFERENT
			// mechanism reaching the same branch, it predates the filter, and it is left
			// exactly as it was rather than swept in here.
			//
			// 🔴 IT NAMES WHICH FILTER, AND WITH TWO OF THEM THAT STOPPED BEING A CONSTANT. A
			// sentence hardcoding "`ref-to`" would send a reader who typed only `--tag` to check
			// an operand they never gave — the empty-result hazard one level up, where the
			// diagnosis is wrong rather than absent. Two filters make three subjects, and both
			// halves of the sentence have to agree with the one chosen.
			subject, possessive := emptyingFilters(r.RefToSkipped, r.TagSkipped)
			near = " Nothing was scanned: " + subject + " removed every readable entry, " +
				"so this zero is " + possessive + " and says nothing about the query."
		}
		out = append(out, "NO MATCH — searched "+scanned+", and nothing cleared the threshold."+near)
		return strings.Join(out, "\n")
	}

	out = append(out, "")
	out = append(out, "SEARCH ("+RecallLabel+") — "+strconv.Itoa(len(r.Hunks))+" of "+
		strconv.Itoa(r.TotalHits)+" hunk"+plural(r.TotalHits)+" at or above "+
		twoPlaces(r.Threshold)+", from "+scanned+":")
	for _, h := range r.Hunks {
		out = append(out, "")
		out = append(out, "  ["+twoPlaces(h.Score)+" "+h.Basis+"] "+h.Scope+"/"+h.Ref+"  "+
			h.Section+"  ("+h.Scope+"/"+h.Filename+":"+strconv.Itoa(h.Start)+"-"+
			strconv.Itoa(h.End())+", sensitivity="+
			SensitivityLabel(h.Sensitivity, h.DeclaredSensitivity)+")")
		for _, line := range h.Lines {
			out = append(out, "    "+line)
		}
	}

	if omitted := r.Omitted(); omitted > 0 {
		out = append(out, "")
		out = append(out, "… "+strconv.Itoa(omitted)+" further hunk"+plural(omitted)+
			" cleared the threshold and were NOT shown (--max-hits "+strconv.Itoa(r.MaxHits)+
			"). This is a display cap, not a judgement about relevance — raise it to see the rest.")
	}
	return strings.Join(out, "\n")
}

// twoPlaces is `format(x, '.2f')`.
//
// ⚠ IT IS NOT A COSMETIC CHOICE OF VERB. Both implementations convert the double to decimal
// with correct rounding and resolve a tie to EVEN, so `0.125` prints `0.12` on both; a
// hand-rolled `math.Round(x*100)/100` would not, and the number reaches a rendered line
// that is compared byte for byte.
func twoPlaces(x float64) string {
	return strconv.FormatFloat(x, 'f', 2, 64)
}

// emptyingFilters names the filter or filters that drove the searched set to zero, plus the
// possessive that agrees with it.
//
// 🔴 TWO RETURN VALUES BECAUSE ONE SENTENCE NAMES THE SUBJECT TWICE. "the `ref-to` filter
// removed every readable entry, so this zero is the FILTER's" — the second half is a possessive
// of the first, and picking one plural form per site is how the two stop agreeing.
//
// ⚠ IT IS NEVER CALLED WITH BOTH ZERO. Its one call site is inside a branch guarded on
// `RefToSkipped > 0 || TagSkipped > 0`, so the fall-through is unreachable — but it returns the
// `ref-to` wording rather than panicking, because the alternative to an unreachable branch is a
// renderer that can crash on a report it was handed. Stated so nobody reads the fall-through as
// a claim that `ref-to` is the default filter.
func emptyingFilters(refToSkipped, tagSkipped int) (subject, possessive string) {
	switch {
	case refToSkipped > 0 && tagSkipped > 0:
		return "the `ref-to` and `tag` filters", "the FILTERS'"
	case tagSkipped > 0:
		return "the `tag` filter", "the FILTER's"
	default:
		return "the `ref-to` filter", "the FILTER's"
	}
}

// joinOrNoneWith is `', '.join(...) or '(none)'` — the empty join is the falsy string, not
// an empty list, which is why the fallback belongs to the JOINED value and not to the slice.
func joinOrNoneWith(values []string) string {
	joined := strings.Join(values, ", ")
	if joined == "" {
		return "(none)"
	}
	return joined
}
