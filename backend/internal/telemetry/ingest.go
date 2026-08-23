package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/chenluuo/smart-project/backend/internal/alert"
	"github.com/chenluuo/smart-project/backend/internal/events"
)

// Payload is the complete device-controlled message body. Device identity,
// ownership, binding and time deliberately live in TrustedSource instead.
type Payload struct {
	Temperature         float64 `json:"temperature"`
	SoilMoisture        float64 `json:"soilMoisture"`
	Light               float64 `json:"light"`
	TemperatureWarning  bool    `json:"temperatureWarning"`
	SoilMoistureWarning bool    `json:"soilMoistureWarning"`
	LightWarning        bool    `json:"lightWarning"`
}

// DecodePayload is the strict MQTT payload boundary. Unknown fields (including
// device identity, status, battery or timestamps) and missing fields are
// rejected rather than silently trusted.
func DecodePayload(raw []byte) (Payload, error) {
	type wirePayload struct {
		Temperature         *float64 `json:"temperature"`
		SoilMoisture        *float64 `json:"soilMoisture"`
		Light               *float64 `json:"light"`
		TemperatureWarning  *bool    `json:"temperatureWarning"`
		SoilMoistureWarning *bool    `json:"soilMoistureWarning"`
		LightWarning        *bool    `json:"lightWarning"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var wire wirePayload
	if err := decoder.Decode(&wire); err != nil {
		return Payload{}, ErrInvalidInput
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return Payload{}, ErrInvalidInput
	}
	if wire.Temperature == nil || wire.SoilMoisture == nil || wire.Light == nil ||
		wire.TemperatureWarning == nil || wire.SoilMoistureWarning == nil || wire.LightWarning == nil {
		return Payload{}, ErrInvalidInput
	}
	return Payload{
		Temperature: *wire.Temperature, SoilMoisture: *wire.SoilMoisture, Light: *wire.Light,
		TemperatureWarning: *wire.TemperatureWarning, SoilMoistureWarning: *wire.SoilMoistureWarning,
		LightWarning: *wire.LightWarning,
	}, nil
}

type TrustedSource struct {
	OwnerID  uint64
	PlotID   uint64
	PlotCode string
	DeviceID uint64
}

type activityWriter interface {
	MarkActive(context.Context, uint64, uint64, time.Time) error
}

type warningSynchronizer interface {
	SyncDeviceWarnings(context.Context, alert.DeviceWarningInput) ([]alert.WarningTransition, error)
}

type IngestService struct {
	latest    LatestStore
	activity  activityWriter
	warnings  warningSynchronizer
	publisher events.Publisher
	history   HistoryWriter // 可选：TDengine 历史写入
	now       func() time.Time
}

func NewIngestService(latest LatestStore, activity activityWriter, warnings warningSynchronizer, publisher events.Publisher) *IngestService {
	return &IngestService{latest: latest, activity: activity, warnings: warnings, publisher: publisher, now: time.Now}
}

// ConfigureHistory 注入历史写入器（TDengine），遥测上报时同步记录原始值。
func (s *IngestService) ConfigureHistory(h HistoryWriter) {
	s.history = h
}

func (s *IngestService) Ingest(ctx context.Context, source TrustedSource, payload Payload) (*Latest, error) {
	source.PlotCode = strings.TrimSpace(source.PlotCode)
	if source.OwnerID == 0 || source.PlotID == 0 || source.DeviceID == 0 || source.PlotCode == "" ||
		invalidPayloadNumber(payload.Temperature) || invalidPayloadNumber(payload.SoilMoisture) || invalidPayloadNumber(payload.Light) ||
		payload.SoilMoisture < 0 || payload.SoilMoisture > 100 || payload.Light < 0 {
		return nil, ErrInvalidInput
	}
	at := s.now().UTC()
	if s.warnings != nil {
		if _, err := s.warnings.SyncDeviceWarnings(ctx, alert.DeviceWarningInput{
			OwnerID: source.OwnerID, PlotID: source.PlotID, DeviceID: source.DeviceID,
			Temperature: payload.Temperature, SoilMoisture: payload.SoilMoisture, Light: payload.Light,
			TemperatureWarning: payload.TemperatureWarning, SoilMoistureWarning: payload.SoilMoistureWarning,
			LightWarning: payload.LightWarning, OccurredAt: at,
		}); err != nil {
			return nil, fmt.Errorf("persist device warnings: %w", err)
		}
	}
	latest := Latest{
		PlotID: source.PlotID, SampleTime: at,
		Temperature:  &MetricValue{Value: payload.Temperature, Unit: "°C"},
		SoilMoisture: &MetricValue{Value: payload.SoilMoisture, Unit: "%"},
		Light:        &MetricValue{Value: payload.Light, Unit: "lx"},
		Warnings:     WarningState{Temperature: payload.TemperatureWarning, SoilMoisture: payload.SoilMoistureWarning, Light: payload.LightWarning},
	}
	if err := s.latest.PutLatest(ctx, latest); err != nil {
		return nil, fmt.Errorf("store latest telemetry: %w", err)
	}
	if s.activity != nil {
		if err := s.activity.MarkActive(ctx, source.OwnerID, source.DeviceID, at); err != nil {
			return nil, fmt.Errorf("mark device active: %w", err)
		}
	}
	if s.history != nil {
		// 原始遥测写入 TDengine（批量异步，失败不阻塞 ingest 主链路）
		if err := s.history.Record(ctx, source, at, payload); err != nil {
			return nil, fmt.Errorf("record telemetry history: %w", err)
		}
	}
	if s.publisher != nil {
		_, _ = events.PublishTelemetryUpdated(s.publisher, events.TelemetryUpdated{
			OwnerID: source.OwnerID, PlotID: source.PlotID, PlotCode: source.PlotCode,
			SoilMoisture: &payload.SoilMoisture, Temperature: &payload.Temperature, Light: &payload.Light, SampleTime: at,
		})
	}
	return &latest, nil
}

func invalidPayloadNumber(value float64) bool {
	return math.IsNaN(value) || math.IsInf(value, 0)
}
