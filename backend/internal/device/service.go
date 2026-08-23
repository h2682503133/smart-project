package device

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
)

var (
	ErrNotFound           = errors.New("device not found")
	ErrAlreadyBound       = errors.New("device is already bound")
	ErrDeviceTypeMismatch = errors.New("device type does not match registered device")
	ErrInvalidInput       = errors.New("invalid device input")
)

type ListFilter struct {
	PlotID     *uint64
	Status     *Status
	DeviceType string
	Page       int
	PageSize   int
	// DerivedStatus and ActiveDeviceIDs are populated by Service after reading
	// server-observed activity. They are never accepted from HTTP callers.
	DerivedStatus   *Status
	ActiveDeviceIDs []uint64
}

type ListItem struct {
	Device Device
	PlotID uint64
}

type ListResult struct {
	Items    []ListItem
	Page     int
	PageSize int
	Total    int64
}

type BindInput struct {
	SerialNo   string
	PlotID     uint64
	Name       string
	DeviceType string
}

type Store interface {
	ListByOwner(context.Context, uint64, ListFilter) ([]ListItem, int64, error)
	Bind(context.Context, uint64, BindInput) (*Device, error)
	Unbind(context.Context, uint64, uint64) error
	FindByIDAndOwner(context.Context, uint64, uint64) (*Device, error)
}

type ActivityStore interface {
	MarkActive(context.Context, uint64, uint64, time.Time) error
	LastSeen(context.Context, uint64, []uint64) (map[uint64]time.Time, error)
	OnlineDeviceIDs(context.Context, uint64, time.Time) ([]uint64, error)
	Forget(context.Context, uint64, uint64) error
}

type Service struct {
	devices      Store
	activity     ActivityStore
	offlineAfter time.Duration
}

func NewService(devices Store, activity ...ActivityStore) *Service {
	service := &Service{devices: devices, offlineAfter: 2 * time.Minute}
	if len(activity) > 0 {
		service.activity = activity[0]
	}
	return service
}

func (s *Service) ConfigureActivityTimeout(value time.Duration) {
	if value > 0 {
		s.offlineAfter = value
	}
}

func (s *Service) List(ctx context.Context, ownerID uint64, filter ListFilter) (ListResult, error) {
	if filter.Page == 0 {
		filter.Page = 1
	}
	if filter.PageSize == 0 {
		filter.PageSize = 20
	}
	if filter.Page < 1 || filter.PageSize < 1 || filter.PageSize > 100 || filter.PlotID != nil && *filter.PlotID == 0 || filter.Status != nil && !ValidStatus(*filter.Status) {
		return ListResult{}, ErrInvalidInput
	}
	filter.DeviceType = strings.TrimSpace(filter.DeviceType)
	if utf8.RuneCountInString(filter.DeviceType) > 64 {
		return ListResult{}, ErrInvalidInput
	}

	queryFilter := filter
	if s.activity != nil && filter.Status != nil && (*filter.Status == StatusOnline || *filter.Status == StatusOffline) {
		activeIDs, err := s.activity.OnlineDeviceIDs(ctx, ownerID, time.Now().UTC().Add(-s.offlineAfter))
		if err != nil {
			return ListResult{}, fmt.Errorf("read device activity: %w", err)
		}
		if *filter.Status == StatusOnline && len(activeIDs) == 0 {
			return ListResult{Items: []ListItem{}, Page: filter.Page, PageSize: filter.PageSize, Total: 0}, nil
		}
		queryFilter.Status = nil
		derivedStatus := *filter.Status
		queryFilter.DerivedStatus = &derivedStatus
		queryFilter.ActiveDeviceIDs = activeIDs
	}

	items, total, err := s.devices.ListByOwner(ctx, ownerID, queryFilter)
	if err != nil {
		return ListResult{}, fmt.Errorf("list devices: %w", err)
	}
	if err := s.applyActivity(ctx, ownerID, items); err != nil {
		return ListResult{}, err
	}
	if items == nil {
		items = []ListItem{}
	}
	return ListResult{Items: items, Page: filter.Page, PageSize: filter.PageSize, Total: total}, nil
}

func (s *Service) Bind(ctx context.Context, ownerID uint64, input BindInput) (*Device, error) {
	input.SerialNo = strings.TrimSpace(input.SerialNo)
	input.Name = strings.TrimSpace(input.Name)
	input.DeviceType = strings.TrimSpace(input.DeviceType)
	if ownerID == 0 || input.PlotID == 0 || input.SerialNo == "" || input.Name == "" || input.DeviceType == "" ||
		utf8.RuneCountInString(input.SerialNo) > 128 || utf8.RuneCountInString(input.Name) > 128 || utf8.RuneCountInString(input.DeviceType) > 64 {
		return nil, ErrInvalidInput
	}
	result, err := s.devices.Bind(ctx, ownerID, input)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("bind device: %w", err)
	}
	clearDynamicDeviceFields(result)
	return result, nil
}

func (s *Service) Unbind(ctx context.Context, ownerID, deviceID uint64) error {
	if ownerID == 0 || deviceID == 0 {
		return ErrInvalidInput
	}
	if err := s.devices.Unbind(ctx, ownerID, deviceID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return fmt.Errorf("unbind device: %w", err)
	}
	if s.activity != nil {
		if err := s.activity.Forget(ctx, ownerID, deviceID); err != nil {
			slog.Warn("remove unbound device activity", "ownerId", ownerID, "deviceId", deviceID, "error", err)
		}
	}
	return nil
}

func (s *Service) Status(ctx context.Context, ownerID, deviceID uint64) (*Device, error) {
	if ownerID == 0 || deviceID == 0 {
		return nil, ErrInvalidInput
	}
	result, err := s.devices.FindByIDAndOwner(ctx, deviceID, ownerID)
	if errors.Is(err, gorm.ErrRecordNotFound) || result == nil {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get device status: %w", err)
	}
	items := []ListItem{{Device: *result}}
	if err := s.applyActivity(ctx, ownerID, items); err != nil {
		return nil, err
	}
	result = &items[0].Device
	return result, nil
}

func (s *Service) applyActivity(ctx context.Context, ownerID uint64, items []ListItem) error {
	ids := make([]uint64, 0, len(items))
	for i := range items {
		clearDynamicDeviceFields(&items[i].Device)
		ids = append(ids, items[i].Device.ID)
	}
	if s.activity == nil || len(ids) == 0 {
		return nil
	}
	lastSeen, err := s.activity.LastSeen(ctx, ownerID, ids)
	if err != nil {
		return fmt.Errorf("read device activity: %w", err)
	}
	cutoff := time.Now().UTC().Add(-s.offlineAfter)
	for i := range items {
		if items[i].Device.Status == StatusUnactivated || items[i].Device.Status == StatusDisabled ||
			items[i].Device.Status == StatusFault || items[i].Device.Status == StatusReconnecting {
			continue
		}
		seenAt, ok := lastSeen[items[i].Device.ID]
		if ok {
			seenAt = seenAt.UTC()
			items[i].Device.LastSeenAt = &seenAt
		}
		if ok && !seenAt.Before(cutoff) {
			items[i].Device.Status = StatusOnline
		} else {
			items[i].Device.Status = StatusOffline
		}
	}
	return nil
}

func clearDynamicDeviceFields(value *Device) {
	if value == nil {
		return
	}
	value.Battery = nil
	value.Signal = nil
	value.StatusMessage = nil
}

func ValidStatus(status Status) bool {
	switch status {
	case StatusUnactivated, StatusOnline, StatusOffline, StatusReconnecting, StatusFault, StatusDisabled:
		return true
	default:
		return false
	}
}
