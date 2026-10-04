package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"
)

type Options struct {
	Address           string
	ShutdownTimeout   time.Duration
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	IdleTimeout       time.Duration
}

type Server struct {
	server          *http.Server
	logger          *slog.Logger
	shutdownTimeout time.Duration
	cancelRequests  context.CancelFunc
	handlers        sync.WaitGroup
}

func NewServer(options Options, handler http.Handler, logger *slog.Logger) *Server {
	return &Server{
		server: &http.Server{Addr: options.Address, Handler: handler,
			ReadHeaderTimeout: options.ReadHeaderTimeout, ReadTimeout: options.ReadTimeout, IdleTimeout: options.IdleTimeout},
		logger: logger, shutdownTimeout: options.ShutdownTimeout,
	}
}

func (s *Server) Run(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.server.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.server.Addr, err)
	}
	return s.Serve(ctx, listener)
}

func (s *Server) Serve(ctx context.Context, listener net.Listener) error {
	requests, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.cancelRequests = cancel
	s.server.BaseContext = func(net.Listener) context.Context { return requests }
	s.server.Handler = trackHandlers(s.server.Handler, &s.handlers)
	s.logger.InfoContext(ctx, "HTTP server listening", "address", listener.Addr().String())
	finished := make(chan error, 1)
	go s.serve(listener, finished)
	var serveErr error
	select {
	case serveErr = <-finished:
		if errors.Is(serveErr, http.ErrServerClosed) {
			serveErr = nil
		}
	case <-ctx.Done():
	}
	return errors.Join(serveErr, s.stop())
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.InfoContext(ctx, "shutting down server")
	err := s.server.Shutdown(ctx)
	if s.cancelRequests != nil {
		s.cancelRequests()
	}
	if err != nil {
		err = errors.Join(err, s.server.Close())
	}
	err = errors.Join(err, waitForHandlers(ctx, &s.handlers))
	if err != nil {
		s.logger.ErrorContext(ctx, "server shutdown failed", "error", err)
	} else {
		s.logger.Info("server stopped")
	}
	return err
}

func (s *Server) serve(listener net.Listener, finished chan<- error) {
	finished <- s.server.Serve(listener)
}

func (s *Server) stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
	defer cancel()
	return s.Shutdown(ctx)
}

func trackHandlers(handler http.Handler, handlers *sync.WaitGroup) http.Handler {
	if handler == nil {
		handler = http.DefaultServeMux
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlers.Add(1)
		defer handlers.Done()
		handler.ServeHTTP(w, r)
	})
}

func waitForHandlers(ctx context.Context, handlers *sync.WaitGroup) error {
	drained := make(chan struct{})
	go notifyWhenDrained(handlers, drained)
	select {
	case <-drained:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func notifyWhenDrained(handlers *sync.WaitGroup, drained chan<- struct{}) {
	handlers.Wait()
	close(drained)
}
