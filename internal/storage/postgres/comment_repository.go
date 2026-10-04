package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"ozon/internal/domain"
)

const commentColumns = "id, post_id, parent_id, author_id, text, created_at"

type CommentRepository struct{ pool *pgxpool.Pool }

func NewCommentRepository(pool *pgxpool.Pool) *CommentRepository {
	return &CommentRepository{pool: pool}
}

func (r *CommentRepository) InsertComment(ctx context.Context, comment domain.Comment) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	var enabled bool
	if err = tx.QueryRow(ctx, "SELECT comments_enabled FROM posts WHERE id=$1 FOR SHARE", comment.PostID).Scan(&enabled); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		return fmt.Errorf("read post permission: %w", err)
	}
	if !enabled {
		return domain.ErrCommentsDisabled
	}
	if comment.ParentID != "" {
		var postID string
		if err = tx.QueryRow(ctx, "SELECT post_id FROM comments WHERE id=$1", comment.ParentID).Scan(&postID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.ErrNotFound
			}
			return fmt.Errorf("read parent comment: %w", err)
		}
		if postID != comment.PostID {
			return domain.ErrInvalidInput
		}
	}
	const query = `
		INSERT INTO comments (id, post_id, parent_id, author_id, text, created_at)
		VALUES ($1, $2, NULLIF($3, ''), $4, $5, $6)`
	_, err = tx.Exec(ctx, query,
		comment.ID, comment.PostID, comment.ParentID,
		comment.AuthorID, comment.Text, comment.CreatedAt,
	)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *CommentRepository) ListComments(ctx context.Context, postID, parentID string, page domain.PageRequest) (domain.Page[domain.Comment], error) {
	result := domain.Page[domain.Comment]{Items: make([]domain.Comment, 0, page.Limit)}
	var exists bool
	if err := r.pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM posts WHERE id=$1)", postID).Scan(&exists); err != nil {
		return result, err
	}
	if !exists {
		return result, domain.ErrNotFound
	}
	if parentID != "" {
		var parentPost string
		if err := r.pool.QueryRow(ctx, "SELECT post_id FROM comments WHERE id=$1", parentID).Scan(&parentPost); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return result, domain.ErrNotFound
			}
			return result, fmt.Errorf("read parent comment: %w", err)
		}
		if parentPost != postID {
			return result, domain.ErrInvalidInput
		}
	}
	query := "SELECT " + commentColumns + " FROM comments WHERE post_id=$1"
	args := []any{postID}
	if parentID == "" {
		query += " AND parent_id IS NULL"
	} else {
		args = append(args, parentID)
		query += " AND parent_id=$2"
	}
	if page.After != nil {
		query += fmt.Sprintf(" AND (created_at,id) > ($%d,$%d)", len(args)+1, len(args)+2)
		args = append(args, page.After.CreatedAt, page.After.ID)
	}
	args = append(args, page.Limit+1)
	query += fmt.Sprintf(" ORDER BY created_at,id LIMIT $%d", len(args))
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return result, err
	}
	defer rows.Close()

	for rows.Next() {
		comment, err := scanComment(rows)
		if err != nil {
			return result, err
		}
		result.Items = append(result.Items, comment)
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

func scanComment(row pgx.Row) (domain.Comment, error) {
	var comment domain.Comment
	var parentID *string
	err := row.Scan(&comment.ID, &comment.PostID, &parentID,
		&comment.AuthorID, &comment.Text, &comment.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return comment, domain.ErrNotFound
	}
	if err != nil {
		return comment, fmt.Errorf("scan comment: %w", err)
	}
	if parentID != nil {
		comment.ParentID = *parentID
	}
	return comment, nil
}
