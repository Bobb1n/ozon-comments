package memory

import (
	"context"
	"time"

	"ozon/internal/domain"
)

type RefreshRepository struct{ db *Database }

func NewRefreshRepository(db *Database) *RefreshRepository { return &RefreshRepository{db: db} }

func (r *RefreshRepository) CreateRefresh(ctx context.Context, token domain.RefreshToken) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.db.refreshMu.Lock()
	defer r.db.refreshMu.Unlock()
	r.cleanupExpired(time.Now())
	r.db.refreshTokens[token.Hash] = token
	return nil
}

func (r *RefreshRepository) RotateRefresh(ctx context.Context, hash, nextHash [32]byte, now time.Time) (domain.RefreshToken, error) {
	if err := ctx.Err(); err != nil {
		return domain.RefreshToken{}, err
	}
	r.db.refreshMu.Lock()
	defer r.db.refreshMu.Unlock()
	r.cleanupExpired(now)
	token, exists := r.db.refreshTokens[hash]
	if !exists || !now.Before(token.ExpiresAt) {
		return domain.RefreshToken{}, domain.ErrUnauthenticated
	}
	if token.Used || token.Revoked {
		r.revokeSession(token.SessionID)
		return domain.RefreshToken{}, domain.ErrUnauthenticated
	}
	token.Used = true
	r.db.refreshTokens[hash] = token
	token.Hash, token.Used = nextHash, false
	r.db.refreshTokens[nextHash] = token
	return token, nil
}

func (r *RefreshRepository) RevokeRefresh(ctx context.Context, hash [32]byte, now time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.db.refreshMu.Lock()
	defer r.db.refreshMu.Unlock()
	r.cleanupExpired(now)
	token, exists := r.db.refreshTokens[hash]
	if !exists || !now.Before(token.ExpiresAt) {
		return domain.ErrUnauthenticated
	}
	r.revokeSession(token.SessionID)
	return nil
}

func (r *RefreshRepository) revokeSession(id string) {
	for hash, token := range r.db.refreshTokens {
		if token.SessionID == id {
			token.Revoked = true
			r.db.refreshTokens[hash] = token
		}
	}
}

func (r *RefreshRepository) cleanupExpired(now time.Time) {
	checked := 0
	for hash, token := range r.db.refreshTokens {
		if !now.Before(token.ExpiresAt) {
			delete(r.db.refreshTokens, hash)
		}
		checked++
		if checked == 64 {
			break
		}
	}
}
