package redact

import (
	"crypto/sha256"
	"fmt"
	"io"
	"regexp"
)

// identity is the NEGATIVE control's redactor: it changes nothing.
type identity struct{}

func (identity) Record(raw []byte) ([]byte, []Hit)          { return raw, nil }
func (identity) Blob(_ string, data []byte) ([]byte, []Hit) { return data, nil }

// corrupter is the SCORER's second negative control: it redacts NOTHING, corrupts the encoding of
// what it returns — bytes appended after the document, or (`double`) a record encoded a second
// time as one JSON string — and claims a hit for every rule a plant names. A scorer that looks for
// a plant only in output it can decode credits this redactor with every plant the encoder escaped
// and every UTF-16 blob; it must score 0.
type corrupter struct {
	rules  []string
	double bool
}

func (c corrupter) hits() []Hit {
	out := make([]Hit, 0, len(c.rules))
	for _, r := range c.rules {
		out = append(out, Hit{Rule: r})
	}
	return out
}

func (c corrupter) Record(raw []byte) ([]byte, []Hit) {
	if c.double {
		return []byte(`"` + jsonEscape(string(raw), true) + `"`), c.hits()
	}
	return append(append([]byte(nil), raw...), " trailing-bytes"...), c.hits()
}

func (c corrupter) Blob(_ string, data []byte) ([]byte, []Hit) {
	// An ODD number of bytes, so a UTF-16 blob stops decoding.
	return append(append([]byte(nil), data...), " trailing-bytes"...), c.hits()
}

// newCorrupters claims every rule name a plant is planted for, in both corruptions.
func newCorrupters(c Corpus) []redactor {
	seen := map[string]bool{}
	var rules []string
	for _, p := range c.Plants {
		if !seen[p.Rule] {
			seen[p.Rule] = true
			rules = append(rules, p.Rule)
		}
	}
	return []redactor{corrupter{rules: rules}, corrupter{rules: rules, double: true}}
}

// greedyRule is the damage counter's control: a rule that eats every long alphanumeric run, which
// a real table must never be. If the corpus cannot see THAT damage, `clean-damaged=0` means nothing.
var greedyRule = Rule{Name: "greedy-control", Re: regexp.MustCompile(`[A-Za-z0-9]{32,}`)}

// SelfTestKey derives the self-test's tag key from the seed. It is NOT a secret and NOT a host
// key — the self-test never touches the host key file, so it can run anywhere, read-only.
func SelfTestKey(seed uint64) []byte {
	sum := sha256.Sum256([]byte(fmt.Sprintf("cairn-capture self-test %d", seed)))
	return sum[:]
}

// Exit codes of [SelfTest]: 0 the measurement holds; 1 a plant was missed or a clean value
// damaged; 2 the instrument could not vouch (a control misbehaved, or P is not the declared P).
const (
	SelfTestOK      = 0
	SelfTestFailed  = 1
	SelfTestNoVouch = 2
)

// selfTestParts is everything one self-test run reads, injectable so each exit-2 branch can be
// driven by a test rather than asserted in prose.
type selfTestParts struct {
	corpus   Corpus
	declared int
	identity redactor
	corrupt  []redactor
	greedy   redactor
	real     redactor
}

// SelfTest runs the corpus measurement for one seed and prints, as its last line,
//
//	SUMMARY redaction: planted=P caught=C clean-damaged=D
//
// 🔴 IT VALIDATES ITS OWN INSTRUMENT BEFORE READING THE VERDICT. Three controls run first and each
// must misbehave in the expected direction, or the run exits 2 ("could not vouch") instead of
// reporting: the IDENTITY redactor must catch 0 plants (the scorer can see a leak), a redactor
// that only CORRUPTS THE ENCODING must catch 0 too (a plant cannot hide in an escape the scorer
// does not decode — [corrupter]), and a GREEDY rule must damage at least one clean value (the
// damage counter can move). And P must equal [DeclaredPlants], asserted rather than read off the
// run.
//
// Missed plants and damaged values are printed by LABEL and rule — never by value.
func SelfTest(w io.Writer, seed uint64) int {
	key := SelfTestKey(seed)
	greedy, err := newWithRules(append(DefaultRules(), greedyRule), key, nil)
	if err != nil {
		fmt.Fprintf(w, "COULD NOT VOUCH: %v\n", err)
		return SelfTestNoVouch
	}
	real, err := New(key, nil)
	if err != nil {
		fmt.Fprintf(w, "COULD NOT VOUCH: %v\n", err)
		return SelfTestNoVouch
	}
	corpus := NewCorpus(seed)
	return selfTest(w, selfTestParts{corpus: corpus, declared: DeclaredPlants, identity: identity{},
		corrupt: newCorrupters(corpus), greedy: greedy, real: real})
}

func selfTest(w io.Writer, p selfTestParts) int {
	c := p.corpus
	if len(c.Plants) != p.declared {
		fmt.Fprintf(w, "COULD NOT VOUCH: the corpus planted %d secrets but declares %d\n", len(c.Plants), p.declared)
		return SelfTestNoVouch
	}
	neg := c.Score(p.identity)
	fmt.Fprintf(w, "control identity-redactor: planted=%d caught=%d (must be 0)\n", neg.Planted, neg.Caught)
	if neg.Caught != 0 {
		fmt.Fprintln(w, "COULD NOT VOUCH: a redactor that changes nothing scored catches, so the scorer cannot see a leak")
		return SelfTestNoVouch
	}
	corCaught := 0
	for _, cr := range p.corrupt {
		corCaught += c.Score(cr).Caught
	}
	fmt.Fprintf(w, "control corrupt-encoding-redactors: planted=%d caught=%d (must be 0)\n", neg.Planted, corCaught)
	if corCaught != 0 || len(p.corrupt) == 0 {
		fmt.Fprintln(w, "COULD NOT VOUCH: a redactor that only corrupts the encoding scored catches, so a plant can hide in an escape")
		return SelfTestNoVouch
	}
	pos := c.Score(p.greedy)
	fmt.Fprintf(w, "control greedy-rule: clean-damaged=%d (must be > 0)\n", pos.CleanDamaged)
	if pos.CleanDamaged == 0 {
		fmt.Fprintln(w, "COULD NOT VOUCH: a rule that eats every long run damaged nothing, so clean-damaged=0 would mean nothing")
		return SelfTestNoVouch
	}
	s := c.Score(p.real)
	for _, m := range s.Missed {
		fmt.Fprintf(w, "MISSED %s (expected rule %s)\n", m.Label, m.Rule)
	}
	for _, d := range s.Damaged {
		fmt.Fprintf(w, "DAMAGED %s\n", d.Label)
	}
	fmt.Fprintf(w, "SUMMARY redaction: planted=%d caught=%d clean-damaged=%d\n", s.Planted, s.Caught, s.CleanDamaged)
	if s.Caught != s.Planted || s.CleanDamaged != 0 {
		return SelfTestFailed
	}
	return SelfTestOK
}
