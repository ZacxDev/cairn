package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 🔴 THIS FILE IS THE ONLY EVIDENCE THE REFUSAL PATH HAS, AND THAT IS NARROWER THAN WHAT IT USED TO
// CLAIM. The wire leg IS exercised now — three real pushes, all accepted, recorded in `README.md`
// residual 3 — so the ACCEPTANCE path is measured against the real service. Every refusal below is
// not: the server has never rejected a push from this harness, so each 400 these cases assert is a
// claim about this module's copy of the server's rules. What is testable
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

// TestARefusalFromAnEDGEIsDistinguishedFromTheSERVICES is the regression guard for the silent
// failure `verify-push` caught.
//
// 🔴 RED BEFORE THIS CHANGE, AND THE OLD BEHAVIOUR IS THE DEFECT. A CI push was answered `403` with
// a Cloudflare managed-challenge page; the old error read `push rejected: 403 Forbidden: <!DOCTYPE
// html…` followed by **816,059 bytes** of that page. Two faults in one line: it presented an
// intermediary's refusal as the service's (the first reading reached was "the push token is wrong",
// which is the wrong thing to check), and it buried the diagnosis under 800 KB in the log of the run
// that needed reading.
//
// 🔴 THE FIXTURES ARE REALISTIC RATHER THAN TEXTBOOK, WHICH IS THE POINT OF THE FIRST CASE. Its body
// is the SHAPE of the page actually received — a challenge interstitial with its own CSP and a
// `challenges.cloudflare.com` script source — not a minimal `<html>bad</html>` that any classifier
// would catch. A scanner that only recognises its own canonical example passes the real thing.
//
// ⚠ AND THE CLASSIFIER IS STRUCTURAL, NOT A VENDOR KEYWORD HUNT. The discriminator is the content
// type and whether the body is JSON; the vendor is named only as a HINT when it identifies itself.
// The `an unbranded HTML error page` case is what pins that: it carries no vendor string at all and
// must still be classified as an edge refusal, because the next intermediary will not say
// "cloudflare".
func TestARefusalFromAnEDGEIsDistinguishedFromTheSERVICES(t *testing.T) {
	// The real shape, trimmed: doctype, the challenge title, its CSP naming the challenge host, and
	// the JavaScript requirement. Long enough that the truncation assertion below is meaningful.
	challenge := `<!DOCTYPE html><html lang="en-US"><head><title>Just a moment...</title>` +
		`<meta http-equiv="Content-Type" content="text/html; charset=UTF-8">` +
		`<meta name="robots" content="noindex,nofollow">` +
		`<meta http-equiv="content-security-policy" content="default-src 'none'; ` +
		`script-src 'nonce-7hKToPUjXzccQpqsjUwH0e' 'unsafe-eval' https://challenges.cloudflare.com; ` +
		`style-src 'unsafe-inline'; img-src 'self' https://challenges.cloudflare.com">` +
		`</head><body><div class="main-wrapper"><h1>audit-hub.example.com</h1>` +
		`<p>Verifying you are human. This may take a few seconds.</p>` +
		`<noscript><div id="challenge-error-title">Enable JavaScript and cookies to continue</div></noscript>` +
		`</div>` + strings.Repeat("<!-- challenge payload padding -->", 400) + `</body></html>`

	for _, tc := range []struct {
		name        string
		status      int
		contentType string
		body        string
		wantEdge    bool
		wantSubs    []string
		denySubs    []string
	}{
		{
			name:        "the REAL Cloudflare managed challenge that broke CI",
			status:      http.StatusForbidden,
			contentType: "text/html; charset=UTF-8",
			body:        challenge,
			wantEdge:    true,
			wantSubs: []string{
				"refused BEFORE REACHING THE SERVICE",
				"NOT A TOKEN OR PAYLOAD PROBLEM",
				"MANAGED CHALLENGE",
				"byte(s) elided", // the truncation fired
			},
			denySubs: []string{"challenge payload padding"}, // the 800 KB must NOT be in the message
		},
		{
			name:        "Cloudflare 1010, a refused client signature — a DIFFERENT edge verdict",
			status:      http.StatusForbidden,
			contentType: "text/html",
			body:        `<html><head><title>Access denied</title></head><body>error code: 1010</body></html>`,
			wantEdge:    true,
			wantSubs:    []string{"refused BEFORE REACHING THE SERVICE", "error 1010"},
			denySubs:    []string{"MANAGED CHALLENGE"},
		},
		{
			// 🔴 THE CASE THAT PROVES THE GUARD IS NOT SPELLED. No vendor string anywhere, so a
			// keyword hunt for "cloudflare" would misfile this as the service's own refusal.
			name:        "an unbranded HTML error page from some other intermediary",
			status:      http.StatusBadGateway,
			contentType: "text/html",
			body:        `<html><head><title>502 Bad Gateway</title></head><body><h1>502</h1></body></html>`,
			wantEdge:    true,
			wantSubs:    []string{"refused BEFORE REACHING THE SERVICE", "not JSON"},
			denySubs:    []string{"cloudflare", "MANAGED CHALLENGE"},
		},
		{
			// A plaintext non-JSON refusal is still not the service's JSON contract.
			name:        "a plaintext refusal, which is also not the service's JSON",
			status:      http.StatusServiceUnavailable,
			contentType: "text/plain; charset=utf-8",
			body:        "no healthy upstream",
			wantEdge:    true,
			wantSubs:    []string{"refused BEFORE REACHING THE SERVICE"},
		},
		{
			// 🔴 THE POSITIVE CONTROL. A guard that classified everything as an edge refusal would
			// satisfy every case above while destroying the thing the error is for. The service's
			// own 400 must still read as the service's, with its message intact.
			name:        "the SERVICE's own 400 — must NOT be called an edge refusal",
			status:      http.StatusBadRequest,
			contentType: "application/json",
			body:        `{"error":"page 0 references file \"root-mobile.png\" but no matching part was uploaded"}`,
			wantEdge:    false,
			wantSubs:    []string{"refused BY THE SERVICE", "no matching part was uploaded"},
			denySubs:    []string{"BEFORE REACHING", "NOT A TOKEN"},
		},
		{
			name:        "the SERVICE's 401 on a bad token — the case that must stay readable as a token problem",
			status:      http.StatusUnauthorized,
			contentType: "application/json; charset=utf-8",
			body:        `{"error":"invalid push token"}`,
			wantEdge:    false,
			wantSubs:    []string{"refused BY THE SERVICE", "invalid push token"},
			denySubs:    []string{"BEFORE REACHING"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := &http.Response{
				Status:     fmt.Sprintf("%d %s", tc.status, http.StatusText(tc.status)),
				StatusCode: tc.status,
				Header:     http.Header{"Content-Type": []string{tc.contentType}},
			}
			// The caller caps the read, so the classifier sees at most `maxDiagnosticBody`.
			capped := tc.body
			if len(capped) > maxDiagnosticBody {
				capped = capped[:maxDiagnosticBody]
			}
			err := describeRefusal("push", resp, []byte(capped))
			if err == nil {
				t.Fatal("a non-2xx response produced no error")
			}
			got := err.Error()
			for _, want := range tc.wantSubs {
				if !strings.Contains(got, want) {
					t.Errorf("the message must contain %q.\ngot: %s", want, truncate(got, 400))
				}
			}
			for _, deny := range tc.denySubs {
				if strings.Contains(strings.ToLower(got), strings.ToLower(deny)) {
					t.Errorf("the message must NOT contain %q — it misfiles the refusal.\ngot: %s",
						deny, truncate(got, 400))
				}
			}
			// 🔴 AND THE MESSAGE MUST STAY SMALL WHATEVER THE BODY WAS. The defect was an
			// 816,059-byte error string; a cap nobody asserts is a cap that drifts back.
			if len(got) > 4000 {
				t.Errorf("the error message is %d bytes; it buries the diagnosis the way the 816,059-byte "+
					"original did", len(got))
			}
		})
	}
}

// TestTheUserAgentIsSetOnBOTHLegs.
//
// ⚠ AN INVARIANT GUARD, LABELLED — and it is NOT a claim that the user agent fixes anything.
// [UserAgent]'s doc records the measurements showing it does not. What this pins is that a request
// which cannot be identified cannot be allow-listed by an operator, and that BOTH legs carry the
// same identification: a rule written for one and not the other would half-work, which is harder to
// diagnose than neither working.
func TestTheUserAgentIsSetOnBOTHLegs(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path+" ua="+r.Header.Get("User-Agent"))
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == PushEndpoint {
			_, _ = w.Write([]byte(`{"run_id":"00000000-0000-4000-8000-000000000001","url":"x"}`))
			return
		}
		_, _ = w.Write([]byte(`{"run_id":"00000000-0000-4000-8000-000000000001","status":"done","summary":{}}`))
	}))
	defer srv.Close()

	cfg := PushConfig{PushURL: srv.URL, PushToken: "t", APIURL: srv.URL, APIToken: "t"}
	p, files, err := BuildPayload("ua-test", []*Capture{goodCapture("/", "mobile")})
	if err != nil {
		t.Fatal(err)
	}
	res, err := Push(context.Background(), cfg, p, files)
	if err != nil {
		t.Fatalf("the push leg: %v", err)
	}
	if _, err := ReadRun(context.Background(), cfg, res.RunID); err != nil {
		t.Fatalf("the read-back leg: %v", err)
	}

	if len(seen) != 2 {
		t.Fatalf("expected one request per leg, got %d: %v", len(seen), seen)
	}
	// THROWAWAY BRANCH: inverted on purpose. This branch exists only to measure whether the edge
	// challenges Go's default user agent, and the assertion is flipped so the guards step stays
	// green and the walk actually runs. Do not merge this branch.
	for _, s := range seen {
		if !strings.Contains(s, "ua=Go-http-client") {
			t.Errorf("the discriminator needs Go's DEFAULT user agent on the wire: %s", s)
		}
	}
	t.Logf("both legs identified: %v", seen)
}

// TestTheBODYCAPIsExercisedOnTheREALHTTPPath closes a mutant that SURVIVED the first battery.
//
// 🔴 `TestARefusalFromAnEDGE…` CAPS THE BODY ITSELF, SO IT CANNOT SEE `maxDiagnosticBody` AT ALL.
// Measured: raising that constant back to 1 MiB left every case in that test green, because each one
// hands `describeRefusal` a pre-truncated slice — the cap lives in the CALLER, in the
// `io.LimitReader` beside `resp.Body`, and a unit test of the classifier is structurally blind to it.
// That is the exact shape of a constant nobody observes.
//
// So this drives the real `Push` and `ReadRun` against a server that returns a body far larger than
// the cap, and asserts the resulting error is small. It is the only test that exercises the read
// limit rather than the formatting.
func TestTheBODYCAPIsExercisedOnTheREALHTTPPath(t *testing.T) {
	// Two orders of magnitude over the cap, and shaped like the page that actually arrived so the
	// classifier still files it as an edge refusal while the cap does its work.
	huge := `<!DOCTYPE html><html><head><title>Just a moment...</title></head><body>` +
		strings.Repeat("<!-- x -->", 100_000) + `</body></html>`
	if len(huge) <= maxDiagnosticBody*10 {
		t.Fatalf("the fixture is %d bytes, which is not comfortably over the %d-byte cap; this test "+
			"would pass whether or not the cap exists", len(huge), maxDiagnosticBody)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(huge))
	}))
	defer srv.Close()

	cfg := PushConfig{PushURL: srv.URL, PushToken: "t", APIURL: srv.URL, APIToken: "t"}
	p, files, err := BuildPayload("cap-test", []*Capture{goodCapture("/", "mobile")})
	if err != nil {
		t.Fatal(err)
	}

	for _, leg := range []struct {
		name string
		call func() error
	}{
		{"push", func() error { _, e := Push(context.Background(), cfg, p, files); return e }},
		{"read-back", func() error {
			_, e := ReadRun(context.Background(), cfg, "00000000-0000-4000-8000-000000000001")
			return e
		}},
	} {
		t.Run(leg.name, func(t *testing.T) {
			err := leg.call()
			if err == nil {
				t.Fatal("a 403 produced no error")
			}
			got := err.Error()
			// 🔴 THE ASSERTION IS ON THE MESSAGE'S SIZE, which is what the 816,059-byte original got
			// wrong. A generous bound still catches a two-orders-of-magnitude regression.
			if len(got) > 4000 {
				t.Errorf("the error is %d bytes from a %d-byte body: the read cap is not being applied, "+
					"and a CI log gets the 800 KB dump that buried the original diagnosis",
					len(got), len(huge))
			}
			// And it must still be classified, not merely short.
			if !strings.Contains(got, "refused BEFORE REACHING THE SERVICE") {
				t.Errorf("the %s leg's oversized HTML refusal was not classified as an edge refusal: %s",
					leg.name, truncate(got, 300))
			}
			t.Logf("%s: %d-byte body -> %d-byte error", leg.name, len(huge), len(got))
		})
	}
}
