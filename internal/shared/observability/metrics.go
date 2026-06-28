package observability

import (
	"github.com/prometheus/client_golang/prometheus"
)

const (
	ResultSuccess     = "success"
	ResultDedup       = "dedup"
	ResultParseError  = "parse_error"
	ResultFetchFailed = "fetch_failed"
	ResultInternal    = "internal"

	ErrorKindFetchFailed        = "fetch_failed"
	ErrorKindSelectorNotMatched = "selector_not_matched"
	ErrorKindInvalidPrice       = "invalid_price"
	ErrorKindConcurrentUpdate   = "concurrent_update"
	ErrorKindInternal           = "internal"
)

var defaultDurationBuckets = []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 20, 30}

type Metrics struct {
	CollectionDuration *prometheus.HistogramVec
	CollectionErrors   *prometheus.CounterVec
}

func New(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		CollectionDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "collection_duration_seconds",
				Help:    "Duração da coleta de uma fonte, em segundos.",
				Buckets: defaultDurationBuckets,
			},
			[]string{"store_id", "strategy", "result"},
		),
		CollectionErrors: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "collection_errors_total",
				Help: "Erros classificados ocorridos durante a coleta.",
			},
			[]string{"store_id", "kind"},
		),
	}

	if reg != nil {
		reg.MustRegister(m.CollectionDuration, m.CollectionErrors)
	}

	return m
}
