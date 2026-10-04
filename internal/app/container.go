package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/caarlos0/env/v11"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	"ozon/internal/service"
	"ozon/internal/storage/memory"
	"ozon/internal/storage/postgres"
	graph "ozon/internal/transport/graphql"
	httpserver "ozon/internal/transport/http"
	"ozon/pkg/logger"
	"ozon/pkg/metrics"
	postgresdb "ozon/pkg/postgres"
)

type Container struct {
	server *httpserver.Server
	pool   *pgxpool.Pool
	logger *slog.Logger
}

func NewContainer() (*Container, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return nil, err
	}
	if err := cfg.ValidateServer(); err != nil {
		return nil, err
	}
	log := logger.NewLogger(cfg.Log.Level, cfg.Log.ServiceName)
	container := &Container{logger: log}
	posts, comments, users, sessions, err := container.createRepositories(cfg)
	if err != nil {
		log.Error("storage initialization failed", "error", err)
		return nil, err
	}
	log.Debug("repositories initialized", "storage", cfg.Storage.Kind)
	auth, err := service.NewAuthService(users, sessions, service.AuthOptions{
		Secret: cfg.Auth.JWTSecret, Issuer: cfg.Auth.JWTIssuer, AccessTTL: cfg.Auth.TokenTTL, RefreshTTL: cfg.Auth.RefreshTTL,
		PasswordCost: cfg.Auth.PasswordCost, MinPasswordLength: cfg.Auth.MinPasswordLength,
	})
	if err != nil {
		container.Close()
		return nil, err
	}
	observer := container.createMetrics(cfg)
	hub := service.NewCommentHub(cfg.Subscription.BufferSize, log, observer)
	resolver := &graph.Resolver{
		Posts:         service.NewPostService(posts, cfg.GraphQL.MaxPageSize),
		Comments:      service.NewCommentService(comments, hub, cfg.GraphQL.MaxPageSize),
		Subscriptions: hub, PageLimit: cfg.GraphQL.MaxPageSize,
	}
	handler := graph.NewHandler(resolver, log, graph.HTTPOptions{
		WebSocketPing: cfg.Subscription.WebSocketPing, BodyLimit: cfg.HTTP.RequestBodyLimit, ComplexityLimit: cfg.GraphQL.QueryComplexity,
		WebSocketInitTimeout: cfg.Subscription.InitTimeout,
		Verifier:             auth,
		Metrics:              observer,
	})
	router := httpserver.NewRouter(handler, log,
		httpserver.AuthOptions{
			Login: auth, Registration: auth, Sessions: auth,
			BodyLimit: cfg.HTTP.RequestBodyLimit, RequestRate: cfg.Auth.RequestRate, RequestBurst: cfg.Auth.RequestBurst,
		},
		httpserver.MetricsOptions{Observer: observer, Handler: observer.Handler()},
	)
	container.server = httpserver.NewServer(httpserver.Options{
		Address: cfg.HTTP.Address, ShutdownTimeout: cfg.HTTP.ShutdownTimeout,
		ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout, ReadTimeout: cfg.HTTP.ReadTimeout, IdleTimeout: cfg.HTTP.IdleTimeout,
	}, router, log)
	log.Info("application initialized", "storage", cfg.Storage.Kind)
	return container, nil
}

func (c *Container) Run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer c.cleanup(stop)
	err := c.server.Run(ctx)
	if err != nil {
		c.logger.Error("application stopped with error", "error", err)
	}
	return err
}

func (c *Container) Close() {
	if c.pool != nil {
		c.pool.Close()
	}
}

func LoadConfig() (Config, error) {
	path := os.Getenv("ENV_FILE")
	explicit := path != ""
	if !explicit {
		path = ".env"
	}
	if err := godotenv.Load(path); err != nil && (explicit || !errors.Is(err, fs.ErrNotExist)) {
		return Config{}, fmt.Errorf("load env file: %w", err)
	}
	cfg, err := env.ParseAs[Config]()
	if err != nil {
		return Config{}, fmt.Errorf("parse environment: %w", err)
	}
	cfg.Database.URL, err = cfg.Database.ConnectionURL()
	return cfg, err
}

func (c *Container) createRepositories(cfg Config) (service.PostRepository, service.CommentRepository, service.UserRepository, service.RefreshRepository, error) {
	switch cfg.Storage.Kind {
	case "inmemory":
		database := memory.NewDatabase()
		return memory.NewPostRepository(database), memory.NewCommentRepository(database), memory.NewUserRepository(database), memory.NewRefreshRepository(database), nil
	case "postgres":
		ctx, cancel := context.WithTimeout(context.Background(), cfg.Database.ConnectTimeout)
		defer cancel()
		pool, err := postgresdb.Open(ctx, cfg.Database.URL)
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("initialize PostgreSQL: %w", err)
		}
		c.pool = pool
		return postgres.NewPostRepository(pool), postgres.NewCommentRepository(pool), postgres.NewUserRepository(pool), postgres.NewRefreshRepository(pool), nil
	default:
		return nil, nil, nil, nil, fmt.Errorf("unsupported storage: %s", cfg.Storage.Kind)
	}
}

func (c *Container) cleanup(stopSignals context.CancelFunc) {
	stopSignals()
	c.Close()
}

func (c *Container) createMetrics(cfg Config) *metrics.Metrics {
	observer := metrics.New(cfg.Metrics.Enabled, cfg.Log.ServiceName)
	if c.pool != nil {
		observer.RegisterPostgresPool(c.poolStats)
	}
	return observer
}

func (c *Container) poolStats() metrics.PoolStats {
	stats := c.pool.Stat()
	return metrics.PoolStats{
		Total: stats.TotalConns(), Idle: stats.IdleConns(), Acquired: stats.AcquiredConns(), Max: stats.MaxConns(),
	}
}
