package codesrc

import (
	"errors"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// Every example maps to a LITERAL canonical string — never one derived from the parser under test.
func TestEveryExampleParsesToItsLiteralCanonicalForm(t *testing.T) {
	cases := []struct{ in, want string }{
		{"git:github.com/example-org/example-repo@main", "git:github.com/example-org/example-repo@main"},
		{"git:github.com/example-org/example-mono//services/widget@release/2.x", "git:github.com/example-org/example-mono//services/widget@release/2.x"},
		{"git:git.example.com/team/sub-group/example-repo@trunk", "git:git.example.com/team/sub-group/example-repo@trunk"},
		// The host is lowercased; the repo path's CASE IS PRESERVED (compared byte-identically, as
		// `TaskRef`'s id half is). A fixture whose path is already lowercase cannot see a mutant that
		// folds it, so this one is mixed-case on purpose.
		{"git:GitHub.COM/Example-Org/Example-Repo@Feature/X", "git:github.com/Example-Org/Example-Repo@Feature/X"},
		// A trailing `.git` is a spelling of the same repository.
		{"git:github.com/example-org/example-repo.git@main", "git:github.com/example-org/example-repo@main"},
		// `#` is NOT refused: git accepts it in a branch (`git check-ref-format --branch issue#12`).
		{"git:github.com/example-org/example-repo@issue#12", "git:github.com/example-org/example-repo@issue#12"},
	}
	for _, tc := range cases {
		src, err := Parse(tc.in)
		if err != nil {
			t.Fatalf("Parse(%q): %v", tc.in, err)
		}
		if got := src.Canonical(); got != tc.want {
			t.Errorf("Parse(%q).Canonical() = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Each refusal has a fixture valid in every respect but one, and the test asserts that refusal's
// OWN rule: a fixture refused for a neighbouring reason is how a dropped check hides.
func TestEachRefusalIsForItsOwnRule(t *testing.T) {
	cases := []struct{ name, in, rule string }{
		{"a scheme", "git:https://github.com/example-org/example-repo@main", RuleScheme},
		{"userinfo", "git:someone@github.com/example-org/example-repo@main", RuleUserinfo},
		{"a port", "git:github.com:8443/example-org/example-repo@main", RulePort},
		{"a non-DNS host", "git:git_host.example.com/example-org/example-repo@main", RuleHost},
		{"a single-label host", "git:example-org/example-repo@main", RuleHost},
		{"a one-segment repo path", "git:github.com/example-repo@main", RuleRepoPath},
		{"a `..` subpath", "git:github.com/example-org/example-mono//services/../widget@main", RuleDotDot},
		{"a missing @", "git:github.com/example-org/example-repo", RuleNoBranch},
		{"a branch with a leading -", "git:github.com/example-org/example-repo@-delete-me", RuleBranchDash},
		{"a branch with a space", "git:github.com/example-org/example-repo@my branch", RuleWhitespace},
		{"a comma", "git:github.com/example-org/example-repo@main,dev", RuleComma},
		{"a control character", "git:github.com/example-org/example-repo@ma\x01in", RuleControl},
		{"invalid UTF-8", "git:github.com/example-org/example-repo@ma\xffin", RuleUTF8},
		{"a C1 control (CSI)", "git:github.com/example-org/example-repo@ma\u009bin", RuleControl},
		{"DEL", "git:github.com/example-org/example-repo@ma\x7fin", RuleControl},
		{"a bidi override", "git:github.com/example-org/example-repo@ma\u202ein", RuleFormat},
		{"a bidi isolate", "git:github.com/example-org/example-repo@ma\u2066in", RuleFormat},
		{"a zero-width space", "git:github.com/example-org/example-repo@ma\u200bin", RuleFormat},
		{"a zero-width joiner", "git:github.com/example-org/example-repo@ma\u200din", RuleFormat},
		{"a BOM", "git:github.com/example-org/example-repo@ma\ufeffin", RuleFormat},
		{"a no-break space", "git:github.com/example-org/example-repo@ma\u00a0in", RuleWhitespace},
		{"NEL", "git:github.com/example-org/example-repo@ma\u0085in", RuleWhitespace},
		{"a trailing # comment", "git:github.com/example-org/example-repo@main # primary", RuleWhitespace},
		{"a second @ (in the branch)", "git:github.com/example-org/example-repo@a@b", RuleBranchAt},
		{"no repo path at all", "git:github.com@main", RuleRepoPath},
		{"no git: prefix", "github.com/example-org/example-repo@main", RulePrefix},
		{"a segment with a leading dot", "git:github.com/example-org/.hidden@main", RuleSegment},
		{"an empty subpath", "git:github.com/example-org/example-mono//@main", RuleEmptySub},
		{"a branch that is not a ref name", "git:github.com/example-org/example-repo@topic..x", RuleBranchRef},
		{"empty", "", RuleEmptySource},
	}
	seen := map[string]bool{}
	for _, tc := range cases {
		if seen[tc.in] {
			t.Fatalf("fixture %q is reused; fixtures must be pairwise distinct", tc.in)
		}
		seen[tc.in] = true
		_, err := Parse(tc.in)
		var pe *ParseError
		if !errors.As(err, &pe) {
			t.Errorf("%s: Parse(%q) = %v, want a *ParseError with rule %q", tc.name, tc.in, err, tc.rule)
			continue
		}
		if pe.Rule != tc.rule {
			t.Errorf("%s: Parse(%q) refused for %q, want its OWN rule %q", tc.name, tc.in, pe.Rule, tc.rule)
		}
	}
}

// Nine distinct sources are refused (Q14's cap), naming the ninth; eight are accepted. With a
// DUPLICATE before them, the refusal names the ninth distinct source's ORIGINAL line (10), never its
// position in the deduped list.
func TestNineSourcesAreRefusedAndEightAreNot(t *testing.T) {
	var nine []string
	for _, r := range []string{"r1", "r2", "r3", "r4", "r5", "r6", "r7", "r8", "r9"} {
		nine = append(nine, "git:github.com/example-org/"+r+"@main")
	}
	if _, err := ParseList(nine[:8]); err != nil {
		t.Fatalf("eight sources must be accepted: %v", err)
	}
	_, err := ParseList(nine)
	var pe *ParseError
	if !errors.As(err, &pe) || pe.Rule != RuleTooMany || pe.Index != 9 {
		t.Fatalf("nine sources: got %v, want rule %q at index 9", err, RuleTooMany)
	}
	withDup := append([]string{nine[0]}, nine...)
	_, err = ParseList(withDup)
	if !errors.As(err, &pe) || pe.Rule != RuleTooMany || pe.Index != 10 || pe.Input != nine[8] {
		t.Fatalf("nine distinct after a duplicate: got %v, want rule %q at the ORIGINAL line 10 naming %q", err, RuleTooMany, nine[8])
	}
}

// Duplicates are DEDUPED (by canonical form), never refused, and ORDER IS KEPT — the first source
// is the primary. The input is deliberately NOT in sorted order, so a sort cannot pass.
func TestDuplicatesAreDroppedAndOrderIsKept(t *testing.T) {
	in := []string{
		"git:github.com/example-org/zeta-repo@main",
		"git:git.example.com/team/alpha-repo@trunk",
		"git:GITHUB.com/example-org/zeta-repo.git@main", // the first one, spelled differently
		"git:github.com/example-org/mid-repo@main",
	}
	got, err := ParseList(in)
	if err != nil {
		t.Fatalf("a duplicate must be dropped, not refused: %v", err)
	}
	want := []string{
		"git:github.com/example-org/zeta-repo@main",
		"git:git.example.com/team/alpha-repo@trunk",
		"git:github.com/example-org/mid-repo@main",
	}
	if c := Canonicals(got); !slices.Equal(c, want) {
		t.Fatalf("ParseList = %q, want %q (deduped, first-seen order)", c, want)
	}
}

// A refused line in a LIST names its 1-based position — the browser form's line number.
func TestAListRefusalNamesItsPosition(t *testing.T) {
	_, err := ParseList([]string{"git:github.com/example-org/example-repo@main", "git:github.com/example-org/other-repo"})
	var pe *ParseError
	if !errors.As(err, &pe) || pe.Index != 2 || pe.Rule != RuleNoBranch {
		t.Fatalf("got %v, want index 2 rule %q", err, RuleNoBranch)
	}
	if !strings.HasPrefix(pe.Error(), "source 2 ") {
		t.Fatalf("message %q does not name the position", pe.Error())
	}
}

// Key is `store.NormalizeRef` of the directory name: two spellings, one LITERAL key.
func TestKeyFoldsTheScopeNameToOneLiteral(t *testing.T) {
	for _, in := range []string{"Alpha-Notes", "alpha-notes", " ALPHA-NOTES "} {
		if got := Key(in); got != "alpha-notes" {
			t.Errorf("Key(%q) = %q, want the literal %q", in, got, "alpha-notes")
		}
	}
}

// The branch rule is written out rather than shelled out, so it is checked against the real
// `git check-ref-format --branch` wherever git exists. TWO TIERS, the repository's rule: a skip is
// acceptable only where `CAIRN_GIT_TESTS_REQUIRED` is unset (the nix check phase has no git).
func TestTheBranchRuleAgreesWithGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		if os.Getenv("CAIRN_GIT_TESTS_REQUIRED") != "" {
			t.Fatalf("CAIRN_GIT_TESTS_REQUIRED is set but git is absent: %v", err)
		}
		t.Skipf("no git here, so the written-out rule cannot be compared with it (%v)", err)
	}
	// No name here carries '@': `Parse` refuses every one by `RuleBranchAt` before this rule runs,
	// and git's `--branch` gives '@' a meaning of its own (it accepts a bare "@" — measured, the
	// first run of this test went red on exactly that).
	names := []string{
		"main", "release/2.x", "feature/x-y_z", "topic..x", "a/.hidden", "a.lock", "a/b.lock/c",
		"/lead", "trail/", "dbl//slash", "end.", "HEAD", "til~de", "car^et",
		"co:lon", "q?m", "st*r", "br[acket", "back\\slash", "ok.dot.inside", "x.lockfile", "v1.2.3",
	}
	for _, n := range names {
		out, err := exec.Command("git", "check-ref-format", "--branch", n).CombinedOutput()
		gitOK := err == nil
		if gitOK != isBranchName(n) {
			t.Errorf("branch %q: git says valid=%v (%s), isBranchName says %v", n, gitOK, strings.TrimSpace(string(out)), isBranchName(n))
		}
	}
	// Positive control on the comparison itself: both verdicts must occur, or the loop compared
	// nothing that could disagree.
	var valid, invalid int
	for _, n := range names {
		if isBranchName(n) {
			valid++
		} else {
			invalid++
		}
	}
	if valid == 0 || invalid == 0 {
		t.Fatalf("the corpus has %d valid and %d invalid names; it must exercise both", valid, invalid)
	}
}
