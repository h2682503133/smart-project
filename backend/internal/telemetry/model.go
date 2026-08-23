package telemetry

import "time"

// MetricValue 表示某个指标的最新数值与单位。
type MetricValue struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

// Latest 是某个地块的最新遥测快照；指标尚未接入时为 nil。
type Latest struct {
	PlotID       uint64       `json:"plotId"`
	SampleTime   time.Time    `json:"sampleTime"`
	SoilMoisture *MetricValue `json:"soilMoisture,omitempty"`
	Temperature  *MetricValue `json:"temperature,omitempty"`
	Light        *MetricValue `json:"light,omitempty"`
	Warnings     WarningState `json:"warnings"`
}

type WarningState struct {
	Temperature  bool `json:"temperature"`
	SoilMoisture bool `json:"soilMoisture"`
	Light        bool `json:"light"`
}

// HistoryPoint 是某个聚合时间窗口内的统计值。
type HistoryPoint struct {
	Time time.Time `json:"time"`
	Avg  float64   `json:"avg"`
	Min  float64   `json:"min"`
	Max  float64   `json:"max"`
}

// History 是某地块单个指标的历史趋势。
type History struct {
	PlotID uint64
	Metric string
	Points []HistoryPoint
}

// HistoryQuery 描述一次历史趋势查询。
type HistoryQuery struct {
	PlotID    uint64
	Metric    string
	StartTime time.Time
	EndTime   time.Time
	Interval  time.Duration
}
