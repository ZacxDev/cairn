package write

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/store"
)

// The `[cb:]` ECHO HOLE in ContentHash/BulletContent — the GO half.
//
// 🔴 WHAT THE DEFECT WAS, AND WHY IT IS A REGRESSION RATHER THAN AN INVARIANT.
// `attributionRe` is anchored at `\z` and `BulletContent` stripped it with a single
// `ReplaceAllString`. The moment a read surface appends the per-bullet citation
// token, a stored line ends `… [cairn: a/b] [cb:xxxxxxxx]` — the anchor no longer
// reaches the attribution, so the WHOLE trailer entered the content hash, the
// idempotency check stopped matching, and a retried append landed a near-duplicate
// carrying a `[cb:…]` in its prose whose value is not that new bullet's id.
//
// The echo is MEASURED, not feared: a downstream consumer's resume flow requires its
// report to echo what it recalled, which fires on 451 of 454 runs (99.3%).
//
// 🔴 TWO SIDES, AND FIXING ONE FIXES NOTHING. `AppendBullet` reduced the STORED
// bullet with `BulletContent` and hashed the REQUEST text raw, so even a perfect
// stored-side strip left the two hashes taken over different strings. Both sides now
// go through `BulletContent`.
//
// Every test below was watched RED against the pre-fix source and the matrix is in
// the commit message. `tests/test_bullet_trailer_strip.py` is the Python twin;
// `tests/parity/` is what compares the two.

const (
	trailerProse = "the drill head overheats above 40C"
	trailerAttr  = " [cairn: zach/s1]"
	trailerToken = " [cb:deadbeef]"
)

func TestEveryTrailerOrderReducesToTheSameContent(t *testing.T) {
	// 🔴 ORDER-FREE, WHICH IS WHAT TWO SEQUENTIAL STRIPS ARE NOT. With the
	// attribution stripped first, `… [cairn: a/b] [cb:deadbeef]` keeps its
	// attribution because the `\z` anchor no longer reaches it.
	for _, row := range []struct {
		trailers string
		label    string
	}{
		{trailerAttr, "attribution only — the shape that already worked"},
		{trailerToken, "token only"},
		{trailerAttr + trailerToken, "attribution THEN token — what a read surface prints"},
		{trailerToken + trailerAttr, "token THEN attribution — what RenderBullet produces"},
		{trailerToken + trailerAttr + trailerToken, "a second echo round trip"},
	} {
		got := BulletContent([]string{"- 2000-01-02: " + trailerProse + row.trailers})
		if got != trailerProse {
			t.Errorf("%s: reduced to %q, want %q — the trailer is in the content hash, "+
				"so a re-POST of this bullet will not be recognised",
				row.label, got, trailerProse)
		}
	}
}

func TestAnEchoedBulletIsRecognisedAsADuplicate(t *testing.T) {
	// 🔴 THE DEFECT AS BEHAVIOUR, because the assertions above are about
	// `BulletContent` and the hole lived in the SEAM between it and the request-side
	// hash. A unit test of the strip alone stays green with `wanted` hashed raw.
	dir := t.TempDir()
	path := filepath.Join(dir, "entry.md")
	body := "---\nservice: synth\n---\n\n## What it is\n\nx\n\n## Pointers\n\n- y\n\n" +
		store.NuanceHeading + "\n\n- 2000-01-01: an older note.\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	status, line, _, err := AppendBullet(path, trailerProse, "zach", "s1", "2000-01-02", nil)
	if err != nil {
		t.Fatal(err)
	}
	if status != "appended" {
		t.Fatalf("the first append is %q, so the premise of this test is gone", status)
	}
	if !strings.HasSuffix(line, trailerAttr) {
		t.Fatalf("the stored line carries no attribution: %q", line)
	}

	// What an agent echoes back: the prose plus whatever a read surface appended. It
	// cannot send the `- ` opener — `BulletRequestProblem` refuses that — so the
	// echo is prose and trailers.
	echoed := trailerProse + trailerAttr + trailerToken
	if problem := BulletRequestProblem([]byte("{}"), map[string]any{
		"text": echoed, "session": "s1",
	}); problem != "" {
		t.Fatalf("the echoed text is not an acceptable request (%q), so this test "+
			"would be measuring the validator rather than the hash", problem)
	}

	status2, line2, _, err := AppendBullet(path, echoed, "zach", "s1", "2000-01-03", nil)
	if err != nil {
		t.Fatal(err)
	}
	if status2 != "duplicate" {
		t.Errorf("the echo landed a %q rather than being recognised: %q", status2, line2)
	}
	if line2 != line {
		t.Errorf("the duplicate names %q, not the bullet already on disk %q", line2, line)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(after), "[cb:") {
		t.Error("a citation token reached the curated file, which is the second half " +
			"of the damage: its value is not the new bullet's id")
	}
}

func TestACitationTokenInProseIsNotStripped(t *testing.T) {
	// ⚠ THE NARROWING, SO THE STRIP IS NOT A FREE-FOR-ALL. Only a TRAILING token is
	// machine-written; one mid-sentence is prose, and stripping it would make two
	// different bullets hash alike.
	quoted := "the id [cb:deadbeef] resolves to the wrong bullet"
	if got := BulletContent([]string{"- 2000-01-02: " + quoted}); got != quoted {
		t.Errorf("a mid-sentence token was stripped: %q", got)
	}
}

func TestAMalformedCitationTokenIsNotStripped(t *testing.T) {
	// The class is 8 LOWERCASE HEX, asserted in BOTH directions so neither bound
	// drifts alone: a looser pattern eats prose, a tighter one misses the real thing.
	for _, bad := range []string{
		" [cb:DEADBEEF]", " [cb:deadbee]", " [cb:deadbeef0]", " [cb:zzzzzzzz]",
	} {
		if got := BulletContent([]string{"- 2000-01-02: " + trailerProse + bad}); got == trailerProse {
			t.Errorf("%q was stripped; the token class is 8 lowercase hex and nothing else", bad)
		}
	}
	if got := BulletContent([]string{"- 2000-01-02: " + trailerProse + " [cb:0123456f]"}); got != trailerProse {
		t.Errorf("a well-formed token was NOT stripped (%q) — the class is too tight", got)
	}
}

func TestTheCitationTokenCountsAgainstBulletTextMax(t *testing.T) {
	// ⚠ THE DECISION, PINNED SO THE COMMENT AND THE CODE CANNOT DRIFT APART.
	// `BulletContent`'s doc comment says an echoed token spends 14 of the 2000
	// characters rather than being exempt; this makes that a fact. The cap is
	// measured on the SUBMITTED text, before any strip.
	over := strings.Repeat("x", BulletTextMax-len(trailerToken)+1) + trailerToken
	if len([]rune(over)) != BulletTextMax+1 {
		t.Fatalf("the fixture is %d runes, not %d", len([]rune(over)), BulletTextMax+1)
	}
	problem := BulletRequestProblem([]byte("{}"), map[string]any{
		"text": over, "session": "s1",
	})
	if !strings.Contains(problem, "max") || !strings.Contains(problem, "1 over") {
		t.Errorf("the token was exempted from the cap: %q", problem)
	}
}
