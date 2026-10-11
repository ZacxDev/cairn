package memo_test

// Guards A and B of decision 19, at the PROPERTY level, with their required mutants.
//
// This file is an EXTERNAL test package on purpose: the property is about what code OUTSIDE
// `internal/memo` can reach, so it is exercised from outside.

import (
	"encoding/hex"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/memo"
	"github.com/ZacxDev/cairn/internal/surface"
)

// ---------------------------------------------------------------- Guard A

// entry is one exported name's ledger row: its CLASS, and its SIGNATURE as printed from the
// source with parameter names stripped.
//
// 🔴 KEYED ON NAME **AND** PRINTED TYPE, NOT NAME ALONE. A name-only ledger (S0's) stayed green
// when an EXISTING name's signature changed — `Statuses() []string` becoming
// `Statuses(...Stored) []string` returns stored text under a name the ledger already trusted
// (S0 audit, carried to S1). Every row now pins what the name IS, so a changed signature is a
// CHANGED row and red.
type entry struct{ class, sig string }

// exportLedger is EVERY exported name in package memo — not only those whose signature mentions
// `Stored` (audit round 6 🟡1: a `DecodeText([]byte) (string, string, error)` mentions no
// Stored and returns raw text). Methods are `Type.Method`, struct fields `Type.Field`, and an
// EMBEDDED field is `Type.<its printed type>` WHATEVER its package or exportedness — an embedded
// `json.RawMessage` in `Rendered` was invisible to S0's ledger (it skipped a qualified name),
// and its promoted bytes would have been invisible to the emitter check too. The ledger fails on
// GROW, SHRINK or CHANGE, so a new way for text to leave the package is a deliberate, reviewed
// edit here, never an accident.
//
// Each row carries its CLASS. Every `emits` row is exercised by `emitters` below against a
// Stored carrying a planted invisible code point, and the two sets are pinned equal.
var exportLedger = map[string]entry{
	// predicates and the send-side fold: take the caller's own string, never a Stored.
	"Unsafe":             {"predicate", "func(rune) bool"},
	"BreaksFence":        {"predicate", "func(string) bool"},
	"SubjectBreaksFence": {"predicate", "func(string) bool"},
	"Normalize":          {"input", "func(string) string"},
	// the authority predicates (S1): booleans over an authorization, no text.
	"MaySend":    {"predicate", "func(control.Authorization, string) bool"},
	"MayRead":    {"predicate", "func(control.Authorization, string) bool"},
	"MayRetract": {"predicate", "func(control.Principal, control.Authorization, string, string, string) bool"},
	// the send-side content rules (S1): the caller's OWN text in, normalised, on its way to
	// storage — never stored text on its way out.
	"CheckSend":        {"input", "func(string, string, time.Duration, *redact.Redactor) (Outgoing, error)"},
	"Outgoing":         {"input", "struct"},
	"Outgoing.Subject": {"input", "string"},
	"Outgoing.Body":    {"input", "string"},
	"Outgoing.TTL":     {"input", "time.Duration"},
	"NewScanner":       {"input", "func() (*redact.Redactor, error)"},
	// refusals: a closed reason and a RULE NAME — never content.
	"Refusal":             {"refusal", "struct"},
	"Refusal.Reason":      {"refusal", "RefusalReason"},
	"Refusal.Rule":        {"refusal", "string"},
	"Refusal.Error":       {"refusal", "(*Refusal) func() string"},
	"RefusalReason":       {"refusal", "string"},
	"RefusedNoWrite":      {"refusal", "const RefusalReason"},
	"RefusedQuota":        {"refusal", "const RefusalReason"},
	"RefusedLiveLimit":    {"refusal", "const RefusalReason"},
	"RefusedSubjectSize":  {"refusal", "const RefusalReason"},
	"RefusedBodySize":     {"refusal", "const RefusalReason"},
	"RefusedCharacters":   {"refusal", "const RefusalReason"},
	"RefusedSecret":       {"refusal", "const RefusalReason"},
	"RefusedTTL":          {"refusal", "const RefusalReason"},
	"RefusedNotPermitted": {"refusal", "const RefusalReason"},
	"ErrNotFound":         {"refusal", `var = errors.New("memo: not found")`},
	// limits: numbers, no text.
	"MaxSubjectBytes":        {"table", "const"},
	"MaxBodyBytes":           {"table", "const"},
	"DefaultTTL":             {"table", "const"},
	"MaxTTL":                 {"table", "const"},
	"RetentionAfterExpiry":   {"table", "const"},
	"SendsPerScopePerWindow": {"table", "const"},
	"QuotaWindow":            {"table", "const"},
	"MaxLivePerScope":        {"table", "const"},
	"TransactionTimeout":     {"table", "const"},
	"CursorMargin":           {"table", "const"},
	"ConfidentSecretRules":   {"table", "func() []string"},
	// the input type and its constructor: text goes IN; nothing returns a Fields.
	"Fields":                  {"input", "struct"},
	"Fields.ID":               {"input", "int64"},
	"Fields.Scope":            {"input", "string"},
	"Fields.SenderKind":       {"input", "string"},
	"Fields.SenderID":         {"input", "string"},
	"Fields.SenderDisplay":    {"input", "string"},
	"Fields.CredentialLabel":  {"input", "string"},
	"Fields.Subject":          {"input", "string"},
	"Fields.Body":             {"input", "string"},
	"Fields.CreatedAt":        {"input", "time.Time"},
	"Fields.ExpiresAt":        {"input", "time.Time"},
	"Fields.RetractedAt":      {"input", "time.Time"},
	"Fields.RetractorKind":    {"input", "string"},
	"Fields.RetractorID":      {"input", "string"},
	"Fields.RetractorDisplay": {"input", "string"},
	"Fields.RetractorLabel":   {"input", "string"},
	"NewStored":               {"input", "func(Fields) Stored"},
	// S3: the client's way in — a `Rendered` the listener SENT, treated as untrusted text and
	// sanitised again on every output (`TestFromRenderedSanitisesEveryWireFieldOnEveryOutput`).
	"FromRendered": {"input", "func(Rendered) (Stored, error)"},
	// the carrier: no exported field, no method.
	"Stored": {"carrier", "struct"},
	// the rendered type: every field already replaced (pinned by reflection in
	// TestEveryFieldsStringIsSanitisedOnEveryOutput and TestEveryRenderedFieldIsAStringOrTheID,
	// and by the `Render` emitter here).
	"Rendered":           {"rendered", "struct"},
	"Rendered.ID":        {"rendered", "int64"},
	"Rendered.Scope":     {"rendered", "string"},
	"Rendered.Sender":    {"rendered", "string"},
	"Rendered.CreatedAt": {"rendered", "string"},
	"Rendered.ExpiresAt": {"rendered", "string"},
	"Rendered.Subject":   {"rendered", "string"},
	"Rendered.Body":      {"rendered", "string"},
	"Rendered.Tombstone": {"rendered", "string"},
	// the only names that can carry stored text OUT.
	"Render":        {"emits", "func(Stored) Rendered"},
	"RenderPreview": {"emits", "func([]Stored) string"},
	"RenderFull":    {"emits", "func([]Stored) string"},
	// a constant table; no text.
	"Statuses": {"table", "func() []string"},
}

// exportedSurface is the package's exported surface as `internal/surface` reads it from source,
// with `override` replacing a file and `extra` adding one.
func exportedSurface(t *testing.T, override, extra map[string]string) map[string]string {
	t.Helper()
	got, err := surface.Exported(".", "memo", override, extra)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 40 {
		t.Fatalf("read only %d exported names — the ledger would be compared against nothing", len(got))
	}
	return got
}

func ledgerSigs() map[string]string {
	out := map[string]string{}
	for k, e := range exportLedger {
		out[k] = e.sig
	}
	return out
}

// TestGuardATheExportedSurfaceIsTheLedger is the ledger's GROW / SHRINK / CHANGE check over the
// real package.
func TestGuardATheExportedSurfaceIsTheLedger(t *testing.T) {
	grew, shrank, changed := surface.Diff(exportedSurface(t, nil, nil), ledgerSigs())
	if len(grew)+len(shrank)+len(changed) > 0 {
		t.Fatalf("package memo's exported surface moved (decision 19, Guard A).\n"+
			"  new names (each is a new way for stored text to leave the package — list it in "+
			"exportLedger with its class, and if it can emit text, exercise it in `emitters`):\n    %s\n"+
			"  names gone from the package (remove them from exportLedger): %v\n"+
			"  names whose SIGNATURE changed (a changed signature can return stored text under a "+
			"trusted name — re-review it, then update its row):\n    %s",
			strings.Join(grew, "\n    "), shrank, strings.Join(changed, "\n    "))
	}
}

// guardAMutants are the escapes rounds 5 and 6 and the S0 audit named, each as the source a
// maintainer would ADD (`src`) or the EDIT they would make to an existing file (`file`/`old`/
// `new`). Each must make the ledger move BY THE NAME the mutant introduces or changes.
var guardAMutants = []struct {
	name, src      string
	file, old, new string
	grows, changes string
}{
	{name: "raw-method", src: "func (s Stored) Raw() string { return \"\" }", grows: "Stored.Raw"},
	{name: "emit-writer", src: "func Emit(w io.Writer, s Stored) {}", grows: "Emit"},
	{name: "string-slice", src: "func Bodies(s []Stored) []string { return nil }", grows: "Bodies"},
	{name: "map-result", src: "func ByID(s []Stored) map[int64]string { return nil }", grows: "ByID"},
	{name: "any-result", src: "func Any(s Stored) any { return nil }", grows: "Any"},
	{name: "pointer-result", src: "func Ptr(s Stored) *string { return nil }", grows: "Ptr"},
	{name: "generic-result", src: "func Gen[T any](s Stored) T { var z T; return z }", grows: "Gen"},
	{name: "bytes-field-result", src: "type Out struct{ B []byte }\nfunc Bytes(s Stored) Out { return Out{} }", grows: "Out.B"},
	{name: "exported-func-var", src: "var Peek = func(Stored) string { return \"\" }", grows: "Peek"},
	{name: "wrapper-type", src: "type Wrapper struct{ S Stored }", grows: "Wrapper.S"},
	{name: "no-stored-in-signature", src: "func DecodeText(b []byte) (string, string, error) { return \"\", \"\", nil }", grows: "DecodeText"},
	{name: "field-on-rendered", src: "type rendered2 struct{ RawBody string }", grows: "rendered2.RawBody"},
	{name: "method-on-unexported", src: "func (s stored) Text() string { return s.body }", grows: "stored.Text"},
	// S0 audit, carried to S1: a CHANGED signature on a name the ledger already trusted.
	{name: "changed-signature-on-existing-name", file: "status.go",
		old: "func Statuses() []string {", new: "func Statuses(s ...Stored) []string {", changes: "Statuses"},
	// S0 audit, carried to S1: an EMBEDDED field from ANOTHER package in Rendered.
	{name: "embedded-foreign-field-in-rendered", file: "stored.go",
		old: "type Rendered struct {\n", new: "type Rendered struct {\n\tjson.RawMessage\n", grows: "Rendered.json.RawMessage"},
	// …and an existing Rendered field whose TYPE changes to bytes.
	{name: "rendered-field-type-changed", file: "stored.go",
		old: "\tSubject, Body, Tombstone string\n", new: "\tSubject, Tombstone string\n\tBody []byte\n", changes: "Rendered.Body"},
}

// TestGuardACatchesEveryRequiredMutant applies each required mutant to the source the ledger
// reads and requires the ledger to go RED naming it. POSITIVE CONTROL first: with no mutant the
// ledger is green, so a red below is the mutant's doing.
func TestGuardACatchesEveryRequiredMutant(t *testing.T) {
	if g, s, c := surface.Diff(exportedSurface(t, nil, nil), ledgerSigs()); len(g)+len(s)+len(c) != 0 {
		t.Fatalf("control: unmutated ledger is not green: %v %v %v", g, s, c)
	}
	caught := 0
	for _, m := range guardAMutants {
		var override, extra map[string]string
		if m.src != "" {
			extra = map[string]string{"mutant_" + m.name + ".go": "package memo\n\nimport \"io\"\n\nvar _ io.Writer\n\n" + m.src + "\n"}
		} else {
			raw, err := os.ReadFile(m.file)
			if err != nil {
				t.Fatal(err)
			}
			if n := strings.Count(string(raw), m.old); n != 1 {
				t.Fatalf("mutant %s: pattern occurs %d times in %s, want 1 — re-derive it", m.name, n, m.file)
			}
			override = map[string]string{m.file: strings.Replace(string(raw), m.old, m.new, 1)}
		}
		grew, _, changed := surface.Diff(exportedSurface(t, override, extra), ledgerSigs())
		if !surface.Moved(grew, changed, m.grows+m.changes) {
			t.Errorf("mutant %s SURVIVED: the ledger did not move by %s%s (grew: %v, changed: %v)",
				m.name, m.grows, m.changes, grew, changed)
			continue
		}
		caught++
	}
	if caught != len(guardAMutants) {
		t.Fatalf("caught %d of %d Guard A mutants", caught, len(guardAMutants))
	}
}

// TestEveryRenderedFieldIsAStringOrTheID — S0 audit, carried to S1: the emitter check below reads
// `Rendered`'s STRING fields, so a field of any other type — a `[]byte`, an embedded
// `json.RawMessage`, a nested struct — would carry text past it unseen. Every field, embedded
// ones included, must be a plain `string` or the `int64` ID; anything else is red here rather
// than invisible there.
func TestEveryRenderedFieldIsAStringOrTheID(t *testing.T) {
	rt := reflect.TypeOf(memo.Rendered{})
	strs := 0
	for _, f := range reflect.VisibleFields(rt) {
		switch {
		case f.Type == reflect.TypeOf(""):
			strs++
		case f.Name == "ID" && f.Type == reflect.TypeOf(int64(0)):
		default:
			t.Errorf("Rendered.%s is a %s — only plain strings (each passed through the render-side "+
				"replacement and checked below) and the int64 ID may be fields of the rendered type", f.Name, f.Type)
		}
	}
	if strs < 7 {
		t.Fatalf("Rendered has only %d string fields — the reflection walked nothing", strs)
	}
}

// plantedInvisible is a ZERO-WIDTH SPACE and a VARIATION SELECTOR in every string field of a
// Fields — exactly the code points the send rule ACCEPTS and only the render-side replacement
// removes, so an emitter that skipped the replacement would carry them out.
func plantedStored(t *testing.T, retracted bool) memo.Stored {
	t.Helper()
	f := memo.Fields{ID: 9, CreatedAt: time.Date(2000, 1, 2, 0, 0, 0, 0, time.UTC), ExpiresAt: time.Date(2000, 1, 5, 0, 0, 0, 0, time.UTC)}
	if retracted {
		f.RetractedAt = time.Date(2000, 1, 3, 0, 0, 0, 0, time.UTC)
	}
	v := reflect.ValueOf(&f).Elem()
	n := 0
	for i := 0; i < v.NumField(); i++ {
		if v.Field(i).Kind() == reflect.String {
			v.Field(i).SetString(v.Type().Field(i).Name + "\u200b" + string(rune(0xE0100)) + "x")
			n++
		}
	}
	if n < 10 {
		t.Fatalf("planted only %d fields", n)
	}
	return memo.NewStored(f)
}

func renderedText(r memo.Rendered) string {
	v := reflect.ValueOf(r)
	var b strings.Builder
	for i := 0; i < v.NumField(); i++ {
		if v.Field(i).Kind() == reflect.String {
			b.WriteString(v.Field(i).String() + "\n")
		}
	}
	return b.String()
}

// emitters exercises every `emits` name in the ledger.
var emitters = map[string]func([]memo.Stored) string{
	"Render": func(s []memo.Stored) string {
		var b strings.Builder
		for _, m := range s {
			b.WriteString(renderedText(memo.Render(m)))
		}
		return b.String()
	},
	"RenderPreview": memo.RenderPreview,
	"RenderFull":    memo.RenderFull,
}

func TestGuardAEveryEmitterAppliesTheReplacement(t *testing.T) {
	var want []string
	for k, e := range exportLedger {
		if e.class == "emits" {
			want = append(want, k)
		}
	}
	var got []string
	for k := range emitters {
		got = append(got, k)
	}
	sort.Strings(want)
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("emitters exercised %v, ledger's emitting names are %v", got, want)
	}
	for _, retracted := range []bool{false, true} {
		in := []memo.Stored{plantedStored(t, retracted)}
		for name, emit := range emitters {
			out := emit(in)
			if out == "" {
				t.Fatalf("%s emitted nothing — the check below would pass over nothing", name)
			}
			for _, r := range out {
				if r != '\n' && memo.Unsafe(r) {
					t.Errorf("%s (retracted=%v) emitted U+%04X", name, retracted, r)
				}
			}
			// POSITIVE CONTROL: the field text did come through (as replaced text), so the
			// emitter is not silent about the very fields the plant reached.
			if !strings.Contains(out, "Body��x") && !retracted {
				t.Errorf("%s: the planted body did not come through replaced", name)
			}
		}
	}
}

// ---------------------------------------------------------------- Guard B

// canaried is a Stored whose every string field carries a distinctive canary, so any of them
// reaching `fmt` output is visible — directly or hex-encoded.
func canaried(t *testing.T) (memo.Stored, []string) {
	t.Helper()
	var f memo.Fields
	v := reflect.ValueOf(&f).Elem()
	var canaries []string
	for i := 0; i < v.NumField(); i++ {
		if v.Field(i).Kind() == reflect.String {
			c := "zqcanary" + strings.ToLower(v.Type().Field(i).Name)
			v.Field(i).SetString(c)
			canaries = append(canaries, c)
		}
	}
	return memo.NewStored(f), canaries
}

type holderUnexported struct{ s memo.Stored }
type holderExported struct{ S memo.Stored }
type holderDeep struct{ inner struct{ h holderUnexported } }

// fmtVerbs is EVERY single-letter verb plus the three flagged %v forms. The plan's property
// names only %v/%+v/%#v; this grid is wider because a plain pointer — the plan's carrier — was
// measured to pass those three and leak through %s and %q (see memo.Stored).
var fmtVerbs = func() []string {
	v := []string{"%v", "%+v", "%#v"}
	for c := 'a'; c <= 'z'; c++ {
		v = append(v, "%"+string(c), "%"+string(c-'a'+'A'))
	}
	return v
}()

func leaks(out string, canaries []string) string {
	for _, c := range canaries {
		if strings.Contains(out, c) || strings.Contains(out, hex.EncodeToString([]byte(c))) ||
			strings.Contains(out, strings.ToUpper(hex.EncodeToString([]byte(c)))) {
			return c
		}
	}
	return ""
}

// TestGuardBNoStoredTextReachesFmt prints a Stored directly, behind a pointer, inside an
// exported field, inside an UNEXPORTED field, two unexported levels deep, in a slice, in a map
// and as `any`, with every verb, and requires no canary in any output. Required mutants, both
// measured red: "`Stored` holds its text in a VALUE field AND has no fmt methods" (round 6 🟡2),
// and "`Stored` holds a plain POINTER" (`%s` and `%q` print the pointee).
func TestGuardBNoStoredTextReachesFmt(t *testing.T) {
	s, canaries := canaried(t)
	holders := map[string]any{
		"direct":           s,
		"pointer":          &s,
		"exported-field":   holderExported{s},
		"unexported-field": holderUnexported{s},
		"two-deep":         holderDeep{inner: struct{ h holderUnexported }{holderUnexported{s}}},
		"slice":            []memo.Stored{s, s},
		"map":              map[string]memo.Stored{"k": s},
		"any":              any(s),
	}
	n := 0
	for hname, h := range holders {
		for _, verb := range fmtVerbs {
			out := fmt.Sprintf(verb, h)
			if c := leaks(out, canaries); c != "" {
				t.Errorf("%s printed with %s leaked %s: %s", hname, verb, c, out)
			}
			n++
		}
	}
	// POSITIVE CONTROL: the same detector, over a VALUE-field carrier holding the same text,
	// DOES see the leak — so the zero above is about memo.Stored, not about a blind detector.
	type valueCarrier struct{ t struct{ body string } }
	vc := valueCarrier{}
	vc.t.body = canaries[0]
	if leaks(fmt.Sprintf("%v", struct{ c valueCarrier }{vc}), canaries) == "" {
		t.Fatalf("control: the detector cannot see a value-field leak")
	}
	if n != len(holders)*len(fmtVerbs) {
		t.Fatalf("printed %d, want %d", n, len(holders)*len(fmtVerbs))
	}
}
