//go:build memobrowser

// The POPULATED memos tab in a real Chromium, over the real `cairn-ui` binary and a real PostgreSQL
// (the S5 row of `claudedocs/plan-cairn-scope-mail.md` asked for "`uiaudit` fixtures for the tab").
//
// 🔴 BEHIND A BUILD TAG BECAUSE IT NEEDS A DATABASE, AND THE MODULE'S ORDINARY RUN HAS NONE. This
// module's `go test ./...` runs in the `uiaudit` job's walk leg with chromium and no PostgreSQL, and a
// test there must refuse rather than skip — so an untagged test would turn that leg red on every run.
// The tag moves the hazard instead of closing it (a tagged test nobody runs is as green as one that
// skips), so `uiaudit/memos_check.sh` is the ONE runner: it stands the database up, runs this test by
// name, reads each clause's own result line, and refuses on a clause with none. The `memos` leg of the
// `uiaudit` CI job invokes it, and `tests/test_uiaudit_job_ledger.py` pins that the leg exists.
//
// What only a browser shows, and what each clause therefore measures:
//
//	a_seeded   the tab is reached by CLICKING it; memos an agent sent are laid out newest first with
//	           the authority's sender line
//	b_send     the form's fields accept typing and its button submits: the memo appears
//	c_draft    a subject the browser's `maxlength` ACCEPTS (61 two-byte letters, 61 UTF-16 units) and the
//	           store refuses (122 bytes) comes back in the form, typed text intact
//	d_retract  the Retract button submits: the row becomes its tombstone, body gone, button gone
//	e_reader   a read-only viewer is shown the tab and the memos, and no form and no button
//	f_walk     the WALK's capture of the populated tab, at every rung of `Viewports`: axe, the touch
//	           measurements, layout, a screenshot — refused by `refuseWalkRegressions`, the function
//	           the walk itself calls, behind a positive control that each capture SAW the memos
//
// ⚠ WHAT IT DOES NOT MEASURE: authorisation. A reader shown no button could still forge the POST;
// that the store refuses it is `cmd/cairn-ui`'s pgtest tier. This is the page, driven as a person
// drives it.
package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/ZacxDev/cairn/internal/ui"
)

// memoRowState is one memo as the page shows it.
type memoRowState struct {
	ID        string `json:"id"`
	Who       string `json:"who"`
	Subject   string `json:"subject"`
	Body      string `json:"body"`
	Tombstone string `json:"tombstone"`
	// HasText is true when the row carries a subject OR a body element at all.
	HasText bool `json:"hasText"`
	// Retract is true when the row carries a Retract button that is actually laid out.
	Retract bool `json:"retract"`
}

// memoTabState is the memos tab, read off the live DOM.
type memoTabState struct {
	Panel    bool           `json:"panel"`
	Outcome  string         `json:"outcome"`
	SendForm bool           `json:"sendForm"`
	ReadOnly bool           `json:"readOnly"`
	Subject  string         `json:"subjectValue"`
	Body     string         `json:"bodyValue"`
	Rows     []memoRowState `json:"rows"`
}

func (s memoTabState) row(id int64) (memoRowState, bool) {
	for _, r := range s.Rows {
		if r.ID == strconv.FormatInt(id, 10) {
			return r, true
		}
	}
	return memoRowState{}, false
}

func (s memoTabState) ids() []string {
	out := make([]string, 0, len(s.Rows))
	for _, r := range s.Rows {
		out = append(out, r.ID)
	}
	return out
}

func (s memoTabState) retractButtons() int {
	n := 0
	for _, r := range s.Rows {
		if r.Retract {
			n++
		}
	}
	return n
}

// The two forms, selected by ACTION — the constants the renderer's own form actions are — for
// `signInSubmit`'s reason: a page that grows another form cannot steal the click.
var (
	memoSendFormSel    = `#scope-memos form[action="` + ui.MemoPath + `"]`
	memoRetractFormSel = `form[action="` + ui.MemoRetractPath + `"]`
)

var memoTabStateJS = fmt.Sprintf(`(() => {
  const panel = document.getElementById("scope-memos");
  if (!panel) return {panel: false, outcome: "", sendForm: false, readOnly: false, subjectValue: "", bodyValue: "", rows: []};
  const text = (el, sel) => { const n = el.querySelector(sel); return n ? n.textContent : ""; };
  const shown = el => !!el && el.getClientRects().length > 0;
  const form = panel.querySelector(%q);
  return {
    panel: true,
    outcome: text(panel, '[data-memo="outcome"]'),
    sendForm: shown(form),
    readOnly: shown(panel.querySelector('[data-memo="read-only"]')),
    subjectValue: form ? form.querySelector("#memo-subject").value : "",
    bodyValue: form ? form.querySelector("#memo-body").value : "",
    rows: Array.from(panel.querySelectorAll("li[data-memo-id]")).map(li => ({
      id: li.getAttribute("data-memo-id"),
      who: text(li, ".who"),
      subject: text(li, '[data-memo="subject"]'),
      body: text(li, '[data-memo="body"]'),
      tombstone: text(li, '[data-memo="tombstone"]'),
      hasText: !!li.querySelector('[data-memo="subject"], [data-memo="body"]'),
      retract: shown(li.querySelector(%q + " button[type=submit]")),
    })),
  };
})()`, `form[action="`+ui.MemoPath+`"]`, memoRetractFormSel)

// The seeded memos, the one the form sends, and the over-long subject. Pairwise distinct texts, so a
// row can only match its own.
const (
	memoSeedOldSubject, memoSeedOldBody = "kiln schedule moves", "the second firing is now on the fourth"
	memoSeedNewSubject, memoSeedNewBody = "glaze batch ready", "batch seven is cured"
	memoFormSubject, memoFormBody       = "shelf plan", "load the top shelf last"
	memoDraftBody                       = "typed beside an over-long subject"
)

// memoDraftSubject is 61 two-byte letters: 61 UTF-16 units, so the form's `maxlength="120"` lets all
// of it be typed, and 122 UTF-8 bytes, so the store's 120-byte limit refuses it.
var memoDraftSubject = strings.Repeat("é", 61)

// TestTheMemosTabInARealBrowser — see the file comment for what each clause measures. The clauses run
// in order over ONE world and each fails with `memos clause (x)`, its own message, which is what
// `memos_check.sh` reads; a clause whose precondition an EARLIER clause established says so rather than
// failing for a reason that is not its own.
func TestTheMemosTabInARealBrowser(t *testing.T) {
	chromiumOrRefuse(t)
	bin, root, dsn := os.Getenv("UIAUDIT_CAIRN_UI"), os.Getenv("UIAUDIT_REPO_ROOT"), os.Getenv("UIAUDIT_MEMO_DSN")
	artifacts := os.Getenv("UIAUDIT_MEMO_ARTIFACTS")
	if bin == "" || root == "" || dsn == "" || artifacts == "" {
		t.Fatal("UIAUDIT_CAIRN_UI, UIAUDIT_REPO_ROOT, UIAUDIT_MEMO_DSN and UIAUDIT_MEMO_ARTIFACTS must name a built " +
			"cairn-ui, its checkout, a PostgreSQL schema of this run's own and a directory for the walk clause's " +
			"screenshots: this is the only test that drives the populated memos tab in a browser, and it REFUSES " +
			"rather than skipping. Run it through uiaudit/memos_check.py, which sets all four.")
	}
	if err := os.MkdirAll(artifacts, 0o700); err != nil {
		t.Fatalf("could not vouch: %v", err)
	}
	port := 18797
	if raw := os.Getenv("UIAUDIT_MEMO_PORT"); raw != "" {
		p, err := strconv.Atoi(raw)
		if err != nil {
			t.Fatalf("UIAUDIT_MEMO_PORT=%q is not a port", raw)
		}
		port = p
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	world, err := BootMemoWorld(ctx, root, bin, t.TempDir(), port, dsn)
	if err != nil {
		t.Fatalf("could not vouch: the memo world did not boot: %v", err)
	}
	defer world.Stop()
	if !strings.Contains(world.Log(), "memos in postgres") {
		t.Fatalf("could not vouch: cairn-ui did not announce a memo store, so the tab would say it has none:\n%s", world.Log())
	}
	// The seeds, SENT through the client listener by the agent credential, oldest first.
	seedOld, err := world.SendAsAgent(ctx, memoSeedOldSubject, memoSeedOldBody)
	if err != nil {
		t.Fatalf("could not vouch: %v\n--- cairn-ui log ---\n%s", err, world.Log())
	}
	// The rendered time is whole seconds and the tab orders by it, then by id; a pause makes the two
	// seeds differ in the TIME, so "newest first" below is not decided by the tie-break alone.
	time.Sleep(1100 * time.Millisecond)
	seedNew, err := world.SendAsAgent(ctx, memoSeedNewSubject, memoSeedNewBody)
	if err != nil {
		t.Fatalf("could not vouch: %v", err)
	}
	agentLine := memoWriterDisplay + " via " + memoAgentLabel + " (user)"
	tabURL := world.BaseURL + ui.ScopePath + "?" + ui.QueryID + "=" + string(world.ScopeID)

	signedIn := func(token string) *Browser {
		t.Helper()
		b, err := NewBrowser(ctx, world.BaseURL, 4*time.Minute)
		if err != nil {
			t.Fatalf("could not vouch: %v", err)
		}
		t.Cleanup(b.Close)
		if _, err := b.SignIn(token); err != nil {
			t.Fatalf("could not vouch: %v\n--- cairn-ui log ---\n%s", err, world.Log())
		}
		return b
	}
	read := func(t *testing.T, b *Browser) memoTabState {
		t.Helper()
		var s memoTabState
		if err := chromedp.Run(b.ctx, chromedp.Evaluate(memoTabStateJS, &s)); err != nil {
			t.Fatalf("reading the memos tab: %v", err)
		}
		return s
	}
	// openTab reaches the tab the way a person does: the scope page, then a CLICK on its Memos tab.
	openTab := func(t *testing.T, b *Browser, clause string) memoTabState {
		t.Helper()
		tabLink := `nav.view-tabs a[data-tab="` + ui.TabMemos + `"]`
		step, stepCancel := context.WithTimeout(b.ctx, 30*time.Second)
		defer stepCancel()
		if err := chromedp.Run(step,
			chromedp.Navigate(tabURL),
			chromedp.WaitVisible(tabLink, chromedp.ByQuery),
			chromedp.Click(tabLink, chromedp.ByQuery),
			chromedp.WaitVisible(`#scope-memos`, chromedp.ByID),
		); err != nil {
			t.Fatalf("memos clause (%s): the Memos tab could not be reached by clicking %s on the scope page: %v", clause, tabLink, err)
		}
		return read(t, b)
	}
	// await polls the tab until `ok` holds, and returns the last state either way: a form POST
	// navigates, and the page that answers it is what every clause reads.
	await := func(t *testing.T, b *Browser, ok func(memoTabState) bool) (memoTabState, bool) {
		t.Helper()
		var last memoTabState
		for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(150 * time.Millisecond) {
			var s memoTabState
			// An evaluation that lands mid-navigation errors; the next poll reads the new page.
			if err := chromedp.Run(b.ctx, chromedp.Evaluate(memoTabStateJS, &s)); err != nil {
				continue
			}
			last = s
			if s.Panel && ok(s) {
				return s, true
			}
		}
		return last, false
	}

	writer := signedIn(world.WriterToken)

	t.Run("a_seeded", func(t *testing.T) {
		s := openTab(t, writer, "a")
		if len(s.Rows) != 2 || s.Rows[0].ID != strconv.FormatInt(seedNew, 10) || s.Rows[1].ID != strconv.FormatInt(seedOld, 10) {
			t.Fatalf("memos clause (a) seeded: the tab lists memos %v, want the two seeded ones newest first [%d %d]",
				s.ids(), seedNew, seedOld)
		}
		for i, want := range []struct{ subject, body string }{{memoSeedNewSubject, memoSeedNewBody}, {memoSeedOldSubject, memoSeedOldBody}} {
			r := s.Rows[i]
			if r.Subject != want.subject || r.Body != want.body || r.Who != agentLine || r.Tombstone != "" {
				t.Errorf("memos clause (a) seeded: row %d shows sender %q subject %q text %q tombstone %q, want %q / %q / %q and no tombstone",
					i, r.Who, r.Subject, r.Body, r.Tombstone, agentLine, want.subject, want.body)
			}
		}
	})

	t.Run("b_send", func(t *testing.T) {
		s := openTab(t, writer, "b")
		if !s.SendForm {
			t.Fatalf("memos clause (b) send: the scope's owner is shown no send form on the memos tab (rows %v)", s.ids())
		}
		before := len(s.Rows)
		if err := chromedp.Run(writer.ctx,
			chromedp.SendKeys(`#memo-subject`, memoFormSubject, chromedp.ByID),
			chromedp.SendKeys(`#memo-body`, memoFormBody, chromedp.ByID),
			chromedp.Click(memoSendFormSel+` button[type=submit]`, chromedp.ByQuery),
		); err != nil {
			t.Fatalf("memos clause (b) send: typing into the form and clicking Send memo: %v", err)
		}
		s, ok := await(t, writer, func(s memoTabState) bool { return s.Outcome != "" })
		if !ok || s.Outcome != "Memo sent." {
			t.Fatalf("memos clause (b) send: after submitting the form the tab's banner reads %q, want %q", s.Outcome, "Memo sent.")
		}
		if len(s.Rows) != before+1 {
			t.Fatalf("memos clause (b) send: the tab lists %d memo(s) after the send, want %d", len(s.Rows), before+1)
		}
		top := s.Rows[0]
		// A browser session sends with NO credential, so the sender line carries no `via`.
		if want := memoWriterDisplay + " (user)"; top.Subject != memoFormSubject || top.Body != memoFormBody || top.Who != want {
			t.Fatalf("memos clause (b) send: the newest memo shows sender %q subject %q text %q, want %q / %q / %q",
				top.Who, top.Subject, top.Body, want, memoFormSubject, memoFormBody)
		}
		if s.Subject != "" || s.Body != "" {
			t.Errorf("memos clause (b) send: after a successful send the form still holds %q / %q, want it empty", s.Subject, s.Body)
		}
	})

	t.Run("c_draft", func(t *testing.T) {
		s := openTab(t, writer, "c")
		if !s.SendForm {
			t.Fatalf("memos clause (c) draft: the scope's owner is shown no send form, so no draft can be typed (rows %v)", s.ids())
		}
		before := len(s.Rows)
		if err := chromedp.Run(writer.ctx,
			chromedp.SendKeys(`#memo-subject`, memoDraftSubject, chromedp.ByID),
			chromedp.SendKeys(`#memo-body`, memoDraftBody, chromedp.ByID),
		); err != nil {
			t.Fatalf("memos clause (c) draft: typing into the form: %v", err)
		}
		// CONTROL: the browser accepted every character — `maxlength` did not stop the over-long
		// subject, which is the whole premise. Without this a browser that truncated it would make
		// the send SUCCEED and the clause would be measuring nothing.
		if typed := read(t, writer); typed.Subject != memoDraftSubject || typed.Body != memoDraftBody {
			t.Fatalf("memos clause (c) draft CONTROL: the form holds a %d-character subject after typing %d — the browser's "+
				"maxlength changed what was typed, so this clause's premise does not hold", len([]rune(typed.Subject)), len([]rune(memoDraftSubject)))
		}
		if err := chromedp.Run(writer.ctx, chromedp.Click(memoSendFormSel+` button[type=submit]`, chromedp.ByQuery)); err != nil {
			t.Fatalf("memos clause (c) draft: clicking Send memo: %v", err)
		}
		s, ok := await(t, writer, func(s memoTabState) bool { return s.Outcome != "" })
		if !ok || !strings.HasPrefix(s.Outcome, "The memo was refused: the subject is longer than") {
			t.Fatalf("memos clause (c) draft: after sending a 122-byte subject the banner reads %q, want the subject-size refusal", s.Outcome)
		}
		if !s.SendForm || s.Subject != memoDraftSubject || s.Body != memoDraftBody {
			t.Fatalf("memos clause (c) draft: the refused send came back with form=%v holding a %d-character subject and text %q, "+
				"want the form holding exactly what was typed (%d characters, %q) — the refusal cost the draft",
				s.SendForm, len([]rune(s.Subject)), s.Body, len([]rune(memoDraftSubject)), memoDraftBody)
		}
		if len(s.Rows) != before {
			t.Errorf("memos clause (c) draft: the refused send changed the listing from %d to %d memo(s)", before, len(s.Rows))
		}
	})

	t.Run("d_retract", func(t *testing.T) {
		s := openTab(t, writer, "d")
		target := seedOld
		row, ok := s.row(target)
		if !ok || row.Tombstone != "" || !row.HasText {
			t.Fatalf("memos clause (d) retract: seeded memo %d is not on the tab as a live memo (rows %v)", target, s.ids())
		}
		if !row.Retract {
			t.Fatalf("memos clause (d) retract: the scope's owner is shown no Retract button on memo %d", target)
		}
		button := fmt.Sprintf(`li[data-memo-id="%d"] %s button[type=submit]`, target, memoRetractFormSel)
		if err := chromedp.Run(writer.ctx, chromedp.Click(button, chromedp.ByQuery)); err != nil {
			t.Fatalf("memos clause (d) retract: clicking Retract on memo %d: %v", target, err)
		}
		s, _ = await(t, writer, func(s memoTabState) bool { return s.Outcome != "" })
		if !strings.HasPrefix(s.Outcome, "Memo retracted.") {
			t.Fatalf("memos clause (d) retract: after clicking Retract the banner reads %q, want it to begin %q", s.Outcome, "Memo retracted.")
		}
		row, ok = s.row(target)
		if !ok {
			t.Fatalf("memos clause (d) retract: the retracted memo %d is no longer listed (rows %v), want its tombstone", target, s.ids())
		}
		if want := "retracted by " + memoWriterDisplay + " (user) at "; !strings.HasPrefix(row.Tombstone, want) {
			t.Fatalf("memos clause (d) retract: memo %d's tombstone line reads %q, want it to begin %q", target, row.Tombstone, want)
		}
		if row.HasText || row.Subject != "" || row.Body != "" || row.Retract {
			t.Fatalf("memos clause (d) retract: the tombstone of memo %d still shows text=%v (subject %q, text %q) or a Retract button (%v)",
				target, row.HasText, row.Subject, row.Body, row.Retract)
		}
		// The other seeded memo is untouched: the click retracted ONE memo, the one clicked.
		if other, ok := s.row(seedNew); !ok || other.Tombstone != "" || other.Body != memoSeedNewBody {
			t.Errorf("memos clause (d) retract: memo %d changed too (present=%v tombstone %q text %q)", seedNew, ok, other.Tombstone, other.Body)
		}
	})

	t.Run("e_reader", func(t *testing.T) {
		// What the writer sees NOW is the comparison: the same memos, and — the positive control for
		// the two absences below — a form and at least one Retract button the reader must not get.
		w := openTab(t, writer, "e")
		if !w.SendForm || w.retractButtons() == 0 {
			t.Fatalf("memos clause (e) reader: the OWNER's tab shows form=%v and %d Retract button(s), so their absence for a "+
				"reader would measure nothing", w.SendForm, w.retractButtons())
		}
		reader := signedIn(world.ReaderToken)
		r := openTab(t, reader, "e")
		if strings.Join(r.ids(), ",") != strings.Join(w.ids(), ",") || len(r.Rows) < 2 {
			t.Fatalf("memos clause (e) reader: the read-only viewer's tab lists memos %v, want the owner's %v", r.ids(), w.ids())
		}
		if got, ok := r.row(seedNew); !ok || got.Subject != memoSeedNewSubject || got.Body != memoSeedNewBody || got.Who != agentLine {
			t.Errorf("memos clause (e) reader: the read-only viewer sees memo %d as %q / %q from %q, want it whole",
				seedNew, got.Subject, got.Body, got.Who)
		}
		if r.SendForm || !r.ReadOnly {
			t.Errorf("memos clause (e) reader: the read-only viewer is shown a send form (%v) or no read-only note (%v)", r.SendForm, !r.ReadOnly)
		}
		if n := r.retractButtons(); n != 0 {
			t.Errorf("memos clause (e) reader: the read-only viewer is shown %d Retract button(s), want none", n)
		}
	})

	t.Run("f_walk", func(t *testing.T) {
		// One more memo, SENT like the seeds, whose subject and text are each one UNBROKEN token:
		// the content most likely to push a page sideways at a phone width, and the reason the walk
		// refuses horizontal overflow at all.
		long, err := world.SendAsAgent(ctx, memoWalkSubject, memoWalkBody)
		if err != nil {
			t.Fatalf("memos clause (f) walk: sending the unbroken-token memo: %v", err)
		}
		// A FRESH browser: the writer's has typed into the form and clicked buttons, and the walk
		// captures the tab as it is first opened, the way every other walked page is.
		b := signedIn(world.WriterToken)
		target := Target{Path: memoTabPath(world), SignedIn: true, LedgerRow: "GET " + ui.ScopePath + " (memo world)"}
		target.PushURL = target.Path
		var captures []*Capture
		for _, vp := range Viewports {
			c, err := b.CaptureTarget(target, vp)
			if err != nil {
				t.Fatalf("memos clause (f) walk: capturing %s at %s: %v\n--- cairn-ui log ---\n%s", target.Path, vp.Name, err, world.Log())
			}
			c.World = MemoWorldName
			captures = append(captures, c)
			t.Log(captureLine("["+MemoWorldName+"] ", c))
			// 🔴 THE POSITIVE CONTROL, AT EVERY RUNG: the page this capture measured HOLDS memos —
			// the two named ones, whole, and the writer's send form laid out. A walk of an EMPTY tab
			// (`ui.NoMemoStore`, or a listing that rendered nothing) passes every refusal below, which
			// is exactly the walk this clause exists to replace.
			s := read(t, b)
			got, okLong := s.row(long)
			seeded, okSeed := s.row(seedNew)
			if !okLong || !okSeed || got.Subject != memoWalkSubject || got.Body != memoWalkBody ||
				seeded.Subject != memoSeedNewSubject || !s.SendForm {
				t.Fatalf("memos clause (f) walk: the capture at %s did not see a POPULATED memos tab — rows %v, memo %d "+
					"present=%v, memo %d present=%v (subject %q), send form=%v — so every refusal over it would be a "+
					"verdict about an empty page", vp.Name, s.ids(), long, okLong, seedNew, okSeed, seeded.Subject, s.SendForm)
			}
			if err := writeMemoArtifact(artifacts, vp, c); err != nil {
				t.Fatalf("memos clause (f) walk: %v", err)
			}
		}
		// 🔴 THE WALK'S OWN REFUSALS, THE SAME FUNCTION `run` CALLS — never a second copy of them:
		// overflow at every width, axe ran, the script allowlist, the content floor at the widest,
		// touch reachability, axe target-size and input font at the touch rungs, header order.
		if err := refuseWalkRegressions(captures); err != nil {
			t.Fatalf("memos clause (f) walk: the populated memos tab fails the walk's refusals: %v", err)
		}
		violations := 0
		for _, c := range captures {
			violations += len(c.Violations)
		}
		t.Logf("memos clause (f) walk: %d capture(s), %d axe violation(s) reported, screenshots in %s", len(captures), violations, artifacts)
	})
}

// The walk clause's memo: a subject at the store's full 120 bytes and a text, each ONE token with
// no break opportunity. Repetitive rather than random, so the send-time secret scan has nothing to
// mistake for a credential.
var (
	memoWalkSubject = strings.Repeat("glaze", 24)
	memoWalkBody    = strings.Repeat("kilnfiring", 40)
)

// memoTabPath is the scope page's memos tab, as the scope page itself links it.
func memoTabPath(w *MemoWorld) string {
	return ui.ScopePath + "?" + url.Values{ui.QueryID: {string(w.ScopeID)}, ui.QueryTab: {ui.TabMemos}}.Encode()
}

// writeMemoArtifact writes one capture's screenshot and axe result where `memos_check.py` collects
// them. Captures are never written into the repository (`main.go`'s reason).
func writeMemoArtifact(dir string, vp Viewport, c *Capture) error {
	if len(c.Screenshot) == 0 {
		return fmt.Errorf("the capture at %s carries no screenshot", vp.Name)
	}
	stem := filepath.Join(dir, "memos-tab-"+vp.Name)
	if err := os.WriteFile(stem+".png", c.Screenshot, 0o600); err != nil {
		return err
	}
	return os.WriteFile(stem+".axe.json", c.AxeJSON, 0o600)
}
