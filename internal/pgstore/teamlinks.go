package pgstore

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/ZacxDev/cairn/internal/control"
	"github.com/ZacxDev/cairn/internal/invite"
)

// TeamLinkStore is `invite.LinkStore` over Postgres.
type TeamLinkStore struct {
	db *DB
}

// NewTeamLinkStore wraps an open database.
func NewTeamLinkStore(db *DB) *TeamLinkStore { return &TeamLinkStore{db: db} }

var _ invite.LinkStore = (*TeamLinkStore)(nil)

// teamLinkColumns is the SELECT list, spelled once — `inviteColumns`' reason.
const teamLinkColumns = `digest, role, inviter, reusable, created_at, expires_at, revoked_at, redemptions`

func scanTeamLink(row interface{ Scan(...any) error }) (invite.TeamLink, error) {
	var (
		l       invite.TeamLink
		role    string
		inviter string
		revoked sql.NullTime
	)
	if err := row.Scan(&l.Digest, &role, &inviter, &l.Reusable,
		&l.CreatedAt, &l.ExpiresAt, &revoked, &l.Redemptions); err != nil {
		return invite.TeamLink{}, err
	}
	l.Role = invite.LinkRole(role)
	l.Inviter = control.ID(inviter)
	if revoked.Valid {
		l.RevokedAt = revoked.Time.UTC()
	}
	l.CreatedAt = l.CreatedAt.UTC()
	l.ExpiresAt = l.ExpiresAt.UTC()
	return l, nil
}

// CreateLink stores a link and its targets in ONE transaction.
//
// 🔴 ONE TRANSACTION, BECAUSE A LINK ROW WITH NO TARGET ROWS IS A LINK THAT REDEEMS TO
// NOTHING — a redemption would be counted, logged and spent while conferring no authority.
// A plain INSERT for the reason `InviteStore.Create` gives: a repeated digest is a 256-bit
// collision and must fail loudly rather than overwrite somebody's live link.
func (s *TeamLinkStore) CreateLink(l invite.TeamLink) error {
	if l.Digest == "" {
		return errors.New("pgstore: a team link with no digest could never be redeemed")
	}
	if l.Inviter == "" || !l.Role.Valid() {
		return fmt.Errorf("pgstore: a team link needs an inviter and a known role (got role %q)", l.Role)
	}
	if err := invite.ValidateTargets(l.Targets); err != nil {
		return fmt.Errorf("pgstore: %w", err)
	}
	ctx, cancel := s.db.opCtx()
	defer cancel()
	tx, err := s.db.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("pgstore: creating a team link: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO team_links (digest, role, inviter, reusable, created_at, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		l.Digest, string(l.Role), string(l.Inviter), l.Reusable, l.CreatedAt.UTC(), l.ExpiresAt.UTC(),
	); err != nil {
		return fmt.Errorf("pgstore: creating a team link: %w", err)
	}
	for _, t := range l.Targets {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO team_link_targets (digest, kind, target_id) VALUES ($1, $2, $3)`,
			l.Digest, string(t.Kind), string(t.ID),
		); err != nil {
			return fmt.Errorf("pgstore: recording a team link's target: %w", err)
		}
	}
	return tx.Commit()
}

// LinkByToken resolves a presented token to a link in any state.
func (s *TeamLinkStore) LinkByToken(presentedToken string) (invite.TeamLink, bool, error) {
	if presentedToken == "" {
		return invite.TeamLink{}, false, nil
	}
	return s.LinkByDigest(invite.Digest(presentedToken))
}

// LinkByDigest resolves a digest to a link in any state, with its targets.
func (s *TeamLinkStore) LinkByDigest(digest string) (invite.TeamLink, bool, error) {
	if digest == "" {
		return invite.TeamLink{}, false, nil
	}
	ctx, cancel := s.db.opCtx()
	defer cancel()
	l, err := scanTeamLink(s.db.sql.QueryRowContext(ctx,
		`SELECT `+teamLinkColumns+` FROM team_links WHERE digest = $1`, digest))
	if errors.Is(err, sql.ErrNoRows) {
		return invite.TeamLink{}, false, nil
	}
	if err != nil {
		return invite.TeamLink{}, false, fmt.Errorf("pgstore: reading a team link: %w", err)
	}
	targets, err := s.targets(digest)
	if err != nil {
		return invite.TeamLink{}, false, err
	}
	l.Targets = targets
	return l, true, nil
}

func (s *TeamLinkStore) targets(digest string) ([]invite.Target, error) {
	ctx, cancel := s.db.opCtx()
	defer cancel()
	rows, err := s.db.sql.QueryContext(ctx,
		`SELECT kind, target_id FROM team_link_targets WHERE digest = $1 ORDER BY kind, target_id`, digest)
	if err != nil {
		return nil, fmt.Errorf("pgstore: reading a team link's targets: %w", err)
	}
	defer rows.Close()
	var out []invite.Target
	for rows.Next() {
		var kind, id string
		if err := rows.Scan(&kind, &id); err != nil {
			return nil, fmt.Errorf("pgstore: reading a team link's targets: %w", err)
		}
		out = append(out, invite.Target{Kind: invite.TargetKind(kind), ID: control.ID(id)})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pgstore: reading a team link's targets: %w", err)
	}
	return out, nil
}

// RedeemLink records one redemption atomically.
//
// 🔴 ONE CONDITIONAL `UPDATE … RETURNING` PICKS THE WINNER, AND THE AUDIT ROW IS IN THE SAME
// TRANSACTION. `InviteStore.Redeem`'s argument, with one more conjunct: a single-use link is
// open only while `redemptions = 0`, so of two concurrent redemptions exactly one finds the
// row and the other gets zero rows. The `team_links_single_use` CHECK is the database
// refusing the same thing a second time, independently of this statement.
//
// 🔴 THE `WHERE` CLAUSE IS A SECOND SPELLING OF `invite.TeamLink.StateAt` — pinned, not
// avoided, by `TestTheTeamLinkRedemptionGuardAgreesWithStateAt`, at the expiry boundary.
// ⚠ If you edit either, edit both and watch that test fail first.
func (s *TeamLinkStore) RedeemLink(presentedToken string, by control.ID, provisioned bool, at time.Time) (invite.TeamLink, error) {
	if presentedToken == "" || by == "" {
		return invite.TeamLink{}, invite.ErrNotRedeemable
	}
	digest := invite.Digest(presentedToken)
	ctx, cancel := s.db.opCtx()
	defer cancel()
	tx, err := s.db.sql.BeginTx(ctx, nil)
	if err != nil {
		return invite.TeamLink{}, fmt.Errorf("pgstore: redeeming a team link: %w", err)
	}
	defer tx.Rollback()
	l, err := scanTeamLink(tx.QueryRowContext(ctx,
		`UPDATE team_links
		    SET redemptions = redemptions + 1
		  WHERE digest = $1
		    AND revoked_at IS NULL
		    AND expires_at > $2
		    AND (reusable OR redemptions = 0)
		RETURNING `+teamLinkColumns,
		digest, at.UTC()))
	if errors.Is(err, sql.ErrNoRows) {
		// Unknown OR not open: one error, `invite.ErrNotRedeemable`'s ruling.
		return invite.TeamLink{}, invite.ErrNotRedeemable
	}
	if err != nil {
		return invite.TeamLink{}, fmt.Errorf("pgstore: redeeming a team link: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO team_link_redemptions (digest, seq, redeemed_by, redeemed_at, provisioned)
		 VALUES ($1, $2, $3, $4, $5)`,
		digest, l.Redemptions, string(by), at.UTC(), provisioned,
	); err != nil {
		return invite.TeamLink{}, fmt.Errorf("pgstore: logging a team link redemption: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return invite.TeamLink{}, fmt.Errorf("pgstore: redeeming a team link: %w", err)
	}
	targets, err := s.targets(digest)
	if err != nil {
		return invite.TeamLink{}, err
	}
	l.Targets = targets
	return l, nil
}

// ConfirmRedemption marks one redemption confirmed. A row that does not exist is an error:
// confirming nothing would let a caller believe a join was logged that was not.
func (s *TeamLinkStore) ConfirmRedemption(digest string, seq int) error {
	ctx, cancel := s.db.opCtx()
	defer cancel()
	res, err := s.db.sql.ExecContext(ctx,
		`UPDATE team_link_redemptions SET confirmed = TRUE WHERE digest = $1 AND seq = $2`, digest, seq)
	if err != nil {
		return fmt.Errorf("pgstore: confirming a team link redemption: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		return fmt.Errorf("pgstore: confirming a team link redemption: %d row(s) matched (%v)", n, err)
	}
	return nil
}

// RevokeLink takes an open link back.
//
// 🔴 ITS "OPEN" IS THE REDEEM GUARD'S "OPEN", CONJUNCT FOR CONJUNCT, so a spent single-use
// link is refused here — `InviteStore.revoke`'s argument: a revoke reported as success
// against a link somebody already used tells the minter they closed a door that is open.
func (s *TeamLinkStore) RevokeLink(digest string, at time.Time) error {
	if digest == "" {
		return invite.ErrNotRedeemable
	}
	ctx, cancel := s.db.opCtx()
	defer cancel()
	res, err := s.db.sql.ExecContext(ctx,
		`UPDATE team_links
		    SET revoked_at = $2
		  WHERE digest = $1
		    AND revoked_at IS NULL
		    AND expires_at > $2
		    AND (reusable OR redemptions = 0)`,
		digest, at.UTC())
	if err != nil {
		return fmt.Errorf("pgstore: revoking a team link: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("pgstore: revoking a team link: %w", err)
	}
	if n == 0 {
		return invite.ErrNotRedeemable
	}
	return nil
}

// LinksBy is every link this principal minted, newest first, with targets.
func (s *TeamLinkStore) LinksBy(inviter control.ID) ([]invite.TeamLink, error) {
	if inviter == "" {
		return nil, nil
	}
	ctx, cancel := s.db.opCtx()
	defer cancel()
	rows, err := s.db.sql.QueryContext(ctx,
		`SELECT `+teamLinkColumns+` FROM team_links WHERE inviter = $1 ORDER BY created_at DESC, digest ASC`,
		string(inviter))
	if err != nil {
		return nil, fmt.Errorf("pgstore: listing team links: %w", err)
	}
	var out []invite.TeamLink
	for rows.Next() {
		l, err := scanTeamLink(rows)
		if err != nil {
			rows.Close()
			return nil, fmt.Errorf("pgstore: listing team links: %w", err)
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("pgstore: listing team links: %w", err)
	}
	rows.Close()
	// The targets are read AFTER the cursor is closed: the pool is bounded (`Open` sets
	// eight), and holding one connection open on a cursor while asking for another per
	// row is how a listing deadlocks a small pool.
	for i := range out {
		targets, err := s.targets(out[i].Digest)
		if err != nil {
			return nil, err
		}
		out[i].Targets = targets
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// LinkRedemptions is one link's redemption log, oldest first.
func (s *TeamLinkStore) LinkRedemptions(digest string) ([]invite.LinkRedemption, error) {
	if digest == "" {
		return nil, nil
	}
	ctx, cancel := s.db.opCtx()
	defer cancel()
	rows, err := s.db.sql.QueryContext(ctx,
		`SELECT seq, redeemed_by, redeemed_at, provisioned, confirmed FROM team_link_redemptions
		  WHERE digest = $1 ORDER BY seq ASC`, digest)
	if err != nil {
		return nil, fmt.Errorf("pgstore: reading a team link's redemptions: %w", err)
	}
	defer rows.Close()
	var out []invite.LinkRedemption
	for rows.Next() {
		r := invite.LinkRedemption{Digest: digest}
		var by string
		if err := rows.Scan(&r.Seq, &by, &r.At, &r.Provisioned, &r.Confirmed); err != nil {
			return nil, fmt.Errorf("pgstore: reading a team link's redemptions: %w", err)
		}
		r.By = control.ID(by)
		r.At = r.At.UTC()
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pgstore: reading a team link's redemptions: %w", err)
	}
	return out, nil
}
