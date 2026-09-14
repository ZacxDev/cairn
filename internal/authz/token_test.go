package authz

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// 🔴 EVERY EXPECTATION IN THIS FILE IS SPELLED BY HAND, NEVER DERIVED FROM THE CODE
// UNDER TEST. A test that interpolates the constant it asserts on asserts `x == x` —
// so the numbers below (43, 32, 42, 4) are written out, and the relation between them
// gets its own test that pins the RELATION rather than the numbers.

// aToken is a credential of exactly the minimum length, built from the character class
// a real generator emits. 🔴 IT IS ASSEMBLED AT RUN TIME AND NEVER WRITTEN AS ONE
// LITERAL: `tests/leakscan.py` refuses a real-looking credential in a public
// repository on sight, and it is right to — a 43-character run of the token charset
// is indistinguishable from a live secret to a reviewer as well as to the scanner.
func aToken(seed byte) string {
	return strings.Repeat(string([]byte{seed}), MinTokenChars)
}

func TestTheRowGrammar(t *testing.T) {
	// 🔴 THE REFUSALS ARE THE POINT OF THIS TABLE, NOT THE ACCEPTANCES. Every guard
	// in the ladder is here with an input no EARLIER guard rejects, which is what makes
	// each row reachable rather than shadowed — the same discipline the ladder itself is
	// built to.
	cases := []struct {
		name string
		row  []string
		// wantErr is a substring of the refusal. The whole sentence is a diagnostic
		// and should stay free to improve; the CLAUSE that identifies which guard
		// fired is what a test may pin.
		wantErr string
		// For accepted rows:
		wantIdentity string
		wantScopes   []string
		wantLegacy   bool
	}{
		{
			name:         "a bare token is the legacy record",
			row:          []string{aToken('a')},
			wantIdentity: "legacy",
			wantLegacy:   true,
		},
		{
			name:         "a mapped row carries its identity and folded scopes",
			row:          []string{aToken('a'), "wide-reader", "Alpha_Notes,beta-notes"},
			wantIdentity: "wide-reader",
			wantScopes:   []string{"alpha-notes", "beta-notes"},
		},
		{
			name:         "a repeated scope is deduped, not refused",
			row:          []string{aToken('a'), "reader", "alpha,alpha"},
			wantIdentity: "reader",
			wantScopes:   []string{"alpha"},
		},
		{
			name:    "guard 6 — two fields is neither shape",
			row:     []string{aToken('a'), "reader"},
			wantErr: "2 fields, expected 1 (a bare legacy token) or 3",
		},
		{
			name:    "guard 6 — four fields, and the comma hint fires on evidence",
			row:     []string{aToken('a'), "reader", "alpha,", "beta"},
			wantErr: "NO SPACES",
		},
		{
			name: "guard 6 — four fields with NO comma gets the two-tokens sentence and not the hint",
			row:  []string{aToken('a'), aToken('b'), "reader", "alpha"},
			// 🔴 THE HINT IS CONDITIONAL ON EVIDENCE IN THE ROW, and this row is the
			// control for that: the `<tokenA> <tokenB>` slip has no comma, so the
			// comma advice would be a guess. Asserting its ABSENCE is what makes the
			// conditional a guard rather than a decoration.
			wantErr: "two tokens on one line is no longer two tokens",
		},
		{
			name:    "guard 7 — an identity that is not the class",
			row:     []string{aToken('a'), "Reader", "alpha"},
			wantErr: "field 2 is not an identity",
		},
		{
			name: "guard 7 — a TOKEN in the identity field, which is the shape it exists for",
			row:  []string{aToken('a'), aToken('b'), "alpha"},
			// 🔴 THE VALUE IS DESCRIBED, NEVER QUOTED. This row is the reachability
			// argument AND the leak control: a token is at least 43 characters and an
			// identity at most 32, so a mis-pasted credential can ONLY land here.
			wantErr: "the value is NOT echoed",
		},
		{
			name:    "guard 8 — a mapped row may not claim the reserved identity",
			row:     []string{aToken('a'), "legacy", "alpha"},
			wantErr: "reserved identity",
		},
		{
			name:    "guard 9 — a bare comma names no scope",
			row:     []string{aToken('a'), "reader", ","},
			wantErr: "empty scope allowlist",
		},
		{
			name:    "guard 10 — a scope outside the charset",
			row:     []string{aToken('a'), "reader", "alpha notes"},
			wantErr: "is not a scope",
		},
		{
			name: "guard 10 — a scope that folds away to nothing",
			row:  []string{aToken('a'), "reader", "___"},
			// The class ALONE accepts `___`; folding it produces the empty string, so
			// the grant would read as working and do nothing. This row is what makes
			// the second clause a guard rather than a restatement of the first.
			wantErr: "folds away to nothing",
		},
		{
			name: "guard 10 — a REAL token in the scope field, which the charset cannot catch",
			row:  []string{aToken('a'), "reader", aToken('b')},
			// 🔴 THE LENGTH CLAUSE IS NOT A THIRD SPELLING OF THE CHARSET ONE. A
			// generated token is drawn from `[A-Za-z0-9_-]` — the scope charset exactly
			// — and folds to a non-empty ref, so only the CAP refuses it. The Python
			// fixture that "covered" this appended an `=`, a character the generator
			// never produces, and the realistic input loaded CLEAN.
			wantErr: "long enough to be a credential",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			record, err := ParseTokenRow(tc.row, 3, 6)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected a refusal containing %q, got a record %+v", tc.wantErr, record)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("refusal does not name this guard:\n got: %s\nwant substring: %s", err, tc.wantErr)
				}
				if !strings.Contains(err.Error(), "line 3 of 6") {
					t.Fatalf("every refusal names the PHYSICAL line, got: %s", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected refusal: %v", err)
			}
			if record.Identity != tc.wantIdentity {
				t.Fatalf("identity: got %q want %q", record.Identity, tc.wantIdentity)
			}
			if record.IsLegacy() != tc.wantLegacy {
				t.Fatalf("IsLegacy: got %v want %v", record.IsLegacy(), tc.wantLegacy)
			}
			if strings.Join(record.Scopes, ",") != strings.Join(tc.wantScopes, ",") {
				t.Fatalf("scopes: got %v want %v", record.Scopes, tc.wantScopes)
			}
		})
	}
}

func TestNoRefusalEverEchoesTheFieldItRefused(t *testing.T) {
	// 🔴 THE PROPERTY IS ENFORCED, NOT ASSERTED ROW BY ROW. It drives a
	// SECRET-BEARING row through the two guards a mis-paste can reach and requires the
	// message to contain no run of the token charset long enough to BE a credential —
	// an assertion that needs to know neither the secret nor which field carried it, so
	// it also catches a future guard that starts quoting something nobody capped.
	secret := aToken('z')
	for _, row := range [][]string{
		{aToken('a'), secret, "alpha"},
		{aToken('a'), "reader", secret},
	} {
		_, err := ParseTokenRow(row, 1, 1)
		if err == nil {
			t.Fatalf("a row with a credential in a non-token field must be refused: %v", row)
		}
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("the refusal echoes the credential verbatim: %s", err)
		}
		if longestTokenCharRun(err.Error()) >= MinTokenChars {
			t.Fatalf("the refusal carries a %d-character run of the token charset, "+
				"which is indistinguishable from a credential: %s",
				longestTokenCharRun(err.Error()), err)
		}
		if !strings.Contains(err.Error(), "fp=") {
			t.Fatalf("the refusal must carry the FINGERPRINT, which is what ties a "+
				"mis-pasted credential to the audit line it will appear under: %s", err)
		}
	}
}

// longestTokenCharRun is the longest run of `[A-Za-z0-9_-]` in a string.
//
// ⚠ IT IS WIDER THAN A SUBSTRING CHECK ON PURPOSE. A refusal that echoed the value
// CASE-FOLDED, or with `_` substituted, would pass a substring test and still publish
// most of the credential's entropy — which is exactly the shape the Python guard 11
// leak took.
func longestTokenCharRun(s string) int {
	best, run := 0, 0
	for _, r := range s {
		isTokenChar := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_' || r == '-'
		if isTokenChar {
			run++
			if run > best {
				best = run
			}
			continue
		}
		run = 0
	}
	return best
}

func TestAScopeCannotBeLongEnoughToBeAToken(t *testing.T) {
	// 🔴 THE RELATION, NOT THE NUMBER. `MaxScopeChars < MinTokenChars` is the whole
	// safety argument — "a value this server would accept as a token cannot be read as
	// a scope name" — and a hand-typed cap is one edit from being a number that
	// silently re-opens it. Same for the identity cap.
	if MaxScopeChars >= MinTokenChars {
		t.Fatalf("a scope may be %d characters and a token floor is %d: a credential "+
			"pasted into field 3 would be a legal scope name", MaxScopeChars, MinTokenChars)
	}
	if MaxIdentityChars >= MinTokenChars {
		t.Fatalf("an identity may be %d characters and a token floor is %d: a "+
			"credential pasted into field 2 could be read as an identity",
			MaxIdentityChars, MinTokenChars)
	}
}

func writeTokenFile(t *testing.T, rows ...string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "tokens")
	if err := os.WriteFile(path, []byte(strings.Join(rows, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTheCrossRowGuards(t *testing.T) {
	a, b := aToken('a'), aToken('b')

	t.Run("guard 11 refuses one credential with two authorities", func(t *testing.T) {
		path := writeTokenFile(t, a+" reader alpha", a+" writer beta")
		_, err := LoadTokens(path, nil, nil)
		if err == nil || !strings.Contains(err.Error(), "duplicate token on lines 1 and 2") {
			t.Fatalf("want a duplicate-token refusal naming both lines, got %v", err)
		}
	})

	t.Run("guard 11 collapses one grant written twice", func(t *testing.T) {
		// 🔴 THE ROTATION SHAPE IS LEGITIMATE. A row pasted twice, verbatim, must
		// collapse to one record — otherwise guard 12 would see two rows claiming one
		// identity and refuse the ordinary file.
		path := writeTokenFile(t, a+" reader alpha,beta", a+" reader alpha,beta")
		records, err := LoadTokens(path, nil, nil)
		if err != nil {
			t.Fatalf("a verbatim duplicate row is the rotation shape: %v", err)
		}
		if len(records) != 1 {
			t.Fatalf("want one collapsed record, got %d", len(records))
		}
	})

	t.Run("guard 11 collapses two SPELLINGS of one grant", func(t *testing.T) {
		// Scope-list ORDER is a spelling, not a disagreement: both rows grant the same
		// SET. The Python original refused this as "two different authorities", which
		// is one grant written twice.
		path := writeTokenFile(t, a+" reader alpha,beta", a+" reader beta,alpha")
		if _, err := LoadTokens(path, nil, nil); err != nil {
			t.Fatalf("scope-list order is a spelling of one grant: %v", err)
		}
	})

	t.Run("guard 11 refuses a bare row beside a mapped row for one token", func(t *testing.T) {
		// 🔴 THE MIGRATION SHAPE THAT USED TO FAIL OPEN. The Python loop dropped a
		// row whose first field it had already seen — before parsing it — so this file
		// loaded as ONE UNRESTRICTED row and the mapped row simply did not exist.
		path := writeTokenFile(t, a, a+" reader alpha")
		_, err := LoadTokens(path, nil, nil)
		if err == nil {
			t.Fatal("a bare row and a mapped row for ONE token grant different " +
				"authorities and must be refused, not silently collapsed to the " +
				"unrestricted one")
		}
		if !strings.Contains(err.Error(), "UNRESTRICTED") {
			t.Fatalf("the refusal must name what the two rows disagree about: %s", err)
		}
	})

	t.Run("guard 12 refuses two rows claiming one identity", func(t *testing.T) {
		path := writeTokenFile(t, a+" reader alpha", b+" reader beta")
		_, err := LoadTokens(path, nil, nil)
		if err == nil || !strings.Contains(err.Error(), "duplicate identity") {
			t.Fatalf("want a duplicate-identity refusal, got %v", err)
		}
	})

	t.Run("guard 12 EXEMPTS legacy, which is an overlap rotation", func(t *testing.T) {
		path := writeTokenFile(t, a, b)
		records, err := LoadTokens(path, nil, nil)
		if err != nil {
			t.Fatalf("two bare rows are an overlap rotation of the shared token: %v", err)
		}
		if len(records) != 2 {
			t.Fatalf("want both credentials live, got %d", len(records))
		}
	})

	t.Run("a blank line does not shift the reported line numbers", func(t *testing.T) {
		// 🔴 THE INDEX IS A PHYSICAL LINE NUMBER, MEASURED. The Python guards once
		// reported an ordinal over NON-BLANK rows while their own comment claimed
		// physical lines — measurably false on any file with a blank line in it, and
		// "the operator can count to it" is the entire reason an index is carried.
		path := writeTokenFile(t, "", a+" reader alpha", "", b+" reader beta")
		_, err := LoadTokens(path, nil, nil)
		if err == nil || !strings.Contains(err.Error(), "lines 2 and 4") {
			t.Fatalf("want the refusal to name physical lines 2 and 4, got %v", err)
		}
	})

	t.Run("guard 4 counts CREDENTIALS and not rows", func(t *testing.T) {
		// Five copies of one token is ONE credential. Counting rows answered "too many
		// tokens: 5, max 4" for a file holding one.
		rows := make([]string, 5)
		for i := range rows {
			rows[i] = a
		}
		if _, err := LoadTokens(writeTokenFile(t, rows...), nil, nil); err != nil {
			t.Fatalf("five copies of one token is one credential: %v", err)
		}
		// …and five DISTINCT ones is over the cap.
		distinct := []string{aToken('a'), aToken('b'), aToken('c'), aToken('d'), aToken('e')}
		_, err := LoadTokens(writeTokenFile(t, distinct...), nil, nil)
		if err == nil || !strings.Contains(err.Error(), "too many tokens: 5") {
			t.Fatalf("want a too-many-tokens refusal counting 5 credentials, got %v", err)
		}
	})

	t.Run("a legacy row makes the process shout", func(t *testing.T) {
		var warned []string
		path := writeTokenFile(t, a, b+" reader alpha")
		if _, err := LoadTokens(path, nil, func(line string) { warned = append(warned, line) }); err != nil {
			t.Fatal(err)
		}
		if len(warned) != 1 {
			t.Fatalf("want exactly one banner, got %d: %v", len(warned), warned)
		}
		for _, want := range []string{"UNRESTRICTED-SCOPE LEGACY MODE", "1 of 2", TokenID(a)} {
			if !strings.Contains(warned[0], want) {
				t.Fatalf("the banner must carry %q so an operator can act on it: %s", want, warned[0])
			}
		}
		if strings.Contains(warned[0], a) {
			t.Fatalf("the banner carries the TOKEN, not its fingerprint: %s", warned[0])
		}
	})

	t.Run("a file with no legacy row emits no banner", func(t *testing.T) {
		// The positive control's other half: a banner that fires unconditionally is a
		// banner an operator learns to ignore.
		var warned []string
		path := writeTokenFile(t, a+" reader alpha")
		if _, err := LoadTokens(path, nil, func(line string) { warned = append(warned, line) }); err != nil {
			t.Fatal(err)
		}
		if len(warned) != 0 {
			t.Fatalf("want no banner for a fully mapped file, got %v", warned)
		}
	})
}

func TestTheAllowlistAsymmetry(t *testing.T) {
	// 🔴 UNRESTRICTED AND EMPTY ARE OPPOSITES, AND ONLY A LEGACY ROW CAN REACH
	// UNRESTRICTED. Both are "falsy" shapes, which is why the type answers the question
	// with a named field instead of with emptiness.
	legacy := LegacyRecord(aToken('a'))
	if !legacy.VisibleScopes().Unrestricted {
		t.Fatal("a bare row is UNRESTRICTED — that is the migration, not a courtesy")
	}
	if !legacy.VisibleScopes().Allows("anything-at-all") {
		t.Fatal("an unrestricted principal sees every scope")
	}

	mapped := TokenRecord{Token: aToken('b'), Identity: "reader", Scopes: []string{"alpha"}}
	if mapped.VisibleScopes().Unrestricted {
		t.Fatal("a mapped row must never resolve to unrestricted")
	}
	if !mapped.VisibleScopes().Allows("Alpha") {
		t.Fatal("the comparison FOLDS both sides: a scope directory spelled " +
			"`Alpha` must match an allowlist naming `alpha`, or the caller's own " +
			"scope is silently emptied")
	}
	if mapped.VisibleScopes().Allows("beta") {
		t.Fatal("a mapped row sees only what it names")
	}

	// A record with NO scopes and no legacy flag — the shape a refactor produces by
	// forgetting to set a field — must see NOTHING, never everything.
	forgotten := TokenRecord{Token: aToken('c'), Identity: "reader"}
	if forgotten.VisibleScopes().Unrestricted {
		t.Fatal("a record with no allowlist and no legacy mark must be the EMPTY set: " +
			"an empty allowlist is the OPPOSITE of unrestricted, and this is the " +
			"fail-closed direction the whole design rests on")
	}
	if forgotten.VisibleScopes().Allows("alpha") {
		t.Fatal("the empty set allows nothing")
	}
}

func TestAuthorizeReturnsTheMatchedRecord(t *testing.T) {
	a, b := aToken('a'), aToken('b')
	table := []TokenRecord{
		LegacyRecord(a),
		{Token: b, Identity: "reader", Scopes: []string{"alpha"}},
	}
	// 🔴 ONE MATCH, THREE FACTS — the fingerprint, the identity and the allowlist all
	// come off the SAME record. A check that returned only a fingerprint would force
	// the scope lookup to be a second search keyed on something else.
	record, err := Authorize("Bearer "+b, table)
	if err != nil {
		t.Fatalf("a configured credential must authenticate: %v", err)
	}
	if record.Identity != "reader" || record.Fingerprint() != TokenID(b) {
		t.Fatalf("the record must be the one that matched, got %+v", record)
	}
	if record.VisibleScopes().Unrestricted {
		t.Fatal("the mapped record's allowlist must not be the legacy one")
	}

	for _, header := range []string{
		"",
		"Bearer",
		"Basic " + b,
		"Bearer " + aToken('c'),
		b, // the bare credential with no scheme
	} {
		if _, err := Authorize(header, table); err == nil {
			t.Fatalf("a malformed or wrong credential must be refused: %q", header)
		}
	}

	// The scheme is case-insensitive per RFC 9110; the credential is not.
	if _, err := Authorize("bearer "+b, table); err != nil {
		t.Fatalf("the scheme is case-insensitive: %v", err)
	}

	// ⚠ ONE PROPERTY OF `Authorize` IS **NOT** PINNED BY ANYTHING HERE, AND IT IS
	// RECORDED RATHER THAN LEFT AS AN UNEXPLAINED SURVIVOR: the absence of an early
	// exit. The loop runs to the end whether or not it has already matched, so the
	// response time does not encode WHICH configured token was presented — during an
	// overlap rotation "you used the old one" is precisely the fact an attacker wants.
	// MEASURED: adding a `break` after the match SURVIVES every assertion in this file,
	// and it must, because the property is a TIMING one and no functional assertion can
	// observe it. The oracle has the same limitation and resolves it by asserting on the
	// CALL through a mock; doing that here would mean injecting a comparator seam into
	// the one function that must not grow one. So the guard is the code comment and this
	// paragraph, and the honest form is to say so instead of counting it as covered.
	if _, err := Authorize("Bearer "+strings.ToUpper(b), table); err == nil {
		t.Fatal("the credential is NOT case-folded")
	}
}

// 🔴 THE SHAPE THAT SERVED THE WHOLE STORE ON A CREDENTIAL THE ORACLE REFUSES TO LOAD.
// 43 × `0xFF`, mode 0600. The oracle will not start on it ('utf-8' codec can't decode
// byte 0xff in position 0, out of `read_text(encoding="utf-8")`, which its `except
// OSError` does not catch); Go's `raw = string(data)` reinterpreted the bytes, counted 43
// runes, cleared MinTokenChars and parsed them as a BARE LEGACY ROW — unrestricted scope,
// fingerprint and all. With `SUBSYSTEM_STORE_TRUSTED_PROXIES` set, which the conformance
// env and every real deployment set, the server came up and answered
// `GET /api/v1/snapshot` with 200 and the full store to a caller presenting those bytes.
//
// Three assertions, because only the conjunction is the finding: it is REFUSED, the
// refusal NAMES the decode problem rather than a length or a row count, and NO record
// comes back. A guard that refused with "token is empty" would pass a test that only
// checked for an error, and would be diagnosing the wrong file.
func TestATokenFileThatIsNotUTF8IsRefusedRatherThanReinterpreted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tokens")
	// 🔴 EXACTLY MinTokenChars BYTES, WHICH IS THE WHOLE POINT: one byte fewer and the
	// length floor would refuse it for an unrelated reason and this test would pass with
	// the decode guard deleted. The literal 43 is spelled by hand for the reason the
	// file header gives; the relation to MinTokenChars is asserted, not interpolated.
	if MinTokenChars != 43 {
		t.Fatalf("this fixture is built for a 43-character floor, got %d", MinTokenChars)
	}
	if err := os.WriteFile(path, bytes.Repeat([]byte{0xff}, 43), 0o600); err != nil {
		t.Fatal(err)
	}

	records, err := LoadTokens(path, nil, func(string) {})
	if err == nil {
		t.Fatalf("43 bytes of 0xFF LOADED as %d record(s) — the oracle refuses to start on this file", len(records))
	}
	if len(records) != 0 {
		t.Fatalf("a refusal must return no records, got %d", len(records))
	}
	// The sentence has to name the DECODE, or the operator is sent to look at the row
	// count of a file that has no rows.
	for _, want := range []string{
		"token file is not valid UTF-8",
		"'utf-8' codec can't decode byte 0xff in position 0: invalid start byte",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("refusal does not name %q: %s", want, err)
		}
	}
}

// 🔴 THE ENVIRONMENT FALLBACK IS THE OTHER HALF, AND IT MUST **NOT** BE GUARDED. Pinned
// as a decision rather than left to be "fixed": `os.environ` on Linux decodes with
// `surrogateescape`, so the oracle turns a non-UTF-8 env token into a `str` with lone
// surrogates and LOADS it. A strict guard here would refuse a credential the oracle
// accepts — a new divergence created by fixing the file path. This test fails the day
// somebody adds one.
func TestTheEnvironmentTokenIsNotHeldToTheFilesDecodeRule(t *testing.T) {
	raw := strings.Repeat("\xff", MinTokenChars)
	records, err := LoadTokens("", map[string]string{"SUBSYSTEM_STORE_TOKEN": raw}, func(string) {})
	if err != nil {
		t.Fatalf("the env token must load exactly as the oracle loads it: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record from the env fallback, got %d", len(records))
	}
}

// 🔴 A FIFO AT THE TOKEN PATH BLOCKED `os.ReadFile` FOREVER — no diagnostic, no exit, a
// process that never finished starting. The oracle's `is_file()` refuses it in
// milliseconds. `os.Stat` + `!IsDir()` is what accepted it.
//
// 🔴 THE TIMEOUT IS THE ASSERTION, and it is why this test is written with a goroutine
// rather than as a straight call: a test that simply called LoadTokens would HANG at the
// pre-fix tree instead of failing, and a hung test is a red nobody can read. 5s is far
// above the microseconds a stat-and-refuse takes and far below any plausible CI budget.
func TestAFifoTokenFileIsRefusedRatherThanBlockingForever(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tokens")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skipf("cannot create a FIFO here: %v", err)
	}

	type outcome struct {
		records []TokenRecord
		err     error
	}
	done := make(chan outcome, 1)
	go func() {
		records, err := LoadTokens(path, nil, func(string) {})
		done <- outcome{records, err}
	}()

	select {
	case got := <-done:
		if got.err == nil {
			t.Fatalf("a FIFO loaded as %d record(s)", len(got.records))
		}
		if !strings.Contains(got.err.Error(), "is not a file") {
			t.Fatalf("a FIFO must be refused by guard 2's sentence, got: %v", got.err)
		}
	case <-time.After(5 * time.Second):
		// 🔴 NOT `t.Fatal` FROM THE OTHER GOROUTINE, and not a leaked reader either: the
		// blocked `os.ReadFile` is unblocked by opening the write end, so the goroutine
		// can finish and the temp dir can be removed.
		if w, openErr := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0); openErr == nil {
			w.Close()
		}
		t.Fatal("LoadTokens BLOCKED on a FIFO for 5s — the oracle refuses it in milliseconds")
	}
}

// A character device is the OTHER side of the same predicate, and it fails in the
// opposite direction — so a fix that only covered the FIFO would leave this open.
// `/dev/null` at the default token path with an env token set: the oracle's `is_file()`
// is false, so it FALLS BACK and serves; `!IsDir()` is true for a character device, so Go
// took its own fallback away, read zero bytes and exited 78 on "token is empty".
func TestACharacterDeviceIsNotATokenFile(t *testing.T) {
	if IsTokenFile("/dev/null") {
		t.Fatal("/dev/null is not a regular file, so it must not satisfy the token-file test")
	}
	// The positive control, because "returns false" is indistinguishable from a
	// predicate wired to nothing: a real token file must satisfy it.
	if path := writeTokenFile(t, aToken('a')); !IsTokenFile(path) {
		t.Fatalf("%s is a regular file and must satisfy the token-file test", path)
	}
	// And a symlink TO one must too — a Kubernetes secret mount is exactly that shape,
	// so a predicate that refused a link would refuse the deployed configuration.
	real := writeTokenFile(t, aToken('b'))
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if !IsTokenFile(link) {
		t.Fatal("a symlink to a regular file is what a secret mount looks like")
	}
}

// 🔴 THE REFUSAL SENTENCES ARE REPRODUCED FROM THE ORACLE, SO A GO IDENTIFIER MUST NOT
// LEAK INTO ONE. Guard 10's message pointed the reader at `RedactedField` — the Go
// function — where the oracle writes “ `redacted_field` “. Nothing pinned it, which is
// exactly why it drifted: the token loader's messages reach stdout on every refused reload
// and are read by machine, and "the port emits a different string here" is not an
// implementation diagnostic the way CPython's `json` wording is.
//
// The whole normalised sentence is asserted, not a substring: a guard on the WORD
// `redacted_field` is walkable by any rewording that keeps the word, and this is prose.
func TestGuardTenQuotesTheORACLESHelperNameAndNotTheGoOne(t *testing.T) {
	// A scope field that is long enough to be a credential — the shape guard 10 exists
	// for. `aToken` is exactly MinTokenChars, which is above MaxScopeChars.
	path := writeTokenFile(t, aToken('a')+" reader "+aToken('b'))
	_, err := LoadTokens(path, nil, func(string) {})
	if err == nil {
		t.Fatal("a token pasted into field 3 must be refused by guard 10")
	}
	got := err.Error()
	if !strings.Contains(got, "see `redacted_field`") {
		t.Fatalf("guard 10 must name the ORACLE's helper: %s", got)
	}
	// 🔴 AND THE GO NAME MUST BE ABSENT. Without this the test passes if the message
	// somehow names BOTH, which is the shape a careless fix produces.
	if strings.Contains(got, "RedactedField") {
		t.Fatalf("the Go identifier leaked into a reproduced sentence: %s", got)
	}
	// The positive control on the guard itself: it is guard 10 speaking, and the value is
	// not echoed.
	if !strings.Contains(got, "invalid scope in token row on line 1 of 1") {
		t.Fatalf("expected guard 10, got: %s", got)
	}
	if strings.Contains(got, aToken('b')) {
		t.Fatalf("guard 10 echoed the credential it refused: %s", got)
	}
}

func TestAOneCharacterCredentialCannotAuthorize(t *testing.T) {
	// 🔴 THE PYTHON ORIGINAL REFUSES A BARE STRING LOUDLY BECAUSE ITERATING ONE YIELDS
	// CHARACTERS, so a single character of a token would have authorized. Go's type
	// system makes that spelling impossible — a `string` is not a `[]TokenRecord` — so
	// this is an INVARIANT GUARD rather than regression coverage, and it is labelled as
	// one. What it pins is that the property survives the port: no prefix of a
	// configured credential authenticates.
	token := aToken('a')
	table := []TokenRecord{LegacyRecord(token)}
	for n := 1; n < len(token); n++ {
		if _, err := Authorize("Bearer "+token[:n], table); err == nil {
			t.Fatalf("a %d-character prefix of the credential authorized", n)
		}
	}
}
