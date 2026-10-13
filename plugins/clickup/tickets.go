package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// 🔴 CLICKUP IS READ, NEVER WRITTEN (operator decision O6; write-back is the plan's B5, future work).
// This file holds the ONLY code that talks to ClickUp, and it has ONE request shape: a `GET` of a
// list's tasks. There is no method here that could send anything else, and the suite asserts the fake
// ClickUp saw nothing but `GET`s.
//
// 🔴 THE TOKEN IS READ FROM A FILE AND SENT IN ONE HEADER (`secretfile`). It is never a flag value,
// never in the environment, and an error body is quoted back only after every occurrence of it is
// replaced.
//
// 🔴 A 429 IS WAITED OUT, NOT RETRIED BLINDLY (R5): ClickUp limits requests per token per minute and
// answers 429 with `X-RateLimit-Reset`, the Unix time the window reopens. The client sleeps until then
// — bounded by MaxWait, past which it gives up rather than hold a timer run open — and asks again.
//
// What is kept of a task: its id, its custom id and its name. The NAME is held in memory for the
// model's candidate list and is stored on the pod only when the instance and the operator both say so
// (Q8); descriptions, comments and assignees are never read off the answer at all.

// Ticket is one ClickUp task as far as this plugin reads it.
type Ticket struct {
	// ID is the CANONICAL task id — what `https://app.clickup.com/t/<id>` addresses.
	ID string
	// CustomID is the workspace's custom task id (`PREFIX-123`), "" when it has none.
	CustomID string
	Name     string
}

var (
	// canonicalID is the pod's rule for a ticket id (`internal/plugins` outputs.go): alphanumeric, so
	// it can carry no `/`, no scheme and no markup.
	canonicalID = regexp.MustCompile(`^[A-Za-z0-9]{1,32}$`)
	// customIDShape is the pod's rule for a custom id.
	customIDShape = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
	listIDShape   = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	allDigits     = regexp.MustCompile(`^[0-9]+$`)
)

// ClickUp is the read-only client.
type ClickUp struct {
	// Base is the API's base URL, no trailing path (`https://api.clickup.com`).
	Base  string
	Token string
	HTTP  *http.Client
	// MaxWait bounds ONE wait for a rate-limit window.
	MaxWait time.Duration
	Log     func(string)
	// Now is the clock a reset time is compared with; nil is time.Now.
	Now func() time.Time
}

// DefaultClickUpURL is ClickUp's public API.
const DefaultClickUpURL = "https://api.clickup.com"

const (
	// maxPages bounds one list's fetch: 100 tasks a page, so 10,000 tasks.
	maxPages = 100
	// maxAttempts bounds how often ONE request is asked again after a 429.
	maxAttempts = 4
	// fallbackWait is the wait when a 429 names no reset this client can read: the window is a minute.
	fallbackWait = 60 * time.Second
	maxListBody  = 16 << 20
)

func (c *ClickUp) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *ClickUp) scrub(s string) string {
	if c.Token == "" {
		return s
	}
	return strings.ReplaceAll(s, c.Token, "[token]")
}

// resetWait is how long a 429 says to wait. `X-RateLimit-Reset` is a Unix time — seconds in every
// answer observed in the documentation, and a value too large to be seconds is read as milliseconds —
// and `Retry-After` (seconds) is honoured when that header is absent.
func (c *ClickUp) resetWait(h http.Header) time.Duration {
	if v := strings.TrimSpace(h.Get("X-RateLimit-Reset")); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			at := time.Unix(n, 0)
			if n > 100_000_000_000 {
				at = time.UnixMilli(n)
			}
			// One second past the stated instant: the header has whole-second precision.
			return max(at.Sub(c.now())+time.Second, 0)
		}
	}
	if v := strings.TrimSpace(h.Get("Retry-After")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return time.Duration(n) * time.Second
		}
	}
	return fallbackWait
}

// get is the ONE request this client makes: a GET, with the 429 wait.
func (c *ClickUp) get(ctx context.Context, path string, q url.Values, into any) error {
	u := strings.TrimRight(c.Base, "/") + path + "?" + q.Encode()
	for attempt := 1; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return fmt.Errorf("clickup %s: %s", path, c.scrub(err.Error()))
		}
		// A ClickUp personal token is sent bare — no `Bearer`.
		req.Header.Set("Authorization", c.Token)
		req.Header.Set("Accept", "application/json")
		resp, err := c.HTTP.Do(req)
		if err != nil {
			return fmt.Errorf("clickup %s could not be reached: %s", path, c.scrub(err.Error()))
		}
		raw, err := io.ReadAll(io.LimitReader(resp.Body, maxListBody+1))
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("clickup %s: reading the answer: %s", path, c.scrub(err.Error()))
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			wait := c.resetWait(resp.Header)
			if attempt >= maxAttempts {
				return fmt.Errorf("clickup %s answered 429 %d times", path, attempt)
			}
			if wait > c.MaxWait {
				return fmt.Errorf("clickup %s answered 429 and its window reopens in %s, past the %s this run will wait",
					path, wait.Round(time.Second), c.MaxWait)
			}
			c.Log(fmt.Sprintf("clickup: rate limited (429) on %s; waiting %s for the window to reopen", path, wait.Round(time.Millisecond)))
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return fmt.Errorf("clickup %s: %w while waiting out a 429", path, ctx.Err())
			}
			continue
		}
		if resp.StatusCode != http.StatusOK {
			detail := c.scrub(string(raw))
			if len(detail) > 300 {
				detail = detail[:300]
			}
			return fmt.Errorf("clickup %s answered %d: %s", path, resp.StatusCode, strings.TrimSpace(detail))
		}
		if len(raw) > maxListBody {
			return fmt.Errorf("clickup %s: the answer is over %d bytes", path, maxListBody)
		}
		if err := json.Unmarshal(raw, into); err != nil {
			return fmt.Errorf("clickup %s: the answer is not the documented JSON: %w", path, err)
		}
		return nil
	}
}

// List is every task of one list, most recently updated first, closed tasks and subtasks included: a
// session may well be the work that closed its ticket.
func (c *ClickUp) List(ctx context.Context, listID string) ([]Ticket, error) {
	if !listIDShape.MatchString(listID) {
		return nil, fmt.Errorf("list id %q is not a ClickUp list id", listID)
	}
	var out []Ticket
	for page := 0; ; page++ {
		if page >= maxPages {
			c.Log(fmt.Sprintf("clickup: list %s holds more than %d tasks; only its most recently updated %d are read",
				listID, len(out), len(out)))
			return out, nil
		}
		var ans struct {
			Tasks *[]struct {
				ID       string  `json:"id"`
				CustomID *string `json:"custom_id"`
				Name     string  `json:"name"`
			} `json:"tasks"`
			LastPage bool `json:"last_page"`
		}
		q := url.Values{"page": {strconv.Itoa(page)}, "include_closed": {"true"}, "subtasks": {"true"},
			"order_by": {"updated"}}
		if err := c.get(ctx, "/api/v2/list/"+listID+"/task", q, &ans); err != nil {
			return nil, err
		}
		if ans.Tasks == nil {
			// An answer without the key is not "an empty list" — it is not the contract.
			return nil, errors.New("clickup: the answer has no `tasks`")
		}
		for _, t := range *ans.Tasks {
			tk := Ticket{ID: t.ID, Name: t.Name}
			if t.CustomID != nil {
				tk.CustomID = *t.CustomID
			}
			out = append(out, tk)
		}
		if ans.LastPage || len(*ans.Tasks) == 0 {
			return out, nil
		}
	}
}

// Index is the workspace's CLOSED ticket list as the matchers read it (decision 14): a custom id, or a
// bare id in a branch name, matches only by MEMBERSHIP here — never by looking like an id.
type Index struct {
	byID     map[string]Ticket
	byCustom map[string]string // custom id → canonical id
	// byFirst is each custom id under its leading alphanumeric run, longest id first — what lets a
	// text be searched once rather than once per ticket.
	byFirst map[string][]string
	// order is the canonical ids in fetch order (most recently updated first).
	order []string
	// Dropped counts tasks whose id is not a canonical id; they are in no map.
	Dropped int
}

// NewIndex builds the index. A task whose id is not canonical is dropped and counted; a custom id
// that is not custom-id shaped, or that two tasks claim, resolves to nothing.
func NewIndex(tickets []Ticket) *Index {
	ix := &Index{byID: map[string]Ticket{}, byCustom: map[string]string{}, byFirst: map[string][]string{}}
	ambiguous := map[string]bool{}
	for _, t := range tickets {
		if !canonicalID.MatchString(t.ID) {
			ix.Dropped++
			continue
		}
		if !customIDShape.MatchString(t.CustomID) {
			t.CustomID = ""
		}
		if _, dup := ix.byID[t.ID]; dup {
			continue
		}
		ix.byID[t.ID] = t
		ix.order = append(ix.order, t.ID)
		if t.CustomID == "" {
			continue
		}
		if prev, taken := ix.byCustom[t.CustomID]; taken && prev != t.ID {
			ambiguous[t.CustomID] = true
			continue
		}
		ix.byCustom[t.CustomID] = t.ID
	}
	for c := range ambiguous {
		delete(ix.byCustom, c)
	}
	for c := range ix.byCustom {
		first := firstRun(c)
		ix.byFirst[first] = append(ix.byFirst[first], c)
	}
	for _, list := range ix.byFirst {
		slices.SortFunc(list, func(a, b string) int {
			if len(a) != len(b) {
				return len(b) - len(a)
			}
			return strings.Compare(a, b)
		})
	}
	return ix
}

// Len is how many tickets the index holds.
func (ix *Index) Len() int { return len(ix.order) }

// Has reports whether id is a fetched canonical id.
func (ix *Index) Has(id string) bool { _, ok := ix.byID[id]; return ok }

// Resolve turns a custom id into its ticket; ok is false when the closed list does not hold it.
func (ix *Index) Resolve(custom string) (Ticket, bool) {
	id, ok := ix.byCustom[custom]
	if !ok {
		return Ticket{}, false
	}
	return ix.byID[id], true
}

func isAlnum(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// firstRun is s's leading alphanumeric run.
func firstRun(s string) string {
	i := 0
	for i < len(s) && isAlnum(s[i]) {
		i++
	}
	return s[:i]
}
