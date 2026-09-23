// Package authz reads and validates the static TOKEN FILE: the deployed secret, its
// guard ladder, and the bearer header it is presented in.
//
// 🔴 IT NO LONGER AUTHORISES A REQUEST, AND THE PREVIOUS SENTENCE HERE SAID IT DID.
// `internal/control` holds the authorization model and `control.Authenticate` is the
// only function that resolves a credential to a principal; what this package produces
// is the INPUT to `internal/control/tokenfile`, which projects the table into a
// `control.Model`. `Authorize`, `ErrRejected` and `TokenRecord.VisibleScopes` were
// deleted rather than left beside the new path — a second authenticator over a second
// spelling of "what may this see" is the two-structures-one-fact shape this file's own
// comments spend most of their length refusing, and it does not stop being that shape
// because one of the two is only reachable from a test.
//
// 🔴 WHAT IS STILL TRUE, AND IS THE WHOLE VALUE HERE: A ROW IS ONE OBJECT. The token,
// its identity and its allowlist come out of one parse, so nothing downstream can
// answer "which token was this" and "what may it see" from two structures that
// disagree. The adapter carries that property forward by building every grant for a
// row out of the SAME record.
package authz

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/ZacxDev/cairn/internal/envalias"
	"github.com/ZacxDev/cairn/internal/pytext"
	"github.com/ZacxDev/cairn/internal/store"
)

// EnvToken names the environment fallback for the token SET, in its current spelling.
// `SUBSYSTEM_STORE_TOKEN` still resolves to it through `internal/envalias`; this file
// never spells the old name, which is what keeps the ledger in one place.
const EnvToken = "CAIRN_TOKEN"

// 256 bits, base64url'd without padding, is 43 characters. A shorter token is
// refused at STARTUP rather than served: a store that came up with a weak token is
// worse than one that did not come up at all, because it looks healthy.
const MinTokenChars = 43

// Overlap rotation needs two (current + previous); three covers a rotation that
// overlaps another. Beyond that a "set" is an accumulation nobody has retired, and
// every one of them is a live credential — so the file is refused rather than
// served, at STARTUP, for the same reason a short token is.
const MaxTokens = 4

// LegacyIdentity is the identity of a legacy row, AND IT MEANS UNRESTRICTED SCOPE.
//
// A bare token line — no identity, no allowlist — is the shape the file had before
// scoping, and it MUST keep loading: the migration puts the mapped rows in beside
// the old shared token, and the rollback is re-adding that one line. A format that
// refused it would make the rollback a code change.
//
// It is a constant because three different things have to agree on it: the parser
// that assigns it, the guard that refuses a MAPPED row from claiming it, and the
// startup warning that names it.
const LegacyIdentity = "legacy"

// 🔴 32, AND THE NUMBER IS LOAD-BEARING RATHER THAN TIDY: it is BELOW
// MinTokenChars, so a token can never be a well-formed identity. That makes "the
// operator put three tokens on one line" structurally impossible to read as
// "token, identity, scopes" — the second field would be at least 43 characters and
// this cap refuses it. A cap ABOVE the token floor would have made that misreading
// silent, and a silent misreading of a credential file is exactly the failure the
// guard ladder below exists for.
const MaxIdentityChars = 32

// MaxScopeChars caps a scope BELOW the token floor, for the same reason, AND THIS
// IS A FIX RATHER THAN SYMMETRY. The identity cap made a mis-paste one field to the
// LEFT structurally impossible to misread; the scope field had no such cap, and the
// mis-paste one field to the RIGHT was therefore read as a legitimate scope NAME.
// Measured on the Python side with a token from the generator the server's own
// README prescribes: `<current> <identity> <new-token>` LOADED CLEAN, because a real
// token contains no `=` and no `.`, so it matched the safe-path class, folded to a
// non-empty ref, and the credential the operator was installing became a scope name
// authorising nothing.
//
// 🔴 DERIVED, NEVER A SECOND LITERAL. `MaxScopeChars < MinTokenChars` is the whole
// safety argument, and a hand-typed 42 is one edit away from being a 44 that
// silently re-opens both halves.
const MaxScopeChars = MinTokenChars - 1

// Lowercase, digits and dashes, starting on an alphanumeric. Deliberately NARROWER
// than the scope class (no `_`, no uppercase): an identity is quoted into the audit
// log and compared for duplicates, so two spellings of one name would be two
// identities to the parser and one to the operator.
var identityComponent = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// SafePathComponent is a scope or ref name as it may appear in a URL path.
//
// 🔴 NO DOT. That makes traversal impossible BY CONSTRUCTION rather than by
// excluding `.` and `..` by name — a structural guard instead of a spelled one,
// which is the difference between "the two spellings I thought of are blocked" and
// "the character that enables them cannot appear". The first Python draft DID permit
// dots and excluded `..` by name beside it; a mutation sweep removed the dot from
// the class and the ENTIRE SUITE STAYED GREEN, because refs travel in the QUERY
// STRING and no test had a dotted path component at all.
var SafePathComponent = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// SafePathComponentPattern is the pattern text a refusal message quotes. It is
// derived from the compiled expression rather than restated, so the message cannot
// describe a class the code does not enforce.
//
// ⚠ The anchors are stripped because the Python constant it mirrors is unanchored
// and applied with `fullmatch`; the CLASS is the part a caller can act on.
var SafePathComponentPattern = strings.TrimSuffix(strings.TrimPrefix(SafePathComponent.String(), "^"), "$")

// TokenRecord is one parsed row of the token file.
//
// 🔴 IT IS NO LONGER A VISIBILITY ANSWER, IT IS THE INPUT TO ONE.
// `internal/control/tokenfile` turns each record into grants — `read,write` over the
// named scopes for a mapped row, `read` over every scope for a bare one — and
// `control.Authorization` is what every narrowing site then consults.
//
// ⚠ THE ASYMMETRY THIS TYPE EXISTS FOR SURVIVES THAT MOVE, and it is worth restating
// because it is the reason `legacy` is a bool: an UNRESTRICTED row is reachable ONLY
// from a bare token line, and an EMPTY allowlist is its OPPOSITE — nothing is visible.
// A record with neither (the shape a refactor produces by forgetting to set a field)
// must confer NOTHING, and it does: the adapter emits no grant for it.
type TokenRecord struct {
	Token    string
	Identity string
	// Scopes is the allowlist. It is nil for a legacy row and non-empty for a
	// mapped one; a mapped row with an empty allowlist is refused at parse time
	// (guard 9), so "nil" and "legacy" are the same condition by construction.
	Scopes []string
	// legacy is carried explicitly rather than derived from `Scopes == nil`,
	// because in Go a nil slice and an empty one are one value and the WHOLE
	// asymmetry of this design is that unrestricted and empty are opposites. A
	// bool cannot be produced by forgetting to set a slice.
	legacy bool
}

// Fingerprint is what the audit log carries. Never the token.
func (r TokenRecord) Fingerprint() string { return TokenID(r.Token) }

// IsLegacy answers whether this record came from a bare row.
//
// 🔴 IT HAS EXACTLY ONE CONSUMER — `internal/control/tokenfile`, which reads it to
// decide the VERBS a row's grants carry (`read` for a bare row, `read,write` for a
// mapped one). The serving path does not call it, and that is the point of the move:
// "may this write" is now a question about a principal's authority, answered by the
// same predicate that answers "may this see", instead of a second question answered
// from the shape of the row.
func (r TokenRecord) IsLegacy() bool { return r.legacy }

// LegacyRecord is the record a bare token line means. ONE PLACE, and it is the same
// rule for the parser and for a programmatic caller, so the meaning of a bare token
// cannot come to differ between the two.
func LegacyRecord(token string) TokenRecord {
	return TokenRecord{Token: token, Identity: LegacyIdentity, legacy: true}
}

// TokenID is a stable, non-reversible handle for the audit log.
//
// 🔴 The log must be able to say WHICH token was used without ever holding the
// token. A truncated sha256 does that; the token itself in a log line is the leak
// the log exists to detect.
func TokenID(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])[:12]
}

// RedactedField is how an UNVALIDATED field of the token file reads in an error
// message.
//
// 🔴 NEVER THE VALUE ITSELF, AND THAT IS A FIX RATHER THAN A PRECAUTION. A guard
// that quotes the field it refused publishes whatever the operator wrote there —
// and the field an operator gets wrong while installing a credential IS the
// credential. `<current> <new> alpha`, a token pasted one field to the left, is the
// shape: MinTokenChars (43) is deliberately ABOVE MaxIdentityChars (32), so a token
// in the identity field cannot be anything BUT too long, always lands on that
// guard, and was quoted back in full.
//
// 🔴 AND IT IS NOT CONFINED TO A CRASH. A reload prints the guard's message on
// every refused SIGHUP, from a process that stays healthy and can be signalled
// again — the stream the audit log is read out of, with wider read access than the
// encrypted secret the token came from.
//
// So the value is replaced by two facts that are not secrets and are enough to act
// on: its LENGTH (a 58-character "identity" is visibly the operator's token rather
// than a typo) and its FINGERPRINT (the SAME id the audit log carries, so if the
// field really was a credential the two can be tied together without either of
// them ever printing the token). The row's POSITION does the locating.
//
// ⚠ A LENGTH OR CHARSET HEURISTIC — "elide it only if it looks like a token" — was
// considered and rejected: it fails OPEN, because the one input it must catch is by
// definition an input nobody classified correctly.
func RedactedField(value string) string {
	return fmt.Sprintf("%d chars, fp=%s", len([]rune(value)), TokenID(value))
}

// authorityOf is how a record's AUTHORITY reads in an error message. Never the
// token.
//
// 🔴 "NOT SECRETS" IS A CLAIM ABOUT THE GUARDS UPSTREAM, NOT ABOUT THE STRINGS,
// AND IT WAS FALSE OF THE SCOPES UNTIL MaxScopeChars EXISTED. Every value this can
// print has passed a guard that caps it BELOW MinTokenChars, so it cannot be a
// credential this server would accept: the identity by guard 7, every scope by
// guard 10. Remove either cap and this starts echoing an unvalidated field again.
func authorityOf(r TokenRecord) string {
	if r.legacy {
		return r.Identity + " (UNRESTRICTED)"
	}
	return r.Identity + " (" + strings.Join(r.Scopes, ",") + ")"
}

// authorityKey is what two rows must AGREE ON to be one grant rather than two
// authorities.
//
// 🔴 A **SET** OF SCOPES, NOT THE ORDERED LIST, and that is a fix. Comparing the
// list positionally made `<tok> zach alpha,beta` and `<tok> zach beta,alpha` a
// refusal reading "two different authorities". Both grant the same set; there is a
// defined answer, so the duplicate guard must not claim there is none.
//
// The TOKEN is not in the key: the collapse is already keyed on it, so every pair
// this compares shares one by construction. LEGACY is, because unrestricted is the
// OPPOSITE of an empty allowlist everywhere else in this package.
func authorityKey(r TokenRecord) string {
	if r.legacy {
		return "legacy\x00"
	}
	sorted := append([]string(nil), r.Scopes...)
	slices.Sort(sorted)
	return r.Identity + "\x00" + strings.Join(dedupe(sorted), "\x1f")
}

// ParseTokenRow turns one non-empty line of the token file into one record, or
// returns an error naming why. Guards 6-10 of the ladder live here; guards 11 and
// 12 are cross-row and live in LoadTokens.
//
// `line` is the PHYSICAL line number and `total` the file's physical line count, so
// "line 6 of 6" is something the operator can count to in an editor. Both are
// passed in rather than derived, because this function sees one row.
func ParseTokenRow(fields []string, line, total int) (TokenRecord, error) {
	if len(fields) != 1 && len(fields) != 3 {
		// 🔴 REFUSED, NOT REINTERPRETED. The previous Python parser split the
		// WHOLE FILE on whitespace, so two tokens separated by a space were two
		// credentials; under the row format that same line reads as
		// `token identity scopes` with a token in the identity field. A line this
		// parser cannot read is a startup failure, never a guess about which of
		// two readings the operator meant.
		//
		// ⚠ AND THE MESSAGE NAMES THE LIKELY TYPO WITHOUT ADMITTING IT. `<tok>
		// zach a, b` is FOUR fields, because the space after the comma splits the
		// scope list in two — "4 fields, expected 1 or 3" is correct and useless.
		// The hint is APPENDED, never substituted, and it is CONDITIONAL on
		// evidence in the row rather than guessed.
		hint := ""
		if len(fields) > 3 {
			for _, f := range fields[2:] {
				if strings.Contains(f, ",") {
					hint = ". Field 3 is a comma-separated list with NO SPACES — write " +
						"`alpha,beta`, not `alpha, beta`: a space is what separates the " +
						"three fields, so `alpha, beta` is two of them"
					break
				}
			}
		}
		return TokenRecord{}, fmt.Errorf(
			"malformed token row on line %d of %d: %d fields, expected 1 (a bare legacy token) or 3 (token, identity, comma-separated scopes). Whitespace separates the three FIELDS, so two tokens on one line is no longer two tokens%s",
			line, total, len(fields), hint)
	}
	token := fields[0]
	if len(fields) == 1 {
		return LegacyRecord(token), nil
	}

	identity := fields[1]
	if len([]rune(identity)) > MaxIdentityChars || !identityComponent.MatchString(identity) {
		// 🔴 THE FIELD IS DESCRIBED, NEVER QUOTED — see RedactedField. This guard
		// is the one a mis-pasted credential ALWAYS lands on, and its message
		// reaches stdout on every refused reload, not just once at startup.
		return TokenRecord{}, fmt.Errorf(
			"invalid identity in token row on line %d of %d: field 2 is not an identity (%s; the value is NOT echoed — a token pasted into this field would be a live credential in a log line) — expected lowercase letters, digits and dashes, starting on an alphanumeric, at most %d characters. If that length looks like your TOKEN, field 2 is where the identity goes: the row is `<token> <identity> <scopes>`. The identity is quoted into the audit log, so it must be one spelling",
			line, total, RedactedField(identity), MaxIdentityChars)
	}
	if identity == LegacyIdentity {
		// 🔴 A MAPPED ROW MAY NOT CLAIM THE UNRESTRICTED NAME. `legacy` in the
		// audit log has to mean exactly one thing — "this request came in on the
		// old shared credential, which can see everything" — or the one line the
		// operator greps to know the migration is finished is ambiguous.
		return TokenRecord{}, fmt.Errorf(
			"reserved identity in token row on line %d of %d: '%s' is what a BARE token line is given, and it means unrestricted scope. Name this row's holder instead",
			line, total, LegacyIdentity)
	}

	rawScopes := strings.Split(fields[2], ",")
	anyScope := false
	for i, raw := range rawScopes {
		rawScopes[i] = pytext.StripWhitespace(raw)
		if rawScopes[i] != "" {
			anyScope = true
		}
	}
	if !anyScope {
		// Reachable with a bare `,`: three fields, a valid identity, and no scope
		// name anywhere in the third.
		return TokenRecord{}, fmt.Errorf(
			"empty scope allowlist in token row on line %d of %d ('%s'): a credential that may see NO scope can never be used. Remove the row, or name the scopes it may read",
			line, total, identity)
	}
	var scopes []string
	for _, raw := range rawScopes {
		// 🔴 THREE CLAUSES, AND NONE IS A SPELLING OF ANOTHER.
		//
		// The CLASS alone accepts `-` and `___`, which fold to the EMPTY STRING:
		// an entry perfectly namable in a URL that matches no index key, i.e. a
		// grant that reads as working and does nothing. So the check runs on the
		// value that actually reaches the comparison, not on the text the operator
		// typed.
		//
		// The LENGTH clause is not covered by the charset one, and that was the
		// assumption this guard shipped on before it was measured WRONG: a real
		// `secrets.token_urlsafe(43)` emits only `[A-Za-z0-9_-]`, so a REAL token
		// matches the safe-path class and folds to a non-empty ref. The fixture
		// that "covered" this appended an `=`, a character that generator never
		// produces, so the realistic input took a different path entirely and
		// loaded clean.
		//
		// The RAW value is measured, not the folded one: folding only lowercases,
		// substitutes and collapses, so it can never LENGTHEN a string — bounding
		// the input bounds the output, and bounding the input is what stops the
		// value reaching guard 11 in the first place.
		folded := store.NormalizeRef(raw)
		if !SafePathComponent.MatchString(raw) || folded == "" || len([]rune(raw)) > MaxScopeChars {
			// 🔴 THE MESSAGE SAYS `` see `redacted_field` ``, WITH THE PYTHON SPELLING, AND
			// THAT IS NOT A TYPO TO BE "FIXED" TO THE GO IDENTIFIER. This sentence is
			// reproduced from the oracle, and a port's job is to be indistinguishable in the
			// bytes it emits — a Go identifier leaking into a message is not an
			// implementation diagnostic the way CPython's `json` wording is, it is just a
			// different string. The Go function is `RedactedField`; the message names the
			// oracle's `redacted_field`, because that is what the oracle writes.
			return TokenRecord{}, fmt.Errorf(
				"invalid scope in token row on line %d of %d ('%s'): field 3 holds a name that is not a scope (%s; the value is NOT echoed — see `redacted_field`) — a scope must match %s, be at most %d characters (deliberately below the %d-character token floor, so a token pasted into field 3 cannot be read as a scope name), AND still name something once folded the way the reader folds a scope. An entry that no request could name, that folds away to nothing, or that is long enough to be a credential, is refused here rather than sitting inert",
				line, total, identity, RedactedField(raw), SafePathComponentPattern, MaxScopeChars, MinTokenChars)
		}
		scopes = append(scopes, folded)
	}
	return TokenRecord{Token: token, Identity: identity, Scopes: dedupe(scopes)}, nil
}

// IsTokenFile is `pathlib.Path(p).is_file()` — S_ISREG on a stat that FOLLOWS a
// symlink — and it is the predicate BOTH places that ask "is there a token file here"
// must use.
//
// 🔴 ONE PREDICATE, TWO CALLERS, AND THE SPLIT IS WHAT MADE THE BUG. `os.Stat` plus
// `!info.IsDir()` reads as "is a file" and is not: it accepts a FIFO, a socket, a
// character device and a block device. Both consequences were measured against the
// oracle, and they point in OPPOSITE directions, which is why neither caller could be
// fixed alone:
//
//   - a FIFO at the token path made `os.ReadFile` BLOCK FOREVER — no diagnostic, no
//     exit, a process that never finishes starting and never serves. The oracle's
//     `is_file()` is false for it, so it refuses in milliseconds with guard 2's
//     sentence. An availability defect where a hang is strictly worse than an error.
//   - `/dev/null` at the DEFAULT token path with `$SUBSYSTEM_STORE_TOKEN` set: the
//     oracle's `is_file()` is false, so it falls back to the environment and serves.
//     `!IsDir` is TRUE for a character device, so Go took the fallback branch away from
//     itself, read zero bytes, and exited 78 on "token is empty".
//
// ⚠ IT FOLLOWS SYMLINKS, DELIBERATELY. A Kubernetes secret mount is a symlink to a
// `..data/` path; refusing one would refuse the deployed shape. `Path.is_file()` has
// exactly this behaviour, which is the reason to state it rather than to reimplement it.
//
// ⚠ AND IT RETURNS FALSE WHERE THE ORACLE **RAISES**, on one input class: `is_file()`
// returns False only for `pathlib._IGNORED_ERRNOS` (ENOENT, ENOTDIR, EBADF, ELOOP) and
// raises for anything else, so an EACCES on the path's parent is a `PermissionError`
// there (uncaught out of `load_tokens` — a traceback and exit 1) and guard 2's named
// refusal at exit 78 here. Both refuse to serve; only the exit code and the wording
// differ, and the Go side is the better of the two. Recorded because "it matches
// `is_file()`" would otherwise read as covering this case too.
func IsTokenFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// LoadTokens resolves the bearer token SET. FILE FIRST, environment only as a
// fallback.
//
// 🔴 A SET, NOT A TOKEN, and that is the whole of rotation. ONE ROW PER LINE — the
// CURRENT credential first, the PREVIOUS one below it. Rotation is then: add the
// new line, watch the audit log until every client's fingerprint has moved, then
// delete the old line. There is no window in which a client is broken, which is the
// reason single-token rotations never actually get performed.
//
// THE ROW FORMAT:
//
//	<token>                                   legacy: identity `legacy`,
//	                                          UNRESTRICTED scope
//	<token>   <identity>   <scope>,<scope>    mapped: named, scoped
//
// Both shapes may appear in one file, and that is not a concession — it is the
// migration and the rollback. A file holding any legacy row emits a LOUD startup
// warning through `warn`, because "unrestricted" is a state somebody has to be able
// to see from the pod log.
//
// Guard order — each reachable by an input no earlier guard rejects. `L` is a
// PHYSICAL LINE NUMBER and `T` the file's physical line count:
//
//  1. some source at all      -> "no token source"
//  2. the file is readable    -> "token file unreadable"
//  3. at least one token      -> "token is empty"
//  4. not an accumulation     -> "too many tokens"
//  5. every token long enough -> "token on line L of T is too short"
//  6. every row parses        -> "malformed token row on line L of T"
//  7. identity is well-formed -> "invalid identity in token row on line L of T"
//  8. identity is not taken   -> "reserved identity in token row on line L of T"
//  9. the allowlist is real   -> "empty scope allowlist in token row on line L of T"
//  10. every scope is namable AND SHORT ENOUGH NOT TO BE A CREDENTIAL
//     -> "invalid scope in token row on line L of T"
//  11. one authority per token -> "duplicate token on lines L and M"
//  12. one row per identity    -> "duplicate identity"
//
// 🔴 NO GUARD ECHOES AN **UNVALIDATED** FIELD, and every earlier wording of that
// claim on the Python side has been false of at least one guard. The common failure
// was the same both times: a claim of the form "guard N is safe because its field is
// validated" where the validation is a CHARSET rather than a LENGTH. Only a cap
// below MinTokenChars supports the sentence, because the sentence is "this cannot be
// a credential" and the charset a token is drawn from is the scope charset exactly.
//
// 🔴 EVERY ROW REACHES THE LADDER, AND THAT IS A FIX, NOT A STYLE CHOICE. The
// Python loop used to drop a line whose FIRST FIELD had already been seen — before
// parsing it, before validating it, silently. Two measured failures, both fail-OPEN:
// a bare row followed by a mapped row for the same token loaded as ONE
// UNRESTRICTED row (the mapped row simply did not exist), and a second row carrying
// an invalid identity AND an invalid scope loaded clean because it was dropped
// before guards 6-10 ran. So: parse first, collapse after, and only rows that grant
// the SAME THING collapse.
//
// 🔴 GUARD 11 RUNS BEFORE GUARD 12, AND THE ORDER IS LOAD-BEARING. A file holding
// one row twice, verbatim, must collapse to one record — otherwise guard 12 would
// see two rows claiming one identity and refuse the ordinary "I pasted the line
// twice" file.
//
// 🔴 GUARD 12 EXEMPTS `legacy`, AND THAT IS NOT AN OVERSIGHT. Two legacy rows are an
// overlap rotation of the old shared token, which is the exact thing guards 1-5 were
// built to support. Two rows naming ONE mapped identity are different: if their
// allowlists disagree there is no defined answer, and if they agree the operator
// wanted `<identity>-prev`.
func LoadTokens(tokenFile string, env map[string]string, warn func(string)) ([]TokenRecord, error) {
	var raw string
	switch {
	case tokenFile != "":
		// GUARD 2 — `IsRegular`, NOT `!IsDir`. See IsTokenFile: `!IsDir` accepted
		// every non-directory, and a FIFO then blocked `os.ReadFile` FOREVER with no
		// log line, where the oracle's `is_file()` refuses in milliseconds.
		if !IsTokenFile(tokenFile) {
			return nil, fmt.Errorf("token file unreadable: %s is not a file", tokenFile)
		}
		data, readErr := os.ReadFile(tokenFile)
		if readErr != nil {
			return nil, fmt.Errorf("token file unreadable: %s (%v)", tokenFile, readErr)
		}
		// 🔴 GUARD 2b — THE FILE MUST DECODE AS TEXT, AND `string(data)` IS NOT A
		// DECODE. This guard has no NUMBER on the oracle's side because the oracle gets
		// it for free: `read_text(encoding="utf-8")` is strict, and the
		// `UnicodeDecodeError` it raises is a `ValueError`, so `main` prints the codec
		// message and exits EXIT_CONFIG. It is numbered 2b rather than 3 because it
		// answers the same question guard 2 does — "can this file be read AS TEXT at
		// all" — and must run before anything counts rows in it.
		//
		// 🔴 MEASURED, AND IT WAS A FULL-STORE READ RATHER THAN A PARSER NICETY. A token
		// file of 43 × `0xFF`, mode 0600: the oracle refuses to start ('utf-8' codec
		// can't decode byte 0xff in position 0). Go's `raw = string(data)` reinterpreted
		// the bytes, counted 43 runes, cleared MinTokenChars and parsed them as a BARE
		// LEGACY ROW — `UNRESTRICTED-SCOPE LEGACY MODE, 1 of 1 token rows`. With
		// SUBSYSTEM_STORE_TRUSTED_PROXIES set, as the conformance env and every real
		// deployment sets it, the server came up and `GET /api/v1/snapshot` with those
		// 43 bytes as the bearer token answered 200 WITH THE WHOLE STORE.
		//
		// ⚠ THE ENVIRONMENT FALLBACK BELOW IS DELIBERATELY **NOT** GUARDED, and that
		// asymmetry is the port being faithful rather than an omission. `os.environ` on
		// Linux is decoded with `surrogateescape`, so a non-UTF-8 env token becomes a
		// `str` carrying lone surrogates and the oracle LOADS it as a credential. Adding
		// a guard here would be a NEW divergence in the opposite direction.
		if problem := pytext.DecodeStrictProblem(data); problem != "" {
			return nil, fmt.Errorf(
				"token file is not valid UTF-8: %s (%s) — a credential file is TEXT, and a byte run that does not decode is refused here rather than reinterpreted, because ANY %d-byte run clears the length floor and would load as a BARE legacy row: an UNRESTRICTED-scope credential nobody wrote",
				tokenFile, problem, MinTokenChars)
		}
		raw = string(data)
	case envalias.Value(env, EnvToken) != "":
		raw = envalias.Value(env, EnvToken)
	default:
		return nil, fmt.Errorf(
			"no token source: pass --token-file, or set $%s. The API is not served without one", EnvToken)
	}

	// 🔴 THE INDEX CARRIED FORWARD IS THE PHYSICAL LINE NUMBER, NOT THE ROW'S
	// POSITION IN THIS LIST. The loop skips blank lines, so the two are not the
	// same thing — and "the operator can find the line" is the ENTIRE
	// justification for carrying an index through guard 12 at all. `total` is the
	// physical line COUNT for the same reason, so "line 6 of 6" is countable in an
	// editor.
	lines := pytext.SplitLines(raw)
	total := len(lines)
	type row struct {
		lineno int
		fields []string
	}
	var rows []row
	for i, text := range lines {
		fields := pytext.SplitWhitespace(text)
		if len(fields) == 0 {
			continue
		}
		rows = append(rows, row{lineno: i + 1, fields: fields})
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("token is empty: the source resolved to whitespace only")
	}
	// 🔴 GUARD 4 COUNTS CREDENTIALS, NOT ROWS. Counting physical rows measured as:
	// 4 distinct tokens plus ONE verbatim duplicate line answered "too many
	// tokens: 5, max 4" for a file holding 4 credentials, and five copies of one
	// token said the same for ONE. That contradicts guard 11, which calls a
	// duplicated row "the rotation shape, and it is legitimate".
	credentials := map[string]struct{}{}
	for _, r := range rows {
		credentials[r.fields[0]] = struct{}{}
	}
	if len(credentials) > MaxTokens {
		return nil, fmt.Errorf(
			"too many tokens: %d, max %d. Every DISTINCT token is a live credential; retire the old ones instead of accumulating them",
			len(credentials), MaxTokens)
	}
	for _, r := range rows {
		if len([]rune(r.fields[0])) < MinTokenChars {
			return nil, fmt.Errorf(
				"token on line %d of %d is too short: %d chars, need >= %d (256 bits base64url). A short token is a guessable one",
				r.lineno, total, len([]rune(r.fields[0])), MinTokenChars)
		}
	}

	records := make([]TokenRecord, 0, len(rows))
	for _, r := range rows {
		record, err := ParseTokenRow(r.fields, r.lineno, total)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}

	// GUARD 11 — one token, one authority. Runs on PARSED records, so a row that
	// would be collapsed has already been through guards 6-10, and two rows that
	// merely SPELL one grant differently are recognised as the same grant rather
	// than as a disagreement: case and `_` vs `-` (by the parser), a repeated
	// scope (by the parser), and scope-list ORDER (by authorityKey, which compares
	// the SET).
	type seenRow struct {
		lineno int
		record TokenRecord
	}
	firstSeen := map[string]seenRow{}
	var collapsed []seenRow
	for i, r := range rows {
		record := records[i]
		prior, present := firstSeen[record.Token]
		if !present {
			firstSeen[record.Token] = seenRow{r.lineno, record}
			collapsed = append(collapsed, seenRow{r.lineno, record})
			continue
		}
		if authorityKey(record) == authorityKey(prior.record) {
			// The rotation shape, and it is legitimate: one row written twice.
			// Order is kept because the FIRST occurrence is the one retained —
			// which also decides which SPELLING of the scope list survives.
			continue
		}
		return nil, fmt.Errorf(
			"duplicate token on lines %d and %d: one credential is given two different authorities — %s and %s — and there is no defined precedence between them. Scoping a token its holder already has means EDITING the bare row, not adding a second one below it; a second holder needs a second token",
			prior.lineno, r.lineno, authorityOf(prior.record), authorityOf(record))
	}

	// GUARD 12 — one row per mapped identity, indexed by PHYSICAL LINE carried
	// through the collapse above.
	byIdentity := map[string]int{}
	for _, c := range collapsed {
		if c.record.IsLegacy() {
			continue
		}
		if first, present := byIdentity[c.record.Identity]; present {
			return nil, fmt.Errorf(
				"duplicate identity '%s': token rows on lines %d and %d both claim it, and their scope allowlists have no defined precedence. Rotate a mapped credential under a second identity (%s-prev), not a second row",
				c.record.Identity, first, c.lineno, c.record.Identity)
		}
		byIdentity[c.record.Identity] = c.lineno
	}

	// 🔴 REBOUND ONCE, so the banner's "N of M" and the returned SET both read the
	// collapsed list. A second name kept alongside is how a later edit ends up
	// counting one list and returning the other.
	out := make([]TokenRecord, 0, len(collapsed))
	for _, c := range collapsed {
		out = append(out, c.record)
	}

	var legacy []TokenRecord
	for _, record := range out {
		if record.IsLegacy() {
			legacy = append(legacy, record)
		}
	}
	if len(legacy) > 0 && warn != nil {
		// 🔴 ONE LOUD LINE, EMITTED HERE RATHER THAN BY THE CALLER. This is the
		// only place that knows a row was bare, and a caller obliged to re-derive
		// it is a caller that can forget to.
		fingerprints := make([]string, 0, len(legacy))
		for _, record := range legacy {
			fingerprints = append(fingerprints, record.Fingerprint())
		}
		warn(fmt.Sprintf(
			"subsystem-store-api: 🔴 UNRESTRICTED-SCOPE LEGACY MODE — %d of %d token rows are bare tokens with no identity and NO scope allowlist (identity='%s'); they can read EVERY scope in the store. Fingerprints: %s. Give each holder its own `<token> <identity> <scopes>` row and delete these lines",
			len(legacy), len(out), LegacyIdentity, strings.Join(fingerprints, ",")))
	}
	return out, nil
}

// PresentedToken pulls the bearer credential out of an Authorization header.
//
// Returns "" for anything that is not a well-formed `Bearer <x>`, so the caller has
// ONE thing to compare and cannot accidentally branch on WHY it was absent.
func PresentedToken(header string) string {
	if header == "" {
		return ""
	}
	// 🔴 SPLIT ON A RUN OF WHITESPACE, NOT ON ONE SPACE, BECAUSE THE ORACLE DOES.
	// `str.split(None, 1)` treats any whitespace run as the separator, so
	// `Bearer\ttoken` authenticates there. Splitting on a single space refused it here
	// — a port that is STRICTER than the oracle on an authentication header is a port
	// that rejects a client the deployed pod accepts, which is a cutover failure rather
	// than a hardening. RFC 9235 does specify SP, so the oracle is the lenient one; the
	// point is that they must agree, and the oracle is the contract.
	fields := pytext.SplitWhitespace(header)
	if len(fields) < 2 || !strings.EqualFold(fields[0], "bearer") {
		return ""
	}
	// The credential is everything after the FIRST run, with its own edges stripped —
	// so an embedded space inside a (malformed) credential is preserved rather than
	// silently truncated, exactly as a one-split does.
	rest := strings.TrimPrefix(pytext.StripWhitespace(header), fields[0])
	return pytext.StripWhitespace(rest)
}

func dedupe(items []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if _, dup := seen[item]; dup {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}
