package store

import (
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
// ⚠ WHAT IT CANNOT SEE. The bodies are hand-written, so this says nothing about the LIVE
// corpus, and nothing about RENDERING — no surface prints an id yet. The differential
// reader fixture is what compares rendered bytes, and `tests/parity/harness.py` is what
// compares the two real clients. Said here rather than left to be discovered, because a
// reader who took this file for full coverage would stop looking.

type citationCase struct {
	Name    string `json:"name"`
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
	// 🔴 A FLOOR BEFORE ANY COMPARISON. An empty `cases` list would make every loop
	// below iterate zero times and report success — the vacuous green this whole
	// arrangement exists to avoid. The number is the count of hand-written cases; it
	// moves when a case is added, which is a deliberate prompt to read this file.
	if len(fx.Cases) < 9 {
		t.Fatalf("the fixture carries %d case(s); it is supposed to carry at least 9, "+
			"so something truncated it and a green run here would mean nothing",
			len(fx.Cases))
	}
	return fx
}

func TestTheCitationIDAgreesWithThePythonOracle(t *testing.T) {
	fx := loadCitationFixture(t)
	compared := 0
	for _, c := range fx.Cases {
		got := ParseJournalBullets(c.Body)
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
	t.Logf("compared %d bullet(s) across %d case(s)", compared, len(fx.Cases))
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
