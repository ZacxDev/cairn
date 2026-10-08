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

// worstCaseLegalPush is the size, in bytes, of the LARGEST body Go's default `json.Marshal` produces
// for a push the decoder accepts. Pinned HERE ONLY, beside the fixture that measures it:
// `MaxPushBody`'s comment and `internal/ui/README.md` point at this test rather than repeat the
// number, so no comment can drift from it.
//
// ⚠ IT WAS 638,245 AND THAT WAS NOT THE WORST CASE. The first fixture spelled `last_activity` as a
// 35-byte RFC 3339 value and the host as the 6-byte `host-a`; but `last_activity` is bounded by the
// 128-byte string bound only, `time.Parse` accepts a fractional second of any length, and a host
// label may be 64 bytes. S2's round-2 audit measured a legal 662,111-byte push; this fixture, built
// field by field at every bound, measures the same 662,111.
const worstCaseLegalPush = 662_111

// TestAWorstCaseLegalPushIsAccepted builds the LARGEST legal push field by field and proves the body
// cap admits it. Each field is at its own bound, spelled as the bytes `json.Marshal` expands most:
//
//   - 256 rows (MaxRows), each with a distinct 64-byte session (the trailer grammar's bound);
//   - `runtime` "opencode", the longest of the closed set;
//   - `target`, `label` and `hotkey` each 128 bytes (MaxStringBytes) of `<`, which `json.Marshal`
//     escapes to six bytes (`<`) — the largest expansion an ACCEPTED character gets: `>` and `&`
//     tie it, U+2028/U+2029 (also six) are refused by `checkString`, and every other accepted rune
//     marshals as itself;
//   - `last_activity` 128 bytes: RFC 3339 with a 102-digit fractional second (it cannot carry a byte
//     `json.Marshal` escapes, so its worst is its length);
//   - a 64-byte host label (`hostLabel`'s bound), on a token bound to that host.
//
// The size is asserted EXACTLY ([worstCaseLegalPush]) and each field's bound is asserted on the
// fixture, so a fixture that drifts below a bound fails rather than measuring a smaller case.
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
	lastActivity := "2000-01-02T03:04:05." + strings.Repeat("1", MaxStringBytes-len("2000-01-02T03:04:05.+01:00")) + "+01:00"
	host := "h" + strings.Repeat("x", 63)
	longest := ""
	for _, rt := range Runtimes {
		if len(rt) > len(longest) {
			longest = rt
		}
	}
	if len(lastActivity) != MaxStringBytes || !ValidHostLabel(host) || ValidHostLabel(host+"x") || longest != "opencode" {
		t.Fatalf("INSTRUMENT: the fixture is not at every bound (last_activity %d bytes, host %d bytes, runtime %q)",
			len(lastActivity), len(host), longest)
	}
	rows := make([]wr, MaxRows)
	for i := range rows {
		rows[i] = wr{Session: fmt.Sprintf("s-%062d", i), Runtime: longest, Target: worst, Label: worst,
			Hotkey: worst, LastActivity: lastActivity}
		if len(rows[i].Session) != 64 {
			t.Fatalf("INSTRUMENT: session %q is not at the 64-byte bound", rows[i].Session)
		}
	}
	body, err := json.Marshal(map[string]any{"schema": 1, "host": host, "rows": rows})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("worst-case legal push: %d bytes (cap %d)", len(body), MaxPushBody)
	if len(body) != worstCaseLegalPush {
		t.Fatalf("the worst-case legal push is %d bytes, but worstCaseLegalPush says %d",
			len(body), worstCaseLegalPush)
	}
	const pushLong = "fixture-presence-push-token-owner-a-long-host-not-real"
	g := newRig(t, nil, append(standardRows(), NewTokenRow(KindPush, ownerA, host, pushLong))...)
	if rec := g.do(http.MethodPost, PushPath, pushLong, string(body), peer1); rec.Code != http.StatusOK {
		t.Fatalf("a worst-case LEGAL push answered %d: %s", rec.Code, rec.Body.String())
	}
	// NEGATIVE CONTROL: one byte more in ONE row's `last_activity` is over the string bound, so the
	// fixture above sits ON the bound rather than somewhere under it.
	rows[0].LastActivity = "2000-01-02T03:04:05." + strings.Repeat("1", MaxStringBytes-len("2000-01-02T03:04:05.+01:00")+1) + "+01:00"
	over, _ := json.Marshal(map[string]any{"schema": 1, "host": host, "rows": rows})
	if rec := g.do(http.MethodPost, PushPath, pushLong, string(over), peer1); rec.Code != http.StatusBadRequest {
		t.Fatalf("a push one byte over the last_activity bound answered %d, want 400", rec.Code)
	}
}
