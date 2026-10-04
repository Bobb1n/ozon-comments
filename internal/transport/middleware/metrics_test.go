package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"ozon/mocks"
)

func TestMetricsStatusAndRoute(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusOK, http.StatusBadRequest, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()
			observer := mocks.NewHTTPMetrics(t)
			observer.On("ObserveHTTP", "GET", "/posts/:id", status, mock.MatchedBy(func(duration time.Duration) bool { return duration >= 0 })).Once()
			router := echo.New()
			router.Use(Metrics(observer))
			router.GET("/posts/:id", func(c *echo.Context) error {
				if status >= 400 {
					return echo.NewHTTPError(status, "failed")
				}
				return c.NoContent(status)
			})
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest("GET", "/posts/private-id?token=secret", nil))
			require.Equal(t, status, recorder.Code)
		})
	}
}

func TestMetricsSkipsScrapesAndHandlesUnmatchedRoutes(t *testing.T) {
	t.Parallel()
	observer := mocks.NewHTTPMetrics(t)
	observer.On("ObserveHTTP", "GET", "", http.StatusNotFound, mock.Anything).Once()
	router := echo.New()
	router.Use(Metrics(observer))
	router.GET("/metrics", func(c *echo.Context) error { return c.NoContent(http.StatusOK) })
	for _, path := range []string{"/metrics", "/unknown/private-id"} {
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", path, nil))
	}
}
