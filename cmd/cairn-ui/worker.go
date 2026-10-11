package main

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/redact"
	"github.com/ZacxDev/cairn/internal/transcript/archive"
	"github.com/ZacxDev/cairn/internal/worker"
)

// The worker listener and the transcript store (slice S3 of `claudedocs/plan-cairn-plugins.md`).
// Flags only, with NO default and NO environment spelling, for presence's reason: a manifest that
// copies another's environment must not switch a third listener on, and the line that does has to
// be a deliberate, reviewable one.
const (
	flagWorkerAddr          = "worker-addr"
	flagWorkerTokens        = "worker-tokens"
	flagWorkerOwner         = "worker-owner"
	flagTranscriptDir       = "transcript-dir"
	flagTranscriptRetention = "transcript-retention"
	flagTranscriptQuota     = "transcript-quota"
	flagArmCapture          = "arm-transcript-capture"
	flagIssueWorker         = "issue-worker-token"
	flagWorkerHost          = "worker-host"
)

// workerSettings is what the operator wrote.
type workerSettings struct {
	addr, tokens, owner   string
	dir, retention, quota string
	armed                 bool
	issue, host           string
}

// workerPlan is a validated, armed-or-not worker configuration.
type workerPlan struct {
	owner     worker.Owner
	dir       string
	retention time.Duration
	quota     int64
}

// workerListener decides whether the worker listener runs.
//
// 🔴 ALL SIX OR NONE. The address, the token file, the owner, the directory, the retention and
// the quota configure ONE thing together; a subset is a half-configuration and refuses, naming
// what is missing. Retention and quota are REQUIRED — decision 15: "nobody gets keep-forever by
// omission" — and the quota is required too, which the plan leaves optional: an unbounded store of
// gigabytes a week is the omission that rule is about, one flag along.
//
// 🔴 `-arm-transcript-capture` IS SEPARATE AND IS NOT IN THE SIX. Configuring the listener does not
// arm capture: with it unset every upload answers 503 (O16 — the arming gate fails today and the
// read ledger does not exist). Setting it without the listener refuses, because an arming line
// that arms nothing would read, to its author, as a deployment that captures.
func workerListener(s workerSettings, m control.Model, storeRoot string) (workerPlan, bool, error) {
	given := map[string]string{flagWorkerAddr: s.addr, flagWorkerTokens: s.tokens, flagWorkerOwner: s.owner,
		flagTranscriptDir: s.dir, flagTranscriptRetention: s.retention, flagTranscriptQuota: s.quota}
	order := []string{flagWorkerAddr, flagWorkerTokens, flagWorkerOwner, flagTranscriptDir, flagTranscriptRetention, flagTranscriptQuota}
	var missing []string
	for _, name := range order {
		v := given[name]
		if v == "" {
			missing = append(missing, "-"+name)
			continue
		}
		if identity.ValueReducesToNothing(v) {
			return workerPlan{}, false, fmt.Errorf("-%s is set to a value that reduces to nothing. Refusing to "+
				"start rather than reading it as unset; give it a value or remove the flag", name)
		}
	}
	if s.issue != "" {
		if s.addr != "" {
			return workerPlan{}, false, fmt.Errorf("-%s mints a token and exits; -%s starts a listener. "+
				"Refusing to do both in one run", flagIssueWorker, flagWorkerAddr)
		}
		return workerPlan{}, false, nil
	}
	if s.host != "" {
		return workerPlan{}, false, fmt.Errorf("-%s names the host a MINTED token is bound to and means "+
			"nothing without -%s. Refusing to start", flagWorkerHost, flagIssueWorker)
	}
	if len(missing) == len(order) {
		if s.armed {
			return workerPlan{}, false, fmt.Errorf("-%s is set but there is no worker listener (no -%s), so it "+
				"would arm nothing. Refusing to start", flagArmCapture, flagWorkerAddr)
		}
		return workerPlan{}, false, nil
	}
	if len(missing) > 0 {
		return workerPlan{}, false, fmt.Errorf("the worker listener needs -%s together; %s missing, so this is a "+
			"half-configuration. Refusing to start; set all six or none", strings.Join(order, ", -"), strings.Join(missing, " and "))
	}
	owner, err := worker.ParseOwner(s.owner)
	if err != nil {
		return workerPlan{}, false, fmt.Errorf("-%s: %v. Refusing to start", flagWorkerOwner, err)
	}
	if _, known := m.PrincipalFor(owner.Kind, owner.ID); !known {
		return workerPlan{}, false, fmt.Errorf("-%s %s names a principal this authority does not hold. Refusing to start",
			flagWorkerOwner, owner)
	}
	if _, _, err := net.SplitHostPort(s.addr); err != nil {
		return workerPlan{}, false, fmt.Errorf("-%s %q is not host:port (%v). Refusing to start", flagWorkerAddr, s.addr, err)
	}
	retention, err := parseRetention(s.retention)
	if err != nil {
		return workerPlan{}, false, fmt.Errorf("-%s: %v. Refusing to start", flagTranscriptRetention, err)
	}
	quota, err := parseQuota(s.quota)
	if err != nil {
		return workerPlan{}, false, fmt.Errorf("-%s: %v. Refusing to start", flagTranscriptQuota, err)
	}
	dir, err := archive.ResolveDir(storeRoot, s.dir)
	if err != nil {
		return workerPlan{}, false, fmt.Errorf("-%s: %v. Refusing to start", flagTranscriptDir, err)
	}
	return workerPlan{owner: owner, dir: dir, retention: retention, quota: quota}, true, nil
}

var daysForm = regexp.MustCompile(`^([1-9][0-9]{0,4})d$`)

// parseRetention reads `90d` or a Go duration (`2160h`). It must be positive.
func parseRetention(v string) (time.Duration, error) {
	if m := daysForm.FindStringSubmatch(v); m != nil {
		n, _ := strconv.Atoi(m[1])
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%q is not a positive duration (`90d`, or a Go duration such as `2160h`)", v)
	}
	return d, nil
}

var quotaForm = regexp.MustCompile(`^([1-9][0-9]{0,15})(B|KiB|MiB|GiB|TiB|KB|MB|GB|TB)?$`)

// parseQuota reads a positive byte count with an optional unit: `20GB` is 20×10⁹, `20GiB` 20×2³⁰.
func parseQuota(v string) (int64, error) {
	m := quotaForm.FindStringSubmatch(v)
	if m == nil {
		return 0, fmt.Errorf("%q is not a positive byte count (`20GB`, `20GiB`, `500MB`, or plain bytes)", v)
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is out of range", v)
	}
	mult := map[string]int64{"": 1, "B": 1, "KB": 1e3, "MB": 1e6, "GB": 1e9, "TB": 1e12,
		"KiB": 1 << 10, "MiB": 1 << 20, "GiB": 1 << 30, "TiB": 1 << 40}[m[2]]
	if n > (1<<62)/mult {
		return 0, fmt.Errorf("%q is out of range", v)
	}
	return n * mult, nil
}

// workerBindRefusal is `cairn-ui`'s reachable-bind refusal applied to the worker listener's OWN
// bind (decision 8), for presence's reason: a separate bind is a separate reachability question.
func workerBindRefusal(addr string, proxyErr error) error {
	return listenerBindRefusal("worker listener", flagWorkerAddr, addr, proxyErr)
}

// openTranscriptArchive builds the store with the pod's OWN redactor for the refusing re-check.
//
// 🔴 THE KEY IS FRESH PER PROCESS AND NEVER THE HOST'S. The re-check reads only whether a rule
// matched; a tag it computes is never stored or shown, so the key needs no persistence, and a pod
// that held the host key could confirm a guessed secret against a stored tag (T15).
func openTranscriptArchive(p workerPlan) (*archive.Archive, error) {
	key := make([]byte, redact.HostKeyBytes)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	red, err := redact.New(key, nil)
	if err != nil {
		return nil, err
	}
	return archive.Open(archive.Config{Dir: p.dir, Retention: p.retention, Quota: p.quota, Recheck: red})
}

// armedWarning is printed when capture is armed. 🔴 It names every precondition the plan sets,
// because the flag is the operator's assertion that they hold and nothing in this binary can check
// them: O15's held-back gate for THIS redactor commit, S11's read ledger, and the pod-side `V`
// this build does not derive.
const armedWarning = "cairn-ui: WARNING transcript capture is ARMED (-" + flagArmCapture + "). The plan arms NO " +
	"instance until (1) O15's arming gate passes on a FRESH auditor-written held-back set for the redactor commit " +
	"being armed (≥ 90% of leaks caught, ≤ 15% of clean lines damaged) and (2) S11's client read ledger has " +
	"landed; and this build does not derive a session's scopes itself, so every stored session is OWNER-ONLY. " +
	"Arming is your assertion that (1) and (2) hold"

// workerMode is the startup line's clause about the third listener and capture.
func workerMode(addr string, owner worker.Owner, rows int, dir string, armed bool) string {
	state := "capture DISARMED (no -" + flagArmCapture + ": every upload answers 503)"
	if armed {
		state = "capture ARMED"
	}
	return fmt.Sprintf("worker listener on %s (sole capture owner %s, %d token row(s), %d route(s)), transcripts in %s, %s",
		addr, owner, rows, len(worker.Routes()), dir, state)
}

// issueWorkerToken mints ONE capture token, appends its DIGEST to the token file, and prints the
// token once to `out`. The wall holds at mint: a file with another owner's rows refuses.
func issueWorkerToken(out, note io.Writer, m control.Model, s workerSettings) error {
	kind := worker.TokenKind(s.issue)
	if !kind.Valid() {
		return fmt.Errorf("-%s %q is not %q", flagIssueWorker, s.issue, worker.KindCapture)
	}
	if s.tokens == "" || identity.ValueReducesToNothing(s.tokens) {
		return fmt.Errorf("-%s needs -%s, the file the digest is appended to", flagIssueWorker, flagWorkerTokens)
	}
	if s.owner == "" {
		return fmt.Errorf("-%s needs -%s (<kind>:<id>)", flagIssueWorker, flagWorkerOwner)
	}
	if !worker.ValidHostLabel(s.host) {
		return fmt.Errorf("-%s needs -%s, the ONE host label the token is bound to (got %q)", flagIssueWorker, flagWorkerHost, s.host)
	}
	owner, err := worker.ParseOwner(s.owner)
	if err != nil {
		return fmt.Errorf("-%s: %v", flagWorkerOwner, err)
	}
	if _, known := m.PrincipalFor(owner.Kind, owner.ID); !known {
		return fmt.Errorf("-%s %s names a principal this authority does not hold", flagWorkerOwner, owner)
	}
	if _, statErr := os.Stat(s.tokens); statErr == nil {
		if _, err := worker.LoadTokens(s.tokens, owner); err != nil {
			return fmt.Errorf("refusing to mint into %s: %w", s.tokens, err)
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("the worker token file %s cannot be checked: %w", s.tokens, statErr)
	}
	token, err := worker.MintToken()
	if err != nil {
		return err
	}
	row := worker.NewTokenRow(kind, owner, s.host, token)
	if err := worker.AppendTokenRow(s.tokens, row); err != nil {
		return fmt.Errorf("the worker token file %s could not be written: %w", s.tokens, err)
	}
	fmt.Fprintf(note, "cairn-ui: minted a %s worker token for %s on host %s; its digest %s… is in %s. The token "+
		"is printed ONCE on stdout and stored nowhere\n", kind, owner, s.host, row.DigestPrefix(), s.tokens)
	_, err = fmt.Fprintln(out, token)
	return err
}

// sweepEvery is how often the retention sweep runs (and once at startup).
const sweepEvery = time.Hour

func runRetentionSweep(stop <-chan struct{}, arc *archive.Archive, log func(string)) {
	sweep := func() {
		deleted, err := arc.Sweep()
		if err != nil {
			log("cairn-ui: the transcript retention sweep failed: " + err.Error())
		}
		if len(deleted) > 0 {
			log(fmt.Sprintf("cairn-ui: the transcript retention sweep deleted %d session(s)", len(deleted)))
		}
	}
	sweep()
	t := time.NewTicker(sweepEvery)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			sweep()
		}
	}
}
