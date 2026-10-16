package identity

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/ZacxDev/cairn/internal/control"
)

// claimName is the alphabet `CAIRN_OIDC_GROUPS_CLAIM` must be spelled in: a top-level claim
// name, not a path. A nested claim (`realm_access.roles`) is not supported, and a `.` is
// refused rather than read as a literal key, because an operator who wrote one meant a path.
var claimName = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// ErrGroupsClaimName refuses a groups-claim setting outside `claimName`.
var ErrGroupsClaimName = errors.New("identity: the groups claim must name ONE top-level claim — letters, digits, `_` and `-`; a nested path is not supported")

// claimedGroups reads the named claim out of a token `Verify` has ALREADY accepted.
//
// 🔴 IT RE-DECODES THE PAYLOAD `Verify` CHECKED, AND THAT IS SAFE ONLY BECAUSE IT IS CALLED
// AFTER `Verify` SUCCEEDED ON THE SAME STRING. `Verify` authenticated `header.payload` as
// sent; splitting the same string the same way yields those exact bytes, so no claim read
// here is attacker-controlled. Calling this before verification would be a decision on
// unauthenticated bytes, which `Verify`'s step-7-before-step-8 rule exists to forbid. It is
// a separate read rather than a field on `Claims` because the claim's NAME is configuration.
//
// 🔴 THE WHOLE TOKEN IS REFUSED ON ANY MALFORMATION, NEVER READ PARTIALLY: an absent claim,
// `null`, a non-array, a non-string member, an invalid name, or too many groups. An absent
// claim is a refusal rather than "no groups", because the two are different operator
// problems — a misnamed claim setting would otherwise authenticate every caller and show
// them nothing, the "came up healthy and answers nothing" shape. An EMPTY array is the one
// way to say "no groups", and it authenticates (`control.FederationNoGroups`).
func claimedGroups(token, name string) ([]string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("%w: %d parts", ErrTokenMalformed, len(parts))
	}
	payload, err := decodeSegment(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: payload segment: %v", ErrTokenMalformed, err)
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(payload, &all); err != nil {
		return nil, fmt.Errorf("%w: payload is not a JSON object (%v)", ErrTokenClaims, err)
	}
	raw, present := all[name]
	if !present {
		return nil, fmt.Errorf("%w: the groups claim %q is absent — an empty list is how an issuer says \"no groups\"", control.ErrGroupsClaim, name)
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, fmt.Errorf("%w: the groups claim %q is not an array", control.ErrGroupsClaim, name)
	}
	var groups []string
	if err := json.Unmarshal(trimmed, &groups); err != nil {
		return nil, fmt.Errorf("%w: the groups claim %q is not an array of strings", control.ErrGroupsClaim, name)
	}
	if groups == nil {
		groups = []string{}
	}
	// Validated here AND in `control.ResolveFederated` — the same predicate, so the two
	// cannot disagree; this call is what makes the refusal name the claim. The NORMALISED
	// list is returned, so the audit line records exactly the set authority was computed
	// from rather than the bytes the issuer happened to send.
	normalized, err := control.NormalizeClaimedGroups(groups)
	if err != nil {
		return nil, fmt.Errorf("groups claim %q: %w", name, err)
	}
	return normalized, nil
}
