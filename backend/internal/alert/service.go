package alert

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/chenluuo/smart-project/backend/internal/events"
	"github.com/chenluuo/smart-project/backend/internal/shared/persistence"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

var (
	ErrInvalidInput = errors.New("invalid alert input")
	ErrNotFound     = errors.New("alert resource not found")
	ErrConflict     = errors.New("alert state conflict")
)

type RuleInput struct {
	Metric          string
	Operator        ComparisonOperator
	Value           float64
	Hysteresis      *float64
	DurationSeconds int
	Level           Level
	Enabled         bool
}

type RuleView struct {
	ID              uint64             `json:"id"`
	PlotID          uint64             `json:"plotId"`
	Metric          string             `json:"metric"`
	Operator        ComparisonOperator `json:"operator"`
	Value           float64            `json:"value"`
	Hysteresis      float64            `json:"hysteresis"`
	Unit            string             `json:"unit"`
	DurationSeconds int                `json:"durationSeconds"`
	Enabled         bool               `json:"enabled"`
	Level           Level              `json:"level"`
}

type RuleUpdateResult struct {
	ID        uint64    `json:"id"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ListFilter struct {
	PlotID    *uint64
	Status    *Status
	StartTime *time.Time
	EndTime   *time.Time
	Page      int
	PageSize  int
}

type AlertListRow struct {
	ID                 uint64
	PlotID             uint64
	PlotCode           string
	Metric             string
	WarningType        *WarningType
	Operator           ComparisonOperator
	Threshold          *decimal.Decimal
	DurationSeconds    int
	Level              Level
	Status             Status
	TriggerValue       decimal.Decimal
	TriggeredAt        time.Time
	AcknowledgedAt     *time.Time
	ConfirmationRemark *string
	ResolvedAt         *time.Time
}

type ListItem struct {
	ID             uint64     `json:"id"`
	PlotID         uint64     `json:"plotId"`
	PlotCode       string     `json:"plotCode"`
	Metric         string     `json:"metric"`
	Level          Level      `json:"level"`
	Status         Status     `json:"status"`
	Title          string     `json:"title"`
	Content        string     `json:"content"`
	CurrentValue   float64    `json:"currentValue"`
	ThresholdValue *float64   `json:"thresholdValue"`
	StartedAt      time.Time  `json:"startedAt"`
	ConfirmedAt    *time.Time `json:"confirmedAt,omitempty"`
	ConfirmRemark  *string    `json:"confirmRemark,omitempty"`
	RecoveredAt    *time.Time `json:"recoveredAt,omitempty"`
}

type ListResult struct {
	Items    []ListItem `json:"items"`
	Page     int        `json:"page"`
	PageSize int        `json:"pageSize"`
	Total    int64      `json:"total"`
}

type ConfirmResult struct {
	ID          uint64    `json:"id"`
	Status      Status    `json:"status"`
	ConfirmedAt time.Time `json:"confirmedAt"`
}

type TriggerInput struct {
	RuleID       uint64
	DeviceID     *uint64
	TriggerValue float64
	TriggeredAt  *time.Time
	TraceID      string
	ForwardBody  json.RawMessage
}

type TriggerRecord struct {
	Alert    Alert
	Created  bool
	OwnerID  uint64
	PlotID   uint64
	PlotCode string
	Metric   string
	Operator ComparisonOperator
}

type TriggerResult struct {
	ID          uint64    `json:"id"`
	Status      Status    `json:"status"`
	Created     bool      `json:"created"`
	TriggeredAt time.Time `json:"triggeredAt"`
}

type DeviceWarningInput struct {
	OwnerID             uint64
	PlotID              uint64
	DeviceID            uint64
	Temperature         float64
	SoilMoisture        float64
	Light               float64
	TemperatureWarning  bool
	SoilMoistureWarning bool
	LightWarning        bool
	OccurredAt          time.Time
}

type WarningTransition struct {
	Alert     Alert
	Created   bool
	Recovered bool
	OwnerID   uint64
	PlotID    uint64
	PlotCode  string
	Metric    string
}

type Store interface {
	ListRulesByOwner(context.Context, uint64, uint64) ([]Rule, error)
	UpsertRuleByOwner(context.Context, uint64, *Rule, *decimal.Decimal) error
	ListAlertsByOwner(context.Context, uint64, ListFilter) ([]AlertListRow, int64, error)
	ConfirmAlertByOwner(context.Context, uint64, uint64, string, time.Time) (*Alert, error)
	CreateTriggeredAlert(context.Context, TriggerInput, time.Time) (*TriggerRecord, error)
	SyncDeviceWarnings(context.Context, DeviceWarningInput, time.Time) ([]WarningTransition, error)
}

type Service struct {
	store     Store
	publisher events.Publisher
	now       func() time.Time
}

func NewService(store Store, publishers ...events.Publisher) *Service {
	service := &Service{store: store, now: time.Now}
	if len(publishers) > 0 {
		service.publisher = publishers[0]
	}
	return service
}

func (s *Service) ListRules(ctx context.Context, ownerID, plotID uint64) ([]RuleView, error) {
	if ownerID == 0 || plotID == 0 {
		return nil, ErrInvalidInput
	}
	rules, err := s.store.ListRulesByOwner(ctx, ownerID, plotID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("list threshold rules: %w", err)
	}
	result := make([]RuleView, 0, len(rules))
	for _, rule := range rules {
		value, _ := rule.Threshold.Float64()
		hysteresis, _ := rule.Hysteresis.Float64()
		result = append(result, RuleView{
			ID: rule.ID, PlotID: rule.PlotID, Metric: rule.Metric,
			Operator: rule.ComparisonOperator, Value: value, Hysteresis: hysteresis, Unit: metricUnit(rule.Metric),
			DurationSeconds: rule.DurationSeconds, Enabled: rule.Enabled, Level: rule.Level,
		})
	}
	return result, nil
}

func (s *Service) UpsertRule(ctx context.Context, ownerID, plotID, thresholdID uint64, input RuleInput) (*RuleUpdateResult, error) {
	input.Metric = strings.TrimSpace(input.Metric)
	input.Operator = ComparisonOperator(strings.ToUpper(strings.TrimSpace(string(input.Operator))))
	input.Level = Level(strings.ToUpper(strings.TrimSpace(string(input.Level))))
	if ownerID == 0 || plotID == 0 || thresholdID == 0 || !validRuleInput(input) {
		return nil, ErrInvalidInput
	}
	now := s.now()
	var hysteresis *decimal.Decimal
	if input.Hysteresis != nil {
		value := decimal.NewFromFloat(*input.Hysteresis)
		hysteresis = &value
	}
	rule := &Rule{
		ID: thresholdID, PlotID: plotID, Name: input.Metric + " " + string(input.Operator),
		Metric: input.Metric, ComparisonOperator: input.Operator,
		Threshold: decimal.NewFromFloat(input.Value), DurationSeconds: input.DurationSeconds,
		Level: input.Level, Enabled: input.Enabled,
		Auditable: persistence.Auditable{CreatedAt: now, UpdatedAt: now},
	}
	if hysteresis != nil {
		rule.Hysteresis = *hysteresis
	}
	if err := s.store.UpsertRuleByOwner(ctx, ownerID, rule, hysteresis); errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, fmt.Errorf("upsert threshold rule: %w", err)
	}
	return &RuleUpdateResult{ID: rule.ID, UpdatedAt: rule.UpdatedAt}, nil
}

func (s *Service) List(ctx context.Context, ownerID uint64, filter ListFilter) (ListResult, error) {
	if filter.Page == 0 {
		filter.Page = 1
	}
	if filter.PageSize == 0 {
		filter.PageSize = 20
	}
	if ownerID == 0 || filter.Page < 1 || filter.PageSize < 1 || filter.PageSize > 100 ||
		filter.PlotID != nil && *filter.PlotID == 0 || filter.Status != nil && !validStatus(*filter.Status) ||
		filter.StartTime != nil && filter.EndTime != nil && filter.StartTime.After(*filter.EndTime) {
		return ListResult{}, ErrInvalidInput
	}
	rows, total, err := s.store.ListAlertsByOwner(ctx, ownerID, filter)
	if err != nil {
		return ListResult{}, fmt.Errorf("list alerts: %w", err)
	}
	items := make([]ListItem, 0, len(rows))
	for _, row := range rows {
		if row.WarningType != nil {
			row.Metric = warningMetric(*row.WarningType)
		}
		current, _ := row.TriggerValue.Float64()
		var threshold *float64
		if row.Threshold != nil {
			value, _ := row.Threshold.Float64()
			threshold = &value
		}
		status := row.Status
		if status == StatusAcknowledged {
			status = StatusConfirmed
		}
		items = append(items, ListItem{
			ID: row.ID, PlotID: row.PlotID, PlotCode: row.PlotCode,
			Metric: row.Metric, Level: row.Level, Status: status,
			Title: alertTitle(row), Content: alertContent(row),
			CurrentValue: current, ThresholdValue: threshold, StartedAt: row.TriggeredAt,
			ConfirmedAt: row.AcknowledgedAt, ConfirmRemark: row.ConfirmationRemark, RecoveredAt: row.ResolvedAt,
		})
	}
	return ListResult{Items: items, Page: filter.Page, PageSize: filter.PageSize, Total: total}, nil
}

func (s *Service) SyncDeviceWarnings(ctx context.Context, input DeviceWarningInput) ([]WarningTransition, error) {
	if input.OwnerID == 0 || input.PlotID == 0 || input.DeviceID == 0 || input.OccurredAt.IsZero() ||
		math.IsNaN(input.Temperature) || math.IsInf(input.Temperature, 0) ||
		math.IsNaN(input.SoilMoisture) || math.IsInf(input.SoilMoisture, 0) ||
		math.IsNaN(input.Light) || math.IsInf(input.Light, 0) {
		return nil, ErrInvalidInput
	}
	transitions, err := s.store.SyncDeviceWarnings(ctx, input, s.now().UTC())
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("sync device warnings: %w", err)
	}
	for _, transition := range transitions {
		if s.publisher == nil {
			continue
		}
		if transition.Created {
			_, _ = events.PublishAlertCreated(s.publisher, events.AlertCreated{
				OwnerID: transition.OwnerID, AlertID: transition.Alert.ID, PlotID: transition.PlotID,
				Level: string(transition.Alert.Level), Title: deviceWarningTitle(transition.PlotCode, transition.Metric),
				CreatedAt: transition.Alert.TriggeredAt,
			})
		}
		if transition.Recovered && transition.Alert.ResolvedAt != nil {
			_, _ = events.PublishAlertRecovered(s.publisher, events.AlertRecovered{
				OwnerID: transition.OwnerID, AlertID: transition.Alert.ID, PlotID: transition.PlotID,
				RecoveredAt: *transition.Alert.ResolvedAt,
			})
		}
	}
	return transitions, nil
}

func (s *Service) Confirm(ctx context.Context, ownerID, alertID uint64, remark string) (*ConfirmResult, error) {
	remark = strings.TrimSpace(remark)
	if ownerID == 0 || alertID == 0 || remark == "" || len([]rune(remark)) > 500 {
		return nil, ErrInvalidInput
	}
	now := s.now()
	result, err := s.store.ConfirmAlertByOwner(ctx, ownerID, alertID, remark, now)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if errors.Is(err, ErrConflict) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, fmt.Errorf("confirm alert: %w", err)
	}
	confirmedAt := now
	if result.AcknowledgedAt != nil {
		confirmedAt = *result.AcknowledgedAt
	}
	return &ConfirmResult{ID: result.ID, Status: StatusConfirmed, ConfirmedAt: confirmedAt}, nil
}

// Trigger creates one active alert and its delivery records. The repository
// serializes triggers for the same rule so repeated telemetry does not produce
// duplicate active alerts or duplicate owner/agent notifications.
func (s *Service) Trigger(ctx context.Context, input TriggerInput) (*TriggerResult, error) {
	input.TraceID = strings.TrimSpace(input.TraceID)
	if input.RuleID == 0 || input.DeviceID != nil && *input.DeviceID == 0 ||
		math.IsNaN(input.TriggerValue) || math.IsInf(input.TriggerValue, 0) || len(input.TraceID) > 64 ||
		len(input.ForwardBody) == 0 || len(input.ForwardBody) > 1024*1024 || !json.Valid(input.ForwardBody) {
		return nil, ErrInvalidInput
	}
	now := s.now().UTC()
	if input.TriggeredAt == nil {
		input.TriggeredAt = &now
	} else {
		triggeredAt := input.TriggeredAt.UTC()
		input.TriggeredAt = &triggeredAt
	}
	record, err := s.store.CreateTriggeredAlert(ctx, input, now)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if errors.Is(err, ErrConflict) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, fmt.Errorf("trigger alert: %w", err)
	}
	if record.Created && s.publisher != nil {
		_, _ = events.PublishAlertCreated(s.publisher, events.AlertCreated{
			OwnerID: record.OwnerID, AlertID: record.Alert.ID, PlotID: record.PlotID,
			Level: string(record.Alert.Level), Title: alertTitle(AlertListRow{
				PlotCode: record.PlotCode, Metric: record.Metric, Operator: record.Operator,
			}),
			CreatedAt: record.Alert.TriggeredAt,
		})
	}
	return &TriggerResult{
		ID: record.Alert.ID, Status: record.Alert.Status,
		Created: record.Created, TriggeredAt: record.Alert.TriggeredAt,
	}, nil
}

func validRuleInput(input RuleInput) bool {
	return input.Metric != "" && len(input.Metric) <= 64 &&
		(input.Operator == OperatorLT || input.Operator == OperatorLTE || input.Operator == OperatorGT || input.Operator == OperatorGTE) &&
		!math.IsNaN(input.Value) && !math.IsInf(input.Value, 0) &&
		(input.Hysteresis == nil || *input.Hysteresis >= 0 && !math.IsNaN(*input.Hysteresis) && !math.IsInf(*input.Hysteresis, 0)) &&
		input.DurationSeconds >= 0 && input.DurationSeconds <= 86400 && validLevel(input.Level)
}

func validLevel(level Level) bool {
	return level == LevelLow || level == LevelMedium || level == LevelHigh
}

func validStatus(status Status) bool {
	return status == StatusActive || status == StatusConfirmed || status == StatusAcknowledged || status == StatusResolved || status == StatusClosed
}

func metricUnit(metric string) string {
	switch strings.ToLower(metric) {
	case "soilmoisture", "humidity", "airhumidity":
		return "%"
	case "temperature", "airtemperature", "soiltemperature":
		return "°C"
	case "rainfall":
		return "mm"
	case "light":
		return "lx"
	default:
		return ""
	}
}

func alertTitle(row AlertListRow) string {
	if row.WarningType != nil {
		return deviceWarningTitle(row.PlotCode, warningMetric(*row.WarningType))
	}
	direction := "异常"
	if row.Operator == OperatorLT || row.Operator == OperatorLTE {
		direction = "偏低"
	} else if row.Operator == OperatorGT || row.Operator == OperatorGTE {
		direction = "偏高"
	}
	metric := row.Metric
	if strings.EqualFold(metric, "soilMoisture") {
		metric = "湿度"
	} else if strings.Contains(strings.ToLower(metric), "temperature") {
		metric = "温度"
	}
	return fmt.Sprintf("%s 地块%s%s", row.PlotCode, metric, direction)
}

func alertContent(row AlertListRow) string {
	if row.WarningType != nil && row.Metric == "" {
		row.Metric = warningMetric(*row.WarningType)
	}
	unit := metricUnit(row.Metric)
	if row.WarningType != nil {
		return fmt.Sprintf("设备上报%s警告，当前值 %s%s", warningMetricLabel(*row.WarningType), row.TriggerValue.String(), unit)
	}
	return fmt.Sprintf("%s%s 触发持续阈值告警", row.TriggerValue.String(), unit)
}

func warningMetric(value WarningType) string {
	switch value {
	case WarningTemperature:
		return "temperature"
	case WarningSoilMoisture:
		return "soilMoisture"
	case WarningLight:
		return "light"
	default:
		return ""
	}
}

func warningMetricLabel(value WarningType) string {
	switch value {
	case WarningTemperature:
		return "温度"
	case WarningSoilMoisture:
		return "湿度"
	case WarningLight:
		return "光照"
	default:
		return "设备"
	}
}

func deviceWarningTitle(plotCode, metric string) string {
	label := metric
	switch metric {
	case "temperature":
		label = "温度"
	case "soilMoisture":
		label = "湿度"
	case "light":
		label = "光照"
	}
	return fmt.Sprintf("%s 地块%s警告", plotCode, label)
}
