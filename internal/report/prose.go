package report

import (
	"sort"
	"strconv"
	"strings"

	"github.com/ZacxDev/cairn/internal/pytext"
	"github.com/ZacxDev/cairn/internal/store"
)

// RecallLabel is the word `/analyze-service` uses, REUSED VERBATIM. Its brief says
// nuance and pointers are `from index` while location and config are re-derived live,
// and "never present index recall as live observation". A reader that invented its own
// label would put two spellings of one provenance claim in front of the same agent, in
// the same session, for the same store.
const RecallLabel = "from index"

// SurfacedHeadings are the sections a printed BODY renders, in the order it renders
// them.
//
// 🔴 `## What it is` USED TO BE EXCLUDED AND THE EXCLUSION WAS WRONG. The stated reason
// was "one line of durable boilerplate", and measured against the oracle's live store
// both halves fail: it is not one line (73 of 73 entries carry it, median 3 lines), and
// NO BRIEFING PATH printed it — so an agent briefed only on an entry could not say what
// the service WAS. It is rendered FIRST because it is the orienting sentence.
var SurfacedHeadings = []string{store.WhatHeading, store.PointersHeading, store.NuanceHeading}

// CountedHeadings is THE SET WHOSE ABSENCE MAKES A NUMBER WRONG — a strictly different
// question from "what does a body print", and the two are kept apart rather than merged.
//
// `missingSections`, `isBare`, the index row's `🔴 NO <heading>` badge and the caveat
// clause explaining that badge all key off THIS list. Their shared meaning is "the
// parser never reached the bullets, so `0 nuance` and a missing `OPEN` badge on that row
// are a PARSE FAILURE and not an empty entry" — a claim about counts. `## What it is`
// feeds no count and no badge, so widening this would put a heading with no numeric
// consequence beside two whose consequence is measured, and grow the one line printed
// for EVERY entry.
var CountedHeadings = []string{store.PointersHeading, store.NuanceHeading}

// The sensitivity vocabulary. 🔴 FAIL-SAFE: absent OR unrecognized ⇒
// `client-confidential`, never public. `public` is a deliberate operator claim a recon
// run may never infer, and this reader's whole job is to put curated,
// client-identifying content in front of an agent that may be one paste away from a
// PUBLIC repo — the marker is the only thing that says so.
const SensitivityFailSafe = "client-confidential"

var knownSensitivities = []string{"client-confidential", "personal", "public"}

// The statuses. They are ONE vocabulary shared by both report types rather than two,
// because a caller must be able to switch on ONE status field — and no two of them share
// a spelling, so a reader cannot half-match.
const (
	StatusScopeAbsent      = "scope-absent"
	StatusScopeUnreadable  = "scope-unreadable"
	StatusScopeEmpty       = "scope-empty"
	StatusRefAmbiguous     = "ref-ambiguous"
	StatusRefAbsent        = "ref-absent"
	StatusRecalled         = "recalled"
	StatusSearchHit        = "search-hit"
	StatusSearchNoMatch    = "search-no-match"
	StatusSearchUnreadable = "search-unreadable"
)

// UnreadableStatuses is THE ONE PLACE THE EXIT CODE IS DECIDED, for both report types —
// callers switch on membership here rather than on either status by name, so a third
// "nothing could be read" outcome cannot be added and silently exit 0.
//
// WHY THESE TWO AND NOTHING ELSE — the rule is CONTENT SERVED, not "was anything wrong".
// A consumer's contract is "if it exits non-zero, print the stderr line, note that
// recall was unavailable, and continue", and a non-zero throws away every entry the run
// DID surface. A scope with 2 good entries and 1 malformed therefore exits 0 with a loud
// in-band MALFORMED block: recall was available, and it was also honest about what it
// could not read.
var UnreadableStatuses = []string{StatusScopeUnreadable, StatusSearchUnreadable}

// The conditional badge kinds whose EXPLANATION is emitted only when that badge is on
// screen. `OPEN` is deliberately NOT here — see CaveatText.
const (
	badgeNearMiss       = "near-miss"
	badgeUnverifiable   = "unverifiable"
	badgeMissingHeading = "missing-heading"
)

// noBadges is an EMPTY badge set, which is not the same thing as NO badge set.
//
// 🔴 THE NIL/EMPTY DISTINCTION IS THE CONTRACT, AND IT IS THE ONE A GO PORT WOULD LOSE
// BY ACCIDENT. `nil` means "the caller did not compute a set" and yields the FULL caveat
// — fail-safe toward saying MORE. An empty non-nil set means "computed, and no badge can
// appear", which is a claim `SearchReport` really can make: search prints matched
// excerpts and never an index row, so no badge can appear in its output.
func noBadges() map[string]bool { return map[string]bool{} }

// badgesPresent is WHICH conditional badge kinds a report will actually render.
//
// 🔴 READ OFF THE ENTRIES THE REPORT IS ABOUT, never off the store. A caveat that
// described the store rather than this output would explain a badge the reader cannot
// see, which is the whole defect this exists to fix.
func badgesPresent(entries []RecalledEntry) map[string]bool {
	kinds := map[string]bool{}
	for _, e := range entries {
		if e.NearMissCount != 0 {
			kinds[badgeNearMiss] = true
		}
		if e.UnverifiableCount != 0 {
			kinds[badgeUnverifiable] = true
		}
		if len(e.MissingSections) != 0 {
			kinds[badgeMissingHeading] = true
		}
	}
	return kinds
}

// CaveatText is what every window this package opens can and cannot see. ONE spelling.
//
// 🔴 A PACKAGE-LEVEL FUNCTION rather than a method on one report, because there are TWO
// report types and a caveat spelled twice is wrong in one of them.
//
// 🔴 THE BADGE EXPLANATIONS ARE CONDITIONAL; EVERY WARNING ABOUT AN ABSENCE IS NOT.
// Measured on the oracle's first real session with this flow: the caveat is ~1,500
// characters and is paid PER CALL, so a targeted `--ref` lookup — the cheap operation
// this design encourages — spent 27% of its output explaining badges its output
// contained NONE of.
//
// 🔴 `OPEN` STAYS UNCONDITIONAL, AND THE ASYMMETRY IS THE POINT. Its clause is not "here
// is what this badge means"; it ends "the absence of that marker means nothing was
// declared, NOT that nothing is open." That is a warning about a MISSING badge, so
// gating it on a badge being present would delete it in exactly the case it was written
// for. Same test for anything added later: if the sentence is only true when the reader
// can SEE the badge, gate it; if it warns about what the reader CANNOT see, it is
// unconditional.
func CaveatText(scope string, badges map[string]bool) string {
	showAll := badges == nil

	var clauses []string
	if showAll || badges[badgeNearMiss] {
		clauses = append(clauses, "`🔴 N NEAR-MISS` — N bullets TRIED to write a marker "+
			"and missed the grammar, so they declare nothing and `N OPEN` is short by up to N")
	}
	if showAll || badges[badgeUnverifiable] {
		clauses = append(clauses, "`⚠ N UNVERIFIABLE` — N `RESOLVED:` bullets name no sha, "+
			"so the closure cannot be checked")
	}
	if showAll || badges[badgeMissingHeading] {
		clauses = append(clauses, "`🔴 NO <heading>` — that heading is absent or renamed, "+
			"so `N nuance` and every openness count on that row are 0 BY PARSE FAILURE and "+
			"not by measurement, and the entry's content is on disk but invisible to this read")
	}
	optional := ""
	if len(clauses) > 0 {
		lead := "One further badge says"
		switch len(clauses) {
		case 3:
			lead = "Three further badges say"
		case 2:
			lead = "Two further badges say"
		}
		optional = " " + lead + " the row's own numbers cannot be trusted: " +
			strings.Join(clauses, "; ") + "."
	}

	return RecallLabel + " — RECALL, NOT LIVE OBSERVATION. These are notes curated by " +
		"PAST sessions in the local store under `" + scope + "`. Nothing here was " +
		"re-derived just now, nothing was matched against anything THIS session has " +
		"done, and an entry is exactly as fresh as the last time someone pruned it " +
		"(prune-on-resolve is manual), so a bullet may describe a gotcha already " +
		"fixed. This window CANNOT see: live state of any kind, any repo whose scope " +
		"has no directory in THIS HOST's store, and any work neither `/analyze-service` nor " +
		"`/handoff` ever recorded. Treat every line as a POINTER to verify, never as " +
		"a current reading. This window read a LOCAL CACHE of the hosted store, not " +
		"the store itself, so it is only as complete as the last `cairn sync` on THIS " +
		"machine: an entry written on the OTHER machine that has not synced here yet " +
		"is invisible, and an absence below is an absence AS OF THAT SYNC, not a fact " +
		"about the store. " +
		"`🔴 N OPEN` on an index row means N bullets DECLARE " +
		"unfinished business — re-check each against the repo, because a remedy that " +
		"has since landed reads exactly like one that has not; the absence of that " +
		"marker means nothing was declared, NOT that nothing is open." +
		optional + " Sensitivity is " +
		"marked per entry; absent means `" + SensitivityFailSafe + "` — never copy an " +
		"entry's content into a public repo."
}

// ScopeLabel is `a/, b/` — how a set of scopes is named in prose. ONE spelling.
//
// The caveat, the search header and the unreadable summary all need it, and two of them
// used to build it inline in slightly different ways. A label spelled at N sites is
// wrong at N−1 of them, and this one sits inside a sentence that says what the tool
// could not see.
func ScopeLabel(scopes []string) string {
	if len(scopes) == 0 {
		return "(no scope)"
	}
	return strings.Join(scopes, "/, ") + "/"
}

// unreadableSummary is the ONE sentence that says "there is content here and none of it
// could be read".
//
// 🔴 IT MUST SHARE NO PHRASE WITH THE EMPTY CASE. The `scope-empty` branch opens
// `NOTHING RECORDED YET`; this opens `NOTHING COULD BE READ`, and the two are different
// facts with opposite next actions — carry on versus fix the store. A consumer reports
// `scope-empty` as an ordinary non-finding, so a shared opening word is all it would
// take for a broken store to be read as an empty one.
func unreadableSummary(label string, malformed []store.MalformedEntry) string {
	n := len(malformed)
	return "NOTHING COULD BE READ — `" + label + "` holds " + strconv.Itoa(n) + " entry file" +
		plural(n) + " and NOT ONE of them could be indexed. This is NOT an empty scope and " +
		"NOT 'nothing recorded yet': there is content here and the tool cannot see it. " +
		"Every reason is listed above; fix the file(s) and re-run."
}

// renderMalformed is the MALFORMED block — per entry, named, with its reason. Empty when
// clean.
//
// 🔴 ONE RENDERER, PRINTED ON EVERY STATUS BY BOTH SURFACES, IMMEDIATELY AFTER THE
// CAVEAT. Not a footer and not a branch: a reject that renders only on the paths somebody
// remembered is a reject that will be missed on the path they did not, and
// `scope-absent` — the most common status in most repos — is exactly where a store-wide
// defect would otherwise never be mentioned.
//
// `elsewhere` is a COUNT with its scopes named, never full rows. A reader is
// scope-scoped, so a broken entry in a scope nobody recalls today is invisible until
// someone does; naming the scopes makes it actionable without putting another scope's
// filenames — which are client-identifying — on this screen.
func renderMalformed(malformed, elsewhere []store.MalformedEntry, label string) []string {
	var out []string
	if len(malformed) > 0 {
		n := len(malformed)
		out = append(out, "")
		out = append(out, "🔴 MALFORMED — "+strconv.Itoa(n)+" entry file"+plural(n)+" in `"+
			label+"` could NOT be indexed and "+isAre(n)+" therefore absent from EVERYTHING "+
			"below (the index, --ref, --search). Not dropped, not hidden: listed here, once each.")
		for _, m := range malformed {
			out = append(out, "  "+m.Line())
		}
		out = append(out, "  (A STORE DEFECT, not an absence of content. Front matter is "+
			"parsed LINE BY LINE, so the usual cause is a value wrapped across two physical "+
			"lines — an `aliases: [...]` list in particular must be on ONE line. Check a scope "+
			"with `cairn validate --scope <scope>`, which names each file that fails to parse.)")
	}
	if len(elsewhere) > 0 {
		byScope := map[string]int{}
		for _, m := range elsewhere {
			key := m.Scope
			if key == "" {
				key = "(no scope)"
			}
			byScope[key]++
		}
		names := make([]string, 0, len(byScope))
		for scope := range byScope {
			names = append(names, scope)
		}
		sort.Strings(names)
		parts := make([]string, 0, len(names))
		for _, scope := range names {
			parts = append(parts, scope+" ("+strconv.Itoa(byScope[scope])+")")
		}
		k := len(elsewhere)
		if len(malformed) == 0 {
			out = append(out, "")
		}
		entryWord := "ies"
		if k == 1 {
			entryWord = "y"
		}
		out = append(out, "  (+"+strconv.Itoa(k)+" further malformed entr"+entryWord+
			" in OTHER scopes of this store, not shown: "+strings.Join(parts, ", ")+
			". Nothing on this screen is affected by them; they are named so a defect in a "+
			"scope nobody recalls today is still visible.)")
	}
	return out
}

// FoldSensitivity applies the schema's fail-safe: absent OR unrecognized ⇒
// client-confidential. Every path that is not an exact, known, operator-written value
// folds to the sensitive end.
func FoldSensitivity(raw string, present bool) string {
	if present {
		v := pytext.Lower(pytext.StripWhitespace(raw))
		for _, known := range knownSensitivities {
			if v == known {
				return v
			}
		}
	}
	return SensitivityFailSafe
}

// DiscardedSensitivity is the marker a file DECLARED and FoldSensitivity overrode, if
// any.
//
// 🔴 THE FOLD IS NEVER WEAKENED — this only makes it VISIBLE. There is a real difference
// between the two inputs that both render as `client-confidential`: nobody said anything
// (the ordinary case, where the default is the whole answer), and somebody WROTE a value
// the schema does not know. Silently rewriting the second reads as "the file says
// client-confidential", which it does not — and the author who typed it gets no signal
// that the schema does not know the word.
//
// DERIVED from FoldSensitivity rather than re-testing membership, so the two cannot
// drift into disagreeing about which values are known.
func DiscardedSensitivity(raw string, present bool) string {
	if !present {
		return ""
	}
	written := pytext.StripWhitespace(raw)
	if written == "" {
		return ""
	}
	if FoldSensitivity(raw, true) == pytext.Lower(written) {
		return ""
	}
	return written
}

// SensitivityLabel is `<effective>` — or `<effective> (declared: <x>)` when a marker was
// overridden.
//
// ONE spelling, taking the two VALUES rather than a report type, because THREE surfaces
// print it — the index row, the featured-entry header and the search hunk — and only two
// of them hold a RecalledEntry.
func SensitivityLabel(effective, declared string) string {
	if declared == "" {
		return effective
	}
	return effective + " (declared: " + declared + ")"
}

// shortHeading is `## Nuance / work-history` → `Nuance / work-history`. ONE spelling.
//
// The index row names a heading in ~60 bytes of budget, so it drops the ATX marker the
// printed body keeps. It still names the heading in FULL — `NO Pointers` and
// `NO Nuance / work-history` are different facts with different next actions, and a
// badge saying only `NO SECTION` would make them one.
func shortHeading(heading string) string {
	return pytext.StripWhitespace(strings.TrimLeft(heading, "#"))
}

// plural is the `{” if n == 1 else 's'}` idiom, which appears often enough that
// spelling it inline is where a typo hides.
func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// entryPlural is `entr{'y' if n == 1 else 'ies'}`'s tail.
func entryPlural(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

func isAre(n int) string {
	if n == 1 {
		return "is"
	}
	return "are"
}
