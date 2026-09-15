package doctor

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ZacxDev/cairn/internal/store"
)

func pyOSError(err error) string { return store.PyOSError(err) }

// Inputs is what `Collect` needs. 🔴 IT TAKES FACTS, NOT SOURCES: the caller does the network
// and the config load, so every branch below is reachable from a test with no server, no
// `$HOME` and no clock.
type Inputs struct {
	ResolvedRoot string
	// StampLines is nil when there is no readable stamp, and StampReason says why.
	StampLines  []string
	StampReason string
	CacheRoot   string
	// MirrorRoot is "" when no mirror is configured. 🔴 UNSET IS THE DEFAULT and it is NOT
	// the same as absent: a deployment that never had a local store has nothing to name, and
	// saying "does not exist" about a path the operator never configured would be a claim
	// about their disk that this code cannot support.
	MirrorRoot     string
	Pod            PodFacts
	Token          string
	HasToken       bool
	TokenReason    string
	IdentityRemedy string
}

// Collect is every check, in the order a reader should read them.
func Collect(in Inputs) []Check {
	var checks []Check

	// 1. WHICH DIRECTORY does this host's reader resolve, and can it date itself?
	if in.StampLines != nil {
		checks = append(checks, newCheck("reader-resolution", OK, fmt.Sprintf(
			"the reader resolves %s, which carries a sync stamp", in.ResolvedRoot)))
	} else {
		checks = append(checks, newCheck("reader-resolution", Problem, fmt.Sprintf(
			"the reader resolves %s, which cannot date itself — %s. A read against it "+
				"refuses (exit 4, the READER's EXIT_UNSTAMPED_READ_STORE). Fix: `cairn sync`.",
			in.ResolvedRoot, in.StampReason)))
	}

	// 2. THE STAMP'S OWN FIELDS, relayed unparsed.
	if in.StampLines == nil {
		checks = append(checks, newCheck("cache-stamp", Problem, fmt.Sprintf(
			"no readable stamp in %s — %s", in.ResolvedRoot, in.StampReason)))
	} else {
		rendered := strings.Join(in.StampLines, "; ")
		if rendered == "" {
			rendered = "(the stamp is empty)"
		}
		checks = append(checks, newCheck("cache-stamp", OK, rendered))
	}

	// 3. THE FROZEN MIRROR. Absent is fine — a fresh host never had one. Present and fully
	//    read-only is fine. Present with a WRITABLE entry is not. Unconfigured is
	//    NOT-OBSERVABLE, which is a different answer from either.
	var mirror *reading
	if in.MirrorRoot != "" {
		r := describe(in.MirrorRoot, readWritable)
		mirror = &r
	}
	switch {
	case mirror == nil:
		checks = append(checks, newCheck("frozen-mirror", NotObservable,
			"no mirror is configured, so there is nothing to check. Set CAIRN_MIRROR_ROOT "+
				"if you migrated from a local store and want its entry files confirmed "+
				"read-only."))
	case !mirror.ok():
		if mirror.absent {
			checks = append(checks, newCheck("frozen-mirror", OK, fmt.Sprintf(
				"%s does not exist — nothing pre-cutover on this host", in.MirrorRoot)))
		} else {
			checks = append(checks, newCheck("frozen-mirror", Unmeasured, mirror.reason))
		}
	case len(mirror.loose) > 0:
		shown := strings.Join(firstN(mirror.loose, 5), ", ")
		if len(mirror.loose) > 5 {
			shown += "…"
		}
		checks = append(checks, newCheck("frozen-mirror", Problem, fmt.Sprintf(
			"%d entry file(s) under %s are still WRITABLE (%s). The mirror is frozen so "+
				"nothing writes to a store the pod does not read; a write to one of these "+
				"lives on this host only. Re-run `cairn-cutover.py --freeze --apply`.",
			len(mirror.loose), in.MirrorRoot, shown)))
	default:
		checks = append(checks, newCheck("frozen-mirror", OK, fmt.Sprintf(
			"every entry file under %s is read-only", in.MirrorRoot)))
	}

	// 4. THE POD. Unreachable, unauthorised and refused are three answers.
	switch {
	case in.Pod.Reached:
		header := in.Pod.SnapshotHeader
		if header == "" {
			header = "no stamp header"
		}
		checks = append(checks, newCheck("pod", OK, "answered a snapshot request — "+header))
	case in.Pod.HTTPStatus == 401 || in.Pod.HTTPStatus == 403:
		checks = append(checks, newCheck("pod", Problem, fmt.Sprintf(
			"the store ANSWERED and refused this credential (HTTP %d) — %s. This is NOT an "+
				"outage: the host is up. A 403 can also be the edge refusing the "+
				"User-Agent rather than the token being wrong.",
			in.Pod.HTTPStatus, in.Pod.Reason)))
	case in.Pod.HTTPStatus != 0:
		checks = append(checks, newCheck("pod", Problem, fmt.Sprintf(
			"the store ANSWERED HTTP %d — %s", in.Pod.HTTPStatus, in.Pod.Reason)))
	default:
		// 🔴 THE PREFIX IS NEUTRAL ON PURPOSE. It used to read "no answer from the store",
		// which CONTRADICTS one of the reasons that arrives here: a snapshot the pod served
		// and the client could not unpack IS an answer. A heading that argues with the
		// reason beneath it is a comment the code falsifies.
		checks = append(checks, newCheck("pod", Unmeasured, fmt.Sprintf(
			"the store's state could not be established — %s. Nothing below that needs "+
				"the pod could be measured.", in.Pod.Reason)))
	}

	// 5. CACHE vs POD. Only readable once the pod answered; otherwise UNMEASURED with the
	//    reason, never a zero.
	cache := describe(in.CacheRoot, readEntryFiles)
	switch {
	case !in.Pod.Reached:
		cached := "unreadable"
		if cache.ok() {
			cached = fmt.Sprintf("%d", cache.count)
		}
		checks = append(checks, newCheck("cache-vs-pod", Unmeasured, fmt.Sprintf(
			"the pod was not reached (%s), so the cache's %s entry file(s) could not be "+
				"compared against anything", in.Pod.Reason, cached)))
	case !cache.ok():
		checks = append(checks, newCheck("cache-vs-pod", Unmeasured, cache.reason))
	case in.Pod.VisibleEntries == nil:
		checks = append(checks, newCheck("cache-vs-pod", Unmeasured,
			"the store answered without an `X-Store-Entries` header, so its own count of "+
				"what it sent is unknown"))
	case cache.count == *in.Pod.VisibleEntries:
		checks = append(checks, newCheck("cache-vs-pod", OK, fmt.Sprintf(
			"%d entry file(s) here, %d in the store's answer to this token — they agree",
			cache.count, *in.Pod.VisibleEntries)))
	default:
		checks = append(checks, newCheck("cache-vs-pod", Problem, fmt.Sprintf(
			"%d entry file(s) here but %d in the store's answer to this token. "+
				"Fix: `cairn sync`.", cache.count, *in.Pod.VisibleEntries)))
	}

	// 6. WHAT THIS TOKEN CANNOT SEE. Two independent readings, both reported.
	checks = append(checks, visibilityCheck(in))

	// 7. THE CREDENTIAL. A fingerprint is measurable here; the IDENTITY behind it is not,
	//    from any client, and says so rather than going quiet.
	if !in.HasToken {
		reason := in.TokenReason
		if reason == "" {
			reason = "no token is configured, so no request can be authenticated"
		}
		checks = append(checks, newCheck("token", Problem, reason))
	} else {
		checks = append(checks, newCheck("token", NotObservable, fmt.Sprintf(
			"fingerprint %s (sha256[:%d], the handle the pod's audit log carries). The "+
				"IDENTITY and the declared scope allowlist behind it live in the pod's "+
				"token file and the API exposes no route that returns them — see the scope "+
				"check above for what this credential can actually reach. To read the "+
				"declared row: %s",
			TokenFingerprint(in.Token), TokenFingerprintChars, in.IdentityRemedy)))
	}

	return checks
}

// visibilityCheck is the scopes and entries on this disk that the store's answer did not carry.
//
// 🔴 `MirrorRoot` IS OPTIONAL AND UNSET IS THE DEFAULT. When the mirror became configurable on
// the Python side this function's signature was NOT widened with it, so it was handed a `None`
// and `doctor` died with `AttributeError: 'NoneType' object has no attribute 'iterdir'` — zero
// stdout, exit 1 — for every deployment that had never set `CAIRN_MIRROR_ROOT` and had a synced
// cache, which is the ordinary state of a new one. The `frozen-mirror` check above had already
// been widened; this one had not, and nothing joined the two.
//
// 🔴 AN UNCONFIGURED MIRROR IS NOT AN UNREADABLE ROOT. It contributes no scopes and it is not a
// hole in this check's coverage, so it must NOT land in `unread` — doing so would downgrade a
// complete answer to UNMEASURED on every default deployment, which is the same
// false-alarm-forever failure in the opposite direction.
func visibilityCheck(in Inputs) Check {
	if !in.Pod.Reached {
		return newCheck("token-scopes", Unmeasured, fmt.Sprintf(
			"the pod was not reached (%s), so nothing is known about which scopes this "+
				"credential can reach", in.Pod.Reason))
	}

	// 🔴 PROVENANCE PER SCOPE, NOT ONE MERGED SET. A scope in the SYNCED CACHE that the pod
	// no longer sends is a credential or a deletion; a scope in the FROZEN MIRROR only is a
	// pre-cutover leftover that may never have reached the pod at all. The first version
	// merged them and offered two remedies that were both about the live store, so an
	// operator could not tell which they were looking at.
	where := map[string][]string{}
	var order []string
	var unread []string
	roots := [][2]string{{"cache", in.CacheRoot}}
	if in.MirrorRoot != "" {
		roots = append(roots, [2]string{"mirror", in.MirrorRoot})
	}
	for _, pair := range roots {
		label, root := pair[0], pair[1]
		r := describe(root, readScopes)
		if !r.ok() {
			// An ABSENT root contributes nothing and is not a failure; an UNREADABLE one is
			// a hole in this check's own coverage and is named in the detail, so a
			// clean-looking answer cannot come from a walk that could not see half its
			// input.
			if !r.absent {
				unread = append(unread, r.reason)
			}
			continue
		}
		for _, name := range r.scopes {
			if _, seen := where[name]; !seen {
				order = append(order, name)
			}
			where[name] = append(where[name], label)
		}
	}

	visible := map[string]struct{}{}
	for _, s := range in.Pod.VisibleScopes {
		visible[s] = struct{}{}
	}
	var missing []string
	for _, name := range order {
		if _, seen := visible[name]; !seen {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)

	hiddenEntries := ""
	if in.Pod.StoreWideEntries != nil && in.Pod.VisibleEntries != nil {
		if gap := *in.Pod.StoreWideEntries - *in.Pod.VisibleEntries; gap > 0 {
			hiddenEntries = fmt.Sprintf(
				" The store reports %d entry file(s) store-wide but sent this token %d, "+
					"so %d live in scopes this credential cannot reach.",
				*in.Pod.StoreWideEntries, *in.Pod.VisibleEntries, gap)
		}
	}

	unreadNote := ""
	if len(unread) > 0 {
		unreadNote = " Some local roots were unreadable: " + strings.Join(unread, "; ")
	}

	if len(missing) == 0 && hiddenEntries == "" {
		// 🔴 AN UNREADABLE LOCAL ROOT MAKES THIS UNMEASURED, NOT OK-WITH-A-NOTE. "every
		// scope on this disk is among them" is a claim about a set this walk could not
		// finish building, and an OK carrying a caveat in its tail is exactly how a partial
		// answer gets read as a clean one.
		if len(unread) > 0 {
			return newCheck("token-scopes", Unmeasured, fmt.Sprintf(
				"the %d scope(s) the store sent are all reachable, but this host's own "+
					"scope set could not be fully read, so nothing is established about "+
					"scopes present locally.%s", len(visible), unreadNote))
		}
		return newCheck("token-scopes", OK, fmt.Sprintf(
			"this credential reaches all %d scope(s) the store sent, and every scope on "+
				"this disk is among them.", len(visible)))
	}
	if len(missing) == 0 {
		return newCheck("token-scopes", Problem, strings.TrimSpace(hiddenEntries)+unreadNote)
	}
	named := make([]string, 0, len(missing))
	var mirrorOnly []string
	for _, s := range missing {
		named = append(named, fmt.Sprintf("%s [%s]", s, strings.Join(where[s], "+")))
		if len(where[s]) == 1 && where[s][0] == "mirror" {
			mirrorOnly = append(mirrorOnly, s)
		}
	}
	leftoverNote := ""
	if len(mirrorOnly) > 0 {
		leftoverNote = fmt.Sprintf(
			" 🔴 %d of them exist ONLY in the frozen pre-cutover mirror (%s) — those may "+
				"never have reached the pod, so the question is whether they were ever "+
				"seeded, NOT whether access was lost.",
			len(mirrorOnly), strings.Join(mirrorOnly, ", "))
	}
	return newCheck("token-scopes", Problem, fmt.Sprintf(
		"%d scope(s) exist on this disk and are NOT in the store's answer to this token, "+
			"each tagged with WHERE it was found: %s. For a scope the store should hold, "+
			"the API cannot tell you which of two things you are seeing — a refused scope "+
			"is byte-identical to one the store has never held, deliberately, so that an "+
			"error cannot enumerate the store. Both readings are actionable: seed the "+
			"scope, or add it to this token's allowlist.%s%s%s",
		len(missing), strings.Join(named, ", "), leftoverNote, hiddenEntries, unreadNote))
}

func firstN(items []string, n int) []string {
	if len(items) <= n {
		return items
	}
	return items[:n]
}
