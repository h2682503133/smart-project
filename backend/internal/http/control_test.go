package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chenluuo/smart-project/backend/internal/control"
)

type controlServiceStub struct {
	issueResult      *control.IssueResult
	irrigationResult *control.IrrigationStatus
	commandResult    *control.CommandResult
	listResult       control.ListResult
	irrigationErr    error
	commandErr       error
	listErr          error
	issueErr         error
	ownerID          uint64
	plotID           uint64
	commandID        string
	filter           control.ListFilter
	issueInput       control.IssueInput
}

func (s *controlServiceStub) Issue(_ context.Context, ownerID, plotID uint64, input control.IssueInput) (*control.IssueResult, error) {
	s.ownerID, s.plotID, s.issueInput = ownerID, plotID, input
	return s.issueResult, s.issueErr
}

func (s *controlServiceStub) IrrigationStatus(_ context.Context, ownerID, plotID uint64) (*control.IrrigationStatus, error) {
	s.ownerID, s.plotID = ownerID, plotID
	return s.irrigationResult, s.irrigationErr
}

func (s *controlServiceStub) Command(_ context.Context, ownerID uint64, commandID string) (*control.CommandResult, error) {
	s.ownerID, s.commandID = ownerID, commandID
	return s.commandResult, s.commandErr
}

func (s *controlServiceStub) List(_ context.Context, ownerID uint64, filter control.ListFilter) (control.ListResult, error) {
	s.ownerID, s.filter = ownerID, filter
	return s.listResult, s.listErr
}

func newControlTestRouter(service controlService) http.Handler {
	return NewRouterWithServices("test", pingerStub{}, authServiceStub{}, nil, nil, service)
}

func TestControlReadEndpointsRequireAuthentication(t *testing.T) {
	router := newControlTestRouter(&controlServiceStub{})
	for _, path := range []string{
		"/api/v1/plots/11/irrigation/status",
		"/api/v1/commands/cmd-1",
		"/api/v1/commands",
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), `"code":40101`) {
			t.Fatalf("GET %s: status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
}

func TestIssueIrrigationCommandRequiresAuthentication(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/plots/11/irrigation/commands", strings.NewReader(`{"action":"CLOSE","mode":"MANUAL"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	newControlTestRouter(&controlServiceStub{}).ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), `"code":40101`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestIssueIrrigationCommand(t *testing.T) {
	createdAt := time.Date(2026, 8, 22, 8, 21, 10, 0, time.UTC)
	service := &controlServiceStub{issueResult: &control.IssueResult{
		CommandID: "cmd-1", PlotID: 11, Action: "OPEN", Status: "SUCCESS", CreatedAt: createdAt,
	}}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/plots/11/irrigation/commands", strings.NewReader(`{"action":"OPEN","durationSeconds":600,"mode":"MANUAL","reason":"土壤湿度低"}`))
	request.Header.Set("Authorization", "Bearer signed-token")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "request-1")
	response := httptest.NewRecorder()
	newControlTestRouter(service).ServeHTTP(response, request)
	if response.Code != http.StatusOK || service.ownerID != 7 || service.plotID != 11 || service.issueInput.DurationSeconds != 600 || service.issueInput.IdempotencyKey != "request-1" {
		t.Fatalf("status=%d owner=%d plot=%d input=%+v body=%s", response.Code, service.ownerID, service.plotID, service.issueInput, response.Body.String())
	}
	for _, want := range []string{`"commandId":"cmd-1"`, `"action":"OPEN"`, `"status":"SUCCESS"`} {
		if !strings.Contains(response.Body.String(), want) {
			t.Fatalf("body=%s, want %s", response.Body.String(), want)
		}
	}
}

func TestIssueIrrigationCommandErrors(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		body       string
		service    *controlServiceStub
		wantStatus int
		wantCode   string
	}{
		{name: "invalid plot", path: "/api/v1/plots/nope/irrigation/commands", body: `{}`, service: &controlServiceStub{}, wantStatus: http.StatusBadRequest, wantCode: `"code":40001`},
		{name: "invalid body", path: "/api/v1/plots/11/irrigation/commands", body: `{`, service: &controlServiceStub{}, wantStatus: http.StatusBadRequest, wantCode: `"code":40001`},
		{name: "invalid fields", path: "/api/v1/plots/11/irrigation/commands", body: `{"action":"OPEN","durationSeconds":1,"mode":"MANUAL"}`, service: &controlServiceStub{issueErr: control.ErrInvalidInput}, wantStatus: http.StatusBadRequest, wantCode: `"code":40001`},
		{name: "missing valve", path: "/api/v1/plots/11/irrigation/commands", body: `{"action":"CLOSE","mode":"MANUAL"}`, service: &controlServiceStub{issueErr: control.ErrNotFound}, wantStatus: http.StatusNotFound, wantCode: `"code":40401`},
		{name: "offline valve", path: "/api/v1/plots/11/irrigation/commands", body: `{"action":"CLOSE","mode":"MANUAL"}`, service: &controlServiceStub{issueErr: control.ErrDeviceOffline}, wantStatus: http.StatusConflict, wantCode: `"code":40902`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))
			request.Header.Set("Authorization", "Bearer signed-token")
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			newControlTestRouter(tt.service).ServeHTTP(response, request)
			if response.Code != tt.wantStatus || !strings.Contains(response.Body.String(), tt.wantCode) {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestIrrigationStatusResponse(t *testing.T) {
	commandID := "cmd-1"
	service := &controlServiceStub{irrigationResult: &control.IrrigationStatus{
		PlotID: 11, ValveDeviceID: 3, State: "ON", Mode: "MANUAL",
		RemainingSeconds: 480, MaxSeconds: 1800, LastCommandID: &commandID,
	}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/plots/11/irrigation/status", nil)
	request.Header.Set("Authorization", "Bearer signed-token")
	response := httptest.NewRecorder()
	newControlTestRouter(service).ServeHTTP(response, request)

	if response.Code != http.StatusOK || service.ownerID != 7 || service.plotID != 11 {
		t.Fatalf("status=%d owner=%d plot=%d body=%s", response.Code, service.ownerID, service.plotID, response.Body.String())
	}
	for _, want := range []string{`"state":"ON"`, `"remainingSeconds":480`, `"lastCommandId":"cmd-1"`} {
		if !strings.Contains(response.Body.String(), want) {
			t.Fatalf("body=%s, want %s", response.Body.String(), want)
		}
	}
}

func TestCommandResultResponse(t *testing.T) {
	createdAt := time.Date(2026, 8, 22, 8, 21, 10, 0, time.FixedZone("CST", 8*60*60))
	ackAt := createdAt.Add(2 * time.Second)
	service := &controlServiceStub{commandResult: &control.CommandResult{
		ID: "cmd-1", PlotID: 11, DeviceID: 3, Action: "OPEN", Status: control.StatusSucceeded,
		RequestPayload: map[string]any{"durationSeconds": 600}, AckPayload: map[string]any{"state": "ON"},
		CreatedAt: createdAt, AckAt: &ackAt,
	}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/commands/cmd-1", nil)
	request.Header.Set("Authorization", "Bearer signed-token")
	response := httptest.NewRecorder()
	newControlTestRouter(service).ServeHTTP(response, request)

	if response.Code != http.StatusOK || service.ownerID != 7 || service.commandID != "cmd-1" {
		t.Fatalf("status=%d owner=%d command=%s body=%s", response.Code, service.ownerID, service.commandID, response.Body.String())
	}
	for _, want := range []string{`"action":"OPEN"`, `"status":"SUCCEEDED"`, `"requestPayload":{"durationSeconds":600}`, `"ackPayload":{"state":"ON"}`} {
		if !strings.Contains(response.Body.String(), want) {
			t.Fatalf("body=%s, want %s", response.Body.String(), want)
		}
	}
}

func TestCommandListParsesFiltersAndReturnsPage(t *testing.T) {
	service := &controlServiceStub{listResult: control.ListResult{
		Items: []control.ListItem{{ID: "cmd-1", PlotCode: "A3", Action: "OPEN", DurationSeconds: 600, Status: control.StatusSucceeded, OperatorName: "张三"}},
		Page:  2, PageSize: 5, Total: 8,
	}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/commands?plotId=11&status=succeeded&page=2&pageSize=5", nil)
	request.Header.Set("Authorization", "Bearer signed-token")
	response := httptest.NewRecorder()
	newControlTestRouter(service).ServeHTTP(response, request)

	if response.Code != http.StatusOK || service.ownerID != 7 || service.filter.PlotID == nil || *service.filter.PlotID != 11 ||
		service.filter.Status == nil || *service.filter.Status != control.StatusSucceeded || service.filter.Page != 2 {
		t.Fatalf("status=%d owner=%d filter=%+v body=%s", response.Code, service.ownerID, service.filter, response.Body.String())
	}
	for _, want := range []string{`"plotCode":"A3"`, `"durationSeconds":600`, `"page":2`, `"total":8`} {
		if !strings.Contains(response.Body.String(), want) {
			t.Fatalf("body=%s, want %s", response.Body.String(), want)
		}
	}
}

func TestControlReadErrors(t *testing.T) {
	tests := []struct {
		path       string
		service    *controlServiceStub
		wantStatus int
		wantCode   string
	}{
		{path: "/api/v1/plots/not-number/irrigation/status", service: &controlServiceStub{}, wantStatus: http.StatusBadRequest, wantCode: `"code":40001`},
		{path: "/api/v1/plots/11/irrigation/status", service: &controlServiceStub{irrigationErr: control.ErrNotFound}, wantStatus: http.StatusNotFound, wantCode: `"code":40401`},
		{path: "/api/v1/commands/missing", service: &controlServiceStub{commandErr: control.ErrNotFound}, wantStatus: http.StatusNotFound, wantCode: `"code":40403`},
		{path: "/api/v1/commands?status=unknown", service: &controlServiceStub{}, wantStatus: http.StatusBadRequest, wantCode: `"code":40001`},
		{path: "/api/v1/commands", service: &controlServiceStub{listErr: errors.New("database unavailable")}, wantStatus: http.StatusInternalServerError, wantCode: `"code":50000`},
	}
	for _, tt := range tests {
		request := httptest.NewRequest(http.MethodGet, tt.path, nil)
		request.Header.Set("Authorization", "Bearer signed-token")
		response := httptest.NewRecorder()
		newControlTestRouter(tt.service).ServeHTTP(response, request)
		if response.Code != tt.wantStatus || !strings.Contains(response.Body.String(), tt.wantCode) {
			t.Fatalf("GET %s: status=%d body=%s", tt.path, response.Code, response.Body.String())
		}
	}
}
