package pytext

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The `DecodeUTF8Replace` seam, replayed against CPython's own answers.
//
// WHAT IT MEASURES. `DecodeUTF8Replace` claims to be
// `bytes.decode("utf-8", errors="replace")`, and three callers depend on that
// claim BYTE-FOR-BYTE: `store.DecodeReplace` (every entry file the reader loads),
// `api.DecodeQuery` (a percent-escaped byte run) and `client.FocusPathsFromText`.
// Nothing about the claim is checked by a compiler and it cannot be checked by
// reading either source — CPython's rule lives in the branch structure of its
// decoder, not in its documentation.
//
// 🔴 AND IT HAD NO TEST AT ALL UNTIL THIS FILE, WHICH IS HOW THE FUNCTION SHIPPED
// WRONG. It emitted one U+FFFD per undecodable BYTE; CPython emits one per maximal
// SUBPART of a would-be-valid sequence. Measured over the 82,176-input sweep below:
// **4,284** inputs differed under the per-byte rule and **0** under the rule that
// replaced it. Every one of the 4,284 carries a multi-byte truncation — so a lone
// `0x80` agreed, and every example anybody wrote by hand was a lone `0x80`.
//
// ⚠ WHAT IT CANNOT SEE. The fixture is a digest plus a named table, so it speaks
// for the enumeration in `tests/pytext_decode_replace.py` and nothing else: no
// CALLER is exercised here, and no rendered byte. `internal/store`'s own tests and
// `tests/parity/harness.py` are what read this function through a caller.

type decodeReplaceCase struct {
	Name             string `json:"name"`
	About            string `json:"about"`
	InputHex         string `json:"input_hex"`
	OutputHex        string `json:"output_hex"`
	ReplacementCount int    `json:"replacement_count"`
}

type decodeReplaceFixture struct {
	Cases []decodeReplaceCase `json:"cases"`
	Sweep struct {
		Count  int    `json:"count"`
		Digest string `json:"digest"`
		Groups []struct {
			Name  string `json:"name"`
			Count int    `json:"count"`
		} `json:"groups"`
		SampleStride int `json:"sample_stride"`
		Samples      []struct {
			Index     int    `json:"index"`
			Group     string `json:"group"`
			InputHex  string `json:"input_hex"`
			OutputHex string `json:"output_hex"`
		} `json:"samples"`
	} `json:"sweep"`
}

func loadDecodeReplaceFixture(t *testing.T) decodeReplaceFixture {
	t.Helper()
	// `testdata/`, beside the package, and named FILE BY FILE in `flake.nix`'s
	// `onlyGo` filter: the `hasPrefix "internal/"` row there sits inside the
	// `type == "directory"` clause, so it carries the directory and not the JSON in
	// it. A fixture that relied on the directory row is green on a dev host and RED
	// in the nix sandbox.
	path := filepath.Join("testdata", "decode_replace.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the decode fixture is unreadable, so this file measures NOTHING: %v", err)
	}
	var fx decodeReplaceFixture
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatalf("the decode fixture does not parse: %v", err)
	}
	// 🔴 FLOORS BEFORE ANY COMPARISON. An empty `cases` list makes the loop below
	// iterate zero times and report success, and a `sweep.count` of 0 makes the
	// digest a stable hash over nothing — the two vacuous greens this arrangement
	// exists to refuse.
	if len(fx.Cases) < 20 {
		t.Fatalf("the fixture carries %d named case(s), expected at least 20 — "+
			"something truncated it and a green run here would mean nothing",
			len(fx.Cases))
	}
	if fx.Sweep.Count < 80000 {
		t.Fatalf("the sweep claims %d input(s), expected at least 80000 — the "+
			"enumeration shrank, so the digest below is a hash over a different corpus",
			fx.Sweep.Count)
	}
	if len(fx.Sweep.Samples) < 100 {
		t.Fatalf("the sweep carries %d sample(s), expected at least 100 — without them "+
			"a red digest cannot name one input", len(fx.Sweep.Samples))
	}
	return fx
}

func TestDecodeUTF8ReplaceMatchesTheNamedCPythonCases(t *testing.T) {
	fx := loadDecodeReplaceFixture(t)
	for _, c := range fx.Cases {
		in, err := hex.DecodeString(c.InputHex)
		if err != nil {
			t.Fatalf("%s: input_hex does not decode: %v", c.Name, err)
		}
		wantBytes, err := hex.DecodeString(c.OutputHex)
		if err != nil {
			t.Fatalf("%s: output_hex does not decode: %v", c.Name, err)
		}
		got := DecodeUTF8Replace(in)
		if got != string(wantBytes) {
			t.Errorf("%s (%s):\n  input    %s\n  go       %s (%d U+FFFD)\n"+
				"  cpython  %s (%d U+FFFD)",
				c.Name, c.About, c.InputHex,
				hex.EncodeToString([]byte(got)), strings.Count(got, "�"),
				c.OutputHex, c.ReplacementCount)
		}
	}
	t.Logf("compared %d named case(s)", len(fx.Cases))
}

// decodeReplaceSweepInputs is the TRANSCRIPTION of `GROUPS` in
// `tests/pytext_decode_replace.py`, in that order. It is spelled twice on purpose:
// the fixture carries a digest, and a digest cannot be compared without rebuilding
// the corpus it was taken over. ⚠ A disagreement about the ENUMERATION therefore
// reads as a red decoder, which is why the per-group counts are checked FIRST and
// reported separately — a count mismatch names the group that drifted.
func decodeReplaceSweepInputs() []struct {
	Name   string
	Inputs [][]byte
} {
	var groups []struct {
		Name   string
		Inputs [][]byte
	}
	add := func(name string, inputs [][]byte) {
		groups = append(groups, struct {
			Name   string
			Inputs [][]byte
		}{name, inputs})
	}

	var singles [][]byte
	for b := 0; b < 256; b++ {
		singles = append(singles, []byte{byte(b)})
	}
	add("single-bytes", singles)

	var twoByte [][]byte
	for a := 0x80; a < 0x100; a++ {
		for b := 0; b < 0x100; b++ {
			twoByte = append(twoByte, []byte{byte(a), byte(b)})
		}
	}
	add("two-byte-leads", twoByte)

	var embedded [][]byte
	for a := 0x80; a < 0x100; a++ {
		for b := 0; b < 0x100; b++ {
			embedded = append(embedded, []byte{'x', byte(a), byte(b), 'z'})
		}
	}
	add("two-byte-leads-embedded-in-ascii", embedded)

	var threeByte [][]byte
	for _, a := range []byte{0xE0, 0xE1, 0xED, 0xEF} {
		for b := 0; b < 0x100; b++ {
			for _, c := range []byte{0x00, 0x41, 0x80, 0xA0, 0xBF, 0xC0, 0xFF} {
				threeByte = append(threeByte, []byte{a, byte(b), c})
			}
		}
	}
	add("three-byte-leads", threeByte)

	var fourByte [][]byte
	for _, a := range []byte{0xF0, 0xF1, 0xF4, 0xF5} {
		for b := 0; b < 0x100; b++ {
			for _, c := range []byte{0x41, 0x80, 0xBF} {
				for _, d := range []byte{0x41, 0x80, 0xBF} {
					fourByte = append(fourByte, []byte{a, byte(b), c, d})
				}
			}
		}
	}
	add("four-byte-leads", fourByte)

	return groups
}

func TestDecodeUTF8ReplaceMatchesTheCPythonSweepDigest(t *testing.T) {
	fx := loadDecodeReplaceFixture(t)
	groups := decodeReplaceSweepInputs()

	// The ENUMERATION first, group by group. A digest mismatch with the counts
	// intact is a decoder difference; a count mismatch is a transcription
	// difference, and conflating the two sends the next reader to the wrong file.
	if len(groups) != len(fx.Sweep.Groups) {
		t.Fatalf("this side enumerates %d group(s), the fixture %d — the transcription "+
			"drifted, so the digest below would be about a different corpus",
			len(groups), len(fx.Sweep.Groups))
	}
	total := 0
	for i, g := range groups {
		want := fx.Sweep.Groups[i]
		if g.Name != want.Name {
			t.Fatalf("group %d is %q here and %q in the fixture", i, g.Name, want.Name)
		}
		if len(g.Inputs) != want.Count {
			t.Fatalf("group %q: this side enumerates %d input(s), the fixture %d",
				g.Name, len(g.Inputs), want.Count)
		}
		total += len(g.Inputs)
	}
	if total != fx.Sweep.Count {
		t.Fatalf("this side enumerates %d input(s) in total, the fixture %d",
			total, fx.Sweep.Count)
	}

	// ⚠ THE SAMPLES ARE WHAT MAKES A RED DIGEST ACTIONABLE, and they are compared
	// BEFORE it: each one carries CPython's exact answer for one input of this same
	// walk, so a failure here quotes an expected VALUE rather than a hash. They are
	// strided rather than hand-picked — a hand-picked sample only ever sees the
	// shapes somebody already suspected.
	index := 0
	sampleAt := make(map[int]int, len(fx.Sweep.Samples))
	for i, s := range fx.Sweep.Samples {
		sampleAt[s.Index] = i
	}
	sampleMismatches := 0
	var firstFew []string

	digest := sha256.New()
	for _, g := range groups {
		for _, in := range g.Inputs {
			got := DecodeUTF8Replace(in)
			gotHex := hex.EncodeToString([]byte(got))
			fmt.Fprintf(digest, "%s:%s\n", hex.EncodeToString(in), gotHex)
			if si, ok := sampleAt[index]; ok {
				s := fx.Sweep.Samples[si]
				if s.InputHex != hex.EncodeToString(in) {
					t.Fatalf("sample %d (group %q): this side walks %s, the fixture %s "+
						"— the enumeration transcription drifted in ORDER, which the "+
						"counts above cannot see",
						index, s.Group, hex.EncodeToString(in), s.InputHex)
				}
				if gotHex != s.OutputHex {
					sampleMismatches++
					if len(firstFew) < 8 {
						firstFew = append(firstFew, fmt.Sprintf(
							"%s: go %s (%d U+FFFD), cpython %s",
							s.InputHex, gotHex, strings.Count(got, "�"), s.OutputHex))
					}
				}
			}
			index++
		}
	}
	if sampleMismatches > 0 {
		t.Errorf("%d of %d sampled input(s) decoded differently from CPython:\n  %s",
			sampleMismatches, len(fx.Sweep.Samples), strings.Join(firstFew, "\n  "))
	}
	if got := hex.EncodeToString(digest.Sum(nil)); got != fx.Sweep.Digest {
		t.Errorf("the sweep digest over %d input(s) is\n  go      %s\n  cpython %s\n"+
			"Regenerate with `python3 tests/pytext_decode_replace.py` and diff; a moved "+
			"digest with the counts intact means a DECODED byte moved. The sampled rows "+
			"above name specific inputs; %d of %d sample(s) differ.",
			total, got, fx.Sweep.Digest, sampleMismatches, len(fx.Sweep.Samples))
	}
	t.Logf("swept %d input(s) across %d group(s); %d sampled rows compared by value",
		total, len(groups), len(fx.Sweep.Samples))
}
