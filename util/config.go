package util

import (
	"time"

	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	// Ports
	OTLPReceiverPort       int `envconfig:"OTLP_RECEIVER_PORT" default:"4317"`
	KedaExternalScalerPort int `envconfig:"KEDA_EXTERNAL_SCALER_PORT" default:"4318"`
	RestApiPort            int `envconfig:"REST_API_PORT" default:"9090"`
	InternalMetricsPort    int `envconfig:"INTERNAL_METRICS_PORT" default:"8080"`

	// Metric Store
	MetricStoreRetentionSeconds int     `envconfig:"METRIC_STORE_RETENTION_SECONDS" default:"120"`
	MetricStoreLazySeries       bool    `envconfig:"METRIC_STORE_LAZY_SERIES" default:"false"`
	MetricStoreLazyAggregates   bool    `envconfig:"METRIC_STORE_LAZY_AGGREGATES" default:"false"`
	MetricStoreErrIfNotFound    bool    `envconfig:"METRIC_STORE_ERROR_IF_NOT_FOUND" default:"false"`
	MetricStoreValueIfNotFound  float64 `envconfig:"METRIC_STORE_VALUE_IF_NOT_FOUND" default:"0."`

	// TLS
	TLSCaFile          string        `envconfig:"OTLP_TLS_CA_FILE" default:""`
	TLSCertFile        string        `envconfig:"OTLP_TLS_CERT_FILE" default:""`
	TLSKeyFile         string        `envconfig:"OTLP_TLS_KEY_FILE" default:""`
	CertReloadInterval time.Duration `envconfig:"OTLP_CERTIFICATE_RELOAD_INTERVAL" default:"5m"`

	// Other
	NoColor  bool `envconfig:"NO_COLOR" default:"false"`
	NoBanner bool `envconfig:"NO_BANNER" default:"false"`
}

func MustParseConfig() *Config {
	ret := new(Config)
	envconfig.MustProcess("", ret)
	return ret
}
