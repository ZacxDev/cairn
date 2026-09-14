package store

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ZacxDev/cairn/internal/pytext"
)

// MalformedEntryError is one entry file that cannot be interpreted. Sentinel:
// `malformed index entry`.
//
// `Why` is the REASON with the sentinel prefix stripped and `Source` is the entry
// it was raised about. Both are carried STRUCTURALLY rather than recovered by
// splitting the message on `": "` — a degrading loader prints one row per bad
// entry, and a row assembled by re-parsing an error message is a second parser
// for a format nothing pins.
type MalformedEntryError struct {
	Source  string
	Why     string
	message string
}

func (e *MalformedEntryError) Error() string { return e.message }

func malformed(source, why string) *MalformedEntryError {
	return &MalformedEntryError{
		Source:  source,
		Why:     why,
		message: fmt.Sprintf("malformed index entry %s: %s", PyRepr(source), why),
	}
}

// MalformedEntry is one rejected entry as DATA rather than as an error — what a
// degrading (collecting) load returns.
//
// `Scope` is the NORMALIZED owning scope, which for a disk load is the directory
// name. It is what lets a reader report this entry against the scope it belongs
// to and no other: a malformed entry in one scope must not appear while recalling
// another, and must not make that other scope look broken.
type MalformedEntry struct {
	Scope    string
	Filename string
	Reason   string
}

// Label is `<scope>/<filename>` — how every surface names this file.
func (m MalformedEntry) Label() string {
	if m.Scope == "" {
		return m.Filename
	}
	return m.Scope + "/" + m.Filename
}

// TaskRef is one `<system>:<id>` reference from an entry's `tasks:` front matter.
type TaskRef struct {
	// System is normalized — lowercased and `-`-folded, like every other ref.
	System string
	// Ident is the id half, BYTE-IDENTICAL to what the file carried.
	Ident string
	// Raw is the whole ref exactly as written, for evidence and error messages.
	Raw string
}

func (t TaskRef) String() string { return t.System + ":" + t.Ident }

// Entry is one validated index entry.
//
// 🔴 EVERY SLICE FIELD IS NON-NIL EVEN WHEN EMPTY, AND THE THREE OF THEM USED TO DISAGREE.
// Measured on an entry with no aliases and no tasks: `Aliases` was an empty slice (because
// `sortedKeys` always allocates) while `RawAliases` and `Tasks` were `nil` — two answers to
// one question inside one struct, and nobody had decided either. The oracle has no such
// split: `aliases`, `raw_aliases` and `tasks` are all tuples on `SubsystemEntry`, `tasks`
// defaults to `()`, and `tasks: []` is documented as meaning exactly what an absent key
// means. So all three are empty slices here.
//
// ⚠ WHY IT MATTERED AT ALL, since `len`, `range`, index and append are identical on nil:
// the difference is invisible until something SERIALISES or COMPARES these, and then it is
// not. `encoding/json` writes a nil slice as `null` and an empty one as `[]`; `== nil` reads
// as "unset", which no absent key here means; `reflect.DeepEqual` against a literal is false
// for nil. There is no JSON surface on `Entry` today and the control plane is where one
// arrives, which is exactly why this was worth settling before it had a consumer rather
// than after.
//
// `TestTheEmptySliceLedgerIsComplete` enforces both halves: it re-measures the claim above,
// and it enumerates the slice fields by reflection so the set cannot GROW or SHRINK without
// somebody deciding for the new field.
type Entry struct {
	// Slug is the filename's slug part, with `service:` and the filename agreeing.
	Slug string
	// Kind comes from a kind-qualified filename (`<slug>.<kind>.md`); "" for a
	// bare `<slug>.md`.
	Kind string
	// Scope is the normalized owning scope. `repo:` is read as `scope:` because
	// older files carry it.
	Scope string
	// Aliases are normalized, deduped and sorted.
	Aliases []string
	// RawAliases are as written in the file, for evidence.
	RawAliases []string
	// Filename is `<slug>.md` or `<slug>.<kind>.md` — the name a candidate list
	// must show, and the name a write must locate the file by.
	Filename string
	// Tasks are the tasks this entry answers, in FILE ORDER, deduped, never
	// normalized.
	Tasks []TaskRef
}

// Ref is the canonical ref that addresses this entry unambiguously.
func (e Entry) Ref() string {
	if e.Kind != "" {
		return e.Slug + "." + e.Kind
	}
	return e.Slug
}

// EntryFromMapping validates one entry mapping. Every rejection carries the
// sentinel `malformed index entry`.
//
// 🔴 THE REFUSAL SENTENCES ARE THE HTTP CONTRACT, NOT DIAGNOSTICS. A `PUT` whose
// body the loader would reject answers `422 unprocessable: the index loader would
// reject these bytes: <this message>`, and the conformance goldens pin it byte
// for byte. Every string below is transcribed from `SubsystemEntry.from_mapping`
// deliberately, including its punctuation and its em dashes.
//
// Accepted keys: `service` (required), `scope` or `repo` (required, one of),
// `aliases` (optional sequence), `kind` (optional), `filename` (optional —
// supplied by the loader, otherwise derived), `tasks` (optional sequence of
// `<system>:<id>` refs) or `task` (optional scalar sugar for a one-element
// `tasks`).
func EntryFromMapping(mapping FrontMatter, source string) (Entry, error) {
	bad := func(why string) error { return malformed(source, why) }

	rawService, isStr := mapping.String("service")
	if !isStr || pytext.StripWhitespace(rawService) == "" {
		return Entry{}, bad("missing or empty `service:` — an entry with no name cannot be addressed")
	}
	slugAll := NormalizeRef(rawService)
	if slugAll == "" {
		return Entry{}, bad(fmt.Sprintf("`service: %s` normalizes to the empty string", PyRepr(rawService)))
	}

	rawScope, scopeIsStr := mapping.String("scope")
	if _, present := mapping["scope"]; !present {
		rawScope, scopeIsStr = mapping.String("repo")
	}
	if !scopeIsStr || pytext.StripWhitespace(rawScope) == "" {
		return Entry{}, bad("missing or empty `scope:` (or the older `repo:`)")
	}
	scope := NormalizeRef(rawScope)
	if scope == "" {
		return Entry{}, bad(fmt.Sprintf("`scope: %s` normalizes to the empty string", PyRepr(rawScope)))
	}

	// Kind may arrive from the filename, the front matter, or both. Both must
	// AGREE: a `kind:` field the filename contradicts is a claim about the entry
	// that nothing enforces, and a declaration no code path honours is worse than
	// no declaration.
	filenameVal, filenamePresent := mapping["filename"]
	fileKind := ""
	filename := ""
	haveFilename := false
	if filenamePresent {
		fn, isFnStr := filenameVal.(string)
		if !isFnStr || !strings.HasSuffix(fn, ".md") {
			return Entry{}, bad(fmt.Sprintf("`filename` %s is not a `.md` name", pyReprAny(filenameVal)))
		}
		filename, haveFilename = fn, true
		var fileSlug string
		fileSlug, fileKind = SplitKind(NormalizeRef(strings.TrimSuffix(fn, ".md")))
		if fileSlug != slugAll {
			return Entry{}, bad(fmt.Sprintf(
				"filename %s has slug %s but `service:` normalizes to %s — the two must agree or a ref reaches the wrong file",
				PyRepr(fn), PyRepr(fileSlug), PyRepr(slugAll)))
		}
	}

	declaredKind := ""
	haveDeclaredKind := false
	if declaredVal, present := mapping["kind"]; present {
		dk, isDkStr := declaredVal.(string)
		if !isDkStr || !isKind(NormalizeRef(dk)) {
			return Entry{}, bad(fmt.Sprintf("`kind: %s` is not one of %s",
				pyReprAny(declaredVal), strings.Join(Kinds, "|")))
		}
		declaredKind, haveDeclaredKind = NormalizeRef(dk), true
	}
	if haveDeclaredKind && fileKind != "" && declaredKind != fileKind {
		return Entry{}, bad(fmt.Sprintf("`kind: %s` contradicts the filename's kind %s",
			PyRepr(declaredKind), PyRepr(fileKind)))
	}
	kind := fileKind
	if kind == "" {
		kind = declaredKind
	}

	// The slug is the filename's SLUG PART. With no filename supplied, honour a
	// kind suffix on `service:` itself — otherwise `service: thing.process` would
	// produce slug `thing.process`, which no ref could ever reach.
	slug := slugAll
	if !haveFilename {
		var serviceKind string
		slug, serviceKind = SplitKind(slugAll)
		if serviceKind != "" {
			if kind != "" && kind != serviceKind {
				return Entry{}, bad(fmt.Sprintf("`service: %s` carries kind %s but `kind: %s` was declared",
					PyRepr(rawService), PyRepr(serviceKind), PyRepr(kind)))
			}
			kind = serviceKind
		}
	}

	rawAliasesIn, aliasErr := sequenceField(mapping, "aliases", source,
		"`aliases:` must be a list, not a bare string")
	if aliasErr != nil {
		return Entry{}, aliasErr
	}
	// 🔴 NON-NIL EVEN WHEN EMPTY, AND THE THREE SLICE FIELDS AGREE ON THAT. See the
	// ledger on `Entry`: `Aliases` came back as an empty slice (`sortedKeys` always
	// allocates) while `RawAliases` and `Tasks` came back `nil` — three fields, two
	// answers, in one struct, none of it decided. The oracle has no such split:
	// `aliases`, `raw_aliases` and `tasks` are all tuples, so none is ever `None`.
	rawAliases := []string{}
	normalizedSet := map[string]struct{}{}
	for _, alias := range rawAliasesIn {
		if pytext.StripWhitespace(alias) == "" {
			return Entry{}, bad(fmt.Sprintf("alias %s is not a non-empty string", PyRepr(alias)))
		}
		na := NormalizeRef(alias)
		if na == "" {
			return Entry{}, bad(fmt.Sprintf("alias %s normalizes to the empty string", PyRepr(alias)))
		}
		rawAliases = append(rawAliases, alias)
		// DEDUPED, not rejected. Two spellings of one alias on ONE entry are a
		// single address, not an ambiguity — that distinction is the whole reason
		// ambiguity is measured per ENTRY and never per alias-occurrence.
		normalizedSet[na] = struct{}{}
	}

	tasks, taskErr := parseTasksField(mapping, source)
	if taskErr != nil {
		return Entry{}, taskErr
	}

	derived := slug + ".md"
	if kind != "" {
		derived = slug + "." + kind + ".md"
	}
	if !haveFilename {
		filename = derived
	}
	return Entry{
		Slug:       slug,
		Kind:       kind,
		Scope:      scope,
		Aliases:    sortedKeys(normalizedSet),
		RawAliases: rawAliases,
		Filename:   filename,
		Tasks:      tasks,
	}, nil
}

// sequenceField reads an optional list-valued key with Python's exact three
// answers: absent-or-empty yields nothing, a non-empty SCALAR is the named
// refusal, and a list is itself.
//
// 🔴 THE `or ()` IS PART OF THE RULE. `mapping.get(k) or ()` makes an EMPTY
// scalar (`aliases:` with nothing after it) an absent key rather than a bare
// string, so the "must be a list, not a bare string" refusal fires only on a key
// that actually carries a scalar value.
func sequenceField(mapping FrontMatter, key, source, scalarWhy string) ([]string, error) {
	v, present := mapping[key]
	if !present {
		return nil, nil
	}
	switch typed := v.(type) {
	case []string:
		return typed, nil
	case string:
		if typed == "" {
			return nil, nil
		}
		return nil, malformed(source, scalarWhy)
	default:
		return nil, malformed(source, fmt.Sprintf("`%s:` must be a list, got %T", key, v))
	}
}

func parseTasksField(mapping FrontMatter, source string) ([]TaskRef, error) {
	bad := func(why string) error { return malformed(source, why) }
	tasksVal, hasTasks := truthy(mapping, "tasks")
	taskVal, hasTask := truthy(mapping, "task")
	if hasTasks && hasTask {
		return nil, bad("both `tasks:` and `task:` are set — `task:` is sugar for a one-element `tasks:`; keep one of them")
	}
	var items []string
	switch {
	case hasTasks:
		switch typed := tasksVal.(type) {
		case []string:
			items = typed
		case string:
			// `tasks:` is a LIST by definition, so a scalar there is a mistake
			// worth naming rather than silently flattening.
			return nil, bad("`tasks:` must be a list, not a bare string — write `tasks: [<system>:<id>]`, or use `task:` for a single ref")
		default:
			return nil, bad(fmt.Sprintf("`tasks:` must be a list, got %T", tasksVal))
		}
	case hasTask:
		s, isStr := taskVal.(string)
		if !isStr {
			return nil, bad(fmt.Sprintf("`task:` must be a single `<system>:<id>` ref, got %T — use `tasks: [...]` for several", taskVal))
		}
		items = []string{s}
	}
	// Non-nil when empty, for the reason the ledger on `Entry` gives: the oracle's
	// `tasks` defaults to `()` and `tasks: []` means the same thing as an absent key, so
	// there is no state here that `nil` would be expressing.
	out := []TaskRef{}
	seen := map[string]struct{}{}
	for _, item := range items {
		ref, err := ParseTaskRef(item)
		if err != nil {
			// The ref's own message already names the fix; the sentinel prefixes
			// the source, so the operator gets file AND remedy in one line.
			return nil, bad(err.Error())
		}
		// Deduped, not rejected — the same reasoning as `aliases:`. One task
		// written twice is one task.
		key := ref.System + "\x00" + ref.Ident
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ref)
	}
	return out, nil
}

// truthy answers `if mapping.get(k):` — present AND not an empty string or empty
// list. The `tasks:`/`task:` exclusivity check is written against Python
// truthiness, so a bare `tasks:` (which reads as `""`) must not count as set.
func truthy(mapping FrontMatter, key string) (any, bool) {
	v, present := mapping[key]
	if !present {
		return nil, false
	}
	switch typed := v.(type) {
	case string:
		return v, typed != ""
	case []string:
		return v, len(typed) > 0
	}
	return v, true
}

// ParseTaskRef turns `<system>:<id>` into a TaskRef, or returns an error naming
// the fix. Split on the FIRST colon only; both halves must be non-empty after
// stripping, because `:428` and `github:` each look like a ref and address
// nothing.
func ParseTaskRef(raw string) (TaskRef, error) {
	text := pytext.StripWhitespace(raw)
	if text == "" {
		return TaskRef{}, fmt.Errorf("task ref is empty — write it as `<system>:<id>`")
	}
	system, ident, sep := strings.Cut(text, ":")
	if !sep {
		return TaskRef{}, fmt.Errorf(
			"task ref %s has no `:` — write it as `<system>:<id>`, e.g. `clickup:868abc123` or `github:owner/repo#428`",
			PyRepr(raw))
	}
	system = pytext.StripWhitespace(system)
	ident = pytext.StripWhitespace(ident)
	if system == "" {
		return TaskRef{}, fmt.Errorf("task ref %s has an empty system half — write it as `<system>:<id>`", PyRepr(raw))
	}
	if ident == "" {
		return TaskRef{}, fmt.Errorf("task ref %s has an empty id half — write it as `<system>:<id>`", PyRepr(raw))
	}
	if pytext.ContainsSpace(system) || pytext.ContainsSpace(ident) {
		return TaskRef{}, fmt.Errorf(
			"task ref %s contains whitespace — one ref per list item; write several as `tasks: [a, b]`",
			PyRepr(raw))
	}
	// 🔴 A COMMA IS A SEPARATOR IN THE FORM THIS SCHEMA IS WRITTEN IN, so a ref
	// containing one cannot survive its own serialization: the writer emits
	// `tasks: [a,b]`, the inline-list reader splits on `,`, and the entry comes
	// back MALFORMED and invisible to every reader.
	if strings.Contains(text, ",") {
		return TaskRef{}, fmt.Errorf(
			"task ref %s contains a comma, which separates items in `tasks: [a, b]` — a ref cannot contain one",
			PyRepr(raw))
	}
	// C0 (0x00-0x1F), DEL (0x7F) and C1 (0x80-0x9F). C1 is included because the
	// claim is "printable text": omitting it left the check narrower than the
	// sentence describing it.
	for _, r := range text {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return TaskRef{}, fmt.Errorf("task ref %s contains a control character — task ids are printable text", PyRepr(raw))
		}
	}
	normalizedSystem := NormalizeRef(system)
	if normalizedSystem == "" {
		return TaskRef{}, fmt.Errorf("task ref %s has a system half that normalizes to the empty string", PyRepr(raw))
	}
	return TaskRef{System: normalizedSystem, Ident: ident, Raw: text}, nil
}

func isKind(k string) bool {
	for _, known := range Kinds {
		if k == known {
			return true
		}
	}
	return false
}

func pyReprAny(v any) string {
	if s, ok := v.(string); ok {
		return PyRepr(s)
	}
	return fmt.Sprintf("%v", v)
}

func sortedKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
