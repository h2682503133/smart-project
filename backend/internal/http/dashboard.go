package http

import (
	"net/http"
	"time"

	"github.com/chenluuo/smart-project/backend/internal/alert"
	"github.com/chenluuo/smart-project/backend/internal/device"
	"github.com/chenluuo/smart-project/backend/internal/plot"
	"github.com/chenluuo/smart-project/backend/internal/telemetry"
	"github.com/gin-gonic/gin"
)

type dashboardHandler struct {
	plots     plotService
	devices   deviceService
	alerts    alertService
	telemetry telemetryService
}

type dashboardMetric struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

type deviceOnlineStat struct {
	Online  int `json:"online"`
	Total   int `json:"total"`
	Offline int `json:"offline"`
}

type alertStat struct {
	Active         int `json:"active"`
	PendingConfirm int `json:"pendingConfirm"`
}

type dashboardPlot struct {
	ID           uint64      `json:"id"`
	Code         string      `json:"code"`
	SoilMoisture *float64    `json:"soilMoisture"`
	Temperature  *float64    `json:"temperature"`
	Light        *float64    `json:"light"`
	Status       plot.Status `json:"status"`
}

type dashboardOverview struct {
	SampleTime      *time.Time       `json:"sampleTime"`
	AvgSoilMoisture *dashboardMetric `json:"avgSoilMoisture"`
	AvgTemperature  *dashboardMetric `json:"avgTemperature"`
	AvgLight        *dashboardMetric `json:"avgLight"`
	DeviceOnline    deviceOnlineStat `json:"deviceOnline"`
	Alerts          alertStat        `json:"alerts"`
	Plots           []dashboardPlot  `json:"plots"`
}

func registerDashboardRoutes(router *gin.Engine, auth authService, plots plotService, devices deviceService, alerts alertService, telemetry telemetryService) {
	handler := dashboardHandler{plots: plots, devices: devices, alerts: alerts, telemetry: telemetry}
	router.GET("/api/v1/dashboard/overview", jwtAuthentication(auth), handler.overview)
}

func (h dashboardHandler) overview(c *gin.Context) {
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

	overview := dashboardOverview{Plots: make([]dashboardPlot, 0, len(allPlots))}
	plotIDs := make([]uint64, 0, len(allPlots))
	for _, p := range allPlots {
		plotIDs = append(plotIDs, p.ID)
	}
	latestValues, err := h.telemetry.LatestByPlots(ctx, plotIDs)
	if err != nil {
		respondError(c, http.StatusInternalServerError, 50000, "服务器内部错误")
		return
	}
	latestByPlot := make(map[uint64]telemetry.Latest, len(latestValues))
	for _, latest := range latestValues {
		latestByPlot[latest.PlotID] = latest
	}

	var soilSum, tempSum, lightSum float64
	var soilCount, tempCount, lightCount int
	for _, p := range allPlots {
		item := dashboardPlot{ID: p.ID, Code: p.Code, Status: p.Status}
		if latest, ok := latestByPlot[p.ID]; ok {
			if !latest.SampleTime.IsZero() && (overview.SampleTime == nil || latest.SampleTime.After(*overview.SampleTime)) {
				t := latest.SampleTime
				overview.SampleTime = &t
			}
			if latest.SoilMoisture != nil {
				item.SoilMoisture = &latest.SoilMoisture.Value
				soilSum += latest.SoilMoisture.Value
				soilCount++
			}
			if latest.Temperature != nil {
				item.Temperature = &latest.Temperature.Value
				tempSum += latest.Temperature.Value
				tempCount++
			}
			if latest.Light != nil {
				item.Light = &latest.Light.Value
				lightSum += latest.Light.Value
				lightCount++
			}
		}
		overview.Plots = append(overview.Plots, item)
	}
	if soilCount > 0 {
		overview.AvgSoilMoisture = &dashboardMetric{Value: soilSum / float64(soilCount), Unit: "%"}
	}
	if tempCount > 0 {
		overview.AvgTemperature = &dashboardMetric{Value: tempSum / float64(tempCount), Unit: "°C"}
	}
	if lightCount > 0 {
		overview.AvgLight = &dashboardMetric{Value: lightSum / float64(lightCount), Unit: "lx"}
	}

	// 设备在线统计
	allDevices, err := h.devices.List(ctx, claims.UserID, device.ListFilter{})
	if err != nil {
		respondError(c, http.StatusInternalServerError, 50000, "服务器内部错误")
		return
	}
	onlineStatus := device.StatusOnline
	onlineDevices, err := h.devices.List(ctx, claims.UserID, device.ListFilter{Status: &onlineStatus})
	if err != nil {
		respondError(c, http.StatusInternalServerError, 50000, "服务器内部错误")
		return
	}
	overview.DeviceOnline = deviceOnlineStat{
		Total:   int(allDevices.Total),
		Online:  int(onlineDevices.Total),
		Offline: int(allDevices.Total) - int(onlineDevices.Total),
	}

	// 告警统计
	activeStatus := alert.StatusActive
	activeAlerts, err := h.alerts.List(ctx, claims.UserID, alert.ListFilter{Status: &activeStatus})
	if err != nil {
		respondError(c, http.StatusInternalServerError, 50000, "服务器内部错误")
		return
	}
	confirmedStatus := alert.StatusConfirmed
	confirmedAlerts, err := h.alerts.List(ctx, claims.UserID, alert.ListFilter{Status: &confirmedStatus})
	if err != nil {
		respondError(c, http.StatusInternalServerError, 50000, "服务器内部错误")
		return
	}
	overview.Alerts = alertStat{
		Active:         int(activeAlerts.Total),
		PendingConfirm: int(confirmedAlerts.Total),
	}

	respondSuccess(c, http.StatusOK, overview)
}
