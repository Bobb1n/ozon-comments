package service_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"ozon/internal/domain"
	"ozon/internal/service"
	"ozon/mocks"
)

func TestCreateCommentPublishesOnlyAfterSaving(t *testing.T) {
	repository := mocks.NewCommentRepository(t)
	publisher := mocks.NewCommentPublisher(t)
	comments := service.NewCommentService(repository, publisher, service.MaxPageSize)
	var saved domain.Comment
	repository.On("InsertComment", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) { saved = args.Get(1).(domain.Comment) }).
		Return(nil).Once()
	publisher.On("Publish", mock.MatchedBy(func(comment domain.Comment) bool {
		return saved.ID != "" && comment.ID == saved.ID && comment.Text == "Привет"
	})).Return().Once()

	comment, err := comments.CreateComment(context.Background(), "reader", "post-1", "", "Привет")
	require.NoError(t, err)
	require.Equal(t, saved, comment)
}

func TestFailedCommentDoesNotNotifySubscribers(t *testing.T) {
	repository := mocks.NewCommentRepository(t)
	publisher := mocks.NewCommentPublisher(t)
	comments := service.NewCommentService(repository, publisher, service.MaxPageSize)
	repository.On("InsertComment", mock.Anything, mock.Anything).
		Return(domain.ErrCommentsDisabled).Once()

	_, err := comments.CreateComment(context.Background(), "reader", "post-1", "", "Comment")
	require.ErrorIs(t, err, domain.ErrCommentsDisabled)
	publisher.AssertNotCalled(t, "Publish", mock.Anything)
}

func TestCommentValidation(t *testing.T) {
	for _, text := range []string{"", " \n", strings.Repeat("я", 2001), string([]byte{0xff})} {
		t.Run("invalid text", func(t *testing.T) {
			repository := mocks.NewCommentRepository(t)
			publisher := mocks.NewCommentPublisher(t)
			comments := service.NewCommentService(repository, publisher, service.MaxPageSize)
			_, err := comments.CreateComment(context.Background(), "reader", "post-1", "", text)
			require.ErrorIs(t, err, domain.ErrInvalidInput)
			repository.AssertNotCalled(t, "InsertComment", mock.Anything, mock.Anything)
			publisher.AssertNotCalled(t, "Publish", mock.Anything)
		})
	}
}

func TestCommentUnicodeLimit(t *testing.T) {
	repository := mocks.NewCommentRepository(t)
	publisher := mocks.NewCommentPublisher(t)
	comments := service.NewCommentService(repository, publisher, service.MaxPageSize)
	repository.On("InsertComment", mock.Anything, mock.Anything).Return(nil).Once()
	publisher.On("Publish", mock.Anything).Return().Once()
	_, err := comments.CreateComment(context.Background(), "reader", "post-1", "", strings.Repeat("я", 2000))
	require.NoError(t, err)
}

func TestCommentIdentityAndPostValidation(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, user, post string
		want             error
	}{
		{"missing identity", "", "p", domain.ErrUnauthenticated},
		{"blank identity", " \t", "p", domain.ErrUnauthenticated},
		{"missing post", "reader", "", domain.ErrInvalidInput},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			repository := mocks.NewCommentRepository(t)
			publisher := mocks.NewCommentPublisher(t)
			comments := service.NewCommentService(repository, publisher, service.MaxPageSize)
			_, err := comments.CreateComment(context.Background(), test.user, test.post, "", "text")
			require.ErrorIs(t, err, test.want)
			repository.AssertNotCalled(t, "InsertComment", mock.Anything, mock.Anything)
			publisher.AssertNotCalled(t, "Publish", mock.Anything)
		})
	}
}

func TestCommentListPassesContextAndErrors(t *testing.T) {
	t.Parallel()
	repository := mocks.NewCommentRepository(t)
	comments := service.NewCommentService(repository, mocks.NewCommentPublisher(t), service.MaxPageSize)
	ctx := t.Context()
	page := domain.PageRequest{Limit: 5}
	want := domain.Page[domain.Comment]{Items: []domain.Comment{{ID: "c", ParentID: "parent"}}}
	repository.On("ListComments", ctx, "p", "parent", page).Return(want, nil).Once()
	got, err := comments.Comments(ctx, "p", "parent", page)
	require.NoError(t, err)
	require.Equal(t, want, got)
	repository.On("ListComments", ctx, "missing", "", page).Return(domain.Page[domain.Comment]{}, domain.ErrNotFound).Once()
	_, err = comments.Comments(ctx, "missing", "", page)
	require.ErrorIs(t, err, domain.ErrNotFound)
	_, err = comments.Comments(ctx, "p", "", domain.PageRequest{})
	require.ErrorIs(t, err, domain.ErrInvalidInput)
}
