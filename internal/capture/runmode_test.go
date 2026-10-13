package capture

import (
	"errors"
	"path/filepath"
	"testing"
)

// TestTheRedactionOffBuildSendsOnlyToLoopback: the positive-control build refuses every URL that
// is not loopback, so it cannot be pointed at a real instance.
func TestTheRedactionOffBuildSendsOnlyToLoopback(t *testing.T) {
	if _, err := ValidateEndpoint("https://worker.example.invalid"); err != nil {
		t.Fatalf("control: a release build refuses an https URL: %v", err)
	}
	redactionOff = true
	defer func() { redactionOff = false }()
	if _, err := ValidateEndpoint("https://worker.example.invalid"); err == nil {
		t.Fatal("the redaction-off build accepted a non-loopback URL")
	}
	if _, err := ValidateEndpoint("http://127.0.0.1:9"); err != nil {
		t.Fatalf("the redaction-off build refused loopback: %v", err)
	}
}

// TestASecondRunIsLockedOut: two overlapping runs never share the state file.
func TestASecondRunIsLockedOut(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	release, err := LockState(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LockState(path); !errors.Is(err, ErrRunning) {
		t.Fatalf("a second lock while the first is held: %v", err)
	}
	release()
	again, err := LockState(path)
	if err != nil {
		t.Fatalf("the lock was not released: %v", err)
	}
	again()
}

// TestPlainHTTPToANamedHostIsAnExplicitExactOptIn: plain `http` to a non-loopback host is refused
// unless the operator named that host EXACTLY (`-allow-plaintext-host`) — a different host, a
// suffix, a prefix, another case and a host:port are each still refused — and the opt-in changes
// nothing else: another scheme, credentials and a query are refused as before, `https` and a
// loopback address pass without reporting the opt-in as used. Synthetic hosts only.
func TestPlainHTTPToANamedHostIsAnExplicitExactOptIn(t *testing.T) {
	const worker = "http://mesh-host.example.invalid:8443"
	if _, _, err := ValidateEndpointAllowing(worker); err == nil {
		t.Fatal("plain http to a non-loopback host was accepted with no opt-in")
	}
	got, plaintext, err := ValidateEndpointAllowing(worker+"/", "other.example.invalid", "mesh-host.example.invalid")
	if err != nil || !plaintext || got != worker {
		t.Fatalf("the exact host, opted in: %q plaintext=%v err=%v — want %q, reported as plaintext", got, plaintext, err, worker)
	}
	for name, allowed := range map[string]string{
		"a different host":      "other.example.invalid",
		"a suffix of the host":  "example.invalid",
		"the host as a suffix":  "evil-mesh-host.example.invalid",
		"a prefix of the host":  "mesh-host",
		"the host as a prefix":  "mesh-host.example.invalid.evil.example.invalid",
		"another case":          "Mesh-Host.example.invalid",
		"the host with a port":  "mesh-host.example.invalid:8443",
		"an empty opt-in":       "",
		"a wildcard":            "*.example.invalid",
		"the whole URL":         worker,
		"the host, with spaces": " mesh-host.example.invalid ",
	} {
		if _, _, err := ValidateEndpointAllowing(worker, allowed); err == nil {
			t.Errorf("%s (%q) admitted plain http to mesh-host.example.invalid", name, allowed)
		}
	}
	for _, raw := range []string{"ftp://mesh-host.example.invalid", "http://user:pw@mesh-host.example.invalid",
		"http://mesh-host.example.invalid/?q=1"} {
		if _, _, err := ValidateEndpointAllowing(raw, "mesh-host.example.invalid"); err == nil {
			t.Errorf("%s was accepted because its host is opted in", raw)
		}
	}
	for _, raw := range []string{"https://mesh-host.example.invalid", "http://127.0.0.1:9"} {
		if _, plaintext, err := ValidateEndpointAllowing(raw, "mesh-host.example.invalid", "127.0.0.1"); err != nil || plaintext {
			t.Errorf("%s: err=%v plaintext=%v — want accepted, and NOT by the opt-in", raw, err, plaintext)
		}
	}
	// The one-argument form is unchanged: no opt-in exists there.
	if _, err := ValidateEndpoint(worker); err == nil {
		t.Fatal("ValidateEndpoint accepted plain http to a non-loopback host")
	}
}
