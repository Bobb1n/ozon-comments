package service_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ozon/internal/domain"
	"ozon/internal/service"
)

func TestValidatePage(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		page  domain.PageRequest
		valid bool
	}{
		{"minimum", domain.PageRequest{Limit: 1}, true},
		{"maximum", domain.PageRequest{Limit: 100}, true},
		{"zero", domain.PageRequest{}, false},
		{"negative", domain.PageRequest{Limit: -1}, false},
		{"above maximum", domain.PageRequest{Limit: 101}, false},
		{"missing ID", domain.PageRequest{Limit: 1, After: &domain.PageCursor{CreatedAt: time.Now()}}, false},
		{"missing time", domain.PageRequest{Limit: 1, After: &domain.PageCursor{ID: "c"}}, false},
		{"complete cursor", domain.PageRequest{Limit: 1, After: &domain.PageCursor{ID: "c", CreatedAt: time.Now()}}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := service.ValidatePage(test.page, service.MaxPageSize)
			if test.valid {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, domain.ErrInvalidInput)
			}
		})
	}
}
