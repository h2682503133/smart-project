package http

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/chenluuo/smart-project/backend/internal/device"
	"github.com/gin-gonic/gin"
)

type deviceHandler struct{ service deviceService }

type bindDeviceRequest struct {
	DeviceSN string `json:"deviceSn"`
	PlotID   uint64 `json:"plotId"`
	Name     string `json:"name"`
	Type     string `json:"type"`
}

type deviceListItem struct {
	ID              uint64        `json:"id"`
	DeviceSN        string        `json:"deviceSn"`
	Name            string        `json:"name"`
	Type            string        `json:"type"`
	PlotID          uint64        `json:"plotId"`
	Status          device.Status `json:"status"`
	Battery         *int          `json:"battery"`
	LastSeenAt      *time.Time    `json:"lastSeenAt"`
	FirmwareVersion *string       `json:"firmwareVersion"`
}

type deviceStatusDetail struct {
	DeviceID   uint64        `json:"deviceId"`
	Status     device.Status `json:"status"`
	Battery    *int          `json:"battery"`
	Signal     *int          `json:"signal"`
	LastSeenAt *time.Time    `json:"lastSeenAt"`
	Message    *string       `json:"message"`
}

func registerDeviceRoutes(router *gin.Engine, auth authService, service deviceService) {
	handler := deviceHandler{service: service}
	devices := router.Group("/api/v1/devices", jwtAuthentication(auth))
	devices.GET("", handler.list)
	devices.POST("/bind", handler.bind)
	devices.DELETE("/:deviceId/binding", handler.unbind)
	devices.GET("/:deviceId/status", handler.status)
}

func (h deviceHandler) list(c *gin.Context) {
	claims, ok := authenticatedClaims(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, 40101, "未登录或访问令牌无效")
		return
	}
	filter, err := parseDeviceListFilter(c)
	if err != nil {
		respondError(c, http.StatusBadRequest, 40001, err.Error())
		return
	}
	result, err := h.service.List(c.Request.Context(), claims.UserID, filter)
	if errors.Is(err, device.ErrInvalidInput) {
		respondError(c, http.StatusBadRequest, 40001, "参数错误：分页、plotId 或 status 不合法")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, 50000, "服务器内部错误")
		return
	}
	items := make([]deviceListItem, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, deviceListItem{
			ID: item.Device.ID, DeviceSN: item.Device.SerialNo, Name: item.Device.Name,
			Type: item.Device.DeviceType, PlotID: item.PlotID, Status: item.Device.Status,
			Battery: item.Device.Battery, LastSeenAt: item.Device.LastSeenAt, FirmwareVersion: item.Device.FirmwareVersion,
		})
	}
	respondSuccess(c, http.StatusOK, gin.H{"items": items, "page": result.Page, "pageSize": result.PageSize, "total": result.Total})
}

func (h deviceHandler) bind(c *gin.Context) {
	claims, ok := authenticatedClaims(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, 40101, "未登录或访问令牌无效")
		return
	}
	var request bindDeviceRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		respondError(c, http.StatusBadRequest, 40001, "参数错误：请求体格式不正确")
		return
	}
	result, err := h.service.Bind(c.Request.Context(), claims.UserID, device.BindInput{
		SerialNo: request.DeviceSN, PlotID: request.PlotID, Name: request.Name, DeviceType: request.Type,
	})
	switch {
	case errors.Is(err, device.ErrInvalidInput):
		respondError(c, http.StatusBadRequest, 40001, "参数错误：deviceSn、plotId、name 和 type 不能为空且长度必须合法")
	case errors.Is(err, device.ErrNotFound):
		respondError(c, http.StatusNotFound, 40401, "地块不存在")
	case errors.Is(err, device.ErrAlreadyBound):
		respondError(c, http.StatusConflict, 40901, "设备已绑定")
	case errors.Is(err, device.ErrDeviceTypeMismatch):
		respondError(c, http.StatusConflict, 40901, "设备类型与已登记信息不一致")
	case err != nil:
		respondError(c, http.StatusInternalServerError, 50000, "服务器内部错误")
	default:
		respondSuccess(c, http.StatusOK, gin.H{"id": result.ID, "deviceSn": result.SerialNo, "status": result.Status})
	}
}

func (h deviceHandler) unbind(c *gin.Context) {
	claims, ok := authenticatedClaims(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, 40101, "未登录或访问令牌无效")
		return
	}
	deviceID, ok := parseDeviceID(c)
	if !ok {
		return
	}
	err := h.service.Unbind(c.Request.Context(), claims.UserID, deviceID)
	switch {
	case errors.Is(err, device.ErrNotFound):
		respondError(c, http.StatusNotFound, 40402, "设备不存在或未绑定")
	case errors.Is(err, device.ErrInvalidInput):
		respondError(c, http.StatusBadRequest, 40001, "参数错误：deviceId 必须为正整数")
	case err != nil:
		respondError(c, http.StatusInternalServerError, 50000, "服务器内部错误")
	default:
		respondSuccess(c, http.StatusOK, true)
	}
}

func (h deviceHandler) status(c *gin.Context) {
	claims, ok := authenticatedClaims(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, 40101, "未登录或访问令牌无效")
		return
	}
	deviceID, ok := parseDeviceID(c)
	if !ok {
		return
	}
	result, err := h.service.Status(c.Request.Context(), claims.UserID, deviceID)
	switch {
	case errors.Is(err, device.ErrNotFound):
		respondError(c, http.StatusNotFound, 40402, "设备不存在")
	case errors.Is(err, device.ErrInvalidInput):
		respondError(c, http.StatusBadRequest, 40001, "参数错误：deviceId 必须为正整数")
	case err != nil:
		respondError(c, http.StatusInternalServerError, 50000, "服务器内部错误")
	default:
		respondSuccess(c, http.StatusOK, deviceStatusDetail{
			DeviceID: result.ID, Status: result.Status, Battery: result.Battery, Signal: result.Signal,
			LastSeenAt: result.LastSeenAt, Message: result.StatusMessage,
		})
	}
}

func parseDeviceListFilter(c *gin.Context) (device.ListFilter, error) {
	filter := device.ListFilter{Page: 1, PageSize: 20, DeviceType: strings.TrimSpace(c.Query("type"))}
	if value := c.Query("plotId"); value != "" {
		parsed, err := strconv.ParseUint(value, 10, 64)
		if err != nil || parsed == 0 {
			return filter, errors.New("参数错误：plotId 必须为正整数")
		}
		filter.PlotID = &parsed
	}
	if value := strings.TrimSpace(c.Query("status")); value != "" {
		status := device.Status(strings.ToUpper(value))
		if !device.ValidStatus(status) {
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

func parseDeviceID(c *gin.Context) (uint64, bool) {
	deviceID, err := strconv.ParseUint(c.Param("deviceId"), 10, 64)
	if err != nil || deviceID == 0 {
		respondError(c, http.StatusBadRequest, 40001, "参数错误：deviceId 必须为正整数")
		return 0, false
	}
	return deviceID, true
}
