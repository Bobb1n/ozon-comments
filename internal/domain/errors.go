package domain

import "errors"

var (
	ErrConflict         = errors.New("login already exists")
	ErrNotFound         = errors.New("not found")
	ErrForbidden        = errors.New("only the author can change comment permissions")
	ErrCommentsDisabled = errors.New("comments are disabled")
	ErrInvalidInput     = errors.New("invalid input")
	ErrUnauthenticated  = errors.New("authentication required or credentials invalid")
)
