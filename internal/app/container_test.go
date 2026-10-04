package app

import (
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestPostgresPoolMetrics(t *testing.T) {
	t.Parallel()
	pool, err := pgxpool.New(t.Context(), "postgres://user@127.0.0.1:1/test?sslmode=disable&pool_max_conns=3")
	require.NoError(t, err)
	container := &Container{pool: pool}
	t.Cleanup(container.Close)
	observer := container.createMetrics(Config{Metrics: MetricsConfig{Enabled: true}, Log: LogConfig{ServiceName: "test"}})
	recorder := httptest.NewRecorder()
	observer.Handler().ServeHTTP(recorder, httptest.NewRequest("GET", "/metrics", nil))
	require.Contains(t, recorder.Body.String(), `postgres_pool_max_connections{service="test"} 3`)
	require.Contains(t, recorder.Body.String(), `postgres_pool_acquired_connections{service="test"} 0`)
}

func TestLoadExplicitEnvironmentFile(t *testing.T) {
	defaultTestConfig(t)
	path := filepath.Join(t.TempDir(), ".env")
	require.NoError(t, os.WriteFile(path, []byte("HTTP_ADDR=127.0.0.1:9090\nLOG_LEVEL=debug\n"), 0600))
	t.Setenv("ENV_FILE", path)
	require.NoError(t, os.Unsetenv("HTTP_ADDR"))
	require.NoError(t, os.Unsetenv("LOG_LEVEL"))
	cfg, err := LoadConfig()
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:9090", cfg.HTTP.Address)
	t.Setenv("HTTP_ADDR", "127.0.0.1:8081")
	cfg, err = LoadConfig()
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:8081", cfg.HTTP.Address, "environment must take precedence over .env")
	t.Setenv("ENV_FILE", filepath.Join(t.TempDir(), "missing.env"))
	_, err = LoadConfig()
	require.ErrorContains(t, err, "load env file")
}

func TestContainerInitialization(t *testing.T) {
	defaultTestConfig(t)
	container, err := NewContainer()
	require.NoError(t, err)
	require.NotNil(t, container.server)
	require.Nil(t, container.pool)
	container.Close()
	t.Setenv("STORAGE", "unknown")
	_, err = NewContainer()
	require.Error(t, err)
	t.Setenv("STORAGE", "postgres")
	t.Setenv("DATABASE_URL", "://bad URL")
	_, err = NewContainer()
	require.ErrorContains(t, err, "initialize PostgreSQL")
	t.Setenv("LOG_LEVEL", "invalid")
	_, err = NewContainer()
	require.ErrorContains(t, err, "parse environment")
	_, _, _, _, err = container.createRepositories(Config{Storage: StorageConfig{Kind: "unknown"}})
	require.ErrorContains(t, err, "unsupported storage")
	stopped := false
	container.cleanup(func() { stopped = true })
	require.True(t, stopped)
}

func TestContainerRunReturnsListenError(t *testing.T) {
	defaultTestConfig(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	t.Setenv("HTTP_ADDR", listener.Addr().String())
	container, err := NewContainer()
	require.NoError(t, err)
	require.ErrorContains(t, container.Run(), "listen on")
}

func defaultTestConfig(t *testing.T) Config {
	t.Helper()
	for _, key := range []string{
		"AUTH_TOKEN_TTL", "AUTH_REFRESH_TTL", "AUTH_PASSWORD_COST", "AUTH_MIN_PASSWORD_LENGTH", "AUTH_JWT_ISSUER", "AUTH_REQUEST_RATE", "AUTH_REQUEST_BURST",
		"METRICS_ENABLED",
		"ENV_FILE", "SERVICE_NAME", "LOG_LEVEL", "HTTP_ADDR", "SHUTDOWN_TIMEOUT", "HTTP_READ_HEADER_TIMEOUT", "HTTP_READ_TIMEOUT", "HTTP_IDLE_TIMEOUT", "HTTP_MAX_BODY_BYTES",
		"DATABASE_URL", "POSTGRES_PASSWORD", "STARTUP_TIMEOUT", "STORAGE", "GRAPHQL_COMPLEXITY_LIMIT", "MAX_PAGE_SIZE", "SUBSCRIPTION_BUFFER", "WS_PING_INTERVAL",
		"MIGRATIONS_PATH", "MIGRATION_TIMEOUT", "MIGRATION_STATEMENT_TIMEOUT", "MIGRATION_LOCK_TIMEOUT",
	} {
		t.Setenv(key, "")
	}
	t.Setenv("AUTH_JWT_SECRET", "test-only-jwt-secret-at-least-32-bytes")
	cfg, err := LoadConfig()
	require.NoError(t, err)
	require.NoError(t, cfg.ValidateServer())
	return cfg
}
