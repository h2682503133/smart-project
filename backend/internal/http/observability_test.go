package http

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chenluuo/smart-project/backend/internal/identity"
	"github.com/gin-gonic/gin"
)

func TestObservabilityMiddlewareRecordsEveryRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	router := gin.New()
	router.Use(observabilityMiddleware())
	router.GET("/resources/:id", func(c *gin.Context) {
		c.Set(claimsContextKey, identity.Claims{UserID: 42})
		if got := RequestIDFromContext(c.Request.Context()); got != "request-123" {
			t.Errorf("request id in context = %q", got)
		}
		if got := TraceIDFromContext(c.Request.Context()); got != "trace-456" {
			t.Errorf("trace id in context = %q", got)
		}
		c.JSON(http.StatusCreated, gin.H{"ok": true})
	})

	request := httptest.NewRequest(http.MethodGet, "/resources/7?secret=not-logged", nil)
	request.Header.Set(requestIDHeader, "request-123")
	request.Header.Set(traceIDHeader, "trace-456")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if got := response.Header().Get(requestIDHeader); got != "request-123" {
		t.Fatalf("response request id = %q", got)
	}
	if got := response.Header().Get(traceIDHeader); got != "trace-456" {
		t.Fatalf("response trace id = %q", got)
	}
	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &record); err != nil {
		t.Fatalf("decode structured log %q: %v", logs.String(), err)
	}
	for key, want := range map[string]any{
		"event": "http_request_completed", "request_id": "request-123",
		"trace_id": "trace-456", "method": http.MethodGet, "route": "/resources/:id",
		"path": "/resources/7", "status": float64(http.StatusCreated), "result": "success",
		"actor_id": float64(42),
	} {
		if got := record[key]; got != want {
			t.Errorf("log[%q] = %#v, want %#v", key, got, want)
		}
	}
	if bytes.Contains(logs.Bytes(), []byte("secret")) {
		t.Fatalf("query string leaked into log: %s", logs.String())
	}
}

func TestObservabilityMiddlewareGeneratesSafeCorrelationIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	router := gin.New()
	router.Use(observabilityMiddleware())
	router.GET("/health", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	request.Header.Set(requestIDHeader, "contains spaces")
	request.Header.Set(traceIDHeader, "")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	requestID := response.Header().Get(requestIDHeader)
	if requestID == "" || requestID == "contains spaces" || validCorrelationID(requestID) == "" {
		t.Fatalf("generated request id = %q", requestID)
	}
	if got := response.Header().Get(traceIDHeader); got != requestID {
		t.Fatalf("generated trace id = %q, want request id %q", got, requestID)
	}
}
