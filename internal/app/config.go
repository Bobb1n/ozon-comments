package app

import (
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Log          LogConfig
	HTTP         HTTPConfig
	Database     DatabaseConfig
	Storage      StorageConfig
	GraphQL      GraphQLConfig
	Subscription SubscriptionConfig
	Migration    MigrationConfig
	Auth         AuthConfig
	Metrics      MetricsConfig
}

type AuthConfig struct {
	JWTSecret         string        `env:"AUTH_JWT_SECRET"`
	JWTIssuer         string        `env:"AUTH_JWT_ISSUER" envDefault:"ozon-comments"`
	TokenTTL          time.Duration `env:"AUTH_TOKEN_TTL" envDefault:"15m"`
	RefreshTTL        time.Duration `env:"AUTH_REFRESH_TTL" envDefault:"168h"`
	PasswordCost      int           `env:"AUTH_PASSWORD_COST" envDefault:"10"`
	MinPasswordLength int           `env:"AUTH_MIN_PASSWORD_LENGTH" envDefault:"12"`
	RequestRate       float64       `env:"AUTH_REQUEST_RATE" envDefault:"2"`
	RequestBurst      int           `env:"AUTH_REQUEST_BURST" envDefault:"5"`
}

type MetricsConfig struct {
	Enabled bool `env:"METRICS_ENABLED" envDefault:"false"`
}

type LogConfig struct {
	ServiceName string     `env:"SERVICE_NAME,notEmpty" envDefault:"ozon-comments"`
	Level       slog.Level `env:"LOG_LEVEL" envDefault:"info"`
}

type HTTPConfig struct {
	Address           string        `env:"HTTP_ADDR,notEmpty" envDefault:":8080"`
	ShutdownTimeout   time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"10s"`
	ReadHeaderTimeout time.Duration `env:"HTTP_READ_HEADER_TIMEOUT" envDefault:"5s"`
	ReadTimeout       time.Duration `env:"HTTP_READ_TIMEOUT" envDefault:"15s"`
	IdleTimeout       time.Duration `env:"HTTP_IDLE_TIMEOUT" envDefault:"60s"`
	RequestBodyLimit  int64         `env:"HTTP_MAX_BODY_BYTES" envDefault:"1048576"`
}

type DatabaseConfig struct {
	URL            string        `env:"DATABASE_URL"`
	Host           string        `env:"POSTGRES_HOST" envDefault:"127.0.0.1"`
	Port           int           `env:"POSTGRES_PORT" envDefault:"5432"`
	User           string        `env:"POSTGRES_USER" envDefault:"ozon"`
	Password       string        `env:"POSTGRES_PASSWORD"`
	Name           string        `env:"POSTGRES_DB" envDefault:"ozon"`
	SSLMode        string        `env:"POSTGRES_SSLMODE" envDefault:"disable"`
	ConnectTimeout time.Duration `env:"STARTUP_TIMEOUT" envDefault:"30s"`
}

type StorageConfig struct {
	Kind string `env:"STORAGE" envDefault:"inmemory"`
}

type GraphQLConfig struct {
	QueryComplexity int `env:"GRAPHQL_COMPLEXITY_LIMIT" envDefault:"1000"`
	MaxPageSize     int `env:"MAX_PAGE_SIZE" envDefault:"100"`
}

type SubscriptionConfig struct {
	BufferSize    int           `env:"SUBSCRIPTION_BUFFER" envDefault:"64"`
	WebSocketPing time.Duration `env:"WS_PING_INTERVAL" envDefault:"20s"`
}

type MigrationConfig struct {
	Path             string        `env:"MIGRATIONS_PATH,notEmpty" envDefault:"./migrations"`
	Timeout          time.Duration `env:"MIGRATION_TIMEOUT" envDefault:"30s"`
	StatementTimeout time.Duration `env:"MIGRATION_STATEMENT_TIMEOUT" envDefault:"15s"`
	LockTimeout      time.Duration `env:"MIGRATION_LOCK_TIMEOUT" envDefault:"15s"`
}

func (cfg Config) ValidateServer() error {
	if cfg.Auth.TokenTTL < time.Second || cfg.Auth.RefreshTTL <= cfg.Auth.TokenTTL || cfg.Auth.RequestRate <= 0 || math.IsNaN(cfg.Auth.RequestRate) || math.IsInf(cfg.Auth.RequestRate, 0) || cfg.Auth.RequestBurst < 1 {
		return fmt.Errorf("authentication limits must be positive")
	}
	if len(cfg.Auth.JWTSecret) < 32 || strings.TrimSpace(cfg.Auth.JWTIssuer) == "" {
		return fmt.Errorf("AUTH_JWT_SECRET must contain at least 32 bytes and AUTH_JWT_ISSUER must not be empty")
	}
	if cfg.Auth.PasswordCost < 10 || cfg.Auth.PasswordCost > 14 || cfg.Auth.MinPasswordLength < 8 || cfg.Auth.MinPasswordLength > 72 {
		return fmt.Errorf("invalid password settings")
	}
	if cfg.Storage.Kind != "inmemory" && cfg.Storage.Kind != "postgres" {
		return fmt.Errorf("STORAGE must be inmemory or postgres")
	}
	if cfg.Storage.Kind == "postgres" && cfg.Database.URL == "" {
		return fmt.Errorf("POSTGRES_PASSWORD or DATABASE_URL is required for postgres")
	}
	if cfg.Database.ConnectTimeout <= 0 {
		return fmt.Errorf("STARTUP_TIMEOUT must be positive")
	}
	if err := cfg.HTTP.validate(); err != nil {
		return err
	}
	if cfg.GraphQL.MaxPageSize < 1 || cfg.GraphQL.QueryComplexity < 1 {
		return fmt.Errorf("GraphQL page and complexity limits must be positive")
	}
	if cfg.Subscription.BufferSize < 1 || cfg.Subscription.WebSocketPing <= 0 {
		return fmt.Errorf("subscription buffer and ping interval must be positive")
	}
	return nil
}

func (cfg Config) ValidateMigration() error {
	if cfg.Database.URL == "" {
		return fmt.Errorf("POSTGRES_PASSWORD or DATABASE_URL is required for migrations")
	}
	if cfg.Migration.Timeout <= 0 || cfg.Database.ConnectTimeout <= 0 || cfg.Migration.StatementTimeout <= 0 || cfg.Migration.LockTimeout <= 0 {
		return fmt.Errorf("migration timeouts must be positive")
	}
	return nil
}

func (cfg HTTPConfig) validate() error {
	if cfg.RequestBodyLimit < 1 {
		return fmt.Errorf("HTTP_MAX_BODY_BYTES must be positive")
	}
	if cfg.ShutdownTimeout <= 0 || cfg.ReadHeaderTimeout <= 0 || cfg.ReadTimeout <= 0 || cfg.IdleTimeout <= 0 {
		return fmt.Errorf("HTTP timeouts must be positive")
	}
	return nil
}

func (cfg DatabaseConfig) ConnectionURL() (string, error) {
	if cfg.URL != "" {
		return cfg.URL, nil
	}
	if cfg.Password == "" {
		return "", nil
	}
	if cfg.Host == "" || cfg.Port < 1 || cfg.Port > 65535 || cfg.User == "" || cfg.Name == "" {
		return "", fmt.Errorf("invalid PostgreSQL connection settings")
	}
	u := url.URL{Scheme: "postgres", User: url.UserPassword(cfg.User, cfg.Password), Host: net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)), Path: "/" + cfg.Name}
	u.RawQuery = url.Values{"sslmode": []string{cfg.SSLMode}}.Encode()
	return u.String(), nil
}
