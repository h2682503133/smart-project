package device

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/chenluuo/smart-project/backend/internal/platform/redisstore"
	"github.com/redis/go-redis/v9"
)

func TestCachedStoreInvalidatesDeviceQueriesAfterBind(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	source := &deviceStoreStub{items: []ListItem{{Device: Device{ID: 31}, PlotID: 11}}, total: 1, device: &Device{ID: 31}}
	store := NewCachedStore(source, &redisstore.Client{Client: client}, time.Minute)
	filter := ListFilter{Page: 1, PageSize: 20}
	for range 2 {
		if _, _, err := store.ListByOwner(context.Background(), 7, filter); err != nil {
			t.Fatalf("ListByOwner() error = %v", err)
		}
	}
	if source.listCalls != 1 {
		t.Fatalf("source list calls before invalidation = %d, want 1", source.listCalls)
	}
	if _, err := store.Bind(context.Background(), 7, BindInput{SerialNo: "SN", PlotID: 11, Name: "sensor", DeviceType: "SOIL"}); err != nil {
		t.Fatalf("Bind() error = %v", err)
	}
	if _, _, err := store.ListByOwner(context.Background(), 7, filter); err != nil {
		t.Fatalf("ListByOwner(after bind) error = %v", err)
	}
	if source.listCalls != 2 {
		t.Fatalf("source list calls after invalidation = %d, want 2", source.listCalls)
	}
}
