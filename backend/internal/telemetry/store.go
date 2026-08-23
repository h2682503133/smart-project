package telemetry

import (
	"context"
	"errors"
)

// ErrNotFound 表示地块暂无遥测数据。
var ErrNotFound = errors.New("telemetry not found")
var ErrInvalidInput = errors.New("invalid telemetry input")

// Store 抽象遥测最新值的读取，未来由 Redis 实现。
type LatestStore interface {
	LatestByPlot(ctx context.Context, plotID uint64) (*Latest, error)
	// LatestByPlots 批量返回多个地块的最新遥测；未接入遥测的地块不出现在结果中。
	LatestByPlots(ctx context.Context, plotIDs []uint64) ([]Latest, error)
	PutLatest(ctx context.Context, latest Latest) error
}

type HistoryStore interface {
	// History 返回某地块单个指标在时间窗口内的聚合趋势。
	History(ctx context.Context, q HistoryQuery) (*History, error)
}

// NullStore 在遥测尚未接入（Redis/TDengine 未实现）时使用，恒返回 ErrNotFound。
type NullStore struct{}

func (NullStore) LatestByPlot(_ context.Context, _ uint64) (*Latest, error) {
	return nil, ErrNotFound
}

func (NullStore) LatestByPlots(_ context.Context, _ []uint64) ([]Latest, error) {
	return []Latest{}, nil
}

func (NullStore) PutLatest(_ context.Context, _ Latest) error { return nil }

func (NullStore) History(_ context.Context, q HistoryQuery) (*History, error) {
	return &History{PlotID: q.PlotID, Metric: q.Metric, Points: []HistoryPoint{}}, nil
}
