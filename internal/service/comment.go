package service

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"ozon/internal/domain"
)

type CommentRepository interface {
	InsertComment(context.Context, domain.Comment) error
	ListComments(context.Context, string, string, domain.PageRequest) (domain.Page[domain.Comment], error)
}

type CommentPublisher interface {
	Publish(domain.Comment)
}

type CommentService struct {
	comments  CommentRepository
	publisher CommentPublisher
	pageLimit int
}

func NewCommentService(comments CommentRepository, publisher CommentPublisher, pageLimit int) *CommentService {
	return &CommentService{comments: comments, publisher: publisher, pageLimit: pageLimit}
}

func (s *CommentService) CreateComment(ctx context.Context, userID, postID, parentID, text string) (domain.Comment, error) {
	if strings.TrimSpace(userID) == "" {
		return domain.Comment{}, domain.ErrUnauthenticated
	}
	if !utf8.ValidString(text) || strings.TrimSpace(text) == "" || utf8.RuneCountInString(text) > domain.MaxCommentLength {
		return domain.Comment{}, fmt.Errorf("%w: comment must contain 1 to %d Unicode characters and not be blank", domain.ErrInvalidInput, domain.MaxCommentLength)
	}
	if postID == "" {
		return domain.Comment{}, domain.ErrInvalidInput
	}

	comment := domain.Comment{
		ID:        uuid.NewString(),
		PostID:    postID,
		ParentID:  parentID,
		AuthorID:  userID,
		Text:      text,
		CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
	if err := s.comments.InsertComment(ctx, comment); err != nil {
		return domain.Comment{}, fmt.Errorf("create comment: %w", err)
	}
	s.publisher.Publish(comment)
	return comment, nil
}

func (s *CommentService) Comments(ctx context.Context, postID, parentID string, page domain.PageRequest) (domain.Page[domain.Comment], error) {
	if err := ValidatePage(page, s.pageLimit); err != nil {
		return domain.Page[domain.Comment]{}, err
	}
	return s.comments.ListComments(ctx, postID, parentID, page)
}
