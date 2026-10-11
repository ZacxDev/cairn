package memo

import (
	"crypto/rand"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/redact"
)

// The limits of decisions 2, 5, 6 and 9 of `claudedocs/plan-cairn-scope-mail.md`, each spelled
// ONCE. Every surface that sends or stores a memo reads these; none restates a number.
const (
	// MaxSubjectBytes and MaxBodyBytes cap a memo AFTER [Normalize] (decision 5).
	MaxSubjectBytes = 120
	MaxBodyBytes    = 4 * 1024
	// DefaultTTL is what a send without a TTL gets, and MaxTTL is the ceiling (decision 2;
	// the 3-day default is the token model's, the 30-day ceiling the invitation one).
	DefaultTTL = 3 * 24 * time.Hour
	MaxTTL     = 30 * 24 * time.Hour
	// RetentionAfterExpiry is how long a row outlives its expiry before a send prunes it. Its
	// EVENTS are kept (decision 2).
	RetentionAfterExpiry = 30 * 24 * time.Hour
	// SendsPerScopePerWindow sends to ONE scope inside QuotaWindow, all senders together — the
	// one quota (O12, decision 9).
	SendsPerScopePerWindow = 50
	QuotaWindow            = 24 * time.Hour
	// MaxLivePerScope bounds the unexpired, unretracted memos of one scope (decision 2), which
	// bounds every read the hook makes.
	MaxLivePerScope = 200
	// TransactionTimeout is what a send arms with `SET LOCAL transaction_timeout` as its
	// transaction's FIRST statement, BEFORE it reads the clock for `created_at` (decision 6).
	// It is what makes [CursorMargin] a bound: no committed row's `created_at` precedes its
	// commit by more than this.
	TransactionTimeout = 30 * time.Second
	// CursorMargin is how far below its high-water mark a client re-asks (decision 6): four
	// times [TransactionTimeout]. `TestTheCursorMarginCoversTheTransactionTimeout` pins the
	// relation, so moving one without the other is red.
	CursorMargin = 2 * time.Minute
)

// MaySend is THE send predicate (O2, decision 9): the pod's entry-write predicate,
// `VisibleScopes(write).Allows(name)`, and nothing else. Every send surface calls it, so a
// memo and an entry can never disagree about who may write a scope. A narrowed credential is
// already narrowed inside `a` (decision 17 included), so nothing here re-checks a narrowing.
func MaySend(a control.Authorization, scope string) bool {
	return a.VisibleScopes(control.VerbWrite).Allows(scope)
}

// MayRead is the read predicate (decision 12), asked NOW, on every read — so a withdrawn grant
// stops delivery immediately.
func MayRead(a control.Authorization, scope string) bool {
	return a.VisibleScopes(control.VerbRead).Allows(scope)
}

// MayRetract is the WHOLE retraction rule of decision 10 (O8), as ONE predicate — the store's
// `Retract`, the client listener (S2) and the browser's retract button (S5) all ask it, and
// none of them adds a clause of its own:
//
//   - the caller must be able to READ the scope NOW; and then
//   - be the SENDER — the principal whose `(kind, id)` sent it — through any credential (so an
//     agent credential narrowed elsewhere cannot retract here, and a sender whose write grant
//     was withdrawn still can, because retraction only reduces exposure); or
//   - hold `admin` on the scope — which, after decision 17, an agent credential never does, so
//     moderation is a human's act.
//
// 🔴 THE TWO RETRACTORS COMPOSE, AND THE COMPOSITION IS THE POINT: an agent credential of a
// principal who IS a scope admin retracts only that principal's OWN memos, because its `a`
// carries no admin.
//
// 🔴 READ GATES THE SENDER AND THE MODERATOR ALIKE — THE OPERATOR'S RULING (the plan's Q19),
// NARROWER THAN O8's WORDING. Through S1 the admin branch asked admin alone while the store
// additionally refused every caller who could not read the scope (uniform `not-found`, T12) — so
// the rule lived in two places and they disagreed for an ADMIN-ONLY grant, which the share form
// allows: this predicate said "may", the store answered `not-found`. S2 took the safe reading and
// the operator has since ruled it: a caller who cannot read a scope learns nothing about its memo
// ids, retraction included, so an admin-only grantee cannot retract (and cannot see the memo it
// would be retracting either). Recorded as Q19, and in the plan's "S2 — measurements and
// corrections" section.
func MayRetract(p control.Principal, a control.Authorization, scope, senderKind, senderID string) bool {
	if !MayRead(a, scope) {
		return false
	}
	if p.ID != "" && string(p.Kind) == senderKind && string(p.ID) == senderID {
		return true
	}
	return a.VisibleScopes(control.VerbAdmin).Allows(scope)
}

// RefusalReason is the CLOSED set of reasons a send or a retraction is refused with (each is
// exit 6 at the client, decision 8). It names a LIMIT, never content.
type RefusalReason string

const (
	RefusedNoWrite      RefusalReason = "no-write"
	RefusedQuota        RefusalReason = "quota"
	RefusedLiveLimit    RefusalReason = "live-limit"
	RefusedSubjectSize  RefusalReason = "subject-too-long"
	RefusedBodySize     RefusalReason = "body-too-long"
	RefusedCharacters   RefusalReason = "characters"
	RefusedSecret       RefusalReason = "secret"
	RefusedTTL          RefusalReason = "ttl"
	RefusedNotPermitted RefusalReason = "not-permitted"
)

// Refusal is a send or retraction this package's rules refused. 🔴 IT CARRIES NO MEMO TEXT:
// `Rule` is a redaction RULE NAME for [RefusedSecret] and empty otherwise — a `redact.Hit`
// carries no part of the secret by construction, and neither does this.
type Refusal struct {
	Reason RefusalReason
	Rule   string
}

func (r *Refusal) Error() string {
	if r.Rule != "" {
		return fmt.Sprintf("memo refused: %s (%s)", r.Reason, r.Rule)
	}
	return "memo refused: " + string(r.Reason)
}

// ErrNotFound is the ONE answer for a memo the caller may not read, an id that does not exist,
// an id in a different scope than the one asked for, and an expired one (decisions 4 and 8,
// threat T12): the pair (scope, id) must not tell a caller which scope an id belongs to.
var ErrNotFound = errors.New("memo: not found")

// confidentSecretRules is decision 11's CONFIDENT subset of `internal/redact`'s rule table: the
// rules that match a credential by its own SHAPE. A hit on one of these REFUSES a send.
//
// 🔴 A LIST OF RULE NAMES, PINNED AS A SET by `TestTheConfidentSubsetIsExactlyThisSet` (GROW or
// SHRINK fails), so a rule added to the redactor later is a deliberate decision to refuse on it.
// NOT here, deliberately: `key-context`, `entropy`, the CLI-flag, query-parameter and
// positional-file rules — the measured ~10% clean-line damage, which as a refusal would turn
// honest memos away at about that rate (plan, STEP 1).
var confidentSecretRules = map[string]bool{
	"pem-private-key": true, "aws-access-key-id": true, "github-token": true, "slack-token": true,
	"slack-webhook": true, "age-secret-key": true, "anthropic-key": true, "sk-key": true,
	"google-api-key": true, "stripe-key": true, "stripe-webhook-secret": true, "clickup-token": true,
	"gitlab-token": true, "npm-token": true, "huggingface-token": true, "jwt": true,
	"authorization": true, "bearer": true, "url-userinfo-password": true,
}

// ConfidentSecretRules lists the subset, sorted — for the test that pins it and for a surface
// that wants to say what the scan refuses on.
func ConfidentSecretRules() []string {
	out := make([]string, 0, len(confidentSecretRules))
	for name := range confidentSecretRules {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// NewScanner builds the redactor the send scan runs, under a FRESH 32-byte key from
// `crypto/rand` (decision 11). The scan reads only `Hit.Rule` and never stores, returns or logs
// a tag, so the key is held in memory only and a restart changing it changes nothing
// observable — no key file, nothing an operator provisions.
func NewScanner() (*redact.Redactor, error) {
	key := make([]byte, 32)
	_, _ = rand.Read(key) // crypto/rand.Read never returns an error (Go 1.24+).
	return redact.New(key, nil)
}

// Outgoing is a send that passed every content rule: the caller's OWN subject and body,
// normalised (`\r\n` folded to `\n`), and its TTL. It is INPUT on its way into storage — never
// stored text on its way out (Guard A's class "input").
type Outgoing struct {
	Subject, Body string
	TTL           time.Duration
}

// CheckSend applies every CONTENT rule a send must pass, in one place: the send-side fence
// predicates ([SubjectBreaksFence], [BreaksFence]), the size caps, the TTL bounds, and the
// confident-secret scan over subject AND body. A refusal is a [*Refusal] naming the limit.
//
// 🔴 A NIL SCANNER IS REFUSED, NOT SKIPPED. A send path that forgot to wire the scan would
// otherwise store secrets with every other check green (e2e sabotage (e)).
//
// ⚠ WHAT THE SCAN CANNOT PROMISE (decision 11): a password in prose, a short or unnamed token,
// an unprefixed hex secret, a novel format — every shape the confident rules do not name goes
// through. It stops the common pasted-token accident; retraction and retention bound the rest.
func CheckSend(subject, body string, ttl time.Duration, scan *redact.Redactor) (Outgoing, error) {
	if scan == nil {
		return Outgoing{}, errors.New("memo: no secret scanner — refusing to send unscanned")
	}
	subject, body = Normalize(subject), Normalize(body)
	if SubjectBreaksFence(subject) || BreaksFence(body) {
		return Outgoing{}, &Refusal{Reason: RefusedCharacters}
	}
	if len(subject) > MaxSubjectBytes {
		return Outgoing{}, &Refusal{Reason: RefusedSubjectSize}
	}
	if len(body) > MaxBodyBytes {
		return Outgoing{}, &Refusal{Reason: RefusedBodySize}
	}
	switch {
	case ttl == 0:
		ttl = DefaultTTL
	case ttl < 0 || ttl > MaxTTL:
		return Outgoing{}, &Refusal{Reason: RefusedTTL}
	}
	for _, text := range []string{subject, body} {
		_, hits := scan.String(text)
		for _, h := range hits {
			if confidentSecretRules[h.Rule] {
				return Outgoing{}, &Refusal{Reason: RefusedSecret, Rule: h.Rule}
			}
		}
	}
	return Outgoing{Subject: subject, Body: body, TTL: ttl}, nil
}
