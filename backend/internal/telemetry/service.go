package telemetry

import "context"

type Service struct {
	latest  LatestStore
	history HistoryStore
}

func NewService(latest LatestStore, history ...HistoryStore) *Service {
	service := &Service{latest: latest, history: NullStore{}}
	if len(history) > 0 && history[0] != nil {
		service.history = history[0]
	}
	return service
}

func (s *Service) LatestByPlot(ctx context.Context, plotID uint64) (*Latest, error) {
	return s.latest.LatestByPlot(ctx, plotID)
}

func (s *Service) LatestByPlots(ctx context.Context, plotIDs []uint64) ([]Latest, error) {
	return s.latest.LatestByPlots(ctx, plotIDs)
}

func (s *Service) History(ctx context.Context, q HistoryQuery) (*History, error) {
	return s.history.History(ctx, q)
}
