package integration_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ozon/internal/domain"
	"ozon/internal/service"
)

func runSessionContract(t *testing.T, auth *service.AuthService, users service.UserRepository, sessions service.RefreshRepository) {
	t.Helper()
	user, err := auth.Register(t.Context(), "registered", "test-password")
	require.NoError(t, err)
	_, err = auth.Register(t.Context(), user.Login, "test-password")
	require.ErrorIs(t, err, domain.ErrConflict)
	first, err := auth.Login(t.Context(), user.Login, "test-password")
	require.NoError(t, err)
	other, err := auth.Login(t.Context(), user.Login, "test-password")
	require.NoError(t, err)
	next, err := auth.Refresh(t.Context(), first.RefreshToken)
	require.NoError(t, err)
	require.NotEqual(t, first.RefreshToken, next.RefreshToken)
	require.WithinDuration(t, first.RefreshExpiresAt, next.RefreshExpiresAt, time.Microsecond)
	_, err = auth.Refresh(t.Context(), first.RefreshToken)
	require.ErrorIs(t, err, domain.ErrUnauthenticated)
	_, err = auth.Refresh(t.Context(), next.RefreshToken)
	require.ErrorIs(t, err, domain.ErrUnauthenticated, "replay must revoke the whole session")
	_, err = auth.Refresh(t.Context(), other.RefreshToken)
	require.NoError(t, err, "a different login must remain valid")
	logout, err := auth.Login(t.Context(), user.Login, "test-password")
	require.NoError(t, err)
	require.NoError(t, auth.Logout(t.Context(), logout.RefreshToken))
	require.NoError(t, auth.Logout(t.Context(), logout.RefreshToken), "logout is idempotent until expiry")
	_, err = auth.Refresh(t.Context(), logout.RefreshToken)
	require.ErrorIs(t, err, domain.ErrUnauthenticated)
	_, err = auth.Authenticate(t.Context(), logout.Token)
	require.NoError(t, err, "logout does not revoke already issued access JWTs")

	t.Run("concurrent refresh", func(t *testing.T) {
		pair, err := auth.Login(t.Context(), user.Login, "test-password")
		require.NoError(t, err)
		var successful atomic.Int64
		var workers sync.WaitGroup
		start := make(chan struct{})
		for range 8 {
			workers.Go(func() {
				<-start
				_, err := auth.Refresh(t.Context(), pair.RefreshToken)
				if err == nil {
					successful.Add(1)
				} else if !errors.Is(err, domain.ErrUnauthenticated) {
					t.Errorf("refresh error: %v", err)
				}
			})
		}
		close(start)
		workers.Wait()
		require.EqualValues(t, 1, successful.Load())
	})
	t.Run("concurrent registration", func(t *testing.T) {
		var successful atomic.Int64
		var workers sync.WaitGroup
		for range 8 {
			workers.Go(func() {
				_, err := auth.Register(t.Context(), "same-login", "test-password")
				if err == nil {
					successful.Add(1)
				} else if !errors.Is(err, domain.ErrConflict) {
					t.Errorf("register error: %v", err)
				}
			})
		}
		workers.Wait()
		require.EqualValues(t, 1, successful.Load())
	})
	now := time.Now()
	expiredHash, nextHash := sha256.Sum256([]byte("expired")), sha256.Sum256([]byte("next"))
	require.NoError(t, sessions.CreateRefresh(t.Context(), domain.RefreshToken{Hash: expiredHash, UserID: user.ID, SessionID: "expired-session", ExpiresAt: now.Add(-time.Minute)}))
	_, err = sessions.RotateRefresh(t.Context(), expiredHash, nextHash, now)
	require.ErrorIs(t, err, domain.ErrUnauthenticated)
	require.ErrorIs(t, sessions.RevokeRefresh(t.Context(), expiredHash, now), domain.ErrUnauthenticated)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.Error(t, sessions.CreateRefresh(ctx, domain.RefreshToken{UserID: user.ID}))
	_, err = sessions.RotateRefresh(ctx, expiredHash, nextHash, now)
	require.Error(t, err)
	require.Error(t, sessions.RevokeRefresh(ctx, expiredHash, now))
	_, err = users.FindUserByLogin(t.Context(), user.Login)
	require.NoError(t, err)
}
