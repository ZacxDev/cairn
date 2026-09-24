// Package testcookie holds the ONE predicate that asks whether a rendered `Set-Cookie`
// header carries an attribute exactly.
//
// 🔴 IT IS A PACKAGE BECAUSE THE PREDICATE WAS OPEN-CODED AT FOUR SITES AND WRONG AT
// THREE. Each read `strings.Contains(header, "Path=/")`, which `Path=/sign-in` satisfies —
// so mutating `identity.SessionCookie`'s and `identity.ClearedSessionCookie`'s `Path` to
// `/sign-in` survived the whole 19-package suite. A conforming browser drops a `__Host-`
// cookie whose path is not exactly `/`, so that mutation signs everybody out through both
// doors, credential form included. Two packages need it (`internal/identity` and
// `internal/ui`), which is why it is here rather than a helper in either.
//
// ⚠ IT IS TEST SUPPORT AND NOTHING IN A SERVING PATH IMPORTS IT. `depspolicy.ImportGraph`
// reads `pkg.Imports`, which excludes test imports, so this package does not enter the
// pod's or the CLI's dependency graph.
package testcookie

import "strings"

// HasExactAttr answers whether a rendered `Set-Cookie` carries `attr` as a WHOLE
// attribute rather than as a prefix of one. An attribute ends at the header's end or at
// the `;` before the next one.
func HasExactAttr(header, attr string) bool {
	for _, field := range strings.Split(header, ";") {
		if strings.TrimSpace(field) == attr {
			return true
		}
	}
	return false
}
