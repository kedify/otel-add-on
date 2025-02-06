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
	"github.com/kedify/otel-add-on/util"
)

const (
	Prefix                            = "keda_otel_scaler_"
	KedaOtelScalerValue               = Prefix + "metric_value"
	KedaOtelScalerValueClamped        = Prefix + "metric_value_clamped"
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
	KedaOtelScalerValueClamped        *prometheus.GaugeVec
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
				Help: "KEDA OTel scaler runtime info.",
			},
			[]string{"version", "goVersion", "arch", "os", "gitSha", "retentionSeconds", "scalerPort", "otlpPort", "internalMetricsPort", "restApiPort"},
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
				Help: "Last value of the metric that was read by the scaler.",
			},
			[]string{"name", "labels", "timeOp", "aggregation"},
		)
		m.metrics.KedaOtelScalerValueClamped = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: KedaOtelScalerValueClamped,
				Help: "Last value of the metric that was read by the scaler within clampMin and clampMax bounds",
			},
			[]string{"name", "labels", "timeOp", "aggregation", "scaledObject", "namespace"},
		)
	})
}

func (m *InternalMetrics) IncMetricRead(name string) {
	m.metrics.KedaOtelScalerMetricPointsRead.With(prometheus.Labels{"name": name}).Inc()
}

func (m *InternalMetrics) IncMetricWrite(name string) {
	m.metrics.KedaOtelScalerMetricPointsWritten.With(prometheus.Labels{"name": name}).Inc()
}

func (m *InternalMetrics) SetMetricValue(name, labels, timeOp, aggregation string, value float64) {
	m.metrics.KedaOtelScalerValue.With(prometheus.Labels{"name": name, "labels": labels, "timeOp": timeOp, "aggregation": aggregation}).Set(value)
}

func (m *InternalMetrics) SetMetricValueClamped(name, labels, timeOp, aggregation, scaledObject, namespace string, value float64) {
	m.metrics.KedaOtelScalerValueClamped.With(prometheus.Labels{
		"name":         name,
		"labels":       labels,
		"timeOp":       timeOp,
		"aggregation":  aggregation,
		"scaledObject": scaledObject,
		"namespace":    namespace,
	}).Set(value)
}

func (m *InternalMetrics) SetRuntimeInfo(cfg *util.Config) prometheus.Labels {
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
	labels := prometheus.Labels{
		"version":             fallBack(build.Version(), "unknown"),
		"goVersion":           runtime.Version(),
		"arch":                runtime.GOARCH,
		"os":                  runtime.GOOS,
		"gitSha":              fallBack(firstN(build.GitCommit(), 7), "unknown"),
		"retentionSeconds":    fmt.Sprintf("%d", cfg.MetricStoreRetentionSeconds),
		"scalerPort":          fmt.Sprintf("%d", cfg.KedaExternalScalerPort),
		"otlpPort":            fmt.Sprintf("%d", cfg.OTLPReceiverPort),
		"internalMetricsPort": fmt.Sprintf("%d", cfg.InternalMetricsPort),
		"restApiPort":         fmt.Sprintf("%d", cfg.RestApiPort),
	}
	m.metrics.KedaOtelScalerRuntimeInfo.With(labels).Set(1)
	return labels
}
