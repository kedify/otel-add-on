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
	MetadataTargetValue       = "targetValue"
	MetadataOperationOverTime = "operationOverTime"

	MetadataOperationOverTimeDefaultValue = types.OpLastOne
)

func ClampValue(lggr logr.Logger, value float64, metadata map[string]string) float64 {
	clampMin, minFound := metadata[MetadataClampMin]
	clampMax, maxFound := metadata[MetadataClampMax]
	if minFound {
		mi, e := strconv.ParseFloat(clampMin, 64)
		if e != nil {
			lggr.Info("  warning: cannot convert "+MetadataClampMin, MetadataClampMin, clampMin, "error", e)
		} else {
			value = math.Max(value, mi)
		}
	}
	if maxFound {
		ma, e := strconv.ParseFloat(clampMax, 64)
		if e != nil {
			lggr.Info("  warning: cannot convert "+MetadataClampMax, MetadataClampMax, clampMax, "error", e)
		} else {
			value = math.Min(value, ma)
		}
	}
	return value
}

func GetOperationOvertTime(lggr logr.Logger, metadata map[string]string) types.OperationOverTime {
	operationOverTime, found := metadata[MetadataOperationOverTime]
	if !found {
		return MetadataOperationOverTimeDefaultValue
	}
	if err := CheckTimeOp(types.OperationOverTime(operationOverTime)); err != nil {
		lggr.Info("  warning: cannot convert read "+MetadataOperationOverTime, MetadataOperationOverTime, operationOverTime)
		return MetadataOperationOverTimeDefaultValue
	}
	return types.OperationOverTime(operationOverTime)
}

func GetTargetValue(metadata map[string]string) (float64, error) {
	targetValueStr, found := metadata[MetadataTargetValue]
	if !found {
		return -1, fmt.Errorf("not found %s", MetadataTargetValue)
	}
	targetValue, err := strconv.ParseFloat(targetValueStr, 64)
	if err != nil {
		return -1, err
	}
	return targetValue, nil
}

func GetMetricQuery(lggr logr.Logger, metadata map[string]string, mp types.Parser) (types.MetricName, types.Labels, types.AggregationOverVectors, error) {
	metricQuery, found := metadata[MetadataMetricQuery]
	if !found {
		err := fmt.Errorf("unable to get metric query from scaled object's metadata")
		lggr.Error(err, "GetMetricQuery")
		return "", nil, "", err
	}
	name, labels, agg, err := mp.Parse(metricQuery)
	if err != nil {
		lggr.Error(err, "GetMetricQuery: cannot parse "+MetadataMetricQuery, MetadataMetricQuery, metricQuery)
		return "", nil, "", err
	}
	return name, labels, agg, nil
}
