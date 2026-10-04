package memory

import (
	"context"

	"ozon/internal/domain"
)

type UserRepository struct{ db *Database }

func NewUserRepository(db *Database) *UserRepository { return &UserRepository{db: db} }

func (r *UserRepository) FindUserByLogin(ctx context.Context, login string) (domain.User, error) {
	if err := ctx.Err(); err != nil {
		return domain.User{}, err
	}
	r.db.usersMu.RLock()
	defer r.db.usersMu.RUnlock()
	user, exists := r.db.users[login]
	if !exists {
		return domain.User{}, domain.ErrNotFound
	}
	return user, nil
}

func (r *UserRepository) CreateUser(ctx context.Context, user domain.User) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.db.usersMu.Lock()
	defer r.db.usersMu.Unlock()
	if _, exists := r.db.users[user.Login]; exists {
		return domain.ErrConflict
	}
	r.db.users[user.Login] = user
	return nil
}
