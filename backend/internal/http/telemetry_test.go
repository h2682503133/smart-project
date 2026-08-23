package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chenluuo/smart-project/backend/internal/alert"
	"github.com/chenluuo/smart-project/backend/internal/device"
	"github.com/chenluuo/smart-project/backend/internal/plot"
	"github.com/chenluuo/smart-project/backend/internal/telemetry"
)

type telemetryServiceStub struct {
	latest       *telemetry.Latest
	latestList   []telemetry.Latest
	history      *telemetry.History
	err          error
	plotID       uint64
	plotIDs      []uint64
	historyQuery telemetry.HistoryQuery
}

func (s *telemetryServiceStub) LatestByPlot(_ context.Context, plotID uint64) (*telemetry.Latest, error) {
	s.plotID = plotID
	return s.latest, s.err
}

func (s *telemetryServiceStub) LatestByPlots(_ context.Context, plotIDs []uint64) ([]telemetry.Latest, error) {
	s.plotIDs = plotIDs
	return s.latestList, s.err
}

func (s *telemetryServiceStub) History(_ context.Context, q telemetry.HistoryQuery) (*telemetry.History, error) {
	s.historyQuery = q
	return s.history, s.err
}

func newTelemetryTestRouter(plots plotService, devices deviceService, telemetry telemetryService) http.Handler {
	return NewRouterWithBackendServices("test", pingerStub{}, authServiceStub{}, plots, devices, nil, nil, nil, nil, telemetry, "service-key")
}

func TestPlotTelemetryRequiresAuthentication(t *testing.T) {
	router := newTelemetryTestRouter(&plotServiceStub{}, &deviceServiceStub{}, &telemetryServiceStub{})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/plots/11/telemetry/latest", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), `"code":40101`) {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestPlotTelemetryValidatesIDAndOwnership(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		plots      plotService
		wantStatus int
		wantCode   string
	}{
		{name: "invalid plotId", path: "/api/v1/plots/not-a-number/telemetry/latest", plots: &plotServiceStub{}, wantStatus: http.StatusBadRequest, wantCode: `"code":40001`},
		{name: "foreign or missing plot", path: "/api/v1/plots/99/telemetry/latest", plots: &plotServiceStub{getErr: plot.ErrNotFound}, wantStatus: http.StatusNotFound, wantCode: `"code":40401`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := newTelemetryTestRouter(tt.plots, &deviceServiceStub{}, &telemetryServiceStub{})
			request := httptest.NewRequest(http.MethodGet, tt.path, nil)
			request.Header.Set("Authorization", "Bearer signed-token")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tt.wantStatus || !strings.Contains(response.Body.String(), tt.wantCode) {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestPlotTelemetryReturnsLatestAndSourceDevices(t *testing.T) {
	soil := telemetry.MetricValue{Value: 27.8, Unit: "%"}
	temp := telemetry.MetricValue{Value: 26.8, Unit: "°C"}
	light := telemetry.MetricValue{Value: 920, Unit: "lx"}
	sampleTime := time.Date(2026, 8, 22, 8, 21, 0, 0, time.FixedZone("CST", 8*60*60))
	telemetryStub := &telemetryServiceStub{latest: &telemetry.Latest{
		PlotID: 11, SampleTime: sampleTime, SoilMoisture: &soil, Temperature: &temp, Light: &light,
	}}
	plotStub := &plotServiceStub{plot: &plot.Plot{ID: 11, OwnerID: 7, Code: "A3", Name: "东侧棚", Status: plot.StatusActive}}
	battery := 87
	deviceStub := &deviceServiceStub{listResult: device.ListResult{Items: []device.ListItem{{
		Device: device.Device{ID: 3, Name: "土壤传感器 03", Status: device.StatusOnline, Battery: &battery},
		PlotID: 11,
	}}}}

	router := newTelemetryTestRouter(plotStub, deviceStub, telemetryStub)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/plots/11/telemetry/latest", nil)
	request.Header.Set("Authorization", "Bearer signed-token")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if telemetryStub.plotID != 11 {
		t.Fatalf("LatestByPlot plotID = %d, want 11", telemetryStub.plotID)
	}
	for _, want := range []string{
		`"plotId":11`,
		`"soilMoisture":{"value":27.8,"unit":"%"}`,
		`"temperature":{"value":26.8,"unit":"°C"}`,
		`"light":{"value":920,"unit":"lx"}`,
		`"name":"土壤传感器 03"`,
		`"status":"ONLINE"`,
		`"battery":87`,
	} {
		if !strings.Contains(response.Body.String(), want) {
			t.Fatalf("body = %s, want it to contain %s", response.Body.String(), want)
		}
	}
}

func newTelemetryListTestRouter(plots plotService, alerts alertService, telemetry telemetryService) http.Handler {
	return NewRouterWithBackendServices("test", pingerStub{}, authServiceStub{}, plots, nil, nil, alerts, nil, nil, telemetry, "service-key")
}

func TestTelemetryListRequiresAuthentication(t *testing.T) {
	router := newTelemetryListTestRouter(&plotServiceStub{}, &alertServiceStub{}, &telemetryServiceStub{})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/telemetry/latest", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), `"code":40101`) {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestTelemetryListReturnsAllPlotsWithStatus(t *testing.T) {
	plotStub := &plotServiceStub{plots: []plot.Plot{
		{ID: 11, OwnerID: 7, Code: "A1", Name: "西侧棚", Status: plot.StatusActive},
		{ID: 12, OwnerID: 7, Code: "A3", Name: "东侧棚", Status: plot.StatusActive},
	}}
	telemetryStub := &telemetryServiceStub{latestList: []telemetry.Latest{
		{PlotID: 12, SoilMoisture: &telemetry.MetricValue{Value: 27.8, Unit: "%"}, Light: &telemetry.MetricValue{Value: 800, Unit: "lx"}},
	}}
	alertStub := &alertServiceStub{listResult: alert.ListResult{Items: []alert.ListItem{{PlotID: 12}}}}

	router := newTelemetryListTestRouter(plotStub, alertStub, telemetryStub)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/telemetry/latest", nil)
	request.Header.Set("Authorization", "Bearer signed-token")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if len(telemetryStub.plotIDs) != 2 {
		t.Fatalf("LatestByPlots plotIDs = %v, want 2 plot IDs", telemetryStub.plotIDs)
	}
	for _, want := range []string{
		`"plotId":11`, `"plotCode":"A1"`, `"status":"NORMAL"`, `"soilMoisture":null`,
		`"plotId":12`, `"plotCode":"A3"`, `"status":"ALERT"`, `"soilMoisture":27.8`, `"light":800`,
	} {
		if !strings.Contains(response.Body.String(), want) {
			t.Fatalf("body = %s, want it to contain %s", response.Body.String(), want)
		}
	}
}

func newTelemetryHistoryTestRouter(plots plotService, telemetry telemetryService) http.Handler {
	return NewRouterWithBackendServices("test", pingerStub{}, authServiceStub{}, plots, nil, nil, nil, nil, nil, telemetry, "service-key")
}

func TestTelemetryHistoryRequiresAuthentication(t *testing.T) {
	router := newTelemetryHistoryTestRouter(&plotServiceStub{}, &telemetryServiceStub{})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/telemetry/history?plotId=11&metric=soilMoisture&range=7d", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), `"code":40101`) {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestTelemetryHistoryValidatesParams(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		plots      plotService
		wantStatus int
		wantCode   string
	}{
		{name: "missing plotId", path: "/api/v1/telemetry/history?metric=soilMoisture&range=7d", plots: &plotServiceStub{}, wantStatus: http.StatusBadRequest, wantCode: `"code":40001`},
		{name: "invalid metric", path: "/api/v1/telemetry/history?plotId=11&metric=wind&range=7d", plots: &plotServiceStub{plot: &plot.Plot{ID: 11, OwnerID: 7, Status: plot.StatusActive}}, wantStatus: http.StatusBadRequest, wantCode: `"code":40001`},
		{name: "foreign plot", path: "/api/v1/telemetry/history?plotId=99&metric=soilMoisture&range=7d", plots: &plotServiceStub{getErr: plot.ErrNotFound}, wantStatus: http.StatusNotFound, wantCode: `"code":40401`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := newTelemetryHistoryTestRouter(tt.plots, &telemetryServiceStub{})
			request := httptest.NewRequest(http.MethodGet, tt.path, nil)
			request.Header.Set("Authorization", "Bearer signed-token")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tt.wantStatus || !strings.Contains(response.Body.String(), tt.wantCode) {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestTelemetryHistoryReturnsPoints(t *testing.T) {
	plotStub := &plotServiceStub{plot: &plot.Plot{ID: 11, OwnerID: 7, Code: "A3", Status: plot.StatusActive}}
	sample := time.Date(2026, 8, 16, 0, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	telemetryStub := &telemetryServiceStub{history: &telemetry.History{
		PlotID: 11, Metric: "soilMoisture",
		Points: []telemetry.HistoryPoint{{Time: sample, Avg: 34.2, Min: 28.5, Max: 39.1}},
	}}

	router := newTelemetryHistoryTestRouter(plotStub, telemetryStub)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/telemetry/history?plotId=11&metric=soilMoisture&startTime=2026-08-15T00:00:00Z&endTime=2026-08-22T00:00:00Z&interval=1d", nil)
	request.Header.Set("Authorization", "Bearer signed-token")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if telemetryStub.historyQuery.PlotID != 11 || telemetryStub.historyQuery.Metric != "soilMoisture" || telemetryStub.historyQuery.Interval != 24*time.Hour {
		t.Fatalf("historyQuery = %+v", telemetryStub.historyQuery)
	}
	for _, want := range []string{
		`"plotId":11`, `"metric":"soilMoisture"`, `"unit":"%"`, `"avg":34.2`, `"min":28.5`, `"max":39.1`,
	} {
		if !strings.Contains(response.Body.String(), want) {
			t.Fatalf("body = %s, want it to contain %s", response.Body.String(), want)
		}
	}
}

func TestTelemetryHistoryAcceptsLight(t *testing.T) {
	plotStub := &plotServiceStub{plot: &plot.Plot{ID: 11, OwnerID: 7, Code: "A3", Status: plot.StatusActive}}
	telemetryStub := &telemetryServiceStub{history: &telemetry.History{PlotID: 11, Metric: "light", Points: []telemetry.HistoryPoint{}}}
	router := newTelemetryHistoryTestRouter(plotStub, telemetryStub)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/telemetry/history?plotId=11&metric=light&range=1h", nil)
	request.Header.Set("Authorization", "Bearer signed-token")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"unit":"lx"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
