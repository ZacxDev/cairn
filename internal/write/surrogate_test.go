package write

import "testing"

func TestUnpairedSurrogateEscape(t *testing.T) {
	// 🔴 THE RULE IS ADJACENCY, AND THE TABLE IS WRITTEN SO EACH HALF OF IT CAN FAIL
	// SEPARATELY. A pattern that matched the whole surrogate block passes every
	// "unpaired is refused" row and fails every "pair is accepted" row — which is
	// exactly how the shipped bug got through a guard that looked tested.
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"a well-formed pair is not unpaired", `{"t":"\ud83d\ude00"}`, ""},
		{"two pairs in a row", `{"t":"\ud83d\ude00\ud83d\ude01"}`, ""},
		{"a lone HIGH surrogate", `{"t":"\ud83d"}`, `\ud83d`},
		{"a lone LOW surrogate", `{"t":"\ude00"}`, `\ude00`},
		{"a low then a high — the wrong order is not a pair", `{"t":"\ude00\ud83d"}`, `\ude00`},
		{"a high with something between it and the low", `{"t":"\ud83dx\ude00"}`, `\ud83d`},
		{"a high followed by a NON-surrogate escape", `{"t":"\ud83d\u0041"}`, `\ud83d`},
		{"ordinary escapes are untouched", `{"t":"\u0041\n\t"}`, ""},
		{"no escapes at all", `{"t":"plain"}`, ""},
		{"a pair after an ordinary escape", `{"t":"\u0041\ud83d\ude00"}`, ""},
		{"upper-case hex is the same escape", `{"t":"\uD83D\uDE00"}`, ""},
		{"a lone upper-case high surrogate", `{"t":"\uD83D"}`, `\uD83D`},
		// 🔴 BACKSLASH PARITY. Two source backslashes are ONE escaped backslash in
		// JSON, so what follows is the literal text `ud83d` and there is no escape
		// here at all — while a scanner that looked for `\u` and restarted one byte
		// in would read one. This row is what caught that: the first version of
		// `unpairedSurrogateEscape` returned `\ud83d` for it.
		{"an escaped backslash before `u` is not an escape", `{"t":"\\ud83d"}`, ""},
		// Four source backslashes are TWO escaped backslashes, same conclusion one
		// pair further along — the parity has to hold for a run, not just for one.
		{"two escaped backslashes before `u`", `{"t":"\\\\ud83d"}`, ""},
		// One source backslash IS an escape introducer, but `d8"}` is not four hex
		// digits, so the escape is malformed and names no surrogate.
		{"a truncated `\\u` escape names no surrogate", `{"t":"\ud8"}`, ""},
		// …and one source backslash with four hex digits IS the lone surrogate.
		{"one backslash with four hex digits is an escape", `{"t":"\ud83d"}`, `\ud83d`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := unpairedSurrogateEscape(tc.raw); got != tc.want {
				t.Fatalf("unpairedSurrogateEscape(%s) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// The strict-decode table moved WITH the function, to
// `pytext.TestDecodeStrictProblem`. A table left behind here would be this package
// asserting another package's contract — the shape that lets the owning package be
// changed while its own suite stays green.
