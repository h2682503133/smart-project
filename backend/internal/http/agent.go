package http

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/chenluuo/smart-project/backend/internal/agent"
	"github.com/gin-gonic/gin"
)

type agentHandler struct{ service agentService }

type createAgentSessionRequest struct {
	PlotID *uint64 `json:"plotId"`
}

type appendAgentMessageRequest struct {
	Role         string          `json:"role" binding:"required"`
	Content      string          `json:"content" binding:"required"`
	Citations    json.RawMessage `json:"citations"`
	PlotID       *uint64         `json:"plotId"`
	ModelVersion string          `json:"modelVersion"`
	TraceID      string          `json:"traceId"`
}

type appendPythonAgentMessageRequest struct {
	Role         string          `json:"role" binding:"required"`
	Content      string          `json:"content" binding:"required"`
	Citations    json.RawMessage `json:"citations"`
	PlotID       *string         `json:"plot_id"`
	ModelVersion string          `json:"model_version"`
	TraceID      string          `json:"trace_id"`
}

func registerAgentRoutes(router *gin.Engine, auth authService, service agentService, internalServiceKey string) {
	handler := agentHandler{service: service}
	api := router.Group("/api/v1/ai", jwtAuthentication(auth))
	api.POST("/sessions", handler.createSession)
	api.GET("/sessions/:sessionId/messages", handler.listMessages)
	api.POST("/sessions/:sessionId/close", handler.closeSession)
	pythonAPI := router.Group("/api/v1/agent", jwtAuthentication(auth))
	pythonAPI.POST("/sessions/:sessionId/messages", handler.appendPythonMessage)

	internal := router.Group("/internal/agent", internalServiceAuthentication(internalServiceKey))
	internal.POST("/sessions/:sessionId/messages", handler.appendMessage)
}

func (h agentHandler) appendPythonMessage(c *gin.Context) {
	claims, ok := authenticatedClaims(c)
	if !ok {
		return
	}
	var request appendPythonAgentMessageRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		respondError(c, http.StatusBadRequest, 40001, "参数错误：role 和 content 不能为空")
		return
	}
	plotID, err := parsePythonPlotID(request.PlotID)
	if err != nil {
		respondError(c, http.StatusBadRequest, 40001, "参数错误：plot_id 必须是正整数")
		return
	}
	traceID := strings.TrimSpace(request.TraceID)
	if traceID == "" {
		traceID = strings.TrimSpace(c.GetHeader("X-Trace-Id"))
	}
	result, err := h.service.AppendMessageByOwner(c.Request.Context(), claims.UserID, c.Param("sessionId"), agent.MessageInput{
		Role: request.Role, Content: request.Content, Citations: request.Citations,
		PlotID: plotID, ModelVersion: request.ModelVersion, TraceID: traceID,
	})
	if err != nil {
		respondAgentError(c, err)
		return
	}
	respondSuccess(c, http.StatusCreated, gin.H{"messageId": result.ID, "createdAt": result.CreatedAt})
}

func parsePythonPlotID(value *string) (*uint64, error) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil, nil
	}
	parsed, err := strconv.ParseUint(strings.TrimSpace(*value), 10, 64)
	if err != nil || parsed == 0 {
		return nil, agent.ErrInvalidInput
	}
	return &parsed, nil
}

func (h agentHandler) createSession(c *gin.Context) {
	claims, ok := authenticatedClaims(c)
	if !ok {
		return
	}
	var request createAgentSessionRequest
	if err := c.ShouldBindJSON(&request); err != nil && !errors.Is(err, io.EOF) {
		respondError(c, http.StatusBadRequest, 40001, "参数错误")
		return
	}
	result, err := h.service.CreateSession(c.Request.Context(), claims.UserID, request.PlotID)
	if err != nil {
		respondAgentError(c, err)
		return
	}
	respondSuccess(c, http.StatusCreated, result)
}

func (h agentHandler) appendMessage(c *gin.Context) {
	var request appendAgentMessageRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		respondError(c, http.StatusBadRequest, 40001, "参数错误：role 和 content 不能为空")
		return
	}
	result, err := h.service.AppendMessage(c.Request.Context(), c.Param("sessionId"), agent.MessageInput{
		Role: request.Role, Content: request.Content, Citations: request.Citations,
		PlotID: request.PlotID, ModelVersion: request.ModelVersion, TraceID: request.TraceID,
	})
	if err != nil {
		respondAgentError(c, err)
		return
	}
	respondSuccess(c, http.StatusCreated, gin.H{"messageId": result.ID, "createdAt": result.CreatedAt})
}

func (h agentHandler) listMessages(c *gin.Context) {
	claims, ok := authenticatedClaims(c)
	if !ok {
		return
	}
	page, pageSize, ok := pagination(c)
	if !ok {
		return
	}
	result, err := h.service.ListMessages(c.Request.Context(), claims.UserID, c.Param("sessionId"), page, pageSize)
	if err != nil {
		respondAgentError(c, err)
		return
	}
	respondSuccess(c, http.StatusOK, result)
}

func (h agentHandler) closeSession(c *gin.Context) {
	claims, ok := authenticatedClaims(c)
	if !ok {
		return
	}
	result, err := h.service.CloseSession(c.Request.Context(), claims.UserID, c.Param("sessionId"))
	if err != nil {
		respondAgentError(c, err)
		return
	}
	respondSuccess(c, http.StatusOK, gin.H{"sessionId": result.ID, "status": result.Status})
}

func respondAgentError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, agent.ErrInvalidInput):
		respondError(c, http.StatusBadRequest, 40001, "参数错误")
	case errors.Is(err, agent.ErrNotFound):
		respondError(c, http.StatusNotFound, 40404, "会话不存在")
	case errors.Is(err, agent.ErrConflict):
		respondError(c, http.StatusConflict, 40903, "会话状态冲突")
	default:
		respondError(c, http.StatusInternalServerError, 50000, "服务器内部错误")
	}
}

func pagination(c *gin.Context) (int, int, bool) {
	page, pageSize := 1, 20
	var err error
	if raw := c.Query("page"); raw != "" {
		page, err = strconv.Atoi(raw)
		if err != nil || page < 1 {
			respondError(c, http.StatusBadRequest, 40001, "参数错误：page 必须为正整数")
			return 0, 0, false
		}
	}
	if raw := c.Query("pageSize"); raw != "" {
		pageSize, err = strconv.Atoi(raw)
		if err != nil || pageSize < 1 || pageSize > 100 {
			respondError(c, http.StatusBadRequest, 40001, "参数错误：pageSize 必须为 1 到 100")
			return 0, 0, false
		}
	}
	return page, pageSize, true
}

func internalServiceAuthentication(expected string) gin.HandlerFunc {
	return func(c *gin.Context) {
		provided := c.GetHeader("X-Internal-Service-Key")
		if expected == "" || len(provided) != len(expected) || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
			respondError(c, http.StatusUnauthorized, 40102, "内部服务认证失败")
			c.Abort()
			return
		}
		c.Next()
	}
}
