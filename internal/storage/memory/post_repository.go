package memory

import (
	"context"
	"sort"

	"ozon/internal/domain"
)

type PostRepository struct{ db *Database }

func NewPostRepository(db *Database) *PostRepository {
	return &PostRepository{db: db}
}

func (r *PostRepository) CreatePost(ctx context.Context, post domain.Post) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.db.mu.Lock()
	defer r.db.mu.Unlock()
	i := len(r.db.postOrder)
	if i > 0 && !isAfterCursor(post.CreatedAt, post.ID, &domain.PageCursor{CreatedAt: r.db.postOrder[i-1].CreatedAt, ID: r.db.postOrder[i-1].ID}) {
		i = sort.Search(len(r.db.postOrder), func(i int) bool {
			existing := r.db.postOrder[i]
			return existing.CreatedAt.After(post.CreatedAt) || (existing.CreatedAt.Equal(post.CreatedAt) && existing.ID >= post.ID)
		})
	}
	r.db.postOrder = append(r.db.postOrder, domain.Post{})
	copy(r.db.postOrder[i+1:], r.db.postOrder[i:])
	r.db.postOrder[i] = post
	r.db.posts[post.ID] = &postData{post: post, comments: make(map[string]domain.Comment), lists: make(map[string][]domain.Comment)}
	return nil
}

func (r *PostRepository) GetPost(ctx context.Context, id string) (domain.Post, error) {
	if err := ctx.Err(); err != nil {
		return domain.Post{}, err
	}
	data, err := r.db.findPost(id)
	if err != nil {
		return domain.Post{}, err
	}
	data.mu.RLock()
	defer data.mu.RUnlock()
	return data.post, nil
}

func (r *PostRepository) ListPosts(ctx context.Context, page domain.PageRequest) (domain.Page[domain.Post], error) {
	if err := ctx.Err(); err != nil {
		return domain.Page[domain.Post]{}, err
	}
	r.db.mu.RLock()
	start := sort.Search(len(r.db.postOrder), func(index int) bool {
		post := r.db.postOrder[index]
		return isAfterCursor(post.CreatedAt, post.ID, page.After)
	})
	end := min(start+page.Limit, len(r.db.postOrder))
	entries := make([]*postData, 0, end-start)
	for _, post := range r.db.postOrder[start:end] {
		entries = append(entries, r.db.posts[post.ID])
	}
	hasNext := end < len(r.db.postOrder)
	r.db.mu.RUnlock()
	items := make([]domain.Post, 0, len(entries))
	for _, entry := range entries {
		entry.mu.RLock()
		items = append(items, entry.post)
		entry.mu.RUnlock()
	}
	return domain.Page[domain.Post]{Items: items, HasNext: hasNext}, nil
}

func (r *PostRepository) SetCommentsEnabled(ctx context.Context, postID, user string, enabled bool) (domain.Post, error) {
	if err := ctx.Err(); err != nil {
		return domain.Post{}, err
	}
	data, err := r.db.findPost(postID)
	if err != nil {
		return domain.Post{}, err
	}
	data.mu.Lock()
	defer data.mu.Unlock()
	post := data.post
	if post.AuthorID != user {
		return domain.Post{}, domain.ErrForbidden
	}
	post.CommentsEnabled = enabled
	data.post = post
	return post, nil
}
