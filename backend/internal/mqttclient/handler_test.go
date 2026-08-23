package mqttclient

import (
	"context"
	"errors"
	"testing"

	"github.com/chenluuo/smart-project/backend/internal/telemetry"
)

type resolverStub struct {
	ownerID  uint64
	deviceSN string
	source   telemetry.TrustedSource
	err      error
}

func (s *resolverStub) ResolveTelemetrySource(_ context.Context, ownerID uint64, deviceSN string) (telemetry.TrustedSource, error) {
	s.ownerID, s.deviceSN = ownerID, deviceSN
	return s.source, s.err
}

type ingestorStub struct {
	source  telemetry.TrustedSource
	payload telemetry.Payload
	called  bool
}

func (s *ingestorStub) Ingest(_ context.Context, source telemetry.TrustedSource, payload telemetry.Payload) (*telemetry.Latest, error) {
	s.called, s.source, s.payload = true, source, payload
	return &telemetry.Latest{}, nil
}

func TestHandlerRoutesBearPiTelemetry(t *testing.T) {
	source := telemetry.TrustedSource{OwnerID: 7, PlotID: 11, PlotCode: "P-01", DeviceID: 3}
	resolver := &resolverStub{source: source}
	ingestor := &ingestorStub{}
	handler := NewHandler("agri", resolver, ingestor)
	raw := []byte(`{"temperature":26.5,"soilMoisture":48,"light":920,"temperatureWarning":false,"soilMoistureWarning":false,"lightWarning":false}`)

	if err := handler.Handle(context.Background(), "agri/7/BEARPI-HM-NANO-001/telemetry", raw); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if resolver.ownerID != 7 || resolver.deviceSN != "BEARPI-HM-NANO-001" {
		t.Fatalf("resolver route = (%d, %q)", resolver.ownerID, resolver.deviceSN)
	}
	if !ingestor.called || ingestor.source != source || ingestor.payload.Temperature != 26.5 {
		t.Fatalf("unexpected ingest call: %+v %+v", ingestor.source, ingestor.payload)
	}
}

func TestHandlerRejectsUntrustedOrMalformedMessages(t *testing.T) {
	valid := []byte(`{"temperature":26.5,"soilMoisture":48,"light":920,"temperatureWarning":false,"soilMoistureWarning":false,"lightWarning":false}`)
	tests := []struct {
		name  string
		topic string
		raw   []byte
	}{
		{name: "wrong suffix", topic: "agri/7/BEARPI/heartbeat", raw: valid},
		{name: "invalid owner", topic: "agri/not-an-id/BEARPI/telemetry", raw: valid},
		{name: "identity in payload", topic: "agri/7/BEARPI/telemetry", raw: append(valid[:len(valid)-1], []byte(`,"deviceId":3}`)...)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ingestor := &ingestorStub{}
			err := NewHandler("agri", &resolverStub{}, ingestor).Handle(context.Background(), tt.topic, tt.raw)
			if err == nil || ingestor.called {
				t.Fatalf("Handle() = %v, ingest called = %v", err, ingestor.called)
			}
		})
	}
}

func TestHandlerPropagatesUnknownDevice(t *testing.T) {
	want := errors.New("not found")
	err := NewHandler("agri", &resolverStub{err: want}, &ingestorStub{}).Handle(context.Background(), "agri/7/BEARPI/telemetry", []byte(`{"temperature":26.5,"soilMoisture":48,"light":920,"temperatureWarning":false,"soilMoistureWarning":false,"lightWarning":false}`))
	if !errors.Is(err, want) {
		t.Fatalf("Handle() error = %v, want wrapped %v", err, want)
	}
}
