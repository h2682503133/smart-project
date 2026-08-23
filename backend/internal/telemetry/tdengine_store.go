package telemetry

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/chenluuo/smart-project/backend/internal/platform/tdengine"
)

// TDengineStore 实现 HistoryStore，从 TDengine 按时间桶聚合查询历史趋势。
//
// 数据模型：超级表 <db>.readings
//   - 字段：ts TIMESTAMP, temperature FLOAT, soil_moisture FLOAT, light FLOAT
//   - 标签：owner_id BIGINT, plot_id BIGINT, device_id BIGINT
// 子表命名：t_{plotId}_{deviceId}（INSERT 时 USING 自动建表）。
type TDengineStore struct {
	client *tdengine.Client
	db     string
}

// NewTDengineStore 创建 TDengine 历史存储。
func NewTDengineStore(client *tdengine.Client, db string) *TDengineStore {
	return &TDengineStore{client: client, db: db}
}

var metricColumn = map[string]string{
	"soilMoisture": "soil_moisture",
	"temperature":  "temperature",
	"light":        "light",
}

// History 返回某地块单个指标在时间窗口内的聚合趋势（avg/min/max）。
func (s *TDengineStore) History(ctx context.Context, q HistoryQuery) (*History, error) {
	column, ok := metricColumn[q.Metric]
	if !ok {
		return nil, ErrInvalidInput
	}
	intervalExpr := tdengineInterval(q.Interval)
	sql := fmt.Sprintf(
		"SELECT _wstart AS time, AVG(%s) AS avg, MIN(%s) AS min, MAX(%s) AS max "+
			"FROM %s.readings "+
			"WHERE plot_id = %d AND ts >= '%s' AND ts <= '%s' "+
			"INTERVAL(%s) FILL(PREV)",
		column, column, column,
		s.db, q.PlotID,
		q.StartTime.UTC().Format("2006-01-02 15:04:05"),
		q.EndTime.UTC().Format("2006-01-02 15:04:05"),
		intervalExpr,
	)
	rows, err := s.client.Query(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("tdengine history: %w", err)
	}
	points := make([]HistoryPoint, 0, len(rows))
	for _, row := range rows {
		if len(row) < 4 {
			continue
		}
		timeRaw, _ := row[0].(string)
		t, err := time.Parse("2006-01-02T15:04:05.000Z", timeRaw)
		if err != nil {
			// TDengine REST 可能返回 "2006-01-02T15:04:05Z"（无毫秒）
			t, err = time.Parse(time.RFC3339, timeRaw)
			if err != nil {
				continue
			}
		}
		avg, err1 := parseFloat(row[1])
		min, err2 := parseFloat(row[2])
		max, err3 := parseFloat(row[3])
		if err1 != nil || err2 != nil || err3 != nil {
			continue // 空桶（无 FILL 前导数据）跳过
		}
		points = append(points, HistoryPoint{Time: t.UTC(), Avg: avg, Min: min, Max: max})
	}
	return &History{PlotID: q.PlotID, Metric: q.Metric, Points: points}, nil
}

// tdengineInterval 把 time.Duration 转为 TDengine INTERVAL 表达式（支持 s/m/h/d）。
func tdengineInterval(d time.Duration) string {
	switch {
	case d%(24*time.Hour) == 0:
		return fmt.Sprintf("%dh", int(d/(24*time.Hour))*24)
	case d%time.Hour == 0:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	case d%time.Minute == 0:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	default:
		return fmt.Sprintf("%ds", int(d/time.Second))
	}
}

func parseFloat(v interface{}) (float64, error) {
	switch val := v.(type) {
	case float64:
		return val, nil
	case string:
		return strconv.ParseFloat(strings.TrimSpace(val), 64)
	default:
		return 0, fmt.Errorf("unexpected type %T", v)
	}
}
