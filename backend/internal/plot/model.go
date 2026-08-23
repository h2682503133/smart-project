package plot

import (
	"github.com/chenluuo/smart-project/backend/internal/shared/persistence"
	"github.com/shopspring/decimal"
)

type Status string

const (
	StatusActive   Status = "ACTIVE"
	StatusDisabled Status = "DISABLED"
)

type Plot struct {
	ID          uint64           `json:"id" gorm:"primaryKey;autoIncrement"`
	OwnerID     uint64           `json:"ownerId" gorm:"column:owner_id;not null;index:idx_plots_owner;uniqueIndex:uk_plots_owner_code,priority:1"`
	Code        string           `json:"code" gorm:"size:32;not null;uniqueIndex:uk_plots_owner_code,priority:2"`
	Name        string           `json:"name" gorm:"size:128;not null"`
	CropType    *string          `json:"cropType,omitempty" gorm:"column:crop_type;size:64"`
	GrowthStage *string          `json:"growthStage,omitempty" gorm:"column:growth_stage;size:64"`
	Area        *decimal.Decimal `json:"area,omitempty" gorm:"type:decimal(12,2)"`
	Location    *string          `json:"location,omitempty" gorm:"size:255"`
	Status      Status           `json:"status" gorm:"size:32;not null"`
	persistence.Auditable
}

func (Plot) TableName() string { return "plots" }
