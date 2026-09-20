#!/usr/bin/env python3
"""THE MUTATION BATTERY OVER THE ROUTING GUARDS — break each one on purpose, watch it die.

```bash
python3 tests/routing_mutants.py             # the whole battery
python3 tests/routing_mutants.py --only row3-refusal-deleted
python3 tests/routing_mutants.py --list      # the ids, without running anything
```

🔴 WHY A SCRIPT AND NOT A NOTE. "Twenty mutants killed" written in a PR body is a claim nobody
can re-run; an auditor reading it has to take the number on trust, and a self-reported mutation
result is exactly the kind of claim that gets asserted without being run. This file is the
measurement, so the next reader re-derives it instead of believing it.

🔴 THREE THINGS THAT MAKE A MUTATION RESULT MEAN ANYTHING, and each one has cost this project a
wrong answer at least once elsewhere:

  * **the mutant must have APPLIED.** Every mutant asserts its anchor occurs EXACTLY once in the
    target file. A substitution that matched nothing scores SURVIVED and reads as "the guard is
    untested" — a finding about the harness wearing a finding about the code.
  * **the mutant must have RUN.** CPython validates a cached `.pyc` on source mtime-in-whole-
    SECONDS plus size, so a same-length edit landing in the same second as the last import is
    INVISIBLE: the test imports the original bytecode and the mutant is scored SURVIVED without
    ever executing. Every run sets `PYTHONDONTWRITEBYTECODE=1` **and** deletes every
    `__pycache__` in the mutant tree before the run.
  * **the batch must contain a mutant you KNOW dies.** `positive-control` is that row. If it
    survives, the runner is wired to nothing and every other verdict in the table is void — the
    harness exits 2 ("could not vouch"), not 1.

🔴 AND A MUTANT MUST DIE FOR **ITS OWN** REASON. A mutant killed by a different guard's error is
green for the wrong reason and would still be green with the guard under test deleted. Each row
declares `kills`: a test name that MUST be among the failures. A mutant that went red without
that test is reported as `KILLED-BY-THE-WRONG-TEST`, which is a failure of the battery, not a
pass.
"""
from __future__ import annotations

import argparse
import os
import re
import shutil
import subprocess
import sys
import tempfile
from dataclasses import dataclass, field
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]

PY_ROUTING = "tests/test_cairn_instances.py"
PY_STORE = "tests/test_subsystem_read_store.py"


@dataclass(frozen=True)
class Mutant:
    id: str
    #: Repo-relative file to edit.
    target: str
    #: The anchor, which must occur EXACTLY once, and what replaces it.
    old: str
    new: str
    #: Why this mutation is a plausible defect rather than a typo.
    why: str
    #: A test name that must be among the failures. `""` means "any failure counts", which is
    #: only used for the positive control.
    kills: str = ""
    #: The suites to run. Narrow on purpose: the battery is worthless if it takes an hour.
    suites: tuple[str, ...] = (PY_ROUTING,)
    #: A Go package to `go test` instead of pytest.
    go_package: str = ""
    extra: dict = field(default_factory=dict)


MUTANTS: list[Mutant] = [
    # --- the positive control ------------------------------------------------
    Mutant(
        id="positive-control",
        target="lib/cairn_instances.py",
        old="        return len(self.instances) > 1",
        new="        raise AssertionError('positive control')",
        why="the row that proves the runner EXECUTES the tree it just edited. A battery whose "
            "control survives is wired to nothing and every other verdict in it is void.",
    ),

    # --- the LABELLING predicate ---------------------------------------------
    Mutant(
        id="multi-instance-counts-the-table",
        target="lib/cairn_instances.py",
        old="        return len(self.instances) > 1",
        new="        return self.routes is not None or len(self.instances) > 1",
        why="THE SHIPPED DEFECT, restored exactly. A table's presence switches labelling on, so "
            "a one-instance host with a table asserts 'with more than one instance configured' "
            "on every recall and refuses every scope the table does not name.",
        kills="test_a_table_on_ONE_instance_LABELS_NOTHING",
    ),
    Mutant(
        id="multi-instance-always-true",
        target="lib/cairn_instances.py",
        old="        return len(self.instances) > 1",
        new="        return True",
        why="every single-instance host labelled — the compatibility guarantee inverted.",
        kills="test_the_banner_carries_NO_instance_label",
    ),
    Mutant(
        id="multi-instance-always-false",
        target="lib/cairn_instances.py",
        old="        return len(self.instances) > 1",
        new="        return False",
        why="nothing ever labelled, so a two-instance host cannot say which store answered.",
        kills="test_a_routed_recall_says_an_absence_may_be_on_ANOTHER_instance",
    ),

    # --- the ROUTING decision, row by row ------------------------------------
    Mutant(
        id="row3-refusal-deleted",
        target="lib/cairn_instances.py",
        old="            if alias not in self.aliases:",
        new="            if False:",
        why="THE SILENT MISROUTE. A table entry naming an alias this host has no config for "
            "resolves to the sole instance instead of refusing — a write landing in a store "
            "nobody decided on.",
        kills="test_row3_a_table_naming_an_UNCONFIGURED_alias_still_REFUSES",
    ),
    Mutant(
        id="row2-refuses-again",
        target="lib/cairn_instances.py",
        old="        if not self.multi_instance:\n            return DEFAULT_ALIAS",
        new="        if False:\n            return DEFAULT_ALIAS",
        why="the one-instance fallback removed, so a scope the table has not been taught yet "
            "becomes unreadable on a host with exactly one store.",
        kills="test_row2_a_table_that_does_NOT_name_the_scope_still_resolves",
    ),
    Mutant(
        id="sole-instance-fallback-inverted",
        target="lib/cairn_instances.py",
        old="        if not self.multi_instance:\n            return DEFAULT_ALIAS",
        new="        if self.multi_instance:\n            return DEFAULT_ALIAS",
        why="the fallback fires on the MULTI-instance host instead — the 'pick one' behaviour "
            "the whole design exists to refuse.",
        kills="test_two_instances_and_NO_table_refuses_rather_than_picking_one",
    ),
    Mutant(
        id="no-table-refusal-deleted",
        target="lib/cairn_instances.py",
        old="        if self.routes is None:\n            raise UnroutedScope(",
        new="        if False:\n            raise UnroutedScope(",
        why="two instances and NO table would fall through to the missing-entry refusal, which "
            "names a table that does not exist and sends the operator to edit nothing. 🔴 THIS "
            "ROW SURVIVED ITS FIRST RUN, and the survival was a real coverage gap rather than "
            "an equivalent mutant: both arms raise `UnroutedScope` at exit 11 and both contain "
            "the words `routing table`, so the guard asserted nothing about WHICH remedy the "
            "operator was sent to. The test now pins the distinguishing sentence.",
        kills="test_two_instances_and_NO_table_refuses_rather_than_picking_one",
    ),
    Mutant(
        id="go-no-table-refusal-deleted",
        target="internal/client/instances.go",
        old="\tif r.Routes == nil {\n\t\tpath, _, _ := RoutesFile(nil)",
        new="\tif false {\n\t\tpath, _, _ := RoutesFile(nil)",
        why="the same gap in the port, added the moment the Python survivor was understood — "
            "both refusals are an `*UnroutedScope` at exit 11, so only the message separates "
            "`create a table` from `edit the one you have`.",
        kills="TestTwoInstancesKeepTheFullRefusalSemantics",
        go_package="./internal/client/",
    ),

    # --- the grader ----------------------------------------------------------
    Mutant(
        id="check-direction1-does-not-ask-the-resolver",
        target="lib/cairn_instances.py",
        old="            try:\n                self.alias_for(scope)\n            except "
            "UnroutedScope:\n                problems.append(\n                    f\"scope "
            "`{scope}` exists but the routing table does not name \"",
        new="            if True:\n                pass\n            if True:\n"
            "                problems.append(\n                    f\"scope "
            "`{scope}` exists but the routing table does not name \"",
        why="finding 3's re-derivation restored: the grader predicts a refusal by subtracting "
            "key sets, so a ONE-instance host is warned about writes that will not refuse.",
        kills="test_an_UNNAMED_scope_is_NOT_a_problem_at_ONE_instance",
    ),
    Mutant(
        id="check-direction2-deleted",
        target="lib/cairn_instances.py",
        old="        for scope in sorted(set(self.routes) - scope_set):",
        new="        for scope in sorted(set()):",
        why="the SILENT direction — a table entry for a scope that holds nothing is not "
            "reported at all, so a genuinely stale one reads as coverage and survives a rename. "
            "It is a NOTE rather than a problem (see `Routing.check`), and deleting a note is "
            "exactly as invisible as deleting a finding.",
        kills="test_an_entry_naming_NO_scope_is_a_NOTE_and_not_a_problem",
    ),
    Mutant(
        id="check-direction3-deleted",
        target="lib/cairn_instances.py",
        old="        for scope in sorted(self.routes):\n            try:",
        new="        for scope in sorted(set()):\n            try:",
        why="an entry naming an unconfigured alias becomes a future refusal nobody can see "
            "until a write hits it.",
        kills="test_an_entry_naming_an_UNCONFIGURED_alias_is_a_problem",
    ),
    Mutant(
        id="check-grades-an-absent-table",
        target="lib/cairn_instances.py",
        old="        if self.routes is None:\n            raise RoutingConfigError(",
        new="        if False:\n            raise RoutingConfigError(",
        why="an absent table read as an empty one would report every scope in the store as "
            "unrouted — findings about a table nobody wrote.",
        kills="test_grading_a_host_with_NO_TABLE_refuses_rather_than_reporting_clean",
    ),

    # --- discovery and the table file ----------------------------------------
    Mutant(
        id="a-bad-instance-file-is-skipped",
        target="lib/cairn_instances.py",
        old="        if not valid_alias(alias) or alias == DEFAULT_ALIAS:",
        new="        if False:",
        why="a file the operator wrote is ignored with no message anywhere, and the scopes they "
            "routed to it refuse later naming the ALIAS rather than the file.",
        kills="test_a_file_that_cannot_be_an_alias_is_an_ERROR_not_a_skip",
    ),
    Mutant(
        id="an-explicit-missing-table-is-not-an-error",
        target="lib/cairn_instances.py",
        old="    elif explicit:\n        raise RoutingConfigError(",
        new="    elif False:\n        raise RoutingConfigError(",
        why="a typo'd $CAIRN_ROUTES turns a fail-loud configuration into a fail-open one.",
        kills="test_an_EXPLICIT_table_path_that_does_not_exist_is_an_ERROR",
    ),
    Mutant(
        id="a-table-value-need-not-be-an-alias",
        target="lib/cairn_instances.py",
        old="        if not valid_alias(alias):",
        new="        if False:",
        why="an alias that is not a path segment reaches `cache_root_for`, which is the guard "
            "between a hand-edited file and a filesystem path.",
        kills="test_a_table_that_is_not_a_FLAT_MAP_OF_ALIASES_is_refused",
    ),

    # --- the cache layout ----------------------------------------------------
    Mutant(
        id="instance-cache-is-a-CHILD-not-a-sibling",
        target="lib/subsystem_read_store.py",
        old="    return root.parent / f\"{root.name}-{alias}\"",
        new="    return root / alias",
        why="every reader enumerates `<root>/<dir>` as the scope list, so an alias directory "
            "under the default root is reported as a scope that exists and holds no entries.",
        kills="test_another_instance_is_a_SIBLING_not_a_child_of_the_default_root",
    ),
    Mutant(
        id="the-default-cache-root-moves",
        target="lib/subsystem_read_store.py",
        old="    if alias == DEFAULT_ALIAS:\n        return root",
        new="    if False:\n        return root",
        why="every existing host has a populated cache at that path and every recall PRINTS it; "
            "moving it orphans the cache and changes the bytes of every report.",
        kills="test_the_default_instance_keeps_the_EXACT_legacy_cache_root",
    ),
    Mutant(
        id="an-alias-may-be-any-string",
        target="lib/subsystem_read_store.py",
        old="_ALIAS = re.compile(r\"^[a-z0-9][a-z0-9-]*$\")",
        new="_ALIAS = re.compile(r\"^.*$\")",
        why="`..` as an alias puts a cache one directory above the root — the reason the check "
            "sits between the table and the filesystem rather than at the filesystem.",
        kills="test_an_alias_that_is_not_a_PATH_SEGMENT_is_refused",
        suites=(PY_ROUTING, PY_STORE),
    ),

    # --- the caveat ----------------------------------------------------------
    Mutant(
        id="the-caveat-always-carries-the-clause",
        target="lib/entry_shape.py",
        old="    if instance is None:\n        return STORE_IS_PER_HOST",
        new="    if False:\n        return STORE_IS_PER_HOST",
        why="the single-instance sentence is byte-mirrored into 25 tracked files; extending it "
            "unconditionally warns every host about a second instance that does not exist there.",
        kills="test_the_single_instance_sentence_is_UNCHANGED",
    ),
    Mutant(
        id="the-caveat-never-carries-the-clause",
        target="lib/entry_shape.py",
        old="    return f\"{STORE_IS_PER_HOST}, {STORE_IS_ONE_INSTANCE.format(instance=instance)}\"",
        new="    return STORE_IS_PER_HOST",
        why="the clause is the whole point of the change: without it a multi-instance absence "
            "still reads as an absence from the fleet.",
        kills="test_the_multi_instance_sentence_EXTENDS_rather_than_replaces",
    ),

    # --- the Go port ---------------------------------------------------------
    Mutant(
        id="go-multi-instance-counts-the-table",
        target="internal/client/instances.go",
        old="func (r Routing) MultiInstance() bool { return len(r.Instances) > 1 }",
        new="func (r Routing) MultiInstance() bool {\n\treturn len(r.Instances) > 1 || "
            "r.Routes != nil\n}",
        why="the shipped Python defect, ported. The Go client would label a one-instance host "
            "that has merely written a table.",
        kills="TestMultiInstanceAsksTheCountAndNotTheTable",
        go_package="./internal/client/",
    ),
    Mutant(
        id="go-row3-refusal-deleted",
        target="internal/client/instances.go",
        old="\t\tif !containsString(r.Aliases(), alias) {",
        new="\t\tif false {",
        why="the silent misroute in the port: an unconfigured alias resolves to the sole "
            "instance, and `writeInstance` then sends a WRITE there.",
        kills="TestTheThreeRowsThatShareOneConfiguration",
        go_package="./internal/client/",
    ),
    Mutant(
        # ⚠ TWO MUTANTS OVER `RefuseUnportedMultiInstance` USED TO LIVE HERE AND WERE DELETED
        # WITH IT, NOT LEFT TO ANCHOR ON NOTHING. An anchor that occurs zero times makes this
        # harness REFUSE, which is the harness working — but a mutant nobody replaced is
        # coverage silently subtracted, so the four below take over the region: the read verbs
        # now ROUTE where the guard used to refuse.
        id="go-read-label-is-unconditional",
        target="internal/client/routes.go",
        old="\tif !routing.MultiInstance() {\n\t\treturn \"\"\n\t}\n\treturn alias",
        new="\treturn alias",
        why="every single-instance host labelled — the compatibility guarantee inverted, on the "
            "Go side this time. Every banner gains `cairn[personal]`, every caveat gains the "
            "multi-instance clause, and `ls-entries` gains a `[personal] ` prefix on every line.",
        kills="TestAOneInstanceHostIsUNLABELLEDOnEveryReadVerb",
        go_package="./internal/client/",
    ),
    Mutant(
        id="go-read-label-is-never-set",
        target="internal/client/routes.go",
        old="\tif !routing.MultiInstance() {\n\t\treturn \"\"\n\t}\n\treturn alias",
        new="\treturn \"\"",
        why="the other direction: a two-instance host reads the right store and never says "
            "which, so an absence still reads as an absence from the fleet.",
        kills="TestRecallROUTESItsScopeAndCARRIESTheCaveatsInstanceClause",
        go_package="./internal/client/",
    ),
    Mutant(
        id="go-a-read-does-not-route-its-scope",
        target="internal/client/routes.go",
        old="\talias, err = routing.AliasFor(scope)",
        new="\talias = DefaultAlias",
        why="THE DEFECT THE DELETED GUARD EXISTED TO PREVENT, now reachable because the reads "
            "route: a routed scope is read out of the DEFAULT instance, so a scope that lives "
            "only on the second store answers `scope-absent` — a confident nothing out of a "
            "store nobody chose.",
        kills="TestRecallROUTESItsScopeAndCARRIESTheCaveatsInstanceClause",
        go_package="./internal/client/",
    ),
    Mutant(
        id="go-a-fan-out-reads-ONE-instance",
        target="internal/client/verbs.go",
        old="\t\tmatches, _ := filepath.Glob(filepath.Join(cache, \"*\", \"*.md\"))",
        new="\t\tmatches, _ := filepath.Glob(filepath.Join(opts.Cache, \"*\", \"*.md\"))",
        why="`ls-entries` walks every instance and lists the DEFAULT one's cache N times — the "
            "entries that exist only on the second store vanish, under a banner that names it.",
        kills="TestLsEntriesWALKSEveryInstanceAndPrefixesTheLINE",
        go_package="./internal/client/",
    ),
    Mutant(
        # ⚠ THE ANCHOR CARRIES ITS SECOND LINE, AND THAT IS NOT DECORATION. The bare
        # `rep, searchErr := report.Search(cache, report.SearchOptions{` occurs TWICE in
        # `verbs.go` — once in `Report`'s scoped arm and once here — so it would make this
        # harness REFUSE. `Scope: ""` is what only the fan-out spells, because only the fan-out
        # names no scope.
        id="go-a-search-fan-out-reads-ONE-instance",
        target="internal/client/verbs.go",
        old='\t\trep, searchErr := report.Search(cache, report.SearchOptions{\n'
            '\t\t\tScope:     "",',
        new='\t\trep, searchErr := report.Search(opts.Cache, report.SearchOptions{\n'
            '\t\t\tScope:     "",',
        why="`search --all-scopes` walks every instance and searches the DEFAULT one's cache N "
            "times: every section prints the right `cairn[<alias>]` banner over the WRONG "
            "store's hits, so an entry that exists only on the second instance is reported as "
            "absent from a fleet-wide search. 🔴 THE SITE HAD MEASURED-ZERO COVERAGE UNTIL THE "
            "KILLING TEST EXISTED — `if true { return ExitOK, nil }` at the top of "
            "`searchEveryInstance` compiled and left `./internal/client` at 51 PASS / 0 FAIL.",
        kills="TestAllScopesFANSOUTToEveryInstanceAndLABELSEachSection",
        go_package="./internal/client/",
    ),
    Mutant(
        id="go-caveat-always-carries-the-clause",
        target="internal/hostid/hostid.go",
        old="\tif instance == \"\" {\n\t\treturn StoreIsPerHost\n\t}",
        new="\tif false {\n\t\treturn StoreIsPerHost\n\t}",
        why="the Go twin of `the-caveat-always-carries-the-clause`. The single-instance sentence "
            "is byte-mirrored into the reader fixture and the parity gate; extending it "
            "unconditionally warns every host — and every POD response — about a second "
            "instance that does not exist there.",
        kills="TestTheCaveatGainsTheInstanceClauseONLYWhenAnAliasIsNamed",
        go_package="./internal/hostid/",
    ),
    Mutant(
        id="go-caveat-never-carries-the-clause",
        target="internal/hostid/hostid.go",
        # ⚠ THE GUARD IS INVERTED RATHER THAN THE RETURN DELETED, AND THE FIRST CUT GOT THIS
        # WRONG. Deleting `return StoreIsPerHost + ", " + fmt.Sprintf(...)` leaves `fmt`
        # imported and unused, so the package does not COMPILE — the mutant was scored
        # KILLED-BY-THE-WRONG-TEST with an empty failure list, which is the harness catching a
        # mutant that died of a build error rather than of the guard it was aimed at.
        old="\tif instance == \"\" {\n\t\treturn StoreIsPerHost\n\t}",
        new="\tif true {\n\t\treturn StoreIsPerHost\n\t}",
        why="the clause is the whole point of the change: without it a multi-instance absence "
            "still reads as an absence from the fleet.",
        kills="TestTheCaveatGainsTheInstanceClauseONLYWhenAnAliasIsNamed",
        go_package="./internal/hostid/",
    ),
    Mutant(
        id="go-check-direction1-does-not-ask-the-resolver",
        target="internal/client/instances.go",
        old="\t\tif _, err := r.AliasFor(scope); err != nil {\n\t\t\tproblems = append(problems, "
            "fmt.Sprintf(\n\t\t\t\t\"scope `%s` exists but the routing table does not name it",
        new="\t\tif _, err := r.AliasFor(scope); err == nil || err != nil {\n\t\t\tproblems = "
            "append(problems, fmt.Sprintf(\n\t\t\t\t\"scope `%s` exists but the routing table "
            "does not name it",
        why="finding 3 in the port: the grader predicts a refusal it has not asked about.",
        kills="TestCheckAsksTheResolverRatherThanSubtractingKeySets",
        go_package="./internal/client/",
    ),

    # --- finding 4: direction two is a NOTE, not a verdict -------------------
    Mutant(
        id="py-direction2-is-a-verdict-again",
        target="lib/cairn_instances.py",
        old="            notes.append(",
        new="            problems.append(",
        why="the SHIPPED DEFECT restored: a table entry for a scope that holds no entries is "
            "graded at exit 11, which fails every table that pre-registers a scope before its "
            "first write — and the remedy its wording implies (delete the line) makes the next "
            "write to that scope REFUSE.",
        kills="test_an_entry_naming_NO_scope_is_a_NOTE_and_not_a_problem",
    ),
    Mutant(
        id="py-routes-check-fails-on-a-note",
        target="cairn",
        old="    return EXIT_UNROUTED if problems else EXIT_OK",
        new="    return EXIT_UNROUTED if problems or notes else EXIT_OK",
        why="the same defect one layer up, and the one a demotion inside `check` alone would "
            "not close: the CLI re-promotes the note by gating its exit code on it.",
        kills="test_the_CLI_REPORTS_a_stale_entry_without_FAILING_on_it",
    ),

    # --- finding 6: an editor lock file is not an instance file --------------
    Mutant(
        id="py-editor-lock-file-refuses-again",
        target="lib/cairn_instances.py",
        old='        if path.name.startswith(".#"):',
        new="        if False:",
        why="Emacs' `.#secondary.env` reaches the hard alias refusal again, so EVERY verb on "
            "that host exits 11 while one buffer is open.",
        kills="test_an_EDITOR_LOCK_FILE_does_not_take_every_verb_to_exit_11",
    ),
    Mutant(
        id="py-dotfile-skip-widened-to-EVERY-dotfile",
        target="lib/cairn_instances.py",
        old='        if path.name.startswith(".#"):',
        new='        if path.name.startswith("."):',
        why="THE SHIPPED DEFECT, restored exactly, and it is the OPPOSITE DIRECTION from the row "
            "above: the skip swallows `instances/.env` — the dotted name an operator is most "
            "likely to write there — so a complete, valid instance config is ignored at exit 0 "
            "with no message anywhere. Both rows are needed because each one alone is satisfied "
            "by a predicate that is wrong in the other direction.",
        kills="test_a_DOTTED_file_that_is_not_an_editor_lock_is_STILL_an_ERROR",
    ),

    # --- findings 1 and 2, in the Go port ------------------------------------
    Mutant(
        id="go-resolve-state-ignores-its-instance",
        target="internal/client/state.go",
        old="\tcfg, err := LoadConfigFor(aliasOrDefault(instance))",
        new="\tcfg, err := LoadConfigFor(DefaultAlias)",
        why="THE SHIPPED DEFECT, at the site that caused it: `ResolveState` takes a cache "
            "directory and loads the DEFAULT instance's credentials, so every instance walk "
            "fetches `personal` N times and unpacks it over the others' caches.",
        kills="TestRoutesCheckReadsEACHInstanceFromITSOWNConfig",
        go_package="./internal/client/",
    ),
    Mutant(
        id="go-routes-check-passes-the-default-alias",
        target="internal/client/routes.go",
        old='ResolveState(cache, opts.NoSync, "", opts.Timeout, instance.Alias)',
        new='ResolveState(cache, opts.NoSync, "", opts.Timeout, DefaultAlias)',
        why="the same defect at the CALLER rather than the callee — threading a parameter is "
            "worth nothing if the one caller that walks instances passes a constant.",
        kills="TestRoutesCheckReadsEACHInstanceFromITSOWNConfig",
        go_package="./internal/client/",
    ),
    Mutant(
        id="go-put-derives-from-the-default-instance",
        target="internal/client/verbs.go",
        old='ResolveState(cache, false, "", opts.Timeout, alias)',
        new='ResolveState(cache, false, "", opts.Timeout, DefaultAlias)',
        why="finding 2's dangerous arm: the ref exists on BOTH stores, so the precondition is "
            "computed from the DEFAULT instance's bytes, sent to the ROUTED one, and announced "
            "as `derived If-Match … from the live snapshot`.",
        kills="TestAPutDerivesItsPreconditionFromTheROUTEDStore",
        go_package="./internal/client/",
    ),
    Mutant(
        id="go-put-uses-the-default-cache",
        target="internal/client/verbs.go",
        old="\talias, cache, err := writeRoute(opts, scope)",
        new="\talias, _, err := writeRoute(opts, scope)\n\tcache := opts.Cache",
        why="finding 2's other half: the routed instance's snapshot is unpacked into the "
            "DEFAULT instance's cache root, so the two stores interleave and `.sync-stamp` "
            "dates whichever synced last.",
        kills="TestAPutDerivesItsPreconditionFromTheROUTEDStore",
        go_package="./internal/client/",
    ),
    Mutant(
        id="go-put-loads-its-credentials-EAGERLY",
        target="internal/client/verbs.go",
        old="\talias, cache, err := writeRoute(opts, scope)",
        new="\talias, _, cache, err := writeInstance(opts, scope)",
        why="THE SHIPPED DIVERGENCE, restored at its site: `put` loads the routed credentials "
            "before `ResolveState` instead of after, so an incomplete routed config escapes to "
            "`cli.go` and is refused in ITS sentence rather than in `put`'s. ⚠ The exit code is "
            "7 either way — the mutant is invisible to every exit-code comparison, which is why "
            "the killing test asserts `runErr == nil` and the sentence, not the code alone.",
        kills="TestAPutLoadsTheROUTEDCredentialsLAZILY",
        go_package="./internal/client/",
    ),

    # --- findings 4, 6 and 7, in the Go port ---------------------------------
    Mutant(
        id="go-direction2-is-a-verdict-again",
        target="internal/client/instances.go",
        old="\t\tnotes = append(notes, fmt.Sprintf(",
        new="\t\tproblems = append(problems, fmt.Sprintf(",
        why="the Python defect, ported: an exists-but-empty scope grades at exit 11.",
        kills="TestAScopeThatEXISTSBUTIsEMPTYIsANoteAndNOTAVerdict",
        go_package="./internal/client/",
    ),
    Mutant(
        id="go-editor-lock-file-refuses-again",
        target="internal/client/instances.go",
        old='\t\tif strings.HasPrefix(entry.Name(), ".#") {',
        new="\t\tif false {",
        why="the Go client refuses every verb at 11 while an editor lock file sits beside an "
            "instance config.",
        kills="TestAnEDITORLockFileDoesNotTakeEveryVerbToExit11",
        go_package="./internal/client/",
    ),
    Mutant(
        id="go-dotfile-skip-widened-to-EVERY-dotfile",
        target="internal/client/instances.go",
        old='\t\tif strings.HasPrefix(entry.Name(), ".#") {',
        new='\t\tif strings.HasPrefix(entry.Name(), ".") {',
        why="the shipped defect in the port, and the OPPOSITE DIRECTION from the row above: the "
            "skip swallows `instances/.env`, so a complete, valid instance config is ignored at "
            "exit 0 with no message. Both clients carry the same narrowed predicate and both "
            "need both directions pinned, or one of them drifts.",
        kills="TestADottedFileThatIsNotAnEditorLockIsStillAnError",
        go_package="./internal/client/",
    ),
    Mutant(
        id="go-direction1-walks-the-slice",
        target="internal/client/instances.go",
        old="\tfor s := range inScopes {",
        new="\tfor _, s := range scopes {",
        why="finding 7: the oracle subtracts SETS, so a repeated scope is one finding there and "
            "as many as the slice holds here. ⚠ An ALIGNMENT — `Routes` deduplicates upstream, "
            "so no CLI input reaches it today.",
        kills="TestDirectionOneCountsAScopeONCEHoweverOftenTheCallerNamesIt",
        go_package="./internal/client/",
    ),
]


def build_tree(work: Path, index: int) -> Path:
    """A COPY of the working tree, without its `.git`.

    🔴 A COPY OF A WORKTREE SHARES ITS GIT DIR UNLESS THE `.git` LINK IS DROPPED — it is a FILE
    holding `gitdir: …`, not a directory — so a stray `git` command inside the copy would act on
    the REAL branch. `ignore_patterns` drops it and the assertion below is what proves it did.
    """
    target = work / f"mutant-{index:02d}"
    shutil.copytree(
        ROOT, target,
        ignore=shutil.ignore_patterns(".git", "__pycache__", ".pytest_cache", "*.pyc"))
    assert not (target / ".git").exists(), "the mutant tree must not carry a .git link"
    return target


def sweep_pycache(tree: Path) -> None:
    for cached in tree.rglob("__pycache__"):
        shutil.rmtree(cached, ignore_errors=True)


def apply_mutation(tree: Path, mutant: Mutant) -> None:
    path = tree / mutant.target
    text = path.read_text(encoding="utf-8")
    occurrences = text.count(mutant.old)
    if occurrences != 1:
        raise SystemExit(
            f"REFUSING: mutant `{mutant.id}` anchors on a string that occurs {occurrences} "
            f"times in {mutant.target}, not once. A substitution that matched nothing scores "
            f"SURVIVED and reads as an untested guard.")
    path.write_text(text.replace(mutant.old, mutant.new), encoding="utf-8")


def failing_tests(output: str, is_go: bool) -> list[str]:
    if is_go:
        return sorted(set(re.findall(r"^--- FAIL: (\S+)", output, re.MULTILINE)))
    # 🔴 READ THE `FAILED <file>::<test>` SUMMARY LINES, NOT AN EXIT CODE. A run that failed to
    # COLLECT reports `no tests ran` and exits non-zero, which a status check would score as a
    # kill — the mutant would then be credited with a death it never caused.
    return sorted(set(re.findall(r"^FAILED \S+::(?:\S+::)?(\w+)", output, re.MULTILINE)))


def run_suites(tree: Path, mutant: Mutant) -> tuple[int, str, int]:
    """`(returncode, output, collected)` for one mutant's suites."""
    env = dict(os.environ)
    env["PYTHONDONTWRITEBYTECODE"] = "1"
    if mutant.go_package:
        proc = subprocess.run(["go", "test", mutant.go_package], cwd=str(tree), env=env,
                              capture_output=True, text=True, timeout=1800)
        return proc.returncode, proc.stdout + proc.stderr, -1
    sweep_pycache(tree)
    proc = subprocess.run(
        [sys.executable, "-m", "pytest", *mutant.suites, "-q", "-p", "no:randomly"],
        cwd=str(tree), env=env, capture_output=True, text=True, timeout=1800)
    output = proc.stdout + proc.stderr
    collected = 0
    match = re.search(r"(\d+) (?:passed|failed)", output)
    if match:
        collected = int(match.group(1))
    return proc.returncode, output, collected


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="tests/routing_mutants.py")
    parser.add_argument("--only", default=None, help="a comma-separated subset of mutant ids")
    parser.add_argument("--list", action="store_true", help="print the ids and stop")
    args = parser.parse_args(argv)

    if args.list:
        for mutant in MUTANTS:
            print(f"{mutant.id}\t{mutant.target}")
        return 0

    wanted = None if args.only is None else set(args.only.split(","))
    selected = [m for m in MUTANTS if wanted is None or m.id in wanted]
    if not selected:
        print(f"REFUSING: --only {args.only!r} selected no mutant", file=sys.stderr)
        return 2

    work = Path(tempfile.mkdtemp(prefix="cairn-mutants-"))
    killed, survived, wrong_reason = [], [], []
    control_died = False
    try:
        for index, mutant in enumerate(selected):
            tree = build_tree(work, index)
            apply_mutation(tree, mutant)
            rc, output, collected = run_suites(tree, mutant)
            failures = failing_tests(output, bool(mutant.go_package))
            if rc == 0:
                survived.append(mutant.id)
                print(f"SURVIVED {mutant.id} — nothing failed. {mutant.why}")
            elif mutant.kills and mutant.kills not in failures:
                wrong_reason.append(mutant.id)
                print(f"KILLED-BY-THE-WRONG-TEST {mutant.id} — expected {mutant.kills}, "
                      f"got {failures}")
            else:
                killed.append(mutant.id)
                print(f"KILLED {mutant.id} by {failures[:4]}"
                      f"{' …' if len(failures) > 4 else ''}")
                if mutant.id == "positive-control":
                    control_died = True
            shutil.rmtree(tree, ignore_errors=True)

        print(f"SUMMARY mutants={len(selected)} killed={len(killed)} "
              f"survived={len(survived)} killed-by-the-wrong-test={len(wrong_reason)}")
        if survived:
            print("surviving: " + ", ".join(survived))
        if wrong_reason:
            print("wrong reason: " + ", ".join(wrong_reason))
        control_selected = any(m.id == "positive-control" for m in selected)
        if control_selected and not control_died:
            print("REFUSING TO VOUCH: the positive control SURVIVED, so this runner did not "
                  "execute the tree it edited and every verdict above is a fact about the "
                  "harness.", file=sys.stderr)
            return 2
        return 1 if (survived or wrong_reason) else 0
    finally:
        shutil.rmtree(work, ignore_errors=True)


if __name__ == "__main__":
    raise SystemExit(main())
