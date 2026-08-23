package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chenluuo/smart-project/backend/internal/alert"
	"github.com/chenluuo/smart-project/backend/internal/device"
	"github.com/chenluuo/smart-project/backend/internal/plot"
	"github.com/chenluuo/smart-project/backend/internal/telemetry"
)

func newDashboardTestRouter(plots plotService, devices deviceService, alerts alertService, telemetry telemetryService) http.Handler {
	return NewRouterWithBackendServices("test", pingerStub{}, authServiceStub{}, plots, devices, nil, alerts, nil, nil, telemetry, "service-key")
}

func TestDashboardRequiresAuthentication(t *testing.T) {
	router := newDashboardTestRouter(&plotServiceStub{}, &deviceServiceStub{}, &alertServiceStub{}, &telemetryServiceStub{})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/overview", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), `"code":40101`) {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestDashboardOverviewAggregates(t *testing.T) {
	plotStub := &plotServiceStub{plots: []plot.Plot{{
		ID: 11, OwnerID: 7, Code: "A1", Name: "西侧棚", Status: plot.StatusActive,
	}}}
	deviceStub := &deviceServiceStub{listResult: device.ListResult{Total: 13}}
	alertStub := &alertServiceStub{listResults: map[alert.Status]alert.ListResult{
		alert.StatusActive:    {Total: 1},
		alert.StatusConfirmed: {Total: 1},
	}}
	telemetryStub := &telemetryServiceStub{latestList: []telemetry.Latest{}}

	router := newDashboardTestRouter(plotStub, deviceStub, alertStub, telemetryStub)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/overview", nil)
	request.Header.Set("Authorization", "Bearer signed-token")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if plotStub.ownerID != 7 {
		t.Fatalf("List ownerID = %d, want 7", plotStub.ownerID)
	}
	for _, want := range []string{
		`"sampleTime":null`,
		`"avgSoilMoisture":null`,
		`"avgTemperature":null`,
		`"deviceOnline":{"online":13,"total":13,"offline":0}`,
		`"alerts":{"active":1,"pendingConfirm":1}`,
		`"id":11`,
		`"code":"A1"`,
		`"soilMoisture":null`,
	} {
		if !strings.Contains(response.Body.String(), want) {
			t.Fatalf("body = %s, want it to contain %s", response.Body.String(), want)
		}
	}
}
