package write

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const entryBody = "---\n" +
	"service: gadget-one\n" +
	"scope: alpha-notes\n" +
	"---\n" +
	"\n" +
	"## What it is\n" +
	"A synthetic entry.\n" +
	"\n" +
	"## Nuance / work-history\n" +
	"- 2000-01-02: the readiness probe reports ready 40s before it is.\n"

func writeEntryFile(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "gadget-one.md")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestContentHashIgnoresTheDateTheActorAndTheSession(t *testing.T) {
	// 🔴 THE IDEMPOTENCY KEY IS THE CONTENT AND NOTHING ELSE, so the same observation
	// re-sent by the same agent after a timeout — or on the next day, or under a
	// different session — is recognised rather than duplicated.
	base := ContentHash("the sidecar drops its lease during a rollout.")
	stored := "- 2000-01-02: the sidecar drops its lease during a rollout. [cairn: wide-reader/conf-1]"
	if ContentHash(BulletContent([]string{stored})) != base {
		t.Fatal("a stored bullet's date and attribution must not enter its hash")
	}
	otherDay := "- 2026-09-14: the sidecar drops its lease during a rollout. [cairn: someone/else]"
	if ContentHash(BulletContent([]string{otherDay})) != base {
		t.Fatal("a different day and a different actor are the same CONTENT")
	}
	// Whitespace is collapsed, so a re-POST that differs only in wrapping is the same
	// bullet — including across the wrap itself.
	wrapped := []string{"- 2000-01-02: the sidecar drops its lease", "  during a rollout."}
	if ContentHash(BulletContent(wrapped)) != base {
		t.Fatalf("a wrapped bullet is one bullet: %q", BulletContent(wrapped))
	}
	// …and a genuinely different sentence is a different bullet.
	if ContentHash("something else entirely") == base {
		t.Fatal("different content must hash differently")
	}
}

func TestAttributionIsASuffixAndTheActorIsAParameter(t *testing.T) {
	line := RenderBullet("  a note  ", "wide-reader", "conf-1", "2000-01-05")
	want := "- 2000-01-05: a note [cairn: wide-reader/conf-1]"
	if line != want {
		t.Fatalf("\n got: %s\nwant: %s", line, want)
	}
	// 🔴 THE POSITION IS LOAD-BEARING. The store's bullet grammar is a PREFIX grammar
	// anchored at position 0, so writing the actor between the date and the text parses
	// as NO MARKER — the badge silently stops rendering, and a vanished badge looks
	// like success. A suffix leaves every prefix rule untouched.
	if !strings.HasPrefix(line, "- 2000-01-05: ") {
		t.Fatal("the dated opener must stay at position 0")
	}
	if !strings.HasSuffix(line, "[cairn: wide-reader/conf-1]") {
		t.Fatal("the attribution must be a suffix")
	}
	// An `OPEN:` bullet must still declare itself after the trailer is added.
	open := RenderBullet("OPEN: rotate the credential", "reader", "s1", "2000-01-05")
	if !strings.HasPrefix(open, "- 2000-01-05: OPEN: ") {
		t.Fatalf("an appended OPEN: bullet must still parse as one: %s", open)
	}
}

func TestAppendBullet(t *testing.T) {
	t.Run("the splice leaves every other byte IDENTICAL", func(t *testing.T) {
		// 🔴 A SPLICE, NEVER A DECODE-AND-REJOIN. The lossy rejoin this replaces
		// silently rewrote the WHOLE file on every append: a byte that is not valid
		// UTF-8 became a replacement character permanently at `200 appended`, every
		// CRLF became LF, and a file with no trailing newline gained one — each of
		// which also changes the revision and invalidates every other client's
		// precondition for a change nobody asked for.
		body := "---\nservice: g\nscope: s\n---\n\r\n## Nuance / work-history\r\n" +
			"- 2000-01-02: an existing note.\r\nlegacy \xff byte here"
		path := writeEntryFile(t, body)
		status, line, _, err := AppendBullet(path, "a new note.", "reader", "s1", "2000-01-05", nil)
		if err != nil {
			t.Fatal(err)
		}
		if status != "appended" {
			t.Fatalf("status %q", status)
		}
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		// The inserted line carries the heading's OWN terminator, so CRLF stays CRLF.
		if !strings.Contains(string(after), "## Nuance / work-history\r\n"+line+"\r\n") {
			t.Fatalf("the splice did not preserve the heading's terminator:\n%q", string(after))
		}
		// Removing the inserted run must give back the original, byte for byte —
		// including the invalid byte and the missing trailing newline.
		restored := strings.Replace(string(after), line+"\r\n", "", 1)
		if restored != body {
			t.Fatalf("the untouched region changed:\n got: %q\nwant: %q", restored, body)
		}
	})

	t.Run("a heading that is the final line with no terminator keeps it that way", func(t *testing.T) {
		body := "---\nservice: g\nscope: s\n---\n\n## Nuance / work-history"
		path := writeEntryFile(t, body)
		_, line, _, err := AppendBullet(path, "first note.", "reader", "s1", "2000-01-05", nil)
		if err != nil {
			t.Fatal(err)
		}
		after, _ := os.ReadFile(path)
		if string(after) != body+"\n"+line {
			t.Fatalf("got %q", string(after))
		}
		if strings.HasSuffix(string(after), "\n") {
			t.Fatal("a file that did not end in a newline must not gain one")
		}
	})

	t.Run("a duplicate writes NOT ONE BYTE and echoes the STORED line", func(t *testing.T) {
		path := writeEntryFile(t, entryBody)
		before, _ := os.ReadFile(path)
		status, line, revision, err := AppendBullet(
			path, "the readiness probe reports ready 40s before it is.",
			"reader", "s1", "2000-01-05", nil)
		if err != nil {
			t.Fatal(err)
		}
		if status != "duplicate" {
			t.Fatalf("status %q", status)
		}
		if line != "- 2000-01-02: the readiness probe reports ready 40s before it is." {
			t.Fatalf("the echoed line must be the one ALREADY ON DISK, got %q", line)
		}
		after, _ := os.ReadFile(path)
		if string(after) != string(before) {
			t.Fatal("a duplicate must write nothing")
		}
		if revision != EntryRevision(before) {
			t.Fatal("…and must report the unchanged revision")
		}
	})

	t.Run("an entry with no nuance heading is refused", func(t *testing.T) {
		path := writeEntryFile(t, "---\nservice: g\nscope: s\n---\n\n## Pointers\n- x\n")
		_, _, _, err := AppendBullet(path, "a note.", "reader", "s1", "2000-01-05", nil)
		var shape *EntryShapeError
		if !errors.As(err, &shape) {
			t.Fatalf("want an entry-shape refusal, got %v", err)
		}
		if !strings.Contains(err.Error(), "nowhere to go") {
			t.Fatalf("the refusal must say why: %s", err)
		}
	})

	t.Run("a bullet in the SECOND nuance section is NOT a duplicate", func(t *testing.T) {
		// 🔴 THE INSERTION SCOPE AND THE DEDUPE SCOPE ARE THE SAME SECTION. When they
		// were not, an entry carrying the heading twice answered `duplicate` — writing
		// nothing — for a genuinely NEW bullet that merely matched one sitting in a
		// section this writer would never have inserted into. Content loss in the
		// direction the design says matters most, and silent: the response says the
		// observation is already recorded.
		body := "---\nservice: g\nscope: s\n---\n\n" +
			"## Nuance / work-history\n- first section note.\n\n" +
			"## Pointers\n- x\n\n" +
			"## Nuance / work-history\n- second section note.\n"
		path := writeEntryFile(t, body)
		status, _, _, err := AppendBullet(path, "second section note.", "reader", "s1", "2000-01-05", nil)
		if err != nil {
			t.Fatal(err)
		}
		if status != "appended" {
			t.Fatalf("a bullet matching only the SECOND section must be appended, got %q", status)
		}
	})

	t.Run("two concurrent appends of DIFFERENT bullets both survive", func(t *testing.T) {
		// 🔴 THE SHIP GATE, AND THE OVERLAP IS FORCED RATHER THAN HOPED FOR. A
		// concurrent-append test driven by wall-clock timing proves nothing on the run
		// where the two goroutines happen not to overlap, and the defect it guards
		// against DESTROYS CONTENT rather than availability — so the interleave seam
		// sits exactly in the window a missing lock leaves open: after the read, before
		// the write.
		path := writeEntryFile(t, entryBody)
		release := make(chan struct{})
		var once sync.Once
		gate := func() {
			// The FIRST writer to reach the seam waits for the second to have read the
			// same original bytes; the second opens the gate. Without the lock both
			// would splice into the same original and the second rename would discard
			// the first append.
			once.Do(func() { close(release) })
			<-release
		}
		var wg sync.WaitGroup
		results := make([]string, 2)
		errs := make([]error, 2)
		for i, text := range []string{"first concurrent note.", "second concurrent note."} {
			wg.Add(1)
			go func(i int, text string) {
				defer wg.Done()
				status, _, _, err := AppendBullet(path, text, "reader", "s1", "2000-01-05", gate)
				results[i], errs[i] = status, err
			}(i, text)
		}
		wg.Wait()
		for i, err := range errs {
			if err != nil {
				t.Fatalf("append %d failed: %v", i, err)
			}
			if results[i] != "appended" {
				t.Fatalf("append %d: status %q", i, results[i])
			}
		}
		after, _ := os.ReadFile(path)
		for _, want := range []string{"first concurrent note.", "second concurrent note."} {
			if !strings.Contains(string(after), want) {
				t.Fatalf("a concurrent append was LOST: %q missing from\n%s", want, after)
			}
		}
	})
}

func TestParseIfMatch(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		// 🔴 IT IS A **LIST**. Reading it as one opaque string made
		// `If-Match: "stale", "<correct>"` — the header a conformant library builds from
		// a list — a permanent 412: fail-closed, and still a bug, because a client that
		// cannot succeed re-sends without the precondition.
		{`"stale", "abc123"`, []string{"stale", "abc123"}},
		{`"abc123"`, []string{"abc123"}},
		// Case is folded because a hex digest is lower-case and hex is not.
		{`"ABC123"`, []string{"abc123"}},
		{`W/"abc123"`, []string{"abc123"}},
		{`w/"abc123"`, []string{"abc123"}},
		{`*`, []string{"*"}},
		// A header that is present and names NO entity-tag is not a precondition.
		{`,`, nil},
		{``, nil},
	}
	for _, tc := range cases {
		got := ParseIfMatch(tc.in)
		if len(got) != len(tc.want) || strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Fatalf("ParseIfMatch(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestReplaceEntry(t *testing.T) {
	valid := "---\nservice: gadget-one\nscope: alpha-notes\n---\n\n## Nuance / work-history\n- x\n"

	t.Run("a correct precondition replaces and returns the new revision", func(t *testing.T) {
		path := writeEntryFile(t, entryBody)
		current := EntryRevision([]byte(entryBody))
		revision, err := ReplaceEntry(path, []byte(valid), []string{current}, "alpha-notes", "gadget-one.md", nil)
		if err != nil {
			t.Fatal(err)
		}
		if revision != EntryRevision([]byte(valid)) {
			t.Fatal("the revision must be the hash of the bytes THIS request sent")
		}
		after, _ := os.ReadFile(path)
		if string(after) != valid {
			t.Fatal("the bytes must land verbatim")
		}
	})

	t.Run("ANY of the candidate revisions is enough", func(t *testing.T) {
		path := writeEntryFile(t, entryBody)
		current := EntryRevision([]byte(entryBody))
		if _, err := ReplaceEntry(path, []byte(valid),
			[]string{"0000000000000000", current}, "alpha-notes", "gadget-one.md", nil); err != nil {
			t.Fatalf("RFC 9110 §13.1.1: the write proceeds if any tag matches: %v", err)
		}
	})

	t.Run("a stale precondition leaves the file UNCHANGED and names the current revision", func(t *testing.T) {
		path := writeEntryFile(t, entryBody)
		_, err := ReplaceEntry(path, []byte(valid), []string{"0000000000000000"},
			"alpha-notes", "gadget-one.md", nil)
		var precondition *PreconditionFailedError
		if !errors.As(err, &precondition) {
			t.Fatalf("want a precondition failure, got %v", err)
		}
		if precondition.Current != EntryRevision([]byte(entryBody)) {
			t.Fatal("a client told only `no` cannot retry, so the CURRENT revision rides the refusal")
		}
		after, _ := os.ReadFile(path)
		if string(after) != entryBody {
			t.Fatal("the file must be untouched")
		}
	})

	t.Run("bytes the loader would reject are refused and NOT written", func(t *testing.T) {
		// 🔴 A REPLACE IS THE ONLY PRIMITIVE THAT CAN DESTROY CONTENT, so a body the
		// reader would classify as MALFORMED is refused instead of written: otherwise one
		// bad write turns a served entry into a malformed block and what it replaced is
		// gone.
		path := writeEntryFile(t, entryBody)
		current := EntryRevision([]byte(entryBody))
		_, err := ReplaceEntry(path, []byte("not an entry at all\n"), []string{current},
			"alpha-notes", "gadget-one.md", nil)
		var shape *EntryShapeError
		if !errors.As(err, &shape) {
			t.Fatalf("want an entry-shape refusal, got %v", err)
		}
		if !strings.Contains(err.Error(), "the index loader would reject these bytes: "+
			"malformed index entry 'gadget-one.md': missing or empty `service:`") {
			t.Fatalf("the 422 body quotes the loader verbatim: %s", err)
		}
		after, _ := os.ReadFile(path)
		if string(after) != entryBody {
			t.Fatal("nothing may be written")
		}
	})

	t.Run("a body that is not valid UTF-8 is refused", func(t *testing.T) {
		path := writeEntryFile(t, entryBody)
		current := EntryRevision([]byte(entryBody))
		_, err := ReplaceEntry(path, []byte("---\nservice: g\xff\nscope: s\n---\n"),
			[]string{current}, "alpha-notes", "gadget-one.md", nil)
		var notUTF8 *BodyNotUTF8Error
		if !errors.As(err, &notUTF8) {
			t.Fatalf("want a strict-decode refusal, got %v", err)
		}
	})

	t.Run("attribution is NOT enforced, and that is a decided limit", func(t *testing.T) {
		// ⚠ A REPLACE WRITES THE CALLER'S BYTES VERBATIM, forged trailers included.
		// Enforcing per-bullet attribution would have to diff the old bullet set against
		// the new one to tell a legitimate rewrite from a forgery, refusing real edits
		// whenever that diff was wrong. What is NOT acceptable is CLAIMING otherwise,
		// which is why this limit is pinned rather than left to be assumed.
		forged := "---\nservice: gadget-one\nscope: alpha-notes\n---\n\n" +
			"## Nuance / work-history\n- 2000-01-02: a note. [cairn: someone-else/sess-9]\n"
		path := writeEntryFile(t, entryBody)
		current := EntryRevision([]byte(entryBody))
		if _, err := ReplaceEntry(path, []byte(forged), []string{current},
			"alpha-notes", "gadget-one.md", nil); err != nil {
			t.Fatalf("a forged trailer is written verbatim by design: %v", err)
		}
		after, _ := os.ReadFile(path)
		if !strings.Contains(string(after), "[cairn: someone-else/sess-9]") {
			t.Fatal("the bytes must land as sent")
		}
	})
}

func TestCreateEntry(t *testing.T) {
	valid := "---\nservice: newcomer-five\nscope: beta-notes\n---\n\n## Nuance / work-history\n- x\n"

	t.Run("it creates the parent scope directory and only the parent", func(t *testing.T) {
		root := t.TempDir()
		target := filepath.Join(root, "beta-notes", "newcomer-five.md")
		revision, err := CreateEntry(target, []byte(valid), "beta-notes", "newcomer-five.md", nil)
		if err != nil {
			t.Fatal(err)
		}
		if revision != EntryRevision([]byte(valid)) {
			t.Fatal("the revision is the hash of the bytes sent")
		}
		// …and a MISSING STORE ROOT is not something a write silently conjures.
		deep := filepath.Join(root, "nope", "deeper", "x.md")
		if _, err := CreateEntry(deep, []byte(valid), "deeper", "x.md", nil); err == nil {
			t.Fatal("only ONE level is created: a missing store root is the reader's to report")
		}
	})

	t.Run("a taken name is reported and the existing bytes are untouched", func(t *testing.T) {
		root := t.TempDir()
		dir := filepath.Join(root, "beta-notes")
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(dir, "newcomer-five.md")
		if err := os.WriteFile(target, []byte("occupied\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := CreateEntry(target, []byte(valid), "beta-notes", "newcomer-five.md", nil)
		var exists *EntryExistsError
		if !errors.As(err, &exists) {
			t.Fatalf("want an already-exists refusal, got %v", err)
		}
		after, _ := os.ReadFile(target)
		if string(after) != "occupied\n" {
			t.Fatal("🔴 A MALFORMED FILE ALREADY OCCUPYING THE NAME IS NOT OVERWRITTEN")
		}
	})

	t.Run("a refused body leaves the NAME free", func(t *testing.T) {
		// 🔴 THE VALIDATION RUNS BEFORE THE NAME IS CLAIMED, which is the opposite
		// order from a replace's. The point is that a caller who fixes its body can
		// retry into the same ref.
		root := t.TempDir()
		target := filepath.Join(root, "beta-notes", "rejected-eight.md")
		if _, err := CreateEntry(target, []byte("not an entry\n"), "beta-notes", "rejected-eight.md", nil); err == nil {
			t.Fatal("invalid bytes must be refused")
		}
		if _, err := os.Stat(target); !os.IsNotExist(err) {
			t.Fatalf("the name must stay free, got %v", err)
		}
	})

	t.Run("a reader NEVER sees a half-written entry", func(t *testing.T) {
		// 🔴 THE NAME AND THE BYTES ARE PUBLISHED IN ONE STEP, AND ONLY A RACING READER
		// CAN SEE THE DIFFERENCE. An exclusive create at the target claims the name and
		// only THEN starts writing, so a concurrent read can glob a `*.md` that is EMPTY
		// or half-written and serve it as a MALFORMED entry — the partial-read hazard the
		// replace path avoids, reproduced by the create path. Linking a complete file
		// publishes both at once.
		//
		// ⚠ IT IS A RACE, SO IT IS MADE WIDE RATHER THAN HOPED FOR: the body is large
		// enough that an incremental write cannot complete between two polls, and the
		// reader spins rather than sleeping. Measured: with the link replaced by an
		// exclusive open plus a write, this observes a short file; unmutated, it never
		// does over 20 runs.
		root := t.TempDir()
		if err := os.Mkdir(filepath.Join(root, "beta-notes"), 0o755); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(root, "beta-notes", "bulky.md")
		filler := strings.Repeat("- 2000-01-02: a long enough note to span many writes.\n", 120000)
		body := "---\nservice: bulky\nscope: beta-notes\n---\n\n## Nuance / work-history\n" + filler
		stop := make(chan struct{})
		short := make(chan int, 1)
		go func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				if data, err := os.ReadFile(target); err == nil && len(data) != len(body) {
					select {
					case short <- len(data):
					default:
					}
					return
				}
			}
		}()
		if _, err := CreateEntry(target, []byte(body), "beta-notes", "bulky.md", nil); err != nil {
			close(stop)
			t.Fatal(err)
		}
		close(stop)
		select {
		case n := <-short:
			t.Fatalf("a reader saw the entry at %d of %d bytes: the name was published "+
				"before the content", n, len(body))
		default:
		}
	})

	t.Run("two concurrent creates: exactly ONE wins", func(t *testing.T) {
		// 🔴 THE EXCLUSIVITY IS THE FILESYSTEM'S, NOT A CHECK THIS PROCESS MAKES. An
		// existence test followed by a write is a TOCTOU: both racers see nothing and the
		// second silently destroys the first — the same lost update the precondition
		// exists to refuse, arriving through the one verb that has no prior revision to
		// name. The seam forces the overlap at the moment the bytes are ready and the
		// name is not yet claimed.
		root := t.TempDir()
		if err := os.Mkdir(filepath.Join(root, "beta-notes"), 0o755); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(root, "beta-notes", "newcomer-five.md")
		release := make(chan struct{})
		var once sync.Once
		gate := func() {
			once.Do(func() { close(release) })
			<-release
		}
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for i := range errs {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_, errs[i] = CreateEntry(target, []byte(valid), "beta-notes", "newcomer-five.md", gate)
			}(i)
		}
		wg.Wait()
		won, lost := 0, 0
		for _, err := range errs {
			var exists *EntryExistsError
			switch {
			case err == nil:
				won++
			case errors.As(err, &exists):
				lost++
			default:
				t.Fatalf("unexpected error: %v", err)
			}
		}
		if won != 1 || lost != 1 {
			t.Fatalf("exactly one create may win: won=%d lost=%d", won, lost)
		}
	})
}

func TestBulletRequestProblem(t *testing.T) {
	ok := map[string]any{"text": "a note.", "session": "conf-1"}
	if problem := BulletRequestProblem([]byte(`{}`), ok); problem != "" {
		t.Fatalf("a well-formed request must be accepted, got %q", problem)
	}

	// 🔴 AN `actor` KEY IS ACCEPTED AND DISCARDED — accepted because a client library
	// may send one and a 400 there would be a compatibility trap, discarded because a
	// client-supplied actor lets any token-holder attribute a bullet to somebody else.
	forged := map[string]any{"text": "a note.", "session": "conf-1", "actor": "somebody-else"}
	if problem := BulletRequestProblem([]byte(`{}`), forged); problem != "" {
		t.Fatalf("an actor key must not turn the request into a 400, got %q", problem)
	}

	cases := []struct {
		name    string
		payload any
		want    string
	}{
		{"not an object", []any{}, "the body must be a JSON object"},
		{"no text", map[string]any{"session": "s"}, "`text` is required"},
		{"blank text", map[string]any{"text": "   ", "session": "s"}, "`text` is required"},
		{"text is not a string", map[string]any{"text": 1.0, "session": "s"}, "`text` is required"},
		{"no session", map[string]any{"text": "x"}, "`session` is required and must match"},
		{"a space in the session", map[string]any{"text": "x", "session": "bad session"},
			"`session` is required and must match"},
		{"an embedded newline", map[string]any{"text": "a\nb", "session": "s"},
			"`text` must be ONE line"},
		// 🔴 THE CASE THE TWO-CHARACTER CHECK WALKED PAST, AND THE REASON THIS CLAUSE
		// ASKS THE SPLITTER INSTEAD OF NAMING CHARACTERS. A literal U+2028 was
		// MEASURED accepted `200 appended` on the oracle, and the ONE line the server
		// rendered became TWO on the next read — the first carrying the caller's prose
		// with no attribution trailer at all, the second an `OPEN:`-marked bullet whose
		// leading `[cairn: …]` an operator reads as somebody ELSE's attribution.
		//
		// ⚠ IT IS HERE BECAUSE A MUTATION SWEEP FOUND ITS ABSENCE: replacing the
		// clause with `strings.Contains(text, "\n") || strings.Contains(text, "\r")`
		// — the exact defect the clause replaced — SURVIVED this table until this row
		// existed. The other nine separators are covered by the same clause and are
		// pinned in `pytext`; this is the one that was measured in the field.
		{"a U+2028 LINE SEPARATOR, which a two-character check misses",
			map[string]any{"text": "a" + string(rune(0x2028)) + "b", "session": "s"},
			"`text` must be ONE line"},
		{"a U+0085 NEXT LINE, for the same reason",
			map[string]any{"text": "a" + string(rune(0x85)) + "b", "session": "s"},
			"`text` must be ONE line"},
		{"a TRAILING newline, which the splitter folds away",
			map[string]any{"text": "a\n", "session": "s"}, "`text` must be ONE line"},
		{"a markdown bullet opener", map[string]any{"text": "- a note", "session": "s"},
			"must not open a markdown bullet"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			problem := BulletRequestProblem([]byte(`{}`), tc.payload)
			if !strings.Contains(problem, tc.want) {
				t.Fatalf("\n got: %q\nwant substring: %q", problem, tc.want)
			}
		})
	}

	t.Run("the over-long message names the OVERAGE", func(t *testing.T) {
		// 🔴 THE EXCESS IS THE ONLY NUMBER THE CALLER CAN ACT ON. Two absolute figures
		// make a caller subtract on every retry, and a caller over by a thousand has to
		// rewrite rather than trim — a different decision, taken from a number they had
		// to compute themselves.
		long := strings.Repeat("x", BulletTextMax+1)
		problem := BulletRequestProblem([]byte(`{}`), map[string]any{"text": long, "session": "s"})
		want := "`text` is 2001 characters, max 2000 — 1 over"
		if problem != want {
			t.Fatalf("\n got: %q\nwant: %q", problem, want)
		}
	})

	t.Run("a control or formatting character is named by CODE POINT", func(t *testing.T) {
		// Named by code point because the offending character is by definition one the
		// caller cannot see in their own error message. Each of these was MEASURED
		// landing in the curated file at `200 appended` before the category test existed.
		for _, tc := range []struct {
			r    rune
			want string
		}{
			{0x00, "U+0000 (Cc)"},   // makes text tools treat the entry as BINARY
			{0x1b, "U+001B (Cc)"},   // rewrites the terminal of whoever renders it
			{0x202e, "U+202E (Cf)"}, // reorders the rendered line: visual spoofing
			{0x200b, "U+200B (Cf)"}, // invisible AND defeats idempotency
			{0x09, "U+0009 (Cc)"},   // ⚠ a TAB is `Cc` and is therefore refused
			{0xe000, "U+E000 (Co)"}, // private use
		} {
			problem := BulletRequestProblem([]byte(`{}`),
				map[string]any{"text": "a" + string(tc.r) + "b", "session": "s"})
			if !strings.Contains(problem, tc.want) {
				t.Fatalf("U+%04X: got %q, want substring %q", tc.r, problem, tc.want)
			}
			if !strings.Contains(problem, "must not be written into a curated entry") {
				t.Fatalf("U+%04X: the refusal must say why: %q", tc.r, problem)
			}
		}
	})

	t.Run("an unpaired surrogate ESCAPE in the raw body is refused", func(t *testing.T) {
		// 🔴 A GO-SIDE GUARD WITH NO ORACLE COUNTERPART, IN THE STRICT DIRECTION.
		// CPython's decoder yields a LONE SURROGATE for `\udc80`, so the `Cs` clause
		// refuses it there; Go's replaces it with U+FFFD, whose category is `So`, so the
		// bullet would otherwise be STORED carrying a replacement character the oracle
		// refuses. Scanning the raw body closes that.
		raw := []byte(`{"text":"a\udc80b","session":"s"}`)
		payload, err := DecodeBulletBody(raw)
		if err != nil {
			t.Fatalf("the decoder accepts it, which is exactly the problem: %v", err)
		}
		problem := BulletRequestProblem(raw, payload)
		if !strings.Contains(problem, "unpaired surrogate escape") {
			t.Fatalf("got %q", problem)
		}
		// The CONTROL on the claim: a PAIRED surrogate escape is ordinary text and must
		// pass, or the guard would refuse every astral-plane character.
		paired := []byte(`{"text":"a😀b","session":"s"}`)
		payload, err = DecodeBulletBody(paired)
		if err != nil {
			t.Fatal(err)
		}
		if problem := BulletRequestProblem(paired, payload); problem != "" {
			t.Fatalf("a paired surrogate escape is ordinary text: %q", problem)
		}
	})

	t.Run("a session at the class boundary is accepted and one past it is not", func(t *testing.T) {
		at := "a" + strings.Repeat("b", 63)
		if problem := BulletRequestProblem([]byte(`{}`),
			map[string]any{"text": "x", "session": at}); problem != "" {
			t.Fatalf("a 64-character session is inside the class: %q", problem)
		}
		over := at + "c"
		if problem := BulletRequestProblem([]byte(`{}`),
			map[string]any{"text": "x", "session": over}); problem == "" {
			t.Fatal("a 65-character session is outside it")
		}
	})
}

func TestDecodeBulletBodyRefusesRatherThanCrashing(t *testing.T) {
	// 🔴 THE ANSWERABLE HALF OF THE TWO ORACLE-SPECIFIC CORPUS CASES. Their bodies quote
	// CPython's own parser diagnostic, which is why they are asserted against the oracle
	// only (see `tests/conformance/requests.json`). What a port must still get right is
	// that a malformed or deeply nested body is an ERROR it can answer — not a dropped
	// connection — and that is pinned here.
	for _, body := range []string{
		"{not json",
		strings.Repeat("[", 50),
		strings.Repeat("[", 200000),
		"",
	} {
		if _, err := DecodeBulletBody([]byte(body)); err == nil {
			t.Fatalf("a malformed body must be an error, not a success: %.20q", body)
		}
	}
	if _, err := DecodeBulletBody([]byte(`{"text":"x","session":"s"}`)); err != nil {
		t.Fatalf("the positive control: a valid body must decode: %v", err)
	}
}
