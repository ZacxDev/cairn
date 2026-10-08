package presence

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/netid"
)

// TestAMintIntoAFileWithoutATrailingNewlineKeepsBothRows: a hand-edited token file whose last
// line has no `\n` must not have the next minted row glued onto it — that corrupts BOTH rows
// (the existing token 401s, the new one too, and the next startup refuses the file).
func TestAMintIntoAFileWithoutATrailingNewlineKeepsBothRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens")
	first := NewTokenRow(KindPush, ownerA, "host-a", pushA)
	if err := os.WriteFile(path, []byte(first.String()), 0o600); err != nil { // NO trailing newline
		t.Fatal(err)
	}
	if err := AppendTokenRow(path, NewTokenRow(KindClaim, ownerA, "host-a", claimA)); err != nil {
		t.Fatal(err)
	}
	if rows, err := LoadTokens(path, ownerA); err != nil || len(rows) != 2 {
		t.Fatalf("after appending to a file with no trailing newline: %d rows, %v", len(rows), err)
	}
	g := &agentRig{t: t, clk: &fakeClock{now: clock0}, tokens: path}
	g.svc = &Service{Store: &Store{Now: g.clk.Now}, Queue: &Queue{Now: g.clk.Now}}
	a, _, err := NewAgent(AgentConfig{TokenFile: path, Owner: ownerA, Service: g.svc,
		Limiter: netid.NewRateLimiter(1000, time.Minute, time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	g.agent = a
	if rec := g.push(pushA, "host-a", "s-0001"); rec.Code != http.StatusOK {
		t.Fatalf("the pre-existing token answered %d", rec.Code)
	}
	if rec, _ := g.claim(claimA); rec.Code != http.StatusOK {
		t.Fatalf("the newly minted token answered %d", rec.Code)
	}
}

// TestATokenInTheWrongColumnIsNeverEchoed: a raw token pasted into columns 1–3 makes the row
// malformed, and the refusal — which reaches a log — names the line and the FIELD, never the value.
func TestATokenInTheWrongColumnIsNeverEchoed(t *testing.T) {
	const secret = "fixture-a-raw-token-pasted-into-the-wrong-column-not-real"
	digest := NewTokenRow(KindPush, ownerA, "host-a", pushA).Digest
	for col, want := range map[int]string{0: "field 1 (kind)", 1: "field 2 (owner)", 2: "field 3 (host)"} {
		fields := []string{"push", ownerA.String(), "host-a", digest}
		fields[col] = secret
		if col == 2 {
			fields[col] = secret + "/x" // token-shaped, and not a host label
		}
		path := filepath.Join(t.TempDir(), "tokens")
		if err := os.WriteFile(path, []byte(strings.Join(fields, " ")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := LoadTokens(path, ownerA)
		if err == nil {
			t.Fatalf("column %d: a token there was accepted", col+1)
		}
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("column %d: the refusal echoes the value: %v", col+1, err)
		}
		if !strings.Contains(err.Error(), "line 1") || !strings.Contains(err.Error(), want) {
			t.Fatalf("column %d: the refusal does not name the line and %q: %v", col+1, want, err)
		}
	}
}

// TestAWorstCaseLegalPushIsAccepted: 256 rows with every free-text string at its 128-byte bound,
// spelled as `<` — which Go's default `json.Marshal` escapes to six bytes (`<`) — is LEGAL,
// so the body cap must admit it. Measured here rather than asserted in a comment.
func TestAWorstCaseLegalPushIsAccepted(t *testing.T) {
	type wr struct {
		Session      string `json:"session"`
		Runtime      string `json:"runtime"`
		Target       string `json:"target"`
		Label        string `json:"label"`
		Hotkey       string `json:"hotkey"`
		LastActivity string `json:"last_activity"`
	}
	worst := strings.Repeat("<", MaxStringBytes)
	rows := make([]wr, MaxRows)
	for i := range rows {
		rows[i] = wr{Session: fmt.Sprintf("s-%062d", i), Runtime: "opencode", Target: worst, Label: worst,
			Hotkey: worst, LastActivity: "2000-01-02T03:04:05.123456789+01:00"}
	}
	body, err := json.Marshal(map[string]any{"schema": 1, "host": "host-a", "rows": rows})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("worst-case legal push: %d bytes (cap %d)", len(body), MaxPushBody)
	if len(body) <= 512<<10 {
		t.Fatalf("the fixture is %d bytes — not over the old 512 KiB cap, so it measures nothing", len(body))
	}
	g := newRig(t, nil, standardRows()...)
	if rec := g.do(http.MethodPost, PushPath, pushA, string(body), peer1); rec.Code != http.StatusOK {
		t.Fatalf("a worst-case LEGAL push answered %d: %s", rec.Code, rec.Body.String())
	}
}
