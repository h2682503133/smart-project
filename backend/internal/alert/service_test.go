package alert

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/chenluuo/smart-project/backend/internal/events"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type alertStoreStub struct {
	rules             []Rule
	rows              []AlertListRow
	total             int64
	confirmed         *Alert
	err               error
	ownerID           uint64
	plotID            uint64
	alertID           uint64
	rule              *Rule
	hysteresis        *decimal.Decimal
	filter            ListFilter
	confirmRemark     string
	confirmedAt       time.Time
	triggerInput      TriggerInput
	triggeredAt       time.Time
	triggerRecord     *TriggerRecord
	warningInput      DeviceWarningInput
	transitions       []WarningTransition
	listCalls         int
	persistenceResult RulePersistenceResult
	syncResult        *ThresholdSyncView
	expiresAt         time.Time
}

func (s *alertStoreStub) ListRulesByOwner(_ context.Context, ownerID, plotID uint64) ([]Rule, error) {
	s.ownerID, s.plotID = ownerID, plotID
	return s.rules, s.err
}

func (s *alertStoreStub) UpsertRuleByOwner(_ context.Context, ownerID uint64, rule *Rule, hysteresis *decimal.Decimal, expiresAt time.Time) (RulePersistenceResult, error) {
	s.ownerID, s.rule, s.hysteresis, s.expiresAt = ownerID, rule, hysteresis, expiresAt
	return s.persistenceResult, s.err
}

func (s *alertStoreStub) ThresholdSyncByOwner(_ context.Context, ownerID, plotID, ruleID uint64) (*ThresholdSyncView, error) {
	s.ownerID, s.plotID, s.alertID = ownerID, plotID, ruleID
	return s.syncResult, s.err
}

func (s *alertStoreStub) ListAlertsByOwner(_ context.Context, ownerID uint64, filter ListFilter) ([]AlertListRow, int64, error) {
	s.listCalls++
	s.ownerID, s.filter = ownerID, filter
	return s.rows, s.total, s.err
}

func (s *alertStoreStub) AdminListAlerts(ctx context.Context, filter ListFilter) ([]AlertListRow, int64, error) {
	return s.ListAlertsByOwner(ctx, 0, filter)
}

func (s *alertStoreStub) ConfirmAlertByOwner(_ context.Context, ownerID, alertID uint64, remark string, now time.Time) (*Alert, error) {
	s.ownerID, s.alertID, s.confirmRemark, s.confirmedAt = ownerID, alertID, remark, now
	return s.confirmed, s.err
}

func (s *alertStoreStub) CreateTriggeredAlert(_ context.Context, input TriggerInput, now time.Time) (*TriggerRecord, error) {
	s.triggerInput, s.triggeredAt = input, now
	return s.triggerRecord, s.err
}

func (s *alertStoreStub) SyncDeviceWarnings(_ context.Context, input DeviceWarningInput, _ time.Time) ([]WarningTransition, error) {
	s.warningInput = input
	return s.transitions, s.err
}

func TestListRulesMapsContractFields(t *testing.T) {
	store := &alertStoreStub{rules: []Rule{{
		ID: 2, PlotID: 11, Metric: "soilMoisture", ComparisonOperator: OperatorLT,
		Threshold: decimal.NewFromInt(28), Hysteresis: decimal.NewFromInt(2), DurationSeconds: 300, Enabled: true, Level: LevelMedium,
	}}}
	result, err := NewService(store).ListRules(context.Background(), 7, 11)
	if err != nil || store.ownerID != 7 || store.plotID != 11 || len(result) != 1 {
		t.Fatalf("ListRules() = (%+v, %v), store=%+v", result, err, store)
	}
	if result[0].Operator != OperatorLT || result[0].Value != 28 || result[0].Hysteresis != 2 || result[0].Unit != "%" {
		t.Fatalf("rule = %+v", result[0])
	}
}

func TestUpsertRuleNormalizesAndValidatesInput(t *testing.T) {
	store := &alertStoreStub{persistenceResult: RulePersistenceResult{
		ConfigVersion: 7,
		Deliveries:    []ThresholdDelivery{{Status: ThresholdSyncPending}, {Status: ThresholdSyncPending}},
	}}
	service := NewService(store)
	now := time.Date(2026, 8, 22, 8, 20, 0, 0, time.UTC)
	hysteresis := 2.5
	service.now = func() time.Time { return now }
	result, err := service.UpsertRule(context.Background(), 7, 11, 2, RuleInput{
		Metric: " soilMoisture ", Operator: "lt", Value: 28, Hysteresis: &hysteresis,
		DurationSeconds: 300, Level: "medium", Enabled: true,
	})
	if err != nil || result.ID != 2 || !result.UpdatedAt.Equal(now) || result.ConfigVersion != 7 ||
		result.SyncStatus != ThresholdSyncPending || result.TargetCount != 2 || !store.expiresAt.Equal(now.Add(defaultThresholdAckTimeout)) {
		t.Fatalf("UpsertRule() = (%+v, %v)", result, err)
	}
	if store.ownerID != 7 || store.rule.PlotID != 11 || store.rule.Metric != "soilMoisture" || store.rule.Level != LevelMedium ||
		store.hysteresis == nil || !store.hysteresis.Equal(decimal.RequireFromString("2.5")) {
		t.Fatalf("stored rule = %+v", store.rule)
	}
	if _, err := service.UpsertRule(context.Background(), 7, 11, 0, RuleInput{
		Metric: "soilMoisture", Operator: OperatorLT, Value: 28, DurationSeconds: 60, Level: LevelMedium, Enabled: true,
	}); err != nil || store.rule.ID != 0 {
		t.Fatalf("create UpsertRule() = (%+v, %v)", store.rule, err)
	}
	_, err = service.UpsertRule(context.Background(), 7, 11, 2, RuleInput{Metric: "soilMoisture", Operator: OperatorLT, Level: "urgent"})
	if !isRuleValidationError(err) {
		t.Fatalf("invalid UpsertRule() error = %v", err)
	}
	negativeHysteresis := -1.0
	_, err = service.UpsertRule(context.Background(), 7, 11, 2, RuleInput{
		Metric: "soilMoisture", Operator: OperatorLT, Value: 28, Hysteresis: &negativeHysteresis,
		DurationSeconds: 300, Level: LevelMedium, Enabled: true,
	})
	if !isRuleValidationError(err) {
		t.Fatalf("negative hysteresis error = %v", err)
	}
	for _, input := range []RuleInput{
		{Metric: "unknown", Operator: OperatorLT, Value: 1, DurationSeconds: 1, Level: LevelLow},
		{Metric: "soilMoisture", Operator: OperatorLT, Value: 101, DurationSeconds: 1, Level: LevelLow},
		{Metric: "temperature", Operator: OperatorGT, Value: -51, DurationSeconds: 1, Level: LevelLow},
	} {
		if _, err := service.UpsertRule(context.Background(), 7, 11, 2, input); !isRuleValidationError(err) {
			t.Fatalf("invalid metric threshold %+v error = %v", input, err)
		}
	}
}

func isRuleValidationError(err error) bool {
	var ruleErr *RuleValidationError
	return errors.As(err, &ruleErr)
}

func TestThresholdSyncMapsStoreErrors(t *testing.T) {
	want := &ThresholdSyncView{RuleID: 2, ConfigVersion: 7, Status: ThresholdSyncApplied, TargetCount: 1}
	store := &alertStoreStub{syncResult: want}
	result, err := NewService(store).ThresholdSync(context.Background(), 7, 11, 2)
	if err != nil || result != want || store.ownerID != 7 || store.plotID != 11 || store.alertID != 2 {
		t.Fatalf("ThresholdSync() = (%+v, %v), store=%+v", result, err, store)
	}
	store.err = gorm.ErrRecordNotFound
	if _, err := NewService(store).ThresholdSync(context.Background(), 7, 11, 2); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ThresholdSync missing error = %v", err)
	}
}

func TestListDefaultsAndMapsLegacyConfirmedState(t *testing.T) {
	started := time.Date(2026, 8, 22, 8, 20, 0, 0, time.UTC)
	threshold := decimal.NewFromInt(30)
	store := &alertStoreStub{rows: []AlertListRow{{
		ID: 3, PlotID: 11, PlotCode: "A3", Metric: "soilMoisture", Operator: OperatorLT,
		Threshold: &threshold, DurationSeconds: 300, Level: LevelMedium,
		Status: StatusAcknowledged, TriggerValue: decimal.RequireFromString("28.6"), TriggeredAt: started,
	}}, total: 1}
	result, err := NewService(store).List(context.Background(), 7, ListFilter{})
	if err != nil || store.filter.Page != 1 || store.filter.PageSize != 20 || result.Total != 1 {
		t.Fatalf("List() = (%+v, %v), filter=%+v", result, err, store.filter)
	}
	item := result.Items[0]
	if item.Status != StatusConfirmed || item.Title != "A3 地块湿度偏低" || item.CurrentValue != 28.6 || item.ThresholdValue == nil || *item.ThresholdValue != 30 {
		t.Fatalf("item = %+v", item)
	}
}

func TestConfirmTrimsRemarkAndMapsStoreErrors(t *testing.T) {
	confirmedAt := time.Date(2026, 8, 22, 8, 22, 30, 0, time.UTC)
	store := &alertStoreStub{confirmed: &Alert{ID: 3, AcknowledgedAt: &confirmedAt}}
	service := NewService(store)
	now := time.Date(2026, 8, 22, 8, 23, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	result, err := service.Confirm(context.Background(), 7, 3, " 已处理 ")
	if err != nil || result.Status != StatusConfirmed || !result.ConfirmedAt.Equal(confirmedAt) || store.confirmRemark != "已处理" {
		t.Fatalf("Confirm() = (%+v, %v), store=%+v", result, err, store)
	}
	store.err = gorm.ErrRecordNotFound
	if _, err := service.Confirm(context.Background(), 7, 4, "已处理"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Confirm() error = %v, want ErrNotFound", err)
	}
	store.err = ErrConflict
	if _, err := service.Confirm(context.Background(), 7, 3, "已处理"); !errors.Is(err, ErrConflict) {
		t.Fatalf("Confirm() error = %v, want ErrConflict", err)
	}
}

func TestTriggerCreatesDeliveryRecordAndDefaultsTime(t *testing.T) {
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	store := &alertStoreStub{triggerRecord: &TriggerRecord{Alert: Alert{
		ID: 9, Level: LevelMedium, Status: StatusActive, TriggeredAt: now,
	}, Created: true, OwnerID: 7, PlotID: 11, PlotCode: "A3", Metric: "soilMoisture", Operator: OperatorLT}}
	broker := events.NewBroker(10)
	subscription := broker.Subscribe(7, "")
	defer subscription.Close()
	service := NewService(store, broker)
	service.now = func() time.Time { return now }

	result, err := service.Trigger(context.Background(), TriggerInput{
		RuleID: 2, TriggerValue: 28.6, TraceID: " trace-1 ", ForwardBody: json.RawMessage(`{"ruleId":2,"triggerValue":28.6}`),
	})
	if err != nil || result.ID != 9 || !result.Created || result.Status != StatusActive {
		t.Fatalf("Trigger() = (%+v, %v)", result, err)
	}
	if store.triggerInput.TriggeredAt == nil || !store.triggerInput.TriggeredAt.Equal(now) ||
		store.triggerInput.TraceID != "trace-1" || !store.triggeredAt.Equal(now) {
		t.Fatalf("stored trigger = %+v at %s", store.triggerInput, store.triggeredAt)
	}
	select {
	case event := <-subscription.Events:
		if event.Type != events.TypeAlertCreated || event.Payload["alertId"] != uint64(9) || event.Payload["title"] != "A3 地块湿度偏低" {
			t.Fatalf("published event = %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for alert.created")
	}
}

func TestTriggerValidatesAndMapsStoreErrors(t *testing.T) {
	store := &alertStoreStub{triggerRecord: &TriggerRecord{}}
	service := NewService(store)
	if _, err := service.Trigger(context.Background(), TriggerInput{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid Trigger() error = %v", err)
	}
	store.err = gorm.ErrRecordNotFound
	input := TriggerInput{RuleID: 2, TriggerValue: 1, ForwardBody: json.RawMessage(`{"ruleId":2,"triggerValue":1}`)}
	if _, err := service.Trigger(context.Background(), input); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing Trigger() error = %v", err)
	}
	store.err = ErrConflict
	if _, err := service.Trigger(context.Background(), input); !errors.Is(err, ErrConflict) {
		t.Fatalf("disabled Trigger() error = %v", err)
	}
}

func TestSyncDeviceWarningsPublishesCreateAndRecoveryWithoutRule(t *testing.T) {
	now := time.Date(2026, 8, 23, 3, 0, 0, 0, time.UTC)
	warningType := WarningLight
	resolvedAt := now.Add(time.Minute)
	store := &alertStoreStub{transitions: []WarningTransition{
		{Alert: Alert{ID: 10, Level: LevelMedium, TriggeredAt: now, WarningType: &warningType}, Created: true, OwnerID: 7, PlotID: 11, PlotCode: "A3", Metric: "light"},
		{Alert: Alert{ID: 9, ResolvedAt: &resolvedAt, WarningType: &warningType}, Recovered: true, OwnerID: 7, PlotID: 11, PlotCode: "A3", Metric: "light"},
	}}
	broker := events.NewBroker(10)
	subscription := broker.Subscribe(7, "")
	defer subscription.Close()
	service := NewService(store, broker)
	service.now = func() time.Time { return now }
	input := DeviceWarningInput{OwnerID: 7, PlotID: 11, DeviceID: 31,
		Temperature: float64PtrTest(26), SoilMoisture: float64PtrTest(30), Light: float64PtrTest(1000),
		LightWarning: boolPtrTest(true), OccurredAt: now}
	result, err := service.SyncDeviceWarnings(context.Background(), input)
	if err != nil || len(result) != 2 || store.warningInput.DeviceID != 31 {
		t.Fatalf("SyncDeviceWarnings() = (%+v, %v), stored=%+v", result, err, store.warningInput)
	}
	eventsSeen := map[string]bool{}
	for range 2 {
		select {
		case event := <-subscription.Events:
			eventsSeen[event.Type] = true
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for warning event")
		}
	}
	if !eventsSeen[events.TypeAlertCreated] || !eventsSeen[events.TypeAlertRecovered] {
		t.Fatalf("events = %v", eventsSeen)
	}
}

func float64PtrTest(value float64) *float64 { return &value }
func boolPtrTest(value bool) *bool          { return &value }

func TestListMapsDeviceWarningWithoutThreshold(t *testing.T) {
	kind := WarningTemperature
	store := &alertStoreStub{rows: []AlertListRow{{
		ID: 3, PlotID: 11, PlotCode: "A3", WarningType: &kind, Level: LevelMedium,
		Status: StatusActive, TriggerValue: decimal.RequireFromString("35.5"), TriggeredAt: time.Now(),
	}}, total: 1}
	result, err := NewService(store).List(context.Background(), 7, ListFilter{})
	if err != nil || len(result.Items) != 1 {
		t.Fatalf("List() = (%+v, %v)", result, err)
	}
	item := result.Items[0]
	if item.Metric != "temperature" || item.Title != "A3 地块温度警告" || item.ThresholdValue != nil {
		t.Fatalf("device warning item = %+v", item)
	}
}
