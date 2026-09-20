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
		if r.BestBelow != nil {
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

// joinOrNoneWith is `', '.join(...) or '(none)'` — the empty join is the falsy string, not
// an empty list, which is why the fallback belongs to the JOINED value and not to the slice.
func joinOrNoneWith(values []string) string {
	joined := strings.Join(values, ", ")
	if joined == "" {
		return "(none)"
	}
	return joined
}
