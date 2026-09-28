package client

import (
	"strings"

	"github.com/ZacxDev/cairn/internal/report"
)

// Options is one parsed invocation. Every verb reads the same struct so a flag cannot mean two
// things depending on which subcommand consumed it.
type Options struct {
	Cache string
	// CacheExplicit records that `--cache` was GIVEN, not merely defaulted.
	//
	// 🔴 THE DEFAULT AND AN EXPLICIT VALUE EQUAL TO IT ARE DIFFERENT FACTS, and comparing the
	// value against the default cannot tell them apart. With more than one instance the
	// default must be resolved PER INSTANCE while an explicit `--cache` pins one directory,
	// so the difference decides whether a fan-out is allowed at all.
	CacheExplicit bool
	Timeout       int

	Scope  string
	Repo   string
	NoSync bool

	Mode string
	// Ref / HasRef separate "no `--ref` was given" from "`--ref` with an empty value", which
	// still narrows and finds nothing. The reader's own `RecallOptions` carries the same pair
	// for the same reason.
	Ref    string
	HasRef bool
	// RefTo / HasRefTo is the REVERSE lookup — `--ref-to <system>:<id>`, "only entries whose
	// `refs:` carries this". Same two-state discriminator, same reason. `--ref` names an
	// ENTRY and this names something an entry POINTS AT, so they compose rather than conflict.
	RefTo    string
	HasRefTo bool
	// Tags is the CATEGORY narrowing — `--tag <name>`, REPEATABLE, with AND semantics.
	//
	// 🔴 THE FIRST AND ONLY REPEATABLE FLAG IN THIS CLI, AND THE FIRST `[]string` ON THIS
	// STRUCT. Every other value-bearing flag here is last-wins by construction: the parser's
	// assignment switch writes a scalar, so `--limit 1 --limit 2` means 2 and always did. A flag
	// whose repetitions are its OPERAND SET cannot use that arm — it has to APPEND — and the
	// hazard is that the wrong arm is silent: `--tag a --tag b` would narrow by `b` alone and
	// answer 200 with more entries than were asked for.
	//
	// ⚠ NO `HasTags` COMPANION, WHERE `Ref` AND `RefTo` EACH HAVE ONE. Those need a flag because
	// `--ref ''` is a real request that narrows and finds nothing; a tag operand that folds away
	// is REFUSED by the option ladder, so there is no empty-but-present state to separate.
	// `report.RecallOptions.Tags` carries the same note.
	//
	// ⚠ OPERANDS AS WRITTEN, NOT FOLDED. `report.canonicalTags` is the one place they are folded,
	// deduped and sorted, so this client and the pod cannot disagree about what a query named.
	Tags []string
	List bool
	// Limit / Page are pointers because `nil` is a DIFFERENT REQUEST from any integer: it is
	// `--limit` that selects full-body mode, and defaulting it at the call site would make
	// "the caller asked for a cap" indistinguishable from "the caller asked for nothing",
	// which is the distinction `mode` is derived from.
	Limit *int
	Page  *int

	Query     string
	AllScopes bool

	JSON bool
	// Check is `routes --check`: grade the table rather than only printing it.
	Check bool

	Text    string
	Session string
	File    string
	IfMatch string
}

// Selection is recall's FLAGS mapped onto the reader's arguments.
type Selection struct {
	Mode  string
	Limit int
	Page  int
}

// RecallSelectionFor is the ONE place that mapping happens.
//
// 🔴 `--limit` IS WHAT SELECTS THE PRE-DIGEST FULL-BODY MODE, AND NOTHING ELSE DOES. Defaulting
// `limit` to the entry limit at the call site would make "the caller asked for a cap"
// indistinguishable from "the caller asked for nothing".
func RecallSelectionFor(list bool, limit, page *int) Selection {
	mode := report.DefaultMode
	switch {
	case list:
		mode = "list"
	case limit != nil:
		mode = "full"
	}
	sel := Selection{Mode: mode, Limit: report.DefaultEntryLimit, Page: 1}
	if limit != nil {
		sel.Limit = *limit
	}
	if page != nil {
		sel.Page = *page
	}
	return sel
}

// selectors is the three flags that select DIFFERENT THINGS, with what each selects.
//
// ⚠ `--search` IS ABSENT FROM THIS PORT'S TABLE, AND ITS ABSENCE IS NOT AN OMISSION. The oracle
// shares this predicate between its reader's own CLI (which has a `--search` flag) and the
// client's `recall` subcommand (which does not — `search` is a separate verb). The client can
// never supply `--search`, so a row for it here would be a branch no input reaches, and the
// message it would produce is unreachable. What IS preserved is the ORDER and the WORDING of the
// three refusals the client can actually produce.
var selectors = [][2]string{
	{"--ref", "one entry's body"},
	{"--list", "the whole index"},
}

// RejectRecallFlags is the flag combinations recall REFUSES, as a message — or "" if coherent.
//
// 🔴 REJECTED, NOT SILENTLY RECONCILED. Every combination below has an obvious "sensible" reading
// and they are DIFFERENT readings, so honouring one would give the caller output they did not ask
// for and no sign of it.
//
// 🔴 THE ORDER IS THE CONTRACT. A call carrying two incoherent pairs gets ONE message, and which
// one it gets is what a test over a single pair cannot see.
// ⚠ `hasRef` IS A FLAG, NOT `ref != ""`. The oracle tests `ref is not None`, so `--ref ''`
// COUNTS as a selector and refuses alongside `--list` — an empty ref still narrows and finds
// nothing, which is a different request from not passing one.
func RejectRecallFlags(hasRef bool, list bool, limit, page *int) string {
	var chosen []string
	what := map[string]string{}
	for _, row := range selectors {
		what[row[0]] = row[1]
		if (row[0] == "--ref" && hasRef) || (row[0] == "--list" && list) {
			chosen = append(chosen, row[0])
		}
	}
	if len(chosen) > 1 {
		descriptions := make([]string, 0, len(chosen))
		for _, flag := range chosen {
			descriptions = append(descriptions, what[flag])
		}
		return strings.Join(chosen, " and ") + " select different things (" +
			strings.Join(descriptions, " vs ") + "). Pass one."
	}
	if list && limit != nil {
		return "--limit is a cap on entry BODIES and --list prints none; " +
			"the index is never truncated. Drop one."
	}
	if page != nil && (hasRef || limit != nil) {
		return "--page pages the INDEX, and --ref/--limit print no index at all. Drop one."
	}
	return ""
}
