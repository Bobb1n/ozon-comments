package integration_test

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"os"
	"strconv"
	"testing"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
	"github.com/stretchr/testify/require"
)

type testDatabaseConfig struct {
	URL      string `env:"TEST_DATABASE_URL"`
	Host     string `env:"TEST_POSTGRES_HOST" envDefault:"127.0.0.1"`
	Port     int    `env:"TEST_POSTGRES_PORT" envDefault:"55432"`
	User     string `env:"TEST_POSTGRES_USER" envDefault:"ozon_test"`
	Password string `env:"TEST_POSTGRES_PASSWORD"`
	Name     string `env:"TEST_POSTGRES_DB" envDefault:"ozon_test"`
	SSLMode  string `env:"TEST_POSTGRES_SSLMODE" envDefault:"disable"`
}

func TestMain(m *testing.M) {
	if err := configureTestDatabase(); err != nil {
		panic(err)
	}
	m.Run()
}

func TestTestDatabaseConnectionURL(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		cfg       testDatabaseConfig
		want      string
		wantError bool
	}{
		{name: "unconfigured", want: ""},
		{name: "explicit URL", cfg: testDatabaseConfig{URL: "postgres://external/test"}, want: "postgres://external/test"},
		{name: "credentials", cfg: testDatabaseConfig{Host: "::1", Port: 55432, User: "test", Password: "a@:/?#", Name: "test", SSLMode: "disable"}, want: "postgres://test:a%40%3A%2F%3F%23@[::1]:55432/test?sslmode=disable"},
		{name: "invalid credentials", cfg: testDatabaseConfig{Password: "secret"}, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := test.cfg.connectionURL()
			if test.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}
}

func configureTestDatabase() error {
	path := os.Getenv("ENV_FILE")
	explicit := path != ""
	if !explicit {
		path = "../../.env"
	}
	if err := godotenv.Load(path); err != nil && (explicit || !errors.Is(err, fs.ErrNotExist)) {
		return fmt.Errorf("load test environment: %w", err)
	}
	cfg, err := env.ParseAs[testDatabaseConfig]()
	if err != nil {
		return fmt.Errorf("parse test database environment: %w", err)
	}
	dsn, err := cfg.connectionURL()
	if err != nil {
		return err
	}
	return os.Setenv("TEST_DATABASE_URL", dsn)
}

func (cfg testDatabaseConfig) connectionURL() (string, error) {
	if cfg.URL != "" {
		return cfg.URL, nil
	}
	if cfg.Password == "" {
		return "", nil
	}
	if cfg.Host == "" || cfg.Port < 1 || cfg.Port > 65535 || cfg.User == "" || cfg.Name == "" {
		return "", fmt.Errorf("invalid test database connection settings")
	}
	u := url.URL{Scheme: "postgres", User: url.UserPassword(cfg.User, cfg.Password), Host: net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)), Path: "/" + cfg.Name}
	u.RawQuery = url.Values{"sslmode": []string{cfg.SSLMode}}.Encode()
	return u.String(), nil
}
