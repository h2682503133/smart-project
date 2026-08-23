package control

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/chenluuo/smart-project/backend/internal/device"
	"github.com/chenluuo/smart-project/backend/internal/events"
	"github.com/chenluuo/smart-project/backend/internal/shared/persistence"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const MaxIrrigationSeconds = 1800

var (
	ErrInvalidInput  = errors.New("invalid control input")
	ErrNotFound      = errors.New("control resource not found")
	ErrDeviceOffline = errors.New("irrigation device is offline")
)

type IssueInput struct {
	Action          string
	DurationSeconds int
	Mode            string
	Reason          string
	IdempotencyKey  string
}

type IssueResult struct {
	CommandID string    `json:"commandId"`
	PlotID    uint64    `json:"plotId"`
	Action    string    `json:"action"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

type ListFilter struct {
	PlotID   *uint64
	Status   *Status
	Page     int
	PageSize int
}

type ListItem struct {
	ID              string    `json:"id"`
	PlotCode        string    `json:"plotCode"`
	Action          string    `json:"action"`
	DurationSeconds int       `json:"durationSeconds"`
	Status          Status    `json:"status"`
	OperatorName    string    `json:"operatorName"`
	CreatedAt       time.Time `json:"createdAt"`
}

type ListResult struct {
	Items    []ListItem `json:"items"`
	Page     int        `json:"page"`
	PageSize int        `json:"pageSize"`
	Total    int64      `json:"total"`
}

type IrrigationStatus struct {
	PlotID           uint64
	ValveDeviceID    uint64
	State            string
	Mode             string
	RemainingSeconds int
	MaxSeconds       int
	LastCommandID    *string
}

type CommandResult struct {
	ID             string
	PlotID         uint64
	DeviceID       uint64
	Action         string
	Status         Status
	RequestPayload map[string]any
	AckPayload     map[string]any
	CreatedAt      time.Time
	AckAt          *time.Time
}

type Store interface {
	FindIrrigationDevice(context.Context, uint64, uint64) (*IrrigationDevice, error)
	FindByIdempotencyKeyAndOwner(context.Context, string, uint64) (*Command, error)
	Create(context.Context, *Command) error
	Save(context.Context, *Command) error
	FindLatestSuccessfulByDeviceAndPlot(context.Context, uint64, uint64) (*Command, error)
	FindByCommandIDAndOwner(context.Context, string, uint64) (*Command, error)
	ListByOwner(context.Context, uint64, ListFilter) ([]CommandListRow, int64, error)
}

type Service struct {
	commands  Store
	publisher events.Publisher
	snapshots IrrigationSnapshotStore
	now       func() time.Time
}

type IrrigationSnapshotStore interface {
	Get(context.Context, uint64, time.Time) (*IrrigationStatus, bool, error)
	Put(context.Context, IrrigationStatus, time.Time) error
}

func NewService(commands Store, publishers ...events.Publisher) *Service {
	service := &Service{commands: commands, now: time.Now}
	if len(publishers) > 0 {
		service.publisher = publishers[0]
	}
	return service
}

func (s *Service) ConfigureSnapshotStore(store IrrigationSnapshotStore) {
	s.snapshots = store
}

func (s *Service) Issue(ctx context.Context, ownerID, plotID uint64, input IssueInput) (*IssueResult, error) {
	input.Action = strings.ToUpper(strings.TrimSpace(input.Action))
	input.Mode = strings.ToUpper(strings.TrimSpace(input.Mode))
	input.Reason = strings.TrimSpace(input.Reason)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if ownerID == 0 || plotID == 0 || !validIssueInput(input) {
		return nil, ErrInvalidInput
	}
	if input.IdempotencyKey != "" {
		existing, err := s.commands.FindByIdempotencyKeyAndOwner(ctx, input.IdempotencyKey, ownerID)
		if err == nil && existing != nil {
			return issueResult(existing), nil
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("find idempotent command: %w", err)
		}
	}

	irrigationDevice, err := s.commands.FindIrrigationDevice(ctx, ownerID, plotID)
	if errors.Is(err, gorm.ErrRecordNotFound) || irrigationDevice == nil {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find irrigation device: %w", err)
	}
	if irrigationDevice.Status != device.StatusOnline {
		return nil, ErrDeviceOffline
	}

	commandID, err := newCommandID()
	if err != nil {
		return nil, err
	}
	payload := map[string]any{"mode": input.Mode, "reason": input.Reason}
	if input.Action == "OPEN" {
		payload["durationSeconds"] = input.DurationSeconds
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode irrigation command: %w", err)
	}
	now := s.now()
	command := &Command{
		CommandID: commandID, DeviceID: irrigationDevice.DeviceID, PlotID: plotID, IssuedBy: ownerID,
		Action: internalAction(input.Action), ParametersJSON: datatypes.JSON(payloadJSON),
		IdempotencyKey: input.IdempotencyKey, Status: StatusPending,
		IssuedAt: now, ExpiresAt: now.Add(30 * time.Second),
		Auditable: persistence.Auditable{CreatedAt: now, UpdatedAt: now},
	}
	if err := s.commands.Create(ctx, command); err != nil {
		return nil, fmt.Errorf("create irrigation command: %w", err)
	}
	command.Status = StatusSucceeded
	command.ExecutedAt = &now
	command.UpdatedAt = now
	if err := s.commands.Save(ctx, command); err != nil {
		return nil, fmt.Errorf("complete irrigation command: %w", err)
	}
	if s.snapshots != nil {
		snapshot := IrrigationStatus{
			PlotID: plotID, ValveDeviceID: irrigationDevice.DeviceID, State: "OFF", Mode: input.Mode,
			MaxSeconds: MaxIrrigationSeconds, LastCommandID: &command.CommandID,
		}
		if input.Action == "OPEN" {
			snapshot.State = "ON"
			snapshot.RemainingSeconds = input.DurationSeconds
		}
		if err := s.snapshots.Put(ctx, snapshot, now.UTC()); err != nil {
			slog.Warn("write irrigation Redis snapshot", "plotId", plotID, "commandId", command.CommandID, "error", err)
		}
	}
	if s.publisher != nil {
		_, _ = events.PublishCommandResult(s.publisher, events.CommandResult{
			OwnerID: ownerID, CommandID: command.CommandID, Status: string(command.Status),
			PlotID: plotID, AckAt: command.ExecutedAt, ChangedAt: command.UpdatedAt,
		})
	}
	return issueResult(command), nil
}

func (s *Service) IrrigationStatus(ctx context.Context, ownerID, plotID uint64) (*IrrigationStatus, error) {
	if ownerID == 0 || plotID == 0 {
		return nil, ErrInvalidInput
	}
	irrigationDevice, err := s.commands.FindIrrigationDevice(ctx, ownerID, plotID)
	if errors.Is(err, gorm.ErrRecordNotFound) || irrigationDevice == nil {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find irrigation device: %w", err)
	}

	result := &IrrigationStatus{
		PlotID: irrigationDevice.PlotID, ValveDeviceID: irrigationDevice.DeviceID,
		State: "OFF", Mode: "MANUAL", MaxSeconds: MaxIrrigationSeconds,
	}
	if s.snapshots != nil {
		cached, hit, err := s.snapshots.Get(ctx, plotID, s.now().UTC())
		if err != nil {
			return nil, fmt.Errorf("read irrigation snapshot: %w", err)
		}
		if hit {
			cached.ValveDeviceID = irrigationDevice.DeviceID
			return cached, nil
		}
	}
	command, err := s.commands.FindLatestSuccessfulByDeviceAndPlot(ctx, irrigationDevice.DeviceID, irrigationDevice.PlotID)
	if errors.Is(err, gorm.ErrRecordNotFound) || command == nil {
		return result, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find latest irrigation command: %w", err)
	}
	payload, err := decodePayload(command.ParametersJSON)
	if err != nil {
		return nil, fmt.Errorf("decode latest irrigation command: %w", err)
	}
	result.LastCommandID = &command.CommandID
	if mode, ok := payload["mode"].(string); ok && strings.TrimSpace(mode) != "" {
		result.Mode = strings.ToUpper(strings.TrimSpace(mode))
	}
	if command.Action == ActionIrrigationOn && commandSucceeded(command.Status) {
		duration := integerPayload(payload, "durationSeconds")
		result.RemainingSeconds = remainingSeconds(command, duration, s.now())
		if duration == 0 || result.RemainingSeconds > 0 {
			result.State = "ON"
		}
	}
	return result, nil
}

func (s *Service) Command(ctx context.Context, ownerID uint64, commandID string) (*CommandResult, error) {
	commandID = strings.TrimSpace(commandID)
	if ownerID == 0 || commandID == "" || len(commandID) > 64 {
		return nil, ErrInvalidInput
	}
	command, err := s.commands.FindByCommandIDAndOwner(ctx, commandID, ownerID)
	if errors.Is(err, gorm.ErrRecordNotFound) || command == nil {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find command: %w", err)
	}
	payload, err := decodePayload(command.ParametersJSON)
	if err != nil {
		return nil, fmt.Errorf("decode command: %w", err)
	}
	result := &CommandResult{
		ID: command.CommandID, PlotID: command.PlotID, DeviceID: command.DeviceID,
		Action: externalAction(command.Action), Status: command.Status, RequestPayload: payload,
		CreatedAt: command.CreatedAt, AckAt: command.ExecutedAt,
	}
	if commandSucceeded(command.Status) {
		state := "OFF"
		if command.Action == ActionIrrigationOn {
			state = "ON"
		}
		result.AckPayload = map[string]any{"state": state}
		if duration := integerPayload(payload, "durationSeconds"); state == "ON" && duration > 0 {
			result.AckPayload["remainingSeconds"] = duration
		}
	}
	return result, nil
}

func (s *Service) List(ctx context.Context, ownerID uint64, filter ListFilter) (ListResult, error) {
	if filter.Page == 0 {
		filter.Page = 1
	}
	if filter.PageSize == 0 {
		filter.PageSize = 20
	}
	if ownerID == 0 || filter.Page < 1 || filter.PageSize < 1 || filter.PageSize > 100 ||
		filter.PlotID != nil && *filter.PlotID == 0 || filter.Status != nil && !ValidStatus(*filter.Status) {
		return ListResult{}, ErrInvalidInput
	}
	rows, total, err := s.commands.ListByOwner(ctx, ownerID, filter)
	if err != nil {
		return ListResult{}, fmt.Errorf("list commands: %w", err)
	}
	items := make([]ListItem, 0, len(rows))
	for _, row := range rows {
		payload, err := decodePayload(row.ParametersJSON)
		if err != nil {
			return ListResult{}, fmt.Errorf("decode command %s: %w", row.CommandID, err)
		}
		items = append(items, ListItem{
			ID: row.CommandID, PlotCode: row.PlotCode, Action: externalAction(row.Action),
			DurationSeconds: integerPayload(payload, "durationSeconds"), Status: row.Status,
			OperatorName: row.OperatorName, CreatedAt: row.CreatedAt,
		})
	}
	return ListResult{Items: items, Page: filter.Page, PageSize: filter.PageSize, Total: total}, nil
}

func ValidStatus(status Status) bool {
	switch status {
	case StatusPending, StatusRejected, StatusSent, StatusAcknowledged, StatusSucceeded, StatusFailed, StatusTimeout, StatusExpired:
		return true
	default:
		return false
	}
}

func validIssueInput(input IssueInput) bool {
	if input.Action != "OPEN" && input.Action != "CLOSE" ||
		input.Mode != "MANUAL" && input.Mode != "AUTO" && input.Mode != "AI_SUGGESTED" ||
		input.IdempotencyKey == "" || len(input.IdempotencyKey) > 64 || len(input.Reason) > 500 {
		return false
	}
	if input.Action == "OPEN" {
		return input.DurationSeconds >= 60 && input.DurationSeconds <= MaxIrrigationSeconds
	}
	return input.DurationSeconds == 0
}

func issueResult(command *Command) *IssueResult {
	status := string(command.Status)
	if command.Status == StatusSucceeded {
		status = "SUCCESS"
	}
	return &IssueResult{
		CommandID: command.CommandID, PlotID: command.PlotID,
		Action: externalAction(command.Action), Status: status, CreatedAt: command.CreatedAt,
	}
}

func internalAction(action string) Action {
	if action == "OPEN" {
		return ActionIrrigationOn
	}
	return ActionIrrigationOff
}

func newCommandID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate command id: %w", err)
	}
	return "cmd_" + hex.EncodeToString(bytes), nil
}

func decodePayload(value []byte) (map[string]any, error) {
	if len(value) == 0 {
		return map[string]any{}, nil
	}
	var result map[string]any
	if err := json.Unmarshal(value, &result); err != nil {
		return nil, err
	}
	if result == nil {
		result = map[string]any{}
	}
	return result, nil
}

func integerPayload(payload map[string]any, key string) int {
	value, ok := payload[key].(float64)
	if !ok || value <= 0 || value > float64(^uint(0)>>1) {
		return 0
	}
	return int(value)
}

func remainingSeconds(command *Command, duration int, now time.Time) int {
	if duration <= 0 {
		return 0
	}
	startedAt := command.CreatedAt
	if command.ExecutedAt != nil {
		startedAt = *command.ExecutedAt
	}
	remaining := duration - int(now.Sub(startedAt).Seconds())
	if remaining < 0 {
		return 0
	}
	if remaining > duration {
		return duration
	}
	return remaining
}

func commandSucceeded(status Status) bool {
	return status == StatusAcknowledged || status == StatusSucceeded
}

func externalAction(action Action) string {
	switch action {
	case ActionIrrigationOn:
		return "OPEN"
	case ActionIrrigationOff:
		return "CLOSE"
	default:
		return string(action)
	}
}
