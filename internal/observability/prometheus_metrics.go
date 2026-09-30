package observability

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/pedroegerland/wager-service/internal/domain/wager"
)

type PrometheusMetrics struct {
	reg *prometheus.Registry

	txResults      *prometheus.CounterVec
	replays        prometheus.Counter
	keyConflicts   prometheus.Counter
	writeConflicts prometheus.Counter
	latency        *prometheus.HistogramVec
	refRetries     prometheus.Counter
	reconDiverge   prometheus.Counter
	outboxPub      prometheus.Counter
	outboxRetry    prometheus.Counter
	outboxLag      prometheus.Gauge
	inboxDup       prometheus.Counter
	consumerRetry  prometheus.Counter
	dlq            prometheus.Counter
}

func NewPrometheusMetrics() *PrometheusMetrics {
	reg := prometheus.NewRegistry()
	reg.MustRegister(prometheus.NewGoCollector(), prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
	f := func(name, help string) prometheus.Counter {
		c := prometheus.NewCounter(prometheus.CounterOpts{Name: name, Help: help})
		reg.MustRegister(c)
		return c
	}
	m := &PrometheusMetrics{
		reg: reg,
		txResults: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "wager_transactions_total", Help: "Transactions by kind and final status.",
		}, []string{"kind", "status"}),
		latency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "wager_processing_seconds", Help: "Time to process an operation end to end.",
			Buckets: prometheus.ExponentialBuckets(0.002, 2, 12),
		}, []string{"kind"}),
		outboxLag: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "outbox_lag_seconds", Help: "Age of the oldest unpublished outbox record.",
		}),
		replays:        f("wager_idempotent_replays_total", "Requests answered from a persisted result."),
		keyConflicts:   f("wager_idempotency_conflicts_total", "Idempotency key reused with a different payload."),
		writeConflicts: f("wager_concurrency_conflicts_total", "Unique/version conflicts that triggered a retry."),
		refRetries:     f("wager_reference_retries_total", "PENDING_REFERENCE re-evaluations."),
		reconDiverge:   f("wallet_reconciliation_divergences_total", "Reconciliations where stored != calculated."),
		outboxPub:      f("outbox_published_total", "Events handed to the broker."),
		outboxRetry:    f("outbox_retries_total", "Outbox publish attempts that failed and were rescheduled."),
		inboxDup:       f("inbox_duplicates_total", "Redelivered messages recognised by the inbox."),
		consumerRetry:  f("consumer_retries_total", "Messages released for redelivery after a transient failure."),
		dlq:            f("consumer_dlq_total", "Messages sent to the dead-letter queue."),
	}
	reg.MustRegister(m.txResults, m.latency, m.outboxLag)
	return m
}

func (m *PrometheusMetrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}

func (m *PrometheusMetrics) TransactionResult(k wager.Kind, s wager.Status) {
	m.txResults.WithLabelValues(string(k), string(s)).Inc()
}
func (m *PrometheusMetrics) IdempotentReplay()    { m.replays.Inc() }
func (m *PrometheusMetrics) IdempotencyConflict() { m.keyConflicts.Inc() }
func (m *PrometheusMetrics) ConcurrencyConflict() { m.writeConflicts.Inc() }
func (m *PrometheusMetrics) ProcessingLatency(k wager.Kind, d time.Duration) {
	m.latency.WithLabelValues(string(k)).Observe(d.Seconds())
}
func (m *PrometheusMetrics) ReferenceRetry()           { m.refRetries.Inc() }
func (m *PrometheusMetrics) ReconciliationDivergence() { m.reconDiverge.Inc() }
func (m *PrometheusMetrics) OutboxPublished()          { m.outboxPub.Inc() }
func (m *PrometheusMetrics) OutboxRetry()              { m.outboxRetry.Inc() }
func (m *PrometheusMetrics) OutboxLag(d time.Duration) { m.outboxLag.Set(d.Seconds()) }
func (m *PrometheusMetrics) InboxDuplicate()           { m.inboxDup.Inc() }
func (m *PrometheusMetrics) ConsumerRetry()            { m.consumerRetry.Inc() }
func (m *PrometheusMetrics) DLQ()                      { m.dlq.Inc() }
