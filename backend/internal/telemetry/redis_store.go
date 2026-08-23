package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const latestSchemaVersion = 1

type latestEnvelope struct {
	SchemaVersion int `json:"schemaVersion"`
	Latest
}

type RedisStore struct {
	client *redis.Client
	ttl    time.Duration
}

func NewRedisStore(client *redis.Client, ttl time.Duration) *RedisStore {
	return &RedisStore{client: client, ttl: ttl}
}

func (s *RedisStore) LatestByPlot(ctx context.Context, plotID uint64) (*Latest, error) {
	raw, err := s.client.Get(ctx, latestKey(plotID)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return decodeLatest(raw)
}

func (s *RedisStore) LatestByPlots(ctx context.Context, plotIDs []uint64) ([]Latest, error) {
	if len(plotIDs) == 0 {
		return []Latest{}, nil
	}
	keys := make([]string, len(plotIDs))
	for i, id := range plotIDs {
		keys[i] = latestKey(id)
	}
	values, err := s.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}
	result := make([]Latest, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		raw, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("unexpected Redis telemetry value type %T", value)
		}
		latest, err := decodeLatest([]byte(raw))
		if err != nil {
			return nil, err
		}
		result = append(result, *latest)
	}
	return result, nil
}

func (s *RedisStore) PutLatest(ctx context.Context, latest Latest) error {
	if err := validateLatest(latest); err != nil {
		return err
	}
	latest.SampleTime = latest.SampleTime.UTC()
	key := latestKey(latest.PlotID)
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		err = s.client.Watch(ctx, func(tx *redis.Tx) error {
			raw, err := tx.Get(ctx, key).Bytes()
			if err == nil {
				existing, decodeErr := decodeLatest(raw)
				if decodeErr != nil {
					return decodeErr
				}
				if !latest.SampleTime.After(existing.SampleTime) {
					return nil
				}
			} else if !errors.Is(err, redis.Nil) {
				return err
			}
			encoded, err := json.Marshal(latestEnvelope{SchemaVersion: latestSchemaVersion, Latest: latest})
			if err != nil {
				return err
			}
			_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
				pipe.Set(ctx, key, encoded, s.ttl)
				return nil
			})
			return err
		}, key)
		if !errors.Is(err, redis.TxFailedErr) {
			return err
		}
	}
	return err
}

func decodeLatest(raw []byte) (*Latest, error) {
	var value latestEnvelope
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("decode latest telemetry: %w", err)
	}
	if value.SchemaVersion != latestSchemaVersion {
		return nil, fmt.Errorf("unsupported latest telemetry schema version %d", value.SchemaVersion)
	}
	if err := validateLatest(value.Latest); err != nil {
		return nil, err
	}
	value.Latest.SampleTime = value.Latest.SampleTime.UTC()
	return &value.Latest, nil
}

func validateLatest(value Latest) error {
	if value.PlotID == 0 || value.SampleTime.IsZero() || value.SoilMoisture == nil || value.Temperature == nil || value.Light == nil ||
		value.SoilMoisture.Unit != "%" || value.Temperature.Unit != "°C" || value.Light.Unit != "lx" ||
		invalidMetric(value.SoilMoisture.Value) || invalidMetric(value.Temperature.Value) || invalidMetric(value.Light.Value) {
		return ErrInvalidInput
	}
	return nil
}

func invalidMetric(value float64) bool { return math.IsNaN(value) || math.IsInf(value, 0) }

func latestKey(plotID uint64) string {
	return "agri:v1:telemetry:latest:" + strconv.FormatUint(plotID, 10)
}
