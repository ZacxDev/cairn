package write

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/ZacxDev/cairn/internal/pytext"
	"github.com/ZacxDev/cairn/internal/store"
)

// EntryShapeError means the target file cannot carry an appended bullet, or the
// bytes offered are not an entry the reader would accept.
//
// Its own type rather than a generic refusal because the two shapes it covers are
// both a request to write into a file whose structure this writer does not
// understand — no nuance heading, or bytes the index loader would not accept — and
// both must FAIL the write rather than reshape somebody's curated markdown.
type EntryShapeError struct{ message string }

func (e *EntryShapeError) Error() string { return e.message }

// PreconditionFailedError means `If-Match` named a revision the file no longer has.
// It carries the CURRENT one, because a client that cannot learn the new revision
// cannot retry — and a client that cannot retry re-sends without the precondition.
type PreconditionFailedError struct{ Current string }

func (e *PreconditionFailedError) Error() string {
	return "entry revision is " + e.Current
}

// EntryExistsError means `If-None-Match: *` was sent and the target already has a
// representation.
//
// 🔴 ITS OWN TYPE, NOT A PRECONDITION FAILURE, EVEN THOUGH BOTH ANSWER 412. The two
// are the same HTTP status and DIFFERENT remedies, which is the exact pair that must
// not be collapsed: a precondition failure means "somebody moved it under you —
// re-sync and re-apply", and this one means "it is already there — you wanted a
// replace". A client that retries the first is right; a client that retries the
// second loops forever. The wire carries the difference in `X-Store-Status`, because
// the status CODE cannot.
//
// ⚠ IT CARRIES THE FILENAME AND NOTHING ELSE, AND THAT IS THE HONEST SHAPE. It is
// returned only from the FILESYSTEM arm — a name the index did not know about, which
// is a file the loader classifies as MALFORMED or a racing create that landed first
// — so there is no resolved entry to name. The other arm (the ref already resolves)
// never reaches here at all; the route answers it directly, where the resolved
// filename IS known. A field for "what it resolved to" would be empty on every
// return, which is a declaration no code path honours.
type EntryExistsError struct{ Filename string }

func (e *EntryExistsError) Error() string {
	return "entry " + e.Filename + " already exists"
}

// BodyNotUTF8Error is the caller's bytes failing a STRICT decode.
//
// ⚠ ITS MESSAGE IS A KNOWN, UNPINNED DIVERGENCE FROM THE ORACLE. Python answers 422
// interpolating a `UnicodeDecodeError` ("'utf-8' codec can't decode byte 0x… in
// position N: …"); reproducing CPython's exact wording would mean transcribing its
// codec diagnostics, and no case in the conformance corpus sends an invalid-UTF-8
// body. What IS pinned for both implementations is the STATUS (422) and the
// `X-Store-Status: entry-shape` header, which is what a client branches on. Stated
// rather than left to look measured.
type BodyNotUTF8Error struct{ Offset int }

func (e *BodyNotUTF8Error) Error() string {
	return fmt.Sprintf("the body is not valid UTF-8 (first bad byte at position %d)", e.Offset)
}

// AppendBullet appends one attributed bullet. It returns the status
// (`appended` or `duplicate`), the rendered or STORED line, and the entry's
// revision afterwards. On `duplicate` NOT ONE BYTE of the file is written and the
// line returned is the bullet already on disk.
//
// 🔴 COMMUTATIVE AND IDEMPOTENT, WHICH IS THE SHIP GATE. Two writers appending
// DIFFERENT bullets to one entry must both survive, and the whole read-modify-write
// therefore happens under the entry lock — an unsynchronised version reads the same
// original bytes twice and the second rename silently discards the first append.
// Re-appending the SAME content is a no-op decided by the content hash over the
// bullets already there, so a retried request cannot double-record.
//
// 🔴 A SPLICE INTO THE ORIGINAL BYTES, NEVER A DECODE-AND-REJOIN. The Python version
// used to decode with a lossy handler, split, and write back a rejoin — which
// silently REWROTE THE WHOLE FILE, and lossily. Three measured effects of one
// ordinary append to an unrelated line: a byte that is not valid UTF-8 anywhere in
// the file became U+FFFD permanently at `200 appended` with no error; every CRLF
// became LF; and a file with no trailing newline gained one. Each of those also
// changes the entry's revision, invalidating every other client's `If-Match` for a
// change nobody asked for.
//
// So the output is `original[:offset] + inserted + original[offset:]` and the
// untouched region is byte-identical BY CONSTRUCTION rather than by a round trip
// anyone has to trust. `offset` is the end of the heading line INCLUDING its
// terminator, and `inserted` carries that same terminator — so CRLF stays CRLF, and
// a heading that is the final line with no terminator at all gets `\n<bullet>`
// appended, preserving "this file does not end in a newline" rather than quietly
// fixing it.
//
// ⚠ GO NEEDS NO `surrogateescape` HERE. Python must decode the file to index into
// it and only that codec round-trips an invalid byte; a Go string IS the bytes, so
// the analysis and the splice operate on the same value and the round trip is the
// identity. See package pytext.
func AppendBullet(path, text, actor, session, today string, interleave func()) (status, line, revision string, err error) {
	lock, err := lockEntry(path)
	if err != nil {
		return "", "", "", err
	}
	defer lock.unlock()

	original, err := os.ReadFile(path)
	if err != nil {
		return "", "", "", err
	}
	textIn := string(original)
	lines := pytext.SplitLines(textIn)
	insertAt, body, ok := store.NuanceBlock(lines)
	if !ok {
		return "", "", "", &EntryShapeError{message: fmt.Sprintf(
			"entry has no `%s` heading, so an appended bullet would have nowhere to go",
			store.NuanceHeading)}
	}
	wanted := ContentHash(text)
	for _, existing := range store.ParseJournalBullets(body) {
		if ContentHash(BulletContent(existing.Lines)) == wanted {
			return "duplicate", existing.Lines[0], EntryRevision(original), nil
		}
	}
	rendered := RenderBullet(text, actor, session, today)
	if interleave != nil {
		// 🔴 THE INTERLEAVE POINT: after the read, before the write — which is
		// exactly the window a missing lock leaves open.
		interleave()
	}
	// `SplitLinesKeepEnds` splits on EXACTLY the same set `SplitLines` above does,
	// so the two lists index alike and `insertAt` means the same line in both.
	// Anything else here is an off-by-one on a file the caller cannot see.
	rawLines := pytext.SplitLinesKeepEnds(textIn)
	heading := ""
	if insertAt-1 < len(rawLines) {
		heading = rawLines[insertAt-1]
	}
	// Whatever the splitter treated as this line's break, VERBATIM — derived by
	// asking the same function, never by guessing a newline and never by stripping a
	// hand-written character class (which would also eat trailing spaces the heading
	// line is entitled to keep).
	withoutBreak := pytext.StripOneLineBreak(heading)
	terminator := heading[len(withoutBreak):]
	offset := len(strings.Join(rawLines[:insertAt], ""))
	inserted := rendered + terminator
	if terminator == "" {
		// The heading is the last line and the file does not end in a newline.
		// Appending `\n<bullet>` keeps it that way.
		inserted = "\n" + rendered
	}
	data := make([]byte, 0, len(original)+len(inserted))
	data = append(data, original[:offset]...)
	data = append(data, inserted...)
	data = append(data, original[offset:]...)
	if err := replaceBytes(path, data); err != nil {
		return "", "", "", err
	}
	return "appended", rendered, EntryRevision(data), nil
}

var ifMatchSplit = regexp.MustCompile(`\s*,\s*`)

// ParseIfMatch is every entity-tag in an `If-Match` header value, unquoted and
// lower-cased.
//
// 🔴 RFC 9110 §13.1.1 SAYS `If-Match` IS A **LIST**, and the Python version used to
// read it as one opaque string. So `If-Match: "stale", "<correct>"` — the exact
// header a client sends when it holds two candidate revisions, and the exact header
// a conformant HTTP library will build from a list — compared the WHOLE string
// against a 16-character hash and answered **412 forever**. Fail-closed, and still a
// bug: a conformant client could never succeed, and a client that cannot succeed
// re-sends without the precondition.
//
// Splitting on commas is safe HERE and would not be in general: an entity-tag may in
// principle contain a comma inside its quotes, but this server's tags are a truncated
// sha256 and cannot. The one place that matters is stated rather than left as a
// silent assumption.
//
// Case is folded because a hex digest is lower-case and hex is not: `"3F2A…"` names
// the same revision as `"3f2a…"`, and refusing it was a precondition failing for a
// reason the caller cannot see.
func ParseIfMatch(raw string) []string {
	var tags []string
	for _, item := range ifMatchSplit.Split(pytext.StripWhitespace(raw), -1) {
		item = pytext.StripWhitespace(item)
		if item == "" {
			continue
		}
		if len(item) >= 2 && strings.EqualFold(item[:2], "W/") {
			item = pytext.StripWhitespace(item[2:])
		}
		tags = append(tags, strings.ToLower(strings.Trim(item, `"`)))
	}
	return tags
}

// ReplaceEntry is a whole-file replace behind an `If-Match` precondition. It
// returns the new revision.
//
// `ifMatch` is the LIST of candidate revisions the caller named; the write proceeds
// if ANY of them is the current one, which is what RFC 9110 §13.1.1 specifies.
//
// 🔴 THE PRECONDITION IS CHECKED UNDER THE SAME LOCK THE WRITE HAPPENS UNDER, and
// checking it outside would make it decorative: two callers could both read revision
// R, both pass, and the second would overwrite the first — the exact lost update the
// precondition exists to refuse.
//
// 🔴 THE NEW BYTES ARE VALIDATED BEFORE THEY LAND, through the index loader's OWN
// mapping. A replace is the only primitive here that can destroy content rather than
// add to it, so a body the reader would classify as MALFORMED is refused instead of
// written: otherwise one bad write turns a served entry into a malformed block and
// the content it replaced is gone.
//
// ⚠ ATTRIBUTION IS NOT ENFORCED HERE, AND THAT IS A DECIDED LIMIT RATHER THAN AN
// OVERSIGHT. "Every appended bullet records actor and session" is a claim about the
// APPEND route. A replace writes the caller's bytes VERBATIM: a body containing a
// forged `[cairn: someone-else/…]` trailer lands exactly as sent, and this server
// does not check it. Enforcing it was considered and DECLINED — a replace exists for
// the whole-file rewrites the store needs, and per-bullet attribution enforcement
// would have to diff the old bullet set against the new one to tell a legitimate
// rewrite from a forged trailer, refusing real edits whenever that diff was wrong.
// The holder of a write-capable token is trusted with the whole file's contents
// already; what is NOT acceptable is CLAIMING otherwise.
func ReplaceEntry(path string, data []byte, ifMatch []string, scope, filename string, interleave func()) (string, error) {
	lock, err := lockEntry(path)
	if err != nil {
		return "", err
	}
	defer lock.unlock()

	original, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	current := EntryRevision(original)
	if !containsTag(ifMatch, current) {
		return "", &PreconditionFailedError{Current: current}
	}
	if err := validateEntryBytes(data, scope, filename); err != nil {
		return "", err
	}
	if interleave != nil {
		interleave()
	}
	if err := replaceBytes(path, data); err != nil {
		return "", err
	}
	return EntryRevision(data), nil
}

// CreateEntry creates an entry that is not there and returns its revision.
//
// The `If-None-Match: *` half of a PUT — RFC 9110 §13.1.2's way of saying "only if
// absent".
//
// 🔴 IT VALIDATES THE BODY THROUGH THE **SAME** LOADER PATH A REPLACE DOES, and that
// is not defensive symmetry — it is the only thing that stops a create landing bytes
// the reader classifies as MALFORMED. A malformed entry is worse on a create than on
// a replace: it is invisible to ref resolution, so the ref it was meant to answer
// keeps resolving to nothing while the file sits there occupying the name, and the
// next create gets a 412 for a file nobody can read.
//
// 🔴 THE VALIDATION RUNS **BEFORE** THE NAME IS CLAIMED, which is the opposite order
// from a replace's (there the precondition must be read under the lock, so the check
// comes first by necessity). Here the whole point is that a refused body must leave
// the store exactly as it found it — including leaving the NAME free, so a caller
// that fixes its body can retry into the same ref.
//
// 🔴 THE PARENT DIRECTORY IS CREATED, AND ONLY THE PARENT. A scope the caller is
// allowed to write that has no directory yet is the genuine first-entry case — it is
// how the store gained every scope it has — and refusing it would make this verb
// unable to do the one thing the local protocol could. Nothing creates ancestors: the
// scope is a single safe path component, so one level is all that can be needed, and a
// missing STORE ROOT is the store-missing error's to report, never something a write
// silently conjures.
func CreateEntry(path string, data []byte, scope, filename string, interleave func()) (string, error) {
	if err := validateEntryBytes(data, scope, filename); err != nil {
		return "", err
	}
	if err := os.Mkdir(filepath.Dir(path), 0o755); err != nil && !errors.Is(err, os.ErrExist) {
		return "", err
	}
	lock, err := lockEntry(path)
	if err != nil {
		return "", err
	}
	defer lock.unlock()
	if err := createBytes(path, data, interleave); err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", &EntryExistsError{Filename: filename}
		}
		return "", err
	}
	return EntryRevision(data), nil
}

// validateEntryBytes answers "would the index loader accept these bytes as an
// entry", through the loader's own mapping and validator.
//
// 🔴 A STRICT DECODE, AND DELIBERATELY NOT THE ENTRY CODEC. This is the CALLER'S
// body, not the store's own bytes: a write is the one primitive that can destroy
// content, so bytes the reader could not parse are refused rather than written.
// Round-tripping them here would let one write leave an entry the index loader
// classifies as MALFORMED. This is the one place the two write primitives are MEANT
// to be strict where the append is permissive, so it is stated rather than left to
// look like the lossy-rewrite bug above.
func validateEntryBytes(data []byte, scope, filename string) error {
	if !utf8.Valid(data) {
		return &BodyNotUTF8Error{Offset: firstInvalidUTF8(data)}
	}
	mapping := store.EntryMapping(string(data), filename, scope)
	if _, err := store.EntryFromMapping(mapping, filename); err != nil {
		return &EntryShapeError{message: "the index loader would reject these bytes: " + err.Error()}
	}
	return nil
}

func firstInvalidUTF8(data []byte) int {
	for i := 0; i < len(data); {
		r, w := utf8.DecodeRune(data[i:])
		if r == utf8.RuneError && w <= 1 {
			return i
		}
		i += w
	}
	return len(data)
}

func containsTag(tags []string, want string) bool {
	for _, tag := range tags {
		if tag == want {
			return true
		}
	}
	return false
}
