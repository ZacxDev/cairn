package scopeuse

import (
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"
)

// perMarkerScan is HeaderScopes as it was before #261's audit: every marker re-reads the rest of
// its line. Kept as the ORACLE the linear version is held to — the same answer, as a set.
func perMarkerScan(s string) []string {
	var out []string
	for rest := s; ; {
		i := strings.Index(rest, HeaderMarker)
		if i < 0 {
			return out
		}
		line := rest[i+len(HeaderMarker):]
		if nl := strings.IndexByte(line, '\n'); nl >= 0 {
			line = line[:nl]
		}
		rest = rest[i+len(HeaderMarker):]
		fields := scopeField.FindAllStringSubmatch(line, -1)
		if len(fields) == 0 {
			out = append(out, Star)
			continue
		}
		for _, f := range fields {
			if ValidScope(f[1]) {
				out = append(out, f[1])
			} else {
				out = append(out, Star)
			}
		}
	}
}

func asSet(in []string) []string {
	out := slices.Clone(in)
	slices.Sort(out)
	return slices.Compact(out)
}

// TestHeaderScopesMatchesThePerMarkerScan generates lines out of the pieces that decide the answer
// — markers, `scope=` with and without whitespace before it, valid and invalid values, spaces,
// tabs, newlines and filler — and requires the linear scan's SET to equal the oracle's on each.
// The control: the oracle disagrees with a scan that ignores later markers on a line, so the
// generator does reach the cases the linear version has to handle.
func TestHeaderScopesMatchesThePerMarkerScan(t *testing.T) {
	pieces := []string{HeaderMarker, HeaderMarker, " scope=alpha-notes", "scope=beta-notes", " scope=",
		" scope=(all", " ", "\t", "\n", "x", "status=recalled", "scope=gamma-notes ", HeaderMarker + "scope=delta-notes"}
	firstOnly := func(s string) []string { // a WRONG scan: one header per line, fields after the first marker only
		var out []string
		for _, line := range strings.Split(s, "\n") {
			i := strings.Index(line, HeaderMarker)
			if i < 0 {
				continue
			}
			fields := scopeField.FindAllStringSubmatch(line[i+len(HeaderMarker):], -1)
			if len(fields) == 0 {
				out = append(out, Star)
			}
			for _, f := range fields {
				if ValidScope(f[1]) {
					out = append(out, f[1])
				} else {
					out = append(out, Star)
				}
			}
		}
		return out
	}
	r := rand.New(rand.NewPCG(1, 2))
	disagreeWithWrong := 0
	for n := 0; n < 20000; n++ {
		var b strings.Builder
		for k := r.IntN(12); k >= 0; k-- {
			b.WriteString(pieces[r.IntN(len(pieces))])
		}
		s := b.String()
		want := asSet(perMarkerScan(s))
		if got := asSet(HeaderScopes(s)); !slices.Equal(got, want) {
			t.Fatalf("on %q the linear scan answers %v and the per-marker oracle %v", s, got, want)
		}
		if !slices.Equal(asSet(firstOnly(s)), want) {
			disagreeWithWrong++
		}
	}
	if disagreeWithWrong == 0 {
		t.Fatal("CONTROL: no generated line told the per-marker answer from a first-marker-only scan, so the " +
			"comparison above never reached the case the linear version must get right")
	}
	t.Logf("20000 generated lines agree; %d of them a first-marker-only scan gets wrong", disagreeWithWrong)
}

// TestTheShorterRuleIsNotEquivalent pins the two lines HeaderScopes' doc names as counterexamples to
// the proposed "first marker's fields, `*` iff nothing at/after the LAST marker" rule: the oracle's
// answers are literal here, and the scan must give the same.
func TestTheShorterRuleIsNotEquivalent(t *testing.T) {
	for line, want := range map[string][]string{
		HeaderMarker + "x " + HeaderMarker + "scope=alpha-notes":                 {Star, "alpha-notes"},
		HeaderMarker + "scope=beta-notes " + HeaderMarker + "scope=alpha-notes": {"alpha-notes", "beta-notes"},
	} {
		want = asSet(want)
		if got := asSet(perMarkerScan(line)); !slices.Equal(got, want) {
			t.Fatalf("ORACLE on %q answers %v, the doc claims %v", line, got, want)
		}
		if got := asSet(HeaderScopes(line)); !slices.Equal(got, want) {
			t.Fatalf("on %q the scan answers %v, want %v", line, got, want)
		}
	}
}

// TestHeaderScopesIsLinearOnARunOfMarkers holds the scan to LINEAR growth on the two whitespace-free
// shapes #261's audits found quadratic, each measured at TWO sizes eight times apart:
//
//   - back-to-back markers (round 1: the old scan re-read the rest of the line after every marker —
//     2 min 39 s at 256 KB);
//   - back-to-back `subsystem-recall:scope=` (round 2: the line-once scan still looked for the end of
//     the `scope=` value after EVERY marker, and with no whitespace that end is the end of the line —
//     39 ms at 32 KB, 186 ms at 64 KB, 691 ms at 128 KB).
//
// A single wall-clock bound is a claim about one size; the growth RATIO between two is the claim
// about the order. Linear grows ~8× across the pair and quadratic ~64×, so the bound sits between
// at 24×, on the best of several runs at each size so one descheduled run cannot decide it. The
// absolute 5 s cap fails a quadratic scan fast at the large size instead of letting it run minutes.
func TestHeaderScopesIsLinearOnARunOfMarkers(t *testing.T) {
	const small, large = 256 << 10, 2 << 20
	for _, tc := range []struct {
		name, unit string
		want       []string
	}{
		{"markers", HeaderMarker, []string{Star}},
		{"marker+scope=", HeaderMarker + "scope=", []string{Star}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			best := func(size int) time.Duration {
				line := strings.Repeat(tc.unit, size/len(tc.unit))
				var min time.Duration
				for run := 0; run < 5; run++ {
					// The scan the Deriver runs, timed by itself: [HeaderScopes] also grows a result
					// slice (one entry per marker here), and once the scan stopped allocating (#261
					// round 3) that slice's doubling and collection dominated the small size's
					// sub-millisecond time and swung the ratio from 11× to 30× across runs.
					var got []string
					seen := map[string]bool{}
					start := time.Now()
					eachHeaderScope(line, func(v string) { seen[v] = true })
					took := time.Since(start)
					if took > 5*time.Second {
						t.Fatalf("one %d-byte line of %q took %v", size, tc.unit, took)
					}
					for v := range seen {
						got = append(got, v)
					}
					if !slices.Equal(asSet(got), tc.want) {
						t.Fatalf("a run of %q answered %v, want %v", tc.unit, asSet(got), tc.want)
					}
					if run == 0 || took < min {
						min = took
					}
				}
				return min
			}
			s, l := best(small), best(large)
			ratio := float64(l) / float64(max(s, time.Microsecond))
			t.Logf("%q: %d B best %v, %d B best %v, growth %.1f× for 8× the input", tc.unit, small, s, large, l, ratio)
			if ratio > 24 {
				t.Fatalf("growth %.1f× for 8× the input (linear ~8×, quadratic ~64×): %v at %d B, %v at %d B",
					ratio, s, small, l, large)
			}
		})
	}
}
