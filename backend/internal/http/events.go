package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/chenluuo/smart-project/backend/internal/events"
	"github.com/chenluuo/smart-project/backend/internal/identity"
	"github.com/gin-gonic/gin"
)

const defaultEventHeartbeatInterval = 15 * time.Second

type eventSubscriber interface {
	Subscribe(uint64, string) *events.Subscription
}

type eventHandler struct {
	subscriber        eventSubscriber
	heartbeatInterval time.Duration
}

func registerEventRoutes(router *gin.Engine, auth authService, subscriber eventSubscriber) {
	handler := eventHandler{subscriber: subscriber, heartbeatInterval: defaultEventHeartbeatInterval}
	eventRoutes := router.Group("/api/v1/events")
	eventRoutes.GET("/stream", jwtAuthentication(auth), handler.stream)
}

func (h eventHandler) stream(c *gin.Context) {
	claimsValue, exists := c.Get(claimsContextKey)
	claims, ok := claimsValue.(identity.Claims)
	if !exists || !ok || claims.UserID == 0 {
		respondError(c, http.StatusUnauthorized, 40101, "未登录或访问令牌无效")
		return
	}
	lastEventID := strings.TrimSpace(c.GetHeader("Last-Event-ID"))
	if len(lastEventID) > 128 || strings.ContainsAny(lastEventID, "\r\n") {
		respondError(c, http.StatusBadRequest, 40001, "Last-Event-ID 无效")
		return
	}
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		respondError(c, http.StatusInternalServerError, 50000, "当前服务器不支持事件流")
		return
	}

	subscription := h.subscriber.Subscribe(claims.UserID, lastEventID)
	defer subscription.Close()
	c.Header("Content-Type", "text/event-stream; charset=utf-8")
	c.Header("Cache-Control", "no-cache, no-transform")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	if _, err := fmt.Fprint(c.Writer, "retry: 3000\n: connected\n\n"); err != nil {
		return
	}
	flusher.Flush()

	heartbeatInterval := h.heartbeatInterval
	if heartbeatInterval <= 0 {
		heartbeatInterval = defaultEventHeartbeatInterval
	}
	heartbeats := time.NewTicker(heartbeatInterval)
	defer heartbeats.Stop()

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case event, open := <-subscription.Events:
			if !open || writeEvent(c.Writer, event) != nil {
				return
			}
			flusher.Flush()
		case now := <-heartbeats.C:
			if _, err := fmt.Fprintf(c.Writer, ": heartbeat %d\n\n", now.Unix()); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func writeEvent(writer http.ResponseWriter, event events.Event) error {
	if event.ID == "" || event.Type == "" || strings.ContainsAny(event.ID, "\r\n") || strings.ContainsAny(event.Type, "\r\n") {
		return events.ErrInvalidEvent
	}
	payload := make(map[string]any, len(event.Payload)+2)
	for key, value := range event.Payload {
		payload[key] = value
	}
	payload["eventTime"] = event.EventTime.Format(time.RFC3339Nano)
	payload["resourceId"] = event.ResourceID
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(writer, "id: %s\nevent: %s\ndata: %s\n\n", event.ID, event.Type, encoded)
	return err
}
