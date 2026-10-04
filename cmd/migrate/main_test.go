package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"ozon/internal/app"
)

func TestParseCommand(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		args    []string
		command string
		invalid bool
	}{
		{"default", nil, commandUp, false},
		{"up", []string{"up"}, commandUp, false},
		{"down", []string{"down"}, commandDown, false},
		{"unknown", []string{"drop"}, "", true},
		{"extra arguments", []string{"down", "all"}, "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			command, err := parseCommand(test.args)
			if test.invalid {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, test.command, command)
		})
	}
}

func TestMigrationInitializationFailsWithInvalidDatabase(t *testing.T) {
	cfg := app.Config{Database: app.DatabaseConfig{URL: "://invalid"}, Migration: app.MigrationConfig{Path: "../../migrations"}}
	_, err := newMigrationRunner(t.Context(), cfg)
	require.ErrorContains(t, err, "migration connection config")
}
