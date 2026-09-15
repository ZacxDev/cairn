package client

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultTimeout is the store commands' bound, in seconds. A DEFAULT, never a ceiling.
const DefaultTimeout = 20

// StoreUnreachable is "the pod could not be read". It carries the reason, never a bare
// failure.
//
// 🔴 THE REASON IS MANDATORY: an unreachable host is reported as UNMEASURED **with a
// reason**, never as a silent success or a zero.
//
// 🔴 `HTTPStatus` IS THE STRUCTURAL DISCRIMINATOR, AND IT EXISTS BECAUSE THE MESSAGE IS NOT
// ONE. "the host never answered" and "the host answered 401" are both raised here —
// deliberately, since a read degrades to the cache either way — but they are NOT the same
// fact to a human: one is an outage, the other a credential to replace. The only thing that
// told them apart was the phrasing of the message, and a caller that greps for `HTTP 401`
// has pinned a format string. Zero means there was no answer at all.
type StoreUnreachable struct {
	Reason     string
	HTTPStatus int
}

func (e *StoreUnreachable) Error() string { return e.Reason }

func unreachable(format string, args ...any) *StoreUnreachable {
	return &StoreUnreachable{Reason: fmt.Sprintf(format, args...)}
}

// StoreCorrupt is "the server answered, and what it sent is not a store we will accept".
//
// 🔴 DELIBERATELY NOT A `StoreUnreachable`. An outage is absorbed into "serving from cache"
// at exit 0, which is right for an outage and wrong for this: a server shipping a traversal
// member, a link, a duplicate, or a count that disagrees with its own header is the one case
// that has to be LOUD. Collapsing the two would let a hostile or broken archive render as a
// reassuring `⚠ SERVED FROM CACHE`.
type StoreCorrupt struct{ Reason string }

func (e *StoreCorrupt) Error() string { return e.Reason }

func corrupt(format string, args ...any) *StoreCorrupt {
	return &StoreCorrupt{Reason: fmt.Sprintf(format, args...)}
}

// Config is the resolved `(url, token)`.
type Config struct {
	URL   string
	Token string
}

// DefaultConfigPath is `~/.config/subsystem-store/env`, mode 0600.
func DefaultConfigPath() string {
	if override := strings.TrimSpace(os.Getenv("SUBSYSTEM_STORE_CONFIG")); override != "" {
		return expandUser(override)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "subsystem-store", "env")
}

// LoadConfig is `(url, token)` from the env file, with the real environment taking priority.
//
// A missing FILE is not fatal on its own — the caller may have exported both values — but a
// missing VALUE is, and the refusal says WHICH one. "No token" must not degrade into an
// unauthenticated request that comes back 401 and gets read as "the store is down".
func LoadConfig() (Config, error) {
	path := DefaultConfigPath()
	fromFile := map[string]string{}
	if data, err := os.ReadFile(path); err == nil {
		for _, line := range splitLines(string(data)) {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
				continue
			}
			key, value, _ := strings.Cut(line, "=")
			fromFile[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	pick := func(name string) string {
		if v := os.Getenv(name); v != "" {
			return v
		}
		return fromFile[name]
	}
	cfg := Config{URL: pick("SUBSYSTEM_STORE_URL"), Token: pick("SUBSYSTEM_STORE_TOKEN")}
	var missing []string
	if cfg.URL == "" {
		missing = append(missing, "SUBSYSTEM_STORE_URL")
	}
	if cfg.Token == "" {
		missing = append(missing, "SUBSYSTEM_STORE_TOKEN")
	}
	if len(missing) > 0 {
		return Config{}, unreachable(
			"config incomplete: %s not set (looked in %s and the environment)",
			strings.Join(missing, ", "), path)
	}
	cfg.URL = strings.TrimRight(cfg.URL, "/")
	return cfg, nil
}

// UnboundedTimeoutReason is why `seconds` is not a usable timeout, or "" if it is fine.
//
// 🔴 CONSOLIDATING THIS IS WHAT FOUND THE BUG IT GUARDS AGAINST on the Python side: the rule
// was open-coded at two call sites and the copies DISAGREED. Here the whole `bool` half of
// that trap is structurally absent — Go has no `bool` that subclasses `int` — so what
// survives is the non-positive check, which is the half that matters: a zero or negative
// bound reaches an HTTP client as NO deadline, i.e. an unbounded wait rather than a default.
func UnboundedTimeoutReason(seconds int) string {
	if seconds <= 0 {
		return fmt.Sprintf("timeout=%d is not positive — a non-positive bound is "+
			"an UNBOUNDED wait, not a default", seconds)
	}
	return ""
}

// 🔴 THE USER-AGENT IS REQUIRED AND THE DEFAULT IS ACTIVELY REFUSED. Measured against the
// live host with the same token and path, three requests differing ONLY in this header:
// curl's default UA answered 200, `Python-urllib/3.12` answered 403, an empty UA answered
// 200. The 403 comes from the EDGE, not the app, so it is neither a token rejection nor the
// store being down — but it arrives looking like both.
//
// ⚠ THE SPELLING IS THE ORACLE'S, AND IT IS NOT VERSIONED PER IMPLEMENTATION. Two clients
// sending two UAs would make an edge rule that allowed one and refused the other a failure
// nobody could attribute, and the parity gate compares bytes the server sends BACK — so the
// request has to be the same request.
const userAgent = "subsystem-store-client/1"

// applyStandardHeaders is the two headers EVERY request to this host must carry, in ONE
// place. Three call sites (`FetchSnapshot`, `SendWrite`, and `doctor`'s probe, which reuses
// `FetchSnapshot` rather than building a second request) make it a rule, and a rule at three
// sites is the shape that regenerates the same bug at N−1 of them.
func applyStandardHeaders(req *http.Request, token string) {
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", userAgent)
}

// httpClient is one bounded client. 🔴 THE BOUND IS A WHOLE-REQUEST DEADLINE, WHICH IS
// STRICTER THAN `urlopen`'s. `urlopen(timeout=n)` bounds each socket operation; a server
// that dribbles one byte per `n-1` seconds can hold a Python client forever and cannot hold
// this one. The difference is in the safe direction for a bounded read and is stated because
// a slow-but-alive pod could be reported unreachable here and served there.
func httpClient(timeoutSeconds int) *http.Client {
	return &http.Client{
		Timeout: time.Duration(timeoutSeconds) * time.Second,
		// 🔴 NO REDIRECT FOLLOWING BEYOND THE DEFAULT, AND NO CREDENTIAL STRIPPING GAME.
		// Go's default policy follows up to 10 redirects and DOES forward the
		// `Authorization` header only to the same host; that is the behaviour `urllib`'s
		// `HTTPRedirectHandler` has too. Left at the default deliberately rather than
		// hardened, so the two clients follow the same edge behaviour.
	}
}

// FetchSnapshot GETs the tar. Every failure becomes a StoreUnreachable NAMING the host.
//
// 🔴 A NON-POSITIVE TIMEOUT IS REFUSED RATHER THAN TRUSTED. The annotation is a type, and a
// type is not a code path; on the Python side mutating either the caller's or the socket
// call's argument to `None` survived the whole suite.
func FetchSnapshot(cfg Config, scope string, timeout int) ([]byte, http.Header, error) {
	if bad := UnboundedTimeoutReason(timeout); bad != "" {
		return nil, nil, unreachable("refusing to fetch %s: %s", cfg.URL, bad)
	}
	target := cfg.URL + "/api/v1/snapshot"
	if scope != "" {
		target += "?scope=" + scope
	}
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return nil, nil, unreachable("%s unreachable: %s", cfg.URL, err)
	}
	applyStandardHeaders(req, cfg.Token)
	resp, err := httpClient(timeout).Do(req)
	if err != nil {
		// ⚠ ONE ARM WHERE THE ORACLE HAS TWO. `urllib` raises `URLError` (whose
		// `.reason` is interpolated) for a DNS or connect failure and a bare `OSError`
		// for a socket timeout, and the two sentences differ in their tail only. Go
		// returns one `*url.Error` for both, so the tail is Go's. The PREFIX — the host
		// and the word `unreachable` — is what a reader greps and what is preserved.
		return nil, nil, unreachable("%s unreachable: %s", cfg.URL, urlErrorReason(err))
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		// 🔴 CARRY THE BODY. The server's 503 names WHICH scope it could not read;
		// reporting only "answered HTTP 503" throws that away and leaves the operator
		// with a code and no cause — the opposite of "UNMEASURED with a reason".
		detail := ""
		if readErr == nil {
			if line := oneLine(body, 300); line != "" {
				detail = " — " + line
			}
		}
		return nil, nil, &StoreUnreachable{
			Reason:     fmt.Sprintf("%s answered HTTP %d%s", cfg.URL, resp.StatusCode, detail),
			HTTPStatus: resp.StatusCode,
		}
	}
	if readErr != nil {
		return nil, nil, unreachable("%s unreachable: %s", cfg.URL, readErr)
	}
	// 🔴 THE HEADER MAP IS RETURNED, NOT A `map[string]string` BUILT FROM IT. `http.Header`
	// canonicalises on `Get`, so `X-Store-Entries` and `x-store-entries` are one key —
	// which is the whole incident the oracle records: this host is behind a proxy that
	// lowercases every header name under HTTP/2, a hand-built dict asked for the mixed-case
	// spelling and got `None`, and the count cross-check against `X-Store-Entries` was
	// therefore INERT in the only environment that matters.
	return body, resp.Header, nil
}

// urlErrorReason unwraps Go's `*url.Error` to the thing that actually failed, so the
// sentence reads `<host> unreachable: dial tcp …` rather than repeating the method and URL
// the sentence already names.
func urlErrorReason(err error) string {
	var uerr *url.Error
	if errors.As(err, &uerr) {
		return uerr.Err.Error()
	}
	return err.Error()
}
