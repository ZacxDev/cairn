package main

// `cairn-server -create-user`: the operator-driven user-creation path.
//
// 🔴 A COMMAND, NOT A ROUTE, AND THAT IS THE DESIGN DECISION RATHER THAN A CONVENIENCE.
// `AGENTS.md`: "Adding a row to a dispatch table is adding a public, internet-reachable
// endpoint." A creation endpoint is the highest-value target this pod could grow — it
// mints principals — and putting it on the HTTP surface would require the route ledger,
// the conformance corpus, the construction-time check and `checks.go-server-declares-its-routes`
// to move together, for a capability that has exactly one caller: a human with `exec`
// into the pod. So it is a flag mode that exits, reached the same way the documented
// token rotation is (`kubectl exec … -- sh -c 'kill -HUP 1'`), and it adds nothing to the
// internet-reachable surface. `api.DeclaredRoutes()` is unchanged by this file, which is
// the checkable half of that sentence.
//
// 🔴 AND IT IS NOT SIGNUP. Nothing on a read path creates a user; see
// `control.ProvisionUser`, whose comment states the rule and what signup would owe.

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/ZacxDev/cairn/internal/api"
	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
)

// createUserFlags is the mode's own flag set, registered by `main` before `flag.Parse`.
type createUserFlags struct {
	enabled  *bool
	provider *string
	subject  *string
	email    *string
	project  *string
	scopes   *string
}

// registerCreateUserFlags declares the mode's flags.
//
// ⚠ THEY ARE PLAIN FLAGS ON THE SERVER BINARY RATHER THAN A SUBCOMMAND, MATCHING
// `-routes`. A subcommand would mean a second `flag.FlagSet` and a second parse of
// `os.Args`, which is a second place the program decides what it was asked to do.
func registerCreateUserFlags() *createUserFlags {
	return &createUserFlags{
		enabled: flag.Bool("create-user", false,
			"CREATE A USER in the control journal named by $"+EnvControlJournal+" and exit. "+
				"An OPERATOR action: a verified session for a subject this control plane holds no "+
				"user for is refused, never provisioned, so somebody with write access to the "+
				"journal has to do this deliberately. Requires -provider, -subject and -project"),
		provider: flag.String("provider", "",
			"the identity provider's namespace, which must EQUAL what the session backend "+
				"resolves against: `supabase` (or $CAIRN_SUPABASE_PROVIDER) for the JWT backend, "+
				"$CAIRN_TRUSTED_HEADER_PROVIDER for the proxy backend. A user created under any "+
				"other string is one that backend will never find"),
		subject: flag.String("subject", "",
			"the provider's own stable id for this person — the JWT's `sub`, or what the "+
				"trusted subject header carries. NOT an email: an email is mutable at the "+
				"provider and reused across providers"),
		email: flag.String("email", "",
			"display only, and optional. It is what the audit line's identity= field and every "+
				"written bullet's ACTOR render when present; without it they carry "+
				"`<provider>:<subject>`"),
		project: flag.String("project", "",
			"the project created for this user, who becomes its OWNER — read, write and admin "+
				"over every scope the project holds"),
		scopes: flag.String("scopes", "",
			"comma-separated scope display names to create in that project. A display name is "+
				"the DIRECTORY name under the store root, and creating the record does NOT "+
				"create the directory. Empty is accepted and creates a user who can reach "+
				"nothing until a scope is created or shared"),
	}
}

// runCreateUser performs the creation and returns the process exit code.
//
// 🔴 IT RETURNS A CODE RATHER THAN CALLING `os.Exit`, SO A TEST CAN RUN IT IN-PROCESS.
// The alternative — re-executing the binary for every case — is what `main_test.go`
// already pays for the server path, and it is worth paying there because the thing under
// test IS the process. Here the thing under test is a journal write, and an in-process
// call lets a test read the journal back in the same function.
func runCreateUser(env map[string]string, f *createUserFlags, out, errOut io.Writer) int {
	journal, err := controlJournalPath(env)
	if err != nil {
		fmt.Fprintln(errOut, reloadSafe("subsystem-store-api: "+err.Error()))
		return exitConfig
	}
	if journal == "" {
		// 🔴 REFUSED RATHER THAN DEFAULTED TO A PATH. A default would put a journal
		// somewhere the running pod is not reading — the two would then disagree
		// silently, and the operator's evidence that the command "worked" would be a file
		// nothing loads.
		fmt.Fprintln(errOut, reloadSafe(fmt.Sprintf(
			"subsystem-store-api: -create-user needs a control journal and $%s is not set. "+
				"Set it to the path this pod reads its journal from — a user written anywhere else "+
				"is a user no running server can resolve a session to",
			EnvControlJournal)))
		return exitConfig
	}

	store, err := control.OpenFileStore(journal)
	if err != nil {
		fmt.Fprintln(errOut, reloadSafe("subsystem-store-api: "+err.Error()))
		return exitConfig
	}

	req := control.NewUser{
		Provider:    *f.provider,
		Subject:     *f.subject,
		Email:       *f.email,
		ProjectName: *f.project,
		ScopeNames:  scopeNames(*f.scopes),
	}
	made, err := control.ProvisionUser(context.Background(), store, req)
	if err != nil {
		// 🔴 65, NOT 78: the pod is configured correctly and the REQUEST was refused. The
		// journal's own message names the event and the field, so it is passed through
		// rather than re-worded — a second wording here would be a second description of
		// a rule that lives in `internal/control`.
		fmt.Fprintln(errOut, reloadSafe("subsystem-store-api: -create-user refused: "+err.Error()))
		return exitDataErr
	}

	// 🔴 THE IDS GO TO STDOUT AND THE CAVEATS TO STDERR, because the ids are what an
	// operator pipes somewhere and the caveats are what they read. Never a token: this
	// path mints no credential at all — a session backend authenticates against the IdP,
	// and `Identity.Fingerprint` is empty for it by design.
	fmt.Fprintln(out, reloadSafe(fmt.Sprintf(
		"cairn-control: created user=%s provider=%s subject=%s project=%s epoch=%d scopes=%s",
		made.User, req.Provider, req.Subject, made.Project, made.Epoch, renderScopes(made.Scopes))))

	if len(made.Scopes) == 0 {
		// 🔴 SAID OUT LOUD, BECAUSE FROM OUTSIDE THE POD THIS STATE IS INDISTINGUISHABLE
		// FROM THE DEFECT THIS WHOLE PATH CLOSES. The user authenticates and resolves to
		// an EMPTY `Authorization`, so every read answers as if the scope did not exist —
		// exactly what a session got before any journal was wired. It is a legitimate
		// intermediate state (create the person, decide access after), which is why it is
		// a line rather than a refusal.
		fmt.Fprintln(errOut, reloadSafe(fmt.Sprintf(
			"subsystem-store-api: WARNING user=%s can reach NOTHING: no scope was created, so this "+
				"person will authenticate and see an empty world, which looks identical to a broken "+
				"sign-in. Re-run with -scopes, or share a scope with them",
			made.User)))
	}
	// ⚠ THE RUNNING POD DOES NOT SEE THIS YET, AND THE LAG IS BOUNDED RATHER THAN
	// INSTANT. This process wrote the file; the server materializes its authority from a
	// `control.Cache` that re-reads the journal on its timer and on SIGHUP. Saying so is
	// the difference between an operator who waits and one who concludes the command did
	// nothing.
	fmt.Fprintln(errOut, reloadSafe(fmt.Sprintf(
		"subsystem-store-api: the running server picks this up within %s, or immediately on "+
			"`kill -HUP 1`", refreshInterval)))
	return 0
}

// controlJournalPath reads $CAIRN_CONTROL_JOURNAL and applies its declared blank policy.
//
// See `EnvControlJournal` for the policy and why it is `refuseBlank`. Absent, and
// present-with-the-EMPTY-string, are both "not set" — the same boundary
// `internal/identity/config.go`'s `touched` draws, and for the same reason: a manifest
// that emits every variable with an empty default is a common shape, so `X=""` is "not
// using this" and `X="  "` is a line the operator wrote and this pod would discard.
func controlJournalPath(env map[string]string) (string, error) {
	raw, present := env[EnvControlJournal]
	if !present || raw == "" {
		return "", nil
	}
	if identity.ValueReducesToNothing(raw) {
		return "", fmt.Errorf(
			"%s=%q reduces to nothing. Read as UNSET it means: this pod resolves browser and "+
				"proxy sessions against the token-file projection, which holds no user any identity "+
				"provider can name — so every sign-in is refused while the pod looks healthy. Give it "+
				"a path or delete the line",
			EnvControlJournal, raw)
	}
	return strings.TrimSpace(raw), nil
}

// scopeNames splits the -scopes value.
//
// ⚠ COMMAS ONLY, AND NOT WHITESPACE, WHICH IS DELIBERATELY NARROWER THAN
// `identity.peerFields`. A scope display name is a DIRECTORY NAME and a directory name may
// contain a space; splitting on whitespace would silently turn one scope into two, each
// pointing at a directory that does not exist. Surrounding whitespace is trimmed because
// `-scopes "a, b"` is what a human types, and an empty element is dropped so a trailing
// comma is not a nameless scope the journal then refuses.
func scopeNames(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func renderScopes(scopes []control.ProvisionedScope) string {
	if len(scopes) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(scopes))
	for _, sc := range scopes {
		parts = append(parts, string(sc.ID)+":"+sc.Name)
	}
	return strings.Join(parts, ",")
}

// openSessionAuthority builds the journal-backed authority the SESSION backends resolve
// against, or reports that this deployment configured none.
//
// 🔴 IT RETURNS A nil `identity.ModelSource` FOR THE UNCONFIGURED CASE, AND THE nil MUST
// BE THE INTERFACE'S OWN. Returning a typed nil `*control.Cache` would make
// `sessions != nil` true inside `identity.FromEnvironment`, which is the check that
// distinguishes "no journal" from "a journal nobody reads" — a typed nil there would turn
// every existing deployment into `ErrSessionAuthorityUnread` and refuse to start. That is
// why the concrete type never leaves this function's `if`.
//
// 🔴 A CACHE, NOT THE `FileStore` ITSELF, FOR THE REASON `control.Cache` EXISTS: reads
// must not touch the authority. And the cache's source is `control.ReloadingSource`,
// because the writer is a different process — see that type's comment, which is the
// difference between a pod that sees a provisioned user and one that materialized the
// journal once at startup.
func openSessionAuthority(ctx context.Context, env map[string]string, warn func(string)) (identity.ModelSource, error) {
	journal, err := controlJournalPath(env)
	if err != nil {
		return nil, err
	}
	if journal == "" {
		return nil, nil
	}
	store, err := control.OpenFileStore(journal)
	if err != nil {
		return nil, err
	}
	cache := control.NewCache(control.ReloadingSource{Store: store},
		control.CacheOptions{MaxAge: api.AuthorityMaxAge})
	// 🔴 MATERIALIZED BEFORE THE LISTENER ACCEPTS, AND A FAILURE IS FATAL — the same
	// asymmetry the Supabase key set draws between a cold start and an outage. A cache
	// that has never materialized authorises NOBODY (`control.NewCache`'s comment), so a
	// pod that came up over an unreadable journal would refuse every session while
	// looking healthy, and the operator's only signal would be users reporting 401s.
	// After one success there is last-known-good to serve and a later read failure must
	// not stop sessions already issued.
	if err := cache.Refresh(ctx); err != nil {
		return nil, err
	}
	if len(cache.Model().Users) == 0 {
		// ⚠ A WARNING RATHER THAN A REFUSAL, AND THE ASYMMETRY IS ABOUT THE ORDER AN
		// OPERATOR CAN ACTUALLY WORK IN. `control.OpenFileStore` CREATES the file if it is
		// absent, so a typo'd path yields an empty journal that authorises nobody — which
		// is worth shouting about. But an empty journal is also the legitimate state
		// between enabling this setting and running `-create-user`, and in a cluster the
		// pod starts before anybody can exec into it, so refusing would be a crash loop
		// with no way out.
		warn(fmt.Sprintf(
			"subsystem-store-api: WARNING the control journal %s holds NO users, so every browser and "+
				"proxy sign-in will be refused. If that path is a typo this file was just created empty. "+
				"Provision with `cairn-server -create-user`",
			journal))
	}
	// 🔴 THE TIMER *AND* SIGHUP, WHICH ARE DIFFERENT CLAIMS. The timer bounds how long an
	// out-of-band write stays invisible with nobody asking; SIGHUP is how an operator who
	// just ran `-create-user` stops waiting. `control.RefreshTriggers` records that two
	// `signal.Notify` channels BOTH receive one SIGHUP — measured — so registering here
	// does not take the signal away from the token reload.
	//
	// 🔴 REGISTERED HERE AND NOT INSIDE THE GOROUTINE, for the same reason `installReload`
	// is installed before the listener accepts: a signal arriving before the goroutine is
	// scheduled would be delivered to a channel nobody had registered yet, and a LOST
	// reload is silent — the operator's `kill -HUP 1` exits 0 having done nothing.
	signals := sighupChannel()
	go func() {
		if err := cache.Run(ctx, control.RefreshTriggers{
			Interval: refreshInterval,
			Signals:  signals,
		}); err != nil && !errors.Is(err, context.Canceled) {
			warn("subsystem-store-api: control journal refresh loop stopped: " + err.Error())
		}
	}()
	return cache, nil
}
