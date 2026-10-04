package app

import (
	"log/slog"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDefaults(t *testing.T) {
	for _, key := range []string{"HTTP_ADDR", "STORAGE", "DATABASE_URL", "LOG_LEVEL", "METRICS_ENABLED"} {
		t.Setenv(key, "")
	}
	cfg, err := LoadConfig()
	require.NoError(t, err)
	require.Equal(t, ":8080", cfg.HTTP.Address)
	require.Equal(t, "inmemory", cfg.Storage.Kind)
	require.Equal(t, slog.LevelInfo, cfg.Log.Level)
	require.False(t, cfg.Metrics.Enabled)
}

func TestMetricsConfiguration(t *testing.T) {
	t.Setenv("METRICS_ENABLED", "true")
	cfg, err := LoadConfig()
	require.NoError(t, err)
	require.True(t, cfg.Metrics.Enabled)
	t.Setenv("METRICS_ENABLED", "invalid")
	_, err = LoadConfig()
	require.Error(t, err)
}

func TestInvalidConfiguration(t *testing.T) {
	for _, test := range []struct{ key, value string }{
		{"STORAGE", "unknown"},
		{"STORAGE", "postgres"},
		{"LOG_LEVEL", "verbose"},
	} {
		t.Run(test.key+"_"+test.value, func(t *testing.T) {
			for _, key := range []string{"STORAGE", "DATABASE_URL", "LOG_LEVEL"} {
				t.Setenv(key, "")
			}
			t.Setenv(test.key, test.value)
			cfg, err := LoadConfig()
			if err == nil {
				err = cfg.ValidateServer()
			}
			require.Error(t, err)
		})
	}
}

func TestConfigurationValidation(t *testing.T) {
	base := defaultTestConfig(t)
	for _, test := range []struct {
		name   string
		change func(*Config)
	}{
		{"unknown storage", func(c *Config) { c.Storage.Kind = "unknown" }},
		{"postgres without URL", func(c *Config) { c.Storage.Kind = "postgres"; c.Database.URL = "" }},
		{"startup timeout", func(c *Config) { c.Database.ConnectTimeout = 0 }},
		{"body limit", func(c *Config) { c.HTTP.RequestBodyLimit = 0 }},
		{"HTTP timeout", func(c *Config) { c.HTTP.ShutdownTimeout = 0 }},
		{"page limit", func(c *Config) { c.GraphQL.MaxPageSize = 0 }},
		{"complexity limit", func(c *Config) { c.GraphQL.QueryComplexity = 0 }},
		{"subscription buffer", func(c *Config) { c.Subscription.BufferSize = 0 }},
		{"subscription ping", func(c *Config) { c.Subscription.WebSocketPing = 0 }},
		{"subscription initialization", func(c *Config) { c.Subscription.InitTimeout = 0 }},
		{"JWT secret too short", func(c *Config) { c.Auth.JWTSecret = "short" }},
		{"JWT issuer empty", func(c *Config) { c.Auth.JWTIssuer = " " }},
		{"JWT TTL too short", func(c *Config) { c.Auth.TokenTTL = time.Millisecond }},
		{"refresh TTL too short", func(c *Config) { c.Auth.RefreshTTL = c.Auth.TokenTTL }},
		{"password cost", func(c *Config) { c.Auth.PasswordCost = 4 }},
		{"password length", func(c *Config) { c.Auth.MinPasswordLength = 0 }},
		{"request rate", func(c *Config) { c.Auth.RequestRate = 0 }},
		{"request burst", func(c *Config) { c.Auth.RequestBurst = 0 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := base
			test.change(&cfg)
			require.Error(t, cfg.ValidateServer())
		})
	}
	base.Database.URL = "postgres://example"
	require.NoError(t, base.ValidateMigration())
	for _, test := range []struct {
		name   string
		change func(*Config)
	}{
		{"missing URL", func(c *Config) { c.Database.URL = "" }},
		{"migration timeout", func(c *Config) { c.Migration.Timeout = -time.Second }},
		{"connect timeout", func(c *Config) { c.Database.ConnectTimeout = 0 }},
		{"statement timeout", func(c *Config) { c.Migration.StatementTimeout = 0 }},
		{"lock timeout", func(c *Config) { c.Migration.LockTimeout = 0 }},
	} {
		t.Run(test.name, func(t *testing.T) { cfg := base; test.change(&cfg); require.Error(t, cfg.ValidateMigration()) })
	}
}

func TestDatabaseConnectionURL(t *testing.T) {
	t.Parallel()
	empty, err := (DatabaseConfig{}).ConnectionURL()
	require.NoError(t, err)
	require.Empty(t, empty)
	cfg := DatabaseConfig{Host: "::1", Port: 55432, User: "test", Password: "a@:/?#", Name: "ozon_test", SSLMode: "disable"}
	dsn, err := cfg.ConnectionURL()
	require.NoError(t, err)
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	password, ok := u.User.Password()
	require.True(t, ok)
	require.Equal(t, cfg.Password, password)
	require.Equal(t, "[::1]:55432", u.Host)
	require.Equal(t, "disable", u.Query().Get("sslmode"))
	cfg.URL = "postgres://external/database"
	dsn, err = cfg.ConnectionURL()
	require.NoError(t, err)
	require.Equal(t, cfg.URL, dsn)
	for _, invalid := range []DatabaseConfig{{Password: "secret"}, {Host: "localhost", Port: 65536, Password: "secret"}, {Host: "localhost", Port: -1, Password: "secret"}} {
		_, err := invalid.ConnectionURL()
		require.Error(t, err)
	}
}

func TestLoadDatabaseCredentials(t *testing.T) {
	defaultTestConfig(t)
	t.Setenv("POSTGRES_HOST", "localhost")
	t.Setenv("POSTGRES_PORT", "5433")
	t.Setenv("POSTGRES_USER", "user")
	t.Setenv("POSTGRES_PASSWORD", "password")
	t.Setenv("POSTGRES_DB", "database")
	t.Setenv("POSTGRES_SSLMODE", "disable")
	cfg, err := LoadConfig()
	require.NoError(t, err)
	require.Equal(t, "postgres://user:password@localhost:5433/database?sslmode=disable", cfg.Database.URL)
	t.Setenv("POSTGRES_PORT", "65536")
	_, err = LoadConfig()
	require.ErrorContains(t, err, "PostgreSQL connection settings")
}
