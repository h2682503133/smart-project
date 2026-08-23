package redisstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/chenluuo/smart-project/backend/internal/config"
	"github.com/redis/go-redis/v9"
)

// Client owns the Redis connection and the small JSON cache primitive shared by
// domain-specific cache decorators. Domain snapshots use the embedded client
// directly so infrastructure errors remain distinguishable from cache misses.
type Client struct {
	*redis.Client
}

func Open(ctx context.Context, cfg config.RedisConfig) (*Client, error) {
	options, err := redis.ParseURL(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parse REDIS_URL: %w", err)
	}
	options.PoolSize = cfg.PoolSize
	options.DialTimeout = cfg.DialTimeout
	options.ReadTimeout = cfg.ReadTimeout
	options.WriteTimeout = cfg.WriteTimeout
	client := &Client{Client: redis.NewClient(options)}
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping Redis: %w", err)
	}
	return client, nil
}

func (c *Client) PingContext(ctx context.Context) error {
	return c.Ping(ctx).Err()
}

func (c *Client) GetJSON(ctx context.Context, key string, destination any) (bool, error) {
	raw, err := c.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(raw, destination); err != nil {
		if deleteErr := c.Del(ctx, key).Err(); deleteErr != nil {
			slog.Warn("delete corrupt Redis cache entry", "key", key, "error", deleteErr)
		}
		return false, fmt.Errorf("decode Redis value %s: %w", key, err)
	}
	return true, nil
}

func (c *Client) SetJSON(ctx context.Context, key string, value any, ttl time.Duration) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode Redis value %s: %w", key, err)
	}
	return c.Set(ctx, key, raw, ttl).Err()
}

func (c *Client) Version(ctx context.Context, key string) (uint64, error) {
	raw, err := c.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	result, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse cache version %s: %w", key, err)
	}
	return result, nil
}

func (c *Client) BumpVersion(ctx context.Context, key string) error {
	return c.Incr(ctx, key).Err()
}
