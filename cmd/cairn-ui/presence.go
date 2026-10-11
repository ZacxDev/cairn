package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/netid"
	"github.com/ZacxDev/cairn/internal/presence"
)

// The presence flags (slice S2 of `claudedocs/plan-cairn-arcs-presence.md`). Flags only, with
// NO default and NO environment spelling: presence on a deployment has to be a deliberate,
// reviewable line naming its sole owner (decision 15), and a manifest that copies another's
// environment must not switch it on.
const (
	flagPresenceAddr   = "presence-agent-addr"
	flagPresenceTokens = "presence-tokens"
	flagPresenceOwner  = "presence-owner"
	flagIssuePresence  = "issue-presence-token"
	flagPresenceHost   = "presence-host"
)

// presenceSettings is what the operator wrote on the command line.
type presenceSettings struct {
	addr, tokens, owner string
	issue, host         string
}

// presenceListener decides whether the agent listener runs, and for which owner.
//
// 🔴 ALL THREE OR NONE. `-presence-agent-addr`, `-presence-tokens` and `-presence-owner` arm
// one thing together; a subset is a half-configuration and refuses, naming what is missing —
// the shape every startup refusal in this program exists against. None at all is the designed
// OFF state: no listener, no token file opened, nothing bound.
//
// 🔴 A VALUE THAT REDUCES TO NOTHING IS REFUSED, NOT READ AS UNSET, for the reason
// `controlJournalDefault` gives: it is a line the operator wrote, and
// `identity.ValueReducesToNothing` is the predicate — never a fresh `strings.TrimSpace`.
//
// 🔴 THE OWNER MUST BE A PRINCIPAL THE AUTHORITY HOLDS. An owner nobody can authenticate as
// would be a listener accepting pushes that no page could ever show.
func presenceListener(s presenceSettings, m control.Model) (presence.Owner, bool, error) {
	given := map[string]string{flagPresenceAddr: s.addr, flagPresenceTokens: s.tokens, flagPresenceOwner: s.owner}
	order := []string{flagPresenceAddr, flagPresenceTokens, flagPresenceOwner}
	var missing []string
	for _, name := range order {
		v := given[name]
		if v == "" {
			missing = append(missing, "-"+name)
			continue
		}
		if identity.ValueReducesToNothing(v) {
			return presence.Owner{}, false, fmt.Errorf("-%s is set to a value that reduces to nothing. Refusing to "+
				"start rather than reading it as unset; give it a value or remove the flag", name)
		}
	}
	if s.issue != "" {
		// Minting is a different mode and is decided by `issuePresenceToken`; it never starts a
		// listener, so only the listener's own address is out of place here.
		if s.addr != "" {
			return presence.Owner{}, false, fmt.Errorf("-%s mints a token and exits; -%s starts a listener. "+
				"Refusing to do both in one run", flagIssuePresence, flagPresenceAddr)
		}
		return presence.Owner{}, false, nil
	}
	if s.host != "" {
		return presence.Owner{}, false, fmt.Errorf("-%s names the host a MINTED token is bound to and means "+
			"nothing without -%s. Refusing to start", flagPresenceHost, flagIssuePresence)
	}
	if len(missing) == len(order) {
		return presence.Owner{}, false, nil
	}
	if len(missing) > 0 {
		return presence.Owner{}, false, fmt.Errorf("the presence agent listener needs -%s, -%s and -%s together; "+
			"%s missing, so this is a half-configuration. Refusing to start; set all three or none",
			flagPresenceAddr, flagPresenceTokens, flagPresenceOwner, strings.Join(missing, " and "))
	}
	owner, err := presence.ParseOwner(s.owner)
	if err != nil {
		return presence.Owner{}, false, fmt.Errorf("-%s: %v. Refusing to start", flagPresenceOwner, err)
	}
	if _, known := m.PrincipalFor(owner.Kind, owner.ID); !known {
		return presence.Owner{}, false, fmt.Errorf("-%s %s names a principal this authority does not hold, so no "+
			"viewer could ever match it. Refusing to start", flagPresenceOwner, owner)
	}
	if _, _, err := net.SplitHostPort(s.addr); err != nil {
		return presence.Owner{}, false, fmt.Errorf("-%s %q is not host:port (%v). Refusing to start",
			flagPresenceAddr, s.addr, err)
	}
	return owner, true, nil
}

// presenceBindRefusal is `cairn-ui`'s reachable-bind refusal applied to the AGENT listener's
// OWN bind (decision 4).
//
// 🔴 A SEPARATE BIND IS A SEPARATE REACHABILITY QUESTION AND MUST NOT INHERIT THE BROWSER
// LISTENER'S VERDICT. A browser listener on loopback says nothing about an agent listener on
// `0.0.0.0`; without an allowlist the failed-token lockout would key on the proxy's address,
// one bucket for every caller behind it. Same predicate (`bindIsReachable`), same allowlist.
func presenceBindRefusal(addr string, proxyErr error) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr // unparseable reads as reachable, `bindIsReachable`'s safe direction
	}
	if bindIsReachable(host) && proxyErr != nil {
		return fmt.Errorf("refusing to serve the presence agent listener on %s: %v. It is reachable from "+
			"outside this machine, so the %s header cannot be trusted from an unlisted peer and the failed-token "+
			"lockout would have one bucket for every caller behind your proxy. Set $%s, or bind -%s to a "+
			"loopback address", addr, proxyErr, netid.ClientIPHeader, netid.EnvTrustedProxies, flagPresenceAddr)
	}
	return nil
}

// resolvePresenceOwner turns a HUMAN-supplied owner — `<kind>:<id>`, an email, a user's display
// name, or a token-file identity (a project name) — into the stable `(Kind, ID)`, ONCE, at mint
// time (decision 3). It must name exactly one principal.
//
// 🔴 A USER MATCHES ON THEIR RENDERED DISPLAY *OR* THEIR STORED EMAIL. Once a user has a display
// name, `PrincipalFor` renders that instead of the email, so matching the display alone would
// make the documented email spelling stop resolving the moment somebody is renamed. One user
// matching both ways is still ONE principal: it is appended once.
func resolvePresenceOwner(m control.Model, raw string) (presence.Owner, error) {
	if o, err := presence.ParseOwner(raw); err == nil {
		if _, known := m.PrincipalFor(o.Kind, o.ID); known {
			return o, nil
		}
		return presence.Owner{}, fmt.Errorf("-%s %s names a principal this authority does not hold", flagPresenceOwner, o)
	}
	var found []presence.Owner
	for id := range m.Users {
		if p, ok := m.PrincipalFor(control.KindUser, id); ok && (p.Display == raw || m.Users[id].Email == raw) {
			found = append(found, presence.OwnerOf(p))
		}
	}
	for id := range m.Projects {
		if p, ok := m.PrincipalFor(control.KindProject, id); ok && p.Display == raw {
			found = append(found, presence.OwnerOf(p))
		}
	}
	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		return presence.Owner{}, fmt.Errorf("-%s %q matches no user email, user display name or project name this authority holds",
			flagPresenceOwner, raw)
	default:
		return presence.Owner{}, fmt.Errorf("-%s %q matches %d principals; name one as <kind>:<id>",
			flagPresenceOwner, raw, len(found))
	}
}

// issuePresenceToken mints ONE presence token, appends its DIGEST to the token file, and prints
// the token once to `out`.
//
// 🔴 THE TOKEN IS WRITTEN TO `out` AND NOWHERE ELSE — never the file, never `note`. The file
// holds `<kind> <owner> <host> <sha256-hex>`; the operator line names the digest's prefix.
//
// 🔴 THE WALL HOLDS AT MINT TOO: if the file already has rows, they must all be this owner's,
// or the mint is refused — otherwise the next start of the listener would refuse instead.
func issuePresenceToken(out, note io.Writer, m control.Model, s presenceSettings) error {
	kind := presence.TokenKind(s.issue)
	if !kind.Valid() {
		return fmt.Errorf("-%s %q is not %q or %q", flagIssuePresence, s.issue, presence.KindPush, presence.KindClaim)
	}
	if s.tokens == "" || identity.ValueReducesToNothing(s.tokens) {
		return fmt.Errorf("-%s needs -%s, the file the digest is appended to", flagIssuePresence, flagPresenceTokens)
	}
	if s.owner == "" {
		return fmt.Errorf("-%s needs -%s (<kind>:<id>, an email, or a project name)", flagIssuePresence, flagPresenceOwner)
	}
	if !presence.ValidHostLabel(s.host) {
		return fmt.Errorf("-%s needs -%s, the ONE host label the token is bound to (got %q)",
			flagIssuePresence, flagPresenceHost, s.host)
	}
	owner, err := resolvePresenceOwner(m, s.owner)
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(s.tokens); statErr == nil {
		if _, err := presence.LoadTokens(s.tokens, owner); err != nil {
			return fmt.Errorf("refusing to mint into %s: %w", s.tokens, err)
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("the presence token file %s cannot be checked: %w", s.tokens, statErr)
	}
	token, err := presence.MintToken()
	if err != nil {
		return err
	}
	row := presence.NewTokenRow(kind, owner, s.host, token)
	if err := presence.AppendTokenRow(s.tokens, row); err != nil {
		return fmt.Errorf("the presence token file %s could not be written: %w", s.tokens, err)
	}
	fmt.Fprintf(note, "cairn-ui: minted a %s presence token for %s on host %s; its digest %s… is in %s. The token "+
		"is printed ONCE on stdout and stored nowhere\n", kind, owner, s.host, row.DigestPrefix(), s.tokens)
	_, err = fmt.Fprintln(out, token)
	return err
}
