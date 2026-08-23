package mqttclient

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/chenluuo/smart-project/backend/internal/telemetry"
)

var ErrInvalidTopic = errors.New("invalid MQTT topic")

type SourceResolver interface {
	ResolveTelemetrySource(context.Context, uint64, string) (telemetry.TrustedSource, error)
}

type Ingestor interface {
	Ingest(context.Context, telemetry.TrustedSource, telemetry.Payload) (*telemetry.Latest, error)
}

type Handler struct {
	prefix   string
	resolver SourceResolver
	ingestor Ingestor
}

func NewHandler(prefix string, resolver SourceResolver, ingestor Ingestor) *Handler {
	return &Handler{prefix: strings.Trim(prefix, "/"), resolver: resolver, ingestor: ingestor}
}

func (h *Handler) Handle(ctx context.Context, topic string, raw []byte) error {
	ownerID, deviceSN, err := parseTelemetryTopic(h.prefix, topic)
	if err != nil {
		return err
	}
	payload, err := telemetry.DecodePayload(raw)
	if err != nil {
		return fmt.Errorf("decode telemetry payload: %w", err)
	}
	source, err := h.resolver.ResolveTelemetrySource(ctx, ownerID, deviceSN)
	if err != nil {
		return fmt.Errorf("resolve telemetry source: %w", err)
	}
	if _, err := h.ingestor.Ingest(ctx, source, payload); err != nil {
		return fmt.Errorf("ingest telemetry: %w", err)
	}
	return nil
}

func parseTelemetryTopic(prefix, topic string) (uint64, string, error) {
	parts := strings.Split(topic, "/")
	if len(parts) != 4 || parts[0] != prefix || parts[2] == "" || parts[3] != "telemetry" {
		return 0, "", ErrInvalidTopic
	}
	ownerID, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil || ownerID == 0 {
		return 0, "", ErrInvalidTopic
	}
	return ownerID, parts[2], nil
}
