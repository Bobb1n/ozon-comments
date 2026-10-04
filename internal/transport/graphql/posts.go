package graphql

import (
	"context"

	"ozon/internal/transport/middleware"

	"ozon/internal/domain"
	"ozon/internal/transport/graphql/model"
)

func (r *mutationResolver) CreatePost(ctx context.Context, input model.CreatePostInput) (*domain.Post, error) {
	post, err := r.Posts.CreatePost(ctx, middleware.UserID(ctx), input.Title, input.Content)
	if err != nil {
		return nil, err
	}
	return &post, nil
}

func (r *mutationResolver) SetCommentsEnabled(ctx context.Context, postID string, enabled bool) (*domain.Post, error) {
	post, err := r.Posts.SetCommentsEnabled(ctx, middleware.UserID(ctx), postID, enabled)
	if err != nil {
		return nil, err
	}
	return &post, nil
}

func (r *queryResolver) Post(ctx context.Context, id string) (*domain.Post, error) {
	post, err := r.Resolver.Posts.Post(ctx, id)
	if err != nil {
		return nil, err
	}
	return &post, nil
}

func (r *queryResolver) Posts(ctx context.Context, first int, after *string) (*model.PostPage, error) {
	request, err := decodePage(first, after, "posts", r.PageLimit)
	if err != nil {
		return nil, err
	}
	page, err := r.Resolver.Posts.Posts(ctx, request)
	if err != nil {
		return nil, err
	}
	return postPage(page), nil
}
