package identity

import (
	"context"
	"errors"

	"github.com/chenluuo/smart-project/backend/internal/shared/persistence"
	drivermysql "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

type Repositories struct {
	Users *persistence.Repository[User]
	Roles *persistence.Repository[Role]
	db    *gorm.DB
}

func NewRepositories(db *gorm.DB) Repositories {
	return Repositories{Users: persistence.NewRepository[User](db), Roles: persistence.NewRepository[Role](db), db: db}
}

func (r Repositories) FindUserByMobile(ctx context.Context, mobile string) (*User, error) {
	var user User
	if err := r.db.WithContext(ctx).Where("mobile = ?", mobile).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return &user, nil
}

func (r Repositories) FindUserByAccountName(ctx context.Context, accountName string) (*User, error) {
	var user User
	if err := r.db.WithContext(ctx).Where("name = ?", accountName).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return &user, nil
}

func (r Repositories) FindUserByID(ctx context.Context, userID uint64) (*User, error) {
	var user User
	if err := r.db.WithContext(ctx).First(&user, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return &user, nil
}

func (r Repositories) CreateUser(ctx context.Context, user *User) error {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(user).Error; err != nil {
			return err
		}
		var role Role
		if err := tx.Where("role_code = ?", "FARMER").First(&role).Error; err != nil {
			return err
		}
		return tx.Create(&UserRole{UserID: user.ID, RoleID: role.ID}).Error
	})
	if err != nil {
		var mysqlErr *drivermysql.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
			return ErrUserConflict
		}
		return err
	}
	return nil
}

func (r Repositories) FindRoleCodesByUserID(ctx context.Context, userID uint64) ([]string, error) {
	var codes []string
	err := r.db.WithContext(ctx).
		Table("roles").
		Select("roles.role_code").
		Joins("JOIN user_roles ON user_roles.role_id = roles.id").
		Where("user_roles.user_id = ?", userID).
		Order("roles.id").
		Scan(&codes).Error
	return codes, err
}

func (r Repositories) FindRoleByCode(ctx context.Context, code string) (*Role, error) {
	var role Role
	if err := r.db.WithContext(ctx).Where("role_code = ?", code).First(&role).Error; err != nil {
		return nil, err
	}
	return &role, nil
}
