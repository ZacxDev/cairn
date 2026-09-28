package pgstore

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/invite"
)

// InviteStore is `invite.Store` over Postgres.
type InviteStore struct {
	db *DB
}

// NewInviteStore wraps an open database.
func NewInviteStore(db *DB) *InviteStore { return &InviteStore{db: db} }

var _ invite.Store = (*InviteStore)(nil)

// inviteColumns is the SELECT list, spelled once so the scan order below cannot drift
// from it. Two spellings of a column list is how a `role` ends up in a `project_id`.
const inviteColumns = `digest, project_id, role, inviter, created_at, expires_at, revoked_at, redeemed_at, redeemed_by`

// scanInvite reads one row in `inviteColumns` order.
//
// ⚠ THE THREE NULLABLE COLUMNS GO THROUGH `sql.NullX` AND THEN TO A ZERO VALUE,
// because `invite.Invite` encodes "not yet" as a ZERO TIME rather than a pointer — and
// `invite.StateAt` reads `IsZero()`. Scanning a NULL straight into a `time.Time` is an
// error, and scanning it into a pointer would make every call site nil-check a field
// whose zero value already means the right thing.
func scanInvite(row interface{ Scan(...any) error }) (invite.Invite, error) {
	var (
		inv        invite.Invite
		project    string
		role       string
		inviter    string
		revoked    sql.NullTime
		redeemed   sql.NullTime
		redeemedBy sql.NullString
	)
	if err := row.Scan(
		&inv.Digest, &project, &role, &inviter,
		&inv.CreatedAt, &inv.ExpiresAt, &revoked, &redeemed, &redeemedBy,
	); err != nil {
		return invite.Invite{}, err
	}
	inv.ProjectID = control.ID(project)
	inv.Role = control.Role(role)
	inv.Inviter = control.ID(inviter)
	if revoked.Valid {
		inv.RevokedAt = revoked.Time.UTC()
	}
	if redeemed.Valid {
		inv.RedeemedAt = redeemed.Time.UTC()
	}
	if redeemedBy.Valid {
		inv.RedeemedBy = control.ID(redeemedBy.String)
	}
	inv.CreatedAt = inv.CreatedAt.UTC()
	inv.ExpiresAt = inv.ExpiresAt.UTC()
	return inv, nil
}

// Create stores one new invitation.
//
// 🔴 A PLAIN INSERT, NOT AN UPSERT — THE OPPOSITE OF `SessionStore.Create`, AND THE
// DIFFERENCE IS DELIBERATE. A repeated session digest is a re-issue of the same
// browser's session and replacing it is correct. A repeated INVITE digest is a
// 256-bit collision from `crypto/rand`, which does not happen — so if it ever does,
// the honest response is to fail loudly rather than to silently overwrite somebody
// else's live invitation with a different project and role.
func (s *InviteStore) Create(inv invite.Invite) error {
	if inv.Digest == "" {
		return errors.New("pgstore: an invitation with no digest could never be redeemed")
	}
	if inv.ProjectID == "" || inv.Role == "" || inv.Inviter == "" {
		return errors.New("pgstore: an invitation needs a project, a role and an inviter — an invitation missing any of the three is authority nobody decided to give")
	}
	if !inv.Role.Valid() {
		return fmt.Errorf("pgstore: unknown role %q", inv.Role)
	}
	ctx, cancel := s.db.opCtx()
	defer cancel()
	if _, err := s.db.sql.ExecContext(ctx,
		`INSERT INTO invites (digest, project_id, role, inviter, created_at, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		inv.Digest, string(inv.ProjectID), string(inv.Role), string(inv.Inviter),
		inv.CreatedAt.UTC(), inv.ExpiresAt.UTC(),
	); err != nil {
		return fmt.Errorf("pgstore: creating an invitation: %w", err)
	}
	return nil
}

// ByToken resolves a presented token to an invitation in any state.
func (s *InviteStore) ByToken(presentedToken string) (invite.Invite, bool, error) {
	if presentedToken == "" {
		return invite.Invite{}, false, nil
	}
	ctx, cancel := s.db.opCtx()
	defer cancel()
	row := s.db.sql.QueryRowContext(ctx,
		`SELECT `+inviteColumns+` FROM invites WHERE digest = $1`, invite.Digest(presentedToken))
	inv, err := scanInvite(row)
	if errors.Is(err, sql.ErrNoRows) {
		return invite.Invite{}, false, nil
	}
	if err != nil {
		return invite.Invite{}, false, fmt.Errorf("pgstore: reading an invitation: %w", err)
	}
	return inv, true, nil
}

// Redeem marks an invitation accepted, atomically.
//
// 🔴 ONE CONDITIONAL `UPDATE … RETURNING`, NOT A READ FOLLOWED BY A WRITE. Two clicks
// on one link arriving together both read `open` under any read-then-write shape, and
// both then go on to write a `member-set` to the control journal — two membership
// records from one invitation, which is the one outcome an invite must never produce.
// A conditional update makes the database pick a winner: exactly one statement finds
// `redeemed_at IS NULL` true, and the loser gets zero rows and [invite.ErrNotRedeemable].
//
// 🔴 THE `WHERE` CLAUSE IS A SECOND SPELLING OF `invite.StateAt`, AND IT IS PINNED
// RATHER THAN AVOIDED. It has to be here — the atomicity IS the condition being
// evaluated by the database — so the honest handling is not to pretend otherwise but to
// assert the two agree, including on the boundary instant where they could differ:
// `expires_at > $at` is open, `StateAt` is expired at `!now.Before(ExpiresAt)`, and
// `TestTheSQLRedemptionGuardAgreesWithStateAt` drives both at `ExpiresAt` exactly. ⚠ If
// you edit either, edit both and watch that test fail first.
func (s *InviteStore) Redeem(presentedToken string, by control.ID, at time.Time) (invite.Invite, error) {
	if presentedToken == "" || by == "" {
		return invite.Invite{}, invite.ErrNotRedeemable
	}
	ctx, cancel := s.db.opCtx()
	defer cancel()
	row := s.db.sql.QueryRowContext(ctx,
		`UPDATE invites
		    SET redeemed_at = $2, redeemed_by = $3
		  WHERE digest = $1
		    AND redeemed_at IS NULL
		    AND revoked_at IS NULL
		    AND expires_at > $2
		RETURNING `+inviteColumns,
		invite.Digest(presentedToken), at.UTC(), string(by))
	inv, err := scanInvite(row)
	if errors.Is(err, sql.ErrNoRows) {
		// Zero rows means the digest is unknown OR the invitation is not open. Both
		// answer the same error: see `invite.ErrNotRedeemable`'s own comment on why the
		// STORE must not distinguish them.
		return invite.Invite{}, invite.ErrNotRedeemable
	}
	if err != nil {
		return invite.Invite{}, fmt.Errorf("pgstore: redeeming an invitation: %w", err)
	}
	return inv, nil
}

// Revoke takes an open invitation back, by token.
func (s *InviteStore) Revoke(presentedToken string, at time.Time) error {
	if presentedToken == "" {
		return invite.ErrNotRedeemable
	}
	return s.revoke(invite.Digest(presentedToken), at)
}

// RevokeByDigest is the administrator's path. See the interface's note on why this one
// method takes a digest.
func (s *InviteStore) RevokeByDigest(digest string, at time.Time) error {
	if digest == "" {
		return invite.ErrNotRedeemable
	}
	return s.revoke(digest, at)
}

// revoke is the one statement both revoke paths run, so the two cannot disagree about
// what "open" means.
func (s *InviteStore) revoke(digest string, at time.Time) error {
	ctx, cancel := s.db.opCtx()
	defer cancel()
	res, err := s.db.sql.ExecContext(ctx,
		`UPDATE invites
		    SET revoked_at = $2
		  WHERE digest = $1
		    AND redeemed_at IS NULL
		    AND revoked_at IS NULL
		    AND expires_at > $2`,
		digest, at.UTC())
	if err != nil {
		return fmt.Errorf("pgstore: revoking an invitation: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("pgstore: revoking an invitation: %w", err)
	}
	if n == 0 {
		// 🔴 REPORTED RATHER THAN SWALLOWED. A revoke that silently succeeded against
		// an already-REDEEMED invitation would tell an administrator they had closed a
		// door that is open — and the remedy for a redeemed invitation is different
		// (remove the membership), so conflating them sends them to the wrong page.
		return invite.ErrNotRedeemable
	}
	return nil
}

// ForProject is every invitation naming a project, newest first.
func (s *InviteStore) ForProject(project control.ID) ([]invite.Invite, error) {
	if project == "" {
		return nil, nil
	}
	ctx, cancel := s.db.opCtx()
	defer cancel()
	rows, err := s.db.sql.QueryContext(ctx,
		`SELECT `+inviteColumns+` FROM invites WHERE project_id = $1 ORDER BY created_at DESC, digest ASC`,
		string(project))
	if err != nil {
		return nil, fmt.Errorf("pgstore: listing invitations: %w", err)
	}
	defer rows.Close()

	var out []invite.Invite
	for rows.Next() {
		inv, err := scanInvite(rows)
		if err != nil {
			return nil, fmt.Errorf("pgstore: listing invitations: %w", err)
		}
		out = append(out, inv)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pgstore: listing invitations: %w", err)
	}
	return out, nil
}
