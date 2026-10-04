package service_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"ozon/internal/domain"
	"ozon/internal/service"
	"ozon/mocks"
)

func TestLoginAndJWT(t *testing.T) {
	t.Parallel()
	users := mocks.NewUserRepository(t)
	auth := authService(t, users)
	user := authUser(t)
	users.On("FindUserByLogin", mock.Anything, "author").Return(user, nil).Twice()
	first, err := auth.Login(t.Context(), " author ", "password")
	require.NoError(t, err)
	second, err := auth.Login(t.Context(), "author", "password")
	require.NoError(t, err)
	require.NotEqual(t, first.Token, second.Token)
	claims := &jwt.RegisteredClaims{}
	parsed, err := jwt.ParseWithClaims(first.Token, claims, func(*jwt.Token) (any, error) { return []byte(testJWTSecret), nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired(), jwt.WithIssuer(testJWTIssuer))
	require.NoError(t, err)
	require.True(t, parsed.Valid)
	require.Equal(t, "author", claims.Subject)
	require.NotEmpty(t, claims.ID)
	require.NotNil(t, claims.IssuedAt)
	require.Equal(t, time.Hour, claims.ExpiresAt.Sub(claims.IssuedAt.Time))
	identity, err := auth.Authenticate(t.Context(), first.Token)
	require.NoError(t, err)
	require.Equal(t, "author", identity.UserID)
	require.Equal(t, first.ExpiresAt, identity.ExpiresAt)
	restarted := authService(t, users)
	identity, err = restarted.Authenticate(t.Context(), first.Token)
	require.NoError(t, err)
	require.Equal(t, "author", identity.UserID)
}

func TestLoginRejectsInvalidCredentials(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, login, password string
		repositoryError       error
		invalidHash           bool
	}{
		{"empty login", "", "password", nil, false},
		{"empty password", "author", "", nil, false},
		{"long login", strings.Repeat("a", 129), "password", nil, false},
		{"long password", "author", strings.Repeat("a", 73), nil, false},
		{"unknown login", "author", "password", domain.ErrNotFound, false},
		{"wrong password", "author", "wrong", nil, false},
		{"corrupt hash", "author", "password", nil, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			users := mocks.NewUserRepository(t)
			auth := authService(t, users)
			if test.login != "" && len(test.login) <= 128 && test.password != "" && len(test.password) <= 72 {
				user := authUser(t)
				if test.invalidHash {
					user.PasswordHash = "invalid"
				}
				users.On("FindUserByLogin", mock.Anything, "author").Return(user, test.repositoryError).Once()
			}
			_, err := auth.Login(t.Context(), test.login, test.password)
			require.ErrorIs(t, err, domain.ErrUnauthenticated)
		})
	}
}

func TestJWTRejectsInvalidTokens(t *testing.T) {
	t.Parallel()
	users := mocks.NewUserRepository(t)
	auth := authService(t, users)
	for _, test := range []struct {
		name   string
		change func(*jwt.RegisteredClaims)
		method jwt.SigningMethod
		key    any
	}{
		{"expired", func(c *jwt.RegisteredClaims) { c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Minute)) }, jwt.SigningMethodHS256, []byte(testJWTSecret)},
		{"missing expiry", func(c *jwt.RegisteredClaims) { c.ExpiresAt = nil }, jwt.SigningMethodHS256, []byte(testJWTSecret)},
		{"wrong issuer", func(c *jwt.RegisteredClaims) { c.Issuer = "other" }, jwt.SigningMethodHS256, []byte(testJWTSecret)},
		{"missing issuer", func(c *jwt.RegisteredClaims) { c.Issuer = "" }, jwt.SigningMethodHS256, []byte(testJWTSecret)},
		{"missing subject", func(c *jwt.RegisteredClaims) { c.Subject = "" }, jwt.SigningMethodHS256, []byte(testJWTSecret)},
		{"future issued at", func(c *jwt.RegisteredClaims) { c.IssuedAt = jwt.NewNumericDate(time.Now().Add(time.Hour)) }, jwt.SigningMethodHS256, []byte(testJWTSecret)},
		{"future not before", func(c *jwt.RegisteredClaims) { c.NotBefore = jwt.NewNumericDate(time.Now().Add(time.Hour)) }, jwt.SigningMethodHS256, []byte(testJWTSecret)},
		{"wrong secret", func(*jwt.RegisteredClaims) {}, jwt.SigningMethodHS256, []byte(strings.Repeat("x", 32))},
		{"wrong algorithm", func(*jwt.RegisteredClaims) {}, jwt.SigningMethodHS512, []byte(testJWTSecret)},
		{"unsigned", func(*jwt.RegisteredClaims) {}, jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			claims := jwt.RegisteredClaims{Subject: "author", Issuer: testJWTIssuer, ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}
			test.change(&claims)
			token, err := jwt.NewWithClaims(test.method, claims).SignedString(test.key)
			require.NoError(t, err)
			_, err = auth.Authenticate(t.Context(), token)
			require.ErrorIs(t, err, domain.ErrUnauthenticated)
		})
	}
	for _, token := range []string{"", "forged", "broken.jwt.token"} {
		_, err := auth.Authenticate(t.Context(), token)
		require.ErrorIs(t, err, domain.ErrUnauthenticated)
	}
}

func TestAuthRepositoryErrorsAndCancellation(t *testing.T) {
	t.Parallel()
	users := mocks.NewUserRepository(t)
	auth := authService(t, users)
	want := errors.New("database unavailable")
	users.On("FindUserByLogin", mock.Anything, "unavailable").Return(domain.User{}, want).Once()
	_, err := auth.Login(t.Context(), "unavailable", "password")
	require.ErrorIs(t, err, want)
	users.On("CreateUser", mock.Anything, mock.Anything).Return(want).Once()
	_, err = auth.Register(t.Context(), "author", "long-test-password")
	require.ErrorIs(t, err, want)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = auth.Login(ctx, "author", "password")
	require.ErrorIs(t, err, context.Canceled)
	_, err = auth.Authenticate(ctx, "token")
	require.ErrorIs(t, err, context.Canceled)
	ctx, cancel = context.WithCancel(t.Context())
	defer cancel()
	users.On("FindUserByLogin", ctx, "author").Run(func(mock.Arguments) { cancel() }).Return(authUser(t), nil).Once()
	_, err = auth.Login(ctx, "author", "password")
	require.ErrorIs(t, err, context.Canceled)
	options := testAuthOptions()
	options.AccessTTL = 0
	_, err = service.NewAuthService(users, nil, options)
	require.Error(t, err)
	options = testAuthOptions()
	options.Secret = "short"
	_, err = service.NewAuthService(users, nil, options)
	require.Error(t, err)
	options = testAuthOptions()
	options.Issuer = " "
	_, err = service.NewAuthService(users, nil, options)
	require.Error(t, err)
}

func TestConcurrentJWTs(t *testing.T) {
	t.Parallel()
	users := mocks.NewUserRepository(t)
	users.On("FindUserByLogin", mock.Anything, "author").Return(authUser(t), nil)
	auth := authService(t, users)
	var workers sync.WaitGroup
	for range 20 {
		workers.Go(func() {
			token, err := auth.Login(t.Context(), "author", "password")
			if err != nil {
				t.Error(err)
				return
			}
			identity, err := auth.Authenticate(t.Context(), token.Token)
			if err != nil || identity.UserID != "author" {
				t.Errorf("identity: %+v, error: %v", identity, err)
			}
		})
	}
	workers.Wait()
}

const testJWTSecret = "test-only-jwt-secret-at-least-32-bytes"

const testJWTIssuer = "ozon-test"

func authService(t *testing.T, users service.UserRepository) *service.AuthService {
	t.Helper()
	sessions := mocks.NewRefreshRepository(t)
	sessions.On("CreateRefresh", mock.Anything, mock.Anything).Return(nil).Maybe()
	auth, err := service.NewAuthService(users, sessions, testAuthOptions())
	require.NoError(t, err)
	return auth
}

func authUser(t *testing.T) domain.User {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	require.NoError(t, err)
	return domain.User{ID: "author", Login: "author", PasswordHash: string(hash)}
}

func testAuthOptions() service.AuthOptions {
	return service.AuthOptions{Secret: testJWTSecret, Issuer: testJWTIssuer, AccessTTL: time.Hour, RefreshTTL: 24 * time.Hour, PasswordCost: bcrypt.MinCost, MinPasswordLength: 12}
}
