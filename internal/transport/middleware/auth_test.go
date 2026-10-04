package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"ozon/internal/domain"
	"ozon/internal/service"
	"ozon/internal/transport/middleware"
	"ozon/mocks"
)

func TestIdentityMiddlewareIgnoresUserHeader(t *testing.T) {
	t.Parallel()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(middleware.UserID(r.Context()))) })
	verifier := mocks.NewTokenVerifier(t)
	verifier.On("Authenticate", mock.Anything, "valid").Return(service.Identity{UserID: "author", ExpiresAt: time.Now().Add(time.Hour)}, nil).Once()
	handler := middleware.Authentication(next, verifier)
	req := httptest.NewRequest(http.MethodGet, "/graphql", nil)
	req.Header.Set("X-User-ID", "forged")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	require.Empty(t, recorder.Body.String())
	req.Header.Set("Authorization", "Bearer valid")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	require.Equal(t, "author", recorder.Body.String())
}

func TestIdentityMiddlewareRejectsBadTokens(t *testing.T) {
	t.Parallel()
	verifier := mocks.NewTokenVerifier(t)
	verifier.On("Authenticate", mock.Anything, "invalid").Return(service.Identity{}, domain.ErrUnauthenticated).Once()
	for _, test := range []struct {
		header   string
		verifier middleware.TokenVerifier
	}{
		{"Basic invalid", verifier}, {"Bearer", verifier}, {"Bearer invalid", verifier}, {"Bearer value", nil},
	} {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/graphql", nil)
		req.Header.Set("Authorization", test.header)
		called := false
		handler := middleware.Authentication(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }), test.verifier)
		handler.ServeHTTP(recorder, req)
		require.Equal(t, 401, recorder.Code)
		require.False(t, called)
	}
	ctx := middleware.WithIdentity(context.Background(), service.Identity{UserID: "author", ExpiresAt: time.Now().Add(-time.Second)})
	require.Empty(t, middleware.UserID(ctx), "expired WebSocket identity must not authorize mutations")
}
