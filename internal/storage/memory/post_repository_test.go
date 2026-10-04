package memory

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ozon/internal/domain"
)

func TestPostPaginationAndCopyIsolation(t *testing.T) {
	t.Parallel()
	posts := NewPostRepository(NewDatabase())
	now := time.Now().UTC()
	ctx := t.Context()
	for _, id := range []string{"c", "a", "b"} {
		require.NoError(t, posts.CreatePost(ctx, domain.Post{ID: id, CreatedAt: now, Title: id}))
	}
	page, err := posts.ListPosts(ctx, domain.PageRequest{Limit: 2})
	require.NoError(t, err)
	require.Equal(t, []string{"a", "b"}, []string{page.Items[0].ID, page.Items[1].ID})
	require.True(t, page.HasNext)
	page.Items[0].Title = "changed"
	post, err := posts.GetPost(ctx, "a")
	require.NoError(t, err)
	require.Equal(t, "a", post.Title)
	page, err = posts.ListPosts(ctx, domain.PageRequest{Limit: 2, After: &domain.PageCursor{CreatedAt: now, ID: "b"}})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, "c", page.Items[0].ID)
	require.False(t, page.HasNext)
	page, err = posts.ListPosts(ctx, domain.PageRequest{Limit: 2, After: &domain.PageCursor{CreatedAt: now.Add(time.Second), ID: "z"}})
	require.NoError(t, err)
	require.Empty(t, page.Items)
}
