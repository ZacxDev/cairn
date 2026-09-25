package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"sort"
	"strings"
	"time"
)

// 🔴 "THE HUB" IS A HOSTED UX-AUDIT SERVICE THIS REPOSITORY MAY NOT NAME, AND THAT IS A
// MEASURED CONSTRAINT RATHER THAN COYNESS. IT IS ALSO THE ONE PLACE THAT SAYS SO — every
// other mention in this module says "the hub" and points here.
//
// It ingests a multipart push of screenshots, axe results and raw layout measurements from
// several producers, computes a deterministic diff against that producer's previous run, and
// serves the result back over a read API. `payload.go` mirrors its push schema; `RunReport`
// below decodes the part of its report this harness reads.
//
// Its project name is in `tests/leakscan.py`'s `denied-identifier` set — the closed digest set
// of real project, repository, cluster and host names a scrub already removed from this public
// repository. AGENTS.md is explicit that the remedy is to replace the name and NOT to add an
// exception, "because there are none". Measured on this tree: a first draft of this module
// spelled the name in prose, in comments, in the four environment variables and in the CI job,
// and `python3 tests/leakscan.py` returned **83 findings across 13 files and exit 1**. That is
// why the environment variables below are `CAIRN_AUDIT_*` rather than carrying the hub's own
// prefix — a detail an operator wiring the four secrets has to know, since every sibling
// producer of this hub uses the other spelling.
//
// 🔴 AND BOTH BASE URLS COME FROM THE ENVIRONMENT, NEVER FROM A LITERAL, FOR A SECOND AND
// INDEPENDENT REASON. `leakscan.py`'s `reachable-hostname` rule is a general unbounded regex
// over several domains applied line-by-line to every file type — so a hostname in a comment, a
// doc, a test fixture or a flag's default value fires it. Two reasons, so closing one would not
// make a literal safe. Docs in this repository use `audit-hub.example.com`; the harness itself
// targets `127.0.0.1`, and the hub's identity arrives only in a secret's VALUE.
//
// PushEndpoint and RunEndpoint are its paths. The HOSTS are never spelled here.
const (
	PushEndpoint = "/api/plugins/runs"
	RunEndpoint  = "/api/audit/runs/"
)

// UserAgent identifies this client on every request it makes.
//
// 🔴 IT IS NOT THE FIX FOR THE CLOUDFLARE REFUSAL, AND SAYING SO IS THE POINT. A CI run was
// answered `403` with a Cloudflare managed-challenge page ("Just a moment…", scripts from
// `challenges.cloudflare.com`, "Enable JavaScript") instead of reaching the app. The obvious theory
// is that Go's default `User-Agent: Go-http-client/2.0` is refused as a non-browser signature.
// **That theory is MEASURED FALSE:**
//
//   - three pushes from a workstation, with this same client and the DEFAULT user agent, were
//     answered `200` and created runs the service still lists;
//   - and an unauthenticated probe of the read API from that workstation is answered `401` by the
//     APP — the app's own `text/plain` refusal, so the request arrived — under Go's default UA, an
//     honest custom UA, and a browser-shaped UA alike. All three reach it.
//
// Same binary, same UA, different network origin: the discriminator is WHERE the request comes
// from, not what it calls itself. A hosted CI runner sits in an address range a managed challenge
// treats as higher risk. **Closing that needs an operator change in front of the service** (an
// allow rule for the push endpoint, or for the runner's ranges) — there is nothing this program can
// do about it, and nothing it SHOULD do: working around a challenge is exactly the wrong response.
//
// So why set it at all? Because a request that cannot be identified cannot be allow-listed. An
// operator writing that rule needs a stable string to write it against, and `Go-http-client/2.0` is
// shared with every other Go program on the internet. That is the whole claim for this constant.
const UserAgent = "cairn-uiaudit/1 (+https://github.com/ZacxDev/cairn)"

// maxDiagnosticBody caps how much of a refusal body reaches an error message.
//
// 🔴 4 KiB, DOWN FROM 1 MiB, BECAUSE THE OLD CAP PUT 800 KB OF CHALLENGE HTML INTO A CI LOG AND AN
// UPLOADED ARTIFACT. Measured: the refusal above produced an 816,059-byte error string. The
// diagnosis — a status and a content type — was in the first 200 bytes; everything after it buried
// the finding in the log of the very run that needed reading. A cap that is generous "just in case"
// is a cap that makes the one case it fires on unreadable.
const maxDiagnosticBody = 4 << 10

// describeRefusal turns a non-2xx response into an error that says WHO refused.
//
// 🔴 A NON-JSON BODY ON THESE ENDPOINTS IS NOT A PUSH FAILURE THE WAY A 400 IS — IT MEANS SOMETHING
// IN FRONT OF THE SERVICE ANSWERED, AND CONFLATING THE TWO COSTS AN INVESTIGATION. The service
// refuses with JSON and a credential-free message; an edge refuses with HTML. Without this
// distinction a `403` carrying a challenge page reads as "bad push token", which is the wrong thing
// to go and check — and was the first reading reached when this happened.
//
// It is deliberately structural rather than a keyword hunt: the test is the CONTENT TYPE and
// whether the body parses as JSON, not the presence of a vendor's name. A guard spelled
// "cloudflare" would miss the next edge, and any intermediary that answers HTML on a JSON endpoint
// is the same finding whatever its name.
func describeRefusal(leg string, resp *http.Response, body []byte) error {
	trimmed := strings.TrimSpace(string(body))
	ct := resp.Header.Get("Content-Type")
	media, _, _ := mime.ParseMediaType(ct)

	var probe any
	looksJSON := json.Unmarshal(body, &probe) == nil

	if media == "text/html" || !looksJSON {
		// Name the vendor only when it identifies itself, as an aid — never as the test.
		hint := ""
		low := strings.ToLower(trimmed)
		switch {
		case strings.Contains(low, "just a moment"), strings.Contains(low, "challenges.cloudflare.com"):
			hint = " It looks like a Cloudflare MANAGED CHALLENGE (an interstitial that expects a browser to run JavaScript), which no HTTP client can pass."
		case strings.Contains(low, "error code: 1010"):
			hint = " It looks like Cloudflare error 1010, a refused client signature."
		}
		return fmt.Errorf("the %s leg was refused BEFORE REACHING THE SERVICE: %s with Content-Type %q, "+
			"and the body is not JSON.%s\n"+
			"🔴 THIS IS NOT A TOKEN OR PAYLOAD PROBLEM — the service answers JSON with a credential-free "+
			"message, so an HTML body means an intermediary answered and the request never arrived. Do not "+
			"go and check the token. Closing it needs a change in front of the service (an allow rule for "+
			"this endpoint or for this client's address range); `UserAgent` in this file records why a user "+
			"agent change is NOT the fix, with the measurements.\n"+
			"first %d byte(s) of the body: %s",
			leg, resp.Status, ct, hint, len(trimmed), truncate(trimmed, 300))
	}

	// The service's own refusal. Its messages are credential-free by design, so echoing one is safe
	// and is the only way a CI log says which rule rejected the push.
	return fmt.Errorf("the %s leg was refused BY THE SERVICE: %s: %s", leg, resp.Status, truncate(trimmed, 1000))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + fmt.Sprintf("… (%d more byte(s) elided; see `maxDiagnosticBody`)", len(s)-n)
}

// PushResult is what the ingest returns.
type PushResult struct {
	RunID string `json:"run_id"`
	URL   string `json:"url"`
}

// PushConfig is the four secrets, read from the environment. All four absent is the FORK-PR
// case and is a clean skip; some-but-not-all is a misconfiguration and is reported as one,
// because a half-configured push is the silent-green shape.
type PushConfig struct {
	PushURL   string
	PushToken string
	APIURL    string
	APIToken  string
}

// Complete answers whether a push can be attempted at all.
func (c PushConfig) Complete() bool {
	return c.PushURL != "" && c.PushToken != "" && c.APIURL != "" && c.APIToken != ""
}

// Missing names the absent variables, so a misconfiguration says WHICH one.
func (c PushConfig) Missing() []string {
	var out []string
	for name, v := range map[string]string{
		"CAIRN_AUDIT_PUSH_URL":   c.PushURL,
		"CAIRN_AUDIT_PUSH_TOKEN": c.PushToken,
		"CAIRN_AUDIT_API_URL":    c.APIURL,
		"CAIRN_AUDIT_API_TOKEN":  c.APIToken,
	} {
		if v == "" {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// Push builds the multipart body and POSTs it.
//
// ✅ THIS LEG IS EXERCISED: three real pushes, all 200, all `status: done`. The measured record —
// what the service accepted, and the prediction it corrected — is `README.md` residual 3, which is
// the canonical site; this comment points there rather than restating it.
//
// ⚠ WHAT IS STILL UNMEASURED IS THE REFUSAL PATH, AND THE ASYMMETRY IS THE POINT. Every push the
// service has seen was ACCEPTED, so none of the 400s `payload_test.go` asserts has ever come back
// from the server — those remain a claim about this module's copy of the server's rules. If the hub
// tightens one, those tests stay green and the push starts failing. Nor has a push gone out from
// CI rather than from a workstation, and no real body has come close to any cap.
func Push(ctx context.Context, cfg PushConfig, payload *PushPayload, files map[string][]byte) (*PushResult, error) {
	metaJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	if err := payload.Validate(files); err != nil {
		return nil, fmt.Errorf("refusing to push a malformed payload: %w", err)
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField("metadata", string(metaJSON)); err != nil {
		return nil, err
	}
	// The parts are written in sorted order so one run's body is byte-stable given the
	// same captures — Go's map iteration is randomised, and a body that reordered itself
	// would make a reproducibility question unanswerable.
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		// The form-NAME is the filename: that is how the server maps a metadata ref to a
		// part. Two different strings here would be an "uploaded file is not referenced"
		// refusal that reads as a bug in the metadata.
		fw, err := mw.CreateFormFile(n, n)
		if err != nil {
			return nil, err
		}
		if _, err := fw.Write(files[n]); err != nil {
			return nil, err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	if body.Len() > MaxBodyBytes {
		return nil, fmt.Errorf("the multipart body is %d bytes (cap %d)", body.Len(), MaxBodyBytes)
	}

	url := strings.TrimRight(cfg.PushURL, "/") + PushEndpoint
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.PushToken)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("User-Agent", UserAgent)

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxDiagnosticBody))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, describeRefusal("push", resp, respBody)
	}
	var res PushResult
	if err := json.Unmarshal(respBody, &res); err != nil {
		return nil, fmt.Errorf("push succeeded but the response was not understood: %w", err)
	}
	if res.RunID == "" {
		return nil, fmt.Errorf("push succeeded but returned no run_id, so the diff cannot be read back")
	}
	return &res, nil
}

// ReadRun fetches the ingested run's report.
//
// 🔴 READ IMMEDIATELY, NOT POLLED. The push computes the P2 diff SYNCHRONOUSLY and returns
// the run id only after it is done, so the report is already there. A polling loop would be
// waiting for something that has already happened, and the first thing it would hide is a
// diff that came back empty for a real reason.
func ReadRun(ctx context.Context, cfg PushConfig, runID string) (*RunReport, error) {
	url := strings.TrimRight(cfg.APIURL, "/") + RunEndpoint + runID
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIToken)
	// The same identification as the push leg, for the same reason — see [UserAgent]. Both legs
	// cross the same edge, so a rule written for one and not the other would half-work, which is
	// worse than neither.
	req.Header.Set("User-Agent", UserAgent)
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode != http.StatusOK {
		// ⚠ A 404 HERE MEANS "WRONG NAME", NOT "NO SUCH RUN", WHEN THE ADDRESS IS A TARGET
		// SPEC KEY — the hub's push spec key and a target's display name are separate
		// strings. This call addresses a run by UUID, which is not subject to that, so a
		// 404 on this path is a genuine miss.
		//
		// 🔴 AND THE SAME EDGE-VERSUS-SERVICE DISTINCTION APPLIES HERE. This leg runs immediately
		// after a push that just succeeded, so an HTML refusal on it would be even more misleading:
		// the obvious reading is "the read key is wrong" when the request never arrived.
		// `describeRefusal` caps the body too, which matters more on this leg than on the push —
		// the read cap is 16 MiB because a real report is large.
		return nil, fmt.Errorf("reading run %s: %w", runID, describeRefusal("read-back", resp, body))
	}
	var r RunReport
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("run %s's report was not understood: %w", runID, err)
	}
	r.raw = body
	return &r, nil
}

// RunReport is the part of the hub's report.json this harness reads back.
//
// It decodes LENIENTLY — no `DisallowUnknownFields` — in the opposite direction from the
// push. The push must match the server's schema exactly because the server refuses an
// unknown key; a READ must tolerate the server growing a field, or every hub release
// would break this job for no reason.
// 🔴 THE KEYS ARE THE HUB'S, COPIED FROM ITS `internal/report` RATHER THAN GUESSED. A
// mis-spelled key here decodes to a zero value and prints a reassuring empty diff block —
// indistinguishable from "nothing changed", which is the shape a silent-zero takes when the
// instrument is a struct tag. `TestTheRunReportDecodesThe hubsOwnKeys` pins them against a
// fixture built from those names.
type RunReport struct {
	RunID   string `json:"run_id"`
	Status  string `json:"status"`
	Label   string `json:"label,omitempty"`
	Summary struct {
		PagesCrawled   int `json:"pages_crawled"`
		A11yViolations int `json:"a11y_violations"`
		ConsoleFirst   int `json:"console_first_party"`
		ConsoleThird   int `json:"console_third_party"`
		NetworkFirst   int `json:"network_first_party"`
		NetworkThird   int `json:"network_third_party"`
	} `json:"summary"`

	// ⚠ `Diff` IS A POINTER BECAUSE THE FIRST RUN HAS NONE. The hub omits the key
	// entirely when there is no previous done run to compare against, and that absence is
	// exactly the day-one state this job must not gate on: an empty diff and a missing one
	// are different facts and the block printed below says which.
	Diff *struct {
		PrevRunID        string   `json:"prev_run_id"`
		PagesAdded       []string `json:"pages_added"`
		PagesRemoved     []string `json:"pages_removed"`
		PagesChanged     int      `json:"pages_changed"`
		PagesSizeChanged int      `json:"pages_size_changed"`

		NewA11yRules      []string `json:"new_a11y_rules"`
		ResolvedA11yRules []string `json:"resolved_a11y_rules,omitempty"`
		A11yDelta         int      `json:"a11y_delta"`
		ConsoleDelta      int      `json:"console_delta"`
		NetworkDelta      int      `json:"network_delta"`

		ChangedPages []struct {
			URL         string  `json:"url"`
			Viewport    string  `json:"viewport"`
			DiffPct     float64 `json:"diff_pct"`
			SizeChanged bool    `json:"size_changed"`
			NotCompared bool    `json:"not_compared,omitempty"`
		} `json:"changed_pages,omitempty"`
	} `json:"diff,omitempty"`

	raw []byte
}

// Raw is the report as it arrived, so a job log can carry the bytes when the decoded view
// disagrees with what a human expected.
func (r *RunReport) Raw() []byte { return r.raw }
