package metrics

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestDisabledMetrics(t *testing.T) {
	t.Parallel()
	m := New(false, "test")
	require.Nil(t, m.Handler())
	m.ObserveHTTP(http.MethodGet, "/healthz", 200, time.Second)
	m.ObserveGraphQLError("INTERNAL_ERROR")
	m.SubscriptionOpened()
	m.SubscriptionClosed()
	m.SubscriptionDropped()
	m.RegisterPostgresPool(func() PoolStats { t.Fatal("disabled collector called"); return PoolStats{} })
}

func TestMetricsRegistryAndLabels(t *testing.T) {
	t.Parallel()
	m := New(true, "test")
	m.ObserveHTTP(http.MethodGet, "/posts/:id", 200, 250*time.Millisecond)
	m.ObserveHTTP("CUSTOM", "", 404, time.Millisecond)
	m.ObserveGraphQLError("NOT_FOUND")
	m.SubscriptionOpened()
	m.SubscriptionOpened()
	m.SubscriptionClosed()
	m.SubscriptionDropped()
	require.Equal(t, 1.0, testutil.ToFloat64(m.requests.WithLabelValues("GET", "/posts/:id", "200")))
	require.Equal(t, 1.0, testutil.ToFloat64(m.requests.WithLabelValues("OTHER", "unmatched", "404")))
	require.Equal(t, 1.0, testutil.ToFloat64(m.graphqlErrors.WithLabelValues("NOT_FOUND")))
	require.Equal(t, 1.0, testutil.ToFloat64(m.subscriptions))
	require.Equal(t, 1.0, testutil.ToFloat64(m.dropped))
	recorder := httptest.NewRecorder()
	m.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `http_request_duration_seconds_sum{method="GET",route="/posts/:id",service="test"} 0.25`)
	require.Contains(t, recorder.Body.String(), "go_goroutines")
	require.Contains(t, recorder.Body.String(), "process_start_time_seconds")
	other := New(true, "test")
	require.Zero(t, testutil.ToFloat64(other.subscriptions))
}

func TestConcurrentMetrics(t *testing.T) {
	t.Parallel()
	m := New(true, "test")
	var workers sync.WaitGroup
	for range 100 {
		workers.Go(func() {
			m.ObserveHTTP("POST", "/graphql", 200, time.Millisecond)
			m.ObserveGraphQLError("INVALID_INPUT")
			m.SubscriptionOpened()
			m.SubscriptionClosed()
			m.SubscriptionDropped()
		})
	}
	workers.Wait()
	require.Equal(t, 100.0, testutil.ToFloat64(m.requests.WithLabelValues("POST", "/graphql", "200")))
	require.Equal(t, 100.0, testutil.ToFloat64(m.graphqlErrors.WithLabelValues("INVALID_INPUT")))
	require.Equal(t, 100.0, testutil.ToFloat64(m.dropped))
	require.Zero(t, testutil.ToFloat64(m.subscriptions))
}
