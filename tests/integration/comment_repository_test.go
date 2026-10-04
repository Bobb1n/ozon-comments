package integration_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"ozon/internal/domain"
	"ozon/internal/service"
	"ozon/internal/storage/memory"
	"ozon/internal/storage/postgres"
	postgresdb "ozon/pkg/postgres"
)

func TestRepositoryContract(t *testing.T) {
	t.Run("memory", func(t *testing.T) {
		database := memory.NewDatabase()
		runContract(t, memory.NewPostRepository(database), memory.NewCommentRepository(database))
		runUserContract(t, memory.NewUserRepository(database), memory.NewRefreshRepository(database))
	})
	t.Run("postgres", func(t *testing.T) {
		dsn := os.Getenv("TEST_DATABASE_URL")
		if dsn == "" {
			t.Skip("set TEST_DATABASE_URL to run PostgreSQL integration tests")
		}
		ctx := context.Background()
		conn, err := pgx.Connect(ctx, dsn)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close(ctx)
		schema := fmt.Sprintf("test_ozon_%d", time.Now().UnixNano())
		name := pgx.Identifier{schema}.Sanitize()
		if _, err = conn.Exec(ctx, "CREATE SCHEMA "+name); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if _, err := conn.Exec(ctx, "DROP SCHEMA "+name+" CASCADE"); err != nil {
				t.Error(err)
			}
		}()
		u, err := url.Parse(dsn)
		if err != nil {
			t.Fatal(err)
		}
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		runner, err := newMigrationRunner(ctx, u.String())
		if err != nil {
			t.Fatal(err)
		}
		defer runner.Close()
		if err := runner.Up(); err != nil {
			t.Fatal(err)
		}
		if err := runner.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			t.Fatalf("repeat migration: %v", err)
		}
		pool, err := postgresdb.Open(ctx, u.String())
		if err != nil {
			t.Fatal(err)
		}
		defer pool.Close()
		runContract(t, postgres.NewPostRepository(pool), postgres.NewCommentRepository(pool))
		runUserContract(t, postgres.NewUserRepository(pool), postgres.NewRefreshRepository(pool))
		verifyPostgresPagination(t, ctx, u.String())
		verifyMigrationRollback(t, ctx, u.String(), conn)
	})
}

type queryCounter struct{ calls atomic.Int64 }

func (c *queryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	c.calls.Add(1)
	return ctx
}

func (*queryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func verifyPostgresPagination(t *testing.T, ctx context.Context, dsn string) {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)
	counter := &queryCounter{}
	cfg.ConnConfig.Tracer = counter
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)
	defer pool.Close()
	posts, comments := postgres.NewPostRepository(pool), postgres.NewCommentRepository(pool)
	now := time.Now().UTC().Truncate(time.Microsecond)
	require.NoError(t, posts.CreatePost(ctx, domain.Post{ID: "pagination", AuthorID: "author", Title: "Pagination", Content: "Content", CommentsEnabled: true, CreatedAt: now}))
	for i := range 100 {
		require.NoError(t, comments.InsertComment(ctx, domain.Comment{ID: fmt.Sprintf("page_%03d", i), PostID: "pagination", AuthorID: "reader", Text: "root", CreatedAt: now}))
	}
	for _, size := range []int{1, 100} {
		counter.calls.Store(0)
		page, err := comments.ListComments(ctx, "pagination", "", domain.PageRequest{Limit: size})
		require.NoError(t, err)
		require.Len(t, page.Items, size)
		require.Equal(t, int64(2), counter.calls.Load(), "number of queries must not grow with page size")
		for _, comment := range page.Items {
			require.Empty(t, comment.ParentID)
		}
	}
	var after *domain.PageCursor
	seen := make(map[string]bool)
	for {
		page, err := comments.ListComments(ctx, "pagination", "", domain.PageRequest{Limit: 7, After: after})
		require.NoError(t, err)
		for _, comment := range page.Items {
			require.False(t, seen[comment.ID], "duplicate cursor item")
			seen[comment.ID] = true
		}
		if !page.HasNext {
			break
		}
		last := page.Items[len(page.Items)-1]
		after = &domain.PageCursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	require.Len(t, seen, 100, "equal timestamps must not lose comments")
	_, err = comments.ListComments(ctx, "missing", "", domain.PageRequest{Limit: 1})
	require.ErrorIs(t, err, domain.ErrNotFound)
	_, err = comments.ListComments(ctx, "pagination", "missing", domain.PageRequest{Limit: 1})
	require.ErrorIs(t, err, domain.ErrNotFound)
	require.NoError(t, posts.CreatePost(ctx, domain.Post{ID: "foreign", AuthorID: "author", Title: "Foreign", Content: "Content", CommentsEnabled: true, CreatedAt: now}))
	_, err = comments.ListComments(ctx, "foreign", "page_000", domain.PageRequest{Limit: 1})
	require.ErrorIs(t, err, domain.ErrInvalidInput)
	_, err = posts.GetPost(ctx, "missing")
	require.ErrorIs(t, err, domain.ErrNotFound)
	_, err = posts.SetCommentsEnabled(ctx, "missing", "author", false)
	require.ErrorIs(t, err, domain.ErrNotFound)
	var nullParents int
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM comments WHERE post_id=$1 AND parent_id IS NULL", "pagination").Scan(&nullParents))
	require.Equal(t, 100, nullParents)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	require.Error(t, posts.CreatePost(canceled, domain.Post{}))
	_, err = posts.GetPost(canceled, "pagination")
	require.Error(t, err)
	_, err = posts.ListPosts(canceled, domain.PageRequest{Limit: 1})
	require.Error(t, err)
	_, err = posts.SetCommentsEnabled(canceled, "pagination", "author", false)
	require.Error(t, err)
	require.Error(t, comments.InsertComment(canceled, domain.Comment{}))
	_, err = comments.ListComments(canceled, "pagination", "", domain.PageRequest{Limit: 1})
	require.Error(t, err)
}

func runContract(t *testing.T, postRepo service.PostRepository, commentRepo service.CommentRepository) {
	ctx := context.Background()
	hub := service.NewCommentHub(64, slog.Default(), nil)
	posts := service.NewPostService(postRepo, service.MaxPageSize)
	comments := service.NewCommentService(commentRepo, hub, service.MaxPageSize)
	post, err := posts.CreatePost(ctx, "author", "Title", "Content")
	if err != nil {
		t.Fatal(err)
	}
	other, err := posts.CreatePost(ctx, "other", "Other", "Content")
	if err != nil {
		t.Fatal(err)
	}
	if p, err := posts.Post(ctx, post.ID); err != nil || p.AuthorID != "author" || !p.CommentsEnabled {
		t.Fatalf("post: %+v, %v", p, err)
	}

	t.Run("post pagination", func(t *testing.T) {
		page, err := posts.Posts(ctx, domain.PageRequest{Limit: 1})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 1 || !page.HasNext {
			t.Fatalf("first page: %+v", page)
		}
		cursor := domain.PageCursor{CreatedAt: page.Items[0].CreatedAt, ID: page.Items[0].ID}
		second, err := posts.Posts(ctx, domain.PageRequest{Limit: 1, After: &cursor})
		if err != nil {
			t.Fatal(err)
		}
		if len(second.Items) != 1 || second.HasNext || second.Items[0].ID == page.Items[0].ID {
			t.Fatalf("second page: %+v", second)
		}
	})

	root, err := comments.CreateComment(ctx, "reader", post.ID, "", "Привет")
	if err != nil {
		t.Fatal(err)
	}
	reply, err := comments.CreateComment(ctx, "reader", post.ID, root.ID, "Ответ")
	if err != nil {
		t.Fatal(err)
	}
	t.Run("roots and replies", func(t *testing.T) {
		roots, err := comments.Comments(ctx, post.ID, "", domain.PageRequest{Limit: 20, After: nil})
		if err != nil {
			t.Fatal(err)
		}
		if len(roots.Items) != 1 || roots.Items[0].ID != root.ID {
			t.Fatalf("roots: %+v", roots)
		}
		replies, err := comments.Comments(ctx, post.ID, root.ID, domain.PageRequest{Limit: 20, After: nil})
		if err != nil {
			t.Fatal(err)
		}
		if len(replies.Items) != 1 || replies.Items[0].ID != reply.ID {
			t.Fatalf("replies: %+v", replies)
		}
	})
	t.Run("validation", func(t *testing.T) {
		missing := "missing"
		tests := []struct {
			name, user, post, text string
			parent                 string
			want                   error
		}{
			{"no identity", "", post.ID, "ok", "", domain.ErrUnauthenticated},
			{"blank", "reader", post.ID, " \n\t", "", domain.ErrInvalidInput},
			{"too long", "reader", post.ID, strings.Repeat("я", 2001), "", domain.ErrInvalidInput},
			{"invalid utf8", "reader", post.ID, string([]byte{0xff}), "", domain.ErrInvalidInput},
			{"missing post", "reader", missing, "ok", "", domain.ErrNotFound},
			{"missing parent", "reader", post.ID, "ok", missing, domain.ErrNotFound},
			{"cross post reply", "reader", other.ID, "ok", root.ID, domain.ErrInvalidInput},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, err := comments.CreateComment(ctx, tt.user, tt.post, tt.parent, tt.text)
				if !errors.Is(err, tt.want) {
					t.Fatalf("got %v, want %v", err, tt.want)
				}
			})
		}
		if _, err := comments.CreateComment(ctx, "reader", other.ID, "", strings.Repeat("я", 2000)); err != nil {
			t.Fatalf("2000 Unicode characters: %v", err)
		}
		if _, err := posts.CreatePost(ctx, "author", " ", "content"); !errors.Is(err, domain.ErrInvalidInput) {
			t.Fatalf("blank title: %v", err)
		}
		if _, err := posts.CreatePost(ctx, "", "title", "content"); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatalf("post identity: %v", err)
		}
	})
	t.Run("permissions", func(t *testing.T) {
		if _, err := posts.SetCommentsEnabled(ctx, "reader", post.ID, false); !errors.Is(err, domain.ErrForbidden) {
			t.Fatal(err)
		}
		if _, err := posts.SetCommentsEnabled(ctx, "author", post.ID, false); err != nil {
			t.Fatal(err)
		}
		if _, err := comments.CreateComment(ctx, "reader", post.ID, root.ID, "blocked"); !errors.Is(err, domain.ErrCommentsDisabled) {
			t.Fatal(err)
		}
		roots, err := comments.Comments(ctx, post.ID, "", domain.PageRequest{Limit: 20, After: nil})
		if err != nil || len(roots.Items) != 1 {
			t.Fatalf("old comments should remain readable: %v", err)
		}
		if _, err := posts.SetCommentsEnabled(ctx, "author", post.ID, true); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("comment pagination and cursor scope", func(t *testing.T) {
		for i := 0; i < 4; i++ {
			if _, err := comments.CreateComment(ctx, "reader", post.ID, "", fmt.Sprintf("root %d", i)); err != nil {
				t.Fatal(err)
			}
		}
		seen := map[string]bool{}
		var after *domain.PageCursor
		for {
			page, err := comments.Comments(ctx, post.ID, "", domain.PageRequest{Limit: 2, After: after})
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range page.Items {
				if seen[c.ID] {
					t.Fatalf("duplicate %s", c.ID)
				}
				seen[c.ID] = true
			}
			if !page.HasNext {
				break
			}
			last := page.Items[len(page.Items)-1]
			cursor := domain.PageCursor{CreatedAt: last.CreatedAt, ID: last.ID}
			after = &cursor
		}
		if len(seen) != 5 {
			t.Fatalf("got %d roots", len(seen))
		}
		if _, err := comments.Comments(ctx, post.ID, "", domain.PageRequest{Limit: 0, After: nil}); !errors.Is(err, domain.ErrInvalidInput) {
			t.Fatal(err)
		}
	})
	t.Run("concurrent writers", func(t *testing.T) {
		concurrent, err := posts.CreatePost(ctx, "author", "Concurrency", "Content")
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := comments.CreateComment(ctx, "reader", concurrent.ID, "", "ok"); err != nil {
					t.Error(err)
				}
			}()
		}
		wg.Wait()
		page, err := comments.Comments(ctx, concurrent.ID, "", domain.PageRequest{Limit: 100, After: nil})
		if err != nil || len(page.Items) != 100 || page.HasNext {
			t.Fatalf("concurrent page: %d, %v", len(page.Items), err)
		}
	})
	t.Run("deep nesting", func(t *testing.T) {
		parent := root.ID
		for i := 0; i < 300; i++ {
			c, err := comments.CreateComment(ctx, "reader", post.ID, parent, "nested")
			if err != nil {
				t.Fatal(err)
			}
			parent = c.ID
		}
		page, err := comments.Comments(ctx, post.ID, parent, domain.PageRequest{Limit: 20, After: nil})
		if err != nil || len(page.Items) != 0 {
			t.Fatalf("leaf: %+v, %v", page, err)
		}
	})
	t.Run("events after successful writes only", func(t *testing.T) {
		subctx, cancel := context.WithCancel(ctx)
		defer cancel()
		ch := hub.Subscribe(subctx, post.ID)
		c, err := comments.CreateComment(ctx, "reader", post.ID, "", "event")
		if err != nil {
			t.Fatal(err)
		}
		select {
		case got := <-ch:
			if got.ID != c.ID {
				t.Fatal("wrong event")
			}
		case <-time.After(time.Second):
			t.Fatal("event timed out")
		}
		if _, err := comments.CreateComment(ctx, "reader", post.ID, "", ""); !errors.Is(err, domain.ErrInvalidInput) {
			t.Fatal(err)
		}
		select {
		case <-ch:
			t.Fatal("invalid comment emitted event")
		default:
		}
	})
}
