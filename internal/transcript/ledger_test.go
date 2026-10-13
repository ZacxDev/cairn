package transcript

import (
	"slices"
	"testing"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/transcript/archive"
	"github.com/ZacxDev/cairn/internal/transcript/transcripttest"
)

// THE CLIENT READ LEDGER IN `V` (S11 of `claudedocs/plan-cairn-plugins.md`, decision 3a; the pod
// fold of closing-condition clause n). Over `visBuild`'s store and viewers.

func ledgerLine(scope string) string {
	return `{"schema":1,"session":"s-ldg","verb":"sessions","instance":"personal","scope":"` + scope +
		`","client":"go","client_version":"(devel)","at":"2000-01-02T03:04:05Z"}`
}

// TestAnUploadedLedgerIsInV: an uploaded `ledger` stream naming only `beta-notes`, for a session
// whose transcript carries no header and no trailer and whose upload DECLARES nothing, puts
// `beta-notes` in `meta.json` and in `V` — so a reader of `beta-notes` sees it and a reader of only
// `alpha-notes` does not (mutant `transcript-pod-ignores-ledger`: `V` is then EMPTY, which is
// owner-only, and the beta reader is refused too).
//
// RED before S11: the pod refused the `ledger` stream and `V` had no ledger half.
func TestAnUploadedLedgerIsInV(t *testing.T) {
	w := visBuild(t)
	owner := string(control.KindUser) + ":" + string(visID(control.PrefixUser, "owner"))
	s := transcripttest.Session{Root: "s-ldg-01", Owner: owner}
	transcripttest.Append(t, w.arc, s, "main", []string{`{"type":"user","message":{"role":"user","content":"note the ladle"}}`})
	transcripttest.Append(t, w.arc, s, archive.LedgerStream, []string{ledgerLine("beta-notes")})
	meta, err := w.arc.ReadMeta("s-ldg-01")
	if err != nil || meta == nil {
		t.Fatalf("not stored: %v", err)
	}
	written, err := WrittenScopes(w.store, []string{meta.Root})
	if err != nil {
		t.Fatal(err)
	}
	if v := VisibilitySet(meta, written); !slices.Equal(meta.Ledger, []string{"beta-notes"}) ||
		!slices.Equal(v, []string{"beta-notes"}) || len(meta.Declared) != 0 || len(written.Scopes) != 0 {
		t.Fatalf("ledger=%v V=%v declared=%v written=%v — want beta-notes from the LEDGER alone", meta.Ledger, v, meta.Declared, written.Scopes)
	}
	for viewer, want := range map[string]bool{"wrote": true, "every": true, "owner": true, "one": false, "none": false} {
		if got := w.sees(t, w.viewers[viewer], "s-ldg-01"); got != want {
			t.Errorf("%s sees the ledgered session: %v, want %v", viewer, got, want)
		}
	}
}

// TestALedgerArrivingLaterLiftsF1: a tool input that names the program, uploaded BEFORE the ledger,
// makes `V` hold `*` (F1: the program ran and nothing says what it read) — and the ledger arriving
// on the next request replaces that `*` with what it names. F1 asks "is the ledger EMPTY" over the
// whole session, so it cannot be decided per request.
//
// RED on S4's per-request derivation, which folded F1's `*` into the grow-only derived set: a
// session that named `cairn` stayed owner-only whatever its ledger later said.
func TestALedgerArrivingLaterLiftsF1(t *testing.T) {
	w := visBuild(t)
	owner := string(control.KindUser) + ":" + string(visID(control.PrefixUser, "owner"))
	s := transcripttest.Session{Root: "s-ldg-02", Owner: owner}
	names := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_ldg0001",` +
		`"name":"Bash","input":{"command":"cairn sessions --scope alpha-notes"}}]}}`
	transcripttest.Append(t, w.arc, s, "main", []string{names})
	if w.sees(t, w.viewers["one"], "s-ldg-02") || w.sees(t, w.viewers["every"], "s-ldg-02") {
		t.Fatal("before the ledger: F1's `*` must make the session owner-only")
	}
	transcripttest.Append(t, w.arc, s, archive.LedgerStream, []string{ledgerLine("alpha-notes")})
	meta, _ := w.arc.ReadMeta("s-ldg-02")
	if v := VisibilitySet(meta, Written{}); !slices.Equal(v, []string{"alpha-notes"}) {
		t.Fatalf("after the ledger: V=%v, want [alpha-notes]", v)
	}
	if !w.sees(t, w.viewers["one"], "s-ldg-02") || w.sees(t, w.viewers["none"], "s-ldg-02") {
		t.Fatal("after the ledger: the alpha reader must see it and the stranger must not")
	}
}
