package ui

import (
	"errors"
	"io"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"time"

	g "maragu.dev/gomponents"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/identity"
	"github.com/ZacxDev/cairn/internal/netid"
	"github.com/ZacxDev/cairn/internal/report"
	"github.com/ZacxDev/cairn/internal/store"
)

// Source is the read half this surface needs, as an interface so the renderer's
// tests can build a world without a store on disk.
//
// 🔴 IT TAKES THE AUTHORIZATION, NOT THE PRINCIPAL. `control.Authorization` is the
// half of an `identity.Identity` that answers "what may this caller see", and it
// came out of the SAME model read that produced the principal. Handing the
// principal instead would make this interface's implementer resolve the authority
// a second time, which is the authenticated-against-one-world-authorised-against-
// another window `control.Principal`'s own comment forbids.
//
// 🔴 `Visible` IS THE WHOLE READ FOR ALL THREE BROWSE PAGES, AND THAT IS A DECISION
// RATHER THAN AN ACCIDENT. `GET /`, `GET /scope` and `GET /entry` are three views of
// ONE narrowed answer: the scope page picks a scope out of it and the entry page picks
// an entry out of that. A per-page read — `Entry(auth, scope, ref)` — was the obvious
// alternative and was refused, because it would put a SECOND place where "may this
// caller see this" is decided, and the two could then disagree about a scope. Deriving
// every page from one answer makes the refusal for "not yours" and the refusal for
// "does not exist" the SAME code path rather than two paths that must be kept
// byte-identical by discipline. See `browseRefusal`.
//
// ⚠ WHAT IT COSTS, MEASURED RATHER THAN WAVED AT: every page load parses every entry
// the caller may read. The store this serves is tens of kilobytes across tens of files
// — `report.Search`'s own comment measures a full scan of it in single-digit
// milliseconds, and search already does exactly that on every query. If a deployment
// ever outgrows that, the fix is a cache in front of THIS function, not a second
// narrowing seam behind it.
type Source interface {
	Visible(auth control.Authorization) ([]Scope, error)
	// Search answers the root page's `?q=`. It is the SAME engine the CLI and the pod
	// use — see [StoreSource.Search] for why a second matcher was never on the table.
	Search(auth control.Authorization, query string) (SearchResults, error)
}

// Scope is one scope's worth of entries, as the pages render them.
type Scope struct {
	// ID is the scope's control-plane id, which is what a URL addresses it by.
	//
	// 🔴 IT IS MINTED OVER A URL-SAFE ALPHABET AND THE NAME IS NOT, WHICH IS THE WHOLE
	// REASON THE LINK IS KEYED ON IT. `control.NewID`/`control.DerivedID` produce an
	// opaque token; a scope NAME is a directory somebody created and is user text. It is
	// also present on BOTH deployment shapes — `internal/control/tokenfile` mints
	// `scopeID(name)` for every scope it projects — so `/scope?id=` is reachable against
	// a token file as well as against a control journal. Empty is possible in principle
	// (an authority that named no id for a scope the index listed) and the card then
	// renders unlinked rather than linking nowhere.
	ID control.ID
	// Name is the scope's display name. USER TEXT.
	Name string
	// Entries are its entries in index order. Every string below is USER TEXT.
	Entries []Entry
	// Malformed are the entry files this scope holds that the loader REFUSED.
	//
	// 🔴 THEY ARE CARRIED RATHER THAN DROPPED, AND THE REASON IS THAT A BROWSER WHICH
	// DROPS THEM DISAGREES WITH THE CLI WHILE LOOKING COMPLETE. `store.LoadStore` loads
	// with `Collect` precisely so a bad entry costs one entry instead of the whole scope,
	// and `internal/report` renders every collected row. A page that rendered only the
	// good ones would show a shorter, tidier, WRONG store.
	Malformed []Malformed
}

// OpenCount is how many of this scope's entries carry at least one DECLARED `OPEN:`
// bullet, summed over entries.
//
// 🔴 A ZERO IS NOT "NOTHING IS OPEN", WHICH IS `report.RecalledEntry.OpenCount`'s OWN
// CAVEAT ONE LEVEL DOWN. The marker is opt-in, so zero means "nothing was declared".
// The card says `declared open` rather than `open` for that reason.
func (s Scope) OpenCount() int {
	n := 0
	for _, e := range s.Entries {
		n += e.OpenCount
	}
	return n
}

// Malformed is one entry file the loader refused, as the page shows it.
type Malformed struct {
	// Label is `<scope>/<filename>`, the spelling every surface in this tree uses.
	Label string
	// Reason is the loader's own refusal sentence. USER-INFLUENCED: it can quote the
	// file's own front matter.
	Reason string
}

// Entry is one entry as the pages render it.
//
// 🔴 EVERY FIELD HERE IS ATTACKER-INFLUENCED, AND `Tasks` IS THE ONE THAT LANDS IN
// A URL POSITION. A store entry's `tasks:` front-matter key is `<system>:<id>`,
// which is the same shape as a URL scheme followed by an opaque part — so
// `javascript:alert(document.domain)` is a WELL-FORMED task ref. See [safeHref].
//
// 🔴 AND `Sections` IS A SECOND URL-POSITION HAZARD IN A SHAPE THE FIRST ONE IS NOT:
// `Ref` reaches a QUERY parameter on this surface's own links. It is a filename stem
// from a file somebody else wrote, not a minted id, so it is percent-encoded on the way
// out (`entryHref`) and matched against the narrowed list on the way in — never parsed
// into a path. See `handleEntryPage`.
//
// ⚠ WHAT EACH FIELD IS IN THE UNDERLYING FILE, because the operator's complaint about
// the first version of this page was exactly that nobody could tell:
//
//	Ref         the addressable name: `<slug>` or `<slug>.<kind>` — the FILENAME minus `.md`
//	Title       `service:` in the front matter, which the loader pins equal to the slug
//	Filename    the file on disk under `<store root>/<scope>/`
//	Aliases     the `aliases:` front-matter sequence, AS WRITTEN (not the folded form)
//	Tasks       the `tasks:` front-matter sequence, AS WRITTEN
//	Sections    the `##` headings `report.SurfacedHeadings` names, with their bodies
//	Bullets     top-level `- ` lines under `## Nuance / work-history`, with continuations
type Entry struct {
	Ref      string
	Title    string
	Filename string
	Aliases  []string
	Tasks    []string

	// Sections are the surfaced headings this entry HAS, in
	// `report.SurfacedHeadings` order. A heading the entry does not carry is absent
	// from this slice and named in `MissingSections` instead — "the section was never
	// started" and "the section is there and unfilled" are different facts about a
	// curated entry and `store.ExtractSections` is careful to keep them apart.
	Sections []Section
	// MissingSections are the COUNTED headings this entry does not carry, spelled as
	// the file would have to spell them.
	MissingSections []string

	// BulletCount, OpenCount and NearMissCount are read off ONE predicate.
	//
	// 🔴 `store.JournalBullet.OpennessPopulation` IS THE SINGLE SOURCE OF THE
	// PRECEDENCE ORDER AND EVERY COUNT HERE COMES FROM IT, WHICH IS WHY THIS IS NOT A
	// SECOND IMPLEMENTATION OF MARKER PARSING. Its own comment records a delta audit
	// on the oracle that found ONE bullet counted twice because two surfaces each
	// decided membership for themselves. Counting `Openness == OpennessOpen` here
	// instead would be that second surface.
	BulletCount   int
	OpenCount     int
	NearMissCount int
}

// Section is one `##` heading of an entry file and what sits under it.
type Section struct {
	// Heading is the heading line VERBATIM, `##` included — the string
	// `store.ExtractSections` matched on, which is what a reader has to type to find
	// it in the file.
	Heading string
	// Body is everything under it, verbatim, with surrounding blank lines trimmed.
	// Rendered as text for a section that is not the journal.
	Body string
	// Bullets are the top-level journal bullets, EMPTY for every section but
	// `store.NuanceHeading`. A nuance section whose body is non-empty and which
	// yields no bullets is its own state — prose before the first bullet — and
	// `Body` is what shows it.
	Bullets []Bullet
}

// Bullet is one top-level journal bullet: the LINE ITEM the operator asked to be able
// to see.
type Bullet struct {
	// Lines is the bullet VERBATIM including its continuation lines. It is a slice
	// because a real bullet is wrapped prose — `store.JournalBullet`'s own comment
	// measures a median of 3 lines and a longest of 19 over the live corpus, so a
	// one-line model would silently truncate most of them.
	Lines []string
	// Date is the ISO date the bullet is dated with, or "". Around 44% of the oracle's
	// corpus carries none, so "" is an ordinary reading rather than a parse failure.
	Date string
	// Population is which of `store`'s six openness populations this bullet is in —
	// exactly one, decided by `store.JournalBullet.OpennessPopulation`. The badge is
	// derived from THIS and never from a substring of the text, which is what keeps an
	// `OPEN:` marker distinguishable from a near-miss that merely looks like one.
	Population string
}

// Text is the bullet rejoined, for the one place a whole bullet is rendered as a
// paragraph.
func (b Bullet) Text() string { return strings.Join(b.Lines, "\n") }

// SearchResults is one answer from `report.Search`, reduced to what the page shows.
type SearchResults struct {
	Query string
	Hits  []Hit
	// TotalHits is hits that cleared the threshold BEFORE truncation, and Omitted is
	// how many of them are not below. Both are rendered: a list that silently stopped
	// at twenty reads as "that is all there is".
	TotalHits int
	Omitted   int
	// BestBelow is the best candidate that did NOT clear the threshold, "" if there
	// was none.
	//
	// 🔴 IT IS WHAT MAKES A ZERO READABLE, and `report.SearchReport.BestBelow`'s own
	// comment is the argument: "the query matched nothing anywhere" and "the best
	// candidate scored 0.50 against a threshold of 0.60" are different facts with
	// different next actions, and a page that prints the same blank for both has
	// diagnosed nothing.
	BestBelow string
	// ScopesSearched is the narrowed set the engine actually walked. It is rendered so
	// an empty result is legible as an AUTHORITY answer when the set is empty.
	ScopesSearched []string
}

// Hit is one matched hunk.
type Hit struct {
	// ScopeID is carried so the hit can LINK to the entry. It is resolved through the
	// same `NamedScopes` traversal the scope cards are, never re-derived.
	ScopeID control.ID
	Scope   string
	Ref     string
	Section string
	Start   int
	Lines   []string
	Score   float64
	// Basis is `report.BasisLine` or `report.BasisEntryName`. Rendered, because a
	// name-only hit is otherwise indistinguishable from a line that matched.
	Basis string
}

// StoreSource reads the real store, narrowed by the caller's authority.
type StoreSource struct{ Root string }

// Visible loads the index the caller may read and projects it to page shapes.
//
// 🔴 `VisibleScopes(control.VerbRead)` IS THE ONLY NARROWING, AND IT IS THE SAME
// SEAM THE POD USES. A second scope check here would be a second implementation of
// visibility, which `internal/control/README.md` exists to refuse.
//
// 🔴 AND THE IDS COME OUT OF THE SAME TRAVERSAL AS THE NARROWING, NOT OUT OF A SECOND
// LOOKUP. `Authorization.NamedScopes` returns id AND display name together for exactly
// this reason — its own comment refuses the shape where a surface takes the names from
// the authority and re-derives the ids from the Model, because the Model holds scopes
// this authority cannot see and the re-derivation is therefore WIDER than the authority
// by construction. `VisibleScopes` is itself derived from `NamedScopes`, so the two
// values below are two projections of one walk.
func (s StoreSource) Visible(auth control.Authorization) ([]Scope, error) {
	named := auth.NamedScopes(control.VerbRead)
	index, err := store.LoadStore(s.Root, "recall", scopeSetOf(named))
	if err != nil {
		return nil, err
	}
	ids := scopeIDsByFoldedName(named)
	var out []Scope
	for _, name := range index.Scopes() {
		entries, err := index.Entries(name)
		if err != nil {
			// An unknown scope cannot happen for a name the index just listed. It is
			// returned rather than skipped so a loader that starts disagreeing with
			// itself is loud instead of quietly rendering a short page.
			return nil, err
		}
		page := Scope{ID: ids[store.NormalizeRef(name)], Name: name}
		for _, e := range entries {
			item, err := s.readEntry(name, e)
			if err != nil {
				return nil, err
			}
			page.Entries = append(page.Entries, item)
		}
		for _, m := range index.MalformedIn(name) {
			page.Malformed = append(page.Malformed, Malformed{Label: m.Label(), Reason: m.Reason})
		}
		out = append(out, page)
	}
	return out, nil
}

// readEntry projects ONE entry file onto [Entry], structure included.
//
// 🔴 IT PARSES WITH `internal/store`'s OWN PARSERS AND WRITES NO MARKDOWN READER. The
// decision and its reasoning are recorded in `internal/ui/README.md`; the short form is
// that `store.ExtractSections` and `store.ParseJournalBullets` are the parsers every
// other reader in this tree uses, they already handle the two things a hand-rolled one
// gets wrong (a `#` inside a code fence is not a heading, an INDENTED `-` is a
// continuation and not a new bullet), and a second parser here would render a structure
// the CLI disagrees with.
//
// ⚠ IT DOES NOT GO THROUGH `internal/report`. That package's job is to render TEXT whose
// bytes are pinned against the Python oracle; this one needs the VALUES, and reaching
// them by parsing `report`'s rendered output back apart would be a second parser with
// extra steps. `report.SurfacedHeadings` and `report.CountedHeadings` ARE imported, so
// the set of headings a browser surfaces cannot drift from the set the CLI prints.
func (s StoreSource) readEntry(scope string, e store.Entry) (Entry, error) {
	item := Entry{
		Ref:      e.Ref(),
		Title:    e.Slug,
		Filename: e.Filename,
		Aliases:  e.RawAliases,
	}
	for _, t := range e.Tasks {
		// `Raw`, not `String()`: the page shows the ref the FILE carries,
		// because the normalisation that produces `System` lowercases and
		// `-`-folds, and a reader comparing the page against the file would
		// otherwise see two spellings of one ref and not know which is real.
		item.Tasks = append(item.Tasks, t.Raw)
	}

	// The file is located from the loader's own scope + filename, never from a path
	// reconstructed out of the ref — `<slug>.<kind>.md` and `<slug>.md` are different
	// files and only the loader knows which one this entry came from. That is
	// `report.ReadEntry`'s rule, restated here because this function is the second
	// reader to depend on it.
	path := filepath.Join(s.Root, scope, e.Filename)
	data, err := os.ReadFile(path)
	if err != nil {
		return Entry{}, store.EntryUnreadable(path, err)
	}
	text := store.DecodeReplace(data)
	sections := store.ExtractSections(text, report.SurfacedHeadings)
	for _, heading := range report.SurfacedHeadings {
		body, present := sections[heading]
		if !present {
			continue
		}
		section := Section{Heading: heading, Body: body}
		if heading == store.NuanceHeading {
			for _, b := range store.ParseJournalBullets(body) {
				section.Bullets = append(section.Bullets, Bullet{
					Lines:      b.Lines,
					Date:       b.Date,
					Population: b.OpennessPopulation(),
				})
			}
		}
		item.Sections = append(item.Sections, section)
	}
	for _, heading := range report.CountedHeadings {
		if _, present := sections[heading]; !present {
			item.MissingSections = append(item.MissingSections, heading)
		}
	}

	for _, b := range store.ParseJournalBullets(sections[store.NuanceHeading]) {
		item.BulletCount++
		switch b.OpennessPopulation() {
		case store.PopulationOpen:
			item.OpenCount++
		case store.PopulationNearMiss:
			item.NearMissCount++
		}
	}
	return item, nil
}

// Search runs the root page's query through `internal/report`'s engine.
//
// 🔴 IT IS THE SAME SCORED, AUTHORITY-NARROWED SEARCH THE CLI AND THE POD RUN, AND A
// SECOND MATCHER WAS NEVER ON THE TABLE. `report.Search` tokenizes, scores, applies the
// fuzzy floor and the short-token exact rule, and reports the best candidate BELOW the
// threshold so a zero is readable. A `strings.Contains` over entry titles here would
// answer differently from `cairn search` for the same query against the same store —
// which is the drift `internal/report` being ONE package exists to prevent.
//
// 🔴 `AllScopes` IS TRUE AND THE NARROWING IS THE INDEX FILTER, WHICH IS `report.Search`'s
// OWN RULING: an all-scopes search names no scope, so there is nothing for a per-scope
// refusal check to refuse, and narrowing the INDEX is what makes a store-wide search
// store-wide over what the caller may see and nothing else.
func (s StoreSource) Search(auth control.Authorization, query string) (SearchResults, error) {
	named := auth.NamedScopes(control.VerbRead)
	rep, err := report.Search(s.Root, report.SearchOptions{
		Query:     query,
		AllScopes: true,
		// The three tuning values are `internal/report`'s own defaults, reached through
		// its constants rather than copied. `internal/api` and `internal/client` spell
		// exactly these three the same way; a fourth surface inventing its own threshold
		// would make the same query score differently in a browser than on a terminal.
		Context:   report.ContextBullet,
		Threshold: report.DefaultThreshold,
		MaxHits:   report.DefaultMaxHits,
	}, scopeSetOf(named))
	if err != nil {
		return SearchResults{}, err
	}

	ids := scopeIDsByFoldedName(named)
	out := SearchResults{
		Query:          query,
		TotalHits:      rep.TotalHits,
		Omitted:        rep.Omitted(),
		ScopesSearched: rep.ScopesSearched,
	}
	if rep.BestBelow != nil {
		out.BestBelow = rep.BestBelow.Ref
	}
	for _, h := range rep.Hunks {
		out.Hits = append(out.Hits, Hit{
			ScopeID: ids[store.NormalizeRef(h.Scope)],
			Scope:   h.Scope,
			Ref:     h.Ref,
			Section: h.Section,
			Start:   h.Start,
			Lines:   h.Lines,
			Score:   h.Score,
			Basis:   h.Basis,
		})
	}
	return out, nil
}

// scopeSetOf is the narrowing, derived from the ONE traversal its caller already made.
//
// ⚠ IT IS `Authorization.VisibleScopes` SPELLED OVER AN ALREADY-WALKED SLICE, NOT A
// SECOND POLICY. Calling `VisibleScopes` beside `NamedScopes` would walk `byScope`
// twice and — more to the point — would be a second place that decides which verb the
// narrowing is about. Both callers here want the scopes reachable with `VerbRead` and
// their ids, from one walk.
func scopeSetOf(named []control.NamedScope) store.ScopeSet {
	names := make([]string, 0, len(named))
	for _, n := range named {
		names = append(names, n.Name)
	}
	return store.VisibleScopeSet(names)
}

// scopeIDsByFoldedName keys the authority's ids on the FOLDED scope name.
//
// 🔴 FOLDED ON BOTH SIDES, BECAUSE THE INDEX'S NAMES AND THE AUTHORITY'S ARE NOT
// GUARANTEED TO BE SPELLED THE SAME. `store.ScopeSet.Allows` folds its probe and
// `tokenfile.foldScope` is `store.NormalizeRef`, so `Alpha_Notes` on disk and
// `alpha-notes` in the model are ONE scope to every reader downstream. A map keyed on
// the raw name would miss on exactly that pair and the card would render unlinked for a
// scope the caller can reach.
func scopeIDsByFoldedName(named []control.NamedScope) map[string]control.ID {
	ids := make(map[string]control.ID, len(named))
	for _, n := range named {
		ids[store.NormalizeRef(n.Name)] = n.ID
	}
	return ids
}

// Server is the UI's HTTP surface.
type Server struct {
	auth        identity.Authenticator
	credentials identity.TokenAuthority
	source      Source
	sharing     Sharing
	sessions    identity.SessionStore
	ttl         time.Duration
	now         func() time.Time
	log         io.Writer

	// oauth and flights are the provider sign-in, and they are one pair rather than two
	// settings for the reason the pair below is: a flight table with no provider to send
	// anybody to holds nothing, and a provider with nowhere to record a PKCE verifier
	// cannot complete a flow. `oauth` is nil on a deployment that has not configured one —
	// see `refuseUnconfiguredOAuth` for why that is SAID rather than refused at
	// construction — and `flights` is never nil, because a table nobody writes to costs an
	// empty map.
	oauth   OAuthAuthority
	flights *flights
	// oauthReady answers whether the provider can be reached at all RIGHT NOW. nil means
	// "no readiness signal was supplied", which is ARMED — see [Server.providerArmed].
	oauthReady func() bool

	// trustedProxies and limiter are the client-identity pair, and they are one pair
	// rather than two settings: the limiter's key IS what `netid.ResolveClient` returns,
	// so a lockout without a trusted-proxy allowlist would bucket every request behind an
	// edge under that edge's own address — one shared key, and the first abuser locks
	// everybody out. `internal/netid`'s own comment calls that the failure the whole
	// client-IP design exists to avoid.
	trustedProxies []netip.Prefix
	limiter        *netid.RateLimiter
}

// Config is what [New] needs. A struct rather than seven positional parameters,
// because six of them are interfaces and a call site that transposed two would still
// compile.
type Config struct {
	// Auth is the chain every request is resolved against. See `AuthBackends`.
	Auth identity.Authenticator
	// Credentials resolves the bearer token a SIGN-IN FORM carries, and it is
	// deliberately a `TokenAuthority` rather than an `Authenticator`.
	//
	// 🔴 A STRING IN, A PRINCIPAL OUT, AND NO `*http.Request` ANYWHERE NEAR IT. The
	// sign-in exchange must resolve the credential the form carried and NOTHING the
	// request also happens to carry — most of all not the session cookie the browser
	// already holds. An `Authenticator` here would take the whole request, so the
	// cookie backend would be in scope and a form submitted with a wrong token but a
	// live cookie would "succeed" as the cookie's principal, minting a fresh session
	// for a credential that was refused. Taking a string makes that unrepresentable
	// rather than avoided by care.
	Credentials identity.TokenAuthority
	// OAuth is the PROVIDER sign-in the GitHub button drives, and it is the one field on
	// this struct that may legitimately be nil.
	//
	// 🔴 NIL MEANS "THIS DEPLOYMENT HAS NO PROVIDER", WHICH IS A CONFIGURATION AND NOT A
	// DEFECT — AND IT IS THE ONE PLACE THIS STRUCT DIVERGES FROM `Sharing`'s RULING, SO THE
	// DIVERGENCE IS ARGUED RATHER THAN ASSUMED. `Sharing` is required precisely because a
	// nil-means-disabled field would put a row in the ledger whose handler was inert. The
	// same objection lands here and is answered rather than ignored: the two OAuth rows
	// answer **501 with a sentence naming the configuration**, and
	// `TestTheGitHubRowsAnswerAnHonestRefusalWhenTheProviderIsNotConfigured` measures both
	// of them, so neither is an unmeasured row. What makes required impossible is the
	// deployment that exists: a surface whose only door is a credential token today would
	// refuse to start, so requiring this would turn a new feature into an outage.
	//
	// ⚠ AND THE BUTTON IS NOT RENDERED WHEN THIS IS NIL. A control that is present and
	// cannot work teaches a user that sign-in is unreliable — the same ruling [Page] makes
	// about the sign-out button it withholds from a caller with no session.
	OAuth OAuthAuthority
	// OAuthReady answers whether the provider's key set has ever been fetched. nil means
	// "no readiness signal", which is ARMED — see [Server.providerArmed] for why the
	// alternative was a startup fatality that took the credential form down with it.
	//
	// ⚠ IT IS A PREDICATE AND NOT A BOOLEAN, SO THE DOOR RE-ARMS WITHOUT A RESTART. The
	// caller's background refresh loop keeps trying; the first success flips this and the
	// next render carries the button.
	OAuthReady func() bool
	// TrustedProxies is the peer allowlist that makes `netid.ClientIPHeader` readable,
	// and it is REQUIRED whenever this surface is reachable by anybody but the local
	// host — `cmd/cairn-ui` refuses to start otherwise, mirroring the pod.
	//
	// 🔴 EMPTY IS NOT "TRUST NOBODY'S HEADER AND CARRY ON" — it is "there is no proxy",
	// which is correct only for a loopback bind. `netid.ResolveClient` then keys on the
	// TCP peer, which for a loopback listener is always the local host, so the limiter
	// would have exactly one bucket. That is fine on a developer's machine and wrong
	// anywhere else, which is why the refusal lives at the bind address rather than here.
	TrustedProxies []netip.Prefix
	// Limiter throttles failed sign-ins. Nil disables it, which is what the unit tests
	// use and what a loopback bring-up gets.
	//
	// ⚠ A NIL LIMITER IS A REAL ABSENCE AND THE HANDLER SAYS SO RATHER THAN PRETENDING:
	// `POST /sign-in` is then unbounded. It is nil-able because the alternative is a
	// mandatory dependency in every test that never signs in, which is how a guard ends
	// up constructed wrongly in fifty places.
	Limiter *netid.RateLimiter
	// Source is the store read, narrowed by the caller's authority.
	Source Source
	// Sharing is the control-plane read and write the share flow needs.
	//
	// 🔴 IT IS REQUIRED, NOT OPTIONAL, EVEN THOUGH A DEPLOYMENT OVER A TOKEN FILE
	// CANNOT WRITE. A nil-means-disabled field would put a route in the ledger whose
	// handler was inert — and the ledger is the thing this surface's guards read to
	// decide what to probe, so an inert row is a row every guard walks and none
	// measures.
	//
	// 🔴 AND THE SENTENCE THAT STOOD HERE WAS FALSE, RETRACTED RATHER THAN QUIETLY
	// REPLACED. It read: "A read-only authority is answered by the WRITE failing with
	// `control.ErrAuthorityReadOnly` … the READS still work, and 'who can see this' is
	// worth serving whether or not this deployment can change it." The SECOND half is
	// wrong for the only read-only authority this tree has, and the first is wrong about
	// which read: `GET /share` (the index) answers 200 and carries the banner —
	// `TestAReadOnlyDeploymentSaysSoOnThePageRatherThanAtTheClick` measures exactly that.
	// What does NOT work is the SCOPE page, "who has access to this", which 404s. `control/tokenfile` confers `admin`
	// on NOBODY, so on such a deployment no scope is administrable, every scope page
	// answers 404, and the write never reaches the sentinel — see `refuseWrite`. ⚠ The
	// same claim was corrected in `README.md` one commit earlier and this copy was left
	// standing: a retraction is a TREE-WIDE SWEEP, not an edit at the site you happened
	// to be reading.
	Sharing Sharing
	// Sessions is the durable session table sign-in writes to and sign-out removes
	// from. It is the SAME store the cookie backend in `Auth` reads; two stores would
	// be a logout that revokes a session nothing authenticates from.
	Sessions identity.SessionStore
	// TTL is a session's absolute lifetime. Zero means `identity.DefaultSessionTTL`;
	// negative is refused.
	TTL time.Duration
	// Now is the clock, injected so expiry is testable without sleeping. It must be
	// the same clock the session store uses, or a session can be live to one and dead
	// to the other.
	Now func() time.Time
	// Log is where operational lines go. Nil means `io.Discard`.
	//
	// 🔴 NOTHING WRITTEN HERE MAY CARRY A SESSION ID, A CSRF TOKEN OR A PRESENTED
	// CREDENTIAL. `TestNoSecretReachesTheLogOrThePage` is what measures that, with a
	// positive control so the zero it reports is not a sink wired to nothing.
	Log io.Writer
}

// ErrNoAuthenticator refuses a server with no way to authenticate anybody, at
// CONSTRUCTION time — the same fail-closed direction `identity.ErrNoBackends`
// takes one level down. A surface that authorises nobody serves nothing and passes
// every health check.
var ErrNoAuthenticator = errors.New("ui: no authenticator was supplied, so no request could ever be authenticated")

// ErrNoSource refuses a server with nothing to render.
var ErrNoSource = errors.New("ui: no source was supplied, so every page would render empty")

// ErrNoSharing refuses a server whose share routes are in the ledger and wired to
// nothing. Separate from `ErrNoSource` because they are separate wirings, and an
// operator reading a startup refusal needs to know which one is missing.
var ErrNoSharing = errors.New("ui: no sharing authority was supplied, so the share routes would be declared and inert")

// ErrNoCredentials refuses a server whose sign-in form could never resolve anything.
// Separate from `ErrNoAuthenticator` because they are separate wirings and an operator
// reading a startup refusal needs to know which one is missing.
var ErrNoCredentials = errors.New("ui: no credential authority was supplied, so no sign-in could ever succeed")

// ErrNoSessions refuses a server with nowhere to put a session. Without it sign-in
// would return a cookie nothing can resolve, which looks like a working sign-in
// followed by an immediate, unexplained sign-out.
var ErrNoSessions = errors.New("ui: no session store was supplied, so a sign-in could mint no session")

// ErrNegativeTTL refuses a session lifetime that is negative. Zero is legal and means
// the default; a negative one would mint sessions that are already expired, so every
// sign-in would appear to succeed and every subsequent request would be refused.
var ErrNegativeTTL = errors.New("ui: the session TTL is negative, so every session would be born expired")

// New builds the server, refusing each missing part with its own sentinel.
func New(cfg Config) (*Server, error) {
	if cfg.Auth == nil {
		return nil, ErrNoAuthenticator
	}
	if cfg.Credentials == nil {
		return nil, ErrNoCredentials
	}
	if cfg.Source == nil {
		return nil, ErrNoSource
	}
	if cfg.Sharing == nil {
		return nil, ErrNoSharing
	}
	if cfg.Sessions == nil {
		return nil, ErrNoSessions
	}
	if cfg.TTL < 0 {
		return nil, ErrNegativeTTL
	}
	ttl := cfg.TTL
	if ttl == 0 {
		ttl = identity.DefaultSessionTTL
	}
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	out := cfg.Log
	if out == nil {
		out = io.Discard
	}
	return &Server{
		auth:        cfg.Auth,
		credentials: cfg.Credentials,
		source:      cfg.Source,
		sharing:     cfg.Sharing,
		sessions:    cfg.Sessions,
		ttl:         ttl,
		now:         now,
		log:         out,

		oauth:      cfg.OAuth,
		oauthReady: cfg.OAuthReady,
		// The SAME clock the server and the session store take, for the reason
		// `Config.Now` records: a flight live to one and dead to the other is a sign-in
		// that fails at the last step for no visible reason.
		flights: newFlights(now),

		trustedProxies: cfg.TrustedProxies,
		limiter:        cfg.Limiter,
	}, nil
}

// ServeHTTP dispatches from the ledger and from nothing else.
//
// 🔴 THE ORDER OF THE GATES IS THE SECURITY MODEL, AND EACH ONE IS DERIVED FROM THE
// REQUEST RATHER THAN OPTED INTO BY A ROW:
//
//  1. the health path, before everything, because a readiness probe broken by a
//     security guard is how the guard gets deleted;
//  2. the SAME-ORIGIN gate, on every state-changing method, before authentication —
//     it costs nothing, it needs no credential, and it is the only thing standing in
//     front of a cross-site POST to the PUBLIC sign-in row, which by definition has no
//     session to carry a token;
//  3. public rows, dispatched with a zero `identity.Identity`;
//  4. the authentication chain, whose refusal is uniform across every remaining path —
//     except for ONE content-negotiated branch on `GET /`, see below;
//  5. the ledger, which answers 404 for a path that is not a row;
//  6. the CSRF TOKEN gate, on every state-changing method that got this far.
//
// 🔴 GATE (5) ANSWERS 404 WHERE IT ANSWERED THE UNIFORM 401, AND THE PREMISE THAT MADE THE
// 401 WORTH ITS COST IS VOID. It read: "a 404 for a path that is not a route would let an
// unauthenticated caller map the URL space." That is true and it does not matter, because
// this repository is PUBLIC and `routes.go` publishes every row — the URL space is mappable
// by reading the file the server is built from. What the 401 bought was therefore nothing an
// attacker did not already have, and what it cost was real: a browser landing on a mistyped
// path was told it was unauthorized, and the "no route" case was indistinguishable from the
// "wrong credential" case in this surface's OWN logs and tests.
//
// 🔴 WHAT IS *KEPT* IS THE PROPERTY THAT WAS ALWAYS THE VALUABLE HALF: A BAD CREDENTIAL IS
// STILL ANSWERED UNIFORMLY. No refusal anywhere on this surface says which half of a
// credential was wrong. That is the oracle over the credential TABLE, and it is a different
// property from the URL space.
//
// ⚠ AND THE NEARBY CLAIM ABOUT THE URL SPACE IS NARROWER THAN THE ONE THIS COMMENT FIRST
// MADE, BECAUSE THE BRANCH BELOW FALSIFIED IT IN THE SAME CHANGE. It said an unauthenticated
// caller "still cannot tell a route from a typo", as a free consequence of gate (4) running
// before gate (5). That is true for every path BUT ONE: a browser asking for `/` is answered
// 303 while a browser asking for `/nonsense` is answered 401, so the root IS distinguishable
// without a credential. Exactly one path, deliberately, and it is the path a sign-in flow has
// to advertise anyway — the same stated narrowing the PUBLIC rows already carry. The credential
// property above is untouched by it: the 303 discloses that `/` exists, never anything about
// who may see it.
//
// 🔴 AND THE ONE CONTENT-NEGOTIATED BRANCH, WHICH IS AN OPERATOR DECISION RATHER THAN A
// CONSEQUENCE. An unauthenticated `GET /` from something that `Accept`s `text/html` is
// answered 303 to the sign-in page; everything else — every other path, every other method,
// and any client that did not ask for HTML — keeps the uniform 401 byte for byte. So a
// browser landing on the root is shown the way in, and a script or a machine client sees
// exactly what it saw before: the machine contract is unmoved, which is the whole reason the
// branch is derived from `Accept` and from the path rather than from a route class.
// `TestTheRootRedirectsABrowserAndRefusesEverythingElse` is what measures both halves.
//
// 🔴 THE CSRF GATE IS AFTER AUTHENTICATION ON PURPOSE, AND THAT IS WHAT MAKES IT
// REACHABLE RATHER THAN SHADOWED. A token check placed ahead of the chain would refuse
// every unauthenticated request before the chain ever ran, so a test asserting "a
// request without a token is refused" would pass against a server whose token check did
// nothing at all. Here the only way to reach it is to be authenticated, which is the
// case `TestTheCSRFGuardIsReachedByAnAUTHENTICATEDRequest` builds.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// (1) Before everything, and the only thing before the origin gate.
	if r.URL.Path == HealthPath {
		writePlain(w, http.StatusOK, healthBody)
		return
	}

	// (2) Same origin, for every method that can change something.
	if stateChanging(r) && !sameOrigin(r) {
		writePlain(w, http.StatusForbidden, crossSiteRefusal)
		return
	}

	rt, known := routes[routeKey{method: r.Method, path: r.URL.Path}]

	// (3) A public row runs with no identity at all. The zero value is passed rather
	// than a synthesized one so a handler that mistakenly read `id.Auth` would see the
	// zero `control.Authorization`, which permits nothing.
	if known && rt.class&classPublic != 0 {
		rt.handle(s, w, r, identity.Identity{})
		return
	}

	// (4)
	id, err := s.auth.Authenticate(r)
	if err != nil || !id.Valid() {
		// 🔴 THE ONE WIDENING, AND IT IS SCOPED TO THE ROOT PATH AND TO A CLIENT THAT
		// ASKED FOR HTML. See [Server.ServeHTTP]'s own comment for the decision; what is
		// here is its narrowness. It is NOT derived from a route class, because a class
		// can only make a route less protected and this branch must not be reachable by
		// declaring one; it is derived from the PATH and the `Accept` header, which is the
		// same "derive it from the request" rule both cross-site gates follow.
		if r.Method == http.MethodGet && r.URL.Path == RootPath && acceptsHTML(r) {
			http.Redirect(w, r, SignInPath, http.StatusSeeOther)
			return
		}
		// 🔴 THE SAME UNIFORM REFUSAL THE POD GIVES, FOR THE SAME REASON, AND IT STILL
		// COVERS AN UNKNOWN PATH — not because gate (5) spells it, but because this gate
		// runs FIRST. A 401 that differed from the bad-credential 401 would let a caller
		// enumerate which paths exist, and THAT half of the property is the one that was
		// always worth its cost: it is about the credential table, not about the URL space.
		//
		// ⚠ `WWW-Authenticate` IS NOT SENT, DELIBERATELY. A browser that receives it
		// raises a native basic-auth dialog, which is a credential prompt this
		// surface does not implement and cannot honour.
		writePlain(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// (5) An authenticated caller asking for a path that is not a row gets the honest
	// answer. See [Server.ServeHTTP] for why this is no longer the uniform 401, and for
	// what is kept instead.
	if !known {
		writePlain(w, http.StatusNotFound, noSuchRoute)
		return
	}

	// (6)
	if stateChanging(r) && !csrfTokenValid(r) {
		writePlain(w, http.StatusForbidden, csrfRefusal)
		return
	}

	rt.handle(s, w, r, id)
}

// stateChanging is the ONE predicate that decides which requests the two cross-site
// gates apply to, so there is no second spelling to disagree with it.
//
// 🔴 IT IS A DENYLIST OF SAFE METHODS RATHER THAN AN ALLOWLIST OF UNSAFE ONES, WHICH IS
// THE OPPOSITE OF `safeHref`'s ruling AND CORRECT FOR THE OPPOSITE REASON. There the
// permitted set is small and closed (two URL schemes) while the dangerous set is open;
// here the SAFE set is the closed one — `GET`, `HEAD` and `OPTIONS` are defined as
// having no side effects — and the unsafe set is open, because a method this server does
// not dispatch today is a method somebody may add tomorrow. Listing the unsafe ones
// would make a new verb default to unguarded.
func stateChanging(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	default:
		return true
	}
}

// noSuchRoute is what gate (5) says. A fixed sentence that does not echo the path: a
// response that repeated what was asked for would reflect caller-chosen text, which is the
// same rule `crossSiteRefusal` follows one file over.
const noSuchRoute = "no such route"

// acceptsHTML answers whether this client asked for HTML.
//
// 🔴 IT LOOKS FOR `text/html` EXPLICITLY AND DOES NOT HONOUR `*/*`, WHICH IS THE WHOLE
// NARROWNESS OF THE ROOT REDIRECT. Every browser sends `text/html` at the front of its
// `Accept`; `curl` sends `*/*`, and a Go client that sets nothing sends no header at all.
// Treating `*/*` as "wants HTML" would move the machine contract — every script that GETs
// `/` with no credential would start receiving a redirect instead of the 401 it was written
// against — which is precisely what this branch is scoped to avoid.
//
// ⚠ IT DOES NOT PARSE `Accept` PROPERLY, AND THAT IS STATED RATHER THAN IMPLIED. A full
// parse would weigh `q=0` — `Accept: text/html;q=0` means "anything BUT html" — so a client
// that spelled that would be redirected here. The cost is a redirect to a page such a client
// will not render; the benefit of not writing a media-type parser is that there is no
// media-type parser. If a caller ever depends on the distinction, this is the function to
// make honest.
func acceptsHTML(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}

func writePlain(w http.ResponseWriter, code int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(body))
}

// handlePage is the entries page's handler. It was once the ONE content handler and is
// no longer — `GET /share` is classed `content` too, and `contentAuthority` in
// `routes_test.go` is where each content route declares WHICH authority it answers from.
// See `routes` for the route this replaced and the sentence that made it wrong.
func (s *Server) handlePage(w http.ResponseWriter, r *http.Request, id identity.Identity) {
	scopes, err := s.source.Visible(id.Auth)
	if err != nil {
		// The reason does not reach the wire. `store.StoreMissingError` and
		// `store.EntryUnreadableError` both carry a filesystem path, and a path is a
		// fact about the deployment rather than about the request.
		writePlain(w, http.StatusInternalServerError, "the store could not be read")
		return
	}

	view := PageView{
		// 🔴 THE CSRF TOKEN RENDERED INTO THIS PAGE IS DERIVED FROM THE COOKIE ON THIS
		// REQUEST, NOT FROM THE IDENTITY. A page reached with an `Authorization` header
		// and no cookie therefore renders an EMPTY token, and its sign-out button will be
		// refused by gate (6) — which is correct rather than a gap: there is no session
		// for that caller to sign out of. Deriving it from the identity would require the
		// identity to carry a session id, and `identity.Identity` deliberately carries no
		// backend discriminator at all.
		Viewer: id.Principal.Display,
		CSRF:   csrfTokenFor(r),
		Scopes: scopes,
	}

	// 🔴 THE QUERY IS A PARAMETER ON THE EXISTING ROOT ROW, NOT A ROUTE OF ITS OWN, AND
	// THAT IS THE HOUSE PATTERN RATHER THAN A SHORTCUT. `routes` is an exact-match map
	// and every served path is a literal key in it; a `/search` row would be a fourth
	// ledger row, a fourth `bareGETAnswer` entry and a fourth thing to classify, to buy a
	// page the root already is. The share flow keys its scope the same way and `routes`
	// records why.
	//
	// ⚠ AND THE ECHO IS SAFE FOR A REASON THAT IS NOT "gomponents ESCAPES IT". The query
	// IS reflected — the input keeps its value so a reader can refine it — which is
	// caller-chosen text on a page, the shape `outcomeFrom` refuses for the share flow's
	// banner. The difference is the POSITION: this text renders inside a form control the
	// reader just typed into, not as a SENTENCE the page presents as its own. A reflected
	// banner can say "your access was suspended, call this number"; a reflected search box
	// says what the reader typed, which is the whole point of the control.
	if query := strings.TrimSpace(r.URL.Query().Get(QueryQuery)); query != "" {
		results, err := s.source.Search(id.Auth, query)
		if err != nil {
			writePlain(w, http.StatusInternalServerError, "the store could not be read")
			return
		}
		view.Query = query
		view.Results = &results
	}
	s.renderPage(w, view)
}

// handleScopePage renders ONE scope's entry list.
//
// 🔴 THE SCOPE IS PICKED OUT OF THE NARROWED ANSWER, WHICH IS WHAT MAKES "not yours"
// AND "does not exist" THE SAME CODE PATH RATHER THAN TWO PATHS HELD EQUAL BY CARE.
// `Source.Visible` has already dropped every scope this credential may not read, so an
// id that is not in the result is refused without this function ever learning whether
// such a scope exists. That is the discipline `scopeRefusal` records one file over: a
// 404-for-unknown beside a 403-for-somebody-else's turns the page into an existence
// oracle over every scope in the deployment, and scope ids are unguessable by
// construction precisely so that refusal is worth something.
//
// ⚠ A REQUEST WITH NO `?id=` IS NOT A REFUSAL. It named nothing, so there is nothing to
// refuse and nothing it could learn; it gets the navigation page, which is also the
// breadcrumb parent for this one.
func (s *Server) handleScopePage(w http.ResponseWriter, r *http.Request, id identity.Identity) {
	scopes, err := s.source.Visible(id.Auth)
	if err != nil {
		writePlain(w, http.StatusInternalServerError, "the store could not be read")
		return
	}
	view := PageView{Viewer: id.Principal.Display, CSRF: csrfTokenFor(r), Scopes: scopes}

	wanted := control.ID(r.URL.Query().Get(QueryID))
	if wanted == "" {
		s.renderNavigate(w, view)
		return
	}
	scope, found := pickScope(scopes, wanted)
	if !found {
		writePlain(w, http.StatusNotFound, browseRefusal)
		return
	}
	view.Scope = &scope
	s.renderScope(w, view)
}

// handleEntryPage renders ONE entry: its sections and its line items.
//
// 🔴 THE `ref` IS USER TEXT IN A URL POSITION AND IT IS MATCHED, NEVER RESOLVED. The
// scope id is minted over a URL-safe alphabet; a ref is a filename stem out of a file
// somebody else wrote, so it can carry a slash, a `..`, a NUL or a percent sequence.
// This function never joins it to a path and never hands it to the loader: it compares
// it against the refs already in the narrowed answer, and anything that matches none of
// them is refused. A `filepath.Join(root, scope, ref+".md")` here would be the traversal
// the rest of this package is built to not need.
//
// 🔴 AND THE REFUSAL IS THE SAME BYTES FOR ALL FOUR WAYS TO MISS — an unknown scope, a
// scope the caller may not read, an unknown ref, and a ref that exists in a DIFFERENT
// scope. Distinguishing any of them is an oracle over the store's shape.
func (s *Server) handleEntryPage(w http.ResponseWriter, r *http.Request, id identity.Identity) {
	scopes, err := s.source.Visible(id.Auth)
	if err != nil {
		writePlain(w, http.StatusInternalServerError, "the store could not be read")
		return
	}
	view := PageView{Viewer: id.Principal.Display, CSRF: csrfTokenFor(r), Scopes: scopes}

	q := r.URL.Query()
	wanted, ref := control.ID(q.Get(QueryScope)), q.Get(QueryRef)
	if wanted == "" || ref == "" {
		// Named nothing, learned nothing. See `handleScopePage`.
		s.renderNavigate(w, view)
		return
	}
	scope, found := pickScope(scopes, wanted)
	if !found {
		writePlain(w, http.StatusNotFound, browseRefusal)
		return
	}
	entry, found := pickEntry(scope, ref)
	if !found {
		writePlain(w, http.StatusNotFound, browseRefusal)
		return
	}
	view.Scope = &scope
	view.Entry = &entry
	s.renderEntry(w, view)
}

// browseRefusal is what a caller gets for a scope or an entry they may not read AND for
// one that does not exist. One answer for both — the rule `scopeRefusal` states in full.
const browseRefusal = "no such scope or entry, or it is not yours to read"

// pickScope and pickEntry are linear scans over a list the caller is about to be shown,
// which is `pick`'s ruling in `sharehandlers.go`: there is no index worth maintaining.
func pickScope(scopes []Scope, id control.ID) (Scope, bool) {
	for _, s := range scopes {
		// An empty id would otherwise match a scope the authority could not name, which
		// is the one case where `?id=` with nothing after it would resolve to a page.
		if s.ID != "" && s.ID == id {
			return s, true
		}
	}
	return Scope{}, false
}

func pickEntry(scope Scope, ref string) (Entry, bool) {
	for _, e := range scope.Entries {
		if e.Ref == ref {
			return e, true
		}
	}
	return Entry{}, false
}

func (s *Server) renderPage(w http.ResponseWriter, view PageView) {
	s.render(w, Page(view))
}

func (s *Server) renderScope(w http.ResponseWriter, view PageView) {
	s.render(w, ScopePage(view))
}

func (s *Server) renderEntry(w http.ResponseWriter, view PageView) {
	s.render(w, EntryPage(view))
}

func (s *Server) renderNavigate(w http.ResponseWriter, view PageView) {
	s.render(w, NavigatePage(view))
}

func (s *Server) render(w http.ResponseWriter, node g.Node) {
	var b strings.Builder
	if err := node.Render(&b); err != nil {
		writePlain(w, http.StatusInternalServerError, "the page could not be rendered")
		return
	}
	writeHTML(w, http.StatusOK, b.String())
}

// writeHTML is the ONE place an HTML response's headers are chosen, so the sign-in page
// and the content page cannot end up under different policies.
//
// 🔴 THIS SURFACE SENDS NO `Content-Security-Policy`, AND THAT IS AN OPERATOR DECISION
// RATHER THAN AN OVERSIGHT. The header was
// `default-src 'none'; style-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'`
// and it was removed on an explicit instruction, challenged once with the blast radius
// below and reaffirmed. `TestTheHTMLResponseSendsNoContentSecurityPolicy` pins the absence,
// so restoring the header is a decision somebody takes there, in the open, rather than a
// line that reappears in a merge.
//
// 🔴 WHAT WAS GIVEN UP, NAMED SO THE NEXT READER DOES NOT HAVE TO RECONSTRUCT IT — all of
// it accepted knowingly on a public, cookie-authenticated surface carrying a
// state-changing share flow:
//
//   - `frame-ancestors 'none'` → this surface is FRAMABLE, so the share flow's grant and
//     revoke POSTs are clickjackable. A clickjacked submit originates INSIDE the page, so
//     its `Origin` really is this origin and the CSRF token in it really is the victim's,
//     and both gates below pass. See the ORIGIN-vs-SITE note after this list: a second
//     draft claimed `SameSite=Lax` prevents this and was WRONG.
//   - `form-action 'self'` → an injected form can be induced to POST offsite.
//   - `base-uri 'none'` → an injected `<base href>` can re-point every relative URL.
//   - `default-src 'none'` → arbitrary script and third-party origins become loadable.
//
// 🔴 `SameSite` IS SITE-SCOPED AND `frame-ancestors` WAS ORIGIN-SCOPED. THEY ARE NOT THE
// SAME BOUNDARY, AND A DRAFT OF THIS COMMENT CONFLATED THEM AND WAS WRONG IN THE UNSAFE
// DIRECTION. That draft said the clickjacking bullet above *"does not reach an AUTHENTICATED
// page"*, because `identity.SessionCookie` sets `SameSite=Lax` and an `<iframe>` load is a
// cross-site subresource request, so the framed document would render the SIGN-IN page. It
// then concluded the accepted exposure was *"SMALLER than recorded"*. Both sentences are
// retracted. This is the record an operator decision was reaffirmed on, so it is corrected
// here rather than quietly reworded to have always said this.
//
// 🔴 WHY IT IS WRONG, AND THE REPOSITORY HAD ALREADY WRITTEN IT DOWN. "Same site" is the
// REGISTRABLE DOMAIN, not the origin. `identity.SessionCookieName`'s own comment quoted in
// full — the draft above stopped one clause short of the words that refute it:
//
//	`Lax` is NOT treated as the CSRF guard — it is a browser-side property this server
//	cannot verify, AND "SAME SITE" STILL INCLUDES A SIBLING SUBDOMAIN. The guard is the
//	token.
//
// So a framer at a sibling host under the same registrable domain is SAME-SITE. Lax attaches
// `__Host-cairn-session` to that framed load, and the `__Host-` prefix does not help: it stops
// a sibling host from SETTING the cookie, it has nothing to do with how the site is computed.
// The framed document therefore renders AUTHENTICATED, with a real [csrfTokenFor] token in it,
// and the induced click submits with `Origin` genuinely equal to this origin. `sameOrigin`
// passes, the token matches, and the originally recorded mechanism follows IN FULL.
// `internal/identity/session.go` already models a hostile sibling host under the registrable
// domain as a real attacker against this exact cookie; that is the same attacker.
//
// 🔴 SO THE BLAST RADIUS IS WHAT IT WAS FIRST RECORDED AS. What `SameSite=Lax` buys is the
// NARROWER case only — a framer at a different registrable domain — and even there it is a
// browser-side property this server cannot verify, unmeasured here, and a single mechanism
// where there used to be two. The live cases, worst first:
//
//   - A SAME-SITE FRAMER: any host sharing this deployment's registrable domain. Full
//     clickjacking of the share flow's state-changing POSTs, both gates satisfied, exactly as
//     the bullet above says. ⚠ Whether such a host EXISTS is a property of the deployment —
//     what else is served under that domain — which this repository cannot see and must not
//     assume away.
//   - A CROSS-SITE FRAMER IN A CLIENT THAT DOES NOT ENFORCE LAX: same mechanism, restored in
//     full, and nothing here would know.
//   - UI REDRESS AGAINST THE UNAUTHENTICATED SIGN-IN PAGE, which needs no cookie at all. It is
//     `classPublic` and answers anybody, so it frames from any origin. A framed sign-in form
//     under an attacker's chrome is a phishing surface, and no gate here addresses it because
//     every request involved is legitimate.
//   - ANY ROUTE A LATER CHANGE MAKES REACHABLE WITHOUT A SESSION inherits that last one, with
//     nothing going red. The standing cost of having no `frame-ancestors`.
//
// ⚠ AND THE EVIDENCE CLASS, BECAUSE IT DID NOT IMPROVE: all of this is DERIVED from the cookie
// constructor and from `SameSite`/`frame-ancestors` scoping rules. No test here and no browser
// run has framed this surface or observed which requests carry the cookie. `session.go` flags
// its neighbouring `Secure`-on-`localhost` claim the same way; these are no better evidenced.
// The lesson the retraction leaves: the wrong draft was ALSO derived, read plausibly, and was
// unsafe — so do not treat the reasoning above as a substitute for measuring it.
//
// ⚠ AND ONE THING IT DID *NOT* BUY, RECORDED BECAUSE THE OPPOSITE IS THE OBVIOUS GUESS:
// the policy was never what blocked the Tailwind build. `style-src 'self'` permits a
// compiled same-origin stylesheet — which is exactly how the stylesheet was served UNDER
// this policy, at [StylesheetPath]. The absent build step was the blocker, and it is now
// present (`tailwind.css` → `app.css`). Deleting the header removed a control; it did not
// unblock the theme.
//
// 🔴 THE CROSS-SITE GATES ARE UNTOUCHED BY ALL OF IT AND MUST STAY THAT WAY. `sameOrigin`
// and `csrfTokenFor` in `session.go` are derived from the request method via
// `stateChanging`, they never read a header this function sets, and they are this surface's
// actual CSRF defence. "The CSP is gone" is not a reason to touch either one. The XSS
// defence is likewise unchanged and was always the real one: gomponents' text and
// attribute-value escaping plus the AST ban on `Raw`/`Rawf`, measured by
// `TestHostileEntryTextIsEscaped` and `TestNoRawNodeConstructorAppearsInTheUIPackage`. What
// is gone is the barrier BEHIND that guard, not the guard.
func writeHTML(w http.ResponseWriter, code int, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// 🔴 `nosniff` IS NOT DECORATION HERE. Every byte of the body below came out of
	// a store entry somebody wrote, and a browser that content-sniffs a response it
	// was told is HTML can be talked into a different type by the leading bytes.
	//
	// ⚠ IT IS NOW THE ONLY HARDENING HEADER ON AN HTML RESPONSE, which is a reason to
	// keep it rather than a reason to reconsider it: it is not part of the policy that
	// was deleted and nothing about that decision reaches it.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(body))
}

// The two `Cache-Control` values the stylesheet is served under, spelled once each so the
// difference between the two rows is a value and not a copied string.
//
// 🔴 `immutable` IS LICENSED BY THE URL, NOT BY THE BYTES BEING STABLE — and that is the whole
// reason there are two values here. It tells a cache never to revalidate for a year, which is
// only ever true of a URL that cannot come to mean different bytes. [StylesheetHashedPath]
// carries a digest of the body it serves, so new bytes are a NEW URL and the old entry is
// simply never asked for again; there is nothing to invalidate because nothing goes stale.
// [StylesheetPath] has no such property and keeps the short value for exactly that reason.
//
// ⚠ AND A `Cache-Control` IS A REQUEST TO EVERY CACHE IN THE PATH, NOT A GUARANTEE — an
// intermediary may serve a longer freshness than this process asks for, and one measurably
// has. That cuts only one way: it makes the short value on the unversioned row weaker than it
// reads, and it makes the hashed row's correctness independent of any cache's cooperation.
const (
	stylesheetCacheVersionless = "public, max-age=300"
	stylesheetCacheImmutable   = "public, max-age=31536000, immutable"
)

// handleStylesheet serves the UNVERSIONED row. It is PUBLIC, it reads nothing, and no page
// links it.
//
// ✅ THE HAZARD THIS COMMENT USED TO DESCRIBE IS CLOSED, AND THE OLD TEXT IS REPLACED RATHER
// THAN LEFT STANDING. It said a short `max-age` was all the URL licensed, that a
// content-hashed path was "the answer that would license `immutable`", and that adding one
// would be a change to the ledger rather than to this line. That is what happened:
// [StylesheetHashedPath] exists, it is the path every page links, `handleHashedStylesheet`
// serves it with `immutable`, and the ledger carries both rows.
//
// 🔴 THIS ROW SURVIVES ON A NARROW ARGUMENT AND KEEPS THE SHORT `max-age` FOR THE SAME REASON
// IT ALWAYS DID: its URL still carries no version. What it buys is that URLs already loose in
// the world — an HTML page rendered before the deploy, a bookmark, a copied link — answer the
// current bytes instead of 404. What it must never become is a path anything LINKS; a page
// that linked it would put every visitor back on an unversioned URL and reinstate the stale
// cache the hashed row exists to close. `TestNoPageLinksTheUnversionedStylesheetPath` is the
// guard, and it reads the rendered HTML rather than this comment.
func (s *Server) handleStylesheet(w http.ResponseWriter, _ *http.Request, _ identity.Identity) {
	writeStylesheet(w, stylesheetCacheVersionless)
}

// handleHashedStylesheet serves the row whose PATH carries the digest of this body, and it is
// the one every page links.
func (s *Server) handleHashedStylesheet(w http.ResponseWriter, _ *http.Request, _ identity.Identity) {
	writeStylesheet(w, stylesheetCacheImmutable)
}

// writeStylesheet is the one response both rows are written by, so the two cannot come to
// serve different bytes or different content types — the only thing either row chooses is its
// `Cache-Control`.
//
// 🔴 IT SERVES A GO VARIABLE AND TOUCHES NO FILESYSTEM, WHICH IS WHY THERE IS NO PATH
// TRAVERSAL TO GET WRONG. The alternative — an `http.FileServer` over a directory — is the
// shape that has to be argued safe: it needs a prefix strip, it follows symlinks, it serves
// whatever somebody drops in the directory, and its route is a PREFIX match, which is a
// second way for a request to reach a handler and one `TestEveryServedPathComesFromTheLedger`
// structurally cannot probe (see `routes`, where the share flow makes the same ruling about
// path parameters). Two exact keys, both computed from data in this binary, have none of
// those questions — a computed key is still an exact key.
//
// ⚠ `nosniff` IS SET HERE TOO, AND NOT BECAUSE THE BYTES ARE UNTRUSTED — they are build
// output checked into this repository, generated from `tailwind.css` and reaching the binary
// through a `//go:embed`, so no input reaches them either. It is set because a browser that
// content-sniffs a stylesheet into
// something else is a browser this response has to be explicit with; the header costs one
// line and its absence is the kind of thing a reader assumes is deliberate.
func writeStylesheet(w http.ResponseWriter, cacheControl string) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", cacheControl)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(stylesheet))
}
