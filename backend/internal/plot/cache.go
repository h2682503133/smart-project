package plot

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

type cachedPlotList struct {
	SchemaVersion int    `json:"schemaVersion"`
	Items         []Plot `json:"items"`
}

type cachedPlotDetail struct {
	SchemaVersion int  `json:"schemaVersion"`
	Item          Plot `json:"item"`
}

func NewCachedStore(next Store, cache cachex.JSONStore, ttl time.Duration) Store {
	if cache == nil {
		return next
	}
	return &cachedStore{next: next, cache: cache, ttl: ttl}
}

func (s *cachedStore) FindByOwner(ctx context.Context, ownerID uint64) ([]Plot, error) {
	key := fmt.Sprintf("agri:v1:cache:plots:owner:%d:list", ownerID)
	var cached cachedPlotList
	if hit, err := s.cache.GetJSON(ctx, key, &cached); err == nil && hit && cached.SchemaVersion == cachex.SchemaVersion {
		return cached.Items, nil
	} else if err != nil {
		slog.Warn("read plot list cache", "key", key, "error", err)
	}
	result, err := s.next.FindByOwner(ctx, ownerID)
	if err == nil {
		if cacheErr := s.cache.SetJSON(ctx, key, cachedPlotList{SchemaVersion: cachex.SchemaVersion, Items: result}, s.ttl); cacheErr != nil {
			slog.Warn("write plot list cache", "key", key, "error", cacheErr)
		}
	}
	return result, err
}

func (s *cachedStore) FindByIDAndOwner(ctx context.Context, plotID, ownerID uint64) (*Plot, error) {
	key := fmt.Sprintf("agri:v1:cache:plots:owner:%d:id:%d", ownerID, plotID)
	var cached cachedPlotDetail
	if hit, err := s.cache.GetJSON(ctx, key, &cached); err == nil && hit && cached.SchemaVersion == cachex.SchemaVersion {
		return &cached.Item, nil
	} else if err != nil {
		slog.Warn("read plot detail cache", "key", key, "error", err)
	}
	value, err := s.next.FindByIDAndOwner(ctx, plotID, ownerID)
	if err == nil && value != nil {
		if cacheErr := s.cache.SetJSON(ctx, key, cachedPlotDetail{SchemaVersion: cachex.SchemaVersion, Item: *value}, s.ttl); cacheErr != nil {
			slog.Warn("write plot detail cache", "key", key, "error", cacheErr)
		}
	}
	return value, err
}
