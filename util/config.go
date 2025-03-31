package util

import (
	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	OTLPReceiverPort            int     `envconfig:"OTLP_RECEIVER_PORT" default:"4317"`
	KedaExternalScalerPort      int     `envconfig:"KEDA_EXTERNAL_SCALER_PORT" default:"4318"`
	RestApiPort                 int     `envconfig:"REST_API_PORT" default:"9090"`
	InternalMetricsPort         int     `envconfig:"INTERNAL_METRICS_PORT" default:"8080"`
	MetricStoreRetentionSeconds int     `envconfig:"METRIC_STORE_RETENTION_SECONDS" default:"120"`
	MetricStoreLazySeries       bool    `envconfig:"METRIC_STORE_LAZY_SERIES" default:"false"`
	MetricStoreLazyAggregates   bool    `envconfig:"METRIC_STORE_LAZY_AGGREGATES" default:"false"`
	MetricStoreErrIfNotFound    bool    `envconfig:"METRIC_STORE_ERROR_IF_NOT_FOUND" default:"false"`
	MetricStoreValueIfNotFound  float64 `envconfig:"METRIC_STORE_VALUE_IF_NOT_FOUND" default:"0."`
	NoColor                     bool    `envconfig:"NO_COLOR" default:"false"`
	NoBanner                    bool    `envconfig:"NO_BANNER" default:"false"`
}

func MustParseConfig() *Config {
	ret := new(Config)
	envconfig.MustProcess("", ret)
	return ret
}
