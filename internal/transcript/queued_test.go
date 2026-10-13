package transcript

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/transcript/transcripttest"
)

// A human message typed WHILE A TURN RUNS is not stored as a `user` record: the runtime queues it and
// writes it as an `attachment` of type `queued_command` with `commandMode: "prompt"` (and
// `origin.kind: "human"`), measured on the runtime's own JSONL. Classed as bookkeeping — as every
// attachment was — it vanished from `view=conversation`, from `class=human-text`, from the skeleton's
// turn index and from the browser surface's collapsed reading, while it is exactly the thing O9 says
// is never cut. The fixture is the generator's `cc-queued-prompt` session: a prompt (1), a call (2)
// and its result (3), the queued prompt (4), a queued TASK NOTIFICATION — bookkeeping, the control —
// (5), and the assistant's reply (6).

func TestAQueuedPromptIsHumanTextAndNothingElseQueuedIs(t *testing.T) {
	cases := []struct {
		name, rec string
		want      []string
	}{
		{"a prompt typed mid-turn is human text",
			`{"type":"attachment","attachment":{"type":"queued_command","prompt":"also this","commandMode":"prompt","origin":{"kind":"human"}}}`,
			[]string{"human-text@/attachment/prompt"}},
		{"in a sidechain it is the parent agent's, never a human's",
			`{"type":"attachment","isSidechain":true,"attachment":{"type":"queued_command","prompt":"x","commandMode":"prompt"}}`,
			[]string{"agent-prompt@/attachment/prompt"}},
		{"a queued TASK NOTIFICATION is the runtime's, and stays bookkeeping",
			`{"type":"attachment","attachment":{"type":"queued_command","prompt":"done","commandMode":"task-notification"}}`,
			[]string{"bookkeeping@"}},
		{"a queued prompt the runtime marks isMeta (a peer's relayed message) stays bookkeeping",
			`{"type":"attachment","attachment":{"type":"queued_command","prompt":"x","commandMode":"prompt","isMeta":true}}`,
			[]string{"bookkeeping@"}},
		{"a prompt that is not a string is unknown — shown and counted, never dropped",
			`{"type":"attachment","attachment":{"type":"queued_command","prompt":[{"type":"text","text":"x"}],"commandMode":"prompt"}}`,
			[]string{"unknown@/attachment/prompt"}},
		{"any other attachment is still bookkeeping",
			`{"type":"attachment","attachment":{"type":"hook_success","content":"x"}}`,
			[]string{"bookkeeping@"}},
	}
	for _, c := range cases {
		if got := classes(ClassifyClaude(rec(t, c.rec))); !slices.Equal(got, c.want) {
			t.Errorf("%s:\n  got  %v\n  want %v", c.name, got, c.want)
		}
	}
}

// queuedFixture stores the generator's `cc-queued-prompt` session and returns its View and the
// queued prompt's text as the generator wrote it.
func queuedFixture(t *testing.T) (*View, string) {
	t.Helper()
	w := loadWorld(t)
	sess, ok := w.Sessions["cc-queued-prompt"]
	if !ok {
		t.Fatal("the synthetic world has no cc-queued-prompt session; regenerate with tests/transcripts/gen.py")
	}
	file, ok := w.Claude.Files["-work-alpha/"+sess.ID+".jsonl"]
	if !ok {
		t.Fatalf("cc-queued-prompt's stream file is not in the world")
	}
	lines := strings.Split(strings.TrimRight(file["utf8"], "\n"), "\n")
	if len(lines) != 6 {
		t.Fatalf("cc-queued-prompt holds %d records, want 6 (the layout this test names)", len(lines))
	}
	var queued struct {
		Attachment struct {
			Type, Prompt, CommandMode string
		}
	}
	if err := json.Unmarshal([]byte(lines[3]), &queued); err != nil || queued.Attachment.CommandMode != "prompt" {
		t.Fatalf("record 4 is not the queued prompt: %v %+v", err, queued)
	}
	a := transcripttest.Open(t)
	transcripttest.Append(t, a, transcripttest.Session{Root: sess.ID, Owner: readOwner}, "main", hostRedacted(t, lines))
	meta, err := a.ReadMeta(sess.ID)
	if err != nil || meta == nil {
		t.Fatalf("not stored: %v", err)
	}
	return &View{Meta: meta, a: a}, queued.Attachment.Prompt
}

func TestAMidTurnPromptIsReadAsAHumanMessageEverywhere(t *testing.T) {
	v, prompt := queuedFixture(t)

	// The conversation view keeps the queued prompt WHOLE and leaves the task notification out.
	p, err := v.Page(RecordsQuery{Stream: "main", View: ViewConversation, MaxBytes: MaxPageBytes})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Records) != 6 {
		t.Fatalf("the conversation page holds %d records, want all 6 (placeholders included)", len(p.Records))
	}
	if r := p.Records[3]; len(r.Omitted) != 0 || !strings.Contains(string(r.Rec), `"commandMode":"prompt"`) {
		t.Errorf("record 4 — what the human typed mid-turn — is %+v in view=conversation; want its stored record, nothing omitted", r.Omitted)
	}
	if r := p.Records[4]; r.Rec != nil || len(r.Omitted) != 1 || r.Omitted[0].Class != Bookkeeping {
		t.Errorf("record 5 — a queued task notification — must stay a bookkeeping placeholder (the control); got %+v", r.Omitted)
	}

	// `class=human-text` finds both messages.
	hp, err := v.Page(RecordsQuery{Stream: "main", Class: HumanText, View: ViewRaw, MaxBytes: MaxPageBytes})
	if err != nil {
		t.Fatal(err)
	}
	var seqs []int64
	for _, r := range hp.Records {
		seqs = append(seqs, r.Seq)
	}
	if !slices.Equal(seqs, []int64{1, 4}) {
		t.Errorf("class=human-text finds records %v, want [1 4]", seqs)
	}

	// The skeleton's turn index opens a turn at it and counts its bytes.
	sk, err := v.Skeleton()
	if err != nil {
		t.Fatal(err)
	}
	turns := sk.Streams[0].Turns
	if len(turns) != 2 || turns[1].SeqFrom != 4 || turns[1].UserBytes != len(prompt) {
		t.Errorf("the skeleton's turns are %+v; want 2, the second opening at record 4 with %d user bytes", turns, len(prompt))
	}
	if c := sk.Streams[0].Classes[HumanText]; c.Units != 2 {
		t.Errorf("the skeleton counts %d human-text units, want 2", c.Units)
	}

	// The collapsed reading (what the browser surface renders) shows it as a human message, WHOLE.
	rd := readAll(t, v.a, v.Meta.Root, 1)
	if rd.TotalTurns != 2 || len(rd.Turns) != 2 {
		t.Fatalf("the collapsed reading has %d turns, want 2", rd.TotalTurns)
	}
	humans := itemsOf(rd.Turns[1].Items, HumanText)
	if len(humans) != 1 || humans[0].Text != prompt || humans[0].Elided {
		t.Errorf("turn 2's human text is %+v; want exactly the queued prompt, whole (%d bytes)", humans, len(prompt))
	}
	if rd.Turns[1].Bookkeeping != 1 {
		t.Errorf("turn 2 counts %d bookkeeping records, want 1 (the task notification)", rd.Turns[1].Bookkeeping)
	}
}
