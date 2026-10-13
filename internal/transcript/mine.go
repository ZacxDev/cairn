package transcript

import (
	"slices"
	"strings"
	"time"
)

// 🔴 "MY SESSIONS" (S7 of `claudedocs/plan-cairn-plugins.md`, operator decision O10): the stored
// sessions the viewer OWNS — and only those. It is the way in to an owner-only session (an empty
// `V`, clause g), which no scope tab and no sessions list can reach, because nothing in the store
// names it.
//
// 🔴 OWNERSHIP, NEVER VISIBILITY. The listing is decision 4's OWNER ARM ([Viewer.owns]) and nothing
// else: a principal that reads every scope of somebody else's session may OPEN it — [Store.View]
// admits them — but it is not THEIRS, so it is not listed on their page. "List what the viewer can
// see" reads as the generous version of the same page and is a different page: it would turn a
// reader's "My sessions" into a feed of everyone's transcripts over their readable scopes. The
// mutant `my-sessions-lists-another-owner` is exactly that substitution.
//
// 🔴 THE OWNER ARM IS THE ONE [Visible] USES, SO IT IS `(Kind, ID)` AND AN UN-NARROWED CREDENTIAL. A
// narrowed credential of the owner lists NOTHING: it is an agent credential cut down to some scopes,
// and the owner-only sessions are exactly what the narrowing withheld (it still opens, through the
// scope arm, a session whose every scope it was narrowed to).
//
// ⚠ IT RETURNS METADATA ONLY — no record, no blob, no scope set. A listed session's content is read
// through [Store.View] like any other, so this function adds no second way to a stored record.

// Owned is one stored session on its owner's "My sessions" page.
type Owned struct {
	Root string
	// Runtime and Host are the uploader's own words about the session (`claude`/`opencode`, a host
	// label).
	Runtime, Host string
	// CreatedAt is the first accepted upload; UpdatedAt the last.
	CreatedAt, UpdatedAt time.Time
}

// MineResult is the viewer's owned sessions, newest upload first, and how many stored sessions
// could not be read at all.
type MineResult struct {
	Sessions []Owned
	// Unreadable counts stored sessions whose `meta.json` did not parse. Their owner is unknown, so
	// they cannot be attributed to this viewer or to anyone; the caller logs the count and shows
	// nothing about it — a count of other people's damage is not this viewer's to read.
	Unreadable int
}

// Mine lists every stored session the viewer owns, newest upload (`UpdatedAt`) first, ties broken
// by root id so the order is stable across requests. A nil Store or a nil archive — a deployment
// with no transcript store — owns nothing for anybody.
//
// ⚠ IT READS EVERY STORED SESSION'S `meta.json`, because "newest first" over sessions nobody
// indexes by owner can only be answered by reading them all; the page bounds what it RENDERS, not
// this. No store walk: ownership never reads `V`.
func (s *Store) Mine(viewer Viewer) (MineResult, error) {
	var out MineResult
	if s == nil || s.archive == nil {
		return out, nil
	}
	roots, err := s.archive.Roots()
	if err != nil {
		return out, err
	}
	for _, root := range roots {
		meta, err := s.archive.ReadMeta(root)
		if err != nil {
			out.Unreadable++
			continue
		}
		// A directory no upload ever committed (no meta yet) is no session.
		if meta == nil {
			continue
		}
		if !viewer.owns(meta) {
			continue
		}
		out.Sessions = append(out.Sessions, Owned{Root: meta.Root, Runtime: meta.Runtime, Host: meta.Host,
			CreatedAt: meta.CreatedAt, UpdatedAt: meta.UpdatedAt})
	}
	slices.SortFunc(out.Sessions, func(a, b Owned) int {
		if c := b.UpdatedAt.Compare(a.UpdatedAt); c != 0 {
			return c
		}
		return strings.Compare(a.Root, b.Root)
	})
	return out, nil
}
