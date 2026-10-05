package touch

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/store"
)

// syntheticScope writes ONE scope of `entries` entries × `perEntry` nuance bullets,
// 3 in 11 trailered (27.3% — the larger of two measured local caches carried 27.6%),
// one session per 4 trailered bullets, ~150-byte bullets with a dated opener.
func syntheticScope(b *testing.B, entries, perEntry int) (string, *store.Index) {
	b.Helper()
	root := b.TempDir()
	prose := strings.Repeat("synthetic observation about a component ", 3)
	n := 0
	for e := 0; e < entries; e++ {
		var sb strings.Builder
		fmt.Fprintf(&sb, "---\nservice: entry-%04d\nscope: bench-notes\n---\n\n## What it is\nSynthetic.\n\n## Nuance / work-history\n", e)
		for i := 0; i < perEntry; i++ {
			fmt.Fprintf(&sb, "- 2000-%02d-%02d: %s%d", 1+n%12, 1+n%28, prose, n)
			if n%11 < 3 {
				fmt.Fprintf(&sb, " [cairn: bench-bot/s-%06d]", n/4)
			}
			sb.WriteString("\n")
			n++
		}
		writeFile(b, filepath.Join(root, "bench-notes", fmt.Sprintf("entry-%04d.md", e)), sb.String())
	}
	ix, err := store.LoadStore(root, "read", store.Unrestricted())
	if err != nil {
		b.Fatal(err)
	}
	return root, ix
}

func benchWrites(b *testing.B, entries, perEntry int) {
	root, ix := syntheticScope(b, entries, perEntry)
	b.ResetTimer()
	var res Result
	for i := 0; i < b.N; i++ {
		var err error
		res, err = Writes(root, ix, "bench-notes")
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	// Positive control: the run measured real work, not an empty scope.
	if res.Coverage.Bullets != entries*perEntry || res.Coverage.Attributed == 0 {
		b.Fatalf("coverage %+v: the benchmark did not scan what it built", res.Coverage)
	}
	b.ReportMetric(float64(res.Coverage.Bullets), "bullets")
	b.ReportMetric(float64(len(res.Sessions)), "sessions")
}

// 5,200 bullets in ONE scope ≈ the larger measured cache's bullet count across ALL of
// its 27 scopes, so a per-scope derivation at this size is already a worst case today.
func BenchmarkWrites1x(b *testing.B) { benchWrites(b, 40, 130) }

// 52,000 bullets in one scope — the plan's 10×.
func BenchmarkWrites10x(b *testing.B) { benchWrites(b, 400, 130) }
