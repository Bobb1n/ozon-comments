package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"ozon/internal/domain"
)

type UserRepository struct{ pool *pgxpool.Pool }

func NewUserRepository(pool *pgxpool.Pool) *UserRepository { return &UserRepository{pool: pool} }

func (r *UserRepository) FindUserByLogin(ctx context.Context, login string) (domain.User, error) {
	var user domain.User
	err := r.pool.QueryRow(ctx, "SELECT id, login, password_hash FROM users WHERE login=$1", login).Scan(&user.ID, &user.Login, &user.PasswordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return user, domain.ErrNotFound
	}
	if err != nil {
		return user, fmt.Errorf("read login user: %w", err)
	}
	return user, nil
}

func (r *UserRepository) CreateUser(ctx context.Context, user domain.User) error {
	_, err := r.pool.Exec(ctx, "INSERT INTO users (id,login,password_hash) VALUES ($1,$2,$3)", user.ID, user.Login, user.PasswordHash)
	var constraint *pgconn.PgError
	if errors.As(err, &constraint) && constraint.Code == "23505" && constraint.ConstraintName == "users_login_key" {
		return domain.ErrConflict
	}
	return err
}
