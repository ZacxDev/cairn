package report

import (
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/codesrc"
)

// TestParseSourcesBodyInvertsRenderText: every status the renderer emits reads back to the same
// facts, behind a preamble — so the auditor's reading and the pod's writing are one contract. A
// count that disagrees with the lines, and a body with no sources line, are errors, never "undeclared".
func TestParseSourcesBodyInvertsRenderText(t *testing.T) {
	srcs := []string{"git:github.com/example-org/example-repo@main", "git:git.example.com/team/sub-group/example-repo@trunk"}
	rec := &codesrc.Record{Sources: srcs, SetBy: "scope-admin", SetAt: "2000-01-03T00:00:00Z", Revision: codesrc.Revision("alpha-notes", srcs)}
	empty := &codesrc.Record{Sources: []string{}, SetBy: "scope-admin", SetAt: "2000-01-03T00:00:00Z", Revision: codesrc.Revision("alpha-notes", []string{})}
	for _, tc := range []struct {
		rep    SourcesReport
		absent bool
		want   []string
	}{
		{SourcesReport{Status: StatusSourcesDeclared, Scope: "alpha-notes", Record: rec}, false, srcs},
		{SourcesReport{Status: StatusSourcesUndeclared, Scope: "alpha-notes", Record: empty}, false, nil},
		{SourcesReport{Status: StatusSourcesUndeclared, Scope: "alpha-notes", JournalAbsent: true}, true, nil},
		{SourcesReport{Status: StatusSourcesUnconfigured, Scope: "alpha-notes"}, false, nil},
		{SourcesReport{Status: StatusScopeAbsent, Scope: "alpha-notes"}, false, nil},
	} {
		got, err := ParseSourcesBody("store: a synthetic preamble\n\n" + tc.rep.RenderText())
		if err != nil {
			t.Fatalf("%s: %v", tc.rep.Status, err)
		}
		if got.Status != tc.rep.Status || got.Scope != "alpha-notes" || got.JournalAbsent != tc.absent ||
			strings.Join(got.Sources, "|") != strings.Join(tc.want, "|") {
			t.Fatalf("%s: read back %+v", tc.rep.Status, got)
		}
	}
	good := SourcesReport{Status: StatusSourcesDeclared, Scope: "alpha-notes", Record: rec}.RenderText()
	for name, body := range map[string]string{
		"no status line":   "sources=declared count=1\nsource=" + srcs[0],
		"a dropped source": strings.Replace(good, "\nsource="+srcs[1], "", 1),
		"an extra source":  good + "\nsource=" + srcs[0],
	} {
		if _, err := ParseSourcesBody(body); err == nil {
			t.Errorf("%s: must be an error, never a short list", name)
		}
	}
}
