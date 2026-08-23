package http

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/chenluuo/smart-project/backend/internal/identity"
	"github.com/gin-gonic/gin"
)

const (
	requestIDHeader = "X-Request-ID"
	traceIDHeader   = "X-Trace-ID"
)

type correlationContextKey uint8

const (
	requestIDContextKey correlationContextKey = iota
	traceIDContextKey
)

// RequestIDFromContext returns the request identifier assigned at the HTTP edge.
func RequestIDFromContext(ctx context.Context) string {
	value, _ := ctx.Value(requestIDContextKey).(string)
	return value
}

// TraceIDFromContext returns the distributed trace identifier assigned at the HTTP edge.
func TraceIDFromContext(ctx context.Context) string {
	value, _ := ctx.Value(traceIDContextKey).(string)
	return value
}

func observabilityMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		startedAt := time.Now()
		requestID := correlationID(c.GetHeader(requestIDHeader))
		traceID := validCorrelationID(c.GetHeader(traceIDHeader))
		if traceID == "" {
			traceID = requestID
		}

		c.Header(requestIDHeader, requestID)
		c.Header(traceIDHeader, traceID)
		ctx := context.WithValue(c.Request.Context(), requestIDContextKey, requestID)
		ctx = context.WithValue(ctx, traceIDContextKey, traceID)
		c.Request = c.Request.WithContext(ctx)

		c.Next()

		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		status := c.Writer.Status()
		result := "success"
		if status >= http.StatusBadRequest {
			result = "failure"
		}
		attrs := []any{
			"event", "http_request_completed",
			"request_id", requestID,
			"trace_id", traceID,
			"method", c.Request.Method,
			"route", route,
			"path", c.Request.URL.Path,
			"status", status,
			"result", result,
			"duration_ms", time.Since(startedAt).Milliseconds(),
			"request_bytes", c.Request.ContentLength,
			"response_bytes", c.Writer.Size(),
			"client_ip", c.ClientIP(),
		}
		if claims, ok := c.Get(claimsContextKey); ok {
			if authenticated, valid := claims.(identity.Claims); valid && authenticated.UserID != 0 {
				attrs = append(attrs, "actor_id", authenticated.UserID)
			}
		}
		if len(c.Errors) > 0 {
			attrs = append(attrs, "error_count", len(c.Errors))
		}

		logger := slog.Default()
		switch {
		case status >= http.StatusInternalServerError:
			logger.Error("HTTP request completed", attrs...)
		case status >= http.StatusBadRequest:
			logger.Warn("HTTP request completed", attrs...)
		default:
			logger.Info("HTTP request completed", attrs...)
		}
	}
}

func correlationID(value string) string {
	if value = validCorrelationID(value); value != "" {
		return value
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return hex.EncodeToString(raw[:])
	}
	return strings.ReplaceAll(time.Now().UTC().Format("20060102T150405.000000000"), ".", "")
}

func validCorrelationID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 64 {
		return ""
	}
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' ||
			char >= '0' && char <= '9' || strings.ContainsRune("._:-", char) {
			continue
		}
		return ""
	}
	return value
}
