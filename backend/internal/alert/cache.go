package alert

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/chenluuo/smart-project/backend/internal/shared/cachex"
	"github.com/shopspring/decimal"
)

type cachedStore struct {
	next  Store
	cache cachex.JSONStore
	ttl   time.Duration
}

type cachedAlertList struct {
	SchemaVersion int            `json:"schemaVersion"`
	Rows          []AlertListRow `json:"rows"`
	Total         int64          `json:"total"`
}

func NewCachedStore(next Store, cache cachex.JSONStore, ttl time.Duration) Store {
	if cache == nil {
		return next
	}
	return &cachedStore{next: next, cache: cache, ttl: ttl}
}

func (s *cachedStore) ListRulesByOwner(ctx context.Context, ownerID, plotID uint64) ([]Rule, error) {
	return s.next.ListRulesByOwner(ctx, ownerID, plotID)
}

func (s *cachedStore) UpsertRuleByOwner(ctx context.Context, ownerID uint64, rule *Rule, hysteresis *decimal.Decimal) error {
	return s.next.UpsertRuleByOwner(ctx, ownerID, rule, hysteresis)
}

func (s *cachedStore) ListAlertsByOwner(ctx context.Context, ownerID uint64, filter ListFilter) ([]AlertListRow, int64, error) {
	if filter.Status == nil || *filter.Status != StatusActive {
		return s.next.ListAlertsByOwner(ctx, ownerID, filter)
	}
	version, err := s.cache.Version(ctx, alertVersionKey(ownerID))
	if err != nil {
		slog.Warn("read alert cache version", "ownerId", ownerID, "error", err)
		return s.next.ListAlertsByOwner(ctx, ownerID, filter)
	}
	key := fmt.Sprintf("agri:v1:cache:alerts:active:owner:%d:g:%d:q:%s", ownerID, version, cachex.Digest(filter))
	var cached cachedAlertList
	if hit, cacheErr := s.cache.GetJSON(ctx, key, &cached); cacheErr == nil && hit && cached.SchemaVersion == cachex.SchemaVersion {
		return cached.Rows, cached.Total, nil
	} else if cacheErr != nil {
		slog.Warn("read active alert cache", "key", key, "error", cacheErr)
	}
	rows, total, err := s.next.ListAlertsByOwner(ctx, ownerID, filter)
	if err == nil {
		if cacheErr := s.cache.SetJSON(ctx, key, cachedAlertList{SchemaVersion: cachex.SchemaVersion, Rows: rows, Total: total}, s.ttl); cacheErr != nil {
			slog.Warn("write active alert cache", "key", key, "error", cacheErr)
		}
	}
	return rows, total, err
}

func (s *cachedStore) ConfirmAlertByOwner(ctx context.Context, ownerID, alertID uint64, remark string, now time.Time) (*Alert, error) {
	result, err := s.next.ConfirmAlertByOwner(ctx, ownerID, alertID, remark, now)
	if err == nil {
		s.bump(ctx, ownerID)
	}
	return result, err
}

func (s *cachedStore) CreateTriggeredAlert(ctx context.Context, input TriggerInput, now time.Time) (*TriggerRecord, error) {
	result, err := s.next.CreateTriggeredAlert(ctx, input, now)
	if err == nil && result != nil && result.Created {
		s.bump(ctx, result.OwnerID)
	}
	return result, err
}

func (s *cachedStore) SyncDeviceWarnings(ctx context.Context, input DeviceWarningInput, now time.Time) ([]WarningTransition, error) {
	result, err := s.next.SyncDeviceWarnings(ctx, input, now)
	if err == nil && len(result) > 0 {
		s.bump(ctx, input.OwnerID)
	}
	return result, err
}

func (s *cachedStore) bump(ctx context.Context, ownerID uint64) {
	if err := s.cache.BumpVersion(ctx, alertVersionKey(ownerID)); err != nil {
		slog.Warn("invalidate active alert cache", "ownerId", ownerID, "error", err)
	}
}

func alertVersionKey(ownerID uint64) string {
	return fmt.Sprintf("agri:v1:cache:alerts:active:owner:%d:version", ownerID)
}
