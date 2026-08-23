package control

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/chenluuo/smart-project/backend/internal/platform/database"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func TestLatestSuccessfulCommandIgnoresNewerFailure(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set TEST_MYSQL_DSN to run MySQL control integration test")
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
	mobile := "137" + suffix[len(suffix)-8:]
	if err := db.Exec(
		"INSERT INTO users(name, mobile, password_hash, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		"control-owner-"+suffix, mobile, "not-used", "ACTIVE", now, now,
	).Error; err != nil {
		t.Fatalf("insert owner: %v", err)
	}
	var owner struct{ ID uint64 }
	if err := db.Table("users").Select("id").Where("mobile = ?", mobile).Take(&owner).Error; err != nil {
		t.Fatalf("read owner: %v", err)
	}
	if err := db.Exec(
		"INSERT INTO plots(owner_id, code, name, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		owner.ID, "CTRL-"+suffix, "控制集成测试地块", "ACTIVE", now, now,
	).Error; err != nil {
		t.Fatalf("insert plot: %v", err)
	}
	var plot struct{ ID uint64 }
	if err := db.Table("plots").Select("id").Where("owner_id = ? AND code = ?", owner.ID, "CTRL-"+suffix).Take(&plot).Error; err != nil {
		t.Fatalf("read plot: %v", err)
	}
	if err := db.Exec(
		"INSERT INTO devices(device_code, serial_no, name, device_type, status, credential_status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		"dev_"+suffix, "sn_"+suffix, "测试阀门", "IRRIGATION_VALVE", "ONLINE", "ACTIVE", now, now,
	).Error; err != nil {
		t.Fatalf("insert device: %v", err)
	}
	var valve struct{ ID uint64 }
	if err := db.Table("devices").Select("id").Where("serial_no = ?", "sn_"+suffix).Take(&valve).Error; err != nil {
		t.Fatalf("read device: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM device_commands WHERE issued_by = ?", owner.ID)
		db.Exec("DELETE FROM devices WHERE id = ?", valve.ID)
		db.Exec("DELETE FROM plots WHERE id = ?", plot.ID)
		db.Exec("DELETE FROM users WHERE id = ?", owner.ID)
	})

	if err := db.Exec(`INSERT INTO device_commands(
		command_id, device_id, plot_id, issued_by, action, parameters_json, idempotency_key,
		status, issued_at, expires_at, executed_at, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"cmd-open-"+suffix, valve.ID, plot.ID, owner.ID, ActionIrrigationOn, `{"durationSeconds":600}`,
		"idem-open-"+suffix, StatusSucceeded, now, now.Add(30*time.Second), now, now, now,
	).Error; err != nil {
		t.Fatalf("insert successful open: %v", err)
	}
	if err := db.Exec(`INSERT INTO device_commands(
		command_id, device_id, plot_id, issued_by, action, parameters_json, idempotency_key,
		status, issued_at, expires_at, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"cmd-close-"+suffix, valve.ID, plot.ID, owner.ID, ActionIrrigationOff, `{}`,
		"idem-close-"+suffix, StatusFailed, now.Add(time.Second), now.Add(31*time.Second), now.Add(time.Second), now.Add(time.Second),
	).Error; err != nil {
		t.Fatalf("insert failed close: %v", err)
	}

	command, err := NewRepository(db).FindLatestSuccessfulByDeviceAndPlot(context.Background(), valve.ID, plot.ID)
	if err != nil {
		t.Fatalf("FindLatestSuccessfulByDeviceAndPlot() error = %v", err)
	}
	if command.CommandID != "cmd-open-"+suffix || command.Action != ActionIrrigationOn || command.Status != StatusSucceeded {
		t.Fatalf("latest successful command = %+v", command)
	}
}
