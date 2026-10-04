package memory

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ozon/internal/domain"
)

func TestAppendAndOutOfOrderComments(t *testing.T) {
	t.Parallel()
	db := NewDatabase()
	posts, comments := NewPostRepository(db), NewCommentRepository(db)
	require.NoError(t, posts.CreatePost(t.Context(), domain.Post{ID: "p", CommentsEnabled: true}))
	now := time.Now().UTC()
	for _, id := range []string{"b", "c", "a"} {
		require.NoError(t, comments.InsertComment(t.Context(), domain.Comment{ID: id, PostID: "p", CreatedAt: now}))
	}
	page, err := comments.ListComments(t.Context(), "p", "", domain.PageRequest{Limit: 3})
	require.NoError(t, err)
	require.Equal(t, []string{"a", "b", "c"}, []string{page.Items[0].ID, page.Items[1].ID, page.Items[2].ID})
}

func TestRepositoryErrorsAndCanceledContext(t *testing.T) {
	t.Parallel()
	db := NewDatabase()
	posts, comments := NewPostRepository(db), NewCommentRepository(db)
	ctx := t.Context()
	require.NoError(t, posts.CreatePost(ctx, domain.Post{ID: "p", AuthorID: "author", CommentsEnabled: true}))
	require.NoError(t, posts.CreatePost(ctx, domain.Post{ID: "other", CommentsEnabled: true}))
	require.NoError(t, comments.InsertComment(ctx, domain.Comment{ID: "root", PostID: "p"}))
	for _, test := range []struct {
		name, post, parent string
		want               error
	}{
		{"missing post", "missing", "", domain.ErrNotFound},
		{"missing parent", "p", "missing", domain.ErrNotFound},
		{"wrong post", "other", "root", domain.ErrInvalidInput},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := comments.ListComments(ctx, test.post, test.parent, domain.PageRequest{Limit: 10})
			require.ErrorIs(t, err, test.want)
			require.ErrorIs(t, comments.InsertComment(ctx, domain.Comment{ID: "invalid", PostID: test.post, ParentID: test.parent}), test.want)
		})
	}
	_, err := posts.GetPost(ctx, "missing")
	require.ErrorIs(t, err, domain.ErrNotFound)
	_, err = posts.SetCommentsEnabled(ctx, "missing", "author", false)
	require.ErrorIs(t, err, domain.ErrNotFound)
	_, err = posts.SetCommentsEnabled(ctx, "p", "stranger", false)
	require.ErrorIs(t, err, domain.ErrForbidden)
	post, err := posts.SetCommentsEnabled(ctx, "p", "author", false)
	require.NoError(t, err)
	require.False(t, post.CommentsEnabled)
	require.ErrorIs(t, comments.InsertComment(ctx, domain.Comment{ID: "blocked", PostID: "p"}), domain.ErrCommentsDisabled)
	page, err := posts.ListPosts(ctx, domain.PageRequest{Limit: 10})
	require.NoError(t, err)
	for _, post := range page.Items {
		if post.ID == "p" {
			require.False(t, post.CommentsEnabled)
		}
	}
	_, err = posts.SetCommentsEnabled(ctx, "p", "author", true)
	require.NoError(t, err)
	require.NoError(t, comments.InsertComment(ctx, domain.Comment{ID: "allowed", PostID: "p"}))

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	require.ErrorIs(t, posts.CreatePost(canceled, domain.Post{}), context.Canceled)
	_, err = posts.GetPost(canceled, "p")
	require.ErrorIs(t, err, context.Canceled)
	_, err = posts.ListPosts(canceled, domain.PageRequest{Limit: 1})
	require.ErrorIs(t, err, context.Canceled)
	_, err = posts.SetCommentsEnabled(canceled, "p", "author", true)
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, comments.InsertComment(canceled, domain.Comment{}), context.Canceled)
	_, err = comments.ListComments(canceled, "p", "", domain.PageRequest{Limit: 1})
	require.ErrorIs(t, err, context.Canceled)
}

func TestConcurrentWritesAndPermissionToggle(t *testing.T) {
	t.Parallel()
	db := NewDatabase()
	posts, comments := NewPostRepository(db), NewCommentRepository(db)
	ctx := t.Context()
	require.NoError(t, posts.CreatePost(ctx, domain.Post{ID: "p", AuthorID: "author", CommentsEnabled: true}))
	var workers sync.WaitGroup
	for i := range 100 {
		workers.Go(func() {
			err := comments.InsertComment(ctx, domain.Comment{ID: fmt.Sprint(i), PostID: "p", CreatedAt: time.Now()})
			if err != nil {
				t.Error(err)
			}
			if _, err := comments.ListComments(ctx, "p", "", domain.PageRequest{Limit: 10}); err != nil {
				t.Error(err)
			}
		})
	}
	workers.Wait()
	page, err := comments.ListComments(ctx, "p", "", domain.PageRequest{Limit: 100})
	require.NoError(t, err)
	require.Len(t, page.Items, 100)
	_, err = posts.SetCommentsEnabled(ctx, "p", "author", false)
	require.NoError(t, err)
	for i := range 20 {
		workers.Go(func() {
			if err := comments.InsertComment(ctx, domain.Comment{ID: fmt.Sprint("blocked", i), PostID: "p"}); err != domain.ErrCommentsDisabled {
				t.Errorf("got %v", err)
			}
		})
	}
	workers.Wait()
	page, err = comments.ListComments(ctx, "p", "", domain.PageRequest{Limit: 100})
	require.NoError(t, err)
	require.Len(t, page.Items, 100)
}

func TestDeepCommentTree(t *testing.T) {
	t.Parallel()
	db := NewDatabase()
	posts, comments := NewPostRepository(db), NewCommentRepository(db)
	ctx := t.Context()
	require.NoError(t, posts.CreatePost(ctx, domain.Post{ID: "p", CommentsEnabled: true}))
	parent := ""
	for i := range 2000 {
		comment := domain.Comment{ID: fmt.Sprint(i), PostID: "p", ParentID: parent}
		require.NoError(t, comments.InsertComment(ctx, comment))
		page, err := comments.ListComments(ctx, "p", parent, domain.PageRequest{Limit: 1})
		require.NoError(t, err)
		require.Equal(t, []domain.Comment{comment}, page.Items)
		require.False(t, page.HasNext)
		parent = comment.ID
	}
}

func TestEqualTimestampsAndIsolation(t *testing.T) {
	database := NewDatabase()
	posts := NewPostRepository(database)
	comments := NewCommentRepository(database)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := posts.CreatePost(ctx, domain.Post{ID: "p", CommentsEnabled: true, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"c", "a", "b"} {
		if err := comments.InsertComment(ctx, domain.Comment{ID: id, PostID: "p", CreatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := comments.ListComments(ctx, "p", "", domain.PageRequest{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if page.Items[0].ID != "a" || page.Items[1].ID != "b" || !page.HasNext {
		t.Fatal(page)
	}
	next, err := comments.ListComments(ctx, "p", "", domain.PageRequest{Limit: 2, After: &domain.PageCursor{CreatedAt: now, ID: "b"}})
	if err != nil || len(next.Items) != 1 || next.Items[0].ID != "c" || next.HasNext {
		t.Fatalf("next: %+v %v", next, err)
	}
	parent := "a"
	if err := comments.InsertComment(ctx, domain.Comment{ID: "reply", PostID: "p", ParentID: parent, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	original := "a"
	replies, err := comments.ListComments(ctx, "p", original, domain.PageRequest{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	replies.Items[0].ParentID = "modified result"
	again, err := comments.ListComments(ctx, "p", original, domain.PageRequest{Limit: 20})
	if err != nil || again.Items[0].ParentID != "a" {
		t.Fatalf("repository value leaked: %+v %v", again, err)
	}
}
