package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// 🔴 THESE TYPES MIRROR THE HUB'S OWN `internal/plugin` SCHEMA AND THE MIRROR IS LOAD-BEARING
// RATHER THAN CONVENIENT. The ingest decodes with `DisallowUnknownFields`, so ONE key this
// side spells differently rejects the entire multi-page push with a 400 — not the page, the
// push. A struct that carried a helpful extra field would be a push that never lands.
//
// They are duplicated rather than imported because importing the hub would put a second
// third-party module in this repository's tree, and the whole reason this program is a
// nested module is that a dependency here must not reach anything the pod or the CLI
// builds. A copy in one file, pinned by `payload_test.go` against the limits the server
// enforces, is the cheaper side of that trade.

// Limits the ingest enforces. Named so a refusal here says the same thing the server's
// would, before a 64 MiB body is uploaded to discover it.
const (
	MaxBodyBytes = 64 << 20  // the whole multipart body
	MaxFileBytes = 16 << 20  // any one part
	MaxPages     = 200       // pages per push
	MaxDigest    = 256 << 10 // an a11y digest, BYTES not runes
)

// EnvLab is the environment this harness declares, and it is not a free choice.
//
// 🔴 `lab` IS WHAT SUPPRESSES THE PERF FINDINGS, AND THIS HARNESS OMITS THE PERF BLOCK
// ENTIRELY ON TOP OF THAT. Localhost LCP/TBT/weight over an inline stylesheet and no
// network are not field-representative in any direction a reader could correct for, so
// sending them and relying on the suppression would be sending a number whose only
// consumer is a banner. Two independent reasons to omit it, which is why closing one
// would not change the answer.
const EnvLab = "lab"

// PushPayload is the `metadata` part.
type PushPayload struct {
	Label       string     `json:"label,omitempty"`
	Environment string     `json:"environment,omitempty"`
	Pages       []PushPage `json:"pages"`
}

// PushPage is one captured view. `URL`+`Viewport` is the identity the hub matches P2
// diffs on across runs, so `URL` must be the route path and never anything carrying a
// port, a temp directory or a timestamp — a label that varied would make every page "new"
// on every push and the diff would never say anything.
type PushPage struct {
	URL        string `json:"url"`
	Viewport   string `json:"viewport"`
	Screenshot string `json:"screenshot"`
	Axe        string `json:"axe,omitempty"`
	Network    string `json:"network,omitempty"`
	A11yDigest string `json:"a11y_digest,omitempty"`

	AxeViolations     int `json:"axe_violations"`
	ConsoleFirstParty int `json:"console_first_party"`
	ConsoleThirdParty int `json:"console_third_party"`
	NetworkFirstParty int `json:"network_first_party"`
	NetworkThirdParty int `json:"network_third_party"`

	Findings []PushFinding `json:"findings,omitempty"`
	Layout   *PushLayout   `json:"layout,omitempty"`
	// ⚠ THERE IS NO `Perf` FIELD, DELIBERATELY. See [EnvLab]. Adding one is a decision
	// about honesty, not a field.
}

// PushFinding is one normalised issue. `Detail` is a STRING on the wire even when it
// carries JSON, which is the server's shape and not a mistake here.
type PushFinding struct {
	Type     string `json:"type"`
	Severity string `json:"severity"`
	Detail   string `json:"detail"`
}

// PushLayout mirrors the hub's raw layout block EXACTLY, snake_case keys included,
// because `vendor-js/layout-smells.js` emits these names and the server derives its
// findings from them through `internal/signals`.
//
// 🔴 RAW COUNTS ONLY. The thresholds (44px taps, 12px text, the mobile-only gating) live in
// `internal/signals` on the server, which is its declared single source of truth. A harness
// that computed a `type=layout` finding would be a second copy of a threshold, and the
// server drops a pushed layout finding when a block is present anyway — so the copy would
// be both wrong and invisible.
type PushLayout struct {
	HorizontalOverflow  bool                `json:"horizontal_overflow,omitempty"`
	ScrollWidth         int                 `json:"scroll_width,omitempty"`
	InnerWidth          int                 `json:"inner_width,omitempty"`
	SmallTapTargets     int                 `json:"small_tap_targets,omitempty"`
	SmallText           int                 `json:"small_text,omitempty"`
	MissingViewportMeta bool                `json:"missing_viewport_meta,omitempty"`
	ImagesNoDims        int                 `json:"images_no_dims,omitempty"`
	Examples            map[string][]string `json:"examples,omitempty"`
}

// findingTypes is the server's closed set, spelled here so a typo is a local refusal
// rather than a 400 after the upload.
var findingTypes = map[string]bool{
	"a11y": true, "console": true, "network": true, "perf": true, "layout": true, "other": true,
}

// BuildPayload turns the captures into the metadata part and the file parts.
//
// Filenames are derived from the target and the viewport and are plain basenames: the
// server rejects a referenced name with any directory component (the ref comes from
// attacker-influenceable metadata on its side), and the reference CLI rejects it before
// sending.
//
// 🔴 ONLY CAPTURES AT A `Push` VIEWPORT REACH THE PAYLOAD, AND THE FILTER IS HERE RATHER
// THAN IN THE WALK. The hub's viewport set is CLOSED — `Validate` refuses anything outside
// `{mobile, desktop}`, which is the server's contract and not a preference — while the walk
// captures five widths because that is what measures a responsive layout. Filtering in the
// walk instead would mean the extra widths were never rendered at all, which is the whole
// thing being bought; filtering here means every width is measured locally and exactly the
// two the hub can diff are sent. A page pushed under a viewport name the hub has never
// stored would be "new" on every run and its pixel diff would never say anything.
func BuildPayload(label string, captures []*Capture) (*PushPayload, map[string][]byte, error) {
	p := &PushPayload{Label: label, Environment: EnvLab}
	files := map[string][]byte{}

	for _, c := range captures {
		if !c.Viewport.Push {
			continue
		}
		stem := slug(c.Target.PushURL) + "-" + c.Viewport.Name
		shot := stem + ".png"
		axe := stem + ".axe.json"
		netw := stem + ".network.json"

		files[shot] = c.Screenshot
		files[axe] = c.AxeJSON

		netJSON, err := json.Marshal(eventList(c.Network))
		if err != nil {
			return nil, nil, err
		}
		files[netw] = netJSON

		pg := PushPage{
			URL:        c.Target.PushURL,
			Viewport:   c.Viewport.Name,
			Screenshot: shot,
			Axe:        axe,
			Network:    netw,

			AxeViolations:     len(c.Violations),
			ConsoleFirstParty: countParty(c.Console, true),
			ConsoleThirdParty: countParty(c.Console, false),
			NetworkFirstParty: countParty(c.Network, true),
			NetworkThirdParty: countParty(c.Network, false),

			Layout: c.Layout,
		}

		// 🔴 THE DIGEST REF IS ATTACHED ONLY WHEN THE DIGEST IS NON-EMPTY, AND THE
		// ASYMMETRY IS THE CONTRACT. An empty digest is a 400 on the whole push; an
		// absent one is byte-for-byte the pre-digest behaviour. So the failure modes are
		// "reject everything" versus "ground nothing", and only one of them is recoverable
		// by the next run.
		if c.HasDigest() {
			name := stem + ".a11y-digest.json"
			files[name] = c.DigestJSON
			pg.A11yDigest = name
		}

		// 🔴 STRUCTURED a11y DETAILS CARRYING THE RULE `id`, NEVER THE LEGACY
		// `"<id> — <help>"` STRING. The hub's P2 delta extracts a TOP-LEVEL `id` from
		// each stored a11y finding's detail. A producer that flattens to the legacy string
		// makes the server DERIVE an id by substring instead — which works, and works
		// silently past the case where the help text itself contains the separator. This
		// is also the field `new_a11y_rules` keys on, so the promotion candidate in the
		// README is a claim about this line.
		for _, v := range c.Violations {
			detail, err := json.Marshal(v)
			if err != nil {
				return nil, nil, err
			}
			pg.Findings = append(pg.Findings, PushFinding{
				Type:     "a11y",
				Severity: severityFor(v.Impact),
				Detail:   string(detail),
			})
		}
		for _, e := range c.Console {
			pg.Findings = append(pg.Findings, PushFinding{
				Type: "console", Severity: "minor", Detail: e.Text,
			})
		}
		for _, e := range c.Network {
			sev := "minor"
			if e.FirstParty {
				sev = "serious"
			}
			pg.Findings = append(pg.Findings, PushFinding{
				Type: "network", Severity: sev, Detail: e.Text,
			})
		}
		// ⚠ NO `type=layout` FINDING IS APPENDED. The raw block above is what the server
		// derives them from.

		p.Pages = append(p.Pages, pg)
	}
	return p, files, nil
}

// severityFor maps axe's impact onto the hub's free-text severity. axe's four values
// already are the hub's four; an absent impact becomes `info` rather than being invented.
func severityFor(impact string) string {
	switch impact {
	case "critical", "serious", "moderate", "minor":
		return impact
	default:
		return "info"
	}
}

func countParty(events []Event, first bool) int {
	n := 0
	for _, e := range events {
		if e.FirstParty == first {
			n++
		}
	}
	return n
}

func eventList(events []Event) []map[string]any {
	out := make([]map[string]any, 0, len(events))
	for _, e := range events {
		out = append(out, map[string]any{"first_party": e.FirstParty, "text": e.Text})
	}
	return out
}

// slug turns a route path into a filename stem. `/` becomes `root`; a query parameter
// becomes part of the stem so the two `/share?scope=…` pages do not collide.
func slug(path string) string {
	s := strings.Trim(path, "/")
	if s == "" {
		s = "root"
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// Validate is this side's copy of the server's own `Validate`, run BEFORE the upload.
//
// 🔴 IT IS NOT A SECOND SOURCE OF TRUTH, IT IS AN EARLY REFUSAL. The server's copy is
// authoritative and this one cannot make a push land that the server would reject. What it
// buys is that a shape defect is a local error naming the field, rather than a 400 whose
// body has to be read out of a CI log after a 64 MiB upload — and that `payload_test.go`
// can exercise the shape rules with no server anywhere, which is the only way this leg is
// testable at all (minting a push target is an operator step behind Supabase).
func (p *PushPayload) Validate(provided map[string][]byte) error {
	if p.Environment != EnvLab && p.Environment != "staging" && p.Environment != "prod" && p.Environment != "" {
		return fmt.Errorf("environment %q is outside the server's closed set", p.Environment)
	}
	if len(p.Pages) == 0 {
		return fmt.Errorf("no pages in payload: a push with nothing in it would create a run that says nothing")
	}
	if len(p.Pages) > MaxPages {
		return fmt.Errorf("too many pages: %d (max %d)", len(p.Pages), MaxPages)
	}

	seen := map[string]bool{}
	total := 0
	for i, pg := range p.Pages {
		if strings.TrimSpace(pg.URL) == "" {
			return fmt.Errorf("page %d: url (the stable diff identity) is required", i)
		}
		if pg.Viewport != Mobile.Name && pg.Viewport != Desktop.Name {
			return fmt.Errorf("page %d: viewport %q is outside the server's closed set", i, pg.Viewport)
		}
		if strings.TrimSpace(pg.Screenshot) == "" {
			return fmt.Errorf("page %d: a screenshot ref is required", i)
		}
		for _, ref := range []string{pg.Screenshot, pg.Axe, pg.Network, pg.A11yDigest} {
			if ref == "" {
				continue
			}
			if strings.ContainsAny(ref, "/\\") || ref == "." || ref == ".." || strings.Contains(ref, "..") {
				return fmt.Errorf("page %d: ref %q is not a plain basename", i, ref)
			}
			body, ok := provided[ref]
			if !ok {
				return fmt.Errorf("page %d references %q but no part carries it", i, ref)
			}
			if len(body) > MaxFileBytes {
				return fmt.Errorf("page %d: part %q is %d bytes (per-file cap %d)", i, ref, len(body), MaxFileBytes)
			}
			if ref == pg.A11yDigest && len(body) > MaxDigest {
				return fmt.Errorf("page %d: the a11y digest %q is %d bytes (cap %d)", i, ref, len(body), MaxDigest)
			}
			if ref == pg.A11yDigest && !digestNonEmpty(body) {
				return fmt.Errorf("page %d: the a11y digest %q is empty; OMIT the ref for that page — an empty digest is a 400 on the WHOLE push", i, ref)
			}
			seen[ref] = true
		}
		for j, f := range pg.Findings {
			if !findingTypes[f.Type] {
				return fmt.Errorf("page %d finding %d: type %q is outside the server's closed set", i, j, f.Type)
			}
			if f.Type == "layout" || f.Type == "perf" {
				return fmt.Errorf("page %d finding %d: this harness must not author a %q finding — "+
					"the server derives those from the raw block via internal/signals", i, j, f.Type)
			}
			if f.Type == "a11y" && !hasTopLevelStringID(f.Detail) {
				return fmt.Errorf("page %d finding %d: an a11y detail must be a JSON object with a "+
					"non-empty top-level \"id\"; without it new_a11y_rules goes through the legacy "+
					"derivation and the promotion candidate is a claim about nothing", i, j)
			}
		}
	}

	// Orphan parts: the server refuses a part nothing references, and so does this.
	var orphans []string
	for name, body := range provided {
		total += len(body)
		if !seen[name] {
			orphans = append(orphans, name)
		}
	}
	if len(orphans) > 0 {
		sort.Strings(orphans)
		return fmt.Errorf("part(s) %s are not referenced by any page", strings.Join(orphans, ", "))
	}
	if total > MaxBodyBytes {
		return fmt.Errorf("the parts total %d bytes (body cap %d)", total, MaxBodyBytes)
	}
	return nil
}

// hasTopLevelStringID is the server's own predicate, spelled the same way: a plain
// free-text string is not valid JSON here and answers false.
func hasTopLevelStringID(s string) bool {
	var meta struct {
		ID string `json:"id"`
	}
	return json.Unmarshal([]byte(s), &meta) == nil && meta.ID != ""
}

func digestNonEmpty(body []byte) bool {
	var d struct {
		Interactive  []json.RawMessage `json:"interactive"`
		FormControls []json.RawMessage `json:"form_controls"`
		Landmarks    []json.RawMessage `json:"landmarks"`
	}
	if err := json.Unmarshal(body, &d); err != nil {
		return false
	}
	return len(d.Interactive)+len(d.FormControls)+len(d.Landmarks) > 0
}
