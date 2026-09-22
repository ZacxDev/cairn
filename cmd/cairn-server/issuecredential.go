package main

// `cairn-server -issue-credential`: the operator-driven credential-issuing path.
//
// 🔴 IT CLOSES A MEASURED DEFECT, NOT A MISSING FEATURE. Before it, this repository had a
// complete credential MODEL — `control.Authenticate`, the narrowing, the digest-collision
// refusal, `EventCredentialIssued` in the closed event set — and nothing that WROTE one.
// The observable was at the other end of the tree: `cairn-ui -control-journal <file>`
// refused to start against a journal `-create-user` had just written, saying "0 credential
// record(s) … no tool in this repository writes a credential into a journal yet". The
// browser surface's write half was unreachable by any path here.
//
// 🔴 A COMMAND, NOT A ROUTE, FOR EXACTLY THE REASON `-create-user` IS ONE, AND MORE SO.
// `AGENTS.md`: "Adding a row to a dispatch table is adding a public, internet-reachable
// endpoint." A route that mints bearer tokens is the highest-value target this pod could
// possibly grow, and it would have to move the route ledger, the conformance corpus, the
// construction-time check and `checks.go-server-declares-its-routes` together — for a
// capability whose one caller is a human with `exec` into the pod. `api.DeclaredRoutes()`
// is unchanged by this file, which is the checkable half of that sentence.
//
// 🔴 AND IT IS NOT A ROTATION TOOL. It issues; it does not revoke. `EventCredentialRevoked`
// still has no writer in this repository, so a rotation here is "issue the new one, then
// hand-append the revocation" — stated because an operator who assumes otherwise leaves the
// old credential live, and because the gap is the same shape as the one this file closes.

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/ZacxDev/cairn/internal/control"
)

// issueCredentialFlags is the mode's own flag set, registered by `main` before
// `flag.Parse` — the same shape `createUserFlags` has, and for the same reason: a
// subcommand would be a second `flag.FlagSet` and a second parse of `os.Args`, which is a
// second place the program decides what it was asked to do.
type issueCredentialFlags struct {
	enabled       *bool
	principalKind *string
	principal     *string
	label         *string
	narrowScopes  *string
}

// registerIssueCredentialFlags declares the mode's flags.
//
// ⚠ PLAIN FLAGS ON THE SERVER BINARY, MATCHING `-routes` AND `-create-user`. See
// `registerCreateUserFlags` for the ruling: a subcommand would be a second `flag.FlagSet`
// and a second parse of `os.Args`.
func registerIssueCredentialFlags() *issueCredentialFlags {
	return &issueCredentialFlags{
		enabled: flag.Bool("issue-credential", false,
			"MINT A BEARER TOKEN for an existing principal in the control journal named by "+
				"$"+EnvControlJournal+", write only its SHA-256 digest there, print the token ONCE "+
				"to stdout, and exit. The token cannot be recovered afterwards by anything in this "+
				"repository. Requires -principal"),
		principalKind: flag.String("principal-kind", string(control.KindUser),
			"`user` or `project` — which kind of principal this credential authenticates AS. A "+
				"project principal is the service-account case, and note that a project is NOT a "+
				"member of itself: it reaches a scope only through an explicit grant"),
		principal: flag.String("principal", "",
			"the principal's id, as printed by -create-user (`usr_…`, or `prj_…` with "+
				"-principal-kind project). NOT a provider subject and not an email: a credential "+
				"binds to the control plane's own immutable id"),
		label: flag.String("label", "",
			"what a human calls this credential in a rotation runbook. Stored VERBATIM in the "+
				"journal, which an operator reads, so it must never be secret and never derived "+
				"from the token"),
		narrowScopes: flag.String("narrow-scopes", "",
			"comma-separated scope IDS (`scp_…`) this credential is restricted to, INTERSECTED "+
				"with whatever its principal can reach at the moment it is asked. Empty means NO "+
				"narrowing — the credential carries its principal's full authority"),
	}
}

// runIssueCredential performs the issue and returns the process exit code.
//
// 🔴 IT RETURNS A CODE RATHER THAN CALLING `os.Exit`, so a test can run it in-process and
// read the journal back in the same function — the same ruling `runCreateUser` records.
//
// 🔴 THE TOKEN GOES TO STDOUT **ALONE**, AND EVERY WORD OF PROSE GOES TO STDERR. That is a
// deliberate divergence from `-create-user`, which puts a machine-readable `cairn-control:`
// line on stdout, and the reason is that here the machine-readable thing IS the secret:
//
//   - `cairn-server -issue-credential … > token` has to produce a usable credential file
//     and nothing else. The pod's token file is one token per line, so a stdout carrying
//     prose alongside the token would produce a file the pod then refuses to parse — and
//     the operator's remedy would be to hand-edit a file containing a live secret.
//   - Prose on the same stream is what makes a human select-and-copy MORE than the token,
//     and a token with a stray line around it fails authentication in a way that looks like
//     a bad credential rather than a bad paste.
//
// ⚠ AND PRINTING IT AT ALL IS A REAL COST, SAID WHERE THE OPERATOR READS IT RATHER THAN
// ONLY HERE. A secret on stdout is re-staged in every transcript that captures the run:
// shell scrollback, `script`, a CI log, a `kubectl exec` recording, an agent session.
// There is no alternative that keeps this a single command — the token exists for one
// instant in one process by construction — so the honest move is to name the exposure and
// say what to do about it, which the stderr block below does.
func runIssueCredential(env map[string]string, f *issueCredentialFlags, out, errOut io.Writer) int {
	warn := func(line string) { fmt.Fprintln(errOut, reloadSafe(line)) }

	journal, err := controlJournalPath(env)
	if err != nil {
		warn("subsystem-store-api: " + err.Error())
		return exitConfig
	}
	if journal == "" {
		// 🔴 REFUSED RATHER THAN DEFAULTED TO A PATH, for the reason `-create-user` states:
		// a default would put a credential somewhere the running pod is not reading, and the
		// operator's evidence that it "worked" would be a token nothing can authenticate.
		warn(fmt.Sprintf(
			"subsystem-store-api: -issue-credential needs a control journal and $%s is not set. Set it "+
				"to the path this pod reads its journal from — a credential written anywhere else is a "+
				"token no running server can resolve",
			EnvControlJournal))
		return exitConfig
	}

	kind, err := principalKindOf(*f.principalKind)
	if err != nil {
		warn("subsystem-store-api: -issue-credential refused: " + err.Error())
		return exitConfig
	}
	principal := control.ID(strings.TrimSpace(*f.principal))
	if principal == "" {
		warn("subsystem-store-api: -issue-credential refused: -principal is required. A credential " +
			"binds to a principal by id; there is no default and no 'the only user', because a " +
			"journal with one user today has two tomorrow and a default would silently follow")
		return exitConfig
	}
	// 🔴 A PREFIX CHECK, WHICH IS A BETTER MESSAGE AND NOT A SECOND AUTHORITY. The journal
	// refuses `-principal-kind user -principal prj_…` anyway — `checkSubject` looks the id up
	// in `m.Users` and does not find it — but it does so with "subject user prj_… does not
	// exist", which sends the operator hunting for a missing record when what they have is a
	// mismatched flag pair. `control.MustPrefix`'s own comment invites exactly this use: a
	// cheap total check that one entity's id has not been passed where another's belongs,
	// and explicitly not a permission check.
	if want := prefixFor(kind); !control.MustPrefix(principal, want) {
		warn(fmt.Sprintf(
			"subsystem-store-api: -issue-credential refused: -principal-kind %s expects an id starting "+
				"%s_, and %q does not. The kind and the id are two halves of one fact here; the journal "+
				"would refuse this as a principal that does not exist, which reads as a missing record "+
				"rather than as a mismatched pair",
			kind, want, principal))
		return exitConfig
	}

	narrowed, err := narrowedScopeIDs(*f.narrowScopes)
	if err != nil {
		warn("subsystem-store-api: -issue-credential refused: " + err.Error())
		return exitConfig
	}

	store, err := control.OpenFileStore(journal)
	if err != nil {
		warn("subsystem-store-api: " + err.Error())
		return exitConfig
	}

	ctx := context.Background()
	issued, err := control.IssueCredential(ctx, store, control.NewCredential{
		SubjectKind:    kind,
		SubjectID:      principal,
		Label:          strings.TrimSpace(*f.label),
		NarrowedScopes: narrowed,
	})
	if err != nil {
		// The journal's own message names the event and the field, so it is passed through
		// rather than re-worded — a second wording here would be a second description of a
		// rule that lives in `internal/control`. 78 is this program's one refusal code; see
		// `exitConfig` for why the second one was deleted rather than narrowed.
		warn("subsystem-store-api: -issue-credential refused: " + err.Error())
		return exitConfig
	}

	// 🔴 THE TOKEN, ALONE, ON STDOUT. Nothing is interpolated around it and `reloadSafe` is
	// deliberately NOT applied: it maps control characters to `?`, which would silently
	// CORRUPT a value the operator is about to authenticate with. It cannot be needed here —
	// the token is `base64.RawURLEncoding` output, whose alphabet is 64 printable ASCII
	// characters — and a sanitiser on a value that cannot contain what it sanitises is a
	// mechanism that can only ever damage the good case.
	fmt.Fprintln(out, issued.Token())

	// The record line, on stderr, keeping `-create-user`'s `cairn-control:` prefix so a
	// script greps the same shape. Only the stream differs, and the stream differs because
	// stdout belongs to the secret.
	warn(fmt.Sprintf(
		"cairn-control: issued credential=%s principal=%s:%s epoch=%d digest=%s label=%q narrowed=%s",
		issued.Credential, kind, principal, issued.Epoch, issued.TokenHash,
		*f.label, renderNarrowing(narrowed)))
	warn("subsystem-store-api: THE TOKEN IS ON STDOUT AND THIS IS THE ONLY TIME IT WILL EVER BE " +
		"SHOWN. Only its SHA-256 digest was written to the journal, and nothing in this repository " +
		"can recover a token from a digest — if it is lost, issue another and revoke this one by " +
		"its credential id")
	warn("subsystem-store-api: ⚠ printing a secret to stdout RE-STAGES IT in anything that captured " +
		"this run — shell scrollback, a CI log, a `kubectl exec` recording, a terminal multiplexer's " +
		"buffer. Prefer redirecting stdout straight to a file or a secret store " +
		"(`… -issue-credential … > token`), which is also exactly the one-token-per-line shape the " +
		"pod's -token-file expects")

	reportWhatTheCredentialCanReach(ctx, store, issued, narrowed, warn)

	// ⚠ THE RUNNING POD DOES NOT SEE THIS YET. This process wrote the file; the server
	// materializes its authority from a `control.Cache` on its TIMER, and SIGHUP is
	// deliberately not one of that loop's triggers (see `openSessionAuthority`). Saying so is
	// the difference between an operator who waits and one who concludes the token is broken
	// and issues a second.
	warn(fmt.Sprintf("subsystem-store-api: the running server picks this up within %s", refreshInterval))
	return 0
}

// reportWhatTheCredentialCanReach says out loud what this credential will actually see.
//
// 🔴 IT ASKS `control.Authenticate` WITH THE TOKEN IT JUST MINTED, RATHER THAN
// RE-DERIVING THE ANSWER. `Resolve` + `Narrow` composed here would be a SECOND spelling of
// "what may this credential see", which is the one-rule-one-place failure the whole
// `internal/control` package exists to refuse — and the second spelling would be the one
// this message is read from. Using the real authentication path makes this line a POSITIVE
// CONTROL as well as a report: a credential that cannot authenticate at the instant it was
// issued is a defect in this program, and it says so rather than exiting 0 on a hopeful
// assumption.
//
// 🔴 AND A CREDENTIAL THAT REACHES NOTHING IS A WARNING, NOT A REFUSAL — the same ruling
// `-create-user` records for a user with no scopes, for the same reason. Issuing a
// credential before deciding access is a real operator sequence (and a project principal
// reaches nothing by default, because a project is not a member of itself). It is also
// indistinguishable, from outside the pod, from the broken deployment this whole path
// closes, which is why it is a loud line rather than a silent success.
//
// ⚠ IT IS A CLAIM ABOUT THIS INSTANT AND NOTHING LATER. Authority is DERIVED — a grant
// added tomorrow widens this credential without reissuing it, and a revocation narrows it —
// so this reports the world the journal describes now, which is what an operator can check
// against what they intended.
func reportWhatTheCredentialCanReach(ctx context.Context, store *control.FileStore, issued control.Issued, narrowed []control.ID, warn func(string)) {
	m, err := store.Model(ctx)
	if err != nil {
		warn("subsystem-store-api: WARNING the credential was written and this command could not " +
			"re-read the journal to report what it reaches: " + err.Error())
		return
	}

	// An id in `-narrow-scopes` that names no scope in the journal is almost always a typo,
	// and its symptom is a credential that authenticates and sees nothing — the exact state
	// that reads as a broken sign-in. It is a WARNING rather than a refusal because `Narrow`
	// INTERSECTS: an unknown id confers nothing and can never confer anything, so it is
	// harmless to authorization and misleading to a human. Refusing it would also put a
	// validity claim at ISSUE time, which is precisely what `control.Narrow`'s own comment
	// rules out.
	var unknown []string
	for _, id := range narrowed {
		if _, held := m.Scopes[id]; !held {
			unknown = append(unknown, string(id))
		}
	}
	if len(unknown) > 0 {
		warn(fmt.Sprintf(
			"subsystem-store-api: WARNING -narrow-scopes names %s, which this journal holds no scope "+
				"for. A narrowing INTERSECTS, so an unknown id confers nothing — this credential is "+
				"narrower than you asked for, permanently, and scope ids are random rather than derived "+
				"so it will not start matching later",
			strings.Join(unknown, ", ")))
	}

	_, auth, err := control.Authenticate(m, issued.Token())
	if err != nil {
		warn(fmt.Sprintf(
			"subsystem-store-api: WARNING the credential %s was written to the journal and does NOT "+
				"authenticate against it (%v). That is a defect in this command, not a configuration "+
				"problem: the record is durable and append-only, so revoke it and report this",
			issued.Credential, err))
		return
	}
	reach := auth.NamedScopes(control.VerbRead)
	if len(reach) == 0 {
		warn(fmt.Sprintf(
			"subsystem-store-api: WARNING credential=%s can reach NOTHING: it authenticates and resolves "+
				"to an EMPTY authority, so every read answers as if the scope did not exist — which looks "+
				"identical to a broken credential from outside the pod. Grant its principal a scope, or "+
				"check -narrow-scopes. ⚠ A PROJECT principal reaches nothing by default: a project is not "+
				"a member of itself",
			issued.Credential))
		return
	}
	names := make([]string, 0, len(reach))
	for _, sc := range reach {
		names = append(names, string(sc.ID)+":"+sc.Name)
	}
	warn(fmt.Sprintf("subsystem-store-api: credential=%s reads %d scope(s): %s",
		issued.Credential, len(names), strings.Join(names, ",")))
}

// principalKindOf maps the flag string to a `control.Kind`.
//
// 🔴 NO DEFAULT-ACCEPT ARM AND NO FOLD. `control.Kind.Valid()` is the model's own closed
// set and this is the one place the operator's spelling meets it; accepting an unknown
// string and letting the journal refuse it later would produce "unknown subject kind" from
// a layer the operator never typed at. Case is NOT folded, because the kind is stored
// verbatim in an append-only record and `Kind.Valid()` compares raw — a journal line
// reading `"subject_kind":"User"` would replay as an unknown kind forever.
func principalKindOf(raw string) (control.Kind, error) {
	kind := control.Kind(strings.TrimSpace(raw))
	if !kind.Valid() {
		return "", fmt.Errorf(
			"-principal-kind %q is not a principal kind. It is %q or %q, lowercase — the value is "+
				"stored verbatim in an append-only record and the model compares it raw, so a "+
				"differently-cased spelling would replay as an unknown kind forever",
			raw, control.KindUser, control.KindProject)
	}
	return kind, nil
}

// prefixFor is the id prefix a principal of that kind carries.
//
// It is exhaustive over `control.Kind` with no default arm reachable from
// `principalKindOf`, which has already refused anything else. The empty return is the
// unreachable case, and it makes `MustPrefix` fail closed rather than pass vacuously —
// a `""` prefix would have `MustPrefix` compare against `"_"`, which no id carries.
func prefixFor(kind control.Kind) string {
	switch kind {
	case control.KindUser:
		return control.PrefixUser
	case control.KindProject:
		return control.PrefixProject
	}
	return ""
}

// narrowedScopeIDs splits the -narrow-scopes value into scope ids.
//
// 🔴 THE CLI CANNOT EXPRESS THE EMPTY NARROWING, AND THAT IS A DECISION RATHER THAN A
// LIMITATION NOBODY NOTICED. `control.NewCredential.NarrowedScopes` distinguishes `nil`
// (no narrowing — the principal's full authority) from a non-nil EMPTY slice (this
// credential sees nothing), and they are opposites. On a command line those two would be
// `-narrow-scopes` absent and `-narrow-scopes ""` — one typed character apart, and no
// operator reading the flag's help would predict which way round they go. So exactly ONE
// spelling maps to `nil`, the ABSENT flag (its zero value), and every other value that
// yields no ids — `""` passed explicitly, `" , , "`, a trailing comma alone — is REFUSED
// rather than silently read as either. The see-nothing credential stays reachable from the
// library and not from here; it has no operator use case, because a credential that sees
// nothing is what revoking one produces.
//
// ⚠ AND THE TWO CASES ARE ONE BRANCH, NOT TWO, BECAUSE THE FIRST DRAFT MADE THEM TWO AND
// GOT THE SECOND WRONG. It special-cased "blank after trimming" and left the yields-nothing
// case to a fall-through it called unreachable — so `-narrow-scopes " , , "` was refused
// with "yielded no ids" instead of the message that explains the nil/empty distinction, a
// worse answer sitting behind a comment asserting it could not happen. One check, at the
// end, over what the parse actually produced.
//
// ⚠ COMMAS ONLY, AND NOT WHITESPACE, matching `scopeNames` — but for a different reason
// worth stating rather than inheriting. A scope NAME may contain a space; a scope ID
// cannot (`control.NewID` emits prefix + `_` + Crockford base32). Splitting on whitespace
// would therefore be safe here and is still not done, because two flags on one command
// that split their lists differently is a rule with two spellings.
func narrowedScopeIDs(raw string) ([]control.ID, error) {
	if raw == "" {
		return nil, nil
	}
	var out []control.ID
	for _, part := range strings.Split(raw, ",") {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		id := control.ID(trimmed)
		if !control.MustPrefix(id, control.PrefixScope) {
			return nil, fmt.Errorf(
				"-narrow-scopes names %q, which is not a scope id. A narrowing addresses scopes by "+
					"their immutable ID (`%s_…`), never by the display name a reader sees — the name is "+
					"mutable and is not unique across projects, so narrowing by it would bind the "+
					"credential to whichever scope resolved that day. `-create-user` prints each created "+
					"scope as `<id>:<name>`",
				trimmed, control.PrefixScope)
		}
		out = append(out, id)
	}
	if len(out) == 0 {
		// Reached by every non-empty value that carries no id: `""` passed explicitly,
		// `" , , "`, a lone comma. It must not fall through as `nil`, which here would not
		// mean "no ids" but "NO NARROWING" — the opposite of what was asked for, and a silent
		// widening of the one flag whose purpose is to restrict.
		return nil, fmt.Errorf(
			"-narrow-scopes %q has no ids in it. It is not a way to say 'this credential sees "+
				"nothing' — OMIT the flag for no narrowing, and note that a see-nothing credential is "+
				"what revoking one produces. An empty value has to be refused rather than guessed at, "+
				"because the two readings ('unrestricted' and 'nothing') are opposites",
			raw)
	}
	return out, nil
}

// renderNarrowing describes the narrowing for the record line.
//
// `none` rather than an empty field, so the line distinguishes "no narrowing was asked
// for" from a value that failed to render — the same reason `renderScopes` spells out
// `none` on the `-create-user` line.
func renderNarrowing(ids []control.ID) string {
	if len(ids) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, string(id))
	}
	return strings.Join(parts, ",")
}
