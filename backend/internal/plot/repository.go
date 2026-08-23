package plot

import (
	"context"

	"github.com/chenluuo/smart-project/backend/internal/shared/persistence"
	"gorm.io/gorm"
)

type Repositories struct {
	Plots *persistence.Repository[Plot]
	db    *gorm.DB
}

func NewRepositories(db *gorm.DB) Repositories {
	return Repositories{Plots: persistence.NewRepository[Plot](db), db: db}
}

func (r Repositories) FindByOwner(ctx context.Context, ownerID uint64) ([]Plot, error) {
	var plots []Plot
	err := r.db.WithContext(ctx).Where("owner_id = ?", ownerID).Order("code ASC, id ASC").Find(&plots).Error
	return plots, err
}

func (r Repositories) FindByIDAndOwner(ctx context.Context, id, ownerID uint64) (*Plot, error) {
	var result Plot
	if err := r.db.WithContext(ctx).Where("id = ? AND owner_id = ?", id, ownerID).First(&result).Error; err != nil {
		return nil, err
	}
	return &result, nil
}
