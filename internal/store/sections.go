package store

import (
	"strings"

	"github.com/ZacxDev/cairn/internal/pytext"
)

// HeadingBlock is one `(heading, body-lines)` pair of an entry file, in document
// order. `Heading` is "" and `IsPreamble` is true for the block BEFORE the first
// heading, so a caller can tell "the front matter and any prose above the first
// section" from a real section whose heading happens to be empty — which no ATX
// heading can be, but which a `""` sentinel alone would not distinguish.
type HeadingBlock struct {
	Heading    string
	IsPreamble bool
	Lines      []string
}

// HeadingBlocks splits an entry into `(heading, body-lines)` blocks.
//
// 🔴 THE ONE HEADING PARSER, mirroring the oracle's `_heading_blocks`, and the
// reason it is its own function is that it has more than one view over it:
// ExtractSections asks "which of these sections does the entry have, and what is in
// them" while a heading INVENTORY asks "what headings does it have at all". A second
// walker would be free to disagree with the first about what a heading IS, and the
// disagreement renders as "the section is absent" directly beside "the heading is
// right there".
//
// A heading is a line beginning with `#` at COLUMN 0, outside a fence; the block key
// is that line right-stripped and otherwise VERBATIM. Every line that is not such a
// heading — fence lines included — belongs to the block it sits in.
//
// 🔴 FENCED BLOCKS ARE SKIPPED. A `#` line inside a code fence is not a heading, and
// treating it as one would END the section early — surfacing HALF an entry's nuance
// while looking exactly like a complete read.
//
// 🔴 A REPEATED HEADING YIELDS A SEPARATE BLOCK EACH TIME, deliberately: merging
// here would destroy the evidence a duplicate-detecting caller needs before anyone
// could report it. ExtractSections is where the merge happens.
//
// ⚠ THIS IS A DIFFERENT NOTION OF "HEADING" FROM `entryBlocks`' IN THE REPORT
// RENDERER, AND BOTH ARE THE ORACLE'S. Here it is `line.startswith("#")`; there it is
// `^(#{1,6})\s+(.*\S)\s*$`, which refuses seven hashes and refuses a `#` with nothing
// after it. So `#######` opens a section for THIS parser and is ordinary text for the
// search splitter. Two parsers, two questions, both transcribed rather than unified —
// unifying them would be a behaviour change in a renderer whose output is pinned byte
// for byte.
func HeadingBlocks(text string) []HeadingBlock {
	blocks := []HeadingBlock{{IsPreamble: true}}
	inFence := false
	for _, line := range pytext.SplitLines(text) {
		if IsFence(line) {
			inFence = !inFence
			blocks[len(blocks)-1].Lines = append(blocks[len(blocks)-1].Lines, line)
			continue
		}
		if !inFence && strings.HasPrefix(line, "#") {
			blocks = append(blocks, HeadingBlock{
				Heading: strings.TrimRightFunc(line, pytext.IsSpace),
			})
			continue
		}
		blocks[len(blocks)-1].Lines = append(blocks[len(blocks)-1].Lines, line)
	}
	return blocks
}

// ExtractSections returns `{heading: body}` for each requested heading FOUND in
// `text`. A heading that is absent is simply not a key.
//
// 🔴 PRESENCE IS TRACKED SEPARATELY FROM CONTENT, which is why this returns a map a
// caller must probe with a two-value lookup rather than a map of every requested
// heading. A heading that appears with nothing under it is PRESENT-AND-EMPTY, not
// absent — "the section was never started" and "the section is there and unfilled"
// are different facts about a curated entry, and only one of them is a reason to go
// look somewhere else. Deriving presence from a non-empty body collapses them, and on
// the oracle it did: an empty section followed by another heading read as absent while
// the same empty section at end-of-file read as present, purely because of what came
// after it.
//
// Bodies are VERBATIM with surrounding BLANK LINES trimmed — `strip("\n")` on the
// oracle, which is why the trim cuts newlines only and leaves a body's own leading
// indentation alone.
//
// Matching is on the EXACT heading string, not a normalized one: these are schema
// headings, not user refs, and normalizing them would fold `## Pointers` and
// `## pointers!` together and quietly widen what the store may look like.
//
// A heading written TWICE has its blocks CONCATENATED under the one key, and whatever
// sat under an intervening heading is dropped. That is a silent merge, and it is why a
// duplicate-heading report must come from a heading inventory rather than from this
// mapping, which cannot show one.
func ExtractSections(text string, headings []string) map[string]string {
	wanted := make(map[string][]string, len(headings))
	for _, h := range headings {
		wanted[h] = nil
	}
	seen := map[string]bool{}
	for _, block := range HeadingBlocks(text) {
		if block.IsPreamble {
			continue
		}
		if _, isWanted := wanted[block.Heading]; !isWanted {
			continue
		}
		seen[block.Heading] = true
		wanted[block.Heading] = append(wanted[block.Heading], block.Lines...)
	}
	out := make(map[string]string, len(seen))
	for _, h := range headings {
		if seen[h] {
			out[h] = strings.Trim(strings.Join(wanted[h], "\n"), "\n")
		}
	}
	return out
}
