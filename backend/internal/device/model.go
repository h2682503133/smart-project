package device

import (
	"time"

	"github.com/chenluuo/smart-project/backend/internal/shared/persistence"
)

type Status string
type CredentialStatus string

const (
	StatusUnactivated  Status           = "UNACTIVATED"
	StatusOnline       Status           = "ONLINE"
	StatusOffline      Status           = "OFFLINE"
	StatusReconnecting Status           = "RECONNECTING"
	StatusFault        Status           = "FAULT"
	StatusDisabled     Status           = "DISABLED"
	CredentialPending  CredentialStatus = "PENDING"
	CredentialActive   CredentialStatus = "ACTIVE"
	CredentialRevoked  CredentialStatus = "REVOKED"
)

type Device struct {
	ID               uint64           `json:"id" gorm:"primaryKey;autoIncrement"`
	DeviceCode       string           `json:"deviceCode" gorm:"column:device_code;size:64;not null;uniqueIndex:uk_devices_code"`
	SerialNo         string           `json:"serialNo" gorm:"column:serial_no;size:128;not null;uniqueIndex:uk_devices_serial"`
	Name             string           `json:"name" gorm:"size:128;not null"`
	DeviceType       string           `json:"deviceType" gorm:"column:device_type;size:64;not null"`
	Model            *string          `json:"model,omitempty" gorm:"size:64"`
	Status           Status           `json:"status" gorm:"size:32;not null"`
	Battery          *int             `json:"battery,omitempty"`
	Signal           *int             `json:"signal,omitempty"`
	FirmwareVersion  *string          `json:"firmwareVersion,omitempty" gorm:"column:firmware_version;size:64"`
	StatusMessage    *string          `json:"statusMessage,omitempty" gorm:"column:status_message;size:255"`
	CredentialStatus CredentialStatus `json:"credentialStatus" gorm:"column:credential_status;size:32;not null"`
	ActivatedAt      *time.Time       `json:"activatedAt,omitempty" gorm:"column:activated_at"`
	LastSeenAt       *time.Time       `json:"lastSeenAt,omitempty" gorm:"column:last_seen_at;index:idx_devices_status_last_seen,priority:2"`
	persistence.Auditable
}

func (Device) TableName() string { return "devices" }

type Binding struct {
	ID        uint64     `json:"id" gorm:"primaryKey;autoIncrement"`
	DeviceID  uint64     `json:"deviceId" gorm:"column:device_id;not null"`
	PlotID    uint64     `json:"plotId" gorm:"column:plot_id;not null"`
	BoundBy   uint64     `json:"boundBy" gorm:"column:bound_by;not null;index:idx_device_bindings_bound_by"`
	BoundAt   time.Time  `json:"boundAt" gorm:"column:bound_at;not null"`
	UnboundAt *time.Time `json:"unboundAt,omitempty" gorm:"column:unbound_at"`
}

func (Binding) TableName() string { return "device_bindings" }
