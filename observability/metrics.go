package observability

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	ProbesSent = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "prequal_probes_sent_total",
	})
	ProbesSucceeded = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "prequal_probes_succeeded_total",
	})
	ProbesFailed = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "prequal_probes_failed_total",
	}, []string{"reason"}) // reasons: "timeout", "non_200", "decode_error", "stale"
	ProbesDropped = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "prequal_probes_dropped_total",
	}, []string{"reason"}) // reasons: "queue_full"
	PoolOccupancy = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "prequal_pool_occupancy",
	})
	ProbeQueueDepth = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "prequal_probe_queue_depth",
	})
	SelectionAlgorithm = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "prequal_selection_algorithm_total",
	}, []string{"algorithm"}) // "prequal", "round-robin", "least-connections", "random_fallback"
)

var (
	proxyRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "prequal_proxy_requests_total",
		Help: "Total number of proxy requests",
	}, []string{"route_key", "status_code"})

	proxyRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "prequal_proxy_request_duration_seconds",
		Help:    "Duration of proxy requests in seconds",
		Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
	}, []string{"route_key"})

	proxyBackendSelectionTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "prequal_proxy_backend_selection_total",
		Help: "Total number of backend selections",
	}, []string{"route_key", "backend", "algorithm"})

	proxyNoRouteTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "prequal_proxy_no_route_total",
		Help: "Total number of requests with no matching route",
	})

	proxyNoBackendsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "prequal_proxy_no_backends_total",
		Help: "Total number of requests with no available backends",
	})

	controllerReconciliationTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "prequal_controller_reconciliation_total",
		Help: "Total number of controller reconciliations",
	}, []string{"result"})

	controllerReconciliationDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "prequal_controller_reconciliation_duration_seconds",
		Help:    "Duration of controller reconciliations in seconds",
		Buckets: prometheus.DefBuckets,
	})

	activeBackends = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "prequal_active_backends",
		Help: "Number of active backends per route",
	}, []string{"route_key"})
)

func init() {
	prometheus.MustRegister(
		ProbesSent,
		ProbesSucceeded,
		ProbesFailed,
		ProbesDropped,
		PoolOccupancy,
		ProbeQueueDepth,
		SelectionAlgorithm,
		proxyRequestsTotal,
		proxyRequestDuration,
		proxyBackendSelectionTotal,
		proxyNoRouteTotal,
		proxyNoBackendsTotal,
		controllerReconciliationTotal,
		controllerReconciliationDuration,
		activeBackends,
	)
}

func RecordRequest(routeKey, statusCode string, duration time.Duration) {
	proxyRequestsTotal.WithLabelValues(routeKey, statusCode).Inc()
	proxyRequestDuration.WithLabelValues(routeKey).Observe(duration.Seconds())
}

func RecordNoRoute() {
	proxyNoRouteTotal.Inc()
}

func RecordNoBackends() {
	proxyNoBackendsTotal.Inc()
}

func RecordReconciliation(result string, duration time.Duration) {
	controllerReconciliationTotal.WithLabelValues(result).Inc()
	controllerReconciliationDuration.Observe(duration.Seconds())
}

func RecordBackendSelection(routeKey, backend, algorithm string) {
	proxyBackendSelectionTotal.WithLabelValues(routeKey, backend, algorithm).Inc()
}

func SetActiveBackends(routeKey string, count float64) {
	activeBackends.WithLabelValues(routeKey).Set(count)
}

func StatusCode(code int) string {
	return strconv.Itoa(code)
}

func RecordProbeSent() {
	ProbesSent.Inc()
}

func RecordProbeSuccess() {
	ProbesSucceeded.Inc()
}

func RecordProbeFailed(reason string) {
	ProbesFailed.WithLabelValues(reason).Inc()
}

func RecordProbeDropped(reason string) {
	ProbesDropped.WithLabelValues(reason).Inc()
}

func RecordPoolOccupancy(size int) {
	PoolOccupancy.Set(float64(size))
}

func RecordProbeQueueDepth(size int) {
	ProbeQueueDepth.Set(float64(size))
}

func RecordSelectionAlgorithm(algo string) {
	SelectionAlgorithm.WithLabelValues(algo).Inc()
}
