package util

import (
	"fmt"
	"time"

	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	// HA settings are opt-in; zero warmup uses the metric retention window.
	HAEnabled       bool `envconfig:"HA_ENABLED" default:"false"`
	HAWarmupSeconds int  `envconfig:"HA_WARMUP_SECONDS" default:"0"`

	// Ports
	OTLPReceiverPort       int `envconfig:"OTLP_RECEIVER_PORT" default:"4317"`
	KedaExternalScalerPort int `envconfig:"KEDA_EXTERNAL_SCALER_PORT" default:"4318"`
	RestApiPort            int `envconfig:"REST_API_PORT" default:"9090"`
	InternalMetricsPort    int `envconfig:"INTERNAL_METRICS_PORT" default:"8080"`

	// Profiling
	ProfilingEnabled bool `envconfig:"PROFILING_ENABLED" default:"false"`
	ProfilingPort    int  `envconfig:"PROFILING_PORT" default:"8081"`

	// Metric Store
	MetricStoreRetentionSeconds int     `envconfig:"METRIC_STORE_RETENTION_SECONDS" default:"120"`
	MetricStoreLazySeries       bool    `envconfig:"METRIC_STORE_LAZY_SERIES" default:"false"`
	MetricStoreLazyAggregates   bool    `envconfig:"METRIC_STORE_LAZY_AGGREGATES" default:"false"`
	MetricStoreErrIfNotFound    bool    `envconfig:"METRIC_STORE_ERROR_IF_NOT_FOUND" default:"false"`
	MetricStoreValueIfNotFound  float64 `envconfig:"METRIC_STORE_VALUE_IF_NOT_FOUND" default:"0."`
	GCStaleAfterMinutes         int     `envconfig:"GC_STALE_AFTER_MINUTES" default:"10"`

	// TLS
	TLSCaFile          string        `envconfig:"OTLP_TLS_CA_FILE" default:""`
	TLSCertFile        string        `envconfig:"OTLP_TLS_CERT_FILE" default:""`
	TLSKeyFile         string        `envconfig:"OTLP_TLS_KEY_FILE" default:""`
	CertReloadInterval time.Duration `envconfig:"OTLP_CERTIFICATE_RELOAD_INTERVAL" default:"5m"`
	TLSKedaComm        bool          `envconfig:"KEDA_TLS_ENABLED" default:"false"`
	TLSKedaCertFile    string        `envconfig:"KEDA_TLS_CERT_FILE" default:""`
	TLSKedaKeyFile     string        `envconfig:"KEDA_TLS_KEY_FILE" default:""`

	// Other
	NoColor  bool `envconfig:"NO_COLOR" default:"false"`
	NoBanner bool `envconfig:"NO_BANNER" default:"false"`
}

func MustParseConfig() *Config {
	ret := new(Config)
	envconfig.MustProcess("", ret)
	if ret.HAEnabled && (ret.HAWarmupSeconds < 0 || ret.MetricStoreRetentionSeconds <= 0 || ret.MetricStoreLazySeries || ret.MetricStoreLazyAggregates) {
		panic("HA requires nonnegative HA_WARMUP_SECONDS, positive METRIC_STORE_RETENTION_SECONDS, and eager metric series and aggregates")
	}
	if ret.GCStaleAfterMinutes < 0 || int64(ret.GCStaleAfterMinutes) > int64((1<<63-1)/time.Minute) {
		panic(fmt.Sprintf("GC_STALE_AFTER_MINUTES must be between 0 and %d", (1<<63-1)/time.Minute))
	}
	return ret
}
