package memory

import (
	"context"
	"sort"

	"ozon/internal/domain"
)

type CommentRepository struct{ db *Database }

func NewCommentRepository(db *Database) *CommentRepository {
	return &CommentRepository{db: db}
}

func (r *CommentRepository) InsertComment(ctx context.Context, comment domain.Comment) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := r.db.findPost(comment.PostID)
	if err != nil {
		return err
	}
	data.mu.Lock()
	defer data.mu.Unlock()
	if !data.post.CommentsEnabled {
		return domain.ErrCommentsDisabled
	}
	if err := r.validateParent(data, comment.ParentID); err != nil {
		return err
	}
	list := data.lists[comment.ParentID]
	i := len(list)
	if i > 0 && !isAfterCursor(comment.CreatedAt, comment.ID, &domain.PageCursor{CreatedAt: list[i-1].CreatedAt, ID: list[i-1].ID}) {
		i = sort.Search(len(list), func(i int) bool {
			existing := list[i]
			return existing.CreatedAt.After(comment.CreatedAt) || (existing.CreatedAt.Equal(comment.CreatedAt) && existing.ID >= comment.ID)
		})
	}
	list = append(list, domain.Comment{})
	copy(list[i+1:], list[i:])
	list[i] = comment
	data.lists[comment.ParentID] = list
	data.comments[comment.ID] = comment
	r.db.commentPosts.Store(comment.ID, comment.PostID)
	return nil
}

func (r *CommentRepository) ListComments(ctx context.Context, postID, parentID string, page domain.PageRequest) (domain.Page[domain.Comment], error) {
	if err := ctx.Err(); err != nil {
		return domain.Page[domain.Comment]{}, err
	}
	data, err := r.db.findPost(postID)
	if err != nil {
		return domain.Page[domain.Comment]{}, err
	}
	data.mu.RLock()
	defer data.mu.RUnlock()
	if err := r.validateParent(data, parentID); err != nil {
		return domain.Page[domain.Comment]{}, err
	}
	list := data.lists[parentID]
	start := sort.Search(len(list), func(index int) bool {
		comment := list[index]
		return isAfterCursor(comment.CreatedAt, comment.ID, page.After)
	})
	end := min(start+page.Limit, len(list))
	items := make([]domain.Comment, 0, end-start)
	items = append(items, list[start:end]...)
	return domain.Page[domain.Comment]{Items: items, HasNext: end < len(list)}, nil
}

func (r *CommentRepository) validateParent(data *postData, parentID string) error {
	if parentID == "" {
		return nil
	}
	if _, exists := data.comments[parentID]; exists {
		return nil
	}
	if _, exists := r.db.commentPosts.Load(parentID); exists {
		return domain.ErrInvalidInput
	}
	return domain.ErrNotFound
}
