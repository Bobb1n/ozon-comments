package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/source/file"

	"ozon/internal/app"
	"ozon/pkg/logger"
	"ozon/pkg/postgres"
)

const (
	commandUp   = "up"
	commandDown = "down"
)

func main() {
	command, err := parseCommand(os.Args[1:])
	if err != nil {
		panic(err)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		panic(err)
	}
	if err := cfg.ValidateMigration(); err != nil {
		panic(err)
	}
	log := logger.NewLogger(cfg.Log.Level, cfg.Log.ServiceName).With("command", command)
	log.Info("database migration started")
	if err := runMigrations(cfg, command); err != nil {
		log.Error("database migration failed", "error", err)
		panic(err)
	}
	log.Info("database migration completed")
}

func parseCommand(args []string) (string, error) {
	if len(args) == 0 {
		return commandUp, nil
	}
	if len(args) != 1 || (args[0] != commandUp && args[0] != commandDown) {
		return "", fmt.Errorf("usage: migrate [up|down]; down rolls back one migration")
	}
	return args[0], nil
}

func runMigrations(cfg app.Config, command string) error {
	signals, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	ctx, cancel := context.WithTimeout(signals, cfg.Migration.Timeout)
	defer func() { cancel(); stop() }()
	runner, err := newMigrationRunner(ctx, cfg)
	if err != nil {
		return err
	}
	stopOnCancel := context.AfterFunc(ctx, func() { runner.GracefulStop <- true })
	if command == commandUp {
		err = applyMigrations(runner)
	} else {
		err = rollbackMigration(runner)
	}
	stopOnCancel()
	sourceErr, databaseErr := runner.Close()
	if errors.Is(err, migrate.ErrNoChange) {
		err = nil
	}
	return errors.Join(err, sourceErr, databaseErr, ctx.Err())
}

func applyMigrations(runner *migrate.Migrate) error { return runner.Up() }

func rollbackMigration(runner *migrate.Migrate) error { return runner.Steps(-1) }

func newMigrationRunner(ctx context.Context, cfg app.Config) (*migrate.Migrate, error) {
	path, err := filepath.Abs(cfg.Migration.Path)
	if err != nil {
		return nil, fmt.Errorf("migration path: %w", err)
	}
	driver, err := postgres.NewMigrationDriver(ctx, cfg.Database.URL, postgres.MigrationOptions{
		ConnectTimeout: cfg.Database.ConnectTimeout, StatementTimeout: cfg.Migration.StatementTimeout,
	})
	if err != nil {
		return nil, err
	}
	source := (&url.URL{Scheme: "file", Path: path}).String()
	runner, err := migrate.NewWithDatabaseInstance(source, "pgx5", driver)
	if err != nil {
		return nil, errors.Join(err, driver.Close())
	}
	runner.LockTimeout = cfg.Migration.LockTimeout
	return runner, nil
}
