package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestAnOptInThatAdmitsNothingIsAWarningNotARefusal: a worker that is `https` (or loopback) by now,
// with `-allow-plaintext-host` still in the unit file, must not stop capture — the leftover opt-in is
// ONE warning line and the pass runs. So is an opt-in for an instance with no `-worker`. A value
// that is not a bare host stays a usage error (a typo, not a leftover). Synthetic hosts only.
func TestAnOptInThatAdmitsNothingIsAWarningNotARefusal(t *testing.T) {
	p := newRunPod(t, true)
	_, tokenFile, env := runWorld(t, runToken)
	var errb bytes.Buffer
	code := run([]string{"--run", "-host", "host-a", "-worker", p.url, "-capture-token-file", tokenFile,
		"-allow-plaintext-host", "127.0.0.1", "-allow-plaintext-host", "client=mesh-host.example.invalid"}, &bytes.Buffer{}, &errb, env)
	if code != exitOK {
		t.Fatalf("a leftover opt-in stopped the pass: exit %d\n%s", code, errb.String())
	}
	for _, want := range []string{
		"WARNING -allow-plaintext-host is set for instance personal, whose -worker does not need it",
		"WARNING -allow-plaintext-host names instance client, which has no -worker client=<url>",
	} {
		if strings.Count(errb.String(), want) != 1 {
			t.Errorf("stderr does not carry, once: %s\n%s", want, errb.String())
		}
	}
	if strings.Contains(errb.String(), "PLAIN HTTP") {
		t.Errorf("an opt-in that admitted nothing was reported as used:\n%s", errb.String())
	}
	if !strings.Contains(errb.String(), " shipped=") {
		t.Errorf("the pass did not run:\n%s", errb.String())
	}
	errb.Reset()
	if code := run([]string{"--run", "-host", "host-a", "-worker", p.url, "-capture-token-file", tokenFile,
		"-allow-plaintext-host", "http://127.0.0.1"}, &bytes.Buffer{}, &errb, env); code != exitUsage {
		t.Fatalf("a malformed opt-in: exit %d, want %d (%s)", code, exitUsage, errb.String())
	}
}
