package identity

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ZacxDev/cairn/internal/control"
)

// testProxySecret is the shared secret every arm below uses. Synthetic, generated
// nowhere near a real deployment, and over `MinProxySecretBytes` so the weak-secret rung
// is reachable only by a case that deliberately shortens it.
var testProxySecret = []byte("synthetic-proxy-secret-for-tests-0123456789")

const testSubjectHeader = "X-Forwarded-User"

// goodProxyConfig is a configuration every construction rung ACCEPTS. Each refusal test
// starts from this and breaks exactly one thing — which is what makes the rung it
// reaches the one it names, rather than whichever guard happens to fire first.
func goodProxyConfig(t *testing.T) TrustedHeaderConfig {
	t.Helper()
	return TrustedHeaderConfig{
		ProxyFronted:  true,
		SubjectHeader: testSubjectHeader,
		Secret:        testProxySecret,
		Provider:      testProvider,
		Authority:     newTestAuthority(t),
	}
}

// TestEveryTrustedHeaderConstructionRefusalIsReachable is the deliverable this backend
// exists for.
//
// 🔴 A GUARD THAT DIES BECAUSE AN EARLIER CHECK REJECTED THE INPUT FIRST IS A GUARD THAT
// HAS NEVER RUN. So every arm starts from a configuration the constructor ACCEPTS
// (asserted first, as this table's positive control) and breaks exactly one field, and
// the assertion names the SPECIFIC sentinel rather than "an error happened".
func TestEveryTrustedHeaderConstructionRefusalIsReachable(t *testing.T) {
	// THE POSITIVE CONTROL. Without it every arm below could pass against a constructor
	// that refuses unconditionally — which is a backend that is not a foot-gun because
	// it is not anything.
	if _, err := NewTrustedHeader(goodProxyConfig(t)); err != nil {
		t.Fatalf("precondition: a fully-configured proxy-fronted deployment must build, got %v", err)
	}

	for _, arm := range []struct {
		name   string
		break_ func(*TrustedHeaderConfig)
		want   error
	}{
		{
			name:   "rung 0 — no authority",
			break_: func(c *TrustedHeaderConfig) { c.Authority = nil },
			want:   ErrNoAuthority,
		},
		{
			name: "rung 1 — the deployment was never DECLARED proxy-fronted",
			// Everything else is perfect. This is the accident the flag exists to stop:
			// a config fragment copied without the one sentence that asserts a fact
			// about the network.
			break_: func(c *TrustedHeaderConfig) { c.ProxyFronted = false },
			want:   ErrTrustedHeaderNotDeclared,
		},
		{
			name:   "rung 2 — no subject header to read an identity from",
			break_: func(c *TrustedHeaderConfig) { c.SubjectHeader = "   " },
			want:   ErrTrustedHeaderNoSubjectHeader,
		},
		{
			name:   "rung 3 — no provider namespace for the subject",
			break_: func(c *TrustedHeaderConfig) { c.Provider = "" },
			want:   ErrTrustedHeaderNoProvider,
		},
		{
			name: "rung 4 — NO SOURCE CHECK AT ALL",
			// The most dangerous configuration in this repository: proxy-fronted
			// declared, a header named, and nothing whatsoever proving the request came
			// from the proxy. Anybody who can open a socket sets the header.
			break_: func(c *TrustedHeaderConfig) { c.Secret = nil; c.RequireClientCert = false },
			want:   ErrTrustedHeaderNoSourceCheck,
		},
		{
			name: "rung 4 — a PEER ALLOWLIST is not a source check",
			// Reachable because rung 4 asks "is there a secret or a certificate", and a
			// peer allowlist is neither. It is a second layer, and accepting it alone
			// would be "safe because of where it happens to be deployed".
			break_: func(c *TrustedHeaderConfig) {
				c.Secret = nil
				c.RequireClientCert = false
				c.ProxyPeers = []string{"192.0.2.0/24"}
			},
			want: ErrTrustedHeaderNoSourceCheck,
		},
		{
			name: "rung 5 — the shared secret is below the floor",
			// Reachable only because rung 4 counts a present-but-short secret as "a
			// source check was configured". The two rungs ask different questions.
			break_: func(c *TrustedHeaderConfig) { c.Secret = []byte("short") },
			want:   ErrTrustedHeaderWeakSecret,
		},
		{
			name:   "rung 6 — a peer entry that is not an address",
			break_: func(c *TrustedHeaderConfig) { c.ProxyPeers = []string{"not-an-address"} },
			want:   ErrTrustedHeaderPeer,
		},
		{
			name: "rung 6 — a peer entry that trusts every peer",
			// `netid.TrustedNetwork`'s own refusal, inherited rather than re-derived.
			break_: func(c *TrustedHeaderConfig) { c.ProxyPeers = []string{"0.0.0.0/0"} },
			want:   ErrTrustedHeaderPeer,
		},
		{
			name:   "rung 6 — a peer entry wider than netid's floor",
			break_: func(c *TrustedHeaderConfig) { c.ProxyPeers = []string{"10.0.0.0/8"} },
			want:   ErrTrustedHeaderPeer,
		},
	} {
		t.Run(arm.name, func(t *testing.T) {
			cfg := goodProxyConfig(t)
			arm.break_(&cfg)
			got, err := NewTrustedHeader(cfg)
			if err == nil {
				t.Fatalf("the constructor ACCEPTED this configuration and returned %v — the refusal this arm names never fires", got)
			}
			if !errors.Is(err, arm.want) {
				t.Fatalf("the WRONG guard fired.\n  got:  %v\n  want: %v\nA guard that dies because an earlier check rejected the input first has never run.", err, arm.want)
			}
			t.Logf("refusal fired with its own message: %v", err)
		})
	}
}

// TestAClientCertificateAloneSatisfiesTheSourceCheck is the other half of rung 4: mTLS
// is an alternative to the shared secret, not an addition to it.
func TestAClientCertificateAloneSatisfiesTheSourceCheck(t *testing.T) {
	cfg := goodProxyConfig(t)
	cfg.Secret = nil
	cfg.RequireClientCert = true
	if _, err := NewTrustedHeader(cfg); err != nil {
		t.Fatalf("a verified client certificate is a source check on its own: %v", err)
	}
}

// TestAnAttackerReachingThePodDirectlyGetsNothing walks every REQUEST-TIME refusal.
//
// 🔴 THIS IS THE QUESTION THE WHOLE BACKEND IS ABOUT: with the trusted-header backend
// armed and configured correctly, what does somebody who can open a socket to the pod
// get by setting the identity header? The answer must be the same refusal a wrong token
// gets, on every path, and each rung must be reachable by a request no earlier rung
// rejects.
func TestAnAttackerReachingThePodDirectlyGetsNothing(t *testing.T) {
	cfg := goodProxyConfig(t)
	cfg.ProxyPeers = []string{"192.0.2.10/32"}
	backend, err := NewTrustedHeader(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// THE POSITIVE CONTROL: a request that IS from the proxy resolves to the real user.
	// Without it every refusal below is satisfied by a backend that refuses everything.
	legit := proxyRequest(t, map[string]string{
		testSubjectHeader:        testSubject,
		DefaultProxySecretHeader: string(testProxySecret),
	}, "192.0.2.10:44444")
	who, err := backend.Authenticate(legit)
	if err != nil {
		t.Fatalf("precondition: a correct proxied request must authenticate, got %v", err)
	}
	if who.Principal.ID != testUserID || who.Principal.Display != testEmail {
		t.Fatalf("precondition: expected the fixture user, got %+v", who.Principal)
	}
	if !who.Auth.Allows(control.DerivedID(control.PrefixScope, "quarry-notes"), control.VerbRead) {
		t.Fatal("precondition: the resolved principal has no authority, so every refusal below would be indistinguishable from this succeeding")
	}
	if who.Fingerprint != "" {
		t.Fatalf("a session carries no token fingerprint, got %q", who.Fingerprint)
	}

	for _, arm := range []struct {
		name    string
		headers map[string]string
		peer    string
		// reason is a distinctive fragment of the refusal this arm must produce. The
		// wire never carries it; this is the only place it is read.
		reason string
		// repeatSubject adds the subject header MORE THAN ONCE, which `headers` (a map,
		// set with `Header.Set`) cannot express.
		repeatSubject []string
		// repeatSecret does the same for the shared-secret header.
		repeatSecret []string
	}{
		{
			name: "the attacker sets the identity header and nothing else",
			// The headline case. No secret, so the source check refuses before the
			// claimed identity is read at all.
			headers: map[string]string{testSubjectHeader: testSubject},
			peer:    "192.0.2.10:44444",
			reason:  "the shared-secret header is absent or duplicated",
		},
		{
			name: "the proxy APPENDS the secret, so a caller can smuggle a second value",
			// A backend that took the first or the last value would match on whichever
			// copy happened to be right. `netid.ClientIP` makes the identical ruling for
			// the client-IP header, and for the identical reason.
			headers:      map[string]string{testSubjectHeader: testSubject},
			peer:         "192.0.2.10:44444",
			reason:       "the shared-secret header is absent or duplicated",
			repeatSecret: []string{"guess", string(testProxySecret)},
		},
		{
			name: "the attacker guesses the secret",
			headers: map[string]string{
				testSubjectHeader:        testSubject,
				DefaultProxySecretHeader: "synthetic-proxy-secret-for-tests-012345678X",
			},
			peer:   "192.0.2.10:44444",
			reason: "the shared secret does not match",
		},
		{
			name: "the attacker holds the secret but is not the allowlisted peer",
			headers: map[string]string{
				testSubjectHeader:        testSubject,
				DefaultProxySecretHeader: string(testProxySecret),
			},
			peer:   "198.51.100.7:44444",
			reason: "the peer is not an allowlisted proxy",
		},
		{
			name: "an unparseable peer",
			headers: map[string]string{
				testSubjectHeader:        testSubject,
				DefaultProxySecretHeader: string(testProxySecret),
			},
			peer:   "not-an-address",
			reason: "the peer address is unparseable",
		},
		{
			name: "the proxy APPENDS rather than overwrites, so the subject arrives twice",
			// Reachable only after the source check passes, which is why the secret is
			// correct here. A backend that took the first or the last value would let a
			// caller smuggle an identity past a proxy that appends.
			headers: map[string]string{
				DefaultProxySecretHeader: string(testProxySecret),
			},
			peer:          "192.0.2.10:44444",
			reason:        "the subject header is absent or duplicated",
			repeatSubject: []string{testSubject, testStranger},
		},
		{
			name: "a subject this control plane has never heard of",
			headers: map[string]string{
				testSubjectHeader:        testStranger,
				DefaultProxySecretHeader: string(testProxySecret),
			},
			peer:   "192.0.2.10:44444",
			reason: "holds no user for",
		},
		{
			name: "an empty subject",
			headers: map[string]string{
				testSubjectHeader:        "   ",
				DefaultProxySecretHeader: string(testProxySecret),
			},
			peer:   "192.0.2.10:44444",
			reason: "the subject header is empty",
		},
		{
			name: "a subject over the length cap",
			headers: map[string]string{
				testSubjectHeader:        strings.Repeat("a", maxSubjectBytes+1),
				DefaultProxySecretHeader: string(testProxySecret),
			},
			peer:   "192.0.2.10:44444",
			reason: "over the length cap",
		},
	} {
		t.Run(arm.name, func(t *testing.T) {
			r := proxyRequest(t, arm.headers, arm.peer)
			for _, extra := range arm.repeatSubject {
				r.Header.Add(testSubjectHeader, extra)
			}
			for _, extra := range arm.repeatSecret {
				r.Header.Add(DefaultProxySecretHeader, extra)
			}
			got, err := backend.Authenticate(r)
			if err == nil {
				t.Fatalf("AUTHENTICATION BYPASS: this request resolved to %v", got.Principal)
			}
			// 🔴 THE WIRE DOES NOT DISCRIMINATE. Whatever the reason, the error a
			// serving path can classify is the one uniform refusal.
			if !errors.Is(err, control.ErrNoCredential{}) {
				t.Fatalf("a refusal here must be classifiable as the uniform no-credential error, got %v", err)
			}
			// 🔴 AND THE REASON IS WATCHED FIRING. A refusal that is present but
			// unreachable is what this assertion exists to distinguish.
			if !strings.Contains(err.Error(), arm.reason) {
				t.Fatalf("the WRONG rung fired.\n  got:  %v\n  want a refusal mentioning: %q", err, arm.reason)
			}
			t.Logf("refusal fired with its own message: %v", err)
		})
	}
}

// TestARequiredClientCertificateIsCheckedAgainstTheVERIFIEDChain.
//
// 🔴 `PeerCertificates` IS NOT `VerifiedChains`, AND READING THE WRONG ONE IS THE WHOLE
// DEFECT. A server that merely REQUESTS a client certificate populates the first and
// leaves the second empty — so a backend reading `PeerCertificates` would accept a
// self-signed certificate nobody checked, which is not a source check at all.
func TestARequiredClientCertificateIsCheckedAgainstTheVerifiedChain(t *testing.T) {
	cfg := goodProxyConfig(t)
	cfg.Secret = nil
	cfg.RequireClientCert = true
	backend, err := NewTrustedHeader(cfg)
	if err != nil {
		t.Fatal(err)
	}
	headers := map[string]string{testSubjectHeader: testSubject}

	// No TLS at all.
	if _, err := backend.Authenticate(proxyRequest(t, headers, "192.0.2.10:1")); err == nil {
		t.Fatal("a plaintext request satisfied a required client certificate")
	}

	// TLS, with a certificate PRESENTED but not VERIFIED — the dangerous middle state.
	presented := proxyRequest(t, headers, "192.0.2.10:1")
	presented.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{{}}}
	if _, err := backend.Authenticate(presented); err == nil {
		t.Fatal("a PRESENTED but unverified client certificate satisfied the source check — `PeerCertificates` is populated by a server that merely requests one, so this is a self-signed certificate nobody checked")
	} else if !strings.Contains(err.Error(), "no verified TLS client certificate") {
		t.Fatalf("refused for the wrong reason: %v", err)
	}

	// TLS with a VERIFIED chain: accepted.
	verified := proxyRequest(t, headers, "192.0.2.10:1")
	verified.TLS = &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{{}}}}
	if _, err := backend.Authenticate(verified); err != nil {
		t.Fatalf("a verified client certificate must satisfy the source check: %v", err)
	}
}

// TestTheSourceCheckRunsBEFOREAnythingTheCallerClaimsAboutWho.
//
// 🔴 THE ORDER IS THE SECURITY PROPERTY. Reading the subject first and checking the
// secret afterwards accepts the same set of requests and is wrong about everything else:
// a caller could probe which subjects this control plane holds by watching how the
// refusals differ. Measured as a RELATIONSHIP rather than by reading the code — a
// request with a KNOWN subject and a request with an UNKNOWN one, both without the
// secret, must produce the SAME refusal.
func TestTheSourceCheckRunsBeforeAnythingTheCallerClaimsAboutWho(t *testing.T) {
	backend, err := NewTrustedHeader(goodProxyConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	known := proxyRequest(t, map[string]string{testSubjectHeader: testSubject}, "192.0.2.10:1")
	unknown := proxyRequest(t, map[string]string{testSubjectHeader: testStranger}, "192.0.2.10:1")

	_, knownErr := backend.Authenticate(known)
	_, unknownErr := backend.Authenticate(unknown)
	if knownErr == nil || unknownErr == nil {
		t.Fatal("a request with no shared secret authenticated")
	}
	if knownErr.Error() != unknownErr.Error() {
		t.Fatalf("a caller with no source credential can tell a KNOWN subject from an UNKNOWN one:\n  known:   %v\n  unknown: %v\nThat is an enumeration API for this control plane's user list.",
			knownErr, unknownErr)
	}
}

// proxyRequest builds a request as it would arrive at the pod.
func proxyRequest(t *testing.T, headers map[string]string, remoteAddr string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/recall/quarry-notes", nil)
	r.RemoteAddr = remoteAddr
	for name, value := range headers {
		r.Header.Set(name, value)
	}
	return r
}
