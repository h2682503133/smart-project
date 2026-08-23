package http

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chenluuo/smart-project/backend/internal/events"
)

func TestEventStreamRequiresAuthentication(t *testing.T) {
	broker := events.NewBroker(10)
	router := NewRouterWithBackendServices(
		"test", pingerStub{}, authServiceStub{}, nil, nil, nil, nil, nil, nil, nil,
		"unused-internal-service-key-32-chars", broker,
	)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/events/stream", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(response.Body.String(), `"code":40101`) {
		t.Fatalf("body = %q, want authentication error", response.Body.String())
	}
}

func TestEventStreamHeadersFilteringReplayHeartbeatAndCleanup(t *testing.T) {
	broker := events.NewBroker(10)
	first, err := broker.Publish(events.Event{Type: "telemetry.updated", OwnerID: 7, ResourceID: "plot-old"})
	if err != nil {
		t.Fatalf("publish first event: %v", err)
	}
	if _, err := broker.Publish(events.Event{Type: "alert.created", OwnerID: 8, ResourceID: "alert-foreign"}); err != nil {
		t.Fatalf("publish foreign event: %v", err)
	}
	replayed, err := broker.Publish(events.Event{
		Type: "command.result", OwnerID: 7, ResourceID: "command-1",
		EventTime: time.Date(2026, 8, 22, 8, 21, 12, 0, time.FixedZone("CST", 8*60*60)),
		Payload:   map[string]any{"status": "SUCCEEDED"},
	})
	if err != nil {
		t.Fatalf("publish replay event: %v", err)
	}

	router := NewRouter("test", pingerStub{}, authServiceStub{})
	handler := eventHandler{subscriber: broker, heartbeatInterval: 10 * time.Millisecond}
	router.GET("/api/v1/events/stream", jwtAuthentication(authServiceStub{}), handler.stream)
	server := httptest.NewServer(router)
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/v1/events/stream", nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer signed-token")
	request.Header.Set("Accept", "text/event-stream")
	request.Header.Set("Last-Event-ID", first.ID)
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("open event stream: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	if contentType := response.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", contentType)
	}
	if value := response.Header.Get("Cache-Control"); value != "no-cache, no-transform" {
		t.Fatalf("Cache-Control = %q", value)
	}
	if value := response.Header.Get("X-Accel-Buffering"); value != "no" {
		t.Fatalf("X-Accel-Buffering = %q", value)
	}

	reader := bufio.NewReader(response.Body)
	stream := readSSEBlocks(t, reader, 2)
	if !strings.Contains(stream, "retry: 3000\n: connected\n\n") {
		t.Fatalf("stream missing initial frame: %q", stream)
	}
	if !strings.Contains(stream, "id: "+replayed.ID+"\nevent: command.result\n") {
		t.Fatalf("stream missing replayed event: %q", stream)
	}
	if strings.Contains(stream, "alert-foreign") {
		t.Fatalf("stream leaked another user's event: %q", stream)
	}
	if !strings.Contains(stream, `"eventTime":"2026-08-22T08:21:12+08:00"`) || !strings.Contains(stream, `"resourceId":"command-1"`) {
		t.Fatalf("stream missing required event metadata: %q", stream)
	}

	heartbeat := readUntilContains(t, reader, ": heartbeat ")
	if !strings.Contains(heartbeat, ": heartbeat ") {
		t.Fatalf("stream missing heartbeat: %q", heartbeat)
	}
	cancel()
	response.Body.Close()
	waitForSubscriberCount(t, broker, 7, 0)
}

func readSSEBlocks(t *testing.T, reader *bufio.Reader, count int) string {
	t.Helper()
	result := ""
	for blocks := 0; blocks < count; {
		line := readLine(t, reader)
		result += line
		if line == "\n" {
			blocks++
		}
	}
	return result
}

func readUntilContains(t *testing.T, reader *bufio.Reader, target string) string {
	t.Helper()
	result := ""
	for !strings.Contains(result, target) {
		result += readLine(t, reader)
	}
	return result
}

func readLine(t *testing.T, reader *bufio.Reader) string {
	t.Helper()
	type readResult struct {
		line string
		err  error
	}
	result := make(chan readResult, 1)
	go func() {
		line, err := reader.ReadString('\n')
		result <- readResult{line: line, err: err}
	}()
	select {
	case value := <-result:
		if value.err != nil && value.err != io.EOF {
			t.Fatalf("read stream: %v", value.err)
		}
		return value.line
	case <-time.After(time.Second):
		t.Fatal("timed out reading event stream")
		return ""
	}
}

func waitForSubscriberCount(t *testing.T, broker *events.Broker, ownerID uint64, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if broker.SubscriberCount(ownerID) == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("subscriber count = %d, want %d", broker.SubscriberCount(ownerID), want)
}
