package redisstore

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/chenluuo/smart-project/backend/internal/config"
	"github.com/redis/go-redis/v9"
)

func TestGetJSONDeletesCorruptCacheEntry(t *testing.T) {
	server := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	client := &Client{Client: redisClient}
	server.Set("broken", "not-json")
	var destination map[string]any
	if hit, err := client.GetJSON(context.Background(), "broken", &destination); err == nil || hit {
		t.Fatalf("GetJSON() = (hit=%t, error=%v), want decode error", hit, err)
	}
	if server.Exists("broken") {
		t.Fatal("corrupt cache entry was not deleted")
	}
}

func TestRedisIntegration(t *testing.T) {
	if os.Getenv("TEST_REDIS_INTEGRATION") != "1" {
		t.Skip("set TEST_REDIS_INTEGRATION=1 to run Redis integration test")
	}
	url := os.Getenv("TEST_REDIS_URL")
	if url == "" {
		url = "redis://localhost:6379/15"
	}
	client, err := Open(context.Background(), config.RedisConfig{
		URL: url, PoolSize: 2, DialTimeout: 2 * time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	key := "agri:v1:test:integration"
	if err := client.Set(context.Background(), key, "ok", time.Minute).Err(); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Del(context.Background(), key).Err() })
	if value, err := client.Get(context.Background(), key).Result(); err != nil || value != "ok" {
		t.Fatalf("Get() = (%q, %v)", value, err)
	}
}
