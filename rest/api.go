package rest

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	swaggerfiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/kedify/otel-add-on/docs"
	"github.com/kedify/otel-add-on/metric"
	"github.com/kedify/otel-add-on/types"
	"github.com/kedify/otel-add-on/util"
)

type MetricDataPayload struct {
	Labels             types.Labels                        `json:"labels"`
	Data               []types.ObservedValue               `json:"data"`
	AggregatesOverTime map[types.OperationOverTime]float64 `json:"aggregatesOverTime"`
	LastUpdate         uint32                              `json:"lastUpdate"`
}

type QueryRequest struct {
	OperationOverTime string `json:"operationOverTime" example:"rate"`
	Query             string `json:"query" example:"avg(runtime_service_invocation_req_recv_total{app_id=nodeapp,src_app_id=pythonapp})"`
}

type OperationResult struct {
	Ok        bool    `json:"ok"`
	Operation string  `json:"operation"`
	Error     string  `json:"error"`
	Value     float64 `json:"value"`
}

type api struct {
	ms   types.MemStore
	info prometheus.Labels
	lggr logr.Logger
}

func Init(restApiPort int, info prometheus.Labels, ms types.MemStore, isDebug bool) error {
	a := api{
		ms:   ms,
		info: info,
		lggr: ctrl.Log.WithName("Gin"),
	}
	if isDebug {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}
	router := gin.New()
	if err := router.SetTrustedProxies(nil); err != nil {
		a.lggr.Error(err, "Disabling trusted proxies failed")
	}
	docs.SwaggerInfo.BasePath = "/"
	router.GET("/", func(ctx *gin.Context) {
		ctx.Redirect(http.StatusPermanentRedirect, "/swagger/index.html")
	})
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerfiles.Handler))
	router.GET("/memstore/names", a.getMetricNames)
	router.GET("/memstore/data", a.getMetricData)
	router.POST("/memstore/query", a.query)
	router.POST("/memstore/reset", a.reset)
	router.GET("/info", a.getInfo)
	a.lggr.Info(fmt.Sprintf("Swagger docs available at: http://localhost:%d", restApiPort))
	return router.Run(fmt.Sprintf(":%d", restApiPort))
}

// @BasePath /
// @Summary resets mem storage
// @Schemes http
// @Description deletes all the data in the internal metric store
// @Tags ops
// @Accept json
// @Produce json
// @Success 200 {object} OperationResult
// @Router /memstore/reset [post]
func (a api) reset(c *gin.Context) {
	metricNames := a.getMetricNamesInternal()
	for _, m := range metricNames {
		a.ms.GetStore().Delete(m)
	}

	c.IndentedJSON(http.StatusOK, OperationResult{
		Operation: "reset",
		Ok:        true,
	})
}

// @BasePath /
// @Summary queries the metric storage
// @Schemes http
// @Description evaluates provided query on top of internal metric storage
// @Tags metrics
// @Accept json
// @Param request body QueryRequest true "QueryRequest")
// @Produce json
// @Success 200 {object} OperationResult
// @Failure	500
// @Router /memstore/query [post]
func (a api) query(c *gin.Context) {
	request := &QueryRequest{}
	if err := c.ShouldBindJSON(request); err != nil {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	p := metric.NewParser()
	name, labels, aggregationOverVectors, err := p.Parse(request.Query)
	if err != nil {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	opOverTime := types.OperationOverTime(request.OperationOverTime)
	if err = util.CheckTimeOp(opOverTime); err != nil {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	value, found, err := a.ms.Get(name, labels, opOverTime, aggregationOverVectors)
	if err != nil {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !found {
		c.IndentedJSON(http.StatusOK, OperationResult{
			Operation: "query",
			Ok:        true,
			Value:     -1,
			Error:     "Metric not found",
		})
		return
	}

	c.IndentedJSON(http.StatusOK, OperationResult{
		Operation: "query",
		Ok:        true,
		Value:     value,
	})
}

// @BasePath /
// @Summary get metric names in the store
// @Schemes http
// @Description this will return the metric names of all tracked metric series in the store
// @Tags metrics
// @Accept json
// @Produce json
// @Success 200 {array} string
// @Router /memstore/names [get]
func (a api) getMetricNames(c *gin.Context) {
	c.IndentedJSON(http.StatusOK, a.getMetricNamesInternal())
}

func (a api) getMetricNamesInternal() []string {
	var metricNames []string
	a.ms.GetStore().Range(func(k1 string, v1 *types.Map[types.LabelsHash, *types.MetricData]) bool {
		metricNames = append(metricNames, k1)
		return true
	})

	return metricNames
}

// @BasePath /
// @Summary get metrics dump
// @Schemes http
// @Description this will return detailed metrics, including all the datapoints and calculated aggregates
// @Tags metrics
// @Accept json
// @Produce json
// @Success 200 {object} map[string][]MetricDataPayload
// @Router /memstore/data [get]
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

// @BasePath /
// @Summary get basic info about the app
// @Schemes http
// @Description this will return versions, ports, ...
// @Tags info
// @Accept json
// @Produce json
// @Success 200
// @Router /info [get]
func (a api) getInfo(c *gin.Context) {
	c.IndentedJSON(http.StatusOK, a.info)
}
