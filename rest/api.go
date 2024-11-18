package rest

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/kedify/otel-add-on/types"
)

type MetricDataPayload struct {
	Labels             types.Labels                        `json:"labels"`
	Data               []types.ObservedValue               `json:"data"`
	AggregatesOverTime map[types.OperationOverTime]float64 `json:"aggregatesOverTime"`
	LastUpdate         uint32                              `json:"lastUpdate"`
}

type api struct {
	ms   types.MemStore
	info prometheus.Labels
}

func Init(restApiPort int, info prometheus.Labels, ms types.MemStore) {
	a := api{
		ms:   ms,
		info: info,
	}
	router := gin.Default()
	router.GET("/memstore/names", a.getMetricNames)
	router.GET("/memstore/data", a.getMetricData)
	router.GET("/info", a.getInfo)
	router.Run(fmt.Sprintf(":%d", restApiPort))
}

func (a api) getMetricNames(c *gin.Context) {
	var metricNames []string
	a.ms.GetStore().Range(func(k1 string, v1 *types.Map[types.LabelsHash, *types.MetricData]) bool {
		metricNames = append(metricNames, k1)
		return true
	})

	c.IndentedJSON(http.StatusOK, metricNames)
}

func (a api) getMetricData(c *gin.Context) {
	metricData := map[string][]*MetricDataPayload{}
	a.ms.GetStore().Range(func(k1 string, v1 *types.Map[types.LabelsHash, *types.MetricData]) bool {
		var dataPoints []*MetricDataPayload
		v1.Range(func(k2 types.LabelsHash, v2 *types.MetricData) bool {
			aggregates := map[types.OperationOverTime]float64{}
			v2.AggregatesOverTime.Range(func(k3 types.OperationOverTime, v3 float64) bool {
				aggregates[k3] = v3
				return true
			})
			dataPoints = append(dataPoints, &MetricDataPayload{
				Labels:             v2.Labels,
				Data:               v2.Data,
				LastUpdate:         v2.LastUpdate,
				AggregatesOverTime: aggregates,
			})
			return true
		})
		metricData[k1] = dataPoints
		return true
	})

	c.IndentedJSON(http.StatusOK, metricData)
}

func (a api) getInfo(c *gin.Context) {
	c.IndentedJSON(http.StatusOK, a.info)
}
