package ui

import (
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/transcript/transcripttest"
)

// 🔴 "MY SESSIONS" THROUGH THE REAL DISPATCHER (S7 of the transcripts plan), over `transcriptWorld`:
// the owner uploaded s-full-01 (V = {alpha, beta, gamma}) and s-solo-02 (V = {}, owner-only); `every`
// reads every scope of s-full-01 and opens it, and uploaded one session of its own.

// listedSessions is every `data-session` row on a "My sessions" page, in order.
func listedSessions(body string) []string {
	var out []string
	for _, m := range regexp.MustCompile(`<li class="entry-row session-row" data-session="([^"]+)"`).FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	return out
}

// TestMySessionsListsTheViewersOwnAndNoOneElses — S7's test plan: the owner's page lists its
// owner-only AND its shared session, each linking a session page that opens; `every`, who reads every
// scope of the owner's shared session and OPENS it (instrument control), does not see it listed on
// ITS page — it lists only its own. RED under `my-sessions-lists-another-owner`.
func TestMySessionsListsTheViewersOwnAndNoOneElses(t *testing.T) {
	w := transcriptWorld(t)
	a := w.archive(t)
	everyKey := string(control.KindUser) + ":" + string(trID(control.PrefixUser, "every"))
	transcripttest.Append(t, a, transcripttest.Session{Root: "s-every-04", Owner: everyKey}, "main", []string{trUser("every's own")})

	owner := getAs(t, w.server(t, "owner", a), MySessionsPath)
	if owner.Code != http.StatusOK {
		t.Fatalf("the owner's page answered %d", owner.Code)
	}
	if got := listedSessions(owner.Body.String()); !slices.Equal(got, []string{"s-full-01", "s-solo-02"}) {
		t.Errorf("the owner lists %v, want its shared s-full-01 AND its owner-only s-solo-02, and not every's", got)
	}
	for _, root := range []string{"s-full-01", "s-solo-02"} {
		if !strings.Contains(owner.Body.String(), `href="`+sessionHref(root)+`"`) {
			t.Errorf("the owner's row for %s does not link its session page", root)
		}
		// Each link OPENS — the owner-only one included, which no other surface links.
		if rec := getAs(t, w.server(t, "owner", a), sessionURL(root)); rec.Code != http.StatusOK ||
			!strings.Contains(rec.Body.String(), `id="transcript"`) {
			t.Errorf("the listed %s does not open with its transcript (%d)", root, rec.Code)
		}
	}

	// INSTRUMENT CONTROL: `every` opens the owner's shared session, transcript and all — so a list by
	// visibility WOULD carry it.
	if rec := getAs(t, w.server(t, "every", a), sessionURL("s-full-01")); rec.Code != http.StatusOK ||
		!strings.Contains(rec.Body.String(), trSecretText) {
		t.Fatalf("INSTRUMENT CONTROL: every cannot open s-full-01 (%d), so its absence below proves nothing", rec.Code)
	}
	every := getAs(t, w.server(t, "every", a), MySessionsPath).Body.String()
	if got := listedSessions(every); !slices.Equal(got, []string{"s-every-04"}) {
		t.Errorf("every lists %v, want exactly its own s-every-04 — not the owner's session it can open", got)
	}
	for _, leak := range []string{"s-full-01", "s-solo-02"} {
		if strings.Contains(every, leak) {
			t.Errorf("every's page names %q, which is not its session", leak)
		}
	}
}

// TestMySessionsIsTheEmptyStateForAViewerWhoOwnsNothing: a signed-in viewer who uploaded nothing, and
// the owner's own NARROWED credential, each get a 200 with the empty sentence and no row — never a
// 404 and never another owner's session.
func TestMySessionsIsTheEmptyStateForAViewerWhoOwnsNothing(t *testing.T) {
	w := transcriptWorld(t)
	a := w.archive(t)
	narrowed := w.ids["owner"]
	narrowed.Auth = control.Narrow(narrowed.Auth, []control.ID{trID(control.PrefixScope, "alpha-notes")}, nil)
	if !narrowed.Auth.Narrowed() {
		t.Fatal("CONTROL: the narrowed credential does not report itself narrowed")
	}
	w.ids["owner-narrowed"] = narrowed
	for _, user := range []string{"none", "one", "owner-narrowed"} {
		rec := getAs(t, w.server(t, user, a), MySessionsPath)
		body := rec.Body.String()
		if rec.Code != http.StatusOK || !strings.Contains(normalizeSpace(visibleText(body)), normalizeSpace(mySessionsEmpty)) {
			t.Errorf("%s: answered %d without the empty state", user, rec.Code)
		}
		if got := listedSessions(body); len(got) != 0 || strings.Contains(body, "s-full-01") || strings.Contains(body, "s-solo-02") {
			t.Errorf("%s: lists %v — it owns nothing", user, got)
		}
	}
}

// TestMySessionsWithNoStoreIsTheEmptyState — S4's convention for a deployment with no transcript
// store: the same bytes as a store holding nothing of the viewer's, a 200 rather than a 404.
func TestMySessionsWithNoStoreIsTheEmptyState(t *testing.T) {
	w := transcriptWorld(t)
	off := getAs(t, w.server(t, "owner", nil), MySessionsPath)
	empty := getAs(t, w.server(t, "owner", transcripttest.OpenAt(t, w.clock.Now)), MySessionsPath)
	if off.Code != http.StatusOK || empty.Code != http.StatusOK || off.Body.String() != empty.Body.String() {
		t.Errorf("no store answers %d (%d bytes) and an empty store %d (%d bytes); want one 200, byte for byte",
			off.Code, off.Body.Len(), empty.Code, empty.Body.Len())
	}
	// POSITIVE CONTROL: the same viewer over a store holding its sessions gets a different page.
	if held := getAs(t, w.server(t, "owner", w.archive(t)), MySessionsPath).Body.String(); held == empty.Body.String() {
		t.Error("POSITIVE CONTROL FAILED: the owner's page is the same with its sessions stored as with none")
	}
}

// TestMySessionsIsPagedNewestFirst: 120 sessions, each last uploaded an hour after the one before —
// so newest first is the REVERSE of upload order and of id order. Three pages of 50/50/20, each in
// order and disjoint, together every session once; the pager links what exists; an unrecognised or
// out-of-range `?page=` lands on page one or the last.
func TestMySessionsIsPagedNewestFirst(t *testing.T) {
	w := transcriptWorld(t)
	a := transcripttest.OpenAt(t, w.clock.Now)
	const n = 120
	for i := 1; i <= n; i++ {
		transcripttest.Append(t, a, w.session(fmt.Sprintf("s-page-%03d", i)), "main", []string{trUser("synthetic")})
		w.clock.Add(time.Hour)
	}
	page := func(q string) string {
		t.Helper()
		rec := getAs(t, w.server(t, "owner", a), MySessionsPath+q)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s%s answered %d", MySessionsPath, q, rec.Code)
		}
		return rec.Body.String()
	}
	var all []string
	for p, size := range map[int]int{1: 50, 2: 50, 3: 20} {
		got := listedSessions(page(fmt.Sprintf("?%s=%d", QueryPage, p)))
		if len(got) != size {
			t.Errorf("page %d lists %d sessions, want %d", p, len(got), size)
		}
		for i, root := range got {
			if want := fmt.Sprintf("s-page-%03d", n-(p-1)*mySessionsPerPage-i); root != want {
				t.Errorf("page %d row %d is %s, want %s — newest upload first", p, i, root, want)
				break
			}
		}
		all = append(all, got...)
	}
	slices.Sort(all)
	if len(slices.Compact(all)) != n {
		t.Errorf("the three pages hold %d distinct sessions, want all %d", len(slices.Compact(all)), n)
	}
	first, mid, last := page(""), page("?page=2"), page("?page=3")
	for name, c := range map[string]struct {
		body       string
		prev, next bool
	}{"page 1": {first, false, true}, "page 2": {mid, true, true}, "page 3": {last, true, false}} {
		if strings.Contains(c.body, `href="/my-sessions?page=`) != (c.prev || c.next) {
			t.Errorf("%s: pager presence is wrong", name)
		}
		if has := strings.Contains(c.body, "← newer"); has != c.prev {
			t.Errorf("%s: a newer-page link is %v, want %v", name, has, c.prev)
		}
		if has := strings.Contains(c.body, "older →"); has != c.next {
			t.Errorf("%s: an older-page link is %v, want %v", name, has, c.next)
		}
	}
	if !strings.Contains(mid, `href="/my-sessions"`) || !strings.Contains(mid, `href="/my-sessions?page=3"`) {
		t.Error("page 2 does not link page 1 (the bare path) and page 3")
	}
	for q, want := range map[string]string{
		"?page=abc": first, "?page=0": first, "?page=-4": first, "?page=1": first,
		"?page=99": last, "?page=99999999999999999999999": first,
	} {
		if got := page(q); !slices.Equal(listedSessions(got), listedSessions(want)) {
			t.Errorf("GET %s%s lists %v…, want the page that holds %v…", MySessionsPath, q,
				head(listedSessions(got)), head(listedSessions(want)))
		}
	}
}

func head(s []string) []string { return s[:min(2, len(s))] }
