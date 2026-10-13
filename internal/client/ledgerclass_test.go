package client

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/codesrc"
)

// TestEveryVerbRecordsWhatItsLedgerClassSays (#266's audit, round 0): EVERY verb in the dispatch
// table — the table `cairn -verbs` prints — has a [LedgerClass], and a served call of it records
// exactly what that class says. Table-driven over `Verbs()`, so a verb with no probe below fails
// here: S8's `transcript` verbs cannot land unclassed.
//
// Each probe is one served call on a fixture where it succeeds (or, for `audit-refs`, reaches its
// record): `scope` is the scope the call resolves. `resolved` must record exactly that scope;
// `listed` exactly `listed`; `all` exactly one `*`; `none` nothing.
func TestEveryVerbRecordsWhatItsLedgerClassSays(t *testing.T) {
	type probe struct {
		host   string // "offline", "arc", "sources", "memo"
		argv   []string
		scope  string
		listed []string
	}
	entry := func(home, scope, ref string) string {
		p := filepath.Join(home, ref+".md")
		if err := os.WriteFile(p, []byte("---\nservice: "+ref+"\nscope: "+scope+"\n---\n\n## What it is\nsynthetic.\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	probes := map[string]func(home string) probe{
		"recall": func(string) probe {
			return probe{"offline", []string{"recall", "--no-sync", "--scope", "beta-notes"}, "beta-notes", nil}
		},
		"search": func(string) probe {
			return probe{"offline", []string{"search", "synthetic", "--no-sync", "--scope", "beta-notes"}, "beta-notes", nil}
		},
		"sessions": func(string) probe {
			return probe{"offline", []string{"sessions", "--no-sync", "--scope", "beta-notes"}, "beta-notes", nil}
		},
		"validate": func(string) probe {
			return probe{"offline", []string{"validate", "--no-sync", "--scope", "beta-notes"}, "beta-notes", nil}
		},
		"ls-entries": func(string) probe {
			return probe{"offline", []string{"ls-entries", "--no-sync"}, "", []string{"alpha-notes", "beta-notes"}}
		},
		"doctor": func(string) probe { return probe{"offline", []string{"doctor", "--no-sync"}, "", nil} },
		"routes": func(string) probe { return probe{"offline", []string{"routes"}, "", nil} },
		"sync":   func(string) probe { return probe{"offline", []string{"sync"}, "", nil} },
		"append": func(string) probe {
			return probe{"arc", []string{"append", "--scope", "alpha-notes", "--ref", "gadget-one", "--text", "x", "--session", "s-1"}, "alpha-notes", nil}
		},
		"create": func(home string) probe {
			return probe{"arc", []string{"create", "--scope", "alpha-notes", "--ref", "gadget-two", "--file", entry(home, "alpha-notes", "gadget-two")}, "alpha-notes", nil}
		},
		"put": func(home string) probe {
			return probe{"arc", []string{"put", "--scope", "alpha-notes", "--ref", "gadget-one", "--file", entry(home, "alpha-notes", "gadget-one")}, "alpha-notes", nil}
		},
		"arc-register": func(home string) probe {
			p := filepath.Join(home, "arc.json")
			if err := os.WriteFile(p, []byte(`{"schema":1,"status":"open","members":[]}`), 0o644); err != nil {
				t.Fatal(err)
			}
			return probe{"arc", []string{"arc-register", "--scope", "alpha-notes", "--slug", "gadget-rollout", "--from", p}, "alpha-notes", nil}
		},
		"arcs": func(string) probe { return probe{"arc", []string{"arcs", "--scope", "alpha-notes"}, "", nil} },
		"arc-show": func(string) probe {
			return probe{"arc-registered", []string{"arc-show", "--scope", "alpha-notes", "--slug", "gadget-rollout"}, "", nil}
		},
		"sources": func(string) probe {
			return probe{"sources", []string{"sources", "--scope", "alpha-notes"}, "alpha-notes", nil}
		},
		"audit-refs": func(string) probe {
			return probe{"sources", []string{"audit-refs", "--scope", "beta-notes"}, "beta-notes", nil}
		},
		"memo-check": func(string) probe {
			return probe{"memo", []string{"memo-check", "--scope", "alpha-notes", "--session", "s-0001"}, "alpha-notes", nil}
		},
		"memo-read": func(string) probe {
			return probe{"memo", []string{"memo-read", "--scope", "alpha-notes"}, "alpha-notes", nil}
		},
		"memo-send": func(string) probe {
			return probe{"memo", []string{"memo-send", "--scope", "alpha-notes", "--subject", "s", "--body", "b"}, "alpha-notes", nil}
		},
		"memo-retract": func(string) probe {
			return probe{"memo", []string{"memo-retract", "--scope", "alpha-notes", "--id", "7"}, "alpha-notes", nil}
		},
	}
	// `arcs --check` is a MODE of `arcs`, not a verb: probed beside it, under the same class.
	extra := []struct {
		verb string
		p    probe
	}{{"arcs", probe{"arc", []string{"arcs", "--check", "--scope", "alpha-notes"}, "", nil}}}

	const session = "ses-ledger-class"
	run := func(t *testing.T, verb string, class LedgerClass, p probe, home string) {
		state := filepath.Join(home, "ledger-state")
		t.Setenv(StateHomeEnv, state)
		t.Setenv(ClaudeSessionEnv, session)
		t.Setenv(OpencodeSessionEnv, "")
		code, out, stderr := runCLI(t, p.argv...)
		var got []string
		for _, r := range readLedgerFile(t, ledgerPath(state, session)) {
			if r.Verb != verb {
				t.Fatalf("%v: a record names verb %q", p.argv, r.Verb)
			}
			got = append(got, r.Scope)
		}
		var want []string
		switch class {
		case LedgerResolved:
			want = []string{p.scope}
		case LedgerListed:
			want = p.listed
		case LedgerAll:
			want = []string{LedgerAllScopes}
		case LedgerNone:
			want = nil
		default:
			t.Fatalf("verb %s has no ledger class (%q)", verb, class)
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s (class %s) %v: recorded %v, want %v (exit %d)\n%s\n%s", verb, class, p.argv, got, want, code, out, stderr)
		}
	}
	host := func(t *testing.T, kind string) string {
		switch kind {
		case "offline":
			home, _ := ledgerHost(t)
			return home
		case "arc", "arc-registered":
			tsrv := arcPod(t, filepath.Join(t.TempDir(), "journal.jsonl"))
			home := arcClientHost(t, tsrv.URL)
			if kind == "arc-registered" {
				t.Setenv(ClaudeSessionEnv, "")
				registerGadget(t, home)
			}
			return home
		case "sources":
			journal := filepath.Join(t.TempDir(), "sources.jsonl")
			if _, err := (codesrc.Journal{Path: journal}).Set("alpha-notes", []string{srcFirst}, codesrc.RevisionNone,
				"scope-admin", time.Date(2000, 1, 3, 0, 0, 0, 0, time.UTC), nil); err != nil {
				t.Fatal(err)
			}
			withAuditor(t)
			tsrv := sourcesPod(t, journal)
			home := arcClientHost(t, tsrv.URL)
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			return home
		case "memo":
			stub := newMemoStub(t, stubToken, &stubMemo{id: 7, scope: "alpha-notes", created: memoT0, subject: "seven"})
			h := newMemoHost(t, "alpha-notes")
			h.configure(t, "", uiLines(stub.srv.URL)...)
			return h.home
		}
		t.Fatalf("no host %q", kind)
		return ""
	}
	for _, v := range Verbs() {
		mk, ok := probes[v.Name]
		if !ok {
			t.Errorf("verb %s (class %q) has no probe here — classify it and add one", v.Name, v.Ledger)
			continue
		}
		t.Run(v.Name, func(t *testing.T) {
			home := t.TempDir()
			p := mk(home)
			run(t, v.Name, v.Ledger, p, host(t, p.host))
		})
	}
	for _, x := range extra {
		t.Run(x.verb+" "+x.p.argv[1], func(t *testing.T) {
			var class LedgerClass
			for _, v := range Verbs() {
				if v.Name == x.verb {
					class = v.Ledger
				}
			}
			run(t, x.verb, class, x.p, host(t, x.p.host))
		})
	}
	if len(probes) != len(Verbs()) {
		t.Errorf("%d probes for %d verbs — a probe names a verb the table does not have", len(probes), len(Verbs()))
	}
}
