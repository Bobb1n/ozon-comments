package httpserver

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"ozon/pkg/metrics"
)

func TestMetricsEndpoint(t *testing.T) {
	t.Parallel()
	for _, enabled := range []bool{true, false} {
		t.Run(map[bool]string{true: "enabled", false: "disabled"}[enabled], func(t *testing.T) {
			t.Parallel()
			observer := metrics.New(enabled, "test")
			router := NewRouter(http.NotFoundHandler(), slog.New(slog.NewJSONHandler(io.Discard, nil)), AuthOptions{}, MetricsOptions{Observer: observer, Handler: observer.Handler()})
			router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/healthz", nil))
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest("GET", "/metrics", nil))
			if !enabled {
				require.Equal(t, http.StatusNotFound, recorder.Code)
				return
			}
			require.Equal(t, http.StatusOK, recorder.Code)
			require.Contains(t, recorder.Body.String(), `http_requests_total{method="GET",route="/healthz",service="test",status="200"} 1`)
			require.NotContains(t, recorder.Body.String(), `route="/metrics"`)
		})
	}
}

func TestEchoRoutes(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		method, path string
		status       int
		contains     string
	}{
		{http.MethodGet, "/healthz", 200, "ok"},
		{http.MethodGet, "/", 200, "Posts and comments"},
		{http.MethodPost, "/graphql", 202, "graphql"},
		{http.MethodGet, "/graphql", 202, "graphql"},
		{http.MethodOptions, "/graphql", 202, "graphql"},
		{http.MethodGet, "/missing", 404, "Not Found"},
		{http.MethodPost, "/healthz", 405, "Method Not Allowed"},
	} {
		t.Run(test.method+test.path, func(t *testing.T) {
			t.Parallel()
			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(202); _, _ = w.Write([]byte("graphql")) })
			router := NewRouter(handler, slog.New(slog.NewJSONHandler(io.Discard, nil)), AuthOptions{}, MetricsOptions{})
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(test.method, test.path, nil))
			require.Equal(t, test.status, recorder.Code)
			require.Contains(t, recorder.Body.String(), test.contains)
		})
	}
}
