package middleware

import (
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v5"
	echomiddleware "github.com/labstack/echo/v5/middleware"
)

func Logging(logger *slog.Logger) echo.MiddlewareFunc {
	return echomiddleware.RequestLoggerWithConfig(echomiddleware.RequestLoggerConfig{
		LogMethod: true, LogURIPath: true, LogStatus: true,
		LogLatency: true, LogResponseSize: true, HandleError: true,
		LogValuesFunc: func(c *echo.Context, values echomiddleware.RequestLoggerValues) error {
			level := slog.LevelInfo
			if values.Status >= http.StatusInternalServerError {
				level = slog.LevelError
			} else if values.Status >= http.StatusBadRequest {
				level = slog.LevelWarn
			}
			logger.Log(c.Request().Context(), level, "HTTP request",
				"method", values.Method, "path", values.URIPath,
				"status", values.Status, "bytes", values.ResponseSize, "duration", values.Latency)
			return nil
		},
	})
}
