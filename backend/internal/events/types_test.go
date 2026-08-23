package events

import (
	"errors"
	"testing"
	"time"
)

func TestTypedPublishersCreateAllDocumentedEventTypes(t *testing.T) {
	broker := NewBroker(10)
	subscription := broker.Subscribe(7, "")
	defer subscription.Close()
	now := time.Date(2026, 8, 22, 8, 21, 0, 0, time.FixedZone("CST", 8*60*60))
	soilMoisture, temperature := 27.8, 26.8
	lastSeenAt := now.Add(-time.Minute)
	ackAt := now.Add(12 * time.Second)

	published := []struct {
		name         string
		publish      func() (Event, error)
		wantType     string
		wantResource string
	}{
		{name: "telemetry updated", wantType: TypeTelemetryUpdated, wantResource: "11", publish: func() (Event, error) {
			return PublishTelemetryUpdated(broker, TelemetryUpdated{
				OwnerID: 7, PlotID: 11, PlotCode: "A3", SoilMoisture: &soilMoisture,
				Temperature: &temperature, SampleTime: now,
			})
		}},
		{name: "alert created", wantType: TypeAlertCreated, wantResource: "21", publish: func() (Event, error) {
			return PublishAlertCreated(broker, AlertCreated{
				OwnerID: 7, AlertID: 21, PlotID: 11, Level: "MEDIUM",
				Title: "A3 地块湿度偏低", CreatedAt: now,
			})
		}},
		{name: "alert recovered", wantType: TypeAlertRecovered, wantResource: "21", publish: func() (Event, error) {
			return PublishAlertRecovered(broker, AlertRecovered{OwnerID: 7, AlertID: 21, PlotID: 11, RecoveredAt: now})
		}},
		{name: "device status changed", wantType: TypeDeviceStatusChanged, wantResource: "31", publish: func() (Event, error) {
			return PublishDeviceStatusChanged(broker, DeviceStatusChanged{
				OwnerID: 7, DeviceID: 31, Status: "OFFLINE", LastSeenAt: &lastSeenAt, ChangedAt: now,
			})
		}},
		{name: "command result", wantType: TypeCommandResult, wantResource: "cmd-1", publish: func() (Event, error) {
			return PublishCommandResult(broker, CommandResult{
				OwnerID: 7, CommandID: "cmd-1", Status: "SUCCEEDED", PlotID: 11, AckAt: &ackAt, ChangedAt: ackAt,
			})
		}},
	}

	for _, item := range published {
		t.Run(item.name, func(t *testing.T) {
			event, err := item.publish()
			if err != nil {
				t.Fatalf("publish: %v", err)
			}
			if event.Type != item.wantType || event.ResourceID != item.wantResource || event.ID == "" {
				t.Fatalf("event = %+v", event)
			}
			select {
			case received := <-subscription.Events:
				if received.ID != event.ID {
					t.Fatalf("received ID = %q, want %q", received.ID, event.ID)
				}
			case <-time.After(time.Second):
				t.Fatal("timed out waiting for typed event")
			}
		})
	}
}

func TestTypedPublishersValidateRequiredRoutingAndResourceFields(t *testing.T) {
	broker := NewBroker(10)
	_, err := PublishAlertRecovered(broker, AlertRecovered{})
	if !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("error = %v, want ErrInvalidEvent", err)
	}
}
