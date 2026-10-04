package service_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"ozon/internal/domain"
	"ozon/internal/service"
	"ozon/mocks"
)

func TestRegistration(t *testing.T) {
	t.Parallel()
	users := mocks.NewUserRepository(t)
	auth := authService(t, users)
	users.On("CreateUser", mock.Anything, mock.MatchedBy(func(user domain.User) bool {
		return user.ID != "" && user.Login == "author" && user.PasswordHash != "test-password"
	})).Return(nil).Once()
	user, err := auth.Register(t.Context(), " author ", "test-password")
	require.NoError(t, err)
	require.Equal(t, "author", user.Login)
	for _, test := range []struct{ login, password string }{
		{"", "test-password"}, {" ", "test-password"}, {strings.Repeat("x", 129), "test-password"},
		{"author", "short"}, {"author", strings.Repeat("x", 73)}, {"author", strings.Repeat("я", 37)},
		{"author", string([]byte{0xff})}, {string([]byte{0xff}), "test-password"},
	} {
		_, err := auth.Register(t.Context(), test.login, test.password)
		require.ErrorIs(t, err, domain.ErrInvalidInput)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = auth.Register(ctx, "author", "test-password")
	require.ErrorIs(t, err, context.Canceled)
}

func TestRefreshAndLogout(t *testing.T) {
	t.Parallel()
	users, sessions := mocks.NewUserRepository(t), mocks.NewRefreshRepository(t)
	auth, err := service.NewAuthService(users, sessions, testAuthOptions())
	require.NoError(t, err)
	value := strings.Repeat("a", 43)
	hash := sha256.Sum256([]byte(value))
	expires := time.Now().Add(24 * time.Hour)
	sessions.On("RotateRefresh", mock.Anything, hash, mock.Anything, mock.Anything).Return(domain.RefreshToken{UserID: "author", ExpiresAt: expires}, nil).Once()
	next, err := auth.Refresh(t.Context(), value)
	require.NoError(t, err)
	require.NotEqual(t, value, next.RefreshToken)
	require.Len(t, next.RefreshToken, 43)
	require.Equal(t, expires, next.RefreshExpiresAt)
	identity, err := auth.Authenticate(t.Context(), next.Token)
	require.NoError(t, err)
	require.Equal(t, "author", identity.UserID)
	sessions.On("RevokeRefresh", mock.Anything, sha256.Sum256([]byte(next.RefreshToken)), mock.Anything).Return(nil).Once()
	require.NoError(t, auth.Logout(t.Context(), next.RefreshToken))
	for _, value := range []string{"", "bad", strings.Repeat("*", 43)} {
		_, err := auth.Refresh(t.Context(), value)
		require.ErrorIs(t, err, domain.ErrUnauthenticated)
		require.ErrorIs(t, auth.Logout(t.Context(), value), domain.ErrUnauthenticated)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = auth.Refresh(ctx, value)
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, auth.Logout(ctx, value), context.Canceled)
}

func TestSessionRepositoryFailures(t *testing.T) {
	t.Parallel()
	users, sessions := mocks.NewUserRepository(t), mocks.NewRefreshRepository(t)
	auth, err := service.NewAuthService(users, sessions, testAuthOptions())
	require.NoError(t, err)
	want := errors.New("storage failure")
	users.On("FindUserByLogin", mock.Anything, "author").Return(authUser(t), nil).Once()
	sessions.On("CreateRefresh", mock.Anything, mock.Anything).Return(want).Once()
	_, err = auth.Login(t.Context(), "author", "password")
	require.ErrorIs(t, err, want)
	value := strings.Repeat("a", 43)
	sessions.On("RotateRefresh", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(domain.RefreshToken{}, want).Once()
	_, err = auth.Refresh(t.Context(), value)
	require.ErrorIs(t, err, want)
	sessions.On("RevokeRefresh", mock.Anything, mock.Anything, mock.Anything).Return(want).Once()
	require.ErrorIs(t, auth.Logout(t.Context(), value), want)
	for _, change := range []func(*service.AuthOptions){
		func(o *service.AuthOptions) { o.PasswordCost = 0 }, func(o *service.AuthOptions) { o.MinPasswordLength = 0 },
		func(o *service.AuthOptions) { o.RefreshTTL = o.AccessTTL },
	} {
		options := testAuthOptions()
		change(&options)
		_, err := service.NewAuthService(users, sessions, options)
		require.Error(t, err)
	}
}
