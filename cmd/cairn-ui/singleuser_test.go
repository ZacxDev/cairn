package main

import (
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/transcript"
	"github.com/ZacxDev/cairn/internal/transcript/transcripttest"
)

// TestTheReadSideCarriesTheSingleUserFlag: the Store `main` builds for both listeners decides under
// `-transcript-single-user` — the owner's narrowed credential reads an owner-only (`V = {*}`) session
// with the flag and is refused it without, and another principal's narrowed credential is refused
// either way. RED if `transcriptReadSide` drops the flag on the floor.
func TestTheReadSideCarriesTheSingleUserFlag(t *testing.T) {
	a := transcripttest.Open(t)
	owner := control.Principal{Kind: control.KindUser, ID: "usr_fixture_owner", Display: "fixture owner"}
	other := control.Principal{Kind: control.KindUser, ID: "usr_fixture_other", Display: "fixture owner"}
	names := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_su0002",` +
		`"name":"Bash","input":{"command":"cairn recall"}}]}}`
	transcripttest.Append(t, a, transcripttest.Session{Root: "s-star-01", Owner: string(owner.Kind) + ":" + string(owner.ID)},
		"main", []string{names})
	readOnly := control.NewVerbSet(control.VerbRead)
	narrowed := control.Narrow(control.Authorization{}, nil, &readOnly)
	if !narrowed.Narrowed() {
		t.Fatal("INSTRUMENT CONTROL: the agent credential is not narrowed")
	}
	if meta, err := a.ReadMeta("s-star-01"); err != nil || meta == nil || !strings.Contains(strings.Join(meta.LedgerScopes(), ","), transcript.Star) {
		t.Fatalf("INSTRUMENT CONTROL: s-star-01 does not carry `*` in its read half (%v)", err)
	}
	storeRoot := t.TempDir()
	for _, tc := range []struct {
		singleUser bool
		who        control.Principal
		want       bool
		clause     string
	}{
		{true, owner, true, ", transcript reads SINGLE-USER (-transcript-single-user: the owner's narrowed credentials read every owned session)"},
		{false, owner, false, ""},
		{true, other, false, ", transcript reads SINGLE-USER (-transcript-single-user: the owner's narrowed credentials read every owned session)"},
	} {
		s, clause := transcriptReadSide(a, storeRoot, tc.singleUser)
		if clause != tc.clause {
			t.Errorf("singleUser=%v: clause %q, want %q", tc.singleUser, clause, tc.clause)
		}
		view, err := s.View(transcript.Viewer{Principal: tc.who, Auth: narrowed}, "s-star-01")
		if err != nil {
			t.Fatal(err)
		}
		if got := view != nil; got != tc.want {
			t.Errorf("singleUser=%v: %s's narrowed credential reads s-star-01 = %v, want %v", tc.singleUser, tc.who.ID, got, tc.want)
		}
	}
}

// TestTheBinaryAnnouncesSingleUserReadsOnlyWhenSet drives `main`: the startup line names the
// single-user read policy with `-transcript-single-user`, and says nothing of it without.
func TestTheBinaryAnnouncesSingleUserReadsOnlyWhenSet(t *testing.T) {
	_, journal := seededJournal(t, credentialLive)
	tokens := workerTokenFile(t)
	const clause = "transcript reads SINGLE-USER (-transcript-single-user"

	off := startPresenceChild(t, journal, workerArgs(t, tokens)...)
	off.serving(t)
	off.workerAddr(t)
	if strings.Contains(off.out.String(), clause) {
		t.Fatalf("a child without -transcript-single-user announces single-user reads:\n%s", off.out.String())
	}
	on := startPresenceChild(t, journal, workerArgs(t, tokens, "-transcript-single-user")...)
	on.serving(t)
	on.workerAddr(t)
	if !strings.Contains(on.out.String(), clause) {
		t.Fatalf("a child with -transcript-single-user does not announce it:\n%s", on.out.String())
	}
}
