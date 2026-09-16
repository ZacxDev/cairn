package identity

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strings"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/netid"
)

// TrustedHeaderBackend is this backend's name in a `Refusal`. Never rendered to a
// caller.
const TrustedHeaderBackend = "trusted-header"

// TrustedHeader reads the identity an upstream OAuth proxy has already established.
//
// 🔴 THIS IS THE MOST DANGEROUS THING IN P4 AND IT IS BUILT AS A FOOT-GUN THAT REFUSES
// TO FIRE. State the failure plainly, because everything below is shaped by it: **if the
// pod is reachable directly, anyone who can open a socket to it can set the identity
// header and become any user in this control plane.** Not "read one scope" — BE that
// user, at that user's full authority, on every route, with the writes attributed to
// them. There is no narrowing anywhere downstream that limits the blast radius, because
// downstream cannot tell this principal from one that presented a credential; that is
// the whole design, and here it is the hazard.
//
// So the backend is built so that the dangerous configuration cannot be reached by
// accident:
//
//  1. **It is never the default.** `api.New` builds a machine-token backend and nothing
//     else. This one exists only if something constructs it.
//  2. **It refuses to exist without an explicit "this deployment is proxy-fronted"
//     declaration.** Not a URL, not a header name — a boolean whose only purpose is to
//     be a sentence an operator had to write.
//  3. **It refuses to exist without a source check**: a shared secret, or a verified
//     client certificate. A peer allowlist alone does NOT satisfy this — see
//     `TrustedHeaderConfig.ProxyPeers`.
//  4. **Every check runs on every request** and every failure is the same
//     `control.ErrNoCredential` a wrong token gets, so a prober learns nothing about
//     which rung they hit.
//
// 🔴 AND THE HONEST LIMIT, STATED BECAUSE THE GUARDS ABOVE READ STRONGER THAN THEY ARE:
// A SHARED SECRET IN A HEADER PROVES THE SENDER KNOWS THE SECRET, NOT THAT THEY ARE THE
// PROXY. Anything that can read the proxy's configuration, or capture one plaintext
// request, can replay it. `netid`'s package doc makes the same point one layer down
// about the client-IP header: this proves the request came through something that holds
// the secret, and narrowing WHO can hold it is a NetworkPolicy's and a TLS
// configuration's job. `RequireClientCert` is the stronger rung and is the one to prefer
// where mTLS is available.
type TrustedHeader struct {
	subjectHeader string
	secretHeader  string
	secret        []byte
	requireCert   bool
	peers         []netip.Prefix
	provider      string
	authority     ModelSource
}

var _ Authenticator = (*TrustedHeader)(nil)

// TrustedHeaderConfig is what a deployment declares.
type TrustedHeaderConfig struct {
	// ProxyFronted is the explicit declaration that this deployment sits behind an
	// authenticating proxy and is not otherwise reachable.
	//
	// 🔴 IT HAS NO OTHER PURPOSE AND THAT IS DELIBERATE. It could have been inferred
	// from "a header name was configured", and inferring it is exactly the accident
	// this backend must not permit: somebody copying a config fragment, or setting one
	// environment variable to see what happens, must not end up with an authentication
	// bypass. A flag that does nothing but assert a fact about the network is a
	// sentence somebody had to write on purpose.
	ProxyFronted bool

	// SubjectHeader carries the provider's stable user id — `X-Forwarded-User` for
	// oauth2-proxy. Required.
	//
	// ⚠ THE SUBJECT, NOT THE EMAIL. `control.User`'s comment states the rule: an email
	// is mutable at the provider and reused across providers, so a deployment that
	// identified users by it would merge two people the day they shared an address.
	SubjectHeader string

	// ⚠ THERE IS DELIBERATELY NO `EmailHeader`, AND SAYING SO IS THE POINT RATHER THAN
	// LEAVING THE ABSENCE TO BE READ AS AN OVERSIGHT. The first draft of this type had
	// one, "for display only" — and it had no reader: `Principal.Display` comes from the
	// `control.User` row this control plane already holds, which is the only copy of a
	// user's email that a UI or an audit line should ever show. A proxy-supplied display
	// name would be caller-controlled text competing with the stored one, and a field
	// nothing reads is the shape this repository refuses elsewhere as exported API with
	// no consumer.

	// SecretHeader carries the shared secret. Empty means DefaultProxySecretHeader.
	SecretHeader string

	// Secret is the shared secret the proxy must present. Either this or
	// RequireClientCert is mandatory.
	Secret []byte

	// RequireClientCert demands a TLS client certificate that this server's own
	// `tls.Config` already verified. Either this or Secret is mandatory.
	//
	// ⚠ IT CHECKS `r.TLS.VerifiedChains`, WHICH IS A FACT THE TLS STACK ESTABLISHED
	// AND NOT ONE THIS CODE CAN ESTABLISH. A server configured with
	// `ClientAuth: tls.RequireAndVerifyClientCert` and a CA pool populates it; a server
	// that merely REQUESTS a certificate populates `PeerCertificates` and leaves the
	// chains empty. This reads the verified half only, so a certificate nobody checked
	// is not a source check.
	RequireClientCert bool

	// ProxyPeers narrows WHICH peer addresses may present the header at all.
	//
	// 🔴 IT IS A SECOND LAYER AND NOT A SOURCE CHECK, AND THE CONSTRUCTOR ENFORCES
	// THAT. The plan names "shared secret or mTLS" for a reason: a peer address proves
	// only that something occupying that address sent the request, and in a cluster the
	// set of things that can occupy an address is a NetworkPolicy question, not an
	// authentication one. Accepting it alone would be exactly the "harmless because of
	// where it happens to be deployed" reasoning `internal/api`'s path-component guard
	// refuses. Entries are parsed by `netid.TrustedNetwork`, so they inherit its
	// refusal of a default route and its per-family width floor rather than re-deriving
	// them.
	ProxyPeers []string

	// Provider is the `control.User.Provider` the subject is resolved against.
	// Required: a deployment fronted by a proxy must say which identity provider's
	// namespace the subject belongs to.
	Provider string

	// Authority is the materialized control plane. Required.
	Authority ModelSource
}

// DefaultProxySecretHeader is where the shared secret is read from when the config
// does not say.
const DefaultProxySecretHeader = "X-Cairn-Proxy-Secret"

// MinProxySecretBytes is the floor on the shared secret.
//
// 32 bytes: the output width of SHA-256, which is the digest it is compared through.
// Below that width the secret is the weakest part of the construction, and a shared
// secret that grants full impersonation of every user is not the place to accept a
// memorable one.
//
// ⚠ THIS USED TO SAY "for the same reason `MinHS256SecretBytes` is 32". That constant
// was the floor on the LEGACY symmetric JWT secret and went with it; the reasoning was
// never borrowed, so it is stated here rather than pointed at. This is now the only
// secret-length floor in the package.
const MinProxySecretBytes = 32

// The construction refusals. Six sentinels, one per rung, because a test that watches
// "the constructor returned an error" has not watched a guard fire — it has watched the
// ladder end. Each is reachable by a configuration every rung above it accepts.
var (
	// ErrTrustedHeaderNotDeclared is rung 1: nobody said this deployment is
	// proxy-fronted.
	ErrTrustedHeaderNotDeclared = errors.New("identity: the trusted-header backend refuses to start because this deployment has not been declared proxy-fronted. If the pod is reachable directly, anyone who can open a socket to it can set the identity header and BECOME any user here")
	// ErrTrustedHeaderNoSubjectHeader is rung 2: no header to read the subject from.
	ErrTrustedHeaderNoSubjectHeader = errors.New("identity: the trusted-header backend has no subject header configured, so it could read no identity at all")
	// ErrTrustedHeaderNoProvider is rung 3: no provider namespace for the subject.
	ErrTrustedHeaderNoProvider = errors.New("identity: the trusted-header backend has no identity provider configured, so a subject could not be resolved to a user")
	// ErrTrustedHeaderNoSourceCheck is rung 4: nothing proves the request came from
	// the proxy.
	ErrTrustedHeaderNoSourceCheck = errors.New("identity: the trusted-header backend refuses to start without a source check — configure a shared secret or require a verified client certificate. A peer allowlist is a second layer and is NOT a source check: it proves only that something occupying that address sent the request")
	// ErrTrustedHeaderWeakSecret is rung 5: the shared secret is below the floor.
	ErrTrustedHeaderWeakSecret = errors.New("identity: the trusted-header shared secret is below the length floor")
	// ErrTrustedHeaderPeer is rung 6: a peer allowlist entry does not parse, or is
	// wider than `netid`'s floor.
	ErrTrustedHeaderPeer = errors.New("identity: a trusted-header proxy peer entry is unusable")
)

// NewTrustedHeader builds the trusted-header backend, or refuses.
//
// 🔴 THE LADDER IS ORDERED SO EVERY RUNG IS REACHABLE, AND THAT ORDERING IS LOAD-BEARING
// RATHER THAN TIDY. A guard that only fires on input an earlier guard already rejects
// has never run, and this repository's rules name that shape explicitly. So: the
// declaration comes first (it is a fact about the deployment, independent of every other
// field); then the two fields with no possible default; then "is there any source check
// at all"; then "is the one you configured strong enough"; then the optional narrowing.
// `TestEveryTrustedHeaderConstructionRefusalIsReachable` walks all six with a
// configuration that satisfies every rung above.
func NewTrustedHeader(cfg TrustedHeaderConfig) (*TrustedHeader, error) {
	if cfg.Authority == nil {
		return nil, ErrNoAuthority
	}
	if !cfg.ProxyFronted {
		return nil, ErrTrustedHeaderNotDeclared
	}
	if strings.TrimSpace(cfg.SubjectHeader) == "" {
		return nil, ErrTrustedHeaderNoSubjectHeader
	}
	if strings.TrimSpace(cfg.Provider) == "" {
		return nil, ErrTrustedHeaderNoProvider
	}
	if len(cfg.Secret) == 0 && !cfg.RequireClientCert {
		return nil, ErrTrustedHeaderNoSourceCheck
	}
	if len(cfg.Secret) > 0 && len(cfg.Secret) < MinProxySecretBytes {
		return nil, fmt.Errorf("%w: %d bytes, floor is %d", ErrTrustedHeaderWeakSecret, len(cfg.Secret), MinProxySecretBytes)
	}
	var peers []netip.Prefix
	for _, item := range cfg.ProxyPeers {
		if strings.TrimSpace(item) == "" {
			continue
		}
		// 🔴 `netid.TrustedNetwork`, NOT A LOCAL PARSE. That function already refuses a
		// default route and enforces a per-family width floor, and it does so with the
		// reasoning written beside it. A second parser here would be the second copy of
		// a predicate that this repository has already watched go wrong at one of two
		// sites.
		prefix, err := netid.TrustedNetwork(item)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrTrustedHeaderPeer, err)
		}
		peers = append(peers, prefix)
	}
	secretHeader := cfg.SecretHeader
	if secretHeader == "" {
		secretHeader = DefaultProxySecretHeader
	}
	return &TrustedHeader{
		subjectHeader: cfg.SubjectHeader,
		secretHeader:  secretHeader,
		secret:        cfg.Secret,
		requireCert:   cfg.RequireClientCert,
		peers:         peers,
		provider:      cfg.Provider,
		authority:     cfg.Authority,
	}, nil
}

// maxSubjectBytes caps the identity header.
//
// A provider subject is a uuid or a numeric id. 256 bytes is a wide margin and is still
// a bound on what an unauthenticated caller can make this pod carry into a map lookup
// and, on success, into an audit line.
const maxSubjectBytes = 256

// Authenticate checks the SOURCE first and the identity second.
//
// 🔴 SOURCE BEFORE IDENTITY, ALWAYS, AND THE ORDER IS THE SECURITY PROPERTY RATHER THAN
// A PERFORMANCE ONE. Reading the subject header first and checking the secret afterwards
// would be correct for the same set of accepted requests and wrong about everything
// else: a caller could probe which subjects exist by watching how the refusals differ in
// timing or in shape. Nothing about the claimed identity is read until the request has
// been established as coming from the proxy.
//
// 🔴 AND EVERY REFUSAL IS THE SAME ERROR ON THE WIRE. `Refusal` carries a reason so a
// test can watch a specific rung fire; `internal/api` maps all of them onto the one
// uniform 401 it gives a wrong token.
func (t *TrustedHeader) Authenticate(r *http.Request) (Identity, error) {
	if len(t.peers) > 0 {
		peer, ok := netid.PeerAddress(r.RemoteAddr)
		if !ok {
			return Identity{}, refuse(TrustedHeaderBackend, "the peer address is unparseable")
		}
		if !netid.PeerIsTrusted(peer, t.peers) {
			return Identity{}, refuse(TrustedHeaderBackend, "the peer is not an allowlisted proxy")
		}
	}
	if t.requireCert {
		// A chain is present only when this server's own `tls.Config` verified it. See
		// `TrustedHeaderConfig.RequireClientCert`.
		if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 {
			return Identity{}, refuse(TrustedHeaderBackend, "no verified TLS client certificate")
		}
	}
	if len(t.secret) > 0 {
		// 🔴 EXACTLY ONE VALUE. A proxy that APPENDS rather than overwrites, or a
		// caller smuggling a second value past one that does, must not get a match on
		// whichever copy happens to be right — the same rule, and the same reasoning,
		// as `netid.ClientIP`'s `len(values) != 1`.
		values := r.Header.Values(t.secretHeader)
		if len(values) != 1 {
			return Identity{}, refuse(TrustedHeaderBackend, "the shared-secret header is absent or duplicated")
		}
		// 🔴 CONSTANT TIME. The comparison is against a secret, and this endpoint is
		// reachable by anyone who can address the pod — the same reasoning
		// `control.EqualHash` records for a token digest. `subtle.ConstantTimeCompare`
		// returns 0 for unequal lengths without comparing, which is fine here: the
		// LENGTH of the configured secret is not derived from its value.
		if subtle.ConstantTimeCompare([]byte(values[0]), t.secret) != 1 {
			return Identity{}, refuse(TrustedHeaderBackend, "the shared secret does not match")
		}
	}

	// The source is established. Only now is anything the caller claims about WHO they
	// are read at all.
	subjects := r.Header.Values(t.subjectHeader)
	if len(subjects) != 1 {
		return Identity{}, refuse(TrustedHeaderBackend, "the subject header is absent or duplicated")
	}
	subject := strings.TrimSpace(subjects[0])
	if subject == "" {
		return Identity{}, refuse(TrustedHeaderBackend, "the subject header is empty")
	}
	if len(subject) > maxSubjectBytes {
		return Identity{}, refuse(TrustedHeaderBackend, "the subject header is over the length cap")
	}

	// One model read. Every fact below comes out of it.
	model := t.authority.Model()
	user, known := model.UserByProviderSubject(t.provider, subject)
	if !known {
		return Identity{}, refuse(TrustedHeaderBackend, "the proxy vouched for a subject this control plane holds no user for")
	}
	principal, held := model.PrincipalFor(control.KindUser, user.ID)
	if !held {
		// Unreachable while the row came from this same model. See the identical arm in
		// `SupabaseJWT.Authenticate` for why it refuses rather than proceeds.
		return Identity{}, refuse(TrustedHeaderBackend, "the user row resolves to no principal")
	}
	return Identity{
		Principal: principal,
		Auth:      control.Resolve(model, principal),
	}, nil
}
