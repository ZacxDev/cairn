package report

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/store"
)

// 🔴 THESE ARE THE CONTRACT WITNESSES FOR THE SESSIONS REPORT. The route is Go-only, so there is
// no oracle to record goldens from; the conformance goldens for `sessions/<scope>` are recorded
// from `cmd/cairn-server` and only detect change. What the report MUST say is pinned here, as
// WHOLE strings — a guard on words is walkable by rewording, and these sentences are the coverage
// contract an empty list depends on.
//
// Every name, session id and date below is synthetic (`*-bot` actors, year-2000 dates). The
// counts are chosen so the numbers that share a line are pairwise distinct (entries 2, bullets
// 12, attributed 7, none 5, refused 1) and so each session's bullet count differs (4, 1, 2):
// a renderer that printed the wrong counter, or one session's count on another's row, prints a
// different string.

const sessionsAlphaOne = "---\nservice: gadget-one\nscope: alpha-notes\n---\n\n" +
	"## What it is\nSynthetic. [cairn: omega-bot/s-9999]\n\n" +
	"## Nuance / work-history\n" +
	// File order is s-0003, s-0001, s-0002 — NOT the rendered order, which is by id.
	"- 2000-01-07: first [cairn: delta-bot/s-0003]\n" +
	"- 2000-01-05: second [cairn: alpha-bot/s-0001]\n" +
	"- 2000-01-03: third [cairn: alpha-bot/s-0001]\n" +
	"- 2000-01-04: two in a run [cairn: beta-bot/s-0002] [cairn: gamma-bot/s-0002]\n" +
	"- 2000-01-02: plain one\n" +
	"- 2000-01-09: later [cairn: delta-bot/s-0003]\n" +
	"- 2000-01-02: plain two\n"

const sessionsAlphaTwo = "---\nservice: gizmo-two\nscope: alpha-notes\n---\n\n" +
	"## Nuance / work-history\n" +
	"- undated [cairn: alpha-bot/s-0001]\n" +
	"- 2000-01-06: refused [cairn: Bad/s-0008]\n" +
	"- 2000-01-04: fourth [cairn: alpha-bot/s-0001]\n" +
	"- 2000-01-08: plain three\n" +
	"- 2000-01-08: plain four\n"

const sessionsUntrailered = "---\nservice: widget-three\nscope: beta-notes\n---\n\n" +
	"## Nuance / work-history\n- 2000-01-02: nobody signed this\n- 2000-01-03: nor this\n"

func sessionsStore(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	put := func(rel, body string) {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put("alpha-notes/gadget-one.md", sessionsAlphaOne)
	put("alpha-notes/gizmo-two.md", sessionsAlphaTwo)
	put("beta-notes/widget-three.md", sessionsUntrailered)
	// One good entry beside one the index REJECTS (no `service:`): a partial scan.
	put("delta-notes/doodad-four.md", "---\nservice: doodad-four\nscope: delta-notes\n---\n\n"+
		"## Nuance / work-history\n- 2000-01-05: kept [cairn: zeta-bot/s-0005]\n")
	put("delta-notes/broken-five.md", "---\nscope: delta-notes\n---\n")
	// Every entry rejected: nothing scanned.
	put("gamma-notes/broken-six.md", "---\nscope: gamma-notes\n---\n")
	if err := os.MkdirAll(filepath.Join(root, "hollow-set"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

const sessionsHeaderLines = "  coverage: writes measured from entry trailers · reads NOT recorded (not collected in this phase)\n" +
	"  attribution: trailers are self-reported — actor as written in the entry (only appended bullets had it set by the pod); session ids are declared by the writer\n"

func TestTheSessionsReportIsPinnedAsWholeStringsPerStatus(t *testing.T) {
	root := sessionsStore(t)
	for _, tc := range []struct {
		scope, status, want string
		exit                int
	}{
		{"alpha-notes", StatusSessionsListed,
			"cairn-sessions: status=sessions-listed scope=alpha-notes\n" + sessionsHeaderLines +
				"  scanned: 2 of 2 entries · 0 unreadable · 0 rejected by the index · 1 bullets with a refused trailer\n" +
				"  attributed: 7 of 12 bullets carry a write trailer (5 have none — their writers are NOT listed)\n" +
				"\n" +
				"sessions: 3 distinct, ordered by session id (byte-wise); dates are each bullet's own `- YYYY-MM-DD:` opener\n" +
				"- s-0001 · actor alpha-bot · 4 bullets · 2000-01-03 → 2000-01-05 (+1 undated)\n" +
				"- s-0002 · actors beta-bot, gamma-bot · 1 bullet · 2000-01-04\n" +
				"- s-0003 · actor delta-bot · 2 bullets · 2000-01-07 → 2000-01-09", 0},
		{"beta-notes", StatusNoAttributedWrites,
			"cairn-sessions: status=no-attributed-writes scope=beta-notes\n" + sessionsHeaderLines +
				"  scanned: 1 of 1 entries · 0 unreadable · 0 rejected by the index · 0 bullets with a refused trailer\n" +
				"  attributed: 0 of 2 bullets carry a write trailer (2 have none — their writers are NOT listed)\n" +
				"\n" +
				"NO ATTRIBUTED WRITES — 2 bullet(s) in `beta-notes/` were scanned and none carries a write trailer. Their writers are UNKNOWN, not absent: a bullet written before trailers existed, or through `put`/`create` without one, names nobody.", 0},
		{"hollow-set", StatusScopeEmpty,
			"cairn-sessions: status=scope-empty scope=hollow-set\n" + sessionsHeaderLines +
				"  scanned: 0 of 0 entries · 0 unreadable · 0 rejected by the index · 0 bullets with a refused trailer\n" +
				"  attributed: 0 of 0 bullets carry a write trailer (0 have none — their writers are NOT listed)\n" +
				"\n" +
				"`hollow-set/` EXISTS AND HOLDS NO ENTRY — nothing was ever recorded there, so no session wrote there through an entry.", 0},
		{"gamma-notes", StatusScopeUnreadable,
			"cairn-sessions: status=scope-unreadable scope=gamma-notes\n" + sessionsHeaderLines +
				"  scanned: 0 of 1 entries · 0 unreadable · 1 rejected by the index · 0 bullets with a refused trailer\n" +
				"  attributed: 0 of 0 bullets carry a write trailer (0 have none — their writers are NOT listed)\n" +
				"\n" +
				"NOTHING IN `gamma-notes/` COULD BE SCANNED — every entry file was rejected by the index or could not be read. This is NOT an empty scope and NOT 'no attributed writes': the bullets were never looked at. `cairn validate --scope gamma-notes` names each file that fails to parse.", 3},
		{"delta-notes", StatusSessionsListed,
			"cairn-sessions: status=sessions-listed scope=delta-notes\n" + sessionsHeaderLines +
				"  scanned: 1 of 2 entries · 0 unreadable · 1 rejected by the index · 0 bullets with a refused trailer\n" +
				"  attributed: 1 of 1 bullets carry a write trailer (0 have none — their writers are NOT listed)\n" +
				"  ⚠ LOWER BOUND — 1 entry file(s) in `delta-notes/` were never scanned, so a session that wrote only there is NOT listed. `cairn validate --scope delta-notes` names each file that fails to parse.\n" +
				"\n" +
				"sessions: 1 distinct, ordered by session id (byte-wise); dates are each bullet's own `- YYYY-MM-DD:` opener\n" +
				"- s-0005 · actor zeta-bot · 1 bullet · 2000-01-05", 0},
		{"ghost-void", StatusScopeAbsent,
			"cairn-sessions: status=scope-absent scope=ghost-void\n" + sessionsHeaderLines +
				"\n" +
				"NO SCOPE `ghost-void/` IS READABLE HERE. A scope that does not exist and one this credential may not read answer identically, by design; a cache that has not synced it answers the same way. Nothing was scanned, so nothing can be concluded about who wrote there.", 0},
	} {
		t.Run(tc.scope, func(t *testing.T) {
			rep, err := Sessions(root, tc.scope, store.Unrestricted())
			if err != nil {
				t.Fatal(err)
			}
			if rep.Status != tc.status {
				t.Fatalf("status %q, want %q", rep.Status, tc.status)
			}
			if got := rep.RenderText(); got != tc.want {
				t.Fatalf("the rendered report moved.\n--- got\n%s\n--- want\n%s", got, tc.want)
			}
			if code, _ := rep.Exit(); code != tc.exit {
				t.Fatalf("exit %d, want %d", code, tc.exit)
			}
		})
	}
}

func TestTheFourEmptyLookingStatusesShareNoBodySentence(t *testing.T) {
	// 🔴 "a real empty must read differently from 'no attributed bullets' and from 'could not
	// read'" — as a RELATIONSHIP, not four independent pins: the body paragraph (everything
	// after the blank line) of each must appear in no other's rendering.
	root := sessionsStore(t)
	bodies := map[string]string{}
	full := map[string]string{}
	for _, scope := range []string{"hollow-set", "beta-notes", "gamma-notes", "ghost-void"} {
		rep, err := Sessions(root, scope, store.Unrestricted())
		if err != nil {
			t.Fatal(err)
		}
		text := rep.RenderText()
		_, body, ok := strings.Cut(text, "\n\n")
		if !ok || body == "" {
			t.Fatalf("%s: no body paragraph in %q", scope, text)
		}
		bodies[rep.Status] = body
		full[rep.Status] = text
	}
	if len(bodies) != 4 {
		t.Fatalf("four scopes produced %d distinct statuses: %v", len(bodies), bodies)
	}
	for a, body := range bodies {
		for b, text := range full {
			if a != b && strings.Contains(text, body) {
				t.Fatalf("the %s body appears in the %s rendering — the two read alike", a, b)
			}
		}
	}
}

func TestTheRefusedScopeRendersExactlyLikeTheAbsentOne(t *testing.T) {
	// 🔴 THE AUTHORISATION IS THE INDEX'S NARROWING, NOT A SECOND CHECK — so a scope the caller
	// may not read must render byte-for-byte what a never-existed scope renders, up to the name.
	// The positive control is the same scope read with it ALLOWED, which must list sessions.
	root := sessionsStore(t)
	narrow := store.VisibleScopeSet([]string{"beta-notes"})
	refused, err := Sessions(root, "alpha-notes", narrow)
	if err != nil {
		t.Fatal(err)
	}
	absent, err := Sessions(root, "ghost-void", narrow)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.ReplaceAll(refused.RenderText(), "alpha-notes", "<SCOPE>")
	want := strings.ReplaceAll(absent.RenderText(), "ghost-void", "<SCOPE>")
	if got != want {
		t.Fatalf("a refused scope is distinguishable from an absent one:\n%s\n---\n%s", got, want)
	}
	allowed, err := Sessions(root, "alpha-notes", store.Unrestricted())
	if err != nil {
		t.Fatal(err)
	}
	if allowed.Status != StatusSessionsListed || !strings.Contains(allowed.RenderText(), "- s-0001 ·") {
		t.Fatalf("the positive control failed: an allowed read must list sessions, got %s", allowed.Status)
	}
}

func TestTheUnreadableWarningIsThisReportsOwnAndTheCodeIsExitFors(t *testing.T) {
	root := sessionsStore(t)
	rep, err := Sessions(root, "gamma-notes", store.Unrestricted())
	if err != nil {
		t.Fatal(err)
	}
	code, warning := rep.Exit()
	want := "cairn-sessions: scope-unreadable: none of the 1 entry file(s) under `gamma-notes/` could be scanned (1 rejected by the index, 0 unreadable) — this is NOT an empty scope and NOT 'no attributed writes'."
	if code != 3 || warning != want {
		t.Fatalf("got (%d, %q)\nwant (3, %q)", code, warning, want)
	}
	// The code is `ExitFor`'s decision, so the two agree for every status.
	for _, status := range []string{StatusScopeAbsent, StatusScopeEmpty, StatusScopeUnreadable,
		StatusNoAttributedWrites, StatusSessionsListed} {
		wantCode, _ := ExitFor(status, "x/", nil)
		gotCode, _ := SessionsReport{Status: status}.Exit()
		if gotCode != wantCode {
			t.Fatalf("%s: Exit()=%d, ExitFor=%d", status, gotCode, wantCode)
		}
	}
}

func TestAMissingStoreIsTheStoresOwnErrorNotAnAbsentScope(t *testing.T) {
	_, err := Sessions(filepath.Join(t.TempDir(), "nowhere"), "alpha-notes", store.Unrestricted())
	var missing *store.StoreMissingError
	if !errors.As(err, &missing) {
		t.Fatalf("a missing store root must be *store.StoreMissingError (the pod's 503), got %v", err)
	}
}
