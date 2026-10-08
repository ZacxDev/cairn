package ui

import (
	"net/http"
	"net/url"
	"strings"
)

// FieldNext is the ONE spelling of the return-to parameter: the query parameter on
// `GET /sign-in`, the hidden field both sign-in forms carry, and the field the GitHub start
// row reads. One name for all three, because a page whose form said `next` while the start
// row read `return_to` would carry the value to the door and drop it on the floor there.
const FieldNext = "next"

// maxNextLen bounds a return-to value. A real one is a path plus a short query; 2048 is the
// length browsers and proxies have historically tolerated in a request line, and anything
// longer is not a page this surface links to.
const maxNextLen = 2048

// safeNext is THE ONE validator for a return-to value, and every place `next` is read goes
// through it: the redirect the dispatcher builds, the sign-in page's hidden field, the
// credential POST, the GitHub start row, and the GitHub callback at the moment of use.
// It returns the value unchanged when it is acceptable and "" when it is not; "" means
// "land on `/`", never an error page, and the rejected value is never echoed anywhere.
//
// 🔴 IT ACCEPTS ONLY A SAME-ORIGIN, PATH-ABSOLUTE REFERENCE, AND EVERY RULE BELOW IS ONE
// SHAPE OF OPEN REDIRECT:
//
//   - non-empty, at most `maxNextLen` bytes;
//   - starts with `/`, which already rules out a scheme (`https:`, `javascript:` — RFC 3986
//     requires a scheme to start with a letter);
//   - no backslash ANYWHERE. Browsers treat `\` as `/` in the authority position of an
//     http(s) URL, so `/\evil.invalid` is `//evil.invalid` to the thing that follows it;
//   - every byte printable ASCII (0x21–0x7e): no control character, no whitespace, no DEL,
//     nothing non-ASCII. A browser STRIPS tab and newline from a URL before parsing it, so
//     `/\t/evil.invalid` is `//evil.invalid` after the strip — rejecting the byte is what
//     makes the second rule hold for the string the browser actually parses;
//   - no `#` at all. A browser never sends a fragment in a request-URI, so nothing a
//     redirect carries can have one; and a fragment carrying an invalid escape makes
//     `url.Parse` fail, which makes `http.Redirect` skip its `path.Clean` and emit the value
//     RAW — so a refused `#` is what keeps the next rule true of the bytes a browser receives;
//   - the PATH (before any `?`) has no empty segment — no `//` ANYWHERE, which includes the
//     leading `//` of a NETWORK-PATH reference (`//evil.invalid`, resolved by a browser to
//     another host), so "no host" is this rule rather than a separate second-character check
//     that it would shadow — and no `.` or `..` segment. `/..//evil.invalid` resolves, after a
//     browser removes the dot segment, to the PATH `//evil.invalid` — same-origin, but one
//     rewrite away from a network-path reference. Refusing the shape rather than relying on
//     `http.Redirect`'s `path.Clean` to normalise it means the value validated is the value
//     sent, byte for byte.
//
// ⚠ THERE IS NO SIGN-IN LOOP CHECK, AND ONE STOOD HERE. It refused `/sign-in`, `/sign-out` and
// the two GitHub rows, decoding the path to do it, on the premise that `next=/sign-out` would
// sign a person straight back out. False: `/sign-out` is POST-only, so a 303 there is a GET
// that answers 404 and revokes nothing (`TestANextOfSignOutSignsNobodyOut`). What is left is a
// nuisance landing, and the check was this function's only decode step, so it was deleted.
// Landing on `/sign-in` cannot loop either: the signed-in shortcut strips one `next` per hop
// and the redirect is GET-only (`TestNoUnauthenticatedRequestShapeLoops`).
//
// ⚠ PERCENT-ESCAPES ARE NOT DECODED FOR THE SHAPE RULES, AND THAT IS A DECISION. A browser
// resolving a `Location` does not decode `%2F` into a delimiter — RFC 3986 §2.2 makes an
// escaped reserved character DATA — so `/%2F%2Fevil.invalid` is a same-origin path on THIS
// host (which answers it 404). It is accepted, and the test that pins the decision pins the
// Location it produces. Decoding first would refuse a harmless value and would also refuse
// a legitimate query that carries an escaped `\` or `//`, e.g. a search for a path. Nothing
// in this function decodes.
func safeNext(raw string) string {
	if raw == "" || len(raw) > maxNextLen {
		return ""
	}
	if raw[0] != '/' {
		return ""
	}
	for i := 0; i < len(raw); i++ {
		if c := raw[i]; c == '\\' || c == '#' || c < 0x21 || c > 0x7e {
			return ""
		}
	}
	p, _, _ := strings.Cut(raw, "?")
	if strings.Contains(p, "//") {
		return ""
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "." || seg == ".." {
			return ""
		}
	}
	return raw
}

// requestedNext reads the return-to value a sign-in request carries and validates it.
//
// 🔴 `GET` READS THE QUERY; EVERY OTHER METHOD READS THE POSTED BODY ONLY. (No row answers
// `HEAD`, so a `HEAD` never reaches a handler that calls this.) The two
// POST rows (`/sign-in`, `/sign-in/github`) read `PostFormValue` for the reason
// `handleOAuthStart` records about the invitation token: `FormValue` is the body UNION the
// query, so a value in the URL of a POST would be honoured. Here that is not a secret, but
// one rule for every field the sign-in doors read is cheaper than a second rule to remember.
func requestedNext(r *http.Request) string {
	if r.Method == http.MethodGet {
		return safeNext(r.URL.Query().Get(FieldNext))
	}
	return safeNext(r.PostFormValue(FieldNext))
}

// landingFor is where a completed sign-in lands: the return-to value its caller already
// validated, or `/` when there is none.
func landingFor(next string) string {
	if next == "" {
		return RootPath
	}
	return next
}

// signInLocation is the `Location` an unauthenticated browser GET is sent to.
//
// 🔴 IT IS A FUNCTION OF THE REQUEST-URI AND NOTHING ELSE, which is the uniformity property:
// it never consults the store, the session table or the route ledger, so the answer for a
// scope that exists, one that does not and one the caller may not read is the same bytes for
// the same URL. A redirect that differed would be an existence oracle open to anybody.
//
// `RequestURI()` and not `URL.Path`, because `Path` is DECODED: a request for
// `/%2F%2Fevil.invalid` has `Path == "//evil.invalid"`, and carrying the decoded form would
// both lose the original URL and turn a harmless escape into a value `safeNext` refuses.
// A return-to of `/` is the default landing, so it is omitted rather than spelled — which
// also keeps the root's redirect exactly `/sign-in`, as it was before this existed.
func signInLocation(r *http.Request) string {
	next := safeNext(r.URL.RequestURI())
	if next == "" || next == RootPath {
		return SignInPath
	}
	return SignInPath + "?" + url.Values{FieldNext: {next}}.Encode()
}

// presentedAuthorization answers whether the request carried an `Authorization` header at
// all — present, even if empty or malformed.
//
// 🔴 PRESENCE, NOT VALIDITY, IS THE SPLIT, BECAUSE THE QUESTION IS WHO IS ASKING. Both
// header-borne backends (the machine token and the provider JWT) read this header; a client
// that sends it is a program holding a credential, and a program must get the uniform 401 it
// was written against, never an HTML redirect it cannot follow meaningfully. A browser
// navigating sends a cookie or nothing.
func presentedAuthorization(r *http.Request) bool {
	return len(r.Header.Values("Authorization")) > 0
}

// redirectsToSignIn is the ONE predicate for gate (4)'s redirect branch: a `GET`, from
// something that asked for HTML, presenting no `Authorization` header. Every conjunct is
// derived from the REQUEST, never from a route class or from what the path names — the rule
// both cross-site gates follow, and the reason the branch cannot become an oracle or a
// state-changing path.
//
// 🔴 `GET` ONLY, NOT `HEAD`, AND A MEASURED LOOP IS WHY. No row in the ledger answers `HEAD`,
// so a `HEAD` redirected to `/sign-in` falls through to this gate again and is redirected to
// `/sign-in` again, for ever — measured on `/`, `/arcs?all=1`, `/sign-in` and `/join?token=x`
// when this predicate admitted `HEAD`. Browsers never navigate with `HEAD`, so nothing was
// gained by it; a `HEAD` keeps the uniform 401 it had before this branch existed.
// `TestNoUnauthenticatedRequestShapeLoops` follows every `Location` for both methods.
func redirectsToSignIn(r *http.Request) bool {
	return r.Method == http.MethodGet &&
		acceptsHTML(r) &&
		!presentedAuthorization(r)
}
