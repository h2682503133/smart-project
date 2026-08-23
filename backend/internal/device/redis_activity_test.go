package device

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestRedisActivityStoreIsolatesOwnersAndDerivesServiceStatus(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	activity := NewRedisActivityStore(client, 24*time.Hour)
	now := time.Now().UTC()
	if err := activity.MarkActive(context.Background(), 7, 31, now); err != nil {
		t.Fatalf("MarkActive() error = %v", err)
	}
	ids, err := activity.OnlineDeviceIDs(context.Background(), 7, now.Add(-2*time.Minute))
	if err != nil || len(ids) != 1 || ids[0] != 31 {
		t.Fatalf("OnlineDeviceIDs(owner 7) = (%v, %v)", ids, err)
	}
	ids, err = activity.OnlineDeviceIDs(context.Background(), 8, now.Add(-2*time.Minute))
	if err != nil || len(ids) != 0 {
		t.Fatalf("OnlineDeviceIDs(owner 8) = (%v, %v)", ids, err)
	}
	battery, signal := 80, 4
	message := "self reported"
	store := &deviceStoreStub{device: &Device{ID: 31, Status: StatusOffline, Battery: &battery, Signal: &signal, StatusMessage: &message}}
	service := NewService(store, activity)
	service.ConfigureActivityTimeout(2 * time.Minute)
	result, err := service.Status(context.Background(), 7, 31)
	if err != nil || result.Status != StatusOnline || result.LastSeenAt == nil {
		t.Fatalf("Status() = (%+v, %v)", result, err)
	}
	if result.Battery != nil || result.Signal != nil || result.StatusMessage != nil {
		t.Fatalf("device-controlled fields were retained: %+v", result)
	}
}

func TestServiceUsesActivityIDsForOnlineFilter(t *testing.T) {
	activity := &activityStoreStub{onlineIDs: []uint64{31}}
	store := &deviceStoreStub{items: []ListItem{{Device: Device{ID: 31, Status: StatusOffline}, PlotID: 11}}, total: 1}
	service := NewService(store, activity)
	status := StatusOnline
	result, err := service.List(context.Background(), 7, ListFilter{Status: &status})
	if err != nil || result.Total != 1 || store.listFilter.DerivedStatus == nil || *store.listFilter.DerivedStatus != StatusOnline || len(store.listFilter.ActiveDeviceIDs) != 1 {
		t.Fatalf("List() = (%+v, %v), query=%+v", result, err, store.listFilter)
	}
}

type activityStoreStub struct {
	onlineIDs []uint64
	lastSeen  map[uint64]time.Time
}

func (s *activityStoreStub) MarkActive(context.Context, uint64, uint64, time.Time) error { return nil }
func (s *activityStoreStub) LastSeen(context.Context, uint64, []uint64) (map[uint64]time.Time, error) {
	return s.lastSeen, nil
}
func (s *activityStoreStub) OnlineDeviceIDs(context.Context, uint64, time.Time) ([]uint64, error) {
	return s.onlineIDs, nil
}
func (s *activityStoreStub) Forget(context.Context, uint64, uint64) error { return nil }
