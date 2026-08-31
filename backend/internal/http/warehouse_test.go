package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chenluuo/smart-project/backend/internal/identity"
	"github.com/chenluuo/smart-project/backend/internal/trade"
	"github.com/shopspring/decimal"
)

type roleAuthStub struct {
	authServiceStub
	role string
}

func (a roleAuthStub) Authenticate(_ context.Context, token string) (identity.Claims, error) {
	if token != "signed-token" {
		return identity.Claims{}, identity.ErrInvalidToken
	}
	return identity.Claims{UserID: 7, AccountName: "tester", Role: a.role}, nil
}

type warehouseServiceStub struct {
	inbound        trade.InboundInput
	materialInput  trade.MaterialInput
	materialWrites int
}

func (s *warehouseServiceStub) ListMaterials(context.Context, trade.PageFilter) (trade.ListResult[trade.Material], error) {
	return trade.ListResult[trade.Material]{Items: []trade.Material{}, Page: 1, PageSize: 20}, nil
}
func (s *warehouseServiceStub) GetMaterial(context.Context, uint64) (*trade.Material, error) {
	return &trade.Material{ID: 1, Name: "番茄", Unit: "kg"}, nil
}
func (s *warehouseServiceStub) CreateMaterial(_ context.Context, in trade.MaterialInput) (*trade.Material, error) {
	s.materialWrites++
	s.materialInput = in
	return &trade.Material{ID: 1, Name: in.Name, Category: in.Category, Unit: in.Unit, Status: trade.StatusActive}, nil
}
func (s *warehouseServiceStub) UpdateMaterial(context.Context, uint64, trade.MaterialInput) (*trade.Material, error) {
	return &trade.Material{ID: 1}, nil
}
func (s *warehouseServiceStub) DeleteMaterial(context.Context, uint64) error { return nil }
func (s *warehouseServiceStub) ListWarehouses(context.Context, trade.PageFilter) (trade.ListResult[trade.Warehouse], error) {
	return trade.ListResult[trade.Warehouse]{Items: []trade.Warehouse{}, Page: 1, PageSize: 20}, nil
}
func (s *warehouseServiceStub) GetWarehouse(context.Context, uint64) (*trade.Warehouse, error) {
	return &trade.Warehouse{ID: 1}, nil
}
func (s *warehouseServiceStub) CreateWarehouse(context.Context, trade.WarehouseInput) (*trade.Warehouse, error) {
	return &trade.Warehouse{ID: 1}, nil
}
func (s *warehouseServiceStub) UpdateWarehouse(context.Context, uint64, trade.WarehouseInput) (*trade.Warehouse, error) {
	return &trade.Warehouse{ID: 1}, nil
}
func (s *warehouseServiceStub) DeleteWarehouse(context.Context, uint64) error { return nil }
func (s *warehouseServiceStub) ListStocks(context.Context, trade.StockFilter) (trade.ListResult[trade.StockView], error) {
	return trade.ListResult[trade.StockView]{Items: []trade.StockView{}, Page: 1, PageSize: 20}, nil
}
func (s *warehouseServiceStub) Inbound(_ context.Context, in trade.InboundInput) (*trade.InboundResult, error) {
	s.inbound = in
	return &trade.InboundResult{RecordID: 9, StockQuantity: in.Quantity}, nil
}
func (s *warehouseServiceStub) ListRecords(context.Context, trade.RecordFilter) (trade.ListResult[trade.RecordView], error) {
	return trade.ListResult[trade.RecordView]{Items: []trade.RecordView{}, Page: 1, PageSize: 20}, nil
}

type orderServiceStub struct{}

func (orderServiceStub) List(context.Context, trade.OrderFilter) (trade.ListResult[trade.OrderView], error) {
	return trade.ListResult[trade.OrderView]{Items: []trade.OrderView{}, Page: 1, PageSize: 20}, nil
}
func (orderServiceStub) Get(context.Context, uint64) (*trade.OrderView, error) {
	return &trade.OrderView{ID: 1, OrderNo: "INT-1"}, nil
}
func (orderServiceStub) CreateOrder(context.Context, uint64, *time.Time, string, []trade.OrderItemInput) (*trade.OrderHeader, error) {
	return &trade.OrderHeader{}, nil
}
func (orderServiceStub) Review(context.Context, uint64, bool) (*trade.OrderHeader, error) {
	return &trade.OrderHeader{}, nil
}
func (orderServiceStub) StartTrade(context.Context, uint64) (*trade.OrderHeader, error) {
	return &trade.OrderHeader{}, nil
}
func (orderServiceStub) Terminate(context.Context, uint64, bool) (*trade.OrderHeader, error) {
	return &trade.OrderHeader{}, nil
}
func (orderServiceStub) Confirm(context.Context, uint64, uint64, []trade.ConfirmItemInput) (*trade.OrderHeader, error) {
	return &trade.OrderHeader{}, nil
}

func TestWarehouseRoutesAuthenticationAndRoleGates(t *testing.T) {
	farmerService := &warehouseServiceStub{}
	farmerAuth := roleAuthStub{role: "FARMER"}
	farmerRouter := NewRouter("test", pingerStub{}, farmerAuth)
	registerWarehouseRoutes(farmerRouter, farmerAuth, farmerService, orderServiceStub{})

	request := httptest.NewRequest(http.MethodGet, "/api/v1/stocks", nil)
	response := httptest.NewRecorder()
	farmerRouter.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated stocks status=%d", response.Code)
	}
	request = httptest.NewRequest(http.MethodPost, "/api/v1/materials", strings.NewReader(`{"name":"番茄","category":"作物","unit":"kg"}`))
	request.Header.Set("Authorization", "Bearer signed-token")
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	farmerRouter.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || farmerService.materialWrites != 0 {
		t.Fatalf("farmer material write status=%d writes=%d", response.Code, farmerService.materialWrites)
	}
	request = httptest.NewRequest(http.MethodGet, "/api/v1/stocks", nil)
	request.Header.Set("Authorization", "Bearer signed-token")
	response = httptest.NewRecorder()
	farmerRouter.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("farmer stock read status=%d", response.Code)
	}
	farmAdminAuth := roleAuthStub{role: "FARM_ADMIN"}
	farmAdminRouter := NewRouter("test", pingerStub{}, farmAdminAuth)
	registerWarehouseRoutes(farmAdminRouter, farmAdminAuth, &warehouseServiceStub{}, orderServiceStub{})
	request = httptest.NewRequest(http.MethodGet, "/api/v1/stock-records", nil)
	request.Header.Set("Authorization", "Bearer signed-token")
	response = httptest.NewRecorder()
	farmAdminRouter.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("farm admin record read status=%d", response.Code)
	}
	request = httptest.NewRequest(http.MethodPost, "/api/v1/stocks/inbound", strings.NewReader(`{"warehouseId":1,"materialId":2,"quantity":"1.000","plotId":3}`))
	request.Header.Set("Authorization", "Bearer signed-token")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "farm-admin-inbound")
	response = httptest.NewRecorder()
	farmAdminRouter.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("farm admin inbound status=%d", response.Code)
	}

	technicianAuth := roleAuthStub{role: "TECHNICIAN"}
	technicianRouter := NewRouter("test", pingerStub{}, technicianAuth)
	registerWarehouseRoutes(technicianRouter, technicianAuth, &warehouseServiceStub{}, orderServiceStub{})
	request = httptest.NewRequest(http.MethodGet, "/api/v1/stocks", nil)
	request.Header.Set("Authorization", "Bearer signed-token")
	response = httptest.NewRecorder()
	technicianRouter.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("technician stock read status=%d", response.Code)
	}

	managerService := &warehouseServiceStub{}
	managerAuth := roleAuthStub{role: "WAREHOUSE_MANAGER"}
	managerRouter := NewRouter("test", pingerStub{}, managerAuth)
	registerWarehouseRoutes(managerRouter, managerAuth, managerService, orderServiceStub{})
	request = httptest.NewRequest(http.MethodPost, "/api/v1/materials", strings.NewReader(`{"name":"番茄","category":"作物","unit":"kg"}`))
	request.Header.Set("Authorization", "Bearer signed-token")
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	managerRouter.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || managerService.materialWrites != 1 {
		t.Fatalf("manager material write status=%d writes=%d body=%s", response.Code, managerService.materialWrites, response.Body.String())
	}

	customerService := &warehouseServiceStub{}
	customerAuth := roleAuthStub{role: "CUSTOMER"}
	customerRouter := NewRouter("test", pingerStub{}, customerAuth)
	registerWarehouseRoutes(customerRouter, customerAuth, customerService, orderServiceStub{})
	request = httptest.NewRequest(http.MethodGet, "/api/v1/market/materials", nil)
	request.Header.Set("Authorization", "Bearer signed-token")
	response = httptest.NewRecorder()
	customerRouter.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("customer market materials status=%d body=%s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodPost, "/api/v1/market/materials", strings.NewReader(`{"name":"土豆","unit":"t"}`))
	request.Header.Set("Authorization", "Bearer signed-token")
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	customerRouter.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || customerService.materialWrites != 1 || customerService.materialInput.Category != "农产品" || customerService.materialInput.Unit != "t" {
		t.Fatalf("customer market material status=%d input=%+v body=%s", response.Code, customerService.materialInput, response.Body.String())
	}
}

func TestWarehouseInboundRequiresIdempotencyAndUsesJWTActor(t *testing.T) {
	service := &warehouseServiceStub{}
	auth := roleAuthStub{role: "WAREHOUSE_MANAGER"}
	router := NewRouter("test", pingerStub{}, auth)
	registerWarehouseRoutes(router, auth, service, orderServiceStub{})
	body := `{"warehouseId":1,"materialId":2,"quantity":"500.125","plotId":3}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/stocks/inbound", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer signed-token")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("missing key status=%d", response.Code)
	}
	request = httptest.NewRequest(http.MethodPost, "/api/v1/stocks/inbound", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer signed-token")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "harvest-1")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("inbound status=%d body=%s", response.Code, response.Body.String())
	}
	if service.inbound.OperatorID != 7 || service.inbound.IdempotencyKey != "harvest-1" || !service.inbound.Quantity.Equal(decimal.RequireFromString("500.125")) {
		t.Fatalf("inbound=%+v", service.inbound)
	}
}
