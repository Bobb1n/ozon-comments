package postgres

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInvalidConnectionConfiguration(t *testing.T) {
	t.Parallel()
	pool, err := Open(t.Context(), "://bad URL")
	require.ErrorContains(t, err, "create PostgreSQL pool")
	require.Nil(t, pool)
}

func TestCanceledConnection(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	dsn := "postgres://user@127.0.0.1:1/db?sslmode=disable"
	pool, err := Open(ctx, dsn)
	require.ErrorContains(t, err, "connect to PostgreSQL")
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, pool)
}
