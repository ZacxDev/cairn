package report

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// THE ARC READ VIEWS — `arc/<home>/<slug>?view=outline|page`, opt-in, beside the full answer.
//
// A summary may hold up to `arcs.MaxSummaryItems` items per list and `arcs.MaxSummaryBytes` in all,
// and a member list up to `arcs.MaxMembers` rows, so the full answer can be larger than a reader
// with a byte budget wants in one read. The outline says how big each section is; a page returns a
// bounded run of ONE section's items, each tagged with its stored index so a reader can cite it and
// resume after it.
//
// 🔴 `X-Arc-View` IS SENT ONLY WHEN THE REQUEST NAMED A VIEW (`view=` present, `full` included), so a
// request that names none is byte-identical to the pre-views answer in its body AND its headers.
//
// 🔴 THE FULL VIEW IS THE DEFAULT AND ITS BYTES DO NOT MOVE. No parameter (or `view=full`) renders
// `ArcReport.RenderText`, unchanged; the views are additions a caller must ask for.
//
// 🔴 EVERY VIEW IS DECIDED AFTER THE VISIBILITY CHECK, NEVER BEFORE IT. `RenderView` renders a view
// only for an `arc-found` report; every other status — `arc-unregistered` (absent OR hidden) and
// `registrations-unconfigured` — is answered with `RenderText`'s bytes whatever view was asked for,
// so a hidden arc is identical to an absent one in every view.

// The view names, as `?view=` spells them and as `X-Arc-View` echoes them.
const (
	ArcViewFull    = "full"
	ArcViewOutline = "outline"
	ArcViewPage    = "page"
)

// The pageable sections, as `?section=` spells them.
const (
	ArcSectionStateNow  = "state_now"
	ArcSectionNextSteps = "next_steps"
	ArcSectionMembers   = "members"
)

// ArcViews and ArcSections are the accepted values, in the order a refusal names them.
var (
	ArcViews    = []string{ArcViewFull, ArcViewOutline, ArcViewPage}
	ArcSections = []string{ArcSectionStateNow, ArcSectionNextSteps, ArcSectionMembers}
)

// Page bounds. A page stops at whichever of `limit` items or `max_bytes` rendered bytes comes first,
// but always carries at least one item when any remain, so an item larger than the byte budget is
// still readable (a page that returned nothing would never advance).
//
// The byte ceiling is above `arcs.MaxSummaryBytes`, so one page CAN hold a whole summary list; the
// default is a third of it.
const (
	ArcPageDefaultLimit    = 20
	ArcPageMaxLimit        = 200
	ArcPageDefaultMaxBytes = 8192
	ArcPageMaxMaxBytes     = 64 * 1024
)

// ArcView is one request's choice of view. The zero value is not valid; `FullArcView` is the default.
type ArcView struct {
	Mode     string
	Section  string
	From     int
	Limit    int
	MaxBytes int
	// At is the summary commit the caller last read the outline at, or "" — a page then says when the
	// stored summary was read at a different commit.
	At string
	// Named is whether the request named a view at all (`view=` present, `full` included). Only then
	// does the answer carry `X-Arc-View`; a request naming none gets the pre-views headers exactly.
	Named bool
}

// FullArcView is the view a request that names none gets.
func FullArcView() ArcView { return ArcView{Mode: ArcViewFull} }

// ArcViewError is a refused view request: its parameters, which alone decide it.
// The pod answers it as a 400 carrying Message.
type ArcViewError struct{ Message string }

func (e *ArcViewError) Error() string { return e.Message }

func viewErr(format string, args ...any) error {
	return &ArcViewError{Message: fmt.Sprintf(format, args...)}
}

// atCommit is what `at=` must look like: the stored source commit's own shape (`arcs` accepts 40 or
// 64 lowercase hex), so a typo is refused instead of reading as "the summary changed".
var atCommit = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

// ValidateArcView is every check that depends on the parameters alone, so it answers the same for
// an arc that exists, one that does not, and one this caller may not see.
func ValidateArcView(v ArcView) error {
	if !slices.Contains(ArcViews, v.Mode) {
		return viewErr("view must be one of %s, got %q", strings.Join(ArcViews, ", "), v.Mode)
	}
	if v.Mode != ArcViewPage {
		return nil
	}
	if !slices.Contains(ArcSections, v.Section) {
		return viewErr("view=page needs section, one of %s, got %q", strings.Join(ArcSections, ", "), v.Section)
	}
	if v.From < 0 {
		return viewErr("from must be an integer >= 0, got %d", v.From)
	}
	if v.Limit < 1 || v.Limit > ArcPageMaxLimit {
		return viewErr("limit must be an integer from 1 to %d, got %d", ArcPageMaxLimit, v.Limit)
	}
	if v.MaxBytes < 1 || v.MaxBytes > ArcPageMaxMaxBytes {
		return viewErr("max_bytes must be an integer from 1 to %d, got %d", ArcPageMaxMaxBytes, v.MaxBytes)
	}
	if v.At != "" && !atCommit.MatchString(v.At) {
		return viewErr("at must be a 40- or 64-character lowercase hex commit, got %q", v.At)
	}
	return nil
}

// RenderView renders the requested view. See the file comment for why every status but `arc-found`,
// and the full view, are `RenderText` itself.
func (r ArcReport) RenderView(v ArcView) string {
	if r.Status != StatusArcFound || v.Mode == ArcViewFull {
		return r.RenderText()
	}
	if v.Mode == ArcViewOutline {
		return r.renderOutline()
	}
	return r.renderPage(v)
}

// sectionItems is a section's items as a page prints them (without the tag), in stored order —
// members in `SortedMembers` order, the order every surface prints them, so an index is stable for
// one registration.
func (r ArcReport) sectionItems(section string) []string {
	switch section {
	case ArcSectionStateNow:
		if r.Reg.Summary != nil {
			return r.Reg.Summary.StateNow
		}
	case ArcSectionNextSteps:
		if r.Reg.Summary != nil {
			return r.Reg.Summary.NextSteps
		}
	case ArcSectionMembers:
		var out []string
		for _, m := range r.SortedMembers() {
			out = append(out, r.MemberLine(m))
		}
		return out
	}
	return nil
}

// summaryCommit is the stored summary's source commit, or "" when the registration has none.
func (r ArcReport) summaryCommit() string {
	if r.Reg.Summary == nil {
		return ""
	}
	return r.Reg.Summary.Source.Commit
}

// ArcNoSummaryLine stands where the summary's provenance line would be, for a registration that
// carries no summary (schema 1, or a push that cleared it).
const ArcNoSummaryLine = "summary: none — this registration carries no summary"

func (r ArcReport) provenanceLine() string {
	if lines := r.SummaryLines(); len(lines) > 0 {
		return lines[0]
	}
	return ArcNoSummaryLine
}

func itemBytes(items []string) int {
	n := 0
	for _, item := range items {
		n += len(item)
	}
	return n
}

// renderOutline is the arc's head, the summary's short fields, and one size line per section — the
// answer a reader with a budget reads first, to decide which section to page.
func (r ArcReport) renderOutline() string {
	var b strings.Builder
	r.writePreamble(&b, " view="+ArcViewOutline)
	r.writeHead(&b)
	b.WriteString("\n" + r.provenanceLine() + "\n")
	if s := r.Reg.Summary; s != nil {
		if s.ClosingCondition != "" {
			b.WriteString("closing condition (stated): " + s.ClosingCondition + "\n")
		}
		b.WriteString("task refs: " + orNone(s.TaskRefs) + "\naliases: " + orNone(s.Aliases) + "\n")
	}
	b.WriteString("\n")
	for _, section := range []string{ArcSectionStateNow, ArcSectionNextSteps} {
		items := r.sectionItems(section)
		fmt.Fprintf(&b, "%s: %d item%s, %d B\n", section, len(items), plural(len(items)), itemBytes(items))
	}
	fmt.Fprintf(&b, "%s: %d\n", ArcSectionMembers, len(r.Reg.Members))
	at := ""
	if c := r.summaryCommit(); c != "" {
		at = "&at=" + c
	}
	fmt.Fprintf(&b, "page a section: ?view=page&section=<%s>&from=0%s (limit default %d, at most %d; max_bytes default %d) "+
		"— or cairn arc-show --section <name> --from 0",
		strings.Join(ArcSections, "|"), at, ArcPageDefaultLimit, ArcPageMaxLimit, ArcPageDefaultMaxBytes)
	return b.String()
}

// renderPage is a bounded run of one section's items, each tagged `[<section>#<index>]` with its
// stored index, ending in `more: from=<next>` or `end`.
func (r ArcReport) renderPage(v ArcView) string {
	items := r.sectionItems(v.Section)
	var b strings.Builder
	r.writePreamble(&b, " view="+ArcViewPage)
	b.WriteString(r.provenanceLine() + "\n")
	if v.At != "" && v.At != r.summaryCommit() {
		fmt.Fprintf(&b, "summary changed since %s; re-read the outline\n", v.At)
	}
	if v.Section == ArcSectionMembers {
		b.WriteString(r.MemberWritesLine() + "\n")
	}
	var lines []string
	used := 0
	next := v.From
	for next < len(items) && len(lines) < v.Limit {
		line := fmt.Sprintf("[%s#%d] %s\n", v.Section, next, items[next])
		if len(lines) > 0 && used+len(line) > v.MaxBytes {
			break
		}
		lines = append(lines, line)
		used += len(line)
		next++
	}
	if len(lines) == 0 {
		fmt.Fprintf(&b, "%s: no items from %d (%d in all)\n", v.Section, v.From, len(items))
	} else {
		fmt.Fprintf(&b, "%s: items %d-%d of %d\n", v.Section, v.From, next-1, len(items))
	}
	for _, line := range lines {
		b.WriteString(line)
	}
	if next < len(items) {
		fmt.Fprintf(&b, "more: from=%d", next)
	} else {
		b.WriteString("end")
	}
	return b.String()
}
