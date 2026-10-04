package integration_test

import (
	"os"
	"testing"

	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/stretchr/testify/require"

	"ozon/internal/app"
)

func TestPostgresContainerInitialization(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires TEST_DATABASE_URL")
	}
	t.Setenv("ENV_FILE", "")
	t.Setenv("STORAGE", "postgres")
	t.Setenv("AUTH_JWT_SECRET", "test-only-jwt-secret-at-least-32-bytes")
	t.Setenv("DATABASE_URL", dsn)
	t.Setenv("METRICS_ENABLED", "true")
	container, err := app.NewContainer()
	require.NoError(t, err)
	container.Close()
}
