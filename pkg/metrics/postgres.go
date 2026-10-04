package metrics

import "github.com/prometheus/client_golang/prometheus"

type PoolStats struct{ Total, Idle, Acquired, Max int32 }

func (m *Metrics) RegisterPostgresPool(stats func() PoolStats) {
	if m.registry == nil {
		return
	}
	for _, metric := range []struct {
		name, help string
		value      func(PoolStats) int32
	}{
		{"postgres_pool_connections", "Current PostgreSQL pool connections.", func(s PoolStats) int32 { return s.Total }},
		{"postgres_pool_idle_connections", "Idle PostgreSQL pool connections.", func(s PoolStats) int32 { return s.Idle }},
		{"postgres_pool_acquired_connections", "In-use PostgreSQL pool connections.", func(s PoolStats) int32 { return s.Acquired }},
		{"postgres_pool_max_connections", "Maximum PostgreSQL pool connections.", func(s PoolStats) int32 { return s.Max }},
	} {
		m.registerer.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: metric.name, Help: metric.help}, func() float64 { return float64(metric.value(stats())) }))
	}
}
