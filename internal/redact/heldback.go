package redact

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// 🔴 THE ARMING GATE (operator decision O15): capture is not armed on ANY instance until a FRESH
// held-back case set — written by an auditor, never seen by whoever tuned the rules, replaced each
// round — scores at least [ArmingRecallFloorPct]% of its leaks caught AND at most
// [ArmingDamageCeilingPct]% of its clean lines damaged. This file is that check, made
// deterministic: `go build -o redact-heldback ./internal/redact/cmd/redact-heldback` and run it on
// `<cases.jsonl>` (⚠ `go run` reports EVERY non-zero exit as 1 — build it to read 1 vs 2). It prints
//
//	leaks caught=X/Y clean damaged=A/B
//
// and exits 0 inside both bounds, 1 outside either, 2 when it cannot vouch.
//
// ⚠ IT CERTIFIES A CASE FILE, NOT A REDACTOR. A pass over a set the fixer has seen is a regression
// result, not the gate; that the set is fresh and held back is a property of who wrote it, which
// no program can check. The record of which set was scored, and by whom, belongs beside the run.
//
// THE CASE FILE is JSON Lines, one case per line (blank lines and `#` lines ignored):
//
//	{"kind":"leak","label":"…","text":"…","secrets":["…"]}
//	{"kind":"clean","label":"…","text":"…"}
//
// with an optional `"form"`: `text` (default; [Redactor.Text]), `record` (a JSONL transcript
// record; [Redactor.Record]) or `blob` (a persisted tool-result file; [Redactor.Blob]).
//
// A LEAK is caught when no window of [HeldBackWindow] characters of any of its secrets appears in
// the redacted output more often than it appears in the input OUTSIDE the secrets (so a window that
// also occurs in the surrounding text does not count as a leak) — searched in the raw output and in
// every decoded JSON string of it. A secret shorter than the window must not appear at all.
// A CLEAN line is damaged when the output differs from the input by a single byte.
//
// IT VALIDATES ITS INSTRUMENT FIRST, and exits 2 rather than reporting when either control fails:
// a redactor that changes nothing must catch 0 leaks (else the oracle cannot see a leak), and a
// redactor that erases everything must catch every leak AND damage every clean line (else neither
// count can move). It also refuses a malformed file, a leak whose secret is not in its text, and a
// set with fewer than [HeldBackMinCases] leaks or clean lines — a percentage over three cases is
// not a gate.

const (
	ArmingRecallFloorPct   = 90
	ArmingDamageCeilingPct = 15
	HeldBackWindow         = 6
	HeldBackMinCases       = 20
)

// HeldBackCase is one line of a held-back case file.
type HeldBackCase struct {
	Kind    string   `json:"kind"`
	Label   string   `json:"label"`
	Text    string   `json:"text"`
	Secrets []string `json:"secrets,omitempty"`
	Form    string   `json:"form,omitempty"`
}

// HeldBackScore is one scoring pass: counts, and the LABELS (never the values) that went wrong.
type HeldBackScore struct {
	Leaks, Caught, Clean, Damaged int
	Missed, DamagedLabels         []string
}

type textRedactor interface {
	redactor
	Text(data []byte) ([]byte, []Hit)
}

// ParseHeldBack reads a case file and validates every case.
func ParseHeldBack(r io.Reader) ([]HeldBackCase, error) {
	var out []HeldBackCase
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var c HeldBackCase
		dec := json.NewDecoder(strings.NewReader(line))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&c); err != nil {
			return nil, fmt.Errorf("line %d: %v", n, err)
		}
		switch c.Form {
		case "", "text", "record", "blob":
		default:
			return nil, fmt.Errorf("line %d (%s): unknown form %q", n, c.Label, c.Form)
		}
		switch c.Kind {
		case "leak":
			if len(c.Secrets) == 0 {
				return nil, fmt.Errorf("line %d (%s): a leak case names no secret", n, c.Label)
			}
			for _, s := range c.Secrets {
				if s == "" || !strings.Contains(hayOf(c.Text, c.Form), s) {
					return nil, fmt.Errorf("line %d (%s): a secret is not in the case's text", n, c.Label)
				}
			}
		case "clean":
			if len(c.Secrets) != 0 {
				return nil, fmt.Errorf("line %d (%s): a clean case names secrets", n, c.Label)
			}
		default:
			return nil, fmt.Errorf("line %d (%s): kind must be leak or clean, not %q", n, c.Label, c.Kind)
		}
		out = append(out, c)
	}
	return out, sc.Err()
}

// hayOf is the text a leak is searched in: the bytes, then every decoded JSON string and key.
func hayOf(text, form string) string {
	var b strings.Builder
	b.WriteString(text)
	if form == "record" || form == "blob" {
		for _, line := range strings.Split(text, "\n") {
			if v, err := decodeJSON(bytes.TrimSpace([]byte(line))); err == nil {
				b.WriteByte(0)
				collectStrings(v, &b)
			}
		}
		if v, err := decodeJSON(bytes.TrimSpace([]byte(text))); err == nil {
			b.WriteByte(0)
			collectStrings(v, &b)
		}
	}
	return b.String()
}

func runCase(r textRedactor, c HeldBackCase) string {
	var out []byte
	switch c.Form {
	case "record":
		out, _ = r.Record([]byte(c.Text))
	case "blob":
		out, _ = r.Blob("held-back.txt", []byte(c.Text))
	default:
		out, _ = r.Text([]byte(c.Text))
	}
	return string(out)
}

// leaked applies the window oracle described in the file doc.
func leaked(c HeldBackCase, out string) bool {
	in := hayOf(c.Text, c.Form)
	masked := in
	for _, s := range c.Secrets {
		masked = strings.ReplaceAll(masked, s, strings.Repeat("\x00", len(s)))
	}
	hay := hayOf(out, c.Form)
	for _, s := range c.Secrets {
		if len(s) < HeldBackWindow {
			if strings.Count(hay, s) > strings.Count(masked, s) {
				return true
			}
			continue
		}
		for i := 0; i+HeldBackWindow <= len(s); i++ {
			w := s[i : i+HeldBackWindow]
			if strings.Count(hay, w) > strings.Count(masked, w) {
				return true
			}
		}
	}
	return false
}

// ScoreHeldBack redacts every case and counts.
func ScoreHeldBack(cases []HeldBackCase, r textRedactor) HeldBackScore {
	var s HeldBackScore
	for _, c := range cases {
		out := runCase(r, c)
		switch c.Kind {
		case "leak":
			s.Leaks++
			if leaked(c, out) {
				s.Missed = append(s.Missed, c.Label)
			} else {
				s.Caught++
			}
		case "clean":
			s.Clean++
			if out != c.Text {
				s.Damaged++
				s.DamagedLabels = append(s.DamagedLabels, c.Label)
			}
		}
	}
	return s
}

// eraser is the POSITIVE control: it replaces every input with a fixed marker.
type eraser struct{}

func (eraser) Record([]byte) ([]byte, []Hit) { return []byte(`"[erased]"`), []Hit{{Rule: "eraser"}} }
func (eraser) Blob(string, []byte) ([]byte, []Hit) {
	return []byte("[erased]"), []Hit{{Rule: "eraser"}}
}
func (eraser) Text([]byte) ([]byte, []Hit) { return []byte("[erased]"), []Hit{{Rule: "eraser"}} }

func (identity) Text(data []byte) ([]byte, []Hit) { return data, nil }

// Exit codes of [HeldBackGate].
const (
	HeldBackPass    = 0
	HeldBackFail    = 1
	HeldBackNoVouch = 2
)

type heldBackParts struct {
	identity, eraser, real textRedactor
}

// HeldBackGate scores one case file against the real redactor (key derived for the run; the tag
// key never changes what is caught) and prints the result. See the file doc.
func HeldBackGate(w io.Writer, src io.Reader) int {
	real, err := New(SelfTestKey(0), nil)
	if err != nil {
		fmt.Fprintf(w, "COULD NOT VOUCH: %v\n", err)
		return HeldBackNoVouch
	}
	return heldBackGate(w, src, heldBackParts{identity: identity{}, eraser: eraser{}, real: real})
}

func heldBackGate(w io.Writer, src io.Reader, p heldBackParts) int {
	cases, err := ParseHeldBack(src)
	if err != nil {
		fmt.Fprintf(w, "COULD NOT VOUCH: the case file is malformed: %v\n", err)
		return HeldBackNoVouch
	}
	neg := ScoreHeldBack(cases, p.identity)
	if neg.Leaks < HeldBackMinCases || neg.Clean < HeldBackMinCases {
		fmt.Fprintf(w, "COULD NOT VOUCH: %d leak and %d clean cases; the gate needs at least %d of each\n",
			neg.Leaks, neg.Clean, HeldBackMinCases)
		return HeldBackNoVouch
	}
	fmt.Fprintf(w, "control identity: leaks caught=%d/%d clean damaged=%d/%d (must be 0 and 0)\n",
		neg.Caught, neg.Leaks, neg.Damaged, neg.Clean)
	if neg.Caught != 0 || neg.Damaged != 0 {
		fmt.Fprintln(w, "COULD NOT VOUCH: a redactor that changes nothing scored, so the oracle cannot see a leak")
		return HeldBackNoVouch
	}
	pos := ScoreHeldBack(cases, p.eraser)
	fmt.Fprintf(w, "control eraser: leaks caught=%d/%d clean damaged=%d/%d (must be all and all)\n",
		pos.Caught, pos.Leaks, pos.Damaged, pos.Clean)
	if pos.Caught != pos.Leaks || pos.Damaged != pos.Clean {
		fmt.Fprintln(w, "COULD NOT VOUCH: a redactor that erases everything did not move both counts to the top")
		return HeldBackNoVouch
	}
	s := ScoreHeldBack(cases, p.real)
	for _, l := range s.Missed {
		fmt.Fprintf(w, "MISSED %s\n", l)
	}
	for _, l := range s.DamagedLabels {
		fmt.Fprintf(w, "DAMAGED %s\n", l)
	}
	fmt.Fprintf(w, "leaks caught=%d/%d clean damaged=%d/%d\n", s.Caught, s.Leaks, s.Damaged, s.Clean)
	recallOK := 100*s.Caught >= ArmingRecallFloorPct*s.Leaks
	damageOK := 100*s.Damaged <= ArmingDamageCeilingPct*s.Clean
	if recallOK && damageOK {
		fmt.Fprintf(w, "VERDICT: PASS (floor %d%% caught, ceiling %d%% damaged)\n", ArmingRecallFloorPct, ArmingDamageCeilingPct)
		return HeldBackPass
	}
	fmt.Fprintf(w, "VERDICT: FAIL (floor %d%% caught: %v; ceiling %d%% damaged: %v)\n",
		ArmingRecallFloorPct, recallOK, ArmingDamageCeilingPct, damageOK)
	return HeldBackFail
}
