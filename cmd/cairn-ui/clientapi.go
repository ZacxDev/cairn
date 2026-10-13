package main

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"time"

	"github.com/ZacxDev/cairn/internal/clientapi"
	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/envalias"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/memo"
	"github.com/ZacxDev/cairn/internal/netid"
	"github.com/ZacxDev/cairn/internal/pgstore"
	"github.com/ZacxDev/cairn/internal/transcript"
)

// flagClientAPIAddr is scope mail's CLIENT LISTENER (S2 of `claudedocs/plan-cairn-scope-mail.md`,
// decision 3): the THIRD listener, serving `internal/clientapi`'s four memo routes — and the
// transcripts plan's three agent read routes (S8) — to agents holding their own narrowed journal
// credential.
//
// A flag only — NO default and NO environment spelling, for presence's reason: a listener that
// accepts bearer credentials from outside the browser has to be a deliberate, reviewable line in
// a manifest, and a manifest that copies another's environment must not switch it on.
const flagClientAPIAddr = "client-api-addr"

// clientAPIRefusal judges `-client-api-addr` against what it needs, BEFORE anything is opened.
// "" is the designed OFF state.
//
// 🔴 IT REFUSES WITHOUT `-control-journal`, AND THAT REFUSAL IS THE STRUCTURAL FORM OF O9. Without
// a journal `openAuthority` takes the TOKEN-FILE branch, and a listener over that projection would
// authenticate the token file's rows — one row every client shares, so every sender would be the
// same sender, which is what the operator reversed O5 to avoid (plan, round 1 🟡1). Exit 78 rather
// than a listener that quietly serves the wrong authority.
//
// 🔴 IT REFUSES WITHOUT `-db-dsn`: the memo store is `internal/pgstore`'s, so a listener with no
// database would answer every route with an error while the startup line announced it.
//
// 🔴 AND IT APPLIES THE REACHABLE-BIND REFUSAL TO ITS OWN BIND, through the ONE spelling every
// second listener shares (`listenerBindRefusal`, `bindIsReachable`'s predicate): a loopback
// browser bind says nothing about a `0.0.0.0` client bind, and without an allowlist the
// failed-token lockout would key every caller behind a proxy into one bucket. It is judged here,
// with the flags, because it reads only the environment — so a misconfigured bind refuses before a
// database is dialled rather than after.
func clientAPIRefusal(addr, journal, dsn string, proxyErr error) error {
	if addr == "" {
		return nil
	}
	if identity.ValueReducesToNothing(addr) {
		return fmt.Errorf("-%s is set to a value that reduces to nothing. Refusing to start rather than reading "+
			"it as unset; give it host:port or remove the flag", flagClientAPIAddr)
	}
	if journal == "" {
		return fmt.Errorf("-%s needs -control-journal: without one this surface's authority is the TOKEN FILE's "+
			"projection, and the client listener would authenticate its rows — one shared row, so every sender "+
			"would be the same sender. Agents use their own narrowed journal credential (scope-mail plan, O9). "+
			"Refusing to start; set -control-journal / $%s, or remove -%s", flagClientAPIAddr, EnvUIControlJournal,
			flagClientAPIAddr)
	}
	if dsn == "" {
		return fmt.Errorf("-%s needs -db-dsn: memos are stored in this surface's database, so a listener without "+
			"one would refuse every request while announcing itself. Refusing to start; set $%s (or -db-dsn), "+
			"or remove -%s", flagClientAPIAddr, EnvUIDatabase, flagClientAPIAddr)
	}
	if _, _, err := net.SplitHostPort(addr); err != nil {
		return fmt.Errorf("-%s %q is not host:port (%v). Refusing to start", flagClientAPIAddr, addr, err)
	}
	return listenerBindRefusal("client API listener", flagClientAPIAddr, addr, proxyErr)
}

// trustedProxiesErr is the allowlist's verdict alone, read through the SAME `netid.LoadTrustedProxies`
// `main` uses for every other listener — so the client bind is judged by the one reader, early.
func trustedProxiesErr() error {
	_, err := netid.LoadTrustedProxies(envalias.Environ())
	return err
}

// openClientAPI builds the client listener over the ONE authority and the ONE database, and binds
// it. It returns the server, the bound listener and the startup line's clause.
//
// 🔴 THE SEND SCAN'S KEY IS FRESH PER PROCESS (decision 11): `memo.NewScanner` draws 32 bytes from
// `crypto/rand` and nothing stores them, because the scan reads only rule NAMES.
//
// `transcripts` is the transcript archive's read side for the agent read routes (transcripts S8), NIL
// on a deployment with no `-transcript-dir`.
func openClientAPI(addr string, machine *identity.MachineToken, authority *control.Cache, db *pgstore.DB,
	limiter *netid.RateLimiter, trusted []netip.Prefix, transcripts *transcript.Store) (*http.Server, net.Listener, string, error) {
	if db == nil {
		// Unreachable while `clientAPIRefusal` requires -db-dsn and `main` refuses a DSN it cannot
		// open; a nil store here would be a listener answering 500 to everything, so it is refused.
		return nil, nil, "", errors.New("the client API listener has no database")
	}
	scan, err := memo.NewScanner()
	if err != nil {
		return nil, nil, "", fmt.Errorf("the memo send scan cannot be built: %w", err)
	}
	memos, err := pgstore.NewMemoStore(db, scan)
	if err != nil {
		return nil, nil, "", err
	}
	handler, err := clientapi.New(clientapi.Config{
		Auth: machine, Model: authority.Model, Memos: memos, Limiter: limiter, TrustedProxies: trusted, Transcripts: transcripts,
		Log: func(line string) { fmt.Fprintln(os.Stderr, "cairn-ui: "+line) },
	})
	if err != nil {
		return nil, nil, "", err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, "", fmt.Errorf("the client API listener cannot bind: %w", err)
	}
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	return srv, ln, fmt.Sprintf("client API on %s (%d route(s), machine tokens only)",
		ln.Addr(), len(clientapi.ClientRoutes())), nil
}
