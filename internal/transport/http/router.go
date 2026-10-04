package httpserver

import (
	"log/slog"
	"net/http"

	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/labstack/echo/v5"

	"ozon/internal/transport/middleware"
)

type MetricsOptions struct {
	Observer middleware.HTTPMetrics
	Handler  http.Handler
}

func NewRouter(graphql http.Handler, logger *slog.Logger, auth AuthOptions, metrics MetricsOptions) *echo.Echo {
	router := echo.New()
	router.Logger = logger
	router.IPExtractor = echo.ExtractIPDirect()
	router.Use(middleware.Logging(logger))
	if metrics.Observer != nil {
		router.Use(middleware.Metrics(metrics.Observer))
	}
	if metrics.Handler != nil {
		router.GET("/metrics", echo.WrapHandler(metrics.Handler))
	}
	router.Any("/graphql", echo.WrapHandler(graphql))
	router.GET("/", echo.WrapHandler(playground.Handler("Posts and comments", "/graphql")))
	router.GET("/healthz", health)
	registerAuth(router, auth)
	return router
}

func health(c *echo.Context) error {
	return c.String(http.StatusOK, "ok\n")
}
