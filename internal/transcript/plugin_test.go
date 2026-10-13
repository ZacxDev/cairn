package transcript

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ZacxDev/cairn/internal/transcript/transcripttest"
)

// TestForPluginsWalksTheStoreOnceAndFindsEveryRootsTrailers: three stored roots whose stored `V` is
// empty (each must be WALKED) are decided over ONE whole-store walk, and each still gets exactly the
// scopes its own trailers are in — `s-a` alpha, `s-b` beta, `s-c` none. RED before #270 round 1:
// three walks (one per root). Synthetic: scopes alpha-notes/beta-notes, sessions s-a/s-b/s-c.
func TestForPluginsWalksTheStoreOnceAndFindsEveryRootsTrailers(t *testing.T) {
	root := t.TempDir()
	for scope, line := range map[string]string{
		"alpha-notes": "- 2000-01-03: a noted [cairn: fixture-bot/s-a]",
		"beta-notes":  "- 2000-01-04: b noted [cairn: fixture-bot/s-b]",
	} {
		if err := os.MkdirAll(filepath.Join(root, scope), 0o755); err != nil {
			t.Fatal(err)
		}
		body := "---\nservice: gadget\nscope: " + scope + "\n---\n\n## Nuance / work-history\n\n" + line + "\n"
		if err := os.WriteFile(filepath.Join(root, scope, "gadget.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a := transcripttest.Open(t)
	for _, r := range []string{"s-a", "s-b", "s-c"} {
		transcripttest.Append(t, a, transcripttest.Session{Root: r, Owner: "user:u-owner"}, "main",
			[]string{`{"type":"user","message":{"role":"user","content":"synthetic"}}`})
	}
	seen := map[string][]string{}
	on := func(m *Meta, v []string) bool { seen[m.Root] = slices.Clone(v); return true }
	before := storeWalks.Load()
	views, err := NewStore(a, root).ForPlugins([]string{"s-a", "s-b", "s-c"}, on, func(string, error) {})
	if err != nil {
		t.Fatal(err)
	}
	if walks := storeWalks.Load() - before; walks != 1 {
		t.Fatalf("deciding three roots walked the store %d times, want 1", walks)
	}
	if len(views) != 3 || !slices.Equal(seen["s-a"], []string{"alpha-notes"}) || !slices.Equal(seen["s-b"], []string{"beta-notes"}) ||
		len(seen["s-c"]) != 0 {
		t.Fatalf("views=%d, V per root %v — want alpha for s-a, beta for s-b, nothing for s-c", len(views), seen)
	}
	// 🔴 A ROOT `on` REFUSES OVER ITS STORED HALF IS STILL WALKED (#270's audit): the fold is not
	// monotone in `V`, so "OFF so far" settles nothing. One walk, however many roots; and none only
	// when no root is stored at all.
	no := func(*Meta, []string) bool { return false }
	before = storeWalks.Load()
	if _, err := NewStore(a, root).ForPlugins([]string{"s-a", "s-b"}, no, nil); err != nil {
		t.Fatal(err)
	}
	if walks := storeWalks.Load() - before; walks != 1 {
		t.Fatalf("two roots whose stored V is refused walked the store %d times, want exactly 1", walks)
	}
	before = storeWalks.Load()
	if _, err := NewStore(a, root).ForPlugins([]string{"s-never-stored"}, no, nil); err != nil {
		t.Fatal(err)
	}
	if walks := storeWalks.Load() - before; walks != 0 {
		t.Fatalf("no stored root walked the store %d times, want 0", walks)
	}
}
