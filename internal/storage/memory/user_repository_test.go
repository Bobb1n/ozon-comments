package memory

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"ozon/internal/domain"
)

func TestUserRepository(t *testing.T) {
	t.Parallel()
	repository := NewUserRepository(NewDatabase())
	user := domain.User{ID: "author", Login: "author", PasswordHash: "hash"}
	require.NoError(t, repository.CreateUser(t.Context(), user))
	require.ErrorIs(t, repository.CreateUser(t.Context(), domain.User{ID: "other", Login: "author", PasswordHash: "replacement"}), domain.ErrConflict)
	got, err := repository.FindUserByLogin(t.Context(), "author")
	require.NoError(t, err)
	require.Equal(t, user, got, "duplicate registration must not overwrite credentials")
	_, err = repository.FindUserByLogin(t.Context(), "unknown")
	require.ErrorIs(t, err, domain.ErrNotFound)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, repository.CreateUser(ctx, user), context.Canceled)
	_, err = repository.FindUserByLogin(ctx, "author")
	require.ErrorIs(t, err, context.Canceled)
}
