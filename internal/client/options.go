package client

import (
	"strings"

	"github.com/ZacxDev/cairn/internal/report"
)

// Options is one parsed invocation. Every verb reads the same struct so a flag cannot mean two
// things depending on which subcommand consumed it.
type Options struct {
	Cache   string
	Timeout int

	Scope  string
	Repo   string
	NoSync bool

	Mode string
	// Ref / HasRef separate "no `--ref` was given" from "`--ref` with an empty value", which
	// still narrows and finds nothing. The reader's own `RecallOptions` carries the same pair
	// for the same reason.
	Ref    string
	HasRef bool
	List   bool
	// Limit / Page are pointers because `nil` is a DIFFERENT REQUEST from any integer: it is
	// `--limit` that selects full-body mode, and defaulting it at the call site would make
	// "the caller asked for a cap" indistinguishable from "the caller asked for nothing",
	// which is the distinction `mode` is derived from.
	Limit *int
	Page  *int

	Query     string
	AllScopes bool

	JSON bool

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
