package scopeuse

import (
	"encoding/json"
	"io"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
)

// alwaysDecode is [Deriver.Content] as it was at #261 round 2's head: EVERY string that opens like
// a JSON document and is one is decoded through a `json.Decoder` token walk, with no gate. Kept as
// the ORACLE the gated, in-place version is held to.
func alwaysDecode(s string, depth int, emit func(string)) {
	if depth < 3 {
		t := strings.TrimSpace(s)
		if len(t) >= 2 && (t[0] == '{' || t[0] == '[') && json.Valid([]byte(t)) {
			dec := json.NewDecoder(strings.NewReader(t))
			for {
				tok, err := dec.Token()
				if err != nil {
					if err != io.EOF {
						panic(err)
					}
					return
				}
				if v, ok := tok.(string); ok {
					alwaysDecode(v, depth+1, emit)
				}
			}
		}
	}
	for _, v := range HeaderScopes(s) {
		emit(v)
	}
	for _, v := range MemoScopes(s) {
		emit(v)
	}
}

// TestTheDecodeGateIsExact (#261 round 3): Content skips a string that holds neither marker raw
// and no `\u`, and walks a document's string tokens in place instead of through a decoder. Both are
// claimed EXACT, so over generated documents — markers written raw, `\u`-escaped and nested a level
// down, with `\n`, `\"`, `\\` and `\/` escapes, duplicate keys and invalid UTF-8 — the gated answer
// must equal the round-2 one as a set. The control: some generated documents are SKIPPED by the
// gate and some still name a scope, so the comparison reaches both sides of it.
func TestTheDecodeGateIsExact(t *testing.T) {
	strs := []string{
		`subsystem-recall: scope=alpha-notes\n`, `subsystem-recall: scope=beta-notes\n`,
		`subsystem-recall: scope=gamma-notes`, `<<<cairn-memo\n| [m-1] from a · delta-notes · t · expires t\n`,
		`<<<cairn-memo\n`, `x\ny`, `\"q\"`, `a\\b`, `\/`, " scope=epsilon-notes", "plain", "\xff\xfe",
		`subsystem-recall:`, `\\u0073ubsystem-recall: scope=zeta-notes`,
	}
	r := rand.New(rand.NewPCG(3, 4))
	str := func() string {
		var b strings.Builder
		for k := r.IntN(3); k >= 0; k-- {
			b.WriteString(strs[r.IntN(len(strs))])
		}
		return b.String()
	}
	var doc func(depth int) string
	doc = func(depth int) string {
		var parts, vals []string
		for k := r.IntN(3); k >= 0; k-- {
			v := `"` + str() + `"`
			if depth < 2 && r.IntN(3) == 0 {
				inner, _ := json.Marshal(doc(depth + 1)) // a document inside a string
				v = string(inner)
			}
			key := "k"
			if r.IntN(2) == 0 {
				key = str()
			}
			parts = append(parts, `"`+key+`":`+v)
			vals = append(vals, v)
		}
		switch r.IntN(5) {
		case 0:
			return "[" + strings.Join(vals, ",") + "]"
		case 1:
			return "{" + strings.Join(parts, ",") // truncated: not a document, scanned raw
		}
		return "{" + strings.Join(parts, ",") + "}"
	}
	skipped, named := 0, 0
	for n := 0; n < 20000; n++ {
		s := doc(0)
		var want []string
		alwaysDecode(s, 0, func(v string) { want = append(want, v) })
		d := NewDeriver()
		d.Content(s)
		got := d.Result().Headers
		if w := asSet(want); !slices.Equal(got, w) && !(len(got) == 0 && len(w) == 0) {
			t.Fatalf("on %s the gated scan answers %v and the always-decode oracle %v", s, got, w)
		}
		if !MayHoldMarker(s) {
			skipped++
		}
		if slices.ContainsFunc(got, func(v string) bool { return v != Star }) {
			named++
		}
	}
	if skipped == 0 || named == 0 {
		t.Fatalf("CONTROL: %d documents skipped by the gate and %d naming a scope — the comparison did not reach both sides", skipped, named)
	}
	t.Logf("20000 documents agree; %d skipped by the gate, %d name a scope", skipped, named)
}

// TestMemoHeadingSplitsFromTheRightWithoutSplitting holds the allocation-free heading parse to the
// `strings.Split` one it replaced, over generated headings built from separators, middle dots,
// spaces and scope-like parts — overlapping ` · · ` runs included, which a right-to-left search
// cuts differently from Split (a first draft did, and read a scope where Split read none). The
// answers must be EQUAL, not merely both invalid.
func TestMemoHeadingSplitsFromTheRightWithoutSplitting(t *testing.T) {
	split := func(line string) string {
		parts := strings.Split(line, " · ")
		if n := len(parts); n >= 4 && strings.HasPrefix(parts[n-1], "expires ") {
			return parts[n-3]
		}
		return ""
	}
	segs := []string{"alpha-notes", "beta-notes", "x", "·", "", " ", "· ·", "a · b"}
	seps := []string{" · ", " · ", " · ", " ·", "· ", " · · "}
	tails := []string{"expires 2000-01-02", "expires", "x"}
	r := rand.New(rand.NewPCG(5, 6))
	read := 0
	for n := 0; n < 20000; n++ {
		var b strings.Builder
		b.WriteString("[m-1] from ")
		for k := r.IntN(6); k >= 0; k-- {
			b.WriteString(segs[r.IntN(len(segs))])
			b.WriteString(seps[r.IntN(len(seps))])
		}
		b.WriteString(tails[r.IntN(len(tails))])
		line := b.String()
		got, _ := memoLineScope(line)
		want := split(line)
		if got != want {
			t.Fatalf("on %q the heading parse reads %q and the split %q", line, got, want)
		}
		if ValidScope(want) {
			read++
		}
	}
	if read == 0 {
		t.Fatal("CONTROL: no generated heading named a valid scope")
	}
	t.Logf("20000 headings agree; %d name a valid scope", read)
}
