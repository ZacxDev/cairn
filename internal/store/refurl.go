package store

import (
	"net/url"
	"sort"
	"strings"
)

// The per-system URL registry: what an entry's `<system>:<id>` ref RESOLVES TO.
//
// 🔴 STRING FORMATTING, NOT ENRICHMENT. Nothing here opens a socket, reads a credential or
// learns a title. The store's value is OFFLINE recall, `internal/depspolicy` bans a new
// dependency in the pod's and the CLI's import graphs, and a resolver that needed the network
// would make `cairn recall` fail differently depending on whether GitHub was up. So a
// template is a format string over the id half and nothing more.
//
// 🔴 TWO TIERS, AND WHICH TIER A SYSTEM IS IN IS DECIDED BY WHETHER ITS HOST IS PUBLIC
// KNOWLEDGE — NOT BY WHO ASKED FOR IT.
//
//	BUILT-IN   one canonical public host, so the base is a constant here and the path shape
//	           is a function of the id half. `github` and `clickup` are the whole set.
//	OPERATOR   every other system. The base comes from `CAIRN_REF_BASE_<SYSTEM>` at runtime
//	           and the URL is `<base>/<escaped id half>`; with nothing supplied the ref
//	           resolves to NO URL and renders exactly as it did before this file existed.
//
// 🔴 THE SECOND TIER EXISTS BECAUSE A SELF-HOSTED TRACKER'S HOSTNAME MAY NOT BE WRITTEN DOWN
// HERE, AND THAT IS A REPOSITORY RULE RATHER THAN A DESIGN PREFERENCE. `AGENTS.md` forbids a
// hostname from any private deployment, `tests/leakscan.py` gates it — and it gates the
// SYSTEM NAMES of that deployment's own tools too, under `denied-identifier`, which is how
// this file came to have two tiers instead of a third hardcoded row. MEASURED, not assumed:
// the first cut named one such system in a `RefURLTemplates` row and `leakscan.py` returned 17
// findings across three files and REFUSED. Do not "complete" the registry by adding a row for
// a self-hosted tool; the operator tier is the supported way to reach one, and it reaches ANY
// of them rather than the ones somebody remembered.
//
// 🔴 AN UNREGISTERED SYSTEM WITH NO BASE RESOLVES TO NO URL, AND THAT IS THE SAFE DIRECTION
// RATHER THAN A LIMITATION. The set of trackers is open; a fallback that invented a host would
// produce a link to the wrong place, which is worse than the text the file carried.
// `internal/ui`'s `taskItem` renders an unresolvable ref as inert text, which is exactly what
// "no URL" means here.

// RefBaseEnvPrefix is the variable-name prefix an operator registers a system's base through:
// `CAIRN_REF_BASE_<SYSTEM>`, the system half upper-cased with `-` folded to `_`.
//
// ⚠ NOT IN `internal/envalias`'S LEDGER, AND THAT IS CORRECT RATHER THAN AN OMISSION. That
// ledger holds RENAMES (`SUBSYSTEM_STORE_*` → `CAIRN_*`, and `CAIRN_SUPABASE_*` →
// `CAIRN_OIDC_*`); these names are new and have no deprecated spelling, so `envalias.Value`
// gives them plain single-name behaviour — which its own `OldName` documents as the
// "never renamed" case.
//
// ⚠ AND THE NAME SPACE IS OPEN BY DESIGN, so no ledger COULD enumerate it: the system half
// comes out of a store file, and any system somebody writes a ref for can be given a base.
const RefBaseEnvPrefix = "CAIRN_REF_BASE_"

// RefURLTemplate is one BUILT-IN system's rule for turning an id half into a URL.
type RefURLTemplate struct {
	// System is the NORMALIZED system half this template answers for.
	System string
	// Base is the origin-plus-prefix. Non-empty for every row: a built-in template with no
	// base would resolve nothing, which is what the operator tier is for.
	Base string
	// Path renders the id half into the path appended to Base. `ok == false` means the id
	// half is not a shape this system can address, which resolves to NO URL rather than to a
	// guess.
	//
	// 🔴 IT RETURNS AN ESCAPED PATH. The id half is BYTE-IDENTICAL to what a store file
	// carried and a store file is attacker-influenced, so every segment goes through
	// `url.PathEscape` here rather than at the render site — a render site escaping it
	// would be the predicate at N places, and this one already knows which parts are
	// segments and which are separators.
	Path func(ident string) (string, bool)
}

// RefURLTemplates is the WHOLE built-in tier, sorted by system.
//
// 🔴 `TestTheRefURLRegistryIsComplete` ASSERTS THE KEY SET EXACTLY, so it fails when the set
// GROWS *or* SHRINKS. A grow-only assertion would let a template be deleted silently, which is
// the direction that turns a working link back into plain text with nothing going red. It
// failing on a GROW is also the gate that sends the next self-hosted system to the operator
// tier instead of into this list.
var RefURLTemplates = []RefURLTemplate{
	{
		System: "clickup",
		Base:   "https://app.clickup.com",
		// `clickup:<task-id>`. ClickUp's own task permalink is `/t/<id>`.
		Path: func(ident string) (string, bool) {
			if strings.Contains(ident, "/") {
				return "", false
			}
			return "/t/" + url.PathEscape(ident), true
		},
	},
	{
		System: "github",
		Base:   "https://github.com",
		// TWO SHAPES, because both are things a person writes and they address different
		// pages: `owner/repo#N` is issue or PR N, `owner/repo` is the repository.
		//
		// ⚠ `/issues/N` IS CORRECT FOR A PULL REQUEST TOO, and that is measured behaviour
		// of GitHub rather than a guess this code makes: `/issues/<n>` redirects to
		// `/pull/<n>` when n is a PR. A ref cannot say which it is — the `<system>:<id>`
		// schema deliberately keeps the id half opaque — so the template picks the form
		// that resolves for both instead of the one that 404s for half of them.
		Path: func(ident string) (string, bool) {
			repo, number, hasNumber := strings.Cut(ident, "#")
			owner, name, hasSlash := strings.Cut(repo, "/")
			if !hasSlash || owner == "" || name == "" || strings.Contains(name, "/") {
				return "", false
			}
			path := "/" + url.PathEscape(owner) + "/" + url.PathEscape(name)
			if !hasNumber {
				return path, true
			}
			if number == "" || !isASCIIDigits(number) {
				return "", false
			}
			return path + "/issues/" + number, true
		},
	},
}

func isASCIIDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// EntryReferences answers "does this entry carry this ref" — the REVERSE LOOKUP's one
// predicate.
//
// 🔴 ONE FUNCTION, BECAUSE THE FILTER RUNS AT THREE CALL SITES AND A PREDICATE OPEN-CODED AT
// N SITES IS WRONG AT N-1. `Recall` narrows a scope's entries, `Search` narrows each searched
// scope's entries, and the oracle spells the same rule in Python; a fourth caller adding
// "which entries reference this" must reach this and not re-derive it.
//
// 🔴 THE SYSTEM HALF IS COMPARED NORMALIZED AND THE ID HALF BYTE-IDENTICALLY, WHICH IS THE
// SCHEMA'S OWN ASYMMETRY RATHER THAN A CHOICE MADE HERE. The system half is normalized
// (`GitHub:` and `github:` are one system) and the id half preserved exactly, because a
// tracker's id may be case-sensitive and folding it would make two different tasks compare
// equal. So a query for `GitHub:example-org/example-repo#428` finds an entry written
// `github:…` and a query for `clickup:ABC` does NOT find `clickup:abc`. Both halves of that
// are load-bearing: pass both sides through `ParseTaskRef` and the asymmetry is applied once.
func EntryReferences(e Entry, want TaskRef) bool {
	for _, have := range e.Tasks {
		if have.System == want.System && have.Ident == want.Ident {
			return true
		}
	}
	return false
}

// RefSystems is every BUILT-IN system, sorted. It is NOT every system that can resolve — the
// operator tier is open — which is why this name says nothing about "supported".
func RefSystems() []string {
	out := make([]string, 0, len(RefURLTemplates))
	for _, t := range RefURLTemplates {
		out = append(out, t.System)
	}
	sort.Strings(out)
	return out
}

// RefBaseEnv is the variable name a system's operator-supplied base is read from.
//
// ⚠ IT ANSWERS FOR A BUILT-IN SYSTEM TOO, AND THE VALUE IS IGNORED THERE. The name is a pure
// function of the system half — a lookup would make the answer depend on the registry, and a
// caller printing "set $X to resolve these" for an unregistered system would then get "" and
// print nothing. `RefURL` is where the built-in base wins; see its own branch.
func RefBaseEnv(system string) string {
	return RefBaseEnvPrefix + strings.ToUpper(strings.ReplaceAll(system, "-", "_"))
}

// RefURL resolves one ref to an absolute URL, or reports that it resolves to none.
//
// `get` reads a configuration value by name — `envalias.OSValue` at a real call site, a map
// lookup in a test. It is injected rather than read from the process environment here because
// this package is a LIBRARY: a function that reached for `os.Getenv` could not be measured at
// two points in one test run, and "measure at ≥2 points" is exactly what the operator tier's
// two states require.
//
// 🔴 THE BUILT-IN TIER WINS OVER THE OPERATOR ONE, AND THAT ORDER IS A DECISION. An operator
// who sets `CAIRN_REF_BASE_GITHUB` does NOT redirect `github:` refs: the built-in row knows
// that system's PATH SHAPE (`owner/repo#N` → `/owner/repo/issues/N`) and the operator tier
// does not, so honouring the override would silently downgrade every GitHub ref to
// `<base>/owner%2Frepo%23N`. A reader who wants that can spell the system half differently in
// the file.
//
// 🔴 IT DOES NOT PROMISE THE RESULT IS SAFE TO PUT IN AN href. An operator-supplied base is a
// string from outside this program, so `javascript:alert(1)#` is a base somebody can set, and
// this function will happily append a path to it. `internal/ui`'s `safeHref` is what refuses
// it, every URL on the browser surface goes through that one function, and
// `TestAJavascriptBaseIsRefusedRatherThanRendered` is the negative control on that claim. A
// second scheme check here would be the same predicate at two sites, wrong at one.
func RefURL(ref TaskRef, get func(string) string) (string, bool) {
	for _, t := range RefURLTemplates {
		if t.System != ref.System {
			continue
		}
		path, ok := t.Path(ref.Ident)
		if !ok {
			return "", false
		}
		return strings.TrimSuffix(t.Base, "/") + path, true
	}
	// The OPERATOR tier. `get` may be nil at a call site that has no configuration to
	// offer — a library caller, or a test measuring the built-in rows alone — and that is
	// the same answer as "the variable is unset" rather than a panic.
	if get == nil {
		return "", false
	}
	base := strings.TrimSpace(get(RefBaseEnv(ref.System)))
	if base == "" {
		return "", false
	}
	// 🔴 THE WHOLE ID HALF IS ONE ESCAPED SEGMENT HERE, AND THE OPERATOR OWNS THE PATH
	// PREFIX. This tier does not know the system's URL shape, so it cannot split the id half
	// into segments — `#` and `/` inside it are DATA and are escaped. An operator whose
	// tracker addresses a task at `/tasks/<id>` therefore sets the base to
	// `https://<host>/tasks`, which is the whole configuration this tier needs and keeps the
	// shape decision with the person who knows it.
	return strings.TrimSuffix(base, "/") + "/" + url.PathEscape(ref.Ident), true
}
