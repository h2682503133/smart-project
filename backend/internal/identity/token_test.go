package identity

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTokenManagerIssueAndParse(t *testing.T) {
	manager, err := NewTokenManager(strings.Repeat("s", 32), "test-api", time.Hour)
	if err != nil {
		t.Fatalf("NewTokenManager() error = %v", err)
	}
	fixedNow := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return fixedNow }

	token, expiresIn, err := manager.Issue(42, "grower", "FARMER")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if expiresIn != 3600 {
		t.Fatalf("expiresIn = %d, want 3600", expiresIn)
	}
	claims, err := manager.Parse(token)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if claims.UserID != 42 || claims.AccountName != "grower" || claims.Subject != "42" || claims.Role != "FARMER" {
		t.Fatalf("unexpected claims: %+v", claims)
	}
}

func TestTokenManagerRejectsTamperedAndExpiredTokens(t *testing.T) {
	manager, err := NewTokenManager(strings.Repeat("s", 32), "test-api", time.Minute)
	if err != nil {
		t.Fatalf("NewTokenManager() error = %v", err)
	}
	fixedNow := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return fixedNow }
	token, _, err := manager.Issue(42, "grower", "FARMER")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	tampered := token[:len(token)-1] + "A"
	if _, err := manager.Parse(tampered); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("Parse(tampered) error = %v, want ErrInvalidToken", err)
	}
	manager.now = func() time.Time { return fixedNow.Add(time.Minute) }
	if _, err := manager.Parse(token); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("Parse(expired) error = %v, want ErrInvalidToken", err)
	}
}
