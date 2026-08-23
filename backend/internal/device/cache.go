package device

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/chenluuo/smart-project/backend/internal/shared/cachex"
)

type cachedStore struct {
	next  Store
	cache cachex.JSONStore
	ttl   time.Duration
}

type cachedList struct {
	SchemaVersion int        `json:"schemaVersion"`
	Items         []ListItem `json:"items"`
	Total         int64      `json:"total"`
}

type cachedDevice struct {
	SchemaVersion int    `json:"schemaVersion"`
	Item          Device `json:"item"`
}

func NewCachedStore(next Store, cache cachex.JSONStore, ttl time.Duration) Store {
	if cache == nil {
		return next
	}
	return &cachedStore{next: next, cache: cache, ttl: ttl}
}

func (s *cachedStore) ListByOwner(ctx context.Context, ownerID uint64, filter ListFilter) ([]ListItem, int64, error) {
	// Activity-filtered queries depend on a rapidly changing Redis sorted set;
	// caching those results would make ONLINE/OFFLINE filters stale.
	if filter.DerivedStatus != nil {
		return s.next.ListByOwner(ctx, ownerID, filter)
	}
	version, err := s.cache.Version(ctx, deviceVersionKey(ownerID))
	if err != nil {
		slog.Warn("read device cache version", "ownerId", ownerID, "error", err)
		return s.next.ListByOwner(ctx, ownerID, filter)
	}
	key := fmt.Sprintf("agri:v1:cache:devices:owner:%d:g:%d:q:%s", ownerID, version, cachex.Digest(filter))
	var cached cachedList
	if hit, cacheErr := s.cache.GetJSON(ctx, key, &cached); cacheErr == nil && hit && cached.SchemaVersion == cachex.SchemaVersion {
		return cached.Items, cached.Total, nil
	} else if cacheErr != nil {
		slog.Warn("read device list cache", "key", key, "error", cacheErr)
	}
	items, total, err := s.next.ListByOwner(ctx, ownerID, filter)
	if err == nil {
		if cacheErr := s.cache.SetJSON(ctx, key, cachedList{SchemaVersion: cachex.SchemaVersion, Items: items, Total: total}, s.ttl); cacheErr != nil {
			slog.Warn("write device list cache", "key", key, "error", cacheErr)
		}
	}
	return items, total, err
}

func (s *cachedStore) Bind(ctx context.Context, ownerID uint64, input BindInput) (*Device, error) {
	result, err := s.next.Bind(ctx, ownerID, input)
	if err == nil {
		s.bump(ctx, ownerID)
	}
	return result, err
}

func (s *cachedStore) Unbind(ctx context.Context, ownerID, deviceID uint64) error {
	err := s.next.Unbind(ctx, ownerID, deviceID)
	if err == nil {
		s.bump(ctx, ownerID)
	}
	return err
}

func (s *cachedStore) FindByIDAndOwner(ctx context.Context, deviceID, ownerID uint64) (*Device, error) {
	version, err := s.cache.Version(ctx, deviceVersionKey(ownerID))
	if err != nil {
		slog.Warn("read device cache version", "ownerId", ownerID, "error", err)
		return s.next.FindByIDAndOwner(ctx, deviceID, ownerID)
	}
	key := fmt.Sprintf("agri:v1:cache:devices:owner:%d:g:%d:id:%d", ownerID, version, deviceID)
	var cached cachedDevice
	if hit, cacheErr := s.cache.GetJSON(ctx, key, &cached); cacheErr == nil && hit && cached.SchemaVersion == cachex.SchemaVersion {
		return &cached.Item, nil
	} else if cacheErr != nil {
		slog.Warn("read device detail cache", "key", key, "error", cacheErr)
	}
	value, err := s.next.FindByIDAndOwner(ctx, deviceID, ownerID)
	if err == nil && value != nil {
		if cacheErr := s.cache.SetJSON(ctx, key, cachedDevice{SchemaVersion: cachex.SchemaVersion, Item: *value}, s.ttl); cacheErr != nil {
			slog.Warn("write device detail cache", "key", key, "error", cacheErr)
		}
	}
	return value, err
}

func (s *cachedStore) bump(ctx context.Context, ownerID uint64) {
	if err := s.cache.BumpVersion(ctx, deviceVersionKey(ownerID)); err != nil {
		slog.Warn("invalidate device cache", "ownerId", ownerID, "error", err)
	}
}

func deviceVersionKey(ownerID uint64) string {
	return fmt.Sprintf("agri:v1:cache:devices:owner:%d:version", ownerID)
}
