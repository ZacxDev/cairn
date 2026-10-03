package store

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The citation-id seam, replayed against the PYTHON implementation's answers.
//
// WHAT IT MEASURES. `JournalBullet.CitationID` exists twice — here and in
// `lib/subsystem_resolver.py` — because `packages.cairn` installs the Python client and
// its `lib/` under `libexec` and nothing else, so `lib/` CANNOT import `internal/`.
// That is packaging, not a choice anyone may undo here, and it is the shape `AGENTS.md`
// already blesses for `server/Dockerfile` against `flake.nix`'s `serverEnv`.
//
// Two independent implementations of a hash must agree FOREVER or the id stops being a
// shared name for a bullet and silently becomes two. Nothing about that agreement is
// checked by a compiler, and it cannot be checked by reading either source: the only
// instrument is running both over the same bodies and comparing strings.
//
// 🔴 THE FIXTURE IS THE ORACLE'S OUTPUT, NOT A SNAPSHOT OF THIS PACKAGE. It records what
// CPython answers, so nothing here can agree with itself. `tests/citation_ids.py` writes
// it and `tests/test_citation_ids.py` REGENERATES it from the live Python
// implementation on every pytest run, so the fixture cannot drift away from the code it
// claims to record — the same two-sided arrangement `markersweep_test.go` uses.
//
// 🔴 AND IT COMPARES THE DECODE, NOT ONLY THE HASH. Each case carries the entry BYTES as
// hex beside their `errors="replace"` decode, and this side decodes the hex with
// `DecodeReplace` before parsing. The first revision replayed the DECODED string, which
// made it structurally unable to see a decode difference — and a decode difference is
// what shipped: `pytext.DecodeUTF8Replace` emitted one U+FFFD per invalid BYTE where
// CPython emits one per maximal SUBPART, so one entry file produced two citation ids.
//
// ⚠ WHAT IT CANNOT SEE. The bodies are hand-written, so this says nothing about the LIVE
// corpus, and nothing about RENDERING. ⚠ THE SECOND HALF OF THAT SENTENCE USED TO READ "no
// surface prints an id yet", AND THAT IS NO LONGER TRUE: both text renderers now append
// ` [cb:<id>]` to every surfaced section line that opens a bullet, so a rendered byte
// depends on this function. What measures that is still not here —
// `internal/report/testdata/reader_fixtures.json` compares the two renderers' bytes (138
// of its lines carry a token), `internal/report/citationtoken_test.go` pins the POSITION
// and the verbatim body, and `tests/parity/harness.py` compares the two real clients. Said
// here rather than left to be discovered, because a reader who took this file for full
// coverage would stop looking.

type citationCase struct {
	Name string `json:"name"`
	// BodyHex is the entry bytes; Body is their `errors="replace"` decode. 🔴 THE
	// HEX IS THE INPUT AND THE STRING IS AN EXPECTATION — replaying `Body` alone is
	// what made the first revision of this fixture structurally unable to see a
	// DECODE difference, which is the difference that shipped.
	BodyHex string `json:"body_hex"`
	Body    string `json:"body"`
	Bullets []struct {
		StartLine  int      `json:"start_line"`
		Lines      []string `json:"lines"`
		CitationID string   `json:"citation_id"`
	} `json:"bullets"`
}

type citationFixture struct {
	Cases []citationCase `json:"cases"`
}

func loadCitationFixture(t *testing.T) citationFixture {
	t.Helper()
	// `testdata/`, beside the package, because the `nix build` tier compiles from an
	// allowlisted source copy that does not carry `tests/`. The path is named
	// file-by-file in `flake.nix`'s `onlyGo` filter — a directory row would NOT carry
	// it, which is the measured two-tier split `marker_oracle_sweep.json` paid for.
	path := filepath.Join("testdata", "citation_ids.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the citation fixture is unreadable, so this file measures NOTHING: %v", err)
	}
	var fx citationFixture
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatalf("the citation fixture does not parse: %v", err)
	}
	// 🔴 AN EXACT COUNT BEFORE ANY COMPARISON, AND EXACT IS THE FIX RATHER THAN THE
	// STYLE. An empty `cases` list would make every loop below iterate zero times and
	// report success — the vacuous green this whole arrangement exists to avoid. The
	// previous spelling was `< 9` and its comment claimed the number "moves when a
	// case is added, which is a deliberate prompt to read this file": a LOWER BOUND
	// pins shrink only, so a tenth case prompted nothing and the sentence was wider
	// than the code. Equality is what makes adding a case land here.
	const handWrittenCases = 14
	if len(fx.Cases) != handWrittenCases {
		t.Fatalf("the fixture carries %d case(s) and this guard expects exactly %d. "+
			"If you ADDED a case, bump the constant — that prompt is the point. If you "+
			"did not, something truncated the fixture and a green run here would mean "+
			"nothing.", len(fx.Cases), handWrittenCases)
	}
	return fx
}

func TestTheCitationIDAgreesWithThePythonOracle(t *testing.T) {
	fx := loadCitationFixture(t)
	compared := 0
	decoded := 0
	for _, c := range fx.Cases {
		// 🔴 THE DECODE IS PART OF WHAT IS BEING COMPARED. The entry bytes go through
		// THIS side's `DecodeReplace`; if that disagrees with CPython's `replace`
		// handler the `lines` below are different lines and the id is a different id,
		// which is exactly the defect this pair of fields was added for. Asserted
		// before the ids so a red run names the decode rather than the hash.
		raw, err := hex.DecodeString(c.BodyHex)
		if err != nil {
			t.Fatalf("%s: body_hex does not decode: %v", c.Name, err)
		}
		body := DecodeReplace(raw)
		if body != c.Body {
			t.Errorf("%s: this side's DecodeReplace of the entry bytes gives %q, the "+
				"oracle's `errors=\"replace\"` decode gives %q — every id below would be "+
				"taken over different text", c.Name, body, c.Body)
			continue
		}
		decoded++
		got := ParseJournalBullets(body)
		if len(got) != len(c.Bullets) {
			t.Errorf("%s: this side parsed %d bullet(s), the oracle %d — the ids below "+
				"would be compared pairwise against different bullets",
				c.Name, len(got), len(c.Bullets))
			continue
		}
		for i, want := range c.Bullets {
			if id := got[i].CitationID(); id != want.CitationID {
				t.Errorf("%s bullet %d: go id %q, python id %q (lines %q)",
					c.Name, i, id, want.CitationID, got[i].Lines)
			}
			if got[i].StartLine != want.StartLine {
				t.Errorf("%s bullet %d: go StartLine %d, python %d",
					c.Name, i, got[i].StartLine, want.StartLine)
			}
			if len(got[i].Lines) != len(want.Lines) {
				t.Errorf("%s bullet %d: go grouped %d line(s), python %d",
					c.Name, i, len(got[i].Lines), len(want.Lines))
				continue
			}
			for j := range want.Lines {
				if got[i].Lines[j] != want.Lines[j] {
					t.Errorf("%s bullet %d line %d: go %q, python %q",
						c.Name, i, j, got[i].Lines[j], want.Lines[j])
				}
			}
			compared++
		}
	}
	// The POSITIVE CONTROL for the loop itself: a fixture whose cases all carried zero
	// bullets would satisfy every assertion above without comparing one id.
	if compared == 0 {
		t.Fatal("not a single bullet was compared, so this test asserted nothing")
	}
	// And the same control for the DECODE half, which has its own way of asserting
	// nothing: every case `continue`ing on a decode mismatch leaves the id loop
	// unreached, and `compared` would be 0 — but a fixture of only EMPTY bodies would
	// decode cleanly and compare no ids, so both counters are needed.
	if decoded != len(fx.Cases) {
		t.Errorf("only %d of %d case(s) got as far as an id comparison",
			decoded, len(fx.Cases))
	}
	t.Logf("compared %d bullet(s) across %d case(s); %d body decode(s) matched",
		compared, len(fx.Cases), decoded)
}

func TestTheCitationIDIsEightLowercaseHexAndDependsOnEveryLine(t *testing.T) {
	// 🔴 THE SHAPE, PINNED SEPARATELY FROM THE AGREEMENT. The test above would stay
	// green if BOTH sides returned a 64-character digest, or uppercase, or the empty
	// string — agreement is not correctness.
	b := JournalBullet{Lines: []string{"- a lesson", "  that wraps"}}
	id := b.CitationID()
	if len(id) != 8 {
		t.Fatalf("CitationID returned %d characters (%q); the contract is 8", len(id), id)
	}
	for _, r := range id {
		if !(r >= '0' && r <= '9') && !(r >= 'a' && r <= 'f') {
			t.Fatalf("CitationID returned a non-lowercase-hex character in %q", id)
		}
	}

	// 🔴 EVERY LINE PARTICIPATES. A hash over `Lines[0]` alone is the plausible
	// simplification, and it would make two bullets sharing an opening line collide —
	// which the oracle fixture's `two-bullets-with-IDENTICAL-opening-lines` case is
	// built to catch, but only while the hash reads past line 0. This asserts the
	// property directly rather than relying on that case surviving an edit.
	tailChanged := JournalBullet{Lines: []string{"- a lesson", "  that wraps DIFFERENTLY"}}
	if tailChanged.CitationID() == id {
		t.Error("two bullets differing only in a CONTINUATION line share an id, so the " +
			"hash is reading the opening line alone")
	}

	// And the empty case is defined rather than panicking: no lines is a valid struct.
	if empty := (JournalBullet{}).CitationID(); len(empty) != 8 {
		t.Errorf("an empty bullet's id is %q; it should still be 8 hex characters", empty)
	}
}
