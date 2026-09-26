package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ZacxDev/cairn/internal/ui"
)

// Exit codes. They are deliberately few, and NONE of them is a gating verdict.
//
// 🔴 A CAPTURED REGRESSION EXITS 0 IN THIS CHANGE. The reason is mechanical rather than
// timid: the first diff against a nonexistent baseline flags every axe rule "new" exactly
// once, so a gate promoted today is red for a reason that has nothing to do with the tree —
// which is the permanently-red gate this repository already refuses, reached from a new
// direction. `README.md` names the ONE predicate that becomes a promotion candidate after
// two baseline runs and what stays advisory forever.
const (
	exitOK = 0
	// exitHarness is the harness itself failing: the pod would not boot, the browser would
	// not start, sign-in did not take, a capture errored. This is a broken instrument, and
	// an instrument that cannot say anything must not report a pass.
	exitHarness = 1
	// exitUsage is a bad invocation.
	exitUsage = 2
	// exitSkipped is the fork-PR case: no push credentials at all. It is a distinct code so
	// a CI step can tell "nothing to push to" from "the push failed", and it is mapped to
	// success by the workflow rather than by this program pretending it ran.
	exitSkipped = 3
)

// pushConfirmation is the line the CI `verify-push` control greps for.
//
// 🔴 THE PUSH PATH IS NON-FATAL BY DESIGN, SO WITHOUT THIS LINE A MISCONFIGURED TOKEN IS A
// SILENT GREEN. That is the whole reason the string is a named constant and not an inline
// `fmt.Printf`: the control in the workflow and the thing it greps for have to be one
// spelling, and a second copy in a YAML file is how a control starts matching nothing.
// Grepping for a string that CAN be absent is only a control once something has been seen
// to produce it — the workflow's own step comment says which run did.
const pushConfirmation = "uiaudit: PUSH CONFIRMED run_id="

func main() {
	repoRoot := flag.String("repo-root", "..", "the cairn checkout whose tests/ and binaries are used")
	uiBinary := flag.String("cairn-ui", "", "path to a built cairn-ui (required)")
	workDir := flag.String("work", "", "scratch directory for the fixture world and the captures (required)")
	port := flag.Int("port", 18771, "loopback port for cairn-ui")
	label := flag.String("label", "cairn-ui", "the run label the hub shows")
	budget := flag.Duration("budget", 8*time.Minute, "wall-clock budget for the whole walk")
	flag.Parse()

	if *uiBinary == "" || *workDir == "" {
		fmt.Fprintln(os.Stderr, "uiaudit: -cairn-ui and -work are required")
		flag.Usage()
		os.Exit(exitUsage)
	}

	if err := run(*repoRoot, *uiBinary, *workDir, *port, *label, *budget); err != nil {
		if code, ok := err.(exitCode); ok {
			os.Exit(int(code))
		}
		fmt.Fprintf(os.Stderr, "uiaudit: %v\n", err)
		os.Exit(exitHarness)
	}
}

type exitCode int

func (e exitCode) Error() string { return fmt.Sprintf("exit %d", int(e)) }

func run(repoRoot, uiBinary, workDir string, port int, label string, budget time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()

	abs, err := filepath.Abs(repoRoot)
	if err != nil {
		return err
	}

	world, err := BootWorld(ctx, abs, uiBinary, filepath.Join(workDir, "world"), port)
	if err != nil {
		return err
	}
	defer world.Stop()
	fmt.Printf("uiaudit: pod up at %s over %d scope(s): %s\n",
		world.BaseURL, len(world.Scopes), strings.Join(world.Scopes, ", "))

	// The walk, derived from the ledger.
	ledger := ui.DeclaredRouteLedger()
	targets, skipped, err := Targets(ledger)
	if err != nil {
		return err
	}
	if err := LedgerAccounting(ledger, targets, skipped); err != nil {
		return err
	}
	fmt.Printf("uiaudit: ledger has %d row(s); %d target(s) derived, %d row(s) skipped\n",
		len(ledger), len(targets), len(skipped))
	for _, s := range skipped {
		fmt.Printf("uiaudit:   skip %s\n", s)
	}

	browser, err := NewBrowser(ctx, world.BaseURL, budget)
	if err != nil {
		return err
	}
	defer browser.Close()
	version, err := browser.Version()
	if err != nil {
		return err
	}
	fmt.Printf("uiaudit: %s\n", version)

	// 🔴 SIGN IN FIRST AND REPORT THE COOKIE'S FLAGS, BECAUSE THAT IS SIGNAL ONE AND IT IS
	// A SIGNAL `/healthz` STRUCTURALLY CANNOT GIVE. A readiness probe answers before the
	// authentication chain runs, so it says `ok` on a pod whose session file is unwritable
	// and whose every login therefore fails — the deployment `internal/ui/README.md`
	// describes. This click is what separates those two worlds, and a `__Host-` cookie that
	// a browser stored and re-sent over plaintext loopback is what closes the claim
	// `internal/identity/session.go` flags as unmeasured.
	cookie, err := browser.SignIn(fixtureToken)
	if err != nil {
		return fmt.Errorf("%w\n--- cairn-ui log ---\n%s", err, world.Log())
	}
	fmt.Printf("uiaudit: SESSION COOKIE HONOURED BY THE BROWSER: name=%s secure=%v httpOnly=%v sameSite=%s path=%s domain=%s\n",
		cookie.Name, cookie.Secure, cookie.HTTPOnly, cookie.SameSite, cookie.Path, cookie.Domain)

	// Captures: signed-in rows first while the session is live, then the public rows after
	// signing out. Two passes rather than one so the state is never a function of order.
	var captures []*Capture
	for _, signedIn := range []bool{true, false} {
		if !signedIn {
			var wantPublic bool
			for _, t := range targets {
				if !t.SignedIn {
					wantPublic = true
				}
			}
			if !wantPublic {
				continue
			}
			if err := browser.SignOut(); err != nil {
				return fmt.Errorf("reaching the signed-out state: %w", err)
			}
			fmt.Println("uiaudit: signed out (by clicking the control, so the server revoked the session)")
		}
		// 🔴 A QUEUE, NOT A LOOP OVER A FIXED SLICE, BECAUSE A PAGE MAY PUBLISH FURTHER
		// TARGETS. The share index's per-scope links are `control.ID`s nobody can guess, so
		// the only honest way to reach them is to read what the surface itself offered — see
		// [ExpandLinks] for the measured reason a guess was worse than useless here.
		//
		// 🔴 AND IT DEDUPES ON `Path`, WHICH BECAME AN OBLIGATION RATHER THAN A TIDINESS
		// WHEN A DISCOVERED PAGE STARTED PUBLISHING LINKS OF ITS OWN. The browse pages link
		// across rows and back: `/` → `/scope?id=X` → `/entry?…` → a breadcrumb to
		// `/scope?id=X`. Without this set that is an unbounded queue, and the symptom is a
		// walk that never returns rather than one that reports a defect. `/scope?id=X` is
		// also published by TWO pages — the root's cards and the parameterless `/scope`
		// navigation list — so even without a cycle it would be captured twice, at five
		// widths each, and pushed as ten pages the hub would diff against themselves.
		queue := make([]Target, 0, len(targets))
		enqueued := map[string]bool{}
		for _, t := range targets {
			if t.SignedIn == signedIn && !enqueued[t.Path] {
				enqueued[t.Path] = true
				queue = append(queue, t)
			}
		}
		for len(queue) > 0 {
			t := queue[0]
			queue = queue[1:]
			for _, vp := range Viewports {
				c, err := browser.CaptureTarget(t, vp)
				if err != nil {
					return fmt.Errorf("%w\n--- cairn-ui log ---\n%s", err, world.Log())
				}
				captures = append(captures, c)
				fmt.Printf("uiaudit: captured %-38s %-9s %d axe=%d layout(tap<44=%d text<12=%d overflow=%v no-viewport-meta=%v) scripts=%d console=%d net=%d digest=%v\n",
					t.Path, vp.Name, c.DocStatus, len(c.Violations),
					c.Layout.SmallTapTargets, c.Layout.SmallText, c.Layout.HorizontalOverflow,
					c.Layout.MissingViewportMeta, c.ScriptCount,
					len(c.Console), len(c.Network), c.HasDigest())

				// Expansion is read from ONE viewport's render, not all five: the hrefs a
				// page publishes are an authority answer and cannot depend on a width, and
				// enqueueing them once per width would capture every discovered page five
				// times. The dedupe above would absorb that, which is exactly why this
				// narrowing is kept explicit rather than left to it — two mechanisms doing
				// one job is how the surviving one stops being read.
				if t.ExpandLinks && vp == Viewports[0] {
					found, declined, bounded := ExpandLinks(t, c.Hrefs, ledger)
					for _, d := range declined {
						fmt.Printf("uiaudit:   declined href %q from %s (not a relative link, carries no query, or names no declared GET row)\n", d, t.Path)
					}
					if bounded > 0 {
						// 🔴 PRINTED, AND NOT AS A DECLINE. These are pages the walk WOULD
						// have visited; the hub's 200-page cap is what stops it, and the
						// arithmetic is `targets × pushed viewports`. Silently taking the
						// first four would make a bounded walk read as a complete one, which
						// is the under-coverage this whole derivation exists against.
						fmt.Printf("uiaudit:   %s published %d further target(s); BOUNDED to the first %d by sorted path "+
							"(MaxExpansionsPerPage — these pages are one template, and the hub's page cap is %d)\n",
							t.Path, bounded+len(found), MaxExpansionsPerPage, MaxPages)
					}
					if len(found) == 0 {
						// ⚠ NOT AN ERROR, AND SAYING WHY IS THE POINT. On the token-file
						// deployment the share index renders "No scope is administrable by
						// this credential" — an authority answer, not an empty store — so
						// zero links is the correct output there. `README.md` declares
						// reaching the per-scope SHARE page as a gap needing a
						// journal-backed world. The BROWSE pages are not in that position:
						// a token-file row is unrestricted over the store's scopes, so
						// `GET /` does publish scope links on this deployment.
						fmt.Printf("uiaudit:   %s published no expandable links\n", t.Path)
					}
					for _, f := range found {
						if enqueued[f.Path] {
							fmt.Printf("uiaudit:   already queued %s (published again by %s)\n", f.Path, t.Path)
							continue
						}
						enqueued[f.Path] = true
						queue = append(queue, f)
						fmt.Printf("uiaudit:   expanded %s -> %s\n", t.Path, f.Path)
					}
				}
			}
		}
	}
	if len(captures) == 0 {
		return fmt.Errorf("the walk captured nothing: %d target(s) derived from %d ledger row(s)", len(targets), len(ledger))
	}

	printSignalSummary(captures, browser.FaviconRefusals())

	if err := refuseWalkRegressions(captures); err != nil {
		return err
	}

	payload, files, err := BuildPayload(label, captures)
	if err != nil {
		return err
	}
	if err := payload.Validate(files); err != nil {
		return fmt.Errorf("the payload this walk built is malformed: %w", err)
	}
	total := 0
	for _, b := range files {
		total += len(b)
	}
	fmt.Printf("uiaudit: payload shape OK — %d page(s), %d part(s), %d byte(s) (body cap %d, per-file cap %d)\n",
		len(payload.Pages), len(files), total, MaxBodyBytes, MaxFileBytes)

	// 🔴 CAPTURES ARE NEVER WRITTEN INTO THE REPOSITORY. This repository is public and
	// `tests/leakscan.py` classifies a file with a NUL byte in its first 8000 as binary and
	// SKIPS IT BY NAME — so a committed PNG carrying a real name would pass the gate
	// untouched. They go to the work directory, which CI publishes as an artifact, and to
	// the hub, which is where a pixel baseline belongs anyway.
	artifacts := filepath.Join(workDir, "artifacts")
	if err := os.MkdirAll(artifacts, 0o700); err != nil {
		return err
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(artifacts, name), body, 0o600); err != nil {
			return err
		}
	}
	metaPath := filepath.Join(artifacts, "metadata.json")
	if err := writeJSON(metaPath, payload); err != nil {
		return err
	}
	fmt.Printf("uiaudit: artifacts in %s (never committed: leakscan skips binaries by name)\n", artifacts)

	cfg := PushConfig{
		PushURL:   os.Getenv("CAIRN_AUDIT_PUSH_URL"),
		PushToken: os.Getenv("CAIRN_AUDIT_PUSH_TOKEN"),
		APIURL:    os.Getenv("CAIRN_AUDIT_API_URL"),
		APIToken:  os.Getenv("CAIRN_AUDIT_API_TOKEN"),
	}
	switch {
	// ⚠ DERIVED FROM `Missing()` RATHER THAN THE LITERAL 4. A hardcoded count means a fifth secret
	// turns every fork-PR skip into a hard failure — the branch below — because "all absent" would
	// never again be true. `AllMissing` asks the config how many it knows about.
	case cfg.AllMissing():
		// The fork-PR case: a fork gets no secrets, and failing there would train everyone
		// to ignore the job. Distinct exit code so the workflow maps it, rather than this
		// program claiming it pushed.
		fmt.Println("uiaudit: no audit-hub credentials in the environment — the capture ran, the push is SKIPPED (fork PRs get no secrets)")
		return exitCode(exitSkipped)
	case !cfg.Complete():
		// 🔴 SOME-BUT-NOT-ALL IS A MISCONFIGURATION AND IS LOUD. A half-configured push is
		// precisely the silent-green shape the `verify-push` control exists for, and it is
		// the one case where guessing would be worse than failing.
		return fmt.Errorf("the audit hub is half-configured: %s absent while the rest are set", strings.Join(cfg.Missing(), ", "))
	}

	res, err := Push(ctx, cfg, payload, files)
	if err != nil {
		// Non-fatal by design — a capture that ran is worth its artifacts even if the push
		// did not land. The `verify-push` control is what stops this from being a silent
		// green, by requiring the confirmation line below.
		fmt.Fprintf(os.Stderr, "uiaudit: push failed (non-fatal): %v\n", err)
		return nil
	}
	// 🔴 THE RUN ID ONLY — THE SERVICE-RETURNED URL IS NOT PRINTED, AND THAT IS A LEAK-SURFACE
	// DECISION RATHER THAN TIDINESS. `res.URL` is an absolute URL built by the hub, so it carries
	// the hostname this repository may not spell (`leakscan.py`'s `reachable-hostname` rule, and
	// the `denied-identifier` set). This line goes into a GitHub Actions log, which is public for
	// a public repository. Actions does mask secret VALUES, and both base URLs are secrets, so the
	// masking would probably catch it — but "probably, via a platform feature" is not the standard
	// the rest of this module holds, and the run id alone is everything a reader needs to address
	// the run through the read API. Do not add the URL back for convenience.
	fmt.Printf("%s%s\n", pushConfirmation, res.RunID)

	report, err := ReadRun(ctx, cfg, res.RunID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "uiaudit: reading the diff back failed (non-fatal): %v\n", err)
		return nil
	}
	block := DiffBlock(report)
	fmt.Print(block)
	if summary := os.Getenv("GITHUB_STEP_SUMMARY"); summary != "" {
		f, err := os.OpenFile(summary, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
		if err == nil {
			fmt.Fprintf(f, "## uiaudit\n\n```\n%s```\n", block)
			f.Close()
		}
	}
	return nil
}

// printSignalSummary reports every signal at the scope it was measured, and labels the
// zeros that are zero BY CONSTRUCTION as structural rather than as passes.
//
// 🔴 A REASSURING ZERO IS INDISTINGUISHABLE FROM AN INSTRUMENT WIRED TO NOTHING, SO EACH
// ZERO HERE SAYS WHICH KIND IT IS. `control_test.go` is the positive control that shows the
// same collectors CAN count; the README reports the pair.
//
// ⚠ AND THE STRUCTURAL CLAIM IS NARROWER THAN AN EARLIER DRAFT OF IT SAID, WHICH IS A
// CORRECTION RATHER THAN A CAVEAT. That draft printed "console=%d network=%d — STRUCTURAL
// ZERO" over both numbers, on the reasoning that a page with no scripts and no subresources
// cannot produce either. The first half holds: `internal/ui` ships an inline stylesheet, no
// script, and an XSS guard asserting `"<img"` can never render, so the console collector has
// nothing to observe. The second half was FALSE, and the walk that printed it had a non-zero
// network count on the same line — because a browser requests `/favicon.ico` on its own
// initiative and this surface has no such row, so it answers the dispatcher's refusal. That
// is counted separately (see [Browser.FaviconRefusals]) and the per-page network zero below
// is a claim about SUBRESOURCES THE PAGE ASKED FOR, which is a narrower sentence than the one
// that was wrong.
// refuseWalkRegressions turns three per-page measurements into a FAILED WALK.
//
// 🔴 A NUMBER IN A SUMMARY LINE IS NOT A GATE, AND THIS FILE ALREADY RECORDS WHAT THAT
// COSTS. `printSignalSummary` has printed `pages with horizontal overflow=N` since this
// harness existed; nothing read it, so a responsive regression would have been a digit in a
// log beside an exit 0. The three below are the properties this surface is supposed to
// have, so each is a refusal:
//
//   - NO HORIZONTAL OVERFLOW AT ANY CAPTURED WIDTH. This is the single layout defect no
//     unit test in this repository can see, and the one content is most likely to cause: a
//     long unbroken line in a store entry's body makes the whole PAGE scroll sideways, on a
//     phone above all. The widths are the whole point — a page can be clean at 390 and 1440
//     and broken at 834, which is why there are five.
//   - NO SCRIPT. `internal/ui` ships none, and part of its XSS story rests on that; the
//     console-zero claim in the summary above is explicitly structural FOR THAT REASON, so
//     the day a script appears both that claim and the guard behind it go quiet at once.
//   - AXE ACTUALLY RAN. `Violations: 0` is produced identically by a clean page and by an
//     injection that never executed, and this whole program's a11y half is inert in the
//     second case. A decodable `testEngine` is what separates them.
//
// ⚠ IT IS NOT A THRESHOLD ON `SmallTapTargets` OR `SmallText`. Those two are the hub's to
// turn into findings — `vendor-js/VENDOR.md` records that the server is the single source
// of those thresholds and that a harness computing one here would be a second, invisible
// copy. The three above are not thresholds: each is a binary property with no number in it.
func refuseWalkRegressions(captures []*Capture) error {
	if len(captures) == 0 {
		return fmt.Errorf("refuseWalkRegressions was handed NO capture, so its clean verdict would be about nothing")
	}
	var overflow, scripted, axeless []string
	widths := map[string]bool{}
	for _, c := range captures {
		where := fmt.Sprintf("%s at %s (%dpx)", c.Target.Path, c.Viewport.Name, c.Viewport.Width)
		widths[c.Viewport.Name] = true
		if c.Layout.HorizontalOverflow {
			overflow = append(overflow, fmt.Sprintf("%s: scrollWidth=%d > innerWidth=%d",
				where, c.Layout.ScrollWidth, c.Layout.InnerWidth))
		}
		if c.ScriptCount != 0 {
			scripted = append(scripted, fmt.Sprintf("%s: document.scripts.length=%d", where, c.ScriptCount))
		}
		// The same discriminator `control_test.go` uses: an axe result that decodes and
		// carries the engine block is an axe result that ran.
		if !bytes.Contains(c.AxeJSON, []byte(`"testEngine"`)) {
			axeless = append(axeless, where)
		}
	}
	// 🔴 THE WIDTH COUNT IS PART OF THE VERDICT, BECAUSE A CLEAN RUN OVER ONE WIDTH IS NOT
	// A CLEAN RUN. A matrix that silently collapsed — a `Viewports` edited to one entry, a
	// capture loop that broke out early — would produce zero overflow findings and read
	// exactly like a responsive surface.
	if len(widths) < len(Viewports) {
		return fmt.Errorf("the walk captured %d distinct viewport(s) (%d declared): the matrix collapsed, so "+
			"a zero overflow count below is a fact about one width rather than about the surface",
			len(widths), len(Viewports))
	}
	var refusals []string
	if len(overflow) > 0 {
		refusals = append(refusals, fmt.Sprintf("HORIZONTAL OVERFLOW on %d capture(s) — the page scrolls "+
			"sideways, which no unit test in this repository can see:\n    %s",
			len(overflow), strings.Join(overflow, "\n    ")))
	}
	if len(scripted) > 0 {
		refusals = append(refusals, fmt.Sprintf("SCRIPT ON THE PAGE on %d capture(s) — this surface ships "+
			"none, and the console-zero claim above is structural only while that holds:\n    %s",
			len(scripted), strings.Join(scripted, "\n    ")))
	}
	if len(axeless) > 0 {
		refusals = append(refusals, fmt.Sprintf("AXE DID NOT RUN on %d capture(s) — a zero violation count "+
			"from a page axe never inspected is indistinguishable from a clean one:\n    %s",
			len(axeless), strings.Join(axeless, "\n    ")))
	}
	if len(refusals) > 0 {
		return fmt.Errorf("the walk measured %d regression class(es) over %d capture(s):\n  %s",
			len(refusals), len(captures), strings.Join(refusals, "\n  "))
	}
	fmt.Printf("uiaudit:   REFUSALS: 0 horizontal overflow, 0 scripts, %d/%d captures carry a decodable axe "+
		"testEngine — over %d distinct width(s): %s\n",
		len(captures)-len(axeless), len(captures), len(widths), viewportWidths())
	return nil
}

// viewportWidths renders the matrix for a log line, so the run's own output says which
// widths its clean verdict covers rather than leaving a reader to look them up.
func viewportWidths() string {
	parts := make([]string, 0, len(Viewports))
	for _, vp := range Viewports {
		push := ""
		if vp.Push {
			push = " PUSHED"
		}
		parts = append(parts, fmt.Sprintf("%s=%d%s", vp.Name, vp.Width, push))
	}
	return strings.Join(parts, ", ")
}

func printSignalSummary(captures []*Capture, faviconRefusals int) {
	var axe, console, netw, tap, text, overflow, noViewport, digests int
	rules := map[string]int{}
	for _, c := range captures {
		axe += len(c.Violations)
		for _, v := range c.Violations {
			rules[v.ID]++
		}
		console += len(c.Console)
		netw += len(c.Network)
		tap += c.Layout.SmallTapTargets
		text += c.Layout.SmallText
		if c.Layout.HorizontalOverflow {
			overflow++
		}
		if c.Layout.MissingViewportMeta {
			noViewport++
		}
		if c.HasDigest() {
			digests++
		}
	}
	ids := make([]string, 0, len(rules))
	for id := range rules {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	fmt.Println("uiaudit: --- signals over the whole walk ---")
	fmt.Printf("uiaudit:   axe violations: %d across %d rule(s): %s\n", axe, len(ids), strings.Join(ids, ", "))
	fmt.Printf("uiaudit:   layout: tap targets under 44px=%d, text under 12px=%d, pages with horizontal overflow=%d, pages missing <meta viewport>=%d\n",
		tap, text, overflow, noViewport)
	fmt.Printf("uiaudit:   a11y digests attached: %d of %d page(s) — any shortfall is a page whose digest came back EMPTY, whose ref is therefore omitted (an empty digest is a 400 on the WHOLE push)\n",
		digests, len(captures))
	// 🔴 THE STRUCTURAL CLAIM IS DERIVED FROM THE LEDGER, BECAUSE A HARDCODED ONE WENT FALSE ON A
	// TREE THAT ALREADY EXISTS. The earlier wording said "this surface ships an inline stylesheet
	// and NO script … there are none" over BOTH numbers. The auth change moves the stylesheet to
	// its own route, so on that tree the page has a real blocking subresource and the sentence is
	// simply untrue — measured by running this walk against the merged tree, where it printed the
	// claim beside a page that had just fetched one. A claim about the surface has to read the
	// surface.
	// 🔴 GUARDED ON THE NUMBER, BECAUSE THE SENTENCE IS ONLY TRUE WHEN THE COUNT IS ZERO — AND THIS
	// IS THE THIRD RECURRENCE OF ONE CLASS IN THIS FUNCTION. The network sibling below was hardcoded,
	// went false when the stylesheet became a route, and was made ledger-derived; the `<meta viewport>`
	// claim was hardcoded and could be manufactured by a thrown script, and is now asserted in
	// `CaptureTarget`. This line was still printing "STRUCTURAL, NOT A PASS" unconditionally, so a
	// surface that grew a script would have had a non-zero count printed beside a sentence saying the
	// collector cannot count. A claim about a measurement has to read the measurement.
	if console == 0 {
		fmt.Printf("uiaudit:   console=0 — 🔴 STRUCTURAL, NOT A PASS: this surface ships NO script, and an existing XSS guard asserts \"<img\" can never render, so the console collector has nothing to observe here whatever the code does. control_test.go counts 2 on a page that does.\n")
	} else {
		fmt.Printf("uiaudit:   console=%d — NOT a structural zero: this surface has grown something that logs, so `doc.go`'s console claim is now false and wants correcting.\n", console)
	}
	// ⚠ THE ROW THAT DECIDES THIS IS THE HASHED ONE, NOT THE UNVERSIONED ONE. Both are in the
	// ledger; only the hashed path is what a page LINKS, so only its presence makes "every page
	// has a real blocking subresource" true. Reading the unversioned row here would keep
	// printing the strong claim on a tree where the pages had stopped fetching anything.
	if hasRow(ui.DeclaredRouteLedger(), "GET "+ui.StylesheetHashedPath) {
		fmt.Printf("uiaudit:   network=%d FAILED subresource request(s) — and this is NOT a structural zero: %s is a route on this tree and every page links it, so every page has a real blocking subresource. Zero here means it was FETCHED SUCCESSFULLY on every page, which is a stronger statement than the structural one it replaces.\n",
			netw, ui.StylesheetHashedPath)
	} else {
		fmt.Printf("uiaudit:   network=%d over subresources the PAGES asked for — same structural caveat: this ledger has no %s row, so there are none.\n",
			netw, ui.StylesheetHashedPath)
	}
	// ⚠ THE COUNT IS RUN-DEPENDENT, WHICH IS THE WHOLE REASON IT IS NOT ATTRIBUTED TO A PAGE.
	// Measured on this tree at two points: one walk recorded a `401 /favicon.ico` (attached,
	// arbitrarily, to whichever page happened to be loading when the browser asked); a later
	// walk over the same tree recorded none. Chromium decides when to ask. So the honest claim
	// is "this surface refuses the request when it arrives", NOT "every visit produces one" —
	// an earlier draft of this line said the latter and the next run contradicted it.
	fmt.Printf("uiaudit:   favicon refusals=%d — NOT a structural zero: no ledger row carries %s, so the dispatcher's uniform refusal answers it whenever chromium asks. WHETHER it asks is run-dependent (measured non-zero on one walk and zero on another over this same tree), which is exactly why it is counted here and kept out of the per-page totals: attributed to a page it would manufacture a P2 network delta that flaps forever.\n",
		faviconRefusals, FaviconPath)
}

// DiffBlock renders the deterministic diff for a job log and a step summary.
//
// It is deterministic in the sense that matters: no LLM output reaches it. The hub's
// persona evaluator and its vision notes are not read at all — blocker-key stability there
// is measured 0.22 and synthesis 0.00, so nothing derived from them could gate anything, and
// printing them beside the deterministic rows would invite exactly that.
func DiffBlock(r *RunReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "uiaudit: --- the hub's deterministic diff (run %s, status %s) ---\n", r.RunID, r.Status)
	fmt.Fprintf(&b, "uiaudit:   summary: pages=%d a11y=%d console=%d/%d network=%d/%d (first/third party)\n",
		r.Summary.PagesCrawled, r.Summary.A11yViolations,
		r.Summary.ConsoleFirst, r.Summary.ConsoleThird,
		r.Summary.NetworkFirst, r.Summary.NetworkThird)
	if r.Diff == nil {
		// ⚠ AN ABSENT DIFF AND AN EMPTY DIFF ARE DIFFERENT FACTS. The hub omits the key
		// when there is no previous done run, which is the day-one state — and the state in
		// which "0 new rules" would be a lie rather than good news.
		fmt.Fprintln(&b, "uiaudit:   NO DIFF: this run has no previous done run to compare against. Every rule will flag \"new\" exactly once on the run AFTER this one; that is the baseline transient, not a regression.")
		return b.String()
	}
	d := r.Diff
	fmt.Fprintf(&b, "uiaudit:   vs %s: pages +%d/-%d, %d changed, %d size-changed\n",
		d.PrevRunID, len(d.PagesAdded), len(d.PagesRemoved), d.PagesChanged, d.PagesSizeChanged)
	fmt.Fprintf(&b, "uiaudit:   new_a11y_rules (%d): %s\n", len(d.NewA11yRules), join(d.NewA11yRules))
	fmt.Fprintf(&b, "uiaudit:   resolved_a11y_rules (%d): %s\n", len(d.ResolvedA11yRules), join(d.ResolvedA11yRules))
	fmt.Fprintf(&b, "uiaudit:   deltas: a11y=%+d console=%+d network=%+d\n", d.A11yDelta, d.ConsoleDelta, d.NetworkDelta)
	for _, cp := range d.ChangedPages {
		note := ""
		if cp.SizeChanged {
			// A full-page height change is a LAYOUT change, not a ~100% pixel regression.
			// The hub gates its own regression count on `!SizeChanged` for that reason,
			// and a reader of this log needs the same caveat beside the number.
			note = " (size changed — read as a layout change, not a pixel regression)"
		}
		if cp.NotCompared {
			note = " (not compared)"
		}
		fmt.Fprintf(&b, "uiaudit:   visual: %s %s %.2f%%%s\n", cp.URL, cp.Viewport, cp.DiffPct, note)
	}
	fmt.Fprintln(&b, "uiaudit:   ADVISORY: nothing above blocks this job. The pixel diff stays advisory indefinitely.")
	return b.String()
}

func join(s []string) string {
	if len(s) == 0 {
		return "(none)"
	}
	out := append([]string(nil), s...)
	sort.Strings(out)
	return strings.Join(out, ", ")
}

func writeJSON(path string, v any) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", " ")
	return enc.Encode(v)
}
