package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/chenluuo/smart-project/backend/internal/trade"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

type warehouseService interface {
	ListMaterials(context.Context, trade.PageFilter) (trade.ListResult[trade.Material], error)
	GetMaterial(context.Context, uint64) (*trade.Material, error)
	CreateMaterial(context.Context, trade.MaterialInput) (*trade.Material, error)
	UpdateMaterial(context.Context, uint64, trade.MaterialInput) (*trade.Material, error)
	DeleteMaterial(context.Context, uint64) error
	ListWarehouses(context.Context, trade.PageFilter) (trade.ListResult[trade.Warehouse], error)
	GetWarehouse(context.Context, uint64) (*trade.Warehouse, error)
	CreateWarehouse(context.Context, trade.WarehouseInput) (*trade.Warehouse, error)
	UpdateWarehouse(context.Context, uint64, trade.WarehouseInput) (*trade.Warehouse, error)
	DeleteWarehouse(context.Context, uint64) error
	ListStocks(context.Context, trade.StockFilter) (trade.ListResult[trade.StockView], error)
	Inbound(context.Context, trade.InboundInput) (*trade.InboundResult, error)
	ListRecords(context.Context, trade.RecordFilter) (trade.ListResult[trade.RecordView], error)
}

// orderService 意向订单服务（查询 + 业务流转）。
type orderService interface {
	List(context.Context, trade.OrderFilter) (trade.ListResult[trade.OrderView], error)
	Get(context.Context, uint64) (*trade.OrderView, error)
	CreateOrder(context.Context, uint64, *time.Time, string, []trade.OrderItemInput) (*trade.OrderHeader, error)
	Review(context.Context, uint64, bool) (*trade.OrderHeader, error)
	StartTrade(context.Context, uint64) (*trade.OrderHeader, error)
	Terminate(context.Context, uint64, bool) (*trade.OrderHeader, error)
	Confirm(context.Context, uint64, uint64, []trade.ConfirmItemInput) (*trade.OrderHeader, error)
}

type warehouseHandler struct {
	service warehouseService
	orders  orderService
}

func registerWarehouseRoutes(router *gin.Engine, auth authService, service warehouseService, orders orderService) {
	h := warehouseHandler{service: service, orders: orders}
	api := router.Group("/api/v1", jwtAuthentication(auth))
	warehouseRead := requireAnyRole("WAREHOUSE_MANAGER", "SYSTEM_ADMIN")
	warehouseWrite := requireAnyRole("WAREHOUSE_MANAGER", "SYSTEM_ADMIN")
	api.GET("/materials", warehouseRead, h.listMaterials)
	api.GET("/materials/:id", warehouseRead, h.getMaterial)
	api.POST("/materials", warehouseWrite, h.createMaterial)
	api.PUT("/materials/:id", warehouseWrite, h.updateMaterial)
	api.DELETE("/materials/:id", warehouseWrite, h.deleteMaterial)
	api.GET("/warehouses", warehouseRead, h.listWarehouses)
	api.GET("/warehouses/:id", warehouseRead, h.getWarehouse)
	api.POST("/warehouses", warehouseWrite, h.createWarehouse)
	api.PUT("/warehouses/:id", warehouseWrite, h.updateWarehouse)
	api.DELETE("/warehouses/:id", warehouseWrite, h.deleteWarehouse)
	api.GET("/stocks", warehouseRead, h.listStocks)
	// 收获入库：FARMER 可写（收获通知），仓库管理员/系统管理员可写
	api.POST("/stocks/inbound", requireAnyRole("FARMER", "WAREHOUSE_MANAGER", "SYSTEM_ADMIN"), h.inbound)
	api.GET("/stock-records", warehouseRead, h.listRecords)
	api.GET("/market/materials", h.listMarketMaterials)
	api.POST("/market/materials", requireAnyRole("CUSTOMER"), h.createMarketMaterial)
	// 意向订单：登录即可查（FARMER/ADMIN/WAREHOUSE_MANAGER 全量，CUSTOMER 仅自己，handler 内按角色过滤）
	api.GET("/orders", h.listOrders)
	api.GET("/orders/:id", h.getOrder)
	// 意向订单业务流转
	api.POST("/orders", requireAnyRole("CUSTOMER"), h.createOrder)
	api.POST("/orders/:id/review", requireAnyRole("WAREHOUSE_MANAGER", "SYSTEM_ADMIN"), h.reviewOrder)
	api.POST("/orders/:id/start-trade", requireAnyRole("WAREHOUSE_MANAGER", "SYSTEM_ADMIN"), h.startTrade)
	api.POST("/orders/:id/terminate", h.terminateOrder)
	api.POST("/orders/:id/confirm", requireAnyRole("WAREHOUSE_MANAGER", "SYSTEM_ADMIN"), h.confirmOrder)
}

type materialRequest struct {
	Name     string             `json:"name"`
	Category string             `json:"category"`
	Unit     string             `json:"unit"`
	Spec     *string            `json:"spec"`
	Status   trade.MasterStatus `json:"status"`
}
type marketMaterialRequest struct {
	Name string `json:"name"`
	Unit string `json:"unit"`
}
type marketMaterialView struct {
	ID                uint64          `json:"id"`
	Name              string          `json:"name"`
	Category          string          `json:"category"`
	Unit              string          `json:"unit"`
	Spec              *string         `json:"spec,omitempty"`
	AvailableQuantity decimal.Decimal `json:"availableQuantity"`
	TotalQuantity     decimal.Decimal `json:"totalQuantity"`
}
type warehouseRequest struct {
	Name     string             `json:"name"`
	Location string             `json:"location"`
	Status   trade.MasterStatus `json:"status"`
}
type inboundRequest struct {
	WarehouseID uint64       `json:"warehouseId"`
	MaterialID  uint64       `json:"materialId"`
	Quantity    decimalInput `json:"quantity"`
	PlotID      uint64       `json:"plotId"`
	Remark      *string      `json:"remark"`
}
type decimalInput struct{ decimal.Decimal }

func (v *decimalInput) UnmarshalJSON(raw []byte) error {
	text := strings.TrimSpace(string(raw))
	if len(text) >= 2 && text[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return err
		}
		text = s
	}
	parsed, err := decimal.NewFromString(text)
	if err != nil {
		return err
	}
	v.Decimal = parsed
	return nil
}

func (h warehouseHandler) listMaterials(c *gin.Context) {
	f, ok := pageFilter(c)
	if !ok {
		return
	}
	result, err := h.service.ListMaterials(c.Request.Context(), f)
	respondWarehouseResult(c, result, err)
}
func (h warehouseHandler) getMaterial(c *gin.Context) {
	id, ok := warehousePathID(c)
	if !ok {
		return
	}
	result, err := h.service.GetMaterial(c.Request.Context(), id)
	respondWarehouseResult(c, result, err)
}
func (h warehouseHandler) createMaterial(c *gin.Context) {
	var r materialRequest
	if !bindWarehouseJSON(c, &r) {
		return
	}
	result, err := h.service.CreateMaterial(c.Request.Context(), trade.MaterialInput{Name: r.Name, Category: r.Category, Unit: r.Unit, Spec: r.Spec, Status: r.Status})
	respondWarehouseCreated(c, result, err)
}
func (h warehouseHandler) listMarketMaterials(c *gin.Context) {
	f, ok := pageFilter(c)
	if !ok {
		return
	}
	active := trade.StatusActive
	f.Status = &active
	materials, err := h.service.ListMaterials(c.Request.Context(), f)
	if err != nil {
		respondWarehouseError(c, err)
		return
	}
	stocks, err := h.service.ListStocks(c.Request.Context(), trade.StockFilter{Page: 1, PageSize: 100})
	if err != nil {
		respondWarehouseError(c, err)
		return
	}
	totalByMaterial := make(map[uint64]decimal.Decimal, len(stocks.Items))
	availableByMaterial := make(map[uint64]decimal.Decimal, len(stocks.Items))
	for _, stock := range stocks.Items {
		totalByMaterial[stock.MaterialID] = totalByMaterial[stock.MaterialID].Add(stock.TotalQuantity)
		availableByMaterial[stock.MaterialID] = availableByMaterial[stock.MaterialID].Add(stock.AvailableQuantity)
	}
	items := make([]marketMaterialView, 0, len(materials.Items))
	for _, material := range materials.Items {
		items = append(items, marketMaterialView{
			ID: material.ID, Name: material.Name, Category: material.Category, Unit: material.Unit, Spec: material.Spec,
			TotalQuantity: totalByMaterial[material.ID], AvailableQuantity: availableByMaterial[material.ID],
		})
	}
	respondSuccess(c, http.StatusOK, gin.H{"items": items, "page": materials.Page, "pageSize": materials.PageSize, "total": materials.Total})
}
func (h warehouseHandler) createMarketMaterial(c *gin.Context) {
	var r marketMaterialRequest
	if !bindWarehouseJSON(c, &r) {
		return
	}
	r.Name = strings.TrimSpace(r.Name)
	r.Unit = strings.ToLower(strings.TrimSpace(r.Unit))
	if r.Unit != "kg" && r.Unit != "t" {
		respondError(c, http.StatusBadRequest, 40001, "参数错误：unit 仅支持 kg 或 t")
		return
	}
	result, err := h.service.CreateMaterial(c.Request.Context(), trade.MaterialInput{Name: r.Name, Category: "农产品", Unit: r.Unit})
	if err == nil {
		respondSuccess(c, http.StatusCreated, result)
		return
	}
	if !errors.Is(err, trade.ErrConflict) {
		respondWarehouseError(c, err)
		return
	}

	active := trade.StatusActive
	materials, listErr := h.service.ListMaterials(c.Request.Context(), trade.PageFilter{Keyword: r.Name, Status: &active, Page: 1, PageSize: 100})
	if listErr != nil {
		respondWarehouseError(c, listErr)
		return
	}
	for _, material := range materials.Items {
		if strings.EqualFold(strings.TrimSpace(material.Name), r.Name) {
			respondSuccess(c, http.StatusOK, material)
			return
		}
	}
	respondWarehouseError(c, err)
}
func (h warehouseHandler) updateMaterial(c *gin.Context) {
	id, ok := warehousePathID(c)
	if !ok {
		return
	}
	var r materialRequest
	if !bindWarehouseJSON(c, &r) {
		return
	}
	result, err := h.service.UpdateMaterial(c.Request.Context(), id, trade.MaterialInput{Name: r.Name, Category: r.Category, Unit: r.Unit, Spec: r.Spec, Status: r.Status})
	respondWarehouseResult(c, result, err)
}
func (h warehouseHandler) deleteMaterial(c *gin.Context) {
	id, ok := warehousePathID(c)
	if !ok {
		return
	}
	if err := h.service.DeleteMaterial(c.Request.Context(), id); err != nil {
		respondWarehouseError(c, err)
		return
	}
	respondSuccess(c, http.StatusOK, gin.H{"id": id})
}

func (h warehouseHandler) listWarehouses(c *gin.Context) {
	f, ok := pageFilter(c)
	if !ok {
		return
	}
	result, err := h.service.ListWarehouses(c.Request.Context(), f)
	respondWarehouseResult(c, result, err)
}
func (h warehouseHandler) getWarehouse(c *gin.Context) {
	id, ok := warehousePathID(c)
	if !ok {
		return
	}
	result, err := h.service.GetWarehouse(c.Request.Context(), id)
	respondWarehouseResult(c, result, err)
}
func (h warehouseHandler) createWarehouse(c *gin.Context) {
	var r warehouseRequest
	if !bindWarehouseJSON(c, &r) {
		return
	}
	result, err := h.service.CreateWarehouse(c.Request.Context(), trade.WarehouseInput{Name: r.Name, Location: r.Location, Status: r.Status})
	respondWarehouseCreated(c, result, err)
}
func (h warehouseHandler) updateWarehouse(c *gin.Context) {
	id, ok := warehousePathID(c)
	if !ok {
		return
	}
	var r warehouseRequest
	if !bindWarehouseJSON(c, &r) {
		return
	}
	result, err := h.service.UpdateWarehouse(c.Request.Context(), id, trade.WarehouseInput{Name: r.Name, Location: r.Location, Status: r.Status})
	respondWarehouseResult(c, result, err)
}
func (h warehouseHandler) deleteWarehouse(c *gin.Context) {
	id, ok := warehousePathID(c)
	if !ok {
		return
	}
	if err := h.service.DeleteWarehouse(c.Request.Context(), id); err != nil {
		respondWarehouseError(c, err)
		return
	}
	respondSuccess(c, http.StatusOK, gin.H{"id": id})
}

func (h warehouseHandler) listStocks(c *gin.Context) {
	page, size, ok := pagination(c)
	if !ok {
		return
	}
	f := trade.StockFilter{Page: page, PageSize: size}
	if !optionalUint(c, "warehouseId", &f.WarehouseID) || !optionalUint(c, "materialId", &f.MaterialID) {
		return
	}
	result, err := h.service.ListStocks(c.Request.Context(), f)
	respondWarehouseResult(c, result, err)
}

func (h warehouseHandler) inbound(c *gin.Context) {
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if key == "" || len(key) > 128 {
		respondError(c, http.StatusBadRequest, 40001, "参数错误：Idempotency-Key 必填且不能超过 128 字符")
		return
	}
	claims, ok := authenticatedClaims(c)
	if !ok {
		return
	}
	var r inboundRequest
	if !bindWarehouseJSON(c, &r) {
		return
	}
	result, err := h.service.Inbound(c.Request.Context(), trade.InboundInput{WarehouseID: r.WarehouseID, MaterialID: r.MaterialID, Quantity: r.Quantity.Decimal, PlotID: r.PlotID, Remark: r.Remark, IdempotencyKey: key, OperatorID: claims.UserID})
	respondWarehouseCreated(c, result, err)
}

func (h warehouseHandler) listRecords(c *gin.Context) {
	page, size, ok := pagination(c)
	if !ok {
		return
	}
	f := trade.RecordFilter{Page: page, PageSize: size}
	if !optionalUint(c, "warehouseId", &f.WarehouseID) || !optionalUint(c, "materialId", &f.MaterialID) || !optionalUint(c, "plotId", &f.PlotID) {
		return
	}
	if raw := strings.ToUpper(strings.TrimSpace(c.Query("type"))); raw != "" {
		value := trade.RecordType(raw)
		if value != trade.RecordTypeIn && value != trade.RecordTypeOut {
			respondError(c, http.StatusBadRequest, 40001, "参数错误：type 必须为 IN 或 OUT")
			return
		}
		f.Type = &value
	}
	if !optionalTime(c, "startAt", &f.StartAt) || !optionalTime(c, "endAt", &f.EndAt) {
		return
	}
	if f.StartAt != nil && f.EndAt != nil && f.StartAt.After(*f.EndAt) {
		respondError(c, http.StatusBadRequest, 40001, "参数错误：startAt 不能晚于 endAt")
		return
	}
	result, err := h.service.ListRecords(c.Request.Context(), f)
	respondWarehouseResult(c, result, err)
}

func pageFilter(c *gin.Context) (trade.PageFilter, bool) {
	page, size, ok := pagination(c)
	if !ok {
		return trade.PageFilter{}, false
	}
	f := trade.PageFilter{Keyword: strings.TrimSpace(c.Query("keyword")), Page: page, PageSize: size}
	if raw := strings.ToUpper(strings.TrimSpace(c.Query("status"))); raw != "" {
		status := trade.MasterStatus(raw)
		if status != trade.StatusActive && status != trade.StatusDisabled {
			respondError(c, http.StatusBadRequest, 40001, "参数错误：status 必须为 ACTIVE 或 DISABLED")
			return f, false
		}
		f.Status = &status
	}
	return f, true
}
func warehousePathID(c *gin.Context) (uint64, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		respondError(c, http.StatusBadRequest, 40001, "参数错误：id 必须为正整数")
		return 0, false
	}
	return id, true
}
func optionalUint(c *gin.Context, name string, target **uint64) bool {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return true
	}
	v, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || v == 0 {
		respondError(c, http.StatusBadRequest, 40001, "参数错误："+name+" 必须为正整数")
		return false
	}
	*target = &v
	return true
}
func optionalTime(c *gin.Context, name string, target **time.Time) bool {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return true
	}
	v, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		respondError(c, http.StatusBadRequest, 40001, "参数错误："+name+" 必须为 RFC3339 时间")
		return false
	}
	*target = &v
	return true
}
func bindWarehouseJSON(c *gin.Context, target any) bool {
	if err := c.ShouldBindJSON(target); err != nil {
		respondError(c, http.StatusBadRequest, 40001, "参数错误：请求体格式不正确")
		return false
	}
	return true
}
func (h warehouseHandler) listOrders(c *gin.Context) {
	claims, ok := authenticatedClaims(c)
	if !ok {
		return
	}
	page, size, ok := pagination(c)
	if !ok {
		return
	}
	f := trade.OrderFilter{Page: page, PageSize: size}
	if raw := strings.ToUpper(strings.TrimSpace(c.Query("status"))); raw != "" {
		status := trade.OrderStatus(raw)
		if !validOrderStatus(status) {
			respondError(c, http.StatusBadRequest, 40001, "参数错误：status 不合法")
			return
		}
		f.Status = &status
	}
	// CUSTOMER 仅能看自己的意向；其余角色（FARMER/ADMIN/WAREHOUSE_MANAGER）看全部
	if claims.Role == "CUSTOMER" {
		f.CustomerID = &claims.UserID
	}
	result, err := h.orders.List(c.Request.Context(), f)
	if err != nil {
		respondWarehouseError(c, err)
		return
	}
	respondSuccess(c, http.StatusOK, result)
}

func (h warehouseHandler) getOrder(c *gin.Context) {
	claims, ok := authenticatedClaims(c)
	if !ok {
		return
	}
	id, ok := warehousePathID(c)
	if !ok {
		return
	}
	result, err := h.orders.Get(c.Request.Context(), id)
	if err != nil {
		respondWarehouseError(c, err)
		return
	}
	// CUSTOMER 仅能看自己的意向
	if claims.Role == "CUSTOMER" && result.CustomerID != claims.UserID {
		respondError(c, http.StatusNotFound, 40401, "仓储资源不存在")
		return
	}
	respondSuccess(c, http.StatusOK, result)
}

func validOrderStatus(v trade.OrderStatus) bool {
	switch v {
	case trade.OrderStatusPending, trade.OrderStatusApproved, trade.OrderStatusTrading,
		trade.OrderStatusConfirmed, trade.OrderStatusClosed, trade.OrderStatusRejected:
		return true
	default:
		return false
	}
}

func respondWarehouseCreated(c *gin.Context, result any, err error) {
	if err != nil {
		respondWarehouseError(c, err)
		return
	}
	respondSuccess(c, http.StatusCreated, result)
}
func respondWarehouseResult(c *gin.Context, result any, err error) {
	if err != nil {
		respondWarehouseError(c, err)
		return
	}
	respondSuccess(c, http.StatusOK, result)
}
func respondWarehouseError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, trade.ErrInvalidInput):
		respondError(c, http.StatusBadRequest, 40001, "参数错误")
	case errors.Is(err, trade.ErrNotFound):
		respondError(c, http.StatusNotFound, 40401, "仓储资源不存在")
	case errors.Is(err, trade.ErrConflict), errors.Is(err, trade.ErrInsufficientStock):
		respondError(c, http.StatusConflict, 40901, "仓储资源冲突或库存不足")
	default:
		respondError(c, http.StatusInternalServerError, 50000, "服务器内部错误")
	}
}

type orderItemRequest struct {
	MaterialID uint64       `json:"materialId"`
	Quantity   decimalInput `json:"quantity"`
}

type createOrderRequest struct {
	ExpectedTime *time.Time         `json:"expectedTime"`
	Remark       string             `json:"remark"`
	Items        []orderItemRequest `json:"items"`
}

func (h warehouseHandler) createOrder(c *gin.Context) {
	claims, ok := authenticatedClaims(c)
	if !ok {
		return
	}
	var request createOrderRequest
	if !bindWarehouseJSON(c, &request) {
		return
	}
	items := make([]trade.OrderItemInput, 0, len(request.Items))
	for _, item := range request.Items {
		items = append(items, trade.OrderItemInput{MaterialID: item.MaterialID, Quantity: item.Quantity.Decimal})
	}
	order, err := h.orders.CreateOrder(c.Request.Context(), claims.UserID, request.ExpectedTime, request.Remark, items)
	if err != nil {
		respondWarehouseError(c, err)
		return
	}
	respondSuccess(c, http.StatusCreated, order)
}

func (h warehouseHandler) reviewOrder(c *gin.Context) {
	orderID, ok := warehousePathID(c)
	if !ok {
		return
	}
	var request struct {
		Action string `json:"action"`
	}
	if !bindWarehouseJSON(c, &request) {
		return
	}
	action := strings.ToLower(strings.TrimSpace(request.Action))
	if action != "approve" && action != "reject" {
		respondError(c, http.StatusBadRequest, 40001, "参数错误：action 必须为 approve 或 reject")
		return
	}
	order, err := h.orders.Review(c.Request.Context(), orderID, action == "approve")
	if err != nil {
		respondWarehouseError(c, err)
		return
	}
	respondSuccess(c, http.StatusOK, order)
}

func (h warehouseHandler) startTrade(c *gin.Context) {
	orderID, ok := warehousePathID(c)
	if !ok {
		return
	}
	order, err := h.orders.StartTrade(c.Request.Context(), orderID)
	if err != nil {
		respondWarehouseError(c, err)
		return
	}
	respondSuccess(c, http.StatusOK, order)
}

func (h warehouseHandler) terminateOrder(c *gin.Context) {
	claims, ok := authenticatedClaims(c)
	if !ok {
		return
	}
	orderID, ok := warehousePathID(c)
	if !ok {
		return
	}
	var cancel bool
	switch claims.Role {
	case "CUSTOMER":
		cancel = true
	case "WAREHOUSE_MANAGER", "SYSTEM_ADMIN":
		cancel = false
	default:
		respondError(c, http.StatusForbidden, 40301, "无权限执行此操作")
		return
	}
	order, err := h.orders.Terminate(c.Request.Context(), orderID, cancel)
	if err != nil {
		respondWarehouseError(c, err)
		return
	}
	respondSuccess(c, http.StatusOK, order)
}

type confirmItemRequest struct {
	MaterialID uint64       `json:"materialId"`
	Quantity   decimalInput `json:"quantity"`
}

type confirmRequest struct {
	Items []confirmItemRequest `json:"items"`
}

func (h warehouseHandler) confirmOrder(c *gin.Context) {
	claims, ok := authenticatedClaims(c)
	if !ok {
		return
	}
	orderID, ok := warehousePathID(c)
	if !ok {
		return
	}
	var request confirmRequest
	if !bindWarehouseJSON(c, &request) {
		return
	}
	items := make([]trade.ConfirmItemInput, 0, len(request.Items))
	for _, item := range request.Items {
		items = append(items, trade.ConfirmItemInput{MaterialID: item.MaterialID, Quantity: item.Quantity.Decimal})
	}
	order, err := h.orders.Confirm(c.Request.Context(), orderID, claims.UserID, items)
	if err != nil {
		respondWarehouseError(c, err)
		return
	}
	respondSuccess(c, http.StatusOK, order)
}
