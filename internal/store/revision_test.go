package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// writeGit materialises a `.git` directory for one scope. No repository is created and
// nothing spawns git: the reader opens these files directly, which is the whole reason it
// keeps the no-subprocess property the hot path depends on.
func writeGit(t *testing.T, root, scope string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		path := filepath.Join(root, scope, ".git", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestScopeRevisionResolvesEveryWayAHEADCanPoint(t *testing.T) {
	const sha = "a11ba11ba11ba11ba11ba11ba11ba11ba11ba11b"
	const packedSha = "b22cb22cb22cb22cb22cb22cb22cb22cb22cb22c"

	for _, tc := range []struct {
		name  string
		files map[string]string
		want  string
	}{
		{
			name:  "a DETACHED head is a bare sha, and is returned as one",
			files: map[string]string{"HEAD": sha + "\n"},
			want:  sha,
		},
		{
			name: "a symbolic ref resolves through a LOOSE ref file",
			files: map[string]string{
				"HEAD":            "ref: refs/heads/main\n",
				"refs/heads/main": sha + "\n",
			},
			want: sha,
		},
		{
			name: "…and through `packed-refs` when the loose file is absent",
			files: map[string]string{
				"HEAD": "ref: refs/heads/main\n",
				// 🔴 THE MATCHING ROW IS IN THE MIDDLE, WITH TRAILING WHITESPACE, AND BOTH
				// FACTS ARE LOAD-BEARING. Its trailing spaces are what make the per-field
				// strip reachable — measured: with the row LAST, the file-level strip an
				// earlier version applied removed them and the mutant deleting the field
				// strip SURVIVED. And a comment line plus a non-matching row before it are
				// what stop a parser that gives up on the first miss from passing.
				"packed-refs": "# pack-refs with: peeled fully-peeled sorted \n" +
					"c33dc33dc33dc33dc33dc33dc33dc33dc33dc33d refs/heads/other\n" +
					packedSha + " refs/heads/main  \n" +
					"e55fe55fe55fe55fe55fe55fe55fe55fe55fe55f refs/tags/v1\n",
			},
			want: packedSha,
		},
		{
			name: "a THIRD field means the row does not match, because the split takes at most two",
			files: map[string]string{
				"HEAD": "ref: refs/heads/main\n",
				// 🔴 `line.split(None, 1)` KEEPS EVERYTHING AFTER THE FIRST FIELD IN ONE
				// PIECE, so `<sha> refs/heads/main extra` has a second field of
				// `refs/heads/main extra` and does NOT match. A port that split on ALL
				// whitespace and compared field [1] would match it and return a sha for a
				// row that names a different thing — measured as a surviving mutant until
				// this row existed.
				"packed-refs": packedSha + " refs/heads/main extra\n",
			},
			want: RevisionUnknown,
		},
		{
			name:  "no `.git` at all is `unknown`, which is the ordinary case",
			files: nil,
			want:  RevisionUnknown,
		},
		{
			name:  "an EMPTY HEAD is `unknown` and never the empty string",
			files: map[string]string{"HEAD": "\n"},
			want:  RevisionUnknown,
		},
		{
			name: "a ref that resolves NOWHERE is `unknown`, not a fabricated sha",
			files: map[string]string{
				"HEAD":        "ref: refs/heads/main\n",
				"packed-refs": "d44ed44ed44ed44ed44ed44ed44ed44ed44ed44e refs/heads/other\n",
			},
			want: RevisionUnknown,
		},
		{
			name: "a loose ref file that is EMPTY is `unknown` and does not fall through to packed-refs",
			files: map[string]string{
				"HEAD":            "ref: refs/heads/main\n",
				"refs/heads/main": "\n",
				"packed-refs":     packedSha + " refs/heads/main\n",
			},
			want: RevisionUnknown,
		},
		{
			name: "a `packed-refs` line with ONE field is skipped rather than mis-read",
			files: map[string]string{
				"HEAD":        "ref: refs/heads/main\n",
				"packed-refs": "justonefield\n" + packedSha + " refs/heads/main\n",
			},
			want: packedSha,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, "alpha"), 0o755); err != nil {
				t.Fatal(err)
			}
			if tc.files != nil {
				writeGit(t, root, "alpha", tc.files)
			}
			got, err := ScopeRevision(root, "alpha", Unrestricted())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestScopeRevisionIsGATEDOnTheAllowlist(t *testing.T) {
	// 🔴 THE ONE ANSWER NOT DERIVED FROM THE NARROWED INDEX, so it is the one place a
	// refused scope could still be told apart from an absent one. Gated BY CONSTRUCTION
	// rather than by the fact that no scope in a served copy is currently a repo — that
	// latent state is exactly how a guard gets left out.
	const sha = "a11ba11ba11ba11ba11ba11ba11ba11ba11ba11b"
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeGit(t, root, "alpha", map[string]string{"HEAD": sha + "\n"})

	// The POSITIVE CONTROL first: the value is readable, so a refusal below is a NARROWING
	// and not a store with no repositories in it.
	if got, err := ScopeRevision(root, "alpha", VisibleScopeSet([]string{"alpha"})); err != nil || got != sha {
		t.Fatalf("an allowed caller must read the sha: %q %v", got, err)
	}
	if got, err := ScopeRevision(root, "alpha", VisibleScopeSet([]string{"beta"})); err != nil || got != RevisionUnknown {
		t.Fatalf("a refused scope must answer %q: got %q %v", RevisionUnknown, got, err)
	}
	// 🔴 AN EMPTY ALLOWLIST IS THE OPPOSITE OF AN ABSENT ONE. Nothing is visible, so
	// nothing is readable — the fail-closed direction of the whole scoped-token design.
	if got, err := ScopeRevision(root, "alpha", VisibleScopeSet(nil)); err != nil || got != RevisionUnknown {
		t.Fatalf("an empty allowlist must refuse: got %q %v", got, err)
	}
	// And the name is FOLDED before comparing, so a directory spelled one way and an
	// allowlist spelling it another still match.
	if got, err := ScopeRevision(root, "alpha", VisibleScopeSet([]string{"ALPHA"})); err != nil || got != sha {
		t.Fatalf("the allowlist folds the name: got %q %v", got, err)
	}
}

func TestAHEADThatIsNotUTF8IsREFUSEDRatherThanUnknown(t *testing.T) {
	// ⚠ THE ORACLE's `read_text(encoding="utf-8")` RAISES here, and the raise is a
	// `ValueError`, so the route answers `400 bad request` carrying the codec's own
	// sentence — not `unknown`, and not a 500. Go's `string(data)` cannot fail, so the
	// refusal has to be asked for; without this the port would answer a revision the oracle
	// refuses to produce.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "alpha", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "alpha", ".git", "HEAD"),
		[]byte{0xff, 0xfe, 0x0a}, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ScopeRevision(root, "alpha", Unrestricted())
	var unreadable *RevisionUnreadableError
	if !errors.As(err, &unreadable) {
		t.Fatalf("got %q %#v, want a *RevisionUnreadableError", got, err)
	}
	// The sentence is the codec's, which is what the route quotes back.
	if want := "'utf-8' codec can't decode byte 0xff in position 0: invalid start byte"; err.Error() != want {
		t.Fatalf("\n got %q\nwant %q", err.Error(), want)
	}
	// 🔴 THE ALLOWLIST IS CHECKED FIRST, SO A REFUSED SCOPE IS NEVER OPENED AT ALL — and
	// this is the order, not an accident of it. A caller who may not see the scope gets
	// `unknown` rather than the codec's sentence, which would otherwise be a channel telling
	// them a file they may not know about exists and is broken.
	if got, err := ScopeRevision(root, "alpha", VisibleScopeSet([]string{"beta"})); err != nil || got != RevisionUnknown {
		t.Fatalf("a refused scope must not be READ at all: got %q %v", got, err)
	}
}
