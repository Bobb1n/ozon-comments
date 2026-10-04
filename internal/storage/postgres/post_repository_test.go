package postgres

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"ozon/internal/domain"
	"ozon/mocks"
)

func TestScanPostErrors(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name           string
		returned, want error
	}{
		{"not found", pgx.ErrNoRows, domain.ErrNotFound},
		{"conversion failed", errors.New("invalid column type"), nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			want := test.want
			if want == nil {
				want = test.returned
			}
			row := mocks.NewRow(t)
			row.On("Scan", mock.Anything).Return(test.returned).Once()
			_, err := scanPost(row)
			require.ErrorIs(t, err, want)
		})
	}
}
