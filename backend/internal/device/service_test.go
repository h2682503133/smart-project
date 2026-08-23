package device

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"
)

type deviceStoreStub struct {
	items      []ListItem
	total      int64
	device     *Device
	listErr    error
	bindErr    error
	unbindErr  error
	statusErr  error
	ownerID    uint64
	deviceID   uint64
	listFilter ListFilter
	bindInput  BindInput
	listCalls  int
}

func (s *deviceStoreStub) ListByOwner(_ context.Context, ownerID uint64, filter ListFilter) ([]ListItem, int64, error) {
	s.listCalls++
	s.ownerID, s.listFilter = ownerID, filter
	return s.items, s.total, s.listErr
}

func (s *deviceStoreStub) Bind(_ context.Context, ownerID uint64, input BindInput) (*Device, error) {
	s.ownerID, s.bindInput = ownerID, input
	return s.device, s.bindErr
}

func (s *deviceStoreStub) Unbind(_ context.Context, ownerID, deviceID uint64) error {
	s.ownerID, s.deviceID = ownerID, deviceID
	return s.unbindErr
}

func (s *deviceStoreStub) FindByIDAndOwner(_ context.Context, deviceID, ownerID uint64) (*Device, error) {
	s.ownerID, s.deviceID = ownerID, deviceID
	return s.device, s.statusErr
}

func TestServiceListDefaultsAndScopesToOwner(t *testing.T) {
	store := &deviceStoreStub{items: []ListItem{{Device: Device{ID: 3}, PlotID: 11}}, total: 1}
	service := NewService(store)
	result, err := service.List(context.Background(), 7, ListFilter{})
	if err != nil || store.ownerID != 7 || store.listFilter.Page != 1 || store.listFilter.PageSize != 20 {
		t.Fatalf("List() = (%+v, %v), ownerID=%d, filter=%+v", result, err, store.ownerID, store.listFilter)
	}
	if result.Total != 1 || len(result.Items) != 1 {
		t.Fatalf("List() result = %+v", result)
	}
}

func TestServiceValidatesDeviceInputs(t *testing.T) {
	service := NewService(&deviceStoreStub{})
	invalidStatus := Status("UNKNOWN")
	if _, err := service.List(context.Background(), 7, ListFilter{Status: &invalidStatus}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("List() error = %v, want ErrInvalidInput", err)
	}
	if _, err := service.Bind(context.Background(), 7, BindInput{PlotID: 11, Name: "sensor", DeviceType: "SOIL"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Bind() error = %v, want ErrInvalidInput", err)
	}
}

func TestServiceMapsMissingOwnedResources(t *testing.T) {
	store := &deviceStoreStub{bindErr: gorm.ErrRecordNotFound, unbindErr: gorm.ErrRecordNotFound, statusErr: gorm.ErrRecordNotFound}
	service := NewService(store)
	if _, err := service.Bind(context.Background(), 7, BindInput{SerialNo: "SN-1", PlotID: 11, Name: "sensor", DeviceType: "SOIL"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Bind() error = %v, want ErrNotFound", err)
	}
	if err := service.Unbind(context.Background(), 7, 3); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Unbind() error = %v, want ErrNotFound", err)
	}
	if _, err := service.Status(context.Background(), 7, 3); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Status() error = %v, want ErrNotFound", err)
	}
}

func TestServicePreservesBindingConflicts(t *testing.T) {
	service := NewService(&deviceStoreStub{bindErr: ErrAlreadyBound})
	_, err := service.Bind(context.Background(), 7, BindInput{SerialNo: " SN-1 ", PlotID: 11, Name: " sensor ", DeviceType: " SOIL "})
	if !errors.Is(err, ErrAlreadyBound) {
		t.Fatalf("Bind() error = %v, want ErrAlreadyBound", err)
	}
}
