package obs

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	reqCount   *prometheus.CounterVec
	reqLatency *prometheus.HistogramVec
	errCount   *prometheus.CounterVec

	upLatency *prometheus.HistogramVec
	upErrors  *prometheus.CounterVec
}

func NewMetrics() *Metrics {
	metrics := &Metrics{
		reqCount: prometheus.NewCounterVec(
			prometheus.CounterOpts{Name: "http_requests_total"},
			[]string{"method", "path", "status"},
		),
		reqLatency: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "http_request_duration_seconds",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"method", "path", "status"},
		),
		errCount: prometheus.NewCounterVec(
			prometheus.CounterOpts{Name: "http_errors_total"},
			[]string{"method", "path", "status"},
		),
		upLatency: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "upstream_duration_seconds",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"dep", "result"},
		),
		upErrors: prometheus.NewCounterVec(
			prometheus.CounterOpts{Name: "upstream_errors_total"},
			[]string{"dep", "kind"},
		),
	}

	prometheus.MustRegister(metrics.reqCount, metrics.reqLatency, metrics.errCount, metrics.upLatency, metrics.upErrors)
	return metrics
}

func (metrics *Metrics) Handler() http.Handler {
	return promhttp.Handler()
}

func (metrics *Metrics) ObserveRequest(method, path string, status int, duration time.Duration) {
	statusStr := strconv.Itoa(status)
	metrics.reqCount.WithLabelValues(method, path, statusStr).Inc()
	metrics.reqLatency.WithLabelValues(method, path, statusStr).Observe(duration.Seconds())
	if status >= 500 {
		metrics.errCount.WithLabelValues(method, path, statusStr).Inc()
	}
}

func (metrics *Metrics) ObserveUpstream(dep string, ok bool, duration time.Duration) {
	result := "ok"
	if !ok {
		result = "err"
	}
	metrics.upLatency.WithLabelValues(dep, result).Observe(duration.Seconds())
}

func (metrics *Metrics) CountUpstreamError(dep, kind string) {
	metrics.upErrors.WithLabelValues(dep, kind).Inc()
}
