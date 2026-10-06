package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 🔴 THE SESSIONS ROUTE IS AUTHORISED BY THE SAME `rq.visible` RECALL IS — measured here as a
// relationship over the served answer, not by reading the handler. A refused scope must answer,
// header for header and byte for byte up to the scope's own name, what a never-existed scope
// answers; and the positive control is the SAME request with a credential that may read it,
// which must list the session the fixture wrote. Without that control, a handler that refused
// every scope would pass the pair. All names and ids are synthetic.
func TestTheSessionsRouteRefusesExactlyLikeAnAbsentScope(t *testing.T) {
	h := newHarness(t)
	trailered := "---\nservice: gizmo-two\nscope: alpha-notes\n---\n\n## What it is\nsynthetic.\n\n" +
		"## Nuance / work-history\n- 2000-01-03: signed [cairn: kappa-bot/s-0042]\n"
	if err := os.WriteFile(filepath.Join(h.root, "alpha-notes", "gizmo-two.md"),
		[]byte(trailered), 0o644); err != nil {
		t.Fatal(err)
	}

	allowed := h.do(t, "GET", "/api/v1/sessions/alpha-notes", wideToken, nil, "")
	if allowed.status != 200 || allowed.headers.Get("X-Store-Status") != "sessions-listed" ||
		!strings.Contains(allowed.body, "\n- s-0042 · actor kappa-bot · 1 bullet · 2000-01-03\n") {
		t.Fatalf("the positive control: a reader of alpha-notes must see the session, got %d %s\n%s",
			allowed.status, allowed.headers.Get("X-Store-Status"), allowed.body)
	}

	refused := h.do(t, "GET", "/api/v1/sessions/alpha-notes", narrowToken, nil, "")
	absent := h.do(t, "GET", "/api/v1/sessions/ghost-void", narrowToken, nil, "")
	if refused.status != 200 || refused.headers.Get("X-Store-Status") != "scope-absent" {
		t.Fatalf("a refused scope must answer scope-absent at 200, got %d %s",
			refused.status, refused.headers.Get("X-Store-Status"))
	}
	if strings.Contains(refused.body, "s-0042") || strings.Contains(refused.body, "kappa-bot") {
		t.Fatalf("a refused scope leaked its sessions:\n%s", refused.body)
	}
	got := strings.ReplaceAll(refused.body, "alpha-notes", "<SCOPE>")
	want := strings.ReplaceAll(absent.body, "ghost-void", "<SCOPE>")
	if got != want {
		t.Fatalf("refused is distinguishable from absent:\n%s\n---\n%s", got, want)
	}
	for _, name := range []string{"X-Store-Status", "X-Store-Exit", "X-Store-Revision",
		"X-Store-Snapshot", "Content-Type"} {
		if refused.headers.Get(name) != absent.headers.Get(name) {
			t.Fatalf("%s differs: refused %q, absent %q", name,
				refused.headers.Get(name), absent.headers.Get(name))
		}
	}
	// `alpha-notes` carries a `.git/HEAD` in this fixture, so `X-Store-Revision` is the one header
	// that COULD tell the two apart — and the allowed read proves it would, if ungated.
	if allowed.headers.Get("X-Store-Revision") == refused.headers.Get("X-Store-Revision") {
		t.Fatal("the revision control failed: the allowed read must expose the real revision")
	}
}
