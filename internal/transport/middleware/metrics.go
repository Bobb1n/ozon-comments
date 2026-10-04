package middleware

import (
	"time"

	"github.com/labstack/echo/v5"
	echomiddleware "github.com/labstack/echo/v5/middleware"
)

type HTTPMetrics interface {
	ObserveHTTP(string, string, int, time.Duration)
}

func Metrics(observer HTTPMetrics) echo.MiddlewareFunc {
	return echomiddleware.RequestLoggerWithConfig(echomiddleware.RequestLoggerConfig{
		LogMethod: true, LogRoutePath: true, LogStatus: true, LogLatency: true, HandleError: true,
		Skipper: func(c *echo.Context) bool { return c.Path() == "/metrics" },
		LogValuesFunc: func(_ *echo.Context, v echomiddleware.RequestLoggerValues) error {
			observer.ObserveHTTP(v.Method, v.RoutePath, v.Status, v.Latency)
			return nil
		},
	})
}
