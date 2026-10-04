package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"ozon/internal/domain"
	"ozon/internal/service"
	"ozon/mocks"
)

func TestPostLookupAndPermissionErrors(t *testing.T) {
	t.Parallel()
	for _, want := range []error{nil, domain.ErrNotFound, domain.ErrForbidden, context.Canceled} {
		name := "success"
		if want != nil {
			name = want.Error()
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			repository := mocks.NewPostRepository(t)
			posts := service.NewPostService(repository, service.MaxPageSize)
			post := domain.Post{ID: "p", AuthorID: "author", CommentsEnabled: false}
			repository.On("GetPost", mock.Anything, "p").Return(post, want).Once()
			got, err := posts.Post(context.Background(), "p")
			require.ErrorIs(t, err, want)
			require.Equal(t, post, got)
			repository.On("SetCommentsEnabled", mock.Anything, "p", "author", false).Return(post, want).Once()
			got, err = posts.SetCommentsEnabled(context.Background(), "author", "p", false)
			require.ErrorIs(t, err, want)
			require.Equal(t, post, got)
		})
	}
}

func TestCreatePost(t *testing.T) {
	repository := mocks.NewPostRepository(t)
	posts := service.NewPostService(repository, service.MaxPageSize)
	repository.On("CreatePost", mock.Anything, mock.MatchedBy(func(post domain.Post) bool {
		return post.ID != "" && post.AuthorID == "author" && post.Title == "Title" &&
			post.Content == "Content" && post.CommentsEnabled && !post.CreatedAt.IsZero()
	})).Return(nil).Once()

	post, err := posts.CreatePost(context.Background(), "author", "Title", "Content")
	require.NoError(t, err)
	require.NotEmpty(t, post.ID)
}

func TestCreatePostRejectsInvalidInputBeforeStorage(t *testing.T) {
	for _, test := range []struct {
		name, userID, title, content string
		want                         error
	}{
		{"missing user", "", "Title", "Content", domain.ErrUnauthenticated},
		{"blank title", "author", "  ", "Content", domain.ErrInvalidInput},
		{"blank content", "author", "Title", "\n", domain.ErrInvalidInput},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := mocks.NewPostRepository(t)
			posts := service.NewPostService(repository, service.MaxPageSize)
			_, err := posts.CreatePost(context.Background(), test.userID, test.title, test.content)
			require.ErrorIs(t, err, test.want)
			repository.AssertNotCalled(t, "CreatePost", mock.Anything, mock.Anything)
		})
	}
}

func TestPostServicePreservesRepositoryError(t *testing.T) {
	repository := mocks.NewPostRepository(t)
	posts := service.NewPostService(repository, service.MaxPageSize)
	want := errors.New("database unavailable")
	repository.On("CreatePost", mock.Anything, mock.Anything).Return(want).Once()
	_, err := posts.CreatePost(context.Background(), "author", "Title", "Content")
	require.ErrorIs(t, err, want)
}

func TestPostServicePassesContextAndPage(t *testing.T) {
	repository := mocks.NewPostRepository(t)
	posts := service.NewPostService(repository, service.MaxPageSize)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	page := domain.PageRequest{Limit: 10}
	want := domain.Page[domain.Post]{Items: []domain.Post{{ID: "post-1"}}}
	repository.On("ListPosts", ctx, page).Return(want, nil).Once()
	got, err := posts.Posts(ctx, page)
	require.NoError(t, err)
	require.Equal(t, want, got)

	_, err = posts.Posts(ctx, domain.PageRequest{Limit: 101})
	require.ErrorIs(t, err, domain.ErrInvalidInput)
}

func TestSetCommentsEnabledRequiresIdentity(t *testing.T) {
	repository := mocks.NewPostRepository(t)
	posts := service.NewPostService(repository, service.MaxPageSize)
	_, err := posts.SetCommentsEnabled(context.Background(), "", "post-1", false)
	require.ErrorIs(t, err, domain.ErrUnauthenticated)
	repository.AssertNotCalled(t, "SetCommentsEnabled", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}
