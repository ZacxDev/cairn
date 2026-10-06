package arcs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Every name, session id and time in this file is synthetic; times are in the year 2000.

var clock = time.Date(2000, 1, 6, 7, 8, 9, 0, time.UTC)

func decode(t *testing.T, body string) (Registration, int) {
	t.Helper()
	reg, unjoinable, err := DecodePayload([]byte(body), "alpha-notes", "gadget-rollout")
	if err != nil {
		t.Fatalf("payload must decode: %v\n%s", err, body)
	}
	return reg, unjoinable
}

func refused(t *testing.T, body, want string) {
	t.Helper()
	_, _, err := DecodePayload([]byte(body), "alpha-notes", "gadget-rollout")
	var bad *PayloadError
	if !errors.As(err, &bad) {
		t.Fatalf("payload must be REFUSED as a *PayloadError, got %v\n%s", err, body)
	}
	if !strings.Contains(bad.Message, want) {
		t.Fatalf("the refusal must say %q, got %q", want, bad.Message)
	}
}

// TestAnAbsentStatusIsUnknownNeverOpen is Q4 at the payload: no status means `unknown`. The
// control is an explicit `open`, which must survive as `open` — otherwise a decoder that mapped
// everything to `unknown` would pass.
func TestAnAbsentStatusIsUnknownNeverOpen(t *testing.T) {
	reg, _ := decode(t, `{"schema":1}`)
	if reg.Status != StatusUnknown || reg.ClosingKind != ClosingNone {
		t.Fatalf("absent status/closing_kind must be unknown/none, got %q/%q", reg.Status, reg.ClosingKind)
	}
	reg, _ = decode(t, `{"schema":1,"status":"open","closing_kind":"check"}`)
	if reg.Status != StatusOpen || reg.ClosingKind != ClosingCheck {
		t.Fatalf("control: an explicit open/check survives, got %q/%q", reg.Status, reg.ClosingKind)
	}
}

func TestTheHomeScopeIsAlwaysDeclaredAndScopesAreFoldedSortedDeduplicated(t *testing.T) {
	reg, _ := decode(t, `{"schema":1,"declared_scopes":["Zeta_Notes","beta-notes","beta-notes"]}`)
	if got := strings.Join(reg.DeclaredScopes, ","); got != "alpha-notes,beta-notes,zeta-notes" {
		t.Fatalf("declared scopes = %s", got)
	}
}

// TestAMemberThatCanNeverJoinATrailerIsCountedNotStored: a session id outside
// `write.SessionComponent` can never equal a trailer's session, so it is dropped AND counted —
// the tooling is told rather than losing it silently. The control is the joinable member beside
// it, which must be kept.
func TestAMemberThatCanNeverJoinATrailerIsCountedNotStored(t *testing.T) {
	reg, unjoinable := decode(t, `{"schema":1,"members":[
		{"session":"s-0001","role":"originated","first_seen":"2000-01-01T00:00:00Z"},
		{"session":"has a space","role":"wrote","first_seen":""}]}`)
	if unjoinable != 1 || len(reg.Members) != 1 || reg.Members[0].Session != "s-0001" {
		t.Fatalf("unjoinable=%d members=%+v", unjoinable, reg.Members)
	}
}

func TestThePayloadValidatorRefusesWhatItCannotStoreHonestly(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{`{"schema":2}`, "schema must be 1"},
		{`{}`, "schema must be 1"},
		{`{"schema":1,"status":"maybe"}`, "status must be open, closed or unknown"},
		{`{"schema":1,"closing_kind":"vibes"}`, "closing_kind must be"},
		// 🔴 UNKNOWN FIELDS ARE REFUSED — the plan's "never stored" list enforced by construction.
		{`{"schema":1,"doc_body":"prose"}`, "unknown field"},
		{`{"schema":1,"members":[{"session":"s-1","role":"wrote","carried":true}]}`, "unknown field"},
		{`{"schema":1,"declared_scopes":[".arcs"]}`, "declared_scopes[0] must match"},
		{`{"schema":1,"members":[{"session":"s-1","role":"lurker"}]}`, "members[0].role must be one of"},
		{`{"schema":1,"members":[{"session":"s-1","role":"wrote"},{"session":"s-1","role":"resumed"}]}`, "appears twice"},
		{`{"schema":1,"commits_total":1,"commits_unstamped":2}`, "commits_unstamped <= commits_total"},
		{`{"schema":1,"reported_at":"yesterday"}`, "reported_at must be RFC 3339"},
		{`{"schema":1} {"schema":1}`, "trailing data"},
	} {
		refused(t, tc.body, tc.want)
	}
}

func readers(reg Registration) (out []string) {
	for _, m := range reg.Members {
		tag := ""
		if m.Carried {
			tag = "+carried"
		}
		out = append(out, m.Session+"/"+m.Role+tag)
	}
	return out
}

// TestAnUnmeasuredLegNeverErasesAMeasuredOne is MERGE RULE 7, as a measured-then-unmeasured push
// sequence. Push 1 measures both legs. Push 2 measures WRITERS only and names a new writer: the
// old writer is REPLACED (the writer leg was measured) and the reader is CARRIED (the reader leg
// was not). Push 3 measures readers and names none: the carried reader is now dropped — a
// measured leg replaces outright.
func TestAnUnmeasuredLegNeverErasesAMeasuredOne(t *testing.T) {
	j := Journal{Path: filepath.Join(t.TempDir(), "journal.jsonl")}
	push := func(body string) Registration {
		t.Helper()
		reg, _ := decode(t, body)
		out, err := j.Register(reg, clock)
		if err != nil {
			t.Fatal(err)
		}
		return out.Record
	}
	push(`{"schema":1,"writers_measured":true,"readers_measured":true,"members":[
		{"session":"s-0001","role":"originated"},{"session":"s-0009","role":"resumed"}]}`)
	got := push(`{"schema":1,"writers_measured":true,"readers_measured":false,"members":[
		{"session":"s-0002","role":"wrote"}]}`)
	if strings.Join(readers(got), " ") != "s-0002/wrote s-0009/resumed+carried" {
		t.Fatalf("after an unmeasured-readers push: %v", readers(got))
	}
	got = push(`{"schema":1,"writers_measured":true,"readers_measured":true,"members":[
		{"session":"s-0002","role":"wrote"}]}`)
	if strings.Join(readers(got), " ") != "s-0002/wrote" {
		t.Fatalf("a MEASURED readers leg replaces outright: %v", readers(got))
	}
}

func TestARepeatedIdenticalPushIsUnchangedAndWritesNothing(t *testing.T) {
	j := Journal{Path: filepath.Join(t.TempDir(), "journal.jsonl")}
	reg, _ := decode(t, `{"schema":1,"status":"open"}`)
	first, err := j.Register(reg, clock)
	if err != nil || first.Status != OutcomeRegistered {
		t.Fatalf("first push: %+v %v", first, err)
	}
	second, err := j.Register(reg, clock.Add(time.Hour))
	if err != nil || second.Status != OutcomeUnchanged {
		t.Fatalf("an identical re-push (later pod clock) must be unchanged: %+v %v", second, err)
	}
	// The control: a different principal pushing the same content IS a change.
	reg.RegisteredBy = "other-writer"
	third, err := j.Register(reg, clock.Add(2*time.Hour))
	if err != nil || third.Status != OutcomeRegistered {
		t.Fatalf("a different registered_by is a change: %+v %v", third, err)
	}
	data, _ := os.ReadFile(j.Path)
	if n := strings.Count(string(data), "\n"); n != 2 {
		t.Fatalf("the journal holds %d lines, want 2 (one per CHANGE):\n%s", n, data)
	}
}

// TestATornTailIsIgnoredOnReadAndSealedBeforeTheNextAppend is the crash case: the last write
// stopped part-way, so the file ends without a newline.
//
// 🔴 TWO PROPERTIES, BOTH RED BY MUTATION: (1) the fold does not apply the fragment and does not
// call it a skipped record — it is a torn tail; (2) the next append writes a newline FIRST, or the
// new record is glued to the fragment and BOTH are lost. The fragment chosen here is a complete
// JSON prefix that is NOT itself valid, plus a second case where the fragment is a whole valid
// object missing only its newline — which a parse-based tail rule would wrongly apply.
func TestATornTailIsIgnoredOnReadAndSealedBeforeTheNextAppend(t *testing.T) {
	good := func(slug string) string {
		return `{"schema":1,"home":"alpha-notes","slug":"` + slug + `","status":"open","closing_kind":"none",` +
			`"declared_scopes":["alpha-notes"],"writers_measured":false,"readers_measured":false,` +
			`"commits_total":0,"commits_unstamped":0,"reported_at":"","members":[],` +
			`"registered_by":"wide-reader","registered_at":"2000-01-02T00:00:00Z"}`
	}
	for _, tc := range []struct{ name, fragment string }{
		{"a partial object", `{"schema":1,"home":"alpha-no`},
		{"a whole object missing only its newline", good("ghost-arc")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "journal.jsonl")
			if err := os.WriteFile(path, []byte(good("first-arc")+"\n"+tc.fragment), 0o600); err != nil {
				t.Fatal(err)
			}
			j := Journal{Path: path}
			snap, err := j.Read()
			if err != nil {
				t.Fatal(err)
			}
			if !snap.TornTail || snap.Records != 1 || snap.Skipped != 0 || len(snap.Latest) != 1 {
				t.Fatalf("read over a torn tail: torn=%v records=%d skipped=%d latest=%d",
					snap.TornTail, snap.Records, snap.Skipped, len(snap.Latest))
			}
			if _, ok := snap.Latest[Key{"alpha-notes", "ghost-arc"}]; ok {
				t.Fatal("a fragment with no newline was APPLIED")
			}
			reg, _ := decode(t, `{"schema":1}`)
			if _, err := j.Register(reg, clock); err != nil {
				t.Fatal(err)
			}
			snap, err = j.Read()
			if err != nil {
				t.Fatal(err)
			}
			// The fragment is now a complete (sealed) line: skipped once, never applied; the new
			// record survives beside the old one.
			if snap.TornTail || snap.Records != 2 || snap.Skipped != 1 {
				t.Fatalf("after the next append: torn=%v records=%d skipped=%d", snap.TornTail, snap.Records, snap.Skipped)
			}
			if _, ok := snap.Latest[Key{"alpha-notes", "gadget-rollout"}]; !ok {
				t.Fatal("the record appended after a torn tail was LOST")
			}
		})
	}
}

// TestOneBadLineDoesNotPoisonTheJournal: a corrupt complete line is skipped and counted; the
// records on both sides of it still fold.
func TestOneBadLineDoesNotPoisonTheJournal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	j := Journal{Path: path}
	for _, slug := range []string{"before-arc", "after-arc"} {
		reg, _, err := DecodePayload([]byte(`{"schema":1}`), "alpha-notes", slug)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := j.Register(reg, clock); err != nil {
			t.Fatal(err)
		}
		if slug == "before-arc" {
			f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
			f.WriteString("{not json}\n" + `{"schema":1,"home":"Alpha Notes"}` + "\n")
			f.Close()
		}
	}
	snap, err := j.Read()
	if err != nil {
		t.Fatal(err)
	}
	if snap.Records != 2 || snap.Skipped != 2 || len(snap.Latest) != 2 || snap.TornTail {
		t.Fatalf("records=%d skipped=%d latest=%d torn=%v", snap.Records, snap.Skipped, len(snap.Latest), snap.TornTail)
	}
}

// TestConcurrentRegistrationsOfOneArcLoseNothing is the lock's own measurement.
//
// 🔴 WHY ONE KEY, NOT N KEYS. `O_APPEND` alone already keeps N distinct appends from interleaving,
// so a distinct-key test would pass with the lock deleted and prove nothing about it. The lock
// exists for the READ-MERGE-APPEND sequence: N concurrent pushes of the SAME arc, each naming one
// new writer with `writers_measured: false`, must each merge against the previous push's result,
// so the final state carries ALL N writers plus the seed member. Two pushes merging against the
// same stale state would silently drop one writer. N = 32 goroutines, each with its own descriptor
// (so `flock` contends exactly as two processes would).
//
// And every line written must parse: `Skipped` == 0 is the "no interleaved bytes" half.
func TestConcurrentRegistrationsOfOneArcLoseNothing(t *testing.T) {
	const n = 32
	j := Journal{Path: filepath.Join(t.TempDir(), "journal.jsonl")}
	seed, _ := decode(t, `{"schema":1,"writers_measured":true,"members":[{"session":"s-seed","role":"originated"}]}`)
	if _, err := j.Register(seed, clock); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			reg, _, err := DecodePayload([]byte(fmt.Sprintf(
				`{"schema":1,"writers_measured":false,"members":[{"session":"s-%04d","role":"wrote"}]}`, i)),
				"alpha-notes", "gadget-rollout")
			if err != nil {
				errs <- err
				return
			}
			<-start
			if _, err := j.Register(reg, clock); err != nil {
				errs <- err
			}
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	snap, err := j.Read()
	if err != nil {
		t.Fatal(err)
	}
	if snap.Skipped != 0 || snap.TornTail || snap.Records != n+1 {
		t.Fatalf("records=%d (want %d) skipped=%d torn=%v — interleaved or lost writes", snap.Records, n+1, snap.Skipped, snap.TornTail)
	}
	final := snap.Latest[Key{"alpha-notes", "gadget-rollout"}]
	if len(final.Members) != n+1 {
		t.Fatalf("the final state carries %d members, want %d: a concurrent push merged against a STALE state and dropped a writer", len(final.Members), n+1)
	}
}

func TestAMissingJournalIsEmptyAndAnUnreadableOneIsNot(t *testing.T) {
	dir := t.TempDir()
	snap, err := Journal{Path: filepath.Join(dir, "never-written.jsonl")}.Read()
	if err != nil || !snap.Missing || len(snap.Latest) != 0 {
		t.Fatalf("a configured journal nobody wrote reads as EMPTY: %+v %v", snap, err)
	}
	// A directory where the file should be: configured and NOT readable — "could not look".
	_, err = Journal{Path: dir}.Read()
	var unreadable *JournalUnreadableError
	if !errors.As(err, &unreadable) {
		t.Fatalf("an unreadable journal must be a *JournalUnreadableError, got %v", err)
	}
}

// --- ResolveJournalPath --------------------------------------------------------------------

func TestResolveJournalPath(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "alpha-notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	link := filepath.Join(outside, "into-root")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	dangling := filepath.Join(outside, "dangling.jsonl")
	if err := os.Symlink(filepath.Join(outside, "nowhere", "x.jsonl"), dangling); err != nil {
		t.Fatal(err)
	}
	fileLink := filepath.Join(outside, "file-link.jsonl")
	if err := os.WriteFile(filepath.Join(root, "alpha-notes", "planted.jsonl"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "alpha-notes", "planted.jsonl"), fileLink); err != nil {
		t.Fatal(err)
	}

	// THE CONTROL: outside, parent exists, file absent → resolves, unchanged.
	want := filepath.Join(outside, "journal.jsonl")
	got, err := ResolveJournalPath(root, want)
	resolvedOutside, _ := filepath.EvalSymlinks(outside)
	if err != nil || got != filepath.Join(resolvedOutside, "journal.jsonl") {
		t.Fatalf("an outside path must resolve: %q %v", got, err)
	}

	for _, tc := range []struct{ name, path string }{
		{"the store root itself", root},
		{"a dot directory under the root", filepath.Join(root, ".arcs", "journal.jsonl")},
		{"a scope directory", filepath.Join(root, "alpha-notes", "journal.jsonl")},
		{"a symlinked parent into the root", filepath.Join(link, "journal.jsonl")},
		{"a symlinked FILE into the root", fileLink},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ResolveJournalPath(root, tc.path)
			var inside *InsideStoreError
			// The dot directory does not exist, so its PARENT check fails first — still a refusal.
			if tc.name == "a dot directory under the root" {
				if err == nil {
					t.Fatal("must refuse")
				}
				return
			}
			if !errors.As(err, &inside) {
				t.Fatalf("must be refused as INSIDE the store root, got %v", err)
			}
		})
	}
	for _, tc := range []struct{ name, path, want string }{
		{"a dangling symlink", dangling, "does not resolve"},
		{"a missing parent directory", filepath.Join(outside, "no-such-dir", "j.jsonl"), "does not exist"},
		{"a directory", outside, "is a directory"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ResolveJournalPath(root, tc.path)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("must refuse with %q, got %v", tc.want, err)
			}
		})
	}
}
