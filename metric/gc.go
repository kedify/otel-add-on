package metric

import (
	"context"
	"time"

	"github.com/kedify/otel-add-on/types"
)

// RunGarbageCollector shares one idle threshold and sweep between exported query
// values and stored series. Zero disables cleanup; cancellation stops the ticker.
func RunGarbageCollector(ctx context.Context, store types.MemStore, metrics *InternalMetrics, staleAfter time.Duration) {
	if staleAfter <= 0 {
		return
	}
	ticker := time.NewTicker(min(time.Minute, staleAfter))
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			metrics.CollectStale(now, staleAfter)
			store.CollectStale(now, staleAfter)
		}
	}
}
