package memory

import (
	"sync"
	"time"

	"ozon/internal/domain"
)

type Database struct {
	mu            sync.RWMutex
	posts         map[string]*postData
	postOrder     []domain.Post
	commentPosts  sync.Map
	usersMu       sync.RWMutex
	users         map[string]domain.User
	refreshMu     sync.Mutex
	refreshTokens map[[32]byte]domain.RefreshToken
}

type postData struct {
	mu       sync.RWMutex
	post     domain.Post
	comments map[string]domain.Comment
	lists    map[string][]domain.Comment
}

func NewDatabase() *Database {
	return &Database{
		posts:         make(map[string]*postData),
		users:         make(map[string]domain.User),
		refreshTokens: make(map[[32]byte]domain.RefreshToken),
	}
}

func isAfterCursor(createdAt time.Time, id string, cursor *domain.PageCursor) bool {
	if cursor == nil {
		return true
	}
	if createdAt.Equal(cursor.CreatedAt) {
		return id > cursor.ID
	}
	return createdAt.After(cursor.CreatedAt)
}

func (db *Database) findPost(id string) (*postData, error) {
	db.mu.RLock()
	post, exists := db.posts[id]
	db.mu.RUnlock()
	if !exists {
		return nil, domain.ErrNotFound
	}
	return post, nil
}
