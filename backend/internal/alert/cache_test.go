package alert

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/chenluuo/smart-project/backend/internal/platform/redisstore"
	"github.com/redis/go-redis/v9"
)

func TestCachedStoreInvalidatesActiveAlertsAfterWarningTransition(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	source := &alertStoreStub{rows: []AlertListRow{{ID: 1}}, total: 1}
	store := NewCachedStore(source, &redisstore.Client{Client: client}, 10*time.Second)
	status := StatusActive
	filter := ListFilter{Status: &status, Page: 1, PageSize: 20}
	for range 2 {
		if _, _, err := store.ListAlertsByOwner(context.Background(), 7, filter); err != nil {
			t.Fatalf("ListAlertsByOwner() error = %v", err)
		}
	}
	if source.listCalls != 1 {
		t.Fatalf("source list calls before invalidation = %d, want 1", source.listCalls)
	}
	source.transitions = []WarningTransition{{Created: true, OwnerID: 7}}
	if _, err := store.SyncDeviceWarnings(context.Background(), DeviceWarningInput{OwnerID: 7}, time.Now()); err != nil {
		t.Fatalf("SyncDeviceWarnings() error = %v", err)
	}
	if _, _, err := store.ListAlertsByOwner(context.Background(), 7, filter); err != nil {
		t.Fatalf("ListAlertsByOwner(after warning) error = %v", err)
	}
	if source.listCalls != 2 {
		t.Fatalf("source list calls after invalidation = %d, want 2", source.listCalls)
	}
}
