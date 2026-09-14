package store

import (
	"regexp"
	"strings"

	"github.com/ZacxDev/cairn/internal/pytext"
)

// ⚠ The `\A` is REDUNDANT-BUT-KEPT, labelled so a sweep does not re-derive it:
// Go's `FindStringSubmatch` searches, and `\A` is what makes "front matter must
// be at the TOP of the file" readable at the pattern rather than at the call
// site. Unlike the Python side — where `.match()` already anchors — here it is
// load-bearing as well as legible: without it a `---` block in the MIDDLE of an
// entry would parse as that entry's front matter.
var frontMatterRe = regexp.MustCompile(`(?s)\A---[ \t]*\r?\n(.*?)\r?\n---[ \t]*(?:\r?\n|\z)`)

// FrontMatter is one entry's `---` block. A value is either a string or a
// []string; nothing else, because the schema's values are ALWAYS strings and
// YAML's implicit typing is actively wrong for it (`service: no` is the boolean
// False in YAML, `service: 1.0` a float, an alias `on`/`yes`/`off` a bool — every
// one of which then fails to normalize as a string).
type FrontMatter map[string]any

// String reads one key as a scalar. `ok` is false when the key is absent OR
// holds a list, which is the distinction every consumer needs: the Python side
// guards each read with `isinstance(..., str)` and degrades rather than raising,
// and this returns the same two answers.
func (fm FrontMatter) String(key string) (string, bool) {
	v, present := fm[key]
	if !present {
		return "", false
	}
	s, isStr := v.(string)
	return s, isStr
}

// List reads one key as a sequence. `ok` is false when the key is absent or
// holds a scalar.
func (fm FrontMatter) List(key string) ([]string, bool) {
	v, present := fm[key]
	if !present {
		return nil, false
	}
	l, isList := v.([]string)
	return l, isList
}

// ParseFrontMatter parses the leading `---` block into a string/list-of-string
// map. Hand-rolled on the Python side for two reasons that both survive the
// port: YAML's implicit typing is wrong for this schema (above), and the reader
// must have no third-party dependency in the serving path.
//
// Handles the three shapes: `key: value`, an inline flow list
// `key: [a, b, c]`, and a block list (`key:` on its own line followed by `- item`
// lines). Quotes are stripped. Unknown keys are preserved so a caller can see
// them; entry validation ignores what it does not need.
//
// 🔴 THE BLOCK FORM IS PARSED BECAUSE NOT PARSING IT CORRUPTED THE MAPPING —
// it was never merely "ignored". Measured on the Python parser before it was
// widened, `tasks:` followed by two `  - <system>:<id>` lines produced the key
// silently EMPTY and EVERY item promoted to a phantom front-matter key by its own
// internal colon. A ref-shaped item always has a colon, so the promotion is the
// rule there rather than the exception; an item WITHOUT one is instead dropped
// silently. Both halves of that are data loss.
//
// 🔴 A `-`-LED LINE IS NEVER A KEY, and that single skip is what closes the
// phantom-key class independently of where any block scan begins or ends.
//
// 🔴 A BARE `key:` WITH NO ITEMS UNDER IT READS AS `""`, NOT AS AN EMPTY LIST.
// A block list is recognised by LOOKAHEAD — the key opens one only when a
// following item actually exists — so no existing key changes type. Handing a
// list to a consumer that has always had a string is an error raised from a file
// the operator would have to guess at.
func ParseFrontMatter(text string) FrontMatter {
	m := frontMatterRe.FindStringSubmatch(text)
	out := FrontMatter{}
	if m == nil {
		return out
	}
	lines := pytext.SplitLines(m[1])

	isSkippable := func(raw string) bool {
		s := pytext.StripWhitespace(raw)
		return s == "" || strings.HasPrefix(s, "#")
	}
	// 🔴 ANY `-`-LED LINE IS AN ITEM, INCLUDING AN EMPTY ONE. The narrower
	// `HasPrefix(s, "- ")` is a defect, not a style choice: a bare `-` does not
	// satisfy it, so the block scan TERMINATED there and every item below was
	// promoted to a phantom key again — and the owning key then read as falsy,
	// so the entry LOADED CLEAN reporting no tasks. Data gone, every surface
	// saying the file is fine.
	isBlockItem := func(raw string) bool {
		s := pytext.StripWhitespace(raw)
		return s == "-" || strings.HasPrefix(s, "- ")
	}
	// blockItemsFrom collects the `- item` run beginning at start. It stops at
	// the first line that is neither an item, a comment, nor blank. An EMPTY
	// item contributes nothing but does NOT stop the scan.
	//
	// 🔴 A BLANK LINE DOES NOT END THE BLOCK. Making one terminate the scan was
	// measured as a REGRESSION against PyYAML: a list separated from its key by
	// a blank line is ordinary, valid YAML, and breaking there dropped the whole
	// list back out of the scan so its items were promoted to phantom keys.
	blockItemsFrom := func(start int) []string {
		var items []string
		for j := start; j < len(lines); j++ {
			candidate := lines[j]
			if isSkippable(candidate) {
				continue
			}
			if !isBlockItem(candidate) {
				break
			}
			item := pytext.StripWhitespace(candidate)
			item = pytext.StripWhitespace(item[1:])
			item = strings.Trim(item, "'\"")
			if item != "" {
				items = append(items, item)
			}
		}
		return items
	}
	// blockEndsAt is the index of the last line belonging to the block opened
	// before start — items, interleaved comments and blank lines alike. It must
	// agree with blockItemsFrom about MEMBERSHIP even where it disagrees about
	// CONTENT, which is why both call isBlockItem.
	//
	// ⚠ Trailing blanks and comments are NOT swallowed: `last` only advances on
	// a real member, so a block followed by a blank line and then a key leaves
	// that key readable.
	blockEndsAt := func(start int) int {
		last := start - 1
		for j := start; j < len(lines); j++ {
			if isSkippable(lines[j]) {
				continue
			}
			if isBlockItem(lines[j]) {
				last = j
				continue
			}
			break
		}
		return last
	}

	consumedThrough := -1
	for i, line := range lines {
		if i <= consumedThrough || isSkippable(line) {
			continue
		}
		if isBlockItem(line) {
			continue
		}
		key, value, sep := strings.Cut(line, ":")
		if !sep {
			continue
		}
		key = pytext.StripWhitespace(key)
		value = pytext.StripWhitespace(value)
		if key == "" {
			continue
		}
		switch {
		case strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]"):
			var items []string
			for _, v := range strings.Split(value[1:len(value)-1], ",") {
				v = strings.Trim(pytext.StripWhitespace(v), "'\"")
				if v != "" {
					items = append(items, v)
				}
			}
			out[key] = items
		case value == "":
			if block := blockItemsFrom(i + 1); len(block) > 0 {
				out[key] = block
			} else {
				// No items followed, so this is an empty scalar, which is what a
				// bare `key:` has always meant here.
				out[key] = ""
			}
			consumedThrough = blockEndsAt(i + 1)
		default:
			out[key] = strings.Trim(value, "'\"")
		}
	}
	return out
}

// EntryMapping turns one entry file's bytes into the mapping entry validation
// would be handed.
//
// 🔴 ONE FUNCTION, SO A VALIDATOR CANNOT BUILD A DIFFERENT ONE. The write path
// has to answer "would the loader accept this file?", and the only honest way to
// answer it is to construct exactly what the loader constructs. Re-spelling these
// three lines at the write path would be the duplicated predicate this codebase
// keeps finding: the day one side learns a new identity field, the validator
// starts blessing entries the reader rejects — the precise drift a write-time
// check exists to prevent.
//
// The directory name is the authority on scope: it is where the file actually
// lives. A `scope:`/`repo:` field that disagrees is stale front matter, not a
// relocation, so `scope` is set unconditionally and `repo` is dropped.
func EntryMapping(text, filename, scope string) FrontMatter {
	fm := ParseFrontMatter(text)
	fm["filename"] = filename
	fm["scope"] = scope
	// ⚠ REDUNDANT-BUT-KEPT, labelled: validation reads `scope` in preference to
	// `repo`, and `scope` was just set unconditionally, so this cannot change any
	// outcome. It stays to keep the mapping honest — leaving a contradicted
	// `repo:` in a map that is also the malformed-entry error's source label puts
	// a stale value in front of whoever reads that error.
	delete(fm, "repo")
	return fm
}
