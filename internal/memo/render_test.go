package memo

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"
)

var update = flag.Bool("update", false, "rewrite testdata/golden/*.golden from the renderer")

// ---------------------------------------------------------------- the corpus

type member struct {
	Name  string `json:"name"`
	Field string `json:"field"`
	Text  string `json:"text"`
}

type corpus struct {
	FixedNonce      string   `json:"fixed_nonce"`
	FenceBreaking   []member `json:"fence_breaking"`
	SendAccepted    []member `json:"send_accepted"`
	RenderExtra     []member `json:"render_extra"`
	Ordinary        []member `json:"ordinary"`
	OrdinaryRefused []member `json:"ordinary_refused"`
}

// loadCorpus reads the committed hostile corpus. 🔴 It REFUSES rather than skips when the file
// is absent: the nix `onlyGo` filter must carry it, and a sandbox build without it must go red
// naming the filter, never pass over nothing.
func loadCorpus(t *testing.T) corpus {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "hostile.json"))
	if err != nil {
		t.Fatalf("hostile corpus unreadable (%v): regenerate with `python3 tests/memo/hostile.py`, "+
			"and check flake.nix's onlyGo filter names internal/memo/testdata/hostile.json", err)
	}
	var c corpus
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatalf("hostile corpus does not parse: %v", err)
	}
	return c
}

func (c corpus) hostile() []member {
	return append(append([]member{}, c.FenceBreaking...), c.SendAccepted...)
}

func (c corpus) all() []member {
	out := c.hostile()
	out = append(out, c.RenderExtra...)
	out = append(out, c.Ordinary...)
	return append(out, c.OrdinaryRefused...)
}

// ---------------------------------------------------------------- fixtures

var t0 = time.Date(2000, 1, 2, 3, 4, 5, 0, time.UTC)

func baseFields(id int64) Fields {
	return Fields{
		ID: id, Scope: "alpha-notes",
		SenderKind: "user", SenderID: "u-0001", SenderDisplay: "writer-a",
		CredentialLabel: "agents@host-a",
		Subject:         "schema change lands tomorrow",
		Body:            "the column rename in the alpha store ships with the next migration",
		CreatedAt:       t0.Add(time.Duration(id) * time.Minute),
		ExpiresAt:       t0.Add(72 * time.Hour),
	}
}

// hostileMemos is the 16 the e2e (d) clause will hold: each member in its own memo, in the
// field the corpus names, ids 1..16 in corpus order.
func hostileMemos(c corpus) []Stored {
	var out []Stored
	for i, m := range c.hostile() {
		f := baseFields(int64(i + 1))
		if m.Field == "subject" {
			f.Subject = m.Text
		} else {
			f.Body = m.Text
		}
		out = append(out, NewStored(f))
	}
	return out
}

// ---------------------------------------------------------------- the structural instrument

type block struct {
	nonce   string
	count   int
	inner   []string
	headers []string
	more    []string
}

var (
	openRE   = regexp.MustCompile(`^<<<cairn-memo untrusted nonce=([0-9a-f]{16}) count=([0-9]+)>>>$`)
	closeRE  = regexp.MustCompile(`^<<<end cairn-memo nonce=([0-9a-f]{16})>>>$`)
	headerRE = regexp.MustCompile(`^\| \[m-[0-9]+\] from `)
	moreRE   = regexp.MustCompile(`^\| and [0-9]+ more on `)
)

// parseBlock is the S0 test plan's STRUCTURAL assertion, applied to every block any test here
// renders: exactly one opening and one closing marker, both at column 0 and first/last, equal
// nonces; every line between them starts with the content prefix; the output is valid UTF-8 and
// holds NO code point `Unsafe` is true for — decoded as runes, never matched as bytes (the
// renderer's own "⏎" and "…" carry continuation bytes in 0x80-0x9F); and no content line carries
// the marker word or the nonce.
func parseBlock(t *testing.T, out string) block {
	t.Helper()
	if !utf8.ValidString(out) {
		t.Fatalf("block is not valid UTF-8")
	}
	for i, r := range out {
		if r != '\n' && Unsafe(r) {
			t.Fatalf("block carries unsafe code point U+%04X at byte %d", r, i)
		}
	}
	if !strings.HasSuffix(out, "\n") {
		t.Fatalf("block does not end in a newline")
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("block has %d lines", len(lines))
	}
	markers := 0
	for _, l := range lines {
		if strings.HasPrefix(l, "<<<") {
			markers++
		}
	}
	if markers != 2 {
		t.Fatalf("want exactly 2 column-0 markers, got %d", markers)
	}
	o := openRE.FindStringSubmatch(lines[0])
	c := closeRE.FindStringSubmatch(lines[len(lines)-1])
	if o == nil || c == nil {
		t.Fatalf("markers malformed:\n%q\n%q", lines[0], lines[len(lines)-1])
	}
	if o[1] != c[1] {
		t.Fatalf("nonces differ: %s vs %s", o[1], c[1])
	}
	b := block{nonce: o[1], inner: lines[1 : len(lines)-1]}
	b.count, _ = strconv.Atoi(o[2])
	for _, l := range b.inner {
		if !strings.HasPrefix(l, contentPrefix) {
			t.Fatalf("content line without the prefix: %q", l)
		}
		low := strings.ToLower(l)
		if strings.Contains(low, markerWord) || strings.Contains(low, b.nonce) {
			t.Fatalf("content line carries the marker word or the nonce: %q", l)
		}
		if headerRE.MatchString(l) {
			b.headers = append(b.headers, l)
		}
		if moreRE.MatchString(l) {
			b.more = append(b.more, l)
		}
	}
	return b
}

// ---------------------------------------------------------------- send side: BreaksFence

func TestBreaksFencePartitionsTheHostileCorpusEightAndEight(t *testing.T) {
	c := loadCorpus(t)
	if len(c.FenceBreaking) != 8 || len(c.SendAccepted) != 8 {
		t.Fatalf("partition is %d+%d, e2e (d) needs exactly 8+8", len(c.FenceBreaking), len(c.SendAccepted))
	}
	judge := func(m member) bool {
		if m.Field == "subject" {
			return SubjectBreaksFence(m.Text)
		}
		return BreaksFence(m.Text)
	}
	for _, m := range c.FenceBreaking {
		if !judge(m) {
			t.Errorf("fence-breaking member %q was ACCEPTED at send", m.Name)
		}
	}
	for _, m := range c.SendAccepted {
		if judge(m) {
			t.Errorf("send-accepted member %q was REFUSED at send", m.Name)
		}
	}
}

func TestBreaksFenceAcceptsOrdinaryTextAndRefusesTheStatedResiduals(t *testing.T) {
	c := loadCorpus(t)
	if len(c.Ordinary) == 0 || len(c.OrdinaryRefused) == 0 {
		t.Fatalf("ordinary corpus empty")
	}
	for _, m := range c.Ordinary {
		if BreaksFence(m.Text) {
			t.Errorf("ordinary text %q REFUSED at send", m.Name)
		}
	}
	// ⚠ STATED COSTS, asserted so nobody "fixes" them silently (decision 5, Q18).
	for _, m := range c.OrdinaryRefused {
		if !BreaksFence(m.Text) {
			t.Errorf("stated residual %q is now ACCEPTED — a change to the send rule, decide it in the plan", m.Name)
		}
	}
}

func TestBreaksFenceRefusesEachHostileClass(t *testing.T) {
	var refuse []string
	for r := rune(0); r < 0x20; r++ {
		if r != '\n' && r != '\t' {
			refuse = append(refuse, "x"+string(r)+"y")
		}
	}
	for r := rune(0x7F); r <= 0x9F; r++ {
		refuse = append(refuse, "x"+string(r)+"y")
	}
	for _, rg := range unicode.Bidi_Control.R16 {
		for r := rune(rg.Lo); r <= rune(rg.Hi); r += rune(rg.Stride) {
			refuse = append(refuse, "x"+string(r)+"y")
		}
	}
	for r := rune(0xE0000); r <= 0xE007F; r++ {
		refuse = append(refuse, "x"+string(r)+"y")
	}
	refuse = append(refuse,
		"x y", "x y", // Zl, Zp
		"x\xed\xa0\x80y",        // a lone surrogate's three-byte encoding
		"x\xffy",                // an invalid byte
		"x\ry",                  // a LONE CR is Cc; only "\r\n" is folded
		"‮", "⁦", "‎", "‏", "؜", // named, so the table walk above has a control
	)
	for _, s := range refuse {
		if !BreaksFence(s) {
			t.Errorf("ACCEPTED at send: %q", s)
		}
	}
	for _, s := range []string{"a\nb", "a\tb", "a\r\nb", "plain", "​", "️", "", "͸"} {
		if BreaksFence(s) {
			t.Errorf("REFUSED at send, but the send rule is narrow: %q", s)
		}
	}
}

func TestSubjectRefusesNewlineAndTab(t *testing.T) {
	for _, s := range []string{"a\nb", "a\tb", "a\r\nb"} {
		if !SubjectBreaksFence(s) {
			t.Errorf("subject ACCEPTED: %q", s)
		}
		if BreaksFence(s) {
			t.Errorf("body REFUSED, but only a SUBJECT refuses \\n and \\t: %q", s)
		}
	}
	if SubjectBreaksFence("one line subject") {
		t.Errorf("an ordinary subject was refused")
	}
	if !SubjectBreaksFence("x‮y") {
		t.Errorf("a subject must also refuse everything a body refuses")
	}
}

func TestNormalizeFoldsCRLFOnly(t *testing.T) {
	if got := Normalize("a\r\nb\rc\n"); got != "a\nb\rc\n" {
		t.Fatalf("Normalize = %q", got)
	}
}

// ---------------------------------------------------------------- render side: Unsafe

func TestUnsafeByCategoryIncludingTagsAndVariationSelectors(t *testing.T) {
	unsafe := []rune{
		0x00, 0x1B, 0x7F, 0x85, 0x9F, // Cc
		0x00AD, 0x180E, 0x200B, 0x200C, 0x200D, 0x200E, 0x202E, 0x2060, 0x2061, 0x2064, 0x2066, 0xFEFF, // Cf
		0xE0001, 0xE0041, 0xE007F, // the TAG block (Cf)
		0xE000, 0xF8FF, 0x10FFFD, // Co
		0x0378, 0xFFFE, 0xFFFF, // Cn (unassigned, noncharacters)
		0x2028, 0x2029, // Zl, Zp
		0xFE00, 0xFE0F, 0x180B, 0xE0100, 0xE01EF, // Variation_Selector (Mn)
	}
	for _, r := range unsafe {
		if !Unsafe(r) {
			t.Errorf("Unsafe(U+%04X) = false", r)
		}
	}
	safe := []rune{'a', 'Z', '0', ' ', '|', 'é', '漢', 'ש', 0x0301 /* combining acute, Mn */, utf8.RuneError, '⏎', '…', '·', 0x1F468}
	for _, r := range safe {
		if Unsafe(r) {
			t.Errorf("Unsafe(U+%04X) = true", r)
		}
	}
}

func TestSanitizeReplacesEveryUnsafeCodePointWithLiteralExpectations(t *testing.T) {
	cases := map[string]string{
		"func f() {\n\tif x {\n\t\treturn\n\t}\n}\n": "func f() { ⏎  if x { ⏎   return ⏎  } ⏎ } ⏎ ",
		"first line\r\nsecond line":                  "first line ⏎ second line",
		"careful ⚠️ here":                            "careful ⚠� here",
		"a​b⁠c":                                      "a�b�c",
		"x" + string(rune(0xE0041)) + "y":            "x�y",
		"looks empty:" + string(rune(0xE0100)):       "looks empty:�",
		"esc\x1b[2J":                                 "esc�[2J",
		"bad\xffbyte":                                "bad�byte",
		"plain":                                      "plain",
	}
	for in, want := range cases {
		if got := sanitize(in); got != want {
			t.Errorf("sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

// INVARIANT GUARD (Guard C's premise, round 5): rendering already-rendered text changes
// nothing, because U+FFFD is So and never Unsafe. The client re-renders what the listener
// already rendered; this pins that the double pass is a no-op.
func TestRenderingRenderedTextIsANoOp(t *testing.T) {
	for _, m := range loadCorpus(t).all() {
		once := sanitize(m.Text)
		if twice := sanitize(once); twice != once {
			t.Errorf("%s: second pass changed the text", m.Name)
		}
	}
}

// ---------------------------------------------------------------- every field, every member

func stringFields(v reflect.Value) []string {
	var out []string
	for i := 0; i < v.NumField(); i++ {
		if v.Field(i).Kind() == reflect.String {
			out = append(out, v.Type().Field(i).Name)
		}
	}
	return out
}

// TestEveryFieldsStringIsSanitisedOnEveryOutput plants EVERY corpus member (both halves, the
// render extras, the ordinary text and a 1 MiB line generated here) in EVERY string field of
// Fields — found by reflection, so a field added later is planted without anyone extending a
// list — and requires each output to hold the fence: the preview block, the full block, and every
// string field of Rendered.
func TestEveryFieldsStringIsSanitisedOnEveryOutput(t *testing.T) {
	c := loadCorpus(t)
	members := append(c.all(), member{Name: "one-mebibyte-line", Text: strings.Repeat("y", 1<<20) + "\x1b[2J"})
	names := stringFields(reflect.ValueOf(Fields{}))
	if len(names) < 10 {
		t.Fatalf("reflection found only %d string fields in Fields: %v", len(names), names)
	}
	planted := 0
	for _, field := range names {
		for _, m := range members {
			f := baseFields(7)
			if strings.HasPrefix(field, "Retractor") {
				f.RetractedAt = t0.Add(time.Hour)
				f.RetractorKind, f.RetractorID, f.RetractorDisplay, f.RetractorLabel = "user", "u-0004", "admin-d", "admin@host-d"
			}
			if field == "SenderID" || field == "SenderKind" {
				f.SenderDisplay = "" // the id is shown only for a principal that no longer resolves
			}
			if field == "RetractorID" {
				f.RetractorDisplay = ""
			}
			reflect.ValueOf(&f).Elem().FieldByName(field).SetString(m.Text)
			s := NewStored(f)
			for _, out := range []string{renderBlock([]Stored{s}, c.FixedNonce, true), renderBlock([]Stored{s}, c.FixedNonce, false)} {
				b := parseBlock(t, out)
				if b.count != 1 || len(b.headers) != 1 {
					t.Fatalf("%s in %s: count=%d headers=%d", m.Name, field, b.count, len(b.headers))
				}
			}
			rv := reflect.ValueOf(Render(s))
			for _, rf := range stringFields(rv) {
				for _, r := range rv.FieldByName(rf).String() {
					if Unsafe(r) {
						t.Fatalf("%s planted in Fields.%s reached Rendered.%s as U+%04X", m.Name, field, rf, r)
					}
				}
			}
			planted++
		}
	}
	// POSITIVE CONTROL: the planting really happened, over every field and member.
	if planted != len(names)*len(members) {
		t.Fatalf("planted %d, want %d", planted, len(names)*len(members))
	}
}

// ---------------------------------------------------------------- Guard D, the renderer half

// TestGuardDTheHostileCorpusIsOneBlockOnBothPaths is the renderer half of e2e clause (d): the
// 16 hostile members render through the PREVIEW path as exactly one block with count=16, exactly
// five previews and the line "and 11 more", and through the FULL path as exactly one block
// holding all 16 bodies — every content line prefixed, no unsafe code point. e2e (d) asserts the
// same properties of the BUILT client against a real and a raw-serving listener (S3); this is
// what it is built on, and what sabotages (d1)/(d2) look like at the renderer.
func TestGuardDTheHostileCorpusIsOneBlockOnBothPaths(t *testing.T) {
	memos := hostileMemos(loadCorpus(t))
	if len(memos) != 16 {
		t.Fatalf("want 16 hostile memos, got %d", len(memos))
	}
	p := parseBlock(t, RenderPreview(memos))
	if p.count != 16 || len(p.headers) != 5 {
		t.Fatalf("preview: count=%d previews=%d, want 16 and 5", p.count, len(p.headers))
	}
	if len(p.more) != 1 || p.more[0] != "| and 11 more on alpha-notes: cairn memo-read --scope alpha-notes" {
		t.Fatalf("preview's K-more line: %q", p.more)
	}
	f := parseBlock(t, RenderFull(memos))
	bodies := 0
	for _, l := range f.inner {
		if strings.HasPrefix(l, "|   body: ") {
			bodies++
		}
	}
	if f.count != 16 || len(f.headers) != 16 || bodies != 16 || len(f.more) != 0 {
		t.Fatalf("full: count=%d headers=%d bodies=%d more=%d, want 16/16/16/0", f.count, len(f.headers), bodies, len(f.more))
	}
}

func TestThePreviewIsNewestFirstAndCapsAtFive(t *testing.T) {
	var memos []Stored
	for id := int64(1); id <= 7; id++ {
		memos = append(memos, NewStored(baseFields(id)))
	}
	b := parseBlock(t, RenderPreview(memos))
	var ids []string
	for _, h := range b.headers {
		ids = append(ids, regexp.MustCompile(`\[m-([0-9]+)\]`).FindStringSubmatch(h)[1])
	}
	if got := strings.Join(ids, ","); got != "7,6,5,4,3" {
		t.Fatalf("preview order %s, want newest first 7,6,5,4,3", got)
	}
	if len(b.more) != 1 || b.more[0] != "| and 2 more on alpha-notes: cairn memo-read --scope alpha-notes" {
		t.Fatalf("K-more: %q", b.more)
	}
}

func TestThePreviewCutsRunesNotBytes(t *testing.T) {
	var m member
	for _, x := range loadCorpus(t).RenderExtra {
		if x.Name == "preview-cut-on-multibyte-rune" {
			m = x
		}
	}
	if m.Name == "" {
		t.Fatalf("corpus lacks the multi-byte cut member")
	}
	f := baseFields(3)
	f.Body = m.Text
	out := renderBlock([]Stored{NewStored(f)}, "0123456789abcdef", true)
	want := "|   preview: " + strings.Repeat("a", 199) + "漢…"
	if !strings.Contains(out, want+"\n") {
		t.Fatalf("preview line wrong; want %q in:\n%s", want, out)
	}
	if cut("short") != "short" {
		t.Fatalf("a short body must not be marked as cut")
	}
}

// ---------------------------------------------------------------- silence, nonce, marker word

func TestSilenceIsZeroBytesWithAPositiveControl(t *testing.T) {
	if out := RenderPreview(nil); len(out) != 0 {
		t.Fatalf("RenderPreview(nil) = %d bytes", len(out))
	}
	if out := RenderFull([]Stored{}); len(out) != 0 {
		t.Fatalf("RenderFull([]) = %d bytes", len(out))
	}
	// POSITIVE CONTROL: one memo is NOT silent, so the zero above is not a renderer wired to nothing.
	if out := RenderPreview([]Stored{NewStored(baseFields(1))}); len(out) == 0 {
		t.Fatalf("one memo rendered zero bytes")
	}
}

func TestEachRenderDrawsAFreshNonce(t *testing.T) {
	m := []Stored{NewStored(baseFields(1))}
	a, b := parseBlock(t, RenderPreview(m)), parseBlock(t, RenderPreview(m))
	if a.nonce == b.nonce {
		t.Fatalf("two renders shared nonce %s", a.nonce)
	}
	c, d := parseBlock(t, RenderFull(m)), parseBlock(t, RenderFull(m))
	if c.nonce == d.nonce {
		t.Fatalf("two full renders shared nonce %s", c.nonce)
	}
}

func TestMarkerWordAndNonceAreReplacedInContent(t *testing.T) {
	c := loadCorpus(t)
	for _, name := range []string{"guessed-nonce", "marker-word-in-subject", "closing-marker-mid-line"} {
		var m member
		for _, x := range c.SendAccepted {
			if x.Name == name {
				m = x
			}
		}
		// POSITIVE CONTROL: the input really carries what must be replaced.
		low := strings.ToLower(m.Text)
		if !strings.Contains(low, markerWord) && !strings.Contains(low, c.FixedNonce) {
			t.Fatalf("%s carries neither the marker word nor the nonce", name)
		}
		f := baseFields(2)
		if m.Field == "subject" {
			f.Subject = m.Text
		} else {
			f.Body = m.Text
		}
		for _, preview := range []bool{true, false} {
			out := renderBlock([]Stored{NewStored(f)}, c.FixedNonce, preview)
			parseBlock(t, out) // fails on the marker word or the nonce in any content line
			if !strings.Contains(out, markerPlaceholder) {
				t.Fatalf("%s: no visible placeholder in\n%s", name, out)
			}
		}
	}
}

// ---------------------------------------------------------------- sender, tombstone, commands

func TestSenderLineIsTheAuthoritysWord(t *testing.T) {
	f := baseFields(1)
	if got := Render(NewStored(f)).Sender; got != "writer-a via agents@host-a (user)" {
		t.Fatalf("sender = %q", got)
	}
	f.SenderDisplay = ""
	if got := Render(NewStored(f)).Sender; got != "user:u-0001 (no longer known) via agents@host-a" {
		t.Fatalf("unknown sender = %q", got)
	}
	f.CredentialLabel = ""
	if got := Render(NewStored(f)).Sender; got != "user:u-0001 (no longer known)" {
		t.Fatalf("unknown sender, no label = %q", got)
	}
}

func TestARetractedMemoShowsTheTombstoneAndNoText(t *testing.T) {
	f := baseFields(4)
	f.RetractedAt = time.Date(2000, 1, 3, 0, 0, 0, 0, time.UTC)
	f.RetractorKind, f.RetractorID, f.RetractorDisplay, f.RetractorLabel = "user", "u-0004", "admin-d", "admin@host-d"
	r := Render(NewStored(f))
	if r.Subject != "" || r.Body != "" {
		t.Fatalf("retracted memo kept text: %+v", r)
	}
	if r.Tombstone != "retracted by admin-d via admin@host-d (user) at 2000-01-03T00:00:00Z" {
		t.Fatalf("tombstone = %q", r.Tombstone)
	}
	out := RenderFull([]Stored{NewStored(f)})
	parseBlock(t, out)
	if strings.Contains(out, f.Subject) || strings.Contains(out, f.Body) {
		t.Fatalf("retracted text reached the block")
	}
}

func TestCommandsNameTheirScopeAndQuoteAnythingButAPlainWord(t *testing.T) {
	f := baseFields(17)
	out := renderBlock([]Stored{NewStored(f)}, "0123456789abcdef", true)
	if !strings.Contains(out, "|   full text: cairn memo-read --scope alpha-notes --id 17\n") {
		t.Fatalf("full-text command missing:\n%s", out)
	}
	f.Scope = "alpha notes; rm -rf ~"
	out = renderBlock([]Stored{NewStored(f)}, "0123456789abcdef", true)
	if !strings.Contains(out, "--scope 'alpha notes; rm -rf ~' --id 17\n") {
		t.Fatalf("hostile scope not quoted:\n%s", out)
	}
	if got := shellWord("it's"); got != `'it'\''s'` {
		t.Fatalf("shellWord quote = %s", got)
	}
}

// ---------------------------------------------------------------- goldens

func TestGoldens(t *testing.T) {
	c := loadCorpus(t)
	one := NewStored(baseFields(17))
	cases := map[string]string{
		"preview_one.golden":     renderBlock([]Stored{one}, c.FixedNonce, true),
		"preview_hostile.golden": renderBlock(hostileMemos(c), c.FixedNonce, true),
		"full_hostile.golden":    renderBlock(hostileMemos(c), c.FixedNonce, false),
	}
	for name, got := range cases {
		parseBlock(t, got)
		path := filepath.Join("testdata", "golden", name)
		if *update {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s unreadable (%v): regenerate with `go test ./internal/memo -run TestGoldens -update`, "+
				"and check flake.nix's onlyGo filter names it", name, err)
		}
		if got != string(want) {
			t.Errorf("%s differs from the renderer's output; regenerate and DIFF, never hand-edit", name)
		}
	}
}

// ---------------------------------------------------------------- the status table

// A pinned CONTRACT with the delivery hook in the tooling repository (decision 7), which keeps
// its own copy; from S4 that hook's test reads this table out of the built binary.
func TestStatusesIsTheClosedSet(t *testing.T) {
	want := "new none no-scope scope-unreadable unconfigured unreachable malformed"
	if got := strings.Join(Statuses(), " "); got != want {
		t.Fatalf("Statuses() = %q, want %q", got, want)
	}
}
