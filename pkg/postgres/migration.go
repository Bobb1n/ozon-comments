package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-migrate/migrate/v4/database"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

type MigrationOptions struct {
	ConnectTimeout   time.Duration
	StatementTimeout time.Duration
}

func NewMigrationDriver(ctx context.Context, databaseURL string, options MigrationOptions) (database.Driver, error) {
	cfg, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("migration connection config: %w", err)
	}
	cfg.ConnectTimeout = options.ConnectTimeout
	cfg.RuntimeParams["statement_timeout"] = strconv.FormatInt(max(1, options.StatementTimeout.Milliseconds()), 10)
	db := stdlib.OpenDB(*cfg)
	if err := db.PingContext(ctx); err != nil {
		return nil, errors.Join(fmt.Errorf("connect migration database: %w", err), db.Close())
	}
	driver, err := migratepgx.WithInstance(db, &migratepgx.Config{StatementTimeout: options.StatementTimeout})
	if err != nil {
		return nil, errors.Join(err, db.Close())
	}
	return driver, nil
}
