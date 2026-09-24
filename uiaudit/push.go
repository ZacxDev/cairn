package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
// ⚠ THIS LEG IS UNEXERCISED IN THIS CHANGE AND SAYING SO IS PART OF SHIPPING IT. Creating
// the hub's plugin target and minting the two keys are Supabase-gated operator steps,
// so no push has been sent from this harness to any server. What IS exercised is the
// payload's SHAPE, offline, against the server's own rules — refs↔parts integrity, no
// unknown fields, per-file and body caps, the closed finding-type set, the structured a11y
// detail and the non-empty-digest rule (`payload_test.go`). The wire leg is the part a
// reader must not read as verified.
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

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// The server's messages are credential-free by design, so echoing one is safe and
		// is the only way a CI log says which rule rejected the push.
		return nil, fmt.Errorf("push rejected: %s: %s", resp.Status, strings.TrimSpace(string(respBody)))
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
		return nil, fmt.Errorf("reading run %s: %s: %s", runID, resp.Status, strings.TrimSpace(string(body)))
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
