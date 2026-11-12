package client

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/transcriptwire"
)

// The bound a `transcript` read is made with. A skeleton reads every stored stream once, so a very
// large session takes longer than the store default (`DefaultTimeout`) to answer; the verb therefore
// takes `--timeout` after the verb like `memo-check` and is given at least `TranscriptTimeoutFloor`.

// recordTranscriptTimeouts swaps the verb's client constructor for one that records each bound the
// VERB passed and then builds the real client, so the request still happens.
func recordTranscriptTimeouts(t *testing.T) *[]int {
	t.Helper()
	var got []int
	real := transcriptHTTPClient
	transcriptHTTPClient = func(seconds int) *http.Client {
		got = append(got, seconds)
		return real(seconds)
	}
	t.Cleanup(func() { transcriptHTTPClient = real })
	return &got
}

// TestTheTranscriptVerbAcceptsTimeoutAfterTheVerb — `transcript … --timeout N` is served, and the help
// lists it. RED before: the verb refused it ("transcript does not accept \"--timeout\"", exit 2), which
// left the store default as the only bound a caller could not raise in the verb's own flags.
func TestTheTranscriptVerbAcceptsTimeoutAfterTheVerb(t *testing.T) {
	stub, _ := transcriptHost(t)
	want := stub.direct(t, transcriptwire.RouteSkeleton, url.Values{transcriptwire.ParamSession: {transcriptStubSession}})
	code, out, stderr := runCLI(t, "transcript", "skeleton", "--session", transcriptStubSession, "--timeout", "377")
	if code != ExitOK || out != want || stderr != "" {
		t.Errorf("transcript skeleton --timeout 377: exit %d, stdout matches %v, stderr %q", code, out == want, stderr)
	}
	code, out, stderr = runCLI(t, "transcript", "tool", "--session", transcriptStubSession, "--seq", "2", "--timeout", "377")
	if code != ExitOK || stderr != "" || !strings.Contains(out, "toolu_c1") {
		t.Errorf("transcript tool --timeout 377: exit %d, stderr %q", code, stderr)
	}
	if help := Usage("transcript"); !strings.Contains(help, "\n  --timeout ") {
		t.Errorf("transcript's help does not list --timeout:\n%s", help)
	}
}

// TestATranscriptReadIsBoundedByTheFloorOrALargerTimeout — the bound the verb hands the client: the
// floor (90 s) when nothing larger was asked for, the asked value when it is larger, in either flag
// position; a non-positive value is refused with no request rather than raised to the floor. Every
// asked value is distinct from the floor and from `DefaultTimeout`, so a literal hardcoded where the
// bound is computed cannot pass.
func TestATranscriptReadIsBoundedByTheFloorOrALargerTimeout(t *testing.T) {
	if TranscriptTimeoutFloor != 90 {
		t.Fatalf("TranscriptTimeoutFloor = %d, want 90", TranscriptTimeoutFloor)
	}
	stub, _ := transcriptHost(t)
	skeleton := []string{"transcript", "skeleton", "--session", transcriptStubSession}
	for name, tc := range map[string]struct {
		argv []string
		want int
	}{
		"no --timeout (the store default, 20)": {skeleton, 90},
		"a smaller --timeout after the verb":   {append(append([]string{}, skeleton...), "--timeout", "7"), 90},
		"a larger --timeout after the verb":    {append(append([]string{}, skeleton...), "--timeout", "377"), 377},
		"a larger global --timeout":            {append([]string{"--timeout", "401"}, skeleton...), 401},
		"a smaller global --timeout":           {append([]string{"--timeout", "3"}, skeleton...), 90},
	} {
		got := recordTranscriptTimeouts(t)
		code, _, stderr := runCLI(t, tc.argv...)
		if code != ExitOK || len(*got) != 1 || (*got)[0] != tc.want {
			t.Errorf("%s: exit %d, bounds passed %v, want [%d]; stderr %q", name, code, *got, tc.want, stderr)
		}
	}
	for _, argv := range [][]string{
		append(append([]string{}, skeleton...), "--timeout", "0"),
		append([]string{"--timeout", "-5"}, skeleton...),
	} {
		got := recordTranscriptTimeouts(t)
		before := stub.count()
		code, _, stderr := runCLI(t, argv...)
		if code != ExitUsage || len(*got) != 0 || stub.count() != before || !strings.Contains(stderr, "not positive") {
			t.Errorf("%v: exit %d (want %d), bounds passed %v, requests %d, stderr %q",
				argv, code, ExitUsage, *got, stub.count()-before, stderr)
		}
	}
}

// TestATranscriptReadOutlivesASmallerTimeout — the floor reaches the REAL client, not only the
// recorder: a listener that answers after 1.5 s is served under `--timeout 1`. RED before: the read
// was made with the caller's 1 s and failed on the deadline.
func TestATranscriptReadOutlivesASmallerTimeout(t *testing.T) {
	stub := newTranscriptStub(t)
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(1500 * time.Millisecond)
		stub.srv.Config.Handler.ServeHTTP(w, r)
	}))
	t.Cleanup(slow.Close)
	h := newMemoHost(t)
	h.configure(t, "", uiLines(slow.URL)...)
	want := stub.direct(t, transcriptwire.RouteSkeleton, url.Values{transcriptwire.ParamSession: {transcriptStubSession}})
	code, out, stderr := runCLI(t, "--timeout", "1", "transcript", "skeleton", "--session", transcriptStubSession)
	if code != ExitOK || out != want {
		t.Errorf("a 1.5 s answer under --timeout 1: exit %d, stdout matches %v, stderr %q", code, out == want, stderr)
	}
}
