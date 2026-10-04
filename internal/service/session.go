package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/google/uuid"

	"ozon/internal/domain"
)

type RefreshRepository interface {
	CreateRefresh(context.Context, domain.RefreshToken) error
	RotateRefresh(context.Context, [32]byte, [32]byte, time.Time) (domain.RefreshToken, error)
	RevokeRefresh(context.Context, [32]byte, time.Time) error
}

func (s *AuthService) Refresh(ctx context.Context, value string) (AuthToken, error) {
	if err := ctx.Err(); err != nil {
		return AuthToken{}, err
	}
	hash, err := refreshHash(value)
	if err != nil {
		return AuthToken{}, err
	}
	next, nextHash := newRefreshToken()
	stored, err := s.sessions.RotateRefresh(ctx, hash, nextHash, time.Now())
	if err != nil {
		return AuthToken{}, fmt.Errorf("rotate refresh token: %w", err)
	}
	return s.tokenPair(stored.UserID, next, stored.ExpiresAt)
}

func (s *AuthService) Logout(ctx context.Context, value string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	hash, err := refreshHash(value)
	if err != nil {
		return err
	}
	return s.sessions.RevokeRefresh(ctx, hash, time.Now())
}

func (s *AuthService) startSession(ctx context.Context, userID string) (AuthToken, error) {
	value, hash := newRefreshToken()
	expiresAt := time.Now().Add(s.refreshTTL)
	pair, err := s.tokenPair(userID, value, expiresAt)
	if err != nil {
		return AuthToken{}, err
	}
	stored := domain.RefreshToken{Hash: hash, UserID: userID, SessionID: uuid.NewString(), ExpiresAt: expiresAt}
	if err := s.sessions.CreateRefresh(ctx, stored); err != nil {
		return AuthToken{}, fmt.Errorf("create refresh session: %w", err)
	}
	return pair, nil
}

func (s *AuthService) tokenPair(userID, refresh string, expiresAt time.Time) (AuthToken, error) {
	token, err := s.issueToken(userID)
	if err != nil {
		return AuthToken{}, err
	}
	token.RefreshToken, token.RefreshExpiresAt = refresh, expiresAt
	return token, nil
}

func newRefreshToken() (string, [32]byte) {
	var secret [32]byte
	_, _ = rand.Read(secret[:])
	value := base64.RawURLEncoding.EncodeToString(secret[:])
	return value, sha256.Sum256([]byte(value))
}

func refreshHash(value string) ([32]byte, error) {
	if len(value) != 43 {
		return [32]byte{}, domain.ErrUnauthenticated
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) != 32 {
		return [32]byte{}, domain.ErrUnauthenticated
	}
	return sha256.Sum256([]byte(value)), nil
}
