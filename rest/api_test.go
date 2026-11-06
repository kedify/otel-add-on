package rest

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/collector/pdata/pcommon"

	"github.com/kedify/otel-add-on/metric"
	"github.com/kedify/otel-add-on/types"
	"github.com/kedify/otel-add-on/util"
)

func TestQueryLabelMatchers(t *testing.T) {
	store := metric.NewMetricStore(&util.Config{MetricStoreRetentionSeconds: 60})
	for _, series := range []struct {
		labels types.Labels
		value  float64
	}{
		{types.Labels{"queue": "orders-us", "environment": "prod"}, 10},
		{types.Labels{"queue": "orders-eu", "environment": "staging"}, 20},
		{types.Labels{"queue": "orders-unknown"}, 5},
		{types.Labels{"queue": "orders-dev", "environment": "dev"}, 100},
		{types.Labels{"queue": "payments", "environment": "prod"}, 100},
	} {
		store.Put(types.NewMetricEntry{
			Name: "queue_depth", Labels: series.labels, MeasurementValue: series.value,
			MeasurementTime: pcommon.Timestamp(time.Now().Unix()),
		})
	}
	for _, tt := range []struct {
		query  string
		status int
		value  float64
		error  bool
	}{
		{`queue_depth{queue=~"orders-.*",environment!="dev"}`, http.StatusOK, 35, false},
		{`count(queue_depth{queue=~"orders-.*",queue!~".*-dev"})`, http.StatusOK, 3, false},
		{`queue_depth{queue=orders-us}`, http.StatusOK, 10, false},
		{"max(\nclamp_min(queue_depth / 2, 1)\n)", http.StatusOK, 50, false},
		{`ln(queue_depth{queue="orders-us"} / 10)`, http.StatusOK, 0, false},
		{"1 + 2 * 3", http.StatusOK, 7, false},
		{`scalar(sum(queue_depth{queue=~"orders-us|orders-eu"})) - queue_depth{queue="orders-us"}`, http.StatusOK, 20, false},
		{`sum(queue_depth{queue="orders-us"}) - (sum(queue_depth{queue="not-created"}) or vector(0))`, http.StatusOK, 10, false},
		{`sum(queue_depth{queue="orders-us"}) - (sum(queue_depth{queue="orders-eu"}) or vector(0))`, http.StatusOK, -10, false},
		{"missing or vector(0)", http.StatusOK, 0, false},
		{"vector(queue_depth)", http.StatusBadRequest, 0, true},
		{"queue_depth or 0", http.StatusBadRequest, 0, true},
		{"scalar(queue_depth)", http.StatusBadRequest, 0, true},
		{"scalar(missing)", http.StatusBadRequest, 0, true},
		{"scalar(1)", http.StatusBadRequest, 0, true},
		{"1 / 0", http.StatusBadRequest, 0, true},
		{`ln(-queue_depth)`, http.StatusBadRequest, 0, true},
		{"missing + 10", http.StatusOK, -1, true},
		{"sum(1)", http.StatusBadRequest, 0, true},
		{`queue_depth{queue="missing"}`, http.StatusOK, -1, true},
		{`queue_depth{queue=~"["}`, http.StatusBadRequest, 0, true},
		{`queue_depth{queue=="orders"}`, http.StatusBadRequest, 0, true},
	} {
		t.Run(tt.query, func(t *testing.T) {
			body, err := json.Marshal(QueryRequest{Query: tt.query, OperationOverTime: "last_one"})
			if err != nil {
				t.Fatal(err)
			}
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/memstore/query", bytes.NewReader(body))
			ctx.Request.Header.Set("Content-Type", "application/json")
			api{ms: store}.query(ctx)
			if recorder.Code != tt.status {
				t.Fatalf("got status %d, want %d: %s", recorder.Code, tt.status, recorder.Body)
			}
			var result OperationResult
			if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Value != tt.value || (result.Error != "") != tt.error {
				t.Errorf("unexpected response: %+v", result)
			}
			if tt.status == http.StatusOK && !result.Ok {
				t.Errorf("expected successful response: %+v", result)
			}
		})
	}
}
