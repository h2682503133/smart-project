package control

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestRedisIrrigationSnapshotCountsDownAndExpires(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := NewRedisIrrigationStore(client, 35*time.Minute)
	now := time.Date(2026, 8, 23, 4, 0, 0, 0, time.UTC)
	commandID := "cmd_1"
	if err := store.Put(context.Background(), IrrigationStatus{
		PlotID: 11, State: "ON", Mode: "MANUAL", RemainingSeconds: 120,
		MaxSeconds: MaxIrrigationSeconds, LastCommandID: &commandID,
	}, now); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	result, hit, err := store.Get(context.Background(), 11, now.Add(30*time.Second))
	if err != nil || !hit || result.State != "ON" || result.RemainingSeconds != 90 {
		t.Fatalf("Get(countdown) = (%+v, %t, %v)", result, hit, err)
	}
	result, hit, err = store.Get(context.Background(), 11, now.Add(2*time.Minute))
	if err != nil || !hit || result.State != "OFF" || result.RemainingSeconds != 0 {
		t.Fatalf("Get(completed) = (%+v, %t, %v)", result, hit, err)
	}
	server.FastForward(35*time.Minute + time.Second)
	if _, hit, err := store.Get(context.Background(), 11, now); err != nil || hit {
		t.Fatalf("Get(expired) = (hit=%t, error=%v)", hit, err)
	}
}
