package graphql

import (
	"context"

	"ozon/internal/domain"
	"ozon/internal/transport/graphql/generated"
)

type PostUseCases interface {
	CreatePost(context.Context, string, string, string) (domain.Post, error)
	Post(context.Context, string) (domain.Post, error)
	Posts(context.Context, domain.PageRequest) (domain.Page[domain.Post], error)
	SetCommentsEnabled(context.Context, string, string, bool) (domain.Post, error)
}

type CommentUseCases interface {
	CreateComment(context.Context, string, string, string, string) (domain.Comment, error)
	Comments(context.Context, string, string, domain.PageRequest) (domain.Page[domain.Comment], error)
}

type CommentSubscriber interface {
	Subscribe(context.Context, string) <-chan domain.Comment
}

type Resolver struct {
	Posts         PostUseCases
	Comments      CommentUseCases
	Subscriptions CommentSubscriber
	PageLimit     int
}

func (r *Resolver) Mutation() generated.MutationResolver {
	return &mutationResolver{r}
}

func (r *Resolver) Query() generated.QueryResolver {
	return &queryResolver{r}
}

func (r *Resolver) Subscription() generated.SubscriptionResolver {
	return &subscriptionResolver{r}
}

func (r *Resolver) Comment() generated.CommentResolver { return &commentResolver{r} }

type mutationResolver struct{ *Resolver }
type queryResolver struct{ *Resolver }
type subscriptionResolver struct{ *Resolver }
type commentResolver struct{ *Resolver }
