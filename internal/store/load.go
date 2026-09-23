package store

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// StoreMissingError is "the store root is not there". Sentinel: `store root not
// found`. It is NOT "nothing recorded yet", and the message says so, because the
// two rendering alike is the confident zero this whole design exists to prevent.
type StoreMissingError struct{ message string }

func (e *StoreMissingError) Error() string { return e.message }

// EntryUnreadableError is "an entry file could not be READ". Sentinel: `index
// entry unreadable`.
//
// ⚠ IT IS A DIFFERENT FACT FROM "malformed", not a harsher spelling of it. A
// malformed entry is a file we read and could not interpret, and it has an honest
// degraded form; an unreadable one means the store was not fully read, so the set
// of entries is UNKNOWN and there is nothing honest to degrade to. That is why an
// OS error still fails closed in BOTH policies.
type EntryUnreadableError struct{ message string }

func (e *EntryUnreadableError) Error() string { return e.message }

// 🔴 THE LOADER'S OWN TABLE, AND IT IS DELIBERATELY NARROWER THAN `/snapshot`'s.
//
// The broad form — mirroring the snapshot's entry table wholesale — was written,
// reviewed and REJECTED on the Python side, because it also refuses `link-to-file`
// and `indeterminate`, which this loader READS (or honestly fails on): that is a
// behaviour change for every local CLI caller. Those two cells are `Take` for
// exactly that reason. The five cells that HAVE been decided were each decided on
// one criterion — "this loader has never successfully read one, so refusing it
// changes no legitimate caller" — and each was measured:
//
//   - `broken-link`   a dangling symlink. A `*.md` glob MATCHES A LEADING DOT, so
//     an editor lock file (`.#entry.md`, a dangling link) is a
//     candidate entry; opening it raised, and an OS error fails
//     closed, so that took `/recall` down for EVERY caller and
//     named the file and its scope in the 503.
//   - `other`         fifo, socket, device. Reading a FIFO BLOCKS until somebody
//     writes to it: on a single-replica deployment the request
//     thread never returns. Not a degradation — a hang.
//   - `link-to-other` a symlink POINTING AT one. Measured wedging an
//     unrestricted `/recall` thread for 25s while `/healthz` stayed
//     200 throughout — the process up, the worker gone. `open()`
//     does not care which path shape reached the fifo.
//   - `directory`     a DIRECTORY named `*.md`. Reading one raises, and that
//   - `link-to-dir`   fails closed into a store-wide 503 for every caller off one
//     stray `mkdir <scope>/<slug>.md` or an rsync artefact.
//
// 🔴 `link-to-file` IS `Take`, AND THAT IS THE POINT OF THE NARROW FORM — it is
// the cell the narrow-vs-broad ruling was actually about. A symlink to a regular
// `*.md` is read today and keeps being read; a mutant flipping this cell to Refuse
// is the over-broad regression the ruling exists to prevent.
//
// ⚠ `indeterminate` and `absent` are `Take` for a DIFFERENT reason from the five
// above. `indeterminate` means "the lstat failed and I could not look", which is
// not "this kind can never be an entry"; `absent` is a file that vanished between
// the directory read and the classification. Reading each fails, and that failure
// is the four-state rule's "the store was not fully READ" — a different fact from
// "this entry is malformed", which must not be quietly folded into it.
var loaderEntryActions = map[Kind]Action{
	KindBrokenLink:    Refuse,
	KindOther:         Refuse,
	KindLinkToOther:   Refuse,
	KindDirectory:     Refuse,
	KindLinkToDir:     Refuse,
	KindRegularFile:   Take,
	KindLinkToFile:    Take,
	KindIndeterminate: Take,
	KindAbsent:        Take,
}

// loaderRefusalReason is the `why` clause each refused kind carries — the
// sentence after the sentinel, so a refused entry reads exactly like every other
// unusable one. It names the SHAPE and never invents a fix, because the
// operator's fix differs per shape (delete the lock file; delete the fifo).
var loaderRefusalReason = map[Kind]string{
	KindBrokenLink: "broken symlink (a dangling target, or a link loop) — not an entry, and " +
		"refused before `open()`. `glob('*.md')` matches a leading dot, so an " +
		"editor lock file such as `.#<entry>.md` lands here; reading it raised " +
		"`index entry unreadable`, which took the whole store down for every " +
		"caller and named this file in the error",
	KindOther: "not a regular file (a fifo, socket or device) — refused before " +
		"`open()`, which on a fifo blocks until somebody writes to it and never " +
		"returns, wedging the reader",
	KindLinkToOther: "a symlink to something that is not a regular file (a fifo, socket or " +
		"device) — refused before `open()`, which blocks on a fifo whether it " +
		"is named directly or reached through a link. Measured wedging a " +
		"`/recall` request thread for 25s while the process stayed healthy",
	KindDirectory: "a directory named `*.md` — refused before `open()`, which on a " +
		"directory raises `IsADirectoryError`; that OSError fails closed, so " +
		"one stray `mkdir <scope>/<slug>.md` (or an rsync/restore artefact) " +
		"answered `/recall` and `/search` with a store-wide 503 for every " +
		"caller. Delete the directory, or move its contents into a `*.md` file",
	KindLinkToDir: "a symlink to a directory — refused before `open()`, which raises " +
		"`IsADirectoryError` whether the directory is named directly or " +
		"reached through a link, and that OSError fails closed into the same " +
		"store-wide 503",
}

// LoaderRefusalReason exposes the table for the test that pins it against
// `loaderEntryActions` in both directions.
func LoaderRefusalReason(kind Kind) (string, bool) {
	reason, ok := loaderRefusalReason[kind]
	return reason, ok
}

// LoaderAction exposes one cell of the loader's table, for the same reason.
func LoaderAction(kind Kind) (Action, bool) {
	action, ok := loaderEntryActions[kind]
	return action, ok
}

// LoadIndex reads `<root>/<scope>/*.md` into an Index. READ-ONLY.
//
// `README.md` is skipped in every scope — each scope directory carries one as its
// store-policy sheet, and it is not an entry.
//
// A scope directory with no entries is REGISTERED, not dropped: an existing empty
// scope must resolve to an honest empty result, while a scope that does not exist
// stays an error.
//
// 🔴 `visible` IS APPLIED BEFORE ANY FILE IS OPENED, AND THAT IS THE POINT OF IT
// BEING HERE RATHER THAN ON THE RESULT. Narrowing the index afterwards still WALKS
// and READS every scope, which with a scoped API caller in front of it is three
// separate defects, all measured on the Python side:
//
//   - DISCLOSURE. One unreadable entry made `/recall` answer 503 carrying the FULL
//     PATH of a file in a scope that caller is not allowed to know exists.
//   - DENIAL OF SERVICE. One unreadable file anywhere in the store broke recall
//     for EVERY caller, including the ones whose own scopes were fine.
//   - A HUNG THREAD. Reading a FIFO named `*.md` blocks until somebody writes to
//     it. On a single-replica service that is worse than the 503.
//
// An unreadable file in a scope the caller CAN see still fails. That is the
// four-state rule and it is unchanged.
//
// 🔴 AND THE ALLOWLIST ALONE IS NOT THE WHOLE FIX — AN UNRESTRICTED CALLER SKIPS
// NOTHING, and a bare legacy row is unrestricted, which is what the pod is
// deployed with. So the candidate's KIND is checked before it is opened, for every
// caller, through `loaderEntryActions`.
//
// THE REFUSED SET, named by KIND so this sentence is machine-readable: a dangling
// symlink (`broken-link`), a fifo/socket/device (`other`), a symlink POINTING AT
// one (`link-to-other`), a directory named `*.md` (`directory`) and a symlink
// pointing at a directory (`link-to-dir`) are refused. (END OF REFUSED SET.)
// Everything this loader has ever successfully READ — regular files AND symlinks
// to regular files — is still read.
//
// ⚠ THE RESIDUAL LEDGER — what this guard does NOT cover, which is exactly the
// kinds `loaderEntryActions` maps to `Take`, because `Take` means the read runs
// and any OS error it raises fails closed into a store-wide
// EntryUnreadableError: `regular-file` (a mode-000 entry), `link-to-file` (the
// same shape through a symlink whose TARGET is unreadable), `indeterminate` (the
// lstat itself failed) and `absent` (the candidate vanished mid-walk).
// (END OF RESIDUAL LEDGER)
func LoadIndex(root string, onMalformed OnMalformed, visible ScopeSet) (*Index, error) {
	collecting := onMalformed == Collect
	var mappings []FrontMatter
	var scopes []string
	// Entries REFUSED by kind before they are opened. Kept separate from
	// `mappings` because they never become one — BuildIndex never sees them, so
	// nothing downstream has to learn a second rejection shape.
	var refused []MalformedEntry

	dirents, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	// Both sorts here are DETERMINISM guards: they fix which entry a
	// malformed-index error names, so the same store always produces the same
	// message. `os.ReadDir` already sorts by name, which is what the Python side
	// achieves with an explicit `sorted()`; it is restated rather than relied on
	// implicitly because the property is the guard, not the library's default.
	names := make([]string, 0, len(dirents))
	for _, d := range dirents {
		names = append(names, d.Name())
	}
	slices.Sort(names)

	for _, name := range names {
		scopePath := filepath.Join(root, name)
		info, statErr := os.Stat(scopePath)
		if statErr != nil {
			// 🔴 `p.is_dir()` DOES NOT RETURN FALSE WHEN THE STAT FAILS — IT RETURNS
			// FALSE FOR FOUR ERRNOS AND **RAISES** FOR EVERY OTHER ONE. The comment
			// that used to sit here said the opposite ("false for anything that …
			// cannot be stat'd"), and `continue` implemented what the comment said.
			// `classify_path`'s docstring states the real rule correctly, three
			// hundred lines away, which is how the two came to disagree.
			//
			// `pathlib._IGNORED_ERRNOS == (ENOENT, ENOTDIR, EBADF, ELOOP)` on the
			// pinned interpreter. Those four mean "this is not a directory, or it is
			// not there" — a fact, so skipping is honest. Anything else (EACCES,
			// ESTALE, EIO) means WE DO NOT KNOW WHAT THIS IS, and skipping it claims
			// an absence about a scope nobody could look at. That claim is the entire
			// defect class this file's action tables exist to refuse.
			//
			// 🔴 MEASURED CONSEQUENCE OF THE `continue`: a store root that is readable
			// but NOT searchable (mode 0o444 — `ReadDir` lists the names, `Stat` on
			// each child fails EACCES) produced an EMPTY INDEX and no error. The
			// oracle raises `PermissionError` out of `load_index`, `load_store` turns
			// it into `EntryUnreadableError`, and the route answers
			// `503 store-unreachable`. Go answered as though the store were empty —
			// "nothing recorded yet" for a store it never read.
			//
			// Returned rather than wrapped here: `LoadStore` already wraps any
			// non-MalformedEntryError from this function in the oracle's own
			// `index entry unreachable: under <root> (<Type>: <err>) — the store was
			// not fully read, so this report would be INCOMPLETE` sentence. Wrapping
			// twice would be a second policy site.
			if isIgnoredStatErrno(statErr) {
				continue
			}
			return nil, statErr
		}
		if !info.IsDir() {
			continue
		}
		if !visible.Allows(name) {
			// 🔴 `continue` BEFORE the scope is registered, not after. Appending
			// first still skips the READ, so it looks equivalent — and through
			// LoadStore it is, because that function's result narrowing drops the
			// key again on the way out. It is NOT equivalent for a caller that
			// uses this function directly, which would get a denied scope's NAME
			// on `Scopes()`: the `known_scopes` enumeration channel, for a
			// directory nothing ever opened.
			continue
		}
		scopes = append(scopes, name)

		// 🔴 THE ENTRY SET COMES FROM ONE FUNCTION, NOT FROM A README TEST OPEN-CODED
		// HERE. This loop used to spell `if entryName == "README.md" { continue }`
		// itself; three other sites spelled the same rule, and two of them spelled it
		// WRONG. See `EntryFileNames`.
		entryNames, readErr := EntryFileNames(scopePath)
		if readErr != nil {
			return nil, readErr
		}
		for _, entryName := range entryNames {
			mdPath := filepath.Join(scopePath, entryName)
			// 🔴 WHAT IS THIS PATH — ASKED BEFORE IT IS OPENED, AND ASKED ONCE.
			// The same classifier `/snapshot` uses; only the action table differs,
			// because the action is a property of the context.
			kind := ClassifyPath(mdPath)
			action, actionErr := ActionFor(kind, loaderEntryActions)
			if actionErr != nil {
				return nil, actionErr
			}
			if action == Refuse {
				reason := loaderRefusalReason[kind]
				if !collecting {
					// 🔴 THE SAME POLICY, NOT A NEW ONE. Under Raise this is
					// indistinguishable from any other rejection.
					return nil, malformed(entryName, reason)
				}
				refused = append(refused, MalformedEntry{
					Scope:    NormalizeRef(name),
					Filename: entryName,
					Reason:   reason,
				})
				continue
			}
			data, readFileErr := os.ReadFile(mdPath)
			if readFileErr != nil {
				return nil, readFileErr
			}
			mappings = append(mappings, EntryMapping(DecodeReplace(data), entryName, name))
		}
	}

	index, buildErr := BuildIndex(mappings, scopes, onMalformed)
	if buildErr != nil {
		return nil, buildErr
	}
	if len(refused) == 0 {
		return index, nil
	}
	// 🔴 MERGED HERE RATHER THAN PASSED INTO BuildIndex. These rows have ALREADY
	// been through the policy above (a non-collecting caller never reaches this
	// line), so handing them to the function whose whole job is to APPLY that
	// policy would be a second, silent policy site.
	index.Malformed = append(index.Malformed, refused...)
	return index, nil
}

// ScopePolicySheet is the ONE filename a cached scope directory carries that is not an
// entry: each scope dir holds one as its store-policy sheet, `/snapshot` ships it, so every
// real cache has them.
//
// 🔴 THE SPELLING IS EXACT AND THAT IS THE WHOLE RULE — not a prefix, not a case fold.
// `readme.md` and `README-old.md` are ORDINARY ENTRIES: the loader walks them, indexes them
// and can reject them as malformed, so a consumer that excluded them would count a file the
// loader counted too and drive a printed numerator below zero.
const ScopePolicySheet = "README.md"

// IsEntryFileName answers "is this filename an ENTRY in a cached scope" — the one rule, in
// one place.
//
// 🔴 IT EXISTS BECAUSE THE RULE WAS OPEN-CODED AT FOUR PRODUCTION SITES AND WAS WRONG AT TWO
// OF THEM, IN THE SAME DIRECTION. The loader below skipped the sheet; `Validate` did not
// (fixed separately, and it printed `3 of 3` over two entries); `LsEntries` did not either,
// and `cairn ls-entries` — the verb `README.md` advertises as "what the cache actually
// holds" — listed every scope's policy sheet as an entry. A predicate open-coded at N sites
// is typically wrong at N-1 of them; consolidating it is what made the disagreement audible.
//
// ⚠ THE `.md` HALF IS NOT REDUNDANT WITH THE CALLER'S GLOB EVEN WHERE THE GLOB ALREADY
// APPLIED IT. Stating the whole rule here is what lets a caller that has NOT globbed (a
// directory walk, an archive member list) ask the same question and get the same answer.
//
// 🔴 AND IT COMPARES THE BASE NAME, BECAUSE THE SENTENCE ABOVE IS A PROMISE THE FIRST CUT
// DID NOT KEEP. That cut compared the WHOLE argument, so the un-globbed callers it invites
// got the opposite answer: an archive member list in this repo is `scope + "/" + name`
// (`internal/snapshot`), the same shape `LsEntries` prints, and `IsEntryFileName(
// "notes/README.md")` returned TRUE — the policy sheet classified as an entry, the exact
// defect the consolidation exists to eliminate, regenerated inside the consolidated rule.
// Basing the comparison makes the predicate TOTAL over its input rather than narrowing the
// promise to "base names only": the answer is now the same however a caller spells the path.
//
// ⚠ THIS CHANGES NOTHING FOR TODAY'S CALLERS, AND THAT WAS CHECKED RATHER THAN ASSUMED.
// `EntryFileNames` below passes `os.ReadDir` names and `LsEntries` passes `filepath.Base`,
// so both were already handing it base names, for which `filepath.Base` is the identity.
// `filepath.Base("")` is `"."`, which carries no `.md` suffix, so the empty name stays false.
func IsEntryFileName(name string) bool {
	base := filepath.Base(name)
	return strings.HasSuffix(base, ".md") && base != ScopePolicySheet
}

// EntryFileNames is the ENTRY files in ONE cached scope directory, sorted — `mdNamesIn`
// with `IsEntryFileName` applied.
//
// 🔴 THE LOADER AND `validate` BOTH GO THROUGH THIS, WHICH IS THE POINT. The original defect
// was two walks behind one printed line: the numerator came from `LoadIndex` and the
// denominator from a bare `*.md` glob, so they could disagree about what an entry is. They
// now cannot, because there is one walk function and one rule.
//
// ⚠ THE ERROR IS THE DIRECTORY READ'S, PROPAGATED. A scope directory that cannot be read is
// NOT an empty scope — see `mdNamesIn`'s caller in `LoadIndex`, which fails closed on it.
func EntryFileNames(dir string) ([]string, error) {
	names, err := mdNamesIn(dir)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(names))
	for _, name := range names {
		if IsEntryFileName(name) {
			out = append(out, name)
		}
	}
	return out, nil
}

// mdNamesIn is the `*.md` glob, sorted.
//
// ⚠ A `*.md` GLOB MATCHES A LEADING DOT — measured on the Python side, not
// assumed — so an editor lock file (`.#entry.md`, a dangling symlink) IS a
// candidate here. That is the `broken-link` cell's whole reason for existing, and
// the `.md` half of the shape needs no separate check because the glob has already
// applied it. Go's `filepath.Glob` behaves the same way, and this function is
// written as an explicit suffix test rather than a glob so the property is stated
// instead of inherited.
func mdNamesIn(dir string) ([]string, error) {
	dirents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, d := range dirents {
		if strings.HasSuffix(d.Name(), ".md") {
			out = append(out, d.Name())
		}
	}
	slices.Sort(out)
	return out, nil
}

// EntryUnreadable is the sentence the READER raises when ONE entry file it was told
// about cannot be opened. It lives here beside LoadStore's store-wide twin so the two
// "the store was not fully read" sentences cannot drift apart, and so a renderer does not
// have to reach for the OS-error class name itself.
//
// ⚠ `nothing was written` IS PART OF THE ORACLE'S SENTENCE and is kept verbatim even
// though the reader has no write path. It is a claim about the RUN, which is what a
// reader of the message needs to know; rewording it would be a divergence with nothing
// behind it.
func EntryUnreadable(path string, cause error) *EntryUnreadableError {
	return &EntryUnreadableError{message: fmt.Sprintf(
		"index entry unreadable: %s (%s: %s) — the store was not fully read, so this "+
			"report is INCOMPLETE; nothing was written",
		path, osErrorTypeName(cause), cause)}
}

// LoadStore resolves the store root and loads its index.
//
// 🔴 ONE PLACE, because every route opens the SAME store for the same reasons and
// a predicate open-coded at two sites is wrong at one of them. `verb` is the only
// thing that differs: a store-missing message has to say what did NOT happen
// ("nothing was recalled" / "nothing was written") or it reads as the ordinary
// nothing-recorded-yet case.
//
// 🔴 IT LOADS WITH `Collect`, AND THAT IS THIS FUNCTION'S POLICY DECISION, NOT THE
// LOADER'S. A malformed entry comes back on `Malformed` and every caller is
// obliged to render it. The measurement that forced it: fail-closed cost the whole
// scope — 2 good entries and 1 bad one served ZERO.
func LoadStore(storeRoot, verb string, visible ScopeSet) (*Index, error) {
	info, err := os.Stat(storeRoot)
	if err != nil || !info.IsDir() {
		return nil, &StoreMissingError{message: fmt.Sprintf(
			"store root not found: %s — expected the `/analyze-service` index store. Nothing was %s; this is NOT 'nothing recorded yet'",
			storeRoot, verb)}
	}
	index, err := LoadIndex(storeRoot, Collect, visible)
	if err != nil {
		if me, isMalformed := err.(*MalformedEntryError); isMalformed {
			// Unreachable under Collect — BuildIndex collects instead of raising
			// and the kind guard above collects too. Reported rather than
			// swallowed, so a future policy change cannot turn a rejection into a
			// silently short index.
			return nil, me
		}
		return nil, &EntryUnreadableError{message: fmt.Sprintf(
			"index entry unreadable: under %s (%s: %s) — the store was not fully read, so this report would be INCOMPLETE",
			storeRoot, osErrorTypeName(err), err)}
	}
	if visible.Unrestricted {
		return index, nil
	}
	// 🔴 REBUILT FROM THE TWO FIELDS, not by mutating the index and not by adding
	// a third field. Every derived answer — `Scopes`, `MalformedIn`,
	// `MalformedOutside`, `Entries`, `Len` — comes from these two, so narrowing
	// exactly them narrows every derived answer at once, including the ones a
	// future accessor adds.
	//
	// ⚠ REDUNDANT WITH THE LOADER'S OWN FILTER TODAY, AND KEPT DELIBERATELY. The
	// loader already skips a denied scope directory entirely, so this rebuild has
	// nothing left to drop and a mutation sweep will score it as an equivalent
	// mutant. It stays because the two filters answer different questions — the
	// loader's decides what is OPENED, this one decides what the RESULT SHAPE is
	// — and both derive their set from the same ScopeSet, so they cannot come to
	// disagree about what an allowlist means.
	narrowed := map[string][]Entry{}
	for scope, entries := range index.byScope {
		if visible.Allows(scope) {
			narrowed[scope] = entries
		}
	}
	var keptMalformed []MalformedEntry
	for _, m := range index.Malformed {
		if visible.Allows(m.Scope) {
			keptMalformed = append(keptMalformed, m)
		}
	}
	return &Index{byScope: narrowed, Malformed: keptMalformed}, nil
}
