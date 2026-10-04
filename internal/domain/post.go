package domain

import "time"

type Post struct {
	ID              string
	AuthorID        string
	Title           string
	Content         string
	CommentsEnabled bool
	CreatedAt       time.Time
}
