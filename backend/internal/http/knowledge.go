package http

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/chenluuo/smart-project/backend/internal/identity"
	"github.com/chenluuo/smart-project/backend/internal/knowledge"
	"github.com/gin-gonic/gin"
)

type knowledgeHandler struct{ service knowledgeService }

func registerKnowledgeRoutes(router *gin.Engine, auth authService, service knowledgeService) {
	handler := knowledgeHandler{service: service}
	api := router.Group("/api/v1/knowledge/docs", jwtAuthentication(auth))
	api.GET("", handler.list)
	api.POST("", requireSystemAdmin(), handler.upload)
	api.POST("/:docId/approve", requireSystemAdmin(), handler.approve)
	api.POST("/:docId/publish", requireSystemAdmin(), handler.publish)
	api.POST("/:docId/archive", requireSystemAdmin(), handler.archive)
}

func (h knowledgeHandler) list(c *gin.Context) {
	documents, err := h.service.ListActive(c.Request.Context(), c.Query("category"))
	if err != nil {
		respondKnowledgeError(c, err)
		return
	}
	respondSuccess(c, http.StatusOK, documents)
}

func (h knowledgeHandler) upload(c *gin.Context) {
	claims, ok := authenticatedClaims(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.service.MaxUploadBytes()+1024*1024)
	fileHeader, err := c.FormFile("file")
	if err != nil {
		respondError(c, http.StatusBadRequest, 40001, "参数错误：file 不能为空")
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		respondError(c, http.StatusBadRequest, 40001, "无法读取上传文件")
		return
	}
	defer file.Close()
	version := 0
	if raw := c.PostForm("version"); raw != "" {
		version, err = strconv.Atoi(raw)
		if err != nil || version < 1 {
			respondError(c, http.StatusBadRequest, 40001, "参数错误：version 必须为正整数")
			return
		}
	}
	document, err := h.service.Upload(c.Request.Context(), claims.UserID, knowledge.UploadInput{
		Title: c.PostForm("title"), Category: c.PostForm("category"), Filename: fileHeader.Filename,
		ContentType: fileHeader.Header.Get("Content-Type"), Source: c.PostForm("source"), Version: version,
		Size: fileHeader.Size, Reader: file, TraceID: c.GetHeader("X-Trace-ID"),
	})
	if err != nil {
		respondKnowledgeError(c, err)
		return
	}
	respondSuccess(c, http.StatusCreated, document)
}

func (h knowledgeHandler) approve(c *gin.Context) { h.transition(c, h.service.Approve) }
func (h knowledgeHandler) publish(c *gin.Context) { h.transition(c, h.service.Publish) }
func (h knowledgeHandler) archive(c *gin.Context) { h.transition(c, h.service.Archive) }

func (h knowledgeHandler) transition(c *gin.Context, transition func(context.Context, uint64, uint64, string) (*knowledge.Document, error)) {
	claims, ok := authenticatedClaims(c)
	if !ok {
		return
	}
	documentID, err := positivePathID(c, "docId")
	if err != nil {
		respondError(c, http.StatusBadRequest, 40001, "参数错误：docId 必须为正整数")
		return
	}
	document, err := transition(c.Request.Context(), claims.UserID, documentID, c.GetHeader("X-Trace-ID"))
	if err != nil {
		respondKnowledgeError(c, err)
		return
	}
	respondSuccess(c, http.StatusOK, document)
}

func requireSystemAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		value, exists := c.Get(claimsContextKey)
		claims, ok := value.(identity.Claims)
		if !exists || !ok || claims.Role != "SYSTEM_ADMIN" {
			respondError(c, http.StatusForbidden, 40301, "需要系统管理员权限")
			c.Abort()
			return
		}
		c.Next()
	}
}

func respondKnowledgeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, knowledge.ErrInvalidInput):
		respondError(c, http.StatusBadRequest, 40001, "参数错误")
	case errors.Is(err, knowledge.ErrNotFound):
		respondError(c, http.StatusNotFound, 40405, "知识文档不存在")
	case errors.Is(err, knowledge.ErrConflict):
		respondError(c, http.StatusConflict, 40904, "知识文档重复或状态冲突")
	case errors.Is(err, knowledge.ErrFileTooLarge):
		respondError(c, http.StatusRequestEntityTooLarge, 41301, "知识文档超过上传大小限制")
	case errors.Is(err, knowledge.ErrUnavailable):
		respondError(c, http.StatusServiceUnavailable, 50301, "对象存储未启用")
	default:
		respondError(c, http.StatusInternalServerError, 50000, "服务器内部错误")
	}
}
