package identity

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type memoryUserStore struct {
	users []*User
}

func (s *memoryUserStore) FindUserByAccountName(_ context.Context, accountName string) (*User, error) {
	for _, user := range s.users {
		if user.AccountName == accountName {
			return user, nil
		}
	}
	return nil, ErrUserNotFound
}

func (s *memoryUserStore) FindUserByMobile(_ context.Context, mobile string) (*User, error) {
	for _, user := range s.users {
		if user.Mobile == mobile {
			return user, nil
		}
	}
	return nil, ErrUserNotFound
}

func (s *memoryUserStore) FindUserByID(_ context.Context, userID uint64) (*User, error) {
	for _, user := range s.users {
		if user.ID == userID {
			return user, nil
		}
	}
	return nil, ErrUserNotFound
}

func (s *memoryUserStore) CreateUser(_ context.Context, user *User) error {
	user.ID = uint64(len(s.users) + 1)
	s.users = append(s.users, user)
	return nil
}

func (s *memoryUserStore) FindRoleCodesByUserID(_ context.Context, userID uint64) ([]string, error) {
	for _, user := range s.users {
		if user.ID == userID {
			return []string{"FARMER"}, nil
		}
	}
	return nil, ErrUserNotFound
}

func TestAuthServiceRegisterAndLogin(t *testing.T) {
	store := &memoryUserStore{}
	tokens, err := NewTokenManager(strings.Repeat("s", 32), "test-api", time.Hour)
	if err != nil {
		t.Fatalf("NewTokenManager() error = %v", err)
	}
	service := NewAuthService(store, tokens)

	user, err := service.Register(context.Background(), RegisterInput{
		Mobile: "+8613812345678", AccountName: "grower", Password: "strong-password",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if user.ID != 1 || user.Status != UserStatusActive {
		t.Fatalf("unexpected user: %+v", user)
	}
	if user.PasswordHash == "strong-password" || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte("strong-password")) != nil {
		t.Fatal("password was not stored as a valid bcrypt hash")
	}

	result, err := service.Login(context.Background(), "grower", "strong-password")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if result.AccessToken == "" || result.ExpiresIn != 3600 || result.User.ID != user.ID || result.Role != "FARMER" {
		t.Fatalf("unexpected login result: %+v", result)
	}
	if _, err := service.Authenticate(context.Background(), result.AccessToken); err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
}

func TestAuthenticateRejectsDisabledUserToken(t *testing.T) {
	store := &memoryUserStore{}
	tokens, err := NewTokenManager(strings.Repeat("s", 32), "test-api", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	service := NewAuthService(store, tokens)
	user, err := service.Register(context.Background(), RegisterInput{
		Mobile: "13812345678", AccountName: "grower", Password: "strong-password",
	})
	if err != nil {
		t.Fatal(err)
	}
	login, err := service.Login(context.Background(), user.AccountName, "strong-password")
	if err != nil {
		t.Fatal(err)
	}
	user.Status = UserStatusDisabled
	if _, err := service.Authenticate(context.Background(), login.AccessToken); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("Authenticate() error = %v, want ErrInvalidToken", err)
	}
}

func TestAuthServiceRejectsDuplicateAndInvalidCredentials(t *testing.T) {
	store := &memoryUserStore{}
	tokens, _ := NewTokenManager(strings.Repeat("s", 32), "test-api", time.Hour)
	service := NewAuthService(store, tokens)
	input := RegisterInput{Mobile: "13812345678", AccountName: "grower", Password: "strong-password"}
	if _, err := service.Register(context.Background(), input); err != nil {
		t.Fatalf("first Register() error = %v", err)
	}
	if _, err := service.Register(context.Background(), input); !errors.Is(err, ErrUserConflict) {
		t.Fatalf("duplicate Register() error = %v, want ErrUserConflict", err)
	}
	if _, err := service.Login(context.Background(), "grower", "wrong-password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login() error = %v, want ErrInvalidCredentials", err)
	}
}

func TestAuthServiceCurrentUser(t *testing.T) {
	style := "plain"
	store := &memoryUserStore{users: []*User{{ID: 7, AccountName: "grower", InteractionStyle: &style}}}
	service := NewAuthService(store, nil)
	result, err := service.CurrentUser(context.Background(), 7)
	if err != nil {
		t.Fatalf("CurrentUser() error = %v", err)
	}
	if result.User.ID != 7 || result.Role != "FARMER" || result.User.InteractionStyle == nil || *result.User.InteractionStyle != "plain" {
		t.Fatalf("unexpected current user: %+v", result)
	}
}
