package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/chenluuo/smart-project/backend/internal/alert"
)

type latestStoreStub struct{ written Latest }

func (s *latestStoreStub) LatestByPlot(context.Context, uint64) (*Latest, error) {
	return nil, ErrNotFound
}
func (s *latestStoreStub) LatestByPlots(context.Context, []uint64) ([]Latest, error) { return nil, nil }
func (s *latestStoreStub) PutLatest(_ context.Context, value Latest) error {
	s.written = value
	return nil
}

type activityWriterStub struct {
	ownerID, deviceID uint64
	at                time.Time
}

func (s *activityWriterStub) MarkActive(_ context.Context, ownerID, deviceID uint64, at time.Time) error {
	s.ownerID, s.deviceID, s.at = ownerID, deviceID, at
	return nil
}

type warningSynchronizerStub struct{ input alert.DeviceWarningInput }

func (s *warningSynchronizerStub) SyncDeviceWarnings(_ context.Context, input alert.DeviceWarningInput) ([]alert.WarningTransition, error) {
	s.input = input
	return nil, nil
}

func TestIngestAcceptsOnlyMeasurementsAndServerSuppliesRouting(t *testing.T) {
	latest, activity, warnings := &latestStoreStub{}, &activityWriterStub{}, &warningSynchronizerStub{}
	service := NewIngestService(latest, activity, warnings, nil)
	now := time.Date(2026, 8, 23, 2, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	payload := Payload{Temperature: 26.5, SoilMoisture: 31.2, Light: 880, TemperatureWarning: true}
	result, err := service.Ingest(context.Background(), TrustedSource{OwnerID: 7, PlotID: 11, PlotCode: "A3", DeviceID: 31}, payload)
	if err != nil || result.Light == nil || result.Light.Value != 880 || !result.Warnings.Temperature {
		t.Fatalf("Ingest() = (%+v, %v)", result, err)
	}
	if warnings.input.OwnerID != 7 || warnings.input.DeviceID != 31 || !warnings.input.OccurredAt.Equal(now) || activity.deviceID != 31 || !latest.written.SampleTime.Equal(now) {
		t.Fatalf("routing warning=%+v activity=%+v latest=%+v", warnings.input, activity, latest.written)
	}
	raw, _ := json.Marshal(payload)
	for _, forbidden := range []string{"deviceId", "ownerId", "plotId", "receivedAt", "status", "battery", "signal"} {
		if json.Valid(raw) && containsJSONField(raw, forbidden) {
			t.Fatalf("device payload contains forbidden field %q: %s", forbidden, raw)
		}
	}
}

func containsJSONField(raw []byte, field string) bool {
	var value map[string]any
	_ = json.Unmarshal(raw, &value)
	_, ok := value[field]
	return ok
}

func TestDecodePayloadRejectsIdentityUnknownAndMissingFields(t *testing.T) {
	valid := []byte(`{"temperature":26,"soilMoisture":30,"light":900,"temperatureWarning":false,"soilMoistureWarning":false,"lightWarning":true}`)
	payload, err := DecodePayload(valid)
	if err != nil || payload.Light != 900 || !payload.LightWarning {
		t.Fatalf("DecodePayload(valid) = (%+v, %v)", payload, err)
	}
	for _, raw := range [][]byte{
		[]byte(`{"temperature":26,"soilMoisture":30,"light":900,"temperatureWarning":false,"soilMoistureWarning":false,"lightWarning":false,"deviceId":31}`),
		[]byte(`{"temperature":26,"soilMoisture":30,"light":900}`),
	} {
		if _, err := DecodePayload(raw); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("DecodePayload(%s) error = %v, want ErrInvalidInput", raw, err)
		}
	}
}
