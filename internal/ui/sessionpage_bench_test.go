package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/arcs"
	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/store"
)

// BenchmarkSessionPageAndScopeTabs is what makes the session page's cost a RE-DERIVABLE measurement
// (`BenchmarkAudience`'s reason). The session page walks EVERY readable scope's entries per request
// (`touch.Writes` per scope) on top of `Visible`, and the scope page computes both per-scope reports
// on every tab for the tab-label counts; nothing is cached, deliberately.
//
// The store is synthetic and sized like a busy deployment: 30 scopes × 100 entries, each entry 6
// nuance bullets of which 2 carry a trailer, drawn from 40 session ids — so one session writes in
// many scopes. Run it:
//
//	go test ./internal/ui/ -run '^$' -bench BenchmarkSessionPageAndScopeTabs -benchtime 20x
//
// ⚠ ABSOLUTE TIMES ARE HOST- AND LOAD-DEPENDENT; the RATIO between the session page and a scope tab
// is the claim that survives a different machine.
func BenchmarkSessionPageAndScopeTabs(b *testing.B) {
	// Two sizes, so the cost is a slope and not one point: 10×30 and 30×100 entries.
	for _, size := range []struct{ scopes, entries int }{{10, 30}, {30, 100}} {
		b.Run(fmt.Sprintf("%dx%d", size.scopes, size.entries), func(b *testing.B) {
			benchSessionPageAndScopeTabs(b, size.scopes, size.entries)
		})
	}
}

func benchSessionPageAndScopeTabs(b *testing.B, scopes, entries int) {
	const sessions = 40
	root := b.TempDir()
	var events []control.Event
	at := time.Date(2000, 6, 1, 12, 0, 0, 0, time.UTC)
	user := control.DerivedID(control.PrefixUser, "bench-reader")
	project := control.DerivedID(control.PrefixProject, "bench-project")
	events = append(events,
		control.Event{Kind: control.EventUserCreated, At: at, UserID: user, Provider: "fixture-provider",
			Subject: "00000000-0000-4000-8000-000000000061", Email: "bench@notes.example.invalid"},
		control.Event{Kind: control.EventProjectCreated, At: at, ProjectID: project, Name: "bench", UserID: user},
		control.Event{Kind: control.EventMemberSet, At: at, ProjectID: project, UserID: user, Role: control.RoleOwner})
	var regs []arcs.Registration
	for s := 0; s < scopes; s++ {
		name := fmt.Sprintf("bench-scope-%02d", s)
		events = append(events, control.Event{Kind: control.EventScopeCreated, At: at,
			ScopeID: control.DerivedID(control.PrefixScope, name), DisplayName: name, ProjectID: project})
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			b.Fatal(err)
		}
		for e := 0; e < entries; e++ {
			var nuance strings.Builder
			for k := 0; k < 6; k++ {
				fmt.Fprintf(&nuance, "- 2000-01-%02d: bullet %d of entry %d in %s, some synthetic prose", 1+(e+k)%28, k, e, name)
				if k < 2 {
					fmt.Fprintf(&nuance, " [cairn: bench-bot/s-bench-%02d]", (s*7+e*3+k)%sessions)
				}
				nuance.WriteString("\n")
			}
			service := fmt.Sprintf("svc-%03d", e)
			body := "---\nservice: " + service + "\nscope: " + name + "\n---\n\n## What it is\n\nsynthetic.\n\n" +
				store.NuanceHeading + "\n\n" + nuance.String()
			if err := os.WriteFile(filepath.Join(dir, service+".md"), []byte(body), 0o644); err != nil {
				b.Fatal(err)
			}
		}
		regs = append(regs, arcs.Registration{Schema: arcs.Schema, Home: name, Slug: "arc-" + name,
			Status: arcs.StatusOpen, ClosingKind: arcs.ClosingCheck, DeclaredScopes: []string{name},
			Members:      []arcs.Member{{Session: fmt.Sprintf("s-bench-%02d", s%sessions), Role: "wrote"}},
			RegisteredBy: "fixture-registrar", RegisteredAt: "2000-01-06T07:08:09Z"})
	}
	m, err := control.Replay(events)
	if err != nil {
		b.Fatal(err)
	}
	p, _ := m.PrincipalFor(control.KindUser, user)
	id := identity.Identity{Principal: p, Auth: control.Resolve(m, p)}
	journal := filepath.Join(b.TempDir(), "journal.jsonl")
	var lines strings.Builder
	for _, r := range regs {
		line, err := json.Marshal(r)
		if err != nil {
			b.Fatal(err)
		}
		lines.Write(line)
		lines.WriteByte('\n')
	}
	if err := os.WriteFile(journal, []byte(lines.String()), 0o600); err != nil {
		b.Fatal(err)
	}
	cfg := testConfig(b, staticAuth{id})
	cfg.Source = StoreSource{Root: root, ArcJournal: journal}
	cfg.Now = func() time.Time { return sessNow }
	srv, err := New(cfg)
	if err != nil {
		b.Fatal(err)
	}
	scope := control.DerivedID(control.PrefixScope, "bench-scope-07")
	for _, tc := range []struct{ name, path string }{
		{"session-page", sessionURL("s-bench-05")},
		// The arcs page (S1 of the arcs/presence plan): ONE whole-store member walk, the session page's
		// shape, not one walk per arc — so its claim is "≤ the session page", stated as a ratio.
		{"arcs-page", ArcsPath},
		{"scope-tab-entries", ScopePath + "?" + QueryID + "=" + string(scope)},
		{"scope-tab-sessions", ScopePath + "?" + QueryID + "=" + string(scope) + "&" + QueryTab + "=" + TabSessions},
		{"scope-tab-arcs", ScopePath + "?" + QueryID + "=" + string(scope) + "&" + QueryTab + "=" + TabArcs},
		// The baseline every browse page already pays: the entry page reads the whole narrowed store.
		{"entry-page", entryHref(scope, "svc-001", false)},
		// The hub pays `Visible` + the arcs walk + the sessions walk for its three counts; the sessions
		// list pays `Visible` + the sessions walk; the scope list is the old root.
		{"hub", RootPath},
		{"sessions-list", SessionsPath},
		{"scopes-list", ScopesPath},
	} {
		b.Run(tc.name, func(b *testing.B) {
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if rec.Code != http.StatusOK {
				b.Fatalf("%s answered %d: %.300s", tc.path, rec.Code, rec.Body.String())
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				rec := httptest.NewRecorder()
				srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
			}
		})
	}
}
