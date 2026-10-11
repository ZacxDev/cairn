//go:build !race

package racegate

// Enabled reports whether this binary was built with `-race`, which sets the `race` build tag.
const Enabled = false
