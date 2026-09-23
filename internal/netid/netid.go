// Package netid decides WHICH address a request is bucketed under, and whether
// the peer that delivered it was a trusted proxy.
//
// 🔴 THE HEADER THIS READS IS CALLER-SUPPLIED DATA, and everything that makes it
// an identity is `PeerIsTrusted`. The Python original read it from whoever
// connected, so ANY peer that could address the pod could send a header naming a
// THIRD PARTY plus five bad tokens, and that third party was locked out for
// fifteen minutes — seeing a 401 indistinguishable from a wrong credential.
// Measured against the deployed pod before the fix: five forged requests, then the
// victim's own valid request.
//
// ⚠ WHAT THIS CANNOT DO. The pod sees the address of whatever last hop connected
// to it — in the deployment that is the in-cluster gateway, never the CDN's own
// address. So this proves "the request came through the gateway", NOT "the request
// came through the CDN". Anything that can occupy that hop can still forge the
// header. Narrowing WHO can occupy it is a NetworkPolicy's job, and it is the
// second layer, not this one.
package netid

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"regexp"
	"strings"

	"github.com/ZacxDev/cairn/internal/envalias"
)

// ClientIPHeader is THE ONLY HEADER THIS SERVER WILL ACCEPT AS A CLIENT IDENTITY,
// and it is trustworthy for exactly one reason: the CDN is the sole public ingress
// and overwrites it on every request it proxies.
//
// 🔴 `X-Forwarded-For` IS DELIBERATELY ABSENT FROM THIS PACKAGE. It is
// caller-supplied, so keying on it lets one attacker rotate through a million
// buckets AND lets them lock out a third party by forging theirs. Behind a gateway
// the TCP peer address is the gateway's for everybody, so keying on THAT is the
// mirror failure — one abuser locks out the world.
const ClientIPHeader = "CF-Connecting-IP"

// EnvTrustedProxies names the peer allowlist that makes ClientIPHeader readable.
const EnvTrustedProxies = "CAIRN_TRUSTED_PROXIES"

// MinTrustedPrefix is A FLOOR ON HOW WIDE ONE ENTRY MAY BE, keyed by address
// family.
//
// Refusing only `/0` checks ONE ENTRY IN ISOLATION and is walkable two ways, both
// measured on the Python side. (a) The two halves of the address space, each
// written as a `/1`, parse clean and together trust every IPv4 peer — no single
// entry is a default route, so a per-entry `/0` check sees nothing wrong. (b) The
// realistic one: an ordinary pod CIDR is accepted and hands the client identity to
// EVERY POD IN THE CLUSTER, which is verbatim the attacker in this design's own
// threat model.
//
// A floor subsumes the union check an audit suggested and is stronger: with /24 as
// the minimum, no set of entries an operator would plausibly type can cover the
// space, and the check stays local to one entry so the error can NAME the offending
// one. Two guards remain rather than one because their diagnostics differ — `/0` is
// "you disabled it", a wide prefix is "you meant a smaller range".
//
// The numbers: a /24 is 256 addresses, generous for a proxy tier; a v6 /64 is one
// LAN segment, the smallest unit an operator is given.
var MinTrustedPrefix = map[bool]int{true: 24, false: 64} // true = IPv4

// TrustedNetwork parses ONE allowlist entry into a prefix, or returns an error.
//
// 🔴 ONE RULE, ONE PLACE. Both the environment reader and a programmatic caller
// need exactly this parse and exactly these refusals, and a predicate open-coded at
// two call sites is wrong at one of them. A `/0` reaching the server through the
// programmatic door would be just as total as one reaching it through the
// environment door.
func TrustedNetwork(item string) (netip.Prefix, error) {
	text := strings.TrimSpace(item)
	prefix, err := netip.ParsePrefix(text)
	if err != nil {
		// A bare address is an entry too — `10.0.0.1` means that host. Python's
		// `ip_network(..., strict=False)` accepts both spellings, and refusing the
		// bare one here would push an operator towards the `/0` the next guard
		// exists to stop.
		addr, addrErr := netip.ParseAddr(text)
		if addrErr != nil {
			return netip.Prefix{}, fmt.Errorf(
				"%s: '%s' is not an IP address or CIDR (%v)", EnvTrustedProxies, item, err)
		}
		prefix = netip.PrefixFrom(addr, addr.BitLen())
	}
	// `Masked()` so `10.1.2.3/24` is accepted as the /24 it names rather than
	// refused for having host bits set — an operator writing a CIDR from memory
	// means the network.
	prefix = prefix.Masked()
	if prefix.Bits() == 0 {
		return netip.Prefix{}, fmt.Errorf(
			"%s: '%s' trusts every peer, which is the defect this setting exists to close. Name the proxy's address",
			EnvTrustedProxies, item)
	}
	floor := MinTrustedPrefix[prefix.Addr().Is4()]
	if prefix.Bits() < floor {
		return netip.Prefix{}, fmt.Errorf(
			"%s: '%s' is too broad — /%d covers %s peers; the floor is /%d. A trusted proxy is a host or a small tier, and a wide range here hands the client identity to everything inside it. List the individual addresses, or several narrower CIDRs",
			EnvTrustedProxies, item, prefix.Bits(), addressCount(prefix), floor)
	}
	return prefix, nil
}

func addressCount(prefix netip.Prefix) string {
	host := prefix.Addr().BitLen() - prefix.Bits()
	if host >= 63 {
		// Beyond int64 the exact figure is noise in a diagnostic; the exponent is
		// the fact the operator needs.
		return fmt.Sprintf("2^%d", host)
	}
	return fmt.Sprintf("%d", int64(1)<<host)
}

var proxySeparators = regexp.MustCompile(`[,\s]+`)

// LoadTrustedProxies resolves the peer allowlist, or returns an error.
//
// 🔴 THERE IS NO DEFAULT, AND THAT IS THE POINT. A default would be a guess about
// somebody else's network, and the only guess that keeps every deployment working is
// a permissive one — which is the defect this function exists to close, shipped as a
// constant. So an unset or empty variable is a misconfiguration that refuses to
// start, visible in a CrashLoopBackOff, exactly like a token file that is missing.
//
// Guard order — each reachable by an input no earlier guard rejects:
//  1. the variable is set and non-blank -> "no trusted proxies"
//  2. every item parses as an address/CIDR -> "not an IP address or CIDR"
//  3. no item is a DEFAULT ROUTE -> "trusts every peer" (by PREFIX LENGTH, so
//     every spelling of a default route is one rule rather than a list somebody
//     has to extend)
func LoadTrustedProxies(env map[string]string) ([]netip.Prefix, error) {
	raw := envalias.Value(env, EnvTrustedProxies)
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf(
			"no trusted proxies: set $%s to the address(es) or CIDR(s) of the proxy that terminates public traffic. The %s header is only read from those peers, and there is deliberately no default",
			EnvTrustedProxies, ClientIPHeader)
	}
	var out []netip.Prefix
	for _, item := range proxySeparators.Split(strings.TrimSpace(raw), -1) {
		if item == "" {
			continue
		}
		prefix, err := TrustedNetwork(item)
		if err != nil {
			return nil, err
		}
		out = append(out, prefix)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no trusted proxies: $%s resolved to no entries", EnvTrustedProxies)
	}
	return out, nil
}

// PeerAddress is the TCP peer, normalised. `ok` false means REFUSE.
//
// 🔴 IPv4-MAPPED IS UNWRAPPED, because a dual-stack listener reports a v4 caller as
// `::ffff:…` and an allowlist written as a /32 would then never match — a
// fail-CLOSED break rather than a hole, but a break that reads as "the guard is
// wrong" and gets widened until it is.
//
// 🔴 IT DOES NOT GO THROUGH RateLimitKey, AND MUST NOT. That function aggregates
// IPv6 to its /64 on purpose, because an attacker picks freely inside their own
// allocation — the exact reasoning that makes it WRONG here: a /64 of trusted
// proxies is 2^64 peers the operator did not name. Two functions that both
// normalise an address, deliberately differently.
func PeerAddress(remoteAddr string) (netip.Addr, bool) {
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	// An IPv6 link-local peer carries a scope id that the parser will not accept as
	// part of an address. The zone is a local interface name, not part of the
	// identity being allowlisted.
	if i := strings.IndexByte(host, '%'); i >= 0 {
		host = host[:i]
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(host))
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

// PeerIsTrusted answers whether this peer is one of the proxies whose client-IP
// header we will read.
func PeerIsTrusted(peer netip.Addr, trusted []netip.Prefix) bool {
	if !peer.IsValid() {
		return false
	}
	for _, prefix := range trusted {
		// No family gate. The Python original had one with a FALSE comment beside
		// it ("`IPv4Address in IPv6Network` raises"); it does not, it returns
		// False, so the clause was dead code that a mutation sweep found by
		// surviving its removal. `netip.Prefix.Contains` likewise reports false
		// across families, so the same clause would be the same dead code here.
		if prefix.Contains(peer) {
			return true
		}
	}
	return false
}

// ClientIP is the client's address from the header, or `ok` false — and false means
// REFUSE, not "unknown".
//
// 🔴 THIS FUNCTION IS ONLY CALLED FOR A TRUSTED PEER. It parses caller-supplied
// bytes; the thing that makes those bytes an identity is PeerIsTrusted, and
// ResolveClient is the ONE place that asks. For an untrusted peer it is not called
// at all — not called and its answer discarded, but never reached — so a forged
// header cannot become a bucket key by any path. There is exactly one call site, and
// that is the guard.
//
// Three ways to get false, all fail-closed at the call site: absent (not a proxied
// request), not an IP address (a forged or mangled value), and MORE THAN ONE (a
// caller trying to smuggle a second value past a proxy that appends rather than
// overwrites).
//
// Returns the NORMALISED form, so an IPv4-mapped address written two ways cannot
// become two buckets for one attacker.
func ClientIP(headers http.Header) (string, bool) {
	values := headers.Values(ClientIPHeader)
	if len(values) != 1 {
		return "", false
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(values[0]))
	if err != nil {
		return "", false
	}
	return RateLimitKey(addr), true
}

// ResolveClient decides WHICH address this request is bucketed under, and whether
// the peer was a trusted proxy. `ok` false means refuse.
//
// 🔴 THIS IS THE WHOLE RULE, AND IT IS THE STANDARD REVERSE-PROXY ONE:
//
//	peer IS trusted     -> the bucket is the client-IP header (fail closed if it
//	                       is absent, unparseable or duplicated)
//	peer is NOT trusted -> the bucket is the PEER'S OWN ADDRESS, and the header is
//	                       not read AT ALL
//
// The security property is *a forged header must never name a THIRD PARTY*. The
// second line satisfies it completely: a caller whose header is ignored can only
// ever lock out ITSELF, which is the definition of a rate limit working.
//
// 🔴 AN UNTRUSTED PEER IS NOT REFUSED, and an earlier version of the Python branch
// got that wrong. Refusing is stricter than the property needs and it broke the
// acceptance procedure outright: a port-forward presents peer 127.0.0.1 while the
// deployment allowlists the node's internal address, so every byte-identity run
// became a 401 — the acceptance criterion, with no documented way left to run it. It
// also turned one wrong address in one variable into a total outage that `/healthz`
// hid. **Distrust is expressed by ignoring what the caller claims, not by hanging up
// on them.**
//
// ⚠ AN UNPARSEABLE PEER still refuses: there is no bucket to charge, which is
// exactly the no-client-IP condition, so it is reported as that rather than as a
// fourth vocabulary item. Unreachable over a real TCP socket; reachable, and tested,
// here.
//
// Both branches normalise through RateLimitKey, so one caller is one bucket
// whichever door it came in by — the same aggregation rule, not a second copy of it.
func ResolveClient(headers http.Header, remoteAddr string, trusted []netip.Prefix) (key string, peerTrusted bool, ok bool) {
	peer, peerOK := PeerAddress(remoteAddr)
	if peerOK && PeerIsTrusted(peer, trusted) {
		ip, ipOK := ClientIP(headers)
		return ip, true, ipOK
	}
	if !peerOK {
		return "", false, false
	}
	return RateLimitKey(peer), false, true
}

// RateLimitKey collapses an address to the unit a lockout should apply to.
//
// 🔴 A FULL IPv6 ADDRESS IS NOT A CLIENT, IT IS A CHOICE. An ordinary residential
// allocation is a /64 — 2^64 addresses the same person can pick freely — so keying
// on the full address gives one attacker 2^64 buckets and the lockout becomes
// decorative. Worse, it is also the cheapest way to grow the failure table without
// bound. So IPv6 is aggregated to its /64.
//
// 🔴 AN IPv4-MAPPED IPv6 ADDRESS IS THE SAME CLIENT AS ITS IPv4 FORM. Left alone,
// one IPv4 caller gets a free second bucket simply by reaching the edge over v6 —
// and the Python guard that claimed "one caller is one bucket" only ever compared
// two spellings of the mapped form, so it did not see this.
func RateLimitKey(addr netip.Addr) string {
	unmapped := addr.Unmap()
	if unmapped.Is4() {
		return unmapped.String()
	}
	prefix, err := unmapped.Prefix(64)
	if err != nil {
		return unmapped.String()
	}
	return prefix.String()
}
