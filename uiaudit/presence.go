package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZacxDev/cairn/internal/presence"
)

// 🔴 PRESENCE (S4 of the arcs/presence plan): THE WALK'S ONE IDENTITY OWNS A LIVE PANE, SO THE
// BADGED STATE OF EVERY SURFACE IS WHAT GETS CAPTURED — the session page's pane badge, the session
// rows on the scope page's and the arc page's sessions tabs, and "live pane" on `/arcs` and `/arc`.
// Without it the walk would only ever render those pages as presence-off, which is byte-identical to
// a deployment with no presence and so says nothing about the badge's layout or contrast.
//
// Everything goes through flags and routes that already ship, `boot.go`'s rule: the token is minted
// by the binary's own `-issue-presence-token` (which resolves `fixtureIdentity` to its principal, so
// no owner id is derived here), the listener is armed by the three presence flags, and the row
// arrives through the real agent route.
//
// ⚠ ONLY THE OWNER HALF, AND THAT IS STRUCTURAL RATHER THAN SKIPPED. A non-owner fixture cannot be
// built against the deployed binary: the single-owner wall refuses any token row for another owner
// (the listener does not start), so a second owner's presence can never reach the store, and the
// walk signs in as ONE identity anyway. "A non-owner sees bytes identical to presence never having
// existed" is measured in-process by `internal/ui`'s `TestPresenceIsInvisibleToEveryoneButItsOwner`.
//
// The values are the plan's sanctioned synthetic ones (`host-a`, `notes:3`, `Alt+n`); the session is
// `fixtureMembers`' — a session the fixture store already carries — so no session id is invented.

// presenceRePush is how often the walk re-pushes, so the row never reaches the 3-minute TTL however
// long the walk runs — the host side's own cadence (the plan's S3 timer is 60 s).
const presenceRePush = 60 * time.Second

type fixturePresence struct {
	tokens, owner, token string
	agentURL, session    string
}

// mintPresence mints ONE push token for `fixtureIdentity` on `host-a` with the binary under audit,
// and reads the resolved owner back out of the row it appended.
func mintPresence(ctx context.Context, uiBinary, dir, store, tokenFile string) (*fixturePresence, error) {
	tokens := filepath.Join(dir, "presence-tokens")
	cmd := exec.CommandContext(ctx, uiBinary,
		"-store", store,
		"-token-file", tokenFile,
		"-session-file", filepath.Join(dir, "sessions.json"),
		"-issue-presence-token", string(presence.KindPush),
		"-presence-owner", fixtureIdentity,
		"-presence-host", "host-a",
		"-presence-tokens", tokens,
	)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("minting the fixture presence token: %w\n%s", err, stderr.String())
	}
	token := strings.TrimSpace(string(out))
	raw, err := os.ReadFile(tokens)
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(string(raw))
	if token == "" || len(fields) != 4 {
		return nil, fmt.Errorf("the mint printed %d byte(s) and wrote %d field(s); want one token and one row", len(token), len(fields))
	}
	owner, err := presence.ParseOwner(fields[1])
	if err != nil {
		return nil, fmt.Errorf("the minted row's owner: %w", err)
	}
	return &fixturePresence{tokens: tokens, owner: owner.String(), token: token}, nil
}

// push replaces host-a's set with the one fixture row, through the real agent route.
func (p *fixturePresence) push(ctx context.Context) error {
	body := `{"schema":1,"host":"host-a","rows":[{"session":"` + p.session + `","runtime":"claude",` +
		`"target":"notes:3","label":"notes","hotkey":"Alt+n","last_activity":""}]}`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.agentURL+presence.PushPath, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("the fixture presence push: %w", err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if resp.StatusCode != http.StatusOK || string(got) != "rows=1" {
		return fmt.Errorf("the fixture presence push answered %s %q, want 200 \"rows=1\"", resp.Status, got)
	}
	return nil
}

// keepPushing re-pushes every [presenceRePush] until `ctx` ends. A failed re-push is printed, not
// fatal: the row it would have refreshed is still live for the rest of its TTL.
func (p *fixturePresence) keepPushing(ctx context.Context) {
	t := time.NewTicker(presenceRePush)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := p.push(ctx); err != nil && ctx.Err() == nil {
				fmt.Fprintln(os.Stderr, "uiaudit: WARNING "+err.Error())
			}
		}
	}
}

// aFreeLoopbackPort asks the kernel for a free loopback port for the agent listener. The window
// between this close and the pod's bind is `refusePortInUse`'s accepted window.
func aFreeLoopbackPort() (int, error) {
	ln, err := net.Listen("tcp", net.JoinHostPort(bindHost, "0"))
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}
