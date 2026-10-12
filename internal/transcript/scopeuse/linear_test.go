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

// TestHeaderScopesIsLinearOnARunOfMarkers is the audit's input at a size the quadratic scan could
// not finish: one 1 MiB line of back-to-back markers (the old scan took 2 min 39 s at 256 KB). The
// bound is generous — 5 s for what measures in milliseconds — because it guards an ORDER of
// growth, not a speed, and a timing test that flakes under load is one nobody believes.
func TestHeaderScopesIsLinearOnARunOfMarkers(t *testing.T) {
	line := strings.Repeat(HeaderMarker, (1<<20)/len(HeaderMarker))
	start := time.Now()
	got := HeaderScopes(line)
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("one 1 MiB line of markers took %v", took)
	}
	if len(asSet(got)) != 1 || got[0] != Star {
		t.Fatalf("a run of scope-less markers answered %v, want only *", asSet(got))
	}
}
