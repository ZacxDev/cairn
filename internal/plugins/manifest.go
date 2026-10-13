// Package plugins is slice S5 of `claudedocs/plan-cairn-plugins.md`: the plugin REGISTRY (manifests
// the operator registers with `cairn-ui -plugin-registry`), the closed capability and output-type
// vocabularies, the per-scope / per-arc / per-session TOGGLES and their journal (`-plugin-journal`),
// the narrowing-only toggle FOLD, the output envelope's validation, and the STATELESS pending query
// (decisions 9, 10 and 13; operator decisions O4, O5 and O7).
//
// 🔴 PLUGINS ARE OUT-OF-PROCESS (O4). Nothing here runs a plugin, holds a plugin's credential (an
// LLM key, a ticket-system token) or calls a model: a plugin is a separate program that reaches
// `cairn-ui`'s WORKER listener with a `plugin` worker token (decision 2) and speaks plain HTTP+JSON —
// never MCP (O7). This package is what that listener and the browser surface decide from.
//
// 🔴 INERT WITH NO REGISTRY, AND EVERY TOGGLE IS OFF BY CONSTRUCTION. A scope's toggle is `off`
// until an admin of that scope says otherwise, an unknown scope name and `*` are `off` whatever the
// journal holds, an empty visibility set resolves OFF, and no finer level can turn anything ON that
// the scope admins did not consent to ([Effective]).
package plugins

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// The capability vocabulary (decision 9). CLOSED: a manifest naming anything else is refused.
const (
	CapPendingList    = "pending:list"
	CapTranscriptRead = "transcript:read"
	capOutputRead     = "output:read:"
	capOutputWrite    = "output:write:"
)

// The output types (decision 9) — CLOSED and IN-TREE, because the renderer must know each type to
// render it safely: a plugin that needs a new type needs a reviewed cairn change, while a new
// plugin producing existing types needs only a registry entry.
const (
	TypeSummary          = "summary"
	TypeTicketEdge       = "ticket-edge"
	TypeTicketSuggestion = "ticket-suggestion"
	// TypeTicketScan is S10's addition (a reviewed in-tree change, as the comment above requires): the
	// record that a ticket matcher EXAMINED a session through a watermark, whatever it found. Without
	// it a matcher that finds nothing has nothing to write, the watermark never advances (decision
	// 13: writing an output IS the advance), and the session is pending — and re-read — forever.
	TypeTicketScan = "ticket-scan"
)

// OutputTypes is the closed output-type set, sorted.
func OutputTypes() []string {
	return []string{TypeSummary, TypeTicketEdge, TypeTicketScan, TypeTicketSuggestion}
}

// IsOutputType reports whether t is a member of the closed set.
func IsOutputType(t string) bool { return slices.Contains(OutputTypes(), t) }

// CapOutputRead and CapOutputWrite spell the two per-type capabilities.
func CapOutputRead(t string) string  { return capOutputRead + t }
func CapOutputWrite(t string) string { return capOutputWrite + t }

// validCapability reports whether c is a member of the closed vocabulary.
func validCapability(c string) bool {
	switch {
	case c == CapPendingList || c == CapTranscriptRead:
		return true
	case strings.HasPrefix(c, capOutputRead):
		return IsOutputType(strings.TrimPrefix(c, capOutputRead))
	case strings.HasPrefix(c, capOutputWrite):
		return IsOutputType(strings.TrimPrefix(c, capOutputWrite))
	}
	return false
}

// ManifestSchema is the one manifest schema this build reads.
const ManifestSchema = 1

// The pending query's defaults (decision 13), each overridable per registry entry.
const (
	DefaultPendingIdleSeconds = 600
	DefaultPendingGrowRecords = 200
)

// Manifest is a plugin's declaration (decision 9), committed beside the plugin and registered on the
// pod by the operator.
type Manifest struct {
	Schema      int    `json:"schema"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	// Capabilities is the WHOLE declaration. The plan's separate `toggle_key` (forced equal to the
	// name, read by nothing) and `output_types` (forced equal to the `output:write:<type>` set, both
	// ways) are DELETED (#270 round 0, D1/D2): toggles are keyed by the name, and the types a plugin
	// writes are derived from its write capabilities ([Manifest.OutputTypes]) — one declaration, so
	// there is no second spelling to disagree with it.
	Capabilities []string `json:"capabilities"`
	// CallsModel is rendered on `/plugins`: whether the plugin sends what it reads to a model
	// provider (T7).
	CallsModel bool `json:"calls_model"`
}

// Entry is one registry row: a manifest plus the instance's pending-trigger settings.
type Entry struct {
	Manifest
	// PendingIdleSeconds and PendingGrowRecords override decision 13's 10 minutes and 200 records.
	PendingIdleSeconds int `json:"pending_idle_seconds,omitempty"`
	PendingGrowRecords int `json:"pending_grow_records,omitempty"`
}

// Has reports whether the entry's manifest declares capability c.
func (e Entry) Has(c string) bool { return slices.Contains(e.Capabilities, c) }

// Writes reports whether the plugin may WRITE outputs of type t: it holds `output:write:<t>`. The one
// predicate the write route asks.
func (e Entry) Writes(t string) bool { return IsOutputType(t) && e.Has(CapOutputWrite(t)) }

// OutputTypes is the types the plugin writes, derived from its `output:write:<type>` capabilities,
// in the closed set's order.
func (m Manifest) OutputTypes() []string {
	var out []string
	for _, t := range OutputTypes() {
		if slices.Contains(m.Capabilities, CapOutputWrite(t)) {
			out = append(out, t)
		}
	}
	return out
}

// Registry is the registered plugins, by name.
type Registry struct {
	entries map[string]Entry
	order   []string
}

// Lookup returns a registered plugin.
func (r *Registry) Lookup(name string) (Entry, bool) {
	if r == nil {
		return Entry{}, false
	}
	e, ok := r.entries[name]
	return e, ok
}

// Holds reports whether name is registered — the worker token wall's question.
func (r *Registry) Holds(name string) bool { _, ok := r.Lookup(name); return ok }

// Entries is every registered plugin, sorted by name.
func (r *Registry) Entries() []Entry {
	if r == nil {
		return nil
	}
	out := make([]Entry, 0, len(r.order))
	for _, n := range r.order {
		out = append(out, r.entries[n])
	}
	return out
}

// Bounds on a registry.
const (
	MaxPlugins        = 32
	maxDescription    = 280
	maxRegistryBytes  = 1 << 20
	maxIdleSeconds    = 7 * 24 * 3600
	maxGrowRecords    = 1_000_000
	registryFileShape = `{"schema":1,"plugins":[<manifest>, …]}`
)

var (
	// nameClass is a plugin name: a short lower-case slug. It is a toggle key, a token-file field and
	// a JSON value, so it carries nothing that needs quoting in any of them.
	nameClass    = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	versionClass = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`)
)

// ValidName reports whether s may be a plugin name.
func ValidName(s string) bool { return nameClass.MatchString(s) }

// registryFile is the `-plugin-registry` document.
type registryFile struct {
	Schema  int     `json:"schema"`
	Plugins []Entry `json:"plugins"`
}

// ErrRegistry marks every refusal of a registry's CONTENT.
var ErrRegistry = errors.New("the plugin registry is refused")

// LoadRegistry reads and validates `-plugin-registry`. Any problem refuses the WHOLE file: a
// registry is the instance-level decision to admit a plugin (decision 10, D3), and half of one is a
// decision nobody made.
func LoadRegistry(path string) (*Registry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("the plugin registry %s cannot be read: %w", path, err)
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxRegistryBytes+1))
	if err != nil {
		return nil, fmt.Errorf("the plugin registry %s cannot be read: %w", path, err)
	}
	if len(raw) > maxRegistryBytes {
		return nil, fmt.Errorf("%w: %s is over %d bytes", ErrRegistry, path, maxRegistryBytes)
	}
	reg, err := ParseRegistry(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return reg, nil
}

// ParseRegistry is [LoadRegistry] over bytes.
func ParseRegistry(raw []byte) (*Registry, error) {
	var doc registryFile
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("%w: not %s with only the manifest's own keys (%v)", ErrRegistry, registryFileShape, err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("%w: trailing content after the registry object", ErrRegistry)
	}
	if doc.Schema != ManifestSchema {
		return nil, fmt.Errorf("%w: registry schema %d is not %d", ErrRegistry, doc.Schema, ManifestSchema)
	}
	if len(doc.Plugins) > MaxPlugins {
		return nil, fmt.Errorf("%w: %d plugins, at most %d", ErrRegistry, len(doc.Plugins), MaxPlugins)
	}
	reg := &Registry{entries: map[string]Entry{}}
	for i, e := range doc.Plugins {
		if err := validateEntry(e); err != nil {
			return nil, fmt.Errorf("%w: plugin %d: %v", ErrRegistry, i, err)
		}
		if _, dup := reg.entries[e.Name]; dup {
			return nil, fmt.Errorf("%w: plugin %q is registered twice", ErrRegistry, e.Name)
		}
		if e.PendingIdleSeconds == 0 {
			e.PendingIdleSeconds = DefaultPendingIdleSeconds
		}
		if e.PendingGrowRecords == 0 {
			e.PendingGrowRecords = DefaultPendingGrowRecords
		}
		reg.entries[e.Name] = e
		reg.order = append(reg.order, e.Name)
	}
	slices.Sort(reg.order)
	return reg, nil
}

// printable reports whether s holds no control character.
func printable(s string) bool {
	return !strings.ContainsFunc(s, unicode.IsControl)
}

// BidiControl reports whether r is a Unicode BIDIRECTIONAL control: the embeddings, overrides and
// isolates (U+202A–202E, U+2066–2069) and the marks (U+200E, U+200F, U+061C). ONE spelling: the
// write path refuses a display string carrying one ([displayable]) and the renderer strips them
// from anything stored before that rule (`internal/ui` `plainText`).
func BidiControl(r rune) bool {
	return r >= 0x202A && r <= 0x202E || r >= 0x2066 && r <= 0x2069 || r == 0x200E || r == 0x200F || r == 0x061C
}

// displayable reports whether s may be stored as a short string the page prints INSIDE its own
// sentence — a model's name, a ticket title, a rationale: no control character and no bidirectional
// control. Escaping does not cover the second: such a character is not markup, it reorders how what
// FOLLOWS it is displayed, so one stored in a title could make the page's own label read as
// something else.
func displayable(s string) bool {
	return printable(s) && !strings.ContainsFunc(s, BidiControl)
}

// ValidateManifest checks one manifest against the closed vocabularies.
func ValidateManifest(m Manifest) error {
	switch {
	case m.Schema != ManifestSchema:
		return fmt.Errorf("schema %d is not %d", m.Schema, ManifestSchema)
	case !ValidName(m.Name):
		return fmt.Errorf("name %q does not match %s", m.Name, nameClass)
	case !versionClass.MatchString(m.Version):
		return fmt.Errorf("version %q does not match %s", m.Version, versionClass)
	case len(m.Description) > maxDescription || !printable(m.Description):
		return fmt.Errorf("description is over %d bytes or carries a control character", maxDescription)
	}
	seenCap := map[string]bool{}
	for _, c := range m.Capabilities {
		if !validCapability(c) {
			return fmt.Errorf("capability %q is not in the closed vocabulary (%s, %s, %s<type>, %s<type>)",
				c, CapPendingList, CapTranscriptRead, capOutputRead, capOutputWrite)
		}
		if seenCap[c] {
			return fmt.Errorf("capability %q is declared twice", c)
		}
		seenCap[c] = true
	}
	return nil
}

func validateEntry(e Entry) error {
	if err := ValidateManifest(e.Manifest); err != nil {
		return err
	}
	if e.PendingIdleSeconds < 0 || e.PendingIdleSeconds > maxIdleSeconds {
		return fmt.Errorf("pending_idle_seconds %d is outside 1–%d", e.PendingIdleSeconds, maxIdleSeconds)
	}
	if e.PendingGrowRecords < 0 || e.PendingGrowRecords > maxGrowRecords {
		return fmt.Errorf("pending_grow_records %d is outside 1–%d", e.PendingGrowRecords, maxGrowRecords)
	}
	return nil
}
