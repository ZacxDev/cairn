package report

// 🔴 THIS IS `difflib.SequenceMatcher(None, a, b).ratio()`, AND IT IS A PORT RATHER
// THAN AN EQUIVALENT. The fuzzy rung of `pairStrength` returns the ratio ITSELF as the
// hunk's score, so the number reaches the rendered line (`[0.95 line]`) and the sort
// key. An "equivalent similarity metric" would be a different contract.
//
// 🔴 IT IS NOT AN LCS, AND THAT IS THE WHOLE REASON IT CANNOT BE SWAPPED FOR ONE.
// CPython computes a RECURSIVE LONGEST-MATCHING-BLOCK decomposition: find the single
// longest matching block, then recurse into the segments left and right of it. That is
// greedy and can score strictly lower than a longest common subsequence — `ratio` over
// `("abcd", "badc")` is 0.5 where the LCS length is 3 (0.75) — so the two disagree on
// ordinary inputs, not only on constructed ones.
//
// Only the TOTAL matched size is needed for the ratio, so the block list is summed as it
// is produced rather than sorted and adjacency-merged first: the merge collapses
// contiguous blocks and preserves the sum, and the sentinel block CPython appends has
// size 0. That is an identity, not an approximation.

// ratio is `2.0 * matches / (len(a) + len(b))`, the way `_calculate_ratio` spells it —
// including its answer of 1.0 for two empty sequences.
func ratio(a, b []rune) float64 {
	length := len(a) + len(b)
	if length == 0 {
		return 1.0
	}
	m := newMatcher(a, b)
	return 2.0 * float64(m.matchedRunes()) / float64(length)
}

type matcher struct {
	a, b []rune
	// b2j maps one element of b to its indices in b, ASCENDING — the order the
	// longest-match scan depends on for its `break`.
	b2j map[rune][]int
}

func newMatcher(a, b []rune) *matcher {
	m := &matcher{a: a, b: b, b2j: make(map[rune][]int, len(b))}
	for j, r := range b {
		m.b2j[r] = append(m.b2j[r], j)
	}
	// 🔴 AUTOJUNK, WHICH IS ON BY DEFAULT AND CHANGES THE ANSWER. For `len(b) >= 200`
	// CPython drops every element occurring more than `len(b)/100 + 1` times from the
	// index, so those positions stop SEEDING a match. They can still be crossed by the
	// extension loops below, which consult `bjunk` — empty here, because `isjunk` is
	// None — and not this set. Reachable: a token is bounded only by the line it came
	// from, and a 200-character token is one pasted hash away.
	if len(b) >= 200 {
		ntest := len(b)/100 + 1
		for r, idxs := range m.b2j {
			if len(idxs) > ntest {
				delete(m.b2j, r)
			}
		}
	}
	return m
}

// matchedRunes is `sum(size for _, _, size in get_matching_blocks())`, produced by
// CPython's own queue-driven decomposition so the greedy choices are made in the same
// order.
func (m *matcher) matchedRunes() int {
	type span struct{ alo, ahi, blo, bhi int }
	queue := []span{{0, len(m.a), 0, len(m.b)}}
	matches := 0
	for len(queue) > 0 {
		// `queue.pop()` — the LAST element. The traversal order does not change the
		// total, but it is kept because this function is a transcription.
		s := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		i, j, k := m.findLongestMatch(s.alo, s.ahi, s.blo, s.bhi)
		if k == 0 {
			continue
		}
		matches += k
		if s.alo < i && s.blo < j {
			queue = append(queue, span{s.alo, i, s.blo, j})
		}
		if i+k < s.ahi && j+k < s.bhi {
			queue = append(queue, span{i + k, s.ahi, j + k, s.bhi})
		}
	}
	return matches
}

// findLongestMatch is CPython's, tie-break included: among the longest matches it
// returns the one starting EARLIEST in a, and of those the earliest in b. That
// tie-break is load-bearing for the decomposition that follows it, so the
// `k > bestsize` comparison is strict and stays strict.
func (m *matcher) findLongestMatch(alo, ahi, blo, bhi int) (besti, bestj, bestsize int) {
	besti, bestj, bestsize = alo, blo, 0
	j2len := map[int]int{}
	for i := alo; i < ahi; i++ {
		newj2len := map[int]int{}
		for _, j := range m.b2j[m.a[i]] {
			if j < blo {
				continue
			}
			if j >= bhi {
				break
			}
			k := j2len[j-1] + 1
			newj2len[j] = k
			if k > bestsize {
				besti, bestj, bestsize = i-k+1, j-k+1, k
			}
		}
		j2len = newj2len
	}
	// The two extension loops. CPython runs FOUR — two that refuse to cross junk and
	// two that only cross it — but `isjunk` is None here, so `bjunk` is empty, the
	// `not isbjunk(...)` guard is always true and the `isbjunk(...)` pair can never
	// advance. Written as the two that can run, with the reason stated, rather than as
	// four of which half are dead.
	for besti > alo && bestj > blo && m.a[besti-1] == m.b[bestj-1] {
		besti, bestj, bestsize = besti-1, bestj-1, bestsize+1
	}
	for besti+bestsize < ahi && bestj+bestsize < bhi && m.a[besti+bestsize] == m.b[bestj+bestsize] {
		bestsize++
	}
	return besti, bestj, bestsize
}
