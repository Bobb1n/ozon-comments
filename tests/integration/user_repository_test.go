package integration_test

import (
	"context"
	"strings"
	"testing"
	"time"

	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"ozon/internal/domain"
	"ozon/internal/service"
)

func runUserContract(t *testing.T, users service.UserRepository, sessions service.RefreshRepository) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("test-password"), bcrypt.MinCost)
	require.NoError(t, err)
	auth, err := service.NewAuthService(users, sessions, service.AuthOptions{Secret: strings.Repeat("s", 32), Issuer: "ozon-test", AccessTTL: time.Hour, RefreshTTL: 24 * time.Hour, PasswordCost: bcrypt.MinCost, MinPasswordLength: 12})
	require.NoError(t, err)
	user := domain.User{ID: "test-author", Login: "test-author", PasswordHash: string(hash)}
	require.NoError(t, users.CreateUser(t.Context(), user))
	require.ErrorIs(t, users.CreateUser(t.Context(), domain.User{ID: "replacement", Login: user.Login, PasswordHash: "replacement"}), domain.ErrConflict)
	got, err := users.FindUserByLogin(t.Context(), user.Login)
	require.NoError(t, err)
	require.Equal(t, user, got)
	token, err := auth.Login(t.Context(), user.Login, "test-password")
	require.NoError(t, err)
	identity, err := auth.Authenticate(t.Context(), token.Token)
	require.NoError(t, err)
	require.Equal(t, user.ID, identity.UserID)
	_, err = users.FindUserByLogin(t.Context(), "unknown")
	require.ErrorIs(t, err, domain.ErrNotFound)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = users.FindUserByLogin(ctx, user.Login)
	require.Error(t, err)
	require.Error(t, users.CreateUser(ctx, user))
	runSessionContract(t, auth, users, sessions)
}
