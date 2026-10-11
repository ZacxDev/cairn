package redact

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// The arming gate's own tests: every exit branch driven, both bounds at their edges, and both
// controls shown to refuse. ⚠ The case files here are written by the FIXER, so the real-redactor
// pass below is a test of the INSTRUMENT, never the gate itself (O15: the gate is a fresh set the
// fixer has not seen).

func caseLine(t *testing.T, c HeldBackCase) string {
	t.Helper()
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	return string(b) + "\n"
}

// stub catches a leak whose text contains "CATCH" and damages a clean line containing "DAMAGE";
// everything else passes through — so a case file can be built to hit any exact pair of counts.
type stub struct{}

func (stub) Text(d []byte) ([]byte, []Hit) {
	s := string(d)
	if strings.Contains(s, "CATCH") || strings.Contains(s, "DAMAGE") {
		return []byte("[gone]"), []Hit{{Rule: "stub"}}
	}
	return d, nil
}
func (s stub) Record(d []byte) ([]byte, []Hit)         { return s.Text(d) }
func (s stub) Blob(_ string, d []byte) ([]byte, []Hit) { return s.Text(d) }

func file(t *testing.T, leaks, caught, clean, damaged int) string {
	t.Helper()
	var b strings.Builder
	for i := 0; i < leaks; i++ {
		text := fmt.Sprintf("leak %d value=s3cretvalue%03d", i, i)
		if i < caught {
			text += " CATCH"
		}
		b.WriteString(caseLine(t, HeldBackCase{Kind: "leak", Label: fmt.Sprintf("l%d", i), Text: text,
			Secrets: []string{fmt.Sprintf("s3cretvalue%03d", i)}}))
	}
	for i := 0; i < clean; i++ {
		text := fmt.Sprintf("clean line %d", i)
		if i < damaged {
			text += " DAMAGE"
		}
		b.WriteString(caseLine(t, HeldBackCase{Kind: "clean", Label: fmt.Sprintf("c%d", i), Text: text}))
	}
	return b.String()
}

func gateWith(real textRedactor, src string) (int, string) {
	var out bytes.Buffer
	code := heldBackGate(&out, strings.NewReader(src), heldBackParts{identity: identity{}, eraser: eraser{}, real: real})
	return code, out.String()
}

// TestTheArmingGateBoundsAreTheOperatorsNumbers: 90% caught passes and one fewer fails; 15%
// damaged passes and one more fails — over 20 cases each, where one case is 5 points.
func TestTheArmingGateBoundsAreTheOperatorsNumbers(t *testing.T) {
	if ArmingRecallFloorPct != 90 || ArmingDamageCeilingPct != 15 {
		t.Fatalf("the gate's bounds moved (%d%%, %d%%): O15 fixes them at 90%% and 15%%", ArmingRecallFloorPct, ArmingDamageCeilingPct)
	}
	cases := []struct {
		name                          string
		leaks, caught, clean, damaged int
		want                          int
		line                          string
	}{
		{"exactly 90% caught, 15% damaged", 20, 18, 20, 3, HeldBackPass, "leaks caught=18/20 clean damaged=3/20"},
		{"85% caught", 20, 17, 20, 3, HeldBackFail, "leaks caught=17/20 clean damaged=3/20"},
		{"20% damaged", 20, 18, 20, 4, HeldBackFail, "leaks caught=18/20 clean damaged=4/20"},
		{"all caught, none damaged", 20, 20, 20, 0, HeldBackPass, "leaks caught=20/20 clean damaged=0/20"},
	}
	for _, c := range cases {
		code, out := gateWith(stub{}, file(t, c.leaks, c.caught, c.clean, c.damaged))
		if code != c.want || !strings.Contains(out, "\n"+c.line+"\n") {
			t.Errorf("%s: exit %d (want %d)\n%s", c.name, code, c.want, out)
		}
	}
}

// TestTheArmingGateRefusesToVouch drives every exit-2 branch.
func TestTheArmingGateRefusesToVouch(t *testing.T) {
	good := file(t, 20, 20, 20, 0)
	if code, out := gateWith(stub{}, good); code != HeldBackPass {
		t.Fatalf("the unbroken parts exit %d:\n%s", code, out)
	}
	notInText := good + caseLine(t, HeldBackCase{Kind: "leak", Label: "absent", Text: "nothing here", Secrets: []string{"zzzzzzzz9"}})
	sources := map[string]string{
		"a leak whose secret is not in its text": notInText,
		"too few leaks":                          file(t, 19, 19, 20, 0),
		"too few clean lines":                    file(t, 20, 20, 19, 0),
		"not JSON":                               good + "{oops\n",
		"unknown kind":                           good + caseLine(t, HeldBackCase{Kind: "maybe", Label: "x", Text: "x"}),
		"an unknown field":                       good + `{"kind":"clean","label":"x","text":"x","secret":"x"}` + "\n",
	}
	for name, src := range sources {
		if code, out := gateWith(stub{}, src); code != HeldBackNoVouch || !strings.Contains(out, "COULD NOT VOUCH") {
			t.Errorf("%s: exit %d, want %d:\n%s", name, code, HeldBackNoVouch, out)
		}
	}
	// The secret-not-in-text refusal is its OWN guard, not the identity control's: the identity
	// control would also refuse that file (an absent secret scores as caught), so only this
	// message shows the parse-time check is the one that fired.
	if _, out := gateWith(stub{}, notInText); !strings.Contains(out, "a secret is not in the case's text") {
		t.Errorf("the absent-secret file was refused, but not by the parse-time check:\n%s", out)
	}
	// The controls: an identity control that redacts, and an eraser control that does not.
	var out bytes.Buffer
	if code := heldBackGate(&out, strings.NewReader(good), heldBackParts{identity: stub{}, eraser: eraser{}, real: stub{}}); code != HeldBackNoVouch {
		t.Errorf("an identity control that catches: exit %d\n%s", code, out.String())
	}
	out.Reset()
	if code := heldBackGate(&out, strings.NewReader(good), heldBackParts{identity: identity{}, eraser: identity{}, real: stub{}}); code != HeldBackNoVouch {
		t.Errorf("an eraser control that erases nothing: exit %d\n%s", code, out.String())
	}
}

// TestTheWindowOracle: a secret whose 6-character piece survives is a leak; a piece that ALSO
// occurs in the text around the secret is not counted against the redactor.
func TestTheWindowOracle(t *testing.T) {
	c := HeldBackCase{Kind: "leak", Text: "user=alphaq password=alphaqz9k2", Secrets: []string{"alphaqz9k2"}}
	if leaked(c, "user=alphaq password=[redacted]") {
		t.Error("a window that also occurs OUTSIDE the secret was counted as a leak")
	}
	if !leaked(c, "user=alphaq password=[redacted]z9k2xx alphaqz") {
		t.Error("a surviving 6-character window of the secret was not counted as a leak")
	}
	rec := HeldBackCase{Kind: "leak", Form: "record", Text: `{"m":"tok=` + `q9\u002dzzzzzzzz"}`, Secrets: []string{"q9-zzzzzzzz"}}
	if !leaked(rec, rec.Text) {
		t.Error("an escaped secret in a record is invisible to the oracle — it must search decoded strings")
	}
}

// TestTheGateRunsTheRealRedactor: the shipped entry point over a fixer-written file of the shapes
// this round targets. An INSTRUMENT test (see the file doc), and a smoke test of the real path.
func TestTheGateRunsTheRealRedactor(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 20; i++ {
		v := fmt.Sprintf("q%02dzk%dmvx7w", i, i%7)
		texts := []string{"12:DB_PASSWORD=%s", "< API_TOKEN=%s", "Environment=SECRET_KEY=%s", "mysql -uroot -p%s db",
			"CREATE USER app IDENTIFIED BY '%s';"}
		b.WriteString(caseLine(t, HeldBackCase{Kind: "leak", Label: fmt.Sprintf("leak-%d", i),
			Text: fmt.Sprintf(texts[i%len(texts)], v), Secrets: []string{v}}))
		b.WriteString(caseLine(t, HeldBackCase{Kind: "clean", Label: fmt.Sprintf("clean-%d", i),
			Text: fmt.Sprintf("commit %040d touches the token refresh path, line %d", i, i)}))
	}
	var out bytes.Buffer
	code := HeldBackGate(&out, strings.NewReader(b.String()))
	if code != HeldBackPass || !strings.Contains(out.String(), "leaks caught=20/20 clean damaged=0/20\n") {
		t.Fatalf("exit %d:\n%s", code, out.String())
	}
}
