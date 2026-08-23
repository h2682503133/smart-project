package identity

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrUserNotFound       = errors.New("user not found")
	ErrUserConflict       = errors.New("username or mobile already exists")
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrUserDisabled       = errors.New("user is disabled")
	ErrInvalidAccountName = errors.New("username must contain 3 to 64 non-whitespace characters")
	ErrInvalidMobile      = errors.New("mobile must be 6 to 15 digits and may start with +")
	ErrInvalidPassword    = errors.New("password must contain 8 to 72 bytes")
)

var mobilePattern = regexp.MustCompile(`^\+?[1-9][0-9]{5,14}$`)

type UserStore interface {
	FindUserByAccountName(context.Context, string) (*User, error)
	FindUserByMobile(context.Context, string) (*User, error)
	FindUserByID(context.Context, uint64) (*User, error)
	CreateUser(context.Context, *User) error
	FindRoleCodesByUserID(context.Context, uint64) ([]string, error)
}

type RegisterInput struct {
	Mobile      string
	AccountName string
	Password    string
}

type LoginResult struct {
	AccessToken string
	ExpiresIn   int64
	User        *User
	Role        string
}

type CurrentUserResult struct {
	User *User
	Role string
}

type AuthService struct {
	users  UserStore
	tokens *TokenManager
}

func NewAuthService(users UserStore, tokens *TokenManager) *AuthService {
	return &AuthService{users: users, tokens: tokens}
}

func (s *AuthService) Register(ctx context.Context, input RegisterInput) (*User, error) {
	input.AccountName = strings.TrimSpace(input.AccountName)
	input.Mobile = strings.TrimSpace(input.Mobile)
	if !validAccountName(input.AccountName) {
		return nil, ErrInvalidAccountName
	}
	if !mobilePattern.MatchString(input.Mobile) {
		return nil, ErrInvalidMobile
	}
	if len(input.Password) < 8 || len(input.Password) > 72 {
		return nil, ErrInvalidPassword
	}

	if _, err := s.users.FindUserByAccountName(ctx, input.AccountName); err == nil {
		return nil, ErrUserConflict
	} else if !errors.Is(err, ErrUserNotFound) {
		return nil, fmt.Errorf("check account name: %w", err)
	}
	if _, err := s.users.FindUserByMobile(ctx, input.Mobile); err == nil {
		return nil, ErrUserConflict
	} else if !errors.Is(err, ErrUserNotFound) {
		return nil, fmt.Errorf("check mobile: %w", err)
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}
	user := &User{
		AccountName:  input.AccountName,
		Mobile:       input.Mobile,
		PasswordHash: string(passwordHash),
		Status:       UserStatusActive,
	}
	if err := s.users.CreateUser(ctx, user); err != nil {
		if errors.Is(err, ErrUserConflict) {
			return nil, err
		}
		return nil, fmt.Errorf("create user: %w", err)
	}
	return user, nil
}

func (s *AuthService) Login(ctx context.Context, accountName, password string) (*LoginResult, error) {
	user, err := s.users.FindUserByAccountName(ctx, strings.TrimSpace(accountName))
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return nil, ErrInvalidCredentials
		}
		return nil, fmt.Errorf("find user: %w", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return nil, ErrInvalidCredentials
	}
	if user.Status != UserStatusActive {
		return nil, ErrUserDisabled
	}
	roles, err := s.users.FindRoleCodesByUserID(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("find user roles: %w", err)
	}
	if len(roles) == 0 {
		return nil, errors.New("user has no assigned role")
	}

	token, expiresIn, err := s.tokens.Issue(user.ID, user.AccountName, roles[0])
	if err != nil {
		return nil, fmt.Errorf("issue token: %w", err)
	}
	return &LoginResult{AccessToken: token, ExpiresIn: expiresIn, User: user, Role: roles[0]}, nil
}

func (s *AuthService) Authenticate(ctx context.Context, token string) (Claims, error) {
	claims, err := s.tokens.Parse(token)
	if err != nil {
		return Claims{}, err
	}
	user, err := s.users.FindUserByID(ctx, claims.UserID)
	if errors.Is(err, ErrUserNotFound) {
		return Claims{}, ErrInvalidToken
	}
	if err != nil {
		return Claims{}, fmt.Errorf("verify token user: %w", err)
	}
	if user.Status != UserStatusActive {
		return Claims{}, ErrInvalidToken
	}
	return claims, nil
}

func (s *AuthService) CurrentUser(ctx context.Context, userID uint64) (*CurrentUserResult, error) {
	if userID == 0 {
		return nil, ErrUserNotFound
	}
	user, err := s.users.FindUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	roles, err := s.users.FindRoleCodesByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("find user roles: %w", err)
	}
	if len(roles) == 0 {
		return nil, errors.New("user has no assigned role")
	}
	return &CurrentUserResult{User: user, Role: roles[0]}, nil
}

func validAccountName(value string) bool {
	length := utf8.RuneCountInString(value)
	if length < 3 || length > 64 {
		return false
	}
	for _, r := range value {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return true
}
