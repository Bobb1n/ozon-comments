package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"ozon/internal/domain"
)

const postColumns = "id, author_id, title, content, comments_enabled, created_at"

type PostRepository struct{ pool *pgxpool.Pool }

func NewPostRepository(pool *pgxpool.Pool) *PostRepository {
	return &PostRepository{pool: pool}
}

func (r *PostRepository) CreatePost(ctx context.Context, post domain.Post) error {
	const query = `
		INSERT INTO posts (id, author_id, title, content, comments_enabled, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`
	_, err := r.pool.Exec(ctx, query,
		post.ID, post.AuthorID, post.Title, post.Content,
		post.CommentsEnabled, post.CreatedAt,
	)
	return err
}

func (r *PostRepository) GetPost(ctx context.Context, id string) (domain.Post, error) {
	return scanPost(r.pool.QueryRow(ctx, "SELECT "+postColumns+" FROM posts WHERE id=$1", id))
}

func (r *PostRepository) ListPosts(ctx context.Context, page domain.PageRequest) (domain.Page[domain.Post], error) {
	query := "SELECT " + postColumns + " FROM posts"
	args := []any{}
	if page.After != nil {
		query += " WHERE (created_at,id) > ($1,$2)"
		args = append(args, page.After.CreatedAt, page.After.ID)
	}
	args = append(args, page.Limit+1)
	query += fmt.Sprintf(" ORDER BY created_at,id LIMIT $%d", len(args))
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return domain.Page[domain.Post]{}, err
	}
	defer rows.Close()

	result := domain.Page[domain.Post]{Items: make([]domain.Post, 0, page.Limit)}
	for rows.Next() {
		post, err := scanPost(rows)
		if err != nil {
			return result, err
		}
		result.Items = append(result.Items, post)
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	if len(result.Items) > page.Limit {
		result.HasNext = true
		result.Items = result.Items[:page.Limit]
	}
	return result, nil
}

func (r *PostRepository) SetCommentsEnabled(ctx context.Context, postID, user string, enabled bool) (domain.Post, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Post{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	post, err := scanPost(tx.QueryRow(ctx, "SELECT "+postColumns+" FROM posts WHERE id=$1 FOR UPDATE", postID))
	if err != nil {
		return post, err
	}
	if post.AuthorID != user {
		return domain.Post{}, domain.ErrForbidden
	}
	if _, err = tx.Exec(ctx, "UPDATE posts SET comments_enabled=$2 WHERE id=$1", postID, enabled); err != nil {
		return domain.Post{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.Post{}, err
	}
	post.CommentsEnabled = enabled
	return post, nil
}

func scanPost(row pgx.Row) (domain.Post, error) {
	var post domain.Post
	err := row.Scan(&post.ID, &post.AuthorID, &post.Title, &post.Content,
		&post.CommentsEnabled, &post.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return post, domain.ErrNotFound
	}
	if err != nil {
		return post, fmt.Errorf("scan post: %w", err)
	}
	return post, nil
}
