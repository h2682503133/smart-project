package mqttclient

import (
	"context"
	"strings"

	"github.com/chenluuo/smart-project/backend/internal/telemetry"
	"gorm.io/gorm"
)

type GormSourceResolver struct {
	db *gorm.DB
}

func NewGormSourceResolver(db *gorm.DB) *GormSourceResolver {
	return &GormSourceResolver{db: db}
}

func (r *GormSourceResolver) ResolveTelemetrySource(ctx context.Context, ownerID uint64, deviceSN string) (telemetry.TrustedSource, error) {
	var source telemetry.TrustedSource
	err := r.db.WithContext(ctx).Table("devices AS d").
		Select("p.owner_id AS owner_id, p.id AS plot_id, p.code AS plot_code, d.id AS device_id").
		Joins("JOIN device_bindings AS b ON b.device_id = d.id AND b.unbound_at IS NULL").
		Joins("JOIN plots AS p ON p.id = b.plot_id").
		Where("p.owner_id = ? AND d.serial_no = ?", ownerID, strings.TrimSpace(deviceSN)).
		Take(&source).Error
	return source, err
}
