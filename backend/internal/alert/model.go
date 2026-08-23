package alert

import (
	"time"

	"github.com/chenluuo/smart-project/backend/internal/shared/persistence"
	"github.com/shopspring/decimal"
)

type ComparisonOperator string
type Level string
type Status string
type Source string
type WarningType string

const (
	OperatorLT      ComparisonOperator = "LT"
	OperatorLTE     ComparisonOperator = "LTE"
	OperatorGT      ComparisonOperator = "GT"
	OperatorGTE     ComparisonOperator = "GTE"
	LevelLow        Level              = "LOW"
	LevelMedium     Level              = "MEDIUM"
	LevelHigh       Level              = "HIGH"
	StatusActive    Status             = "ACTIVE"
	StatusConfirmed Status             = "CONFIRMED"
	// StatusAcknowledged is retained for rows written before the API contract
	// standardized the user-facing state name as CONFIRMED.
	StatusAcknowledged  Status      = "ACKNOWLEDGED"
	StatusResolved      Status      = "RESOLVED"
	StatusClosed        Status      = "CLOSED"
	SourceRule          Source      = "RULE"
	SourceDevice        Source      = "DEVICE"
	WarningTemperature  WarningType = "TEMPERATURE"
	WarningSoilMoisture WarningType = "SOIL_MOISTURE"
	WarningLight        WarningType = "LIGHT"
)

type Rule struct {
	ID                 uint64             `json:"id" gorm:"primaryKey;autoIncrement"`
	PlotID             uint64             `json:"plotId" gorm:"column:plot_id;not null"`
	Name               string             `json:"name" gorm:"size:128;not null"`
	Metric             string             `json:"metric" gorm:"size:64;not null"`
	ComparisonOperator ComparisonOperator `json:"comparisonOperator" gorm:"column:comparison_operator;size:16;not null"`
	Threshold          decimal.Decimal    `json:"threshold" gorm:"type:decimal(14,4);not null"`
	DurationSeconds    int                `json:"durationSeconds" gorm:"column:duration_seconds;not null"`
	Hysteresis         decimal.Decimal    `json:"hysteresis" gorm:"type:decimal(14,4);not null;default:0"`
	Level              Level              `json:"level" gorm:"size:16;not null"`
	Enabled            bool               `json:"enabled" gorm:"not null;default:true"`
	persistence.Auditable
}

func (Rule) TableName() string { return "alert_rules" }

type Alert struct {
	ID                 uint64          `json:"id" gorm:"primaryKey;autoIncrement"`
	RuleID             *uint64         `json:"ruleId,omitempty" gorm:"column:rule_id;index:idx_alerts_rule_status,priority:1"`
	PlotID             uint64          `json:"plotId" gorm:"column:plot_id;not null;index:idx_alerts_plot_status,priority:1"`
	DeviceID           *uint64         `json:"deviceId,omitempty" gorm:"column:device_id;index:idx_alerts_device_triggered,priority:1"`
	Source             Source          `json:"source" gorm:"size:16;not null"`
	WarningType        *WarningType    `json:"warningType,omitempty" gorm:"column:warning_type;size:32"`
	ActiveDedupKey     *string         `json:"-" gorm:"column:active_dedup_key;size:128;uniqueIndex:uk_alerts_active_dedup"`
	AcknowledgedBy     *uint64         `json:"acknowledgedBy,omitempty" gorm:"column:acknowledged_by;index:idx_alerts_ack_user"`
	Level              Level           `json:"level" gorm:"size:16;not null"`
	Status             Status          `json:"status" gorm:"size:32;not null;index:idx_alerts_rule_status,priority:2"`
	TriggerValue       decimal.Decimal `json:"triggerValue" gorm:"column:trigger_value;type:decimal(14,4);not null"`
	TriggeredAt        time.Time       `json:"triggeredAt" gorm:"column:triggered_at;not null;index:idx_alerts_device_triggered,priority:2"`
	AcknowledgedAt     *time.Time      `json:"acknowledgedAt,omitempty" gorm:"column:acknowledged_at"`
	ConfirmationRemark *string         `json:"confirmationRemark,omitempty" gorm:"column:confirmation_remark;size:500"`
	ResolvedAt         *time.Time      `json:"resolvedAt,omitempty" gorm:"column:resolved_at"`
	persistence.Auditable
}

func (Alert) TableName() string { return "alerts" }
