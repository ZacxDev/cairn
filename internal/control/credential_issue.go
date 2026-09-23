package control

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"time"
)

// TokenEntropyBytes is 32 — 256 bits from `crypto/rand`, which
// `base64.RawURLEncoding` renders as exactly `tokenChars` characters.
//
// 🔴 43 IS NOT A WIDTH CHOSEN HERE; IT IS THE FLOOR TWO OTHER SURFACES ALREADY REFUSE
// BELOW, SO MINTING AT IT MAKES THE SURFACES AGREE BY CONSTRUCTION RATHER THAN BY
// REVIEW. `authz.MinTokenChars` is 43 and the pod refuses to START on a token-file row
// shorter than that ("a store that came up with a weak token is worse than one that did
// not come up at all"); `cmd/cairn-ui` inherits the same refusal through the same
// loader. A credential minted narrower here would be a credential this repository's own
// programs reject as guessable — issued by the tool whose whole job is to produce a
// working one.
//
// ⚠ THE AGREEMENT IS PINNED BY A TEST AND DELIBERATELY NOT BY AN IMPORT. `internal/control`
// holds no configuration and parses no token file (see the package doc: it stops at the
// library boundary), so importing the token-file parser into the authority model to share
// one integer would put the FILE FORMAT on the model's dependency list. The test is
// `TestTheMintedWidthAgreesWithTheTokenFileFloor`, which imports both and compares them —
// a seam guard, since neither package can see the other's constant.
const TokenEntropyBytes = 32

// tokenChars is what `TokenEntropyBytes` renders to under `base64.RawURLEncoding`.
//
// DERIVED, NEVER A SECOND LITERAL: base64 emits one character per 6 bits and
// `RawURLEncoding` adds no padding, so the width is ceil(bytes*8/6) and writing `43`
// beside `32` would be two spellings of one fact that drift the first time the entropy
// moves.
//
// ⚠ UNEXPORTED, BECAUSE NOTHING OUTSIDE THIS PACKAGE READS IT AND EXPORTING IT INVITED A
// SECOND READING OF THE WIDTH. It was exported for the seam guard in
// `credential_issue_test.go`, which lives in this package and needs no export, and every
// other surface that cares about the floor reads `authz.MinTokenChars` — the constant the
// seam guard compares this one to. An exported name with no production consumer reads as
// a supported knob.
const tokenChars = (TokenEntropyBytes*8 + 5) / 6

// ErrNoSuchPrincipal refuses a credential aimed at an entity this journal does not hold.
//
// ⚠ IT IS AN EARLY REFUSAL, NOT A SECOND AUTHORITY. `Model.apply` refuses the same batch
// under `Append`'s `flock` — that is the authoritative rule and it holds for every writer,
// including one that never comes through this function.
//
// 🔴 A SENTINEL IS ONLY A GUARD WHERE SOMETHING BRANCHES ON IT, AND FOR A WHILE NOTHING
// DID — `errors.Is(…, ErrNoSuchPrincipal)` had exactly one consumer tree-wide and it was
// this package's own test. The consumer that justifies it is
// `cmd/cairn-server/issuecredential.go`, which branches here to print a refusal naming the
// missing principal instead of passing through a message about replay. What it buys is a
// better FIRST LINE for an operator; `IssueCredential`'s own comment states what it does
// not buy, including two claims that were measured false.
var ErrNoSuchPrincipal = errors.New("control: no such principal in this control journal")

// IssueCredential mints a bearer token for an existing principal, records ONLY its
// digest in the journal, and hands the raw token back to the caller exactly once.
//
// 🔴 WHY IT EXISTS, STATED AS THE DEFECT IT CLOSES. `Authenticate`, `Credential`,
// `Narrow` and the digest-collision refusal have all been here since P3; what was
// missing was any path that WROTE an `EventCredentialIssued`. Measured on `origin/main`:
// `cmd/cairn-ui -control-journal <file>` refused to start with "0 credential record(s)"
// against a journal `cairn-server -create-user` had just written, because that path mints
// a user and no credential and nothing else in this repository minted one either. The
// browser surface's entire write half was unreachable by any path here — not because the
// model lacked a rule, but because nothing called it.
//
// 🔴 THE RAW TOKEN LEAVES THROUGH `Issued.Token()` AND THROUGH NOTHING ELSE. The journal
// receives `HashToken(token)` and nothing else; the token is never logged here, never
// interpolated into an error, and never a field any formatting verb can reach — see
// `Issued`'s own comment for the mechanism and, more importantly, for what that mechanism
// does NOT cover.
//
// 🔴 ONE BATCH, FOR THE REASON `ProvisionUser` STATES ONE LEVEL UP. It is a single event
// today, so "a half-issued credential" is not a state this path can produce anyway — but
// the shape is kept because the next thing anybody adds here is an issue-plus-grant, and a
// path that appends twice can leave a credential the operator was told about and a grant
// nobody has. `Append` validates the whole batch against a CLONE before a byte reaches
// disk.
//
// ⚠ WHAT IT DELIBERATELY DOES NOT DO. It does not GRANT anything: a credential carries its
// principal's authority, computed by `Resolve` at the moment it is asked, so issuing one to
// a principal that can reach nothing produces a credential that authenticates and sees
// nothing. That is a legitimate intermediate state (issue the credential, decide access
// after) and it is indistinguishable from the outside from a broken deployment, which is
// why `cmd/cairn-server -issue-credential` says so on stderr rather than this function
// refusing. It also does not REVOKE: `EventCredentialRevoked` exists and has no writer,
// which is the same gap this function closes one event over, and it is NOT closed here —
// rotation is a separate operator action with its own refusals to think about.
func IssueCredential(ctx context.Context, s Store, req NewCredential) (Issued, error) {
	at := req.At
	if at.IsZero() {
		// `FileStore.Append` stamps a zero `At` too, so this is belt-and-braces there —
		// but `Store` is an interface and a backend is not required to stamp, while
		// `Event.validate` refuses a zero time from every writer.
		at = time.Now().UTC()
	}

	current, err := s.Model(ctx)
	if err != nil {
		return Issued{}, fmt.Errorf("reading the control journal: %w", err)
	}
	// 🔴 WHAT THIS PRE-CHECK BUYS IS A BETTER FIRST LINE OF THE REFUSAL, AND THAT IS THE
	// WHOLE OF IT. `apply` refuses this same batch under the lock, so as a CORRECTNESS
	// check it is redundant and is not claimed otherwise. It returns `ErrNoSuchPrincipal`,
	// which `cmd/cairn-server -issue-credential` BRANCHES on to say "this journal holds no
	// user with id …" — an answer an operator can act on — instead of relaying a sentence
	// about a batch that would not replay.
	//
	// ⚠ TWO STRONGER CLAIMS STOOD HERE AND ARE RETRACTED, WRITTEN DOWN SO THE NEXT READER
	// DOES NOT RE-DERIVE THEM:
	//
	//   - "through `Append` a bad subject and a failed `write(2)` are both an opaque
	//     `fmt.Errorf`" is FALSE, measured on the two error paths: the first reads
	//     `… would not replay: subject user usr_x does not exist` and the second
	//     `control journal append: <errno>`. They were already plainly distinguishable as
	//     TEXT. What they were not was distinguishable by TYPE, which is what a caller
	//     needs to branch — and that, not legibility, is what the sentinel adds.
	//   - "a request that cannot succeed never causes a secret to exist" is TRUE and WEAK.
	//     A token that never reached the journal authenticates to nothing: there is no
	//     record carrying its digest, so its worst case is bytes in one process's heap,
	//     not an authority anybody can use.
	//
	// ⚠ `checkSubject`, NOT `PrincipalFor`, AND THE TWO ARE NOT THE SAME QUESTION.
	// `Authenticate` additionally requires `PrincipalFor` to know the principal, which is
	// existence PLUS a non-empty `Display`. That second half is unreachable from any
	// journal a `FileStore` will replay — `EventProjectCreated` requires a non-empty name
	// and `EventUserCreated` a non-empty provider and subject, and no event kind deletes or
	// renames a principal — so checking it here would be an unreachable branch pretending
	// to be a guard. The three constraints that make it unreachable are enumerated at
	// `cmd/cairn-ui`'s `TestAJournalWhoseCredentialsAreALLREVOKEDIsRefused`, and if any of
	// them moves, this choice moves with it.
	if err := current.checkSubject(req.SubjectKind, req.SubjectID); err != nil {
		return Issued{}, fmt.Errorf(
			"%w: %s — a credential names its principal by id, and a credential bound to an entity the journal does not hold would authenticate to nobody: `Authenticate` refuses it at `PrincipalFor` and answers exactly as an unknown token does, so the mistake would surface as a credential that simply never works",
			ErrNoSuchPrincipal, err)
	}

	credentialID, err := NewID(PrefixCredential)
	if err != nil {
		return Issued{}, err
	}

	// 🔴 `crypto/rand`, AND THE FAILURE IS FATAL TO THE CALL RATHER THAN FALLING BACK.
	//
	// ⚠ ON THE PINNED TOOLCHAIN THIS BRANCH IS UNREACHABLE, AND THE PREVIOUS DESCRIPTION OF
	// IT WAS WRONG RATHER THAN MERELY OPTIMISTIC. It said `rand.Read` "cannot partially
	// fill — it either returns the full read or an error". Since Go 1.24 `crypto/rand.Read`
	// ALWAYS fills the buffer and ALWAYS returns a nil error: an irrecoverable failure of
	// the system source is a PANIC inside the package, not a value this call can inspect.
	// `go.mod` says 1.25 and `flake.nix` builds with `buildGo125Module`, so on every
	// toolchain this repository pins, `err` here is nil.
	//
	// 🔴 THE BRANCH STAYS, AND NOT AS DECORATION. `rand.Read` keeps the `(int, error)`
	// signature, so dropping the check means writing `_, _ = rand.Read(buf)` — a line that
	// silently accepts a short fill on any build where the guarantee does not hold, which
	// includes an older toolchain somebody vendors this into. A caller-visible refusal is
	// the only safe answer either way: a token assembled from a short or absent entropy
	// read is a credential whose secrecy nobody measured, written into an append-only
	// authority with no undo. The same reasoning as `NewID`, one level up in consequence:
	// an id is an unguessable handle, this is the secret itself.
	buf := make([]byte, TokenEntropyBytes)
	if _, err := rand.Read(buf); err != nil {
		return Issued{}, fmt.Errorf("minting a credential token: %w", err)
	}
	// `RawURLEncoding`: URL-safe alphabet and NO padding. Padding would put `=` in a value
	// that travels in an `Authorization` header, in a shell word and in a file the pod
	// parses line by line, and the `=` carries no information here — the length is fixed by
	// `TokenEntropyBytes`.
	token := base64.RawURLEncoding.EncodeToString(buf)
	digest := HashToken(token)

	after, err := s.Append(ctx, Event{
		Kind: EventCredentialIssued, At: at, Actor: req.Actor,
		CredentialID: credentialID,
		SubjectKind:  req.SubjectKind, SubjectID: req.SubjectID,
		TokenHash: digest,
		// The label is passed through unexamined; see `NewCredential.Label` for what
		// `encoding/json` then does to a value that is not valid UTF-8.
		Label: req.Label,
		// `copyIDs`, NOT `append([]ID(nil), …)`: the latter flattens a non-nil EMPTY
		// narrowing into nil, which means NO narrowing and is its exact opposite. A
		// credential issued to see nothing would see everything its principal does, and the
		// flattening happens inside a line that reads like defensive copying.
		NarrowedScopes: copyIDs(req.NarrowedScopes),
	})
	if err != nil {
		return Issued{}, err
	}
	return Issued{
		Credential: credentialID,
		TokenHash:  digest,
		Epoch:      after.Epoch,
		token:      &token,
	}, nil
}

// NewCredential is the one credential an operator is issuing.
type NewCredential struct {
	// SubjectKind and SubjectID name the principal this credential authenticates AS —
	// a user, or a project acting as a service account. Both required, and the pair must
	// already exist in the journal.
	//
	// ⚠ A PROJECT PRINCIPAL HAS NO AUTHORITY OVER ITS OWN PROJECT'S SCOPES. A project is
	// not a member of itself, so a service account reaches a scope only through an explicit
	// grant — the narrow answer `internal/control/README.md` records as a deliberate
	// ruling, and the single most likely surprise for whoever issues the first CI
	// credential.
	SubjectKind Kind
	SubjectID   ID
	// Label is what a human calls this credential in a UI or a rotation runbook. NEVER
	// secret, and never derived from the token: it is stored UNEXAMINED in the journal,
	// which is an operator-readable file.
	//
	// ⚠ "VERBATIM" IS WHAT THIS COMMENT USED TO SAY AND IT IS FALSE, MEASURED RATHER THAN
	// REASONED. The journal is newline-delimited JSON and `encoding/json` COERCES a string
	// that is not valid UTF-8, replacing each bad byte with U+FFFD: measured on the pinned
	// toolchain, `"a\xff\xfeb"` is written as `"a��b"` and reads back as the
	// two-replacement-character string, which is NOT the value that was passed. Every other
	// operator-supplied string in the journal — a project name, a scope display name, an
	// email — travels the same encoder and is coerced the same way.
	//
	// ⚠ AND THE COERCION IS IDEMPOTENT ON THE VALUE, NOT ON THE BYTES. Re-encoding what
	// came back yields the same STRING, so replaying a journal repeatedly never drifts
	// further; the JSON bytes do differ (the first encode escapes as `�`, the second
	// writes the character literally), which matters only to a byte-for-byte comparison of
	// two files written by different paths.
	//
	// 🔴 IT IS NOT REJECTED HERE, AND THAT IS A DECISION. A refusal at issue time would be
	// a rule about the DURABLE FORMAT spelled at one of the several string fields that
	// format carries — the one-rule-many-places shape this package exists to refuse — and
	// what it would buy is nothing an authority depends on: no code branches on a label,
	// `Authenticate` never reads one, and a coerced label cannot widen or narrow what any
	// credential reaches. If a byte-exact label ever matters, the check belongs in
	// `Event.validate` over every string field at once, not at this one caller.
	Label string
	// NarrowedScopes restricts this credential to a subset of what its principal can reach.
	//
	// 🔴 `nil` MEANS NO NARROWING; A NON-NIL EMPTY SLICE MEANS NOTHING IS VISIBLE. They are
	// opposites, the journal format preserves the distinction deliberately (see
	// `Event.NarrowedScopes`), and `Narrow` applies it by INTERSECTION at authenticate time
	// — so naming a scope the principal does not have confers nothing, now or later.
	NarrowedScopes []ID
	// Actor is recorded as the `actor` of the event. Empty is accepted: an operator running
	// a command in a pod has no `control.ID` of their own to name.
	Actor ID
	// At stamps the event. Zero means `time.Now().UTC()`.
	At time.Time
}

// Issued is what `IssueCredential` created. It is the ONLY thing in this package that
// ever holds a raw bearer token, and it holds it for exactly as long as the caller keeps
// the value.
//
// 🔴 THE TOKEN IS AN UNEXPORTED FIELD BEHIND `Token()`, AND THE REDACTION IS
// `fmt.Formatter`, BECAUSE "REMEMBER NOT TO PRINT THIS" IS NOT A MECHANISM. The realistic
// leak is not a caller deciding to log a secret; it is `%v` or `%+v` of a struct that
// happens to contain one — in an error path, a debug line, a test failure message — which
// is the shape `authz.RedactedField` exists for on the token-file side.
//
// 🔴 AND `Stringer` ALONE WAS MEASURED INSUFFICIENT, WHICH IS WHY `Format` EXISTS BESIDE
// IT RATHER THAN A COMMENT SAYING SO. `fmt` consults `Stringer` for `%v %s %q %x %X` and
// `GoStringer` for `%#v`, and REFLECTS the operand for every other verb — and a reflected
// struct prints its unexported field's VALUE, inside `%!d(string=…)`. Measured over
// `Issued` and `*Issued` at the commit that shipped the `Stringer`-only redaction:
// **14 of 22 verbs rendered the raw token** (`%d %b %o %O %c %U %e %E %f %F %g %G %t %p`).
// `fmt` consults a `Formatter` BEFORE `Stringer` and for EVERY verb, so implementing it is
// what makes the redaction as wide as its own description.
//
// 🔴 A `Format` METHOD ON THE *FIELD'S* TYPE WOULD NOT HAVE WORKED, AND THAT IS THE SAME
// MECHANISM THAT PRODUCED THE LEAK. `fmt`'s reflection walker consults a value's
// formatting methods only when it can `Interface()` that value, and a field reached by
// reflection through an UNEXPORTED name cannot be interfaced — so no method on the field's
// type is ever called, whatever the type is. The defence has to sit on the STRUCT, which
// `fmt` receives as a whole operand.
//
// 🔴 AND THE TOKEN LIVES BEHIND A POINTER FOR THE ONE VERB `Formatter` DOES NOT REACH.
// `fmt` handles `%T` and `%p` before any formatting interface, and `%p` of a NON-pointer
// operand falls into `badVerb`, which sets `erroring` and then re-renders the operand as
// `%v` — and the method dispatch returns early while `erroring`, so `Format` is skipped and
// the struct is reflected. Measured: `Format` alone leaves `%p` leaking, 1 of 22. A pointer
// FIELD is rendered as an ADDRESS rather than followed (`fmt` dereferences only at depth
// 0), so `*string` closes it: 0 of 22. Both halves are load-bearing, and
// `TestNoRenderingOfIssuedContainsTheToken` is the sweep that measures all three numbers.
//
// ⚠ `go vet` IS A REAL MITIGATION AND NOT A SUFFICIENT ONE, WHICH IS WHY THE TYPE HAS TO
// DEFEND ITSELF. Measured: vet's printf check flags `fmt.Sprintf("%d", issued)` when the
// format is a CONSTANT, and says nothing about the same call with the format in a variable
// — and a format string assembled or passed at runtime is exactly what a logging helper
// has.
//
// 🔴 AND HERE IS WHAT IT DOES NOT COVER, BECAUSE A GUARD'S DESCRIPTION HAS TO BE AS WIDE
// AS ITS BODY:
//
//   - `Token()` itself. Once a caller takes the string it is theirs, and printing it is
//     exactly what `cairn-server -issue-credential` does on purpose. The redaction makes
//     the leak DELIBERATE, not impossible.
//   - `encoding/json` and every other reflection-based encoder that skips unexported
//     fields. Those drop the token silently rather than leaking it — the safe direction,
//     but it means marshalling an `Issued` produces a record with no token in it, which is
//     a different surprise.
//   - A debugger, a core dump, or a reflection walker that reads unexported memory. This
//     is a hygiene property of one process's formatting, not a confidentiality boundary.
//   - The journal, which never sees it at all — that is enforced upstream, by
//     `IssueCredential` passing `HashToken(token)` and by `Event.validate` refusing a
//     `token_hash` that is not a 64-character hex digest.
//   - Any formatter that is not `fmt`. `Format` is an agreement with one package; a
//     structured logger, a template engine or a debugger's pretty-printer that walks
//     fields by reflection is bound only by the pointer indirection, which hides the
//     value behind an address rather than refusing to render it.
type Issued struct {
	// Credential is the id of the record written to the journal.
	Credential ID
	// TokenHash is the digest that was stored. NOT secret: it is in the journal, which an
	// operator reads, and it is what a `grep` finds the record by.
	TokenHash string
	// Epoch is the journal epoch after the append — the number `control.Cache` compares to
	// decide whether a pod is serving this credential yet.
	Epoch uint64

	// token is the raw bearer token. Unexported so that no struct literal comparison in a
	// test failure and no encoder can reach it without going through `Token()`, and a
	// POINTER so that `fmt`'s reflection walker renders it as an address — see this type's
	// own comment for the `%p` measurement that makes the indirection load-bearing rather
	// than a style choice. nil is the zero value, and `Token()` answers "" for it.
	token *string
}

// Token returns the raw bearer token, which exists in this process and nowhere else.
//
// 🔴 THE JOURNAL HOLDS ONLY THE DIGEST, SO THIS IS THE ONLY TIME IT CAN EVER BE READ.
// Nothing in this repository can recover a token from a `credential-issued` record —
// that is the property the whole design rests on — so a caller that loses it has to
// issue another one and revoke this.
//
// ⚠ A ZERO `Issued` ANSWERS "" RATHER THAN PANICKING. Every failure return in this file is
// `Issued{}, err`, so the zero value is a shape callers genuinely hold; a nil dereference
// there would turn a refusal into a crash.
func (i Issued) Token() string {
	if i.token == nil {
		return ""
	}
	return *i.token
}

// String renders an Issued for a log line, WITHOUT the token.
//
// The digest is included deliberately: it is not secret, it is already in the journal,
// and it is the only handle that lets an operator find the record this value describes.
func (i Issued) String() string {
	return fmt.Sprintf("credential=%s digest=%s epoch=%d token=<redacted: shown once, on issue>",
		i.Credential, i.TokenHash, i.Epoch)
}

// GoString covers `%#v` — the verb somebody reaches for when they specifically want to
// see every field, which is exactly when a redaction that only handled `%v` would fail.
func (i Issued) GoString() string {
	return "control.Issued{" + i.String() + "}"
}

// Format is the redaction that covers EVERY verb, which `String` and `GoString` between
// them do not.
//
// 🔴 IT EXISTS BECAUSE `Stringer`/`GoStringer` ARE CONSULTED FOR SIX VERBS AND EVERY OTHER
// VERB REFLECTS THE OPERAND — **14 of 22 rendered the raw token** before this method
// existed. (`%T` is the one of the remaining sixteen that never could: it prints a type
// name and nothing else.) The type's own comment carries that measurement, why a `Format`
// on the FIELD's type could not have worked, and why the token additionally lives behind a
// pointer.
//
// ⚠ THE SIX VERBS `Stringer`/`GoStringer` ALREADY HANDLED KEEP THEIR EXACT OUTPUT, AND
// THAT IS DELIBERATE RATHER THAN INCIDENTAL. `fmt` hands a `Stringer`'s result back to the
// SAME verb — so `%q` of an `Issued` was a quoted string and `%x` was the hex of one — and
// a `Format` that wrote the plain rendering under every verb would silently change six
// call sites' output while closing sixteen. `%q`, `%x` and `%X` are therefore re-rendered
// through `fmt` rather than written raw.
//
// ⚠ WIDTH AND PRECISION FLAGS ARE DROPPED, WHICH IS A REAL NARROWING AND IS ACCEPTED —
// MEASURED, NOT ASSUMED: `%20v` of a `Stringer` pads to twenty columns and `%20v` of this
// type does not. Reproducing every flag means rebuilding the format
// string out of `fmt.State`, which is a second implementation of `fmt`'s own parser living
// inside a redaction; what this type is formatted into is a log line, and no caller in
// this repository pads one.
func (i Issued) Format(f fmt.State, verb rune) {
	rendered := i.String()
	if verb == 'v' && f.Flag('#') {
		rendered = i.GoString()
	}
	switch verb {
	case 'q', 'x', 'X':
		fmt.Fprintf(f, "%"+string(verb), rendered)
	default:
		io.WriteString(f, rendered)
	}
}

// A compile-time proof that the redaction is the WIDE one. `Stringer` is consulted for six
// verbs and `Formatter` for every verb, so losing this interface is a silent return to a
// 14-of-22 leak that every existing call site still renders "correctly".
var _ fmt.Formatter = Issued{}
