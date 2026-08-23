package http

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/chenluuo/smart-project/backend/internal/alert"
	"github.com/chenluuo/smart-project/backend/internal/device"
	"github.com/chenluuo/smart-project/backend/internal/plot"
	"github.com/chenluuo/smart-project/backend/internal/telemetry"
	"github.com/gin-gonic/gin"
)

type telemetryHandler struct {
	plots     plotService
	devices   deviceService
	telemetry telemetryService
}

type telemetryMetric struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

type plotMetrics struct {
	SoilMoisture *telemetryMetric `json:"soilMoisture,omitempty"`
	Temperature  *telemetryMetric `json:"temperature,omitempty"`
	Light        *telemetryMetric `json:"light,omitempty"`
}

type sourceDevice struct {
	ID      uint64        `json:"id"`
	Name    string        `json:"name"`
	Status  device.Status `json:"status"`
	Battery *int          `json:"battery"`
}

type plotTelemetry struct {
	PlotID        uint64         `json:"plotId"`
	SampleTime    *time.Time     `json:"sampleTime"`
	Metrics       plotMetrics    `json:"metrics"`
	SourceDevices []sourceDevice `json:"sourceDevices"`
}

func registerTelemetryRoutes(router *gin.Engine, auth authService, plots plotService, devices deviceService, svc telemetryService) {
	handler := telemetryHandler{plots: plots, devices: devices, telemetry: svc}
	router.GET("/api/v1/plots/:plotId/telemetry/latest", jwtAuthentication(auth), handler.latest)
}

func (h telemetryHandler) latest(c *gin.Context) {
	claims, ok := authenticatedClaims(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, 40101, "未登录或访问令牌无效")
		return
	}
	plotID, err := strconv.ParseUint(c.Param("plotId"), 10, 64)
	if err != nil || plotID == 0 {
		respondError(c, http.StatusBadRequest, 40001, "参数错误：plotId 必须为正整数")
		return
	}
	ctx := c.Request.Context()

	if _, err := h.plots.Get(ctx, claims.UserID, plotID); err != nil {
		if errors.Is(err, plot.ErrNotFound) {
			respondError(c, http.StatusNotFound, 40401, "地块不存在")
		} else {
			respondError(c, http.StatusInternalServerError, 50000, "服务器内部错误")
		}
		return
	}

	result := plotTelemetry{PlotID: plotID, Metrics: plotMetrics{}, SourceDevices: []sourceDevice{}}

	latest, err := h.telemetry.LatestByPlot(ctx, plotID)
	switch {
	case err == nil && latest != nil:
		if !latest.SampleTime.IsZero() {
			t := latest.SampleTime
			result.SampleTime = &t
		}
		if latest.SoilMoisture != nil {
			result.Metrics.SoilMoisture = &telemetryMetric{Value: latest.SoilMoisture.Value, Unit: latest.SoilMoisture.Unit}
		}
		if latest.Temperature != nil {
			result.Metrics.Temperature = &telemetryMetric{Value: latest.Temperature.Value, Unit: latest.Temperature.Unit}
		}
		if latest.Light != nil {
			result.Metrics.Light = &telemetryMetric{Value: latest.Light.Value, Unit: latest.Light.Unit}
		}
	case errors.Is(err, telemetry.ErrNotFound):
		// 遥测尚未接入，指标保持为空
	default:
		respondError(c, http.StatusInternalServerError, 50000, "服务器内部错误")
		return
	}

	plotIDFilter := plotID
	boundDevices, err := h.devices.List(ctx, claims.UserID, device.ListFilter{PlotID: &plotIDFilter})
	if err != nil {
		respondError(c, http.StatusInternalServerError, 50000, "服务器内部错误")
		return
	}
	for _, item := range boundDevices.Items {
		result.SourceDevices = append(result.SourceDevices, sourceDevice{
			ID: item.Device.ID, Name: item.Device.Name, Status: item.Device.Status, Battery: item.Device.Battery,
		})
	}

	respondSuccess(c, http.StatusOK, result)
}

type telemetryListHandler struct {
	plots     plotService
	alerts    alertService
	telemetry telemetryService
}

type plotLatestItem struct {
	PlotID       uint64     `json:"plotId"`
	PlotCode     string     `json:"plotCode"`
	SoilMoisture *float64   `json:"soilMoisture"`
	Temperature  *float64   `json:"temperature"`
	Light        *float64   `json:"light"`
	Status       string     `json:"status"`
	SampleTime   *time.Time `json:"sampleTime"`
}

func registerTelemetryListRoutes(router *gin.Engine, auth authService, plots plotService, alerts alertService, telemetry telemetryService) {
	handler := telemetryListHandler{plots: plots, alerts: alerts, telemetry: telemetry}
	router.GET("/api/v1/telemetry/latest", jwtAuthentication(auth), handler.list)
}

func (h telemetryListHandler) list(c *gin.Context) {
	claims, ok := authenticatedClaims(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, 40101, "未登录或访问令牌无效")
		return
	}
	ctx := c.Request.Context()

	allPlots, err := h.plots.List(ctx, claims.UserID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, 50000, "服务器内部错误")
		return
	}

	// 可选 plotId 过滤（farmId 已废弃，按当前用户地块范围 + 可选 plotId）
	if raw := c.Query("plotId"); raw != "" {
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || id == 0 {
			respondError(c, http.StatusBadRequest, 40001, "参数错误：plotId 必须为正整数")
			return
		}
		filtered := make([]plot.Plot, 0, len(allPlots))
		for _, p := range allPlots {
			if p.ID == id {
				filtered = append(filtered, p)
			}
		}
		allPlots = filtered
	}

	// 批量读取遥测（NullStore 返回空；接 Redis 后走 MGET）
	plotIDs := make([]uint64, 0, len(allPlots))
	for _, p := range allPlots {
		plotIDs = append(plotIDs, p.ID)
	}
	latestList, err := h.telemetry.LatestByPlots(ctx, plotIDs)
	if err != nil {
		respondError(c, http.StatusInternalServerError, 50000, "服务器内部错误")
		return
	}
	latestByPlot := make(map[uint64]telemetry.Latest, len(latestList))
	for _, l := range latestList {
		latestByPlot[l.PlotID] = l
	}

	// 活动告警地块集合，用于派生 NORMAL/ALERT
	activeStatus := alert.StatusActive
	activeAlerts, err := h.alerts.List(ctx, claims.UserID, alert.ListFilter{Status: &activeStatus, PageSize: 100})
	if err != nil {
		respondError(c, http.StatusInternalServerError, 50000, "服务器内部错误")
		return
	}
	alertPlots := make(map[uint64]bool)
	for _, item := range activeAlerts.Items {
		alertPlots[item.PlotID] = true
	}

	items := make([]plotLatestItem, 0, len(allPlots))
	for _, p := range allPlots {
		item := plotLatestItem{PlotID: p.ID, PlotCode: p.Code, Status: "NORMAL"}
		if alertPlots[p.ID] {
			item.Status = "ALERT"
		}
		if l, ok := latestByPlot[p.ID]; ok {
			if !l.SampleTime.IsZero() {
				t := l.SampleTime
				item.SampleTime = &t
			}
			if l.SoilMoisture != nil {
				item.SoilMoisture = &l.SoilMoisture.Value
			}
			if l.Temperature != nil {
				item.Temperature = &l.Temperature.Value
			}
			if l.Light != nil {
				item.Light = &l.Light.Value
			}
		}
		items = append(items, item)
	}

	respondSuccess(c, http.StatusOK, items)
}

type telemetryHistoryHandler struct {
	plots     plotService
	telemetry telemetryService
}

type historyPointView struct {
	Time time.Time `json:"time"`
	Avg  float64   `json:"avg"`
	Min  float64   `json:"min"`
	Max  float64   `json:"max"`
}

type historyView struct {
	PlotID uint64             `json:"plotId"`
	Metric string             `json:"metric"`
	Unit   string             `json:"unit"`
	Points []historyPointView `json:"points"`
}

var historyRanges = map[string]time.Duration{
	"1h":  time.Hour,
	"24h": 24 * time.Hour,
	"7d":  7 * 24 * time.Hour,
	"30d": 30 * 24 * time.Hour,
}

var historyIntervals = map[string]time.Duration{
	"5m": 5 * time.Minute,
	"1h": time.Hour,
	"1d": 24 * time.Hour,
}

func registerTelemetryHistoryRoutes(router *gin.Engine, auth authService, plots plotService, telemetry telemetryService) {
	handler := telemetryHistoryHandler{plots: plots, telemetry: telemetry}
	router.GET("/api/v1/telemetry/history", jwtAuthentication(auth), handler.history)
}

func (h telemetryHistoryHandler) history(c *gin.Context) {
	claims, ok := authenticatedClaims(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, 40101, "未登录或访问令牌无效")
		return
	}
	ctx := c.Request.Context()

	plotID, err := strconv.ParseUint(c.Query("plotId"), 10, 64)
	if err != nil || plotID == 0 {
		respondError(c, http.StatusBadRequest, 40001, "参数错误：plotId 必须为正整数")
		return
	}
	if _, err := h.plots.Get(ctx, claims.UserID, plotID); err != nil {
		if errors.Is(err, plot.ErrNotFound) {
			respondError(c, http.StatusNotFound, 40401, "地块不存在")
		} else {
			respondError(c, http.StatusInternalServerError, 50000, "服务器内部错误")
		}
		return
	}

	metric := c.Query("metric")
	unit, ok := telemetryMetricUnit(metric)
	if !ok {
		respondError(c, http.StatusBadRequest, 40001, "参数错误：metric 必须是 soilMoisture、temperature 或 light")
		return
	}

	// 时间窗口：range 或 startTime+endTime
	now := time.Now()
	var start, end time.Time
	if raw := c.Query("range"); raw != "" {
		d, ok := historyRanges[raw]
		if !ok {
			respondError(c, http.StatusBadRequest, 40001, "参数错误：range 必须是 1h/24h/7d/30d")
			return
		}
		end = now
		start = now.Add(-d)
	} else {
		startRaw, endRaw := c.Query("startTime"), c.Query("endTime")
		if startRaw == "" || endRaw == "" {
			respondError(c, http.StatusBadRequest, 40001, "参数错误：缺少时间范围（range 或 startTime+endTime）")
			return
		}
		start, err = time.Parse(time.RFC3339, startRaw)
		if err != nil {
			respondError(c, http.StatusBadRequest, 40001, "参数错误：startTime 必须是 RFC3339 格式")
			return
		}
		end, err = time.Parse(time.RFC3339, endRaw)
		if err != nil {
			respondError(c, http.StatusBadRequest, 40001, "参数错误：endTime 必须是 RFC3339 格式")
			return
		}
	}
	if !start.Before(end) {
		respondError(c, http.StatusBadRequest, 40001, "参数错误：startTime 必须早于 endTime")
		return
	}
	if end.Sub(start) > 30*24*time.Hour {
		respondError(c, http.StatusBadRequest, 40001, "参数错误：时间范围不能超过 30 天")
		return
	}

	// 聚合粒度，默认 1h
	interval := time.Hour
	if raw := c.Query("interval"); raw != "" {
		d, ok := historyIntervals[raw]
		if !ok {
			respondError(c, http.StatusBadRequest, 40001, "参数错误：interval 必须是 5m/1h/1d")
			return
		}
		interval = d
	}

	result, err := h.telemetry.History(ctx, telemetry.HistoryQuery{
		PlotID: plotID, Metric: metric, StartTime: start, EndTime: end, Interval: interval,
	})
	if err != nil {
		respondError(c, http.StatusInternalServerError, 50000, "服务器内部错误")
		return
	}

	points := make([]historyPointView, 0, len(result.Points))
	for _, p := range result.Points {
		points = append(points, historyPointView{Time: p.Time, Avg: p.Avg, Min: p.Min, Max: p.Max})
	}

	respondSuccess(c, http.StatusOK, historyView{PlotID: plotID, Metric: metric, Unit: unit, Points: points})
}

func telemetryMetricUnit(metric string) (string, bool) {
	switch metric {
	case "soilMoisture":
		return "%", true
	case "temperature":
		return "°C", true
	case "light":
		return "lx", true
	default:
		return "", false
	}
}
