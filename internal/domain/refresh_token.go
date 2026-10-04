package domain

import "time"

type RefreshToken struct {
	Hash      [32]byte
	UserID    string
	SessionID string
	ExpiresAt time.Time
	Used      bool
	Revoked   bool
}
