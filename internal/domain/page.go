package domain

import "time"

type PageCursor struct {
	CreatedAt time.Time
	ID        string
}

type PageRequest struct {
	Limit int
	After *PageCursor
}

type Page[T any] struct {
	Items   []T
	HasNext bool
}
