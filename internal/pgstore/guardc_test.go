package pgstore_test

// Guard C of decision 19 (`claudedocs/plan-cairn-scope-mail.md`), the SERVER side, at the level
// of this package's exported surface.
//
// 🔴 THE PROPERTY: no exported RESULT or exported FIELD in `internal/pgstore` carries memo text
// in any type other than `memo.Stored`. PARAMETERS are not constrained (round 5's scope
// correction) — `MemoStore.Send` takes the subject and body to store.
//
// It is TWO checks, because they catch different mutants:
//   - a LEDGER of the WHOLE package's exported surface — every name WITH its printed signature —
//     failing on GROW, SHRINK or CHANGE, so a new exported type carrying a memo column, in ANY
//     file, is a reviewed edit here rather than an accident; and
//   - a STRUCTURAL rule over `MemoStore`'s methods: every result is one of a closed set of types
//     that cannot hold text (ids, times, errors) or `memo.Stored` itself — so a method returning
//     a raw row or a `[]string` of bodies is red even if somebody also edits the ledger.
//
// ⚠ IT READS SOURCE (`internal/surface`), so its scope is what the source SAYS: a type alias or
// a named type hiding a string is a name it shows, not an underlying type it resolves.

import (
	"os"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/surface"
)

func pgstoreSurface(t *testing.T, override, extra map[string]string) map[string]string {
	t.Helper()
	got, err := surface.Exported(".", "pgstore", override, extra)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 30 {
		t.Fatalf("read only %d exported names — the ledger would be compared against nothing", len(got))
	}
	return got
}

func TestGuardCThePackageSurfaceIsTheLedger(t *testing.T) {
	grew, shrank, changed := surface.Diff(pgstoreSurface(t, nil, nil), pgstoreLedger)
	if len(grew)+len(shrank)+len(changed) > 0 {
		t.Fatalf("internal/pgstore's exported surface moved (decision 19, Guard C).\n"+
			"  new names (a new exported RESULT or FIELD here must not carry memo text in any type but "+
			"memo.Stored — review, then add the row):\n    %s\n"+
			"  names gone: %v\n  names whose signature changed:\n    %s",
			strings.Join(grew, "\n    "), shrank, strings.Join(changed, "\n    "))
	}
}

// pgstoreLedger is EVERY exported name in `internal/pgstore`, with its printed signature. The
// MEMO rows are the ones Guard C is about; the rest are listed so that a new exported type
// carrying a memo column cannot arrive under a non-memo name unseen.
var pgstoreLedger = map[string]string{
	// ── memos (S1): text leaves ONLY as memo.Stored ──
	"MemoStore":                  "struct",
	"NewMemoStore":               "func(*DB, *secretscan.Redactor) (*MemoStore, error)",
	"MemoStore.Send":             "(*MemoStore) func(context.Context, control.Principal, control.Authorization, string, string, string, time.Duration) (int64, time.Time, error)",
	"MemoStore.List":             "(*MemoStore) func(context.Context, control.Model, control.Authorization, MemoQuery) ([]memo.Stored, error)",
	"MemoStore.All":              "(*MemoStore) func(context.Context, control.Model, control.Authorization, string) ([]memo.Stored, error)",
	"MaxUnexpiredPerScope":       "const",
	"MemoStore.Get":              "(*MemoStore) func(context.Context, control.Model, control.Authorization, string, int64) (memo.Stored, error)",
	"MemoStore.Retract":          "(*MemoStore) func(context.Context, control.Principal, control.Authorization, string, int64) error",
	"MemoQuery":                  "struct",
	"MemoQuery.Scope":            "string", // the scope ASKED FOR — a parameter's field, never a stored column read back
	"MemoQuery.After":            "time.Time",
	"MemoQuery.Limit":            "int",
	"MemoQuery.IncludeRetracted": "bool",
	// ── the pool and the schema ──
	"DB":                   "struct",
	"DB.Now":               "func() time.Time",
	"DB.SQL":               "(*DB) func() *sql.DB",
	"DB.Close":             "(*DB) func() error",
	"DB.Migrate":           "(*DB) func(context.Context) error",
	"DriverName":           "const",
	"ErrNoDSN":             `var = errors.New("pgstore: no connection string was configured, so no session or invite could ever be resolved")`,
	"Open":                 "func(context.Context, string) (*DB, error)",
	"Migration":            "struct",
	"Migration.Version":    "int",
	"Migration.Name":       "string",
	"Migration.Statements": "[]string",
	"StatementTimeout":     "const",
	// ── sessions ──
	"SessionStore":        "struct",
	"NewSessionStore":     "func(*DB) *SessionStore",
	"SessionStore.Lookup": "(*SessionStore) func(string) (identity.Session, bool, error)",
	"SessionStore.Create": "(*SessionStore) func(identity.Session) error",
	"SessionStore.Revoke": "(*SessionStore) func(string) error",
	"SessionStore.Prune":  "(*SessionStore) func() error",
	"SessionStore.Count":  "(*SessionStore) func() (int, error)",
	// ── invitations ──
	"InviteStore":                "struct",
	"NewInviteStore":             "func(*DB) *InviteStore",
	"InviteStore.Create":         "(*InviteStore) func(invite.Invite) error",
	"InviteStore.ByToken":        "(*InviteStore) func(string) (invite.Invite, bool, error)",
	"InviteStore.Redeem":         "(*InviteStore) func(string, control.ID, time.Time) (invite.Invite, error)",
	"InviteStore.Revoke":         "(*InviteStore) func(string, time.Time) error",
	"InviteStore.RevokeByDigest": "(*InviteStore) func(string, time.Time) error",
	"InviteStore.ForProject":     "(*InviteStore) func(control.ID) ([]invite.Invite, error)",
	// ── team links ──
	"TeamLinkStore":                   "struct",
	"NewTeamLinkStore":                "func(*DB) *TeamLinkStore",
	"TeamLinkStore.CreateLink":        "(*TeamLinkStore) func(invite.TeamLink) error",
	"TeamLinkStore.LinkByToken":       "(*TeamLinkStore) func(string) (invite.TeamLink, bool, error)",
	"TeamLinkStore.LinkByDigest":      "(*TeamLinkStore) func(string) (invite.TeamLink, bool, error)",
	"TeamLinkStore.RedeemLink":        "(*TeamLinkStore) func(string, control.ID, bool, time.Time) (invite.TeamLink, error)",
	"TeamLinkStore.ConfirmRedemption": "(*TeamLinkStore) func(string, int) error",
	"TeamLinkStore.RevokeLink":        "(*TeamLinkStore) func(string, time.Time) error",
	"TeamLinkStore.LinksBy":           "(*TeamLinkStore) func(control.ID) ([]invite.TeamLink, error)",
	"TeamLinkStore.LinkRedemptions":   "(*TeamLinkStore) func(string) ([]invite.LinkRedemption, error)",
}

// memoResultTypes is the CLOSED set of result types a `MemoStore` method may have: ids, times,
// errors — none can hold text — and `memo.Stored`, the one carrier of stored text.
var memoResultTypes = map[string]bool{
	"int64": true, "time.Time": true, "error": true, "memo.Stored": true, "[]memo.Stored": true,
}

// memoResults parses the result list out of a printed method signature.
func memoResults(sig string) []string {
	i := strings.Index(sig, ") func(")
	if i < 0 {
		return nil
	}
	rest := sig[i+len(") func("):]
	depth, j := 1, 0
	for ; j < len(rest) && depth > 0; j++ {
		switch rest[j] {
		case '(':
			depth++
		case ')':
			depth--
		}
	}
	res := strings.TrimSpace(rest[j:])
	res = strings.TrimSuffix(strings.TrimPrefix(res, "("), ")")
	if res == "" {
		return nil
	}
	var out []string
	for _, r := range strings.Split(res, ",") {
		out = append(out, strings.TrimSpace(r))
	}
	return out
}

// memoResultViolations lists every MemoStore method result outside `memoResultTypes`.
func memoResultViolations(s map[string]string) []string {
	var bad []string
	for name, sig := range s {
		if !strings.HasPrefix(name, "MemoStore.") {
			continue
		}
		for _, r := range memoResults(sig) {
			if !memoResultTypes[r] {
				bad = append(bad, name+" returns "+r)
			}
		}
	}
	return bad
}

// TestGuardCMemoStoreReturnsNoTextButStored is the STRUCTURAL half. POSITIVE CONTROL: the parser
// finds the results of `Send` (three) and `List`, so a zero below is not a parser that sees no
// results.
func TestGuardCMemoStoreReturnsNoTextButStored(t *testing.T) {
	s := pgstoreSurface(t, nil, nil)
	if got := memoResults(s["MemoStore.Send"]); len(got) != 3 {
		t.Fatalf("POSITIVE CONTROL FAILED: parsed %v as Send's results", got)
	}
	if got := memoResults(s["MemoStore.List"]); len(got) != 2 || got[0] != "[]memo.Stored" {
		t.Fatalf("POSITIVE CONTROL FAILED: parsed %v as List's results", got)
	}
	if bad := memoResultViolations(s); len(bad) > 0 {
		t.Fatalf("a MemoStore method returns a type that can carry memo text outside memo.Stored: %v", bad)
	}
}

// guardCMutants are decision 19's required server-side mutants, each as the edit a maintainer
// would make. Each must move the ledger by the name it introduces or changes; the ones that
// change a MemoStore result must ALSO fail the structural rule.
var guardCMutants = []struct {
	name, file, old, new, src string
	moves                     string
	structural                bool
}{
	{name: "memostore-returns-raw-row-type",
		src:   "package pgstore\ntype MemoRow struct{ Subject, Body string }\nfunc (s *MemoStore) Rows() ([]MemoRow, error) { return nil, nil }\n",
		moves: "MemoStore.Rows", structural: true},
	{name: "memostore-returns-string-slice-of-bodies",
		src:   "package pgstore\nfunc (s *MemoStore) Bodies() ([]string, error) { return nil, nil }\n",
		moves: "MemoStore.Bodies", structural: true},
	{name: "get-returns-the-body-string", file: "memos.go",
		old:   "scope string, id int64) (memo.Stored, error) {",
		new:   "scope string, id int64) (string, error) {",
		moves: "MemoStore.Get", structural: true},
	{name: "exported-struct-with-a-memo-column-in-another-file",
		src:   "package pgstore\ntype Row struct{ Body string }\n",
		moves: "Row.Body"},
}

func TestGuardCCatchesEveryRequiredMutant(t *testing.T) {
	if g, s, c := surface.Diff(pgstoreSurface(t, nil, nil), pgstoreLedger); len(g)+len(s)+len(c) != 0 {
		t.Fatalf("control: the unmutated ledger is not green: %v %v %v", g, s, c)
	}
	for _, m := range guardCMutants {
		var override, extra map[string]string
		if m.src != "" {
			extra = map[string]string{"mutant_" + m.name + ".go": m.src}
		} else {
			raw, err := os.ReadFile(m.file)
			if err != nil {
				t.Fatal(err)
			}
			if n := strings.Count(string(raw), m.old); n != 1 {
				t.Fatalf("mutant %s: pattern occurs %d times in %s, want 1 — re-derive it", m.name, n, m.file)
			}
			override = map[string]string{m.file: strings.Replace(string(raw), m.old, m.new, 1)}
		}
		s := pgstoreSurface(t, override, extra)
		grew, _, changed := surface.Diff(s, pgstoreLedger)
		if !surface.Moved(grew, changed, m.moves) {
			t.Errorf("mutant %s SURVIVED the ledger: it did not move by %s (grew %v, changed %v)", m.name, m.moves, grew, changed)
		}
		if m.structural && len(memoResultViolations(s)) == 0 {
			t.Errorf("mutant %s SURVIVED the structural rule", m.name)
		}
	}
}
