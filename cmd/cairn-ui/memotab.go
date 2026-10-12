package main

import (
	"context"
	"fmt"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/memo"
	"github.com/ZacxDev/cairn/internal/pgstore"
	"github.com/ZacxDev/cairn/internal/ui"
)

// browserMemos adapts `pgstore.MemoStore` to `ui.Memos` — the browser's memos tab (S5 of
// `claudedocs/plan-cairn-scope-mail.md`). `internal/ui` cannot import `internal/pgstore` (it would put
// the PostgreSQL driver into the nested `uiaudit` module's graph), so the one translation lives here,
// and it is a translation only: every predicate is the store's.
type browserMemos struct {
	store *pgstore.MemoStore
	model func() control.Model
}

var _ ui.Memos = browserMemos{}

func (b browserMemos) Model() control.Model { return b.model() }

// All is the store's own whole-scope listing — tombstones included, since the tab shows a retracted
// memo as its tombstone (decision 10) — so nothing in this process pages.
func (b browserMemos) All(ctx context.Context, m control.Model, auth control.Authorization, scope string) ([]memo.Stored, error) {
	return b.store.All(ctx, m, auth, scope)
}

func (b browserMemos) Send(ctx context.Context, caller control.Principal, auth control.Authorization,
	scope, subject, body string, ttl time.Duration) (int64, time.Time, error) {
	return b.store.Send(ctx, caller, auth, scope, subject, body, ttl)
}

func (b browserMemos) Retract(ctx context.Context, caller control.Principal, auth control.Authorization, scope string, id int64) error {
	return b.store.Retract(ctx, caller, auth, scope, id)
}

// wireBrowserMemos builds the tab's store, or answers NIL — the interface nil, never a typed nil
// (`inviting`'s trap in `main`) — when this deployment cannot hold memos.
//
// 🔴 IT NEEDS A DATABASE AND A CONTROL JOURNAL, THE CLIENT LISTENER'S TWO CONDITIONS (`clientAPIRefusal`)
// FOR THE SAME REASONS. Without a database there is no memo store. Without a journal this surface's
// authority is the TOKEN FILE's projection, whose shared rows would make every browser sender the same
// sender — the reason O9 reversed O5 for agents applies to a person signed in with one of those rows.
// Unlike the listener this is not a refusal to start: the tab is one view of a page that has others,
// so it says [ui.NoMemoStore] and the two writes answer 501.
//
// ⚠ THE SEND SCAN'S KEY IS FRESH PER PROCESS, and separate from the client listener's (decision 11):
// the scan reads only rule NAMES, so two keys change nothing observable.
func wireBrowserMemos(db *pgstore.DB, journal string, authority *control.Cache) (ui.Memos, error) {
	if db == nil || journal == "" {
		return nil, nil
	}
	scan, err := memo.NewScanner()
	if err != nil {
		return nil, fmt.Errorf("the memo send scan cannot be built: %w", err)
	}
	store, err := pgstore.NewMemoStore(db, scan)
	if err != nil {
		return nil, err
	}
	return browserMemos{store: store, model: authority.Model}, nil
}

// memoMode is the startup line's clause, read off the WIRED value for the reason `stateMode` reads
// `cfg`: a caption from the flags would announce a store the server was never handed.
func memoMode(memos ui.Memos) string {
	if memos == nil {
		return "NO memo store (needs $" + EnvUIDatabase + " and a control journal: the memos tab says so " +
			"and its two writes answer 501)"
	}
	return "memos in postgres"
}
