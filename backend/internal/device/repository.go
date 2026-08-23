package device

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/chenluuo/smart-project/backend/internal/plot"
	"github.com/chenluuo/smart-project/backend/internal/shared/persistence"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repositories struct {
	Devices  *persistence.Repository[Device]
	Bindings *persistence.Repository[Binding]
	db       *gorm.DB
}

func NewRepositories(db *gorm.DB) Repositories {
	return Repositories{Devices: persistence.NewRepository[Device](db), Bindings: persistence.NewRepository[Binding](db), db: db}
}

func (r Repositories) FindByCode(ctx context.Context, code string) (*Device, error) {
	var result Device
	if err := r.db.WithContext(ctx).Where("device_code = ?", code).First(&result).Error; err != nil {
		return nil, err
	}
	return &result, nil
}

func (r Repositories) FindActiveBinding(ctx context.Context, deviceID uint64) (*Binding, error) {
	var result Binding
	if err := r.db.WithContext(ctx).Where("device_id = ? AND unbound_at IS NULL", deviceID).First(&result).Error; err != nil {
		return nil, err
	}
	return &result, nil
}

func (r Repositories) ListByOwner(ctx context.Context, ownerID uint64, filter ListFilter) ([]ListItem, int64, error) {
	query := r.db.WithContext(ctx).Table("devices AS d").
		Joins("JOIN device_bindings AS b ON b.device_id = d.id AND b.unbound_at IS NULL").
		Joins("JOIN plots AS p ON p.id = b.plot_id").
		Where("p.owner_id = ?", ownerID)
	if filter.PlotID != nil {
		query = query.Where("b.plot_id = ?", *filter.PlotID)
	}
	if filter.Status != nil {
		query = query.Where("d.status = ?", *filter.Status)
	}
	if filter.DerivedStatus != nil {
		query = query.Where("d.status IN ?", []Status{StatusOnline, StatusOffline})
		if *filter.DerivedStatus == StatusOnline {
			query = query.Where("d.id IN ?", filter.ActiveDeviceIDs)
		} else if len(filter.ActiveDeviceIDs) > 0 {
			query = query.Where("d.id NOT IN ?", filter.ActiveDeviceIDs)
		}
	}
	if filter.DeviceType != "" {
		query = query.Where("d.device_type = ?", filter.DeviceType)
	}

	var total int64
	if err := query.Distinct("d.id").Count(&total).Error; err != nil {
		return nil, 0, err
	}
	type row struct {
		Device
		PlotID uint64 `gorm:"column:plot_id"`
	}
	var rows []row
	err := query.Select("d.*, b.plot_id AS plot_id").Order("d.id ASC").
		Limit(filter.PageSize).Offset((filter.Page - 1) * filter.PageSize).Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	items := make([]ListItem, 0, len(rows))
	for _, result := range rows {
		items = append(items, ListItem{Device: result.Device, PlotID: result.PlotID})
	}
	return items, total, nil
}

func (r Repositories) Bind(ctx context.Context, ownerID uint64, input BindInput) (*Device, error) {
	var result Device
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var ownedPlot plot.Plot
		if err := tx.Select("id").Where("id = ? AND owner_id = ?", input.PlotID, ownerID).First(&ownedPlot).Error; err != nil {
			return err
		}

		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("serial_no = ?", input.SerialNo).First(&result).Error
		switch {
		case err == nil:
			if result.DeviceType != input.DeviceType {
				return ErrDeviceTypeMismatch
			}
		case errors.Is(err, gorm.ErrRecordNotFound):
			code, err := newDeviceCode()
			if err != nil {
				return err
			}
			result = Device{DeviceCode: code, SerialNo: input.SerialNo, Name: input.Name, DeviceType: input.DeviceType, Status: StatusOffline, CredentialStatus: CredentialPending}
			if err := tx.Create(&result).Error; err != nil {
				return err
			}
		default:
			return err
		}

		var active Binding
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("device_id = ? AND unbound_at IS NULL", result.ID).First(&active).Error
		if err == nil {
			return ErrAlreadyBound
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		updates := map[string]any{"name": input.Name}
		if result.Status == StatusUnactivated {
			updates["status"] = StatusOffline
			result.Status = StatusOffline
		}
		if err := tx.Model(&result).Updates(updates).Error; err != nil {
			return err
		}
		result.Name = input.Name
		return tx.Create(&Binding{DeviceID: result.ID, PlotID: input.PlotID, BoundBy: ownerID, BoundAt: time.Now()}).Error
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (r Repositories) Unbind(ctx context.Context, ownerID, deviceID uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var binding Binding
		err := tx.Table("device_bindings AS b").Select("b.*").
			Joins("JOIN plots AS p ON p.id = b.plot_id").
			Where("b.device_id = ? AND b.unbound_at IS NULL AND p.owner_id = ?", deviceID, ownerID).
			Clauses(clause.Locking{Strength: "UPDATE"}).First(&binding).Error
		if err != nil {
			return err
		}
		return tx.Model(&Binding{}).Where("id = ? AND unbound_at IS NULL", binding.ID).Update("unbound_at", time.Now()).Error
	})
}

func (r Repositories) FindByIDAndOwner(ctx context.Context, deviceID, ownerID uint64) (*Device, error) {
	var result Device
	err := r.db.WithContext(ctx).Table("devices AS d").Select("d.*").
		Joins("JOIN device_bindings AS b ON b.device_id = d.id AND b.unbound_at IS NULL").
		Joins("JOIN plots AS p ON p.id = b.plot_id").
		Where("d.id = ? AND p.owner_id = ?", deviceID, ownerID).First(&result).Error
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func newDeviceCode() (string, error) {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate device code: %w", err)
	}
	return "dev_" + hex.EncodeToString(bytes), nil
}
