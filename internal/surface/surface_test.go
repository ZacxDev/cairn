package surface

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const fixture = `package sample

import (
	"encoding/json"
	"io"
)

type Out struct {
	json.RawMessage
	inner
	Name, Kind string
	hidden     int
	Run        func(int) error
}

type inner struct{ Body []byte }

type Reader interface{ Read(p []byte) (n int, err error) }

type Alias = Out

type Reason string

func Emit(w io.Writer, s Out) (int, error) { return 0, nil }

func (o *Out) Raw(x, y string) string { return "" }

var Peek = func(Out) string { return "" }

var ErrX = io.EOF

const Limit = 3

const Typed Reason = "r"

func unexported() {}
`

// TestExportedReadsEverySurfaceShape is the reader's own ledger over a fixture holding every
// shape it claims to see — an embedded field from ANOTHER package, an embedded UNEXPORTED type,
// a func-typed field, an alias, a func-literal variable, a method with names to strip — pinned
// as a whole map, so a shape it stopped recording is red here before a ledger relies on it.
func TestExportedReadsEverySurfaceShape(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sample.go"), []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Exported(dir, "sample", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"Out":                 "struct",
		"Out.json.RawMessage": "embedded",
		"Out.inner":           "embedded",
		"Out.Name":            "string",
		"Out.Kind":            "string",
		"Out.Run":             "func(int) error",
		"inner.Body":          "[]byte",
		"Reader":              "interface",
		"Reader.Read":         "func([]byte) (int, error)",
		"Alias":               "= Out",
		"Reason":              "string",
		"Emit":                "func(io.Writer, Out) (int, error)",
		"Out.Raw":             "(*Out) func(string, string) string",
		"Peek":                "var = func(Out) string",
		"ErrX":                "var = io.EOF",
		"Limit":               "const",
		"Typed":               "const Reason",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("surface:\n got %v\nwant %v", got, want)
	}

	// An OVERRIDE that changes an existing signature is a CHANGE, and an EXTRA file is a GROW.
	changed, err := Exported(dir, "sample", map[string]string{"sample.go": fixture[:len(fixture)-len("func unexported() {}\n")] +
		"func Statuses(s ...Out) []string { return nil }\n"}, map[string]string{"x.go": "package sample\nfunc Extra() {}\n"})
	if err != nil {
		t.Fatal(err)
	}
	grew, shrank, moved := Diff(changed, want)
	if !Moved(grew, moved, "Statuses") || !Moved(grew, moved, "Extra") || len(shrank) != 0 {
		t.Fatalf("Diff did not report the two growths: grew %v shrank %v changed %v", grew, shrank, moved)
	}
	edited := map[string]string{}
	for k, v := range want {
		edited[k] = v
	}
	edited["Out.Name"] = "[]byte"
	_, _, ch := Diff(edited, want)
	if !Moved(nil, ch, "Out.Name") {
		t.Fatalf("a changed field type was not reported: %v", ch)
	}
	if _, err := Exported(dir, "sample", map[string]string{"nope.go": ""}, nil); err == nil {
		t.Fatal("an override naming no file was accepted — a mutant aimed at a renamed file would silently do nothing")
	}
}
