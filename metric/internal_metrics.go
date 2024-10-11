package metric

import (
	"fmt"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	crm "sigs.k8s.io/controller-runtime/pkg/metrics"

	"github.com/kedify/otel-add-on/build"
	"github.com/kedify/otel-add-on/scaler"
	"github.com/kedify/otel-add-on/util"
)

const (
	Prefix                            = "keda_otel_scaler_"
	KedaOtelScalerValue               = Prefix + "metric_points_value"
	KedaOtelScalerMetricPointsWritten = Prefix + "metric_points_written"
	KedaOtelScalerMetricPointsRead    = Prefix + "metric_points_read"
	KedaOtelScalerRuntimeInfo         = Prefix + "runtime_info"
)

// capital letter is a separator
var (
	regex    = regexp.MustCompile("[A-Z]")
	instance = InternalMetrics{}
)

// collectors contains list of metrics.
type collectors struct {
	KedaOtelScalerValue               *prometheus.GaugeVec
	KedaOtelScalerMetricPointsWritten *prometheus.CounterVec
	KedaOtelScalerMetricPointsRead    *prometheus.CounterVec
	KedaOtelScalerRuntimeInfo         *prometheus.GaugeVec
}

type InternalMetrics struct {
	registerOnce sync.Once
	initOnce     sync.Once
	metrics      collectors
}

func Metrics() *InternalMetrics {
	return &instance
}

func (m *InternalMetrics) Register() (err error) {
	m.registerOnce.Do(func() {
		for _, r := range m.registry() {
			if err = crm.Registry.Register(r); err != nil {
				return
			}
		}
	})
	if err != nil {
		return fmt.Errorf("can't register prometheus metrics: %s", err)
	}
	return
}

// Unregister metrics
func (m *InternalMetrics) Unregister() {
	for _, r := range m.registry() {
		crm.Registry.Unregister(r)
	}
}

func (m *InternalMetrics) registry() (r map[string]prometheus.Collector) {
	r = make(map[string]prometheus.Collector)
	val := reflect.Indirect(reflect.ValueOf(m.metrics))
	for i := 0; i < val.Type().NumField(); i++ {
		n := val.Type().Field(i).Name
		if !val.Field(i).IsNil() {
			var v = val.FieldByName(n).Interface().(prometheus.Collector)
			name := strings.ToLower(strings.Join(util.SplitAfter(n, regex), "_"))
			r[name] = v
		}
	}
	return
}

func (m *InternalMetrics) Init() {
	m.initOnce.Do(func() {
		m.metrics.KedaOtelScalerRuntimeInfo = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: KedaOtelScalerRuntimeInfo,
				Help: "KEDA OTEL scaler runtime info.",
			},
			[]string{"version", "go_version", "arch", "os", "git_sha", "retention_seconds", "scaler_port", "otlp_port"},
		)
		m.metrics.KedaOtelScalerMetricPointsRead = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: KedaOtelScalerMetricPointsRead,
				Help: "Number of successful reads from the in-memory metric store.",
			},
			[]string{"name"},
		)
		m.metrics.KedaOtelScalerMetricPointsWritten = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: KedaOtelScalerMetricPointsWritten,
				Help: "Number of successful writes to the in-memory metric store.",
			},
			[]string{"name"},
		)

		m.metrics.KedaOtelScalerValue = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: KedaOtelScalerValue,
				Help: "Gslb status count for Failover strategy.",
			},
			[]string{"name", "query", "time_op", "aggregation"},
		)
	})
}

func (m *InternalMetrics) IncMetricRead(name string) {
	m.metrics.KedaOtelScalerMetricPointsRead.With(prometheus.Labels{"name": name}).Inc()
}

func (m *InternalMetrics) IncMetricWrite(name string) {
	m.metrics.KedaOtelScalerMetricPointsWritten.With(prometheus.Labels{"name": name}).Inc()
}

func (m *InternalMetrics) SetMetricValue(name, query, timeOp, aggregation string) {
	m.metrics.KedaOtelScalerValue.With(prometheus.Labels{"name": name, "query": query, "time_op": timeOp, "aggregation": aggregation}).Inc()
}

func (m *InternalMetrics) SetRuntimeInfo(cfg *scaler.Config) {
	firstN := func(value string, n int) string {
		if len(value) < n {
			return value
		}
		return value[:n]
	}
	fallBack := func(value string, fallback string) string {
		if len(value) == 0 {
			return fallback
		}
		return value
	}
	// labels: "version", "go_version", "arch", "os", "git_sha", "retention_seconds", "scaler_port", "otlp_port"
	m.metrics.KedaOtelScalerRuntimeInfo.With(
		prometheus.Labels{
			"version":           fallBack(build.Version(), "unknown"),
			"go_version":        runtime.Version(),
			"arch":              runtime.GOARCH,
			"os":                runtime.GOOS,
			"git_sha":           fallBack(firstN(build.GitCommit(), 7), "unknown"),
			"retention_seconds": fmt.Sprintf("%d", cfg.MetricStoreRetentionSeconds),
			"scaler_port":       fmt.Sprintf("%d", cfg.KedaExternalScalerPort),
			"otlp_port":         fmt.Sprintf("%d", cfg.OTLPReceiverPort),
		}).Set(1)
}
