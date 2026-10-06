package client

import (
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/doctor"
)

// 🔴 THE CLIENT HALF OF THE ARC ORPHAN CHECK (S5): `cairn arcs --check`. The report's content is
// pinned by `internal/report/arcscheck_test.go`; this file pins what only the CLIENT decides —
// that the body is the pod's VERBATIM, that the exit is doctor's 0/9/10 recomputed from the
// status, and that every way of NOT getting a check answer is 10, never 0.

func registerGadget(t *testing.T, home string) {
	t.Helper()
	payload := filepath.Join(home, "arc.json")
	if err := os.WriteFile(payload, []byte(`{"schema":1,"status":"open","members":[`+
		`{"session":"s-0007","role":"originated","first_seen":""}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out, errOut := runCLI(t, "arc-register", "--scope", "alpha-notes", "--slug", "gadget-rollout", "--from", payload); code != ExitOK {
		t.Fatalf("arc-register: %d\n%s\n%s", code, out, errOut)
	}
}

// TestArcsCheckRoundTripsThroughTheRealPodOnDoctorsCodes: clean is 0 with the pod's body verbatim;
// a corrupt journal line (appended by hand, as a crash or a bad writer would leave it) is 9.
func TestArcsCheckRoundTripsThroughTheRealPodOnDoctorsCodes(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "journal.jsonl")
	tsrv := arcPod(t, journal)
	home := arcClientHost(t, tsrv.URL)
	registerGadget(t, home)

	code, out, errOut := runCLI(t, "arcs", "--check", "--scope", "alpha-notes")
	want, _ := podBody(t, tsrv.URL, "/api/v1/arcs/alpha-notes?check=1")
	if code != 0 || out != want || !strings.Contains(out, "status=arcs-check-clean") || errOut != "" {
		t.Fatalf("a clean check: %d\n%s\n--- pod\n%s\n--- stderr\n%s", code, out, want, errOut)
	}
	code, out, _ = runCLI(t, "arcs", "--check", "--all-scopes", "--scope", "alpha-notes")
	want, _ = podBody(t, tsrv.URL, "/api/v1/arcs/alpha-notes?check=1&all_scopes=1")
	if code != 0 || out != want || !strings.Contains(out, "  checked: every arc visible to you\n") {
		t.Fatalf("--all-scopes asks all_scopes=1: %d\n%s\n--- pod\n%s", code, out, want)
	}

	f, err := os.OpenFile(journal, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("{not a record}\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	code, out, _ = runCLI(t, "arcs", "--check", "--scope", "alpha-notes")
	if code != 9 || !strings.Contains(out, "status=arcs-check-findings") || !strings.Contains(out, "\n- journal-damaged · ") {
		t.Fatalf("a damaged journal is a finding at 9: %d\n%s", code, out)
	}
}

// TestEveryWayOfNotGettingACheckAnswerIs10: an off-state pod, a pod that is not there, and — the
// version-skew hazard — a pod that ignores `?check=1` and answers a LISTING with `X-Store-Exit: 0`.
// Each must be 10 ("could not look"); the third is the one a client that trusted the header would
// have printed as a passed check.
func TestEveryWayOfNotGettingACheckAnswerIs10(t *testing.T) {
	off := arcPod(t, "")
	arcClientHost(t, off.URL)
	code, out, _ := runCLI(t, "arcs", "--check", "--scope", "alpha-notes")
	if code != 10 || !strings.Contains(out, "status=registrations-unconfigured") {
		t.Fatalf("an off-state pod: 10, got %d\n%s", code, out)
	}

	off.Close()
	code, _, errOut := runCLI(t, "arcs", "--check", "--scope", "alpha-notes")
	if code != 10 || !strings.Contains(errOut, "arcs --check could NOT look") {
		t.Fatalf("an unreachable pod: 10, got %d\n%s", code, errOut)
	}

	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Store-Status", "arcs-listed")
		w.Header().Set("X-Store-Exit", "0")
		w.Write([]byte("cairn-arcs: status=arcs-listed scope=alpha-notes\n"))
	}))
	t.Cleanup(old.Close)
	arcClientHost(t, old.URL)
	code, out, errOut = runCLI(t, "arcs", "--check", "--scope", "alpha-notes")
	if code != 10 || !strings.Contains(errOut, "the pod answered status 'arcs-listed', which is not a check answer") {
		t.Fatalf("a pod that ignored ?check=1: 10, got %d\n%s\n--- stderr\n%s", code, out, errOut)
	}
	// The CONTROL for that case: the same fake pod answering a check status IS believed.
	agree := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("check") != "1" {
			w.WriteHeader(400)
			return
		}
		w.Header().Set("X-Store-Status", "arcs-check-findings")
		w.Header().Set("X-Store-Exit", "0") // deliberately wrong: the STATUS decides
		w.Write([]byte("cairn-arcs-check: status=arcs-check-findings\n"))
	}))
	t.Cleanup(agree.Close)
	arcClientHost(t, agree.URL)
	if code, _, _ := runCLI(t, "arcs", "--check", "--scope", "alpha-notes"); code != 9 {
		t.Fatalf("CONTROL: a check status is believed, and decides the exit over X-Store-Exit — want 9, got %d", code)
	}
}

func TestArcsCheckFlags(t *testing.T) {
	for _, argv := range [][]string{
		{"arcs", "--check"},
		{"arcs", "--check", "--all-scopes", "--repo", "."},
	} {
		if _, opts, err := Parse(argv); err != nil || !opts.Check {
			t.Fatalf("%v must parse with Check set: %v", argv, err)
		}
	}
	if _, _, err := Parse([]string{"arcs", "--check=yes"}); err == nil {
		t.Fatal("--check takes no value")
	}
	arcClientHost(t, "http://192.0.2.1:9")
	if code, _, errOut := runCLI(t, "arcs", "--all-scopes", "--scope", "alpha-notes"); code != ExitUsage ||
		!strings.Contains(errOut, "--all-scopes only widens --check") {
		t.Fatalf("--all-scopes on a listing is usage, before the network: %d\n%s", code, errOut)
	}
}

// TestTheArcsCheckAddedNoExitCode is Q5's constraint ASSERTED, not a ledger moved: both code tables
// `cairn -exit-codes` prints, spelled LITERALLY. A constant added for the check — or anywhere — turns
// this red, beside the two cross-client ledgers in `tests/test_go_client_ledgers.py` and
// `tests/test_cairn_doctor.py` that it must also keep green.
func TestTheArcsCheckAddedNoExitCode(t *testing.T) {
	wantClient := map[string]int{
		"EXIT_OK": 0, "EXIT_USAGE": 2, "EXIT_UNREACHABLE_NO_CACHE": 3, "EXIT_REFRESH_FAILED": 4,
		"EXIT_CORRUPT": 5, "EXIT_WRITE_REFUSED": 6, "EXIT_WRITE_UNREACHABLE": 7,
		"EXIT_WRITE_PRECONDITION": 8, "EXIT_WRITE_EXISTS": 9, "EXIT_UNROUTED": 11,
	}
	wantDoctor := map[string]int{"EXIT_DOCTOR_OK": 0, "EXIT_DOCTOR_PROBLEM": 9, "EXIT_DOCTOR_UNMEASURED": 10}
	if got := ExitCodes(); !maps.Equal(got, wantClient) {
		t.Fatalf("the client's exit-code table moved:\n got %v\nwant %v", got, wantClient)
	}
	if got := doctor.ExitCodes(); !maps.Equal(got, wantDoctor) {
		t.Fatalf("doctor's exit-code table moved:\n got %v\nwant %v", got, wantDoctor)
	}
}
