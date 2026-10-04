package httpserver

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	graph "ozon/internal/transport/graphql"
)

func TestShutdownWithCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	server := &Server{
		server:          &http.Server{Addr: "127.0.0.1:0"},
		logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		shutdownTimeout: time.Second,
	}
	require.NoError(t, server.Run(ctx))
}

func TestShutdownDeadlineCancelsBlockedHandler(t *testing.T) {
	entered := make(chan struct{})
	_, cancel, finished, address, _ := startTestContainer(t, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
	}))
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		client := &http.Client{Timeout: 3 * time.Second}
		response, err := client.Get("http://" + address)
		if err == nil {
			_ = response.Body.Close()
		}
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("handler did not start")
	}
	cancel()
	require.ErrorIs(t, waitForShutdown(t, finished), context.DeadlineExceeded)
	select {
	case <-clientDone:
	case <-time.After(time.Second):
		t.Fatal("connection was not closed")
	}
}

func TestServeClosedListener(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	require.NoError(t, listener.Close())
	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	server := NewServer(Options{ShutdownTimeout: time.Second}, NewRouter(http.NotFoundHandler(), log, AuthOptions{}, MetricsOptions{}), log)
	require.ErrorIs(t, server.Serve(t.Context(), listener), net.ErrClosed)
}

func TestEchoServerServesHealthAndStops(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	server := NewServer(Options{ShutdownTimeout: time.Second}, NewRouter(http.NotFoundHandler(), log, AuthOptions{}, MetricsOptions{}), log)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- server.Serve(ctx, listener) }()
	client := &http.Client{Timeout: time.Second}
	response, err := client.Get("http://" + listener.Addr().String() + "/healthz")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.NoError(t, response.Body.Close())
	cancel()
	require.NoError(t, waitForShutdown(t, finished))
}

func TestWaitForHandlersDeadline(t *testing.T) {
	var handlers sync.WaitGroup
	handlers.Add(1)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := waitForHandlers(ctx, &handlers)
	handlers.Done()
	require.True(t, errors.Is(err, context.Canceled))
}

func TestShutdownDrainsHTTPBeforeCancelingRequests(t *testing.T) {
	entered := make(chan context.Context, 1)
	release := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	container, cancel, finished, address, closed := startTestContainer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- r.Context()
		<-release
		w.WriteHeader(http.StatusNoContent)
	}))
	response := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 3 * time.Second}
		r, err := client.Get("http://" + address)
		if err == nil {
			err = r.Body.Close()
		}
		response <- err
	}()
	var request context.Context
	select {
	case request = <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("HTTP handler did not start")
	}
	cancel()

	require.Eventually(t, func() bool { return !container.serverIsListening(address) }, time.Second, time.Millisecond)
	require.NoError(t, request.Err(), "a signal must not cancel a draining HTTP request")
	select {
	case <-closed:
		t.Fatal("storage closed before the request finished")
	default:
	}
	close(release)
	require.NoError(t, <-response)
	require.NoError(t, waitForShutdown(t, finished))
	<-closed
}

func TestShutdownClosesGraphQLWebSocket(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	options := graph.HTTPOptions{WebSocketPing: 20 * time.Second, BodyLimit: 1 << 20, ComplexityLimit: 1000}
	_, cancel, finished, address, _ := startTestContainer(t, NewRouter(graph.NewHandler(&graph.Resolver{}, log, options), log, AuthOptions{}, MetricsOptions{}))
	dialer := websocket.Dialer{Subprotocols: []string{"graphql-transport-ws"}}
	conn, response, err := dialer.Dial("ws://"+address+"/graphql", nil)
	if response != nil {
		defer response.Body.Close()
	}
	require.NoError(t, err)
	defer conn.Close()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(3*time.Second)))
	require.NoError(t, conn.WriteJSON(map[string]string{"type": "connection_init"}))
	var ack map[string]string
	require.NoError(t, conn.ReadJSON(&ack))
	require.Equal(t, "connection_ack", ack["type"])
	cancel()
	_, _, err = conn.ReadMessage()
	require.True(t, websocket.IsCloseError(err, websocket.CloseNormalClosure), "unexpected close: %v", err)
	require.NoError(t, waitForShutdown(t, finished))
}

func TestShutdownClosesUpgradedConnectionBeforeStorage(t *testing.T) {
	ended := make(chan struct{})
	upgrader := websocket.Upgrader{}
	_, cancel, finished, address, closed := startTestContainer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		<-r.Context().Done()
		_ = conn.Close()
		close(ended)
	}))
	conn, response, err := websocket.DefaultDialer.Dial("ws://"+address, nil)
	if response != nil {
		defer response.Body.Close()
	}
	require.NoError(t, err)
	defer conn.Close()
	cancel()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(3*time.Second)))
	_, _, err = conn.ReadMessage()
	require.Error(t, err)
	require.NoError(t, waitForShutdown(t, finished))
	<-closed
	select {
	case <-ended:
	default:
		t.Fatal("storage closed before the upgraded handler finished")
	}
}

func startTestContainer(t *testing.T, handler http.Handler) (*Server, context.CancelFunc, <-chan error, string, <-chan struct{}) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	closed := make(chan struct{})
	container := &Server{
		server:          &http.Server{Handler: handler, ReadHeaderTimeout: time.Second},
		logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		shutdownTimeout: time.Second,
	}
	finished := make(chan error, 1)
	go func() {
		err := container.Serve(ctx, listener)
		close(closed)
		finished <- err
	}()
	return container, cancel, finished, listener.Addr().String(), closed
}

func (c *Server) serverIsListening(address string) bool {
	conn, err := net.DialTimeout("tcp", address, 20*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func waitForShutdown(t *testing.T, finished <-chan error) error {
	t.Helper()
	select {
	case err := <-finished:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("server did not stop")
		return nil
	}
}
