package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// 🔴 A TRANSCRIPT (S4 of the transcripts plan): THE WALK'S ONE IDENTITY OWNS A STORED SESSION, SO THE
// SESSION PAGE IS CAPTURED WITH ITS TRANSCRIPT SECTION — the collapsed reading, the reveals, a
// nested subagent — and the raw-record page it links. Without it every `/session` capture renders
// the no-transcript page, which is byte-identical to a deployment with no transcript store and so
// says nothing about the section's layout, its overflow at 390 px, or its touch targets.
//
// Everything goes through flags and routes that already ship, `presence.go`'s rule: the capture
// token is minted by the binary's own `-issue-worker-token`, the store is opened by the six worker
// flags, capture is armed by its own flag, and the records arrive through the real upload route —
// so the pod's refusing re-check and its own scope derivation run over them, as in production.
//
// ⚠ ONLY THE OWNER HALF, for presence's structural reason: the worker listener admits ONE capture
// owner, and the walk signs in as one identity. "A refused viewer gets the bytes of a session with
// no transcript" is measured in-process by `internal/ui` and end to end by `tests/plugins/e2e.sh`.
//
// The session is `fixtureMembers`' — one the fixture store already carries — and every record is
// synthetic text written here.

type fixtureTranscript struct {
	tokens, owner, token string
	workerAddr, dir      string
}

// mintCapture mints ONE capture token for `owner` on `host-a` with the binary under audit.
func mintCapture(ctx context.Context, uiBinary, dir, store, tokenFile, owner string) (*fixtureTranscript, error) {
	tokens := filepath.Join(dir, "worker-tokens")
	cmd := exec.CommandContext(ctx, uiBinary,
		"-store", store,
		"-token-file", tokenFile,
		"-session-file", filepath.Join(dir, "sessions.json"),
		"-issue-worker-token", "capture",
		"-worker-owner", owner,
		"-worker-host", "host-a",
		"-worker-tokens", tokens,
	)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("minting the fixture capture token: %w\n%s", err, stderr.String())
	}
	token := strings.TrimSpace(string(out))
	if token == "" {
		return nil, fmt.Errorf("the capture-token mint printed nothing")
	}
	// The store gets a directory of its OWN: `cairn-ui` refuses one that holds any of its state.
	tdir := filepath.Join(dir, "transcripts")
	if err := os.MkdirAll(tdir, 0o700); err != nil {
		return nil, err
	}
	return &fixtureTranscript{tokens: tokens, owner: owner, token: token, dir: tdir}, nil
}

// flags are the `cairn-ui` arguments that open the transcript store and arm capture.
func (f *fixtureTranscript) flags() []string {
	return []string{
		"-worker-addr", f.workerAddr,
		"-worker-tokens", f.tokens,
		"-worker-owner", f.owner,
		"-transcript-dir", f.dir,
		"-transcript-retention", "90d",
		"-transcript-quota", "1GB",
		"-arm-transcript-capture",
	}
}

// upload stores the synthetic session through the real upload route: a main stream that carries
// every kind of unit decision 18 renders differently, and the subagent one of its calls spawned.
func (f *fixtureTranscript) upload(ctx context.Context, session string) error {
	text := func(role, s string) map[string]any {
		if role == "user" {
			return map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": s}}
		}
		return map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant",
			"content": []any{map[string]any{"type": "text", "text": s}}}}
	}
	call := func(id, name string, input map[string]any) map[string]any {
		return map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant",
			"content": []any{map[string]any{"type": "tool_use", "id": id, "name": name, "input": input}}}}
	}
	result := func(id, content string, isError bool, duplicate map[string]any) map[string]any {
		block := map[string]any{"type": "tool_result", "tool_use_id": id, "content": content}
		if isError {
			block["is_error"] = true
		}
		return map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": []any{block}},
			"toolUseResult": duplicate}
	}
	// One token with no break opportunity, longer than a phone is wide: the overflow refusal must
	// be able to see a transcript line that cannot wrap on its own.
	unbroken := strings.Repeat("synthetic", 12)
	main := []map[string]any{
		{"type": "permission-mode", "permissionMode": "default"},
		text("user", "Please tidy the gadget notes and say what changed.\n\nKeep the widget section as it is: "+unbroken),
		text("assistant", strings.Repeat("The gadget notes were tidied one paragraph at a time, and each change is listed below. ", 24)),
		{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "thinking", "thinking": "The widget section stays; only the gadget notes move."},
			map[string]any{"type": "tool_use", "id": "toolu_uiaudit01", "name": "Bash", "input": map[string]any{"command": "ls notes/" + unbroken}},
		}}},
		result("toolu_uiaudit01", "gadget.md\nwidget.md\n", false, map[string]any{"stdout": "gadget.md\nwidget.md\n", "stderr": ""}),
		call("toolu_uiaudit02", "Bash", map[string]any{"command": "false"}),
		result("toolu_uiaudit02", "Exit code 1", true, map[string]any{"stdout": "", "stderr": ""}),
		call("toolu_uiaudit03", "Agent", map[string]any{"description": "count the gadgets", "subagent_type": "general-purpose",
			"prompt": "Count the gadgets in the notes."}),
		result("toolu_uiaudit03", "There are seven gadgets.", false, map[string]any{"agentId": "uiaudit01"}),
		{"type": "system", "subtype": "compact_boundary", "content": "Conversation compacted"},
		{"type": "user", "isCompactSummary": true, "message": map[string]any{"role": "user",
			"content": "This session is being continued from an earlier one that tidied the gadget notes."}},
		{"type": "ai-title", "aiTitle": "tidy the gadget notes"},
		text("user", "Thanks. Anything left?"),
		text("assistant", "Nothing is left: the gadget notes are tidy and the widget section is untouched."),
	}
	sub := []map[string]any{
		{"isSidechain": true, "agentId": "uiaudit01", "type": "user", "message": map[string]any{"role": "user",
			"content": "Count the gadgets in the notes."}},
		{"isSidechain": true, "agentId": "uiaudit01", "type": "assistant", "message": map[string]any{"role": "assistant",
			"content": []any{map[string]any{"type": "text", "text": "There are seven gadgets."}}}},
	}
	for _, s := range []struct {
		stream  string
		records []map[string]any
	}{{"main", main}, {"subagent:uiaudit01", sub}} {
		recs := make([]map[string]any, len(s.records))
		for i, r := range s.records {
			recs[i] = map[string]any{"src": fmt.Sprint(i), "rec": r}
		}
		body, err := json.Marshal(map[string]any{"schema": 1, "runtime": "claude", "host": "host-a", "from": "",
			"to": fmt.Sprint(len(recs)), "declared_scopes": []string{}, "records": recs})
		if err != nil {
			return err
		}
		target := "http://" + f.workerAddr + "/capture/v1/sessions/" + url.PathEscape(session) + "/streams/" +
			url.PathEscape(s.stream) + "/records"
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+f.token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
		if err != nil {
			return fmt.Errorf("the fixture transcript upload (%s): %w", s.stream, err)
		}
		got, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("the fixture transcript upload (%s) answered %s %q, want 200", s.stream, resp.Status, got)
		}
	}
	return nil
}
