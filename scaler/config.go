package scaler

import (
	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	OTLPReceiverPort            int  `envconfig:"OTLP_RECEIVER_PORT" default:"4317"`
	NoColor                     bool `envconfig:"NO_COLOR" default:"false"`
	NoBanner                    bool `envconfig:"NO_BANNER" default:"false"`
	KedaExternalScalerPort      int  `envconfig:"KEDA_EXTERNAL_SCALER_PORT" default:"4318"`
	MetricStoreRetentionSeconds int  `envconfig:"METRIC_STORE_RETENTION_SECONDS" default:"120"`
}

func MustParseConfig() *Config {
	ret := new(Config)
	envconfig.MustProcess("", ret)
	return ret
}
