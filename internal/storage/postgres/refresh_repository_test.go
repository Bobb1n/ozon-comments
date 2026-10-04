package postgres

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"ozon/internal/domain"
)

func TestRefreshError(t *testing.T) {
	t.Parallel()
	other := errors.New("connection lost")
	for _, test := range []struct {
		name        string
		given, want error
	}{
		{"missing token", pgx.ErrNoRows, domain.ErrUnauthenticated},
		{"database error", other, other},
		{"success", nil, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, refreshError(test.given), test.want)
		})
	}
}
