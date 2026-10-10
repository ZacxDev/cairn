package redact

import (
	"encoding/base64"
	"math"
	"regexp"
	"strings"
)

// 🔴 ENTROPY (operator decision O15): a long token that looks RANDOM is redacted wherever it
// stands, named or not. It is the rule that needs no notation at all, which is why it exists —
// a token pasted into prose, a command, a URL path or a log line has no key for the key-context
// rule to read.
//
// A run is a maximal stretch of the base64/base64url alphabet (`A-Z a-z 0-9 + / _ -`, then up to
// two `=`), skipped whole when an integrity-digest label stands before it (`h1:`, `sha256:`,
// `sha512-`) or it is the base64 of a BINARY payload (O12). A run that reads as a PATH — some
// `/`-separated piece is a lower-case word — is split at `/` and each piece judged alone
// ([pathPieces]). A piece, trimmed of edge separators, is redacted when ALL of:
//
//   - it is at least [MinEntropyToken] characters;
//   - it carries an upper-case letter, a lower-case letter AND a digit — so a git SHA, a sha256
//     digest, a lower-case UUID, a Nix store hash and a `snake_case_identifier` are not candidates
//     (⚠ the cost: a lower-case or hex secret with no key in front of it is not caught by this rule);
//   - it is not WORDY ([wordy]): under [maxWordFraction] of it sits in word-shaped letter runs
//     (`[A-Z]?[a-z]{3,}`), and not 45% with a third of its letters vowels;
//   - it changes character class at no fewer than [minTransitionRate] of its positions;
//   - it is not an identifier, slug or path by its `_`/`-`/`/` segments — two thirds of them words,
//     holding at least 35% of its characters ([identifierSegments]) — nor an alphabet literal
//     ([sequential]);
//   - its Shannon entropy is at least [minEntropyBits] bits per character;
//   - it is not a structural ID a transcript is made of (`toolu_`, `msg_`, `req_`, `ses_`, `prt_`,
//     `call_`) or an SRI digest (`sha512-…`), and not itself the base64 of a binary payload.
//
// The thresholds are MEASURED, not argued: `TestTheEntropyRuleThresholds` pins the catch rate on
// random tokens and the clean shapes it was tuned against (with a positive control).
//
// ⚠ THE COST ON CLEAN TEXT, measured while tuning: an ACRONYM-heavy identifier of 20+ characters
// with digits (`UTF8StringToUTF16LEBytes`, `AWSSDKv2S3PutObjectInput`) passes every test above and
// IS redacted; a random token under 20 characters, or one missing a character class (about 2% of
// 20-character alphanumerics), is not.

// MinEntropyToken is the shortest run the entropy rule considers.
const MinEntropyToken = 20

const (
	maxWordFraction = 0.7
	minEntropyBits  = 3.5
)

var (
	entropyRun = regexp.MustCompile(`[A-Za-z0-9+/_\-]{20,}={0,2}`)
	wordRun    = regexp.MustCompile(`[A-Z]?[a-z]{3,}`)
	// Structural IDs of the transcripts themselves, and integrity digests.
	idPrefix     = regexp.MustCompile(`^(?:srvtoolu|toolu|msg|req|ses|prt|call)_|^sha(?:1|256|384|512)-`)
	digestBefore = regexp.MustCompile(`(?:\bh1:|\bsha(?:1|256|384|512)[:-])$`)
)

func entropySpans(s string) [][2]int {
	var out [][2]int
	for _, loc := range entropyRun.FindAllStringIndex(s, -1) {
		if digestBefore.MatchString(s[max(0, loc[0]-8):loc[0]]) || binaryBase64(s[loc[0]:loc[1]]) {
			continue
		}
		for _, p := range pathPieces(s, loc[0], loc[1]) {
			lo, hi := p[0], p[1]
			// Separators at the edges are not part of a token.
			for lo < hi && strings.IndexByte("-_/=+", s[lo]) >= 0 {
				lo++
			}
			for hi > lo && strings.IndexByte("-_/", s[hi-1]) >= 0 {
				hi--
			}
			if hi-lo < MinEntropyToken || !looksRandom(s[lo:hi]) || idPrefix.MatchString(s[lo:hi]) || binaryBase64(s[lo:hi]) {
				continue
			}
			out = append(out, [2]int{lo, hi})
		}
	}
	return out
}

var pathWord = regexp.MustCompile(`^[a-z]{3,}(?:[-_.][a-z0-9]+)*$`)

// pathPieces splits a run at `/` when it reads as a PATH — some piece is a lower-case word
// (`tool-results`, `firefox`) — so each piece is judged on its own: a random ID in a path is
// still caught, the path around it is not. A base64 run is one piece (its `/`-separated pieces
// are not lower-case words).
func pathPieces(s string, lo, hi int) [][2]int {
	run := s[lo:hi]
	if !strings.Contains(run, "/") {
		return [][2]int{{lo, hi}}
	}
	parts := strings.Split(run, "/")
	path := false
	for _, p := range parts {
		path = path || pathWord.MatchString(p)
	}
	if !path {
		return [][2]int{{lo, hi}}
	}
	var out [][2]int
	at := lo
	for _, p := range parts {
		out = append(out, [2]int{at, at + len(p)})
		at += len(p) + 1
	}
	return out
}

// looksRandom is the token test, without the context checks.
func looksRandom(t string) bool {
	var up, low, dig bool
	for i := 0; i < len(t); i++ {
		c := t[i]
		switch {
		case c >= 'A' && c <= 'Z':
			up = true
		case c >= 'a' && c <= 'z':
			low = true
		case c >= '0' && c <= '9':
			dig = true
		}
	}
	if !up || !low || !dig {
		return false
	}
	if wordy(t) || transitionRate(t) < minTransitionRate || identifierSegments(t) || sequential(t) {
		return false
	}
	return shannon(t) >= minEntropyBits
}

// wordy: most of the token sits in word-shaped letter runs, or a good part does AND its letters
// carry English's share of vowels (about a third; a random token's letters about a fifth) —
// `TestAnUnknownRouteIsA404AndNeverA405`, whose short words (`Is`, `A`) keep its runs under 0.7.
func wordy(t string) bool {
	wf := wordFraction(t)
	return wf >= maxWordFraction || (wf >= 0.45 && vowelRatio(t) >= 0.3)
}

func vowelRatio(t string) float64 {
	letters, vowels := 0, 0
	for i := 0; i < len(t); i++ {
		c := t[i] | 0x20
		if c >= 'a' && c <= 'z' {
			letters++
			if strings.IndexByte("aeiou", c) >= 0 {
				vowels++
			}
		}
	}
	if letters == 0 {
		return 0
	}
	return float64(vowels) / float64(letters)
}

// minTransitionRate: a random token changes character class (upper, lower, digit, other) at about
// 0.6 of its positions; a camelCase identifier at about one in five — once per word
// (`TestA401WithoutWWWAuthenticate` measures 0.23).
const minTransitionRate = 0.35

func transitionRate(t string) float64 {
	class := func(c byte) int {
		switch {
		case c >= 'A' && c <= 'Z':
			return 0
		case c >= 'a' && c <= 'z':
			return 1
		case c >= '0' && c <= '9':
			return 2
		}
		return 3
	}
	n := 0
	for i := 1; i < len(t); i++ {
		if class(t[i]) != class(t[i-1]) {
			n++
		}
	}
	return float64(n) / float64(len(t)-1)
}

var wordSegment = regexp.MustCompile(`^(?:[A-Za-z][a-z]*|[A-Z]+|(?:[A-Z][a-z]+){2,}[0-9]{0,2}|[0-9]{1,4}|[A-Za-z]{1,3}[0-9]{1,3}[a-z]*)$`)

// minWordChars is the share of an identifier's characters that must sit in word segments.
const minWordChars = 0.35

// identifierSegments: split on `_`, `-` and `/`, a token of at least three segments is an
// identifier, a slug or a path — `test_a_404_carries_ETag`, `en-US/firefox/12x/notes` — when its
// word segments (`Foo`, `bar`, `HTTP`, `ClientCert`, `ImmSigned16`, `404`, `v2`) hold two thirds of its
// CHARACTERS, or are two thirds of its SEGMENTS and hold at least [minWordChars] of its
// characters. A random base64url token has a separator about once in 32 characters and its pieces
// are not words.
//
// 🔴 EITHER COUNT, WITH A FLOOR, BECAUSE EACH ALONE FAILED (review rounds 4 and 5). Counted by
// SEGMENT alone (round 4), base64 with a `/` near each end (`qk/<59 random characters>/VOC`) read
// as a path of three segments, two of them "words", and shipped. Counted by CHARACTER alone
// (round 5), one non-word segment sank a real identifier — `ClientCert-RSA-AES256-GCM-SHA384`,
// `_cgo_be59f0f25121_Cfunc_puts`, `GO_NID_X9_62_prime256v1`, the SPDX id `BSD-Systemics-W3Works` —
// and the rule redacted them: 1,068 entropy lines of the Go standard library's source that round 4
// had left alone. The segment count is back, and the character floor is what keeps a long random
// run with a short word at each end from passing it; the character rule stays for the identifiers
// only IT reads (`arg_Vd_arrangement_size_Q___8B_00__16B_01`, whose `8B`/`16B` are not words).
// `TestRoundSixIdentifierSegments` pins both directions.
func identifierSegments(t string) bool {
	segs := strings.FieldsFunc(t, func(r rune) bool { return r == '_' || r == '-' || r == '/' })
	if len(segs) < 3 {
		return false
	}
	words, wordChars, total := 0, 0, 0
	for _, s := range segs {
		total += len(s)
		if len(s) <= 16 && wordSegment.MatchString(s) {
			words++
			wordChars += len(s)
		}
	}
	return 3*wordChars >= 2*total || (3*words >= 2*len(segs) && float64(wordChars) >= minWordChars*float64(total))
}

// sequential: most adjacent pairs ascend by one — an alphabet literal (`ABC…xyz0123…`).
func sequential(t string) bool {
	n := 0
	for i := 1; i < len(t); i++ {
		if t[i] == t[i-1]+1 {
			n++
		}
	}
	return 2*n > len(t)
}

func wordFraction(t string) float64 {
	n := 0
	for _, m := range wordRun.FindAllStringIndex(t, -1) {
		n += m[1] - m[0]
	}
	return float64(n) / float64(len(t))
}

func shannon(t string) float64 {
	var counts [256]int
	for i := 0; i < len(t); i++ {
		counts[t[i]]++
	}
	h := 0.0
	n := float64(len(t))
	for _, c := range counts {
		if c > 0 {
			p := float64(c) / n
			h -= p * math.Log2(p)
		}
	}
	return h
}

// binaryBase64 reports a run that decodes, in any of the four encodings, to a binary payload.
func binaryBase64(t string) bool {
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(t); err == nil {
			return IsBinary(b)
		}
	}
	return false
}
