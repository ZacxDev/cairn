package doctor

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/ZacxDev/cairn/internal/store"
)

// 🔴 THE EXIT CODES, OWNED BY THE COMMAND AND DOCUMENTED BY IT. `Render` prints the legend on
// every run, so a caller never has to find a skill to learn what a number meant.
//
// 🔴 THEY ARE SCOPED PER COMMAND, NOT GLOBALLY UNIQUE, AND 9 IS SHARED ON PURPOSE. `doctor`
// returns 0/9/10. The client returns 0/3/4/5 for read outcomes, 6/7/8/9 for write outcomes, and
// 2 for USAGE — which is a third bucket, not a read outcome. So two numbers are shared: 0,
// which means success in both because that is what 0 means, and 9, which is this command's "a
// check MEASURED a problem" and `create`'s `ExitWriteExists`. Neither is ambiguous where a code
// is actually read — at ONE call site, which already knows the verb it invoked.
//
// 🔴 WHAT THE PER-COMMAND SCOPING DOES NOT COVER IS THE ONLY THING TO PROTECT: a code a
// `doctor` invocation can return ALONGSIDE these. 2 (a bad flag) is one. 1 is the other, and it
// is the more insidious, because it is not any client constant — nothing in the nine client
// codes is 1 — so a set-intersection ledger against the client cannot see it and it has to be
// asserted separately. BOTH must stay clear of the three below: a doctor code of 1 would be
// indistinguishable from a crash, exactly as a doctor code of 2 would be from a usage error.
//
// 🔴 SO DO NOT RENUMBER THE 9 TO MAKE IT UNIQUE, and do not read the overlap as licence to add
// another. Renumbering changes a contract this command PRINTS, to remove a collision that was
// never a defect. A NEW overlap is safe only if it too is unreachable from any one call site,
// which is a fact about the dispatcher and not about which numbers happen to look free.
const (
	ExitOK         = 0
	ExitProblem    = 9
	ExitUnmeasured = 10
)

// LegendRow is one `(code, why)` pair of the printed legend.
type LegendRow struct {
	Code int
	Why  string
}

// ExitLegend is the legend, and it is THE one definition: `--help`'s epilog and every run's own
// footer both render from here, so no caller can restate the numbers wrongly.
var ExitLegend = []LegendRow{
	{ExitOK, "every check measured, and every answer is fine"},
	{ExitProblem, "at least one check MEASURED a problem"},
	{ExitUnmeasured, "no problem measured, but at least one check COULD NOT LOOK — " +
		"this is not a clean bill of health"},
}

// ExitCodes is doctor's own code set as data, so a ledger can DISCOVER it rather than hand-list
// it. The Python side's equivalent is discovered by AST; a compiled binary needs this.
func ExitCodes() map[string]int {
	return map[string]int{
		"EXIT_DOCTOR_OK":         ExitOK,
		"EXIT_DOCTOR_PROBLEM":    ExitProblem,
		"EXIT_DOCTOR_UNMEASURED": ExitUnmeasured,
	}
}

// ExitCode is PROBLEM outranks UNMEASURED outranks OK.
//
// 🔴 `NOT-OBSERVABLE` CONTRIBUTES NOTHING, and that is deliberate. It marks a fact no client can
// reach, so escalating on it would make this command non-zero on every healthy run forever —
// the permanently-red gate that trains everyone to click through.
func ExitCode(checks []Check) int {
	states := map[string]struct{}{}
	for _, c := range checks {
		states[c.State] = struct{}{}
	}
	if _, bad := states[Problem]; bad {
		return ExitProblem
	}
	if _, blind := states[Unmeasured]; blind {
		return ExitUnmeasured
	}
	return ExitOK
}

var markers = map[string]string{
	OK: "  ", Problem: "🔴", Unmeasured: "⚠ ", NotObservable: "· ",
}

// ParseRow is the INVERSE of `Render`: it splits one rendered line into its NAME and STATE
// columns, and returns `("", "")` for a line that is not a check row — a heading, a legend row,
// the verdict, a blank.
//
// 🔴 THE GLYPHS ARE NOT ALL THE SAME WIDTH, WHICH IS WHY A FIXED OFFSET AND A `TrimSpace` ARE
// BOTH WRONG. `OK` is two SPACES, `Problem` is a single astral rune, and the other two are a
// rune plus a space — so a rune offset that finds the name on one state misses it on another,
// and `strings.TrimSpace(line)` reaches the name on `OK` rows ONLY, silently skipping every
// other state. A guard written that way is true of whichever states its fixture happens to
// produce. Measured: `internal/client`'s one-instance `doctor` fixture renders 2 OK rows and 5
// non-OK ones, and the `TrimSpace` spelling inspected the 2. The state is checked against
// `States` so a footer or legend line cannot be mistaken for a row.
//
// 🔴 THIS IS A FUNCTION RATHER THAN AN EXPORTED `markers` TABLE, AND THE DIFFERENCE IS THE ONE
// THE DEFECT ENTRY NAMED. Handing a consumer the table exports a RENDERING DETAIL and invites
// every consumer to write its own stripping loop — which is the same predicate at N sites,
// wrong at N−1 of them in the same direction. `markers` is now unexported and `Render` and this
// function are its only readers, so there is nothing for a second copy to disagree with.
//
// ⚠ IT HAS NO PRODUCTION CALLER, AND SAYING SO IS BETTER THAN LEAVING IT TO BE FOUND. Its
// callers are tests, in this package and in `internal/client` — the assertion they make is
// genuinely about the compiled client's STDOUT, so the parsing has to live somewhere. The
// choice is where, not whether, and beside the renderer it inverts is the only place where a
// glyph change cannot silently outrun it.
func ParseRow(line string) (name, state string) {
	for _, marker := range markers {
		rest, found := strings.CutPrefix(line, marker)
		if !found {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) < 2 {
			continue
		}
		for _, known := range States {
			if fields[1] == known {
				return fields[0], known
			}
		}
	}
	return "", ""
}

// Render is the report, plus the exit legend, plus a one-line verdict.
//
// ⚠ THE COLUMN WIDTHS ARE COUNTED IN CODE POINTS, NOT BYTES, because the oracle's `ljust` and
// `:<14` are `str` operations. Every check name and state here is ASCII, so the two agree today
// — the rune count is used anyway, so that a non-ASCII check name added later does not silently
// shift one column on one implementation only.
func Render(checks []Check) string {
	width := 0
	for _, c := range checks {
		if n := utf8.RuneCountInString(c.Name); n > width {
			width = n
		}
	}
	var lines []string
	for _, c := range checks {
		lines = append(lines, fmt.Sprintf("%s %s  %s %s",
			markers[c.State], ljust(c.Name, width), ljust(c.State, 14), c.Detail))
	}
	code := ExitCode(checks)
	counts := countStates(checks)
	var parts []string
	for _, s := range States {
		parts = append(parts, fmt.Sprintf("%s=%d", s, counts[s]))
	}
	lines = append(lines, "")
	lines = append(lines, "checks: "+strings.Join(parts, "  ")+fmt.Sprintf("   -> exit %d", code))
	var legend []string
	for _, row := range ExitLegend {
		legend = append(legend, fmt.Sprintf("%d = %s", row.Code, row.Why))
	}
	lines = append(lines, "exit codes: "+strings.Join(legend, "; "))
	return strings.Join(lines, "\n")
}

func countStates(checks []Check) map[string]int {
	counts := map[string]int{}
	for _, s := range States {
		counts[s] = 0
	}
	for _, c := range checks {
		counts[c.State]++
	}
	return counts
}

func ljust(s string, width int) string {
	if pad := width - utf8.RuneCountInString(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}

// JSON is `json.dumps(to_dict(checks), indent=2)`, byte for byte.
//
// 🔴 HAND-WRITTEN BECAUSE `encoding/json` CANNOT PRODUCE THESE BYTES. Three differences, each
// of which the parity gate would fail on: Go sorts map keys where CPython preserves insertion
// order; Go escapes `<`, `>` and `&` as `<`/`>`/`&` by default where CPython does
// not; and CPython's `ensure_ascii=True` default escapes every non-ASCII character as `\uXXXX`
// where Go emits it raw. Every `detail` here carries an em dash, so the third difference is not
// a corner case — it is on EVERY line.
//
// ⚠ THIS IS A WRITER FOR THIS ONE DOCUMENT SHAPE, NOT A GENERAL ENCODER. It handles the four
// value kinds `to_dict` produces (object, array, string, int) and nothing else, deliberately: a
// general Python-compatible encoder would be a second serialiser to keep true.
func JSON(checks []Check) string {
	var b strings.Builder
	b.WriteString("{\n")
	b.WriteString(`  "checks": [`)
	if len(checks) == 0 {
		b.WriteString("]")
	} else {
		b.WriteString("\n")
		for i, c := range checks {
			b.WriteString("    {\n")
			b.WriteString(`      "name": ` + pyJSONString(c.Name) + ",\n")
			b.WriteString(`      "state": ` + pyJSONString(c.State) + ",\n")
			b.WriteString(`      "detail": ` + pyJSONString(c.Detail) + "\n")
			b.WriteString("    }")
			if i < len(checks)-1 {
				b.WriteString(",")
			}
			b.WriteString("\n")
		}
		b.WriteString("  ]")
	}
	b.WriteString(",\n")
	counts := countStates(checks)
	b.WriteString(`  "counts": {` + "\n")
	for i, s := range States {
		b.WriteString(fmt.Sprintf(`    %s: %d`, pyJSONString(s), counts[s]))
		if i < len(States)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("  },\n")
	b.WriteString(fmt.Sprintf(`  "exit": %d`, ExitCode(checks)) + ",\n")
	b.WriteString(`  "exit_legend": {` + "\n")
	for i, row := range ExitLegend {
		b.WriteString(fmt.Sprintf(`    %s: %s`,
			pyJSONString(fmt.Sprintf("%d", row.Code)), pyJSONString(row.Why)))
		if i < len(ExitLegend)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("  }\n")
	b.WriteString("}")
	return b.String()
}

// pyJSONString is `store.PyJSONString`, reached through one name so this file's call sites read
// like the document they produce. 🔴 IT IS NOT A SECOND IMPLEMENTATION — `append`'s request body
// needs the identical escaping rules, and two copies would let one surface exercise a
// server-side guard the other never reaches.
func pyJSONString(s string) string { return store.PyJSONString(s) }
