package control

import (
	"context"
	"database/sql"
	"errors"

	"github.com/chenluuo/smart-project/backend/internal/device"
	"github.com/chenluuo/smart-project/backend/internal/shared/persistence"
	"gorm.io/gorm"
)

type IrrigationDevice struct {
	DeviceID uint64        `gorm:"column:device_id"`
	PlotID   uint64        `gorm:"column:plot_id"`
	Status   device.Status `gorm:"column:status"`
}

type CommandListRow struct {
	Command
	PlotCode     string `gorm:"column:plot_code"`
	OperatorName string `gorm:"column:operator_name"`
}

type Repository struct {
	*persistence.Repository[Command]
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{Repository: persistence.NewRepository[Command](db), db: db}
}

func (r *Repository) FindByCommandID(ctx context.Context, commandID string) (*Command, error) {
	return r.findOne(ctx, "command_id = ?", commandID)
}

func (r *Repository) FindByIdempotencyKey(ctx context.Context, key string) (*Command, error) {
	return r.findOne(ctx, "idempotency_key = ?", key)
}

func (r *Repository) FindByDeviceAndStatuses(ctx context.Context, deviceID uint64, statuses []Status) ([]Command, error) {
	var commands []Command
	err := r.db.WithContext(ctx).Where("device_id = ? AND status IN ?", deviceID, statuses).Find(&commands).Error
	return commands, err
}

func (r *Repository) FindIrrigationDevice(ctx context.Context, ownerID, plotID uint64) (*IrrigationDevice, error) {
	var result IrrigationDevice
	// 注意：不能用 gorm 的 First(&struct{}) —— 该结构体无主键字段时
	// GORM 会把 device_id 当主键并自动追加 ORDER BY p.device_id（plots 无此列报 1054）。
	// 用显式 Row() 取第一行，完全绕开 GORM 默认排序生成。
	err := r.db.WithContext(ctx).Table("plots AS p").
		Select("d.id AS device_id, p.id AS plot_id, d.status AS status").
		Joins("JOIN device_bindings AS b ON b.plot_id = p.id AND b.unbound_at IS NULL").
		Joins("JOIN devices AS d ON d.id = b.device_id").
		Where("p.id = ? AND p.owner_id = ? AND d.device_type = ?", plotID, ownerID, "IRRIGATION_VALVE").
		Order("d.id").
		Limit(1).
		Row().Scan(&result.DeviceID, &result.PlotID, &result.Status)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, gorm.ErrRecordNotFound
		}
		return nil, err
	}
	return &result, nil
}

func (r *Repository) FindByIdempotencyKeyAndOwner(ctx context.Context, key string, ownerID uint64) (*Command, error) {
	var result Command
	err := r.db.WithContext(ctx).Table("device_commands AS c").Select("c.*").
		Joins("JOIN plots AS p ON p.id = c.plot_id").
		Where("c.idempotency_key = ? AND p.owner_id = ?", key, ownerID).
		First(&result).Error
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (r *Repository) FindLatestSuccessfulByDeviceAndPlot(ctx context.Context, deviceID, plotID uint64) (*Command, error) {
	var result Command
	err := r.db.WithContext(ctx).Where(
		"device_id = ? AND plot_id = ? AND status IN ?",
		deviceID, plotID, []Status{StatusAcknowledged, StatusSucceeded},
	).
		Order("created_at DESC, id DESC").First(&result).Error
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (r *Repository) FindByCommandIDAndOwner(ctx context.Context, commandID string, ownerID uint64) (*Command, error) {
	var result Command
	err := r.db.WithContext(ctx).Table("device_commands AS c").Select("c.*").
		Joins("JOIN plots AS p ON p.id = c.plot_id").
		Where("c.command_id = ? AND p.owner_id = ?", commandID, ownerID).
		First(&result).Error
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (r *Repository) ListByOwner(ctx context.Context, ownerID uint64, filter ListFilter) ([]CommandListRow, int64, error) {
	query := r.db.WithContext(ctx).Table("device_commands AS c").
		Joins("JOIN plots AS p ON p.id = c.plot_id").
		Joins("JOIN users AS u ON u.id = c.issued_by").
		Where("p.owner_id = ?", ownerID)
	if filter.PlotID != nil {
		query = query.Where("c.plot_id = ?", *filter.PlotID)
	}
	if filter.Status != nil {
		query = query.Where("c.status = ?", *filter.Status)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []CommandListRow
	err := query.Select("c.*, p.code AS plot_code, u.name AS operator_name").
		Order("c.created_at DESC, c.id DESC").
		Limit(filter.PageSize).Offset((filter.Page - 1) * filter.PageSize).
		Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func (r *Repository) findOne(ctx context.Context, query string, value any) (*Command, error) {
	var command Command
	if err := r.db.WithContext(ctx).Where(query, value).First(&command).Error; err != nil {
		return nil, err
	}
	return &command, nil
}
