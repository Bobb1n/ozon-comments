package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"ozon/internal/domain"
)

type PostRepository interface {
	CreatePost(context.Context, domain.Post) error
	GetPost(context.Context, string) (domain.Post, error)
	ListPosts(context.Context, domain.PageRequest) (domain.Page[domain.Post], error)
	SetCommentsEnabled(context.Context, string, string, bool) (domain.Post, error)
}

type PostService struct {
	posts     PostRepository
	pageLimit int
}

func NewPostService(posts PostRepository, pageLimit int) *PostService {
	return &PostService{posts: posts, pageLimit: pageLimit}
}

func (s *PostService) CreatePost(ctx context.Context, userID, title, content string) (domain.Post, error) {
	if strings.TrimSpace(userID) == "" {
		return domain.Post{}, domain.ErrUnauthenticated
	}
	if strings.TrimSpace(title) == "" || strings.TrimSpace(content) == "" {
		return domain.Post{}, fmt.Errorf("%w: title and content must not be blank", domain.ErrInvalidInput)
	}

	post := domain.Post{
		ID:              uuid.NewString(),
		AuthorID:        userID,
		Title:           title,
		Content:         content,
		CommentsEnabled: true,
		CreatedAt:       time.Now().UTC().Truncate(time.Microsecond),
	}
	if err := s.posts.CreatePost(ctx, post); err != nil {
		return domain.Post{}, fmt.Errorf("create post: %w", err)
	}
	return post, nil
}

func (s *PostService) Post(ctx context.Context, id string) (domain.Post, error) {
	return s.posts.GetPost(ctx, id)
}

func (s *PostService) Posts(ctx context.Context, page domain.PageRequest) (domain.Page[domain.Post], error) {
	if err := ValidatePage(page, s.pageLimit); err != nil {
		return domain.Page[domain.Post]{}, err
	}
	return s.posts.ListPosts(ctx, page)
}

func (s *PostService) SetCommentsEnabled(ctx context.Context, userID, postID string, enabled bool) (domain.Post, error) {
	if strings.TrimSpace(userID) == "" {
		return domain.Post{}, domain.ErrUnauthenticated
	}
	return s.posts.SetCommentsEnabled(ctx, postID, userID, enabled)
}
