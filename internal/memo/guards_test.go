package memo_test

// Guards A and B of decision 19, at the PROPERTY level, with their required mutants.
//
// This file is an EXTERNAL test package on purpose: the property is about what code OUTSIDE
// `internal/memo` can reach, so it is exercised from outside.

import (
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/memo"
)

// ---------------------------------------------------------------- Guard A

// exportLedger is EVERY exported name in package memo — not only those whose signature mentions
// `Stored` (audit round 6 🟡1: a `DecodeText([]byte) (string, string, error)` mentions no
// Stored and returns raw text). Methods are `Type.Method`, struct fields `Type.Field`. The
// ledger fails on GROW or SHRINK, so a new way for text to leave the package is a deliberate,
// reviewed edit here, never an accident.
//
// Each entry carries its CLASS. Every `emits` entry is exercised by `emitters` below against a
// Stored carrying a planted invisible code point, and the two sets are pinned equal.
var exportLedger = map[string]string{
	// predicates and the send-side fold: take the caller's own string, never a Stored.
	"Unsafe":             "predicate",
	"BreaksFence":        "predicate",
	"SubjectBreaksFence": "predicate",
	"Normalize":          "input",
	// the input type and its constructor: text goes IN; nothing returns a Fields.
	"Fields":                  "input",
	"Fields.ID":               "input",
	"Fields.Scope":            "input",
	"Fields.SenderKind":       "input",
	"Fields.SenderID":         "input",
	"Fields.SenderDisplay":    "input",
	"Fields.CredentialLabel":  "input",
	"Fields.Subject":          "input",
	"Fields.Body":             "input",
	"Fields.CreatedAt":        "input",
	"Fields.ExpiresAt":        "input",
	"Fields.RetractedAt":      "input",
	"Fields.RetractorKind":    "input",
	"Fields.RetractorID":      "input",
	"Fields.RetractorDisplay": "input",
	"Fields.RetractorLabel":   "input",
	"NewStored":               "input",
	// the carrier: no exported field, no method.
	"Stored": "carrier",
	// the rendered type: every string field already replaced (pinned by reflection in
	// TestEveryFieldsStringIsSanitisedOnEveryOutput, and by the `Render` emitter here).
	"Rendered":           "rendered",
	"Rendered.ID":        "rendered",
	"Rendered.Scope":     "rendered",
	"Rendered.Sender":    "rendered",
	"Rendered.CreatedAt": "rendered",
	"Rendered.ExpiresAt": "rendered",
	"Rendered.Subject":   "rendered",
	"Rendered.Body":      "rendered",
	"Rendered.Tombstone": "rendered",
	// the only names that can carry stored text OUT.
	"Render":        "emits",
	"RenderPreview": "emits",
	"RenderFull":    "emits",
	// a constant table; no text.
	"Statuses": "table",
}

// exportedNames reads the package's NON-test sources plus any `extra` file and returns every
// exported identifier: top-level functions (generic ones too), methods on ANY receiver, types,
// struct fields (embedded ones by type name) and interface methods of ANY struct/interface type,
// and package-level variables and constants.
func exportedNames(t *testing.T, extra map[string]string) []string {
	t.Helper()
	fset := token.NewFileSet()
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var files []*ast.File
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	for name, src := range extra {
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatalf("mutant %s does not parse: %v", name, err)
		}
		files = append(files, f)
	}
	if len(files) < 4 {
		t.Fatalf("read only %d source files — the ledger would be built from nothing", len(files))
	}
	seen := map[string]bool{}
	add := func(s string) { seen[s] = true }
	for _, f := range files {
		if f.Name.Name != "memo" {
			t.Fatalf("%s is package %s", fset.File(f.Pos()).Name(), f.Name.Name)
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if !d.Name.IsExported() {
					continue
				}
				if d.Recv == nil {
					add(d.Name.Name)
					continue
				}
				add(recvName(d.Recv.List[0].Type) + "." + d.Name.Name)
			case *ast.GenDecl:
				for _, s := range d.Specs {
					switch s := s.(type) {
					case *ast.TypeSpec:
						if s.Name.IsExported() {
							add(s.Name.Name)
						}
						members(s.Name.Name, s.Type, add)
					case *ast.ValueSpec:
						for _, n := range s.Names {
							if n.IsExported() {
								add(n.Name)
							}
						}
					}
				}
			}
		}
	}
	var out []string
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func recvName(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.StarExpr:
		return recvName(e.X)
	case *ast.IndexExpr:
		return recvName(e.X)
	case *ast.IndexListExpr:
		return recvName(e.X)
	case *ast.Ident:
		return e.Name
	}
	return fmt.Sprintf("%T", e)
}

// members records the exported fields of a struct type and the exported methods of an
// interface type — of ANY type, exported or not, because an unexported type's exported field is
// reachable through any exported name that returns a value of it.
func members(typeName string, e ast.Expr, add func(string)) {
	var list *ast.FieldList
	switch e := e.(type) {
	case *ast.StructType:
		list = e.Fields
	case *ast.InterfaceType:
		list = e.Methods
	default:
		return
	}
	for _, fl := range list.List {
		if len(fl.Names) == 0 { // embedded
			n := recvName(fl.Type)
			if ast.IsExported(n) {
				add(typeName + "." + n)
			}
			continue
		}
		for _, n := range fl.Names {
			if n.IsExported() {
				add(typeName + "." + n.Name)
			}
		}
	}
}

func ledgerNames() []string {
	var out []string
	for k := range exportLedger {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func diff(got, want []string) (grew, shrank []string) {
	g, w := map[string]bool{}, map[string]bool{}
	for _, s := range got {
		g[s] = true
	}
	for _, s := range want {
		w[s] = true
	}
	for _, s := range got {
		if !w[s] {
			grew = append(grew, s)
		}
	}
	for _, s := range want {
		if !g[s] {
			shrank = append(shrank, s)
		}
	}
	return
}

// TestGuardATheExportedSurfaceIsTheLedger is the ledger's GROW-or-SHRINK check over the real
// package.
func TestGuardATheExportedSurfaceIsTheLedger(t *testing.T) {
	grew, shrank := diff(exportedNames(t, nil), ledgerNames())
	if len(grew) > 0 || len(shrank) > 0 {
		t.Fatalf("package memo's exported surface moved (decision 19, Guard A).\n"+
			"  new names (each is a new way for stored text to leave the package — list it in "+
			"exportLedger with its class, and if it can emit text, exercise it in `emitters`): %v\n"+
			"  names gone from the package (remove them from exportLedger): %v", grew, shrank)
	}
}

// guardAMutants are the escapes rounds 5 and 6 named, each as the source a maintainer would ADD.
// Each must make the ledger GROW, and by the name the mutant introduces.
var guardAMutants = []struct{ name, src, adds string }{
	{"raw-method", "func (s Stored) Raw() string { return \"\" }", "Stored.Raw"},
	{"emit-writer", "func Emit(w io.Writer, s Stored) {}", "Emit"},
	{"string-slice", "func Bodies(s []Stored) []string { return nil }", "Bodies"},
	{"map-result", "func ByID(s []Stored) map[int64]string { return nil }", "ByID"},
	{"any-result", "func Any(s Stored) any { return nil }", "Any"},
	{"pointer-result", "func Ptr(s Stored) *string { return nil }", "Ptr"},
	{"generic-result", "func Gen[T any](s Stored) T { var z T; return z }", "Gen"},
	{"bytes-field-result", "type Out struct{ B []byte }\nfunc Bytes(s Stored) Out { return Out{} }", "Out.B"},
	{"exported-func-var", "var Peek = func(Stored) string { return \"\" }", "Peek"},
	{"wrapper-type", "type Wrapper struct{ S Stored }", "Wrapper.S"},
	{"no-stored-in-signature", "func DecodeText(b []byte) (string, string, error) { return \"\", \"\", nil }", "DecodeText"},
	{"field-on-rendered", "type rendered2 struct{ RawBody string }", "rendered2.RawBody"},
	{"method-on-unexported", "func (s stored) Text() string { return s.body }", "stored.Text"},
}

// TestGuardACatchesEveryRequiredMutant applies each required mutant to the source the ledger
// reads and requires the ledger to go RED naming it. POSITIVE CONTROL first: with no mutant the
// ledger is green, so a red below is the mutant's doing.
func TestGuardACatchesEveryRequiredMutant(t *testing.T) {
	if grew, shrank := diff(exportedNames(t, nil), ledgerNames()); len(grew)+len(shrank) != 0 {
		t.Fatalf("control: unmutated ledger is not green: %v %v", grew, shrank)
	}
	caught := 0
	for _, m := range guardAMutants {
		src := "package memo\n\nimport \"io\"\n\nvar _ io.Writer\n\n" + m.src + "\n"
		grew, _ := diff(exportedNames(t, map[string]string{"mutant_" + m.name + ".go": src}), ledgerNames())
		found := false
		for _, g := range grew {
			if g == m.adds {
				found = true
			}
		}
		if !found {
			t.Errorf("mutant %s SURVIVED: the ledger did not grow by %s (grew: %v)", m.name, m.adds, grew)
			continue
		}
		caught++
	}
	if caught != len(guardAMutants) {
		t.Fatalf("caught %d of %d Guard A mutants", caught, len(guardAMutants))
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
			v.Field(i).SetString(v.Type().Field(i).Name + "​" + string(rune(0xE0100)) + "x")
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
	for k, class := range exportLedger {
		if class == "emits" {
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
