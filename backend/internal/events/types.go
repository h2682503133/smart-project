package events

import (
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	TypeTelemetryUpdated    = "telemetry.updated"
	TypeAlertCreated        = "alert.created"
	TypeAlertRecovered      = "alert.recovered"
	TypeDeviceStatusChanged = "device.status.changed"
	TypeCommandResult       = "command.result"
)

type Publisher interface {
	Publish(Event) (Event, error)
}

func ValidType(eventType string) bool {
	switch eventType {
	case TypeTelemetryUpdated, TypeAlertCreated, TypeAlertRecovered, TypeDeviceStatusChanged, TypeCommandResult:
		return true
	default:
		return false
	}
}

type TelemetryUpdated struct {
	OwnerID      uint64
	PlotID       uint64
	PlotCode     string
	SoilMoisture *float64
	Temperature  *float64
	Light        *float64
	SampleTime   time.Time
}

type AlertCreated struct {
	OwnerID   uint64
	AlertID   uint64
	PlotID    uint64
	Level     string
	Title     string
	CreatedAt time.Time
}

type AlertRecovered struct {
	OwnerID     uint64
	AlertID     uint64
	PlotID      uint64
	RecoveredAt time.Time
}

type DeviceStatusChanged struct {
	OwnerID    uint64
	DeviceID   uint64
	Status     string
	LastSeenAt *time.Time
	ChangedAt  time.Time
}

type CommandResult struct {
	OwnerID   uint64
	CommandID string
	Status    string
	PlotID    uint64
	AckAt     *time.Time
	ChangedAt time.Time
}

func PublishTelemetryUpdated(publisher Publisher, update TelemetryUpdated) (Event, error) {
	update.PlotCode = strings.TrimSpace(update.PlotCode)
	if publisher == nil || update.OwnerID == 0 || update.PlotID == 0 || update.PlotCode == "" || update.SampleTime.IsZero() ||
		update.SoilMoisture == nil && update.Temperature == nil && update.Light == nil || invalidNumber(update.SoilMoisture) || invalidNumber(update.Temperature) || invalidNumber(update.Light) {
		return Event{}, ErrInvalidEvent
	}
	payload := map[string]any{
		"plotId":     update.PlotID,
		"plotCode":   update.PlotCode,
		"sampleTime": update.SampleTime.Format(time.RFC3339Nano),
	}
	if update.SoilMoisture != nil {
		payload["soilMoisture"] = *update.SoilMoisture
	}
	if update.Temperature != nil {
		payload["temperature"] = *update.Temperature
	}
	if update.Light != nil {
		payload["light"] = *update.Light
	}
	return publisher.Publish(Event{
		Type: TypeTelemetryUpdated, OwnerID: update.OwnerID, EventTime: update.SampleTime,
		ResourceID: resourceID(update.PlotID), Payload: payload,
	})
}

func PublishAlertCreated(publisher Publisher, created AlertCreated) (Event, error) {
	created.Level = strings.TrimSpace(created.Level)
	created.Title = strings.TrimSpace(created.Title)
	if publisher == nil || created.OwnerID == 0 || created.AlertID == 0 || created.PlotID == 0 ||
		created.Level == "" || created.Title == "" || created.CreatedAt.IsZero() {
		return Event{}, ErrInvalidEvent
	}
	return publisher.Publish(Event{
		Type: TypeAlertCreated, OwnerID: created.OwnerID, EventTime: created.CreatedAt,
		ResourceID: resourceID(created.AlertID), Payload: map[string]any{
			"alertId": created.AlertID, "plotId": created.PlotID, "level": created.Level,
			"title": created.Title, "createdAt": created.CreatedAt.Format(time.RFC3339Nano),
		},
	})
}

func PublishAlertRecovered(publisher Publisher, recovered AlertRecovered) (Event, error) {
	if publisher == nil || recovered.OwnerID == 0 || recovered.AlertID == 0 || recovered.PlotID == 0 || recovered.RecoveredAt.IsZero() {
		return Event{}, ErrInvalidEvent
	}
	return publisher.Publish(Event{
		Type: TypeAlertRecovered, OwnerID: recovered.OwnerID, EventTime: recovered.RecoveredAt,
		ResourceID: resourceID(recovered.AlertID), Payload: map[string]any{
			"alertId": recovered.AlertID, "plotId": recovered.PlotID,
			"recoveredAt": recovered.RecoveredAt.Format(time.RFC3339Nano),
		},
	})
}

func PublishDeviceStatusChanged(publisher Publisher, changed DeviceStatusChanged) (Event, error) {
	changed.Status = strings.TrimSpace(changed.Status)
	if publisher == nil || changed.OwnerID == 0 || changed.DeviceID == 0 || changed.Status == "" || changed.ChangedAt.IsZero() {
		return Event{}, ErrInvalidEvent
	}
	payload := map[string]any{"deviceId": changed.DeviceID, "status": changed.Status}
	if changed.LastSeenAt != nil {
		payload["lastSeenAt"] = changed.LastSeenAt.Format(time.RFC3339Nano)
	}
	return publisher.Publish(Event{
		Type: TypeDeviceStatusChanged, OwnerID: changed.OwnerID, EventTime: changed.ChangedAt,
		ResourceID: resourceID(changed.DeviceID), Payload: payload,
	})
}

func PublishCommandResult(publisher Publisher, result CommandResult) (Event, error) {
	result.CommandID = strings.TrimSpace(result.CommandID)
	result.Status = strings.TrimSpace(result.Status)
	if publisher == nil || result.OwnerID == 0 || result.CommandID == "" || result.Status == "" || result.PlotID == 0 || result.ChangedAt.IsZero() {
		return Event{}, ErrInvalidEvent
	}
	payload := map[string]any{"commandId": result.CommandID, "status": result.Status, "plotId": result.PlotID}
	if result.AckAt != nil {
		payload["ackAt"] = result.AckAt.Format(time.RFC3339Nano)
	}
	return publisher.Publish(Event{
		Type: TypeCommandResult, OwnerID: result.OwnerID, EventTime: result.ChangedAt,
		ResourceID: result.CommandID, Payload: payload,
	})
}

func resourceID(id uint64) string {
	return strconv.FormatUint(id, 10)
}

func invalidNumber(value *float64) bool {
	return value != nil && (math.IsNaN(*value) || math.IsInf(*value, 0))
}
