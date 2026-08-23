package plot

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"
)

type plotStoreStub struct {
	plots     []Plot
	plot      *Plot
	listErr   error
	getErr    error
	ownerID   uint64
	plotID    uint64
	listCalls int
}

func (s *plotStoreStub) FindByOwner(_ context.Context, ownerID uint64) ([]Plot, error) {
	s.listCalls++
	s.ownerID = ownerID
	return s.plots, s.listErr
}

func (s *plotStoreStub) FindByIDAndOwner(_ context.Context, plotID, ownerID uint64) (*Plot, error) {
	s.plotID = plotID
	s.ownerID = ownerID
	return s.plot, s.getErr
}

func TestServiceScopesPlotsToOwner(t *testing.T) {
	store := &plotStoreStub{plots: []Plot{{ID: 11, OwnerID: 7, Code: "A1"}}, plot: &Plot{ID: 11, OwnerID: 7, Code: "A1"}}
	service := NewService(store)

	results, err := service.List(context.Background(), 7)
	if err != nil || len(results) != 1 || store.ownerID != 7 {
		t.Fatalf("List() = (%+v, %v), ownerID = %d", results, err, store.ownerID)
	}
	result, err := service.Get(context.Background(), 7, 11)
	if err != nil || result.ID != 11 || store.ownerID != 7 || store.plotID != 11 {
		t.Fatalf("Get() = (%+v, %v), ownerID = %d, plotID = %d", result, err, store.ownerID, store.plotID)
	}
}

func TestServiceMapsMissingPlot(t *testing.T) {
	service := NewService(&plotStoreStub{getErr: gorm.ErrRecordNotFound})
	if _, err := service.Get(context.Background(), 7, 99); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() error = %v, want ErrNotFound", err)
	}
}

func TestServiceWrapsStoreFailures(t *testing.T) {
	databaseErr := errors.New("database unavailable")
	service := NewService(&plotStoreStub{listErr: databaseErr, getErr: databaseErr})
	if _, err := service.List(context.Background(), 7); !errors.Is(err, databaseErr) {
		t.Fatalf("List() error = %v, want wrapped database error", err)
	}
	if _, err := service.Get(context.Background(), 7, 11); !errors.Is(err, databaseErr) {
		t.Fatalf("Get() error = %v, want wrapped database error", err)
	}
}
