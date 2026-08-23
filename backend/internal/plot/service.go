package plot

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

var ErrNotFound = errors.New("plot not found")

type Store interface {
	FindByOwner(ctx context.Context, ownerID uint64) ([]Plot, error)
	FindByIDAndOwner(ctx context.Context, plotID, ownerID uint64) (*Plot, error)
}

type Service struct {
	plots Store
}

func NewService(plots Store) *Service {
	return &Service{plots: plots}
}

func (s *Service) List(ctx context.Context, ownerID uint64) ([]Plot, error) {
	plots, err := s.plots.FindByOwner(ctx, ownerID)
	if err != nil {
		return nil, fmt.Errorf("list plots: %w", err)
	}
	return plots, nil
}

func (s *Service) Get(ctx context.Context, ownerID, plotID uint64) (*Plot, error) {
	result, err := s.plots.FindByIDAndOwner(ctx, plotID, ownerID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get plot: %w", err)
	}
	if result == nil {
		return nil, ErrNotFound
	}
	return result, nil
}
