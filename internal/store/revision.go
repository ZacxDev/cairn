package store

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/ZacxDev/cairn/internal/pytext"
)

// RevisionUnknown is what every failure answers: an absent repo, a detached or
// unresolvable ref, an unreadable file.
//
// 🔴 "unknown" IS HONEST; A FABRICATED SHA WOULD BE QUOTED INTO A REPORT AND BELIEVED.
const RevisionUnknown = "unknown"

// ScopeRevision is the scope's git HEAD, read from the filesystem — `git` is never
// spawned. An agent can quote `scope@sha` and have it be checkable later.
//
// Reading `.git` directly keeps the no-subprocess, no-network property the reader
// documents as load-bearing for the hot path.
//
// 🔴 IT IS A HEADER-LEVEL DISCRIMINATOR, SO IT IS GATED — AND GATED BY CONSTRUCTION
// RATHER THAN BY THE FACT THAT IT CURRENTLY CANNOT LEAK. `X-Store-Revision` is computed
// from `<store>/<scope>/.git/HEAD`, a path OUTSIDE the index entirely, so narrowing the
// index does not reach it. Today no scope in a served copy is a git repo, so it answers
// `unknown` for everything and the leak is LATENT — which is exactly the state in which
// a guard gets left out, and the day a scope becomes a repo the header starts telling a
// caller which refused scopes exist. An unrestricted ScopeSet is unrestricted here too,
// matching every other seam.
//
// ⚠ THE SCOPE IS USED RAW FOR THE PATH AND NORMALIZED ONLY FOR THE ALLOWLIST CHECK,
// which is the oracle's own asymmetry and not a transcription slip: the caller asked for
// a directory by name, and the answer has to be about the directory that name reaches.
// Every caller has already refused a component that is not a safe path component.
//
// ⚠ AND THE DECODE IS STRICT, WHICH IS WHY THIS RETURNS AN ERROR AT ALL. The oracle's
// `read_text(encoding="utf-8")` RAISES on a `HEAD` that is not valid UTF-8, and that
// raise is a `ValueError`, so the route answers `400 bad request` carrying the codec's
// own sentence — not `unknown`, and not a 500. Go's `string(data)` cannot fail, so the
// refusal has to be asked for. ⚠ ONE MEASURED DIFFERENCE REMAINS AND IS RECORDED RATHER
// THAN CHASED: on the oracle the raise happens while the response's arguments are being
// evaluated, AFTER the `result=200` audit line is already written, so that request emits
// TWO audit lines (200 then 400) where this one emits a single 400. The corpus cannot
// see it — no `world.json` entry can declare invalid UTF-8 in a `.git/HEAD` — and
// reproducing a mid-response raise to duplicate a log line would be a worse trade than
// naming it here.
func ScopeRevision(storeRoot, scope string, visible ScopeSet) (string, error) {
	if !visible.Allows(scope) {
		return RevisionUnknown, nil
	}
	gitDir := filepath.Join(storeRoot, scope, ".git")
	head, err := readGitText(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return RevisionUnknown, err
	}
	if head == nil {
		return RevisionUnknown, nil
	}
	if !strings.HasPrefix(*head, "ref:") {
		if *head == "" {
			return RevisionUnknown, nil
		}
		return *head, nil
	}
	ref := pytext.StripWhitespace(strings.SplitN(*head, ":", 2)[1])
	// ⚠ `filepath.Join` CLEANS, so a `ref:` naming `../…` cannot climb out of the git
	// directory — the oracle's `git / ref` does not clean and can. That is a
	// NARROWING, and it is the safe direction: the value comes from a file inside the
	// store, and a HEAD pointing outside its own repo is not a revision this should
	// report. Named because it is a deliberate divergence, not an accident.
	loose, err := readGitText(filepath.Join(gitDir, ref))
	if err != nil {
		return RevisionUnknown, err
	}
	if loose != nil {
		if *loose != "" {
			return *loose, nil
		}
		return RevisionUnknown, nil
	}
	packed, err := readGitText(filepath.Join(gitDir, "packed-refs"))
	if err != nil {
		return RevisionUnknown, err
	}
	if packed == nil {
		return RevisionUnknown, nil
	}
	for _, line := range pytext.SplitLines(*packed) {
		if strings.HasPrefix(line, "#") {
			continue
		}
		// `line.split(None, 1)` — AT MOST TWO FIELDS, which is why this is not
		// `SplitWhitespace`: a ref name containing whitespace stays whole in the second
		// field, and only then is it stripped and compared.
		sha, rest, twoFields := splitWhitespaceOnce(line)
		if !twoFields {
			continue
		}
		if pytext.StripWhitespace(rest) == ref {
			return sha, nil
		}
	}
	return RevisionUnknown, nil
}

// splitWhitespaceOnce is `str.split(None, 1)`: leading whitespace discarded, the first
// field, then EVERYTHING after the whitespace run that ends it — trailing whitespace
// included. `twoFields` is false when the line holds one field or none.
func splitWhitespaceOnce(line string) (first, rest string, twoFields bool) {
	runes := []rune(line)
	i := 0
	for i < len(runes) && pytext.IsSpace(runes[i]) {
		i++
	}
	start := i
	for i < len(runes) && !pytext.IsSpace(runes[i]) {
		i++
	}
	if start == i {
		return "", "", false
	}
	first = string(runes[start:i])
	for i < len(runes) && pytext.IsSpace(runes[i]) {
		i++
	}
	if i == len(runes) {
		return first, "", false
	}
	return first, string(runes[i:]), true
}

// readGitText reads one `.git` file the way the oracle does: `(nil, nil)` for any
// OSError — which is what makes an absent repo an ordinary `unknown` — a STRIPPED
// string otherwise, and an error only when the bytes are not valid UTF-8.
//
// ⚠ THE STRIP IS `str.strip()`, WHICH IS 29 CODE POINTS AND NOT `TrimSpace`'s SET.
func readGitText(path string) (*string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}
	if problem := pytext.DecodeStrictProblem(data); problem != "" {
		return nil, &RevisionUnreadableError{message: problem}
	}
	value := pytext.StripWhitespace(string(data))
	return &value, nil
}

// RevisionUnreadableError is a `.git` file that is not valid UTF-8. It is the oracle's
// `UnicodeDecodeError`, which is a `ValueError` there — so the route answers
// `400 bad request` and quotes the codec's sentence.
type RevisionUnreadableError struct{ message string }

func (e *RevisionUnreadableError) Error() string { return e.message }
