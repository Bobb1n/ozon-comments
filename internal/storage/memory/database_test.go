package memory

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ozon/internal/domain"
)

func TestBlockedPostDoesNotBlockOtherPosts(t *testing.T) {
	t.Parallel()
	db := NewDatabase()
	posts, comments := NewPostRepository(db), NewCommentRepository(db)
	for _, id := range []string{"busy", "free"} {
		require.NoError(t, posts.CreatePost(t.Context(), domain.Post{ID: id, CommentsEnabled: true}))
	}
	busy, err := db.findPost("busy")
	require.NoError(t, err)
	busy.mu.Lock()
	defer busy.mu.Unlock()
	finished := make(chan error, 1)
	go func() { finished <- comments.InsertComment(t.Context(), domain.Comment{ID: "c", PostID: "free"}) }()
	select {
	case err := <-finished:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("unrelated post was blocked")
	}
}
