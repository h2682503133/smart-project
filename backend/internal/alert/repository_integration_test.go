package alert

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/chenluuo/smart-project/backend/internal/platform/database"
	"github.com/shopspring/decimal"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func TestCreateTriggeredAlertPersistsOwnerNotificationAndOriginalAgentRequest(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set TEST_MYSQL_DSN to run MySQL alert integration test")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open MySQL: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get SQL DB: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := database.Migrate(context.Background(), sqlDB); err != nil {
		t.Fatalf("migrate MySQL: %v", err)
	}

	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	mobile := "139" + suffix[len(suffix)-8:]
	plotCode := "A3-" + suffix
	userResult := db.Exec("INSERT INTO users(name, mobile, password_hash, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		"alert-owner-"+suffix, mobile, "not-used", "ACTIVE", now, now)
	if userResult.Error != nil {
		t.Fatalf("insert owner: %v", userResult.Error)
	}
	var owner struct{ ID uint64 }
	if err := db.Table("users").Select("id").Where("mobile = ?", mobile).Take(&owner).Error; err != nil {
		t.Fatalf("read owner: %v", err)
	}
	plotResult := db.Exec("INSERT INTO plots(owner_id, code, name, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		owner.ID, plotCode, "集成测试地块", "ACTIVE", now, now)
	if plotResult.Error != nil {
		t.Fatalf("insert plot: %v", plotResult.Error)
	}
	var plot struct{ ID uint64 }
	if err := db.Table("plots").Select("id").Where("owner_id = ? AND code = ?", owner.ID, plotCode).Take(&plot).Error; err != nil {
		t.Fatalf("read plot: %v", err)
	}
	rule := Rule{
		PlotID: plot.ID, Name: "soil moisture low", Metric: "soilMoisture",
		ComparisonOperator: OperatorLT, Threshold: decimal.NewFromInt(30), DurationSeconds: 300,
		Hysteresis: decimal.NewFromInt(2), Level: LevelHigh, Enabled: true,
	}
	if err := db.Create(&rule).Error; err != nil {
		t.Fatalf("insert rule: %v", err)
	}
	var createdAlertID uint64
	t.Cleanup(func() {
		if createdAlertID != 0 {
			db.Exec("DELETE FROM outbox_events WHERE aggregate_type = ? AND aggregate_id = ?", "ALERT", strconv.FormatUint(createdAlertID, 10))
		}
		db.Exec("DELETE FROM notifications WHERE alert_id IN (SELECT id FROM alerts WHERE rule_id = ?)", rule.ID)
		db.Exec("DELETE FROM alerts WHERE rule_id = ?", rule.ID)
		db.Exec("DELETE FROM alert_rules WHERE id = ?", rule.ID)
		db.Exec("DELETE FROM plots WHERE id = ?", plot.ID)
		db.Exec("DELETE FROM users WHERE id = ?", owner.ID)
	})
	repository := NewRepositories(db)
	updatedRule := rule
	updatedRule.Threshold = decimal.NewFromInt(29)
	updatedRule.Hysteresis = decimal.Zero
	if err := repository.UpsertRuleByOwner(context.Background(), owner.ID, &updatedRule, nil); err != nil {
		t.Fatalf("update rule without hysteresis: %v", err)
	}
	var storedRule Rule
	if err := db.First(&storedRule, rule.ID).Error; err != nil {
		t.Fatalf("read updated rule: %v", err)
	}
	if !storedRule.Threshold.Equal(decimal.NewFromInt(29)) || !storedRule.Hysteresis.Equal(decimal.NewFromInt(2)) {
		t.Fatalf("updated rule threshold=%s hysteresis=%s", storedRule.Threshold, storedRule.Hysteresis)
	}

	original := json.RawMessage(`{"ruleId":1,"triggerValue":28.6,"extra":{"source":"telemetry"}}`)
	input := TriggerInput{RuleID: rule.ID, TriggerValue: 28.6, TriggeredAt: &now, TraceID: "trace-1", ForwardBody: original}
	first, err := repository.CreateTriggeredAlert(context.Background(), input, now)
	if err != nil || !first.Created {
		t.Fatalf("first CreateTriggeredAlert() = (%+v, %v)", first, err)
	}
	createdAlertID = first.Alert.ID
	if first.OwnerID != owner.ID || first.PlotID != plot.ID || first.PlotCode != plotCode || first.Metric != rule.Metric {
		t.Fatalf("trigger routing metadata = %+v", first)
	}
	second, err := repository.CreateTriggeredAlert(context.Background(), input, now.Add(time.Second))
	if err != nil || second.Created || second.Alert.ID != first.Alert.ID {
		t.Fatalf("duplicate CreateTriggeredAlert() = (%+v, %v), first=%+v", second, err, first)
	}
	confirmedAt := now.Add(2 * time.Second)
	confirmed, err := repository.ConfirmAlertByOwner(context.Background(), owner.ID, first.Alert.ID, "已处理", confirmedAt)
	if err != nil || confirmed.AcknowledgedAt == nil || !confirmed.AcknowledgedAt.Equal(confirmedAt) {
		t.Fatalf("first ConfirmAlertByOwner() = (%+v, %v)", confirmed, err)
	}
	retried, err := repository.ConfirmAlertByOwner(context.Background(), owner.ID, first.Alert.ID, "重复确认", now.Add(3*time.Second))
	if err != nil || retried.AcknowledgedAt == nil || !retried.AcknowledgedAt.Equal(confirmedAt) ||
		retried.ConfirmationRemark == nil || *retried.ConfirmationRemark != "已处理" {
		t.Fatalf("retry ConfirmAlertByOwner() = (%+v, %v)", retried, err)
	}

	var alertCount, notificationCount, outboxCount int64
	db.Table("alerts").Where("id = ?", first.Alert.ID).Count(&alertCount)
	db.Table("notifications").Where("alert_id = ? AND user_id = ? AND channel = ? AND status = ?", first.Alert.ID, owner.ID, "IN_APP", "SENT").Count(&notificationCount)
	db.Table("outbox_events").Where("aggregate_type = ? AND aggregate_id = ? AND event_type = ?", "ALERT", first.Alert.ID, "ALERT_TRIGGERED").Count(&outboxCount)
	if alertCount != 1 || notificationCount != 1 || outboxCount != 1 {
		t.Fatalf("counts alert=%d notification=%d outbox=%d", alertCount, notificationCount, outboxCount)
	}
	var outboxRow struct{ Payload []byte }
	if err := db.Table("outbox_events").Select("payload").Where("aggregate_type = ? AND aggregate_id = ?", "ALERT", first.Alert.ID).Take(&outboxRow).Error; err != nil {
		t.Fatalf("read outbox payload: %v", err)
	}
	var want, got any
	if json.Unmarshal(original, &want) != nil || json.Unmarshal(outboxRow.Payload, &got) != nil || !equalJSON(want, got) {
		t.Fatalf("forwarded payload = %s, want semantic JSON %s", outboxRow.Payload, original)
	}
}

func equalJSON(left, right any) bool {
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return string(leftJSON) == string(rightJSON)
}

func TestSyncDeviceWarningsCreatesRecoversAndRetriggers(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set TEST_MYSQL_DSN to run MySQL warning integration test")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open MySQL: %v", err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := database.Migrate(context.Background(), sqlDB); err != nil {
		t.Fatalf("migrate MySQL: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	mobile := "138" + suffix[len(suffix)-8:]
	if err := db.Exec("INSERT INTO users(name, mobile, password_hash, status, created_at, updated_at) VALUES (?, ?, ?, 'ACTIVE', ?, ?)", "warning-owner-"+suffix, mobile, "unused", now, now).Error; err != nil {
		t.Fatalf("insert owner: %v", err)
	}
	var owner struct{ ID uint64 }
	db.Table("users").Select("id").Where("mobile = ?", mobile).Take(&owner)
	plotCode := "W-" + suffix
	if err := db.Exec("INSERT INTO plots(owner_id, code, name, status, created_at, updated_at) VALUES (?, ?, 'warning plot', 'ACTIVE', ?, ?)", owner.ID, plotCode, now, now).Error; err != nil {
		t.Fatalf("insert plot: %v", err)
	}
	var plot struct{ ID uint64 }
	db.Table("plots").Select("id").Where("owner_id = ? AND code = ?", owner.ID, plotCode).Take(&plot)
	deviceCode, serial := "wd-"+suffix, "ws-"+suffix
	if err := db.Exec("INSERT INTO devices(device_code, serial_no, name, device_type, status, credential_status, created_at, updated_at) VALUES (?, ?, 'sensor', 'SOIL', 'OFFLINE', 'ACTIVE', ?, ?)", deviceCode, serial, now, now).Error; err != nil {
		t.Fatalf("insert device: %v", err)
	}
	var device struct{ ID uint64 }
	db.Table("devices").Select("id").Where("device_code = ?", deviceCode).Take(&device)
	if err := db.Exec("INSERT INTO device_bindings(device_id, plot_id, bound_by, bound_at) VALUES (?, ?, ?, ?)", device.ID, plot.ID, owner.ID, now).Error; err != nil {
		t.Fatalf("insert binding: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM notifications WHERE alert_id IN (SELECT id FROM alerts WHERE device_id = ?)", device.ID)
		db.Exec("DELETE FROM alerts WHERE device_id = ?", device.ID)
		db.Exec("DELETE FROM device_bindings WHERE device_id = ?", device.ID)
		db.Exec("DELETE FROM devices WHERE id = ?", device.ID)
		db.Exec("DELETE FROM plots WHERE id = ?", plot.ID)
		db.Exec("DELETE FROM users WHERE id = ?", owner.ID)
	})
	repository := NewRepositories(db)
	input := DeviceWarningInput{
		OwnerID: owner.ID, PlotID: plot.ID, DeviceID: device.ID,
		Temperature: 36.5, SoilMoisture: 25, Light: 1000, TemperatureWarning: true, OccurredAt: now,
	}
	first, err := repository.SyncDeviceWarnings(context.Background(), input, now)
	if err != nil || len(first) != 1 || !first[0].Created || first[0].Alert.RuleID != nil {
		t.Fatalf("first SyncDeviceWarnings() = (%+v, %v)", first, err)
	}
	duplicate, err := repository.SyncDeviceWarnings(context.Background(), input, now.Add(time.Second))
	if err != nil || len(duplicate) != 0 {
		t.Fatalf("duplicate SyncDeviceWarnings() = (%+v, %v)", duplicate, err)
	}
	input.TemperatureWarning = false
	resolved, err := repository.SyncDeviceWarnings(context.Background(), input, now.Add(2*time.Second))
	if err != nil || len(resolved) != 1 || !resolved[0].Recovered {
		t.Fatalf("resolved SyncDeviceWarnings() = (%+v, %v)", resolved, err)
	}
	input.TemperatureWarning = true
	retriggered, err := repository.SyncDeviceWarnings(context.Background(), input, now.Add(3*time.Second))
	if err != nil || len(retriggered) != 1 || !retriggered[0].Created || retriggered[0].Alert.ID == first[0].Alert.ID {
		t.Fatalf("retriggered SyncDeviceWarnings() = (%+v, %v)", retriggered, err)
	}
}
