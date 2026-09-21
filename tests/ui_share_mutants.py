#!/usr/bin/env python3
"""The mutation battery over `internal/ui`'s SHARE FLOW.

🔴 A GUARD NOBODY HAS WATCHED FAIL PROVES NOTHING, AND THIS FILE IS WHERE THE SHARE
FLOW'S GUARDS ARE WATCHED FAILING. It is the same shape as `tests/control_mutants.py`
and carries the same four refusals, each of which exists because the corresponding
mistake was made in this repository before:

1. **THE ANCHOR'S OCCURRENCE COUNT IS ASSERTED BEFORE THE EDIT.** A pattern that no
   longer matches produces a mutant that was never applied, and an unapplied mutant
   reports a confident `SURVIVED` about code that was never changed. That has happened
   here twice — see `tests/parity/README.md` — and it happened again while this battery
   was being written: one anchor was written with a leading SPACE where the source has a
   TAB, and the first run reported `ANCHOR-COUNT-0` rather than a false survival.
2. **A MUTANT THAT DOES NOT COMPILE IS `DID-NOT-BUILD`, NOT A KILL.** A mutant that dies
   at the build dies for the wrong reason and proves nothing about any guard.
3. **ATTRIBUTION IS BY WHICH TEST FAILED.** A kill by a different test is reported
   `KILLED-BUT-MISATTRIBUTED`: it means the named guard may be doing nothing and
   something else caught the change.
4. **AN UNMUTATED RUN IS THE POSITIVE CONTROL.** A battery whose suite is already red
   scores every mutant KILLED and measures nothing.

⚠ THE VERB NARROWING IS NOT PART OF THE BYTECODE HAZARD `tests/control_mutants.py`
DOCUMENTS. That one is about CPython caching a same-length edit made inside one second;
this battery mutates GO source and shells out to `go test`, which keys its build cache
on content rather than on a whole-second mtime. The Go test-result cache is bypassed
with `-count=1` regardless, because a cached PASS for a tree that has changed would be
the same failure wearing different clothes.

Run it:

    python3 -u tests/ui_share_mutants.py

`-u` because this battery's output is block-buffered to a file otherwise, and a log that
looks frozen is what two agents have already sat watching.
"""

from __future__ import annotations

import os
import subprocess
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent

#: Every mutant: a name, the file, the anchor to replace, the replacement, and the test
#: that MUST be the one to fail.
#:
#: 🔴 EACH REPLACEMENT IS THE NARROWEST EXPRESSION THAT CAN BE WRONG. A mutant that
#: removes a guard TOGETHER WITH its enclosing condition dies for the wrong reason and
#: proves nothing about the guard.
MUTANTS: list[tuple[str, str, str, str, str]] = [
    (
        "audience-reads-grant-rows-instead-of-Resolve",
        "internal/ui/sharing.go",
        "\tfor _, p := range principals(m) {\n\t\tauth := control.Resolve(m, p)\n"
        "\t\tverbs := auth.VerbsOn(scope)",
        "\tfor _, p := range principalsFromGrantsOnly(m, scope) {\n\t\tauth := control.Resolve(m, p)\n"
        "\t\tverbs := auth.VerbsOn(scope)",
        "TestTheAudienceIsComputedFromResolveNotFromGrantRows",
    ),
    (
        "notice-drops-the-already-copied-clause",
        "internal/ui/render.go",
        '\t"revoking a share stops future syncs: it does not recall entries already copied onto " +\n'
        '\t"somebody\'s machine."',
        '\t"revoking a share stops future syncs."',
        "TestTheReplicaHonestyNoticeIsPinnedWhole",
    ),
    (
        "verbs-read-single-value",
        "internal/ui/sharehandlers.go",
        "\tfor _, raw := range r.PostForm[FieldVerb] {\n\t\tverbs = append(verbs, control.Verb(raw))\n\t}",
        '\tif raw := r.PostFormValue(FieldVerb); raw != "" {\n\t\tverbs = append(verbs, control.Verb(raw))\n\t}',
        "TestEveryTickedVerbReachesTheGrant",
    ),
    (
        "subject-accepted-from-the-form",
        "internal/ui/sharehandlers.go",
        "\tsubject, ok := pick(candidates, control.ID(r.PostFormValue(FieldSubject)))\n"
        "\tif !ok {\n\t\twritePlain(w, http.StatusForbidden, shareWriteRefusal)\n\t\treturn\n\t}",
        "\tsubject := Subject{Kind: control.KindUser, ID: control.ID(r.PostFormValue(FieldSubject))}\n"
        "\t_ = candidates",
        "TestTheSubjectIsValidatedAgainstCandidatesRatherThanAcceptedFromTheForm",
    ),
    (
        "unshare-skips-the-object-authority-check",
        "internal/ui/sharing.go",
        "\tif g.ObjectKind != control.ObjectScope || !auth.Allows(g.ObjectID, control.VerbAdmin) {",
        "\tif g.ObjectKind != control.ObjectScope {\n\t\t_ = auth",
        "TestARevokeIsAuthorisedFromTheGrantRatherThanFromTheForm",
    ),
    (
        "share-page-skips-the-authority-check",
        "internal/ui/sharehandlers.go",
        "\tif !id.Auth.Allows(scope, control.VerbAdmin) {\n"
        "\t\twritePlain(w, http.StatusNotFound, scopeRefusal)\n\t\treturn\n\t}",
        "\t_ = scopeRefusal",
        "TestTheSharePageRefusesAScopeThisCallerCannotAdminister",
    ),
    (
        "page-never-reports-a-read-only-authority",
        "internal/ui/sharehandlers.go",
        "\t\tReadOnly: !s.sharing.Writable(),",
        "\t\tReadOnly: false,",
        "TestAReadOnlyDeploymentSaysSoOnThePageRatherThanAtTheClick",
    ),
    # 🔴 THE WRITE PATH'S AUTHORITY CHECK IS DELIBERATELY REDUNDANT, SO THE MUTANT HAS TO
    # REMOVE BOTH HALVES AT ONCE. The handler checks so it can choose an HTTP status;
    # `ControlSharing.Share` checks because the interface is exported and a second caller
    # that forgot would be authorised by omission. Removing EITHER alone is observably
    # equivalent — the other still answers 403 with the same body — which is what defence
    # in depth means, and recording that as a SURVIVOR would be a finding that is not one.
    # What must be killable is removing both, and that is this row. See `PAIRED`.
    (
        "share-write-authority-check-removed-at-BOTH-sites",
        "internal/ui/sharehandlers.go",
        "\tscope := control.ID(r.PostFormValue(FieldScope))\n"
        "\tif !id.Auth.Allows(scope, control.VerbAdmin) {\n"
        "\t\twritePlain(w, http.StatusForbidden, shareWriteRefusal)\n\t\treturn\n\t}",
        "\tscope := control.ID(r.PostFormValue(FieldScope))",
        "TestTheSharePageRefusesAScopeThisCallerCannotAdminister",
    ),
]

#: The second half of the BOTH-sites mutant, in a different file, applied in the same
#: run. Its anchor count is asserted exactly as the primary one's is.
PAIRED = (
    "internal/ui/sharing.go",
    "\tif !auth.Allows(scope, control.VerbAdmin) {\n\t\treturn Effect{}, ErrNotPermitted\n\t}",
    "\t_ = auth",
)

#: The helper the audience mutant needs. It is appended rather than substituted because
#: a mutant that leaves the tree uncompilable dies at the build, which proves nothing.
HELPER = """

// principalsFromGrantsOnly is a MUTANT helper: the WRONG answer to "who can see this",
// namely the grant table alone. It exists only while this battery runs.
func principalsFromGrantsOnly(m control.Model, scope control.ID) []control.Principal {
	var out []control.Principal
	for _, g := range m.Grants {
		if !g.Live() || g.ObjectKind != control.ObjectScope || g.ObjectID != scope {
			continue
		}
		if p, known := m.PrincipalFor(g.SubjectKind, g.SubjectID); known {
			out = append(out, p)
		}
	}
	return out
}
"""

PACKAGE = "./internal/ui/"


def run(*args: str) -> subprocess.CompletedProcess:
    return subprocess.run(args, cwd=REPO, capture_output=True, text=True)


def main() -> int:
    if run("go", "version").returncode != 0:
        print("ui-share-mutants: no go toolchain on PATH — REFUSING rather than skipping. "
              "A skip nobody counts is a pass, and this battery is the only thing watching "
              "these guards fail.", file=sys.stderr)
        return 2

    control_run = run("go", "test", "-count=1", PACKAGE)
    if control_run.returncode != 0:
        print("POSITIVE CONTROL FAILED: the UNMUTATED package is already red, so every mutant "
              "below would score KILLED while measuring nothing.\n" + control_run.stdout[-3000:],
              file=sys.stderr)
        return 2
    print("POSITIVE CONTROL: the unmutated package is green")

    results: list[tuple[str, str, str]] = []
    for name, path, anchor, repl, killer in MUTANTS:
        target = REPO / path
        original = target.read_text()
        occurrences = original.count(anchor)
        if occurrences != 1:
            results.append((name, f"ANCHOR-COUNT-{occurrences} (did not apply)", killer))
            continue

        mutated = original.replace(anchor, repl, 1)
        if name.startswith("audience-"):
            mutated += HELPER
        target.write_text(mutated)

        paired_path = paired_original = None
        if "BOTH-sites" in name:
            paired_path = REPO / PAIRED[0]
            paired_original = paired_path.read_text()
            if paired_original.count(PAIRED[1]) != 1:
                target.write_text(original)
                results.append((name, "PAIRED-ANCHOR (did not apply)", killer))
                continue
            paired_path.write_text(paired_original.replace(PAIRED[1], PAIRED[2], 1))

        try:
            if run("go", "vet", PACKAGE).returncode != 0:
                results.append((name, "DID-NOT-BUILD", killer))
                continue
            out = run("go", "test", "-count=1", "-run", f"^{killer}$", PACKAGE)
            if out.returncode == 0:
                results.append((name, "SURVIVED", killer))
            elif f"--- FAIL: {killer}" in out.stdout:
                results.append((name, "KILLED", killer))
            else:
                results.append((name, "KILLED-BUT-MISATTRIBUTED", killer))
        finally:
            target.write_text(original)
            if paired_path is not None and paired_original is not None:
                paired_path.write_text(paired_original)

    print(f"\n{'MUTANT':<52}{'VERDICT':<30}NAMED KILLER")
    for name, verdict, killer in results:
        print(f"{name:<52}{verdict:<30}{killer}")

    killed = sum(1 for _, v, _ in results if v == "KILLED")
    survived = sum(1 for _, v, _ in results if v == "SURVIVED")
    other = len(results) - killed - survived
    print(f"\nSUMMARY mutants={len(results)} killed={killed} survived={survived} other={other}")

    # A survivor or an unapplied mutant is a FAILURE. `other` covers both
    # `DID-NOT-BUILD` and `ANCHOR-COUNT-*`, which are the two outcomes that look like
    # results and are not.
    return 0 if survived == 0 and other == 0 else 1


if __name__ == "__main__":
    os.environ.setdefault("GOFLAGS", "")
    sys.exit(main())
