package main

// The JWKS FILE source, measured through the real `main`.
//
// 🔴 THE LIBRARY TESTS PROVE `KeySet` READS A FILE; THESE PROVE THE PROGRAM STARTS ON ONE,
// REFUSES A BAD ONE AT 78, AND RE-READS IT ON SIGHUP. Different claims: `main` is where a
// startup read becomes an exit code, and where a signal becomes a re-read. The issuer named
// below is under `example.invalid` (RFC 2606) and resolves nowhere, so a child that came up
// did so WITHOUT the issuer being reachable — which is the property the file source exists
// for, observed rather than argued.
//
// ⚠ THE VARIABLES ARE LITERALS AND NO NEW SYMBOL IS REFERENCED, so this file compiles against
// the tree before the file source existed, and is what shows that tree RED: there an issuer
// with only `CAIRN_OIDC_JWKS_FILE` is `ErrSupabaseNoKeys` at 78, so no arm below that expects
// a running child can pass, and the refusal arms fail on the path their output must name.

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	jwksFileEnv  = "CAIRN_OIDC_JWKS_FILE"
	jwksURLEnv   = "CAIRN_OIDC_JWKS_URL"
	oidcIssuer   = "CAIRN_OIDC_ISSUER"
	unreachableI = "https://idp.example.invalid/realms/notes"
)

// ecJWK mints a P-256 public JWK. Generated at test time, never pasted.
func ecJWK(t *testing.T, kid string) map[string]any {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	x, y := make([]byte, 32), make([]byte, 32)
	key.X.FillBytes(x)
	key.Y.FillBytes(y)
	return map[string]any{
		"kty": "EC", "kid": kid, "use": "sig", "alg": "ES256", "crv": "P-256",
		"x": base64.RawURLEncoding.EncodeToString(x), "y": base64.RawURLEncoding.EncodeToString(y),
	}
}

func jwksDoc(t *testing.T, keys ...map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"keys": keys})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// oidcChild builds a child `main` armed with an OIDC verifier over the given extra env.
// A control journal is configured because an armed verifier without one is its own 78.
func oidcChild(t *testing.T, extra ...string) (*exec.Cmd, *lockedBuffer) {
	t.Helper()
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte(testToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(self,
		"-store", t.TempDir(),
		"-host", "127.0.0.1",
		"-port", strconv.Itoa(freePort(t)),
		"-token-file", tokenFile)
	child.Env = append([]string{
		runServerEnv + "=1",
		testRefreshEnv + "=" + testRefreshPeriod.String(),
		"SUBSYSTEM_STORE_TRUSTED_PROXIES=192.0.2.0/24",
		EnvControlJournal + "=" + filepath.Join(t.TempDir(), "control.journal"),
		oidcIssuer + "=" + unreachableI,
	}, extra...)
	log := &lockedBuffer{}
	child.Stdout, child.Stderr = log, log
	return child, log
}

// runToExit runs a child that is expected to REFUSE, under a deadline — the regression's
// shape is "the child serves forever", which must fail rather than hang.
func runToExit(t *testing.T, extra ...string) (int, string) {
	t.Helper()
	child, log := oidcChild(t, extra...)
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- child.Wait() }()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		_ = child.Process.Kill()
		<-done
		t.Fatalf("the child did not exit within 20s — it CAME UP:\n%s", log.String())
	}
	return child.ProcessState.ExitCode(), log.String()
}

// startToListening runs a child that is expected to COME UP, and returns it serving.
func startToListening(t *testing.T, extra ...string) (*exec.Cmd, *lockedBuffer) {
	t.Helper()
	child, log := oidcChild(t, extra...)
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() { _, _ = child.Process.Wait(); close(exited) }()
	t.Cleanup(func() { _ = child.Process.Kill(); <-exited })
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(log.String(), "listening on") {
			return child, log
		}
		select {
		case <-exited:
			t.Fatalf("the child exited instead of coming up:\n%s", log.String())
		default:
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the child never reached `listening on`:\n%s", log.String())
	return nil, nil
}

// TestTheBinaryStartsOnAJWKSFileWithTheIssuerUnreachable is the claim the source exists for.
func TestTheBinaryStartsOnAJWKSFileWithTheIssuerUnreachable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jwks.json")
	if err := os.WriteFile(path, jwksDoc(t, ecJWK(t, "ec-1")), 0o600); err != nil {
		t.Fatal(err)
	}
	startToListening(t, jwksFileEnv+"="+path)
}

// TestTheBinaryREFUSESABadJWKSFileAtStartup — each arm exits 78 AND names what to fix, and a
// running child of the same harness is the positive control (the test above), so "exited 78"
// is not a fact about a harness that refuses everything.
func TestTheBinaryREFUSESABadJWKSFileAtStartup(t *testing.T) {
	dir := t.TempDir()
	malformed := filepath.Join(dir, "malformed.json")
	if err := os.WriteFile(malformed, []byte(`{"keys": [`), 0o600); err != nil {
		t.Fatal(err)
	}
	noKey := filepath.Join(dir, "no-usable-key.json")
	if err := os.WriteFile(noKey, jwksDoc(t, map[string]any{"kty": "oct", "kid": "k", "k": "c2VjcmV0"}), 0o600); err != nil {
		t.Fatal(err)
	}
	good := filepath.Join(dir, "good.json")
	if err := os.WriteFile(good, jwksDoc(t, ecJWK(t, "ec-1")), 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "absent.json")

	for _, arm := range []struct {
		name string
		env  []string
		want []string
	}{
		{"malformed", []string{jwksFileEnv + "=" + malformed}, []string{malformed, "jwks is not JSON", "Refusing to start"}},
		{"missing", []string{jwksFileEnv + "=" + missing}, []string{missing, "no such file or directory", "Refusing to start"}},
		{"no usable key", []string{jwksFileEnv + "=" + noKey}, []string{noKey, "the JWKS document carries no usable key", "Refusing to start"}},
		{"relative", []string{jwksFileEnv + "=conf/jwks.json"}, []string{`"conf/jwks.json" is relative, so which file it names depends on the working directory`}},
		{"beside a URL", []string{jwksFileEnv + "=" + good, jwksURLEnv + "=https://idp.example.invalid/jwks"},
			[]string{"one key set, one source"}},
	} {
		// ⚠ EVERY FRAGMENT IS A PHRASE FROM THE REFUSAL ITSELF, NEVER A BARE WORD. The child's
		// output carries `t.TempDir()` paths, and those embed the SUBTEST'S NAME — so a bare
		// "relative" was satisfied by the temp path of the arm named "relative" while the
		// relative-path refusal was deleted (measured: that mutant passed this arm).
		t.Run(arm.name, func(t *testing.T) {
			code, out := runToExit(t, arm.env...)
			if code != 78 {
				t.Fatalf("a %s JWKS file must exit 78 (EX_CONFIG), got %d\n%s", arm.name, code, out)
			}
			for _, want := range arm.want {
				if !strings.Contains(out, want) {
					t.Fatalf("the refusal must contain %q, got:\n%s", want, out)
				}
			}
		})
	}
}

// TestSIGHUPReReadsAJWKSFileAndARefusalKeepsTheLastGoodSet — both verdicts of the re-read,
// observed on the program's own output after a real signal.
func TestSIGHUPReReadsAJWKSFileAndARefusalKeepsTheLastGoodSet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jwks.json")
	if err := os.WriteFile(path, jwksDoc(t, ecJWK(t, "ec-1")), 0o600); err != nil {
		t.Fatal(err)
	}
	child, log := startToListening(t, jwksFileEnv+"="+path)

	waitFor := func(want string) string {
		t.Helper()
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			for _, line := range strings.Split(log.String(), "\n") {
				if strings.Contains(line, want) {
					return line
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("no line containing %q:\n%s", want, log.String())
		return ""
	}
	replace := func(body []byte) {
		t.Helper()
		if err := os.WriteFile(path+".next", body, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(path+".next", path); err != nil {
			t.Fatal(err)
		}
	}

	// A broken file: the re-read is refused and the one good key keeps serving.
	replace([]byte("not a key set"))
	if err := child.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	line := waitFor("JWKS file re-read on SIGHUP refused")
	if !strings.Contains(line, "last-known-good 1 key(s)") {
		t.Fatalf("a refused re-read must say what is still serving, got %q", line)
	}

	// A rotated file with two keys: the re-read lands and says so, with the count that MOVED.
	replace(jwksDoc(t, ecJWK(t, "ec-2"), ecJWK(t, "ec-3")))
	if err := child.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	if line := waitFor("JWKS file re-read on SIGHUP: serving"); !strings.Contains(line, "serving 2 key(s) (was 1)") {
		t.Fatalf("the accepted re-read must report the new count against the old, got %q", line)
	}

	// The token reload on the SAME signal still ran — the key re-read is beside it, not
	// instead of it.
	if !strings.Contains(log.String(), reloadLoaded) {
		t.Fatalf("the token-file reload must still answer SIGHUP:\n%s", log.String())
	}
}
