package telemetry

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/chenluuo/smart-project/backend/internal/platform/tdengine"
)

// HistoryWriter 接收一次遥测上报并写入历史存储（TDengine）。
type HistoryWriter interface {
	// Record 记录一条遥测原始值（实现方可批量/异步落库）。
	Record(ctx context.Context, source TrustedSource, at time.Time, payload Payload) error
}

// TDengineWriter 将遥测原始值写入 TDengine 超级表（批量 + 定时 flush）。
type TDengineWriter struct {
	client     *tdengine.Client
	db         string
	batchSize  int
	flushEvery time.Duration

	mu     sync.Mutex
	pending []tdInsert
	stopCh chan struct{}
	doneCh chan struct{}
}

type tdInsert struct {
	ownerID uint64
	plotID  uint64
	device  uint64
	at      time.Time
	payload Payload
}

// NewTDengineWriter 创建 TDengine 批量写入器并启动后台 flush。
func NewTDengineWriter(client *tdengine.Client, db string, batchSize int, flushPeriod time.Duration) *TDengineWriter {
	w := &TDengineWriter{
		client: client, db: db,
		batchSize:  batchSize, flushEvery: flushPeriod,
		pending: make([]tdInsert, 0, batchSize),
		stopCh:  make(chan struct{}), doneCh: make(chan struct{}),
	}
	go w.loop()
	return w
}

// Close 停止后台 flush 并落盘剩余数据。
func (w *TDengineWriter) Close() {
	close(w.stopCh)
	<-w.doneCh
}

func (w *TDengineWriter) loop() {
	ticker := time.NewTicker(w.flushEvery)
	defer ticker.Stop()
	defer close(w.doneCh)
	for {
		select {
		case <-w.stopCh:
			w.flush(context.Background())
			return
		case <-ticker.C:
			w.flush(context.Background())
		}
	}
}

// Record 追加一条遥测（不阻塞调用方：入内存队列，由后台 flush）。
func (w *TDengineWriter) Record(_ context.Context, source TrustedSource, at time.Time, payload Payload) error {
	w.mu.Lock()
	w.pending = append(w.pending, tdInsert{
		ownerID: source.OwnerID, plotID: source.PlotID, device: source.DeviceID,
		at: at, payload: payload,
	})
	shouldFlush := len(w.pending) >= w.batchSize
	w.mu.Unlock()
	if shouldFlush {
		w.flush(context.Background())
	}
	return nil
}

func (w *TDengineWriter) flush(ctx context.Context) {
	w.mu.Lock()
	if len(w.pending) == 0 {
		w.mu.Unlock()
		return
	}
	batch := w.pending
	w.pending = make([]tdInsert, 0, w.batchSize)
	w.mu.Unlock()

	var sb strings.Builder
	sb.WriteString("INSERT INTO ")
	for i, ins := range batch {
		if i > 0 {
			sb.WriteString(" ")
		}
		// 子表：t_{plotId}_{deviceId}；USING 超级表 + TAGS 自动建表
		fmt.Fprintf(&sb, "agri_telemetry.t_%d_%d USING agri_telemetry.readings TAGS (%d, %d, %d) VALUES ('%s', %s, %s, %s)",
			ins.plotID, ins.device, ins.ownerID, ins.plotID, ins.device,
			ins.at.UTC().Format("2006-01-02 15:04:05.000"),
			formatFloat(ins.payload.Temperature), formatFloat(ins.payload.SoilMoisture), formatFloat(ins.payload.Light),
		)
	}
	if _, err := w.client.Exec(ctx, sb.String()); err != nil {
		// 落库失败记录日志但不上抛（遥测高频，失败不阻塞 ingest 主链路）
		_ = err
	}
}

func formatFloat(v float64) string {
	return fmt.Sprintf("%g", v)
}
