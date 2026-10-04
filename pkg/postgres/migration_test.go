package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestInvalidMigrationConfiguration(t *testing.T) {
	t.Parallel()
	driver, err := NewMigrationDriver(t.Context(), "://bad URL", MigrationOptions{})
	require.ErrorContains(t, err, "migration connection config")
	require.Nil(t, driver)
}

func TestCanceledMigrationConnection(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	driver, err := NewMigrationDriver(ctx, "postgres://user@127.0.0.1:1/db?sslmode=disable", MigrationOptions{ConnectTimeout: time.Second, StatementTimeout: time.Second})
	require.ErrorContains(t, err, "connect migration database")
	require.Nil(t, driver)
}
