package middleware

import (
	"context"
	"net/http"
	"strings"
	"time"

	"ozon/internal/domain"
	"ozon/internal/service"
)

type TokenVerifier interface {
	Authenticate(context.Context, string) (service.Identity, error)
}

type userKey struct{}

func Authentication(next http.Handler, verifier TokenVerifier) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		value := r.Header.Get("Authorization")
		if value == "" {
			next.ServeHTTP(w, r)
			return
		}
		identity, err := AuthenticateBearer(r.Context(), value, verifier)
		if err != nil {
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), identity)))
	})
}

func AuthenticateBearer(ctx context.Context, value string, verifier TokenVerifier) (service.Identity, error) {
	parts := strings.Fields(value)
	if verifier == nil || len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return service.Identity{}, domain.ErrUnauthenticated
	}
	return verifier.Authenticate(ctx, parts[1])
}

func WithIdentity(ctx context.Context, identity service.Identity) context.Context {
	return context.WithValue(ctx, userKey{}, identity)
}

func UserID(ctx context.Context) string {
	identity, _ := ctx.Value(userKey{}).(service.Identity)
	if !time.Now().Before(identity.ExpiresAt) {
		return ""
	}
	return identity.UserID
}
