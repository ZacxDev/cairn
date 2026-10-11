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
//	unconfigured      no memo listener URL or token                exit 3
//	unreachable       the listener could not be reached in time    exit 3
//	malformed         the listener answered something unreadable   exit 5
//
// The exit codes are the existing ledger's (decision 8); a token refines its code and never
// contradicts it.
func Statuses() []string {
	return []string{"new", "none", "no-scope", "scope-unreadable", "unconfigured", "unreachable", "malformed"}
}
