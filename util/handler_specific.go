package util

import (
	"fmt"
	"math"
	"strconv"

	"github.com/go-logr/logr"

	"github.com/kedify/otel-add-on/types"
)

const (
	MetadataClampMin          = "clampMin"
	MetadataClampMax          = "clampMax"
	MetadataMetricQuery       = "metricQuery"
	MetadataOperationOverTime = "operationOverTime"

	MetadataOperationOverTimeDefaultValue = types.OpLastOne
)

func ClampValue(lggr logr.Logger, value float64, metadata map[string]string) float64 {
	clampMin, clampMinFound := metadata[MetadataClampMin]
	clampMax, clampMaxFound := metadata[MetadataClampMax]
	if clampMinFound {
		mi, e := strconv.Atoi(clampMin)
		if e != nil {
			lggr.Info("  warning: cannot convert clampMin value: ", "value", clampMin, "error", e)
		} else {
			value = math.Max(value, float64(mi))
		}
	}
	if clampMaxFound {
		ma, e := strconv.Atoi(clampMax)
		if e != nil {
			lggr.Info("  warning: cannot convert clampMax value: ", "value", clampMax, "error", e)
		} else {
			value = math.Min(value, float64(ma))
		}
	}
	return value
}

func GetOperationOvertTime(lggr logr.Logger, metadata map[string]string) types.OperationOverTime {
	operationOverTime, operationOverTimeFound := metadata[MetadataOperationOverTime]
	if !operationOverTimeFound {
		return MetadataOperationOverTimeDefaultValue
	}
	if err := CheckTimeOp(types.OperationOverTime(operationOverTime)); err != nil {
		lggr.Info("  warning: cannot convert read operationOverTime: ", "operationOverTime", operationOverTime)
		return MetadataOperationOverTimeDefaultValue
	}
	return types.OperationOverTime(operationOverTime)
}

func GetMetricQuery(lggr logr.Logger, metadata map[string]string, mp types.Parser) (types.MetricName, types.Labels, types.AggregationOverVectors, error) {
	metricQuery, ok := metadata["metricQuery"]
	if !ok {
		err := fmt.Errorf("unable to get metric query from scaled object's metadata")
		lggr.Error(err, "GetMetrics")
		return "", nil, "", err
	}
	name, labels, agg, err := mp.Parse(metricQuery)
	if err != nil {
		lggr.Error(err, "GetMetrics")
		return "", nil, "", err
	}

	if !ok {
		err := fmt.Errorf("unable to get metric query from scaled object's metadata")
		lggr.Error(err, "GetMetrics")
		return "", nil, "", err
	}
	return name, labels, agg, nil
}
