package service

import (
	"fmt"

	"ozon/internal/domain"
)

const MaxPageSize = 100

func ValidatePage(page domain.PageRequest, limit int) error {
	if page.Limit < 1 || page.Limit > limit {
		return fmt.Errorf("%w: page size must be between 1 and %d", domain.ErrInvalidInput, limit)
	}
	if page.After != nil && (page.After.ID == "" || page.After.CreatedAt.IsZero()) {
		return fmt.Errorf("%w: incomplete page cursor", domain.ErrInvalidInput)
	}
	return nil
}
