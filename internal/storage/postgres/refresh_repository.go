package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"ozon/internal/domain"
)

type RefreshRepository struct{ pool *pgxpool.Pool }

func NewRefreshRepository(pool *pgxpool.Pool) *RefreshRepository {
	return &RefreshRepository{pool: pool}
}

func (r *RefreshRepository) CreateRefresh(ctx context.Context, token domain.RefreshToken) error {
	if err := r.cleanupExpired(ctx, time.Now()); err != nil {
		return err
	}
	_, err := r.pool.Exec(ctx, `INSERT INTO refresh_tokens (token_hash,user_id,session_id,expires_at) VALUES ($1,$2,$3,$4)`, token.Hash[:], token.UserID, token.SessionID, token.ExpiresAt)
	return err
}

func (r *RefreshRepository) RotateRefresh(ctx context.Context, hash, nextHash [32]byte, now time.Time) (domain.RefreshToken, error) {
	if err := r.cleanupExpired(ctx, now); err != nil {
		return domain.RefreshToken{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.RefreshToken{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	id, err := lockRefreshSession(ctx, tx, hash)
	if err != nil {
		return domain.RefreshToken{}, err
	}
	var token domain.RefreshToken
	err = tx.QueryRow(ctx, `SELECT user_id,expires_at,used,revoked FROM refresh_tokens WHERE token_hash=$1 FOR UPDATE`, hash[:]).Scan(&token.UserID, &token.ExpiresAt, &token.Used, &token.Revoked)
	if err != nil {
		return domain.RefreshToken{}, refreshError(err)
	}
	if !now.Before(token.ExpiresAt) {
		return domain.RefreshToken{}, domain.ErrUnauthenticated
	}
	if token.Used || token.Revoked {
		if err := revokeRefreshSession(ctx, tx, id); err != nil {
			return domain.RefreshToken{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.RefreshToken{}, err
		}
		return domain.RefreshToken{}, domain.ErrUnauthenticated
	}
	if _, err := tx.Exec(ctx, `UPDATE refresh_tokens SET used=TRUE WHERE token_hash=$1`, hash[:]); err != nil {
		return domain.RefreshToken{}, err
	}
	token.Hash, token.SessionID = nextHash, id
	if _, err := tx.Exec(ctx, `INSERT INTO refresh_tokens (token_hash,user_id,session_id,expires_at) VALUES ($1,$2,$3,$4)`, nextHash[:], token.UserID, id, token.ExpiresAt); err != nil {
		return domain.RefreshToken{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.RefreshToken{}, err
	}
	return token, nil
}

func (r *RefreshRepository) RevokeRefresh(ctx context.Context, hash [32]byte, now time.Time) error {
	if err := r.cleanupExpired(ctx, now); err != nil {
		return err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	id, err := lockRefreshSession(ctx, tx, hash)
	if err != nil {
		return err
	}
	var expiresAt time.Time
	if err := tx.QueryRow(ctx, `SELECT expires_at FROM refresh_tokens WHERE token_hash=$1`, hash[:]).Scan(&expiresAt); err != nil {
		return refreshError(err)
	}
	if !now.Before(expiresAt) {
		return domain.ErrUnauthenticated
	}
	if err := revokeRefreshSession(ctx, tx, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *RefreshRepository) cleanupExpired(ctx context.Context, now time.Time) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM refresh_tokens WHERE token_hash IN (SELECT token_hash FROM refresh_tokens WHERE expires_at <= $1 ORDER BY expires_at LIMIT 64)`, now)
	return err
}

func lockRefreshSession(ctx context.Context, tx pgx.Tx, hash [32]byte) (string, error) {
	var id string
	if err := tx.QueryRow(ctx, `SELECT session_id FROM refresh_tokens WHERE token_hash=$1`, hash[:]).Scan(&id); err != nil {
		return "", refreshError(err)
	}
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, id)
	return id, err
}

func revokeRefreshSession(ctx context.Context, tx pgx.Tx, id string) error {
	_, err := tx.Exec(ctx, `UPDATE refresh_tokens SET revoked=TRUE WHERE session_id=$1`, id)
	return err
}

func refreshError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrUnauthenticated
	}
	return err
}
