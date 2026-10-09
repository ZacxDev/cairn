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
		var seed []Target
		for _, t := range targets {
			if t.SignedIn == signedIn {
				seed = append(seed, t)
			}
		}
		got, err := walkQueue(browser, world, seed, ledger, "")
		if err != nil {
			return err
		}
		captures = append(captures, got...)
	}
	if len(captures) == 0 {
		return fmt.Errorf("the walk captured nothing: %d target(s) derived from %d ledger row(s)", len(targets), len(ledger))
	}
	favicons := browser.FaviconRefusals()

	// 🔴 THE JOURNAL-BACKED WORLD, BESIDE THE TOKEN-FILE ONE AND NEVER INSTEAD OF IT. Both are
	// supported deployments; the journal one is the shape a deployed instance runs, and it is the
	// only one in which the per-scope share page and its grant form exist. See [BootJournalWorld]
	// for why this answers `boot.go`'s "a page no deployment serves" objection on its own terms,
	// and [journalWorldPaths] for why it walks two rows rather than the ledger again.
	//
	// ⚠ ITS PORT IS PICKED FREE, NOT `port+1`. The token-file world's presence agent already holds a
	// RANDOM free loopback port, which could be `port+1`; picking after that world is up means the
	// agent's port is taken by then and cannot be chosen. `refusePortInUse` still guards the window.
	journalPort, err := aFreeLoopbackPort()
	if err != nil {
		return err
	}
	journalCaptures, err := walkJournalWorld(ctx, abs, uiBinary, filepath.Join(workDir, "journal-world"), journalPort, budget, targets, ledger)
	if err != nil {
		return err
	}
	if err := refuseJournalWorldFellBack(journalCaptures); err != nil {
		return err
	}
	captures = append(captures, journalCaptures...)

	printSignalSummary(captures, favicons)

	// 🔴 TOUCH REACHABILITY IS CHECKED IN ONE PLACE, `refuseWalkRegressions` (clause (c) of the
	// mobile plan's closing condition), AND THE TOUCH SUMMARY PRINTS ONLY AFTER IT PASSED — so a
	// walk whose touch rungs were never touch refuses without printing target sizes measured under
	// a mouse pointer. ⚠ An earlier draft also called `refuseUnreachableTouch` here, BEFORE the
	// summary; that made the call inside `refuseWalkRegressions` unreachable on a real walk.
	if err := refuseWalkRegressions(captures); err != nil {
		return err
	}
	printTouchSummary(captures)

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

// walkQueue captures every seed target at every declared width, following the links each
// link-expanded page publishes, and tags every capture with `worldName`.
//
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
func walkQueue(browser *Browser, world *World, seed []Target, ledger []string, worldName string) ([]*Capture, error) {
	var captures []*Capture
	queue := make([]Target, 0, len(seed))
	enqueued := map[string]bool{}
	for _, t := range seed {
		if !enqueued[t.Path] {
			enqueued[t.Path] = true
			queue = append(queue, t)
		}
	}
	label := ""
	if worldName != "" {
		label = "[" + worldName + "] "
	}
	for len(queue) > 0 {
		t := queue[0]
		queue = queue[1:]
		for _, vp := range Viewports {
			c, err := browser.CaptureTarget(t, vp)
			if err != nil {
				return nil, fmt.Errorf("%s%w\n--- cairn-ui log ---\n%s", label, err, world.Log())
			}
			c.World = worldName
			captures = append(captures, c)
			// `main=NNNpx/MM%` is printed on EVERY capture, not only the ones the floor
			// binds: the number is the only way a reader of this log can see the width
			// ladder working at four widths it is not asserted at. The `touch(…)` block is
			// printed with `coarse=` FIRST, so every touch number on the line carries the
			// pointer state it was measured under.
			fmt.Printf("uiaudit: captured %s%-38s %-9s %d axe=%d layout(tap<44=%d text<12=%d overflow=%v no-viewport-meta=%v main=%dpx/%.0f%%) touch(coarse=%v target-size=%d box<24=%d/%d input<16px=%d/%d) scripts=%d console=%d net=%d digest=%v\n",
				label, t.Path, vp.Name, c.DocStatus, len(c.Violations),
				c.Layout.SmallTapTargets, c.Layout.SmallText, c.Layout.HorizontalOverflow,
				c.Layout.MissingViewportMeta,
				c.Content.MainWidth, 100*float64(c.Content.MainWidth)/float64(c.Content.InnerWidth),
				c.Pointer.Coarse, c.TargetSizeNodes(), c.Touch.TargetsUnder24, c.Touch.TargetsMeasured,
				len(c.Touch.SmallInputs), c.Touch.InputsMeasured,
				c.ScriptCount(),
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
					fmt.Printf("uiaudit:   %sdeclined href %q from %s (not a relative link, carries no query, or names no declared GET row)\n", label, d, t.Path)
				}
				if bounded > 0 {
					// 🔴 PRINTED, AND NOT AS A DECLINE. These are pages the walk WOULD
					// have visited; the hub's 200-page cap is what stops it, and the
					// arithmetic is `targets × pushed viewports`. Silently taking the
					// first four would make a bounded walk read as a complete one, which
					// is the under-coverage this whole derivation exists against.
					fmt.Printf("uiaudit:   %s%s published %d further target(s); BOUNDED to %d, round-robin by row over the sorted paths "+
						"(MaxExpansionsPerPage — one template per row; the hub's page cap is %d)\n",
						label, t.Path, bounded+len(found), MaxExpansionsPerPage, MaxPages)
				}
				if len(found) == 0 {
					// ⚠ NOT AN ERROR, AND SAYING WHY IS THE POINT. On the token-file
					// deployment the share index renders "No scope is administrable by
					// this credential" — an authority answer, not an empty store — so
					// zero links is the correct output there; the journal-backed world
					// beside it is what reaches the per-scope SHARE page. The BROWSE pages
					// are not in that position: a token-file row is unrestricted over the
					// store's scopes, so `GET /` does publish scope links on this deployment.
					fmt.Printf("uiaudit:   %s%s published no expandable links\n", label, t.Path)
				}
				for _, f := range found {
					if enqueued[f.Path] {
						fmt.Printf("uiaudit:   %salready queued %s (published again by %s)\n", label, f.Path, t.Path)
						continue
					}
					enqueued[f.Path] = true
					queue = append(queue, f)
					fmt.Printf("uiaudit:   %sexpanded %s -> %s\n", label, t.Path, f.Path)
				}
			}
		}
	}
	return captures, nil
}

// journalWorldPaths is the set of ledger rows the journal-backed world walks.
//
// 🔴 TWO ROWS, NOT THE LEDGER AGAIN, AND THE NARROWING IS A STATED CHOICE RATHER THAN A BUDGET
// ACCIDENT. These are the rows whose page an `admin`-bearing authority renders DIFFERENTLY: the
// share index lists administrable scopes (and links each per-scope page, which carries the grant
// form), and the invite index is where a project manager would mint. Every other row renders the
// same templates over the same store in both worlds, and walking them twice would double the
// walk's wall time to measure nothing the token-file walk did not.
//
// ⚠ THE INVITE MINT FORM IS NOT REACHED EVEN HERE, DELIBERATELY: it needs `-db-dsn`
// (PostgreSQL), and without one the invite rows render `ui.NoInviteStore` — the state this world
// captures and `refuseJournalWorldFellBack` asserts. Capturing the form means a `services:
// postgres` on the uiaudit job, deferred in the mobile plan (Q10) until a defect is found there.
var journalWorldPaths = map[string]bool{
	ui.SharePath:  true,
	ui.InvitePath: true,
}

// JournalWorld is the [Capture.World] label of the journal-backed world.
const JournalWorld = "journal"

// walkJournalWorld boots the journal-backed pod, signs in with the credential the journal was
// seeded with, and walks [journalWorldPaths].
//
// ⚠ A SECOND BROWSER, NOT THE FIRST ONE REUSED: the two pods are on one host and differ only by
// PORT, and cookies are scoped by host and not by port — so one jar would carry both worlds'
// `__Host-` session cookie under one name and each sign-in would overwrite the other's.
func walkJournalWorld(ctx context.Context, repoRoot, uiBinary, dir string, port int, budget time.Duration,
	targets []Target, ledger []string) ([]*Capture, error) {
	world, err := BootJournalWorld(ctx, repoRoot, uiBinary, dir, port)
	if err != nil {
		return nil, err
	}
	defer world.Stop()
	fmt.Printf("uiaudit: [%s] pod up at %s over %d scope(s), authority = a control journal seeded through internal/control\n",
		JournalWorld, world.BaseURL, len(world.Scopes))

	browser, err := NewBrowser(ctx, world.BaseURL, budget)
	if err != nil {
		return nil, err
	}
	defer browser.Close()
	if _, err := browser.SignIn(world.Token); err != nil {
		return nil, fmt.Errorf("[%s] %w\n--- cairn-ui log ---\n%s", JournalWorld, err, world.Log())
	}

	var seed []Target
	for _, t := range targets {
		if t.SignedIn && journalWorldPaths[t.Path] {
			seed = append(seed, t)
		}
	}
	if len(seed) != len(journalWorldPaths) {
		return nil, fmt.Errorf("[%s] the ledger-derived targets carry %d of the %d signed-in rows this world walks "+
			"(%v): a row was renamed or reclassified, and this world would walk less than it says",
			JournalWorld, len(seed), len(journalWorldPaths), journalWorldPaths)
	}
	return walkQueue(browser, world, seed, ledger, JournalWorld)
}

// refuseJournalWorldFellBack refuses a journal-world walk that did not reach the state it
// exists to capture.
//
// 🔴 IT ASSERTS THE RENDERED STATE, NOT THE FLAG THE POD WAS BOOTED WITH, because the failure it
// guards is silent: a pod that came up on the token-file authority renders `/share` and `/invite`
// at 200 with real-looking pages, and every collector would describe them in detail. Three
// claims, each failing with its own line:
//
//   - at least one per-scope share page (`/share?…`) was captured WITH the grant form (a form
//     posting to `ui.SharePath`) — the page the token-file world cannot reach at all;
//   - no share capture carries `ui.ReadOnlyAuthority`, the token-file world's own sentence;
//   - the invite index was captured carrying `ui.NoInviteStore`, which is the declared,
//     UNCAPTURED-mint-form state of a world with no database (Q10).
func refuseJournalWorldFellBack(captures []*Capture) error {
	var grantForms, readOnlyShare, inviteNoStore, inviteCaptures int
	for _, c := range captures {
		path, _, _ := strings.Cut(c.Target.Path, "?")
		if path == ui.SharePath {
			if strings.Contains(c.Target.Path, "?") && slicesContains(c.FormActions, ui.SharePath) {
				grantForms++
			}
			if slicesContains(c.ReadOnlyNotices, ui.ReadOnlyAuthority) {
				readOnlyShare++
			}
		}
		if path == ui.InvitePath {
			inviteCaptures++
			if slicesContains(c.ReadOnlyNotices, ui.NoInviteStore) {
				inviteNoStore++
			}
		}
	}
	var bad []string
	if grantForms == 0 {
		bad = append(bad, fmt.Sprintf("NO per-scope share page with its grant form (a form posting to %s) was captured "+
			"over %d capture(s) — the one page this world exists to reach", ui.SharePath, len(captures)))
	}
	if readOnlyShare > 0 {
		bad = append(bad, fmt.Sprintf("%d share capture(s) carry the TOKEN-FILE authority's read-only notice — the "+
			"pod is not serving from the control journal it was given", readOnlyShare))
	}
	if inviteNoStore == 0 {
		bad = append(bad, fmt.Sprintf("the invite index was captured %d time(s) and NONE carries the no-database "+
			"notice, so the state recorded as 'mint form uncaptured' is not the state that was measured", inviteCaptures))
	}
	if len(bad) > 0 {
		return fmt.Errorf("the JOURNAL-BACKED WORLD FELL BACK or did not reach its pages:\n  %s", strings.Join(bad, "\n  "))
	}
	fmt.Printf("uiaudit: [%s] reached: %d per-scope share capture(s) with the grant form, 0 carrying the token-file "+
		"notice; %d invite capture(s) in the no-database state (the MINT FORM is UNCAPTURED: it needs -db-dsn)\n",
		JournalWorld, grantForms, inviteNoStore)
	return nil
}

func slicesContains(s []string, want string) bool {
	for _, v := range s {
		if v == want {
			return true
		}
	}
	return false
}

// refuseUnreachableTouch refuses a walk whose touch emulation is not what each capture's
// viewport claims: `(pointer: coarse)` must match at EVERY touch capture and must NOT match at
// EVERY non-touch capture.
//
// 🔴 IT REFUSES FROM S0 ON, WHILE THE MEASUREMENTS IT VOUCHES FOR ARE ONLY REPORTED, BECAUSE IT
// IS A CLAIM ABOUT THE HARNESS AND NOT ABOUT THE PAGE. A touch measurement taken under a mouse
// pointer is a measurement of the desktop layout labelled "mobile", and every `@media (pointer:
// coarse)` rule the mobile plan adds would be invisible to it — measured: before this change all
// 265 captures read coarse=false, touch rungs included. And the OTHER half is not decoration:
// touch emulation persists across navigations in one tab, so a walk that forgot to switch it OFF
// measures every laptop capture as a phone.
//
// ⚠ IT ALSO REFUSES A CAPTURE SET WITH NO CAPTURE ON ONE SIDE: "false at every non-touch capture"
// over zero non-touch captures is the reassuring zero this program refuses everywhere else.
// `TestTheReachabilityRefusalRefusesAnEMPTYSide` pins both directions (touch-only, non-touch-only).
// ⚠ AN INVARIANT GUARD ON A REAL WALK, LABELLED: `Viewports` declares two touch rungs and three
// others, and `refuseWalkRegressions` refuses a collapsed matrix first, so a walk reaches this
// branch only if that DECLARATION changes to all-touch or no-touch — which is what it is for.
func refuseUnreachableTouch(captures []*Capture) error {
	var bad []string
	touchSeen, otherSeen := 0, 0
	for _, c := range captures {
		where := fmt.Sprintf("%s at %s (%dpx)", c.Target.Path, c.Viewport.Name, c.Viewport.Width)
		if c.World != "" {
			where = "[" + c.World + "] " + where
		}
		if c.Pointer == nil {
			bad = append(bad, where+": no pointer probe was taken")
			continue
		}
		if c.Viewport.Touch {
			touchSeen++
		} else {
			otherSeen++
		}
		if c.Pointer.Coarse != c.Viewport.Touch {
			bad = append(bad, fmt.Sprintf("%s: (pointer: coarse) is %v on a %s capture (maxTouchPoints=%d)",
				where, c.Pointer.Coarse, touchWord(c.Viewport.Touch), c.Pointer.MaxTouchPoints))
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("TOUCH REACHABILITY FAILED on %d capture(s) — the touch measurements would describe a "+
			"pointer the page never saw, so the walk refuses rather than reports them. A touch capture that is "+
			"not coarse means emulation never reached the page; a non-touch capture that IS coarse means it was "+
			"never switched off (it persists across navigations):\n    %s",
			len(bad), strings.Join(bad, "\n    "))
	}
	if touchSeen == 0 || otherSeen == 0 {
		return fmt.Errorf("TOUCH REACHABILITY measured %d touch and %d non-touch capture(s): a two-sided claim with "+
			"an empty side is a claim about nothing", touchSeen, otherSeen)
	}
	return nil
}

func touchWord(touch bool) string {
	if touch {
		return "TOUCH"
	}
	return "non-touch"
}

// printTouchSummary reports the S0 touch measurements per world and viewport. REPORT ONLY.
func printTouchSummary(captures []*Capture) {
	type key struct{ world, vp string }
	type agg struct {
		captures, coarse, hoverNone, tsNodes, tsPages, under24, targets, inputs, small int
		pages                                                                          map[string]bool
		selectors                                                                      map[string]float64
	}
	sums := map[key]*agg{}
	for _, c := range captures {
		k := key{c.World, c.Viewport.Name}
		a := sums[k]
		if a == nil {
			a = &agg{pages: map[string]bool{}, selectors: map[string]float64{}}
			sums[k] = a
		}
		a.captures++
		if c.Pointer != nil && c.Pointer.Coarse {
			a.coarse++
		}
		if c.Pointer != nil && c.Pointer.HoverNone {
			a.hoverNone++
		}
		if n := c.TargetSizeNodes(); n > 0 {
			a.tsNodes += n
			a.tsPages++
		}
		if c.Touch != nil {
			a.under24 += c.Touch.TargetsUnder24
			a.targets += c.Touch.TargetsMeasured
			a.inputs += c.Touch.InputsMeasured
			a.small += len(c.Touch.SmallInputs)
			for _, in := range c.Touch.SmallInputs {
				a.pages[c.Target.Path] = true
				a.selectors[in.Selector] = in.FontPx
			}
		}
	}
	fmt.Println("uiaudit: --- touch measurements (REPORT ONLY in S0; S1 refuses on target-size and input font) ---")
	for _, world := range []string{"", JournalWorld} {
		name := "token-file world"
		if world != "" {
			name = world + " world"
		}
		for _, vp := range Viewports {
			a := sums[key{world, vp.Name}]
			if a == nil {
				continue
			}
			sels := make([]string, 0, len(a.selectors))
			for s, px := range a.selectors {
				sels = append(sels, fmt.Sprintf("%s=%gpx", s, px))
			}
			sort.Strings(sels)
			fmt.Printf("uiaudit:   %-16s %-9s captures=%d coarse=%d/%d axe target-size=%d node(s) on %d page(s) | boxes<24px=%d of %d | inputs<%dpx=%d of %d on %d page(s) %s\n",
				name, vp.Name, a.captures, a.coarse, a.captures, a.tsNodes, a.tsPages, a.under24, a.targets,
				minInputFontPx, a.small, a.inputs, len(a.pages), strings.Join(sels, " "))
		}
	}
	hover, total := 0, 0
	for _, a := range sums {
		hover += a.hoverNone
		total += a.captures
	}
	fmt.Printf("uiaudit:   (hover: none) matched on %d of %d capture(s) — ⚠ A BLIND SPOT, NOT A FINDING: headless chromium "+
		"answers hover:none at every width, so every `hover:` rule is unmeasured by this walk (README, blind set)\n", hover, total)
}

// contentWidthFloor is the share of the viewport the page's own `<main>` must occupy at the
// widest captured width.
//
// 🔴 IT IS A FLOOR, NOT A TARGET. A page that uses MORE of the display satisfies it; nothing
// here asks any page to fill the screen, and nothing here has an opinion about line length
// below this line. It exists only to refuse the one shape the overflow refusal cannot see.
//
// 🔴 IT IS DERIVED FROM THE DECLARED LAYOUT AT ONE STATED WIDTH RATHER THAN CHOSEN FOR
// FEEL, AND EVERY VALUE BELOW WAS MEASURED IN A REAL CHROMIUM OVER THIS SURFACE AT
// `contentFloorWidth`:
//
//	the original defect  the `ultra` rung dead, shell capped at `xl`'s 80rem
//	                     = 1232px of content in a 3440px viewport = 35.8%
//	the previous intent  `ultra:max-w-[112rem]` (1792px) less `ultra:px-12` (48px a side)
//	                     = 1696px of content in a 3440px viewport = 49.3%
//	the intent now       `ultra:max-w-[200rem]` (3200px) less the same gutters
//	                     = 3104px of content in a 3440px viewport = 90.2%
//
// 🔴 AND 0.45 WAS RE-DERIVED TO 0.80 WHEN THE RUNG MOVED, WHICH IS THE WHOLE POINT OF
// PINNING A DERIVATION RATHER THAN A NUMBER. 0.45 still refuses the original defect, so it
// would have stayed green — and it would ALSO have stayed green for a silent revert to the
// 112rem rung, which is now a regression against an operator decision rather than the
// intent. A floor that admits both the old intent and the new one is not measuring the
// layout any more, it is measuring that SOME rung survived.
//
// 0.80 refuses the dead rung by 44.2 points, refuses the retired 112rem rung by 30.7, and
// admits the current layout with 10.2 to spare. That headroom is what keeps it a floor and
// not a golden value: widening the ultra gutters to `px-24` still passes (3008px, 87.4%) and
// so does `px-48` (2816px, 81.9%), while any change that puts the shell back on a narrower
// cap does not.
//
// ⚠ IT IS STILL A FLOOR AND NOT A TARGET, AND IT STILL HAS NO OPINION ABOUT LINE LENGTH.
// The prose inside the shell is capped separately — `--measure-code` / `--measure-prose` in
// `internal/ui/tailwind.css` — precisely because "the container uses the display" and "a
// sentence is a readable width" are different claims. Nothing here can see the second one:
// this measures `<main>`, and `<main>` carries the grids.
const contentWidthFloor = 0.80

// contentFloorWidth is the viewport width `contentWidthFloor` was derived at, pinned as a
// literal because the fraction is NOT scale-free: the shell's cap is an absolute 200rem, so
// one honest layout is 90% of 3440 and 58% of 5400. Pinning the width is what keeps the
// fraction a claim with a scope rather than a number that silently goes wrong the day
// somebody widens the matrix — `refuseWalkRegressions` refuses rather than measures if the
// widest declared viewport stops being this one.
const contentFloorWidth = 3440

// signinMainClass is the `<main>` class of the ONE page exempt from the content floor.
//
// 🔴 THE EXEMPTION REQUIRES THE CLASS **AND** THE PATH, AND EITHER ALONE WOULD BE A HOLE.
// The sign-in page's `<main>` IS its card — a `max-w-md` form, 13% of an ultrawide viewport
// — and that is design rather than defect: a credential field stretched across 1696px is
// worse, not better. But an exemption spelled as a CLASS is walked by putting that class on
// a page that should be wide, and one spelled as a PATH is walked by moving wide content
// behind the sign-in route. Requiring both makes each direction loud, and a rename of either
// makes the floor FIRE with this message rather than pass in silence.
const signinMainClass = "signin-main"

// joinMainClass is the `<main>` class of the SECOND one-card public page.
//
// 🔴 IT IS A SECOND EXEMPT PAGE RATHER THAN A WIDENED FIRST ONE, AND THE STYLESHEET HAD
// ALREADY DECIDED THAT — WHICH IS EXACTLY WHY THIS WENT UNNOTICED. `tailwind.css` gives
// `.signin-main` and `.join-main` ONE rule, and says why in as many words: "The sign-in
// page has one card and no list, so its `<main>` IS the card. The JOIN page is the same
// shape for the same reason — one public page, one message, one button — and it shares the
// rule rather than declaring a second one that could drift away from it." The CSS shared
// the shape; the exemption below did not follow, so `/join` rendered 448px of an ultrawide
// 3440px viewport (13.0%) and the floor refused it — correctly, on the letter, and wrongly
// on the intent.
//
// ⚠ MEASURED RATHER THAN REASONED: the walk that caught it is the FIRST one this branch
// ever ran, because `uiaudit` runs in CI and 28(b4) shipped `JoinPage` with no PR. A page
// can therefore be added to a shared CSS rule and be invisible to this floor until somebody
// opens one.
const joinMainClass = "join-main"

// contentFloorExemptPages is the LEDGER of pages the content floor does not bind, as
// (path, class) PAIRS.
//
// 🔴 A LEDGER OF PAIRS RATHER THAN TWO SETS, BECAUSE THE TWO-CONDITION PROPERTY IS THE
// WHOLE GUARD AND A SET-vs-SET FORM WOULD SILENTLY DESTROY IT. `signinMainClass` records
// why the predicate reads both: a CLASS-only exemption is walked by putting that class on a
// page that should be wide, and a PATH-only one by moving wide content behind an exempt
// route. Matching `path ∈ paths && class ∈ classes` would newly admit the CROSS pairs —
// `/join` wearing `signin-main`, `/sign-in` wearing `join-main` — neither of which anybody
// decided. Each row is one decision, and a capture must match a row WHOLE.
var contentFloorExemptPages = []struct {
	Path      string
	MainClass string
}{
	{Path: ui.SignInPath, MainClass: signinMainClass},
	{Path: ui.JoinPath, MainClass: joinMainClass},
}

// contentFloorExempt answers whether this capture is one of the pages the floor does not
// bind. See [signinMainClass] for why each row reads two things and not one.
func contentFloorExempt(c *Capture) bool {
	for _, row := range contentFloorExemptPages {
		if c.Target.Path == row.Path && c.Content.MainClass == row.MainClass {
			return true
		}
	}
	return false
}

// exemptPagesForLog renders the ledger for the summary line, so the clean verdict names
// WHICH pages it excused rather than a bare count.
func exemptPagesForLog() string {
	out := make([]string, 0, len(contentFloorExemptPages))
	for _, row := range contentFloorExemptPages {
		out = append(out, fmt.Sprintf("%s with class %q", row.Path, row.MainClass))
	}
	return strings.Join(out, ", ")
}

// refuseWalkRegressions turns four per-page measurements into a FAILED WALK.
//
// 🔴 A NUMBER IN A SUMMARY LINE IS NOT A GATE, AND THIS FILE ALREADY RECORDS WHAT THAT
// COSTS. `printSignalSummary` has printed `pages with horizontal overflow=N` since this
// harness existed; nothing read it, so a responsive regression would have been a digit in a
// log beside an exit 0. The four below are the properties this surface is supposed to
// have, so each is a refusal:
//
//   - NO HORIZONTAL OVERFLOW AT ANY CAPTURED WIDTH. This is the single layout defect no
//     unit test in this repository can see, and the one content is most likely to cause: a
//     long unbroken line in a store entry's body makes the whole PAGE scroll sideways, on a
//     phone above all. The widths are the whole point — a page can be clean at 390 and 1440
//     and broken at 834, which is why there are five.
//   - NO SCRIPT BUT THE ALLOWLISTED ONES. `internal/ui`'s RENDERER emits exactly the
//     same-origin `src`s `ui.AllowedScriptSources` names (today: the scope page's entry
//     filter), at most once each, and NO inline script — part of its XSS story rests on that.
//     An inline script, a foreign `src` or a duplicate is a refusal. ⚠ IT WAS "NO SCRIPT AT
//     ALL" until the operator chose a client-side filter; a count cannot tell the allowed
//     script from an injected one, so the refusal reads the list. ⚠ THE RENDERER, NOT THE
//     SERVED PAGE: this walk boots its own pod on loopback, so an edge CDN that injects script
//     downstream is invisible to it — and on the deployed surface one does. Scope and
//     measurement: [Capture.ScriptSrcs].
//   - AXE ACTUALLY RAN. `Violations: 0` is produced identically by a clean page and by an
//     injection that never executed, and this whole program's a11y half is inert in the
//     second case. A decodable `testEngine` is what separates them.
//   - THE CONTENT IS NOT A COLUMN IN A SEA OF DARK AT THE WIDEST WIDTH. 🔴 THIS ONE IS HERE
//     BECAUSE THE OVERFLOW REFUSAL ABOVE IS STRUCTURALLY BLIND TO IT, WHICH IS MEASURED AND
//     NOT SUSPECTED: a walk reported `0 overflow` over 65 captures at five widths and was
//     CORRECT, on a tree whose pages rendered 36% of an ultrawide viewport — a container
//     that is too NARROW never overflows, so the entire class "the page ignores the
//     viewport" shipped green through every check this harness had. Too-wide and too-narrow
//     are different claims and both are asserted; neither replaces the other.
//
// ⚠ IT IS NOT A THRESHOLD ON `SmallTapTargets` OR `SmallText`. Those two are the hub's to
// turn into findings — `vendor-js/VENDOR.md` records that the server is the single source
// of those thresholds and that a harness computing one here would be a second, invisible
// copy. The content floor is not one of those: it is not a rendering heuristic the hub has
// an opinion about, it is a property of THIS surface's own declared width ladder, derived
// from it and asserted nowhere else.
func refuseWalkRegressions(captures []*Capture) error {
	if len(captures) == 0 {
		return fmt.Errorf("refuseWalkRegressions was handed NO capture, so its clean verdict would be about nothing")
	}
	var overflow, scripted, axeless, narrow []string
	widths := map[string]bool{}
	// Clause (c) of the mobile plan's closing condition starts here: the touch measurements
	// are only readable if touch emulation reached the page, so that is checked first. See
	// [refuseUnreachableTouch]. This is its ONLY call site; `run` prints the touch summary after it passed.
	touchErr := refuseUnreachableTouch(captures)
	// The content floor's own accounting: how many captures it actually looked at, how many
	// it let through as the declared exemption, and the narrowest fraction it saw. All three
	// are printed on a clean run, because "0 refusals" from a predicate that inspected
	// nothing is the reassuring zero this program refuses everywhere else.
	floorMeasured, floorExempt := 0, 0
	floorMin, floorMinWhere := 1.0, ""
	for _, c := range captures {
		where := fmt.Sprintf("%s at %s (%dpx)", c.Target.Path, c.Viewport.Name, c.Viewport.Width)
		widths[c.Viewport.Name] = true
		// 🔴 THE FLOOR BINDS AT THE WIDEST DECLARED WIDTH ONLY, AND THE VIEWPORT IS MATCHED BY
		// VALUE RATHER THAN BY ITS NAME STRING — the same correction `Viewport.Touch` carries:
		// a behavioural property keyed on a name is a property a width renamed anything else
		// silently loses. Below `--breakpoint-ultra` the shell's cap is at or above the
		// viewport, so the fraction is near 1 at every other width and the assertion would be
		// vacuous there; at 3440 it is the whole question.
		if c.Viewport == Ultrawide {
			if c.Content == nil {
				return fmt.Errorf("%s carries no content box, so the content floor would be deciding "+
					"from a nil measurement", where)
			}
			// 🔴 THE NUMERATOR'S OWN INSTRUMENT CHECK, AND IT IS A SEPARATE REFUSAL BECAUSE A
			// FRACTION WITH AN UNMEASURED NUMERATOR IS NOT A SMALL FRACTION, IT IS NO FRACTION.
			// A document with no `<main>` yields `main_width: 0`, which the floor below would
			// report as the narrowest page that has ever existed — a true refusal for a false
			// reason, sending the reader after a width ladder when the page has no content
			// landmark at all. Two `<main>`s make it arbitrary which was measured.
			if c.Content.MainCount != 1 {
				return fmt.Errorf("%s has %d <main> element(s), and the content floor is a claim about exactly "+
					"one: zero makes its width a 0 that reads as an infinitely narrow page, two makes it "+
					"arbitrary which one was measured", where, c.Content.MainCount)
			}
			if c.Content.MainWidth <= 0 {
				return fmt.Errorf("%s reports a <main> %dpx wide: no rendered element is zero pixels wide, so "+
					"the content floor would be refusing a measurement that never happened",
					where, c.Content.MainWidth)
			}
			floorMeasured++
			if contentFloorExempt(c) {
				floorExempt++
			} else {
				frac := float64(c.Content.MainWidth) / float64(c.Content.InnerWidth)
				if frac < floorMin {
					floorMin, floorMinWhere = frac, where
				}
				if frac < contentWidthFloor {
					narrow = append(narrow, fmt.Sprintf(
						"%s: <main> rendered %dpx of innerWidth=%dpx = %.1f%% of the viewport, floor is %.0f%% "+
							"(body=%dpx, <main class=%q>)",
						where, c.Content.MainWidth, c.Content.InnerWidth, frac*100, contentWidthFloor*100,
						c.Content.BodyWidth, c.Content.MainClass))
				}
			}
		}
		if c.Layout.HorizontalOverflow {
			overflow = append(overflow, fmt.Sprintf("%s: scrollWidth=%d > innerWidth=%d",
				where, c.Layout.ScrollWidth, c.Layout.InnerWidth))
		}
		for _, bad := range scriptsOutsideAllowlist(c.ScriptSrcs) {
			scripted = append(scripted, fmt.Sprintf("%s: %s", where, bad))
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
	// 🔴 THE FLOOR'S SCOPE IS PINNED TO A LITERAL WIDTH, AND A MATRIX THAT MOVED PAST IT
	// REFUSES RATHER THAN MEASURES. `contentWidthFloor` is a fraction of a viewport and the
	// shell's cap is an absolute 200rem, so the same correct layout scores 90% at 3440 and
	// 58% at 5400: widening the widest capture without re-deriving the fraction would turn
	// an honest tree red, and NARROWING it would make the floor pass on a layout that never
	// reached the `ultra` breakpoint at all. Both are re-derivations, so both stop here.
	if Ultrawide.Width != contentFloorWidth {
		return fmt.Errorf("the content floor of %.0f%% was derived at a %dpx viewport and the widest declared "+
			"one is now %dpx: the fraction is not scale-free (the shell's cap is an absolute 200rem), so it has "+
			"to be re-derived at the new width rather than carried over",
			contentWidthFloor*100, contentFloorWidth, Ultrawide.Width)
	}
	if floorMeasured-floorExempt <= 0 {
		return fmt.Errorf("the content floor BOUND 0 capture(s): %d were taken at the %dpx viewport and %d of "+
			"those were the declared exemption, so its clean verdict would be about nothing. A walk that "+
			"captured no page at the widest declared width, or only the sign-in page there, has not measured "+
			"this property at all",
			floorMeasured, Ultrawide.Width, floorExempt)
	}
	var refusals []string
	if touchErr != nil {
		refusals = append(refusals, touchErr.Error())
	}
	if len(overflow) > 0 {
		refusals = append(refusals, fmt.Sprintf("HORIZONTAL OVERFLOW on %d capture(s) — the page scrolls "+
			"sideways, which no unit test in this repository can see:\n    %s",
			len(overflow), strings.Join(overflow, "\n    ")))
	}
	// ⚠ THE MESSAGE SAYS "THIS ORIGIN" RATHER THAN "THIS SURFACE" ON PURPOSE, because the pod
	// this walk boots is the only thing it can speak for — see [Capture.ScriptCount].
	if len(scripted) > 0 {
		refusals = append(refusals, fmt.Sprintf("SCRIPT ON THE PAGE outside the allowlist, %d finding(s) — this "+
			"ORIGIN renders only `ui.AllowedScriptSources`, once each, and never inline:\n    %s",
			len(scripted), strings.Join(scripted, "\n    ")))
	}
	if len(axeless) > 0 {
		refusals = append(refusals, fmt.Sprintf("AXE DID NOT RUN on %d capture(s) — a zero violation count "+
			"from a page axe never inspected is indistinguishable from a clean one:\n    %s",
			len(axeless), strings.Join(axeless, "\n    ")))
	}
	if len(narrow) > 0 {
		refusals = append(refusals, fmt.Sprintf("CONTENT TOO NARROW on %d capture(s) at the %dpx viewport — "+
			"the page renders a column and leaves the rest of the display empty. 🔴 THE OVERFLOW REFUSAL "+
			"ABOVE CANNOT SEE THIS: a container that is too narrow never overflows, so this whole class is "+
			"green on every other check here:\n    %s",
			len(narrow), Ultrawide.Width, strings.Join(narrow, "\n    ")))
	}
	if len(refusals) > 0 {
		return fmt.Errorf("the walk measured %d regression class(es) over %d capture(s):\n  %s",
			len(refusals), len(captures), strings.Join(refusals, "\n  "))
	}
	fmt.Printf("uiaudit:   REFUSALS: 0 horizontal overflow, 0 scripts outside the allowlist, %d/%d captures carry a decodable axe "+
		"testEngine — over %d distinct width(s): %s\n",
		len(captures)-len(axeless), len(captures), len(widths), viewportWidths())
	// ⚠ THIS LINE IS THE RESULT OF A REFUSAL THAT PASSED, NOT AN INDEPENDENT MEASUREMENT: reaching
	// it means `refuseUnreachableTouch` found no mismatch, so the two numerators can only equal
	// their denominators here. What it adds is that the numerators are counted from what the PROBE
	// READ (`Pointer.Coarse`), not from the viewport's declaration, and that it names both sides.
	touchN, coarseAtTouch, otherN, fineAtOther := 0, 0, 0, 0
	for _, c := range captures {
		if c.Viewport.Touch {
			touchN++
			if c.Pointer != nil && c.Pointer.Coarse {
				coarseAtTouch++
			}
		} else {
			otherN++
			if c.Pointer != nil && !c.Pointer.Coarse {
				fineAtOther++
			}
		}
	}
	fmt.Printf("uiaudit:   TOUCH REACHABILITY refusal PASSED: the probe read (pointer: coarse) TRUE at %d of %d touch "+
		"capture(s) and FALSE at %d of %d non-touch capture(s)\n", coarseAtTouch, touchN, fineAtOther, otherN)
	// 🔴 THE FLOOR REPORTS ITS NARROWEST MEASUREMENT RATHER THAN A ZERO. "0 refusals" is
	// produced identically by a surface that widens and by a predicate that inspected
	// nothing; the number below moves when the layout does, which is what makes the clean
	// verdict readable as a measurement.
	fmt.Printf("uiaudit:   CONTENT FLOOR: narrowest non-exempt <main> at %dpx used %.1f%% of the viewport "+
		"(%s), floor %.0f%% — over %d capture(s) at that width, %d exempt (%s)\n",
		Ultrawide.Width, floorMin*100, floorMinWhere, contentWidthFloor*100,
		floorMeasured, floorExempt, exemptPagesForLog())
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

// printSignalSummary reports every signal at the scope it was measured, and labels the
// zeros that are zero BY CONSTRUCTION as structural rather than as passes.
//
// ⚠ THIS BLOCK USED TO SIT ABOVE `refuseWalkRegressions`, WHERE GODOC ATTACHED IT TO THAT
// FUNCTION INSTEAD — a doc comment on the wrong function is a wrong claim, and it was moved
// here rather than left for the next reader to trip over.
//
// 🔴 A REASSURING ZERO IS INDISTINGUISHABLE FROM AN INSTRUMENT WIRED TO NOTHING, SO EACH
// ZERO HERE SAYS WHICH KIND IT IS. `control_test.go` is the positive control that shows the
// same collectors CAN count; the README reports the pair.
//
// ⚠ AND THE STRUCTURAL CLAIM IS NARROWER THAN AN EARLIER DRAFT OF IT SAID, WHICH IS A
// CORRECTION RATHER THAN A CAVEAT. That draft printed "console=%d network=%d — STRUCTURAL
// ZERO" over both numbers, on the reasoning that a page with no scripts and no subresources
// cannot produce either. The first half held then: `internal/ui` shipped an inline stylesheet,
// no script, and an XSS guard asserting `"<img"` can never render, so the console collector had
// nothing to observe. ⚠ It holds now only on pages without the one allowlisted script (the
// scope page's entry filter) — the console line below says which kind of zero it printed. The second half was FALSE, and the walk that printed it had a non-zero
// network count on the same line — because a browser requests `/favicon.ico` on its own
// initiative and this surface has no such row, so it answers the dispatcher's refusal. That
// is counted separately (see [Browser.FaviconRefusals]) and the per-page network zero below
// is a claim about SUBRESOURCES THE PAGE ASKED FOR, which is a narrower sentence than the one
// that was wrong.
func printSignalSummary(all []*Capture, faviconRefusals int) {
	s := summarizeSignals(all)
	captures := s.tokenFile
	ids := s.ruleIDs()

	// What each line covers, stated in the header because the two worlds are summed APART: every
	// line below is the TOKEN-FILE world's except the digests line (PUSHED captures only, which are
	// all token-file) and the one `journal world` line, which is that world's own sums.
	fmt.Printf("uiaudit: --- signals: the lines below are the TOKEN-FILE world's %d capture(s), except the digests "+
		"line (PUSHED captures only) and the `journal world` line (its %d capture(s), never pushed) ---\n",
		len(captures), s.journal)
	fmt.Printf("uiaudit:   axe violations: %d across %d rule(s): %s\n", s.axe, len(ids), strings.Join(ids, ", "))
	fmt.Printf("uiaudit:   layout: tap targets under 44px=%d, text under 12px=%d, pages with horizontal overflow=%d, pages missing <meta viewport>=%d\n",
		s.tap, s.text, s.overflow, s.noViewport)
	fmt.Printf("uiaudit:   a11y digests attached: %d of %d PUSHED page(s) — any shortfall is a page whose digest came back EMPTY, whose ref is therefore omitted (an empty digest is a 400 on the WHOLE push)\n",
		s.digests, s.pushed)
	console, netw := s.console, s.netw
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
	// ⚠ AND THE ZERO IS NO LONGER STRUCTURAL ON EVERY PAGE: the scope page now runs the ONE
	// allowlisted script (`ui.AllowedScriptSources`), and a script that threw would land here. So
	// on a walk that captured a page carrying it, zero is a MEASUREMENT — the filter ran and logged
	// nothing — and the sentence says which.
	scripted := 0
	for _, c := range captures {
		if c.ScriptCount() > 0 {
			scripted++
		}
	}
	if console == 0 && scripted > 0 {
		fmt.Printf("uiaudit:   console=0 — a MEASUREMENT on %d capture(s) that ran the allowlisted script (it threw and logged nothing there), and structural on the rest, which render no script. control_test.go counts 2 on a page that does log. (ORIGIN, not the served page — a downstream injector is out of this walk's reach; see Capture.ScriptSrcs.)\n", scripted)
	} else if console == 0 {
		fmt.Printf("uiaudit:   console=0 — 🔴 STRUCTURAL, NOT A PASS: no captured page carried a script, and an existing XSS guard asserts \"<img\" can never render, so the console collector had nothing to observe. control_test.go counts 2 on a page that does. (ORIGIN, not the served page — a downstream injector is out of this walk's reach; see Capture.ScriptSrcs.)\n")
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
	// The journal world's OWN line. Its grant form exists nowhere else and is never pushed, so this
	// is the only summed place its defects appear.
	if s.journal > 0 {
		j := s.journalSums
		fmt.Printf("uiaudit:   journal world (%d capture(s), never pushed): axe violations=%d across %d rule(s): %s | "+
			"console=%d network=%d | tap targets under 44px=%d, text under 12px=%d, pages with horizontal "+
			"overflow=%d, pages missing <meta viewport>=%d\n",
			s.journal, j.axe, len(j.rules), strings.Join(j.ruleIDs(), ", "), j.console, j.netw,
			j.tap, j.text, j.overflow, j.noViewport)
	}
}

// worldSums is one world's whole-walk signals.
type worldSums struct {
	rules                                               map[string]int
	axe, console, netw, tap, text, overflow, noViewport int
}

func (w *worldSums) add(c *Capture) {
	if w.rules == nil {
		w.rules = map[string]int{}
	}
	w.axe += len(c.Violations)
	for _, v := range c.Violations {
		w.rules[v.ID]++
	}
	w.console += len(c.Console)
	w.netw += len(c.Network)
	w.tap += c.Layout.SmallTapTargets
	w.text += c.Layout.SmallText
	if c.Layout.HorizontalOverflow {
		w.overflow++
	}
	if c.Layout.MissingViewportMeta {
		w.noViewport++
	}
}

// ruleIDs is the sorted rule list for a log line.
func (w worldSums) ruleIDs() []string {
	ids := make([]string, 0, len(w.rules))
	for id := range w.rules {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// signalSums is what `printSignalSummary` prints, computed apart from the printing so the
// SCOPE of each number is testable. The embedded [worldSums] is the TOKEN-FILE world's.
type signalSums struct {
	worldSums
	tokenFile []*Capture
	// journal and journalSums are the journal-backed world's, summed APART — see
	// [summarizeSignals].
	journal     int
	journalSums worldSums
	// digests and pushed are over PUSHED captures only ([pushedCapture]), because the line says
	// what reaches the hub.
	digests, pushed int
}

// summarizeSignals counts the whole-walk signals PER WORLD.
//
// 🔴 THE TWO WORLDS ARE SUMMED APART, AND NEITHER IS DROPPED — BOTH DIRECTIONS WERE A DEFECT HERE.
// When the journal world landed it was silently folded into the token-file lines: "tap targets
// under 44px" moved from 2785 to 3070 and "a11y digests attached" from 265/265 to 295/295 with no
// token-file page changing. The first fix then EXCLUDED it — and summed it nowhere, so an axe
// violation on the grant form (rendered only in that world, which is never pushed) appeared in one
// per-capture line beside a summary saying "axe violations: 0". Separate sums are both answers.
func summarizeSignals(all []*Capture) signalSums {
	s := signalSums{worldSums: worldSums{rules: map[string]int{}}, journalSums: worldSums{rules: map[string]int{}}}
	for _, c := range all {
		if pushedCapture(c) {
			s.pushed++
			if c.HasDigest() {
				s.digests++
			}
		}
		if c.World != "" {
			s.journal++
			s.journalSums.add(c)
			continue
		}
		s.tokenFile = append(s.tokenFile, c)
		s.worldSums.add(c)
	}
	return s
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

// scriptsOutsideAllowlist is the walk's script refusal: one line per script element that is
// INLINE, names a `src` `ui.AllowedScriptSources` does not, or repeats an allowlisted one.
//
// 🔴 THE ALLOWLIST IS READ FROM `internal/ui`, NEVER COPIED HERE. A second list in this module
// would be a second place deciding what a page may run, and it would go stale in the direction
// nobody notices — a script added to the renderer and to the copy, or removed from one.
func scriptsOutsideAllowlist(srcs []string) []string {
	allowed := map[string]bool{}
	for _, src := range ui.AllowedScriptSources() {
		allowed[src] = true
	}
	var out []string
	seen := map[string]int{}
	for _, src := range srcs {
		switch {
		case src == "":
			out = append(out, "an INLINE script")
		case !allowed[src]:
			out = append(out, fmt.Sprintf("a script from %q, which the allowlist does not name", src))
		default:
			seen[src]++
			if seen[src] == 2 {
				out = append(out, fmt.Sprintf("the allowlisted script %q, more than once", src))
			}
		}
	}
	return out
}
