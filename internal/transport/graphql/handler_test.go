package graphql_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"ozon/internal/domain"
	"ozon/internal/service"
	"ozon/internal/storage/memory"
	graph "ozon/internal/transport/graphql"
	httpserver "ozon/internal/transport/http"
	"ozon/internal/transport/middleware"
	"ozon/mocks"
)

func TestGraphQLErrorMetrics(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ name, query, code string }{
		{"service error", `mutation {createPost(input:{title:"Title",content:"Content"}){id}}`, "UNAUTHENTICATED"},
		{"schema validation", `query {unknownField}`, "GRAPHQL_VALIDATION"},
		{"internal error", `query {post(id:"p"){id}}`, "INTERNAL_ERROR"},
		{"successful query", `query {post(id:"p"){id}}`, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			posts := mocks.NewPostUseCases(t)
			switch test.name {
			case "service error":
				posts.On("CreatePost", mock.Anything, "", "Title", "Content").Return(domain.Post{}, domain.ErrUnauthenticated).Once()
			case "internal error":
				posts.On("Post", mock.Anything, "p").Return(domain.Post{}, errors.New("private database error")).Once()
			case "successful query":
				posts.On("Post", mock.Anything, "p").Return(domain.Post{ID: "p"}, nil).Once()
			}
			observer := mocks.NewGraphQLMetrics(t)
			if test.code != "" {
				observer.On("ObserveGraphQLError", test.code).Once()
			}
			handler := graph.NewHandler(&graph.Resolver{Posts: posts, PageLimit: 100}, slog.New(slog.NewJSONHandler(io.Discard, nil)), graph.HTTPOptions{BodyLimit: 1024, ComplexityLimit: 1000, Metrics: observer})
			body, err := json.Marshal(map[string]string{"query": test.query})
			require.NoError(t, err)
			req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)
			if test.name != "schema validation" {
				require.Equal(t, http.StatusOK, recorder.Code)
			}
			require.NotContains(t, recorder.Body.String(), "private database error")
		})
	}
}

func TestForgedUserCannotCreatePost(t *testing.T) {
	t.Parallel()
	posts := mocks.NewPostUseCases(t)
	posts.On("CreatePost", mock.Anything, "", "Title", "Content").Return(domain.Post{}, domain.ErrUnauthenticated).Once()
	handler := graph.NewHandler(&graph.Resolver{Posts: posts, PageLimit: 100}, slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)), graph.HTTPOptions{BodyLimit: 1024, ComplexityLimit: 1000})
	req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(`{"query":"mutation {createPost(input:{title:\"Title\",content:\"Content\"}){id}}"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", "author")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	require.Contains(t, recorder.Body.String(), "UNAUTHENTICATED")
}

func TestGraphQLServiceErrors(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, query, code string
		prepare           func(*mocks.PostUseCases, *mocks.CommentUseCases)
	}{
		{"create post", `mutation {createPost(input:{title:"t",content:"c"}){id}}`, "INVALID_INPUT", func(p *mocks.PostUseCases, _ *mocks.CommentUseCases) {
			p.On("CreatePost", mock.Anything, "author", "t", "c").Return(domain.Post{}, domain.ErrInvalidInput).Once()
		}},
		{"get post", `{post(id:"p"){id}}`, "NOT_FOUND", func(p *mocks.PostUseCases, _ *mocks.CommentUseCases) {
			p.On("Post", mock.Anything, "p").Return(domain.Post{}, domain.ErrNotFound).Once()
		}},
		{"list posts", `{posts{items{id}}}`, "INTERNAL_ERROR", func(p *mocks.PostUseCases, _ *mocks.CommentUseCases) {
			p.On("Posts", mock.Anything, mock.Anything).Return(domain.Page[domain.Post]{}, errors.New("secret database password")).Once()
		}},
		{"toggle permission", `mutation {setCommentsEnabled(postId:"p",enabled:false){id}}`, "FORBIDDEN", func(p *mocks.PostUseCases, _ *mocks.CommentUseCases) {
			p.On("SetCommentsEnabled", mock.Anything, "author", "p", false).Return(domain.Post{}, domain.ErrForbidden).Once()
		}},
		{"create comment", `mutation {createComment(input:{postId:"p",text:"text"}){id}}`, "COMMENTS_DISABLED", func(_ *mocks.PostUseCases, c *mocks.CommentUseCases) {
			c.On("CreateComment", mock.Anything, "author", "p", "", "text").Return(domain.Comment{}, domain.ErrCommentsDisabled).Once()
		}},
		{"list comments", `{comments(postId:"p"){items{id}}}`, "NOT_FOUND", func(_ *mocks.PostUseCases, c *mocks.CommentUseCases) {
			c.On("Comments", mock.Anything, "p", "", mock.Anything).Return(domain.Page[domain.Comment]{}, domain.ErrNotFound).Once()
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			posts, comments := mocks.NewPostUseCases(t), mocks.NewCommentUseCases(t)
			test.prepare(posts, comments)
			h := testHandler(t, &graph.Resolver{Posts: posts, Comments: comments, PageLimit: 100}, 1<<20, 1000)
			result := request(t, h, "author", test.query, nil)
			errorCode(t, result, test.code)
			if test.code == "INTERNAL_ERROR" {
				require.Equal(t, "internal server error", result.Errors[0].Message)
			}
		})
	}
}

func TestGraphQLRejectsInputBeforeService(t *testing.T) {
	t.Parallel()
	for _, query := range []string{
		`mutation {createComment(input:{postId:"p",parentId:"",text:"text"}){id}}`,
		`{comments(postId:"p",parentId:""){items{id}}}`,
		`{comments(postId:"p",first:0){items{id}}}`,
		`{posts(first:101){items{id}}}`,
		`{posts(after:"broken"){items{id}}}`,
	} {
		t.Run(query, func(t *testing.T) {
			t.Parallel()
			h := testHandler(t, &graph.Resolver{Posts: mocks.NewPostUseCases(t), Comments: mocks.NewCommentUseCases(t), PageLimit: 100}, 1<<20, 1000)
			errorCode(t, request(t, h, "author", query, nil), "INVALID_INPUT")
		})
	}
}

func TestGraphQLListsDoNotLoadEveryNodeSeparately(t *testing.T) {
	t.Parallel()
	for _, size := range []int{1, 100} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			t.Parallel()
			posts, comments := mocks.NewPostUseCases(t), mocks.NewCommentUseCases(t)
			postPage := domain.Page[domain.Post]{}
			commentPage := domain.Page[domain.Comment]{}
			for i := range size {
				postPage.Items = append(postPage.Items, domain.Post{ID: fmt.Sprint(i), CreatedAt: time.Now()})
				commentPage.Items = append(commentPage.Items, domain.Comment{ID: fmt.Sprint(i), PostID: "p", CreatedAt: time.Now()})
			}
			posts.On("Posts", mock.Anything, domain.PageRequest{Limit: size}).Return(postPage, nil).Once()
			comments.On("Comments", mock.Anything, "p", "", domain.PageRequest{Limit: size}).Return(commentPage, nil).Once()
			h := testHandler(t, &graph.Resolver{Posts: posts, Comments: comments, PageLimit: 100}, 1<<20, 10000)
			result := request(t, h, "", fmt.Sprintf(`{posts(first:%d){items{id title authorId}} comments(postId:"p",first:%d){items{id text authorId parentId}}}`, size, size), nil)
			noErrors(t, result)
			posts.AssertNotCalled(t, "Post", mock.Anything, mock.Anything)
		})
	}
}

func TestGraphQLLimitsAndPanicRecovery(t *testing.T) {
	t.Parallel()
	t.Run("body limit", func(t *testing.T) {
		posts := mocks.NewPostUseCases(t)
		result := request(t, testHandler(t, &graph.Resolver{Posts: posts, PageLimit: 100}, 64, 1000), "", `{posts{items{id}}}`, map[string]any{"padding": strings.Repeat("x", 200)})
		require.NotEmpty(t, result.Errors)
		posts.AssertNotCalled(t, "Posts", mock.Anything, mock.Anything)
	})
	t.Run("complexity limit", func(t *testing.T) {
		posts := mocks.NewPostUseCases(t)
		result := request(t, testHandler(t, &graph.Resolver{Posts: posts, PageLimit: 100}, 1<<20, 1), "", `{posts{items{id title content authorId}}}`, nil)
		require.NotEmpty(t, result.Errors)
		posts.AssertNotCalled(t, "Posts", mock.Anything, mock.Anything)
	})
	t.Run("panic is sanitized", func(t *testing.T) {
		posts := mocks.NewPostUseCases(t)
		posts.On("Post", mock.Anything, "p").Run(func(mock.Arguments) { panic("secret") }).Return(domain.Post{}, nil).Once()
		result := request(t, testHandler(t, &graph.Resolver{Posts: posts, PageLimit: 100}, 1<<20, 1000), "", `{post(id:"p"){id}}`, nil)
		errorCode(t, result, "INTERNAL_ERROR")
		require.Equal(t, "internal server error", result.Errors[0].Message)
	})
	t.Run("schema validation", func(t *testing.T) {
		result := request(t, testHandler(t, &graph.Resolver{}, 1<<20, 1000), "", `{unknownField}`, nil)
		require.NotEmpty(t, result.Errors)
		require.Contains(t, result.Errors[0].Message, "unknownField")
	})
}

func TestWeightedComplexityRejectsAliasesBeforeStorage(t *testing.T) {
	t.Parallel()
	posts := mocks.NewPostUseCases(t)
	h := testHandler(t, &graph.Resolver{Posts: posts, PageLimit: 100}, 1<<20, 250)
	result := request(t, h, "", `{a:posts(first:100){items{id title}} b:posts(first:100){items{id title}}}`, nil)
	require.NotEmpty(t, result.Errors)
	posts.AssertNotCalled(t, "Posts", mock.Anything, mock.Anything)
}

func TestSubscriptionRejectsMissingPost(t *testing.T) {
	t.Parallel()
	posts, subscribers := mocks.NewPostUseCases(t), mocks.NewCommentSubscriber(t)
	posts.On("Post", mock.Anything, "missing").Return(domain.Post{}, domain.ErrNotFound).Once()
	resolver := &graph.Resolver{Posts: posts, Subscriptions: subscribers}
	_, err := resolver.Subscription().CommentAdded(context.Background(), "missing")
	require.ErrorIs(t, err, domain.ErrNotFound)
	subscribers.AssertNotCalled(t, "Subscribe", mock.Anything, mock.Anything)
}

func testHandler(t *testing.T, resolver *graph.Resolver, bodyLimit int64, complexity int) http.Handler {
	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	return httpserver.NewRouter(graph.NewHandler(resolver, log, graph.HTTPOptions{BodyLimit: bodyLimit, ComplexityLimit: complexity, WebSocketPing: time.Second, Verifier: testVerifier(t)}), log, httpserver.AuthOptions{}, httpserver.MetricsOptions{})
}

func TestGraphQLGETAndOptions(t *testing.T) {
	t.Parallel()
	h := testHandler(t, &graph.Resolver{}, 1<<20, 1000)
	for _, test := range []struct{ method, path string }{
		{http.MethodGet, "/graphql?query=%7B__typename%7D"},
		{http.MethodOptions, "/graphql"},
	} {
		recorder := httptest.NewRecorder()
		h.ServeHTTP(recorder, httptest.NewRequest(test.method, test.path, nil))
		require.Equal(t, http.StatusOK, recorder.Code)
	}
}

type response struct {
	Data   map[string]json.RawMessage `json:"data"`
	Errors []struct {
		Message    string         `json:"message"`
		Extensions map[string]any `json:"extensions"`
	} `json:"errors"`
}

func request(t *testing.T, h http.Handler, user, query string, variables map[string]any) response {
	t.Helper()
	body, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if user != "" {
		req.Header.Set("Authorization", "Bearer "+user)
	}
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, req)
	var result response
	if err = json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("HTTP %d: %s", recorder.Code, recorder.Body.String())
	}
	return result
}

func noErrors(t *testing.T, r response) {
	t.Helper()
	if len(r.Errors) != 0 {
		t.Fatalf("GraphQL errors: %+v", r.Errors)
	}
}

func errorCode(t *testing.T, r response, want string) {
	t.Helper()
	if len(r.Errors) != 1 || r.Errors[0].Extensions["code"] != want {
		t.Fatalf("want %s, got %+v", want, r.Errors)
	}
}

func TestGraphQLWorkflow(t *testing.T) {
	b := service.NewCommentHub(64, slog.Default(), nil)
	database := memory.NewDatabase()
	posts := service.NewPostService(memory.NewPostRepository(database), service.MaxPageSize)
	comments := service.NewCommentService(memory.NewCommentRepository(database), b, service.MaxPageSize)
	options := graph.HTTPOptions{WebSocketPing: 20 * time.Second, BodyLimit: 1 << 20, ComplexityLimit: 1000, Verifier: testVerifier(t)}
	h := httpserver.NewRouter(graph.NewHandler(&graph.Resolver{Posts: posts, Comments: comments, Subscriptions: b, PageLimit: service.MaxPageSize}, slog.Default(), options), slog.Default(), httpserver.AuthOptions{}, httpserver.MetricsOptions{})
	create := `mutation { createPost(input:{title:"Title",content:"Content"}) { id title authorId commentsEnabled createdAt } }`
	errorCode(t, request(t, h, "", create, nil), "UNAUTHENTICATED")
	r := request(t, h, "author", create, nil)
	noErrors(t, r)
	var post struct {
		ID, Title, AuthorID string
		CommentsEnabled     bool
	}
	if err := json.Unmarshal(r.Data["createPost"], &post); err != nil {
		t.Fatal(err)
	}
	if post.ID == "" || post.AuthorID != "author" || !post.CommentsEnabled {
		t.Fatal(post)
	}
	add := `mutation($post:ID!,$parent:ID){createComment(input:{postId:$post,parentId:$parent,text:"Привет"}){id postId parentId authorId text}}`
	r = request(t, h, "reader", add, map[string]any{"post": post.ID})
	noErrors(t, r)
	var root struct {
		ID       string
		ParentID *string
	}
	if err := json.Unmarshal(r.Data["createComment"], &root); err != nil {
		t.Fatal(err)
	}
	if root.ID == "" || root.ParentID != nil {
		t.Fatal(root)
	}
	r = request(t, h, "reader", add, map[string]any{"post": post.ID, "parent": root.ID})
	noErrors(t, r)
	query := `query($post:ID!,$parent:ID!){post(id:$post){title} roots:comments(postId:$post){items{id text} endCursor hasNextPage} replies:comments(postId:$post,parentId:$parent){items{id parentId}}}`
	r = request(t, h, "", query, map[string]any{"post": post.ID, "parent": root.ID})
	noErrors(t, r)
	var roots struct {
		Items []struct{ ID string }
	}
	if err := json.Unmarshal(r.Data["roots"], &roots); err != nil || len(roots.Items) != 1 || roots.Items[0].ID != root.ID {
		t.Fatalf("roots: %s, %v", r.Data["roots"], err)
	}
	toggle := `mutation($id:ID!){setCommentsEnabled(postId:$id,enabled:false){commentsEnabled}}`
	errorCode(t, request(t, h, "reader", toggle, map[string]any{"id": post.ID}), "FORBIDDEN")
	noErrors(t, request(t, h, "author", toggle, map[string]any{"id": post.ID}))
	errorCode(t, request(t, h, "reader", add, map[string]any{"post": post.ID}), "COMMENTS_DISABLED")
	r = request(t, h, "", `query($id:ID!){post(id:$id){commentsEnabled} comments(postId:$id){items{id}}}`, map[string]any{"id": post.ID})
	noErrors(t, r)
	if strings.Contains(string(r.Data["post"]), "true") {
		t.Fatal("disabled state was not persisted")
	}
	noErrors(t, request(t, h, "author", `mutation($id:ID!){setCommentsEnabled(postId:$id,enabled:true){commentsEnabled}}`, map[string]any{"id": post.ID}))
	noErrors(t, request(t, h, "reader", add, map[string]any{"post": post.ID}))
	errorCode(t, request(t, h, "", `query { post(id:"missing") {id} }`, nil), "NOT_FOUND")
	errorCode(t, request(t, h, "", `query { posts(first:101) {items{id}} }`, nil), "INVALID_INPUT")
	r = request(t, h, "", `query { posts {items{title}} }`, nil)
	noErrors(t, r)
	if strings.Contains(string(r.Data["posts"]), "authorId") {
		t.Fatal("unrequested field was returned")
	}
}

type observedHub struct {
	*service.CommentHub
	ready chan struct{}
}

func (b *observedHub) Subscribe(ctx context.Context, postID string) <-chan domain.Comment {
	ch := b.CommentHub.Subscribe(ctx, postID)
	close(b.ready)
	return ch
}

func TestGraphQLSubscription(t *testing.T) {
	b := &observedHub{CommentHub: service.NewCommentHub(64, slog.Default(), nil), ready: make(chan struct{})}
	database := memory.NewDatabase()
	posts := service.NewPostService(memory.NewPostRepository(database), service.MaxPageSize)
	comments := service.NewCommentService(memory.NewCommentRepository(database), b, service.MaxPageSize)
	post, err := posts.CreatePost(context.Background(), "author", "Title", "Content")
	if err != nil {
		t.Fatal(err)
	}
	options := graph.HTTPOptions{WebSocketPing: 20 * time.Second, BodyLimit: 1 << 20, ComplexityLimit: 1000, Verifier: testVerifier(t)}
	server := httptest.NewServer(httpserver.NewRouter(graph.NewHandler(&graph.Resolver{Posts: posts, Comments: comments, Subscriptions: b, PageLimit: service.MaxPageSize}, slog.Default(), options), slog.Default(), httpserver.AuthOptions{}, httpserver.MetricsOptions{}))
	defer server.Close()
	dialer := websocket.Dialer{Subprotocols: []string{"graphql-transport-ws"}}
	ws, _, err := dialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/graphql", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if err = ws.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err = ws.WriteJSON(map[string]any{"type": "connection_init", "payload": map[string]any{"Authorization": "Bearer reader"}}); err != nil {
		t.Fatal(err)
	}
	var ack map[string]any
	if err = ws.ReadJSON(&ack); err != nil {
		t.Fatal(err)
	}
	if ack["type"] != "connection_ack" {
		t.Fatal(ack)
	}
	if err = ws.WriteJSON(map[string]any{"id": "1", "type": "subscribe", "payload": map[string]any{"query": `subscription($post:ID!){commentAdded(postId:$post){id text parentId}}`, "variables": map[string]any{"post": post.ID}}}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-b.ready:
	case <-time.After(5 * time.Second):
		t.Fatal("subscription not registered")
	}
	comment, err := comments.CreateComment(context.Background(), "reader", post.ID, "", "live")
	if err != nil {
		t.Fatal(err)
	}
	var event struct {
		Type    string
		Payload struct {
			Data struct{ CommentAdded struct{ ID, Text string } }
		}
	}
	if err = ws.ReadJSON(&event); err != nil {
		t.Fatal(err)
	}
	if event.Type != "next" || event.Payload.Data.CommentAdded.ID != comment.ID || event.Payload.Data.CommentAdded.Text != "live" {
		t.Fatalf("event: %+v", event)
	}
	if err = ws.WriteJSON(map[string]any{"id": "1", "type": "complete"}); err != nil {
		t.Fatal(err)
	}
}

func testVerifier(t *testing.T) middleware.TokenVerifier {
	t.Helper()
	verifier := mocks.NewTokenVerifier(t)
	for _, user := range []string{"author", "reader"} {
		verifier.On("Authenticate", mock.Anything, user).Return(service.Identity{UserID: user, ExpiresAt: time.Now().Add(time.Hour)}, nil).Maybe()
	}
	return verifier
}

func TestAuthenticatedWorkflow(t *testing.T) {
	t.Parallel()
	db := memory.NewDatabase()
	users := memory.NewUserRepository(db)
	auth, err := service.NewAuthService(users, memory.NewRefreshRepository(db), service.AuthOptions{Secret: strings.Repeat("s", 32), Issuer: "ozon-test", AccessTTL: time.Hour, RefreshTTL: 24 * time.Hour, PasswordCost: bcrypt.MinCost, MinPasswordLength: 12})
	require.NoError(t, err)
	hub := service.NewCommentHub(8, slog.Default(), nil)
	resolver := &graph.Resolver{
		Posts:         service.NewPostService(memory.NewPostRepository(db), 100),
		Comments:      service.NewCommentService(memory.NewCommentRepository(db), hub, 100),
		Subscriptions: hub, PageLimit: 100,
	}
	handler := graph.NewHandler(resolver, slog.Default(), graph.HTTPOptions{Verifier: auth, BodyLimit: 1024, ComplexityLimit: 1000, WebSocketPing: time.Second})
	router := httpserver.NewRouter(handler, slog.Default(), httpserver.AuthOptions{Login: auth, Registration: auth, Sessions: auth, BodyLimit: 1024, RequestRate: 100, RequestBurst: 20}, httpserver.MetricsOptions{})
	register := func(login string) domain.User {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(`{"login":"`+login+`","password":"test-password"}`)))
		require.Equal(t, http.StatusCreated, recorder.Code)
		require.NotContains(t, recorder.Body.String(), "passwordHash")
		var user domain.User
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &user))
		return user
	}
	authorUser := register("author")
	register("reader")
	login := func(id string) string {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"login":"`+id+`","password":"test-password"}`)))
		require.Equal(t, http.StatusOK, recorder.Code)
		var token service.AuthToken
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &token))
		return token.Token
	}
	author, reader := login("author"), login("reader")
	created := request(t, router, author, `mutation {createPost(input:{title:"Title",content:"Content"}){id authorId}}`, nil)
	noErrors(t, created)
	var post struct{ ID, AuthorID string }
	require.NoError(t, json.Unmarshal(created.Data["createPost"], &post))
	require.Equal(t, authorUser.ID, post.AuthorID)
	toggle := `mutation($id:ID!){setCommentsEnabled(postId:$id,enabled:false){commentsEnabled}}`
	errorCode(t, request(t, router, reader, toggle, map[string]any{"id": post.ID}), "FORBIDDEN")
	noErrors(t, request(t, router, author, toggle, map[string]any{"id": post.ID}))
	noErrors(t, request(t, router, "", `query {posts(first:1){items{id}}}`, nil))
}
