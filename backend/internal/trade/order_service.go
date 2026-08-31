package trade

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// OrderService 意向订单查询服务（列表/详情 + 占用聚合）。
type OrderService struct {
	repository *OrderRepository
	warehouse  *WarehouseService
}

func NewOrderService(repository *OrderRepository) *OrderService {
	return &OrderService{repository: repository}
}

// ConfigureWarehouse 注入仓储服务（用于成交扣库存）。在仓储服务创建后调用。
func (s *OrderService) ConfigureWarehouse(w *WarehouseService) { s.warehouse = w }

// ReservedByMaterials 实现 WarehouseService 的 ReservationReader：TRADING 状态占用量。
func (s *OrderService) ReservedByMaterials(ctx context.Context, materialIDs []uint64) (map[uint64]decimal.Decimal, error) {
	return s.repository.ReservedByMaterials(ctx, materialIDs)
}

// List 意向列表：每单明细 + 该物料可用数量（库存总量 − TRADING 占用）。
func (s *OrderService) List(ctx context.Context, f OrderFilter) (ListResult[OrderView], error) {
	headers, items, names, total, err := s.repository.ListOrders(ctx, f)
	if err != nil {
		return ListResult[OrderView]{}, fmt.Errorf("list orders: %w", err)
	}
	views, err := s.buildViews(ctx, headers, items, names)
	if err != nil {
		return ListResult[OrderView]{}, err
	}
	return ListResult[OrderView]{Items: views, Page: f.Page, PageSize: f.PageSize, Total: total}, nil
}

// Get 意向详情。
func (s *OrderService) Get(ctx context.Context, id uint64) (*OrderView, error) {
	if id == 0 {
		return nil, ErrInvalidInput
	}
	header, items, customerName, err := s.repository.GetOrder(ctx, id)
	if err != nil {
		return nil, err
	}
	views, err := s.buildViews(ctx, []OrderHeader{*header}, items, map[uint64]string{header.ID: customerName})
	if err != nil {
		return nil, err
	}
	if len(views) == 0 {
		return nil, ErrNotFound
	}
	return &views[0], nil
}

func (s *OrderService) buildViews(ctx context.Context, headers []OrderHeader, items []OrderItem, names map[uint64]string) ([]OrderView, error) {
	if len(headers) == 0 {
		return []OrderView{}, nil
	}
	materialIDs := make([]uint64, 0)
	seen := map[uint64]bool{}
	for _, item := range items {
		if !seen[item.MaterialID] {
			materialIDs = append(materialIDs, item.MaterialID)
			seen[item.MaterialID] = true
		}
	}
	totals, err := s.repository.StockTotalsByMaterials(ctx, materialIDs)
	if err != nil {
		return nil, fmt.Errorf("read stock totals: %w", err)
	}
	reserved, err := s.repository.ReservedByMaterials(ctx, materialIDs)
	if err != nil {
		return nil, fmt.Errorf("read stock reservations: %w", err)
	}
	infos, err := s.repository.MaterialInfosByIDs(ctx, materialIDs)
	if err != nil {
		return nil, fmt.Errorf("read material infos: %w", err)
	}

	itemsByOrder := make(map[uint64][]OrderItem, len(headers))
	for _, item := range items {
		itemsByOrder[item.OrderID] = append(itemsByOrder[item.OrderID], item)
	}

	views := make([]OrderView, 0, len(headers))
	for _, header := range headers {
		view := OrderView{
			ID: header.ID, OrderNo: header.OrderNo, Status: header.Status,
			CustomerID: header.CustomerID, CustomerName: names[header.ID],
			ExpectedTime: header.ExpectedTime, Remark: header.Remark, CreatedAt: header.CreatedAt,
			Items: make([]OrderItemView, 0, len(itemsByOrder[header.ID])),
		}
		for _, item := range itemsByOrder[header.ID] {
			available := decimal.Zero
			if total, ok := totals[item.MaterialID]; ok {
				available = total.Sub(reserved[item.MaterialID])
				if available.IsNegative() {
					available = decimal.Zero
				}
			}
			info := infos[item.MaterialID]
			view.Items = append(view.Items, OrderItemView{
				MaterialID: item.MaterialID, MaterialName: info.Name, Unit: info.Unit,
				Quantity: item.Quantity, AvailableQuantity: available,
			})
		}
		views = append(views, view)
	}
	return views, nil
}

// CreateOrder 发意向：仅记录需求，库存将在实际成交扣库时校验。
func (s *OrderService) CreateOrder(ctx context.Context, customerID uint64, expectedTime *time.Time, remark string, items []OrderItemInput) (*OrderHeader, error) {
	if customerID == 0 || len(items) == 0 {
		return nil, ErrInvalidInput
	}
	orderItems := make([]OrderItem, 0, len(items))
	seen := make(map[uint64]struct{}, len(items))
	for _, item := range items {
		if item.MaterialID == 0 || !validQuantity(item.Quantity) {
			return nil, ErrInvalidInput
		}
		if _, dup := seen[item.MaterialID]; dup {
			return nil, ErrInvalidInput
		}
		seen[item.MaterialID] = struct{}{}
		orderItems = append(orderItems, OrderItem{MaterialID: item.MaterialID, Quantity: item.Quantity})
	}

	order := &OrderHeader{
		OrderNo:      newOrderNo(),
		Status:       OrderStatusPending,
		CustomerID:   customerID,
		ExpectedTime: expectedTime,
		Remark:       optionalString(remark),
	}
	if _, _, err := s.repository.CreateOrder(ctx, order, orderItems); err != nil {
		return nil, err
	}
	return order, nil
}

// Review 审批：approve=true → APPROVED，否则 → REJECTED。
func (s *OrderService) Review(ctx context.Context, orderID uint64, approve bool) (*OrderHeader, error) {
	if orderID == 0 {
		return nil, ErrInvalidInput
	}
	target := OrderStatusRejected
	if approve {
		target = OrderStatusApproved
	}
	return s.repository.Transition(ctx, orderID, target)
}

// StartTrade 进入交易：APPROVED → TRADING（开始占用库存）。
func (s *OrderService) StartTrade(ctx context.Context, orderID uint64) (*OrderHeader, error) {
	if orderID == 0 {
		return nil, ErrInvalidInput
	}
	return s.repository.Transition(ctx, orderID, OrderStatusTrading)
}

// Terminate 取消（cancel=true，PENDING→DELETED）或关闭（cancel=false，TRADING→CLOSED）。
func (s *OrderService) Terminate(ctx context.Context, orderID uint64, cancel bool) (*OrderHeader, error) {
	if orderID == 0 {
		return nil, ErrInvalidInput
	}
	target := OrderStatusClosed
	if cancel {
		target = OrderStatusDeleted
	}
	return s.repository.Transition(ctx, orderID, target)
}

// Confirm 成交：行锁订单 → 校验实成交量 → 分配仓库 → 扣库存 → 更新实成交量 → 软删订单。
// 整个流程运行在仓储事务内，保证扣库存与软删原子。
func (s *OrderService) Confirm(ctx context.Context, orderID, operatorID uint64, actualItems []ConfirmItemInput) (*OrderHeader, error) {
	if s.warehouse == nil {
		return nil, ErrInvalidInput
	}
	if orderID == 0 || operatorID == 0 || len(actualItems) == 0 {
		return nil, ErrInvalidInput
	}
	actual := make(map[uint64]decimal.Decimal, len(actualItems))
	for _, item := range actualItems {
		if item.MaterialID == 0 || !validQuantity(item.Quantity) {
			return nil, ErrInvalidInput
		}
		actual[item.MaterialID] = item.Quantity
	}

	var result *OrderHeader
	err := s.warehouse.repository.Transaction(ctx, func(tx *Repository) error {
		repo := s.repository.WithTx(tx.db)
		order, err := repo.LockOrder(ctx, orderID)
		if err != nil {
			return err
		}
		if order.Status != OrderStatusTrading {
			return ErrConflict
		}
		items, err := repo.itemsByOrderIDs(ctx, []uint64{orderID})
		if err != nil {
			return err
		}
		if len(actual) != len(items) {
			return ErrInvalidInput
		}
		for _, item := range items {
			qty, ok := actual[item.MaterialID]
			if !ok || qty.LessThanOrEqual(decimal.Zero) || qty.GreaterThan(item.Quantity) {
				return ErrInvalidInput
			}
		}

		materialIDs := make([]uint64, 0, len(items))
		for _, item := range items {
			materialIDs = append(materialIDs, item.MaterialID)
		}
		stocks, err := repo.StocksByMaterials(ctx, materialIDs)
		if err != nil {
			return err
		}
		stockByMaterial := make(map[uint64]decimal.Decimal)
		for _, stock := range stocks {
			stockByMaterial[stock.MaterialID] = decimalOrZero(stockByMaterial, stock.MaterialID).Add(stock.Quantity)
		}
		for _, item := range items {
			if decimalOrZero(stockByMaterial, item.MaterialID).LessThan(actual[item.MaterialID]) {
				return ErrInsufficientStock
			}
		}

		outbound := make([]OutboundItem, 0, len(items))
		for _, item := range items {
			remaining := actual[item.MaterialID]
			for _, stock := range stocks {
				if stock.MaterialID != item.MaterialID || remaining.LessThanOrEqual(decimal.Zero) {
					continue
				}
				deduct := decimal.Min(stock.Quantity, remaining)
				outbound = append(outbound, OutboundItem{WarehouseID: stock.WarehouseID, MaterialID: item.MaterialID, Quantity: deduct})
				remaining = remaining.Sub(deduct)
			}
		}

		if err := s.warehouse.DeductForOrderWithRepository(ctx, tx, order.OrderNo, operatorID, outbound); err != nil {
			return err
		}
		for _, item := range items {
			if err := repo.UpdateItemQuantity(ctx, item.ID, actual[item.MaterialID]); err != nil {
				return err
			}
		}
		if err := repo.SoftDelete(ctx, orderID); err != nil {
			return err
		}
		order.Status = OrderStatusDeleted
		result = order
		return nil
	})
	return result, err
}

func newOrderNo() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "INT-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	return "INT-" + strconv.FormatInt(time.Now().UnixNano(), 10) + "-" + hex.EncodeToString(b[:])
}

func optionalString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func decimalOrZero(m map[uint64]decimal.Decimal, id uint64) decimal.Decimal {
	if v, ok := m[id]; ok {
		return v
	}
	return decimal.Zero
}
