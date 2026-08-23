package plot

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/chenluuo/smart-project/backend/internal/platform/redisstore"
	"github.com/redis/go-redis/v9"
)

func TestCachedStoreReadsThroughAndReusesPlotList(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	source := &plotStoreStub{plots: []Plot{{ID: 11, OwnerID: 7, Code: "A3"}}}
	store := NewCachedStore(source, &redisstore.Client{Client: client}, time.Minute)
	for range 2 {
		plots, err := store.FindByOwner(context.Background(), 7)
		if err != nil || len(plots) != 1 {
			t.Fatalf("FindByOwner() = (%v, %v)", plots, err)
		}
	}
	if source.listCalls != 1 {
		t.Fatalf("source list calls = %d, want 1", source.listCalls)
	}
}

func TestCachedStoreFallsBackWhenRedisIsUnavailable(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), DialTimeout: 20 * time.Millisecond, ReadTimeout: 20 * time.Millisecond, WriteTimeout: 20 * time.Millisecond, MaxRetries: 0})
	t.Cleanup(func() { _ = client.Close() })
	server.Close()
	source := &plotStoreStub{plots: []Plot{{ID: 11}}}
	store := NewCachedStore(source, &redisstore.Client{Client: client}, time.Minute)
	result, err := store.FindByOwner(context.Background(), 7)
	if err != nil || len(result) != 1 || source.listCalls != 1 {
		t.Fatalf("FindByOwner() = (%v, %v), calls=%d", result, err, source.listCalls)
	}
}
