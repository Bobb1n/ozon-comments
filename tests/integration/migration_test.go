package integration_test

import (
	"context"
	"errors"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5"

	postgresdb "ozon/pkg/postgres"
)

func verifyMigrationRollback(t *testing.T, ctx context.Context, databaseURL string, connection *pgx.Conn) {
	t.Helper()
	runner, err := newMigrationRunner(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		sourceErr, databaseErr := runner.Close()
		if sourceErr != nil || databaseErr != nil {
			t.Errorf("close migrations: %v, %v", sourceErr, databaseErr)
		}
	}()
	version, dirty, err := runner.Version()
	if err != nil || dirty || version != 4 {
		t.Fatalf("migration version: %d, dirty=%v, error=%v", version, dirty, err)
	}
	if err := runner.Steps(-1); err != nil {
		t.Fatalf("rollback refresh tokens: %v", err)
	}
	version, dirty, err = runner.Version()
	if err != nil || dirty || version != 3 {
		t.Fatalf("after rollback: %d, dirty=%v, error=%v", version, dirty, err)
	}
	if err := runner.Up(); err != nil {
		t.Fatalf("reapply comments: %v", err)
	}
	if err := runner.Down(); err != nil {
		t.Fatalf("rollback all migrations: %v", err)
	}

	if err := connection.Ping(ctx); err != nil {
		t.Fatal(err)
	}
}

func newMigrationRunner(ctx context.Context, databaseURL string) (*migrate.Migrate, error) {
	path, err := filepath.Abs("../../migrations")
	if err != nil {
		return nil, err
	}
	driver, err := postgresdb.NewMigrationDriver(ctx, databaseURL, postgresdb.MigrationOptions{
		ConnectTimeout: 10 * time.Second, StatementTimeout: 15 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	runner, err := migrate.NewWithDatabaseInstance((&url.URL{Scheme: "file", Path: path}).String(), "pgx5", driver)
	if err != nil {
		return nil, errors.Join(err, driver.Close())
	}
	return runner, nil
}
