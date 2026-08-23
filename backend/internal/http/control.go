package http

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/chenluuo/smart-project/backend/internal/control"
	"github.com/gin-gonic/gin"
)

type controlHandler struct{ service controlService }

type irrigationCommandRequest struct {
	Action          string `json:"action"`
	DurationSeconds int    `json:"durationSeconds"`
	Mode            string `json:"mode"`
	Reason          string `json:"reason"`
}

func registerControlRoutes(router *gin.Engine, auth authService, service controlService) {
	handler := controlHandler{service: service}
	api := router.Group("/api/v1", jwtAuthentication(auth))
	api.GET("/plots/:plotId/irrigation/status", handler.irrigationStatus)
	api.POST("/plots/:plotId/irrigation/commands", handler.issue)
	api.GET("/commands/:commandId", handler.command)
	api.GET("/commands", handler.list)
}

func (h controlHandler) issue(c *gin.Context) {
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
	var request irrigationCommandRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		respondError(c, http.StatusBadRequest, 40001, "参数错误：请求体格式不正确")
		return
	}
	result, err := h.service.Issue(c.Request.Context(), claims.UserID, plotID, control.IssueInput{
		Action: request.Action, DurationSeconds: request.DurationSeconds,
		Mode: request.Mode, Reason: request.Reason, IdempotencyKey: c.GetHeader("Idempotency-Key"),
	})
	switch {
	case errors.Is(err, control.ErrInvalidInput):
		respondError(c, http.StatusBadRequest, 40001, "参数错误：action、durationSeconds、mode、reason 或 Idempotency-Key 不合法")
	case errors.Is(err, control.ErrNotFound):
		respondError(c, http.StatusNotFound, 40401, "地块不存在或未绑定灌溉阀门")
	case errors.Is(err, control.ErrDeviceOffline):
		respondError(c, http.StatusConflict, 40902, "灌溉阀门不在线，无法执行实时控制")
	case err != nil:
		respondError(c, http.StatusInternalServerError, 50000, "服务器内部错误")
	default:
		respondSuccess(c, http.StatusOK, result)
	}
}

func (h controlHandler) irrigationStatus(c *gin.Context) {
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
	result, err := h.service.IrrigationStatus(c.Request.Context(), claims.UserID, plotID)
	switch {
	case errors.Is(err, control.ErrNotFound):
		respondError(c, http.StatusNotFound, 40401, "地块不存在或未绑定灌溉阀门")
	case errors.Is(err, control.ErrInvalidInput):
		respondError(c, http.StatusBadRequest, 40001, "参数错误：plotId 必须为正整数")
	case err != nil:
		respondError(c, http.StatusInternalServerError, 50000, "服务器内部错误")
	default:
		respondSuccess(c, http.StatusOK, gin.H{
			"plotId": result.PlotID, "valveDeviceId": result.ValveDeviceID,
			"state": result.State, "mode": result.Mode, "remainingSeconds": result.RemainingSeconds,
			"maxSeconds": result.MaxSeconds, "lastCommandId": result.LastCommandID,
		})
	}
}

func (h controlHandler) command(c *gin.Context) {
	claims, ok := authenticatedClaims(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, 40101, "未登录或访问令牌无效")
		return
	}
	commandID := strings.TrimSpace(c.Param("commandId"))
	if commandID == "" || len(commandID) > 64 {
		respondError(c, http.StatusBadRequest, 40001, "参数错误：commandId 不能为空且长度不能超过 64")
		return
	}
	result, err := h.service.Command(c.Request.Context(), claims.UserID, commandID)
	switch {
	case errors.Is(err, control.ErrNotFound):
		respondError(c, http.StatusNotFound, 40403, "命令不存在")
	case errors.Is(err, control.ErrInvalidInput):
		respondError(c, http.StatusBadRequest, 40001, "参数错误：commandId 不合法")
	case err != nil:
		respondError(c, http.StatusInternalServerError, 50000, "服务器内部错误")
	default:
		respondSuccess(c, http.StatusOK, gin.H{
			"id": result.ID, "plotId": result.PlotID, "deviceId": result.DeviceID,
			"action": result.Action, "status": result.Status,
			"requestPayload": result.RequestPayload, "ackPayload": result.AckPayload,
			"createdAt": result.CreatedAt, "ackAt": result.AckAt,
		})
	}
}

func (h controlHandler) list(c *gin.Context) {
	claims, ok := authenticatedClaims(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, 40101, "未登录或访问令牌无效")
		return
	}
	filter, err := parseCommandListFilter(c)
	if err != nil {
		respondError(c, http.StatusBadRequest, 40001, err.Error())
		return
	}
	result, err := h.service.List(c.Request.Context(), claims.UserID, filter)
	switch {
	case errors.Is(err, control.ErrInvalidInput):
		respondError(c, http.StatusBadRequest, 40001, "参数错误：分页、plotId 或 status 不合法")
	case err != nil:
		respondError(c, http.StatusInternalServerError, 50000, "服务器内部错误")
	default:
		respondSuccess(c, http.StatusOK, result)
	}
}

func parseCommandListFilter(c *gin.Context) (control.ListFilter, error) {
	filter := control.ListFilter{Page: 1, PageSize: 20}
	if value := c.Query("plotId"); value != "" {
		plotID, err := strconv.ParseUint(value, 10, 64)
		if err != nil || plotID == 0 {
			return filter, errors.New("参数错误：plotId 必须为正整数")
		}
		filter.PlotID = &plotID
	}
	if value := strings.TrimSpace(c.Query("status")); value != "" {
		status := control.Status(strings.ToUpper(value))
		if !control.ValidStatus(status) {
			return filter, errors.New("参数错误：status 不合法")
		}
		filter.Status = &status
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
