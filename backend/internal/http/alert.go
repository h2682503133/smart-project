package http

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/chenluuo/smart-project/backend/internal/alert"
	"github.com/gin-gonic/gin"
)

type alertHandler struct{ service alertService }

type thresholdRuleRequest struct {
	Metric          string                   `json:"metric"`
	Operator        alert.ComparisonOperator `json:"operator"`
	Value           *float64                 `json:"value"`
	Hysteresis      *float64                 `json:"hysteresis"`
	DurationSeconds int                      `json:"durationSeconds"`
	Level           alert.Level              `json:"level"`
	Enabled         *bool                    `json:"enabled"`
}

type confirmAlertRequest struct {
	Remark string `json:"remark"`
}

type triggerAlertRequest struct {
	RuleID       uint64   `json:"ruleId"`
	DeviceID     *uint64  `json:"deviceId"`
	TriggerValue *float64 `json:"triggerValue"`
	TriggeredAt  *string  `json:"triggeredAt"`
	TraceID      string   `json:"traceId"`
}

func registerAlertRoutes(router *gin.Engine, auth authService, service alertService) {
	handler := alertHandler{service: service}
	api := router.Group("/api/v1", jwtAuthentication(auth))
	api.GET("/plots/:plotId/thresholds", handler.listRules)
	api.PUT("/plots/:plotId/thresholds/:thresholdId", handler.upsertRule)
	api.GET("/alerts", handler.list)
	api.GET("/alerts/logs", handler.logs)
	api.POST("/alerts/:alertId/confirm", handler.confirm)
}

func registerInternalAlertRoutes(router *gin.Engine, service alertService, internalServiceKey string) {
	handler := alertHandler{service: service}
	internal := router.Group("/internal/alerts", internalServiceAuthentication(internalServiceKey))
	internal.POST("/trigger", handler.trigger)
}

func (h alertHandler) listRules(c *gin.Context) {
	claims, ok := authenticatedClaims(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, 40101, "未登录或访问令牌无效")
		return
	}
	plotID, err := positivePathID(c, "plotId")
	if err != nil {
		respondError(c, http.StatusBadRequest, 40001, "参数错误：plotId 必须为正整数")
		return
	}
	result, err := h.service.ListRules(c.Request.Context(), claims.UserID, plotID)
	switch {
	case errors.Is(err, alert.ErrInvalidInput):
		respondError(c, http.StatusBadRequest, 40001, "参数错误：plotId 必须为正整数")
	case errors.Is(err, alert.ErrNotFound):
		respondError(c, http.StatusNotFound, 40401, "地块不存在")
	case err != nil:
		respondError(c, http.StatusInternalServerError, 50000, "服务器内部错误")
	default:
		respondSuccess(c, http.StatusOK, result)
	}
}

func (h alertHandler) upsertRule(c *gin.Context) {
	claims, ok := authenticatedClaims(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, 40101, "未登录或访问令牌无效")
		return
	}
	plotID, plotErr := positivePathID(c, "plotId")
	thresholdID, thresholdErr := positivePathID(c, "thresholdId")
	if plotErr != nil || thresholdErr != nil {
		respondError(c, http.StatusBadRequest, 40001, "参数错误：plotId 和 thresholdId 必须为正整数")
		return
	}
	var request thresholdRuleRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.Value == nil || request.Enabled == nil {
		respondError(c, http.StatusBadRequest, 40001, "参数错误：请求体格式不正确")
		return
	}
	result, err := h.service.UpsertRule(c.Request.Context(), claims.UserID, plotID, thresholdID, alert.RuleInput{
		Metric: request.Metric, Operator: request.Operator, Value: *request.Value, Hysteresis: request.Hysteresis,
		DurationSeconds: request.DurationSeconds, Level: request.Level, Enabled: *request.Enabled,
	})
	switch {
	case errors.Is(err, alert.ErrInvalidInput):
		respondError(c, http.StatusBadRequest, 40001, "参数错误：阈值规则字段不合法")
	case errors.Is(err, alert.ErrNotFound):
		respondError(c, http.StatusNotFound, 40401, "地块或阈值规则不存在")
	case err != nil:
		respondError(c, http.StatusInternalServerError, 50000, "服务器内部错误")
	default:
		respondSuccess(c, http.StatusOK, result)
	}
}

func (h alertHandler) list(c *gin.Context) {
	h.listWithFilter(c, false)
}

func (h alertHandler) logs(c *gin.Context) {
	h.listWithFilter(c, true)
}

func (h alertHandler) listWithFilter(c *gin.Context, logs bool) {
	claims, ok := authenticatedClaims(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, 40101, "未登录或访问令牌无效")
		return
	}
	filter, err := parseAlertListFilter(c, logs)
	if err != nil {
		respondError(c, http.StatusBadRequest, 40001, err.Error())
		return
	}
	result, err := h.service.List(c.Request.Context(), claims.UserID, filter)
	if errors.Is(err, alert.ErrInvalidInput) {
		respondError(c, http.StatusBadRequest, 40001, "参数错误：告警筛选条件不合法")
	} else if err != nil {
		respondError(c, http.StatusInternalServerError, 50000, "服务器内部错误")
	} else {
		respondSuccess(c, http.StatusOK, result)
	}
}

func (h alertHandler) confirm(c *gin.Context) {
	claims, ok := authenticatedClaims(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, 40101, "未登录或访问令牌无效")
		return
	}
	alertID, err := positivePathID(c, "alertId")
	if err != nil {
		respondError(c, http.StatusBadRequest, 40001, "参数错误：alertId 必须为正整数")
		return
	}
	var request confirmAlertRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		respondError(c, http.StatusBadRequest, 40001, "参数错误：请求体格式不正确")
		return
	}
	result, err := h.service.Confirm(c.Request.Context(), claims.UserID, alertID, request.Remark)
	switch {
	case errors.Is(err, alert.ErrInvalidInput):
		respondError(c, http.StatusBadRequest, 40001, "参数错误：remark 不能为空且不能超过 500 个字符")
	case errors.Is(err, alert.ErrNotFound):
		respondError(c, http.StatusNotFound, 40404, "告警不存在")
	case errors.Is(err, alert.ErrConflict):
		respondError(c, http.StatusConflict, 40903, "告警当前状态不能确认")
	case err != nil:
		respondError(c, http.StatusInternalServerError, 50000, "服务器内部错误")
	default:
		respondSuccess(c, http.StatusOK, result)
	}
}

func (h alertHandler) trigger(c *gin.Context) {
	var request triggerAlertRequest
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1024*1024+1))
	if err != nil || len(body) == 0 || len(body) > 1024*1024 || json.Unmarshal(body, &request) != nil || request.TriggerValue == nil {
		respondError(c, http.StatusBadRequest, 40001, "参数错误：ruleId 和 triggerValue 不能为空")
		return
	}
	var triggeredAt *time.Time
	if request.TriggeredAt != nil {
		parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(*request.TriggeredAt))
		if err != nil {
			respondError(c, http.StatusBadRequest, 40001, "参数错误：triggeredAt 必须为 ISO 8601 时间")
			return
		}
		triggeredAt = &parsed
	}
	result, err := h.service.Trigger(c.Request.Context(), alert.TriggerInput{
		RuleID: request.RuleID, DeviceID: request.DeviceID, TriggerValue: *request.TriggerValue,
		TriggeredAt: triggeredAt, TraceID: request.TraceID, ForwardBody: append([]byte(nil), body...),
	})
	switch {
	case errors.Is(err, alert.ErrInvalidInput):
		respondError(c, http.StatusBadRequest, 40001, "参数错误：告警触发数据不合法")
	case errors.Is(err, alert.ErrNotFound):
		respondError(c, http.StatusNotFound, 40404, "告警规则或绑定设备不存在")
	case errors.Is(err, alert.ErrConflict):
		respondError(c, http.StatusConflict, 40903, "告警规则未启用")
	case err != nil:
		respondError(c, http.StatusInternalServerError, 50000, "服务器内部错误")
	default:
		status := http.StatusOK
		if result.Created {
			status = http.StatusCreated
		}
		respondSuccess(c, status, result)
	}
}

func parseAlertListFilter(c *gin.Context, logs bool) (alert.ListFilter, error) {
	filter := alert.ListFilter{Page: 1, PageSize: 20}
	if value := c.Query("plotId"); value != "" {
		plotID, err := strconv.ParseUint(value, 10, 64)
		if err != nil || plotID == 0 {
			return filter, errors.New("参数错误：plotId 必须为正整数")
		}
		filter.PlotID = &plotID
	}
	if value := strings.TrimSpace(c.Query("status")); value != "" {
		status := alert.Status(strings.ToUpper(value))
		filter.Status = &status
	}
	if logs {
		if value := c.Query("farmId"); value != "" {
			if farmID, err := strconv.ParseUint(value, 10, 64); err != nil || farmID == 0 {
				return filter, errors.New("参数错误：farmId 必须为正整数")
			}
		}
		if value := c.Query("startTime"); value != "" {
			parsed, err := time.Parse(time.RFC3339, value)
			if err != nil {
				return filter, errors.New("参数错误：startTime 必须为 ISO 8601 时间")
			}
			filter.StartTime = &parsed
		}
		if value := c.Query("endTime"); value != "" {
			parsed, err := time.Parse(time.RFC3339, value)
			if err != nil {
				return filter, errors.New("参数错误：endTime 必须为 ISO 8601 时间")
			}
			filter.EndTime = &parsed
		}
	}
	var err error
	if value := c.Query("page"); value != "" {
		filter.Page, err = strconv.Atoi(value)
		if err != nil || filter.Page < 1 {
			return filter, errors.New("参数错误：page 必须为正整数")
		}
	}
	if value := c.Query("pageSize"); value != "" {
		filter.PageSize, err = strconv.Atoi(value)
		if err != nil || filter.PageSize < 1 || filter.PageSize > 100 {
			return filter, errors.New("参数错误：pageSize 必须为 1 到 100 的整数")
		}
	}
	return filter, nil
}

func positivePathID(c *gin.Context, name string) (uint64, error) {
	value, err := strconv.ParseUint(c.Param(name), 10, 64)
	if err != nil || value == 0 {
		return 0, errors.New("invalid id")
	}
	return value, nil
}
