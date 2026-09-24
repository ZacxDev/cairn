package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// 🔴 THIS FILE IS THE ONLY EVIDENCE THE PUSH LEG HAS, BECAUSE THE WIRE LEG CANNOT BE
// EXERCISED. Creating the hub's plugin target and minting the push and read keys are
// Supabase-gated operator steps, so no push has ever left this harness. What is testable
// without a server is the payload's SHAPE against the rules the server enforces — and every
// one of those rules, when broken, rejects the WHOLE multi-page push rather than the page,
// which is why an offline refusal is worth having.
//
// ⚠ AND IT IS A CLAIM ABOUT THIS MODULE'S COPY OF THE SERVER'S RULES, NOT ABOUT THE SERVER.
// If the hub tightens one, these tests stay green and the push starts failing. That is a
// declared limit of this leg, stated in `README.md` beside the unexercised-wire note.

func goodCapture(path, viewport string) *Capture {
	return &Capture{
		Target:   Target{Path: path, PushURL: path, LedgerRow: "GET " + path},
		Viewport: Viewport{Name: viewport},
		// A one-byte screenshot: this file tests SHAPE, and a real PNG would make the
		// fixture a binary blob in a public repository for no gain.
		Screenshot: []byte{0x89},
		AxeJSON:    []byte(`{"violations":[]}`),
		DigestJSON: []byte(`{"interactive":[{"tag":"button","selector":"#token","focusable":true}],"form_controls":[],"landmarks":[]}`),
		Layout:     &PushLayout{ScrollWidth: 390, InnerWidth: 390},
	}
}

func TestTheBuiltPayloadSatisfiesEveryShapeRuleTheServerEnforces(t *testing.T) {
	captures := []*Capture{goodCapture("/", "mobile"), goodCapture("/", "desktop")}
	captures[0].Violations = []AxeViolation{{ID: "color-contrast", Impact: "serious", Help: "Elements must meet contrast", Nodes: 3}}

	p, files, err := BuildPayload("cairn-ui", captures)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Validate(files); err != nil {
		t.Fatalf("the honest payload must validate: %v", err)
	}
	if p.Environment != EnvLab {
		t.Errorf("environment must be %q (the perf-honesty signal), got %q", EnvLab, p.Environment)
	}

	// 🔴 NO UNKNOWN FIELDS AND NO `perf` KEY. The ingest decodes with
	// `DisallowUnknownFields`, so one extra key is a 400 on the whole push. The check is a
	// ROUND TRIP through a decoder configured the same way, which is a different claim from
	// reading the struct tags: a field added without a tag would serialise under its Go name
	// and the tags would still look right.
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	var mirror PushPayload
	if err := dec.Decode(&mirror); err != nil {
		t.Fatalf("the payload does not survive a strict decode, which is what the ingest does: %v", err)
	}
	if strings.Contains(string(raw), `"perf"`) {
		t.Error("the payload carries a perf block: localhost perf is not field-representative and this harness must omit it")
	}

	// A structured a11y detail carrying the rule id, never the legacy string.
	var seenA11y bool
	for _, pg := range p.Pages {
		for _, f := range pg.Findings {
			if f.Type != "a11y" {
				continue
			}
			seenA11y = true
			if !hasTopLevelStringID(f.Detail) {
				t.Errorf("an a11y detail without a top-level string id makes new_a11y_rules go through the legacy derivation: %q", f.Detail)
			}
			if strings.Contains(f.Detail, " — ") && !strings.HasPrefix(f.Detail, "{") {
				t.Errorf("that is the LEGACY flattened string, not a structured detail: %q", f.Detail)
			}
		}
	}
	if !seenA11y {
		// A positive control on this test itself: with no a11y finding in the fixture the
		// loop above proves nothing and would pass vacuously.
		t.Fatal("the fixture produced no a11y finding, so the structured-detail assertions never ran")
	}
}

// TestValidateREFUSESEachShapeDefectTheServerWouldRejectWith400 is the negative control.
//
// 🔴 EACH SUBTEST BREAKS ONE THING AND MUST FAIL FOR THAT THING'S OWN REASON. A refusal that
// fired because an EARLIER check happened to catch the mutation would be green for the wrong
// reason and would stay green with the guard under test deleted, so every case asserts a
// substring only its own branch emits.
func TestValidateREFUSESEachShapeDefectTheServerWouldRejectWith400(t *testing.T) {
	base := func() (*PushPayload, map[string][]byte) {
		p, f, err := BuildPayload("cairn-ui", []*Capture{goodCapture("/", "mobile")})
		if err != nil {
			t.Fatal(err)
		}
		return p, f
	}

	for _, tc := range []struct {
		name    string
		mutate  func(*PushPayload, map[string][]byte)
		wantSub string
	}{
		{
			name:    "a referenced part that was never uploaded",
			mutate:  func(p *PushPayload, f map[string][]byte) { delete(f, p.Pages[0].Screenshot) },
			wantSub: "no part carries it",
		},
		{
			name:    "an orphan part nothing references",
			mutate:  func(p *PushPayload, f map[string][]byte) { f["stray.png"] = []byte{0x89} },
			wantSub: "not referenced by any page",
		},
		{
			name: "a part over the 16 MiB per-file cap",
			mutate: func(p *PushPayload, f map[string][]byte) {
				f[p.Pages[0].Screenshot] = make([]byte, MaxFileBytes+1)
			},
			wantSub: "per-file cap",
		},
		{
			name:    "a viewport outside the server's closed set",
			mutate:  func(p *PushPayload, f map[string][]byte) { p.Pages[0].Viewport = "tablet" },
			wantSub: "outside the server's closed set",
		},
		{
			name: "an EMPTY a11y digest, which 400s the whole push rather than the page",
			mutate: func(p *PushPayload, f map[string][]byte) {
				f[p.Pages[0].A11yDigest] = []byte(`{"interactive":[],"form_controls":[],"landmarks":[]}`)
			},
			wantSub: "an empty digest is a 400 on the WHOLE push",
		},
		{
			// 🔴 REACHABILITY IS THE POINT OF THIS CASE, NOT THE CAP. The digest cap
			// (256 KiB) is checked in the same loop as the per-file cap (16 MiB) and AFTER
			// it, so a mutation that overshot both would die on the per-file check and this
			// guard's own assertion would never execute — green for the wrong reason, and
			// still green with the digest check deleted. The size below is deliberately
			// BETWEEN the two caps, which is the only band in which this guard can speak,
			// and the asserted substring is one only it emits.
			name: "an a11y digest over its own 256 KiB cap but UNDER the 16 MiB per-file cap",
			mutate: func(p *PushPayload, f map[string][]byte) {
				big := make([]byte, MaxDigest+1)
				if MaxDigest+1 >= MaxFileBytes {
					panic("the digest cap is no longer below the per-file cap; this case can no longer reach its guard")
				}
				// Still valid, non-empty digest JSON — padded inside a string field so the
				// refusal is about SIZE and not about the shape.
				prefix := `{"interactive":[{"tag":"button","selector":"#x","focusable":true}],"form_controls":[],"landmarks":[],"pad":"`
				copy(big, prefix)
				for i := len(prefix); i < len(big)-2; i++ {
					big[i] = 'x'
				}
				big[len(big)-2] = '"'
				big[len(big)-1] = '}'
				f[p.Pages[0].A11yDigest] = big
			},
			wantSub: "the a11y digest",
		},
		{
			name: "a hand-authored layout finding, which the server would drop anyway",
			mutate: func(p *PushPayload, f map[string][]byte) {
				p.Pages[0].Findings = append(p.Pages[0].Findings,
					PushFinding{Type: "layout", Severity: "minor", Detail: "tap target too small"})
			},
			wantSub: "derives those from the raw block via internal/signals",
		},
		{
			name: "an a11y finding flattened to the LEGACY string",
			mutate: func(p *PushPayload, f map[string][]byte) {
				p.Pages[0].Findings = append(p.Pages[0].Findings,
					PushFinding{Type: "a11y", Severity: "serious", Detail: "color-contrast — Elements must meet contrast"})
			},
			wantSub: `non-empty top-level "id"`,
		},
		{
			name: "a ref with a directory component",
			mutate: func(p *PushPayload, f map[string][]byte) {
				body := f[p.Pages[0].Screenshot]
				delete(f, p.Pages[0].Screenshot)
				p.Pages[0].Screenshot = "../escape.png"
				f["../escape.png"] = body
			},
			wantSub: "is not a plain basename",
		},
		{
			name:    "no pages at all",
			mutate:  func(p *PushPayload, f map[string][]byte) { p.Pages = nil; clear(f) },
			wantSub: "no pages in payload",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, f := base()
			if err := p.Validate(f); err != nil {
				t.Fatalf("the unmutated fixture must be clean, or this control measures the fixture: %v", err)
			}
			tc.mutate(p, f)
			err := p.Validate(f)
			if err == nil {
				t.Fatal("the mutation was accepted: the server would 400 the whole push and this gate saw nothing")
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("the refusal fired for the wrong reason.\n  want substring: %q\n  got:            %v", tc.wantSub, err)
			}
		})
	}
}

// TestTooManyPagesIsREFUSEDBeforeTheUpload.
//
// ⚠ AN INVARIANT GUARD. The ledger would have to grow past 100 rows for this walk to reach
// 200 pages, so no plausible near-term tree violates it. It is pinned because the cap is the
// server's and a push over it is rejected entirely — a cheap local refusal beats a 64 MiB
// upload that discovers the same thing.
func TestTooManyPagesIsREFUSEDBeforeTheUpload(t *testing.T) {
	var captures []*Capture
	for i := 0; i <= MaxPages/2; i++ {
		captures = append(captures, goodCapture("/p"+string(rune('a'+i%26))+string(rune('a'+i/26)), "mobile"))
		captures = append(captures, goodCapture("/p"+string(rune('a'+i%26))+string(rune('a'+i/26)), "desktop"))
	}
	p, f, err := BuildPayload("cairn-ui", captures)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Pages) <= MaxPages {
		t.Fatalf("the fixture built %d page(s), which is under the cap: the assertion below cannot fire", len(p.Pages))
	}
	err = p.Validate(f)
	if err == nil || !strings.Contains(err.Error(), "too many pages") {
		t.Fatalf("want a page-cap refusal, got %v", err)
	}
}

// TestTheRunReportDecodesTheHubsOwnKeys pins the read-back struct tags against a fixture
// written from the hub's `internal/report` field names.
//
// 🔴 A MIS-SPELLED KEY HERE IS A SILENT ZERO, WHICH IS THE WORST SHAPE A READ CAN HAVE. It
// would decode to an empty diff and print "0 new rules" — indistinguishable from "nothing
// changed" and therefore from a healthy run. The control is that each field in the fixture
// carries a DISTINCT non-zero value, so a tag pointing at the wrong key produces a wrong
// number rather than the right one by luck.
func TestTheRunReportDecodesTheHubsOwnKeys(t *testing.T) {
	const fixture = `{
	 "run_id": "00000000-0000-4000-8000-000000000001",
	 "status": "done",
	 "label": "cairn-ui",
	 "summary": {"pages_crawled": 8, "a11y_violations": 5,
	  "console_first_party": 3, "console_third_party": 2,
	  "network_first_party": 7, "network_third_party": 4},
	 "diff": {
	  "prev_run_id": "00000000-0000-4000-8000-000000000002",
	  "pages_added": ["/share?scope=alpha-notes"],
	  "pages_removed": [],
	  "pages_changed": 6,
	  "pages_size_changed": 1,
	  "new_a11y_rules": ["color-contrast", "region"],
	  "resolved_a11y_rules": ["label"],
	  "a11y_delta": 9,
	  "console_delta": -1,
	  "network_delta": 11,
	  "changed_pages": [{"url": "/", "viewport": "mobile", "diff_pct": 12.5, "size_changed": true}]
	 }
	}`
	var r RunReport
	if err := json.Unmarshal([]byte(fixture), &r); err != nil {
		t.Fatal(err)
	}
	// Every value below is distinct from every other AND from any constant the assertion
	// names, so a tag pointing at a neighbouring key cannot produce the expected number.
	for _, c := range []struct {
		what string
		got  any
		want any
	}{
		{"run_id", r.RunID, "00000000-0000-4000-8000-000000000001"},
		{"status", r.Status, "done"},
		{"summary.pages_crawled", r.Summary.PagesCrawled, 8},
		{"summary.a11y_violations", r.Summary.A11yViolations, 5},
		{"summary.console_first_party", r.Summary.ConsoleFirst, 3},
		{"summary.console_third_party", r.Summary.ConsoleThird, 2},
		{"summary.network_first_party", r.Summary.NetworkFirst, 7},
		{"summary.network_third_party", r.Summary.NetworkThird, 4},
		{"diff.pages_changed", r.Diff.PagesChanged, 6},
		{"diff.pages_size_changed", r.Diff.PagesSizeChanged, 1},
		{"diff.a11y_delta", r.Diff.A11yDelta, 9},
		{"diff.console_delta", r.Diff.ConsoleDelta, -1},
		{"diff.network_delta", r.Diff.NetworkDelta, 11},
		{"diff.new_a11y_rules length", len(r.Diff.NewA11yRules), 2},
		{"diff.resolved_a11y_rules length", len(r.Diff.ResolvedA11yRules), 1},
		{"diff.changed_pages length", len(r.Diff.ChangedPages), 1},
		{"diff.changed_pages[0].diff_pct", r.Diff.ChangedPages[0].DiffPct, 12.5},
	} {
		if c.got != c.want {
			t.Errorf("%s decoded to %v, want %v — the struct tag does not match the hub's key", c.what, c.got, c.want)
		}
	}

	// 🔴 AND THE ABSENT-DIFF CASE IS A DIFFERENT FACT FROM AN EMPTY ONE. The hub omits the
	// whole key on a first run; a value type would decode that to a zeroed diff and the block
	// would print "0 new rules" on the exact run where that sentence is meaningless.
	var first RunReport
	if err := json.Unmarshal([]byte(`{"run_id":"x","status":"done","summary":{}}`), &first); err != nil {
		t.Fatal(err)
	}
	if first.Diff != nil {
		t.Fatal("a report with no `diff` key must leave Diff nil, so the block can say NO DIFF rather than zero")
	}
	block := DiffBlock(&first)
	if !strings.Contains(block, "NO DIFF") {
		t.Fatalf("the first-run block must say NO DIFF rather than print zeros; got:\n%s", block)
	}
	if !strings.Contains(DiffBlock(&r), "new_a11y_rules (2): color-contrast, region") {
		t.Fatalf("the diff block must carry the rule ids; got:\n%s", DiffBlock(&r))
	}
}

// TestASizeChangedPageIsAnnotatedAsALayoutChange pins the caveat rather than the number.
//
// A full-page height shift makes a pixel diff read as a near-100% regression. The hub gates
// its own visual-regression count on `!size_changed` for that reason, and a log line carrying
// the percentage without the caveat is a number a reader will act on wrongly.
func TestASizeChangedPageIsAnnotatedAsALayoutChange(t *testing.T) {
	var r RunReport
	if err := json.Unmarshal([]byte(`{"run_id":"x","status":"done","summary":{},"diff":{
	 "prev_run_id":"y","new_a11y_rules":[],
	 "changed_pages":[{"url":"/","viewport":"desktop","diff_pct":97.4,"size_changed":true}]}}`), &r); err != nil {
		t.Fatal(err)
	}
	block := DiffBlock(&r)
	if !strings.Contains(block, "size changed — read as a layout change, not a pixel regression") {
		t.Fatalf("a size-changed page must carry the caveat beside the percentage; got:\n%s", block)
	}
	if !strings.Contains(block, "ADVISORY: nothing above blocks") {
		t.Fatalf("the block must say it is advisory; got:\n%s", block)
	}
}

// TestSlugKeepsTheTwoShareTargetsDistinct.
//
// The two `/share?scope=…` pages differ only in a query parameter. A slug that dropped the
// query would collide their filenames, and the second write would silently replace the first
// — one page's captures attributed to the other, with no error anywhere.
func TestSlugKeepsTheTwoShareTargetsDistinct(t *testing.T) {
	a := slug("/share?scope=alpha-notes")
	b := slug("/share?scope=tied-notes")
	if a == b {
		t.Fatalf("both share targets slugged to %q: one page's captures would overwrite the other's", a)
	}
	if got := slug("/"); got != "root" {
		t.Errorf(`slug("/") = %q, want "root" (an empty stem makes a filename that is just an extension)`, got)
	}
	for _, s := range []string{a, b, slug("/")} {
		if strings.ContainsAny(s, "/\\?=.") {
			t.Errorf("slug %q is not a plain basename: the server refuses a ref with a path component", s)
		}
	}
}
