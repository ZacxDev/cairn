package main

// `cairn-server -revoke-credential`: the operator-driven revocation of one issued credential
// (scope-mail plan, decision 16).
//
// 🔴 IT CLOSES A STATED GAP: `credential-revoked` was in the closed event set, validated and
// applied, and nothing in this repository WROTE one — `-issue-credential`'s own advice was to
// "hand-append" it. A hand edit of an append-only authority is the one remedy this repository
// refuses to prescribe elsewhere, so the revocation is a command, through the same journal
// writer `-issue-credential` uses.
//
// 🔴 A COMMAND, NOT A ROUTE, for `-issue-credential`'s reason: a route that changes who may
// authenticate would be a public, internet-reachable endpoint over the authority itself.
// `api.DeclaredRoutes()` is unchanged by this file.

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
)

type revokeCredentialFlags struct {
	enabled    *bool
	credential *string
}

// registerRevokeCredentialFlags declares the mode's flags — plain flags on the server binary,
// matching `-issue-credential`; see `registerCreateUserFlags` for why not a subcommand.
func registerRevokeCredentialFlags() *revokeCredentialFlags {
	return &revokeCredentialFlags{
		enabled: flag.Bool("revoke-credential", false,
			"REVOKE one issued credential: append ONE `credential-revoked` record to the control "+
				"journal named by $"+EnvControlJournal+", print the label it revoked, and exit. Every "+
				"surface reading that journal refuses the token on its next authority refresh. Requires "+
				"-credential"),
		credential: flag.String("credential", "",
			"the credential's id (`crd_…`), as `-issue-credential` printed it on its "+
				"`cairn-control: issued credential=…` line. Never the token: the journal holds only a "+
				"digest, and a token on argv is a token in shell history"),
	}
}

// runRevokeCredential performs the revocation and returns the process exit code.
//
// 🔴 AN UNKNOWN ID AND AN ALREADY-REVOKED ONE ARE EACH REFUSED IN THEIR OWN WORDS AND APPEND
// NOTHING. The already-revoked case is the one that matters: `apply` would accept a second
// revocation as a no-op, so appending it would report success to an operator who typed the
// id of a credential that was ALREADY dead while the one they meant to cut off keeps working.
//
// 🔴 THE LABEL IS PRINTED, because the label is how a human tells which HOST they just cut off
// (`agents@host-a`): an operator who revoked the wrong one learns it here, not when that
// host's agents stop hearing memos.
func runRevokeCredential(env map[string]string, f *revokeCredentialFlags, out, errOut io.Writer) int {
	warn := func(line string) { fmt.Fprintln(errOut, reloadSafe(line)) }

	journal, err := controlJournalPath(env)
	if err != nil {
		warn("subsystem-store-api: " + err.Error())
		return exitConfig
	}
	if journal == "" {
		warn(fmt.Sprintf(
			"subsystem-store-api: -revoke-credential needs a control journal and $%s is not set. Set it "+
				"to the path the running surfaces read their journal from — a revocation written anywhere "+
				"else retires nothing anybody is serving",
			EnvControlJournal))
		return exitConfig
	}
	id := control.ID(strings.TrimSpace(*f.credential))
	if id == "" {
		warn("subsystem-store-api: -revoke-credential refused: -credential is required. There is no " +
			"default — revoking 'the only credential' is a guess that becomes wrong the day there are two")
		return exitConfig
	}
	if !control.MustPrefix(id, control.PrefixCredential) {
		warn(fmt.Sprintf(
			"subsystem-store-api: -revoke-credential refused: %q is not a credential id (`%s_…`). It is "+
				"neither a token nor a label: the journal holds neither by which a credential could be found",
			id, control.PrefixCredential))
		return exitConfig
	}

	ctx := context.Background()
	store, err := control.OpenFileStore(journal)
	if err != nil {
		warn("subsystem-store-api: " + err.Error())
		return exitConfig
	}
	if m, err := store.Model(ctx); err == nil {
		warnAboutDroppedRecords(m, journal, warn)
	}

	r, err := control.RevokeCredential(ctx, store, id, "", time.Now().UTC())
	switch {
	case errors.Is(err, control.ErrNoSuchCredential):
		warn(fmt.Sprintf(
			"subsystem-store-api: -revoke-credential refused: this control journal holds no credential %s, "+
				"so nothing was written. Journal: %s", id, journal))
		return exitConfig
	case errors.Is(err, control.ErrAlreadyRevoked):
		warn(fmt.Sprintf(
			"subsystem-store-api: -revoke-credential refused: credential %s is ALREADY revoked, so nothing "+
				"was written. If you meant to cut off a credential that is still working, this is not its "+
				"id. Journal: %s", id, journal))
		return exitConfig
	case err != nil:
		warn("subsystem-store-api: -revoke-credential refused: " + err.Error())
		return exitConfig
	}

	// The record line on STDOUT, in `-create-user`'s `cairn-control:` shape — nothing secret is
	// in it, so unlike `-issue-credential` this mode's stdout is free for a script to read.
	fmt.Fprintln(out, reloadSafe(fmt.Sprintf(
		"cairn-control: revoked credential=%s principal=%s:%s label=%q epoch=%d",
		r.Credential.ID, r.Credential.PrincipalKind, r.Credential.PrincipalID, r.Credential.Label, r.Epoch)))
	warn(fmt.Sprintf(
		"subsystem-store-api: revoked the credential labelled %q. Every surface reading this journal "+
			"refuses its token on its next refresh — within %s for this binary's own loop — and NOT while "+
			"a surface's refreshes keep failing: the last-known-good model keeps serving until one succeeds",
		r.Credential.Label, refreshInterval))
	return 0
}
