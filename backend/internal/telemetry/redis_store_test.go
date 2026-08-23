package telemetry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestRedisStoreLatestRoundTripBatchOrderingAndTTL(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := NewRedisStore(client, 5*time.Minute)
	now := time.Date(2026, 8, 23, 1, 2, 3, 0, time.UTC)
	latest := completeLatest(11, now, 28, 26, 900)
	if err := store.PutLatest(context.Background(), latest); err != nil {
		t.Fatalf("PutLatest() error = %v", err)
	}
	// Older observations cannot overwrite a newer snapshot.
	older := completeLatest(11, now.Add(-time.Minute), 1, 1, 1)
	if err := store.PutLatest(context.Background(), older); err != nil {
		t.Fatalf("PutLatest(older) error = %v", err)
	}
	got, err := store.LatestByPlot(context.Background(), 11)
	if err != nil || got.Temperature.Value != 26 || got.Light.Value != 900 || !got.SampleTime.Equal(now) {
		t.Fatalf("LatestByPlot() = (%+v, %v)", got, err)
	}
	batch, err := store.LatestByPlots(context.Background(), []uint64{10, 11, 12})
	if err != nil || len(batch) != 1 || batch[0].PlotID != 11 {
		t.Fatalf("LatestByPlots() = (%+v, %v)", batch, err)
	}
	server.FastForward(5*time.Minute + time.Second)
	if _, err := store.LatestByPlot(context.Background(), 11); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired LatestByPlot() error = %v, want ErrNotFound", err)
	}
}

func completeLatest(plotID uint64, at time.Time, soil, temperature, light float64) Latest {
	return Latest{
		PlotID: plotID, SampleTime: at,
		SoilMoisture: &MetricValue{Value: soil, Unit: "%"},
		Temperature:  &MetricValue{Value: temperature, Unit: "°C"},
		Light:        &MetricValue{Value: light, Unit: "lx"},
	}
}
