package util

import (
	"os"
	"testing"
)

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
