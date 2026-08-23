package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chenluuo/smart-project/backend/internal/device"
)

type deviceServiceStub struct {
	listResult device.ListResult
	device     *device.Device
	listErr    error
	bindErr    error
	unbindErr  error
	statusErr  error
	ownerID    uint64
	deviceID   uint64
	filter     device.ListFilter
	bindInput  device.BindInput
}

func (s *deviceServiceStub) List(_ context.Context, ownerID uint64, filter device.ListFilter) (device.ListResult, error) {
	s.ownerID, s.filter = ownerID, filter
	return s.listResult, s.listErr
}

func (s *deviceServiceStub) Bind(_ context.Context, ownerID uint64, input device.BindInput) (*device.Device, error) {
	s.ownerID, s.bindInput = ownerID, input
	return s.device, s.bindErr
}

func (s *deviceServiceStub) Unbind(_ context.Context, ownerID, deviceID uint64) error {
	s.ownerID, s.deviceID = ownerID, deviceID
	return s.unbindErr
}

func (s *deviceServiceStub) Status(_ context.Context, ownerID, deviceID uint64) (*device.Device, error) {
	s.ownerID, s.deviceID = ownerID, deviceID
	return s.device, s.statusErr
}

func newDeviceTestRouter(service deviceService) http.Handler {
	return NewRouterWithServices("test", pingerStub{}, authServiceStub{}, nil, service)
}

func TestDeviceEndpointsRequireAuthentication(t *testing.T) {
	router := newDeviceTestRouter(&deviceServiceStub{})
	requests := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/devices"},
		{http.MethodPost, "/api/v1/devices/bind"},
		{http.MethodDelete, "/api/v1/devices/3/binding"},
		{http.MethodGet, "/api/v1/devices/3/status"},
	}
	for _, requestData := range requests {
		request := httptest.NewRequest(requestData.method, requestData.path, nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), `"code":40101`) {
			t.Fatalf("%s %s returned status %d and body %s", requestData.method, requestData.path, response.Code, response.Body.String())
		}
	}
}

func TestDeviceListParsesFiltersAndReturnsPage(t *testing.T) {
	battery, firmware := 87, "1.0.3"
	lastSeen := time.Date(2026, 8, 22, 8, 21, 0, 0, time.FixedZone("CST", 8*60*60))
	service := &deviceServiceStub{listResult: device.ListResult{
		Items: []device.ListItem{{Device: device.Device{ID: 3, SerialNo: "SN-3", Name: "土壤传感器", DeviceType: "SOIL", Status: device.StatusOnline, Battery: &battery, FirmwareVersion: &firmware, LastSeenAt: &lastSeen}, PlotID: 11}},
		Page:  2, PageSize: 5, Total: 8,
	}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/devices?plotId=11&status=online&type=SOIL&page=2&pageSize=5", nil)
	request.Header.Set("Authorization", "Bearer signed-token")
	response := httptest.NewRecorder()
	newDeviceTestRouter(service).ServeHTTP(response, request)

	if response.Code != http.StatusOK || service.ownerID != 7 || service.filter.PlotID == nil || *service.filter.PlotID != 11 || service.filter.Status == nil || *service.filter.Status != device.StatusOnline {
		t.Fatalf("status=%d ownerID=%d filter=%+v body=%s", response.Code, service.ownerID, service.filter, response.Body.String())
	}
	for _, want := range []string{`"deviceSn":"SN-3"`, `"plotId":11`, `"battery":87`, `"page":2`, `"total":8`} {
		if !strings.Contains(response.Body.String(), want) {
			t.Fatalf("body = %s, want %s", response.Body.String(), want)
		}
	}
}

func TestBindDevice(t *testing.T) {
	service := &deviceServiceStub{device: &device.Device{ID: 3, SerialNo: "SN-3", Status: device.StatusOffline}}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/devices/bind", strings.NewReader(`{"deviceSn":"SN-3","plotId":11,"name":"土壤传感器","type":"SOIL"}`))
	request.Header.Set("Authorization", "Bearer signed-token")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	newDeviceTestRouter(service).ServeHTTP(response, request)
	if response.Code != http.StatusOK || service.ownerID != 7 || service.bindInput.PlotID != 11 || service.bindInput.SerialNo != "SN-3" {
		t.Fatalf("status=%d ownerID=%d input=%+v body=%s", response.Code, service.ownerID, service.bindInput, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"status":"OFFLINE"`) {
		t.Fatalf("body = %s", response.Body.String())
	}
}

func TestDeviceStatusAndUnbindHideUnavailableDevices(t *testing.T) {
	tests := []struct {
		method     string
		path       string
		service    *deviceServiceStub
		wantStatus int
		wantCode   string
	}{
		{method: http.MethodGet, path: "/api/v1/devices/not-number/status", service: &deviceServiceStub{}, wantStatus: http.StatusBadRequest, wantCode: `"code":40001`},
		{method: http.MethodGet, path: "/api/v1/devices/3/status", service: &deviceServiceStub{statusErr: device.ErrNotFound}, wantStatus: http.StatusNotFound, wantCode: `"code":40402`},
		{method: http.MethodDelete, path: "/api/v1/devices/3/binding", service: &deviceServiceStub{unbindErr: device.ErrNotFound}, wantStatus: http.StatusNotFound, wantCode: `"code":40402`},
		{method: http.MethodGet, path: "/api/v1/devices/3/status", service: &deviceServiceStub{statusErr: errors.New("database unavailable")}, wantStatus: http.StatusInternalServerError, wantCode: `"code":50000`},
	}
	for _, tt := range tests {
		request := httptest.NewRequest(tt.method, tt.path, nil)
		request.Header.Set("Authorization", "Bearer signed-token")
		response := httptest.NewRecorder()
		newDeviceTestRouter(tt.service).ServeHTTP(response, request)
		if response.Code != tt.wantStatus || !strings.Contains(response.Body.String(), tt.wantCode) {
			t.Fatalf("%s %s: status=%d body=%s", tt.method, tt.path, response.Code, response.Body.String())
		}
	}
}

func TestUnbindDevice(t *testing.T) {
	service := &deviceServiceStub{}
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/devices/3/binding", nil)
	request.Header.Set("Authorization", "Bearer signed-token")
	response := httptest.NewRecorder()
	newDeviceTestRouter(service).ServeHTTP(response, request)
	if response.Code != http.StatusOK || service.ownerID != 7 || service.deviceID != 3 || !strings.Contains(response.Body.String(), `"data":true`) {
		t.Fatalf("status=%d ownerID=%d deviceID=%d body=%s", response.Code, service.ownerID, service.deviceID, response.Body.String())
	}
}

func TestDeviceStatusResponse(t *testing.T) {
	battery, signal, message := 45, 71, "网关重连中"
	service := &deviceServiceStub{device: &device.Device{ID: 3, Status: device.StatusReconnecting, Battery: &battery, Signal: &signal, StatusMessage: &message}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/devices/3/status", nil)
	request.Header.Set("Authorization", "Bearer signed-token")
	response := httptest.NewRecorder()
	newDeviceTestRouter(service).ServeHTTP(response, request)
	if response.Code != http.StatusOK || service.ownerID != 7 || service.deviceID != 3 {
		t.Fatalf("status=%d ownerID=%d deviceID=%d body=%s", response.Code, service.ownerID, service.deviceID, response.Body.String())
	}
	for _, want := range []string{`"status":"RECONNECTING"`, `"signal":71`, `"message":"网关重连中"`} {
		if !strings.Contains(response.Body.String(), want) {
			t.Fatalf("body = %s, want %s", response.Body.String(), want)
		}
	}
}
