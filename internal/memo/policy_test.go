package memo_test

import (
	"errors"
	"math/rand"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/memo"
	"github.com/ZacxDev/cairn/internal/redact"
)

// ---------------------------------------------------------------- the predicates

// A synthetic world (plan, "Examples are synthetic"): one project owning `alpha-notes` and
// `beta-notes`, whose OWNER is writer-a (so writer-a holds admin on both — there IS an admin
// for verb narrowing to strip); reader-b with a read grant on alpha-notes; admin-d with an
// ADMIN-ONLY grant on beta-notes and nothing on alpha-notes.
var w0 = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

const (
	uWriter = control.ID("usr_writer")
	uReader = control.ID("usr_reader")
	uAdmin  = control.ID("usr_admind")
	pNotes  = control.ID("prj_notes")
	sAlpha  = control.ID("scp_alpha")
	sBeta   = control.ID("scp_beta")

	tokWriter      = "memo-test-writer-full-credential-0000000000000"
	tokAgentAlpha  = "memo-test-writer-agent-alpha-0000000000000000"
	tokAgentBeta   = "memo-test-writer-agent-beta-00000000000000000"
	tokReader      = "memo-test-reader-credential-00000000000000000"
	tokAdminD      = "memo-test-admin-d-credential-0000000000000000"
	tokWriterBetaS = "memo-test-writer-beta-scope-only-00000000000"
)

func world(t *testing.T) control.Model {
	t.Helper()
	rw := control.NewVerbSet(control.VerbRead, control.VerbWrite)
	cred := func(id control.ID, user control.ID, tok string, scopes []control.ID) control.Event {
		return control.Event{Kind: control.EventCredentialIssued, At: w0, CredentialID: id,
			SubjectKind: control.KindUser, SubjectID: user, TokenHash: control.HashToken(tok),
			Label: string(id), NarrowedScopes: scopes}
	}
	m, err := control.Replay([]control.Event{
		{Kind: control.EventUserCreated, At: w0, UserID: uWriter, Provider: "p", Subject: "s1", DisplayName: "writer-a"},
		{Kind: control.EventUserCreated, At: w0, UserID: uReader, Provider: "p", Subject: "s2", DisplayName: "reader-b"},
		{Kind: control.EventUserCreated, At: w0, UserID: uAdmin, Provider: "p", Subject: "s3", DisplayName: "admin-d"},
		{Kind: control.EventProjectCreated, At: w0, ProjectID: pNotes, Name: "notes", UserID: uWriter},
		{Kind: control.EventMemberSet, At: w0, ProjectID: pNotes, UserID: uWriter, Role: control.RoleOwner},
		{Kind: control.EventScopeCreated, At: w0, ScopeID: sAlpha, DisplayName: "alpha-notes", ProjectID: pNotes},
		{Kind: control.EventScopeCreated, At: w0, ScopeID: sBeta, DisplayName: "beta-notes", ProjectID: pNotes},
		{Kind: control.EventGranted, At: w0, GrantID: "grt_reader", SubjectKind: control.KindUser, SubjectID: uReader,
			ObjectKind: control.ObjectScope, ObjectID: sAlpha, Verbs: control.NewVerbSet(control.VerbRead)},
		{Kind: control.EventGranted, At: w0, GrantID: "grt_admind", SubjectKind: control.KindUser, SubjectID: uAdmin,
			ObjectKind: control.ObjectScope, ObjectID: sBeta, Verbs: control.NewVerbSet(control.VerbAdmin)},
		cred("crd_writer", uWriter, tokWriter, nil),
		cred("crd_agent_alpha", uWriter, tokAgentAlpha, []control.ID{sAlpha}),
		{Kind: control.EventCredentialVerbsNarrowed, At: w0, CredentialID: "crd_agent_alpha", Verbs: rw},
		cred("crd_agent_beta", uWriter, tokAgentBeta, []control.ID{sBeta}),
		{Kind: control.EventCredentialVerbsNarrowed, At: w0, CredentialID: "crd_agent_beta", Verbs: rw},
		cred("crd_writer_beta_scope_only", uWriter, tokWriterBetaS, []control.ID{sBeta}),
		cred("crd_reader", uReader, tokReader, nil),
		cred("crd_admind", uAdmin, tokAdminD, nil),
	})
	if err != nil {
		t.Fatalf("building the world: %v", err)
	}
	return m
}

func authn(t *testing.T, m control.Model, tok string) (control.Principal, control.Authorization) {
	t.Helper()
	p, a, err := control.Authenticate(m, tok)
	if err != nil {
		t.Fatalf("authenticating %q: %v", tok[:20], err)
	}
	return p, a
}

// TestThePredicatesAreRelationshipsOverOneModel — the S1 predicate table: write → may send;
// read-only → may not; admin-only on ANOTHER scope → may not here; narrowed to beta-notes → may
// neither send to nor read alpha-notes; the un-narrowed same principal → may. Each row is
// pinned literally, so a mutant deleting or swapping a clause flips a named row.
func TestThePredicatesAreRelationshipsOverOneModel(t *testing.T) {
	m := world(t)
	for _, tc := range []struct {
		who             string
		tok             string
		send, read      bool
		scope           string
		admin           bool // VisibleScopes(admin) — what retract-as-moderator asks
		viaNarrowedBeta bool
	}{
		{"writer-a, un-narrowed (POSITIVE CONTROL)", tokWriter, true, true, "alpha-notes", true, false},
		{"reader-b, read only", tokReader, false, true, "alpha-notes", false, false},
		{"admin-d, admin only on ANOTHER scope", tokAdminD, false, false, "alpha-notes", false, false},
		{"admin-d on its admin-only scope: admin is not write or read", tokAdminD, false, false, "beta-notes", true, false},
		{"writer-a narrowed to beta-notes, asking alpha-notes", tokAgentBeta, false, false, "alpha-notes", false, true},
		{"writer-a narrowed to beta-notes, asking beta-notes", tokAgentBeta, true, true, "beta-notes", false, true},
		{"writer-a agent on alpha-notes: read,write, no admin", tokAgentAlpha, true, true, "alpha-notes", false, false},
		{"a scope that does not exist", tokWriter, false, false, "gamma-notes", false, false},
		{"a scope named in another CASE folds to the same name", tokWriter, true, true, "ALPHA-NOTES", true, false},
	} {
		_, a := authn(t, m, tc.tok)
		if got := memo.MaySend(a, tc.scope); got != tc.send {
			t.Errorf("%s: MaySend(%s) = %v, want %v", tc.who, tc.scope, got, tc.send)
		}
		if got := memo.MayRead(a, tc.scope); got != tc.read {
			t.Errorf("%s: MayRead(%s) = %v, want %v", tc.who, tc.scope, got, tc.read)
		}
		if got := a.VisibleScopes(control.VerbAdmin).Allows(tc.scope); got != tc.admin {
			t.Errorf("%s: admin on %s = %v, want %v (the fixture's premise)", tc.who, tc.scope, got, tc.admin)
		}
	}
}

// TestMayRetractComposesTheSenderAndTheModerator — decision 10 and e2e (g)'s decision-17
// control: the sender may (through a credential that can READ the scope); a scope admin may; a
// reader may not; an agent credential of a principal who IS a scope admin retracts that
// principal's OWN memo and NOT another's.
func TestMayRetractComposesTheSenderAndTheModerator(t *testing.T) {
	m := world(t)
	writerP, writerA := authn(t, m, tokWriter)
	agentP, agentA := authn(t, m, tokAgentAlpha)
	betaP, betaA := authn(t, m, tokWriterBetaS)
	readerP, readerA := authn(t, m, tokReader)
	sentByWriter := []string{string(control.KindUser), string(uWriter)}
	sentByReader := []string{string(control.KindUser), string(uReader)}
	sentByAdmin := []string{string(control.KindUser), string(uAdmin)}

	for _, tc := range []struct {
		who    string
		p      control.Principal
		a      control.Authorization
		sender []string
		want   bool
	}{
		{"the sender, un-narrowed", writerP, writerA, sentByWriter, true},
		{"a scope ADMIN retracting another's memo (moderation)", writerP, writerA, sentByReader, true},
		{"a reader retracting another's memo", readerP, readerA, sentByWriter, false},
		{"a reader retracting their OWN memo", readerP, readerA, sentByReader, true},
		{"the agent credential retracting its principal's own memo", agentP, agentA, sentByWriter, true},
		{"the agent credential of a scope ADMIN retracting ANOTHER's memo (decision 17)", agentP, agentA, sentByAdmin, false},
		{"the sender through a credential narrowed AWAY from the scope", betaP, betaA, sentByWriter, false},
		{"same id, different KIND", writerP, readerA, []string{string(control.KindProject), string(uWriter)}, false},
	} {
		if got := memo.MayRetract(tc.p, tc.a, "alpha-notes", tc.sender[0], tc.sender[1]); got != tc.want {
			t.Errorf("%s: MayRetract = %v, want %v", tc.who, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------- the send-side content rules

func scanner(t *testing.T) *redact.Redactor {
	t.Helper()
	s, err := memo.NewScanner()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestTheConfidentSubsetIsExactlyThisSet pins decision 11's subset as a SET (GROW or SHRINK
// fails), and requires each name to be a rule the redactor actually has — a name that matches
// no rule would be a refusal that can never fire.
func TestTheConfidentSubsetIsExactlyThisSet(t *testing.T) {
	want := []string{"age-secret-key", "anthropic-key", "authorization", "aws-access-key-id", "bearer",
		"clickup-token", "github-token", "gitlab-token", "google-api-key", "huggingface-token", "jwt",
		"npm-token", "pem-private-key", "sk-key", "slack-token", "slack-webhook", "stripe-key",
		"stripe-webhook-secret", "url-userinfo-password"}
	if got := memo.ConfidentSecretRules(); !reflect.DeepEqual(got, want) {
		t.Fatalf("the confident subset moved:\n got %v\nwant %v", got, want)
	}
	have := map[string]bool{}
	for _, r := range redact.DefaultRules() {
		have[r.Name] = true
	}
	for _, name := range want {
		if !have[name] {
			t.Errorf("confident rule %q is not in redact.DefaultRules()", name)
		}
	}
	for _, excluded := range []string{"key-context", "entropy"} {
		if !have[excluded] {
			t.Fatalf("precondition: the redactor has no %q rule, so excluding it means nothing", excluded)
		}
	}
}

// secretSamples are generated at RUN time from a SEEDED RNG — never committed (the #216 corpus
// rule): one per confident rule, each with the text around it, and the same text with the
// value removed.
func secretSamples() map[string]string {
	rng := rand.New(rand.NewSource(20000101))
	pick := func(alphabet string, n int) string {
		b := make([]byte, n)
		for i := range b {
			b[i] = alphabet[rng.Intn(len(alphabet))]
		}
		return string(b)
	}
	const up = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	const dig = "0123456789"
	const alnum = up + "abcdefghijklmnopqrstuvwxyz" + dig
	b64 := alnum + "+/"
	return map[string]string{
		"pem-private-key": "-----BEGIN " + "RSA PRIVATE KEY-----\n" + pick(b64, 64) + "\n" + pick(b64, 64) +
			"\n-----END " + "RSA PRIVATE KEY-----",
		"aws-access-key-id":     "AK" + "IA" + pick(up+dig, 16),
		"github-token":          "gh" + "p_" + pick(alnum, 36),
		"slack-token":           "xo" + "xb-" + pick(dig, 12) + "-" + pick(alnum, 24),
		"slack-webhook":         "https://hooks." + "slack.com/services/" + pick(up+dig, 9) + "/" + pick(up+dig, 11) + "/" + pick(alnum, 24),
		"age-secret-key":        "AGE-SECRET" + "-KEY-" + pick(up+dig, 59),
		"anthropic-key":         "sk-" + "ant-" + pick(alnum, 40),
		"sk-key":                "sk-" + pick(alnum, 40),
		"google-api-key":        "AI" + "za" + pick(alnum, 35),
		"stripe-key":            "sk" + "_live_" + pick(alnum, 24),
		"stripe-webhook-secret": "wh" + "sec_" + pick(alnum, 32),
		"clickup-token":         "pk" + "_" + pick(dig, 8) + "_" + pick(up+dig, 32),
		"gitlab-token":          "gl" + "pat-" + pick(alnum, 20),
		"npm-token":             "np" + "m_" + pick(alnum, 36),
		"huggingface-token":     "hf" + "_" + pick(alnum, 34),
		"jwt":                   "ey" + "J" + pick(alnum, 20) + ".ey" + "J" + pick(alnum, 30) + "." + pick(alnum, 30),
		"authorization":         "X-Api" + "-Key: " + pick(dig, 4) + pick(alnum, 28),
		"bearer":                "Bea" + "rer " + pick(dig, 4) + pick(alnum, 28),
		"url-userinfo-password": "postgres://deploy:" + pick(dig, 3) + pick(alnum, 13) + "@db.example.invalid/notes",
	}
}

// TestEachConfidentRuleRefusesAndTheTextWithoutTheValueIsAccepted — for each rule in the
// subset, a synthetic sample in the BODY refuses naming that rule (never the text), the same
// sample in the SUBJECT refuses too, and the surrounding text with the value removed is
// accepted. The sample table must cover the subset exactly.
func TestEachConfidentRuleRefusesAndTheTextWithoutTheValueIsAccepted(t *testing.T) {
	scan := scanner(t)
	samples := secretSamples()
	var covered []string
	for k := range samples {
		covered = append(covered, k)
	}
	sort.Strings(covered)
	if !reflect.DeepEqual(covered, memo.ConfidentSecretRules()) {
		t.Fatalf("the samples cover %v, the subset is %v", covered, memo.ConfidentSecretRules())
	}
	for rule, secret := range samples {
		body := "deploy note: the new value is " + secret + " — rotate the old one"
		_, err := memo.CheckSend("rotation", body, 0, scan)
		var ref *memo.Refusal
		if !errors.As(err, &ref) || ref.Reason != memo.RefusedSecret {
			t.Errorf("%s: a body carrying the value was not refused as a secret: %v", rule, err)
			continue
		}
		if ref.Rule != rule {
			t.Errorf("%s: refused naming rule %q", rule, ref.Rule)
		}
		if strings.Contains(err.Error(), secret) {
			t.Errorf("%s: the refusal carries the secret", rule)
		}
		if strings.Contains(secret, "\n") {
			continue // a subject is one line; the PEM case is body-only
		}
		if _, err := memo.CheckSend("value "+secret, "x", 0, scan); !errors.As(err, &ref) || ref.Reason != memo.RefusedSecret {
			t.Errorf("%s: a SUBJECT carrying the value was not refused: %v", rule, err)
		}
		if _, err := memo.CheckSend("rotation", "deploy note: the new value is in the vault — rotate the old one", 0, scan); err != nil {
			t.Errorf("%s: the same text WITHOUT the value was refused: %v", rule, err)
		}
	}
}

// TestNonConfidentHitsAreAccepted — a body only `key-context` or `entropy` matches is ACCEPTED
// (decision 11: those rules damage ~10% of clean lines). The POSITIVE CONTROL is the redactor
// itself: each sample IS a hit, on exactly the rule it is for, so the acceptance is the subset's
// doing and not a sample the scanner never saw. RED under a mutant that refuses on ANY hit.
func TestNonConfidentHitsAreAccepted(t *testing.T) {
	scan := scanner(t)
	rng := rand.New(rand.NewSource(20000102))
	const alnum = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	pick := func(n int) string {
		b := make([]byte, n)
		for i := range b {
			b[i] = alnum[rng.Intn(len(alnum))]
		}
		return string(b)
	}
	for rule, body := range map[string]string{
		"key-context": "set DATABASE_PASSWORD=" + pick(6) + "x9 before the run",
		"entropy":     "the build id is " + pick(48) + " if you need it",
	} {
		_, hits := scan.String(body)
		var rules []string
		for _, h := range hits {
			rules = append(rules, h.Rule)
		}
		if len(rules) == 0 || rules[0] != rule {
			t.Fatalf("POSITIVE CONTROL FAILED: the %s sample is matched by %v, not by %s alone", rule, rules, rule)
		}
		if _, err := memo.CheckSend("note", body, 0, scan); err != nil {
			t.Errorf("a body only %s matches was refused: %v", rule, err)
		}
	}
}

// TestCheckSendCapsFenceAndTTL — the size caps at their boundaries, CRLF folded before
// measuring, the fence predicates, the TTL bounds, and a nil scanner REFUSED rather than
// skipped.
func TestCheckSendCapsFenceAndTTL(t *testing.T) {
	scan := scanner(t)
	refused := func(err error, want memo.RefusalReason) bool {
		var r *memo.Refusal
		return errors.As(err, &r) && r.Reason == want
	}
	if _, err := memo.CheckSend(strings.Repeat("s", memo.MaxSubjectBytes), strings.Repeat("b", memo.MaxBodyBytes), 0, scan); err != nil {
		t.Fatalf("a subject and body exactly AT the caps were refused: %v", err)
	}
	if _, err := memo.CheckSend(strings.Repeat("s", memo.MaxSubjectBytes+1), "b", 0, scan); !refused(err, memo.RefusedSubjectSize) {
		t.Errorf("a %d-byte subject: %v", memo.MaxSubjectBytes+1, err)
	}
	if _, err := memo.CheckSend("s", strings.Repeat("b", memo.MaxBodyBytes+1), 0, scan); !refused(err, memo.RefusedBodySize) {
		t.Errorf("a %d-byte body: %v", memo.MaxBodyBytes+1, err)
	}
	// CRLF is folded BEFORE the cap is measured: 2048 "\r\n" pairs are 4096 bytes after folding.
	out, err := memo.CheckSend("s", strings.Repeat("a\r\n", memo.MaxBodyBytes/2), 0, scan)
	if err != nil || strings.Contains(out.Body, "\r") {
		t.Errorf("CRLF text at the cap after folding: err=%v, CR kept=%v", err, strings.Contains(out.Body, "\r"))
	}
	if _, err := memo.CheckSend("two\nlines", "b", 0, scan); !refused(err, memo.RefusedCharacters) {
		t.Errorf("a subject with a newline: %v", err)
	}
	if _, err := memo.CheckSend("s", "x‮y", 0, scan); !refused(err, memo.RefusedCharacters) {
		t.Errorf("a body with U+202E: %v", err)
	}
	if out, err := memo.CheckSend("s", "b", 0, scan); err != nil || out.TTL != memo.DefaultTTL {
		t.Errorf("no TTL: got %v, %v; want the default %v", out.TTL, err, memo.DefaultTTL)
	}
	if out, err := memo.CheckSend("s", "b", memo.MaxTTL, scan); err != nil || out.TTL != memo.MaxTTL {
		t.Errorf("the ceiling TTL: %v %v", out.TTL, err)
	}
	for _, ttl := range []time.Duration{memo.MaxTTL + time.Second, -time.Second} {
		if _, err := memo.CheckSend("s", "b", ttl, scan); !refused(err, memo.RefusedTTL) {
			t.Errorf("TTL %v: %v", ttl, err)
		}
	}
	if _, err := memo.CheckSend("s", "b", 0, nil); err == nil {
		t.Error("a nil scanner was accepted — the send would store unscanned text")
	}
}

// TestTheCursorMarginCoversTheTransactionTimeout pins decision 6's relation: the client's
// margin is at least four times the send transaction's bound, so a row committed late (by up to
// the timeout) is still inside the window a session re-asks.
func TestTheCursorMarginCoversTheTransactionTimeout(t *testing.T) {
	if memo.CursorMargin < 4*memo.TransactionTimeout {
		t.Fatalf("CursorMargin %v < 4 × TransactionTimeout %v", memo.CursorMargin, memo.TransactionTimeout)
	}
	if memo.TransactionTimeout != 30*time.Second || memo.CursorMargin != 2*time.Minute {
		t.Fatalf("decision 6's values moved: timeout %v, margin %v", memo.TransactionTimeout, memo.CursorMargin)
	}
}
