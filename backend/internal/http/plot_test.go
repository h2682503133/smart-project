package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chenluuo/smart-project/backend/internal/plot"
	"github.com/chenluuo/smart-project/backend/internal/shared/persistence"
	"github.com/shopspring/decimal"
)

type plotServiceStub struct {
	plots   []plot.Plot
	plot    *plot.Plot
	listErr error
	getErr  error
	ownerID uint64
	plotID  uint64
}

func (s *plotServiceStub) List(_ context.Context, ownerID uint64) ([]plot.Plot, error) {
	s.ownerID = ownerID
	return s.plots, s.listErr
}

func (s *plotServiceStub) Get(_ context.Context, ownerID, plotID uint64) (*plot.Plot, error) {
	s.ownerID = ownerID
	s.plotID = plotID
	return s.plot, s.getErr
}

func TestPlotEndpointsRequireAuthentication(t *testing.T) {
	router := NewRouterWithPlotService("test", pingerStub{}, authServiceStub{}, &plotServiceStub{})
	for _, path := range []string{"/api/v1/plots", "/api/v1/plots/11"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), `"code":40101`) {
			t.Fatalf("GET %s returned status %d and body %s", path, response.Code, response.Body.String())
		}
	}
}

func TestPlotListReturnsOnlyAuthenticatedOwnersPlots(t *testing.T) {
	updatedAt := time.Date(2026, 8, 22, 8, 21, 0, 0, time.FixedZone("CST", 8*60*60))
	service := &plotServiceStub{plots: []plot.Plot{{
		ID: 11, OwnerID: 7, Code: "A1", Name: "西侧棚", Status: plot.StatusActive,
		Auditable: persistence.Auditable{UpdatedAt: updatedAt},
	}}}
	router := NewRouterWithPlotService("test", pingerStub{}, authServiceStub{}, service)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/plots", nil)
	request.Header.Set("Authorization", "Bearer signed-token")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if service.ownerID != 7 {
		t.Fatalf("List() ownerID = %d, want 7", service.ownerID)
	}
	for _, want := range []string{`"code":0`, `"id":11`, `"code":"A1"`, `"name":"西侧棚"`, `"soilMoisture":null`, `"alertCount":0`} {
		if !strings.Contains(response.Body.String(), want) {
			t.Fatalf("body = %s, want it to contain %s", response.Body.String(), want)
		}
	}
	if strings.Contains(response.Body.String(), "ownerId") {
		t.Fatalf("response leaked owner ID: %s", response.Body.String())
	}
}

func TestPlotDetail(t *testing.T) {
	cropName := "番茄"
	area := decimal.RequireFromString("320.50")
	createdAt := time.Date(2026, 8, 1, 9, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	service := &plotServiceStub{plot: &plot.Plot{
		ID: 11, OwnerID: 7, Code: "A3", Name: "东侧棚", CropType: &cropName, Area: &area,
		Status: plot.StatusActive, Auditable: persistence.Auditable{CreatedAt: createdAt},
	}}
	router := NewRouterWithPlotService("test", pingerStub{}, authServiceStub{}, service)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/plots/11", nil)
	request.Header.Set("Authorization", "Bearer signed-token")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK || service.ownerID != 7 || service.plotID != 11 {
		t.Fatalf("status = %d, ownerID = %d, plotID = %d, body = %s", response.Code, service.ownerID, service.plotID, response.Body.String())
	}
	for _, want := range []string{`"id":11`, `"code":"A3"`, `"cropName":"番茄"`, `"area":320.5`} {
		if !strings.Contains(response.Body.String(), want) {
			t.Fatalf("body = %s, want it to contain %s", response.Body.String(), want)
		}
	}
	if strings.Contains(response.Body.String(), "ownerId") {
		t.Fatalf("response leaked owner ID: %s", response.Body.String())
	}
}

func TestPlotDetailValidatesIDAndHidesUnavailablePlots(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		service    *plotServiceStub
		wantStatus int
		wantCode   string
	}{
		{name: "invalid ID", path: "/api/v1/plots/not-a-number", service: &plotServiceStub{}, wantStatus: http.StatusBadRequest, wantCode: `"code":40001`},
		{name: "missing or foreign plot", path: "/api/v1/plots/99", service: &plotServiceStub{getErr: plot.ErrNotFound}, wantStatus: http.StatusNotFound, wantCode: `"code":40401`},
		{name: "service failure", path: "/api/v1/plots/11", service: &plotServiceStub{getErr: errors.New("database unavailable")}, wantStatus: http.StatusInternalServerError, wantCode: `"code":50000`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := NewRouterWithPlotService("test", pingerStub{}, authServiceStub{}, tt.service)
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
