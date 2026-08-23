package device

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisActivityStore struct {
	client *redis.Client
	ttl    time.Duration
}

func NewRedisActivityStore(client *redis.Client, ttl time.Duration) *RedisActivityStore {
	return &RedisActivityStore{client: client, ttl: ttl}
}

func (s *RedisActivityStore) MarkActive(ctx context.Context, ownerID, deviceID uint64, at time.Time) error {
	key := activityKey(ownerID)
	pipe := s.client.TxPipeline()
	pipe.ZAdd(ctx, key, redis.Z{Score: float64(at.UTC().UnixMilli()), Member: strconv.FormatUint(deviceID, 10)})
	pipe.Expire(ctx, key, s.ttl)
	_, err := pipe.Exec(ctx)
	return err
}

func (s *RedisActivityStore) LastSeen(ctx context.Context, ownerID uint64, deviceIDs []uint64) (map[uint64]time.Time, error) {
	result := make(map[uint64]time.Time, len(deviceIDs))
	pipe := s.client.Pipeline()
	commands := make(map[uint64]*redis.FloatCmd, len(deviceIDs))
	for _, id := range deviceIDs {
		commands[id] = pipe.ZScore(ctx, activityKey(ownerID), strconv.FormatUint(id, 10))
	}
	_, err := pipe.Exec(ctx)
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}
	for id, command := range commands {
		score, err := command.Result()
		if errors.Is(err, redis.Nil) {
			continue
		}
		if err != nil {
			return nil, err
		}
		result[id] = time.UnixMilli(int64(score)).UTC()
	}
	return result, nil
}

func (s *RedisActivityStore) OnlineDeviceIDs(ctx context.Context, ownerID uint64, cutoff time.Time) ([]uint64, error) {
	members, err := s.client.ZRangeByScore(ctx, activityKey(ownerID), &redis.ZRangeBy{
		Min: strconv.FormatInt(cutoff.UTC().UnixMilli(), 10), Max: "+inf",
	}).Result()
	if err != nil {
		return nil, err
	}
	result := make([]uint64, 0, len(members))
	for _, member := range members {
		id, err := strconv.ParseUint(member, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse device activity member %q: %w", member, err)
		}
		result = append(result, id)
	}
	return result, nil
}

func (s *RedisActivityStore) Forget(ctx context.Context, ownerID, deviceID uint64) error {
	return s.client.ZRem(ctx, activityKey(ownerID), strconv.FormatUint(deviceID, 10)).Err()
}

func activityKey(ownerID uint64) string {
	return fmt.Sprintf("agri:v1:device:last_seen:%d", ownerID)
}
