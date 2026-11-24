package util

import (
	"os"
	"testing"
	"time"
)

func TestMetricStoreRetentionConfig(t *testing.T) {
	t.Setenv("HA_WARMUP_SECONDS", "0")
	t.Setenv("METRIC_STORE_RETENTION_SECONDS", "45")
	if got := MustParseConfig().MetricStoreRetentionSeconds; got != 45 {
		t.Fatalf("retention override = %d, want 45", got)
	}
	if err := os.Unsetenv("METRIC_STORE_RETENTION_SECONDS"); err != nil {
		t.Fatal(err)
	}
	cfg := MustParseConfig()
	if cfg.MetricStoreRetentionSeconds != 90 || cfg.HAWarmupDuration() != 90*time.Second {
		t.Fatalf("default retention = %ds, HA warmup = %v, want 90s for both",
			cfg.MetricStoreRetentionSeconds, cfg.HAWarmupDuration())
	}
}

func TestGCConfig(t *testing.T) {
	for _, tt := range []struct {
		value string
		want  int
	}{
		{"", 10},
		{"0", 0},
		{"7", 7},
	} {
		t.Run(tt.value, func(t *testing.T) {
			t.Setenv("GC_STALE_AFTER_MINUTES", tt.value)
			if tt.value == "" {
				if err := os.Unsetenv("GC_STALE_AFTER_MINUTES"); err != nil {
					t.Fatal(err)
				}
			}
			if got := MustParseConfig().GCStaleAfterMinutes; got != tt.want {
				t.Fatalf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestGCConfigRejectsInvalidTimeout(t *testing.T) {
	for _, value := range []string{"-1", "153722868", "1.5"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("GC_STALE_AFTER_MINUTES", value)
			defer func() {
				if recover() == nil {
					t.Fatal("invalid GC timeout was accepted")
				}
			}()
			MustParseConfig()
		})
	}
}
