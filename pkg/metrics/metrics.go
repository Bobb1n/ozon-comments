package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	registry      *prometheus.Registry
	registerer    prometheus.Registerer
	requests      *prometheus.CounterVec
	duration      *prometheus.HistogramVec
	graphqlErrors *prometheus.CounterVec
	subscriptions prometheus.Gauge
	dropped       prometheus.Counter
}

func New(enabled bool, serviceName string) *Metrics {
	if !enabled {
		return &Metrics{}
	}
	registry := prometheus.NewRegistry()
	registerer := prometheus.WrapRegistererWith(prometheus.Labels{"service": serviceName}, registry)
	m := &Metrics{
		registry: registry, registerer: registerer,
		requests:      prometheus.NewCounterVec(prometheus.CounterOpts{Name: "http_requests_total", Help: "Completed HTTP requests."}, []string{"method", "route", "status"}),
		duration:      prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "http_request_duration_seconds", Help: "HTTP request duration in seconds.", Buckets: prometheus.DefBuckets}, []string{"method", "route"}),
		graphqlErrors: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "graphql_errors_total", Help: "GraphQL errors, including errors returned with HTTP 200."}, []string{"code"}),
		subscriptions: prometheus.NewGauge(prometheus.GaugeOpts{Name: "subscriptions_active", Help: "Currently active comment subscriptions."}),
		dropped:       prometheus.NewCounter(prometheus.CounterOpts{Name: "subscriptions_dropped_total", Help: "Subscriptions disconnected because their buffer was full."}),
	}
	registerer.MustRegister(m.requests, m.duration, m.graphqlErrors, m.subscriptions, m.dropped, collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	return m
}

func (m *Metrics) Handler() http.Handler {
	if m.registry == nil {
		return nil
	}
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

func (m *Metrics) ObserveHTTP(method, route string, status int, duration time.Duration) {
	if m.registry == nil {
		return
	}
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodHead, http.MethodOptions:
	default:
		method = "OTHER"
	}
	if route == "" {
		route = "unmatched"
	}
	m.requests.WithLabelValues(method, route, strconv.Itoa(status)).Inc()
	m.duration.WithLabelValues(method, route).Observe(duration.Seconds())
}

func (m *Metrics) ObserveGraphQLError(code string) {
	if m.registry != nil {
		m.graphqlErrors.WithLabelValues(code).Inc()
	}
}

func (m *Metrics) SubscriptionOpened() {
	if m.registry != nil {
		m.subscriptions.Inc()
	}
}

func (m *Metrics) SubscriptionClosed() {
	if m.registry != nil {
		m.subscriptions.Dec()
	}
}

func (m *Metrics) SubscriptionDropped() {
	if m.registry != nil {
		m.dropped.Inc()
	}
}
