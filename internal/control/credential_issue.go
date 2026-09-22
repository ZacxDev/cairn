package control

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"
)

// TokenEntropyBytes is 32 — 256 bits from `crypto/rand`, which
// `base64.RawURLEncoding` renders as exactly `TokenChars` characters.
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

// TokenChars is what `TokenEntropyBytes` renders to under `base64.RawURLEncoding`.
//
// DERIVED, NEVER A SECOND LITERAL: base64 emits one character per 6 bits and
// `RawURLEncoding` adds no padding, so the width is ceil(bytes*8/6) and writing `43`
// beside `32` would be two spellings of one fact that drift the first time the entropy
// moves.
const TokenChars = (TokenEntropyBytes*8 + 5) / 6

// ErrNoSuchPrincipal refuses a credential aimed at an entity this journal does not hold.
//
// ⚠ IT IS AN EARLY REFUSAL, NOT A SECOND AUTHORITY, AND THE DISTINCTION IS THE WHOLE
// REASON THE SENTINEL EXISTS RATHER THAN THE CHECK ALONE. `Model.apply` refuses the same
// batch under `Append`'s `flock` — that is the authoritative rule and it holds for every
// writer, including one that never comes through this function. What this buys is stated
// at `IssueCredential`: the refusal happens BEFORE a secret is minted, and the caller can
// tell "you named a principal that is not there" from "the journal would not write",
// which through `Append` are one opaque `fmt.Errorf`.
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
	// 🔴 BEFORE THE MINT, NOT MERELY BEFORE THE APPEND, AND THAT ORDERING IS THE POINT.
	// `apply` refuses this same batch under the lock, so as a CORRECTNESS check this is
	// redundant and is not claimed otherwise. What it buys is that a request which cannot
	// succeed never causes a secret to exist at all: a token minted for a doomed append
	// lives in this process's heap, in whatever the caller does on its error path, and in
	// any core dump taken afterwards — for a request the journal was always going to
	// refuse. The second thing it buys is the SENTINEL: through `Append` a bad subject and
	// a failed `write(2)` are both an opaque `fmt.Errorf`, and an operator surface needs to
	// tell "you typed the wrong id" from "the volume is gone".
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
	// `rand.Read` on modern Go cannot partially fill — it either returns the full read or
	// an error — but a caller-visible refusal is the only safe answer either way: a token
	// assembled from a short or absent entropy read is a credential whose secrecy nobody
	// measured, written into an append-only authority with no undo. The same reasoning as
	// `NewID`, one level up in consequence: an id is an unguessable handle, this is the
	// secret itself.
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
		Label:     req.Label,
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
		token:      token,
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
	// secret, and never derived from the token: it is stored verbatim in the journal, which
	// is an operator-readable file.
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
// 🔴 THE TOKEN IS AN UNEXPORTED FIELD BEHIND `Token()`, AND `String`/`GoString` REDACT,
// BECAUSE "REMEMBER NOT TO PRINT THIS" IS NOT A MECHANISM. The realistic leak is not a
// caller deciding to log a secret; it is `%v` or `%+v` of a struct that happens to contain
// one — in an error path, a debug line, a test failure message — which is the shape
// `authz.RedactedField` exists for on the token-file side. Implementing `Stringer` means
// `%v`, `%+v`, `%s` and `%q` all route through a redacted rendering, and `GoStringer`
// covers `%#v`, which is the verb a reader reaches for precisely when they want to see
// everything.
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
//     `token_hash` that is not a lowercase hex digest.
type Issued struct {
	// Credential is the id of the record written to the journal.
	Credential ID
	// TokenHash is the digest that was stored. NOT secret: it is in the journal, which an
	// operator reads, and it is what a `grep` finds the record by.
	TokenHash string
	// Epoch is the journal epoch after the append — the number `control.Cache` compares to
	// decide whether a pod is serving this credential yet.
	Epoch uint64

	// token is the raw bearer token. Unexported so that no formatting verb, no struct
	// literal comparison in a test failure and no encoder can reach it without going
	// through `Token()`.
	token string
}

// Token returns the raw bearer token, which exists in this process and nowhere else.
//
// 🔴 THE JOURNAL HOLDS ONLY THE DIGEST, SO THIS IS THE ONLY TIME IT CAN EVER BE READ.
// Nothing in this repository can recover a token from a `credential-issued` record —
// that is the property the whole design rests on — so a caller that loses it has to
// issue another one and revoke this.
func (i Issued) Token() string { return i.token }

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
