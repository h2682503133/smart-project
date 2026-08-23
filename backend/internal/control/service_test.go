package control

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chenluuo/smart-project/backend/internal/device"
	"github.com/chenluuo/smart-project/backend/internal/events"
	"github.com/chenluuo/smart-project/backend/internal/shared/persistence"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type storeStub struct {
	irrigationDevice *IrrigationDevice
	latest           *Command
	command          *Command
	rows             []CommandListRow
	total            int64
	irrigationErr    error
	latestErr        error
	commandErr       error
	listErr          error
	idempotencyErr   error
	createErr        error
	saveErr          error
	ownerID          uint64
	plotID           uint64
	deviceID         uint64
	commandID        string
	filter           ListFilter
	created          *Command
	saved            *Command
	idempotent       *Command
}

func (s *storeStub) FindByIdempotencyKeyAndOwner(_ context.Context, key string, ownerID uint64) (*Command, error) {
	s.ownerID, s.commandID = ownerID, key
	return s.idempotent, s.idempotencyErr
}

func (s *storeStub) Create(_ context.Context, command *Command) error {
	s.created = command
	return s.createErr
}

func (s *storeStub) Save(_ context.Context, command *Command) error {
	copy := *command
	s.saved = &copy
	return s.saveErr
}

func (s *storeStub) FindIrrigationDevice(_ context.Context, ownerID, plotID uint64) (*IrrigationDevice, error) {
	s.ownerID, s.plotID = ownerID, plotID
	return s.irrigationDevice, s.irrigationErr
}

func (s *storeStub) FindLatestSuccessfulByDeviceAndPlot(_ context.Context, deviceID, plotID uint64) (*Command, error) {
	s.deviceID, s.plotID = deviceID, plotID
	return s.latest, s.latestErr
}

func (s *storeStub) FindByCommandIDAndOwner(_ context.Context, commandID string, ownerID uint64) (*Command, error) {
	s.commandID, s.ownerID = commandID, ownerID
	return s.command, s.commandErr
}

func (s *storeStub) ListByOwner(_ context.Context, ownerID uint64, filter ListFilter) ([]CommandListRow, int64, error) {
	s.ownerID, s.filter = ownerID, filter
	return s.rows, s.total, s.listErr
}

func TestIrrigationStatusUsesLatestSuccessfulCommand(t *testing.T) {
	ackAt := time.Date(2026, 8, 22, 8, 20, 0, 0, time.UTC)
	store := &storeStub{
		irrigationDevice: &IrrigationDevice{DeviceID: 3, PlotID: 11},
		latest: &Command{
			CommandID: "cmd-1", DeviceID: 3, PlotID: 11, Action: ActionIrrigationOn,
			Status: StatusSucceeded, ParametersJSON: datatypes.JSON(`{"durationSeconds":600,"mode":"auto"}`),
			ExecutedAt: &ackAt,
		},
	}
	service := NewService(store)
	service.now = func() time.Time { return ackAt.Add(2 * time.Minute) }

	result, err := service.IrrigationStatus(context.Background(), 7, 11)
	if err != nil {
		t.Fatal(err)
	}
	if store.ownerID != 7 || store.deviceID != 3 || store.plotID != 11 {
		t.Fatalf("store arguments: owner=%d device=%d plot=%d", store.ownerID, store.deviceID, store.plotID)
	}
	if result.State != "ON" || result.Mode != "AUTO" || result.RemainingSeconds != 480 || result.LastCommandID == nil || *result.LastCommandID != "cmd-1" {
		t.Fatalf("result = %+v", result)
	}
}

func TestIssueOpenCommandCompletesBeforeReturning(t *testing.T) {
	now := time.Date(2026, 8, 22, 8, 21, 10, 0, time.UTC)
	store := &storeStub{
		irrigationDevice: &IrrigationDevice{DeviceID: 3, PlotID: 11, Status: device.StatusOnline},
		idempotencyErr:   gorm.ErrRecordNotFound,
	}
	broker := events.NewBroker(10)
	subscription := broker.Subscribe(7, "")
	defer subscription.Close()
	service := NewService(store, broker)
	service.now = func() time.Time { return now }
	result, err := service.Issue(context.Background(), 7, 11, IssueInput{
		Action: " open ", DurationSeconds: 600, Mode: "manual", Reason: "土壤湿度低", IdempotencyKey: "request-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "SUCCESS" || result.Action != "OPEN" || result.PlotID != 11 || result.CommandID == "" {
		t.Fatalf("result = %+v", result)
	}
	if store.created == nil || store.saved == nil || store.created.DeviceID != 3 || store.created.IssuedBy != 7 ||
		store.saved.Status != StatusSucceeded || store.saved.ExecutedAt == nil || store.created.IdempotencyKey != "request-1" {
		t.Fatalf("created=%+v saved=%+v", store.created, store.saved)
	}
	payload, err := decodePayload(store.created.ParametersJSON)
	if err != nil || integerPayload(payload, "durationSeconds") != 600 || payload["mode"] != "MANUAL" {
		t.Fatalf("payload=%v err=%v", payload, err)
	}
	select {
	case event := <-subscription.Events:
		if event.Type != events.TypeCommandResult || event.Payload["status"] != string(StatusSucceeded) || event.Payload["plotId"] != uint64(11) {
			t.Fatalf("published event = %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for command.result")
	}
}

func TestIssueCommandValidationAndDeviceState(t *testing.T) {
	tests := []struct {
		name  string
		store *storeStub
		input IssueInput
		want  error
	}{
		{name: "missing idempotency key", store: &storeStub{}, input: IssueInput{Action: "CLOSE", Mode: "MANUAL"}, want: ErrInvalidInput},
		{name: "short open duration", store: &storeStub{}, input: IssueInput{Action: "OPEN", DurationSeconds: 59, Mode: "MANUAL", IdempotencyKey: "request-1"}, want: ErrInvalidInput},
		{name: "close with duration", store: &storeStub{}, input: IssueInput{Action: "CLOSE", DurationSeconds: 60, Mode: "MANUAL", IdempotencyKey: "request-1"}, want: ErrInvalidInput},
		{name: "missing valve", store: &storeStub{irrigationErr: gorm.ErrRecordNotFound}, input: IssueInput{Action: "CLOSE", Mode: "MANUAL", IdempotencyKey: "request-1"}, want: ErrNotFound},
		{name: "offline valve", store: &storeStub{irrigationDevice: &IrrigationDevice{DeviceID: 3, PlotID: 11, Status: device.StatusOffline}}, input: IssueInput{Action: "CLOSE", Mode: "MANUAL", IdempotencyKey: "request-1"}, want: ErrDeviceOffline},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewService(tt.store).Issue(context.Background(), 7, 11, tt.input)
			if !errors.Is(err, tt.want) {
				t.Fatalf("error=%v, want %v", err, tt.want)
			}
		})
	}
}

func TestIssueCommandIsIdempotent(t *testing.T) {
	createdAt := time.Date(2026, 8, 22, 8, 21, 10, 0, time.UTC)
	store := &storeStub{idempotent: &Command{
		CommandID: "cmd-existing", PlotID: 11, Action: ActionIrrigationOn, Status: StatusSucceeded,
		Auditable: structAuditable(createdAt),
	}}
	result, err := NewService(store).Issue(context.Background(), 7, 11, IssueInput{
		Action: "OPEN", DurationSeconds: 600, Mode: "MANUAL", IdempotencyKey: "request-1",
	})
	if err != nil || result.CommandID != "cmd-existing" || store.created != nil {
		t.Fatalf("result=%+v created=%+v err=%v", result, store.created, err)
	}
}

func TestIrrigationStatusWithoutCommandsDefaultsToOff(t *testing.T) {
	store := &storeStub{irrigationDevice: &IrrigationDevice{DeviceID: 3, PlotID: 11}, latestErr: gorm.ErrRecordNotFound}
	result, err := NewService(store).IrrigationStatus(context.Background(), 7, 11)
	if err != nil || result.State != "OFF" || result.RemainingSeconds != 0 || result.LastCommandID != nil {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestIrrigationStatusHidesUnownedOrMissingPlot(t *testing.T) {
	store := &storeStub{irrigationErr: gorm.ErrRecordNotFound}
	_, err := NewService(store).IrrigationStatus(context.Background(), 7, 11)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v", err)
	}
}

func TestCommandResultMapsStoredCommand(t *testing.T) {
	createdAt := time.Date(2026, 8, 22, 8, 20, 0, 0, time.UTC)
	ackAt := createdAt.Add(2 * time.Second)
	store := &storeStub{command: &Command{
		CommandID: "cmd-1", DeviceID: 3, PlotID: 11, Action: ActionIrrigationOn, Status: StatusSucceeded,
		ParametersJSON: datatypes.JSON(`{"durationSeconds":600}`), ExecutedAt: &ackAt,
		Auditable: structAuditable(createdAt),
	}}
	result, err := NewService(store).Command(context.Background(), 7, " cmd-1 ")
	if err != nil {
		t.Fatal(err)
	}
	if store.ownerID != 7 || store.commandID != "cmd-1" || result.Action != "OPEN" || result.AckAt != &ackAt {
		t.Fatalf("store=%+v result=%+v", store, result)
	}
	if result.RequestPayload["durationSeconds"] != float64(600) || result.AckPayload["remainingSeconds"] != 600 {
		t.Fatalf("payloads: request=%v ack=%v", result.RequestPayload, result.AckPayload)
	}
}

func TestCommandListDefaultsPaginationAndMapsItems(t *testing.T) {
	createdAt := time.Date(2026, 8, 22, 8, 20, 0, 0, time.UTC)
	store := &storeStub{total: 1, rows: []CommandListRow{{
		Command: Command{CommandID: "cmd-1", Action: ActionIrrigationOff, Status: StatusSucceeded,
			ParametersJSON: datatypes.JSON(`{}`), Auditable: structAuditable(createdAt)},
		PlotCode: "A3", OperatorName: "张三",
	}}}
	result, err := NewService(store).List(context.Background(), 7, ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if store.filter.Page != 1 || store.filter.PageSize != 20 || result.Total != 1 || len(result.Items) != 1 || result.Items[0].Action != "CLOSE" {
		t.Fatalf("filter=%+v result=%+v", store.filter, result)
	}
}

func structAuditable(createdAt time.Time) persistence.Auditable {
	return persistence.Auditable{CreatedAt: createdAt}
}
