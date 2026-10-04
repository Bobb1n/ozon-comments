package graphql

import (
	"context"

	"ozon/internal/transport/middleware"

	"ozon/internal/domain"
	"ozon/internal/transport/graphql/model"
)

func (r *mutationResolver) CreateComment(ctx context.Context, input model.CreateCommentInput) (*domain.Comment, error) {
	parentID, err := parentIDValue(input.ParentID)
	if err != nil {
		return nil, err
	}
	comment, err := r.Comments.CreateComment(ctx, middleware.UserID(ctx), input.PostID, parentID, input.Text)
	if err != nil {
		return nil, err
	}
	return &comment, nil
}

func (r *queryResolver) Comments(ctx context.Context, postID string, parentID *string, first int, after *string) (*model.CommentPage, error) {
	parent, err := parentIDValue(parentID)
	if err != nil {
		return nil, err
	}
	scope := commentScope(postID, parentID)
	request, err := decodePage(first, after, scope, r.PageLimit)
	if err != nil {
		return nil, err
	}
	page, err := r.Resolver.Comments.Comments(ctx, postID, parent, request)
	if err != nil {
		return nil, err
	}
	return commentPage(page, scope), nil
}

func (r *subscriptionResolver) CommentAdded(ctx context.Context, postID string) (<-chan *domain.Comment, error) {
	if _, err := r.Posts.Post(ctx, postID); err != nil {
		return nil, err
	}

	subscriptionContext, cancel := context.WithCancel(ctx)
	source := r.Subscriptions.Subscribe(subscriptionContext, postID)
	output := make(chan *domain.Comment)
	go forwardComments(subscriptionContext, cancel, source, output)
	return output, nil
}

func (r *commentResolver) ParentID(_ context.Context, comment *domain.Comment) (*string, error) {
	if comment.ParentID == "" {
		return nil, nil
	}
	return &comment.ParentID, nil
}

func parentIDValue(parentID *string) (string, error) {
	if parentID == nil {
		return "", nil
	}
	if *parentID == "" {
		return "", domain.ErrInvalidInput
	}
	return *parentID, nil
}

func forwardComments(ctx context.Context, cancel context.CancelFunc, source <-chan domain.Comment, output chan<- *domain.Comment) {
	defer func() { close(output); cancel() }()
	for {
		select {
		case <-ctx.Done():
			return
		case comment, open := <-source:
			if !open {
				return
			}
			select {
			case output <- &comment:
			case <-ctx.Done():
				return
			}
		}
	}
}
