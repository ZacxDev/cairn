package scopeuse

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/client"
)

// The F2 cases with LITERAL expectations. Every scope name is distinct from every other case's and
// from every constant the code names (`subsystem-store`, `cache`, `personal`, `lock`, `*`), so a
// mutant that hardcodes a literal, or returns another case's answer, cannot pass.

// TestF2NamedCacheReadAddsThatScope: case 1 — a PROVABLE read of a file under the default cache
// root, `<home>/.cache/subsystem-store/<scope>/…` with a valid scope, is a NAMED read and adds that
// scope, not `*`.
func TestF2NamedCacheReadAddsThatScope(t *testing.T) {
	cases := map[string][]string{
		"grep -n rate ~/.cache/subsystem-store/quartz-lantern/prom-s.md":               {"quartz-lantern"},
		"head -n 40 /home/u1/.cache/subsystem-store/ember_7/nix.md":                    {"ember_7"},
		"cat /root/.cache/subsystem-store/oak9/x.md":                                   {"oak9"},
		"cat ~/.cache/subsystem-store/stilt/a.md ~/.cache/subsystem-store/curlew/b.md": {"stilt", "curlew"},
		// Controls: a git range is not a `..` word, a long flag is not a path. (A flag naming a
		// cache, `--cached`, is `*` since #314 round 4: TestF2GrammarRejectsCacheWords.)
		"grep -c main..HEAD ~/.cache/subsystem-store/sanderling/x.md": {"sanderling"},
		"grep --count x ~/.cache/subsystem-store/knot5/x.md":          {"knot5"},
		// The name inside the scope's own path is not a second root.
		"cat ~/.cache/subsystem-store/dunlin/subsystem-store-notes.md": {"dunlin"},
	}
	for in, want := range cases {
		if got := CacheScopes(in); !slices.Equal(got, want) {
			t.Errorf("CacheScopes(%q) = %v, want %v", in, got, want)
		}
	}
	// Through the Deriver: the scope reaches V and Declare, and F2 contributes no `*`.
	d := NewDeriver()
	d.ToolInput("head -n 40 /home/u1/.cache/subsystem-store/ember_7/nix.md")
	r := d.Result()
	if r.F2 || !slices.Equal(r.V, []string{"ember_7"}) || !slices.Equal(r.Declare, []string{"ember_7"}) {
		t.Fatalf("F2=%v V=%v Declare=%v, want no F2 and [ember_7] in both", r.F2, r.V, r.Declare)
	}
	// It survives a capture run's Evidence round-trip.
	d2 := NewDeriver()
	d2.Restore(d.Evidence())
	if got := d2.Result().V; !slices.Equal(got, []string{"ember_7"}) {
		t.Fatalf("after Restore: V = %v, want [ember_7]", got)
	}
}

// TestF2UnattributableCacheReadAddsStar: case 2 and 3 — a cache path that cannot be attributed to
// ONE default-instance scope still fails closed.
func TestF2UnattributableCacheReadAddsStar(t *testing.T) {
	star := []string{Star}
	cases := map[string][]string{
		"S=/home/u1/.cache/subsystem-store; for f in $S/*/x.md; do cat $f; done": star,
		"ls ~/.cache/subsystem-store":                                            star,
		"ls ~/.cache/subsystem-store/":                                           star,
		`ls "$HOME/.cache/subsystem-store"`:                                      star,
		"cat ~/.cache/subsystem-store/*/index.md":                                star,
		"ls ~/.cache/subsystem-store*":                                           star,
		"cat ~/.cache/subsystem-store/bad.name1/x.md":                            star, // fails ValidScope
		"cat ~/.cache/subsystem-store/$SC1/x.md":                                 star,
		"cat ~/.cache/subsystem-store/{alder1,birch1}/x.md":                      star,
		`cat ~/.cache/subsystem-store/no"tes1"/x.md`:                             star, // the shell reads notes1, not no
		"cat ~/.cache/subsystem-store//x.md":                                     star,
		"cat ~/.cache/subsystem-store/.sync-stamp":                               star,
		"cat ~/.cache/subsystem-store/lark1/../finch1/x.md":                      star,
		"cd ~/.cache/subsystem-store/lark2 && cat x.md":                          star,
		// #314 round 1: ANY cd-family word in an input naming the cache, wherever it goes.
		"cd /work/repo && cat ~/.cache/subsystem-store/knot6/x.md": star,
		"cd ~/.cache && cat subsystem-store/lark3/x.md":            star, // relative: cwd unknown
		"cat ./subsystem-store/lark4/x.md":                         star,
		"ls ~/.cache/subsystem-store.new-abc/lark5":                star, // a sync staging sibling
		"C=~/.cache; cat $C/subsystem-store/lark6/x.md":            star, // an unrecognised spelling
		`cat "$C"/subsystem-store`:                                 star,
		// Case 3: ANOTHER instance's cache root. Derived scopes carry no instance, so a named read
		// there cannot be attributed — `*`, never its scope.
		"S=/home/u1/.cache/subsystem-store-client/ridge-x1; for f in $S/*; do :; done": star,
		"cat ~/.cache/subsystem-store-client/pipit/x.md":                               star,
	}
	for in, want := range cases {
		if got := CacheScopes(in); !slices.Equal(got, want) {
			t.Errorf("CacheScopes(%q) = %v, want %v", in, got, want)
		}
	}
	// The same checks where NOTHING ELSE re-checks them: a bash `description` is skipped by the
	// grammar ([provable]) but still scanned for the cache name, so an unattributable path there is
	// decided by this detection alone. Each row beside a provable command, so `*` sits beside that
	// command's scope (`*` makes the session owner-only whatever else is in the set).
	checkUnits(t, []unitCase{
		{"invalid component in a description", "bash", map[string]any{
			"command": "cat ~/.cache/subsystem-store/rowan-c1/x.md", "description": "~/.cache/subsystem-store/$SC2/x.md"}, []string{Star, "rowan-c1"}},
		{"another instance in a description", "bash", map[string]any{
			"command": "cat ~/.cache/subsystem-store/rowan-c2/x.md", "description": "~/.cache/subsystem-store-client/pipit2/x.md"}, []string{Star, "rowan-c2"}},
	})
	d := NewDeriver()
	d.ToolInput("S=/home/u1/.cache/subsystem-store; grep -l x $S/*/y.md")
	if r := d.Result(); !r.F2 || !slices.Equal(r.V, star) || !slices.Equal(r.Declare, star) {
		t.Fatalf("bare root: F2=%v V=%v Declare=%v, want F2 and [*] in both", r.F2, r.V, r.Declare)
	}
}

// TestF2NonCachePathsAddNothing: case 4 — `subsystem-store` that is not a cache path.
func TestF2NonCachePathsAddNothing(t *testing.T) {
	for _, in := range []string{
		"kubectl -n subsystem-store exec deploy/subsystem-store -- ls /data",
		"kubectl --namespace=subsystem-store get pods",
		"bash scripts/test-check-subsystem-store-phase1.sh",
		// A cd-family word only poisons an input that names the cache.
		"cd /work/repo && cat apps/subsystem-store/README.md",
		"cat clusters/lab1/apps/subsystem-store/ui-deployment.yaml",
		"cat ~/.config/subsystem-store/env",
		"ls /home/u1/.config/subsystem-store/instances/",
		"ls -la ~/.cache/subsystem-store.lock ~/.cache/subsystem-store-client.lock",
		"docker pull ghcr.io/zacxdev/subsystem-stores:v1",
		"git log main..HEAD -- internal/subsystem-store/x.go",
	} {
		if got := CacheScopes(in); len(got) != 0 {
			t.Errorf("CacheScopes(%q) = %v, want nothing", in, got)
		}
		d := NewDeriver()
		d.ToolInput(in)
		if r := d.Result(); r.F2 || len(r.V) != 0 {
			t.Errorf("%q: F2=%v V=%v, want neither", in, r.F2, r.V)
		}
	}
}

// TestCacheScopesMatchesTheClientsCacheRoot: the root F2 parses is the root the client resolves,
// for the default instance and for another one, so moving either turns this red.
func TestCacheScopesMatchesTheClientsCacheRoot(t *testing.T) {
	t.Setenv("HOME", "/home/u9")
	def, err := client.CacheRootFor(client.DefaultAlias)
	if err != nil {
		t.Fatal(err)
	}
	if got := CacheScopes("cat " + filepath.Join(def, "sandpiper", "a.md")); !slices.Equal(got, []string{"sandpiper"}) {
		t.Fatalf("a read under the client's default root %s: %v, want [sandpiper]", def, got)
	}
	other, err := client.CacheRootFor("client")
	if err != nil {
		t.Fatal(err)
	}
	if got := CacheScopes("cat " + filepath.Join(other, "sandpiper", "a.md")); !slices.Equal(got, []string{Star}) {
		t.Fatalf("a read under the client instance's root %s: %v, want [*]", other, got)
	}
}

// TestCacheScopesIsLinear: the pod runs F2 over every tool input it stores, so it is held to
// linear growth the way [TestHeaderScopesIsLinearOnARunOfMarkers] holds the header scan, on the
// shapes that would make a per-occurrence rescan quadratic: a run of alias suffixes (one `-`-joined
// word, so a rescan from each occurrence reads to its end), a run of cache roots, and a run of
// quoted relative roots. Same bound and method: best of five at two sizes, growth ratio under 24×.
func TestCacheScopesIsLinear(t *testing.T) {
	const small, large = 256 << 10, 2 << 20
	for _, tc := range []struct {
		name, prefix, unit string
		want               []string
	}{
		{"alias run", "", CacheRootName + "-", nil},
		{"cache roots", "", "/.cache/" + CacheRootName, []string{Star}},
		{"quoted relative roots", "", `"` + CacheRootName + "/", []string{Star}},
		// #314: NAMED reads reach the grammar ([provable]): one word it rejects, and words it
		// accepts, single-quoted segments and commands, each run to the end of the input.
		{"named reads in one word", "", "/.cache/" + CacheRootName + "/aa/x", []string{Star}},
		{"named reads as words", "cat ", "~/.cache/" + CacheRootName + "/aa/x.md ", []string{"aa"}},
		{"quoted words", "cat ~/.cache/" + CacheRootName + "/aa/x.md ", "'a|b' ", []string{"aa"}},
		{"commands", "cat ~/.cache/" + CacheRootName + "/aa/x.md", " | wc", []string{"aa"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			best := func(size int) time.Duration {
				in := tc.prefix + strings.Repeat(tc.unit, size/len(tc.unit))
				lower := asciiLower(in)
				var min time.Duration
				for run := 0; run < 5; run++ {
					seen := map[string]bool{}
					start := time.Now()
					eachCacheRead([]string{in}, []string{lower}, nil, func(v string) { seen[v] = true })
					took := time.Since(start)
					if took > 5*time.Second {
						t.Fatalf("one %d-byte input of %q took %v", size, tc.unit, took)
					}
					var got []string
					for v := range seen {
						got = append(got, v)
					}
					if !slices.Equal(asSet(got), asSet(tc.want)) {
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
