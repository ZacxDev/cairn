package main

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ZacxDev/cairn/internal/arcs"
	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/plugins"
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
	// plugin is the plugin a minted `plugin` token is bound to; registry is `-plugin-registry`, which
	// a plugin mint reads to refuse a name nobody registered.
	plugin, registry string
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
	if s.plugin != "" {
		return workerPlan{}, false, fmt.Errorf("-%s names the plugin a MINTED token is bound to and means "+
			"nothing without -%s plugin. Refusing to start", flagWorkerPlugin, flagIssueWorker)
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

// statePath is one file `cairn-ui` itself reads or writes, named by what sets it as an operator
// spells it: `-<flag>`, or `$<VARIABLE>` for a file configured by the environment alone.
type statePath struct{ flag, path string }

// stateInsideTranscriptDir refuses a transcript directory that would HOLD one of `cairn-ui`'s own
// state files (AP1, the half `archive.CheckOwnership` cannot see).
//
// 🔴 THE OWNERSHIP CHECK READS WHAT IS IN THE DIRECTORY AT STARTUP; THIS READS WHAT WILL BE. A
// file this process creates LATER — an arc journal on its first write, a session table on a fresh
// volume — is absent when the store opens, so the ownership check passes, the file appears, and the
// NEXT restart is refused: the outage AP1 names, one restart later. Every path is resolved the way
// the directory was (symlinks followed, through the nearest existing parent), so a symlinked
// spelling of the same place is caught.
func stateInsideTranscriptDir(dir string, paths []statePath) error {
	for _, p := range paths {
		if p.path == "" {
			continue
		}
		resolved, err := resolveMaybeAbsent(p.path)
		if err != nil {
			return fmt.Errorf("%s %s does not resolve (%v), so it cannot be checked against -%s", p.flag, p.path, err, flagTranscriptDir)
		}
		if arcs.Inside(dir, resolved) {
			return fmt.Errorf("%s %s resolves INSIDE -%s %s. The transcript store owns that directory whole — its "+
				"retention sweep journals into it and its open refuses anything it did not write — so `cairn-ui`'s own "+
				"state there is a refused restart waiting to happen. Give the transcripts a directory of their own",
				p.flag, p.path, flagTranscriptDir, dir)
		}
	}
	return nil
}

// resolveMaybeAbsent is `EvalSymlinks` for a path that may not exist yet: the nearest existing
// ancestor is resolved and the rest re-joined.
func resolveMaybeAbsent(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	var rest []string
	for cur := abs; ; {
		r, err := filepath.EvalSymlinks(cur)
		if err == nil {
			return filepath.Join(append([]string{r}, rest...)...), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return abs, nil
		}
		rest = append([]string{filepath.Base(cur)}, rest...)
		cur = parent
	}
}

// ownerStillHeld is the per-request owner check `worker.Config.OwnerHeld` asks (AP3): the SAME
// predicate `workerListener` applies at startup, over the authority's CURRENT model.
func ownerStillHeld(authority interface{ Model() control.Model }, owner worker.Owner) func() bool {
	return func() bool {
		_, known := authority.Model().PrincipalFor(owner.Kind, owner.ID)
		return known
	}
}

// armedWarning is printed when capture is armed. 🔴 It names what the plan still asks and what the
// operator WAIVED, because the flag is the operator's assertion and nothing in this binary can
// check it: S11's read ledger — without which this build's own derivation of a session's read
// scopes marks owner-only every session whose tool input names `cairn` or the cache directory, as
// well as any carrying a header that names no scope — and, waived by
// operator decision O17, not passed, O15's held-back gate, so this redactor is NOT measured to
// that bar.
const armedWarning = "cairn-ui: WARNING transcript capture is ARMED (-" + flagArmCapture + "). O15's arming gate " +
	"(≥ 90% of leaks caught on a fresh held-back set) was WAIVED by operator decision O17, not passed: this " +
	"redactor is not measured to that bar, so a secret it misses is stored. The plan also asks that S11's client " +
	"read ledger has landed: this build holds no ledger, so a stored session whose tool input names cairn or its " +
	"cache directory, or that carries a read header naming no scope, is visible to its OWNER only. Arming is your " +
	"assertion that you accept both"

// workerMode is the startup line's clause about the third listener and capture.
func workerMode(addr string, owner worker.Owner, rows int, dir string, armed bool) string {
	state := "capture DISARMED (no -" + flagArmCapture + ": every upload answers 503)"
	if armed {
		state = "capture ARMED"
	}
	return fmt.Sprintf("worker listener on %s (sole capture owner %s, %d token row(s), %d route(s)), transcripts in %s, %s",
		addr, owner, rows, len(worker.Routes()), dir, state)
}

// issueWorkerToken mints ONE worker token — a `capture` token bound to `-worker-owner` on
// `-worker-host`, or a `plugin` token bound to ONE plugin `-plugin-registry` holds — appends its
// DIGEST to the token file, and prints the token once to `out`. The wall holds at mint: a file with
// another owner's rows, or a row for an unregistered plugin, refuses.
func issueWorkerToken(out, note io.Writer, m control.Model, s workerSettings) error {
	kind := worker.TokenKind(s.issue)
	if !kind.Valid() {
		return fmt.Errorf("-%s %q is not %q or %q", flagIssueWorker, s.issue, worker.KindCapture, worker.KindPlugin)
	}
	if s.tokens == "" || identity.ValueReducesToNothing(s.tokens) {
		return fmt.Errorf("-%s needs -%s, the file the digest is appended to", flagIssueWorker, flagWorkerTokens)
	}
	if s.owner == "" {
		return fmt.Errorf("-%s needs -%s (<kind>:<id>): the token file's capture rows are checked against it", flagIssueWorker, flagWorkerOwner)
	}
	owner, err := worker.ParseOwner(s.owner)
	if err != nil {
		return fmt.Errorf("-%s: %v", flagWorkerOwner, err)
	}
	if _, known := m.PrincipalFor(owner.Kind, owner.ID); !known {
		return fmt.Errorf("-%s %s names a principal this authority does not hold", flagWorkerOwner, owner)
	}
	// 🔴 THE FILE'S EXISTING PLUGIN ROWS ARE ASKED OF THE REGISTRY WHEN ONE IS NAMED, AND ADMITTED
	// WHEN NONE IS — whichever kind is being minted. A capture mint with a nil wall refused every file
	// that already held a plugin row (#S5 audit round 1), leaving hand-editing as the only way to add a
	// capture token beside one. Without a registry this mint does not vouch for plugin rows, and SAYS
	// so; startup refuses a file holding a plugin row its registry does not hold (or any plugin row, with
	// no registry), so an unchecked row is found at the next restart at the latest.
	wall := worker.Wall{Owner: owner, Plugins: func(string) bool { return true }}
	if s.registry != "" {
		reg, err := plugins.LoadRegistry(s.registry)
		if err != nil {
			return fmt.Errorf("-%s: %v", flagPluginRegistry, err)
		}
		wall.Plugins = reg.Holds
	}
	var row worker.TokenRow
	token, err := worker.MintToken()
	if err != nil {
		return err
	}
	if kind == worker.KindPlugin {
		if s.host != "" {
			return fmt.Errorf("-%s binds a CAPTURE token to a host; a plugin token is bound to a plugin (-%s)", flagWorkerHost, flagWorkerPlugin)
		}
		if s.registry == "" {
			return fmt.Errorf("-%s plugin needs -%s: a token for a plugin nobody registered authenticates nothing", flagIssueWorker, flagPluginRegistry)
		}
		if !wall.Plugins(s.plugin) {
			return fmt.Errorf("-%s plugin needs -%s naming a plugin -%s registers (got %q)", flagIssueWorker, flagWorkerPlugin, flagPluginRegistry, s.plugin)
		}
		row = worker.NewPluginTokenRow(s.plugin, token)
	} else {
		if s.plugin != "" {
			return fmt.Errorf("-%s binds a PLUGIN token; a capture token is bound to a host (-%s)", flagWorkerPlugin, flagWorkerHost)
		}
		if !worker.ValidHostLabel(s.host) {
			return fmt.Errorf("-%s needs -%s, the ONE host label the token is bound to (got %q)", flagIssueWorker, flagWorkerHost, s.host)
		}
		row = worker.NewTokenRow(kind, owner, s.host, token)
	}
	if _, statErr := os.Stat(s.tokens); statErr == nil {
		existing, err := worker.LoadTokens(s.tokens, wall)
		if err != nil {
			return fmt.Errorf("refusing to mint into %s: %w", s.tokens, err)
		}
		if s.registry == "" && slices.ContainsFunc(existing, func(r worker.TokenRow) bool { return r.Kind == worker.KindPlugin }) {
			fmt.Fprintf(note, "cairn-ui: NOTE %s holds plugin row(s) this mint did not check (no -%s); a row for a "+
				"plugin the registry does not hold refuses the whole file at the next start\n", s.tokens, flagPluginRegistry)
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("the worker token file %s cannot be checked: %w", s.tokens, statErr)
	}
	if err := worker.AppendTokenRow(s.tokens, row); err != nil {
		return fmt.Errorf("the worker token file %s could not be written: %w", s.tokens, err)
	}
	bound := "for " + owner.String() + " on host " + s.host
	if kind == worker.KindPlugin {
		bound = "for plugin " + s.plugin
	}
	fmt.Fprintf(note, "cairn-ui: minted a %s worker token %s; its digest %s… is in %s. The token "+
		"is printed ONCE on stdout and stored nowhere\n", kind, bound, row.DigestPrefix(), s.tokens)
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
