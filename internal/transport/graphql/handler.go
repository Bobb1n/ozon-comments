package graphql

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/99designs/gqlgen/graphql"
	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/vektah/gqlparser/v2/gqlerror"

	"ozon/internal/domain"
	"ozon/internal/transport/graphql/generated"
	"ozon/internal/transport/middleware"
)

type HTTPOptions struct {
	WebSocketPing        time.Duration
	WebSocketInitTimeout time.Duration
	BodyLimit            int64
	ComplexityLimit      int
	Verifier             middleware.TokenVerifier
	Metrics              GraphQLMetrics
}

type GraphQLMetrics interface{ ObserveGraphQLError(string) }

func NewHandler(resolver *Resolver, logger *slog.Logger, options HTTPOptions) http.Handler {
	server := handler.New(generated.NewExecutableSchema(generated.Config{Resolvers: resolver, Complexity: pageComplexity(resolver.PageLimit, options.ComplexityLimit)}))
	configureTransports(server, options)
	server.Use(extension.Introspection{})
	server.Use(extension.FixedComplexityLimit(options.ComplexityLimit))
	configureErrors(server, logger, options.Metrics)
	authenticated := middleware.Authentication(server, options.Verifier)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") == "" && !readRequestBody(w, r, options.BodyLimit) {
			return
		}
		authenticated.ServeHTTP(w, r)
	})
}

func configureTransports(server *handler.Server, options HTTPOptions) {
	server.AddTransport(transport.Websocket{
		Implementation:        websocketImplementation{},
		PayloadReadLimit:      &options.BodyLimit,
		InitTimeout:           options.WebSocketInitTimeout,
		KeepAlivePingInterval: options.WebSocketPing,
		InitFunc: func(ctx context.Context, payload transport.InitPayload) (context.Context, *transport.InitPayload, error) {
			if authorization, exists := payload["Authorization"]; exists {
				value, valid := authorization.(string)
				if !valid || options.Verifier == nil {
					return ctx, nil, domain.ErrUnauthenticated
				}
				identity, err := middleware.AuthenticateBearer(ctx, value, options.Verifier)
				if err != nil {
					return ctx, nil, err
				}
				ctx = middleware.WithIdentity(ctx, identity)
			}
			return ctx, nil, nil
		},
	})
	server.AddTransport(transport.Options{})
	server.AddTransport(transport.GET{})
	server.AddTransport(transport.POST{})
}

func readRequestBody(w http.ResponseWriter, r *http.Request, limit int64) bool {
	if r.Body == nil {
		return true
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err == nil {
		r.Body = io.NopCloser(bytes.NewReader(body))
		return true
	}
	status, message := http.StatusBadRequest, "could not read request body"
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		status, message = http.StatusRequestEntityTooLarge, "request body too large"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(graphql.Response{Errors: gqlerror.List{
		{Message: message, Extensions: map[string]any{"code": "INVALID_INPUT"}},
	}})
	return false
}

func configureErrors(server *handler.Server, logger *slog.Logger, metrics GraphQLMetrics) {
	server.SetErrorPresenter(errorPresenter(logger, metrics))
	server.SetRecoverFunc(func(ctx context.Context, value any) error {
		logger.ErrorContext(ctx, "GraphQL panic", "panic", value)
		return errors.New("internal server error")
	})
}

func errorPresenter(logger *slog.Logger, metrics GraphQLMetrics) graphql.ErrorPresenterFunc {
	return func(ctx context.Context, err error) *gqlerror.Error {
		result := graphql.DefaultErrorPresenter(ctx, err)
		code := errorCode(err)
		if code == "INTERNAL_ERROR" {
			var validation *gqlerror.Error
			if errors.As(err, &validation) && len(validation.Path) == 0 && len(validation.Locations) > 0 {
				if metrics != nil {
					metrics.ObserveGraphQLError("GRAPHQL_VALIDATION")
				}
				return result
			}
			logger.ErrorContext(ctx, "GraphQL operation failed", "error", err)
			result.Message = "internal server error"
		}
		if result.Extensions == nil {
			result.Extensions = make(map[string]any)
		}
		result.Extensions["code"] = code
		if metrics != nil {
			metrics.ObserveGraphQLError(code)
		}
		return result
	}
}

func errorCode(err error) string {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return "NOT_FOUND"
	case errors.Is(err, domain.ErrForbidden):
		return "FORBIDDEN"
	case errors.Is(err, domain.ErrCommentsDisabled):
		return "COMMENTS_DISABLED"
	case errors.Is(err, domain.ErrInvalidInput):
		return "INVALID_INPUT"
	case errors.Is(err, domain.ErrUnauthenticated):
		return "UNAUTHENTICATED"
	default:
		return "INTERNAL_ERROR"
	}
}
