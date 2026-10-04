package dto

import "time"

type Cursor struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
	Scope     string    `json:"scope"`
}
