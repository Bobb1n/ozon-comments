package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"ozon/internal/domain"
	"ozon/mocks"
)

func TestScanCommentErrors(t *testing.T) {
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
			_, err := scanComment(row)
			require.ErrorIs(t, err, want)
		})
	}
}

func TestScanCommentNullableParent(t *testing.T) {
	t.Parallel()
	for _, parent := range []string{"", "parent"} {
		t.Run("parent="+parent, func(t *testing.T) {
			t.Parallel()
			row := mocks.NewRow(t)
			now := time.Now().UTC()
			row.On("Scan", mock.Anything).
				Run(func(args mock.Arguments) {
					dest := args[0].([]any)
					*dest[0].(*string) = "c"
					*dest[1].(*string) = "p"
					if parent != "" {
						*dest[2].(**string) = &parent
					}
					*dest[3].(*string) = "reader"
					*dest[4].(*string) = "text"
					*dest[5].(*time.Time) = now
				}).Return(nil).Once()
			comment, err := scanComment(row)
			require.NoError(t, err)
			require.Equal(t, domain.Comment{ID: "c", PostID: "p", ParentID: parent, AuthorID: "reader", Text: "text", CreatedAt: now}, comment)
		})
	}
}
