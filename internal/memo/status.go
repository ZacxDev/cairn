package memo

// Statuses is the CLOSED set of `memo-status=<token>` values `memo-check` writes as its FIRST
// stderr line on every run (decision 7), in the order `memo-check --statuses` will print them.
// The hook branches on this token, never on reason text, so the set is a contract with a
// second repository: the delivery hook carries its own copy, and from S4 its test reads this
// table out of the BUILT binary and compares — the `-verbs` pattern.
//
//	new               a block is on stdout                         exit 0
//	none              nothing new                                  exit 0
//	no-scope          no configured instance holds this scope      exit 0
//	scope-unreadable  the cache holds it, the listener refuses     exit 3
//	unconfigured      no memo listener URL or token; NO request    exit 3
//	refused           the listener refused the credential (401)    exit 3
//	unreachable       the listener could not be reached in time    exit 3
//	malformed         the listener answered something unreadable   exit 5
//
// The exit codes are the existing ledger's (decision 8); a token refines its code and never
// contradicts it.
//
// 🔴 `refused` IS SPLIT FROM `unconfigured` BECAUSE THE HOOK MUST TELL THEM APART (operator
// decision, the batched defect of the hook's round 0). Both are exit 3, but only `refused` SENT a
// credential the listener counted: its failed-credential lockout is per HOST (5 in 60 s → 15 min),
// so a refusal is what the hook backs the whole host off for. `unconfigured` sent nothing and costs
// the listener nothing. One token for both made the hook back off a host that merely lacked keys,
// or — read the other way — would make it stop backing off a real refusal.
//
// ⚠ ADDING A TOKEN IS A DEPLOY-ORDER CHANGE, NOT ONLY A CONTRACT CHANGE: a hook that does not know
// a token reads it as `malformed`, which neither stops the session nor backs the host off. The
// client reaches hosts only through the tooling repository's pin of this flake, so the hook that
// knows the token must land on that repository's main BEFORE any pin bump past the commit adding it.
func Statuses() []string {
	return []string{"new", "none", "no-scope", "scope-unreadable", "unconfigured", "refused", "unreachable", "malformed"}
}
