package pgstore_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ZacxDev/cairn/internal/pgstore"
)

// The connection string must not reach an error, and this file is UNTAGGED on purpose.
//
// 🔴 IT NEEDS NO SERVER, WHICH IS THE WHOLE REASON IT CAN GATE ANYTHING. The leak it
// pins happens during DSN PARSING — before any socket — so these cases run in
// `go test ./...`, in every nix derivation's `doCheck`, and in CI's `go` job. Putting
// them behind the `pgtest` tag would have left the guard in the one tier that needs a
// database, which is the tier a sandbox cannot run.
//
// 🔴 WHAT WENT WRONG, BECAUSE THE SHAPE IS THE LESSON. `Open` wrapped the driver's error
// with `%w` under the text "the connection string is deliberately not echoed — it carries
// a password". Not interpolating the DSN into the FORMAT says nothing about the error
// being WRAPPED: `lib/pq` hands a `postgres://…` DSN to `net/url.Parse`, and a `*url.Error`
// carries the raw string verbatim. The refusal therefore printed the password inside a
// sentence asserting twice that it did not.
//
// ⚠ AND THE REPO'S OWN TESTS WERE GREEN THROUGHOUT, because every DSN in them is the
// KEYWORD/VALUE form, which reports only the offending key. The leaking form is the URL
// one — the form a Kubernetes secret carries and the form `README.md` documents. A
// fixture that can only produce the safe shape cannot see the unsafe one.

// aDSNCarryingAPassword is the value under test, in both spellings a deployment uses.
//
// 🔴 THE SECRET IS A DISTINCT, IMPROBABLE STRING so a match cannot be an accident of the
// surrounding text, and it is NOT a realistic-looking credential: this file is committed
// to a PUBLIC repository and `AGENTS.md` forbids credential-shaped fixtures.
const (
	fixtureSecret = "NOT-A-REAL-PASSWORD-e3b0c44298fc"
	urlDSN        = "postgres://cairn:" + fixtureSecret + "@db.invalid:not-a-port/cairn?sslmode=require"
	kvDSN         = "host=db.invalid user=cairn password=" + fixtureSecret + " oops"
)

// TestOpenNeverEchoesTheConnectionString drives the two DSN spellings through the real
// `Open` and asserts the secret is absent from what comes back.
//
// 🔴 THE URL ARM IS THE REGRESSION AND THE KEYWORD/VALUE ARM IS THE CONTROL — and the
// control matters, because it is the shape that was ALWAYS safe. A guard that only drove
// the URL form could pass against a redactor that replaced every error with a constant,
// which would be a different defect (an operator with no idea what is wrong).
func TestOpenNeverEchoesTheConnectionString(t *testing.T) {
	for _, arm := range []struct {
		name string
		dsn  string
		// wantDetail is a fragment of the DRIVER's own diagnosis that must SURVIVE
		// redaction. Without it this case is satisfied by a redactor that throws the
		// error away, and the refusal an operator reads would name nothing actionable.
		wantDetail string
	}{
		{
			name: "the URL form, which is what a secret carries",
			dsn:  urlDSN,
			// `net/url` reports the offending port; that is the actionable half.
			wantDetail: "not-a-port",
		},
		{
			name:       "the keyword/value form, which never leaked",
			dsn:        kvDSN,
			wantDetail: "oops",
		},
	} {
		t.Run(arm.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()

			_, err := pgstore.Open(ctx, arm.dsn)
			if err == nil {
				t.Fatal("POSITIVE CONTROL FAILED: Open ACCEPTED a malformed connection string, so " +
					"the absence asserted below is a statement about a call that did not fail")
			}
			got := err.Error()
			if strings.Contains(got, fixtureSecret) {
				t.Errorf("the refusal CONTAINS the password.\n  got: %s\n"+
					"This message is the line an operator is told is safe to paste, at the exact "+
					"moment they are debugging a bad secret — and it says twice that it does not "+
					"echo the connection string.", got)
			}
			if strings.Contains(got, arm.dsn) {
				t.Errorf("the refusal contains the whole connection string verbatim:\n  %s", got)
			}
			if !strings.Contains(got, arm.wantDetail) {
				t.Errorf("the refusal has lost the driver's own diagnosis (%q is absent):\n  %s\n"+
					"Redacting the secret must not redact the REASON, or an operator is told only "+
					"that something is wrong.", arm.wantDetail, got)
			}
		})
	}
}

// TestTheRedactionSurvivesAWrappedUnwrap pins the one cost the redactor accepts, so that
// a later change cannot reintroduce the leak through `errors.Unwrap`.
//
// 🔴 A REDACTOR THAT LEFT THE ORIGINAL REACHABLE WOULD BE COSMETIC. The leaking string is
// the ORIGINAL error's own `Error()`, so any caller that unwraps and prints would undo the
// redaction. This walks the chain to the bottom and requires the secret absent from EVERY
// link.
//
// ⚠ IT DOES NOT ASSERT THAT `redact` RETURNS A NEW ERROR, AND AN EARLIER VERSION OF THIS
// DOCSTRING SAID IT DID. On the `*url.Error` path — the only one that ever leaked —
// `redact` mutates `uerr.URL` in place and then returns the ORIGINAL error unchanged;
// `errors.New` is reached only for a DSN mentioned outside that field. So what this case
// pins is the absence of the secret from the chain, which is the property that matters and
// is strictly weaker than "a new value". Stating the weaker true claim beats restating the
// stronger false one: a maintainer who believed the chain was severed by construction would
// happily rewrite pass 1 to work on a copy, and only this walk would catch them.
func TestTheRedactionSurvivesAWrappedUnwrap(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	_, err := pgstore.Open(ctx, urlDSN)
	if err == nil {
		t.Fatal("POSITIVE CONTROL FAILED: Open accepted a malformed connection string")
	}
	depth := 0
	for e := err; e != nil; e = unwrap(e) {
		if strings.Contains(e.Error(), fixtureSecret) {
			t.Fatalf("the password is reachable at unwrap depth %d:\n  %s\n"+
				"A redactor that leaves the original wrapped is cosmetic: any caller that "+
				"unwraps and logs restores the leak.", depth, e.Error())
		}
		depth++
		if depth > 20 {
			t.Fatal("the error chain does not terminate")
		}
	}
	// 🔴 THE WALK MUST HAVE WALKED. A chain of length 1 would satisfy the loop above
	// without ever testing an unwrap, which is the property this case exists for.
	if depth < 2 {
		t.Fatalf("the error chain is %d link(s) deep, so the unwrap above asserted nothing", depth)
	}
}

// unwrap is `errors.Unwrap` without importing it into the loop above's condition, kept
// separate so the walk reads as a walk.
func unwrap(err error) error {
	u, ok := err.(interface{ Unwrap() error })
	if !ok {
		return nil
	}
	return u.Unwrap()
}
