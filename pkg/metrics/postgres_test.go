package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPostgresPoolCollector(t *testing.T) {
	t.Parallel()
	m := New(true, "test")
	stats := PoolStats{Total: 3, Idle: 2, Acquired: 1, Max: 10}
	m.RegisterPostgresPool(func() PoolStats { return stats })
	for _, expected := range []PoolStats{stats, {Total: 5, Idle: 1, Acquired: 4, Max: 20}} {
		stats = expected
		recorder := httptest.NewRecorder()
		m.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
		require.Equal(t, http.StatusOK, recorder.Code)
		families, err := m.registry.Gather()
		require.NoError(t, err)
		values := map[string]float64{}
		for _, family := range families {
			if len(family.Metric) > 0 && family.Metric[0].Gauge != nil {
				values[family.GetName()] = family.Metric[0].Gauge.GetValue()
			}
		}
		require.Equal(t, float64(expected.Total), values["postgres_pool_connections"])
		require.Equal(t, float64(expected.Idle), values["postgres_pool_idle_connections"])
		require.Equal(t, float64(expected.Acquired), values["postgres_pool_acquired_connections"])
		require.Equal(t, float64(expected.Max), values["postgres_pool_max_connections"])
	}
}
