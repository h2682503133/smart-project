package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type irrigationEnvelope struct {
	SchemaVersion   int       `json:"schemaVersion"`
	PlotID          uint64    `json:"plotId"`
	State           string    `json:"state"`
	Mode            string    `json:"mode"`
	DurationSeconds int       `json:"durationSeconds"`
	MaxSeconds      int       `json:"maxSeconds"`
	LastCommandID   *string   `json:"lastCommandId,omitempty"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type RedisIrrigationStore struct {
	client *redis.Client
	ttl    time.Duration
}

func NewRedisIrrigationStore(client *redis.Client, ttl time.Duration) *RedisIrrigationStore {
	return &RedisIrrigationStore{client: client, ttl: ttl}
}

func (s *RedisIrrigationStore) Put(ctx context.Context, status IrrigationStatus, at time.Time) error {
	payload := irrigationEnvelope{
		SchemaVersion: 1, PlotID: status.PlotID, State: status.State, Mode: status.Mode,
		DurationSeconds: status.RemainingSeconds, MaxSeconds: status.MaxSeconds,
		LastCommandID: status.LastCommandID, UpdatedAt: at.UTC(),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, irrigationKey(status.PlotID), raw, s.ttl).Err()
}

func (s *RedisIrrigationStore) Get(ctx context.Context, plotID uint64, now time.Time) (*IrrigationStatus, bool, error) {
	raw, err := s.client.Get(ctx, irrigationKey(plotID)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var payload irrigationEnvelope
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, false, fmt.Errorf("decode irrigation snapshot: %w", err)
	}
	if payload.SchemaVersion != 1 || payload.PlotID != plotID {
		return nil, false, fmt.Errorf("invalid irrigation snapshot schema or plot")
	}
	remaining := payload.DurationSeconds
	if payload.State == "ON" && remaining > 0 {
		remaining -= int(now.UTC().Sub(payload.UpdatedAt).Seconds())
		if remaining <= 0 {
			remaining = 0
			payload.State = "OFF"
		}
	}
	return &IrrigationStatus{
		PlotID: plotID, State: payload.State, Mode: payload.Mode, RemainingSeconds: remaining,
		MaxSeconds: payload.MaxSeconds, LastCommandID: payload.LastCommandID,
	}, true, nil
}

func irrigationKey(plotID uint64) string {
	return fmt.Sprintf("agri:v1:irrigation:status:%d", plotID)
}
