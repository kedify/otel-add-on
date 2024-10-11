package scaler

import (
	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	OTLPReceiverPort            int  `envconfig:"OTLP_RECEIVER_PORT" default:"4317"`
	KedaExternalScalerPort      int  `envconfig:"KEDA_EXTERNAL_SCALER_PORT" default:"4318"`
	InternalMetricsPort         int  `envconfig:"INTERNAL_METRICS_PORT" default:"8080"`
	MetricStoreRetentionSeconds int  `envconfig:"METRIC_STORE_RETENTION_SECONDS" default:"120"`
	NoColor                     bool `envconfig:"NO_COLOR" default:"false"`
	NoBanner                    bool `envconfig:"NO_BANNER" default:"false"`
}

func MustParseConfig() *Config {
	ret := new(Config)
	envconfig.MustProcess("", ret)
	return ret
}
